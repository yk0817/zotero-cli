---
description: Regenerate the README CLI command table from `zotero-cli schema`. README のコマンド表を schema から再生成する。
user-invocable: true
---

# Sync README command table

Regenerate the auto-generated command reference in `README.md` from the
canonical `zotero-cli schema` output, so the docs match the code.

This is the on-demand counterpart of the `git push` hook
(`scripts/hook-readme-sync.sh`), which runs the same generator automatically.
両者は同じ生成スクリプト `scripts/gen-readme.sh` を呼ぶ。

## Steps

1. Run the generator from the repo root:

   ```bash
   bash scripts/gen-readme.sh
   ```

   - It rewrites only the block between the
     `<!-- BEGIN AUTO-GENERATED COMMANDS -->` / `<!-- END ... -->` markers.
   - It is idempotent: if the table already matches the schema it prints
     "already up to date" and changes nothing.

2. If `$ARGUMENTS` contains `--check`, run in verification mode instead (no
   write; exits non-zero and prints a diff when `README.md` is stale):

   ```bash
   bash scripts/gen-readme.sh --check
   ```

3. Report what changed. If `README.md` was modified, show the diff and remind
   the user to commit it (`git add README.md`).

## Notes

- Do NOT hand-edit the table between the markers; it is overwritten on the next
  run. Edit command names/descriptions/args at their source (the cobra command
  definitions in `cmd/zotero-cli/`), then regenerate.
- Requires `go` and `jq` on PATH.
