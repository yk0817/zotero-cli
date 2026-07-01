package resolve

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yk0817/zotero-cli/zotero"
)

const crossrefWorksURL = "https://api.crossref.org/works/"

type crossrefResponse struct {
	Message crossrefWork `json:"message"`
}

type crossrefWork struct {
	DOI            string           `json:"DOI"`
	Type           string           `json:"type"`
	Title          []string         `json:"title"`
	Author         []crossrefAuthor `json:"author"`
	ContainerTitle []string         `json:"container-title"`
	Issued         crossrefDate     `json:"issued"`
	Abstract       string           `json:"abstract"`
	URL            string           `json:"URL"`
	Publisher      string           `json:"publisher"`
	ISBN           []string         `json:"ISBN"`
}

type crossrefAuthor struct {
	Given  string `json:"given"`
	Family string `json:"family"`
	Name   string `json:"name"`
}

type crossrefDate struct {
	DateParts [][]int `json:"date-parts"`
}

// ResolveDOI fetches a work's metadata from Crossref and maps it to Zotero
// item metadata.
func (c *Client) ResolveDOI(ctx context.Context, doi string) (zotero.ItemData, error) {
	doi = strings.TrimSpace(doi)
	if doi == "" {
		return zotero.ItemData{}, fmt.Errorf("empty DOI")
	}

	body, err := c.get(ctx, crossrefWorksURL+doi, "application/json")
	if err != nil {
		return zotero.ItemData{}, err
	}

	var parsed crossrefResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return zotero.ItemData{}, fmt.Errorf("failed to parse Crossref response: %w", err)
	}
	w := parsed.Message

	data := zotero.ItemData{
		ItemType:     crossrefItemType(w.Type),
		Title:        firstNonEmpty(w.Title),
		Creators:     crossrefCreators(w.Author),
		Date:         formatDateParts(w.Issued.DateParts),
		AbstractNote: stripMarkup(w.Abstract),
		URL:          w.URL,
		DOI:          w.DOI,
		Publisher:    w.Publisher,
		ISBN:         firstNonEmpty(w.ISBN),
	}
	setContainerTitle(&data, firstNonEmpty(w.ContainerTitle))

	if data.Title == "" {
		return zotero.ItemData{}, fmt.Errorf("Crossref returned no title for DOI %q", doi)
	}
	return data, nil
}

// crossrefItemType maps a Crossref work type to the closest Zotero item type,
// defaulting to journalArticle for unknown types (Crossref is dominated by
// articles, and journalArticle accepts the common fields we populate).
func crossrefItemType(t string) string {
	switch t {
	case "journal-article":
		return "journalArticle"
	case "proceedings-article":
		return "conferencePaper"
	case "book":
		return "book"
	case "book-chapter", "book-part", "book-section":
		return "bookSection"
	case "posted-content":
		return "preprint"
	case "report", "report-component":
		return "report"
	case "dataset":
		return "dataset"
	case "dissertation":
		return "thesis"
	default:
		return "journalArticle"
	}
}

// setContainerTitle stores the container name in the field Zotero defines for
// the item type. Zotero rejects an item that carries a field invalid for its
// type, so an article's container goes to publicationTitle, a paper's to
// proceedingsTitle, and a chapter's to bookTitle; other types have no
// container field and drop it.
func setContainerTitle(data *zotero.ItemData, container string) {
	if container == "" {
		return
	}
	switch data.ItemType {
	case "journalArticle":
		data.PublicationTitle = container
	case "conferencePaper":
		data.ProceedingsTitle = container
	case "bookSection":
		data.BookTitle = container
	}
}

func crossrefCreators(authors []crossrefAuthor) []zotero.Creator {
	creators := make([]zotero.Creator, 0, len(authors))
	for _, a := range authors {
		switch {
		case a.Family != "":
			creators = append(creators, zotero.Creator{
				CreatorType: "author",
				FirstName:   strings.TrimSpace(a.Given),
				LastName:    strings.TrimSpace(a.Family),
			})
		case a.Name != "":
			// An organisation author has a single name field.
			creators = append(creators, zotero.Creator{CreatorType: "author", Name: collapseWS(a.Name)})
		}
	}
	return creators
}

// formatDateParts renders Crossref's [[year, month, day]] structure as an
// ISO-ish date string, using only the components present (year, year-month, or
// year-month-day).
func formatDateParts(parts [][]int) string {
	if len(parts) == 0 || len(parts[0]) == 0 {
		return ""
	}
	p := parts[0]
	switch len(p) {
	case 1:
		return fmt.Sprintf("%04d", p[0])
	case 2:
		return fmt.Sprintf("%04d-%02d", p[0], p[1])
	default:
		return fmt.Sprintf("%04d-%02d-%02d", p[0], p[1], p[2])
	}
}
