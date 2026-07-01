package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yk0817/zotero-cli/additem"
	"github.com/yk0817/zotero-cli/resolve"
	"github.com/yk0817/zotero-cli/zotero"
)

// recordingTransport captures the last request and returns a canned response.
type recordingTransport struct {
	lastMethod string
	lastBody   []byte
	response   string
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.lastMethod = req.Method
	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		r.lastBody = body
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(r.response)),
		Header:     http.Header{},
	}, nil
}

func newStubClient(response string) (*zotero.Client, *recordingTransport) {
	rt := &recordingTransport{response: response}
	c := zotero.NewClient("test-key", "12345")
	c.HTTPClient = &http.Client{Transport: rt}
	return c, rt
}

func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("expected non-empty tool result")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	return tc.Text
}

// pathStubTransport serves canned JSON responses keyed by URL path.
type pathStubTransport struct {
	responses map[string]string
}

func (s *pathStubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, ok := s.responses[req.URL.Path]
	status := http.StatusOK
	if !ok {
		status = http.StatusNotFound
		body = "not found"
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}, nil
}

func newPathStubClient(responses map[string]string) *zotero.Client {
	c := zotero.NewClient("test-key", "12345")
	c.HTTPClient = &http.Client{Transport: &pathStubTransport{responses: responses}}
	return c
}

// Contract: search results are one "[KEY] Title (Authors, Date)" line per
// item so the LLM can pick up the key for follow-up calls; attachments and
// notes are excluded because they are not papers.
func TestSearchHandlerFormatsResults(t *testing.T) {
	client := newPathStubClient(map[string]string{
		"/users/12345/items": `[
			{"key":"ITEM0001","data":{"itemType":"journalArticle","title":"Attention Is All You Need","creators":[{"lastName":"Vaswani","firstName":"Ashish"}],"date":"2017"}},
			{"key":"ATTACH01","data":{"itemType":"attachment","filename":"a.pdf"}}
		]`,
	})
	handler := searchHandler(client)

	res, _, err := handler(context.Background(), nil, searchInput{Query: "attention"})

	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	text := textOf(t, res)
	if !strings.Contains(text, "ITEM0001") || !strings.Contains(text, "Attention Is All You Need") {
		t.Errorf("expected formatted item line, got %q", text)
	}
	if strings.Contains(text, "ATTACH01") {
		t.Errorf("expected attachments to be filtered out, got %q", text)
	}
}

// Contract: an empty result must say "No results found" explicitly — a blank
// tool result gives the calling LLM no way to tell "no matches" from a
// malfunction. This must hold both when the API returns nothing and when it
// returns only attachments/notes that all get filtered out.
func TestSearchHandlerNoResults(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{name: "api returns empty list", response: `[]`},
		{
			name: "all hits filtered out as attachments and notes",
			response: `[
				{"key":"ATTACH01","data":{"itemType":"attachment","filename":"paper.pdf"}},
				{"key":"NOTE0001","data":{"itemType":"note","note":"a note"}}
			]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newPathStubClient(map[string]string{
				"/users/12345/items": tt.response,
			})
			handler := searchHandler(client)

			res, _, err := handler(context.Background(), nil, searchInput{Query: "nothing"})

			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if textOf(t, res) != "No results found" {
				t.Errorf("expected no-results message, got %q", textOf(t, res))
			}
		})
	}
}

// Contract: zero annotations triggers the sync hint instead of a bare empty
// result — the most common cause is an unsynced Zotero client, and the LLM
// must not conclude "no marks exist" (see MEMORY.md: this misread happened).
func TestAnnotationsHandlerReturnsSyncHintWhenEmpty(t *testing.T) {
	client := newPathStubClient(map[string]string{
		"/users/12345/items/ITEM0001/children": `[]`,
	})
	handler := annotationsHandler(client)

	res, _, err := handler(context.Background(), nil, annotationsInput{ItemKey: "ITEM0001"})

	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if textOf(t, res) != syncHint {
		t.Errorf("expected sync hint, got %q", textOf(t, res))
	}
}

// Contract: zotero_get_context renders every section the bundle contains —
// metadata, abstract, full text, annotations, existing notes, attachments —
// because the tool's purpose is to give the LLM the complete picture of one
// item in a single call.
func TestContextHandlerRendersAllSections(t *testing.T) {
	client := newPathStubClient(map[string]string{
		"/users/12345/items/ITEM0001":          `{"key":"ITEM0001","data":{"itemType":"journalArticle","title":"Test Paper","creators":[{"lastName":"Doe","firstName":"Jane"}],"date":"2024","DOI":"10.1234/x","publicationTitle":"Journal of Tests","abstractNote":"An abstract."}}`,
		"/users/12345/items/ITEM0001/fulltext": `{"content":"full text body","indexedPages":3,"totalPages":3}`,
		"/users/12345/items/ITEM0001/children": `[
			{"key":"NOTE0001","data":{"itemType":"note","note":"<p>existing note</p>"}},
			{"key":"ATTACH01","data":{"itemType":"attachment","filename":"paper.pdf"}}
		]`,
		"/users/12345/items/ATTACH01/children": `[
			{"key":"ANN00001","data":{"itemType":"annotation","annotationType":"highlight","annotationText":"key sentence","annotationSortIndex":"00001|000001|00001","annotationPageLabel":"2","annotationColor":"#ffd400"}}
		]`,
	})
	handler := contextHandler(client)

	res, _, err := handler(context.Background(), nil, itemKeyInput{ItemKey: "ITEM0001"})

	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	text := textOf(t, res)
	for _, want := range []string{
		"Test Paper", "Doe, Jane", "10.1234/x", "Journal of Tests", "An abstract.",
		"full text body", "key sentence", "existing note", "paper.pdf",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
}

// Contract: a missing item is an error, not an empty context — returning a
// blank bundle would let the LLM "summarize" a paper that does not exist.
func TestContextHandlerItemNotFound(t *testing.T) {
	client := newPathStubClient(map[string]string{})
	handler := contextHandler(client)

	_, _, err := handler(context.Background(), nil, itemKeyInput{ItemKey: "MISSING1"})

	if err == nil {
		t.Fatal("expected error for missing item, got nil")
	}
}

// Contract: zotero_add_note creates the note via POST, reports the created
// key back to the caller, and normalizes tags — the ai-generated marker comes
// first exactly once even when the caller also passes it, and blank tags are
// dropped. Tags are asserted on the decoded payload (not raw-string matching)
// so a regression cannot hide behind the note body containing the same word.
func TestAddNoteHandlerCreatesNote(t *testing.T) {
	client, rt := newStubClient(`{"successful":{"0":{"key":"NOTE5678"}},"failed":{}}`)
	handler := addNoteHandler(client)

	res, _, err := handler(context.Background(), nil, addNoteInput{
		ItemKey: "ITEM0001",
		Body:    "summary text",
		Tags:    []string{"summary", "ai-generated", " "},
	})

	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	text := textOf(t, res)
	if !strings.Contains(text, "NOTE5678") {
		t.Errorf("expected result to contain created key, got %q", text)
	}
	if rt.lastMethod != http.MethodPost {
		t.Errorf("expected POST request, got %s", rt.lastMethod)
	}

	var payload []struct {
		Tags []struct {
			Tag string `json:"tag"`
		} `json:"tags"`
	}
	if err := json.Unmarshal(rt.lastBody, &payload); err != nil || len(payload) != 1 {
		t.Fatalf("request body is not a 1-item JSON array: %v (%s)", err, rt.lastBody)
	}
	var got []string
	for _, tag := range payload[0].Tags {
		got = append(got, tag.Tag)
	}
	want := []string{"ai-generated", "summary"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected tags %v, got %v", want, got)
	}
}

// Contract: every handler that takes an item key validates it BEFORE any
// API call. item_key comes from an LLM and is interpolated into the request
// path, so an unvalidated value like "ABCD1234/children" would silently
// rewrite the endpoint instead of failing cleanly.
func TestHandlersRejectInvalidItemKeyWithoutAPICall(t *testing.T) {
	tests := []struct {
		name string
		call func(*zotero.Client) error
	}{
		{name: "add_note", call: func(c *zotero.Client) error {
			_, _, err := addNoteHandler(c)(context.Background(), nil, addNoteInput{ItemKey: "bad", Body: "x"})
			return err
		}},
		{name: "get_annotations", call: func(c *zotero.Client) error {
			_, _, err := annotationsHandler(c)(context.Background(), nil, annotationsInput{ItemKey: "ABCD1234/children"})
			return err
		}},
		{name: "get_context", call: func(c *zotero.Client) error {
			_, _, err := contextHandler(c)(context.Background(), nil, itemKeyInput{ItemKey: ""})
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, rt := newStubClient(`{}`)

			err := tt.call(client)

			if err == nil {
				t.Fatal("expected error for invalid item key, got nil")
			}
			if rt.lastMethod != "" {
				t.Errorf("expected no API call for invalid key, got %s", rt.lastMethod)
			}
		})
	}
}

// Contract: an empty body is rejected locally — creating a blank note in the
// user's library would be silent data pollution.
func TestAddNoteHandlerRejectsEmptyBody(t *testing.T) {
	client, rt := newStubClient(`{}`)
	handler := addNoteHandler(client)

	_, _, err := handler(context.Background(), nil, addNoteInput{ItemKey: "ITEM0001", Body: "  \n "})

	if err == nil {
		t.Fatal("expected error for empty body, got nil")
	}
	if rt.lastMethod != "" {
		t.Errorf("expected no API call for empty body, got %s", rt.lastMethod)
	}
}

// stubResolver returns canned metadata so add_item tests run offline.
type stubResolver struct {
	data  zotero.ItemData
	err   error
	calls int
}

func (s *stubResolver) resolve() (zotero.ItemData, error) { s.calls++; return s.data, s.err }

func (s *stubResolver) ResolveDOI(_ context.Context, _ string) (zotero.ItemData, error) {
	return s.resolve()
}
func (s *stubResolver) ResolveArXiv(_ context.Context, _ string) (zotero.ItemData, error) {
	return s.resolve()
}
func (s *stubResolver) ResolveISBN(_ context.Context, _ string) (zotero.ItemData, error) {
	return s.resolve()
}
func (s *stubResolver) ResolveURL(_ context.Context, _ string) (zotero.ItemData, error) {
	return s.resolve()
}

// methodStubTransport serves one body for GET (the dedup search) and another
// for POST (the create), so add_item's two-step flow can be exercised offline.
type methodStubTransport struct {
	getBody  string
	postBody string
	lastPost []byte
}

func (m *methodStubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := m.getBody
	if req.Method == http.MethodPost {
		if req.Body != nil {
			m.lastPost, _ = io.ReadAll(req.Body)
		}
		body = m.postBody
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}, nil
}

func newMethodStubClient(getBody, postBody string) (*zotero.Client, *methodStubTransport) {
	rt := &methodStubTransport{getBody: getBody, postBody: postBody}
	c := zotero.NewClient("test-key", "12345")
	c.HTTPClient = &http.Client{Transport: rt}
	return c, rt
}

// Contract: zotero_add_item resolves the identifier and creates the item when
// it's new, reporting the created key and sending the resolved title in the
// POST payload (asserted on the decoded body, not raw-string matching).
func TestAddItemHandlerCreatesItem(t *testing.T) {
	client, rt := newMethodStubClient(`[]`, `{"successful":{"0":{"key":"NEW00001"}},"failed":{}}`)
	resolver := &stubResolver{data: zotero.ItemData{ItemType: "journalArticle", Title: "A Paper", DOI: "10.1/x"}}
	handler := addItemHandler(client, resolver)

	res, _, err := handler(context.Background(), nil, addItemInput{DOI: "10.1/x"})

	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if text := textOf(t, res); !strings.Contains(text, "NEW00001") {
		t.Errorf("expected created key in result, got %q", text)
	}
	var payload []struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(rt.lastPost, &payload); err != nil || len(payload) != 1 {
		t.Fatalf("POST body is not a 1-item array: %v (%s)", err, rt.lastPost)
	}
	if payload[0].Title != "A Paper" {
		t.Errorf("created item title = %q, want A Paper", payload[0].Title)
	}
}

// Contract: an identifier already in the library is skipped, not duplicated —
// an autonomous tool must never flood the library with copies.
func TestAddItemHandlerSkipsExisting(t *testing.T) {
	client, rt := newMethodStubClient(`[{"key":"OLD00001","data":{"DOI":"10.1/x"}}]`, `{}`)
	resolver := &stubResolver{data: zotero.ItemData{ItemType: "journalArticle", Title: "A Paper", DOI: "10.1/x"}}
	handler := addItemHandler(client, resolver)

	res, _, err := handler(context.Background(), nil, addItemInput{DOI: "10.1/x"})

	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	text := textOf(t, res)
	if !strings.Contains(text, "OLD00001") || !strings.Contains(text, "Already in library") {
		t.Errorf("expected skip message with existing key, got %q", text)
	}
	if rt.lastPost != nil {
		t.Errorf("expected no create POST when duplicate exists, got body %s", rt.lastPost)
	}
}

// Contract: exactly one identifier is required; zero identifiers is a usage
// error caught before any resolve or API call.
func TestAddItemHandlerRequiresOneIdentifier(t *testing.T) {
	client, _ := newMethodStubClient(`[]`, `{}`)
	resolver := &stubResolver{}

	_, _, err := addItemHandler(client, resolver)(context.Background(), nil, addItemInput{})

	if err == nil {
		t.Fatal("expected error for missing identifier, got nil")
	}
	if resolver.calls != 0 {
		t.Errorf("resolver called %d times, want 0 (no identifier)", resolver.calls)
	}
}

// Contract: an invalid collection key is rejected before resolving, since it is
// interpolated into a request path and comes from the model.
func TestAddItemHandlerValidatesCollectionKey(t *testing.T) {
	client, _ := newMethodStubClient(`[]`, `{}`)
	resolver := &stubResolver{}

	_, _, err := addItemHandler(client, resolver)(context.Background(), nil, addItemInput{DOI: "10.1/x", Collection: "bad"})

	if err == nil {
		t.Fatal("expected error for invalid collection key, got nil")
	}
	if resolver.calls != 0 {
		t.Errorf("resolver called %d times, want 0 (invalid collection)", resolver.calls)
	}
}

// Contract: a resolver failure (e.g. unknown identifier) surfaces as an error,
// never a phantom created item.
func TestAddItemHandlerResolveError(t *testing.T) {
	client, rt := newMethodStubClient(`[]`, `{"successful":{"0":{"key":"X"}},"failed":{}}`)
	resolver := &stubResolver{err: resolve.ErrNotFound}
	handler := addItemHandler(client, resolver)

	_, _, err := handler(context.Background(), nil, addItemInput{DOI: "10.9/nope"})

	if err == nil {
		t.Fatal("expected error for unresolved identifier, got nil")
	}
	if rt.lastPost != nil {
		t.Errorf("expected no create POST on resolve failure, got %s", rt.lastPost)
	}
}

// Contract: the human-readable result distinguishes a created item from a
// skipped duplicate, so the caller (and the model) can tell what happened.
func TestFormatAddItemResult(t *testing.T) {
	created := formatAddItemResult(additem.Result{Action: additem.ActionCreated, ItemKey: "NEW00001", Title: "T", ItemType: "journalArticle", IdentifierKind: "doi", Identifier: "10.1/x"})
	if !strings.Contains(created, "created") || !strings.Contains(created, "NEW00001") {
		t.Errorf("created message = %q", created)
	}
	skipped := formatAddItemResult(additem.Result{Action: additem.ActionSkipped, ItemKey: "OLD00001", Title: "T", IdentifierKind: "doi"})
	if !strings.Contains(skipped, "Already in library") || !strings.Contains(skipped, "OLD00001") {
		t.Errorf("skipped message = %q", skipped)
	}
}
