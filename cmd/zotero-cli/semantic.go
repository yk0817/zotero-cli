package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/yk0817/zotero-cli/embed"
	"github.com/yk0817/zotero-cli/semindex"
	"github.com/yk0817/zotero-cli/zotero"
)

// Embedding backend defaults. The default base URL is Ollama's
// OpenAI-compatible endpoint, so "local by default" needs no configuration; the
// model default (nomic-embed-text) is a small local embedding model. The API key
// is read only from the environment (never config), matching secret-handling.
const (
	defaultEmbedBaseURL = "http://localhost:11434/v1"
	defaultEmbedModel   = "nomic-embed-text"
	embedAPIKeyEnv      = "ZOTERO_EMBED_API_KEY"
)

func embedBaseURL(cfg *Config) string {
	if cfg.EmbedBaseURL != "" {
		return cfg.EmbedBaseURL
	}
	return defaultEmbedBaseURL
}

func embedModel(cfg *Config) string {
	if cfg.EmbedModel != "" {
		return cfg.EmbedModel
	}
	return defaultEmbedModel
}

// resolveEmbedder builds an HTTP embedder from config (base URL + model) and the
// environment (API key only).
func resolveEmbedder(cfg *Config) *embed.HTTPEmbedder {
	return embed.New(embedBaseURL(cfg), embedModel(cfg), os.Getenv(embedAPIKeyEnv))
}

// indexPath is the on-disk location of the vector index, under the same config
// directory as config.json.
func indexPath() string {
	return filepath.Join(configDir(), "index", "items.gob")
}

// embedText is the text that represents an item for embedding: title and
// abstract joined by a newline. Empty only when both are empty.
func embedText(item zotero.Item) string {
	title := strings.TrimSpace(item.Data.Title)
	abstract := strings.TrimSpace(item.Data.AbstractNote)
	return strings.TrimSpace(title + "\n" + abstract)
}

// searchSemantic embeds the query and returns index entries ranked by cosine
// similarity, truncated to limit (limit <= 0 returns all).
func searchSemantic(ctx context.Context, idx *semindex.Index, emb embed.Embedder, query string, limit int) ([]semindex.Result, error) {
	vecs, err := emb.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("embedder returned %d vectors for the query, want 1", len(vecs))
	}
	return idx.Search(vecs[0], limit), nil
}

// runSemanticSearch loads the index, validates it against the active config,
// embeds the query, and prints ranked results. A missing index is NOT_FOUND
// (with a build hint), and an empty index prints an explicit message rather than
// nothing so an LLM/user does not read silence as "no matches exist".
func runSemanticSearch(ctx context.Context, query string, limit int) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	idx, err := semindex.Load(indexPath())
	if err != nil {
		if errors.Is(err, semindex.ErrIndexNotFound) {
			return &CLIError{Code: ErrCodeNotFound, Message: "no semantic index found", Suggestion: "Run 'zotero-cli index build' first"}
		}
		return &CLIError{Code: ErrCodeIOError, Message: err.Error()}
	}
	if err := idx.CompatibleWith(cfg.UserID, embedModel(cfg)); err != nil {
		return &CLIError{Code: ErrCodeValidation, Message: err.Error()}
	}

	results, err := searchSemantic(ctx, idx, resolveEmbedder(cfg), query, limit)
	if err != nil {
		return &CLIError{Code: ErrCodeAPIError, Message: err.Error(), Suggestion: "check that your embedding server is running at " + embedBaseURL(cfg)}
	}

	if isJSON() {
		if results == nil {
			results = []semindex.Result{}
		}
		return printJSON(results)
	}
	if len(idx.Entries) == 0 {
		fmt.Println("No results — the index is empty, run 'zotero-cli index build'")
		return nil
	}
	printSemanticResults(results)
	return nil
}

// printSemanticResults renders ranked semantic hits as a table.
func printSemanticResults(results []semindex.Result) {
	if len(results) == 0 {
		fmt.Println("No results found")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "RANK\tSCORE\tKEY\tTITLE")
	fmt.Fprintln(w, "----\t-----\t---\t-----")
	for i, r := range results {
		fmt.Fprintf(w, "%d\t%.3f\t%s\t%s\n", i+1, r.Score, r.ItemKey, zotero.Truncate(r.Title, 60))
	}
	w.Flush()
}
