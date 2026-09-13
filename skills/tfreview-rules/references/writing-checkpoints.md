# Writing checkpoints

A checkpoint:

```yaml
checkpoints_for_resource:
  <resource_type>:
    - id: <type-prefix>-<what>          # stable; state and comments key on it
      aspect: <existing aspect id>
      severity: medium | high | critical
      requires: [diff]                 # only when the trigger is HCL structure
      references:                      # primary sources; never sent to the model
        - https://...
      guidance: |
        <question> <what to look at, and from where> <one sentence of why, only if sourced>
```

## Guidance is a question about this plan

Bad — no attribute, no source, nothing to decide:

```yaml
guidance: Follow least privilege.
```

Bad — incident narrative, remediation, and an unsourced mechanism; the model cannot use any of
it, and "in this repository" leaks context into a rule:

```yaml
guidance: |
  In this repository a bucket was once emptied by mistake. The fix was to add a
  lifecycle rule. The API is known to be slow here.
```

Good:

```yaml
guidance: |
  Is the role being granted roles/owner, roles/editor, or a *.admin role at the project
  level? Project-level grants apply to every current and future resource in the project,
  so say whether the same access could be expressed on a single resource instead.
```

## Rules

- Open with a yes/no question the plan can answer.
- Name the attribute(s) and the value(s) that count.
- Say where to decide from: `changed_keys` for "this change did X", `after` for "the end state
  is X", `actions` for create / delete / replace.
- State the negative case when it is easy to confuse ("removing a member is not a grant").
- A mechanism sentence ("…because the API rejects…") only when a URL in `references` says so.
  When the guidance asserts a provider default, name the provider version it was confirmed
  against.
- No remediation, no incident history, no "in this repository".
- `severity`: `critical` only for an unambiguous accident (data loss, outage, public exposure);
  `high` for a likely failed apply or unwanted access; `medium` for noise, drift, cost.
- `aspect`: pick an existing aspect. Add a new aspect only for a risk class none of them
  covers, with a `title` naming the risk; `checks: []` is valid for an aspect that only
  checkpoints aggregate into.
- Never write a concrete project id, account id, ARN, bucket, repository, or service account
  name.
