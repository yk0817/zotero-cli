package zotero

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// recordingTransport captures the last write request and returns a canned response.
type recordingTransport struct {
	lastMethod string
	lastPath   string
	lastBody   []byte
	lastHeader http.Header
	status     int
	response   string
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.lastMethod = req.Method
	r.lastPath = req.URL.Path
	r.lastHeader = req.Header.Clone()
	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		r.lastBody = body
	}
	return &http.Response{
		StatusCode: r.status,
		Body:       io.NopCloser(strings.NewReader(r.response)),
		Header:     http.Header{},
	}, nil
}

func newRecordingClient(status int, response string) (*Client, *recordingTransport) {
	rt := &recordingTransport{status: status, response: response}
	c := NewClient("test-key", "12345")
	c.HTTPClient = &http.Client{Transport: rt}
	return c, rt
}

const createNoteSuccess = `{"successful":{"0":{"key":"NOTE5678"}},"failed":{}}`

func decodeNotePayload(t *testing.T, body []byte) map[string]interface{} {
	t.Helper()
	var payload []map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("request body is not a JSON array: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("expected 1 item in payload, got %d", len(payload))
	}
	return payload[0]
}

// Contract: CreateNote POSTs to /items and returns the key Zotero assigned
// to the created note, so callers can reference it afterwards.
func TestCreateNoteReturnsKey(t *testing.T) {
	client, rt := newRecordingClient(http.StatusOK, createNoteSuccess)

	key, err := client.CreateNote("ITEM0001", "hello", []string{"ai-generated"})

	if err != nil {
		t.Fatalf("CreateNote returned error: %v", err)
	}
	if key != "NOTE5678" {
		t.Errorf("expected key NOTE5678, got %s", key)
	}
	if rt.lastMethod != http.MethodPost || rt.lastPath != "/users/12345/items" {
		t.Errorf("expected POST /users/12345/items, got %s %s", rt.lastMethod, rt.lastPath)
	}
}

// Contract: a plain-text body is converted to note HTML (newlines become
// paragraphs) before being sent, because Zotero notes are HTML documents
// and raw text would lose its line structure.
func TestCreateNoteWrapsPlainTextAsHTML(t *testing.T) {
	client, rt := newRecordingClient(http.StatusOK, createNoteSuccess)

	_, err := client.CreateNote("ITEM0001", "line1\nline2", nil)

	if err != nil {
		t.Fatalf("CreateNote returned error: %v", err)
	}
	note := decodeNotePayload(t, rt.lastBody)
	want := "<p>line1</p>\n<p>line2</p>"
	if note["note"] != want {
		t.Errorf("expected note %q, got %q", want, note["note"])
	}
	if note["parentItem"] != "ITEM0001" {
		t.Errorf("expected parentItem ITEM0001, got %v", note["parentItem"])
	}
}

// Contract: a body that is already HTML is sent as-is — double-wrapping it
// in <p> tags would corrupt the markup the caller built deliberately.
func TestCreateNoteKeepsHTMLAsIs(t *testing.T) {
	client, rt := newRecordingClient(http.StatusOK, createNoteSuccess)

	_, err := client.CreateNote("ITEM0001", "<h1>Title</h1>", nil)

	if err != nil {
		t.Fatalf("CreateNote returned error: %v", err)
	}
	note := decodeNotePayload(t, rt.lastBody)
	if note["note"] != "<h1>Title</h1>" {
		t.Errorf("expected HTML kept as-is, got %q", note["note"])
	}
}

// Contract: tags passed to CreateNote are attached to the note item so
// downstream filters (e.g. searching notes by tag) can find it.
func TestCreateNoteSendsTags(t *testing.T) {
	client, rt := newRecordingClient(http.StatusOK, createNoteSuccess)

	_, err := client.CreateNote("ITEM0001", "body", []string{"ai-generated", "summary"})

	if err != nil {
		t.Fatalf("CreateNote returned error: %v", err)
	}
	note := decodeNotePayload(t, rt.lastBody)
	tags, ok := note["tags"].([]interface{})
	if !ok || len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %v", note["tags"])
	}
}

// Contract: when Zotero accepts the request but reports the item in
// "failed" (e.g. invalid parent key), CreateNote must surface an error
// instead of returning an empty key as success.
func TestCreateNoteFailedResponse(t *testing.T) {
	client, _ := newRecordingClient(http.StatusOK, `{"successful":{},"failed":{"0":{"code":400,"message":"bad"}}}`)

	_, err := client.CreateNote("ITEM0001", "body", nil)

	if err == nil {
		t.Fatal("expected error for failed response, got nil")
	}
}

// Contract: non-2xx responses (auth failure, rate limit) become errors —
// a write must never be reported as successful on an HTTP error.
func TestCreateNoteAPIError(t *testing.T) {
	client, _ := newRecordingClient(http.StatusForbidden, "forbidden")

	_, err := client.CreateNote("ITEM0001", "body", nil)

	if err == nil {
		t.Fatal("expected error for HTTP 403, got nil")
	}
}

// Contract: plain text is HTML-escaped before wrapping, so characters that
// are meaningful in HTML (&, <, >) survive verbatim in the saved note
// instead of being parsed as markup and swallowed by Zotero's renderer.
func TestPlainTextToNoteHTMLEscapes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "html special chars escaped",
			input: "loss i<j and AT&T",
			want:  "<p>loss i&lt;j and AT&amp;T</p>",
		},
		{
			name:  "leading < that is not a tag is treated as plain text",
			input: "<- this arrow is not HTML",
			want:  "<p>&lt;- this arrow is not HTML</p>",
		},
		{
			name:  "real tag passes through unchanged",
			input: "<p>already html</p>",
			want:  "<p>already html</p>",
		},
		{
			name:  "html comment passes through unchanged",
			input: "<!-- comment -->",
			want:  "<!-- comment -->",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PlainTextToNoteHTML(tt.input)
			if got != tt.want {
				t.Errorf("PlainTextToNoteHTML(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
