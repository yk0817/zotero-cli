#!/usr/bin/env bash
# hook-readme-sync.sh のルーティング回帰テスト。
# 実行: bash scripts/tests/test-hook-readme-sync.sh（全パスで exit 0）
#
# `git push` を含まない Bash 呼び出しは generator（go）を起動せず素通しすること、
# PreToolUse として入力 JSON をそのまま stdout に渡すことを検証する。stale 検出
# など生成ロジック自体は test-gen-readme.sh が担保する。
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOK="$SCRIPT_DIR/../hook-readme-sync.sh"

if ! command -v jq > /dev/null 2>&1; then
  echo "SKIP: jq not installed"
  exit 0
fi

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

PASS=0
FAIL=0
assert() {
  if [ "$2" = "0" ]; then PASS=$((PASS + 1)); else echo "FAIL: $1" >&2; FAIL=$((FAIL + 1)); fi
}

# run_hook <command> — 入力 JSON を組み立てて hook に流し、stdout/exit を捕捉
run_hook() {
  jq -n --arg cmd "$1" '{tool_input: {command: $cmd}}' \
    | bash "$HOOK" > "$TMP_ROOT/stdout.txt" 2> "$TMP_ROOT/stderr.txt"
}

# --- ケース1: 非 push コマンドは generator を呼ばず素通し（exit 0）---
run_hook "ls -la"
[ "$?" -eq 0 ]; assert "non-push command passes (exit 0)" "$?"

# --- ケース2: PreToolUse なので入力 JSON を stdout にそのまま渡す ---
run_hook "git status"
allowed=$?
grep -q 'git status' "$TMP_ROOT/stdout.txt"; assert "passes input JSON through on stdout" "$?"
[ "$allowed" -eq 0 ]; assert "git status (non-push) allowed" "$?"

# --- ケース3: 'push' 単独（'git push' ではない）は誤検出しない（exit 0, go 不起動）---
run_hook "echo pushing to remote"
[ "$?" -eq 0 ]; assert "the word 'push' alone is not treated as a git push" "$?"

echo "hook-readme-sync routing tests: ${PASS} passed, ${FAIL} failed"
[ "$FAIL" -eq 0 ]
