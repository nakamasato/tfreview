# How tfreview works

## Review flow

1. tfreview evaluates each configured `match` against every plan target. These
   deterministic checks run on every review.
2. Checks still undecided after matching are sent to the configured judge.
   With the Anthropic provider, checks for a target are judged together in one
   call. Jev scores changes separately. A matching `ask` check is included;
   plain `instructions` checks are included as well.
3. Verdicts for the same check across targets are merged, keeping the more
   dangerous result: `hit` > `unverifiable` > `miss` > `skipped`.
4. If any target has no answer for an `ask` check, that check falls back to the
   deterministic match result across targets.
5. Each aspect takes the highest severity among its checks. The pull request
   takes the highest severity among its aspects.

If the judge fails, times out, or returns an unparseable response, that
target's checks become `skipped`. A skipped check makes the verdict incomplete
and the label `tfreview:unknown`.

## Plan data

`tfreview extract` reduces Terraform's `show -json` output for review. It
retains resulting (`after`) values and changed keys; `before` values do not
leave the runner. Terraform attributes marked sensitive are removed. See
[Limitations](limitations.md) for how sensitive data is handled.

The plan is the judge's only input. No repository files are read, and the
deep-dive tools can inspect the plan and query the scoring judge but cannot
access the repository or network.

## Providers

- `anthropic` judges checks with an Anthropic API call.
- `jev` scores each change separately and returns probabilities. Values between
  the configured thresholds remain `unverifiable` unless a deep dive is used.
- `deep_check.provider: anthropic` can take a second look at changes left undecided. It
  requires `ANTHROPIC_API_KEY`.
- `claude-cli` uses the local `claude` command and is intended for local work.
- `mock` returns fixed test verdicts and requires `TFREVIEW_ALLOW_MOCK=1`.

If a required provider API key is missing, review continues with judged checks
skipped and a `tfreview:unknown` result.

## Incremental state

`state.json` stores verdicts keyed by target and the SHA-256 digest of that
target's reduced plan plus the configuration. An unchanged target can reuse
its prior verdicts without another LLM call. `skipped` targets are not stored,
so a temporary provider failure is retried on the next run.

In GitHub Actions, the state is cached by pull request. Locally, pass a prior
state file with `--state-in` to reuse verdicts between runs.
