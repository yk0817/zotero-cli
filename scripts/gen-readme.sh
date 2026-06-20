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
# Anchor at line start so a mention of the marker text in prose (e.g. in the
# "Keeping the README in sync" section) is not mistaken for the real marker.
grep -q "^<!-- ${BEGIN_MARKER}" "$README" || fail "marker not found in README: ${BEGIN_MARKER}"
grep -q "^<!-- ${END_MARKER}" "$README" || fail "marker not found in README: ${END_MARKER}"

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
render() {
  local table_file="$1"
  awk -v tf="$table_file" -v b="^<!-- $BEGIN_MARKER" -v e="^<!-- $END_MARKER" '
    $0 ~ b { print; while ((getline line < tf) > 0) print line; skip = 1; next }
    $0 ~ e { skip = 0; print; next }
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
