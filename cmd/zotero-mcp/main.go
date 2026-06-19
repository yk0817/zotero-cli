// Command zotero-mcp is an MCP server exposing the Zotero Web API.
// Read tools cover search, annotations, and item context. The write tools are
// zotero_add_note (creates notes tagged "ai-generated") and zotero_delete_note
// (removes a single such note). It shares the zotero package with the CLI and
// reads the same config file (~/.config/zotero-cli/config.json).
//
// Delete guardrails. Because this server runs unattended, zotero_delete_note is
// fenced so the model can only ever undo its own writes:
//   - notes only (refuses papers, attachments, annotations),
//   - ai-generated tag required (refuses human-written notes),
//   - one key per call, no bulk/by-query deletion,
//   - confirm=true required (a bare call only previews).
//
// The notes-only and tag checks live in zotero.DeleteNote; the preview gate is
// enforced here in the handler.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yk0817/zotero-cli/zotero"
)

const serverVersion = "0.1.0"

// config mirrors the CLI config file format.
type config struct {
	APIKey string `json:"api_key"`
	UserID string `json:"user_id"`
}

func loadClient() (*zotero.Client, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve home directory: %w", err)
	}
	path := filepath.Join(home, ".config", "zotero-cli", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config not found at %s (run 'zotero-cli config' to set up): %w", path, err)
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config %s: %w", path, err)
	}
	if cfg.APIKey == "" || cfg.UserID == "" {
		return nil, fmt.Errorf("API key or user ID not set in %s", path)
	}
	return zotero.NewClient(cfg.APIKey, cfg.UserID), nil
}

// textResult wraps a string as an MCP tool result.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

// formatItem renders item metadata as readable text.
func formatItem(item *zotero.Item) string {
	var b strings.Builder
	d := item.Data
	fmt.Fprintf(&b, "Key:      %s\n", item.Key)
	fmt.Fprintf(&b, "Type:     %s\n", d.ItemType)
	fmt.Fprintf(&b, "Title:    %s\n", d.Title)
	fmt.Fprintf(&b, "Authors:  %s\n", zotero.FormatAuthors(d.Creators))
	fmt.Fprintf(&b, "Date:     %s\n", d.Date)
	if d.PublicationTitle != "" {
		fmt.Fprintf(&b, "Journal:  %s\n", d.PublicationTitle)
	}
	if d.DOI != "" {
		fmt.Fprintf(&b, "DOI:      %s\n", d.DOI)
	}
	fmt.Fprintf(&b, "Tags:     %s\n", zotero.FormatTags(d.Tags))
	if d.AbstractNote != "" {
		fmt.Fprintf(&b, "\nAbstract:\n%s\n", d.AbstractNote)
	}
	return b.String()
}

const syncHint = "(no annotations — if you expect annotations here, check that Zotero sync is enabled; annotations are only available via the Web API after syncing)"

type searchInput struct {
	Query string `json:"query" jsonschema:"search keyword"`
	Tag   string `json:"tag,omitempty" jsonschema:"optional tag filter"`
}

type itemKeyInput struct {
	ItemKey string `json:"item_key" jsonschema:"8-character alphanumeric Zotero item key"`
}

type annotationsInput struct {
	ItemKey string `json:"item_key" jsonschema:"8-character alphanumeric Zotero item key"`
	Color   string `json:"color,omitempty" jsonschema:"optional color filter, e.g. #ff0000"`
	Type    string `json:"type,omitempty" jsonschema:"optional type filter: highlight, underline, note, ink, image"`
}

type addNoteInput struct {
	ItemKey string   `json:"item_key" jsonschema:"8-character alphanumeric Zotero item key of the parent item"`
	Body    string   `json:"body" jsonschema:"note content; plain text (paragraphs split on newlines) or HTML"`
	Tags    []string `json:"tags,omitempty" jsonschema:"optional extra tags; the 'ai-generated' tag is always added"`
}

type deleteNoteInput struct {
	ItemKey string `json:"item_key" jsonschema:"8-character alphanumeric key of the note to delete; must be a note, not a paper or attachment"`
	Confirm bool   `json:"confirm" jsonschema:"must be true to actually delete; when false (default) the note is only previewed, not deleted"`
}

func searchHandler(client *zotero.Client) mcp.ToolHandlerFor[searchInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input searchInput) (*mcp.CallToolResult, any, error) {
		items, err := client.SearchItems(input.Query, input.Tag)
		if err != nil {
			return nil, nil, fmt.Errorf("search failed: %w", err)
		}
		var b strings.Builder
		found := 0
		for _, item := range items {
			if item.Data.ItemType == "attachment" || item.Data.ItemType == "note" {
				continue
			}
			found++
			fmt.Fprintf(&b, "[%s] %s (%s, %s)\n",
				item.Key,
				item.Data.Title,
				zotero.FormatAuthors(item.Data.Creators),
				item.Data.Date,
			)
		}
		if found == 0 {
			return textResult("No results found"), nil, nil
		}
		return textResult(b.String()), nil, nil
	}
}

func annotationsHandler(client *zotero.Client) mcp.ToolHandlerFor[annotationsInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input annotationsInput) (*mcp.CallToolResult, any, error) {
		if err := zotero.ValidateItemKey(input.ItemKey); err != nil {
			return nil, nil, err
		}
		anns, err := client.GetAnnotations(input.ItemKey)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get annotations: %w", err)
		}
		if len(anns) == 0 {
			return textResult(syncHint), nil, nil
		}
		filtered := zotero.FilterAnnotations(anns, input.Color, input.Type)
		if len(filtered) == 0 {
			return textResult("No annotations match the given filter"), nil, nil
		}
		var b strings.Builder
		for _, a := range filtered {
			b.WriteString(zotero.FormatAnnotation(a))
			b.WriteString("\n")
		}
		return textResult(b.String()), nil, nil
	}
}

func contextHandler(client *zotero.Client) mcp.ToolHandlerFor[itemKeyInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input itemKeyInput) (*mcp.CallToolResult, any, error) {
		if err := zotero.ValidateItemKey(input.ItemKey); err != nil {
			return nil, nil, err
		}
		bundle, err := client.GetContext(input.ItemKey, true)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get context: %w", err)
		}
		if bundle.Item == nil {
			return nil, nil, fmt.Errorf("item %s not found", input.ItemKey)
		}

		var b strings.Builder
		fmt.Fprintf(&b, "=== ITEM: %s ===\n", bundle.Item.Key)
		b.WriteString(formatItem(bundle.Item))

		if bundle.FullText != nil && bundle.FullText.Content != "" {
			pageInfo := ""
			if bundle.FullText.TotalPages > 0 {
				pageInfo = fmt.Sprintf(" (%d pages)", bundle.FullText.TotalPages)
			}
			fmt.Fprintf(&b, "\n=== FULL TEXT%s ===\n%s\n", pageInfo, bundle.FullText.Content)
		}

		fmt.Fprintf(&b, "\n=== ANNOTATIONS (%d) ===\n", len(bundle.Annotations))
		if len(bundle.Annotations) == 0 {
			b.WriteString(syncHint + "\n")
		}
		for _, a := range bundle.Annotations {
			b.WriteString(zotero.FormatAnnotation(a))
			b.WriteString("\n")
		}

		if len(bundle.Notes) > 0 {
			fmt.Fprintf(&b, "\n=== NOTES (%d) ===\n", len(bundle.Notes))
			for i, note := range bundle.Notes {
				fmt.Fprintf(&b, "--- Note %d (%s) ---\n%s\n\n", i+1, note.Key, note.Data.Note)
			}
		}

		if len(bundle.Attachments) > 0 {
			b.WriteString("\n=== ATTACHMENTS ===\n")
			for _, att := range bundle.Attachments {
				name := att.Data.Filename
				if name == "" {
					name = att.Data.Title
				}
				fmt.Fprintf(&b, "- %s (key: %s)\n", name, att.Key)
			}
		}

		return textResult(b.String()), nil, nil
	}
}

func addNoteHandler(client *zotero.Client) mcp.ToolHandlerFor[addNoteInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input addNoteInput) (*mcp.CallToolResult, any, error) {
		if err := zotero.ValidateItemKey(input.ItemKey); err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(input.Body) == "" {
			return nil, nil, fmt.Errorf("note body is empty")
		}

		tags := zotero.NoteTags(input.Tags)
		key, err := client.CreateNote(input.ItemKey, input.Body, tags)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create note: %w", err)
		}
		return textResult(fmt.Sprintf("Note created: %s (parent: %s, tags: %s)",
			key, input.ItemKey, strings.Join(tags, ", "))), nil, nil
	}
}

// deleteNoteHandler deletes one AI-generated note. The two-step confirm is the
// guardrail the LLM sees: a first call previews (so the model and the human in
// the loop can see exactly which note is targeted), and only an explicit
// confirm=true second call performs the deletion.
//
// The note is fetched once: that single read drives the preview, the
// courtesy pre-checks below, AND the deletion (via DeleteNoteItem), so the
// item shown and the item deleted cannot diverge across a second round-trip.
// The pre-checks here exist only to gate the preview and return clear errors;
// the authoritative structural guards (notes-only, ai-generated-tag-only,
// lost-update-safe) live in zotero.DeleteNoteItem and run again on the same
// item, so they hold even if this handler is wrong.
func deleteNoteHandler(client *zotero.Client) mcp.ToolHandlerFor[deleteNoteInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input deleteNoteInput) (*mcp.CallToolResult, any, error) {
		if err := zotero.ValidateItemKey(input.ItemKey); err != nil {
			return nil, nil, err
		}

		item, err := client.GetItem(input.ItemKey)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch %s: %w", input.ItemKey, err)
		}
		// courtesy pre-checks (also enforced inside DeleteNoteItem)
		if item.Data.ItemType != "note" {
			return nil, nil, fmt.Errorf("refusing to delete %s: item type is %q, not \"note\"", input.ItemKey, item.Data.ItemType)
		}
		if !item.HasTag(zotero.AIGeneratedTag) {
			return nil, nil, fmt.Errorf("refusing to delete %s: note lacks the %q tag (this tool only deletes AI-generated notes)", input.ItemKey, zotero.AIGeneratedTag)
		}

		if !input.Confirm {
			return textResult(fmt.Sprintf("Would delete note %s (parent: %s, tags: %s). Call again with confirm=true to delete.",
				item.Key, item.Data.ParentItem, zotero.FormatTags(item.Data.Tags))), nil, nil
		}

		if err := client.DeleteNoteItem(item, true); err != nil {
			return nil, nil, fmt.Errorf("failed to delete note: %w", err)
		}
		return textResult(fmt.Sprintf("Note deleted: %s", input.ItemKey)), nil, nil
	}
}

func main() {
	client, err := loadClient()
	if err != nil {
		log.Fatalf("zotero-mcp: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "zotero",
		Version: serverVersion,
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "zotero_search",
		Description: "Search Zotero library items by keyword. Returns item keys with titles, authors, and dates.",
	}, searchHandler(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "zotero_get_annotations",
		Description: "Get PDF annotations (highlights, underlines, comments) of a Zotero item in reading order. Optionally filter by color or annotation type.",
	}, annotationsHandler(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "zotero_get_context",
		Description: "Get all information about a Zotero item: metadata, abstract, full text, annotations, notes, and attachments.",
	}, contextHandler(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "zotero_add_note",
		Description: "Add a note (memo, summary, comment) to a Zotero item. The note is tagged 'ai-generated'. Body accepts plain text or HTML.",
	}, addNoteHandler(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "zotero_delete_note",
		Description: "Delete a single AI-generated note by its key. Guardrailed: only deletes items of type 'note' that carry the 'ai-generated' tag (never papers, attachments, or human-written notes), one key per call. Call with confirm=false first to preview, then confirm=true to delete.",
	}, deleteNoteHandler(client))

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("zotero-mcp: server error: %v", err)
	}
}
