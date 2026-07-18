package zotero

import (
	"fmt"
	"strings"
	"testing"
)

// itemsPageJSON builds a JSON array of n minimal items ([{"key":...,"version":1},...]).
func itemsPageJSON(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf(`{"key":"K%07d","version":1}`, i)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// Contract: ListAllItems follows pagination and returns every item, not just the
// first page. The Zotero API caps a page at 100; a library larger than one page
// must not be silently truncated (same class as the GetChildren pagination bug),
// or the semantic index would be built over a partial library.
func TestListAllItemsPaginates(t *testing.T) {
	// Page 1 is a full 100 items (more pages expected); page 2 is a short 30
	// (signals the end).
	client, qt := newQueryClient("[]")
	qt.queue = []string{itemsPageJSON(100), itemsPageJSON(30)}

	items, err := client.ListAllItems()

	if err != nil {
		t.Fatalf("ListAllItems: %v", err)
	}
	if len(items) != 130 {
		t.Errorf("got %d items, want 130 (100+30 across two pages)", len(items))
	}
	if len(qt.urls) != 2 {
		t.Fatalf("made %d requests, want 2", len(qt.urls))
	}
	if got := qt.urls[0].Query().Get("start"); got != "0" {
		t.Errorf("page 1 start = %q, want 0", got)
	}
	if got := qt.urls[1].Query().Get("start"); got != "100" {
		t.Errorf("page 2 start = %q, want 100", got)
	}
	if got := qt.urls[0].Query().Get("itemType"); got != "-attachment || note" {
		t.Errorf("itemType = %q, want -attachment || note", got)
	}
}
