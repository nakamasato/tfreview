// Package claudecli judges through the local `claude` CLI (Claude Code) instead of
// the Anthropic API, so a run is billed to the subscription and needs no API key.
package claudecli

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/nakamasato/tfreview/internal/llm"
	"github.com/nakamasato/tfreview/internal/llm/anthropic"
)

const timeout = 15 * time.Minute

// `claude -p` has no tool_choice, so the tool contract in BuildSystem is replaced by a
// plain-JSON contract of the same shape, which lets anthropic.ParseAnswers stay shared.
const jsonOnly = `
You have no tools available. Ignore the instruction to use the report_verdicts tool.
Output ONLY a raw JSON object, with no prose and no code fences:
{"verdicts":[{"check_id":"...","verdict":"hit|miss|unverifiable","reason":"..."}]}
`

type Options struct {
	Model        string
	MaxPlanChars int
	Bin          string
}

type Provider struct {
	opts Options

	mu   sync.Mutex
	cost float64
}

func New(opts Options) *Provider {
	if opts.Bin == "" {
		opts.Bin = "claude"
	}
	return &Provider{opts: opts}
}

func (p *Provider) Name() string  { return "claude-cli" }
func (p *Provider) Model() string { return p.opts.Model }

// CostUSD is what the CLI reported for this run. It is list-price equivalent, not an
// amount billed: a subscription run has no per-token charge.
func (p *Provider) CostUSD() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cost
}

type cliResult struct {
	Result    string  `json:"result"`
	IsError   bool    `json:"is_error"`
	TotalCost float64 `json:"total_cost_usd"`
	Usage     struct {
		InputTokens              int64 `json:"input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
	} `json:"usage"`
}

func (p *Provider) Judge(ctx context.Context, req llm.Request) ([]llm.Answer, llm.Usage, error) {
	planJSON := anthropic.PlanJSON(req.Plan)
	if len(planJSON) > p.opts.MaxPlanChars {
		return nil, llm.Usage{}, fmt.Errorf("%w: %d > %d chars", anthropic.ErrPlanTooLarge, len(planJSON), p.opts.MaxPlanChars)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// --setting-sources / --strict-mcp-config / --allowed-tools strip CLAUDE.md, skills
	// and MCP from the session; without them the verdict reflects the caller's local setup.
	cmd := exec.CommandContext(ctx, p.opts.Bin,
		"-p", anthropic.BuildUser(req, planJSON),
		"--output-format", "json",
		"--model", p.opts.Model,
		"--system-prompt", anthropic.BuildSystem(req.Language)+jsonOnly,
		"--setting-sources", "",
		"--strict-mcp-config",
		"--allowed-tools", "",
		"--max-turns", "1",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, llm.Usage{}, fmt.Errorf("claude -p: %w", err)
	}

	var res cliResult
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, llm.Usage{}, fmt.Errorf("parse claude -p output: %w", err)
	}
	p.mu.Lock()
	p.cost += res.TotalCost
	p.mu.Unlock()

	usage := llm.Usage{
		Calls:            1,
		InputTokens:      res.Usage.InputTokens,
		CacheWriteTokens: res.Usage.CacheCreationInputTokens,
		CacheReadTokens:  res.Usage.CacheReadInputTokens,
		OutputTokens:     res.Usage.OutputTokens,
	}
	if res.IsError {
		return nil, usage, fmt.Errorf("claude -p failed: %s", res.Result)
	}
	body, err := jsonObject(res.Result)
	if err != nil {
		return nil, usage, err
	}
	answers, err := anthropic.ParseAnswers(body)
	return answers, usage, err
}

// jsonObject pulls the JSON object out of a text reply, tolerating a code fence or a
// stray sentence around it.
func jsonObject(s string) (json.RawMessage, error) {
	i, j := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if i < 0 || j < i {
		return nil, fmt.Errorf("no JSON object in response: %q", s)
	}
	return json.RawMessage(s[i : j+1]), nil
}
