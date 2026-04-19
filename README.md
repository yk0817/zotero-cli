# zotero-cli

A command-line client and Go library for the [Zotero Web API](https://www.zotero.org/support/dev/web_api/v3/start).

## Installation

```bash
go install zotero-cli/cmd/zotero-cli@latest
```

Or build from source:

```bash
git clone https://github.com/yk0817/zotero-cli.git
cd zotero-cli
go build -o zotero-cli ./cmd/zotero-cli/
```

## Setup

1. Get your API key from [Zotero Settings](https://www.zotero.org/settings/keys)
2. Run the config command:

```bash
zotero-cli config
# Enter your User ID and API Key when prompted
```

Credentials are stored in `~/.config/zotero-cli/config.json` with `0600` permissions.

## CLI Usage

### Global Options

```bash
--output json    # Structured JSON output (default: text)
```

All commands support `--output json`. JSON responses use a standard envelope:

```json
{"ok": true, "data": <result>}
{"ok": false, "error": {"code": "CONFIG_NOT_FOUND", "message": "...", "suggestion": "..."}}
```

### Search & Browse

```bash
# Search items by keyword
zotero-cli search "knowledge graph"
zotero-cli search "ESG" --tag "review"
zotero-cli search "ontology" --title    # title-only filter

# List recent items
zotero-cli list
zotero-cli list --collection ZVUDP75D --limit 10

# Full-text search
zotero-cli fullsearch "transformer architecture"
```

### Item Details

```bash
# Show item metadata
zotero-cli get FQVL7ZHM

# Show full text
zotero-cli fulltext FQVL7ZHM
zotero-cli fulltext FQVL7ZHM --max-chars 5000

# Show all info (metadata, full text, notes, attachments)
zotero-cli context FQVL7ZHM
zotero-cli context FQVL7ZHM --with-notes
zotero-cli context FQVL7ZHM --json
```

### Export

```bash
# Export as BibTeX
zotero-cli bibtex "knowledge graph"
zotero-cli bibtex --collection ZVUDP75D
zotero-cli bibtex --all

# Batch export for literature review
zotero-cli export --tag "review" --format json
zotero-cli export --collection ZVUDP75D --format full
zotero-cli export --keys "FQVL7ZHM,99NU4NKK"
```

### Notes

```bash
# Add a note to an item
zotero-cli add-note FQVL7ZHM --body "This paper is relevant to my research."
zotero-cli add-note FQVL7ZHM --body-file notes.txt
cat notes.md | zotero-cli add-note FQVL7ZHM --body-file -

# Preview without making API call
zotero-cli add-note FQVL7ZHM --body "test" --dry-run
```

### Collections

```bash
zotero-cli collections
```

### AI Agent Support

```bash
# Discover all commands and flags as JSON
zotero-cli schema

# Structured JSON output for any command
zotero-cli search "attention" --output json
zotero-cli get FQVL7ZHM --output json

# Dry-run for write operations
zotero-cli add-note FQVL7ZHM --body "test" --dry-run --output json
```

See [CLAUDE.md](CLAUDE.md) for error codes and detailed agent integration guide.

## Library Usage

The `zotero` package can be used as a standalone Go library:

```go
package main

import (
	"fmt"
	"github.com/yk0817/zotero-cli/zotero"
)

func main() {
	client := zotero.NewClient("your-api-key", "your-user-id")

	// Search items
	items, _ := client.SearchItems("knowledge graph", "")
	for _, item := range items {
		fmt.Printf("%s: %s\n", item.Key, item.Data.Title)
	}

	// Get full context (metadata + fulltext + notes + attachments)
	ctx, _ := client.GetContext("FQVL7ZHM")
	fmt.Println(ctx.Item.Data.Title)
	fmt.Println(ctx.FullText.Content)

	// Create a note
	key, _ := client.CreateNote("FQVL7ZHM", "My notes here", []string{"review"})
	fmt.Println("Created note:", key)
}
```

## Claude Code Integration

This project includes [Claude Code](https://claude.com/claude-code) custom skills for AI-powered paper analysis. No AI dependencies are added to the CLI itself — all analysis runs entirely within Claude Code.

All skills are available as slash commands when running Claude Code in the `zotero-cli` directory. Each skill has both Japanese (`/skill`) and English (`/skill-en`) versions.

### `/summarize` — Paper Summarization

```bash
/summarize FQVL7ZHM                                # Ochiai-style 6-point summary (default)
/summarize FQVL7ZHM --save                          # Summarize and save as a Zotero note
/summarize FQVL7ZHM --format brief                  # Brief summary
/summarize FQVL7ZHM --format abstract               # Structured abstract
/summarize FQVL7ZHM --format custom "Summarize in 3 lines"  # Custom prompt
/summarize knowledge graph embedding                # Search → select → summarize
```

### `/critique` (`/critique-en`) — Critical Paper Analysis

Systematically analyze strengths, weaknesses, methodological validity, and research gaps.

```bash
/critique FQVL7ZHM                                    # Critical analysis
/critique "knowledge graph corporate" --save           # Search → analyze → save
/critique FQVL7ZHM --perspective "データセットの一般化可能性"  # Focus on a specific aspect
```

### `/compare` (`/compare-en`) — Paper Comparison

Compare methods, results, and contributions across multiple papers in a structured table.

```bash
/compare FQVL7ZHM 99NU4NKK                     # Compare 2 papers
/compare FQVL7ZHM 99NU4NKK EUL3QYDP --focus method  # Focus on methods
/compare "knowledge graph ESG" --limit 3 --save      # Search → top 3 → compare → save
```

### `/survey-table` (`/survey-table-en`) — Survey Table Generation

Auto-generate a Markdown literature review table from collections, tags, or specified papers.

```bash
/survey-table --tag "GNN"                                    # Filter by tag
/survey-table --collection ZVUDP75D --columns "手法,データ,精度"  # Custom columns
/survey-table --keys "FQVL7ZHM,99NU4NKK,EUL3QYDP" --save       # Specified papers → table → save
```

### `/related-work` (`/related-work-en`) — Related Work Section Generation

Auto-generate a related work section draft with `\cite{}` references.

```bash
/related-work --collection ZVUDP75D --lang ja                        # Japanese output
/related-work --tag "corporate-governance" --theme "KGを用いたガバナンス分析"  # With theme
/related-work --keys "FQVL7ZHM,99NU4NKK" --lang en --save               # English → save
```

### Common Options

| Option | Description | Available in |
|--------|-------------|--------------|
| `--save` | Save output as a Zotero note | All skills |
| `--focus <aspect>` | Focus comparison on a specific aspect | `/compare` |
| `--perspective <text>` | Focus analysis on a specific viewpoint | `/critique` |
| `--columns "<cols>"` | Custom table columns | `/survey-table` |
| `--tag <tag>` | Filter by Zotero tag | `/survey-table`, `/related-work` |
| `--collection <key>` | Filter by collection | `/survey-table`, `/related-work` |
| `--keys "<k1>,<k2>"` | Specify papers by itemKey | `/survey-table`, `/related-work` |
| `--theme <text>` | Specify theme context | `/related-work` |
| `--lang <ja\|en>` | Output language | `/related-work` |

All skills pipe CLI commands (`search`, `context`, `export`, `bibtex`, `add-note`) under the hood.

## Project Structure

```
zotero-cli/
├── zotero/              # Go library package
│   ├── client.go        # API client and methods
│   └── types.go         # Data types (Item, Collection, etc.)
├── cmd/zotero-cli/
│   ├── main.go          # CLI application
│   ├── output.go        # JSON output helpers and error types
│   ├── schema.go        # Schema command for agent discovery
│   └── validate.go      # Input validation
├── .claude/commands/
│   ├── summarize.md     # Paper summarization (Japanese)
│   ├── critique.md      # Critical analysis (Japanese)
│   ├── critique-en.md   # Critical analysis (English)
│   ├── compare.md       # Paper comparison (Japanese)
│   ├── compare-en.md    # Paper comparison (English)
│   ├── survey-table.md  # Survey table generation (Japanese)
│   ├── survey-table-en.md # Survey table generation (English)
│   ├── related-work.md  # Related work section (Japanese)
│   └── related-work-en.md # Related work section (English)
└── go.mod
```

## License

MIT
