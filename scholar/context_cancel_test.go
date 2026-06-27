package scholar

import (
	"context"
	"errors"
	"testing"
)

// Contract: References honours an already-cancelled context, returning promptly
// with an error that unwraps to context.Canceled instead of hanging until the
// per-request 30s timeout. This is the guard that makes the Ctrl-C fix
// meaningful: cancellation must propagate from the root context through get()
// and the HTTP layer (http.NewRequestWithContext). If this regressed, a user
// pressing Ctrl-C during an in-flight Semantic Scholar call would be stuck
// waiting for requestTimeout rather than aborting immediately.
func TestReferencesHonoursCancelledContext(t *testing.T) {
	routes := map[string][]stubResponse{
		"/graph/v1/paper/PID/references": ok(`{"data":[]}`),
	}
	client, stub := newStubClient(routes)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.References(ctx, "PID", 25)

	if err == nil {
		t.Fatal("expected an error for an already-cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected error to unwrap to context.Canceled, got %v", err)
	}
	if len(stub.requests) != 0 {
		t.Errorf("expected the request to be aborted before transport recorded it, got %d requests", len(stub.requests))
	}
}

// Contract: Citations also honours an already-cancelled context, returning
// promptly with a context.Canceled error. Both citation directions share the
// same get()/paged() path, so the forward edge must abort on cancellation just
// like the backward edge — neither should hang until the per-request timeout.
func TestCitationsHonoursCancelledContext(t *testing.T) {
	routes := map[string][]stubResponse{
		"/graph/v1/paper/PID/citations": ok(`{"data":[]}`),
	}
	client, _ := newStubClient(routes)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Citations(ctx, "PID", 25)

	if err == nil {
		t.Fatal("expected an error for an already-cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected error to unwrap to context.Canceled, got %v", err)
	}
}
