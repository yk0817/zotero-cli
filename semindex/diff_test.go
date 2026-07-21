package semindex

import (
	"sort"
	"testing"
)

// Contract: Diff partitions the current library against the index so update
// re-embeds only what changed. A new key or a bumped Version must be re-embedded;
// an unchanged item must be kept without an embed call (cost); an item gone from
// the library must be removed. Getting any bucket wrong means stale, missing, or
// needlessly re-embedded vectors.
func TestDiffPartitions(t *testing.T) {
	idx := &Index{Entries: []Entry{
		{ItemKey: "KEEP", Version: 1, Vector: []float32{1}},
		{ItemKey: "CHANGED", Version: 1, Vector: []float32{1}},
		{ItemKey: "GONE", Version: 1, Vector: []float32{1}},
	}}
	current := []ItemRef{
		{Key: "KEEP", Version: 1},
		{Key: "CHANGED", Version: 2},
		{Key: "NEW", Version: 1},
	}

	toEmbed, keep, removed := idx.Diff(current)

	sort.Strings(toEmbed)
	if len(toEmbed) != 2 || toEmbed[0] != "CHANGED" || toEmbed[1] != "NEW" {
		t.Errorf("toEmbed = %v, want [CHANGED NEW]", toEmbed)
	}
	if len(keep) != 1 || keep[0].ItemKey != "KEEP" {
		t.Errorf("keep = %+v, want [KEEP]", keep)
	}
	if len(removed) != 1 || removed[0] != "GONE" {
		t.Errorf("removed = %v, want [GONE]", removed)
	}
}
