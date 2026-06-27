// Package scholar is a minimal client for the Semantic Scholar Graph API,
// used to walk a paper's citation network (references it cites, and papers
// that cite it). Zotero has no citation graph of its own, so this fills that
// gap. The API works without a key on a low-rate tier; a key (read from
// SEMANTIC_SCHOLAR_API_KEY) merely raises the limit and is never logged.
package scholar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// DefaultBaseURL is the Semantic Scholar Graph API host.
const DefaultBaseURL = "https://api.semanticscholar.org"

// paperFields is the field selection requested for each cited/citing paper.
// CorpusId is deliberately excluded from externalIds handling (it is numeric)
// by decoding externalIds into the ExternalIDs struct, which ignores it.
const paperFields = "title,year,authors,citationCount,externalIds,abstract,venue"

// DefaultLimit caps how many references/citations are returned when the caller
// does not specify one.
const DefaultLimit = 25

// maxPageSize is the Graph API's per-request cap for the references/citations
// endpoints; larger results are walked via the offset/next cursor.
const maxPageSize = 100

// requestTimeout bounds a single HTTP call so a hung connection cannot stall
// the CLI indefinitely.
const requestTimeout = 30 * time.Second

// ErrPaperNotFound is returned by ResolvePaperID when none of DOI, arXiv ID, or
// title can be matched to a Semantic Scholar paper. Callers distinguish this
// from a transport/API error to report NOT_FOUND rather than API_ERROR.
var ErrPaperNotFound = errors.New("paper not found on Semantic Scholar")

// Client talks to the Semantic Scholar Graph API. HTTPClient is exported so a
// test can inject a stub RoundTripper (the same injection pattern the zotero
// package uses), and BaseURL so it can point at an httptest server.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// NewClient builds a client pointed at the public API, reading an optional key
// from SEMANTIC_SCHOLAR_API_KEY. The key value is only ever placed in the
// x-api-key request header — never logged or returned in errors.
func NewClient() *Client {
	return &Client{
		BaseURL:    DefaultBaseURL,
		APIKey:     os.Getenv("SEMANTIC_SCHOLAR_API_KEY"),
		HTTPClient: &http.Client{Timeout: requestTimeout},
	}
}

// Author is a paper author as returned by the Graph API.
type Author struct {
	AuthorID string `json:"authorId"`
	Name     string `json:"name"`
}

// ExternalIDs captures the two cross-references useful for downstream linking.
// Other keys the API returns (CorpusId, MAG, ACL, …) are intentionally ignored;
// CorpusId in particular is numeric and would break a map[string]string decode.
type ExternalIDs struct {
	DOI   string `json:"DOI"`
	ArXiv string `json:"ArXiv"`
}

// PaperRef is one node in the citation network.
type PaperRef struct {
	PaperID       string      `json:"paperId"`
	Title         string      `json:"title"`
	Year          int         `json:"year"`
	Authors       []Author    `json:"authors"`
	CitationCount int         `json:"citationCount"`
	ExternalIDs   ExternalIDs `json:"externalIds"`
	Abstract      string      `json:"abstract"`
	Venue         string      `json:"venue"`
}

// get issues a GET and returns the body and status. A non-2xx is not an error
// here (the caller decides whether, e.g., a 404 is "not found" or fatal); only
// transport/read failures produce err.
func (c *Client) get(ctx context.Context, path string) (body []byte, status int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, 0, err
	}
	if c.APIKey != "" {
		req.Header.Set("x-api-key", c.APIKey)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("Semantic Scholar request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read Semantic Scholar response: %w", err)
	}
	return body, resp.StatusCode, nil
}

// ResolvePaperID maps a Zotero item's identifiers to a Semantic Scholar paperId.
// It tries DOI, then arXiv ID, then a title search — stopping at the first hit.
// A missing identifier is skipped; an API/transport error aborts immediately
// (so a 500 is never mistaken for "not found"). Returns ErrPaperNotFound when
// every available identifier fails to match.
func (c *Client) ResolvePaperID(ctx context.Context, doi, arxivID, title string) (string, error) {
	candidates := []string{}
	if doi != "" {
		candidates = append(candidates, "DOI:"+doi)
	}
	if arxivID != "" {
		candidates = append(candidates, "ARXIV:"+arxivID)
	}
	for _, cand := range candidates {
		id, found, err := c.lookupPaperID(ctx, cand)
		if err != nil {
			return "", err
		}
		if found {
			return id, nil
		}
	}

	if title != "" {
		id, found, err := c.searchPaperID(ctx, title)
		if err != nil {
			return "", err
		}
		if found {
			return id, nil
		}
	}
	return "", ErrPaperNotFound
}

// identifierNotResolved reports whether status means the identifier was bad or
// unknown — not a transport fault or server error. 400 (malformed), 404
// (unknown), and 422 (unprocessable/unregistered DOI) all mean "this
// identifier did not resolve"; auth (401/403), rate-limit (429), and all 5xx
// are hard errors that must abort the resolution chain.
func identifierNotResolved(status int) bool {
	return status == http.StatusBadRequest ||
		status == http.StatusNotFound ||
		status == http.StatusUnprocessableEntity
}

// lookupPaperID resolves a single prefixed id (e.g. "DOI:..."/"ARXIV:...") to a
// canonical paperId. 400, 404, and 422 all mean "no such paper" (found=false,
// no error) so resolution falls through to the next candidate; any other
// non-200 is a propagated error.
func (c *Client) lookupPaperID(ctx context.Context, id string) (string, bool, error) {
	params := url.Values{}
	params.Set("fields", "paperId")
	body, status, err := c.get(ctx, "/graph/v1/paper/"+id+"?"+params.Encode())
	if err != nil {
		return "", false, err
	}
	if identifierNotResolved(status) {
		return "", false, nil
	}
	if status != http.StatusOK {
		return "", false, apiError(status, body)
	}
	var p struct {
		PaperID string `json:"paperId"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return "", false, fmt.Errorf("failed to parse Semantic Scholar paper: %w", err)
	}
	if p.PaperID == "" {
		return "", false, nil
	}
	return p.PaperID, true, nil
}

// searchPaperID falls back to a title search, returning the first candidate
// whose title actually matches the query. An empty/unmatched result set means
// "not found" (found=false), not an error.
//
// why: the search endpoint always returns *something* for any non-empty query,
// so blindly taking the top hit silently misidentifies the paper for
// generic/short titles (e.g. "Introduction" → some unrelated "Introduction to
// …") and the entire citation network is then built for the WRONG paper, with
// found=true and no error. A silent misidentification is worse than a visible
// failure, so we request several candidates and accept one only if its
// normalized title token-set matches the query (titleMatches); otherwise we
// report not-found and let ResolvePaperID end in ErrPaperNotFound.
func (c *Client) searchPaperID(ctx context.Context, title string) (string, bool, error) {
	params := url.Values{}
	params.Set("query", title)
	params.Set("limit", "5")
	params.Set("fields", "paperId,title")
	body, status, err := c.get(ctx, "/graph/v1/paper/search?"+params.Encode())
	if err != nil {
		return "", false, err
	}
	if status != http.StatusOK {
		return "", false, apiError(status, body)
	}
	var resp struct {
		Data []struct {
			PaperID string `json:"paperId"`
			Title   string `json:"title"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", false, fmt.Errorf("failed to parse Semantic Scholar search: %w", err)
	}
	for _, hit := range resp.Data {
		if hit.PaperID == "" {
			continue
		}
		if titleMatches(title, hit.Title) {
			return hit.PaperID, true, nil
		}
	}
	return "", false, nil
}

// minTitleMatch is the Jaccard-similarity floor for accepting a search hit as
// the same paper as the query. 0.6 is chosen as a balance: high enough to reject
// unrelated papers that merely share a generic word (query "Introduction" vs
// "Introduction to Quantum Computing" scores ~0.25), yet low enough to tolerate
// a trailing subtitle or a couple of extra/reordered words once both titles are
// normalized (case- and punctuation-insensitive).
const minTitleMatch = 0.6

// normalizeTitle lowercases s and collapses every run of non-alphanumeric
// characters into a single space, trimming the result, so title comparison is
// insensitive to case and punctuation ("Attention Is All You Need." and
// "attention is all you need" normalize identically). Letter/digit membership
// uses unicode classification so non-ASCII titles are handled too.
func normalizeTitle(s string) string {
	var b strings.Builder
	prevSpace := true // leading separators must not produce a leading space
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			prevSpace = false
			continue
		}
		if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// titleMatches reports whether candidate names the same paper as query. It
// accepts when the normalized titles are identical, or when the Jaccard
// similarity (|A∩B|/|A∪B|) of their normalized token sets reaches
// minTitleMatch. Token-set (rather than substring) matching tolerates a few
// reordered/added words while still rejecting titles that only share a generic
// word — the guard against silent misidentification.
func titleMatches(query, candidate string) bool {
	nq := normalizeTitle(query)
	nc := normalizeTitle(candidate)
	if nq == "" || nc == "" {
		return false
	}
	if nq == nc {
		return true
	}

	qset := tokenSet(nq)
	cset := tokenSet(nc)
	intersection := 0
	for tok := range qset {
		if _, ok := cset[tok]; ok {
			intersection++
		}
	}
	union := len(qset) + len(cset) - intersection
	if union == 0 {
		return false
	}
	return float64(intersection)/float64(union) >= minTitleMatch
}

// tokenSet splits an already-normalized title into its set of distinct tokens.
func tokenSet(normalized string) map[string]struct{} {
	tokens := strings.Fields(normalized)
	set := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		set[t] = struct{}{}
	}
	return set
}

// References returns the papers that id cites (backward edges), in the order the
// API lists them (the paper's own reference order), following pagination up to
// limit. limit<=0 falls back to DefaultLimit.
func (c *Client) References(ctx context.Context, id string, limit int) ([]PaperRef, error) {
	return c.paged(ctx, id, "references", "citedPaper", limit)
}

// Citations returns the papers that cite id (forward edges), following
// pagination up to limit. Ordering is left to the caller (the CLI sorts these
// by citation count). limit<=0 falls back to DefaultLimit.
func (c *Client) Citations(ctx context.Context, id string, limit int) ([]PaperRef, error) {
	return c.paged(ctx, id, "citations", "citingPaper", limit)
}

// paged walks the references/citations cursor. endpoint is the path segment
// ("references"/"citations") and field is the wrapper key each row nests the
// paper under ("citedPaper"/"citingPaper"). A row whose paper is null (the
// neighbour is not in Semantic Scholar) is skipped rather than emitted as an
// empty PaperRef. Always returns a non-nil slice so an empty network is
// "[]" (a normal state), never null.
func (c *Client) paged(ctx context.Context, id, endpoint, field string, limit int) ([]PaperRef, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	out := []PaperRef{}
	offset := 0
	for len(out) < limit {
		pageSize := limit - len(out)
		if pageSize > maxPageSize {
			pageSize = maxPageSize
		}

		params := url.Values{}
		params.Set("fields", paperFields)
		params.Set("offset", strconv.Itoa(offset))
		params.Set("limit", strconv.Itoa(pageSize))
		path := fmt.Sprintf("/graph/v1/paper/%s/%s?%s", id, endpoint, params.Encode())

		body, status, err := c.get(ctx, path)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, apiError(status, body)
		}

		var resp struct {
			Next *int                         `json:"next"`
			Data []map[string]json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse Semantic Scholar %s: %w", endpoint, err)
		}

		before := len(out)
		for _, row := range resp.Data {
			raw, ok := row[field]
			if !ok {
				continue
			}
			var ref PaperRef
			if err := json.Unmarshal(raw, &ref); err != nil {
				return nil, fmt.Errorf("failed to parse Semantic Scholar %s entry: %w", endpoint, err)
			}
			if ref.PaperID == "" && ref.Title == "" {
				continue // neighbour not in the S2 graph (null paper)
			}
			out = append(out, ref)
			if len(out) >= limit {
				break
			}
		}

		if resp.Next == nil || len(resp.Data) == 0 {
			break
		}
		// A non-empty page that yielded no usable row (every neighbour was a
		// null paper) must end the walk: advancing the cursor would keep fetching
		// pages of unindexed papers, up to ~limit/pageSize requests each able to
		// block for requestTimeout, so the CLI looks frozen.
		if len(out) == before {
			break
		}
		offset = *resp.Next
	}
	return out, nil
}

// maxErrorBodyLen caps the body snippet in error messages. Large outage pages
// (multi-KB HTML) from Semantic Scholar would otherwise flood LLM-agent token
// budgets and pollute logs; the status code already identifies the failure.
const maxErrorBodyLen = 200

// apiError formats a non-2xx response, including the status code so callers and
// tests can assert on it.
func apiError(status int, body []byte) error {
	return fmt.Errorf("Semantic Scholar API error (HTTP %d): %.*s", status, maxErrorBodyLen, body)
}
