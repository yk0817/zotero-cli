#!/usr/bin/env bash
# hook-validate-test-contract.sh の回帰テスト。
# 実行: bash scripts/tests/test-hook-validate-test-contract.sh（全パスで exit 0）
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOK="$SCRIPT_DIR/../hook-validate-test-contract.sh"

if ! command -v jq > /dev/null 2>&1; then
  echo "SKIP: jq not installed (required to build hook input)"
  exit 0
fi

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

PASS=0
FAIL=0

# run_hook <file_path> — JSONを組み立ててhookに流し、exit codeを返す
run_hook() {
  jq -n --arg fp "$1" '{tool_input: {file_path: $fp}}' \
    | bash "$HOOK" > /dev/null 2> "$TMP_ROOT/stderr.txt"
}

assert_case() {
  local name="$1" want_exit="$2" got_exit="$3" want_stderr="${4:-}"
  if [ "$got_exit" -ne "$want_exit" ]; then
    echo "FAIL: $name (exit: want $want_exit, got $got_exit)" >&2
    cat "$TMP_ROOT/stderr.txt" >&2
    FAIL=$((FAIL + 1))
    return
  fi
  if [ -n "$want_stderr" ] && ! grep -q "$want_stderr" "$TMP_ROOT/stderr.txt"; then
    echo "FAIL: $name (stderr does not contain: $want_stderr)" >&2
    cat "$TMP_ROOT/stderr.txt" >&2
    FAIL=$((FAIL + 1))
    return
  fi
  PASS=$((PASS + 1))
}

# --- ケース1: 非テストファイルは素通し ---
cat > "$TMP_ROOT/main.go" <<'EOF'
package main

func main() {}
EOF
run_hook "$TMP_ROOT/main.go"
assert_case "non-test go file passes through" 0 $?

# --- ケース2: 全Test関数にContractコメントがあれば通過 ---
cat > "$TMP_ROOT/ok_test.go" <<'EOF'
package zotero

import "testing"

// Contract: example behavior is fixed so callers can rely on it.
func TestExample(t *testing.T) {
	t.Log("ok")
}

// Contract: another behavior, spanning
// multiple comment lines.
func TestAnother(t *testing.T) {
	t.Log("ok")
}
EOF
run_hook "$TMP_ROOT/ok_test.go"
assert_case "test file with Contract comments passes" 0 $?

# --- ケース3: Contract欠落はブロックされ、関数名がstderrに出る ---
cat > "$TMP_ROOT/missing_test.go" <<'EOF'
package zotero

import "testing"

// Contract: this one is fine.
func TestCovered(t *testing.T) {
	t.Log("ok")
}

// このコメントには契約が書かれていない
func TestMissingContract(t *testing.T) {
	t.Log("ng")
}
EOF
run_hook "$TMP_ROOT/missing_test.go"
assert_case "missing Contract is blocked with function name" 2 $? "TestMissingContract"

# --- ケース4: コメントとfuncの間の空行は無効（Goのdocコメント規約） ---
cat > "$TMP_ROOT/blank_test.go" <<'EOF'
package zotero

import "testing"

// Contract: detached comment does not count.

func TestDetachedComment(t *testing.T) {
	t.Log("ng")
}
EOF
run_hook "$TMP_ROOT/blank_test.go"
assert_case "blank line between comment and func is blocked" 2 $? "TestDetachedComment"

# --- ケース5: TestMainはContract不要 ---
cat > "$TMP_ROOT/main_test.go" <<'EOF'
package zotero

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
EOF
run_hook "$TMP_ROOT/main_test.go"
assert_case "TestMain is exempt" 0 $?

# --- ケース6: 存在しないファイルは素通し（削除直後など） ---
run_hook "$TMP_ROOT/no_such_test.go"
assert_case "nonexistent file passes through" 0 $?

# --- ケース7: file_pathを持たないツール入力は素通し ---
printf '%s' '{"tool_input":{"command":"ls"}}' \
  | bash "$HOOK" > /dev/null 2> "$TMP_ROOT/stderr.txt"
assert_case "input without file_path passes through" 0 $?

# --- ケース8: gofmt崩れはブロック（gofmt導入時のみ） ---
if command -v gofmt > /dev/null 2>&1; then
  cat > "$TMP_ROOT/fmt_test.go" <<'EOF'
package zotero

import "testing"

// Contract: formatting is enforced.
func TestBadFormat(t *testing.T) {
		t.Log( "badly formatted" )
}
EOF
  run_hook "$TMP_ROOT/fmt_test.go"
  assert_case "gofmt violation is blocked" 2 $? "gofmt"
else
  echo "SKIP: gofmt not installed"
fi

# --- ケース9: テスト関数でないTest接頭辞ヘルパー（小文字続き）は対象外 ---
cat > "$TMP_ROOT/helper_test.go" <<'EOF'
package zotero

import "testing"

// Contract: real test functions are still checked.
func TestReal(t *testing.T) {
	t.Log("ok")
}

func Testdata(x int) int {
	return x
}
EOF
run_hook "$TMP_ROOT/helper_test.go"
assert_case "lowercase Test-prefix helper is exempt" 0 $?

# --- ケース10: 構文エラーのファイルは素通しせずブロック（gofmt導入時のみ） ---
if command -v gofmt > /dev/null 2>&1; then
  cat > "$TMP_ROOT/broken_test.go" <<'EOF'
package zotero

import "testing"

// Contract: syntax errors must not pass the gate.
func TestBroken(t *testing.T) {
	t.Log("missing brace"
}
EOF
  run_hook "$TMP_ROOT/broken_test.go"
  assert_case "syntax error is blocked" 2 $? "gofmt"
fi

# --- ケース11: --check-all はリポジトリ内の全 *_test.go を走査する ---
REPO_DIR="$TMP_ROOT/repo"
mkdir -p "$REPO_DIR"
git -C "$REPO_DIR" init -q
git -C "$REPO_DIR" config user.email "test@example.com"
git -C "$REPO_DIR" config user.name "test"
cat > "$REPO_DIR/good_test.go" <<'EOF'
package zotero

import "testing"

// Contract: covered.
func TestGood(t *testing.T) {
	t.Log("ok")
}
EOF
cat > "$REPO_DIR/bad_test.go" <<'EOF'
package zotero

import "testing"

func TestBadNoContract(t *testing.T) {
	t.Log("ng")
}
EOF
git -C "$REPO_DIR" add -A
(cd "$REPO_DIR" && bash "$HOOK" --check-all > /dev/null 2> "$TMP_ROOT/stderr.txt")
assert_case "--check-all detects violation in tracked files" 2 $? "TestBadNoContract"

rm "$REPO_DIR/bad_test.go"
git -C "$REPO_DIR" add -A
(cd "$REPO_DIR" && bash "$HOOK" --check-all > /dev/null 2> "$TMP_ROOT/stderr.txt")
assert_case "--check-all passes when all tracked files comply" 0 $?

echo "---"
echo "PASS: $PASS, FAIL: $FAIL"
[ "$FAIL" -eq 0 ]
