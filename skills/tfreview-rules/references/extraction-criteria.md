# Extraction criteria

What to read, and in which order, is in `selection-strategy.md`. This file decides what
survives.

## Gates

Apply in order. The first gate a card fails drops it.

1. **The resource type is in the coverage map** — declared in a module CI plans.
2. **There is evidence of one of two kinds.** A plausible failure with neither is not evidence.
   - *history*: it happened, or was explicitly avoided in a PR or in the current code.
   - *docs*: the provider or cloud documentation for a managed type states the consequence
     itself — not a best practice ("follow least privilege", "enable versioning"), but "setting
     X does Y". Provider source code may confirm a constraint the docs state; it is not evidence
     on its own. A docs card's severity is at most `high`, and the report labels it `docs`.
     A docs card whose consequence is only drift or cost is dropped unless history backs it.
3. **The trigger is visible in the plan for that resource type**: a value in `after`, a key in
   `changed_keys`, or `actions`. Where the error surfaced does not matter — an apply-time
   rejection whose cause is an attribute value in the plan passes this gate. It fails when:
   - it needs the previous value (a threshold "dropped", a setting "was" something) and the
     change is not expressible as `changed_keys` plus the new value;
   - it needs `after` of a deleted resource (it is empty);
   - it depends on timing, ordering, or eventual consistency between resources;
   - the provider rejects the value during validate or plan — no plan exists to review;
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
   resource type in `stateful-delete`, another guard attribute), list it under "Proposed edits
   to generic checks" in the report instead of writing a checkpoint.
6. **No repository-specific values** — project ids, account ids, ARNs, service account
   addresses, bucket or repository names. Generalise to the attribute and the value class, or
   drop.

Environment-specific facts (an organisation policy, a network layout, the kind of account
that owns the resources) may stay in the consuming repository's own config, but only as the
plan-visible condition; the mechanism still needs a primary source.

When roots pin different provider majors and a default differs between them, write one
checkpoint phrased by what `after` shows under each (a value versus null) and name both
versions.
