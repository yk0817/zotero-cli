package resolve

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const openLibraryCLRS = `{
  "ISBN:9780262033848": {
    "title": "Introduction to Algorithms",
    "subtitle": "Third Edition",
    "authors": [
      {"name": "Thomas H. Cormen"},
      {"name": "Charles E. Leiserson"}
    ],
    "publish_date": "2009",
    "publishers": [{"name": "MIT Press"}],
    "url": "https://openlibrary.org/books/OL24215500M/Introduction_to_Algorithms"
  }
}`

// Contract: ResolveISBN maps an OpenLibrary book record to a Zotero book item —
// title (with subtitle), authors split into first/last, publish date,
// publisher, and the ISBN — so scanning an ISBN produces a complete book
// record.
func TestResolveISBNParsesOpenLibrary(t *testing.T) {
	client, _ := newTestClient(map[string]stubResponse{
		"openlibrary.org/api/books": {status: 200, body: openLibraryCLRS},
	})

	data, err := client.ResolveISBN(context.Background(), "9780262033848")

	if err != nil {
		t.Fatalf("ResolveISBN returned error: %v", err)
	}
	if data.ItemType != "book" {
		t.Errorf("itemType = %q, want book", data.ItemType)
	}
	if data.Title != "Introduction to Algorithms: Third Edition" {
		t.Errorf("title = %q, want title with subtitle", data.Title)
	}
	if data.Date != "2009" {
		t.Errorf("date = %q, want 2009", data.Date)
	}
	if data.Publisher != "MIT Press" {
		t.Errorf("publisher = %q, want MIT Press", data.Publisher)
	}
	if data.ISBN != "9780262033848" {
		t.Errorf("ISBN = %q", data.ISBN)
	}
	if len(data.Creators) != 2 {
		t.Fatalf("expected 2 creators, got %d (%v)", len(data.Creators), data.Creators)
	}
	if data.Creators[0].FirstName != "Thomas H." || data.Creators[0].LastName != "Cormen" {
		t.Errorf("creator[0] = %+v, want Thomas H. Cormen", data.Creators[0])
	}
}

// Contract: a hyphenated/spaced ISBN is normalized to bare digits before it is
// used as the OpenLibrary bibkey and stored, so "978-0-262-03384-8" and
// "9780262033848" resolve to the same book and dedupe against each other.
func TestResolveISBNNormalizesInput(t *testing.T) {
	client, st := newTestClient(map[string]stubResponse{
		"openlibrary.org/api/books": {status: 200, body: openLibraryCLRS},
	})

	data, err := client.ResolveISBN(context.Background(), "978-0-262-03384-8")

	if err != nil {
		t.Fatalf("ResolveISBN returned error: %v", err)
	}
	if data.ISBN != "9780262033848" {
		t.Errorf("stored ISBN = %q, want normalized digits", data.ISBN)
	}
	got := st.requests[0].String()
	if !strings.Contains(got, "bibkeys=ISBN:9780262033848") {
		t.Errorf("request URL = %q, want normalized bibkey", got)
	}
}

// Contract: OpenLibrary answers an unknown ISBN with an empty JSON object, not
// an HTTP error, so an empty result maps to ErrNotFound rather than creating a
// blank book.
func TestResolveISBNNotFound(t *testing.T) {
	client, _ := newTestClient(map[string]stubResponse{
		"openlibrary.org/api/books": {status: 200, body: `{}`},
	})

	_, err := client.ResolveISBN(context.Background(), "9999999999999")

	if err == nil {
		t.Fatal("expected error for unknown ISBN, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// Contract: the lookup uses OpenLibrary's Books API in data mode, so the
// response actually carries the structured fields (authors, publishers) the
// mapping relies on.
func TestResolveISBNRequestsBooksAPI(t *testing.T) {
	client, st := newTestClient(map[string]stubResponse{
		"openlibrary.org/api/books": {status: 200, body: openLibraryCLRS},
	})

	_, err := client.ResolveISBN(context.Background(), "9780262033848")

	if err != nil {
		t.Fatalf("ResolveISBN returned error: %v", err)
	}
	got := st.requests[0].String()
	if !strings.Contains(got, "openlibrary.org/api/books") || !strings.Contains(got, "jscmd=data") {
		t.Errorf("request URL = %q, want the OpenLibrary books data API", got)
	}
}
