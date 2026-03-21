package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "zotero-cli",
		Short: "Zotero Web API CLIクライアント",
	}

	// config command
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "APIキーとユーザーIDを設定",
		RunE: func(cmd *cobra.Command, args []string) error {
			reader := bufio.NewReader(os.Stdin)

			fmt.Print("Zotero User ID: ")
			userID, _ := reader.ReadString('\n')
			userID = strings.TrimSpace(userID)

			fmt.Print("Zotero API Key: ")
			apiKey, _ := reader.ReadString('\n')
			apiKey = strings.TrimSpace(apiKey)

			if userID == "" || apiKey == "" {
				return fmt.Errorf("ユーザーIDとAPIキーは必須です")
			}

			cfg := &Config{APIKey: apiKey, UserID: userID}
			if err := saveConfig(cfg); err != nil {
				return err
			}
			fmt.Printf("設定を保存しました: %s\n", configPath())
			return nil
		},
	}

	// search command
	var searchTag string
	var searchTitle bool
	searchCmd := &cobra.Command{
		Use:   "search <query>",
		Short: "キーワードでアイテムを検索",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			client := NewZoteroClient(cfg)
			query := strings.Join(args, " ")
			items, err := client.SearchItems(query, searchTag)
			if err != nil {
				return err
			}
			if searchTitle {
				lowerQuery := strings.ToLower(query)
				var filtered []Item
				for _, item := range items {
					if strings.Contains(strings.ToLower(item.Data.Title), lowerQuery) {
						filtered = append(filtered, item)
					}
				}
				items = filtered
			}
			if len(items) == 0 {
				fmt.Println("検索結果がありません")
				return nil
			}
			printItemTable(items)
			return nil
		},
	}
	searchCmd.Flags().StringVar(&searchTag, "tag", "", "タグで絞り込み")
	searchCmd.Flags().BoolVar(&searchTitle, "title", false, "タイトルのみで絞り込み")

	// list command
	var listCollection string
	var listLimit int
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "アイテム一覧を表示",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			client := NewZoteroClient(cfg)
			items, err := client.ListItems(listCollection, listLimit)
			if err != nil {
				return err
			}
			if len(items) == 0 {
				fmt.Println("アイテムがありません")
				return nil
			}
			printItemTable(items)
			return nil
		},
	}
	listCmd.Flags().StringVar(&listCollection, "collection", "", "コレクションキーで絞り込み")
	listCmd.Flags().IntVar(&listLimit, "limit", 25, "表示件数")

	// get command
	getCmd := &cobra.Command{
		Use:   "get <itemKey>",
		Short: "アイテムの詳細を表示",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			client := NewZoteroClient(cfg)
			item, err := client.GetItem(args[0])
			if err != nil {
				return err
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
		Short: "BibTeX形式で出力",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			client := NewZoteroClient(cfg)
			query := strings.Join(args, " ")
			if query == "" && !bibtexAll && bibtexCollection == "" {
				return fmt.Errorf("検索クエリ、--all、または --collection を指定してください")
			}
			bib, err := client.GetBibTeX(query, bibtexCollection, bibtexAll)
			if err != nil {
				return err
			}
			fmt.Print(bib)
			return nil
		},
	}
	bibtexCmd.Flags().BoolVar(&bibtexAll, "all", false, "全アイテムを出力")
	bibtexCmd.Flags().StringVar(&bibtexCollection, "collection", "", "コレクションキーで絞り込み")

	// collections command
	collectionsCmd := &cobra.Command{
		Use:   "collections",
		Short: "コレクション一覧を表示",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			client := NewZoteroClient(cfg)
			collections, err := client.ListCollections()
			if err != nil {
				return err
			}
			if len(collections) == 0 {
				fmt.Println("コレクションがありません")
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
		Short: "論文のフルテキストを表示",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			client := NewZoteroClient(cfg)

			item, err := client.GetItem(args[0])
			if err != nil {
				return err
			}

			fmt.Println("=== METADATA ===")
			fmt.Printf("Key:      %s\n", item.Key)
			fmt.Printf("Title:    %s\n", item.Data.Title)
			fmt.Printf("Authors:  %s\n", formatAuthors(item.Data.Creators))
			fmt.Printf("Date:     %s\n", item.Data.Date)
			if item.Data.DOI != "" {
				fmt.Printf("DOI:      %s\n", item.Data.DOI)
			}

			if item.Data.AbstractNote != "" {
				fmt.Println("\n=== ABSTRACT ===")
				fmt.Println(item.Data.AbstractNote)
			}

			ft, err := client.GetFullText(args[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "\nフルテキストは利用できません: %v\n", err)
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
	fulltextCmd.Flags().IntVar(&fulltextMaxChars, "max-chars", 0, "最大文字数（0で無制限）")

	// fullsearch command
	var fullsearchTag string
	var fullsearchLimit int
	fullsearchCmd := &cobra.Command{
		Use:   "fullsearch <query>",
		Short: "フルテキストを含めた全文検索",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			client := NewZoteroClient(cfg)
			query := strings.Join(args, " ")
			items, err := client.FullTextSearch(query, fullsearchTag, fullsearchLimit)
			if err != nil {
				return err
			}
			if len(items) == 0 {
				fmt.Println("検索結果がありません")
				return nil
			}
			printItemTable(items)
			return nil
		},
	}
	fullsearchCmd.Flags().StringVar(&fullsearchTag, "tag", "", "タグで絞り込み")
	fullsearchCmd.Flags().IntVar(&fullsearchLimit, "limit", 25, "表示件数")

	// context command
	var contextWithNotes bool
	var contextJSON bool
	contextCmd := &cobra.Command{
		Use:   "context <itemKey>",
		Short: "論文の全情報をまとめて表示",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			client := NewZoteroClient(cfg)
			itemKey := args[0]

			item, err := client.GetItem(itemKey)
			if err != nil {
				return err
			}

			ft, _ := client.GetFullText(itemKey)

			children, _ := client.GetChildren(itemKey)
			var notes, attachments []Item
			for _, child := range children {
				switch child.Data.ItemType {
				case "note":
					notes = append(notes, child)
				case "attachment":
					attachments = append(attachments, child)
				}
			}

			if contextJSON {
				bundle := ContextBundle{
					Item:        item,
					FullText:    ft,
					Notes:       notes,
					Attachments: attachments,
				}
				data, err := json.MarshalIndent(bundle, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			// Text output
			fmt.Printf("=== ITEM: %s ===\n", item.Key)
			printItemDetail(item)

			if ft != nil {
				pageInfo := ""
				if ft.TotalPages > 0 {
					pageInfo = fmt.Sprintf(" (%d pages)", ft.TotalPages)
				}
				fmt.Printf("\n=== FULL TEXT%s ===\n", pageInfo)
				fmt.Println(ft.Content)
			}

			if contextWithNotes && len(notes) > 0 {
				fmt.Printf("\n=== NOTES (%d) ===\n", len(notes))
				for i, note := range notes {
					fmt.Printf("--- Note %d (%s) ---\n", i+1, note.Key)
					fmt.Println(note.Data.Note)
					fmt.Println()
				}
			}

			if len(attachments) > 0 {
				fmt.Printf("\n=== ATTACHMENTS ===\n")
				for _, att := range attachments {
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
	contextCmd.Flags().BoolVar(&contextWithNotes, "with-notes", false, "ノートも表示")
	contextCmd.Flags().BoolVar(&contextJSON, "json", false, "JSON形式で出力")

	// add-note command
	var noteBody string
	var noteBodyFile string
	var noteTags string
	addNoteCmd := &cobra.Command{
		Use:   "add-note <parentItemKey>",
		Short: "アイテムにノートを追加",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			client := NewZoteroClient(cfg)

			content := noteBody
			if noteBodyFile == "-" {
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("標準入力の読み込みに失敗: %w", err)
				}
				content = string(data)
			} else if noteBodyFile != "" {
				data, err := os.ReadFile(noteBodyFile)
				if err != nil {
					return fmt.Errorf("ファイルの読み込みに失敗: %w", err)
				}
				content = string(data)
			}

			if content == "" {
				return fmt.Errorf("--body または --body-file でノート内容を指定してください")
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

			key, err := client.CreateNote(args[0], content, tags)
			if err != nil {
				return err
			}
			fmt.Printf("ノートを作成しました: %s\n", key)
			return nil
		},
	}
	addNoteCmd.Flags().StringVar(&noteBody, "body", "", "ノートの内容")
	addNoteCmd.Flags().StringVar(&noteBodyFile, "body-file", "", "ノートの内容をファイルから読み込み（-で標準入力）")
	addNoteCmd.Flags().StringVar(&noteTags, "tags", "", "カンマ区切りのタグ（ai-generatedは自動追加）")

	// export command
	var exportCollection string
	var exportTag string
	var exportKeys string
	var exportFormat string
	var exportLimit int
	exportCmd := &cobra.Command{
		Use:   "export",
		Short: "一括エクスポート（文献レビュー用）",
		RunE: func(cmd *cobra.Command, args []string) error {
			if exportCollection == "" && exportTag == "" && exportKeys == "" {
				return fmt.Errorf("--collection、--tag、または --keys のいずれかを指定してください")
			}

			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			client := NewZoteroClient(cfg)

			var items []Item
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

			if len(items) == 0 {
				fmt.Println("アイテムがありません")
				return nil
			}

			if exportFormat == "json" {
				type exportItem struct {
					Item     Item              `json:"item"`
					FullText *FullTextResponse `json:"fullText,omitempty"`
				}
				var results []exportItem
				for i, item := range items {
					ei := exportItem{Item: item}
					if exportFormat == "json" || exportFormat == "full" {
						fmt.Fprintf(os.Stderr, "Fetching %d/%d...\n", i+1, len(items))
						ft, _ := client.GetFullText(item.Key)
						ei.FullText = ft
					}
					results = append(results, ei)
				}
				data, err := json.MarshalIndent(results, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
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
	exportCmd.Flags().StringVar(&exportCollection, "collection", "", "コレクションキー")
	exportCmd.Flags().StringVar(&exportTag, "tag", "", "タグで絞り込み")
	exportCmd.Flags().StringVar(&exportKeys, "keys", "", "カンマ区切りのアイテムキー")
	exportCmd.Flags().StringVar(&exportFormat, "format", "summary", "出力形式: summary, full, json")
	exportCmd.Flags().IntVar(&exportLimit, "limit", 100, "最大件数")

	rootCmd.AddCommand(configCmd, searchCmd, listCmd, getCmd, bibtexCmd, collectionsCmd,
		fulltextCmd, fullsearchCmd, contextCmd, addNoteCmd, exportCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func printItemTable(items []Item) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tTITLE\tAUTHORS\tDATE\tTYPE")
	fmt.Fprintln(w, "---\t-----\t-------\t----\t----")
	for _, item := range items {
		if item.Data.ItemType == "attachment" || item.Data.ItemType == "note" {
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			item.Key,
			truncate(item.Data.Title, 60),
			truncate(formatAuthors(item.Data.Creators), 40),
			item.Data.Date,
			item.Data.ItemType,
		)
	}
	w.Flush()
}

func printItemDetail(item *Item) {
	d := item.Data
	fmt.Printf("Key:      %s\n", item.Key)
	fmt.Printf("Type:     %s\n", d.ItemType)
	fmt.Printf("Title:    %s\n", d.Title)
	fmt.Printf("Authors:  %s\n", formatAuthors(d.Creators))
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
	fmt.Printf("Tags:     %s\n", formatTags(d.Tags))
	if d.AbstractNote != "" {
		fmt.Printf("\nAbstract:\n%s\n", d.AbstractNote)
	}
}

func printCollectionTable(collections []Collection) {
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
