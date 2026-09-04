# tfreview

Go 1.24 / cobra CLI. Takes only the `terraform plan` result as input, reviews it
against criteria defined in YAML, and emits a PR comment and label for each check.
Status is alpha (breaking changes allowed until v1).

## Commands

`extract` (reduce plan JSON) / `review` (judge) / `comment` (post to PR) / `fetch` (fetch a PR's plan)

## Structure

- `cmd/tfreview/` - CLI wiring only. No logic lives here
- `internal/` - split into `plan`, `match`, `judge`, `llm`, `render`, `state`, `github`, `config`
- `internal/config/default.yaml` - built-in checks used when there's no `.tfreview.yaml`

## Invariants

- The plan's `before` never leaves the runner. `extract` keeps only `after` and `changed_keys`
- The LLM only returns hit / miss. Severity (level) is decided by config
- Judgments are cached per target, keyed by a hash of plan + config

## Verification

Run the same checks as CI before pushing:
`go build ./... && go vet ./... && go test ./... && golangci-lint run`

## Public repository

Use neutral names like `acme/widgets` in examples and tests. Never write real company
names, internal repository names, or credentials in code / docs / tests / commit messages.

## Language & commit conventions

- Issues, pull requests, and docs in this repository are written in English.
- Commit messages and PR titles follow [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`, etc.).
