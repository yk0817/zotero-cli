package additem

import (
	"testing"

	"github.com/yk0817/zotero-cli/zotero"
)

// stubLibrary records calls and returns canned data, so Run's orchestration is
// tested without a real Zotero API.
type stubLibrary struct {
	searchResults []zotero.Item
	searchErr     error
	createdKey    string
	createErr     error
	updateErr     error

	searchQueries []string
	created       []zotero.ItemData
	updatedKey    string
	updatedFields map[string]interface{}
}

func (s *stubLibrary) FullTextSearch(query, tag string, limit int) ([]zotero.Item, error) {
	s.searchQueries = append(s.searchQueries, query)
	return s.searchResults, s.searchErr
}

func (s *stubLibrary) CreateItem(data zotero.ItemData) (string, error) {
	if s.createErr != nil {
		return "", s.createErr
	}
	s.created = append(s.created, data)
	return s.createdKey, nil
}

func (s *stubLibrary) UpdateItem(itemKey string, version int, fields map[string]interface{}) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	s.updatedKey = itemKey
	s.updatedFields = fields
	return nil
}

// Contract: exactly one identifier may be given. Zero or more than one is an
// error caught before any network call, so add never guesses which identifier
// the caller meant or silently ignores an extra one.
func TestSelectIdentifier(t *testing.T) {
	tests := []struct {
		name                  string
		doi, arxiv, isbn, url string
		wantKind, wantValue   string
		wantErr               bool
	}{
		{name: "doi", doi: "10.1/x", wantKind: KindDOI, wantValue: "10.1/x"},
		{name: "arxiv", arxiv: "1706.03762", wantKind: KindArxiv, wantValue: "1706.03762"},
		{name: "isbn", isbn: "9780262033848", wantKind: KindISBN, wantValue: "9780262033848"},
		{name: "url", url: "https://example.com", wantKind: KindURL, wantValue: "https://example.com"},
		{name: "none", wantErr: true},
		{name: "two", doi: "10.1/x", arxiv: "1706.03762", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, value, err := SelectIdentifier(tt.doi, tt.arxiv, tt.isbn, tt.url)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got (%q, %q)", kind, value)
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

// Contract: ValidateIfExists accepts only the three defined modes.
func TestValidateIfExists(t *testing.T) {
	for _, mode := range []string{IfExistsSkip, IfExistsUpdate, IfExistsDuplicate} {
		if err := ValidateIfExists(mode); err != nil {
			t.Errorf("mode %q should be valid: %v", mode, err)
		}
	}
	if err := ValidateIfExists("bogus"); err == nil {
		t.Error("expected error for invalid mode")
	}
}

// Contract: duplicate detection matches a candidate only when its identifier
// field EXACTLY equals the one being added (case-insensitively for DOI,
// version-insensitively for arXiv, separator-insensitively for ISBN). A loose
// quick-search may return near-matches; accepting them would wrongly skip
// creating a distinct item.
func TestFindDuplicate(t *testing.T) {
	items := []zotero.Item{
		{Key: "AAAA1111", Data: zotero.ItemData{DOI: "10.1038/Nature", URL: "https://arxiv.org/abs/1706.03762v5", ISBN: "9780262033848"}},
		{Key: "BBBB2222", Data: zotero.ItemData{DOI: "10.9/other"}},
	}

	tests := []struct {
		name    string
		kind    string
		value   string
		wantKey string
	}{
		{name: "doi case-insensitive", kind: KindDOI, value: "10.1038/nature", wantKey: "AAAA1111"},
		{name: "doi no match", kind: KindDOI, value: "10.1/absent", wantKey: ""},
		{name: "arxiv version-insensitive", kind: KindArxiv, value: "1706.03762", wantKey: "AAAA1111"},
		{name: "isbn hyphen-insensitive", kind: KindISBN, value: "978-0-262-03384-8", wantKey: "AAAA1111"},
		{name: "url exact", kind: KindURL, value: "https://arxiv.org/abs/1706.03762v5", wantKey: "AAAA1111"},
		{name: "url no match", kind: KindURL, value: "https://example.com/other", wantKey: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindDuplicate(items, tt.kind, tt.value)

			if tt.wantKey == "" {
				if got != nil {
					t.Fatalf("expected no duplicate, got %s", got.Key)
				}
				return
			}
			if got == nil || got.Key != tt.wantKey {
				t.Fatalf("got %v, want %s", got, tt.wantKey)
			}
		})
	}
}

// Contract: URL dedup uses the resolved/canonical URL (og:url stored on the
// item), not the raw string the caller passed, so a page whose canonical URL
// differs from the typed one is still recognized as already-present. Other
// kinds dedupe on the identifier value itself.
func TestDedupValue(t *testing.T) {
	data := zotero.ItemData{URL: "https://example.com/canonical"}

	if got := DedupValue(KindURL, "https://example.com/typed?utm=1", data); got != "https://example.com/canonical" {
		t.Errorf("url DedupValue = %q, want the resolved canonical URL", got)
	}
	if got := DedupValue(KindDOI, "10.1/x", data); got != "10.1/x" {
		t.Errorf("doi DedupValue = %q, want the identifier value", got)
	}
}

// Contract: user tags and collection are merged into the resolved metadata
// (tags deduplicated against resolver-set tags), and the input is not mutated.
func TestApplyOptions(t *testing.T) {
	data := zotero.ItemData{Tags: []zotero.Tag{{Tag: "resolved"}}}

	got := ApplyOptions(data, []string{"to-read", "resolved"}, "ABCD1234")

	if len(got.Collections) != 1 || got.Collections[0] != "ABCD1234" {
		t.Errorf("collections = %v, want [ABCD1234]", got.Collections)
	}
	count := map[string]int{}
	for _, tag := range got.Tags {
		count[tag.Tag]++
	}
	if count["resolved"] != 1 {
		t.Errorf("tag \"resolved\" appears %d times, want 1", count["resolved"])
	}
	if count["to-read"] != 1 {
		t.Errorf("tag \"to-read\" missing: %v", got.Tags)
	}
	if len(data.Tags) != 1 {
		t.Errorf("input data.Tags mutated: %v", data.Tags)
	}
}

// Contract: with no existing item, Run creates a new one and reports the key,
// searching by the identifier first.
func TestRunCreatesWhenNoDuplicate(t *testing.T) {
	lib := &stubLibrary{createdKey: "NEW00001"}
	data := zotero.ItemData{ItemType: "journalArticle", Title: "T", DOI: "10.1/x"}

	result, err := Run(lib, data, KindDOI, "10.1/x", IfExistsSkip)

	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if result.Action != ActionCreated || result.ItemKey != "NEW00001" {
		t.Errorf("result = %+v, want created NEW00001", result)
	}
	if result.Duplicate {
		t.Error("Duplicate should be false when none found")
	}
	if len(lib.created) != 1 {
		t.Errorf("expected 1 CreateItem call, got %d", len(lib.created))
	}
	if len(lib.searchQueries) != 1 || lib.searchQueries[0] != "10.1/x" {
		t.Errorf("search queries = %v, want [10.1/x]", lib.searchQueries)
	}
}

// Contract: --if-exists skip (default) reports the existing item and creates
// nothing when a duplicate is present — the guard against re-adding papers.
func TestRunSkipsExistingByDefault(t *testing.T) {
	lib := &stubLibrary{
		searchResults: []zotero.Item{{Key: "OLD00001", Data: zotero.ItemData{DOI: "10.1/x"}}},
		createdKey:    "SHOULD_NOT_BE_USED",
	}
	data := zotero.ItemData{ItemType: "journalArticle", Title: "T", DOI: "10.1/x"}

	result, err := Run(lib, data, KindDOI, "10.1/x", IfExistsSkip)

	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if result.Action != ActionSkipped || result.ItemKey != "OLD00001" || !result.Duplicate {
		t.Errorf("result = %+v, want skipped OLD00001 duplicate=true", result)
	}
	if len(lib.created) != 0 {
		t.Errorf("expected no CreateItem call, got %d", len(lib.created))
	}
}

// Contract: --if-exists update patches the existing item's bibliographic fields
// with its version, and never sends tags/collections so the item's existing
// tags and collection memberships are preserved.
func TestRunUpdatesExisting(t *testing.T) {
	lib := &stubLibrary{
		searchResults: []zotero.Item{{Key: "OLD00001", Version: 7, Data: zotero.ItemData{DOI: "10.1/x"}}},
	}
	data := zotero.ItemData{ItemType: "journalArticle", Title: "New Title", DOI: "10.1/x", Tags: []zotero.Tag{{Tag: "x"}}, Collections: []string{"COLL1234"}}

	result, err := Run(lib, data, KindDOI, "10.1/x", IfExistsUpdate)

	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if result.Action != ActionUpdated || result.ItemKey != "OLD00001" {
		t.Errorf("result = %+v, want updated OLD00001", result)
	}
	if lib.updatedKey != "OLD00001" {
		t.Errorf("updated key = %q, want OLD00001", lib.updatedKey)
	}
	if lib.updatedFields["title"] != "New Title" {
		t.Errorf("update should patch title, got %v", lib.updatedFields["title"])
	}
	if _, has := lib.updatedFields["tags"]; has {
		t.Error("update must not send tags (would clobber existing tags)")
	}
	if _, has := lib.updatedFields["collections"]; has {
		t.Error("update must not send collections (would clobber memberships)")
	}
}

// Contract: --if-exists duplicate creates a second item even though a duplicate
// exists, and reports Duplicate=true so the caller knows a known duplicate was
// intentionally added.
func TestRunDuplicateForcesCreate(t *testing.T) {
	lib := &stubLibrary{
		searchResults: []zotero.Item{{Key: "OLD00001", Data: zotero.ItemData{DOI: "10.1/x"}}},
		createdKey:    "NEW00002",
	}
	data := zotero.ItemData{ItemType: "journalArticle", DOI: "10.1/x"}

	result, err := Run(lib, data, KindDOI, "10.1/x", IfExistsDuplicate)

	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if result.Action != ActionCreated || result.ItemKey != "NEW00002" || !result.Duplicate {
		t.Errorf("result = %+v, want created NEW00002 duplicate=true", result)
	}
}

// Contract: a URL add dedupes on the resolved canonical URL, so the search and
// the match both use og:url rather than the raw input.
func TestRunUsesCanonicalURLForDedup(t *testing.T) {
	lib := &stubLibrary{
		searchResults: []zotero.Item{{Key: "OLD00001", Data: zotero.ItemData{URL: "https://example.com/canonical"}}},
	}
	data := zotero.ItemData{ItemType: "webpage", URL: "https://example.com/canonical"}

	result, err := Run(lib, data, KindURL, "https://example.com/typed?utm=1", IfExistsSkip)

	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if result.Action != ActionSkipped {
		t.Errorf("expected skip on canonical-URL match, got %+v", result)
	}
	if lib.searchQueries[0] != "https://example.com/canonical" {
		t.Errorf("search query = %q, want the canonical URL", lib.searchQueries[0])
	}
}

// Contract: an invalid --if-exists mode is rejected by Run (via
// duplicateAction) rather than silently defaulting to a write, so a typo cannot
// cause an unintended create/update.
func TestRunRejectsInvalidMode(t *testing.T) {
	lib := &stubLibrary{createdKey: "SHOULD_NOT_BE_USED"}
	data := zotero.ItemData{ItemType: "journalArticle", DOI: "10.1/x"}

	_, err := Run(lib, data, KindDOI, "10.1/x", "bogus")

	if err == nil {
		t.Fatal("expected error for invalid mode, got nil")
	}
	if len(lib.created) != 0 {
		t.Errorf("expected no CreateItem call on invalid mode, got %d", len(lib.created))
	}
}

// Contract: a CreateItem failure is surfaced as an error, never a phantom
// success.
func TestRunSurfacesCreateFailure(t *testing.T) {
	lib := &stubLibrary{createErr: errCreate}
	data := zotero.ItemData{ItemType: "journalArticle", DOI: "10.1/x"}

	_, err := Run(lib, data, KindDOI, "10.1/x", IfExistsSkip)

	if err == nil {
		t.Fatal("expected error from CreateItem failure, got nil")
	}
}

var errCreate = errStub("create failed")

type errStub string

func (e errStub) Error() string { return string(e) }
