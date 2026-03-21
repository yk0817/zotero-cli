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
```

### Collections

```bash
zotero-cli collections
```

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

## Project Structure

```
zotero-cli/
├── zotero/              # Go library package
│   ├── client.go        # API client and methods
│   └── types.go         # Data types (Item, Collection, etc.)
├── cmd/zotero-cli/
│   └── main.go          # CLI application
└── go.mod
```

## License

MIT
