# zotero-cli

Zotero Web API CLI client. Go + Cobra.

## Build & Test

```bash
go build ./...
go test -race ./...
```

## Test Harness

- **テストを書く前に `knowledge-base/qa-perspectives.md`（観点チェックリスト）と `knowledge-base/test-style-guide.md`（形式）を Read する**。P1 観点は必ずテストに紐付ける
- 全 Test 関数に `// Contract:` コメント必須 — hook（`scripts/hook-validate-test-contract.sh`）が `*_test.go` 書き込み直後に検証して違反を差し戻し、CI が全ファイルを再検証する
- テスト設計の一括実行は `/test-design <対象>`（観点マトリクス→生成→検証→カバレッジ表）
- バグ修正時は回帰テスト追加後、qa-perspectives.md の「過去バグ由来の観点」表に追記する

## AI Agent Usage

### Structured JSON Output

All commands support `--output json` for structured responses:

```bash
zotero-cli search "attention" --output json
zotero-cli get ABCD1234 --output json
zotero-cli collections --output json
zotero-cli annotations ABCD1234 --output json
```

JSON responses use a standard envelope:

```json
{"ok": true, "data": ...}
{"ok": false, "error": {"code": "...", "message": "...", "suggestion": "..."}}
```

Empty results return `{"ok": true, "data": []}` (not an error).

### Command Discovery

```bash
zotero-cli schema
```

Returns JSON array of all commands with flags, types, and defaults.

### Dry Run

```bash
zotero-cli add-note ABCD1234 --body "test" --dry-run --output json
```

Shows the payload without making an API call.

### Error Codes

| Code | Meaning |
|------|---------|
| `CONFIG_NOT_FOUND` | No config file; run `zotero-cli config` |
| `CONFIG_INVALID` | Config file malformed or missing fields |
| `VALIDATION_ERROR` | Invalid input (bad item key, control chars) |
| `INVALID_ARGUMENT` | Missing required flag or argument |
| `API_ERROR` | Zotero API returned an error |
| `NOT_FOUND` | Requested resource not found |
| `IO_ERROR` | File read/stdin error |

### Annotations

PDF annotations (highlights/comments) live under each attachment's children and are only visible via the Web API after Zotero sync. `zotero-cli annotations <key>` returns them in reading order (`annotationSortIndex`); when the result is empty the CLI prints a sync hint to stderr — do not interpret an empty result as "no marks exist".

## Project Structure

- `cmd/zotero-cli/` — CLI entry point (Cobra commands)
- `cmd/zotero-mcp/` — MCP server (stdio, official Go SDK); read tools + `zotero_add_note` write tool
- `zotero/` — API client library (types, HTTP client)
- `.claude/commands/` — Claude Code slash commands for paper analysis
