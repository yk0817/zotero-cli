package zotero

import "testing"

// Contract: ExtractArxivID finds the arXiv identifier from the item URL or an
// arXiv-style DOI, and returns "" when none is present. Zotero has no arXiv
// field, so this is the only way to resolve a preprint that lacks a regular
// DOI — a parse miss silently breaks citation resolution and add's arXiv
// duplicate detection alike (both callers depend on this single helper).
func TestExtractArxivID(t *testing.T) {
	tests := []struct {
		name string
		data ItemData
		want string
	}{
		{name: "abs URL", data: ItemData{URL: "https://arxiv.org/abs/2301.01234"}, want: "2301.01234"},
		{name: "abs URL with version", data: ItemData{URL: "https://arxiv.org/abs/2301.01234v2"}, want: "2301.01234v2"},
		{name: "pdf URL", data: ItemData{URL: "http://arxiv.org/pdf/1706.03762"}, want: "1706.03762"},
		{name: "arXiv-style DOI", data: ItemData{DOI: "10.48550/arXiv.2105.00001"}, want: "2105.00001"},
		{name: "regular DOI yields nothing", data: ItemData{DOI: "10.1145/3292500.3330701"}, want: ""},
		{name: "non-arxiv URL yields nothing", data: ItemData{URL: "https://example.com/paper"}, want: ""},
		{name: "empty item yields nothing", data: ItemData{}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractArxivID(tt.data); got != tt.want {
				t.Errorf("ExtractArxivID() = %q, want %q", got, tt.want)
			}
		})
	}
}
