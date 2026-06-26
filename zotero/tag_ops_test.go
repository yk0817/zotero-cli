package zotero

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// tagRecordingTransport records the method, path, body and headers of the last
// request so the tag write tests can assert the optimistic-concurrency header
// and the PATCH payload shape (the shared recordingTransport does not capture
// headers).
type tagRecordingTransport struct {
	lastMethod string
	lastPath   string
	lastBody   []byte
	lastHeader http.Header
	status     int
	response   string
}

func (r *tagRecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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

func newTagRecordingClient(status int, response string) (*Client, *tagRecordingTransport) {
	rt := &tagRecordingTransport{status: status, response: response}
	c := NewClient("test-key", "12345")
	c.HTTPClient = &http.Client{Transport: rt}
	return c, rt
}

func tagsFromStrings(ss []string) []Tag {
	out := []Tag{}
	for _, s := range ss {
		out = append(out, Tag{Tag: s})
	}
	return out
}

func tagStringsOf(tags []Tag) []string {
	out := []string{}
	for _, t := range tags {
		out = append(out, t.Tag)
	}
	return out
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Contract: ListTags follows pagination instead of trusting one page. The
// Zotero tags endpoint caps a page at 100 (and defaults to 25 without an
// explicit limit), so a library with more than one page of tags must not have
// the closed vocabulary silently truncated — a missing tag would tempt a
// caller into creating a duplicate.
func TestListTagsPaginates(t *testing.T) {
	fullPage := make([]string, 100)
	for i := range fullPage {
		fullPage[i] = fmt.Sprintf(`{"tag":"tag%03d","meta":{"numItems":1}}`, i)
	}
	secondPage := `[{"tag":"tag100","meta":{"numItems":2}}]`

	client, qt := newQueryClient("")
	qt.queue = []string{"[" + strings.Join(fullPage, ",") + "]", secondPage}

	tags, err := client.ListTags()

	if err != nil {
		t.Fatalf("ListTags returned error: %v", err)
	}
	if len(tags) != 101 {
		t.Fatalf("expected 101 tags across 2 pages, got %d", len(tags))
	}
	if len(qt.urls) != 2 {
		t.Fatalf("expected 2 paginated requests, got %d", len(qt.urls))
	}
	if qt.urls[0].Path != "/users/12345/tags" {
		t.Errorf("expected tags path, got %s", qt.urls[0].Path)
	}
	if got := qt.urls[0].Query().Get("limit"); got != "100" {
		t.Errorf("expected limit=100, got %q", got)
	}
	if got := qt.urls[1].Query().Get("start"); got != "100" {
		t.Errorf("expected second request to resume at start=100, got %q", got)
	}
}

// Contract: ListTags parses the tag name and its usage count (meta.numItems),
// which the closed-vocabulary listing shows so a human can tell established
// tags from one-off ones.
func TestListTagsParsesNumItems(t *testing.T) {
	client, qt := newQueryClient(`[{"tag":"nlp","meta":{"type":0,"numItems":5}}]`)

	tags, err := client.ListTags()

	if err != nil {
		t.Fatalf("ListTags returned error: %v", err)
	}
	if len(tags) != 1 || tags[0].Tag != "nlp" || tags[0].Meta.NumItems != 5 {
		t.Errorf("unexpected tags: %+v", tags)
	}
	if qt.lastURL.Path != "/users/12345/tags" {
		t.Errorf("expected tags path, got %s", qt.lastURL.Path)
	}
}

// Contract: an HTTP error from the tags endpoint surfaces as an error — a 500
// must never be reported as "no tags", which would make the closed vocabulary
// look empty and wrongly invite new-tag creation.
func TestListTagsAPIError(t *testing.T) {
	client, qt := newQueryClient("boom")
	qt.status = http.StatusInternalServerError

	_, err := client.ListTags()

	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

// Contract: a malformed JSON body is an error — a truncated or non-JSON tags
// response must never be silently treated as an empty vocabulary.
func TestListTagsInvalidJSON(t *testing.T) {
	client, _ := newQueryClient("not json")

	_, err := client.ListTags()

	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// Contract: ApplyTagDelta computes the resulting tag set purely (no network):
// removals are case-insensitive, additions never duplicate an existing tag
// (case-insensitive), existing order is preserved, and the input slice is
// never mutated. This is the offline preview the dry-run shows and the PATCH
// body sends, so a wrong set here would silently mis-tag items.
func TestApplyTagDelta(t *testing.T) {
	tests := []struct {
		name    string
		current []string
		add     []string
		remove  []string
		want    []string
	}{
		{name: "add new tag appends", current: []string{"a"}, add: []string{"b"}, want: []string{"a", "b"}},
		{name: "remove existing tag drops it", current: []string{"a", "b"}, remove: []string{"a"}, want: []string{"b"}},
		{name: "remove is case-insensitive", current: []string{"NLP"}, remove: []string{"nlp"}, want: []string{}},
		{name: "add existing tag is a no-op (no duplicate)", current: []string{"a"}, add: []string{"a"}, want: []string{"a"}},
		{name: "add differing only in case is a no-op", current: []string{"NLP"}, add: []string{"nlp"}, want: []string{"NLP"}},
		{name: "tag removed then re-added ends up added", current: []string{"a"}, add: []string{"a"}, remove: []string{"a"}, want: []string{"a"}},
		{name: "order preserved, new tags appended in order", current: []string{"a", "b"}, add: []string{"c", "d"}, want: []string{"a", "b", "c", "d"}},
		{name: "whitespace-only add is dropped", current: []string{"a"}, add: []string{"  "}, want: []string{"a"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := tagsFromStrings(tt.current)

			got := ApplyTagDelta(current, tt.add, tt.remove)

			if !equalStringSlice(tagStringsOf(got), tt.want) {
				t.Errorf("ApplyTagDelta(%v, add=%v, remove=%v) = %v, want %v",
					tt.current, tt.add, tt.remove, tagStringsOf(got), tt.want)
			}
			if !equalStringSlice(tagStringsOf(current), tt.current) {
				t.Errorf("ApplyTagDelta mutated its input slice: %v != %v", tagStringsOf(current), tt.current)
			}
		})
	}
}

// Contract: UpdateItemTags PATCHes /items/<key> with the resulting tag set and
// echoes the item's version in If-Unmodified-Since-Version, so a concurrent
// edit is rejected (412) rather than silently clobbering another client's tag
// change. The body must carry the computed set (decoded and checked), not the
// raw add/remove lists.
func TestUpdateItemTagsPatchesWithVersionHeader(t *testing.T) {
	client, rt := newTagRecordingClient(http.StatusNoContent, "")
	item := &Item{Key: "ITEM0001", Version: 42, Data: ItemData{Tags: []Tag{{Tag: "old"}}}}

	result, err := client.UpdateItemTags(item, []string{"new"}, []string{"old"})

	if err != nil {
		t.Fatalf("UpdateItemTags returned error: %v", err)
	}
	if rt.lastMethod != http.MethodPatch || rt.lastPath != "/users/12345/items/ITEM0001" {
		t.Errorf("expected PATCH /users/12345/items/ITEM0001, got %s %s", rt.lastMethod, rt.lastPath)
	}
	if got := rt.lastHeader.Get("If-Unmodified-Since-Version"); got != "42" {
		t.Errorf("expected If-Unmodified-Since-Version=42, got %q", got)
	}

	var payload map[string][]Tag
	if err := json.Unmarshal(rt.lastBody, &payload); err != nil {
		t.Fatalf("PATCH body is not the expected JSON object: %v", err)
	}
	if got := tagStringsOf(payload["tags"]); !equalStringSlice(got, []string{"new"}) {
		t.Errorf("expected PATCH body tags [new], got %v", got)
	}
	if got := tagStringsOf(result); !equalStringSlice(got, []string{"new"}) {
		t.Errorf("expected returned tags [new], got %v", got)
	}
}

// Contract: a non-204 response (e.g. 412 version conflict) becomes an error
// that mentions the status code — a tag update must never be reported as
// successful when the API rejected it, or the caller would believe the library
// changed when it did not.
func TestUpdateItemTagsAPIError(t *testing.T) {
	client, _ := newTagRecordingClient(http.StatusPreconditionFailed, "conflict")
	item := &Item{Key: "ITEM0001", Version: 1}

	_, err := client.UpdateItemTags(item, []string{"new"}, nil)

	if err == nil {
		t.Fatal("expected error for HTTP 412, got nil")
	}
	if !strings.Contains(err.Error(), "412") {
		t.Errorf("expected error to mention status 412, got %v", err)
	}
}
