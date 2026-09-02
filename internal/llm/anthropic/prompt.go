package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nakamasato/tfreview/internal/llm"
	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
)

const toolName = "report_verdicts"

func BuildSystem(language string) string {
	lang := "in English"
	if language == "ja" {
		lang = "in Japanese (日本語)"
	}
	return fmt.Sprintf(`You review Terraform changes. Judge each check id using ONLY the given `+"`terraform plan`"+` result.
- Never mark a check as hit based on something the plan does not show.
- If the plan cannot answer the question, return "unverifiable" and say why.
- `+"`changed_keys`"+` lists the attributes whose value changed in this plan; use it to decide whether something was added or relaxed by this change.
- reason: 1-2 sentences %s, naming the resource addresses it relies on.
- Return exactly one entry per check id via the %s tool.
`, lang, toolName)
}

func PlanJSON(p *plan.Plan) string {
	b, _ := json.MarshalIndent(p, "", "  ")
	return string(b)
}

func BuildUser(req llm.Request, planJSON string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Target\n\n`%s`\n\n# terraform plan result\n\n```json\n%s\n```\n\n# Checks\n\n", req.Plan.Target, planJSON)
	for _, c := range req.Checks {
		fmt.Fprintf(&sb, "- %s: %s\n", c.ID, strings.TrimSpace(c.Question))
	}
	return sb.String()
}

type toolInput struct {
	Verdicts []struct {
		CheckID string `json:"check_id"`
		Verdict string `json:"verdict"`
		Reason  string `json:"reason"`
	} `json:"verdicts"`
}

func ParseAnswers(input json.RawMessage) ([]llm.Answer, error) {
	var in toolInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("parse tool input: %w", err)
	}
	var out []llm.Answer
	for _, v := range in.Verdicts {
		k := model.VerdictKind(v.Verdict)
		if k != model.VerdictHit && k != model.VerdictMiss && k != model.VerdictUnverifiable {
			continue
		}
		out = append(out, llm.Answer{CheckID: v.CheckID, Kind: k, Reason: v.Reason})
	}
	return out, nil
}
