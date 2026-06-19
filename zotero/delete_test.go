package zotero

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// methodTransport returns a different canned response per HTTP method, so a
// two-step flow (GET the item, then DELETE it) can be exercised in one test.
// It records the DELETE request to assert on its method, path, and the
// optimistic-concurrency version header.
type methodTransport struct {
	getStatus int
	getBody   string
	delStatus int
	delBody   string

	deleteCalled  bool
	deletePath    string
	deleteVersion string
}

func (m *methodTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	status, body := m.getStatus, m.getBody
	if req.Method == http.MethodDelete {
		m.deleteCalled = true
		m.deletePath = req.URL.Path
		m.deleteVersion = req.Header.Get("If-Unmodified-Since-Version")
		status, body = m.delStatus, m.delBody
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}, nil
}

func newMethodClient(t *methodTransport) *Client {
	c := NewClient("test-key", "12345")
	c.HTTPClient = &http.Client{Transport: t}
	return c
}

func noteItemJSON(version int, tags ...string) string {
	var tagJSON []string
	for _, tag := range tags {
		tagJSON = append(tagJSON, fmt.Sprintf(`{"tag":%q}`, tag))
	}
	return fmt.Sprintf(`{"key":"NOTE5678","version":%d,"data":{"itemType":"note","parentItem":"ITEM0001","tags":[%s]}}`,
		version, strings.Join(tagJSON, ","))
}

// Contract: deleting a note issues DELETE /items/<key> and echoes the item's
// version in If-Unmodified-Since-Version, so Zotero can reject the delete if
// the note changed since we read it (lost-update protection).
func TestDeleteNoteSendsVersionedDelete(t *testing.T) {
	rt := &methodTransport{
		getStatus: http.StatusOK,
		getBody:   noteItemJSON(42, AIGeneratedTag),
		delStatus: http.StatusNoContent,
	}
	client := newMethodClient(rt)

	deleted, err := client.DeleteNote("NOTE5678", false)
	if err != nil {
		t.Fatalf("DeleteNote returned error: %v", err)
	}
	if deleted.Key != "NOTE5678" {
		t.Errorf("expected returned key NOTE5678, got %s", deleted.Key)
	}
	if !rt.deleteCalled {
		t.Fatal("expected a DELETE request, none was sent")
	}
	if rt.deletePath != "/users/12345/items/NOTE5678" {
		t.Errorf("expected DELETE /users/12345/items/NOTE5678, got %s", rt.deletePath)
	}
	if rt.deleteVersion != "42" {
		t.Errorf("expected If-Unmodified-Since-Version 42, got %q", rt.deleteVersion)
	}
}

// Contract: (guardrail) a key that resolves to a non-note item is refused and
// NO delete request is sent — a mistyped key must never destroy a paper or
// attachment.
func TestDeleteNoteRefusesNonNote(t *testing.T) {
	rt := &methodTransport{
		getStatus: http.StatusOK,
		getBody:   `{"key":"PAPER001","version":7,"data":{"itemType":"preprint","title":"A paper"}}`,
		delStatus: http.StatusNoContent,
	}
	client := newMethodClient(rt)

	_, err := client.DeleteNote("PAPER001", false)
	if err == nil {
		t.Fatal("expected error deleting a non-note item, got nil")
	}
	if rt.deleteCalled {
		t.Error("a DELETE request was sent for a non-note item; it must be blocked before any API write")
	}
	if !strings.Contains(err.Error(), "not \"note\"") {
		t.Errorf("expected error to explain the type guard, got: %v", err)
	}
}

// Contract: (guardrail) with requireAIGenerated, a note lacking the
// ai-generated tag is refused and not deleted — autonomous callers must not be
// able to remove human-written notes.
func TestDeleteNoteRequiresAIGeneratedTagWhenAsked(t *testing.T) {
	rt := &methodTransport{
		getStatus: http.StatusOK,
		getBody:   noteItemJSON(3), // no tags
		delStatus: http.StatusNoContent,
	}
	client := newMethodClient(rt)

	_, err := client.DeleteNote("NOTE5678", true)
	if err == nil {
		t.Fatal("expected error deleting a human note under requireAIGenerated, got nil")
	}
	if rt.deleteCalled {
		t.Error("a DELETE was sent for a note lacking the ai-generated tag; it must be blocked")
	}
}

// Contract: with requireAIGenerated, an ai-generated note IS deleted — the tag
// guard permits the caller to remove notes it created.
func TestDeleteNoteAllowsAIGeneratedNote(t *testing.T) {
	rt := &methodTransport{
		getStatus: http.StatusOK,
		getBody:   noteItemJSON(5, AIGeneratedTag, "ai-critique"),
		delStatus: http.StatusNoContent,
	}
	client := newMethodClient(rt)

	if _, err := client.DeleteNote("NOTE5678", true); err != nil {
		t.Fatalf("DeleteNote returned error for an ai-generated note: %v", err)
	}
	if !rt.deleteCalled {
		t.Error("expected the ai-generated note to be deleted")
	}
}

// Contract: a non-2xx delete response (e.g. 412 version conflict, 404 gone)
// becomes an error — a failed deletion must never be reported as success.
func TestDeleteNoteSurfacesAPIError(t *testing.T) {
	rt := &methodTransport{
		getStatus: http.StatusOK,
		getBody:   noteItemJSON(9, AIGeneratedTag),
		delStatus: http.StatusPreconditionFailed,
		delBody:   "version conflict",
	}
	client := newMethodClient(rt)

	_, err := client.DeleteNote("NOTE5678", false)
	if err == nil {
		t.Fatal("expected error on HTTP 412, got nil")
	}
}

// Contract: HasTag matches tags case-insensitively so the tag guard is not
// defeated by casing differences in the stored tag.
func TestItemHasTag(t *testing.T) {
	item := &Item{Data: ItemData{Tags: []Tag{{Tag: "AI-Generated"}, {Tag: "ai-critique"}}}}
	if !item.HasTag(AIGeneratedTag) {
		t.Errorf("expected HasTag(%q) to match %q case-insensitively", AIGeneratedTag, "AI-Generated")
	}
	if item.HasTag("nonexistent") {
		t.Error("expected HasTag to return false for a missing tag")
	}
}
