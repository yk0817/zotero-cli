package main

import (
	"context"
	"testing"

	"github.com/yk0817/zotero-cli/semindex"
	"github.com/yk0817/zotero-cli/zotero"
)

// Contract: embed settings fall back to the documented local defaults when the
// config leaves them blank, and the config values win when set. This is what
// makes "local by default, remote when configured" true.
func TestEmbedDefaults(t *testing.T) {
	if got := embedBaseURL(&Config{}); got != defaultEmbedBaseURL {
		t.Errorf("base url default = %q, want %q", got, defaultEmbedBaseURL)
	}
	if got := embedModel(&Config{}); got != defaultEmbedModel {
		t.Errorf("model default = %q, want %q", got, defaultEmbedModel)
	}
	if got := embedBaseURL(&Config{EmbedBaseURL: "http://x/v1"}); got != "http://x/v1" {
		t.Errorf("config base url should win, got %q", got)
	}
	if got := embedModel(&Config{EmbedModel: "custom"}); got != "custom" {
		t.Errorf("config model should win, got %q", got)
	}
}

// Contract: the API key is read only from the environment, never from Config —
// so a key can never be persisted to config.json on disk.
func TestResolveEmbedderKeyFromEnvOnly(t *testing.T) {
	t.Setenv(embedAPIKeyEnv, "sk-secret")

	e := resolveEmbedder(&Config{UserID: "u", EmbedModel: "m"})

	if e.APIKey != "sk-secret" {
		t.Errorf("APIKey = %q, want it from env", e.APIKey)
	}
	if e.Model != "m" {
		t.Errorf("Model = %q, want m", e.Model)
	}
}

// Contract: embedText joins title and abstract into the text that represents an
// item, and returns empty only when both are empty (so build can skip items with
// nothing to embed rather than sending blank input).
func TestEmbedText(t *testing.T) {
	full := zotero.Item{Data: zotero.ItemData{Title: "Attention", AbstractNote: "We propose..."}}
	if got := embedText(full); got != "Attention\nWe propose..." {
		t.Errorf("embedText = %q", got)
	}

	titleOnly := zotero.Item{Data: zotero.ItemData{Title: "Only title"}}
	if got := embedText(titleOnly); got != "Only title" {
		t.Errorf("title-only embedText = %q, want %q", got, "Only title")
	}

	empty := zotero.Item{Data: zotero.ItemData{}}
	if got := embedText(empty); got != "" {
		t.Errorf("empty embedText = %q, want \"\"", got)
	}
}

// queryEmbedder returns a fixed query vector regardless of input, so the test
// controls which index entry ranks first.
type queryEmbedder struct{ vec []float32 }

func (q queryEmbedder) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	out := make([][]float32, len(inputs))
	for i := range inputs {
		out[i] = q.vec
	}
	return out, nil
}

// Contract: searchSemantic embeds the query once and returns index entries
// ranked by similarity, truncated to limit. This is the user-visible semantic
// ranking; wrong order or wrong count defeats the feature.
func TestSearchSemanticRanks(t *testing.T) {
	idx := &semindex.Index{Entries: []semindex.Entry{
		{ItemKey: "FAR", Title: "far", Vector: []float32{0, 1}},
		{ItemKey: "NEAR", Title: "near", Vector: []float32{1, 0}},
	}}
	emb := queryEmbedder{vec: []float32{1, 0}}

	got, err := searchSemantic(context.Background(), idx, emb, "anything", 10)

	if err != nil {
		t.Fatalf("searchSemantic: %v", err)
	}
	if len(got) != 2 || got[0].ItemKey != "NEAR" {
		t.Fatalf("ranking = %+v, want NEAR first", got)
	}
}
