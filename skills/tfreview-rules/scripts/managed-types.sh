#!/usr/bin/env bash
# Count the resource types CI actually plans: the .tf files directly in each root module, plus
# every local module reachable from it through `source = "./..."` / `"../..."`.
#
# A module only reads the .tf files in its own directory, so subdirectories that no module
# calls are not planned and are not counted.
#
# Usage: managed-types.sh <root-dir>...
# Output (TSV, most declared first):  count  type  roots
set -euo pipefail

[ $# -gt 0 ] || { echo "usage: managed-types.sh <root-dir>..." >&2; exit 2; }

tfs() { find "$1" -maxdepth 1 -name '*.tf' -type f; }

walk() {
  local dir="$1" root="$2" seen="$3"
  grep -qxF "$dir" "$seen" && return 0
  echo "$dir" >> "$seen"
  local files
  files="$(tfs "$dir")"
  [ -n "$files" ] || return 0
  # shellcheck disable=SC2086
  grep -hoE '^resource "[a-z0-9_]+"' $files \
    | awk -v r="$root" '{ gsub(/^resource "|"$/, ""); print $0 "\t" r }' || true
  # Only `source` inside a module block: provisioners and object resources also have a
  # `source` path, and following those would count types CI never plans. Line-anchored so
  # commented-out blocks are skipped.
  # shellcheck disable=SC2086
  awk '/^module[[:space:]]+"/ { m = 1 } /^}/ { m = 0 }
       m && match($0, /^[[:space:]]*source[[:space:]]*=[[:space:]]*"\.\.?\/[^"]+"/) {
         s = substr($0, RSTART, RLENGTH); sub(/^[^"]*"/, "", s); sub(/"$/, "", s); print s }' $files \
    | sort -u \
    | while read -r rel; do
        local child
        child="$(cd "$dir" && cd "$rel" 2>/dev/null && pwd)" || continue
        walk "$child" "$root" "$seen"
      done
}

seen="$(mktemp)"
trap 'rm -f "$seen"' EXIT
for root in "$@"; do
  : > "$seen"
  walk "$(cd "$root" && pwd)" "${root%/}" "$seen"
done | awk -F'\t' '
  { n[$1]++; if (index("," r[$1] ",", "," $2 ",") == 0) r[$1] = (r[$1] == "" ? $2 : r[$1] "," $2) }
  END { for (t in n) printf "%d\t%s\t%s\n", n[t], t, r[t] }
' | sort -t$'\t' -k1,1nr -k2,2
