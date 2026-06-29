package scholar

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// Contract: a 400 from the DOI lookup (malformed/unregistered DOI) is treated
// as "identifier did not resolve" so resolution falls through to the arXiv
// candidate. Without this fix, a bad DOI aborts the entire chain even when a
// valid arXiv ID is available — violating the DOI→arXiv→title fallback
// promise in the ResolvePaperID doc comment.
func TestResolvePaperIDFallsBackWhenDOIReturns400(t *testing.T) {
	doi := "10.invalid/bad"
	routes := map[string][]stubResponse{
		"/graph/v1/paper/DOI:" + doi:       {{status: http.StatusBadRequest, body: `{"message":"bad DOI"}`}},
		"/graph/v1/paper/ARXIV:2301.00001": ok(paperID("S2ARXIV400")),
	}
	client, stub := newStubClient(routes)

	id, err := client.ResolvePaperID(context.Background(), doi, "2301.00001", "")

	if err != nil {
		t.Fatalf("ResolvePaperID returned error: %v", err)
	}
	if id != "S2ARXIV400" {
		t.Errorf("expected paperId S2ARXIV400 (from arXiv fallback), got %q", id)
	}
	var hitArxiv bool
	for _, u := range stub.requests {
		if strings.Contains(u.Path, "ARXIV:") {
			hitArxiv = true
		}
	}
	if !hitArxiv {
		t.Error("expected the arXiv lookup route to be hit after the DOI 400, but it was not")
	}
}

// Contract: a 422 from the DOI lookup (syntactically valid but unregistered
// DOI) is treated as "identifier did not resolve" and resolution falls through
// to the arXiv candidate. Semantic Scholar returns 422 for DOIs that pass
// format checks but have no registered paper; this must not abort the chain
// when another identifier is available.
func TestResolvePaperIDFallsBackWhenDOIReturns422(t *testing.T) {
	doi := "10.9999/unregistered"
	routes := map[string][]stubResponse{
		"/graph/v1/paper/DOI:" + doi:       {{status: http.StatusUnprocessableEntity, body: `{"message":"unprocessable"}`}},
		"/graph/v1/paper/ARXIV:2301.99999": ok(paperID("S2ARXIV422")),
	}
	client, _ := newStubClient(routes)

	id, err := client.ResolvePaperID(context.Background(), doi, "2301.99999", "")

	if err != nil {
		t.Fatalf("ResolvePaperID returned error: %v", err)
	}
	if id != "S2ARXIV422" {
		t.Errorf("expected paperId S2ARXIV422 (from arXiv fallback), got %q", id)
	}
}

// Contract: a 429 (rate-limited) from the DOI lookup propagates immediately as
// a hard error that includes "429" in the message. The title /search fallback
// must NOT run — a rate-limit on one endpoint means further requests will also
// fail, and attempting them wastes remaining quota and misleads the caller.
func TestResolvePaperIDAbortsWith429AndSkipsSearch(t *testing.T) {
	routes := map[string][]stubResponse{
		"/graph/v1/paper/DOI:10.0/rate": {{status: http.StatusTooManyRequests, body: "rate limited"}},
	}
	client, stub := newStubClient(routes)

	_, err := client.ResolvePaperID(context.Background(), "10.0/rate", "", "fallback title")

	if err == nil {
		t.Fatal("expected a propagated error for HTTP 429, got nil")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("expected status 429 in error message, got %v", err)
	}
	for _, u := range stub.requests {
		if strings.Contains(u.Path, "/search") {
			t.Errorf("title /search ran despite a rate-limit error; it must not")
		}
	}
}

// Contract: a 500 from the DOI lookup is a server error that propagates
// immediately — it must never be treated as "not found" and silently fall
// through to the arXiv or title-search fallback. This guards against a
// regression where extending the "not-found" status set accidentally captures
// 5xx responses.
func TestResolvePaperIDAbortsWith500(t *testing.T) {
	routes := map[string][]stubResponse{
		"/graph/v1/paper/DOI:10.0/bad": {{status: http.StatusInternalServerError, body: "server error"}},
	}
	client, stub := newStubClient(routes)

	_, err := client.ResolvePaperID(context.Background(), "10.0/bad", "2301.00002", "fallback title")

	if err == nil || errors.Is(err, ErrPaperNotFound) {
		t.Fatalf("expected a propagated server error, got %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected status 500 in error message, got %v", err)
	}
	if len(stub.requests) != 1 {
		t.Errorf("expected only 1 request (DOI lookup), got %d — fallback must not run after a server error", len(stub.requests))
	}
}
