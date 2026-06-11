package zotero

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// queryRecordingTransport records every GET request URL and returns canned
// bodies: entries from queue first (one per request), then response forever.
type queryRecordingTransport struct {
	urls     []*url.URL
	lastURL  *url.URL
	queue    []string
	response string
	status   int
}

func (q *queryRecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	q.lastURL = req.URL
	q.urls = append(q.urls, req.URL)
	body := q.response
	if len(q.queue) > 0 {
		body = q.queue[0]
		q.queue = q.queue[1:]
	}
	status := q.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}, nil
}

func newQueryClient(response string) (*Client, *queryRecordingTransport) {
	qt := &queryRecordingTransport{response: response}
	c := NewClient("test-key", "12345")
	c.HTTPClient = &http.Client{Transport: qt}
	return c, qt
}

// Contract: SearchItems forwards query and tag as q/tag params to /items
// and parses the result — this fixes the request shape the Zotero API expects.
func TestSearchItemsSendsQueryAndTag(t *testing.T) {
	client, qt := newQueryClient(`[{"key":"ITEM0001","data":{"itemType":"journalArticle","title":"Attention"}}]`)

	items, err := client.SearchItems("attention", "nlp")

	if err != nil {
		t.Fatalf("SearchItems returned error: %v", err)
	}
	if len(items) != 1 || items[0].Key != "ITEM0001" {
		t.Fatalf("expected [ITEM0001], got %v", items)
	}
	query := qt.lastURL.Query()
	if query.Get("q") != "attention" {
		t.Errorf("expected q=attention, got %q", query.Get("q"))
	}
	if query.Get("tag") != "nlp" {
		t.Errorf("expected tag=nlp, got %q", query.Get("tag"))
	}
	if qt.lastURL.Path != "/users/12345/items" {
		t.Errorf("expected path /users/12345/items, got %s", qt.lastURL.Path)
	}
}

// Contract: empty filters are omitted entirely — sending q= or tag= as empty
// strings would change the API's matching behavior.
func TestSearchItemsOmitsEmptyParams(t *testing.T) {
	client, qt := newQueryClient(`[]`)

	_, err := client.SearchItems("", "")

	if err != nil {
		t.Fatalf("SearchItems returned error: %v", err)
	}
	query := qt.lastURL.Query()
	if query.Has("q") || query.Has("tag") {
		t.Errorf("expected q and tag to be omitted, got %s", qt.lastURL.RawQuery)
	}
}

// Contract: HTTP errors and malformed JSON surface as errors — a 500 or a
// truncated body must never be reported as "no results".
func TestSearchItemsAPIError(t *testing.T) {
	client, qt := newQueryClient("internal error")
	qt.status = http.StatusInternalServerError

	_, err := client.SearchItems("x", "")

	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

func TestSearchItemsInvalidJSON(t *testing.T) {
	client, _ := newQueryClient("not json")

	_, err := client.SearchItems("x", "")

	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// Contract: GetItem addresses /items/<key> and unmarshals the single-item
// response (not a list).
func TestGetItem(t *testing.T) {
	client, qt := newQueryClient(`{"key":"ITEM0001","data":{"itemType":"journalArticle","title":"Test Paper"}}`)

	item, err := client.GetItem("ITEM0001")

	if err != nil {
		t.Fatalf("GetItem returned error: %v", err)
	}
	if item.Key != "ITEM0001" || item.Data.Title != "Test Paper" {
		t.Errorf("unexpected item: %+v", item)
	}
	if qt.lastURL.Path != "/users/12345/items/ITEM0001" {
		t.Errorf("expected item path, got %s", qt.lastURL.Path)
	}
}

// Contract: ListCollections parses the collection list shape (key + data.name).
func TestListCollections(t *testing.T) {
	client, _ := newQueryClient(`[{"key":"COLL0001","data":{"name":"Papers"}}]`)

	colls, err := client.ListCollections()

	if err != nil {
		t.Fatalf("ListCollections returned error: %v", err)
	}
	if len(colls) != 1 || colls[0].Data.Name != "Papers" {
		t.Errorf("unexpected collections: %+v", colls)
	}
}

// Contract: GetBibTeX requests the bibtex export format; the default page
// size is 25 and --all raises it to 100 (asserted in the next test).
func TestGetBibTeX(t *testing.T) {
	client, qt := newQueryClient("@article{vaswani2017, title={Attention}}")

	bib, err := client.GetBibTeX("attention", "", false)

	if err != nil {
		t.Fatalf("GetBibTeX returned error: %v", err)
	}
	if !strings.Contains(bib, "@article") {
		t.Errorf("expected BibTeX entry, got %q", bib)
	}
	if qt.lastURL.Query().Get("limit") != "25" {
		t.Errorf("expected limit=25 without --all, got %q", qt.lastURL.Query().Get("limit"))
	}
}

// Contract: a collection key reroutes the export to that collection's items.
func TestGetBibTeXCollectionAndAll(t *testing.T) {
	client, qt := newQueryClient("@article{x}")

	_, err := client.GetBibTeX("", "COLL0001", true)

	if err != nil {
		t.Fatalf("GetBibTeX returned error: %v", err)
	}
	if qt.lastURL.Path != "/users/12345/collections/COLL0001/items" {
		t.Errorf("expected collection path, got %s", qt.lastURL.Path)
	}
	if qt.lastURL.Query().Get("limit") != "100" {
		t.Errorf("expected limit=100 with all=true, got %q", qt.lastURL.Query().Get("limit"))
	}
}

// Contract: GetFullText reads the /fulltext endpoint and exposes the page
// counts callers display alongside the content.
func TestGetFullText(t *testing.T) {
	client, qt := newQueryClient(`{"content":"body text","indexedPages":5,"totalPages":7}`)

	ft, err := client.GetFullText("ITEM0001")

	if err != nil {
		t.Fatalf("GetFullText returned error: %v", err)
	}
	if ft.Content != "body text" || ft.TotalPages != 7 {
		t.Errorf("unexpected fulltext response: %+v", ft)
	}
	if qt.lastURL.Path != "/users/12345/items/ITEM0001/fulltext" {
		t.Errorf("expected fulltext path, got %s", qt.lastURL.Path)
	}
}

// Contract: full-text search differs from plain search only by
// qmode=everything — that param is what makes Zotero search inside PDFs.
func TestFullTextSearchSetsQmodeEverything(t *testing.T) {
	client, qt := newQueryClient(`[]`)

	_, err := client.FullTextSearch("transformer", "", 0)

	if err != nil {
		t.Fatalf("FullTextSearch returned error: %v", err)
	}
	query := qt.lastURL.Query()
	if query.Get("qmode") != "everything" {
		t.Errorf("expected qmode=everything, got %q", query.Get("qmode"))
	}
	if query.Get("limit") != "25" {
		t.Errorf("expected default limit 25, got %q", query.Get("limit"))
	}
}

// Contract: a collection key scopes the listing to that collection's items.
func TestListItems(t *testing.T) {
	client, qt := newQueryClient(`[{"key":"ITEM0001"}]`)

	items, err := client.ListItems("COLL0001", 10)

	if err != nil {
		t.Fatalf("ListItems returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if qt.lastURL.Path != "/users/12345/collections/COLL0001/items" {
		t.Errorf("expected collection items path, got %s", qt.lastURL.Path)
	}
}

// Contract: limit<=0 falls back to 100 instead of sending limit=0 (which the
// API would read as "default 25").
func TestListItemsByTagDefaultsLimit(t *testing.T) {
	client, qt := newQueryClient(`[]`)

	_, err := client.ListItemsByTag("nlp", 0)

	if err != nil {
		t.Fatalf("ListItemsByTag returned error: %v", err)
	}
	query := qt.lastURL.Query()
	if query.Get("tag") != "nlp" {
		t.Errorf("expected tag=nlp, got %q", query.Get("tag"))
	}
	if query.Get("limit") != "100" {
		t.Errorf("expected default limit 100, got %q", query.Get("limit"))
	}
}

// Contract: GetChildren follows pagination instead of trusting one page.
// The Zotero API caps a page at 100 items (and defaults to 25 without an
// explicit limit) — a paper with more than one page of annotations must not
// have the rest silently dropped.
func TestGetChildrenPaginates(t *testing.T) {
	fullPage := make([]string, 100)
	for i := range fullPage {
		fullPage[i] = fmt.Sprintf(`{"key":"CHILD%03d"}`, i)
	}
	secondPage := `[{"key":"CHILD100"},{"key":"CHILD101"}]`

	client, qt := newQueryClient("")
	qt.queue = []string{"[" + strings.Join(fullPage, ",") + "]", secondPage}

	children, err := client.GetChildren("ITEM0001")

	if err != nil {
		t.Fatalf("GetChildren returned error: %v", err)
	}
	if len(children) != 102 {
		t.Fatalf("expected 102 children across 2 pages, got %d", len(children))
	}
	if len(qt.urls) != 2 {
		t.Fatalf("expected 2 paginated requests, got %d", len(qt.urls))
	}
	if got := qt.urls[1].Query().Get("start"); got != "100" {
		t.Errorf("expected second request to resume at start=100, got %q", got)
	}
}

// Contract: a result set smaller than one page needs exactly one request —
// pagination must not add a useless second round trip.
func TestGetChildrenSinglePage(t *testing.T) {
	client, qt := newQueryClient(`[{"key":"CHILD001"}]`)

	children, err := client.GetChildren("ITEM0001")

	if err != nil {
		t.Fatalf("GetChildren returned error: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
	if len(qt.urls) != 1 {
		t.Errorf("expected exactly 1 request for a partial page, got %d", len(qt.urls))
	}
}

// Contract: batch lookup joins keys with commas and sizes the limit to the
// key count so every requested item fits in one response.
func TestGetItemsByKeys(t *testing.T) {
	client, qt := newQueryClient(`[{"key":"AAAA0001"},{"key":"BBBB0002"}]`)

	items, err := client.GetItemsByKeys([]string{"AAAA0001", "BBBB0002"})

	if err != nil {
		t.Fatalf("GetItemsByKeys returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	query := qt.lastURL.Query()
	if query.Get("itemKey") != "AAAA0001,BBBB0002" {
		t.Errorf("expected itemKey param joined with comma, got %q", query.Get("itemKey"))
	}
	if query.Get("limit") != "2" {
		t.Errorf("expected limit=2, got %q", query.Get("limit"))
	}
}
