#!/usr/bin/env bash
# README の CLI コマンド一覧を、唯一の真実である `zotero-cli schema` の出力から
# 自動生成して同期する。これによりドキュメントがコードからズレない。
#
# Regenerate the CLI command table in README.md from the canonical
# `zotero-cli schema` output, so the docs cannot drift from the code.
#
# Usage:
#   scripts/gen-readme.sh           # regenerate the marked block in README.md in place
#   scripts/gen-readme.sh --check   # exit 1 (and show a diff) if README.md is stale; no write
#
# The block is delimited in README.md by:
#   <!-- BEGIN AUTO-GENERATED COMMANDS ... -->
#   <!-- END AUTO-GENERATED COMMANDS -->
set -u

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# Overridable for hermetic tests; default to the repo's own README and schema.
README="${GEN_README_TARGET:-${REPO_ROOT}/README.md}"
BEGIN_MARKER="BEGIN AUTO-GENERATED COMMANDS"
END_MARKER="END AUTO-GENERATED COMMANDS"

fail() { echo "[gen-readme] ${1}" >&2; exit 1; }

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

command -v jq > /dev/null 2>&1 || fail "jq is required"
# go is only needed when reading the live schema (not when a test supplies one).
[ -n "${GEN_README_SCHEMA_FILE:-}" ] || command -v go > /dev/null 2>&1 || fail "go is required"
[ -f "$README" ] || fail "README not found: ${README}"
# Require EXACTLY ONE of each marker, anchored at line start. Anchoring stops a
# prose mention of the marker text (e.g. in "Keeping the README in sync") from
# counting; requiring exactly one rejects a corrupt README before render, since
# a missing END would truncate trailing content and a duplicate BEGIN would
# inject the table twice.
require_one_marker() {
  local label="$1" pattern="$2" n
  n="$(grep -c "$pattern" "$README")"
  [ "$n" -eq 1 ] || fail "expected exactly 1 ${label} marker in README, found ${n}"
}
require_one_marker "BEGIN" "^<!-- ${BEGIN_MARKER}"
require_one_marker "END" "^<!-- ${END_MARKER}"

# build_table — schema の JSON から Markdown 表を組み立てて stdout に出す。
# パイプ文字はセルを壊すのでエスケープする（実データには通常含まれないが保険）。
build_table() {
  local json err
  err="${TMP_DIR}/schema_err"
  if [ -n "${GEN_README_SCHEMA_FILE:-}" ]; then
    json="$(cat "$GEN_README_SCHEMA_FILE" 2> "$err")" || {
      cat "$err" >&2
      fail "failed to read schema file: ${GEN_README_SCHEMA_FILE}"
    }
  else
    json="$(cd "$REPO_ROOT" && go run ./cmd/zotero-cli schema --output json 2> "$err")" || {
      cat "$err" >&2
      fail "failed to run 'zotero-cli schema'"
    }
  fi
  printf '%s\n' "$json" | jq -r '
    "| Command | Description | Arguments |",
    "|---------|-------------|-----------|",
    (sort_by(.name)[] |
      "| `\(.name)` | \(.description | gsub("\\|"; "\\|")) | \((.args // "—") | gsub("\\|"; "\\|")) |")
  ' || fail "failed to build table from schema JSON"
}

# render — README 全文を生成し、マーカー間を生成表で差し替えて stdout に出す。
# マーカーは index($0,m)==1（行頭の literal 一致）で判定する。正規表現ではない
# ので、マーカー文字列に括弧等のメタ文字が入っても安全。
render() {
  local table_file="$1"
  awk -v tf="$table_file" -v b="<!-- $BEGIN_MARKER" -v e="<!-- $END_MARKER" '
    index($0, b) == 1 { print; while ((getline line < tf) > 0) print line; skip = 1; next }
    index($0, e) == 1 { skip = 0; print; next }
    skip != 1 { print }
  ' "$README"
}

main() {
  local check=0
  [ "${1:-}" = "--check" ] && check=1

  local tmp_table="${TMP_DIR}/table" tmp_out="${TMP_DIR}/out"

  # 表の前後を空行で囲む（見出し直後でも Markdown 表が崩れないように）。
  {
    printf '\n'
    build_table
    printf '\n'
  } > "$tmp_table"

  render "$tmp_table" > "$tmp_out"

  # Safety net: the rendered output must still contain the END marker. If render
  # ever dropped trailing content, this aborts before we overwrite the README.
  grep -q "^<!-- ${END_MARKER}" "$tmp_out" \
    || fail "internal: rendered output lost the END marker; aborting to avoid data loss"

  if [ "$check" -eq 1 ]; then
    if ! diff -u "$README" "$tmp_out" > /dev/null; then
      {
        echo "[gen-readme] README.md is out of date with 'zotero-cli schema':"
        diff -u "$README" "$tmp_out" || true
        echo "[gen-readme] Run: scripts/gen-readme.sh   (then commit README.md)"
      } >&2
      exit 1
    fi
    echo "[gen-readme] README.md is up to date."
    exit 0
  fi

  if diff -q "$README" "$tmp_out" > /dev/null; then
    echo "[gen-readme] README.md already up to date."
    exit 0
  fi
  cp "$tmp_out" "$README"
  echo "[gen-readme] README.md updated from schema."
}

main "$@"
