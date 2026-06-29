package scholar

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// searchData builds a /paper/search response body from (paperId, title) pairs,
// in order, so a test can stage several candidates for the title-match guard to
// scan.
func searchData(pairs ...[2]string) []stubResponse {
	items := make([]string, 0, len(pairs))
	for _, p := range pairs {
		items = append(items, fmt.Sprintf(`{"paperId":%q,"title":%q}`, p[0], p[1]))
	}
	return ok(fmt.Sprintf(`{"data":[%s]}`, strings.Join(items, ",")))
}

// Contract: a generic/short query whose top search hit is an unrelated paper is
// rejected — ResolvePaperID returns ErrPaperNotFound instead of silently
// adopting the wrong paperId. Without the title-match guard, the search endpoint
// always returns *something* for a query like "Introduction", so the entire
// citation network would be built for an unrelated paper with found=true and no
// error: a silent misidentification worse than a visible failure.
func TestResolvePaperIDRejectsUnrelatedTitleHit(t *testing.T) {
	routes := map[string][]stubResponse{
		"/graph/v1/paper/search": searchData(
			[2]string{"S2WRONG", "Introduction to Quantum Computing"},
		),
	}
	client, _ := newStubClient(routes)

	_, err := client.ResolvePaperID(context.Background(), "", "", "Introduction")

	if !errors.Is(err, ErrPaperNotFound) {
		t.Fatalf("expected ErrPaperNotFound for an unrelated top hit, got %v", err)
	}
}

// Contract: title matching is insensitive to case and punctuation, so a query
// that differs from the indexed title only in capitalization and a trailing
// period still resolves. The guard must not reject genuine matches, or real
// papers with cosmetic title differences would become unresolvable.
func TestResolvePaperIDMatchesDespiteCaseAndPunctuation(t *testing.T) {
	routes := map[string][]stubResponse{
		"/graph/v1/paper/search": searchData(
			[2]string{"S2ATT", "Attention Is All You Need."},
		),
	}
	client, _ := newStubClient(routes)

	id, err := client.ResolvePaperID(context.Background(), "", "", "Attention is all you need")

	if err != nil {
		t.Fatalf("ResolvePaperID returned error: %v", err)
	}
	if id != "S2ATT" {
		t.Errorf("expected paperId S2ATT, got %q", id)
	}
}

// Contract: the guard scans the candidate list rather than trusting index 0 — an
// unrelated first hit followed by a matching later hit (within the top 5)
// resolves to the matching one. This proves the resolver picks the best match,
// not blindly the first, so a near-miss top result cannot shadow the correct
// paper.
func TestResolvePaperIDScansCandidatesForMatch(t *testing.T) {
	routes := map[string][]stubResponse{
		"/graph/v1/paper/search": searchData(
			[2]string{"S2WRONG", "A Completely Different Survey"},
			[2]string{"S2RIGHT", "Deep Residual Learning for Image Recognition"},
		),
	}
	client, _ := newStubClient(routes)

	id, err := client.ResolvePaperID(context.Background(), "", "", "Deep Residual Learning for Image Recognition")

	if err != nil {
		t.Fatalf("ResolvePaperID returned error: %v", err)
	}
	if id != "S2RIGHT" {
		t.Errorf("expected the matching later candidate S2RIGHT, got %q", id)
	}
}
