package main

import (
	"testing"

	"github.com/yk0817/zotero-cli/zotero"
)

// Contract: exactly one identifier flag may be given. Zero flags or more than
// one is a usage error caught before any network call, so the command never
// guesses which identifier the user meant or silently ignores an extra one.
func TestSelectIdentifier(t *testing.T) {
	tests := []struct {
		name      string
		doi       string
		arxiv     string
		isbn      string
		url       string
		wantKind  string
		wantValue string
		wantErr   bool
	}{
		{name: "doi only", doi: "10.1/x", wantKind: kindDOI, wantValue: "10.1/x"},
		{name: "arxiv only", arxiv: "1706.03762", wantKind: kindArxiv, wantValue: "1706.03762"},
		{name: "isbn only", isbn: "9780262033848", wantKind: kindISBN, wantValue: "9780262033848"},
		{name: "url only", url: "https://example.com", wantKind: kindURL, wantValue: "https://example.com"},
		{name: "none set", wantErr: true},
		{name: "two set", doi: "10.1/x", arxiv: "1706.03762", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, value, err := selectIdentifier(tt.doi, tt.arxiv, tt.isbn, tt.url)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got kind=%q value=%q", kind, value)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if kind != tt.wantKind || value != tt.wantValue {
				t.Errorf("got (%q, %q), want (%q, %q)", kind, value, tt.wantKind, tt.wantValue)
			}
		})
	}
}

// Contract: duplicate detection matches a candidate only when its identifier
// field EXACTLY equals the one being added (case-insensitively for DOI,
// version-insensitively for arXiv, separator-insensitively for ISBN). A loose
// quick-search may return near-matches; accepting them would wrongly skip
// creating a distinct item, so the post-filter is strict.
func TestFindDuplicate(t *testing.T) {
	items := []zotero.Item{
		{Key: "AAAA1111", Data: zotero.ItemData{DOI: "10.1038/Nature", URL: "https://arxiv.org/abs/1706.03762v5", ISBN: "9780262033848"}},
		{Key: "BBBB2222", Data: zotero.ItemData{DOI: "10.9/other"}},
	}

	tests := []struct {
		name    string
		kind    string
		value   string
		wantKey string // "" means no duplicate expected
	}{
		{name: "doi case-insensitive match", kind: kindDOI, value: "10.1038/nature", wantKey: "AAAA1111"},
		{name: "doi no match", kind: kindDOI, value: "10.1/absent", wantKey: ""},
		{name: "arxiv version-insensitive match", kind: kindArxiv, value: "1706.03762", wantKey: "AAAA1111"},
		{name: "isbn hyphen-insensitive match", kind: kindISBN, value: "978-0-262-03384-8", wantKey: "AAAA1111"},
		{name: "url exact match", kind: kindURL, value: "https://arxiv.org/abs/1706.03762v5", wantKey: "AAAA1111"},
		{name: "url no match", kind: kindURL, value: "https://example.com/other", wantKey: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findDuplicate(items, tt.kind, tt.value)

			if tt.wantKey == "" {
				if got != nil {
					t.Fatalf("expected no duplicate, got %s", got.Key)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected duplicate %s, got none", tt.wantKey)
			}
			if got.Key != tt.wantKey {
				t.Errorf("got %s, want %s", got.Key, tt.wantKey)
			}
		})
	}
}

// Contract: --if-exists chooses what happens on a duplicate. With no duplicate
// the action is always "create"; with a duplicate, skip/update/duplicate map to
// skip/update/create. An invalid mode is a usage error. This pins the write
// decision that governs library pollution.
func TestDuplicateAction(t *testing.T) {
	dup := &zotero.Item{Key: "DUP00001"}

	tests := []struct {
		name       string
		dup        *zotero.Item
		mode       string
		wantAction string
		wantErr    bool
	}{
		{name: "no dup always creates", dup: nil, mode: ifExistsSkip, wantAction: actionCreate},
		{name: "dup + skip", dup: dup, mode: ifExistsSkip, wantAction: actionSkip},
		{name: "dup + update", dup: dup, mode: ifExistsUpdate, wantAction: actionUpdate},
		{name: "dup + duplicate", dup: dup, mode: ifExistsDuplicate, wantAction: actionCreate},
		{name: "invalid mode", dup: dup, mode: "bogus", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, err := duplicateAction(tt.dup, tt.mode)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for mode %q", tt.mode)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if action != tt.wantAction {
				t.Errorf("got action %q, want %q", action, tt.wantAction)
			}
		})
	}
}

// Contract: user-supplied --tags and --collection are merged into the resolved
// metadata before it is written, so `add --tags a,b --collection KEY` files and
// tags the new item in one shot. Resolved tags (if any) are preserved.
func TestApplyAddOptions(t *testing.T) {
	data := zotero.ItemData{
		ItemType: "journalArticle",
		Title:    "T",
		Tags:     []zotero.Tag{{Tag: "resolved"}},
	}

	got := applyAddOptions(data, []string{"to-read", "ml"}, "ABCD1234")

	if len(got.Collections) != 1 || got.Collections[0] != "ABCD1234" {
		t.Errorf("collections = %v, want [ABCD1234]", got.Collections)
	}
	tagSet := map[string]bool{}
	for _, tag := range got.Tags {
		tagSet[tag.Tag] = true
	}
	for _, want := range []string{"resolved", "to-read", "ml"} {
		if !tagSet[want] {
			t.Errorf("expected tag %q in %v", want, got.Tags)
		}
	}
}

// Contract: applyAddOptions does not duplicate a tag the resolver already set,
// so passing --tags with a value already present does not create two identical
// tags (which would clutter the item and downstream tag filters).
func TestApplyAddOptionsDeduplicatesTags(t *testing.T) {
	data := zotero.ItemData{Tags: []zotero.Tag{{Tag: "ml"}}}

	got := applyAddOptions(data, []string{"ml"}, "")

	count := 0
	for _, tag := range got.Tags {
		if tag.Tag == "ml" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("tag \"ml\" appears %d times, want 1", count)
	}
}
