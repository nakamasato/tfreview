# tfreview

tfreview checks a Terraform plan against risks you define and reports the
verdict on a pull request as one comment and one `tfreview:*` label.

## Why tfreview

- **Plan-only review.** The only input is the `terraform plan` result. No agent
  walks your repository, so verdicts are stable and each target costs one API call.
- **Your criteria, in YAML.** What counts as dangerous lives in `.tfreview.yaml`.
  Deterministic checks (`match`) and judged checks (`instructions`) combine into four
  check types; the config decides the severity, the LLM only says hit / miss.
- **Incremental.** Verdicts are cached per target by the hash of plan + config. A
  push that does not change a target's plan re-uses its verdicts: no drift, no
  extra cost.
- **One comment, one label.** The comment is replaced in place, never stacked.
  `tfreview:critical` on the PR list tells you where to look first.
- **Many targets, one verdict.** Monorepos and multi-environment layouts are
  judged together; the most dangerous target wins.
- **Blocking is opt-in.** By default it only reports. `--fail-on critical` turns
  it into a required check.
- **Same verdict locally.** `tfreview fetch --pr N` pulls the plan CI already
  produced, so you (or your AI agent) can review from a laptop.

> **Status:** alpha. Breaking changes may occur before v1.

## Install

Download a binary from [Releases](https://github.com/nakamasato/tfreview/releases),
or install with Go:

```sh
go install github.com/nakamasato/tfreview/cmd/tfreview@latest
```

## Quick start

Create a Terraform plan JSON, reduce it, and review it locally:

```sh
terraform show -json tfplan > plan.json
tfreview extract --show-json plan.json --target default --out default.json
tfreview review --plan default.json
```

To review pull requests in CI, add the
[GitHub Action](https://github.com/nakamasato/tfreview) to a workflow and
provide an LLM API key. The action can also fail a check at a chosen severity.

## Docs

- [GitHub Actions and CLI usage](docs/usage.md)
- [Configuration and checks](docs/configuration.md)
- [How verdicts and incremental state work](docs/how-it-works.md)
- [Evaluation and limitations](docs/limitations.md)

MIT License. See [LICENSE](LICENSE).
