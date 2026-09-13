#!/usr/bin/env bash
# Rank a repository's pull requests by how likely they carry review knowledge.
#
# Usage: rank-prs.sh <owner/name> [apply-check-regex] [max-prs]
#   apply-check-regex  matched against check run names on the merge commit (default: apply)
#   max-prs            newest PRs to scan (default: 1000)
#
# Output (TSV, highest score first):
#   score  number  state  apply  comments  threads  commits  author  title  top-dirs
set -euo pipefail

repo="${1:?usage: rank-prs.sh <owner/name> [apply-check-regex] [max-prs]}"
apply_re="${2:-apply}"
max="${3:-1000}"
owner="${repo%%/*}"
name="${repo##*/}"

# shellcheck disable=SC2016 # $owner etc. are GraphQL variables, not shell
query='
query($owner: String!, $name: String!, $endCursor: String) {
  repository(owner: $owner, name: $name) {
    pullRequests(first: 50, after: $endCursor, orderBy: {field: CREATED_AT, direction: DESC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        number title state
        author { login }
        comments { totalCount }
        reviewThreads { totalCount }
        commits { totalCount }
        files(first: 100) { nodes { path } }
        mergeCommit {
          checkSuites(first: 20) {
            nodes { checkRuns(first: 20) { nodes { name conclusion } } }
          }
        }
      }
    }
  }
}'

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
# --paginate walks every page; stop reading once max PRs are collected.
gh api graphql --paginate -f owner="$owner" -f name="$name" -f query="$query" \
  --jq '.data.repository.pullRequests.nodes[]' | head -n "$max" > "$tmp"

jq -r --arg re "$apply_re" '
  def bot: (.author.login // "") | test("\\[bot\\]$|^renovate|^dependabot"; "i");
  ([.mergeCommit.checkSuites.nodes[]?.checkRuns.nodes[]?
     | select(.name | test($re; "i")) | .conclusion] | any(. == "FAILURE")) as $apply_failed
  | (.title | test("^revert|\\brevert"; "i")) as $revert
  | (.title | test("\\bfix|follow.?up|hotfix|#[0-9]+"; "i")) as $fixup
  | ( (if $apply_failed then 5 else 0 end)
    + (if $revert then 4 elif $fixup then 2 else 0 end)
    + (if .state == "CLOSED" and .comments.totalCount > 0 then 2 else 0 end)
    + ([.reviewThreads.totalCount, 3] | min)
    + ([(.comments.totalCount / 3 | floor), 3] | min)
    + (if .commits.totalCount > 3 then 1 else 0 end)
    ) as $score
  # A dependency bot PR only counts when something went wrong with it.
  | select((bot | not) or $apply_failed or $revert)
  | select($score > 0)
  | [ $score, .number, .state,
      (if $apply_failed then "apply-failed" else "-" end),
      .comments.totalCount, .reviewThreads.totalCount, .commits.totalCount,
      (.author.login // "-"), .title,
      ([.files.nodes[].path | split("/") | .[0:2] | join("/")] | unique | join(","))
    ] | @tsv
' "$tmp" | sort -t$'\t' -k1,1nr -k2,2nr
