package resolve

import (
	"context"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"

	"github.com/yk0817/zotero-cli/zotero"
)

const arxivQueryURL = "http://export.arxiv.org/api/query?id_list="

type arxivFeed struct {
	XMLName xml.Name     `xml:"feed"`
	Entries []arxivEntry `xml:"entry"`
}

type arxivEntry struct {
	ID        string        `xml:"id"`
	Title     string        `xml:"title"`
	Summary   string        `xml:"summary"`
	Published string        `xml:"published"`
	Authors   []arxivAuthor `xml:"author"`
	// DOI matches <arxiv:doi> by local name; encoding/xml ignores the prefix.
	DOI string `xml:"doi"`
}

type arxivAuthor struct {
	Name string `xml:"name"`
}

// arxivAbsID extracts the arXiv number (with any version) from an abs URL.
var arxivAbsID = regexp.MustCompile(`arxiv\.org/abs/([^\s<]+)`)

// arxivVersion matches a trailing version suffix such as "v7".
var arxivVersion = regexp.MustCompile(`v[0-9]+$`)

// ResolveArXiv fetches a preprint's metadata from the arXiv API and maps it to
// Zotero item metadata.
func (c *Client) ResolveArXiv(ctx context.Context, id string) (zotero.ItemData, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return zotero.ItemData{}, fmt.Errorf("empty arXiv ID")
	}

	body, err := c.get(ctx, arxivQueryURL+id, "application/atom+xml")
	if err != nil {
		return zotero.ItemData{}, err
	}

	var feed arxivFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return zotero.ItemData{}, fmt.Errorf("failed to parse arXiv response: %w", err)
	}

	// arXiv answers an unknown ID with an empty feed or a synthetic "Error"
	// entry (id under /api/errors), not an HTTP error.
	if len(feed.Entries) == 0 {
		return zotero.ItemData{}, fmt.Errorf("%w: arXiv %s", ErrNotFound, id)
	}
	e := feed.Entries[0]
	if strings.Contains(e.ID, "/api/errors") || strings.EqualFold(collapseWS(e.Title), "Error") {
		return zotero.ItemData{}, fmt.Errorf("%w: arXiv %s", ErrNotFound, id)
	}

	absID := arxivAbsID.FindStringSubmatch(e.ID)
	fullID := id
	if len(absID) == 2 {
		fullID = absID[1]
	}
	baseID := arxivVersion.ReplaceAllString(fullID, "")

	doi := strings.TrimSpace(e.DOI)
	if doi == "" {
		doi = "10.48550/arXiv." + baseID
	}

	return zotero.ItemData{
		ItemType:     "preprint",
		Title:        collapseWS(e.Title),
		Creators:     arxivCreators(e.Authors),
		Date:         datePart(e.Published),
		AbstractNote: collapseWS(e.Summary),
		URL:          "https://arxiv.org/abs/" + fullID,
		DOI:          doi,
	}, nil
}

func arxivCreators(authors []arxivAuthor) []zotero.Creator {
	creators := make([]zotero.Creator, 0, len(authors))
	for _, a := range authors {
		if c := personCreator(a.Name); c.LastName != "" || c.Name != "" {
			creators = append(creators, c)
		}
	}
	return creators
}

// datePart returns the YYYY-MM-DD prefix of an RFC3339 timestamp, or the whole
// string if it has no 'T' separator.
func datePart(ts string) string {
	ts = strings.TrimSpace(ts)
	if i := strings.IndexByte(ts, 'T'); i > 0 {
		return ts[:i]
	}
	return ts
}
