package semindex

import (
	"math"
	"sort"
)

// Result is one ranked item. JSON tags match the CLI output contract
// ({itemKey,title,score}) so the command can print it directly.
type Result struct {
	ItemKey string  `json:"itemKey"`
	Title   string  `json:"title"`
	Score   float64 `json:"score"`
}

// Cosine returns the cosine similarity of a and b, computed in float64. A zero
// vector on either side yields 0 rather than NaN. Extra trailing dimensions on
// the longer vector are ignored (vectors are expected to share Dim).
func Cosine(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		av, bv := float64(a[i]), float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// Search ranks all entries by cosine similarity to query, descending, and
// returns the top k. k <= 0 returns every entry.
func (idx *Index) Search(query []float32, k int) []Result {
	results := make([]Result, 0, len(idx.Entries))
	for _, e := range idx.Entries {
		results = append(results, Result{ItemKey: e.ItemKey, Title: e.Title, Score: Cosine(query, e.Vector)})
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if k > 0 && len(results) > k {
		results = results[:k]
	}
	return results
}
