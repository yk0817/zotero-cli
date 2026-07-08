package oapdf

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yk0817/zotero-cli/zotero"
)

// pdfBytes is a minimal payload that passes the %PDF magic-byte check without
// being a real PDF (tests never parse it, only transport and validate it).
const pdfBytes = "%PDF-1.7\n%\xe2\xe3\xcf\xd3\nfake body\n%%EOF\n"

// cannedResp is a stubbed HTTP response keyed by URL substring.
type cannedResp struct {
	status      int
	body        string
	contentType string
}

// routeStub serves canned responses by URL substring and records every request
// URL in order, so a test can assert both the response handling and which
// sources were (not) contacted — all without a live network call.
type routeStub struct {
	routes   map[string]cannedResp
	requests []string
}

func (s *routeStub) RoundTrip(req *http.Request) (*http.Response, error) {
	s.requests = append(s.requests, req.URL.String())
	for frag, resp := range s.routes {
		if strings.Contains(req.URL.String(), frag) {
			h := http.Header{}
			if resp.contentType != "" {
				h.Set("Content-Type", resp.contentType)
			}
			return &http.Response{
				StatusCode: resp.status,
				Body:       io.NopCloser(strings.NewReader(resp.body)),
				Header:     h,
			}, nil
		}
	}
	return &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader("no matching route")),
		Header:     http.Header{},
	}, nil
}

// newTestFetcher builds a Fetcher whose HTTP calls are served by the stub, with
// distinct .test hosts per source so their URL substrings never collide.
func newTestFetcher(email string, routes map[string]cannedResp) (*Fetcher, *routeStub) {
	st := &routeStub{routes: routes}
	return &Fetcher{
		HTTPClient:       &http.Client{Transport: st},
		ArxivBaseURL:     "https://arxiv.test",
		UnpaywallBaseURL: "https://unpaywall.test/v2",
		Email:            email,
	}, st
}

func (s *routeStub) contacted(frag string) bool {
	for _, u := range s.requests {
		if strings.Contains(u, frag) {
			return true
		}
	}
	return false
}

// Contract: when the item is an arXiv paper (id recoverable from its URL), the
// fetcher downloads arxiv.org/pdf/<id> and reports the arXiv source. Getting the
// URL wrong or mislabelling the source would attach the wrong file / mislead the
// JSON consumer about provenance.
func TestFetchArxivReturnsPDF(t *testing.T) {
	f, st := newTestFetcher("", map[string]cannedResp{
		"arxiv.test/pdf/2301.01234": {status: 200, body: pdfBytes, contentType: "application/pdf"},
	})
	data := zotero.ItemData{ItemType: "preprint", URL: "https://arxiv.org/abs/2301.01234"}

	res, err := f.Fetch(context.Background(), data, "")

	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if res.Source != SourceArxiv {
		t.Errorf("source = %q, want %q", res.Source, SourceArxiv)
	}
	if string(res.Content) != pdfBytes {
		t.Errorf("content = %q, want the downloaded PDF bytes", res.Content)
	}
	if !strings.Contains(res.URL, "arxiv.test/pdf/2301.01234") {
		t.Errorf("url = %q, want the arXiv pdf URL", res.URL)
	}
	if !st.contacted("arxiv.test/pdf/2301.01234") {
		t.Errorf("expected a request to the arXiv pdf endpoint, got %v", st.requests)
	}
}

// Contract: for a DOI item, the fetcher asks Unpaywall for the OA location and
// downloads best_oa_location.url_for_pdf. The Unpaywall query MUST carry the
// contact email (the API rejects requests without it) and the DOI in the path.
func TestFetchUnpaywallReturnsPDF(t *testing.T) {
	unpaywallJSON := `{"is_oa":true,"best_oa_location":{"url_for_pdf":"https://oa.test/paper.pdf","url":"https://oa.test/paper"},"oa_locations":[]}`
	f, st := newTestFetcher("me@example.com", map[string]cannedResp{
		"unpaywall.test/v2/10.1/x": {status: 200, body: unpaywallJSON, contentType: "application/json"},
		"oa.test/paper.pdf":        {status: 200, body: pdfBytes, contentType: "application/pdf"},
	})
	data := zotero.ItemData{ItemType: "journalArticle", DOI: "10.1/x"}

	res, err := f.Fetch(context.Background(), data, "")

	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if res.Source != SourceUnpaywall {
		t.Errorf("source = %q, want %q", res.Source, SourceUnpaywall)
	}
	if string(res.Content) != pdfBytes {
		t.Errorf("content mismatch: %q", res.Content)
	}
	if !st.contacted("email=me%40example.com") {
		t.Errorf("Unpaywall request must include the email query param, got %v", st.requests)
	}
	if !st.contacted("unpaywall.test/v2/10.1/x") {
		t.Errorf("Unpaywall request must include the DOI in the path, got %v", st.requests)
	}
}

// Contract: Unpaywall falls back to the first oa_locations[] entry with a PDF
// URL when best_oa_location has none, so an OA copy is not missed just because
// the "best" location lacks a direct PDF link.
func TestFetchUnpaywallScansOALocations(t *testing.T) {
	unpaywallJSON := `{"is_oa":true,"best_oa_location":{"url_for_pdf":null,"url":"https://oa.test/landing"},"oa_locations":[{"url_for_pdf":"https://oa.test/backup.pdf"}]}`
	f, _ := newTestFetcher("me@example.com", map[string]cannedResp{
		"unpaywall.test/v2/10.1/x": {status: 200, body: unpaywallJSON, contentType: "application/json"},
		"oa.test/backup.pdf":       {status: 200, body: pdfBytes, contentType: "application/pdf"},
	})
	data := zotero.ItemData{DOI: "10.1/x"}

	res, err := f.Fetch(context.Background(), data, "")

	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if res.Source != SourceUnpaywall || string(res.Content) != pdfBytes {
		t.Errorf("expected OA-locations fallback PDF, got source=%q", res.Source)
	}
}

// Contract: when adding from a URL, the fetcher scrapes the source page's
// <meta name="citation_pdf_url"> and downloads that PDF. This is the only source
// for a page-added item with no arXiv id or DOI.
func TestFetchCitationPDFURLReturnsPDF(t *testing.T) {
	page := `<html><head><meta name="citation_pdf_url" content="https://journal.test/full.pdf"></head></html>`
	f, st := newTestFetcher("", map[string]cannedResp{
		"journal.test/article":  {status: 200, body: page, contentType: "text/html"},
		"journal.test/full.pdf": {status: 200, body: pdfBytes, contentType: "application/pdf"},
	})
	data := zotero.ItemData{ItemType: "journalArticle", URL: "https://journal.test/article"}

	res, err := f.Fetch(context.Background(), data, "https://journal.test/article")

	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if res.Source != SourceCitationPDF {
		t.Errorf("source = %q, want %q", res.Source, SourceCitationPDF)
	}
	if string(res.Content) != pdfBytes {
		t.Errorf("content mismatch: %q", res.Content)
	}
	if !st.contacted("journal.test/full.pdf") {
		t.Errorf("expected the citation_pdf_url to be downloaded, got %v", st.requests)
	}
}

// Contract: with no arXiv id, no DOI, and no source page, there is no OA source
// to try, so Fetch returns ErrNoOAPDF (not a generic error) so the caller can
// report an accurate "no open-access PDF" skip reason.
func TestFetchReturnsErrNoOAPDFWhenNoSource(t *testing.T) {
	f, _ := newTestFetcher("me@example.com", map[string]cannedResp{})
	data := zotero.ItemData{ItemType: "book", Title: "A Book"}

	res, err := f.Fetch(context.Background(), data, "")

	if res != nil {
		t.Fatalf("expected nil result, got %+v", res)
	}
	if !errors.Is(err, ErrNoOAPDF) {
		t.Fatalf("expected ErrNoOAPDF, got %v", err)
	}
}

// Contract: an item Unpaywall reports as not open-access (or with no PDF URL)
// yields ErrNoOAPDF rather than attaching nothing silently or erroring hard.
func TestFetchUnpaywallNoOAReturnsErrNoOAPDF(t *testing.T) {
	f, _ := newTestFetcher("me@example.com", map[string]cannedResp{
		"unpaywall.test/v2/10.1/closed": {status: 200, body: `{"is_oa":false,"best_oa_location":null,"oa_locations":[]}`, contentType: "application/json"},
	})
	data := zotero.ItemData{DOI: "10.1/closed"}

	_, err := f.Fetch(context.Background(), data, "")

	if !errors.Is(err, ErrNoOAPDF) {
		t.Fatalf("expected ErrNoOAPDF for a closed-access DOI, got %v", err)
	}
}

// Contract: a located PDF URL that fails to download (5xx) is a real download
// error, NOT ErrNoOAPDF — the caller still skips the attachment, but the error
// carries the status so the failure is diagnosable and not confused with
// "no OA available".
func TestFetchDownloadFailureIsNotNoOAPDF(t *testing.T) {
	f, _ := newTestFetcher("", map[string]cannedResp{
		"arxiv.test/pdf/2301.01234": {status: 500, body: "boom", contentType: "text/plain"},
	})
	data := zotero.ItemData{URL: "https://arxiv.org/abs/2301.01234"}

	res, err := f.Fetch(context.Background(), data, "")

	if res != nil {
		t.Fatalf("expected nil result on download failure, got %+v", res)
	}
	if err == nil {
		t.Fatal("expected an error on download failure")
	}
	if errors.Is(err, ErrNoOAPDF) {
		t.Error("a 5xx download failure must not be reported as ErrNoOAPDF")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should carry the status code, got %v", err)
	}
}

// Contract: a 200 response that is not actually a PDF (e.g. an HTML paywall /
// login page served in place of the file) is rejected, so the library is never
// polluted with an HTML file masquerading as a paper PDF.
func TestFetchRejectsNonPDFResponse(t *testing.T) {
	f, _ := newTestFetcher("", map[string]cannedResp{
		"arxiv.test/pdf/2301.01234": {status: 200, body: "<html><body>Please log in</body></html>", contentType: "text/html"},
	})
	data := zotero.ItemData{URL: "https://arxiv.org/abs/2301.01234"}

	res, err := f.Fetch(context.Background(), data, "")

	if res != nil {
		t.Fatalf("expected nil result for a non-PDF body, got %+v", res)
	}
	if err == nil || errors.Is(err, ErrNoOAPDF) {
		t.Fatalf("expected a non-nil, non-ErrNoOAPDF error, got %v", err)
	}
	if !strings.Contains(err.Error(), "PDF") {
		t.Errorf("error should explain the body is not a PDF, got %v", err)
	}
}

// Contract: Unpaywall requires a contact email, so with none configured the
// fetcher does NOT contact Unpaywall at all (avoiding a guaranteed-rejected
// request) and treats the DOI source as unavailable.
func TestFetchSkipsUnpaywallWhenEmailMissing(t *testing.T) {
	f, st := newTestFetcher("", map[string]cannedResp{
		"unpaywall.test": {status: 200, body: `{"is_oa":true,"best_oa_location":{"url_for_pdf":"https://oa.test/p.pdf"}}`, contentType: "application/json"},
	})
	data := zotero.ItemData{DOI: "10.1/x"}

	_, err := f.Fetch(context.Background(), data, "")

	if st.contacted("unpaywall.test") {
		t.Errorf("Unpaywall must not be contacted without an email, got %v", st.requests)
	}
	if !errors.Is(err, ErrNoOAPDF) {
		t.Fatalf("expected ErrNoOAPDF when the only source (Unpaywall) is unusable, got %v", err)
	}
}

// Contract: arXiv is tried before Unpaywall and the fetcher stops at the first
// success, so an arXiv paper that also has a DOI does not trigger a needless
// (and slower) Unpaywall lookup.
func TestFetchPrefersArxivAndShortCircuits(t *testing.T) {
	f, st := newTestFetcher("me@example.com", map[string]cannedResp{
		"arxiv.test/pdf/2301.01234": {status: 200, body: pdfBytes, contentType: "application/pdf"},
		"unpaywall.test":            {status: 200, body: pdfBytes, contentType: "application/json"},
	})
	data := zotero.ItemData{URL: "https://arxiv.org/abs/2301.01234", DOI: "10.48550/arXiv.2301.01234"}

	res, err := f.Fetch(context.Background(), data, "")

	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if res.Source != SourceArxiv {
		t.Errorf("source = %q, want arXiv (tried first)", res.Source)
	}
	if st.contacted("unpaywall.test") {
		t.Errorf("Unpaywall must not be contacted once arXiv succeeds, got %v", st.requests)
	}
}

// errTransport always fails the round trip, simulating a transport-level error
// (e.g. connection refused) so a test can inspect the error text get() produces.
type errTransport struct{}

func (errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("dial tcp 203.0.113.7:443: connect: connection refused")
}

// Contract: isBlockedIP is the SSRF allow/deny decision. Loopback, private,
// link-local (unicast + the 169.254.169.254 cloud-metadata range) and the
// unspecified address must be blocked; ordinary public addresses must be
// allowed. A wrong verdict here either lets an attacker probe internal hosts or
// breaks all real fetches.
func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{name: "ipv4 loopback", ip: "127.0.0.1", want: true},
		{name: "ipv6 loopback", ip: "::1", want: true},
		{name: "private 10/8", ip: "10.1.2.3", want: true},
		{name: "private 172.16/12", ip: "172.16.5.4", want: true},
		{name: "private 192.168/16", ip: "192.168.0.1", want: true},
		{name: "link-local / cloud metadata", ip: "169.254.169.254", want: true},
		{name: "ipv6 link-local unicast", ip: "fe80::1", want: true},
		{name: "ipv6 link-local multicast", ip: "ff02::1", want: true},
		{name: "unique local ipv6", ip: "fd00::1", want: true},
		{name: "unspecified v4", ip: "0.0.0.0", want: true},
		{name: "unspecified v6", ip: "::", want: true},
		{name: "ipv4-mapped loopback", ip: "::ffff:127.0.0.1", want: true},
		{name: "public dns 1.1.1.1", ip: "1.1.1.1", want: false},
		{name: "public dns 8.8.8.8", ip: "8.8.8.8", want: false},
		{name: "public ipv6", ip: "2606:4700:4700::1111", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("bad test IP %q", tt.ip)
			}
			if got := isBlockedIP(ip); got != tt.want {
				t.Errorf("isBlockedIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

// Contract: the default (production) fetcher refuses to connect to an internal
// host even when a source URL points there. An httptest server listens on
// 127.0.0.1, so pointing arXiv at it exercises the real dial guard: the request
// must be blocked before the server's handler runs, and nothing is attached.
func TestDefaultFetcherRefusesInternalHost(t *testing.T) {
	handlerHit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerHit = true
		w.Write([]byte(pdfBytes))
	}))
	defer srv.Close()

	f := NewFetcher("") // the guarded default client, not a stub
	f.ArxivBaseURL = srv.URL
	data := zotero.ItemData{URL: "https://arxiv.org/abs/2301.01234"}

	res, err := f.Fetch(context.Background(), data, "")

	if res != nil {
		t.Fatalf("expected no result when the only source is an internal host, got %+v", res)
	}
	if err == nil || errors.Is(err, ErrNoOAPDF) {
		t.Fatalf("expected a hard block error, got %v", err)
	}
	if !strings.Contains(err.Error(), "SSRF") && !strings.Contains(err.Error(), "blocked") {
		t.Errorf("error should explain the internal-host block, got %v", err)
	}
	if handlerHit {
		t.Error("the request reached the internal server; the dial guard did not block it")
	}
}

// Contract: a non-http(s) scheme (here delivered via a hostile citation_pdf_url)
// is refused before any dispatch, so the fetcher cannot be tricked into reading
// a local file:// path. The dial guard only sees TCP connects, so this must be
// caught at the URL level.
func TestFetchRejectsNonHTTPScheme(t *testing.T) {
	page := `<html><head><meta name="citation_pdf_url" content="file:///etc/passwd"></head></html>`
	f, _ := newTestFetcher("", map[string]cannedResp{
		"journal.test/article": {status: 200, body: page, contentType: "text/html"},
	})
	data := zotero.ItemData{URL: "https://journal.test/article"}

	res, err := f.Fetch(context.Background(), data, "https://journal.test/article")

	if res != nil {
		t.Fatalf("expected no result for a file:// PDF URL, got %+v", res)
	}
	if err == nil || errors.Is(err, ErrNoOAPDF) {
		t.Fatalf("expected a scheme-rejection error, got %v", err)
	}
	if !strings.Contains(err.Error(), "file") || !strings.Contains(err.Error(), "scheme") {
		t.Errorf("error should name the rejected scheme, got %v", err)
	}
}

// Contract: the Unpaywall contact email is a query parameter, so a fetch failure
// must NOT echo it in the error (which becomes a skip reason printed to stderr
// and included in --json). The path is kept for diagnosis; the email is not.
func TestFetchDoesNotLeakEmailOnUnpaywallFailure(t *testing.T) {
	f := &Fetcher{
		HTTPClient:       &http.Client{Transport: errTransport{}},
		UnpaywallBaseURL: "https://unpaywall.test/v2",
		Email:            "secret-leak@example.com",
	}
	data := zotero.ItemData{DOI: "10.1/x"}

	_, err := f.Fetch(context.Background(), data, "")

	if err == nil {
		t.Fatal("expected an error from the failed Unpaywall fetch")
	}
	if strings.Contains(err.Error(), "secret-leak") || strings.Contains(err.Error(), "email=") {
		t.Errorf("error leaked the contact email: %v", err)
	}
	if !strings.Contains(err.Error(), "unpaywall.test/v2/10.1/x") {
		t.Errorf("error should keep the redacted path for diagnosis, got %v", err)
	}
}

// Contract: redactURL removes the query string (which carries the email) while
// preserving scheme/host/path, and degrades gracefully on an unparseable URL.
func TestRedactURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "strips email query", in: "https://api.unpaywall.org/v2/10.1/x?email=me@example.com", want: "https://api.unpaywall.org/v2/10.1/x"},
		{name: "no query unchanged", in: "https://arxiv.org/pdf/2301.01234", want: "https://arxiv.org/pdf/2301.01234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactURL(tt.in); got != tt.want {
				t.Errorf("redactURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
