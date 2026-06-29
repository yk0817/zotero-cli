package main

import (
	"testing"

	"github.com/yk0817/zotero-cli/scholar"
	"github.com/yk0817/zotero-cli/zotero"
)

// Contract: extractArxivID finds the arXiv identifier from the item URL or an
// arXiv-style DOI, and returns "" when none is present. Zotero has no arXiv
// field, so this is the only way the citations command can resolve a preprint
// that lacks a regular DOI — a parse miss here silently breaks resolution for
// arXiv-only papers.
func TestExtractArxivID(t *testing.T) {
	tests := []struct {
		name string
		data zotero.ItemData
		want string
	}{
		{name: "abs URL", data: zotero.ItemData{URL: "https://arxiv.org/abs/2301.01234"}, want: "2301.01234"},
		{name: "abs URL with version", data: zotero.ItemData{URL: "https://arxiv.org/abs/2301.01234v2"}, want: "2301.01234v2"},
		{name: "pdf URL", data: zotero.ItemData{URL: "http://arxiv.org/pdf/1706.03762"}, want: "1706.03762"},
		{name: "arXiv-style DOI", data: zotero.ItemData{DOI: "10.48550/arXiv.2105.00001"}, want: "2105.00001"},
		{name: "regular DOI yields nothing", data: zotero.ItemData{DOI: "10.1145/3292500.3330701"}, want: ""},
		{name: "non-arxiv URL yields nothing", data: zotero.ItemData{URL: "https://example.com/paper"}, want: ""},
		{name: "empty item yields nothing", data: zotero.ItemData{}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractArxivID(tt.data)
			if got != tt.want {
				t.Errorf("extractArxivID() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Contract: author display truncates to "et al." past three names and shows "-"
// when no author is known, so the citation table never prints a blank author
// cell (an empty cell reads as a render bug to a human or LLM).
func TestFormatScholarAuthors(t *testing.T) {
	tests := []struct {
		name    string
		authors []scholar.Author
		want    string
	}{
		{name: "none", authors: nil, want: "-"},
		{name: "named author with empty name skipped", authors: []scholar.Author{{Name: ""}}, want: "-"},
		{name: "one", authors: []scholar.Author{{Name: "A"}}, want: "A"},
		{name: "three kept in full", authors: []scholar.Author{{Name: "A"}, {Name: "B"}, {Name: "C"}}, want: "A; B; C"},
		{name: "four truncated", authors: []scholar.Author{{Name: "A"}, {Name: "B"}, {Name: "C"}, {Name: "D"}}, want: "A; B; C et al."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatScholarAuthors(tt.authors)
			if got != tt.want {
				t.Errorf("formatScholarAuthors() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Contract: a zero/unknown year renders as "-" (not "0"), so a paper with no
// year in Semantic Scholar does not show a misleading year-zero in the table.
func TestFormatYear(t *testing.T) {
	if got := formatYear(0); got != "-" {
		t.Errorf("formatYear(0) = %q, want %q", got, "-")
	}
	if got := formatYear(2017); got != "2017" {
		t.Errorf("formatYear(2017) = %q, want %q", got, "2017")
	}
}
