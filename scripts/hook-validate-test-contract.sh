#!/usr/bin/env bash
# PostToolUse(Write|Edit) hook: *_test.go の品質ゲート。
#   1. すべてのトップレベル Test 関数の直前に「Contract:」コメントがあること
#      （knowledge-base/test-style-guide.md の必須ルール。TestMain は対象外）
#   2. gofmt 済みであること
# 違反は exit 2 でブロックし、理由を stderr に出す。
# 対象外ファイル・内部エラー以外の判定不能ケースは素通し（exit 0）。
set -u

input="$(cat)"
# 入力JSONはstdoutへパススルーする（hook規約）
printf '%s' "$input"

if ! command -v jq > /dev/null 2>&1; then
  # 内部エラー（検出によるブロックと区別する）。安全網ではないので素通し
  echo "[hook-validate-test-contract] internal: jq not installed; skipping" >&2
  exit 0
fi

file_path="$(printf '%s' "$input" | jq -r '.tool_input.file_path // empty' | head -1)"

case "$file_path" in
  *_test.go) ;;
  *) exit 0 ;;
esac

if [ ! -f "$file_path" ]; then
  exit 0
fi

# --- 検証1: Contract コメント ---
# 直前のコメントブロック（空行で分断されないもの）に Contract: が含まれない
# トップレベル Test 関数を列挙する。TestMain は対象外。
missing="$(awk '
  /^\/\// {
    if ($0 ~ /Contract:/) has = 1
    next
  }
  /^func Test/ {
    name = $2
    sub(/\(.*/, "", name)
    if (name != "TestMain" && !has) print name
    has = 0
    next
  }
  { has = 0 }
' "$file_path")"

if [ -n "$missing" ]; then
  {
    echo "[hook-validate-test-contract] Contract コメントのない Test 関数があります:"
    printf '  - %s\n' $missing
    echo "各 Test 関数の直前に // Contract: で「何の契約を固定するか・なぜ必要か」を書いてください。"
    echo "規約: knowledge-base/test-style-guide.md"
  } >&2
  exit 2
fi

# --- 検証2: gofmt ---
if command -v gofmt > /dev/null 2>&1; then
  if [ -n "$(gofmt -l "$file_path")" ]; then
    echo "[hook-validate-test-contract] gofmt 未適用です: ${file_path} (gofmt -w で整形してください)" >&2
    exit 2
  fi
fi

exit 0
