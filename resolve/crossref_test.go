package resolve

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const crossrefJournal = `{
  "status": "ok",
  "message": {
    "DOI": "10.1038/s41586-021-03819-2",
    "type": "journal-article",
    "title": ["Highly accurate protein structure prediction with AlphaFold"],
    "author": [
      {"given": "John", "family": "Jumper"},
      {"given": "Richard", "family": "Evans"}
    ],
    "container-title": ["Nature"],
    "issued": {"date-parts": [[2021, 7, 15]]},
    "abstract": "<jats:p>Proteins are <jats:italic>essential</jats:italic> to life.</jats:p>",
    "URL": "https://doi.org/10.1038/s41586-021-03819-2"
  }
}`

// Contract: ResolveDOI turns a Crossref work record into Zotero item metadata
// with the right field mapping — title, authors as first/last creators, an
// ISO date, the journal as publicationTitle, and the DOI — so the created
// item is a faithful bibliographic record, not a blank stub.
func TestResolveDOIParsesCrossrefMetadata(t *testing.T) {
	client, _ := newTestClient(map[string]stubResponse{
		"api.crossref.org/works/10.1038": {status: 200, body: crossrefJournal},
	})

	data, err := client.ResolveDOI(context.Background(), "10.1038/s41586-021-03819-2")

	if err != nil {
		t.Fatalf("ResolveDOI returned error: %v", err)
	}
	if data.ItemType != "journalArticle" {
		t.Errorf("itemType = %q, want journalArticle", data.ItemType)
	}
	if data.Title != "Highly accurate protein structure prediction with AlphaFold" {
		t.Errorf("title = %q", data.Title)
	}
	if data.DOI != "10.1038/s41586-021-03819-2" {
		t.Errorf("DOI = %q", data.DOI)
	}
	if data.PublicationTitle != "Nature" {
		t.Errorf("publicationTitle = %q, want Nature", data.PublicationTitle)
	}
	if data.Date != "2021-07-15" {
		t.Errorf("date = %q, want 2021-07-15", data.Date)
	}
	if len(data.Creators) != 2 {
		t.Fatalf("expected 2 creators, got %d (%v)", len(data.Creators), data.Creators)
	}
	first := data.Creators[0]
	if first.CreatorType != "author" || first.FirstName != "John" || first.LastName != "Jumper" {
		t.Errorf("creator[0] = %+v, want author John Jumper", first)
	}
}

// Contract: the JATS/XML markup Crossref wraps abstracts in is stripped to
// plain text before storage, so the abstractNote does not carry raw
// <jats:p> tags into the Zotero item.
func TestResolveDOIStripsAbstractMarkup(t *testing.T) {
	client, _ := newTestClient(map[string]stubResponse{
		"api.crossref.org": {status: 200, body: crossrefJournal},
	})

	data, err := client.ResolveDOI(context.Background(), "10.1038/s41586-021-03819-2")

	if err != nil {
		t.Fatalf("ResolveDOI returned error: %v", err)
	}
	if strings.Contains(data.AbstractNote, "<") {
		t.Errorf("abstract still contains markup: %q", data.AbstractNote)
	}
	if data.AbstractNote != "Proteins are essential to life." {
		t.Errorf("abstract = %q", data.AbstractNote)
	}
}

// Contract: Crossref work types map to the closest Zotero item type, because
// the item type drives which fields Zotero accepts and how the reference is
// cited. An unknown type falls back to journalArticle rather than an invalid
// type that Zotero would reject.
func TestResolveDOIMapsWorkType(t *testing.T) {
	tests := []struct {
		crossrefType string
		want         string
	}{
		{"journal-article", "journalArticle"},
		{"proceedings-article", "conferencePaper"},
		{"book", "book"},
		{"book-chapter", "bookSection"},
		{"posted-content", "preprint"},
		{"something-new", "journalArticle"},
	}

	for _, tt := range tests {
		t.Run(tt.crossrefType, func(t *testing.T) {
			body := `{"status":"ok","message":{"DOI":"10.1/x","type":"` + tt.crossrefType + `","title":["T"]}}`
			client, _ := newTestClient(map[string]stubResponse{"api.crossref.org": {status: 200, body: body}})

			data, err := client.ResolveDOI(context.Background(), "10.1/x")

			if err != nil {
				t.Fatalf("ResolveDOI returned error: %v", err)
			}
			if data.ItemType != tt.want {
				t.Errorf("type %q mapped to %q, want %q", tt.crossrefType, data.ItemType, tt.want)
			}
		})
	}
}

// Contract: the container title (journal/proceedings/book) is stored in the
// field Zotero actually defines for that item type. Sending it under the wrong
// field name (e.g. publicationTitle on a conferencePaper) makes Zotero reject
// the whole item as an invalid field, so the mapping is type-specific.
func TestResolveDOIMapsContainerToTypeSpecificField(t *testing.T) {
	tests := []struct {
		name         string
		crossrefType string
		wantPub      string
		wantProc     string
		wantBook     string
	}{
		{name: "journal", crossrefType: "journal-article", wantPub: "Nature"},
		{name: "conference", crossrefType: "proceedings-article", wantProc: "Nature"},
		{name: "book section", crossrefType: "book-chapter", wantBook: "Nature"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"status":"ok","message":{"DOI":"10.1/x","type":"` + tt.crossrefType + `","title":["T"],"container-title":["Nature"]}}`
			client, _ := newTestClient(map[string]stubResponse{"api.crossref.org": {status: 200, body: body}})

			data, err := client.ResolveDOI(context.Background(), "10.1/x")

			if err != nil {
				t.Fatalf("ResolveDOI returned error: %v", err)
			}
			if data.PublicationTitle != tt.wantPub {
				t.Errorf("publicationTitle = %q, want %q", data.PublicationTitle, tt.wantPub)
			}
			if data.ProceedingsTitle != tt.wantProc {
				t.Errorf("proceedingsTitle = %q, want %q", data.ProceedingsTitle, tt.wantProc)
			}
			if data.BookTitle != tt.wantBook {
				t.Errorf("bookTitle = %q, want %q", data.BookTitle, tt.wantBook)
			}
		})
	}
}

// Contract: the request goes to Crossref's /works/<doi> endpoint. Hitting the
// wrong host or path would silently resolve the wrong record or none, so the
// request shape is pinned.
func TestResolveDOIRequestsWorksPath(t *testing.T) {
	client, st := newTestClient(map[string]stubResponse{
		"api.crossref.org": {status: 200, body: `{"status":"ok","message":{"DOI":"10.1/x","type":"journal-article","title":["T"]}}`},
	})

	_, err := client.ResolveDOI(context.Background(), "10.1/x")

	if err != nil {
		t.Fatalf("ResolveDOI returned error: %v", err)
	}
	if len(st.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(st.requests))
	}
	got := st.requests[0].String()
	if !strings.Contains(got, "api.crossref.org/works/10.1/x") {
		t.Errorf("request URL = %q, want it to hit /works/10.1/x", got)
	}
}

// Contract: a DOI that Crossref does not know (HTTP 404) is reported as a
// not-found error, distinct from a transport failure, so the CLI can tell the
// user the identifier is wrong rather than reporting a spurious success.
func TestResolveDOINotFound(t *testing.T) {
	client, _ := newTestClient(map[string]stubResponse{
		"api.crossref.org": {status: 404, body: "Resource not found."},
	})

	_, err := client.ResolveDOI(context.Background(), "10.9999/nope")

	if err == nil {
		t.Fatal("expected error for unknown DOI, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected a not-found error, got %v", err)
	}
}
