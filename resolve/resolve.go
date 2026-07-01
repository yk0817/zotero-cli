// Package resolve turns an external identifier (DOI, arXiv ID, ISBN, or URL)
// into Zotero item metadata, so the `add` command can create a library item
// without the user typing the bibliographic details by hand.
//
// Each source has its own resolver method on Client (ResolveDOI via Crossref,
// ResolveArXiv via the arXiv API, ResolveISBN via OpenLibrary, ResolveURL via
// embedded page metadata). Client.HTTPClient is exported so tests inject a stub
// transport and run fully offline.
package resolve

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/yk0817/zotero-cli/zotero"
)

const (
	resolveTimeout = 20 * time.Second
	// userAgent identifies the client to Crossref/arXiv/OpenLibrary, which ask
	// callers to be identifiable (Crossref's "polite pool" etiquette).
	userAgent = "zotero-cli (+https://github.com/yk0817/zotero-cli)"
)

// ErrNotFound signals that a source does not know the identifier (e.g. an HTTP
// 404 or an empty result), as opposed to a transport or parse failure. Callers
// use errors.Is to tell "you typed a wrong identifier" apart from "the lookup
// service is down".
var ErrNotFound = errors.New("identifier not found")

// Client resolves identifiers to Zotero item metadata.
type Client struct {
	HTTPClient *http.Client
}

// NewClient returns a Client with a bounded HTTP timeout.
func NewClient() *Client {
	return &Client{HTTPClient: &http.Client{Timeout: resolveTimeout}}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// get performs a GET and returns the body, translating a 404 into ErrNotFound
// and any other non-2xx into an error so callers can distinguish an unknown
// identifier from an API/transport failure.
func (c *Client) get(ctx context.Context, rawURL, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", rawURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response from %s: %w", rawURL, err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, rawURL)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("resolver API error (HTTP %d) from %s: %s", resp.StatusCode, rawURL, snippet(body))
	}
	return body, nil
}

// snippet trims a response body to a short single line for error messages.
func snippet(body []byte) string {
	s := collapseWS(string(body))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

var markupTag = regexp.MustCompile(`<[^>]+>`)

// stripMarkup removes XML/HTML tags and collapses whitespace, turning a JATS
// or HTML fragment (e.g. a Crossref abstract) into plain text.
func stripMarkup(s string) string {
	s = markupTag.ReplaceAllString(s, "")
	return collapseWS(html.UnescapeString(s))
}

// collapseWS collapses all runs of whitespace to single spaces and trims.
func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// firstNonEmpty returns the first non-empty string in the slice, trimmed.
func firstNonEmpty(values []string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return collapseWS(trimmed)
		}
	}
	return ""
}

// personCreator splits a full name into an author creator using the last
// whitespace-separated token as the family name. Sources that give a single
// name string (arXiv, OpenLibrary) go through here; sources with structured
// given/family names (Crossref) do not.
func personCreator(full string) zotero.Creator {
	full = collapseWS(full)
	if full == "" {
		return zotero.Creator{}
	}
	if idx := strings.LastIndex(full, " "); idx >= 0 {
		return zotero.Creator{CreatorType: "author", FirstName: full[:idx], LastName: full[idx+1:]}
	}
	return zotero.Creator{CreatorType: "author", LastName: full}
}
