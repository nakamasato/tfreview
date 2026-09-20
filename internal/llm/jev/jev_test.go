package jev

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestAsk(t *testing.T) {
	var got request
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &got); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"q":{"type":"noul","noul":0.93}},"usage":{"input_tokens":1302,"output_tokens":116}}`))
	}))
	defer srv.Close()

	c := New(Options{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client()})
	resp, err := c.Ask(context.Background(), map[string]any{"target": "prd"}, map[string]Question{
		"q": {Type: "noul", Instructions: "x", Criteria: &Criteria{True: "t", False: "f"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer k" {
		t.Errorf("Authorization = %q", auth)
	}
	if got.Model != DefaultModel {
		t.Errorf("model = %q, want %q", got.Model, DefaultModel)
	}
	if got.Questions["q"].Criteria.False != "f" {
		t.Errorf("criteria not sent: %+v", got.Questions["q"])
	}
	if resp.Answers["q"].Noul != 0.93 {
		t.Errorf("noul = %v", resp.Answers["q"].Noul)
	}
	if resp.Usage.InputTokens != 1302 {
		t.Errorf("input_tokens = %d", resp.Usage.InputTokens)
	}
}

func TestAskRetries(t *testing.T) {
	for _, tc := range []struct {
		name      string
		code      int
		failTimes int
		wantCalls int
		wantErr   bool
	}{
		{name: "429 is retried", code: http.StatusTooManyRequests, failTimes: 1, wantCalls: 2},
		{name: "529 is retried", code: 529, failTimes: 1, wantCalls: 2},
		{name: "400 is not", code: http.StatusBadRequest, failTimes: 99, wantCalls: 1, wantErr: true},
		{name: "retries give up", code: 529, failTimes: 99, wantCalls: 2, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if calls <= tc.failTimes {
					w.WriteHeader(tc.code)
					return
				}
				_, _ = w.Write([]byte(`{"answers":{}}`))
			}))
			defer srv.Close()

			// retryBaseDelay is a second, so one retry is all a unit test can afford.
			c := New(Options{BaseURL: srv.URL, HTTP: srv.Client(), MaxRetries: 1})
			_, err := c.Ask(context.Background(), nil, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if calls != tc.wantCalls {
				t.Errorf("calls = %d, want %d", calls, tc.wantCalls)
			}
		})
	}
}

func TestAskTooLarge(t *testing.T) {
	c := New(Options{BaseURL: "http://127.0.0.1:0", MaxRequestChars: 10})
	_, err := c.Ask(context.Background(), map[string]any{"target": "prd"}, nil)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestAskLive(t *testing.T) {
	if os.Getenv("TFREVIEW_LIVE") == "" {
		t.Skip("set TFREVIEW_LIVE=1 to call the real API")
	}
	c := New(Options{APIKey: os.Getenv("TYPESAFE_API_KEY")})
	p := testPlan()
	resp, err := c.Ask(context.Background(), BuildState(p, 1, true, 200), Focus(map[string]Question{
		"stateful_delete": {
			Type:         "noul",
			Instructions: "The change at `focus` destroys a resource whose stored data is lost with it.",
			Criteria: &Criteria{
				True:  "The action is delete or replace and the resource type stores persistent data.",
				False: "The action is create or update, or the resource stores no persistent data.",
			},
		},
	}, 1))
	if err != nil {
		t.Fatal(err)
	}
	got := resp.Answers["stateful_delete"].Noul
	t.Logf("model=%s noul=%.2f input_tokens=%d", resp.Model, got, resp.Usage.InputTokens)
	if got < 0.7 {
		t.Errorf("replacing an RDS instance should read as a stateful destroy, got %.2f", got)
	}
}
