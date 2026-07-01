package resolve

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const arxivAttention = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>http://arxiv.org/abs/1706.03762v7</id>
    <title>Attention Is All
      You Need</title>
    <summary>  The dominant sequence transduction models are based on complex
      recurrent networks.  </summary>
    <published>2017-06-12T17:57:34Z</published>
    <author><name>Ashish Vaswani</name></author>
    <author><name>Noam Shazeer</name></author>
  </entry>
</feed>`

const arxivEmpty = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"></feed>`

const arxivErrorEntry = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>http://arxiv.org/api/errors#incorrect_id_format_for_9999.99999</id>
    <title>Error</title>
    <summary>incorrect id format for 9999.99999</summary>
  </entry>
</feed>`

// Contract: ResolveArXiv parses the arXiv Atom feed into a preprint item —
// whitespace-collapsed title, authors split into first/last, an ISO date from
// the published timestamp, and the abstract — so an arXiv ID yields a complete
// preprint record rather than a bare title.
func TestResolveArXivParsesMetadata(t *testing.T) {
	client, _ := newTestClient(map[string]stubResponse{
		"export.arxiv.org": {status: 200, body: arxivAttention},
	})

	data, err := client.ResolveArXiv(context.Background(), "1706.03762")

	if err != nil {
		t.Fatalf("ResolveArXiv returned error: %v", err)
	}
	if data.ItemType != "preprint" {
		t.Errorf("itemType = %q, want preprint", data.ItemType)
	}
	if data.Title != "Attention Is All You Need" {
		t.Errorf("title = %q (whitespace should be collapsed)", data.Title)
	}
	if data.Date != "2017-06-12" {
		t.Errorf("date = %q, want 2017-06-12", data.Date)
	}
	if !strings.HasPrefix(data.AbstractNote, "The dominant sequence") {
		t.Errorf("abstract = %q", data.AbstractNote)
	}
	if len(data.Creators) != 2 {
		t.Fatalf("expected 2 creators, got %d (%v)", len(data.Creators), data.Creators)
	}
	if data.Creators[0].FirstName != "Ashish" || data.Creators[0].LastName != "Vaswani" {
		t.Errorf("creator[0] = %+v, want Ashish Vaswani", data.Creators[0])
	}
}

// Contract: an arXiv item carries both a canonical abs URL and the arXiv DOI
// (10.48550/arXiv.<id>, version-stripped), so a later `add` of the same paper
// can detect the duplicate by either the URL (via the citations command's
// arXiv-ID matcher) or the DOI.
func TestResolveArXivSetsURLAndArxivDOI(t *testing.T) {
	client, _ := newTestClient(map[string]stubResponse{
		"export.arxiv.org": {status: 200, body: arxivAttention},
	})

	data, err := client.ResolveArXiv(context.Background(), "1706.03762")

	if err != nil {
		t.Fatalf("ResolveArXiv returned error: %v", err)
	}
	if !strings.Contains(data.URL, "arxiv.org/abs/1706.03762") {
		t.Errorf("url = %q, want an arxiv abs URL", data.URL)
	}
	if data.DOI != "10.48550/arXiv.1706.03762" {
		t.Errorf("DOI = %q, want 10.48550/arXiv.1706.03762", data.DOI)
	}
}

// Contract: the request goes to the arXiv query API with the id in id_list, so
// the resolver asks for exactly the requested preprint.
func TestResolveArXivRequestsQueryPath(t *testing.T) {
	client, st := newTestClient(map[string]stubResponse{
		"export.arxiv.org": {status: 200, body: arxivAttention},
	})

	_, err := client.ResolveArXiv(context.Background(), "1706.03762")

	if err != nil {
		t.Fatalf("ResolveArXiv returned error: %v", err)
	}
	if len(st.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(st.requests))
	}
	got := st.requests[0].String()
	if !strings.Contains(got, "export.arxiv.org/api/query") || !strings.Contains(got, "id_list=1706.03762") {
		t.Errorf("request URL = %q, want the arXiv query API with id_list", got)
	}
}

// Contract: an id arXiv does not know is reported as not-found. arXiv answers a
// bad id with either an empty feed or an "Error" entry rather than an HTTP
// error, so both shapes must map to ErrNotFound — otherwise `add` would create
// an item titled "Error".
func TestResolveArXivNotFound(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty feed", body: arxivEmpty},
		{name: "error entry", body: arxivErrorEntry},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := newTestClient(map[string]stubResponse{
				"export.arxiv.org": {status: 200, body: tt.body},
			})

			_, err := client.ResolveArXiv(context.Background(), "9999.99999")

			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("expected ErrNotFound, got %v", err)
			}
		})
	}
}
