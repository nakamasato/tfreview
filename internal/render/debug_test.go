package render

import (
	"strings"
	"testing"

	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
	"github.com/stretchr/testify/require"
)

func debugFixture() (*Result, []*plan.Plan) {
	r := &Result{
		Score: model.SeverityHigh, Label: "tfreview:high", Model: "m",
		Categories: []CategoryResult{{Checks: []CheckResult{
			{ID: "public-exposure", Level: model.SeverityHigh, Verdict: model.VerdictHit, Reason: "open to 0.0.0.0/0", Source: model.SourceLLM},
			{ID: "stateful-delete", Level: model.SeverityCritical, Verdict: model.VerdictMiss, Reason: "no resource matched", Source: model.SourceRule},
		}}},
	}
	p := &plan.Plan{Target: "prd", Counts: plan.Counts{Add: 1}, Resources: []plan.Resource{{
		Address: "aws_security_group_rule.ssh", Actions: []string{"create"},
		After: map[string]any{"from_port": 22},
	}}}
	return r, []*plan.Plan{p}
}

func TestDebugPlain(t *testing.T) {
	r, plans := debugFixture()
	out := Debug(r, plans, false)
	require.NotContains(t, out, "\x1b[")
	require.Contains(t, out, "aws_security_group_rule.ssh")
	require.Contains(t, out, "from_port=22")
	// A miss collapses to its id on the shared line, without its reason.
	require.Contains(t, out, "miss         stateful-delete")
	require.NotContains(t, out, "no resource matched")
}

func TestDebugColorKeepsColumnsAligned(t *testing.T) {
	r, plans := debugFixture()
	out := Debug(r, plans, true)
	require.Contains(t, out, "\x1b[")
	// Colouring must not eat the padding: stripped of escapes, the coloured and the
	// plain rendering have to be byte-identical.
	require.Equal(t, Debug(r, plans, false), stripANSI(out))
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\x1b' {
			b.WriteByte(s[i])
			continue
		}
		for i < len(s) && s[i] != 'm' {
			i++
		}
	}
	return b.String()
}
