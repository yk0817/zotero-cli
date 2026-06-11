package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

func TestSearchHandlerNoResults(t *testing.T) {
	client := newPathStubClient(map[string]string{
		"/users/12345/items": `[]`,
	})
	handler := searchHandler(client)

	res, _, err := handler(context.Background(), nil, searchInput{Query: "nothing"})

	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if textOf(t, res) != "No results found" {
		t.Errorf("expected no-results message, got %q", textOf(t, res))
	}
}

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

func TestContextHandlerItemNotFound(t *testing.T) {
	client := newPathStubClient(map[string]string{})
	handler := contextHandler(client)

	_, _, err := handler(context.Background(), nil, itemKeyInput{ItemKey: "MISSING1"})

	if err == nil {
		t.Fatal("expected error for missing item, got nil")
	}
}

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
	body := string(rt.lastBody)
	if strings.Count(body, "ai-generated") != 1 {
		t.Errorf("expected ai-generated tag exactly once, got body %s", body)
	}
	if !strings.Contains(body, "summary") {
		t.Errorf("expected extra tag in body, got %s", body)
	}
}

func TestAddNoteHandlerRejectsInvalidKey(t *testing.T) {
	client, rt := newStubClient(`{}`)
	handler := addNoteHandler(client)

	_, _, err := handler(context.Background(), nil, addNoteInput{ItemKey: "bad", Body: "x"})

	if err == nil {
		t.Fatal("expected error for invalid item key, got nil")
	}
	if rt.lastMethod != "" {
		t.Errorf("expected no API call for invalid key, got %s", rt.lastMethod)
	}
}

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
