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

# 2段フィルタ。まず生の JSON 文字列に "git push" が無ければ jq すら起動せず即
# 素通し（大半の Bash 呼び出しはここで終わる）。本判定は下の jq でパースした
# tool_input.command に対して行う（こちらが正本）。
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
# 再生成自体が失敗した場合（go/jq 未導入など）は、ツール不調で push を妨げない
# ことを優先して fail-open で通す（意図的。ここを exit 1/2 にすると hook が
# ビルドの硬依存になる）。原因を push のブロックと混同させないメッセージにする。
if ! bash "$GEN" > /dev/null 2>&1; then
  echo "[hook-readme-sync] could not regenerate README (go/jq unavailable?); allowing push without sync" >&2
  exit 0
fi

{
  echo "[hook-readme-sync] README.md was out of date with 'zotero-cli schema' and has been"
  echo "regenerated on disk. Review and commit it, then push again:"
  echo "    git add README.md && git commit -m 'docs: sync README command table' "
} >&2
exit 2
