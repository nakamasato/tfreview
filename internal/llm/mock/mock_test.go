package mock

import (
	"context"
	"errors"
	"testing"

	"github.com/nakamasato/tfreview/internal/llm"
	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
	"github.com/stretchr/testify/require"
)

func TestMockReturnsAnswersPerTarget(t *testing.T) {
	p := &Provider{Answers: map[string][]llm.Answer{"prd": {{CheckID: "a", Kind: model.VerdictHit, Reason: "r"}}}}
	got, usage, err := p.Judge(context.Background(), llm.Request{Plan: &plan.Plan{Target: "prd"}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, 1, usage.Calls)
	require.Len(t, p.Calls, 1)

	got, _, err = p.Judge(context.Background(), llm.Request{Plan: &plan.Plan{Target: "other"}})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestMockError(t *testing.T) {
	p := &Provider{Err: errors.New("boom")}
	_, _, err := p.Judge(context.Background(), llm.Request{Plan: &plan.Plan{Target: "prd"}})
	require.Error(t, err)
}
