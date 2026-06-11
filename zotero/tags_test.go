package zotero

import (
	"reflect"
	"testing"
)

// Contract: every AI-created note carries AIGeneratedTag exactly once, no
// matter what the caller passes — downstream skills filter on this tag to
// tell AI notes from human notes, so duplicates or omissions break them.
func TestNoteTags(t *testing.T) {
	tests := []struct {
		name  string
		extra []string
		want  []string
	}{
		{name: "nil extra yields marker only", extra: nil, want: []string{"ai-generated"}},
		{name: "extra tags appended after marker", extra: []string{"summary", "nlp"}, want: []string{"ai-generated", "summary", "nlp"}},
		{name: "duplicate marker not added twice", extra: []string{"ai-generated", "x"}, want: []string{"ai-generated", "x"}},
		{name: "marker dedup is case-insensitive", extra: []string{"AI-Generated"}, want: []string{"ai-generated"}},
		{name: "whitespace trimmed and empties dropped", extra: []string{" summary ", "", "  "}, want: []string{"ai-generated", "summary"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NoteTags(tt.extra)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NoteTags(%v) = %v, want %v", tt.extra, got, tt.want)
			}
		})
	}
}
