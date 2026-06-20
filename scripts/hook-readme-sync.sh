#!/usr/bin/env bash
# PreToolUse(Bash) hook: `git push` の直前に README のコマンド表を schema と
# 同期する。push されるコミットにドキュメントのズレを持ち込ませないための門番。
#
# PreToolUse(Bash) hook: before a `git push`, regenerate the README command
# table from `zotero-cli schema`. If it was stale, the file is updated on disk
# and the push is BLOCKED (exit 2) so the human reviews and commits the change
# — a hook cannot inject a file into the in-flight commit, so it stops and asks.
#
# exit codes: 0 = allow the push, 2 = block (reason on stderr).
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GEN="${SCRIPT_DIR}/gen-readme.sh"

# PreToolUse: stdin の入力 JSON はそのまま後続に渡す（パススルー）。
input="$(cat)"
printf '%s' "$input"

# 対象は `git push` のみ。それ以外の Bash 呼び出しは即素通し（go run を避ける）。
case "$input" in
  *"git push"*) ;;
  *) exit 0 ;;
esac

if ! command -v jq > /dev/null 2>&1; then
  echo "[hook-readme-sync] internal: jq not installed; skipping" >&2
  exit 0
fi

command="$(printf '%s' "$input" | jq -r '.tool_input.command // empty')"
case "$command" in
  *"git push"*) ;;
  *) exit 0 ;;
esac

# 既に同期済みなら push を通す。
if bash "$GEN" --check > /dev/null 2>&1; then
  exit 0
fi

# ズレていた: README を更新したうえで push を止め、コミットを促す。
if ! bash "$GEN" > /dev/null 2>&1; then
  echo "[hook-readme-sync] internal: failed to regenerate README; allowing push" >&2
  exit 0
fi

{
  echo "[hook-readme-sync] README.md was out of date with 'zotero-cli schema' and has been"
  echo "regenerated on disk. Review and commit it, then push again:"
  echo "    git add README.md && git commit -m 'docs: sync README command table' "
} >&2
exit 2
