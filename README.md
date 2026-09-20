# tfreview

A CLI and a GitHub Action that review a `terraform plan` against the risks *you*
define, and post the verdict to the pull request as one comment and one
`tfreview:*` label. The action runs the same CLI, so the verdict you get in CI is
the verdict you get on your laptop.

> Status: alpha. Until `v1`, any release may contain breaking changes — to the
> flags, the config schema, the output files, or the action inputs.

<!-- screenshot of the comment: docs/comment.png (add after the first real run) -->

## Why tfreview

- **Plan-only review.** The only input is the `terraform plan` result. No agent
  walks your repository, so verdicts are stable and each target costs one API call.
- **Your criteria, in YAML.** What counts as dangerous lives in `.tfreview.yaml`.
  Deterministic checks (`match`) and LLM checks (`question`) combine into four
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

## Quick start (GitHub Actions)

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

Other inputs: `plan-json` (glob of plan JSON already reduced by `tfreview extract`;
mutually exclusive with `show-json`) and `version` (the tfreview release to
install; defaults to the tag the action itself was referenced by).

`show-json` takes a glob of `terraform show -json` outputs; the target name is
the file name without extension (use one file per directory/environment for a
monorepo). Without a `.tfreview.yaml` the built-in, provider-neutral checks are
used. The action outputs `score`, `label`, `incomplete`, and `out-dir` for
downstream steps.

For local use, `tfreview fetch --pr N` downloads the plan JSON for that PR. It
works out of the box with the `tfreview-plan` artifact the action uploads, and
also auto-detects raw `terraform show -json` artifacts from other pipelines
(running `extract` on them itself); use `--target-prefix` if your artifact
names don't match a common naming convention.

## CLI

| Command | What it does |
| --- | --- |
| `tfreview extract --show-json plan.json --target prd --out prd.json` | Reduce `terraform show -json` to what review needs (`after` only, plus `changed_keys`; `before` never leaves the runner) |
| `tfreview review --plan prd.json --plan dev.json [--state-in state.json] [--fail-on high]` | Judge; writes `result.json`, `comment.md`, `label.txt`, `state.json` |
| `tfreview comment --pr 123 [--repo owner/name]` | Upsert the comment and set the label |
| `tfreview fetch --pr 123 [--repo owner/name] [--out-dir DIR] [--artifact NAME] [--target-prefix STR]` | Download the plan JSON a CI run uploaded (`--out-dir` default `tfreview-plans`); auto-detects the artifact when `--artifact` is omitted |

`--repo` defaults to the `GITHUB_REPOSITORY` environment variable, then the
`origin` remote of the current directory (github.com only).

Without `ANTHROPIC_API_KEY` set, `review` still runs, prints a warning to
stderr, and labels the result `tfreview:unknown` since no LLM checks could be
judged.

`--print debug` writes a coloured, terminal-oriented view of the run to stdout:
the plan attributes each check was given, then the verdicts with hits first and
misses collapsed to one line. `--provider` / `--model` override `llm.provider` /
`llm.model` for a one-off run.

`--provider claude-cli` judges through the local `claude` CLI instead of the
API, so it needs no `ANTHROPIC_API_KEY` and is billed to the Claude
subscription. It is for local iteration only — CI has no `claude` CLI.

`--fail-on-rule-only` narrows `--fail-on` to verdicts a `match` decided
(deterministic checks, or an `ask` check that fell back to its match result
because the LLM didn't answer) — an LLM `hit` alone won't fail the build.

Install: `go install github.com/nakamasato/tfreview/cmd/tfreview@latest` or the
tarball from Releases.

## Configuration

Everything lives in `.tfreview.yaml` at the repository root (override the path
with `--config` / the action's `config` input). Without one, the built-in
defaults below apply. `llm.provider: mock` (fixed verdicts, no API calls; for
tests only) additionally requires the environment variable
`TFREVIEW_ALLOW_MOCK=1`, so it can't accidentally run for real.

```yaml
language: en                 # default en. Language of the fixed comment text and LLM instructions
llm:
  provider: anthropic        # anthropic | claude-cli (the local `claude` CLI, no API key) | mock
  model: claude-opus-5
  max_plan_chars: 100000     # skip the LLM call and mark every check unverifiable above this size
  max_tokens: 128000         # max_tokens for the judging call; lower it only for a model with a smaller output cap
  pricing:                   # USD / Mtok, used only for the footer's cost estimate; built-in default if omitted
    input: 5.00
    cache_write: 6.25
    cache_read: 0.50
    output: 25.00
  jev:                       # the scoring judge; see Scored checks below
    model: jev-latest
    hit_threshold: 0.70      # a score at or above this is a hit
    miss_threshold: 0.30     # at or below is a miss; the band between is undecided
    max_value_chars: 2000    # shorten long individual attribute values
    concurrency: 4           # scored checks in flight at once
aspects:
  - id: destruction
    title: Destruction / downtime
    checks:
      - id: delete-or-replace
        severity: critical                 # none < medium < high < critical
        match: { actions: [delete] }       # actions / types / targets only, each a list of strings
        verdict_on_match: ask              # hit (default) / ask / unverifiable
        question: |
          Is a running resource deleted or replaced? ...
        instructions: |                    # the same check, for a judge that scores
          The change at `focus` deletes or replaces a resource that serves traffic.
        criteria:
          true: the action is delete or replace, and the resource serves requests
          false: the action is create or update, or the resource serves nothing
```

- If `aspects` is omitted, the built-in defaults are used. If present, it
  replaces them entirely — there is no merge.
- `id` must be unique within aspects and within checks. A duplicate id, an
  invalid `severity`, an unknown `match` key, or a check with none of `match`,
  `question` or `instructions` is a config error (`review` exits 2).
- The config's SHA-256 is mixed into the digest used to key incremental state.

### Check types

| Type | Config | What it judges | LLM |
| --- | --- | --- | --- |
| A. Fact | `match` (`verdict_on_match: hit`) | A fact visible in the plan | Not used |
| B. Interpretation | `question` only | Visible in the plan, but needs judgment | Used |
| B′. Fact + interpretation | `match` + `question` + `verdict_on_match: ask` | What changed is deterministic; whether it is dangerous needs judgment | Used. If no answer comes back, the match result stands |
| C. Unverifiable | `match` + `verdict_on_match: unverifiable` | The plan cannot show this in principle | Not used. Reports "unverifiable by plan" |

`severity` is always decided by the config. The LLM only returns hit / miss and a reason.

### Scored checks

`question` is addressed to a judge that answers in prose. `instructions` states
the same check as a proposition for a judge that scores it — TypeSafe AI's System
One (Jev) returns a probability and no text — and `criteria` says what puts the
proposition on each side. A check can carry both; which one is used depends on
the judge. Scoring is configurable but not yet selectable from `llm.provider`.

A score becomes a verdict by `llm.jev.hit_threshold` and `miss_threshold`, so an
exception has to be written into `criteria` rather than left to the judge's
discretion — a probability has nowhere to record a caveat. A question referring
to `focus` is aimed at the change being judged. The defaults are the band the
API documentation uses in its examples; they are a starting point for
`eval/`, not a calibrated recommendation.

### Severities

The axes are recoverability and production impact.

| Severity | Criterion |
| --- | --- |
| `critical` | Cannot be undone, or takes production down |
| `high` | Can be undone, but damage can go unnoticed for a while |
| `medium` | Can be undone and the impact is contained |
| `none` | Not applicable |

### Built-in default checks

Provider-neutral, used when `.tfreview.yaml` has no `aspects`.

| Aspect | Check | Type | Severity |
| --- | --- | --- | --- |
| destruction | delete-or-replace | B′ (`actions: [delete]` + ask) | critical |
| data-loss | stateful-delete | A (`actions: [delete]` + major DB/storage types) | critical |
| data-loss | guard-relaxed | B (force_destroy / deletion_protection etc. relaxed) | critical |
| exposure | privilege-grant | B (privilege expansion, wildcards) | high |
| exposure | public-exposure | B (0.0.0.0/0, public access) | high |
| cost | recurring-charge | B (recurring charges increase) | high |

`examples/aws.yaml` and `examples/gcp.yaml` are full, provider-specific configs
you can copy to `.tfreview.yaml` and edit. Set `language: ja` to get the fixed
comment text and LLM instructions in Japanese instead of English.

## How it works

1. `match` is evaluated for every check and every target — deterministic and
   free, so it always runs from scratch.
2. For each target, checks left undecided by `match` (plain `question` checks,
   and `ask` checks that matched) are sent to the LLM in a single call. If
   incremental state has a verdict for that target already (same plan +
   config digest), the call is skipped and the cached verdict is reused.
3. The same check's verdicts across targets are merged, keeping the more
   dangerous one: `hit` > `unverifiable` > `miss` > `skipped`.
4. `ask` fallback: if any target's answer for a check came back missing, the
   whole check reverts to what `match` alone decided, so a real `miss` can't
   be pushed aside by another target's `skipped`.
5. Scores aggregate by max: an aspect scores the max of its checks, the PR
   scores the max of its aspects.

If the LLM call fails, times out, or returns something that can't be parsed,
every check for that target becomes `skipped` — the process never crashes.
When any check is `skipped`, the comment says the verdict is incomplete and
the label is `tfreview:unknown` instead of a severity.

Incremental state (`state.json`) keys verdicts by target, under the SHA-256 of
that target's (reduced) plan JSON plus the config. A push that doesn't change
a target's plan or the config reuses its cached verdicts instead of calling
the LLM again. `skipped` targets are never written to state, so a transient
LLM failure doesn't get pinned for the life of the PR — the next run retries it.

## Evaluating judgement quality

`eval/cases/*.json` are labelled plan fixtures; `eval/eval_test.go` judges each
one and scores the verdicts against the labels. A case lists only the checks
expected to be anything other than `miss`, so a false positive on any other
check fails it too.

```
TFREVIEW_EVAL=1 go test ./eval -v -count=1 -timeout 20m
```

It calls a real LLM, so it is skipped unless `TFREVIEW_EVAL=1` is set, and
`-count=1` is required or Go serves a cached result instead of re-judging.
`TFREVIEW_EVAL_MODEL` overrides the model. Only disagreements are printed,
followed by the accuracy, token counts and cost.

## Limitations

- Anything not in the plan is not seen (workflow files, CODEOWNERS, removing
  `prevent_destroy`).
- The config comes from the PR branch. This is a review aid, not a defense
  against a malicious insider.
- LLM verdicts (🤖) can be wrong. Deterministic verdicts (🔧) cannot.
- `extract` only removes attributes that Terraform itself marked
  `sensitive` in the plan, and only at the top level: if just part of a
  nested attribute is sensitive, the whole attribute is dropped rather than
  partially masked. Anything Terraform did not mark `sensitive` — user data
  scripts, policy documents, environment variables, and the like — passes
  through to plan data unchanged and is sent to the configured LLM provider
  even if it happens to contain secrets. Mark such attributes `sensitive =
  true` in the provider/module, or exclude the resource from tfreview
  (e.g. via `match.targets`), if this is a concern.

## License

MIT
