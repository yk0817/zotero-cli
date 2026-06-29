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

### All Commands

The table below is generated from `zotero-cli schema` and kept in sync by
`scripts/gen-readme.sh` (run automatically before `git push`; see
[Keeping the README in sync](#keeping-the-readme-in-sync)). 日本語: 下の表は
`zotero-cli schema` から自動生成されます。手で編集しても次回の生成で上書きされます。

<!-- BEGIN AUTO-GENERATED COMMANDS (scripts/gen-readme.sh — do not edit by hand) -->

| Command | Description | Arguments |
|---------|-------------|-----------|
| `add-note` | Add a note to an item | parentItemKey: 8-character alphanumeric item key of parent item (required) |
| `annotations` | Show annotations (highlights/comments) of an item | itemKey: 8-character alphanumeric item key (required) |
| `bibtex` | Export as BibTeX | query: search keyword (optional, requires --all or --collection if omitted) |
| `collections` | List collections | none |
| `config` | Set up API key and user ID | none (interactive prompt) |
| `context` | Show all information about an item | itemKey: 8-character alphanumeric item key (required) |
| `delete-note` | Delete a single note (guardrailed: notes only, one key, approval required) | itemKey: 8-character alphanumeric key of the note to delete (required) |
| `export` | Batch export for literature review | none |
| `fullsearch` | Full-text search | query: search keyword (required) |
| `fulltext` | Show full text of an item | itemKey: 8-character alphanumeric item key (required) |
| `get` | Show item details | itemKey: 8-character alphanumeric item key (required) |
| `list` | List items | none |
| `search` | Search items by keyword | query: search keyword (required) |
| `tag` | Add or remove tags on an item | itemKey: 8-character alphanumeric item key (required) |
| `tags` | List all tags in the library (closed vocabulary source) | none |
| `upload` | Upload a file as an attachment | filePath: path to the file to upload (required) |

<!-- END AUTO-GENERATED COMMANDS -->

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

# Show all info (metadata, full text, annotations, notes, attachments)
zotero-cli context FQVL7ZHM
zotero-cli context FQVL7ZHM --with-notes
zotero-cli context FQVL7ZHM --with-annotations
zotero-cli context FQVL7ZHM --json
```

### Annotations

Retrieve PDF annotations (highlights, underlines, comments) in reading order. Annotations are fetched from the children of each attachment, so they are only available after Zotero sync.

```bash
# All annotations of an item
zotero-cli annotations FQVL7ZHM

# Filter by color (useful for color-coded reading)
zotero-cli annotations FQVL7ZHM --color "#ff0000"

# Filter by type: highlight, underline, note, ink, image
zotero-cli annotations FQVL7ZHM --type highlight

# Structured output
zotero-cli annotations FQVL7ZHM --output json
```

Text output format:

```
[highlight p.9 #aaaaaa] "selected passage from the PDF"
  ↳ comment: your comment on the highlight
[note p.12] a note placed on the page
[ink p.15 — no text]
```

`ink` / `image` annotations (e.g., handwritten notes from GoodNotes) carry no text; only their page position is shown.

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

# Delete a single note (guardrailed, with approval flow)
zotero-cli delete-note NOTE5678            # shows the note, then asks: type 'yes' to confirm
zotero-cli delete-note NOTE5678 --yes      # pre-approves (skips the prompt; for scripts/agents)
```

**Delete guardrails.** `delete-note` is fenced so it can only remove the cheapest-to-recreate item, one at a time:

- **Notes only** — refuses any item whose type is not `note` (papers, PDFs, annotations are rejected up front), so a mistyped key cannot destroy a library item.
- **Approval required** — the command always prints the note first, then requires explicit approval: interactively you type `yes`; non-interactively you pass `--yes`. Deletion never happens from a bare command. (In `--output json` mode `--yes` is mandatory, since a prompt cannot be shown.)
- **One key, no bulk** — there is intentionally no delete-by-tag/by-query/all; a single approval can only remove a single note. Mass deletion would have to be scripted by looping over keys you have already listed and approved.
- **Lost-update safe** — uses the item's version (`If-Unmodified-Since-Version`), so a note edited since you read it is rejected (HTTP 412) instead of clobbered.

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

	// Get full context (metadata + fulltext + annotations + notes + attachments)
	ctx, _ := client.GetContext("FQVL7ZHM")
	fmt.Println(ctx.Item.Data.Title)
	fmt.Println(ctx.FullText.Content)

	// Get annotations in reading order
	anns, _ := client.GetAnnotations("FQVL7ZHM")
	for _, a := range anns {
		fmt.Println(zotero.FormatAnnotation(a))
	}

	// Create a note
	key, _ := client.CreateNote("FQVL7ZHM", "My notes here", []string{"review"})
	fmt.Println("Created note:", key)
}
```

## MCP Server

`cmd/zotero-mcp` is an [MCP](https://modelcontextprotocol.io/) server that exposes the same `zotero` package over stdio. It reuses the CLI config (`~/.config/zotero-cli/config.json`), so run `zotero-cli config` first.

### Build

```bash
go build -o zotero-mcp ./cmd/zotero-mcp/
```

### Setup (Claude Code)

```bash
claude mcp add zotero -- ~/zotero-cli/zotero-mcp
```

Or add to `.mcp.json` / `~/.claude/settings.json` manually:

```json
{
  "mcpServers": {
    "zotero": {
      "command": "/Users/you/zotero-cli/zotero-mcp"
    }
  }
}
```

### Setup (Cursor and other MCP clients)

Add the same `command` entry to the client's MCP config (e.g., `~/.cursor/mcp.json`).

### Tools

| Tool | Arguments | Description |
|------|-----------|-------------|
| `zotero_search` | `query`, `tag?` | Search library items; returns keys, titles, authors, dates |
| `zotero_get_annotations` | `item_key`, `color?`, `type?` | PDF annotations in reading order, optionally filtered |
| `zotero_get_context` | `item_key` | Metadata + abstract + full text + annotations + notes + attachments |
| `zotero_add_note` | `item_key`, `body`, `tags?` | Create a child note on an item; always tagged `ai-generated` |
| `zotero_delete_note` | `item_key`, `confirm` | Delete a single `ai-generated` note; `confirm=false` previews, `confirm=true` deletes |

`zotero_add_note` and `zotero_delete_note` are the only write tools; everything else is read-only. If an item has no synced annotations, the read tools return a hint about Zotero sync instead of an empty response.

Because the MCP server runs unattended, `zotero_delete_note` carries an extra guardrail beyond the CLI: it deletes **only notes that carry the `ai-generated` tag**, so the model can undo notes it created but can never remove a human-written note, a paper, or an attachment. It also takes one key per call (no bulk deletion) and requires `confirm=true` after a preview.

## Claude Code Integration

This project includes [Claude Code](https://claude.com/claude-code) custom skills for AI-powered paper analysis. No AI dependencies are added to the CLI itself — all analysis runs entirely within Claude Code.

All skills are available as slash commands when running Claude Code in the `zotero-cli` directory. Each skill has both Japanese (`/skill`) and English (`/skill-en`) versions.

### `/summarize` (`/summarize-en`) — Paper Summarization

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

### `/discuss` (`/discuss-en`) — Close-Reading Discussion

Discuss a paper interactively around your own Zotero highlights and comments (fetched via the `annotations` command). Distinct from `/summarize`: it quotes each of your marked passages, explains why it matters, responds to your comments, and connects them to the full text.

```bash
/discuss FQVL7ZHM                        # Discuss around all annotations
/discuss "attention is all you need"     # Search → select → discuss
/discuss FQVL7ZHM --color "#ff0000"      # Focus on red highlights only
```

### `/tag` (`/tag-en`) — Closed-Vocabulary Tagging

Tag a paper using **only tags that already exist** in the library. The skill reads the paper, fetches the existing tag list (`zotero-cli tags`), and picks solely from that set — it never invents a new tag. Concepts with no matching existing tag are reported as candidates only, not applied. This keeps the library's tag vocabulary from sprawling.

```bash
/tag FQVL7ZHM                 # Suggest & apply existing tags (max 5 by default)
/tag FQVL7ZHM --dry-run        # Preview which existing tags would be applied
/tag FQVL7ZHM --max 3          # Cap the number of tags applied
/tag "graph attention network"  # Search → select → tag
```

### `/tag-new` (`/tag-new-en`) — Create a New Tag

The **only** entry point that intentionally extends the vocabulary. It checks for exact and near-duplicate existing tags first (warns and suggests `/tag` if one already covers it), then applies the new tag.

```bash
/tag-new FQVL7ZHM --tag "dynamic-knowledge-graph"   # Create & apply a new tag
/tag-new FQVL7ZHM --tag "DKG" --tag "temporal-gnn"   # Multiple new tags
```

Underlying CLI primitives: `zotero-cli tags` (list existing tags) and `zotero-cli tag <key> --add/--remove [--dry-run]` (edit an item's tags). The CLI itself is an unconstrained primitive; the closed-vocabulary rule lives in the `/tag` skill.

### `/extract-methods` (`/extract-methods-en`) — Reproducibility Method Extraction

Extract reproducibility-focused details (experimental setup, datasets, metrics, hyperparameters, baselines) from a paper's full text into a structured table. Distinct from `/summarize`: it captures only the facts needed to reproduce the work, marking anything unstated as unknown rather than guessing.

```bash
/extract-methods FQVL7ZHM                 # Method table from the paper's full text
/extract-methods FQVL7ZHM --save           # Extract and save as a Zotero note
/extract-methods "graph attention network"  # Search → select → extract
```

### `/gap` (`/gap-en`) — Research Gap Analysis

Identify open research gaps and candidate research questions across a set of papers (collection, tag, or specified item keys). Distinct from `/survey-table` (organizes) and `/related-work` (drafts prose): it asks "what should be investigated next?".

```bash
/gap --tag "GNN"                                   # Gaps across a tag
/gap --collection ZVUDP75D --save                   # Collection → gaps + RQs → save
/gap --keys "FQVL7ZHM,99NU4NKK,EUL3QYDP"             # Specified papers
```

### Common Options

| Option | Description | Available in |
|--------|-------------|--------------|
| `--save` | Save output as a Zotero note | All skills except `/discuss` |
| `--dry-run` | Preview tag changes without writing | `/tag` |
| `--max <n>` | Cap how many tags are applied | `/tag` |
| `--tag <new>` | New tag to create and apply (repeatable) | `/tag-new` |
| `--color <hex>` | Focus on annotations of a specific color | `/discuss` |
| `--focus <aspect>` | Focus comparison on a specific aspect | `/compare` |
| `--perspective <text>` | Focus analysis on a specific viewpoint | `/critique` |
| `--columns "<cols>"` | Custom table columns | `/survey-table` |
| `--tag <tag>` | Filter by Zotero tag | `/survey-table`, `/related-work`, `/gap` |
| `--collection <key>` | Filter by collection | `/survey-table`, `/related-work`, `/gap` |
| `--keys "<k1>,<k2>"` | Specify papers by itemKey | `/survey-table`, `/related-work`, `/gap` |
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
├── cmd/zotero-mcp/
│   └── main.go          # MCP server (stdio): read tools + zotero_add_note
├── .claude/commands/
│   ├── summarize.md     # Paper summarization (Japanese)
│   ├── critique.md      # Critical analysis (Japanese)
│   ├── critique-en.md   # Critical analysis (English)
│   ├── compare.md       # Paper comparison (Japanese)
│   ├── compare-en.md    # Paper comparison (English)
│   ├── survey-table.md  # Survey table generation (Japanese)
│   ├── survey-table-en.md # Survey table generation (English)
│   ├── related-work.md  # Related work section (Japanese)
│   ├── related-work-en.md # Related work section (English)
│   ├── discuss.md       # Close-reading discussion (Japanese)
│   └── discuss-en.md    # Close-reading discussion (English)
└── go.mod
```

## Keeping the README in sync

The [command table](#all-commands) is generated from `zotero-cli schema` — the
single source of truth — so it never drifts from the code. Do not hand-edit the
block between the `<!-- BEGIN AUTO-GENERATED COMMANDS -->` markers; change the
command definitions in `cmd/zotero-cli/` and regenerate instead.

```bash
bash scripts/gen-readme.sh           # rewrite the table in place
bash scripts/gen-readme.sh --check   # CI/pre-push: exit 1 + diff if stale (no write)
```

Three layers keep it honest:

- **Pre-push hook** (`scripts/hook-readme-sync.sh`, wired in `.claude/settings.json`)
  — before a `git push`, regenerates the table; if it was stale it updates
  `README.md` and blocks the push so you review and commit the change.
- **CI** runs `gen-readme.sh --check`, so a stale README fails the build even if
  the push happened without the hook.
- **`/sync-readme`** — a Claude Code slash command for an on-demand regenerate.

日本語: コマンド表は `zotero-cli schema` から自動生成される。マーカー間は手で
編集せず、`cmd/zotero-cli/` の定義を直して再生成する。push 前フック・CI・手動
コマンドの3層でズレを防ぐ。

## License

MIT
