# tfreview

Review a `terraform plan` against the risks *you* define, and post the verdict to
the pull request as one comment and one `tfreview:*` label.

<!-- screenshot of the comment: docs/comment.png (add after the first real run) -->

## Why tfreview

- **Plan-only review.** The only input is the `terraform plan` result. No agent
  walks your repository, so verdicts are stable and each target costs one API call.
- **Your criteria, in YAML.** What counts as dangerous lives in `.tfreview.yaml`.
  Deterministic checks (`match`) and LLM checks (`question`) combine into four
  check types; the config decides the level, the LLM only says hit / miss.
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

      - uses: nakamasato/tfreview@v1
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

For local use, `tfreview fetch --pr N` downloads the plan JSON the action
uploaded for that PR, so you can run `tfreview review` (or hand the plan to an
AI agent) without re-running `terraform plan`.

## CLI

| Command | What it does |
| --- | --- |
| `tfreview extract --show-json plan.json --target prd --out prd.json` | Reduce `terraform show -json` to what review needs (`after` only, plus `changed_keys`; `before` never leaves the runner) |
| `tfreview review --plan prd.json --plan dev.json [--state-in state.json] [--fail-on high]` | Judge; writes `result.json`, `comment.md`, `label.txt`, `state.json` |
| `tfreview comment --pr 123 [--repo owner/name]` | Upsert the comment and set the label |
| `tfreview fetch --pr 123 [--repo owner/name] [--out-dir DIR] [--artifact NAME]` | Download the plan JSON a CI run uploaded (`--out-dir` default `tfreview-plans`, `--artifact` default `tfreview-plan`) |

`--repo` defaults to the `GITHUB_REPOSITORY` environment variable.

`--fail-on-machine-only` narrows `--fail-on` to verdicts a `match` decided
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
  provider: anthropic        # anthropic only, for now
  model: claude-opus-5
  max_plan_chars: 100000     # skip the LLM call and mark every check unverifiable above this size
  pricing:                   # USD / Mtok, used only for the footer's cost estimate; built-in default if omitted
    input: 5.00
    cache_write: 6.25
    cache_read: 0.50
    output: 25.00
categories:
  - id: destruction
    title: Destruction / downtime
    checks:
      - id: delete-or-replace
        level: critical                    # none < medium < high < critical
        match: { actions: [delete] }       # actions / types / targets only, each a list of strings
        verdict_on_match: ask              # hit (default) / ask / unverifiable
        question: |
          Is a running resource deleted or replaced? ...
```

- If `categories` is omitted, the built-in defaults are used. If present, it
  replaces them entirely — there is no merge.
- `id` must be unique within categories and within checks. A duplicate id, an
  invalid `level`, an unknown `match` key, or a check with neither `match` nor
  `question` is a config error (`review` exits 2).
- The config's SHA-256 is mixed into the digest used to key incremental state.

### Check types

| Type | Config | What it judges | LLM |
| --- | --- | --- | --- |
| A. Fact | `match` (`verdict_on_match: hit`) | A fact visible in the plan | Not used |
| B. Interpretation | `question` only | Visible in the plan, but needs judgment | Used |
| B′. Fact + interpretation | `match` + `question` + `verdict_on_match: ask` | What changed is deterministic; whether it is dangerous needs judgment | Used. If no answer comes back, the match result stands |
| C. Unverifiable | `match` + `verdict_on_match: unverifiable` | The plan cannot show this in principle | Not used. Reports "unverifiable by plan" |

`level` is always decided by the config. The LLM only returns hit / miss and a reason.

### Levels

The axes are recoverability and production impact.

| Level | Criterion |
| --- | --- |
| `critical` | Cannot be undone, or takes production down |
| `high` | Can be undone, but damage can go unnoticed for a while |
| `medium` | Can be undone and the impact is contained |
| `none` | Not applicable |

### Built-in default checks

Provider-neutral, used when `.tfreview.yaml` has no `categories`.

| Category | Check | Type | Level |
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
5. Scores aggregate by max: a category scores the max of its checks, the PR
   scores the max of its categories.

If the LLM call fails, times out, or returns something that can't be parsed,
every check for that target becomes `skipped` — the process never crashes.
When any check is `skipped`, the comment says the verdict is incomplete and
the label is `tfreview:unknown` instead of a level.

Incremental state (`state.json`) keys verdicts by target, under the SHA-256 of
that target's (reduced) plan JSON plus the config. A push that doesn't change
a target's plan or the config reuses its cached verdicts instead of calling
the LLM again. `skipped` targets are never written to state, so a transient
LLM failure doesn't get pinned for the life of the PR — the next run retries it.

## Limitations

- Anything not in the plan is not seen (workflow files, CODEOWNERS, removing
  `prevent_destroy`).
- The config comes from the PR branch. This is a review aid, not a defense
  against a malicious insider.
- LLM verdicts (🤖) can be wrong. Deterministic verdicts (🔧) cannot.

## License

MIT
