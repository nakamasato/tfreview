# Reviewing historical pull requests locally

Use `tfreview eval` to fetch plans from past PRs, review them with the current
configuration, and write a local Markdown or HTML report. The plan files are kept in a
temporary directory and removed when the command exits; no plan fixtures need
to be added to the repository.

Run it directly with Go and pass one or more PR numbers:

```sh
go run ./cmd/tfreview eval --repo acme/widgets --pr 123,456
```

For a browsable HTML report:

```sh
go run ./cmd/tfreview eval --repo acme/widgets --pr 123,456 --format html --out /tmp/pr-review.html
```

The command uses the normal GitHub token lookup (`GITHUB_TOKEN`, `GH_TOKEN`, or
`gh auth token`) and the current `.tfreview.yaml`, falling back to built-in
checks when there is no config. The model defaults to `claude-sonnet-5-5`. Set
`--config`, `--provider`, and `--model` to compare a specific setup. For example:

To produce both requested phases, pass Jev as the primary scorer and Anthropic
as its second-pass judge:

```sh
go run ./cmd/tfreview eval --repo acme/widgets --pr 123,456 --provider jev --deep-dive anthropic --model claude-sonnet-5-5 --format html --out /tmp/pr-review.html
```

Set `TYPESAFE_API_KEY` and `ANTHROPIC_API_KEY` before running. The Phase 1
matrix shows each resource's Jev score for each check; green is at or below the
miss threshold, red is at or above the hit threshold, and yellow is between.

```sh
go run ./cmd/tfreview eval --repo acme/widgets --pr 123,456 \
  --provider jev --deep-dive anthropic --model claude-sonnet-5-5 --format html --out /tmp/pr-review.html
```

The default report name is `tfreview-eval-<timestamp>.md` for Markdown or
`.html` for HTML; both are ignored by Git. Choose an explicit path under
`/tmp` for extra separation. The report
contains check verdicts, resource addresses, and reasons, so treat it as
private and review it before sharing. Only the report remains after the run;
the downloaded plans are deleted with the temporary directory.

The report shows what the current tfreview configuration decides for each
historical plan, with per-check detail and estimated cost. It is useful for
spotting unexpected findings and reviewing missed risks. It does not claim
accuracy against ground truth automatically: a reviewer still needs to assess
the plan and mark whether each finding is correct. Historical artifacts may
have expired. A plan recreated today can differ because Terraform state,
providers, and remote data may have changed.

This local workflow complements `TFREVIEW_EVAL=1 go test ./eval`, which remains
the labeled fixture evaluation for prompt and model regression checks.
