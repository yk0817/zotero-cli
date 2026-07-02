package resolve

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yk0817/zotero-cli/zotero"
)

const openLibraryBooksURL = "https://openlibrary.org/api/books?format=json&jscmd=data&bibkeys=ISBN:"

type openLibraryBook struct {
	Title       string            `json:"title"`
	Subtitle    string            `json:"subtitle"`
	Authors     []openLibraryName `json:"authors"`
	PublishDate string            `json:"publish_date"`
	Publishers  []openLibraryName `json:"publishers"`
	URL         string            `json:"url"`
}

type openLibraryName struct {
	Name string `json:"name"`
}

// ResolveISBN looks a book up by ISBN via OpenLibrary and maps it to Zotero
// book item metadata.
func (c *Client) ResolveISBN(ctx context.Context, isbn string) (zotero.ItemData, error) {
	normalized := NormalizeISBN(isbn)
	if normalized == "" {
		return zotero.ItemData{}, fmt.Errorf("empty ISBN")
	}

	body, err := c.get(ctx, openLibraryBooksURL+normalized, "application/json")
	if err != nil {
		return zotero.ItemData{}, err
	}

	var results map[string]openLibraryBook
	if err := json.Unmarshal(body, &results); err != nil {
		return zotero.ItemData{}, fmt.Errorf("failed to parse OpenLibrary response: %w", err)
	}

	book, ok := results["ISBN:"+normalized]
	if !ok || (book.Title == "" && len(book.Authors) == 0) {
		return zotero.ItemData{}, fmt.Errorf("%w: ISBN %s", ErrNotFound, normalized)
	}

	title := collapseWS(book.Title)
	if sub := collapseWS(book.Subtitle); sub != "" {
		title = title + ": " + sub
	}

	return zotero.ItemData{
		ItemType:  "book",
		Title:     title,
		Creators:  openLibraryCreators(book.Authors),
		Date:      collapseWS(book.PublishDate),
		Publisher: firstOpenLibraryName(book.Publishers),
		ISBN:      normalized,
		URL:       book.URL,
	}, nil
}

// NormalizeISBN strips separators, keeping the 10/13 digits and a trailing X
// check character (uppercased), so hyphenated and bare ISBNs compare equal.
func NormalizeISBN(isbn string) string {
	var b strings.Builder
	for _, r := range isbn {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == 'x' || r == 'X':
			b.WriteRune('X')
		}
	}
	return b.String()
}

func openLibraryCreators(authors []openLibraryName) []zotero.Creator {
	creators := make([]zotero.Creator, 0, len(authors))
	for _, a := range authors {
		if c := personCreator(a.Name); c.LastName != "" || c.Name != "" {
			creators = append(creators, c)
		}
	}
	return creators
}

func firstOpenLibraryName(names []openLibraryName) string {
	for _, n := range names {
		if s := collapseWS(n.Name); s != "" {
			return s
		}
	}
	return ""
}
