# zotero-cli

Zotero Web API CLI client. Go + Cobra.

## Build & Test

```bash
go build ./cmd/zotero-cli/
```

## AI Agent Usage

### Structured JSON Output

All commands support `--output json` for structured responses:

```bash
zotero-cli search "attention" --output json
zotero-cli get ABCD1234 --output json
zotero-cli collections --output json
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

## Project Structure

- `cmd/zotero-cli/` — CLI entry point (Cobra commands)
- `zotero/` — API client library (types, HTTP client)
- `.claude/commands/` — Claude Code slash commands for paper analysis
