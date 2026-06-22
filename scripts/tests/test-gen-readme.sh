#!/usr/bin/env bash
# gen-readme.sh の回帰テスト。
# 実行: bash scripts/tests/test-gen-readme.sh（全パスで exit 0）
#
# hermetic: 実 README や go には触れず、GEN_README_TARGET（対象ファイル）と
# GEN_README_SCHEMA_FILE（schema JSON のスタブ）で隔離する。
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GEN="$SCRIPT_DIR/../gen-readme.sh"

if ! command -v jq > /dev/null 2>&1; then
  echo "SKIP: jq not installed"
  exit 0
fi

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

PASS=0
FAIL=0

assert() {
  local name="$1" cond="$2"
  if [ "$cond" = "0" ]; then
    PASS=$((PASS + 1))
  else
    echo "FAIL: ${name}" >&2
    FAIL=$((FAIL + 1))
  fi
}

# schema スタブ（A: 2コマンド, B: 3コマンド）
cat > "$TMP_ROOT/schemaA.json" <<'EOF'
[{"name":"beta","description":"Second cmd","args":"y: optional"},
 {"name":"alpha","description":"First cmd","args":"x: required"}]
EOF
cat > "$TMP_ROOT/schemaB.json" <<'EOF'
[{"name":"beta","description":"Second cmd","args":"y: optional"},
 {"name":"alpha","description":"First cmd","args":"x: required"},
 {"name":"gamma","description":"Third cmd","args":null}]
EOF

# マーカー付き README スケルトン。ブロックの後ろにマーカー文字列を「言及」する
# 散文を置き、それが誤ってマーカー扱いされない（行頭アンカー）ことも検証する。
README="$TMP_ROOT/README.md"
make_readme() {
  cat > "$README" <<'EOF'
# Title

intro paragraph

<!-- BEGIN AUTO-GENERATED COMMANDS (test) -->
<!-- END AUTO-GENERATED COMMANDS -->

## After

Edit the block between the `<!-- BEGIN AUTO-GENERATED COMMANDS -->` markers.

TAIL_SENTINEL
EOF
}

run() { GEN_README_TARGET="$README" GEN_README_SCHEMA_FILE="$1" bash "$GEN" "${2:-}" > /dev/null 2>&1; }

# --- ケース1: 生成（default）でブロックが埋まり、後続の散文と末尾が残る ---
make_readme
run "$TMP_ROOT/schemaA.json"
assert "default fills the block" "$?"
grep -q '| `alpha` | First cmd | x: required |' "$README"; assert "alpha row present" "$?"
grep -q 'TAIL_SENTINEL' "$README"; assert "content after block survives (no marker-in-prose drop)" "$?"
# alpha が beta より前（sort_by(.name)）
awk '/`alpha`/{a=NR} /`beta`/{b=NR} END{exit !(a<b)}' "$README"; assert "commands sorted by name" "$?"

# --- ケース2: 同じ schema なら --check は通る（冪等）---
run "$TMP_ROOT/schemaA.json" --check
assert "--check passes when in sync" "$?"

# --- ケース3: schema が変わると --check は stale を検出（exit 1）---
GEN_README_TARGET="$README" GEN_README_SCHEMA_FILE="$TMP_ROOT/schemaB.json" bash "$GEN" --check > /dev/null 2>&1
[ "$?" -eq 1 ]; assert "--check detects drift (exit 1)" "$?"

# --- ケース4: default で再生成すると追従し、--check が再び通る ---
run "$TMP_ROOT/schemaB.json"
assert "default re-syncs to new schema" "$?"
grep -q '| `gamma` |' "$README"; assert "new command added" "$?"
run "$TMP_ROOT/schemaB.json" --check
assert "--check passes after re-sync" "$?"

# --- ケース5: マーカーが無い README はエラー（exit 1, 理由を表示）---
NOMARK="$TMP_ROOT/nomarker.md"
printf '# No markers here\n\njust text\n' > "$NOMARK"
err="$(GEN_README_TARGET="$NOMARK" GEN_README_SCHEMA_FILE="$TMP_ROOT/schemaA.json" bash "$GEN" 2>&1)"
code=$?
[ "$code" -eq 1 ]; assert "missing markers fails (exit 1)" "$?"
printf '%s' "$err" | grep -q 'expected exactly 1 BEGIN marker'; assert "missing markers explains why" "$?"

# --- ケース6: マーカーが重複した README はエラー（二重挿入を防ぐ）---
DUP="$TMP_ROOT/dup.md"
cat > "$DUP" <<'EOF'
# Dup

<!-- BEGIN AUTO-GENERATED COMMANDS (a) -->
<!-- END AUTO-GENERATED COMMANDS -->

<!-- BEGIN AUTO-GENERATED COMMANDS (b) -->
<!-- END AUTO-GENERATED COMMANDS -->
EOF
err="$(GEN_README_TARGET="$DUP" GEN_README_SCHEMA_FILE="$TMP_ROOT/schemaA.json" bash "$GEN" 2>&1)"
code=$?
[ "$code" -eq 1 ]; assert "duplicate markers fail (exit 1)" "$?"
printf '%s' "$err" | grep -q 'found 2'; assert "duplicate markers report the count" "$?"

echo "gen-readme tests: ${PASS} passed, ${FAIL} failed"
[ "$FAIL" -eq 0 ]
