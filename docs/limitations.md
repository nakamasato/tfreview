# Evaluation and limitations

## Evaluation

`eval/cases/*.json` contains labeled plan fixtures. The evaluation judges each
fixture and compares its verdicts with the labels. A case lists checks expected
to be something other than `miss`, so unexpected positive verdicts also fail
the evaluation.

```sh
TFREVIEW_EVAL=1 go test ./eval -v -count=1 -timeout 20m
```

This calls a real LLM and is skipped unless `TFREVIEW_EVAL=1` is set. Use
`-count=1` to avoid Go's test result cache. `TFREVIEW_EVAL_MODEL` overrides the
model. The evaluation prints disagreements, accuracy, token counts, and cost.

## Limitations

- Only information in the Terraform plan is reviewed. Workflow files,
  CODEOWNERS, and changes such as removing `prevent_destroy` are not visible.
- Configuration is loaded from the pull request branch, so tfreview is a
  review aid rather than protection against a malicious insider.
- LLM verdicts can be wrong. Deterministic verdicts are based on the configured
  rules.
- `extract` removes the values Terraform marked `sensitive`, including those
  nested inside blocks: sensitive keys are removed and sensitive list elements
  are replaced with `(sensitive)`. The rest of the block is kept. If
  `after_sensitive` cannot be parsed, the whole `after` is dropped. Attributes
  Terraform did not mark sensitive pass through
  unchanged, including scripts, policy documents, and environment variables;
  they are sent to the configured LLM provider with the plan. Mark such
  attributes `sensitive = true` in the provider or module, or exclude the
  resource from tfreview (for example with `match.targets`) if needed.
