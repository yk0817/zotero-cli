package main

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/yk0817/zotero-cli/zotero"
)

// noteTagPattern strips HTML tags when building a plain-text note preview.
var noteTagPattern = regexp.MustCompile(`<[^>]*>`)

// Config holds the API credentials.
type Config struct {
	APIKey string `json:"api_key"`
	UserID string `json:"user_id"`
}

func configDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "zotero-cli")
}

func configPath() string {
	return filepath.Join(configDir(), "config.json")
}

func loadConfig() (*Config, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return nil, &CLIError{
			Code:       ErrCodeConfigNotFound,
			Message:    "config not found",
			Suggestion: "Run 'zotero-cli config' to set up",
		}
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, &CLIError{
			Code:       ErrCodeConfigInvalid,
			Message:    fmt.Sprintf("failed to read config: %v", err),
			Suggestion: "Check config file format or run 'zotero-cli config' to re-create",
		}
	}
	if cfg.APIKey == "" || cfg.UserID == "" {
		return nil, &CLIError{
			Code:       ErrCodeConfigInvalid,
			Message:    "API key or user ID not set",
			Suggestion: "Run 'zotero-cli config' to set up",
		}
	}
	return &cfg, nil
}

func saveConfig(cfg *Config) error {
	if err := os.MkdirAll(configDir(), 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0600)
}

func newClient() (*zotero.Client, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	return zotero.NewClient(cfg.APIKey, cfg.UserID), nil
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "zotero-cli",
		Short: "Zotero Web API CLI client",
	}
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true

	rootCmd.PersistentFlags().StringVar(&outputFormat, "output", "text", "Output format: text or json")

	// config command
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Set up API key and user ID",
		Annotations: map[string]string{
			"args": "none (interactive prompt)",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			reader := bufio.NewReader(os.Stdin)

			fmt.Print("Zotero User ID: ")
			userID, _ := reader.ReadString('\n')
			userID = strings.TrimSpace(userID)

			fmt.Print("Zotero API Key: ")
			apiKey, _ := reader.ReadString('\n')
			apiKey = strings.TrimSpace(apiKey)

			if userID == "" || apiKey == "" {
				return &CLIError{
					Code:    ErrCodeValidation,
					Message: "user ID and API key are required",
				}
			}

			cfg := &Config{APIKey: apiKey, UserID: userID}
			if err := saveConfig(cfg); err != nil {
				return err
			}
			if isJSON() {
				return printJSON(map[string]string{
					"configPath": configPath(),
					"message":    "Config saved",
				})
			}
			fmt.Printf("Config saved: %s\n", configPath())
			return nil
		},
	}

	// search command
	var searchTag string
	var searchTitle bool
	searchCmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search items by keyword",
		Args:  cobra.MinimumNArgs(1),
		Annotations: map[string]string{
			"args": "query: search keyword (required)",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			query := strings.Join(args, " ")
			if err := sanitizeInput(query); err != nil {
				return err
			}
			items, err := client.SearchItems(query, searchTag)
			if err != nil {
				return err
			}
			if searchTitle {
				lowerQuery := strings.ToLower(query)
				var filtered []zotero.Item
				for _, item := range items {
					if strings.Contains(strings.ToLower(item.Data.Title), lowerQuery) {
						filtered = append(filtered, item)
					}
				}
				items = filtered
			}
			if isJSON() {
				if items == nil {
					items = []zotero.Item{}
				}
				return printJSON(items)
			}
			if len(items) == 0 {
				fmt.Println("No results found")
				return nil
			}
			printItemTable(items)
			return nil
		},
	}
	searchCmd.Flags().StringVar(&searchTag, "tag", "", "Filter by tag")
	searchCmd.Flags().BoolVar(&searchTitle, "title", false, "Filter by title only")

	// list command
	var listCollection string
	var listLimit int
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List items",
		Annotations: map[string]string{
			"args": "none",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			items, err := client.ListItems(listCollection, listLimit)
			if err != nil {
				return err
			}
			if isJSON() {
				if items == nil {
					items = []zotero.Item{}
				}
				return printJSON(items)
			}
			if len(items) == 0 {
				fmt.Println("No items found")
				return nil
			}
			printItemTable(items)
			return nil
		},
	}
	listCmd.Flags().StringVar(&listCollection, "collection", "", "Filter by collection key")
	listCmd.Flags().IntVar(&listLimit, "limit", 25, "Number of items to display")

	// get command
	getCmd := &cobra.Command{
		Use:   "get <itemKey>",
		Short: "Show item details",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"args": "itemKey: 8-character alphanumeric item key (required)",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateItemKey(args[0]); err != nil {
				return err
			}
			client, err := newClient()
			if err != nil {
				return err
			}
			item, err := client.GetItem(args[0])
			if err != nil {
				return err
			}
			if isJSON() {
				return printJSON(item)
			}
			printItemDetail(item)
			return nil
		},
	}

	// bibtex command
	var bibtexAll bool
	var bibtexCollection string
	bibtexCmd := &cobra.Command{
		Use:   "bibtex [query]",
		Short: "Export as BibTeX",
		Annotations: map[string]string{
			"args": "query: search keyword (optional, requires --all or --collection if omitted)",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			query := strings.Join(args, " ")
			if query == "" && !bibtexAll && bibtexCollection == "" {
				return &CLIError{
					Code:    ErrCodeInvalidArgument,
					Message: "specify a query, --all, or --collection",
				}
			}
			bib, err := client.GetBibTeX(query, bibtexCollection, bibtexAll)
			if err != nil {
				return err
			}
			if isJSON() {
				return printJSON(map[string]string{"bibtex": bib})
			}
			fmt.Print(bib)
			return nil
		},
	}
	bibtexCmd.Flags().BoolVar(&bibtexAll, "all", false, "Export all items")
	bibtexCmd.Flags().StringVar(&bibtexCollection, "collection", "", "Filter by collection key")

	// collections command
	collectionsCmd := &cobra.Command{
		Use:   "collections",
		Short: "List collections",
		Annotations: map[string]string{
			"args": "none",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			collections, err := client.ListCollections()
			if err != nil {
				return err
			}
			if isJSON() {
				if collections == nil {
					collections = []zotero.Collection{}
				}
				return printJSON(collections)
			}
			if len(collections) == 0 {
				fmt.Println("No collections found")
				return nil
			}
			printCollectionTable(collections)
			return nil
		},
	}

	// fulltext command
	var fulltextMaxChars int
	fulltextCmd := &cobra.Command{
		Use:   "fulltext <itemKey>",
		Short: "Show full text of an item",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"args": "itemKey: 8-character alphanumeric item key (required)",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateItemKey(args[0]); err != nil {
				return err
			}
			client, err := newClient()
			if err != nil {
				return err
			}

			item, err := client.GetItem(args[0])
			if err != nil {
				return err
			}

			ft, ftErr := client.GetFullText(args[0])

			if isJSON() {
				type fulltextData struct {
					Item     *zotero.Item             `json:"item"`
					FullText *zotero.FullTextResponse `json:"fullText,omitempty"`
				}
				result := fulltextData{Item: item}
				if ftErr == nil {
					content := ft.Content
					if fulltextMaxChars > 0 && len(content) > fulltextMaxChars {
						content = content[:fulltextMaxChars]
					}
					result.FullText = &zotero.FullTextResponse{
						Content:      content,
						IndexedPages: ft.IndexedPages,
						TotalPages:   ft.TotalPages,
					}
				}
				return printJSON(result)
			}

			fmt.Println("=== METADATA ===")
			fmt.Printf("Key:      %s\n", item.Key)
			fmt.Printf("Title:    %s\n", item.Data.Title)
			fmt.Printf("Authors:  %s\n", zotero.FormatAuthors(item.Data.Creators))
			fmt.Printf("Date:     %s\n", item.Data.Date)
			if item.Data.DOI != "" {
				fmt.Printf("DOI:      %s\n", item.Data.DOI)
			}

			if item.Data.AbstractNote != "" {
				fmt.Println("\n=== ABSTRACT ===")
				fmt.Println(item.Data.AbstractNote)
			}

			if ftErr != nil {
				fmt.Fprintf(os.Stderr, "\nFull text not available: %v\n", ftErr)
				return nil
			}

			pageInfo := ""
			if ft.TotalPages > 0 {
				pageInfo = fmt.Sprintf(" (%d pages)", ft.TotalPages)
			}
			fmt.Printf("\n=== FULL TEXT%s ===\n", pageInfo)

			content := ft.Content
			if fulltextMaxChars > 0 && len(content) > fulltextMaxChars {
				content = content[:fulltextMaxChars] + fmt.Sprintf("\n[TRUNCATED at %d chars]", fulltextMaxChars)
			}
			fmt.Println(content)
			return nil
		},
	}
	fulltextCmd.Flags().IntVar(&fulltextMaxChars, "max-chars", 0, "Max characters (0 for unlimited)")

	// fullsearch command
	var fullsearchTag string
	var fullsearchLimit int
	fullsearchCmd := &cobra.Command{
		Use:   "fullsearch <query>",
		Short: "Full-text search",
		Args:  cobra.MinimumNArgs(1),
		Annotations: map[string]string{
			"args": "query: search keyword (required)",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			query := strings.Join(args, " ")
			if err := sanitizeInput(query); err != nil {
				return err
			}
			items, err := client.FullTextSearch(query, fullsearchTag, fullsearchLimit)
			if err != nil {
				return err
			}
			if isJSON() {
				if items == nil {
					items = []zotero.Item{}
				}
				return printJSON(items)
			}
			if len(items) == 0 {
				fmt.Println("No results found")
				return nil
			}
			printItemTable(items)
			return nil
		},
	}
	fullsearchCmd.Flags().StringVar(&fullsearchTag, "tag", "", "Filter by tag")
	fullsearchCmd.Flags().IntVar(&fullsearchLimit, "limit", 25, "Number of items to display")

	// annotations command
	var annotationsColor string
	var annotationsType string
	annotationsCmd := &cobra.Command{
		Use:   "annotations <itemKey>",
		Short: "Show annotations (highlights/comments) of an item",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"args": "itemKey: 8-character alphanumeric item key (required)",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateItemKey(args[0]); err != nil {
				return err
			}
			client, err := newClient()
			if err != nil {
				return err
			}
			anns, err := client.GetAnnotations(args[0])
			if err != nil {
				return err
			}
			if len(anns) == 0 {
				fmt.Fprintln(os.Stderr, "Note: annotations are only available via the Web API after Zotero sync. If you expect annotations here, check that sync is enabled.")
			}
			filtered := zotero.FilterAnnotations(anns, annotationsColor, annotationsType)
			if isJSON() {
				if filtered == nil {
					filtered = []zotero.Item{}
				}
				return printJSON(filtered)
			}
			if len(filtered) == 0 {
				fmt.Println("No annotations found")
				return nil
			}
			for _, a := range filtered {
				fmt.Println(zotero.FormatAnnotation(a))
			}
			return nil
		},
	}
	annotationsCmd.Flags().StringVar(&annotationsColor, "color", "", "Filter by color (e.g. #ff0000)")
	annotationsCmd.Flags().StringVar(&annotationsType, "type", "", "Filter by type: highlight, underline, note, ink, image")

	// context command
	var contextWithNotes bool
	var contextWithAnnotations bool
	var contextJSON bool
	contextCmd := &cobra.Command{
		Use:   "context <itemKey>",
		Short: "Show all information about an item",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"args": "itemKey: 8-character alphanumeric item key (required)",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateItemKey(args[0]); err != nil {
				return err
			}
			client, err := newClient()
			if err != nil {
				return err
			}
			itemKey := args[0]

			bundle, err := client.GetContext(itemKey, contextWithAnnotations)
			if err != nil {
				return err
			}

			if isJSON() || contextJSON {
				if isJSON() {
					return printJSON(bundle)
				}
				data, err := json.MarshalIndent(bundle, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			// Text output
			fmt.Printf("=== ITEM: %s ===\n", bundle.Item.Key)
			printItemDetail(bundle.Item)

			if bundle.FullText != nil {
				pageInfo := ""
				if bundle.FullText.TotalPages > 0 {
					pageInfo = fmt.Sprintf(" (%d pages)", bundle.FullText.TotalPages)
				}
				fmt.Printf("\n=== FULL TEXT%s ===\n", pageInfo)
				fmt.Println(bundle.FullText.Content)
			}

			if contextWithAnnotations {
				fmt.Printf("\n=== ANNOTATIONS (%d) ===\n", len(bundle.Annotations))
				if len(bundle.Annotations) == 0 {
					fmt.Println("(none — if you expect annotations, check that Zotero sync is enabled)")
				}
				for _, a := range bundle.Annotations {
					fmt.Println(zotero.FormatAnnotation(a))
				}
			}

			if contextWithNotes && len(bundle.Notes) > 0 {
				fmt.Printf("\n=== NOTES (%d) ===\n", len(bundle.Notes))
				for i, note := range bundle.Notes {
					fmt.Printf("--- Note %d (%s) ---\n", i+1, note.Key)
					fmt.Println(note.Data.Note)
					fmt.Println()
				}
			}

			if len(bundle.Attachments) > 0 {
				fmt.Printf("\n=== ATTACHMENTS ===\n")
				for _, att := range bundle.Attachments {
					name := att.Data.Filename
					if name == "" {
						name = att.Data.Title
					}
					fmt.Printf("- %s (key: %s)\n", name, att.Key)
				}
			}

			return nil
		},
	}
	contextCmd.Flags().BoolVar(&contextWithNotes, "with-notes", false, "Include notes")
	contextCmd.Flags().BoolVar(&contextWithAnnotations, "with-annotations", false, "Include annotations (highlights/comments)")
	contextCmd.Flags().BoolVar(&contextJSON, "json", false, "Output as JSON (legacy, prefer --output json)")

	// add-note command
	var noteBody string
	var noteBodyFile string
	var noteTags string
	var noteDryRun bool
	addNoteCmd := &cobra.Command{
		Use:   "add-note <parentItemKey>",
		Short: "Add a note to an item",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"args": "parentItemKey: 8-character alphanumeric item key of parent item (required)",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateItemKey(args[0]); err != nil {
				return err
			}

			content := noteBody
			if noteBodyFile == "-" {
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return &CLIError{Code: ErrCodeIOError, Message: fmt.Sprintf("failed to read stdin: %v", err)}
				}
				content = string(data)
			} else if noteBodyFile != "" {
				if err := sanitizeInput(noteBodyFile); err != nil {
					return err
				}
				data, err := os.ReadFile(noteBodyFile)
				if err != nil {
					return &CLIError{Code: ErrCodeIOError, Message: fmt.Sprintf("failed to read file: %v", err)}
				}
				content = string(data)
			}

			if content == "" {
				return &CLIError{
					Code:       ErrCodeInvalidArgument,
					Message:    "note content is empty",
					Suggestion: "Specify note content with --body or --body-file",
				}
			}

			var extraTags []string
			if noteTags != "" {
				extraTags = strings.Split(noteTags, ",")
			}
			tags := zotero.NoteTags(extraTags)

			if noteDryRun {
				payload := map[string]any{
					"parentItem": args[0],
					"content":    content,
					"tags":       tags,
				}
				if isJSON() {
					return printJSON(map[string]any{
						"dryRun":  true,
						"payload": payload,
					})
				}
				fmt.Println("=== DRY RUN (no API call will be made) ===")
				fmt.Printf("Parent Item: %s\n", args[0])
				fmt.Printf("Tags:        %s\n", strings.Join(tags, ", "))
				fmt.Printf("Content:\n%s\n", content)
				return nil
			}

			client, err := newClient()
			if err != nil {
				return err
			}

			key, err := client.CreateNote(args[0], content, tags)
			if err != nil {
				return err
			}
			if isJSON() {
				return printJSON(map[string]string{"noteKey": key})
			}
			fmt.Printf("Note created: %s\n", key)
			return nil
		},
	}
	addNoteCmd.Flags().StringVar(&noteBody, "body", "", "Note content")
	addNoteCmd.Flags().StringVar(&noteBodyFile, "body-file", "", "Read note content from file (- for stdin)")
	addNoteCmd.Flags().StringVar(&noteTags, "tags", "", "Comma-separated tags (ai-generated is always added)")
	addNoteCmd.Flags().BoolVar(&noteDryRun, "dry-run", false, "Show payload without making API call")

	// delete-note command
	//
	// Guardrails layered on top of the zotero.DeleteNote structural guards
	// (notes-only, single-key, lost-update-safe):
	//   - Approval flow. The command ALWAYS shows what it is about to delete,
	//     then requires explicit approval before doing it. Interactively that
	//     means typing "yes" at a prompt; for non-interactive callers (scripts,
	//     this CLI driven by an agent) the --yes flag pre-approves the one key
	//     that was named. Either way the destructive step never happens from a
	//     bare command — there is no path where deletion occurs without a
	//     deliberate, per-invocation confirmation.
	//   - No bulk deletion. The command takes exactly one key and has no
	//     by-tag / by-query / "delete all" mode, so a single approval can only
	//     ever remove a single note.
	//   - Type-checked before the prompt. We fetch and reject non-note items up
	//     front, so the preview makes it obvious you pointed at the wrong key
	//     before any approval is given.
	// The CLI intentionally does NOT require the ai-generated tag: a human has
	// named one explicit key and approved it, so they may delete any note (the
	// MCP path keeps that extra guard because it runs unattended).
	var deleteNoteYes bool
	deleteNoteCmd := &cobra.Command{
		Use:   "delete-note <itemKey>",
		Short: "Delete a single note (guardrailed: notes only, one key, approval required)",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"args": "itemKey: 8-character alphanumeric key of the note to delete (required)",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateItemKey(args[0]); err != nil {
				return err
			}
			client, err := newClient()
			if err != nil {
				return err
			}

			item, err := client.GetItem(args[0])
			if err != nil {
				return err
			}
			if item.Data.ItemType != "note" {
				return &CLIError{
					Code:       ErrCodeValidation,
					Message:    fmt.Sprintf("refusing to delete %s: item type is %q, not \"note\"", args[0], item.Data.ItemType),
					Suggestion: "This command only deletes notes. Delete other item types in the Zotero app.",
				}
			}

			// JSON mode cannot prompt; require --yes as the machine-readable
			// approval. Without it we report what would be deleted and stop.
			if isJSON() {
				if !deleteNoteYes {
					return printJSON(map[string]any{
						"wouldDelete":          true,
						"noteKey":              item.Key,
						"parentItem":           item.Data.ParentItem,
						"tags":                 tagStrings(item.Data.Tags),
						"confirmationRequired": "re-run with --yes to delete",
					})
				}
				if err := client.DeleteNoteItem(item, false); err != nil {
					return err
				}
				return printJSON(map[string]any{"deleted": true, "noteKey": item.Key})
			}

			// Text mode: show the note, then require approval.
			fmt.Println("=== NOTE TO DELETE ===")
			printNotePreview(item)
			if !deleteNoteYes {
				fmt.Print("\nDelete this note? Type 'yes' to confirm: ")
				answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				if strings.TrimSpace(answer) != "yes" {
					fmt.Println("Aborted — no note was deleted.")
					return nil
				}
			}

			if err := client.DeleteNoteItem(item, false); err != nil {
				return err
			}
			fmt.Printf("Note deleted: %s\n", item.Key)
			return nil
		},
	}
	deleteNoteCmd.Flags().BoolVar(&deleteNoteYes, "yes", false, "Pre-approve deletion (skips the interactive prompt; required in --output json mode)")

	// export command
	var exportCollection string
	var exportTag string
	var exportKeys string
	var exportFormat string
	var exportLimit int
	exportCmd := &cobra.Command{
		Use:   "export",
		Short: "Batch export for literature review",
		Annotations: map[string]string{
			"args": "none",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if exportCollection == "" && exportTag == "" && exportKeys == "" {
				return &CLIError{
					Code:       ErrCodeInvalidArgument,
					Message:    "no filter specified",
					Suggestion: "Specify --collection, --tag, or --keys",
				}
			}

			client, err := newClient()
			if err != nil {
				return err
			}

			var items []zotero.Item
			switch {
			case exportKeys != "":
				keys := strings.Split(exportKeys, ",")
				items, err = client.GetItemsByKeys(keys)
			case exportCollection != "":
				items, err = client.ListItems(exportCollection, exportLimit)
			case exportTag != "":
				items, err = client.ListItemsByTag(exportTag, exportLimit)
			}
			if err != nil {
				return err
			}

			if isJSON() || exportFormat == "json" {
				if items == nil {
					items = []zotero.Item{}
				}
				if isJSON() && exportFormat != "json" && exportFormat != "full" {
					return printJSON(items)
				}
				type exportItem struct {
					Item     zotero.Item              `json:"item"`
					FullText *zotero.FullTextResponse `json:"fullText,omitempty"`
				}
				var results []exportItem
				for i, item := range items {
					ei := exportItem{Item: item}
					fmt.Fprintf(os.Stderr, "Fetching %d/%d...\n", i+1, len(items))
					ft, _ := client.GetFullText(item.Key)
					ei.FullText = ft
					results = append(results, ei)
				}
				if results == nil {
					results = []exportItem{}
				}
				if isJSON() {
					return printJSON(results)
				}
				data, err := json.MarshalIndent(results, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			if len(items) == 0 {
				fmt.Println("No items found")
				return nil
			}

			for i, item := range items {
				if i > 0 {
					fmt.Println("\n" + strings.Repeat("=", 60))
				}
				fmt.Printf("\n=== [%d/%d] %s ===\n", i+1, len(items), item.Key)
				printItemDetail(&item)

				if exportFormat == "full" {
					fmt.Fprintf(os.Stderr, "Fetching %d/%d...\n", i+1, len(items))
					ft, err := client.GetFullText(item.Key)
					if err == nil && ft.Content != "" {
						pageInfo := ""
						if ft.TotalPages > 0 {
							pageInfo = fmt.Sprintf(" (%d pages)", ft.TotalPages)
						}
						fmt.Printf("\n=== FULL TEXT%s ===\n", pageInfo)
						fmt.Println(ft.Content)
					}
				}
			}
			return nil
		},
	}
	exportCmd.Flags().StringVar(&exportCollection, "collection", "", "Collection key")
	exportCmd.Flags().StringVar(&exportTag, "tag", "", "Filter by tag")
	exportCmd.Flags().StringVar(&exportKeys, "keys", "", "Comma-separated item keys")
	exportCmd.Flags().StringVar(&exportFormat, "format", "summary", "Output format: summary, full, json")
	exportCmd.Flags().IntVar(&exportLimit, "limit", 100, "Max items")

	// upload command
	var uploadParent string
	var uploadTags string
	var uploadDryRun bool
	var uploadTitle string
	uploadCmd := &cobra.Command{
		Use:   "upload <filePath>",
		Short: "Upload a file as an attachment",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"args": "filePath: path to the file to upload (required)",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := args[0]
			if err := validateFilePath(filePath); err != nil {
				return err
			}

			fileData, err := os.ReadFile(filePath)
			if err != nil {
				return &CLIError{Code: ErrCodeIOError, Message: fmt.Sprintf("failed to read file: %v", err)}
			}

			fileInfo, err := os.Stat(filePath)
			if err != nil {
				return &CLIError{Code: ErrCodeIOError, Message: fmt.Sprintf("failed to stat file: %v", err)}
			}

			filename := filepath.Base(filePath)
			filesize := fileInfo.Size()
			mtime := fileInfo.ModTime().UnixMilli()

			hash := md5.Sum(fileData)
			md5hex := hex.EncodeToString(hash[:])

			contentType := mime.TypeByExtension(filepath.Ext(filePath))
			if contentType == "" {
				contentType = "application/octet-stream"
			}

			tags := []string{}
			if uploadTags != "" {
				for _, t := range strings.Split(uploadTags, ",") {
					t = strings.TrimSpace(t)
					if t != "" {
						tags = append(tags, t)
					}
				}
			}

			title := uploadTitle
			if title == "" {
				title = filename
			}

			if uploadDryRun {
				payload := map[string]any{
					"filename":    filename,
					"title":       title,
					"filesize":    filesize,
					"md5":         md5hex,
					"mtime":       mtime,
					"contentType": contentType,
					"parentItem":  uploadParent,
					"tags":        tags,
				}
				if isJSON() {
					return printJSON(map[string]any{
						"dryRun":  true,
						"payload": payload,
					})
				}
				fmt.Println("=== DRY RUN (no API call will be made) ===")
				fmt.Printf("File:         %s\n", filePath)
				fmt.Printf("Filename:     %s\n", filename)
				fmt.Printf("Title:        %s\n", title)
				fmt.Printf("Size:         %d bytes\n", filesize)
				fmt.Printf("MD5:          %s\n", md5hex)
				fmt.Printf("Content-Type: %s\n", contentType)
				if uploadParent != "" {
					fmt.Printf("Parent Item:  %s\n", uploadParent)
				}
				if len(tags) > 0 {
					fmt.Printf("Tags:         %s\n", strings.Join(tags, ", "))
				}
				return nil
			}

			if uploadParent != "" {
				if err := validateItemKey(uploadParent); err != nil {
					return err
				}
			}

			client, err := newClient()
			if err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "Creating attachment item...\n")
			attachKey, err := client.CreateAttachment(uploadParent, filename, title, contentType, tags)
			if err != nil {
				return fmt.Errorf("failed to create attachment: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Attachment created: %s\n", attachKey)

			fmt.Fprintf(os.Stderr, "Requesting upload authorization...\n")
			auth, err := client.GetUploadAuthorization(attachKey, filename, filesize, md5hex, mtime)
			if err != nil {
				return fmt.Errorf("failed to get upload authorization: %w", err)
			}

			fmt.Fprintf(os.Stderr, "Uploading file content (%d bytes)...\n", filesize)
			if err := client.UploadFileContent(auth, fileData); err != nil {
				return fmt.Errorf("failed to upload file: %w", err)
			}

			fmt.Fprintf(os.Stderr, "Registering upload...\n")
			if err := client.RegisterUpload(attachKey, auth.UploadKey); err != nil {
				return fmt.Errorf("failed to register upload: %w", err)
			}

			if isJSON() {
				return printJSON(map[string]string{
					"attachmentKey": attachKey,
					"filename":      filename,
				})
			}
			fmt.Printf("Upload complete: %s (key: %s)\n", filename, attachKey)
			return nil
		},
	}
	uploadCmd.Flags().StringVar(&uploadParent, "parent", "", "Parent item key (standalone attachment if omitted)")
	uploadCmd.Flags().StringVar(&uploadTags, "tags", "", "Comma-separated tags")
	uploadCmd.Flags().BoolVar(&uploadDryRun, "dry-run", false, "Show payload without making API call")
	uploadCmd.Flags().StringVar(&uploadTitle, "title", "", "Attachment title (defaults to filename)")

	// tags command
	tagsCmd := &cobra.Command{
		Use:   "tags",
		Short: "List all tags in the library (closed vocabulary source)",
		Annotations: map[string]string{
			"args": "none",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			tags, err := client.ListTags()
			if err != nil {
				return err
			}
			if isJSON() {
				if tags == nil {
					tags = []zotero.LibraryTag{}
				}
				return printJSON(tags)
			}
			if len(tags) == 0 {
				fmt.Println("No tags found")
				return nil
			}
			printTagTable(tags)
			return nil
		},
	}

	// tag command
	//
	// Edits an item's own tag set. The CLI deliberately does NOT enforce a
	// closed vocabulary — it adds/removes exactly the tags it is given. The
	// vocabulary constraint (only reuse existing tags, never invent new ones)
	// lives in the `tag` skill, which fetches the existing tag list first and
	// chooses from it; the separate `tag-new` skill is the only place that
	// intentionally introduces a new tag. Keeping the constraint in the skill
	// layer leaves the CLI a clean primitive usable from either path.
	var tagAdd []string
	var tagRemove []string
	var tagDryRun bool
	tagCmd := &cobra.Command{
		Use:   "tag <itemKey>",
		Short: "Add or remove tags on an item",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"args": "itemKey: 8-character alphanumeric item key (required)",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateItemKey(args[0]); err != nil {
				return err
			}
			if len(tagAdd) == 0 && len(tagRemove) == 0 {
				return &CLIError{
					Code:       ErrCodeInvalidArgument,
					Message:    "no tag changes specified",
					Suggestion: "Specify at least one --add or --remove",
				}
			}
			if err := validateTags(tagAdd); err != nil {
				return err
			}
			if err := validateTags(tagRemove); err != nil {
				return err
			}

			client, err := newClient()
			if err != nil {
				return err
			}

			// A read is needed even for --dry-run, because the resulting tag
			// set is the current tags with the deltas applied. The read is
			// idempotent; --dry-run only guarantees no *write* is performed.
			item, err := client.GetItem(args[0])
			if err != nil {
				return err
			}

			result := zotero.ApplyTagDelta(item.Data.Tags, tagAdd, tagRemove)

			if tagDryRun {
				if isJSON() {
					return printJSON(tagDryRunPayload(args[0], tagAdd, tagRemove, result))
				}
				fmt.Println("=== DRY RUN (no API call will be made) ===")
				fmt.Printf("Item:        %s\n", args[0])
				if len(tagAdd) > 0 {
					fmt.Printf("Add:         %s\n", strings.Join(tagAdd, ", "))
				}
				if len(tagRemove) > 0 {
					fmt.Printf("Remove:      %s\n", strings.Join(tagRemove, ", "))
				}
				fmt.Printf("Result tags: %s\n", strings.Join(tagStrings(result), ", "))
				return nil
			}

			updated, err := client.UpdateItemTags(item, tagAdd, tagRemove)
			if err != nil {
				return err
			}
			if isJSON() {
				return printJSON(tagResultPayload(args[0], updated))
			}
			fmt.Printf("Tags updated: %s\n", args[0])
			fmt.Printf("Tags: %s\n", strings.Join(tagStrings(updated), ", "))
			return nil
		},
	}
	tagCmd.Flags().StringArrayVar(&tagAdd, "add", nil, "Tag to add (repeatable)")
	tagCmd.Flags().StringArrayVar(&tagRemove, "remove", nil, "Tag to remove (repeatable)")
	tagCmd.Flags().BoolVar(&tagDryRun, "dry-run", false, "Show resulting tags without making API call")

	// citations command
	citationsCmd := newCitationsCmd()

	// add command
	addCmd := newAddCmd()

	// schema command
	schemaCmd := newSchemaCmd(rootCmd)

	rootCmd.AddCommand(configCmd, searchCmd, listCmd, getCmd, bibtexCmd, collectionsCmd,
		fulltextCmd, fullsearchCmd, annotationsCmd, contextCmd, addNoteCmd, deleteNoteCmd, exportCmd, uploadCmd,
		tagsCmd, tagCmd, citationsCmd, addCmd, schemaCmd)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		cliErr := classifyError(err)
		if isJSON() {
			printErrorJSON(cliErr.Code, cliErr.Message, cliErr.Suggestion)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", cliErr.Message)
			if cliErr.Suggestion != "" {
				fmt.Fprintf(os.Stderr, "Suggestion: %s\n", cliErr.Suggestion)
			}
		}
		os.Exit(1)
	}
}

func printItemTable(items []zotero.Item) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tTITLE\tAUTHORS\tDATE\tTYPE")
	fmt.Fprintln(w, "---\t-----\t-------\t----\t----")
	for _, item := range items {
		if item.Data.ItemType == "attachment" || item.Data.ItemType == "note" {
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			item.Key,
			zotero.Truncate(item.Data.Title, 60),
			zotero.Truncate(zotero.FormatAuthors(item.Data.Creators), 40),
			item.Data.Date,
			item.Data.ItemType,
		)
	}
	w.Flush()
}

func printItemDetail(item *zotero.Item) {
	d := item.Data
	fmt.Printf("Key:      %s\n", item.Key)
	fmt.Printf("Type:     %s\n", d.ItemType)
	fmt.Printf("Title:    %s\n", d.Title)
	fmt.Printf("Authors:  %s\n", zotero.FormatAuthors(d.Creators))
	fmt.Printf("Date:     %s\n", d.Date)
	if d.PublicationTitle != "" {
		fmt.Printf("Journal:  %s\n", d.PublicationTitle)
	}
	if d.DOI != "" {
		fmt.Printf("DOI:      %s\n", d.DOI)
	}
	if d.URL != "" {
		fmt.Printf("URL:      %s\n", d.URL)
	}
	fmt.Printf("Tags:     %s\n", zotero.FormatTags(d.Tags))
	if d.AbstractNote != "" {
		fmt.Printf("\nAbstract:\n%s\n", d.AbstractNote)
	}
}

// tagStrings extracts plain tag strings for human-readable output.
func tagStrings(tags []zotero.Tag) []string {
	out := []string{}
	for _, t := range tags {
		out = append(out, t.Tag)
	}
	return out
}

// emptyIfNil normalizes a nil slice to a non-nil empty slice so JSON renders it
// as `[]` rather than `null`, matching the project convention that empty
// results are `[]` (see CLAUDE.md and the `tags`/`collections` commands).
func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// tagDryRunPayload builds the --dry-run JSON payload. add/remove are normalized
// to non-nil slices (the unspecified side must not serialize as null), and
// resultTags carries full []zotero.Tag objects so the shape matches the `tags`
// field of `get`/`context` rather than diverging into a bare string array.
func tagDryRunPayload(itemKey string, add, remove []string, result []zotero.Tag) map[string]any {
	return map[string]any{
		"dryRun": true,
		"payload": map[string]any{
			"itemKey":    itemKey,
			"add":        emptyIfNil(add),
			"remove":     emptyIfNil(remove),
			"resultTags": result,
		},
	}
}

// tagResultPayload builds the post-update JSON payload. tags carries full
// []zotero.Tag objects so consumers can rely on the same `{"tag":...}` shape
// `get`/`context` emit.
func tagResultPayload(itemKey string, tags []zotero.Tag) map[string]any {
	return map[string]any{
		"itemKey": itemKey,
		"tags":    tags,
	}
}

// validateTags rejects empty/whitespace-only tags and tags containing control
// characters or path-traversal sequences, matching the VALIDATION policy used
// for other user input. Tag values are written verbatim to the library, so a
// blank or control-laden tag would pollute the vocabulary. Unlike free text,
// a tag is a single-line vocabulary term, so the whitespace control characters
// sanitizeInput tolerates (newline/carriage-return/tab) are also rejected — a
// tab in particular would break the `tags` table (tabwriter is tab-delimited).
func validateTags(tags []string) error {
	for _, t := range tags {
		if strings.TrimSpace(t) == "" {
			return &CLIError{
				Code:       ErrCodeValidation,
				Message:    "tag is empty",
				Suggestion: "Provide a non-empty tag value",
			}
		}
		if err := sanitizeInput(t); err != nil {
			return err
		}
		if strings.ContainsAny(t, "\n\r\t") {
			return &CLIError{
				Code:       ErrCodeValidation,
				Message:    "tag contains a newline or tab",
				Suggestion: "Tags must be a single line without tabs or newlines",
			}
		}
	}
	return nil
}

// printTagTable renders the library tag list with each tag's usage count.
func printTagTable(tags []zotero.LibraryTag) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TAG\tITEMS")
	fmt.Fprintln(w, "---\t-----")
	for _, t := range tags {
		fmt.Fprintf(w, "%s\t%d\n", t.Tag, t.Meta.NumItems)
	}
	w.Flush()
}

// printNotePreview shows what a delete-note would remove: the key, its parent,
// tags, and a short snippet of the body so the human can recognise the note
// before confirming with --yes.
func printNotePreview(item *zotero.Item) {
	fmt.Printf("Key:        %s\n", item.Key)
	if item.Data.ParentItem != "" {
		fmt.Printf("Parent:     %s\n", item.Data.ParentItem)
	}
	fmt.Printf("Tags:       %s\n", zotero.FormatTags(item.Data.Tags))
	snippet := zotero.Truncate(noteSnippet(item.Data.Note), 200)
	if snippet != "" {
		fmt.Printf("Preview:    %s\n", snippet)
	}
}

// noteSnippet reduces note HTML to a one-line plain-text preview.
func noteSnippet(noteHTML string) string {
	s := noteTagPattern.ReplaceAllString(noteHTML, " ")
	return strings.Join(strings.Fields(s), " ")
}

func printCollectionTable(collections []zotero.Collection) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tNAME\tITEMS")
	fmt.Fprintln(w, "---\t----\t-----")
	for _, col := range collections {
		fmt.Fprintf(w, "%s\t%s\t%d\n",
			col.Key,
			col.Data.Name,
			col.Data.NumItems,
		)
	}
	w.Flush()
}
