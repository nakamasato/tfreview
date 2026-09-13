# Extraction criteria

## Sources

Read signals first, in this order. They are cheap to list and point at the few PRs worth
reading in full.

| Signal | How to find it |
| --- | --- |
| Reverts | `gh pr list --repo R --state merged --search "revert in:title" --limit 100 --json number,title` |
| Fix-ups of a previous PR | titles with `fix`, `follow up`, `hotfix`, or a `#<n>` reference to an earlier PR |
| Failed apply / plan in bot comments | `gh pr view <n> --repo R --json comments --jq '.comments[].body'`, then grep for `Error:` and the failure heading your CI bot posts |
| Failed apply workflow runs | `gh run list --repo R --workflow <apply workflow> --status failure --json databaseId,displayTitle,headSha`, then `gh run view <id> --repo R --log-failed`. Logs expire (90 days by default); a run with no log still names the PR |
| Closed without merging | `gh pr list --repo R --state closed --json number,title,mergedAt --jq '.[] \| select(.mergedAt == null)'` |
| Risk avoided on purpose | PR bodies that say why something was *not* done (kept disabled, pinned, left manual) |

Dependency update PRs are signals only when a fix-up or revert follows them.

For each signalled PR read the body, `gh pr diff <n> --repo R`, and the PR it fixes or reverts.

## Gates

Apply in order. The first gate a card fails drops it.

1. **The resource type is still declared** in the repository (SKILL.md step 1).
2. **It happened, or was explicitly avoided in a PR.** A plausible failure nobody hit or
   named is not evidence.
3. **The trigger is visible in the plan for that resource type**: a value in `after`, a key in
   `changed_keys`, or `actions`. Where the error surfaced does not matter — an apply-time
   rejection whose cause is an attribute value in the plan passes this gate. It fails when:
   - it needs the previous value (a threshold "dropped", a setting "was" something) and the
     change is not expressible as `changed_keys` plus the new value;
   - it needs `after` of a deleted resource (it is empty);
   - it depends on timing, ordering, or eventual consistency between resources;
   - the cause is outside the plan — credentials, CI permissions, quotas, billing, state locks,
     stale plans, lock files, provider warnings — **and** no attribute in the plan separates the
     failing case from the working one. When an attribute does (the failure occurs only for one
     value of an attribute), the card passes and the trigger is that attribute.
4. **HCL structure** (`lifecycle`, `ignore_changes`, `depends_on`, `for_each` shape) passes only
   with `requires: [diff]`.
5. **Not already covered** by a generic check in `aspects[].checks`. A generic check covers a
   card when it would hit on the same trigger — for example any card whose trigger is only
   `actions` containing delete is covered by `delete-or-replace`. A card passes when its trigger
   is narrower (a specific attribute or value). If a generic check needs widening (another
   resource type in `stateful-delete`, another guard attribute), propose that edit instead of
   a checkpoint.
6. **No repository-specific values** — project ids, account ids, ARNs, service account
   addresses, bucket or repository names. Generalise to the attribute and the value class, or
   drop.

Environment-specific facts (an organisation policy, a network layout) may stay in the
consuming repository's own config, but only as the plan-visible condition; the mechanism still
needs a primary source.
