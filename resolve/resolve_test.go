package resolve

import (
	"io"
	"net/http"
	"net/url"
	"strings"
)

// stubResponse is a canned HTTP response for the stub transport.
type stubResponse struct {
	status int
	body   string
}

// stubTransport routes requests to canned responses by URL substring and
// records every request URL, so tests can assert the request shape without
// touching the network. An unmatched request returns 404 (a real "not found"
// the resolvers must handle), never a live call.
type stubTransport struct {
	routes   map[string]stubResponse
	requests []*url.URL
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.requests = append(s.requests, req.URL)
	for frag, resp := range s.routes {
		if strings.Contains(req.URL.String(), frag) {
			return &http.Response{
				StatusCode: resp.status,
				Body:       io.NopCloser(strings.NewReader(resp.body)),
				Header:     http.Header{},
			}, nil
		}
	}
	return &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader("no matching route")),
		Header:     http.Header{},
	}, nil
}

// newTestClient builds a resolve.Client whose HTTP calls are served by the
// stub transport, so resolvers run fully offline.
func newTestClient(routes map[string]stubResponse) (*Client, *stubTransport) {
	st := &stubTransport{routes: routes}
	return &Client{HTTPClient: &http.Client{Transport: st}}, st
}
