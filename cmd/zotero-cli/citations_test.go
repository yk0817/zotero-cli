package main

import (
	"testing"

	"github.com/yk0817/zotero-cli/scholar"
)

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
