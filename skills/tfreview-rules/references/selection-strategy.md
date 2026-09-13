# Selection strategy

Reading the newest N PRs spends most of the budget on PRs that went fine, and leaves most
managed resource types without a single card. Select by signal, and measure coverage per
managed resource type instead of PR count.

## 1. Coverage map

List the root modules CI plans (from the CI configuration: the directories its plan jobs run
in), then count what they manage:

```bash
scripts/managed-types.sh <root-dir>...
```

It follows local `module` sources from each root and ignores subdirectories no module calls
and commented-out module blocks, so it lists what CI actually plans. Every type in its output
gets a row in the coverage table (SKILL.md § Report). Types outside it fail gate 1.

## 2. Rank PRs by signal

```bash
scripts/rank-prs.sh <owner/name> [apply-check-regex] [max-prs]
```

One GraphQL sweep over up to 1000 PRs. It scores each PR:

| Signal | Score | Why it matters |
| --- | --- | --- |
| A check run matching `apply-check-regex` failed on the merge commit | +5 | The change reached apply and broke. Check runs outlive their logs |
| Title is a revert | +4 | Something was bad enough to undo |
| Title is a fix-up (`fix`, `follow up`, `hotfix`, `#<n>`) | +2 | Points at the PR that needed fixing |
| Closed without merging, with comments | +2 | Abandoned after something was found |
| Review threads (max 3) | +1 each | Line comments name a concrete concern |
| Comments, per 3 (max 3) | +1 | Many plan/apply bot comments mean many pushes to get it right |
| More than 3 commits | +1 | Iteration |

Dependency bot PRs are kept only when they failed apply or were reverted. The last column is
the first two path segments of every file touched: map them to roots, and roots to types with
the coverage map.

For a fix-up or revert, read the PR it points at in the same pass; the pair is one incident.

## 3. Read per type until saturated

Work down the coverage map, most-declared types first:

1. Take the highest-ranked PRs touching the type's roots. Read body, diff and bot comments
   (`gh pr view <n> --repo R --json body,comments`, `gh pr diff <n> --repo R`).
2. Stop reading PRs for a type after **two consecutive PRs add no new card**, or when no ranked
   PR touches it.
3. Search the current code of the type's roots for guards: `ignore_changes`,
   `prevent_destroy`, and comments giving a reason. `git log -S '<text>' --oneline --follow --
   <file>` finds the PR that added one, including across file moves.
4. Read the provider resource page for the type at the version the root pins
   (`.terraform.lock.hcl`). Every managed type gets this pass, whatever its history.

Incidents recorded under a predecessor (a deprecated inline block or an old resource type that
the current type replaced) become cards under the current type, labelled `predecessor` in the
report.

Failed apply logs expire (90 days by default), but the failed run and the bot comments on the
PR stay. Read those before giving up on a failure's cause.
