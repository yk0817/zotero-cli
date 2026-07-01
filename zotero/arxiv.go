package zotero

import (
	"regexp"
	"strings"
)

// arxivURLPattern extracts an arXiv identifier (with an optional version) from
// an abs/pdf URL such as https://arxiv.org/abs/2301.01234v2.
var arxivURLPattern = regexp.MustCompile(`arxiv\.org/(?:abs|pdf)/([0-9]{4}\.[0-9]{4,5}(?:v[0-9]+)?)`)

// ExtractArxivID returns the arXiv ID for an item from its URL or an
// arXiv-style DOI (10.48550/arXiv.<id>), or "" if none is present. Zotero has
// no dedicated arXiv field, so the URL and DOI are the only reliable sources;
// this is shared by the citations command and add's duplicate detection.
func ExtractArxivID(d ItemData) string {
	if m := arxivURLPattern.FindStringSubmatch(d.URL); m != nil {
		return m[1]
	}
	if id := strings.TrimPrefix(d.DOI, "10.48550/arXiv."); id != d.DOI {
		return id
	}
	return ""
}
