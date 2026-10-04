# Usage

## GitHub Actions

Create a workflow that produces a Terraform plan JSON and passes it to the
action. The action reviews the plan, uploads it as an artifact, and posts one
comment and one label on pull requests.

```yaml
name: tfreview
on:
  pull_request:

permissions:
  contents: read
  pull-requests: write
  issues: write

jobs:
  plan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: hashicorp/setup-terraform@v4
      - run: terraform init
      - run: terraform plan -out=tfplan
      - run: terraform show -json tfplan > plan.json
      - uses: nakamasato/tfreview@v0
        with:
          show-json: plan.json
          anthropic-api-key: ${{ secrets.ANTHROPIC_API_KEY }}
          fail-on: critical
```

`show-json` accepts a glob of `terraform show -json` files. Each file is a
target named after its filename without the extension, which works for
multi-environment repositories when each environment writes a separate file.
Alternatively, set `plan-json` to a glob of files already processed by
`tfreview extract`; the two inputs are mutually exclusive. The `version` input
selects a tfreview release and defaults to the action ref.

The action uses built-in checks when `.tfreview.yaml` is absent. Its outputs
are `score`, `label`, `incomplete`, and `out-dir`. It uploads the reduced plan
as the `tfreview-plan` artifact for seven days.

## Local CLI

Install the CLI using the instructions in the [README](../README.md). Reduce
Terraform's plan JSON before reviewing it:

```sh
terraform show -json tfplan > plan.json
tfreview extract --show-json plan.json --target prod --out prod.json
tfreview review --plan prod.json --fail-on high
```

`review` writes `result.json`, `comment.md`, `label.txt`, and `state.json` to
`tfreview-out` by default. It can take multiple `--plan` arguments for multiple
targets. The command reports findings by default. Use `--fail-on medium`,
`high`, or `critical` to return a failing exit code at that severity; add
`--fail-on-rule-only` to fail only on deterministic rules.

To post or update a pull request comment and label, run:

```sh
tfreview comment --result tfreview-out/result.json --pr 123 --repo owner/name
```

`--repo` defaults to `GITHUB_REPOSITORY`, then the current directory's GitHub
`origin` remote. Set `GITHUB_TOKEN` for authentication.

To fetch the plan uploaded by an action run:

```sh
tfreview fetch --pr 123 --repo owner/name
```

This downloads to `tfreview-plans` by default. It recognizes the `tfreview-plan`
artifact and raw `terraform show -json` artifacts from other pipelines. Use
`--artifact` to select an artifact explicitly and `--target-prefix` when
artifact filenames do not follow the naming convention tfreview detects.

To save the review as an HTML report, use the existing `review` command:

```sh
go run ./cmd/tfreview review --plan /tmp/plan.json --format html --out-dir /tmp/tfreview-review
```

`review.html` is written inside `--out-dir`, alongside the usual
`comment.md` and `result.json`. For a historical PR, use `fetch` to download
its saved plan artifact, then pass the extracted plan to `review`. The HTML
report contains resource addresses and verdict reasons, so review it before
sharing. Pass `--light-provider jev --deep-provider anthropic` to run Jev
scoring followed by individual Anthropic judgments. `--light-model` and
`--deep-model` select the models for each phase.

For local iteration, `--light-provider claude-cli` uses an installed
`claude` CLI instead of an API key. `--light-provider` and `--light-model`
override the first phase for one run. `--format json` writes the result JSON to
stdout; `--format comment` writes the PR comment Markdown. `--debug` prints
plan attributes, per-phase check results, and verdicts to stderr.

When run locally, tfreview checks GitHub Releases for a newer version and
prints an upgrade command to stderr when one is available. It skips this check
in CI. Set `TFREVIEW_NO_UPDATE_CHECK=1` to disable it; network errors do not
affect command execution.

## Commands

| Command | Purpose |
| --- | --- |
| `tfreview extract --show-json plan.json --target prod --out prod.json` | Reduce Terraform plan JSON for review. |
| `tfreview review --plan prod.json [--plan staging.json]` | Judge one or more reduced plans. |
| `tfreview comment --result result.json --pr 123` | Update a pull request comment and label. |
| `tfreview fetch --pr 123` | Download a plan artifact from a pull request workflow. |
