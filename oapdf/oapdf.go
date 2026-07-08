// Package oapdf locates and downloads an open-access PDF for a resolved Zotero
// item, so `add --attach-pdf` can attach the paper's full text. It tries, in
// order: arXiv (arxiv.org/pdf/<id>), Unpaywall (DOI -> OA location), and the
// source page's citation_pdf_url meta tag, stopping at the first that yields a
// valid PDF.
//
// All HTTP dependencies are injectable (base URLs + *http.Client) so tests stub
// every source and run fully offline, mirroring the resolve/scholar packages.
package oapdf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/yk0817/zotero-cli/zotero"
)

// Defaults for the injectable base URLs.
const (
	DefaultArxivBaseURL     = "https://arxiv.org"
	DefaultUnpaywallBaseURL = "https://api.unpaywall.org/v2"
)

const (
	fetchTimeout = 60 * time.Second
	// dialTimeout bounds a single TCP connect (used by the guarded transport).
	dialTimeout = 15 * time.Second
	// maxPDFBytes caps a single download so a hostile or misbehaving server
	// cannot make the CLI buffer an unbounded body into memory.
	maxPDFBytes = 100 << 20 // 100 MiB
	// userAgent identifies the client to arXiv/Unpaywall/publishers, matching the
	// "polite" etiquette the resolve package already follows.
	userAgent = "zotero-cli (+https://github.com/yk0817/zotero-cli)"
	// pdfMagic is the required leading signature of a PDF file (per the PDF spec).
	// Validating it guards against attaching an HTML paywall/login page that a
	// server returned with a 200 in place of the real file.
	pdfMagic = "%PDF-"
)

// ErrNoOAPDF signals that no open-access PDF could be located for the item (no
// applicable source, or every source reports no OA copy) — as opposed to a
// transport/download failure. Callers use errors.Is to tell "no OA available"
// apart from "the download failed", though for `add` both are non-fatal: the
// item is still created and only the attachment is skipped.
var ErrNoOAPDF = errors.New("no open-access PDF found")

// PDF source names, surfaced in Result.Source and the CLI JSON output.
const (
	SourceArxiv       = "arxiv"
	SourceUnpaywall   = "unpaywall"
	SourceCitationPDF = "citation_pdf_url"
)

// Result is a downloaded OA PDF.
type Result struct {
	Content []byte // the PDF bytes
	URL     string // the URL the PDF was downloaded from
	Source  string // one of the Source* constants
}

// Fetcher locates and downloads OA PDFs. HTTPClient, ArxivBaseURL, and
// UnpaywallBaseURL are exported so tests inject a stub transport and point each
// source at a test host. Email is the Unpaywall contact address; when empty,
// Unpaywall is skipped (the API rejects requests without it).
type Fetcher struct {
	HTTPClient       *http.Client
	ArxivBaseURL     string
	UnpaywallBaseURL string
	Email            string
}

// NewFetcher returns a Fetcher pointed at the public arXiv/Unpaywall endpoints,
// with a bounded HTTP timeout and an SSRF-guarded transport (see
// guardedTransport). The guard lives only in this default client so injected
// test clients keep dialing nothing.
func NewFetcher(email string) *Fetcher {
	return &Fetcher{
		HTTPClient:       &http.Client{Timeout: fetchTimeout, Transport: guardedTransport()},
		ArxivBaseURL:     DefaultArxivBaseURL,
		UnpaywallBaseURL: DefaultUnpaywallBaseURL,
		Email:            email,
	}
}

func (f *Fetcher) httpClient() *http.Client {
	if f.HTTPClient != nil {
		return f.HTTPClient
	}
	return http.DefaultClient
}

// guardedTransport clones the stdlib default transport and blocks dials to
// internal addresses. Because PDF URLs come from remote data (Unpaywall's
// url_for_pdf, a scraped citation_pdf_url) — and because Go follows redirects to
// any host — an unguarded fetch is an SSRF vector: a hostile URL could point at
// cloud metadata (169.254.169.254), localhost services, or RFC1918 hosts. The
// net.Dialer.Control hook runs before every connect (initial request AND each
// redirect) and sees the concrete resolved IP, so it also closes the
// resolve-then-connect (DNS-rebinding) gap that a URL-string check would miss.
func guardedTransport() *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = (&net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: 30 * time.Second,
		Control:   blockInternalDial,
	}).DialContext
	return tr
}

// blockInternalDial is a net.Dialer.Control hook that refuses to connect to a
// non-global address. address is the already-resolved ip:port about to be
// dialed. It fails closed: an unparseable host is blocked.
func blockInternalDial(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("blocked dial to %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("blocked dial to non-IP host %q", host)
	}
	if isBlockedIP(ip) {
		return fmt.Errorf("blocked dial to internal address %s (SSRF guard)", ip)
	}
	return nil
}

// isBlockedIP reports whether ip is one the fetcher must never dial: loopback,
// private (RFC1918 / ULA), link-local unicast or multicast (covers cloud
// metadata at 169.254.169.254), or the unspecified address. Kept pure so it is
// unit-testable without a network.
func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}

// Fetch locates and downloads an OA PDF for the item, trying arXiv, then
// Unpaywall, then the source page's citation_pdf_url, and returning the first
// that yields a valid PDF. sourcePageURL is the page to scrape for
// citation_pdf_url (pass the item URL only when adding from a URL; "" disables
// that source).
//
// It returns ErrNoOAPDF when no source produces a PDF. If a source's PDF URL was
// found but the download or validation failed, it returns that failure (which is
// deliberately NOT ErrNoOAPDF) so the skip reason is diagnosable. Both outcomes
// are non-fatal for the caller.
func (f *Fetcher) Fetch(ctx context.Context, data zotero.ItemData, sourcePageURL string) (*Result, error) {
	var firstErr error
	note := func(err error) {
		if firstErr == nil && err != nil && !errors.Is(err, ErrNoOAPDF) {
			firstErr = err
		}
	}

	// 1. arXiv: direct PDF URL derived from the item's arXiv id.
	if id := zotero.ExtractArxivID(data); id != "" {
		res, err := f.downloadPDF(ctx, f.arxivPDFURL(id), SourceArxiv)
		if err == nil {
			return res, nil
		}
		note(err)
	}

	// 2. Unpaywall: DOI -> OA location. Requires an email; without one the API
	//    rejects the request, so we skip the source entirely rather than make a
	//    call guaranteed to fail.
	if data.DOI != "" && f.Email != "" {
		pdfURL, err := f.unpaywallPDFURL(ctx, data.DOI)
		if err != nil {
			note(err)
		} else if pdfURL != "" {
			res, err := f.downloadPDF(ctx, pdfURL, SourceUnpaywall)
			if err == nil {
				return res, nil
			}
			note(err)
		}
	}

	// 3. citation_pdf_url: scrape the source page (only when adding from a URL).
	if sourcePageURL != "" {
		pdfURL, err := f.citationPDFURL(ctx, sourcePageURL)
		if err != nil {
			note(err)
		} else if pdfURL != "" {
			res, err := f.downloadPDF(ctx, pdfURL, SourceCitationPDF)
			if err == nil {
				return res, nil
			}
			note(err)
		}
	}

	if firstErr != nil {
		return nil, firstErr
	}
	return nil, ErrNoOAPDF
}

// arxivPDFURL builds the direct PDF URL for an arXiv id (which may carry a
// version suffix, e.g. 2301.01234v2).
func (f *Fetcher) arxivPDFURL(id string) string {
	return strings.TrimRight(f.ArxivBaseURL, "/") + "/pdf/" + id
}

// unpaywallResponse is the subset of the Unpaywall record we consume.
type unpaywallResponse struct {
	IsOA           bool                `json:"is_oa"`
	BestOALocation *unpaywallLocation  `json:"best_oa_location"`
	OALocations    []unpaywallLocation `json:"oa_locations"`
}

type unpaywallLocation struct {
	URLForPDF string `json:"url_for_pdf"`
}

// unpaywallPDFURL queries Unpaywall for the DOI and returns the OA PDF URL, or
// "" if the item is not open access / has no direct PDF link. A 404 (DOI unknown
// to Unpaywall) is treated as "no OA" rather than an error.
func (f *Fetcher) unpaywallPDFURL(ctx context.Context, doi string) (string, error) {
	// Build the URL via url.URL so the DOI's slash stays literal in the path
	// (Unpaywall keys on the raw DOI) while any other special characters are
	// percent-encoded — the same idiom the Crossref resolver uses.
	base, err := url.Parse(f.UnpaywallBaseURL)
	if err != nil {
		return "", fmt.Errorf("unpaywall: invalid base URL %q: %w", f.UnpaywallBaseURL, err)
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimSpace(doi)
	base.RawQuery = url.Values{"email": {f.Email}}.Encode()
	reqURL := base.String()

	body, status, err := f.get(ctx, reqURL, "application/json")
	if err != nil {
		return "", err
	}
	if status == http.StatusNotFound {
		return "", nil
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("unpaywall: HTTP %d for DOI %q", status, doi)
	}

	var resp unpaywallResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("unpaywall: invalid response for DOI %q: %w", doi, err)
	}
	if !resp.IsOA {
		return "", nil
	}
	if resp.BestOALocation != nil {
		if u := strings.TrimSpace(resp.BestOALocation.URLForPDF); u != "" {
			return u, nil
		}
	}
	for _, loc := range resp.OALocations {
		if u := strings.TrimSpace(loc.URLForPDF); u != "" {
			return u, nil
		}
	}
	return "", nil
}

// citationMetaRE matches a <meta name="citation_pdf_url" content="..."> tag in
// either attribute order and with single or double quotes.
var citationMetaRE = regexp.MustCompile(
	`(?is)<meta\b[^>]*\bname\s*=\s*["']citation_pdf_url["'][^>]*\bcontent\s*=\s*["']([^"']+)["']` +
		`|<meta\b[^>]*\bcontent\s*=\s*["']([^"']+)["'][^>]*\bname\s*=\s*["']citation_pdf_url["']`)

// citationPDFURL fetches the source page and extracts its citation_pdf_url meta
// tag, returning "" when the page has none.
func (f *Fetcher) citationPDFURL(ctx context.Context, pageURL string) (string, error) {
	body, status, err := f.get(ctx, pageURL, "text/html")
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("citation_pdf_url: HTTP %d fetching %s", status, pageURL)
	}
	m := citationMetaRE.FindSubmatch(body)
	if m == nil {
		return "", nil
	}
	// Group 1 (content-after-name) or group 2 (content-before-name), whichever
	// matched.
	for _, g := range m[1:] {
		if len(g) > 0 {
			return strings.TrimSpace(string(g)), nil
		}
	}
	return "", nil
}

// downloadPDF fetches a candidate PDF URL and validates that the response really
// is a PDF (magic bytes) before accepting it.
func (f *Fetcher) downloadPDF(ctx context.Context, rawURL, source string) (*Result, error) {
	body, status, err := f.get(ctx, rawURL, "application/pdf")
	if err != nil {
		return nil, fmt.Errorf("%s: downloading %s: %w", source, rawURL, err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("%s: HTTP %d downloading %s", source, status, rawURL)
	}
	if !looksLikePDF(body) {
		return nil, fmt.Errorf("%s: response from %s is not a PDF (did not start with %q)", source, rawURL, pdfMagic)
	}
	return &Result{Content: body, URL: rawURL, Source: source}, nil
}

// looksLikePDF reports whether the body begins with the PDF signature, tolerating
// the leading whitespace some servers prepend.
func looksLikePDF(body []byte) bool {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	return bytes.HasPrefix(trimmed, []byte(pdfMagic))
}

// get performs a GET and returns the body (capped at maxPDFBytes), the HTTP
// status, and a transport/read error. A non-2xx status is NOT an error here so
// callers can branch on it (e.g. 404 -> "no OA"); only transport and read
// failures return err.
//
// Only http(s) URLs are fetched: the dial guard sees only TCP connects, so a
// non-network scheme (file://, gopher://, ...) must be rejected here before
// dispatch. Every URL placed in an error is redacted (query stripped) so the
// Unpaywall contact email never leaks into a message that surfaces as a skip
// reason on stderr or in --json.
func (f *Fetcher) get(ctx context.Context, rawURL, accept string) ([]byte, int, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid URL %q: %w", redactURL(rawURL), err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, 0, fmt.Errorf("refusing to fetch non-http(s) URL %q (scheme %q)", redactURL(rawURL), parsed.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := f.httpClient().Do(req)
	if err != nil {
		// client.Do returns a *url.Error whose message embeds the full request
		// URL (including the email query); unwrap it to the underlying cause so
		// only the redacted URL is shown.
		return nil, 0, fmt.Errorf("request to %s failed: %w", redactURL(rawURL), stripURLError(err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPDFBytes+1))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading response from %s: %w", redactURL(rawURL), err)
	}
	if len(body) > maxPDFBytes {
		return nil, resp.StatusCode, fmt.Errorf("response from %s exceeds %d bytes", redactURL(rawURL), maxPDFBytes)
	}
	return body, resp.StatusCode, nil
}

// stripURLError unwraps a *url.Error to its underlying cause. A *url.Error's own
// message includes the request URL verbatim (which may carry the email query),
// so wrapping the cause instead keeps that URL out of error text while
// preserving the errors.Is/As chain to the real failure.
func stripURLError(err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) && uerr.Err != nil {
		return uerr.Err
	}
	return err
}

// redactURL strips the query string from a URL so secrets carried there (the
// Unpaywall email) never appear in error text. The path is kept for diagnosis.
func redactURL(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil {
		u.RawQuery = ""
		return u.String()
	}
	if i := strings.IndexByte(rawURL, '?'); i >= 0 {
		return rawURL[:i]
	}
	return rawURL
}
