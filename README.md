# tfreview

tfreview checks a Terraform plan against risks you define and reports the
verdict on a pull request as one comment and one `tfreview:*` label.

## Why tfreview

Infrastructure changes can cause outages, data loss, or unexpected cost. A
Terraform plan shows what will change, but teams need a consistent way to flag
the risks that matter to them before applying it. tfreview checks the plan
against your rules, uses an LLM only when context is needed, and puts the
result where the team reviews changes: on the pull request. It reports by
default, so you can adopt it before making findings merge-blocking.

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
