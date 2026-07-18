package semindex

import "testing"

// Contract: cosine is 1 for identical direction, 0 for orthogonal, and 0 (not
// NaN) for a zero vector. Ranking correctness and crash-freeness both depend on
// this; a NaN would poison sort order.
func TestCosine(t *testing.T) {
	if got := Cosine([]float32{1, 0}, []float32{2, 0}); got < 0.999 || got > 1.001 {
		t.Errorf("parallel = %v, want ~1", got)
	}
	if got := Cosine([]float32{1, 0}, []float32{0, 1}); got < -0.001 || got > 0.001 {
		t.Errorf("orthogonal = %v, want ~0", got)
	}
	if got := Cosine([]float32{0, 0}, []float32{1, 1}); got != 0 {
		t.Errorf("zero vector = %v, want 0", got)
	}
}

// Contract: Search returns entries ordered by descending similarity to the
// query and truncates to k. This is the whole ranking guarantee semantic search
// makes; if order or top-k were wrong the feature is useless.
func TestSearchRanksAndTruncates(t *testing.T) {
	idx := &Index{Entries: []Entry{
		{ItemKey: "FAR", Vector: []float32{0, 1}},
		{ItemKey: "NEAR", Vector: []float32{1, 0}},
		{ItemKey: "MID", Vector: []float32{1, 1}},
	}}

	got := idx.Search([]float32{1, 0}, 2)

	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (k truncation)", len(got))
	}
	if got[0].ItemKey != "NEAR" || got[1].ItemKey != "MID" {
		t.Errorf("order = [%s %s], want [NEAR MID]", got[0].ItemKey, got[1].ItemKey)
	}
	if got[0].Score < got[1].Score {
		t.Errorf("scores not descending: %v < %v", got[0].Score, got[1].Score)
	}
}

// Contract: k <= 0 returns all entries (no truncation), so a caller asking for
// "everything ranked" is not silently given nothing.
func TestSearchZeroKReturnsAll(t *testing.T) {
	idx := &Index{Entries: []Entry{{ItemKey: "A", Vector: []float32{1}}, {ItemKey: "B", Vector: []float32{1}}}}

	if got := idx.Search([]float32{1}, 0); len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}
