# tfreview

tfreview checks a Terraform plan against risks you define and reports the
verdict on a pull request as one comment and one `tfreview:*` label. Its judge
uses the plan as input, without reading repository files.

Use it to catch risky infrastructure changes before they are applied. Simple
rules are deterministic; checks that need context can be judged by an LLM. By
default, tfreview reports findings without blocking a merge.

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
