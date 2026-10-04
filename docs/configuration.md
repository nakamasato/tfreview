# Configuration

Configuration is read from `.tfreview.yaml` at the repository root. Set
`--config` in the CLI or the action's `config` input to use another path. With
no file, tfreview uses its built-in checks. If `aspects` is present, it
replaces the built-in checks entirely.

Here is a small example:

```yaml
language: en
llm:
  provider: anthropic # anthropic | claude-cli | jev | mock
  model: claude-sonnet-5-5
aspects:
  - id: resource-deletion
    title: Resource deletion
    checks:
      - id: resource-deletion
        severity: critical
        match:
          actions: [delete]
        verdict_on_match: ask
        instructions: |
          The change at `focus` removes or recreates a resource that serves requests.
        criteria:
          true: the action is destroy or replace, and the resource serves requests
          false: anything else, including an add or change action
```

The full configuration also supports language, model limits, pricing for the
cost estimate, a second-pass deep dive, and Jev scoring options. `model` is
used by the primary Anthropic or Claude CLI provider; Jev uses `jev.model`.
`deep_dive_model` selects the model for `deep_dive`.

```yaml
language: en
llm:
  provider: jev
  max_plan_chars: 100000
  max_tokens: 128000
  pricing: # USD per million tokens; used for the comment estimate
    input: 5.00
    cache_write: 6.25
    cache_read: 0.50
    output: 25.00
  deep_dive: anthropic # "" (off) | anthropic
  deep_dive_model: claude-sonnet-5-5 # used by llm.deep_dive
  jev: # used when provider: jev; requires TYPESAFE_API_KEY
    model: jev-latest
    hit_threshold: 0.70
    miss_threshold: 0.30
    max_value_chars: 2000
    concurrency: 4
aspects:
  - id: resource-deletion
    title: Resource deletion
    checks:
      - id: resource-deletion
        severity: critical
        match: { actions: [delete] }
        verdict_on_match: ask
        instructions: |
          The change at `focus` removes or recreates a resource that serves requests.
        criteria:
          true: the action is destroy or replace, and the resource serves requests
          false: anything else, including an add or change action
```

With `provider: jev`, Jev uses `jev.model` (`jev-latest` by default), while
Anthropic uses `deep_dive_model` for individual judgments.

## Aspects and checks

An aspect groups checks and has a title. Each check has a unique `id`, a
`severity` (`none`, `medium`, `high`, or `critical`), and one or both of:

- `match`: deterministic filters over plan actions, resource types, or targets.
- `instructions`: a proposition judged using the plan and an LLM.

`verdict_on_match` controls a matching rule: `hit` (default), `ask` (ask the
LLM whether the matched change is risky), or `unverifiable` (report that the
plan cannot establish the condition). `severity` always comes from the
configuration; the LLM only returns hit or miss with a reason.

| Check form | What it means | Uses LLM |
| --- | --- | --- |
| `match` | A fact visible in the plan | No |
| `instructions` | A visible change that needs judgment | Yes |
| `match` + `instructions` + `verdict_on_match: ask` | The match finds a change; judgment determines risk | Yes |
| `match` + `verdict_on_match: unverifiable` | The plan cannot establish this condition | No |

IDs must be unique among aspects and among checks. Invalid severity, unknown
`match` keys, duplicate IDs, or a check with neither `match` nor
`instructions` are configuration errors. The configuration SHA-256 is part of
the incremental-state key.

## Writing instructions

Write `instructions` as a proposition. `criteria.true` defines what makes it
true, and `criteria.false` describes the complement, including exclusions.
This keeps prose-based and score-based judges aligned.

`focus` identifies the change under review. `focus.changed_keys` lists changed
attributes, `focus.after` contains resulting attributes, and
`focus.referred_by` lists other changes that refer to it. The `before` values
are not sent to the judge, so it can determine that an attribute changed but
not whether a number or string increased or decreased.

For `jev`, scores at or above `hit_threshold` are hits and scores at or below
`miss_threshold` are misses. Scores in between are `unverifiable`. These
thresholds are starting points, not calibrated recommendations.

## Built-in checks and examples

The provider-neutral built-ins are used when no `aspects` are configured:

| Aspect | Check | Severity | Summary |
| --- | --- | --- | --- |
| resource-deletion | resource-deletion | critical | Ask about deleted resources that serve requests. |
| data-loss | stateful-delete | critical | Match deletion of major database or storage types. |
| data-loss | data-loss | critical | Judge data store destruction or a relaxed guard. |
| polp | polp | high | Judge broad write access, wildcards, public access, or `0.0.0.0/0`. |
| cost | cost | high | Judge increases in recurring charges. |

See [`examples/aws.yaml`](../examples/aws.yaml) and
[`examples/gcp.yaml`](../examples/gcp.yaml) for provider-specific starting
points. Set `language: ja` for Japanese fixed comment text and judging
instructions.

Use `llm.provider: mock` only for tests. It requires `TFREVIEW_ALLOW_MOCK=1`
as an additional safeguard.
