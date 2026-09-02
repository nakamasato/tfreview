#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
# curl を差し替えて URL を記録する
fake="$(mktemp -d)"
cat > "$fake/curl" <<'EOS'
#!/usr/bin/env bash
echo "$@" >> "$FAKE_LOG"
# 最後の引数が出力先。中身は空 tar
tar -czf "${@: -1}" -T /dev/null
EOS
cat > "$fake/tar" <<'EOS'
#!/usr/bin/env bash
# 展開先に空の tfreview を置く
for ((i=1;i<=$#;i++)); do [ "${!i}" = "-C" ] && j=$((i+1)) && touch "${!j}/tfreview"; done
exit 0
EOS
chmod +x "$fake/curl" "$fake/tar"
export FAKE_LOG="$fake/log"
PATH="$fake:$PATH" bash ./install.sh v1.2.3 "$fake/bin" >/dev/null
grep -q "releases/download/v1.2.3/tfreview_" "$FAKE_LOG"
PATH="$fake:$PATH" bash ./install.sh latest "$fake/bin" >/dev/null
grep -q "releases/latest/download/tfreview_" "$FAKE_LOG"
test -x "$fake/bin/tfreview"
echo ok
