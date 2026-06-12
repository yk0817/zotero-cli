#!/usr/bin/env bash
# *_test.go の品質ゲート。
#   1. すべてのトップレベル Test 関数の直前に「Contract:」コメント（// 行コメント）
#      があること（knowledge-base/test-style-guide.md。TestMain と、go test が
#      テストと見なさない小文字続きの Test* ヘルパーは対象外）
#   2. gofmt 済みであること（構文エラーも gofmt が報告するためここで落ちる）
#
# 使い方:
#   - PostToolUse(Write|Edit) hook: stdin の JSON から file_path を読む。
#     違反は exit 2 — 書き込み自体は済んでいるため「取り消し」ではなく、
#     stderr の理由が Claude に返り即時修正を要求する
#   - --check-all: git 管理下の全 *_test.go を走査（CI・手動編集のバイパス対策）
set -u

# check_file <path> — 違反があれば理由をstderrに出して1を返す
check_file() {
  local file="$1"
  local missing fmt_out

  # 直前のコメントブロック（空行で分断されないもの）に Contract: がない
  # トップレベル Test 関数を列挙する。go test の規約に合わせ、Test の直後が
  # 小文字のもの（Testdata 等のヘルパー）と TestMain は対象外。
  missing="$(awk '
    /^\/\// {
      if ($0 ~ /Contract:/) has = 1
      next
    }
    /^func Test[A-Z_(]/ {
      name = $2
      sub(/\(.*/, "", name)
      if (name != "TestMain" && !has) print name
      has = 0
      next
    }
    { has = 0 }
  ' "$file")"

  if [ -n "$missing" ]; then
    {
      echo "[hook-validate-test-contract] Contract コメントのない Test 関数があります (${file}):"
      printf '  - %s\n' $missing
      echo "各 Test 関数の直前に // Contract: で「何の契約を固定するか・なぜ必要か」を書いてください。"
      echo "規約: knowledge-base/test-style-guide.md"
    } >&2
    return 1
  fi

  if command -v gofmt > /dev/null 2>&1; then
    # 2>&1: gofmt は構文エラーを stderr に出すため、捕捉しないと壊れたファイルが素通しする
    fmt_out="$(gofmt -l "$file" 2>&1)"
    if [ -n "$fmt_out" ]; then
      {
        echo "[hook-validate-test-contract] gofmt 未適用または構文エラーです: ${file}"
        echo "$fmt_out"
        echo "(gofmt -w で整形してください)"
      } >&2
      return 1
    fi
  fi

  return 0
}

# --- モード1: リポジトリ全体走査（CI用） ---
if [ "${1:-}" = "--check-all" ]; then
  bad=0
  while IFS= read -r f; do
    [ -f "$f" ] || continue
    check_file "$f" || bad=1
  done < <(git ls-files '*_test.go')
  [ "$bad" -eq 0 ] && exit 0
  exit 2
fi

# --- モード2: PostToolUse hook（stdinのJSONから対象を特定） ---
input="$(cat)"

# 高速パス: 入力に _test.go が含まれなければ jq を起動せず素通し
case "$input" in
  *_test.go*) ;;
  *) exit 0 ;;
esac

if ! command -v jq > /dev/null 2>&1; then
  # 内部エラー（検出によるブロックと区別する）。安全網ではないので素通し
  echo "[hook-validate-test-contract] internal: jq not installed; skipping" >&2
  exit 0
fi

file_path="$(printf '%s' "$input" | jq -r '.tool_input.file_path // empty')"

case "$file_path" in
  *_test.go) ;;
  *) exit 0 ;;
esac

if [ ! -f "$file_path" ]; then
  exit 0
fi

check_file "$file_path" || exit 2
exit 0
