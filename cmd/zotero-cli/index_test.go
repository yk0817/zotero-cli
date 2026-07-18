package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yk0817/zotero-cli/semindex"
	"github.com/yk0817/zotero-cli/zotero"
)

// fakeLister returns a fixed item list, standing in for the Zotero client.
type fakeLister struct{ items []zotero.Item }

func (f fakeLister) ListAllItems() ([]zotero.Item, error) { return f.items, nil }

// fakeEmbedder returns a deterministic vector per input ([len(text)]) and records
// how many inputs it embedded, to prove batching and skip behaviour.
type fakeEmbedder struct{ embedded int }

func (f *fakeEmbedder) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	f.embedded += len(inputs)
	out := make([][]float32, len(inputs))
	for i, s := range inputs {
		out[i] = []float32{float32(len(s))}
	}
	return out, nil
}

func item(key string, version int, title, abstract string) zotero.Item {
	return zotero.Item{Key: key, Version: version, Data: zotero.ItemData{Title: title, AbstractNote: abstract}}
}

// Contract: buildIndex embeds every item that has text (title/abstract), skips
// items with neither, stamps the header (library, model, dim from the first
// vector), and carries each item's Version for later incremental update.
func TestBuildIndexEmbedsAllWithTextAndSkipsEmpty(t *testing.T) {
	lister := fakeLister{items: []zotero.Item{
		item("AAAA1111", 3, "Title A", "Abstract A"),
		item("BBBB2222", 7, "Title B", ""),
		item("CCCC3333", 1, "", ""), // no text -> skipped
	}}
	emb := &fakeEmbedder{}

	idx, skipped, err := buildIndex(context.Background(), lister, emb, "user9", "model-x", 64)

	if err != nil {
		t.Fatalf("buildIndex: %v", err)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	if emb.embedded != 2 {
		t.Errorf("embedded %d inputs, want 2", emb.embedded)
	}
	if len(idx.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(idx.Entries))
	}
	if idx.Header.LibraryID != "user9" || idx.Header.Model != "model-x" || idx.Header.Dim != 1 {
		t.Errorf("header = %+v", idx.Header)
	}
	if idx.Entries[0].ItemKey != "AAAA1111" || idx.Entries[0].Version != 3 {
		t.Errorf("entry0 = %+v, want key AAAA1111 v3", idx.Entries[0])
	}
}

// Contract: batching splits inputs into batchSize chunks but still embeds every
// item exactly once — a large library must be fully indexed regardless of batch
// size.
func TestBuildIndexBatches(t *testing.T) {
	var items []zotero.Item
	for i := 0; i < 5; i++ {
		items = append(items, item("K"+string(rune('A'+i)), 1, "t", "a"))
	}
	emb := &fakeEmbedder{}

	idx, _, err := buildIndex(context.Background(), fakeLister{items: items}, emb, "u", "m", 2)

	if err != nil {
		t.Fatalf("buildIndex: %v", err)
	}
	if emb.embedded != 5 || len(idx.Entries) != 5 {
		t.Errorf("embedded=%d entries=%d, want 5/5", emb.embedded, len(idx.Entries))
	}
}

// Contract: index status reads the on-disk index and reports its metadata with
// no network call. A missing index reports NOT_FOUND (so the user is told to
// build), not a crash or a generic error.
func TestIndexStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	err := runIndexStatus()
	assertCLIErrorCode(t, err, ErrCodeNotFound)

	idx := &semindex.Index{
		Header:  semindex.Header{SchemaVersion: semindex.SchemaVersion, LibraryID: "u", Model: "m", Dim: 1},
		Entries: []semindex.Entry{{ItemKey: "K", Version: 1, Vector: []float32{1}}},
	}
	if err := idx.Save(filepath.Join(home, ".config", "zotero-cli", "index", "items.gob")); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := runIndexStatus(); err != nil {
		t.Fatalf("status after build: %v", err)
	}
}

// Contract: updateIndex re-embeds only new/changed items, keeps unchanged
// entries without an embed call, drops removed items, and reports accurate
// counts. This is the cost-saving contract of incremental update; a wrong bucket
// means stale vectors or needless API calls.
func TestUpdateIndexIncremental(t *testing.T) {
	existing := &semindex.Index{
		Header: semindex.Header{SchemaVersion: semindex.SchemaVersion, LibraryID: "u", Model: "m", Dim: 1},
		Entries: []semindex.Entry{
			{ItemKey: "KEEP", Version: 1, Title: "keep", Vector: []float32{1}},
			{ItemKey: "CHANGED", Version: 1, Title: "old", Vector: []float32{1}},
			{ItemKey: "GONE", Version: 1, Title: "gone", Vector: []float32{1}},
		},
	}
	lister := fakeLister{items: []zotero.Item{
		item("KEEP", 1, "keep", "a"),
		item("CHANGED", 2, "changed", "a"),
		item("NEW", 1, "new", "a"),
	}}
	emb := &fakeEmbedder{}

	idx, stats, err := updateIndex(context.Background(), lister, emb, existing, 64)

	if err != nil {
		t.Fatalf("updateIndex: %v", err)
	}
	if emb.embedded != 2 {
		t.Errorf("embedded %d, want 2 (CHANGED, NEW only)", emb.embedded)
	}
	if stats.Added != 1 || stats.Updated != 1 || stats.Removed != 1 || stats.Unchanged != 1 {
		t.Errorf("stats = %+v, want added1 updated1 removed1 unchanged1", stats)
	}
	if stats.Total != 3 || len(idx.Entries) != 3 {
		t.Errorf("total=%d entries=%d, want 3/3 (GONE dropped)", stats.Total, len(idx.Entries))
	}
	for _, e := range idx.Entries {
		if e.ItemKey == "CHANGED" && (e.Version != 2 || e.Title != "changed") {
			t.Errorf("CHANGED entry not refreshed: %+v", e)
		}
	}
}
