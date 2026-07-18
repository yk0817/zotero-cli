package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/yk0817/zotero-cli/embed"
	"github.com/yk0817/zotero-cli/semindex"
	"github.com/yk0817/zotero-cli/zotero"
)

// embedBatchSize bounds how many item texts are sent to the embedder per HTTP
// request, so a large library does not build one enormous request body.
const embedBatchSize = 64

// itemLister is the subset of *zotero.Client the index commands need, so tests
// substitute a fake.
type itemLister interface {
	ListAllItems() ([]zotero.Item, error)
}

// buildIndex fetches all items, embeds title+abstract for those with text (in
// batches), and returns a fresh index plus the count of items skipped for having
// no text. Dim is taken from the first embedded vector.
func buildIndex(ctx context.Context, lister itemLister, emb embed.Embedder, libraryID, model string, batchSize int) (*semindex.Index, int, error) {
	items, err := lister.ListAllItems()
	if err != nil {
		return nil, 0, err
	}

	var texts []string
	var withText []zotero.Item
	for _, it := range items {
		if txt := embedText(it); txt != "" {
			texts = append(texts, txt)
			withText = append(withText, it)
		}
	}
	skipped := len(items) - len(withText)

	entries := make([]semindex.Entry, 0, len(withText))
	dim := 0
	for start := 0; start < len(texts); start += batchSize {
		end := start + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := emb.Embed(ctx, texts[start:end])
		if err != nil {
			return nil, 0, err
		}
		for j, v := range vecs {
			if dim == 0 {
				dim = len(v)
			}
			it := withText[start+j]
			entries = append(entries, semindex.Entry{
				ItemKey: it.Key,
				Version: it.Version,
				Title:   it.Data.Title,
				Vector:  v,
			})
		}
	}

	idx := &semindex.Index{
		Header: semindex.Header{
			SchemaVersion: semindex.SchemaVersion,
			LibraryID:     libraryID,
			Model:         model,
			Dim:           dim,
			BuiltAt:       time.Now().UTC(),
		},
		Entries: entries,
	}
	return idx, skipped, nil
}

type updateStats struct {
	Added     int
	Updated   int
	Removed   int
	Unchanged int
	Total     int
}

// updateIndex re-embeds only new/changed items (by version), keeps unchanged
// entries, drops removed ones, and returns the merged index plus counts. The
// header is carried over from the existing index (same library/model/dim).
func updateIndex(ctx context.Context, lister itemLister, emb embed.Embedder, existing *semindex.Index, batchSize int) (*semindex.Index, updateStats, error) {
	items, err := lister.ListAllItems()
	if err != nil {
		return nil, updateStats{}, err
	}

	byKey := make(map[string]zotero.Item, len(items))
	refs := make([]semindex.ItemRef, 0, len(items))
	for _, it := range items {
		byKey[it.Key] = it
		refs = append(refs, semindex.ItemRef{Key: it.Key, Version: it.Version})
	}
	oldKeys := make(map[string]bool, len(existing.Entries))
	for _, e := range existing.Entries {
		oldKeys[e.ItemKey] = true
	}

	toEmbed, keep, removed := existing.Diff(refs)
	removedCount := len(removed)

	// Embed only items that still have text; a changed item that lost its text
	// falls out of the index (not carried over). If such an item was previously
	// indexed, its disappearance is a removal — count it, so the reported stats
	// account for every entry that left the index, not just items deleted from
	// the library.
	var texts []string
	var embItems []zotero.Item
	added, updated := 0, 0
	for _, key := range toEmbed {
		it := byKey[key]
		txt := embedText(it)
		if txt == "" {
			if oldKeys[key] {
				removedCount++
			}
			continue
		}
		texts = append(texts, txt)
		embItems = append(embItems, it)
		if oldKeys[key] {
			updated++
		} else {
			added++
		}
	}

	newEntries := make([]semindex.Entry, 0, len(embItems))
	for start := 0; start < len(texts); start += batchSize {
		end := start + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := emb.Embed(ctx, texts[start:end])
		if err != nil {
			return nil, updateStats{}, err
		}
		for j, v := range vecs {
			it := embItems[start+j]
			newEntries = append(newEntries, semindex.Entry{ItemKey: it.Key, Version: it.Version, Title: it.Data.Title, Vector: v})
		}
	}

	merged := append(keep, newEntries...)
	idx := &semindex.Index{Header: existing.Header, Entries: merged}
	idx.Header.BuiltAt = time.Now().UTC()

	stats := updateStats{
		Added:     added,
		Updated:   updated,
		Removed:   removedCount,
		Unchanged: len(keep),
		Total:     len(merged),
	}
	return idx, stats, nil
}

// runIndexStatus loads the index and reports its metadata. Extracted from the
// cobra RunE so it is unit-testable. A missing index maps to NOT_FOUND with a
// build suggestion.
func runIndexStatus() error {
	path := indexPath()
	idx, err := semindex.Load(path)
	if err != nil {
		if errors.Is(err, semindex.ErrIndexNotFound) {
			return &CLIError{Code: ErrCodeNotFound, Message: "no semantic index found", Suggestion: "Run 'zotero-cli index build' first"}
		}
		return &CLIError{Code: ErrCodeIOError, Message: err.Error()}
	}
	if isJSON() {
		return printJSON(map[string]any{
			"path":    path,
			"indexed": len(idx.Entries),
			"model":   idx.Header.Model,
			"dim":     idx.Header.Dim,
			"builtAt": idx.Header.BuiltAt,
		})
	}
	fmt.Fprintf(os.Stdout, "Index: %s\nItems: %d\nModel: %s (dim %d)\nBuilt: %s\n",
		path, len(idx.Entries), idx.Header.Model, idx.Header.Dim, idx.Header.BuiltAt.Format(time.RFC3339))
	return nil
}

func newIndexCmd() *cobra.Command {
	indexCmd := &cobra.Command{
		Use:   "index",
		Short: "Manage the local semantic search index",
		Annotations: map[string]string{
			"args": "none — use a subcommand: build, update, status",
		},
	}

	buildCmd := &cobra.Command{
		Use:         "build",
		Short:       "Build the semantic index from all library items (title + abstract)",
		Annotations: map[string]string{"args": "none"},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			client := zotero.NewClient(cfg.APIKey, cfg.UserID)
			emb := resolveEmbedder(cfg)

			idx, skipped, err := buildIndex(cmd.Context(), client, emb, cfg.UserID, embedModel(cfg), embedBatchSize)
			if err != nil {
				return &CLIError{Code: ErrCodeAPIError, Message: err.Error(), Suggestion: "check that your embedding server is running at " + embedBaseURL(cfg)}
			}
			path := indexPath()
			if err := idx.Save(path); err != nil {
				return &CLIError{Code: ErrCodeIOError, Message: err.Error()}
			}

			if isJSON() {
				return printJSON(map[string]any{
					"indexed": len(idx.Entries),
					"skipped": skipped,
					"model":   idx.Header.Model,
					"dim":     idx.Header.Dim,
					"path":    path,
				})
			}
			fmt.Fprintf(os.Stdout, "Indexed %d items (skipped %d) -> %s\n", len(idx.Entries), skipped, path)
			return nil
		},
	}

	statusCmd := &cobra.Command{
		Use:         "status",
		Short:       "Show semantic index metadata (path, item count, model)",
		Annotations: map[string]string{"args": "none"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIndexStatus()
		},
	}

	updateCmd := &cobra.Command{
		Use:         "update",
		Short:       "Incrementally update the semantic index (only changed items)",
		Annotations: map[string]string{"args": "none"},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			existing, err := semindex.Load(indexPath())
			if err != nil {
				if errors.Is(err, semindex.ErrIndexNotFound) {
					return &CLIError{Code: ErrCodeNotFound, Message: "no semantic index found", Suggestion: "Run 'zotero-cli index build' first"}
				}
				return &CLIError{Code: ErrCodeIOError, Message: err.Error()}
			}
			if err := existing.CompatibleWith(cfg.UserID, embedModel(cfg)); err != nil {
				return &CLIError{Code: ErrCodeValidation, Message: err.Error()}
			}
			client := zotero.NewClient(cfg.APIKey, cfg.UserID)
			emb := resolveEmbedder(cfg)

			idx, stats, err := updateIndex(cmd.Context(), client, emb, existing, embedBatchSize)
			if err != nil {
				return &CLIError{Code: ErrCodeAPIError, Message: err.Error(), Suggestion: "check that your embedding server is running at " + embedBaseURL(cfg)}
			}
			if err := idx.Save(indexPath()); err != nil {
				return &CLIError{Code: ErrCodeIOError, Message: err.Error()}
			}
			if isJSON() {
				return printJSON(map[string]any{
					"added": stats.Added, "updated": stats.Updated, "removed": stats.Removed,
					"unchanged": stats.Unchanged, "total": stats.Total,
				})
			}
			fmt.Fprintf(os.Stdout, "Updated index: +%d ~%d -%d (unchanged %d, total %d)\n",
				stats.Added, stats.Updated, stats.Removed, stats.Unchanged, stats.Total)
			return nil
		},
	}

	indexCmd.AddCommand(buildCmd, statusCmd, updateCmd)
	return indexCmd
}
