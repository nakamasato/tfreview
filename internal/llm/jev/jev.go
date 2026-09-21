// Package jev asks TypeSafe AI's System One (Jev) a set of yes/no questions about a
// plan and gets back one probability per question.
//
// Jev answers with numbers only. A noul answer carries a single probability and no
// confidence field, and the model never returns prose, so a caller that needs a
// reason for a reviewer has to produce it some other way.
package jev

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://api.typesafe.ai/v1/systemone"
	DefaultModel   = "jev-latest"

	defaultMaxRetries = 4
	defaultTimeout    = 2 * time.Minute
	retryBaseDelay    = time.Second
)

// ErrTooLarge means the request was not sent. Jev's budget is roughly 64k tokens for
// the state plus all questions; past that the call fails, and trimming the state
// further would mean dropping the values a verdict turns on.
var ErrTooLarge = errors.New("request exceeds max_request_chars")

// Criteria bounds a noul question: what makes the proposition true, and what makes it
// false. Either side may be a nested JSON structure rather than a sentence.
type Criteria struct {
	True  any `json:"true,omitempty"`
	False any `json:"false,omitempty"`
}

type Question struct {
	// Type is the question primitive. Only "noul" is used here; choice and score
	// exist in the API but decide severity, which config owns, not the model.
	Type         string    `json:"type"`
	Instructions any       `json:"instructions"`
	Criteria     *Criteria `json:"criteria,omitempty"`
}

// Answer holds a noul probability in Noul. Values near 0.5 mean yes and no are
// about equally supported, not that the model is uncertain in a reportable way.
type Answer struct {
	Type string  `json:"type"`
	Noul float64 `json:"noul"`
}

type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
}

type Options struct {
	APIKey  string
	Model   string
	BaseURL string
	// MaxRequestChars caps the marshalled request body. 0 disables the check.
	MaxRequestChars int
	MaxRetries      int
	HTTP            *http.Client
}

type Client struct{ opts Options }

func New(opts Options) *Client {
	if opts.Model == "" {
		opts.Model = DefaultModel
	}
	if opts.BaseURL == "" {
		opts.BaseURL = DefaultBaseURL
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = defaultMaxRetries
	}
	if opts.HTTP == nil {
		opts.HTTP = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{opts: opts}
}

func (c *Client) Model() string { return c.opts.Model }

type request struct {
	Model     string              `json:"model"`
	State     any                 `json:"state"`
	Questions map[string]Question `json:"questions"`
}

func (c *Client) Ask(ctx context.Context, state any, questions map[string]Question) (*Response, error) {
	body, err := json.Marshal(request{Model: c.opts.Model, State: state, Questions: questions})
	if err != nil {
		return nil, err
	}
	if m := c.opts.MaxRequestChars; m > 0 && len(body) > m {
		return nil, fmt.Errorf("%w: %d > %d chars", ErrTooLarge, len(body), m)
	}

	var lastErr error
	for attempt := 0; ; attempt++ {
		resp, err := c.post(ctx, body)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !retryable(err) || attempt >= c.opts.MaxRetries {
			return nil, lastErr
		}
		// Jitter so concurrent per-change calls don't all come back at the same moment.
		delay := retryBaseDelay << attempt
		delay += time.Duration(rand.Int63n(int64(delay)))
		select {
		case <-ctx.Done():
			return nil, errors.Join(ctx.Err(), lastErr)
		case <-time.After(delay):
		}
	}
}

func (c *Client) post(ctx context.Context, body []byte) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.opts.BaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.opts.APIKey)
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.opts.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = httpResp.Body.Close() }()

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode != http.StatusOK {
		return nil, &statusError{Code: httpResp.StatusCode, Body: string(raw)}
	}
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, &parseError{err: err}
	}
	return &resp, nil
}

type parseError struct{ err error }

func (e *parseError) Error() string { return "jev: parse response: " + e.err.Error() }
func (e *parseError) Unwrap() error { return e.err }

type statusError struct {
	Code int
	Body string
}

func (e *statusError) Error() string {
	body := strings.TrimSpace(e.Body)
	if len(body) > 300 {
		body = body[:300] + "..."
	}
	return fmt.Sprintf("jev: http %d: %s", e.Code, body)
}

// retryable covers what the API documents as retryable — 429 and 529 — plus the rest
// of 5xx and transport errors, which are the same "try again" case from here.
func retryable(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// An untrusted certificate is a configuration problem, and retrying it burns the
	// whole backoff budget on every change in the plan before reporting the real cause.
	var cve *tls.CertificateVerificationError
	if errors.As(err, &cve) {
		return false
	}
	var se *statusError
	if errors.As(err, &se) {
		return se.Code == http.StatusTooManyRequests || se.Code >= 500
	}
	// A parse failure means a body arrived and was wrong; retrying won't fix it.
	var pe *parseError
	return !errors.As(err, &pe)
}
