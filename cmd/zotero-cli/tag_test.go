package main

import (
	"encoding/json"
	"testing"

	"github.com/yk0817/zotero-cli/zotero"
)

// Contract: validateTags accepts ordinary single-line vocabulary terms but
// rejects empties, path traversal, null bytes, and — unlike free-text input —
// the whitespace control characters (newline/carriage-return/tab) that
// sanitizeInput tolerates. A tab in a tag would corrupt the tab-delimited
// `tags` table; a newline would split one tag across rows.
func TestValidateTags(t *testing.T) {
	tests := []struct {
		name    string
		tag     string
		wantErr bool
	}{
		{name: "plain tag ok", tag: "machine-learning", wantErr: false},
		{name: "tag with spaces ok", tag: "graph neural network", wantErr: false},
		{name: "empty rejected", tag: "", wantErr: true},
		{name: "whitespace-only rejected", tag: "   ", wantErr: true},
		{name: "tab rejected", tag: "a\tb", wantErr: true},
		{name: "newline rejected", tag: "a\nb", wantErr: true},
		{name: "carriage return rejected", tag: "a\rb", wantErr: true},
		{name: "null byte rejected", tag: "a\x00b", wantErr: true},
		{name: "other control char rejected", tag: "a\x07b", wantErr: true},
		{name: "path traversal rejected", tag: "../etc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTags([]string{tt.tag})
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateTags(%q) error = %v, wantErr %v", tt.tag, err, tt.wantErr)
			}
			if err != nil {
				assertCLIErrorCode(t, err, ErrCodeValidation)
			}
		})
	}
}

// Contract: emptyIfNil turns a nil slice into a non-nil empty slice (so JSON
// serializes `[]`, not `null`) while leaving a populated slice untouched.
func TestEmptyIfNil(t *testing.T) {
	if got := emptyIfNil(nil); got == nil || len(got) != 0 {
		t.Errorf("emptyIfNil(nil) = %v, want non-nil empty slice", got)
	}
	in := []string{"x"}
	if got := emptyIfNil(in); len(got) != 1 || got[0] != "x" {
		t.Errorf("emptyIfNil(%v) = %v, want unchanged", in, got)
	}
}

// Contract: the --dry-run JSON payload serializes the unspecified delta side as
// an empty array, never null (project convention: empty results are `[]`), and
// resultTags keeps the `[{"tag":...}]` object shape of get/context rather than
// degrading to a bare string array. Asserted by inspecting the serialized form,
// not the in-memory map.
func TestTagDryRunPayloadShape(t *testing.T) {
	raw, err := json.Marshal(tagDryRunPayload("ITEM0001", nil, nil, []zotero.Tag{{Tag: "auto", Type: 1}}))
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var envelope struct {
		Payload map[string]json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got := string(envelope.Payload["add"]); got != "[]" {
		t.Errorf("add serialized as %s, want []", got)
	}
	if got := string(envelope.Payload["remove"]); got != "[]" {
		t.Errorf("remove serialized as %s, want []", got)
	}

	var resultTags []zotero.Tag
	if err := json.Unmarshal(envelope.Payload["resultTags"], &resultTags); err != nil {
		t.Fatalf("resultTags is not an array of {tag,...} objects (shape regressed?): %v", err)
	}
	if len(resultTags) != 1 || resultTags[0].Tag != "auto" || resultTags[0].Type != 1 {
		t.Errorf("resultTags = %+v, want [{auto 1}]", resultTags)
	}
}

// Contract: the post-update JSON payload emits tags as `[{"tag":...,"type":...}]`
// objects, matching the `tags` field of get/context, so a consumer comparing
// before/after tag state does not break on a type mismatch. A regression to a
// bare []string would fail to decode into []zotero.Tag.
func TestTagResultPayloadShape(t *testing.T) {
	raw, err := json.Marshal(tagResultPayload("ITEM0001", []zotero.Tag{{Tag: "auto", Type: 1}, {Tag: "manual"}}))
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded struct {
		ItemKey string       `json:"itemKey"`
		Tags    []zotero.Tag `json:"tags"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("tags is not an array of {tag,...} objects (shape regressed to []string?): %v", err)
	}
	if decoded.ItemKey != "ITEM0001" {
		t.Errorf("itemKey = %q, want ITEM0001", decoded.ItemKey)
	}
	if len(decoded.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(decoded.Tags))
	}
	if decoded.Tags[0].Tag != "auto" || decoded.Tags[0].Type != 1 {
		t.Errorf("tags[0] = %+v, want {auto 1}", decoded.Tags[0])
	}
	if decoded.Tags[1].Tag != "manual" || decoded.Tags[1].Type != 0 {
		t.Errorf("tags[1] = %+v, want {manual 0}", decoded.Tags[1])
	}
}
