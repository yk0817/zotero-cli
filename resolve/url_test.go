package resolve

import (
	"context"
	"testing"
)

const htmlHighwire = `<html><head>
<meta name="citation_title" content="Deep Residual Learning for Image Recognition">
<meta name="citation_author" content="He, Kaiming">
<meta name="citation_author" content="Zhang, Xiangyu">
<meta name="citation_journal_title" content="CVPR">
<meta name="citation_doi" content="10.1109/CVPR.2016.90">
<meta name="citation_publication_date" content="2016/06/27">
<title>Should be ignored in favor of citation_title</title>
</head><body>...</body></html>`

const htmlOpenGraph = `<html><head>
<meta property="og:title" content="A Great Blog Post">
<meta property="og:url" content="https://example.com/canonical">
<title>A Great Blog Post - Example Blog</title>
</head></html>`

const htmlAttrReversed = `<html><head>
<meta content="Reversed Attribute Order" property="og:title">
</head></html>`

const htmlNoMeta = `<html><head></head><body>just text, no title or meta</body></html>`

// Contract: a scholarly page exposing Highwire citation_* meta tags resolves to
// a journalArticle with title, authors (parsed from "Last, First"), journal,
// DOI and date — so `add --url` of an article landing page yields a real
// bibliographic record, not just a webpage bookmark.
func TestResolveURLParsesHighwireMeta(t *testing.T) {
	client, _ := newTestClient(map[string]stubResponse{
		"example.org/paper": {status: 200, body: htmlHighwire},
	})

	data, err := client.ResolveURL(context.Background(), "https://example.org/paper")

	if err != nil {
		t.Fatalf("ResolveURL returned error: %v", err)
	}
	if data.ItemType != "journalArticle" {
		t.Errorf("itemType = %q, want journalArticle", data.ItemType)
	}
	if data.Title != "Deep Residual Learning for Image Recognition" {
		t.Errorf("title = %q", data.Title)
	}
	if data.PublicationTitle != "CVPR" {
		t.Errorf("publicationTitle = %q, want CVPR", data.PublicationTitle)
	}
	if data.DOI != "10.1109/CVPR.2016.90" {
		t.Errorf("DOI = %q", data.DOI)
	}
	if data.Date != "2016-06-27" {
		t.Errorf("date = %q, want 2016-06-27", data.Date)
	}
	if len(data.Creators) != 2 {
		t.Fatalf("expected 2 creators, got %d (%v)", len(data.Creators), data.Creators)
	}
	if data.Creators[0].FirstName != "Kaiming" || data.Creators[0].LastName != "He" {
		t.Errorf("creator[0] = %+v, want Kaiming He (from \"He, Kaiming\")", data.Creators[0])
	}
}

// Contract: a page without citation meta falls back to OpenGraph/<title> and
// resolves to a webpage, using og:url as the canonical URL. A blog post must
// still be addable even though it carries no scholarly metadata.
func TestResolveURLFallsBackToWebpage(t *testing.T) {
	client, _ := newTestClient(map[string]stubResponse{
		"example.com/post": {status: 200, body: htmlOpenGraph},
	})

	data, err := client.ResolveURL(context.Background(), "https://example.com/post")

	if err != nil {
		t.Fatalf("ResolveURL returned error: %v", err)
	}
	if data.ItemType != "webpage" {
		t.Errorf("itemType = %q, want webpage", data.ItemType)
	}
	if data.Title != "A Great Blog Post" {
		t.Errorf("title = %q, want og:title", data.Title)
	}
	if data.URL != "https://example.com/canonical" {
		t.Errorf("url = %q, want og:url", data.URL)
	}
	// A webpage has no publicationTitle/DOI field; those must stay empty so the
	// item is not rejected by Zotero as carrying invalid fields.
	if data.PublicationTitle != "" || data.DOI != "" {
		t.Errorf("webpage carried article-only fields: pub=%q doi=%q", data.PublicationTitle, data.DOI)
	}
}

// Contract: meta attributes are parsed regardless of order (content before
// name/property), because real pages emit them both ways and an order-sensitive
// parser would silently miss the title.
func TestResolveURLParsesReversedAttributeOrder(t *testing.T) {
	client, _ := newTestClient(map[string]stubResponse{
		"example.com": {status: 200, body: htmlAttrReversed},
	})

	data, err := client.ResolveURL(context.Background(), "https://example.com/x")

	if err != nil {
		t.Fatalf("ResolveURL returned error: %v", err)
	}
	if data.Title != "Reversed Attribute Order" {
		t.Errorf("title = %q, want it parsed despite reversed attribute order", data.Title)
	}
}

const htmlTitleWithGT = `<html><head>
<meta name="citation_title" content="Proofs that P > NP for restricted models">
<meta name="citation_journal_title" content="J. Fake Results">
</head></html>`

// Contract: a meta tag whose attribute value contains a literal '>' is parsed
// in full, not truncated at the first '>'. A quote-unaware scan would drop the
// title — often the only title source — making ResolveURL fail on a page it
// should resolve.
func TestResolveURLHandlesGreaterThanInAttribute(t *testing.T) {
	client, _ := newTestClient(map[string]stubResponse{
		"example.org": {status: 200, body: htmlTitleWithGT},
	})

	data, err := client.ResolveURL(context.Background(), "https://example.org/paper")

	if err != nil {
		t.Fatalf("ResolveURL returned error: %v", err)
	}
	if data.Title != "Proofs that P > NP for restricted models" {
		t.Errorf("title = %q, want the full title including '>'", data.Title)
	}
}

// Contract: when a page has no discernible title (no citation_title, og:title,
// or <title>), ResolveURL errors instead of creating a titleless item — an
// item with no title is useless and reads as a failure to a human or LLM.
func TestResolveURLNoTitleErrors(t *testing.T) {
	client, _ := newTestClient(map[string]stubResponse{
		"example.com": {status: 200, body: htmlNoMeta},
	})

	_, err := client.ResolveURL(context.Background(), "https://example.com/empty")

	if err == nil {
		t.Fatal("expected error for a page with no title, got nil")
	}
}

// Contract: the requested URL is fetched as given, so the resolver reads the
// page the user actually asked about.
func TestResolveURLRequestsTheGivenURL(t *testing.T) {
	client, st := newTestClient(map[string]stubResponse{
		"example.org/paper": {status: 200, body: htmlHighwire},
	})

	_, err := client.ResolveURL(context.Background(), "https://example.org/paper")

	if err != nil {
		t.Fatalf("ResolveURL returned error: %v", err)
	}
	if len(st.requests) != 1 || st.requests[0].String() != "https://example.org/paper" {
		t.Errorf("requests = %v, want a single GET of the given URL", st.requests)
	}
}
