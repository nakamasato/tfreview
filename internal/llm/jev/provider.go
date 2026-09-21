package jev

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/nakamasato/tfreview/internal/llm"
	"github.com/nakamasato/tfreview/internal/match"
	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
)

// DefaultMaxRequestChars is set from the documented budget of roughly 64k tokens for
// the state plus every question. Measured against real plans a compact request runs
// about three characters per token, and this leaves headroom below that.
const DefaultMaxRequestChars = 150_000

// ProviderOptions adds what a whole-plan pass needs on top of one call's Options.
type ProviderOptions struct {
	Options
	HitThreshold  float64
	MissThreshold float64
	MaxValueChars int
	Concurrency   int
}

// Provider scores every change in a plan and reduces the scores to one verdict per
// check. It is the broad first pass: each change is judged on its own, so a plan of a
// hundred changes costs a hundred small calls rather than one call holding everything.
type Provider struct {
	client *Client
	opts   ProviderOptions
}

func NewProvider(opts ProviderOptions) *Provider {
	if opts.MaxRequestChars == 0 {
		opts.MaxRequestChars = DefaultMaxRequestChars
	}
	if opts.Concurrency < 1 {
		opts.Concurrency = 1
	}
	return &Provider{client: New(opts.Options), opts: opts}
}

func (p *Provider) Name() string  { return "jev" }
func (p *Provider) Model() string { return p.client.Model() }

// scored is one change's answer for one check.
type scored struct {
	score   float64
	address string
	err     error
}

func (p *Provider) Judge(ctx context.Context, req llm.Request) ([]llm.Answer, llm.Usage, error) {
	questions := Questions(req.Checks)
	if len(questions) == 0 || !req.Plan.HasChanges() {
		return nil, llm.Usage{}, nil
	}
	results := make([]map[string]scored, len(req.Plan.Resources))
	usages := make([]llm.Usage, len(req.Plan.Resources))

	sem := make(chan struct{}, p.opts.Concurrency)
	var wg sync.WaitGroup
	for i := range req.Plan.Resources {
		asked := applicable(questions, req.Checks, req.Plan, i)
		if len(asked) == 0 {
			continue
		}
		wg.Add(1)
		go func(i int, asked map[string]Question) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i], usages[i] = p.ask(ctx, req.Plan, i, asked)
		}(i, asked)
	}
	wg.Wait()

	var usage llm.Usage
	for _, u := range usages {
		usage.Add(u)
	}
	return p.reduce(req.Checks, results), usage, nil
}

// ask sends one change. A request that does not fit is retried with the other changes
// cut back to their addresses, which is the only part of the state that scales with
// the size of the plan.
func (p *Provider) ask(ctx context.Context, pl *plan.Plan, i int, asked map[string]Question) (map[string]scored, llm.Usage) {
	focused := Focus(asked, i)
	state := BuildState(pl, i, true, p.opts.MaxValueChars)

	resp, err := p.client.Ask(ctx, state, focused)
	if errors.Is(err, ErrTooLarge) {
		resp, err = p.client.Ask(ctx, Headers(state, i), focused)
	}
	out := map[string]scored{}
	if err != nil {
		for id := range asked {
			out[id] = scored{address: pl.Resources[i].Address, err: err}
		}
		return out, llm.Usage{Calls: 1}
	}
	for id := range asked {
		a, ok := resp.Answers[id]
		if !ok {
			out[id] = scored{address: pl.Resources[i].Address, err: fmt.Errorf("no answer for %q", id)}
			continue
		}
		out[id] = scored{score: a.Noul, address: pl.Resources[i].Address}
	}
	return out, llm.Usage{Calls: 1, InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens}
}

// applicable drops the questions whose check has a `match` this change does not satisfy.
// Asking about a resource the check was never meant to cover only invites a distractor.
func applicable(questions map[string]Question, checks []model.Check, pl *plan.Plan, i int) map[string]Question {
	out := map[string]Question{}
	for _, ck := range checks {
		q, ok := questions[ck.ID]
		if !ok {
			continue
		}
		if !ck.Match.IsZero() && !matchesResource(ck, pl, i) {
			continue
		}
		out[ck.ID] = q
	}
	return out
}

func matchesResource(ck model.Check, pl *plan.Plan, i int) bool {
	for _, c := range match.Candidates(ck.Match, pl) {
		if c.Address == pl.Resources[i].Address {
			return true
		}
	}
	return false
}

// reduce takes the highest score per check: one change carrying a risk is enough for the
// check to hit, which is the same max that aggregates aspects and targets elsewhere.
func (p *Provider) reduce(checks []model.Check, results []map[string]scored) []llm.Answer {
	var out []llm.Answer
	for _, ck := range checks {
		if ck.Instructions == "" {
			continue
		}
		var best scored
		var hits, undecided []string
		var failures []string
		asked := false
		for _, byCheck := range results {
			s, ok := byCheck[ck.ID]
			if !ok {
				continue
			}
			asked = true
			if s.err != nil {
				failures = append(failures, s.address)
				continue
			}
			if s.score > best.score || best.address == "" {
				best = s
			}
			switch {
			case s.score >= p.opts.HitThreshold:
				hits = append(hits, fmt.Sprintf("%s (%.2f)", s.address, s.score))
			case s.score > p.opts.MissThreshold:
				undecided = append(undecided, fmt.Sprintf("%s (%.2f)", s.address, s.score))
			}
		}
		if !asked {
			continue
		}
		out = append(out, p.answer(ck, best, hits, undecided, failures))
	}
	return out
}

func (p *Provider) answer(ck model.Check, best scored, hits, undecided, failures []string) llm.Answer {
	a := llm.Answer{CheckID: ck.ID, Score: best.score}
	sort.Strings(hits)
	sort.Strings(undecided)
	switch {
	case len(hits) > 0:
		a.Kind = model.VerdictHit
		a.Reason = "scored at or above the hit threshold: " + strings.Join(hits, ", ")
		a.Resources = addresses(hits)
	case len(undecided) > 0:
		// Between the thresholds nothing is settled. Reporting it as unverifiable rather
		// than a miss is what hands these changes to a closer look instead of burying them.
		a.Kind = model.VerdictUnverifiable
		a.Reason = "scored between the thresholds, needs a closer look: " + strings.Join(undecided, ", ")
		a.Resources = addresses(undecided)
	case len(failures) > 0 && best.address == "":
		a.Kind = model.VerdictSkipped
		a.Reason = "scoring failed for " + strings.Join(failures, ", ")
	default:
		a.Kind = model.VerdictMiss
		a.Reason = fmt.Sprintf("no change scored above %.2f (highest %.2f on %s)",
			p.opts.MissThreshold, best.score, best.address)
	}
	if len(failures) > 0 && a.Kind != model.VerdictSkipped {
		a.Reason += " (not scored: " + strings.Join(failures, ", ") + ")"
	}
	return a
}

// addresses strips the score off the "address (0.42)" form the reason uses, so a later
// pass gets the addresses rather than having to read them back out of prose.
func addresses(scored []string) []string {
	out := make([]string, 0, len(scored))
	for _, s := range scored {
		if i := strings.LastIndex(s, " ("); i > 0 {
			s = s[:i]
		}
		out = append(out, s)
	}
	return out
}
