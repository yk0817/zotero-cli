package zotero

import "testing"

// Contract: FormatAuthors produces the compact display form used in search
// results and item details: "Last, First" joined with ";", institutional
// authors by their single name field, capped at 3 with "et al.", and "-" when
// unknown so table columns never collapse to empty strings.
func TestFormatAuthors(t *testing.T) {
	tests := []struct {
		name     string
		creators []Creator
		want     string
	}{
		{name: "empty returns dash", creators: nil, want: "-"},
		{
			name:     "single author last-first",
			creators: []Creator{{LastName: "Vaswani", FirstName: "Ashish"}},
			want:     "Vaswani, Ashish",
		},
		{
			name:     "institutional name field",
			creators: []Creator{{Name: "OpenAI"}},
			want:     "OpenAI",
		},
		{
			name: "more than three truncated with et al",
			creators: []Creator{
				{LastName: "A", FirstName: "1"},
				{LastName: "B", FirstName: "2"},
				{LastName: "C", FirstName: "3"},
				{LastName: "D", FirstName: "4"},
			},
			want: "A, 1; B, 2; C, 3 et al.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatAuthors(tt.creators)
			if got != tt.want {
				t.Errorf("FormatAuthors() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Contract: tags render comma-joined, "-" when absent (same rationale as
// FormatAuthors: display columns must never be empty).
func TestFormatTags(t *testing.T) {
	tests := []struct {
		name string
		tags []Tag
		want string
	}{
		{name: "empty returns dash", tags: nil, want: "-"},
		{name: "joined with comma", tags: []Tag{{Tag: "nlp"}, {Tag: "survey"}}, want: "nlp, survey"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatTags(tt.tags)
			if got != tt.want {
				t.Errorf("FormatTags() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Contract: Truncate counts runes, not bytes — Japanese titles must cut at
// N characters with an ellipsis, never mid-rune (which would emit mojibake).
func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{name: "shorter than max unchanged", s: "abc", max: 5, want: "abc"},
		{name: "exactly max unchanged", s: "abcde", max: 5, want: "abcde"},
		{name: "longer than max gets ellipsis", s: "abcdef", max: 5, want: "abcd…"},
		{name: "multibyte counted as runes", s: "あいうえおか", max: 5, want: "あいうえ…"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Truncate(tt.s, tt.max)
			if got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
		})
	}
}
