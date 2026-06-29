package scholar

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// Contract: when Semantic Scholar returns a non-200 with a very large body
// (e.g. a multi-KB HTML error page during an outage), the error message length
// must be bounded well under the raw body size so LLM-agent token budgets are
// not wasted and logs are not polluted. The HTTP status code must still appear
// in full so callers can classify the error.
func TestAPIErrorLargeBodyIsTruncated(t *testing.T) {
	largeBody := strings.Repeat("x", 5000)
	routes := map[string][]stubResponse{
		"/graph/v1/paper/PID/references": {
			{status: http.StatusTooManyRequests, body: largeBody},
		},
	}
	client, _ := newStubClient(routes)

	_, err := client.References(context.Background(), "PID", 25)

	if err == nil {
		t.Fatal("expected error for HTTP 429, got nil")
	}
	msg := err.Error()
	if len(msg) >= 300 {
		t.Errorf("error message length %d is not bounded: %q…", len(msg), msg[:80])
	}
	if !strings.Contains(msg, "429") {
		t.Errorf("expected status 429 in error message, got %q", msg)
	}
}

// Contract: when the error body is short, it is preserved intact — truncation
// must not corrupt a brief, informative message. The status code must still
// appear so callers can identify the failure class without parsing the body.
func TestAPIErrorShortBodyIsPreservedIntact(t *testing.T) {
	shortBody := "rate limit exceeded"
	routes := map[string][]stubResponse{
		"/graph/v1/paper/PID/references": {
			{status: http.StatusInternalServerError, body: shortBody},
		},
	}
	client, _ := newStubClient(routes)

	_, err := client.References(context.Background(), "PID", 25)

	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, shortBody) {
		t.Errorf("expected short body %q to appear intact in error, got %q", shortBody, msg)
	}
	if !strings.Contains(msg, "500") {
		t.Errorf("expected status 500 in error message, got %q", msg)
	}
}
