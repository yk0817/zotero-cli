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
	status     int
	response   string
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.lastMethod = req.Method
	r.lastPath = req.URL.Path
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

func TestCreateNoteFailedResponse(t *testing.T) {
	client, _ := newRecordingClient(http.StatusOK, `{"successful":{},"failed":{"0":{"code":400,"message":"bad"}}}`)

	_, err := client.CreateNote("ITEM0001", "body", nil)

	if err == nil {
		t.Fatal("expected error for failed response, got nil")
	}
}

func TestCreateNoteAPIError(t *testing.T) {
	client, _ := newRecordingClient(http.StatusForbidden, "forbidden")

	_, err := client.CreateNote("ITEM0001", "body", nil)

	if err == nil {
		t.Fatal("expected error for HTTP 403, got nil")
	}
}
