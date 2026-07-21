package embed

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// roundTripFunc adapts a function to http.RoundTripper so a test injects a
// canned transport without a network.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// Contract: Embed posts {model,input} to <BaseURL>/embeddings and maps the
// response embeddings back in input order. A CLI that builds an index from this
// must get one vector per input, correctly ordered, or every downstream ranking
// is wrong.
func TestEmbedSendsRequestAndParsesVectors(t *testing.T) {
	var gotPath, gotBody string
	e := New("http://local.test/v1", "m-embed", "")
	e.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		return jsonResponse(200, `{"data":[{"embedding":[1,2,3]},{"embedding":[4,5,6]}]}`), nil
	})}

	vecs, err := e.Embed(context.Background(), []string{"alpha", "beta"})

	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if gotPath != "/v1/embeddings" {
		t.Errorf("path = %q, want /v1/embeddings", gotPath)
	}
	var req struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}
	if err := json.Unmarshal([]byte(gotBody), &req); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	if req.Model != "m-embed" {
		t.Errorf("model = %q, want m-embed", req.Model)
	}
	if len(req.Input) != 2 || req.Input[0] != "alpha" || req.Input[1] != "beta" {
		t.Errorf("input = %v, want [alpha beta]", req.Input)
	}
	if len(vecs) != 2 || vecs[0][0] != 1 || vecs[1][2] != 6 {
		t.Errorf("vecs = %v, want [[1 2 3] [4 5 6]]", vecs)
	}
}

// Contract: the Authorization header is sent only when an API key is configured.
// Local backends (Ollama) need no key; sending an empty Bearer would be wrong,
// and forgetting it for a remote API would 401.
func TestEmbedAuthorizationHeaderOnlyWithKey(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		wantHdr string
	}{
		{name: "no key omits header", key: "", wantHdr: ""},
		{name: "key sets bearer", key: "sk-123", wantHdr: "Bearer sk-123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			e := New("http://local.test/v1", "m", tc.key)
			e.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				got = r.Header.Get("Authorization")
				return jsonResponse(200, `{"data":[{"embedding":[1]}]}`), nil
			})}

			_, err := e.Embed(context.Background(), []string{"x"})

			if err != nil {
				t.Fatalf("Embed: %v", err)
			}
			if got != tc.wantHdr {
				t.Errorf("Authorization = %q, want %q", got, tc.wantHdr)
			}
		})
	}
}

// Contract: a non-2xx response is an error carrying the status, and a
// vector/input count mismatch is an error — both distinguish "failed" from a
// silently short or empty result that would corrupt the index.
func TestEmbedErrors(t *testing.T) {
	t.Run("non-2xx", func(t *testing.T) {
		e := New("http://local.test/v1", "m", "")
		e.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(500, `err`), nil
		})}
		_, err := e.Embed(context.Background(), []string{"x"})
		if err == nil || !strings.Contains(err.Error(), "500") {
			t.Fatalf("want error mentioning 500, got %v", err)
		}
	})
	t.Run("count mismatch", func(t *testing.T) {
		e := New("http://local.test/v1", "m", "")
		e.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(200, `{"data":[{"embedding":[1]}]}`), nil
		})}
		_, err := e.Embed(context.Background(), []string{"x", "y"})
		if err == nil {
			t.Fatal("want error on 1 vector for 2 inputs, got nil")
		}
	})
}
