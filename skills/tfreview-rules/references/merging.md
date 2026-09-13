# Merging into .tfreview.yaml

## No .tfreview.yaml yet

The built-in checks apply only when a config has neither `aspects` nor
`checkpoints_for_resource`. Adding checkpoints therefore replaces the built-ins, and
`config.Parse` rejects a config without `aspects`. Start from the built-ins of the tfreview
version the repository runs:

```bash
tfreview --version
gh api "repos/nakamasato/tfreview/contents/internal/config/default.yaml?ref=v<version>" \
  --jq .content | base64 -d > .tfreview.yaml
```

Keep the copied aspects and their checks unchanged; edits to generic checks are proposed
separately, one at a time, so each can be judged on its own. New aspects that checkpoints need
are appended after the copied ones.

## Existing .tfreview.yaml

For each new checkpoint:

1. Look under the same resource type.
2. If an existing checkpoint asks the same question, merge the guidance into it and append
   `references`. Do not add a near-duplicate id.
3. Otherwise append it.

Keep ids stable. `state.json` and the PR comment key on them: renaming an id silently
re-judges every target and loses its history. Change the guidance, not the id.

Ids must be unique across all checks and checkpoints; `config.Parse` fails on a collision.
