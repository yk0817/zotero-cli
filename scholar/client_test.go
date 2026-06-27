package scholar

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// stubResponse is one canned reply (status + body) for a path.
type stubResponse struct {
	status int
	body   string
}

// scholarStub serves canned responses keyed by request path. A path may hold a
// queue of responses (consumed in order, the last one repeating) so a single
// path can return successive pages; it records every request URL and the last
// request header so tests can assert query shape and the API-key header.
type scholarStub struct {
	routes     map[string][]stubResponse
	requests   []*url.URL
	lastHeader http.Header
}

func (s *scholarStub) RoundTrip(req *http.Request) (*http.Response, error) {
	// The citation network is read-only; a write would be a bug.
	if req.Method != http.MethodGet {
		return &http.Response{
			StatusCode: http.StatusMethodNotAllowed,
			Body:       io.NopCloser(strings.NewReader("mutating request in read-only client")),
			Header:     http.Header{},
		}, nil
	}
	s.requests = append(s.requests, req.URL)
	s.lastHeader = req.Header.Clone()

	q := s.routes[req.URL.Path]
	r := stubResponse{status: http.StatusNotFound, body: `{}`}
	switch {
	case len(q) == 1:
		r = q[0]
	case len(q) > 1:
		r = q[0]
		s.routes[req.URL.Path] = q[1:]
	}
	return &http.Response{
		StatusCode: r.status,
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Header:     http.Header{},
	}, nil
}

func newStubClient(routes map[string][]stubResponse) (*Client, *scholarStub) {
	stub := &scholarStub{routes: routes}
	c := &Client{
		BaseURL:    "http://s2.test",
		HTTPClient: &http.Client{Transport: stub},
	}
	return c, stub
}

func ok(body string) []stubResponse { return []stubResponse{{status: http.StatusOK, body: body}} }
func notFound() []stubResponse      { return []stubResponse{{status: http.StatusNotFound, body: `{}`}} }
func paperID(id string) string      { return fmt.Sprintf(`{"paperId":%q}`, id) }
func searchHit(id string) string    { return fmt.Sprintf(`{"data":[{"paperId":%q,"title":"X"}]}`, id) }

// Contract: a DOI resolves to a canonical paperId via the /paper/DOI:<doi>
// lookup. This fixes the resolution path the CLI relies on for the common case
// (Zotero items usually carry a DOI); a wrong path would make every citation
// lookup fail.
func TestResolvePaperIDViaDOI(t *testing.T) {
	doi := "10.18653/v1/N18-3011"
	routes := map[string][]stubResponse{
		"/graph/v1/paper/DOI:" + doi: ok(paperID("S2PAPER1")),
	}
	client, stub := newStubClient(routes)

	id, err := client.ResolvePaperID(context.Background(), doi, "", "")

	if err != nil {
		t.Fatalf("ResolvePaperID returned error: %v", err)
	}
	if id != "S2PAPER1" {
		t.Errorf("expected paperId S2PAPER1, got %q", id)
	}
	if got := stub.requests[0].Query().Get("fields"); got != "paperId" {
		t.Errorf("expected fields=paperId on lookup, got %q", got)
	}
}

// Contract: when the DOI is absent/unmatched, resolution falls back to the
// arXiv ID. A missing DOI must not abort resolution — many preprints have only
// an arXiv ID.
func TestResolvePaperIDFallsBackToArxiv(t *testing.T) {
	routes := map[string][]stubResponse{
		"/graph/v1/paper/DOI:10.0/x":       notFound(),
		"/graph/v1/paper/ARXIV:2301.00001": ok(paperID("S2ARXIV")),
	}
	client, _ := newStubClient(routes)

	id, err := client.ResolvePaperID(context.Background(), "10.0/x", "2301.00001", "")

	if err != nil {
		t.Fatalf("ResolvePaperID returned error: %v", err)
	}
	if id != "S2ARXIV" {
		t.Errorf("expected paperId S2ARXIV, got %q", id)
	}
}

// Contract: with neither DOI nor arXiv matching, resolution falls back to a
// title search and takes the top hit. This is the last-resort path for items
// that carry only a title.
func TestResolvePaperIDFallsBackToTitleSearch(t *testing.T) {
	routes := map[string][]stubResponse{
		"/graph/v1/paper/search": ok(searchHit("S2SEARCH")),
	}
	client, stub := newStubClient(routes)

	id, err := client.ResolvePaperID(context.Background(), "", "", "Attention Is All You Need")

	if err != nil {
		t.Fatalf("ResolvePaperID returned error: %v", err)
	}
	if id != "S2SEARCH" {
		t.Errorf("expected paperId S2SEARCH, got %q", id)
	}
	if got := stub.requests[0].Query().Get("query"); got != "Attention Is All You Need" {
		t.Errorf("expected the title forwarded as query, got %q", got)
	}
}

// Contract: when no identifier matches, ResolvePaperID returns ErrPaperNotFound
// (not a generic error), so the CLI can report NOT_FOUND rather than API_ERROR
// and distinguish "couldn't identify the paper" from "the API failed".
func TestResolvePaperIDNotFound(t *testing.T) {
	routes := map[string][]stubResponse{
		"/graph/v1/paper/DOI:10.0/x": notFound(),
		"/graph/v1/paper/search":     ok(`{"data":[]}`),
	}
	client, _ := newStubClient(routes)

	_, err := client.ResolvePaperID(context.Background(), "10.0/x", "", "no such paper")

	if !errors.Is(err, ErrPaperNotFound) {
		t.Fatalf("expected ErrPaperNotFound, got %v", err)
	}
}

// Contract: a non-404 error during resolution aborts immediately and propagates
// the status code — a 500 on the DOI lookup must never be silently treated as
// "not found" (which would wrongly trigger a title-search fallback or NOT_FOUND).
func TestResolvePaperIDPropagatesServerError(t *testing.T) {
	routes := map[string][]stubResponse{
		"/graph/v1/paper/DOI:10.0/x": {{status: http.StatusInternalServerError, body: "boom"}},
	}
	client, stub := newStubClient(routes)

	_, err := client.ResolvePaperID(context.Background(), "10.0/x", "", "fallback title")

	if err == nil || errors.Is(err, ErrPaperNotFound) {
		t.Fatalf("expected a propagated server error, got %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected status 500 in error, got %v", err)
	}
	// The fallback title search must NOT have run after a hard error.
	for _, u := range stub.requests {
		if strings.Contains(u.Path, "/search") {
			t.Errorf("title search ran despite a server error during DOI lookup")
		}
	}
}

func refRow(field, id, title string, year, cites int) string {
	return fmt.Sprintf(`{%q:{"paperId":%q,"title":%q,"year":%d,"citationCount":%d,"authors":[{"name":"A"}],"externalIds":{"DOI":"d","CorpusId":42}}}`,
		field, id, title, year, cites)
}

// Contract: References parses the citedPaper wrapper into PaperRef, decoding
// externalIds while ignoring the numeric CorpusId key (which would break a
// map[string]string decode). A parse regression here would silently drop the
// backward citation list.
func TestReferencesParses(t *testing.T) {
	body := fmt.Sprintf(`{"data":[%s,%s]}`,
		refRow("citedPaper", "R1", "First Ref", 2017, 100),
		refRow("citedPaper", "R2", "Second Ref", 2019, 5))
	routes := map[string][]stubResponse{
		"/graph/v1/paper/PID/references": ok(body),
	}
	client, stub := newStubClient(routes)

	refs, err := client.References(context.Background(), "PID", 25)

	if err != nil {
		t.Fatalf("References returned error: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 references, got %d", len(refs))
	}
	if refs[0].PaperID != "R1" || refs[0].Title != "First Ref" || refs[0].Year != 2017 || refs[0].CitationCount != 100 {
		t.Errorf("unexpected first reference: %+v", refs[0])
	}
	if refs[0].ExternalIDs.DOI != "d" {
		t.Errorf("expected externalIds.DOI=d, got %q", refs[0].ExternalIDs.DOI)
	}
	if got := stub.requests[0].Query().Get("fields"); got != paperFields {
		t.Errorf("expected fields=%q, got %q", paperFields, got)
	}
}

// Contract: a citing-paper row whose paper is null (the neighbour is not in the
// S2 graph) is skipped, not emitted as an empty PaperRef — otherwise the output
// table would show phantom blank rows.
func TestCitationsSkipsNullPapers(t *testing.T) {
	body := fmt.Sprintf(`{"data":[%s,{"citingPaper":null}]}`,
		refRow("citingPaper", "C1", "Citing", 2020, 3))
	routes := map[string][]stubResponse{
		"/graph/v1/paper/PID/citations": ok(body),
	}
	client, _ := newStubClient(routes)

	cites, err := client.Citations(context.Background(), "PID", 25)

	if err != nil {
		t.Fatalf("Citations returned error: %v", err)
	}
	if len(cites) != 1 || cites[0].PaperID != "C1" {
		t.Errorf("expected [C1] with the null paper skipped, got %+v", cites)
	}
}

// Contract: References follows the offset/next cursor instead of trusting one
// page, so a backward list longer than a page is not silently truncated. The
// second request must resume at the offset the API's "next" cursor named.
func TestReferencesFollowsPagination(t *testing.T) {
	page1 := fmt.Sprintf(`{"next":2,"data":[%s,%s]}`,
		refRow("citedPaper", "R1", "One", 2017, 1),
		refRow("citedPaper", "R2", "Two", 2018, 2))
	page2 := fmt.Sprintf(`{"data":[%s]}`,
		refRow("citedPaper", "R3", "Three", 2019, 3))
	routes := map[string][]stubResponse{
		"/graph/v1/paper/PID/references": {
			{status: http.StatusOK, body: page1},
			{status: http.StatusOK, body: page2},
		},
	}
	client, stub := newStubClient(routes)

	refs, err := client.References(context.Background(), "PID", 25)

	if err != nil {
		t.Fatalf("References returned error: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("expected 3 references across 2 pages, got %d", len(refs))
	}
	if len(stub.requests) != 2 {
		t.Fatalf("expected 2 paginated requests, got %d", len(stub.requests))
	}
	if got := stub.requests[1].Query().Get("offset"); got != "2" {
		t.Errorf("expected second request offset=2 (from next cursor), got %q", got)
	}
}

// Contract: the limit is honoured — once enough neighbours are collected, no
// further page is fetched. This bounds API usage and the size of the table the
// caller renders.
func TestReferencesRespectsLimit(t *testing.T) {
	body := fmt.Sprintf(`{"next":3,"data":[%s,%s,%s]}`,
		refRow("citedPaper", "R1", "One", 2017, 1),
		refRow("citedPaper", "R2", "Two", 2018, 2),
		refRow("citedPaper", "R3", "Three", 2019, 3))
	routes := map[string][]stubResponse{
		"/graph/v1/paper/PID/references": ok(body),
	}
	client, stub := newStubClient(routes)

	refs, err := client.References(context.Background(), "PID", 2)

	if err != nil {
		t.Fatalf("References returned error: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected exactly 2 references (limit), got %d", len(refs))
	}
	if len(stub.requests) != 1 {
		t.Errorf("expected a single request when limit fits in one page, got %d", len(stub.requests))
	}
	if got := stub.requests[0].Query().Get("limit"); got != "2" {
		t.Errorf("expected page limit=2, got %q", got)
	}
}

// Contract: an empty network returns a non-nil empty slice and no error — "no
// references" is a normal state the CLI renders as data:[], never null and
// never an error.
func TestReferencesEmptyIsNotError(t *testing.T) {
	routes := map[string][]stubResponse{
		"/graph/v1/paper/PID/references": ok(`{"data":[]}`),
	}
	client, _ := newStubClient(routes)

	refs, err := client.References(context.Background(), "PID", 25)

	if err != nil {
		t.Fatalf("References returned error: %v", err)
	}
	if refs == nil {
		t.Fatal("expected a non-nil empty slice, got nil")
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 references, got %d", len(refs))
	}
}

// Contract: a non-200 from the references endpoint surfaces as an error that
// names the status code — an API failure must never be reported as "no
// references", which would let an agent conclude the paper cites nothing.
func TestReferencesAPIError(t *testing.T) {
	routes := map[string][]stubResponse{
		"/graph/v1/paper/PID/references": {{status: http.StatusTooManyRequests, body: "rate limited"}},
	}
	client, _ := newStubClient(routes)

	_, err := client.References(context.Background(), "PID", 25)

	if err == nil {
		t.Fatal("expected error for HTTP 429, got nil")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("expected status 429 in error, got %v", err)
	}
}

// Contract: malformed JSON is an error, never silently an empty list — a
// truncated body must not be mistaken for "no references".
func TestReferencesInvalidJSON(t *testing.T) {
	routes := map[string][]stubResponse{
		"/graph/v1/paper/PID/references": ok("not json"),
	}
	client, _ := newStubClient(routes)

	_, err := client.References(context.Background(), "PID", 25)

	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// Contract: the API key, when set, is sent as the x-api-key header (raising the
// rate limit); when unset, no such header is sent so the keyless low-rate tier
// still works. The key must travel only in the header.
func TestAPIKeyHeaderBehaviour(t *testing.T) {
	routes := map[string][]stubResponse{
		"/graph/v1/paper/PID/references": ok(`{"data":[]}`),
	}

	withKey, stubWith := newStubClient(routes)
	withKey.APIKey = "secret-key"
	if _, err := withKey.References(context.Background(), "PID", 25); err != nil {
		t.Fatalf("References returned error: %v", err)
	}
	if got := stubWith.lastHeader.Get("x-api-key"); got != "secret-key" {
		t.Errorf("expected x-api-key header to carry the key, got %q", got)
	}

	noKey, stubNo := newStubClient(map[string][]stubResponse{
		"/graph/v1/paper/PID/references": ok(`{"data":[]}`),
	})
	if _, err := noKey.References(context.Background(), "PID", 25); err != nil {
		t.Fatalf("References returned error: %v", err)
	}
	if stubNo.lastHeader.Get("x-api-key") != "" {
		t.Errorf("expected no x-api-key header when key is unset, got %q", stubNo.lastHeader.Get("x-api-key"))
	}
}
