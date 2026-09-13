---
name: tfreview-rules
description: Use when creating or extending a repository's .tfreview.yaml from its own Terraform history — failed applies, reverts, fix-up PRs, PR descriptions — or when asked to add tfreview checkpoints for a resource type.
allowed-tools: Bash, Read, Write, Edit, Grep, Glob, WebFetch, WebSearch
---

# tfreview-rules

Turn what already went wrong (or was deliberately avoided) in a Terraform repository into
`checkpoints_for_resource` entries that tfreview can judge from a plan.

A checkpoint the plan cannot decide is worse than none: it returns `unverifiable` on every PR
and teaches readers to ignore the comment. Most of this procedure exists to drop candidates.

## Inputs tfreview gives the model

Per resource: `type`, `actions`, `after` (end state; empty on delete), `changed_keys`
(attributes whose value changed). Plan-wide: `counts`. Optionally the PR diff
(`requires: [diff]`) and PR title/body (`requires: [pr]`). There is **no `before` value**.
Every checkpoint must be answerable from these alone.

## Procedure

1. **Inventory resource types still in use.** Only these can get checkpoints.
   ```bash
   grep -rhoE '^resource "[a-z0-9_]+"' --include='*.tf' --exclude-dir=.terraform \
     --exclude-dir=.git --exclude-dir=.claude . | sort | uniq -c | sort -rn
   ```
   When the request names resource types, find the files that declare them and list the PRs
   that touched those files; step 2 then runs over that list only.
   ```bash
   grep -rlE '^resource "<type>"' --include='*.tf' --exclude-dir=.terraform --exclude-dir=.git \
     --exclude-dir=.claude . | xargs git log --oneline --
   ```
   Failures found on a neighbouring type (the attachment or policy resource of the
   requested one) become cards under their own type, marked "neighbouring" in the report.
2. **Collect failure signals before reading anything in depth.** See
   `references/extraction-criteria.md` § Sources for the list and the commands. Always pass
   `--repo <owner>/<name>` to `gh`. A shell loop of `gh` calls can be refused or fail on TLS in
   a sandbox: put the loop in a script file and run it with `bash`.
3. **Read each signalled PR together with the PR it fixes or reverts** — body, diff, and bot
   comments carrying plan/apply output. Human review comments are often absent.
4. **Write one candidate card per failure mode**, grouped by resource type:
   `type · what happened · plan-visible trigger (attribute + value) · evidence PRs`.
5. **Run every card through the gates** in `references/extraction-criteria.md`. Record each
   dropped card with the gate that dropped it.
6. **Confirm the mechanism in a primary source** (provider resource docs, cloud API docs,
   provider issue tracker) with WebFetch. A PR author's explanation is not a source. No source
   → the guidance states only the plan-visible condition, with no claim about why. The current
   source contradicts the failure (the documented limit or default no longer holds for the
   provider version the repository pins) → drop the card. A doc that only shows the pattern in
   an example, without a reason, confirms the condition but not a mechanism.
7. **Write the checkpoints** with `references/writing-checkpoints.md`.
8. **Fold them into `.tfreview.yaml`** with `references/merging.md`.
9. **Validate** (below).
10. **Report and ask for approval** (below). Write the file only after approval.

## Validate

Schema — any extracted plan works, the mock provider needs no API key:

```bash
tfreview extract --show-json <any terraform show -json output> --target t --out /tmp/t.json
TFREVIEW_ALLOW_MOCK=1 tfreview review --provider mock --config .tfreview.yaml \
  --plan /tmp/t.json --out-dir /tmp/tfreview-out
```

Decidability — for each checkpoint whose source PR still has a plan artifact:

```bash
tfreview fetch --pr <n> --repo <owner>/<name> --out-dir /tmp/tfreview-plans
tfreview review --plan /tmp/tfreview-plans/*.json --config .tfreview.yaml --out-dir /tmp/tfreview-out
```

The checkpoint must be `hit` in `/tmp/tfreview-out/result.json`. If it is not, rewrite it to
ask only about what that plan shows, or drop it. If `fetch` finds no artifact, check why before
giving up — `fetch` reports an expired artifact the same way as a missing one, and artifact
retention can be much shorter than the PR history:

```bash
gh api "repos/<owner>/<name>/actions/artifacts?per_page=100" \
  --jq '.artifacts[] | [.name, .expired, .created_at] | @tsv'
```

Expired, absent, or only a binary plan → mark the checkpoint **unverified** in the report.
Never report it as verified because the schema passed.

## Report

Show this before writing, then the YAML diff:

| checkpoint | type | aspect / severity | evidence PRs | trigger in plan | doc URL or "none" | verified: hit / unverified |
| --- | --- | --- | --- | --- | --- | --- |

Followed by the dropped cards: `card · gate that dropped it`.

When no card survives, report the table empty with the dropped cards, and write no YAML:
a config with no checkpoints judges exactly like no config.

## Common mistakes

| Mistake | Instead |
| --- | --- |
| Keeping a card because it "might" be decidable, with a note saying so | A card that fails a gate is dropped; the note goes in the dropped list |
| Rule for a resource type the repo no longer declares, or for a failure that never happened | Gate 1 and gate 2 |
| Guidance that explains the incident, the fix, or "in this repository…" | Guidance is the question the model answers about this plan |
| Mechanism copied from the PR body into guidance | Confirm it in a primary source, or state only the condition |
| Copying `aspects` from memory or from an old example | Copy from the installed version's `internal/config/default.yaml`, see `references/merging.md` |
| "Validated" meaning the mock run exited 0 | Schema passing says nothing about whether a checkpoint can hit |
