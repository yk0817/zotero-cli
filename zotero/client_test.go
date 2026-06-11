package zotero

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// stubTransport serves canned JSON responses keyed by URL path.
type stubTransport struct {
	responses map[string]string
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// annotation retrieval must stay read-only
	if req.Method != http.MethodGet {
		return &http.Response{
			StatusCode: http.StatusMethodNotAllowed,
			Body:       io.NopCloser(strings.NewReader("mutating request in read-only test")),
			Header:     http.Header{},
		}, nil
	}
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

func newStubClient(responses map[string]string) *Client {
	c := NewClient("test-key", "12345")
	c.HTTPClient = &http.Client{Transport: &stubTransport{responses: responses}}
	return c
}

func annotationJSON(key, annType, text, comment, sortIndex string) string {
	return fmt.Sprintf(`{"key":%q,"data":{"itemType":"annotation","annotationType":%q,"annotationText":%q,"annotationComment":%q,"annotationSortIndex":%q,"annotationPageLabel":"9","annotationColor":"#aaaaaa"}}`,
		key, annType, text, comment, sortIndex)
}

func TestGetAnnotations(t *testing.T) {
	// Arrange: top-level item ITEM0001 with two attachments; annotations out of reading order
	responses := map[string]string{
		"/users/12345/items/ITEM0001/children": `[
			{"key":"NOTE0001","data":{"itemType":"note","note":"a note"}},
			{"key":"ATTACH01","data":{"itemType":"attachment","filename":"a.pdf"}},
			{"key":"ATTACH02","data":{"itemType":"attachment","filename":"b.pdf"}}
		]`,
		"/users/12345/items/ATTACH01/children": "[" +
			annotationJSON("ANN00002", "highlight", "second", "", "00008|000412|00574") + "," +
			annotationJSON("ANN00001", "note", "", "first comment", "00002|000100|00100") +
			"]",
		"/users/12345/items/ATTACH02/children": "[" +
			annotationJSON("ANN00003", "ink", "", "", "00010|000001|00001") +
			"]",
	}
	client := newStubClient(responses)

	// Act
	anns, err := client.GetAnnotations("ITEM0001")

	// Assert
	if err != nil {
		t.Fatalf("GetAnnotations returned error: %v", err)
	}
	if len(anns) != 3 {
		t.Fatalf("expected 3 annotations, got %d", len(anns))
	}
	wantOrder := []string{"ANN00001", "ANN00002", "ANN00003"}
	for i, want := range wantOrder {
		if anns[i].Key != want {
			t.Errorf("position %d: expected %s, got %s", i, want, anns[i].Key)
		}
	}
}

func TestGetAnnotationsEmptyWhenNoAttachments(t *testing.T) {
	responses := map[string]string{
		"/users/12345/items/ITEM0001/children": `[]`,
	}
	client := newStubClient(responses)

	anns, err := client.GetAnnotations("ITEM0001")

	if err != nil {
		t.Fatalf("GetAnnotations returned error: %v", err)
	}
	if len(anns) != 0 {
		t.Errorf("expected 0 annotations, got %d", len(anns))
	}
}

func TestGetAnnotationsDirectAttachmentKey(t *testing.T) {
	// Passing an attachment key directly: its children are annotations
	responses := map[string]string{
		"/users/12345/items/ATTACH01/children": "[" +
			annotationJSON("ANN00001", "highlight", "text", "", "00001|000001|00001") +
			"]",
	}
	client := newStubClient(responses)

	anns, err := client.GetAnnotations("ATTACH01")

	if err != nil {
		t.Fatalf("GetAnnotations returned error: %v", err)
	}
	if len(anns) != 1 || anns[0].Key != "ANN00001" {
		t.Errorf("expected [ANN00001], got %v", anns)
	}
}

func TestFormatAnnotation(t *testing.T) {
	tests := []struct {
		name string
		item Item
		want string
	}{
		{
			name: "highlight with comment",
			item: Item{Data: ItemData{
				AnnotationType:      "highlight",
				AnnotationText:      "selected text",
				AnnotationComment:   "my comment",
				AnnotationColor:     "#ff0000",
				AnnotationPageLabel: "9",
			}},
			want: "[highlight p.9 #ff0000] \"selected text\"\n  ↳ comment: my comment",
		},
		{
			name: "underline without comment",
			item: Item{Data: ItemData{
				AnnotationType:      "underline",
				AnnotationText:      "underlined",
				AnnotationColor:     "#aaaaaa",
				AnnotationPageLabel: "3",
			}},
			want: "[underline p.3 #aaaaaa] \"underlined\"",
		},
		{
			name: "note annotation",
			item: Item{Data: ItemData{
				AnnotationType:      "note",
				AnnotationComment:   "standalone note",
				AnnotationPageLabel: "5",
			}},
			want: "[note p.5] standalone note",
		},
		{
			name: "ink without text",
			item: Item{Data: ItemData{
				AnnotationType:      "ink",
				AnnotationPageLabel: "12",
			}},
			want: "[ink p.12 — no text]",
		},
		{
			name: "image without page label",
			item: Item{Data: ItemData{
				AnnotationType: "image",
			}},
			want: "[image — no text]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatAnnotation(tt.item)
			if got != tt.want {
				t.Errorf("FormatAnnotation() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFilterAnnotations(t *testing.T) {
	anns := []Item{
		{Key: "A1", Data: ItemData{AnnotationType: "highlight", AnnotationColor: "#FF0000"}},
		{Key: "A2", Data: ItemData{AnnotationType: "note", AnnotationColor: "#aaaaaa"}},
		{Key: "A3", Data: ItemData{AnnotationType: "highlight", AnnotationColor: "#aaaaaa"}},
	}

	tests := []struct {
		name     string
		color    string
		annType  string
		wantKeys []string
	}{
		{name: "no filter returns all", wantKeys: []string{"A1", "A2", "A3"}},
		{name: "color filter is case-insensitive", color: "#ff0000", wantKeys: []string{"A1"}},
		{name: "type filter", annType: "highlight", wantKeys: []string{"A1", "A3"}},
		{name: "combined filter", color: "#aaaaaa", annType: "highlight", wantKeys: []string{"A3"}},
		{name: "no match returns empty", color: "#00ff00", wantKeys: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterAnnotations(anns, tt.color, tt.annType)
			if len(got) != len(tt.wantKeys) {
				t.Fatalf("expected %d annotations, got %d", len(tt.wantKeys), len(got))
			}
			for i, want := range tt.wantKeys {
				if got[i].Key != want {
					t.Errorf("position %d: expected %s, got %s", i, want, got[i].Key)
				}
			}
		})
	}
}

func TestGetContextIncludesAnnotations(t *testing.T) {
	responses := map[string]string{
		"/users/12345/items/ITEM0001":          `{"key":"ITEM0001","data":{"itemType":"journalArticle","title":"Test Paper"}}`,
		"/users/12345/items/ITEM0001/fulltext": `{"content":"full text","indexedPages":10,"totalPages":10}`,
		"/users/12345/items/ITEM0001/children": `[
			{"key":"ATTACH01","data":{"itemType":"attachment","filename":"a.pdf"}}
		]`,
		"/users/12345/items/ATTACH01/children": "[" +
			annotationJSON("ANN00001", "highlight", "text", "", "00001|000001|00001") +
			"]",
	}
	client := newStubClient(responses)

	bundle, err := client.GetContext("ITEM0001")

	if err != nil {
		t.Fatalf("GetContext returned error: %v", err)
	}
	if len(bundle.Annotations) != 1 || bundle.Annotations[0].Key != "ANN00001" {
		t.Errorf("expected annotations [ANN00001], got %v", bundle.Annotations)
	}
}
