package anthropic

import (
	"context"
	"os"
	"testing"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/nakamasato/tfreview/internal/llm"
	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
	"github.com/stretchr/testify/require"
)

func TestJudgeRejectsLargePlan(t *testing.T) {
	p := New(Options{Model: "claude-opus-5", MaxPlanChars: 10})
	big := &plan.Plan{Target: "t", Resources: []plan.Resource{{Address: "aws_s3_bucket.a_long_name"}}}
	_, _, err := p.Judge(context.Background(), llm.Request{Plan: big, Checks: []model.Check{{ID: "a", Question: "q"}}})
	require.ErrorIs(t, err, ErrPlanTooLarge)
}

func TestCheckTruncatedDetectsMaxTokens(t *testing.T) {
	resp := &sdk.Message{StopReason: sdk.StopReasonMaxTokens}
	err := checkTruncated(resp, 16000, 5)
	require.ErrorIs(t, err, ErrResponseTruncated)
	require.Contains(t, err.Error(), "max_tokens=16000")
	require.Contains(t, err.Error(), "checks=5")
}

func TestCheckTruncatedIgnoresOtherStopReasons(t *testing.T) {
	resp := &sdk.Message{StopReason: sdk.StopReasonToolUse}
	require.NoError(t, checkTruncated(resp, 16000, 5))
}

func TestNewDefaultsMaxTokens(t *testing.T) {
	p := New(Options{Model: "claude-opus-5"})
	require.Equal(t, defaultMaxTokens, p.opts.MaxTokens)
}

func TestJudgeLive(t *testing.T) {
	if os.Getenv("TFREVIEW_LIVE") == "" {
		t.Skip("set TFREVIEW_LIVE=1 to call the real API")
	}
	p := New(Options{Model: "claude-opus-5", MaxPlanChars: 100000})
	pl := &plan.Plan{Target: "prd", Resources: []plan.Resource{{Address: "aws_db_instance.main", Type: "aws_db_instance", Actions: []string{"delete"}}}}
	got, usage, err := p.Judge(context.Background(), llm.Request{Plan: pl, Language: "en", Checks: []model.Check{{ID: "delete-or-replace", Question: "Is a running resource deleted?"}}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, model.VerdictHit, got[0].Kind)
	require.Equal(t, 1, usage.Calls)
}
