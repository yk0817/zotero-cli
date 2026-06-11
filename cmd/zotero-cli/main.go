package main

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/yk0817/zotero-cli/zotero"
)

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

			bundle, err := client.GetContext(itemKey)
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

			tags := []string{"ai-generated"}
			if noteTags != "" {
				for _, t := range strings.Split(noteTags, ",") {
					t = strings.TrimSpace(t)
					if t != "" && t != "ai-generated" {
						tags = append(tags, t)
					}
				}
			}

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

	// schema command
	schemaCmd := newSchemaCmd(rootCmd)

	rootCmd.AddCommand(configCmd, searchCmd, listCmd, getCmd, bibtexCmd, collectionsCmd,
		fulltextCmd, fullsearchCmd, annotationsCmd, contextCmd, addNoteCmd, exportCmd, uploadCmd, schemaCmd)

	if err := rootCmd.Execute(); err != nil {
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
