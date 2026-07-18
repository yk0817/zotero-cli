# Semantic Search — Phase 1 (Slice A) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add item-level semantic search to zotero-cli — a local-default embedding backend, a flat-file vector index with `index build/update/status`, and `search --semantic`.

**Architecture:** Two new sibling packages (`embed/`, `semindex/`) plus a paginating `zotero.ListAllItems` and CLI wiring in `cmd/zotero-cli`. `embed.HTTPEmbedder` calls an OpenAI-compatible `/v1/embeddings` endpoint (default local Ollama). `semindex` stores one float32 vector per item (built from title+abstract) in a gob file and ranks by brute-force cosine. Orchestration is factored into pure functions taking interfaces, so every path is tested offline.

**Tech Stack:** Go 1.26, cobra, stdlib `encoding/gob` + `net/http`. No cgo, no new third-party deps.

**Design doc:** `docs/design/2026-07-18-semantic-search.md`.

## Global Constraints

- Module path: `github.com/yk0817/zotero-cli`. Go 1.26.1.
- No new third-party dependencies; no cgo. Keep the binary light.
- English-first bilingual for docs/comments/README; code comments explain *why*.
- Every `func TestXxx` has a `// Contract:` comment (hook-enforced) stating what breaks and for whom.
- Tests are hermetic: inject `http.RoundTripper` / fake `Embedder` / stub client; never hit the network. Run `go test -race ./...`.
- JSON output uses the `{"ok":true,"data":...}` envelope; empty results are explicit, never an empty string. Empty slices serialize as `[]`, not `null`.
- Read `knowledge-base/qa-perspectives.md` and `knowledge-base/test-style-guide.md` before writing tests.
- Secrets: the embedding API key comes only from `$ZOTERO_EMBED_API_KEY`; never write it to config or print it. Do NOT copy `oapdf`'s SSRF dial guard into `embed` (default endpoint is localhost).
- Commit messages: Conventional Commits, English-first. Reference `#27`.

---

### Task 1: `embed` package — OpenAI-compatible HTTP embedder

**Files:**
- Create: `embed/embed.go`
- Test: `embed/embed_test.go`

**Interfaces:**
- Produces:
  - `type Embedder interface { Embed(ctx context.Context, inputs []string) ([][]float32, error) }`
  - `type HTTPEmbedder struct { BaseURL, Model, APIKey string; HTTPClient *http.Client }`
  - `func New(baseURL, model, apiKey string) *HTTPEmbedder`
  - `func (e *HTTPEmbedder) Embed(ctx context.Context, inputs []string) ([][]float32, error)`

- [ ] **Step 1: Write the failing tests**

Create `embed/embed_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go -C <repo> test ./embed/`
Expected: FAIL — `undefined: New` / package has no non-test files.

- [ ] **Step 3: Write the implementation**

Create `embed/embed.go`:

```go
// Package embed turns text into vectors via an OpenAI-compatible
// POST {BaseURL}/embeddings endpoint. The same client talks to a local server
// (Ollama / llama.cpp / LM Studio) or a remote API (OpenAI / Gemini-compatible);
// only BaseURL and APIKey differ. There is deliberately no SSRF dial guard here
// (unlike oapdf): the base URL comes from the user's own config and the default
// target is localhost, which such a guard would block.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const requestTimeout = 60 * time.Second

// Embedder turns a batch of texts into vectors. One small method, injectable so
// callers depend on the interface and tests use a deterministic fake.
type Embedder interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
}

// HTTPEmbedder is an Embedder backed by an OpenAI-compatible HTTP endpoint.
type HTTPEmbedder struct {
	BaseURL    string
	Model      string
	APIKey     string // optional; sent as Authorization: Bearer when non-empty
	HTTPClient *http.Client
}

// New returns an HTTPEmbedder with a bounded-timeout client.
func New(baseURL, model, apiKey string) *HTTPEmbedder {
	return &HTTPEmbedder{
		BaseURL:    baseURL,
		Model:      model,
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: requestTimeout},
	}
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed sends inputs in a single request and returns one vector per input, in
// order. An empty input slice returns nil without a request.
func (e *HTTPEmbedder) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(embedRequest{Model: e.Model, Input: inputs})
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(e.BaseURL, "/") + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.APIKey)
	}
	client := e.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: request to %s failed: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embed: reading response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embed: HTTP %d from %s", resp.StatusCode, endpoint)
	}

	var parsed embedResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("embed: invalid response from %s: %w", endpoint, err)
	}
	if len(parsed.Data) != len(inputs) {
		return nil, fmt.Errorf("embed: got %d vectors for %d inputs", len(parsed.Data), len(inputs))
	}
	out := make([][]float32, len(parsed.Data))
	for i, d := range parsed.Data {
		out[i] = d.Embedding
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go -C <repo> test -race ./embed/`
Expected: PASS (3 tests).

- [ ] **Step 5: gofmt + commit**

Run: `gofmt -w embed/embed.go embed/embed_test.go`

```bash
git -C <repo> add embed/
git -C <repo> commit -m "feat(embed): OpenAI-compatible HTTP embedder (local default)

Refs #27"
```

---

### Task 2: `semindex` — index types, atomic gob load/save, compatibility check

**Files:**
- Create: `semindex/index.go`
- Test: `semindex/index_test.go`

**Interfaces:**
- Produces:
  - `const SchemaVersion = 1`
  - `var ErrIndexNotFound = errors.New("semindex: index not found")`
  - `type Header struct { SchemaVersion int; LibraryID, Model string; Dim int; BuiltAt time.Time }`
  - `type Entry struct { ItemKey string; Version int; Title string; Vector []float32 }`
  - `type Index struct { Header Header; Entries []Entry }`
  - `func Load(path string) (*Index, error)` (returns `ErrIndexNotFound` when the file is absent)
  - `func (idx *Index) Save(path string) error` (atomic tmp+rename)
  - `func (idx *Index) CompatibleWith(libraryID, model string) error`

- [ ] **Step 1: Write the failing tests**

Create `semindex/index_test.go`:

```go
package semindex

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sampleIndex() *Index {
	return &Index{
		Header:  Header{SchemaVersion: SchemaVersion, LibraryID: "u1", Model: "m", Dim: 3, BuiltAt: time.Unix(0, 0).UTC()},
		Entries: []Entry{{ItemKey: "AAAA1111", Version: 5, Title: "T", Vector: []float32{1, 0, 0}}},
	}
}

// Contract: Save then Load round-trips the header and entries exactly. The index
// is the sole source of truth for semantic search; a lossy encode would silently
// change rankings or lose items.
func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "items.gob")
	idx := sampleIndex()

	if err := idx.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Header != idx.Header {
		t.Errorf("header = %+v, want %+v", got.Header, idx.Header)
	}
	if len(got.Entries) != 1 || got.Entries[0].ItemKey != "AAAA1111" || got.Entries[0].Version != 5 {
		t.Errorf("entries = %+v", got.Entries)
	}
}

// Contract: Load of a missing file returns ErrIndexNotFound (not a generic os
// error), so the CLI can tell "no index yet — run build" apart from a real IO
// failure and give the right suggestion.
func TestLoadMissingReturnsSentinel(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.gob"))

	if !errors.Is(err, ErrIndexNotFound) {
		t.Fatalf("want ErrIndexNotFound, got %v", err)
	}
}

// Contract: Save is atomic — it must not leave a .tmp file behind on success,
// so a crashed/failed build never yields a half-written index that Load would
// accept as valid.
func TestSaveLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.gob")

	if err := sampleIndex().Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp file still present: %v", err)
	}
}

// Contract: CompatibleWith rejects an index built for a different library or
// model, so search never ranks a query embedding against vectors from a
// different embedding space (which would return meaningless results).
func TestCompatibleWith(t *testing.T) {
	idx := sampleIndex()

	if err := idx.CompatibleWith("u1", "m"); err != nil {
		t.Errorf("matching header should pass, got %v", err)
	}
	if err := idx.CompatibleWith("u2", "m"); err == nil {
		t.Error("different library should fail")
	}
	if err := idx.CompatibleWith("u1", "other"); err == nil {
		t.Error("different model should fail")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go -C <repo> test ./semindex/`
Expected: FAIL — undefined `Index`, `Load`, etc.

- [ ] **Step 3: Write the implementation**

Create `semindex/index.go`:

```go
// Package semindex is a local, flat-file vector index over Zotero items: one
// float32 vector per item (built from title+abstract), stored as gob and
// searched by brute-force cosine. It is intentionally dependency-free and sized
// for a personal library (thousands of items), so no ANN/vector DB is needed.
package semindex

import (
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SchemaVersion is bumped when the on-disk layout changes incompatibly.
const SchemaVersion = 1

// ErrIndexNotFound is returned by Load when the index file does not exist, so
// callers can distinguish "not built yet" from a real IO error.
var ErrIndexNotFound = errors.New("semindex: index not found")

// Header binds an index file to the library and model it was built with. A
// mismatch on load means the vectors are from a different embedding space and
// must not be searched.
type Header struct {
	SchemaVersion int
	LibraryID     string
	Model         string
	Dim           int
	BuiltAt       time.Time
}

// Entry is one item's vector plus the minimum needed to rank, display, and
// incrementally re-embed it.
type Entry struct {
	ItemKey string
	Version int
	Title   string
	Vector  []float32
}

// Index is the full on-disk structure.
type Index struct {
	Header  Header
	Entries []Entry
}

// Load reads and gob-decodes the index at path. A missing file yields
// ErrIndexNotFound.
func Load(path string) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrIndexNotFound
		}
		return nil, err
	}
	defer f.Close()

	var idx Index
	if err := gob.NewDecoder(f).Decode(&idx); err != nil {
		return nil, fmt.Errorf("semindex: decode %s: %w", path, err)
	}
	return &idx, nil
}

// Save gob-encodes the index to a temp file and renames it over path, so a
// failed encode never replaces a good index with a partial one.
func (idx *Index) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(f).Encode(idx); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("semindex: encode: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// CompatibleWith returns a descriptive error when the on-disk header does not
// match the active library or embedding model.
func (idx *Index) CompatibleWith(libraryID, model string) error {
	if idx.Header.LibraryID != libraryID {
		return fmt.Errorf("index was built for library %q, current is %q; run 'index build'", idx.Header.LibraryID, libraryID)
	}
	if idx.Header.Model != model {
		return fmt.Errorf("index was built with model %q, current is %q; run 'index build'", idx.Header.Model, model)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go -C <repo> test -race ./semindex/`
Expected: PASS (4 tests).

- [ ] **Step 5: gofmt + commit**

Run: `gofmt -w semindex/index.go semindex/index_test.go`

```bash
git -C <repo> add semindex/index.go semindex/index_test.go
git -C <repo> commit -m "feat(semindex): index types with atomic gob load/save

Refs #27"
```

---

### Task 3: `semindex` — cosine + ranked search

**Files:**
- Create: `semindex/search.go`
- Test: `semindex/search_test.go`

**Interfaces:**
- Consumes: `Index`, `Entry` (Task 2).
- Produces:
  - `func Cosine(a, b []float32) float64`
  - `type Result struct { ItemKey string `+"`json:\"itemKey\"`"+`; Title string `+"`json:\"title\"`"+`; Score float64 `+"`json:\"score\"`"+` }`
  - `func (idx *Index) Search(query []float32, k int) []Result`

- [ ] **Step 1: Write the failing tests**

Create `semindex/search_test.go`:

```go
package semindex

import "testing"

// Contract: cosine is 1 for identical direction, 0 for orthogonal, and 0 (not
// NaN) for a zero vector. Ranking correctness and crash-freeness both depend on
// this; a NaN would poison sort order.
func TestCosine(t *testing.T) {
	if got := Cosine([]float32{1, 0}, []float32{2, 0}); got < 0.999 || got > 1.001 {
		t.Errorf("parallel = %v, want ~1", got)
	}
	if got := Cosine([]float32{1, 0}, []float32{0, 1}); got < -0.001 || got > 0.001 {
		t.Errorf("orthogonal = %v, want ~0", got)
	}
	if got := Cosine([]float32{0, 0}, []float32{1, 1}); got != 0 {
		t.Errorf("zero vector = %v, want 0", got)
	}
}

// Contract: Search returns entries ordered by descending similarity to the
// query and truncates to k. This is the whole ranking guarantee semantic search
// makes; if order or top-k were wrong the feature is useless.
func TestSearchRanksAndTruncates(t *testing.T) {
	idx := &Index{Entries: []Entry{
		{ItemKey: "FAR", Vector: []float32{0, 1}},
		{ItemKey: "NEAR", Vector: []float32{1, 0}},
		{ItemKey: "MID", Vector: []float32{1, 1}},
	}}

	got := idx.Search([]float32{1, 0}, 2)

	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (k truncation)", len(got))
	}
	if got[0].ItemKey != "NEAR" || got[1].ItemKey != "MID" {
		t.Errorf("order = [%s %s], want [NEAR MID]", got[0].ItemKey, got[1].ItemKey)
	}
	if got[0].Score < got[1].Score {
		t.Errorf("scores not descending: %v < %v", got[0].Score, got[1].Score)
	}
}

// Contract: k <= 0 returns all entries (no truncation), so a caller asking for
// "everything ranked" is not silently given nothing.
func TestSearchZeroKReturnsAll(t *testing.T) {
	idx := &Index{Entries: []Entry{{ItemKey: "A", Vector: []float32{1}}, {ItemKey: "B", Vector: []float32{1}}}}

	if got := idx.Search([]float32{1}, 0); len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go -C <repo> test ./semindex/ -run TestCosine`
Expected: FAIL — undefined `Cosine`.

- [ ] **Step 3: Write the implementation**

Create `semindex/search.go`:

```go
package semindex

import (
	"math"
	"sort"
)

// Result is one ranked item. JSON tags match the CLI output contract
// ({itemKey,title,score}) so the command can print it directly.
type Result struct {
	ItemKey string  `json:"itemKey"`
	Title   string  `json:"title"`
	Score   float64 `json:"score"`
}

// Cosine returns the cosine similarity of a and b, computed in float64. A zero
// vector on either side yields 0 rather than NaN. Extra trailing dimensions on
// the longer vector are ignored (vectors are expected to share Dim).
func Cosine(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		av, bv := float64(a[i]), float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// Search ranks all entries by cosine similarity to query, descending, and
// returns the top k. k <= 0 returns every entry.
func (idx *Index) Search(query []float32, k int) []Result {
	results := make([]Result, 0, len(idx.Entries))
	for _, e := range idx.Entries {
		results = append(results, Result{ItemKey: e.ItemKey, Title: e.Title, Score: Cosine(query, e.Vector)})
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if k > 0 && len(results) > k {
		results = results[:k]
	}
	return results
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go -C <repo> test -race ./semindex/`
Expected: PASS (all semindex tests).

- [ ] **Step 5: gofmt + commit**

```bash
gofmt -w semindex/search.go semindex/search_test.go
git -C <repo> add semindex/search.go semindex/search_test.go
git -C <repo> commit -m "feat(semindex): cosine similarity and ranked search

Refs #27"
```

---

### Task 4: `semindex` — incremental diff

**Files:**
- Create: `semindex/diff.go`
- Test: `semindex/diff_test.go`

**Interfaces:**
- Consumes: `Index`, `Entry` (Task 2).
- Produces:
  - `type ItemRef struct { Key string; Version int }`
  - `func (idx *Index) Diff(current []ItemRef) (toEmbed []string, keep []Entry, removed []string)`

- [ ] **Step 1: Write the failing test**

Create `semindex/diff_test.go`:

```go
package semindex

import (
	"sort"
	"testing"
)

// Contract: Diff partitions the current library against the index so update
// re-embeds only what changed. A new key or a bumped Version must be re-embedded;
// an unchanged item must be kept without an embed call (cost); an item gone from
// the library must be removed. Getting any bucket wrong means stale, missing, or
// needlessly re-embedded vectors.
func TestDiffPartitions(t *testing.T) {
	idx := &Index{Entries: []Entry{
		{ItemKey: "KEEP", Version: 1, Vector: []float32{1}},
		{ItemKey: "CHANGED", Version: 1, Vector: []float32{1}},
		{ItemKey: "GONE", Version: 1, Vector: []float32{1}},
	}}
	current := []ItemRef{
		{Key: "KEEP", Version: 1},
		{Key: "CHANGED", Version: 2},
		{Key: "NEW", Version: 1},
	}

	toEmbed, keep, removed := idx.Diff(current)

	sort.Strings(toEmbed)
	if len(toEmbed) != 2 || toEmbed[0] != "CHANGED" || toEmbed[1] != "NEW" {
		t.Errorf("toEmbed = %v, want [CHANGED NEW]", toEmbed)
	}
	if len(keep) != 1 || keep[0].ItemKey != "KEEP" {
		t.Errorf("keep = %+v, want [KEEP]", keep)
	}
	if len(removed) != 1 || removed[0] != "GONE" {
		t.Errorf("removed = %v, want [GONE]", removed)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go -C <repo> test ./semindex/ -run TestDiff`
Expected: FAIL — undefined `ItemRef` / `Diff`.

- [ ] **Step 3: Write the implementation**

Create `semindex/diff.go`:

```go
package semindex

// ItemRef is the minimal library-item identity Diff needs: its key and current
// version. Keeping Diff on refs (not full items) keeps semindex decoupled from
// the zotero package.
type ItemRef struct {
	Key     string
	Version int
}

// Diff partitions current library items against the index:
//   - toEmbed: keys that are new, or whose Version changed (must be re-embedded)
//   - keep:    existing entries still present and unchanged (reused as-is)
//   - removed: index keys no longer in the library (dropped)
func (idx *Index) Diff(current []ItemRef) (toEmbed []string, keep []Entry, removed []string) {
	byKey := make(map[string]Entry, len(idx.Entries))
	for _, e := range idx.Entries {
		byKey[e.ItemKey] = e
	}
	seen := make(map[string]bool, len(current))
	for _, ref := range current {
		seen[ref.Key] = true
		if e, ok := byKey[ref.Key]; ok && e.Version == ref.Version {
			keep = append(keep, e)
		} else {
			toEmbed = append(toEmbed, ref.Key)
		}
	}
	for _, e := range idx.Entries {
		if !seen[e.ItemKey] {
			removed = append(removed, e.ItemKey)
		}
	}
	return toEmbed, keep, removed
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go -C <repo> test -race ./semindex/`
Expected: PASS.

- [ ] **Step 5: gofmt + commit**

```bash
gofmt -w semindex/diff.go semindex/diff_test.go
git -C <repo> add semindex/diff.go semindex/diff_test.go
git -C <repo> commit -m "feat(semindex): incremental diff by item version

Refs #27"
```

---

### Task 5: `zotero.ListAllItems` — paginating full-library enumerator

**Files:**
- Modify: `zotero/client.go` (add method near `ListItems`)
- Test: `zotero/read_test.go` (add a test using the existing `queryRecordingTransport`)

**Interfaces:**
- Consumes: existing `Client`, `doRequest`, `Item`.
- Produces: `func (c *Client) ListAllItems() ([]Item, error)`

**Before writing:** open `zotero/read_test.go` and read how `queryRecordingTransport` queues responses and records request URLs (the `TestGetChildrenPaginates` test is the model). Reuse it; do not create a new transport.

- [ ] **Step 1: Write the failing test**

Add to `zotero/read_test.go` (adapt field/constructor names to the actual `queryRecordingTransport` in that file):

```go
// Contract: ListAllItems follows pagination and returns every item, not just the
// first page. The Zotero API caps a page at 100; a library larger than one page
// must not be silently truncated (same class as the GetChildren pagination bug),
// or the semantic index would be built over a partial library.
func TestListAllItemsPaginates(t *testing.T) {
	// First page: a full 100 items -> more pages expected. Second page: 30 items
	// -> a short page signals the end. (Build the JSON arrays with a helper; each
	// element only needs a "key".)
	page1 := itemsJSON(100)
	page2 := itemsJSON(30)

	tr := &queryRecordingTransport{responses: []string{page1, page2}}
	c := NewClient("k", "u")
	c.HTTPClient = &http.Client{Transport: tr}

	items, err := c.ListAllItems()

	if err != nil {
		t.Fatalf("ListAllItems: %v", err)
	}
	if len(items) != 130 {
		t.Errorf("got %d items, want 130 (100+30 across two pages)", len(items))
	}
	if len(tr.requests) != 2 {
		t.Fatalf("made %d requests, want 2", len(tr.requests))
	}
	if got := tr.requests[0].Query().Get("start"); got != "0" {
		t.Errorf("page 1 start = %q, want 0", got)
	}
	if got := tr.requests[1].Query().Get("start"); got != "100" {
		t.Errorf("page 2 start = %q, want 100", got)
	}
	if got := tr.requests[0].Query().Get("itemType"); got != "-attachment || note" {
		t.Errorf("itemType = %q, want -attachment || note", got)
	}
}

// itemsJSON builds a JSON array of n minimal items ([{"key":"K0000000"},...]).
func itemsJSON(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf(`{"key":"K%07d","version":1}`, i)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
```

> Note: if `queryRecordingTransport`'s fields differ (e.g. it records URLs as strings, or uses a different constructor), adjust the test to match the existing helper rather than changing the helper. The assertions to preserve: two requests, `start=0` then `start=100`, `itemType=-attachment || note`, 130 items total.

- [ ] **Step 2: Run test to verify it fails**

Run: `go -C <repo> test ./zotero/ -run TestListAllItemsPaginates`
Expected: FAIL — undefined `ListAllItems`.

- [ ] **Step 3: Write the implementation**

Add to `zotero/client.go` (imports `strconv` if not present):

```go
// ListAllItems enumerates every top-level item in the library (excluding
// attachments and notes), following pagination until a short page. Unlike
// ListItems, it does not stop at one page — the semantic index must be built
// over the whole library, not the API's default first page.
func (c *Client) ListAllItems() ([]Item, error) {
	const pageSize = 100
	var all []Item
	for start := 0; ; start += pageSize {
		params := url.Values{}
		params.Set("limit", strconv.Itoa(pageSize))
		params.Set("start", strconv.Itoa(start))
		params.Set("sort", "dateModified")
		params.Set("direction", "desc")
		params.Set("itemType", "-attachment || note")

		body, err := c.doRequest("/items", params, "application/json")
		if err != nil {
			return nil, err
		}
		var page []Item
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
		all = append(all, page...)
		if len(page) < pageSize {
			break
		}
	}
	return all, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go -C <repo> test -race ./zotero/ -run TestListAllItemsPaginates`
Expected: PASS. Then run `go -C <repo> test -race ./zotero/` to confirm no regressions.

- [ ] **Step 5: gofmt + commit**

```bash
gofmt -w zotero/client.go zotero/read_test.go
git -C <repo> add zotero/client.go zotero/read_test.go
git -C <repo> commit -m "feat(zotero): ListAllItems paginates the whole library

Refs #27"
```

---

### Task 6: CLI config plumbing — embedder resolution, index path, embed text

**Files:**
- Modify: `cmd/zotero-cli/main.go` (add fields to `Config`; add prompts to `config`)
- Create: `cmd/zotero-cli/semantic.go` (shared helpers for the semantic commands)
- Test: `cmd/zotero-cli/semantic_test.go`

**Interfaces:**
- Consumes: `Config`, `configDir()` (main.go); `embed.New` (Task 1); `zotero.Item` (existing).
- Produces:
  - `const defaultEmbedBaseURL, defaultEmbedModel, embedAPIKeyEnv string`
  - `func embedBaseURL(cfg *Config) string`
  - `func embedModel(cfg *Config) string`
  - `func resolveEmbedder(cfg *Config) *embed.HTTPEmbedder`
  - `func indexPath() string`
  - `func embedText(item zotero.Item) string`

- [ ] **Step 1: Add config fields**

In `cmd/zotero-cli/main.go`, extend the `Config` struct (keep existing fields):

```go
type Config struct {
	APIKey       string `json:"api_key"`
	UserID       string `json:"user_id"`
	Email        string `json:"email,omitempty"`
	EmbedBaseURL string `json:"embed_base_url,omitempty"`
	EmbedModel   string `json:"embed_model,omitempty"`
}
```

In the `config` command's `RunE`, after the email prompt and before building `cfg`, add optional prompts (blank keeps the default):

```go
fmt.Print("Embedding base URL for semantic search (optional, blank = local Ollama default): ")
embedURL, _ := reader.ReadString('\n')
embedURL = strings.TrimSpace(embedURL)

fmt.Print("Embedding model (optional, blank = nomic-embed-text): ")
embedMdl, _ := reader.ReadString('\n')
embedMdl = strings.TrimSpace(embedMdl)
```

Then set them on the struct:

```go
cfg := &Config{APIKey: apiKey, UserID: userID, Email: email, EmbedBaseURL: embedURL, EmbedModel: embedMdl}
```

- [ ] **Step 2: Write the failing tests**

Create `cmd/zotero-cli/semantic_test.go`:

```go
package main

import (
	"testing"

	"github.com/yk0817/zotero-cli/zotero"
)

// Contract: embed settings fall back to the documented local defaults when the
// config leaves them blank, and the config values win when set. This is what
// makes "local by default, remote when configured" true.
func TestEmbedDefaults(t *testing.T) {
	if got := embedBaseURL(&Config{}); got != defaultEmbedBaseURL {
		t.Errorf("base url default = %q, want %q", got, defaultEmbedBaseURL)
	}
	if got := embedModel(&Config{}); got != defaultEmbedModel {
		t.Errorf("model default = %q, want %q", got, defaultEmbedModel)
	}
	if got := embedBaseURL(&Config{EmbedBaseURL: "http://x/v1"}); got != "http://x/v1" {
		t.Errorf("config base url should win, got %q", got)
	}
	if got := embedModel(&Config{EmbedModel: "custom"}); got != "custom" {
		t.Errorf("config model should win, got %q", got)
	}
}

// Contract: the API key is read only from the environment, never from Config —
// so a key can never be persisted to config.json on disk.
func TestResolveEmbedderKeyFromEnvOnly(t *testing.T) {
	t.Setenv(embedAPIKeyEnv, "sk-secret")

	e := resolveEmbedder(&Config{UserID: "u", EmbedModel: "m"})

	if e.APIKey != "sk-secret" {
		t.Errorf("APIKey = %q, want it from env", e.APIKey)
	}
	if e.Model != "m" {
		t.Errorf("Model = %q, want m", e.Model)
	}
}

// Contract: embedText joins title and abstract into the text that represents an
// item, and returns empty only when both are empty (so build can skip items with
// nothing to embed rather than sending blank input).
func TestEmbedText(t *testing.T) {
	full := zotero.Item{Data: zotero.ItemData{Title: "Attention", AbstractNote: "We propose..."}}
	if got := embedText(full); got != "Attention\nWe propose..." {
		t.Errorf("embedText = %q", got)
	}

	titleOnly := zotero.Item{Data: zotero.ItemData{Title: "Only title"}}
	if got := embedText(titleOnly); got != "Only title" {
		t.Errorf("title-only embedText = %q, want %q", got, "Only title")
	}

	empty := zotero.Item{Data: zotero.ItemData{}}
	if got := embedText(empty); got != "" {
		t.Errorf("empty embedText = %q, want \"\"", got)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go -C <repo> test ./cmd/zotero-cli/ -run 'TestEmbed|TestResolveEmbedder'`
Expected: FAIL — undefined `embedBaseURL`, etc.

- [ ] **Step 4: Write the implementation**

Create `cmd/zotero-cli/semantic.go`:

```go
package main

import (
	"path/filepath"
	"strings"

	"github.com/yk0817/zotero-cli/embed"
	"github.com/yk0817/zotero-cli/zotero"
)

// Embedding backend defaults. The default base URL is Ollama's
// OpenAI-compatible endpoint, so "local by default" needs no configuration; the
// model default (nomic-embed-text) is a small local embedding model. The API key
// is read only from the environment (never config), matching secret-handling.
const (
	defaultEmbedBaseURL = "http://localhost:11434/v1"
	defaultEmbedModel   = "nomic-embed-text"
	embedAPIKeyEnv      = "ZOTERO_EMBED_API_KEY"
)

func embedBaseURL(cfg *Config) string {
	if cfg.EmbedBaseURL != "" {
		return cfg.EmbedBaseURL
	}
	return defaultEmbedBaseURL
}

func embedModel(cfg *Config) string {
	if cfg.EmbedModel != "" {
		return cfg.EmbedModel
	}
	return defaultEmbedModel
}

// resolveEmbedder builds an HTTP embedder from config (base URL + model) and the
// environment (API key only).
func resolveEmbedder(cfg *Config) *embed.HTTPEmbedder {
	return embed.New(embedBaseURL(cfg), embedModel(cfg), getenv(embedAPIKeyEnv))
}

// indexPath is the on-disk location of the vector index, under the same config
// directory as config.json.
func indexPath() string {
	return filepath.Join(configDir(), "index", "items.gob")
}

// embedText is the text that represents an item for embedding: title and
// abstract joined by a newline. Empty only when both are empty.
func embedText(item zotero.Item) string {
	title := strings.TrimSpace(item.Data.Title)
	abstract := strings.TrimSpace(item.Data.AbstractNote)
	return strings.TrimSpace(title + "\n" + abstract)
}
```

Add a tiny `getenv` indirection at the bottom of `semantic.go` (so it reads `os.Getenv`; kept as a named helper only to group the import). If you prefer, call `os.Getenv` directly and import `os` here:

```go
import "os"

func getenv(k string) string { return os.Getenv(k) }
```

(Simplest: import `os` in `semantic.go` and use `os.Getenv(embedAPIKeyEnv)` inline in `resolveEmbedder`, dropping `getenv`. Pick one and keep it consistent.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go -C <repo> test -race ./cmd/zotero-cli/ -run 'TestEmbed|TestResolveEmbedder'`
Expected: PASS.

- [ ] **Step 6: gofmt + commit**

```bash
gofmt -w cmd/zotero-cli/main.go cmd/zotero-cli/semantic.go cmd/zotero-cli/semantic_test.go
git -C <repo> add cmd/zotero-cli/main.go cmd/zotero-cli/semantic.go cmd/zotero-cli/semantic_test.go
git -C <repo> commit -m "feat(cli): embedding config plumbing (defaults, env key, index path)

Refs #27"
```

---

### Task 7: CLI `index build` command

**Files:**
- Create: `cmd/zotero-cli/index.go` (the `index` parent + `build` subcommand + `buildIndex` orchestration)
- Modify: `cmd/zotero-cli/main.go` (register `indexCmd` in `AddCommand`)
- Test: `cmd/zotero-cli/index_test.go`

**Interfaces:**
- Consumes: `embed.Embedder` (Task 1); `semindex.Index/Entry/Header` (Task 2); `resolveEmbedder`, `embedText`, `indexPath` (Task 6); `zotero.Item`.
- Produces:
  - `type itemLister interface { ListAllItems() ([]zotero.Item, error) }`
  - `func buildIndex(ctx context.Context, lister itemLister, emb embed.Embedder, libraryID, model string, batchSize int) (idx *semindex.Index, skipped int, err error)`
  - `func newIndexCmd() *cobra.Command`

- [ ] **Step 1: Write the failing test**

Create `cmd/zotero-cli/index_test.go`:

```go
package main

import (
	"context"
	"testing"

	"github.com/yk0817/zotero-cli/zotero"
)

// fakeLister returns a fixed item list, standing in for the Zotero client.
type fakeLister struct{ items []zotero.Item }

func (f fakeLister) ListAllItems() ([]zotero.Item, error) { return f.items, nil }

// fakeEmbedder returns a deterministic vector per input: [len(text)] so tests
// can assert wiring without a model. It records how many inputs it was asked to
// embed, to prove batching and skip behaviour.
type fakeEmbedder struct{ embedded int }

func (f *fakeEmbedder) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	f.embedded += len(inputs)
	out := make([][]float32, len(inputs))
	for i, s := range inputs {
		out[i] = []float32{float32(len(s))}
	}
	return out, nil
}

func item(key string, version int, title, abstract string) zotero.Item {
	return zotero.Item{Key: key, Version: version, Data: zotero.ItemData{Title: title, AbstractNote: abstract}}
}

// Contract: buildIndex embeds every item that has text (title/abstract), skips
// items with neither, stamps the header (library, model, dim from the first
// vector), and carries each item's Version for later incremental update.
func TestBuildIndexEmbedsAllWithTextAndSkipsEmpty(t *testing.T) {
	lister := fakeLister{items: []zotero.Item{
		item("AAAA1111", 3, "Title A", "Abstract A"),
		item("BBBB2222", 7, "Title B", ""),
		item("CCCC3333", 1, "", ""), // no text -> skipped
	}}
	emb := &fakeEmbedder{}

	idx, skipped, err := buildIndex(context.Background(), lister, emb, "user9", "model-x", 64)

	if err != nil {
		t.Fatalf("buildIndex: %v", err)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	if emb.embedded != 2 {
		t.Errorf("embedded %d inputs, want 2", emb.embedded)
	}
	if len(idx.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(idx.Entries))
	}
	if idx.Header.LibraryID != "user9" || idx.Header.Model != "model-x" || idx.Header.Dim != 1 {
		t.Errorf("header = %+v", idx.Header)
	}
	if idx.Entries[0].ItemKey != "AAAA1111" || idx.Entries[0].Version != 3 {
		t.Errorf("entry0 = %+v, want key AAAA1111 v3", idx.Entries[0])
	}
}

// Contract: batching splits inputs into batchSize chunks but still embeds every
// item exactly once — a large library must be fully indexed regardless of batch
// size.
func TestBuildIndexBatches(t *testing.T) {
	var items []zotero.Item
	for i := 0; i < 5; i++ {
		items = append(items, item("K"+string(rune('A'+i)), 1, "t", "a"))
	}
	emb := &fakeEmbedder{}

	idx, _, err := buildIndex(context.Background(), fakeLister{items: items}, emb, "u", "m", 2)

	if err != nil {
		t.Fatalf("buildIndex: %v", err)
	}
	if emb.embedded != 5 || len(idx.Entries) != 5 {
		t.Errorf("embedded=%d entries=%d, want 5/5", emb.embedded, len(idx.Entries))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go -C <repo> test ./cmd/zotero-cli/ -run TestBuildIndex`
Expected: FAIL — undefined `buildIndex`.

- [ ] **Step 3: Write the implementation**

Create `cmd/zotero-cli/index.go`:

```go
package main

import (
	"context"
	"time"

	"github.com/spf13/cobra"
	"github.com/yk0817/zotero-cli/embed"
	"github.com/yk0817/zotero-cli/semindex"
	"github.com/yk0817/zotero-cli/zotero"
)

// embedBatchSize bounds how many item texts are sent to the embedder per HTTP
// request, so a large library does not build one enormous request body.
const embedBatchSize = 64

// itemLister is the subset of *zotero.Client the index commands need, so tests
// substitute a fake.
type itemLister interface {
	ListAllItems() ([]zotero.Item, error)
}

// buildIndex fetches all items, embeds title+abstract for those with text (in
// batches), and returns a fresh index plus the count of items skipped for having
// no text. Dim is taken from the first embedded vector.
func buildIndex(ctx context.Context, lister itemLister, emb embed.Embedder, libraryID, model string, batchSize int) (*semindex.Index, int, error) {
	items, err := lister.ListAllItems()
	if err != nil {
		return nil, 0, err
	}

	var texts []string
	var withText []zotero.Item
	for _, it := range items {
		if txt := embedText(it); txt != "" {
			texts = append(texts, txt)
			withText = append(withText, it)
		}
	}
	skipped := len(items) - len(withText)

	entries := make([]semindex.Entry, 0, len(withText))
	dim := 0
	for start := 0; start < len(texts); start += batchSize {
		end := start + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := emb.Embed(ctx, texts[start:end])
		if err != nil {
			return nil, 0, err
		}
		for j, v := range vecs {
			if dim == 0 {
				dim = len(v)
			}
			it := withText[start+j]
			entries = append(entries, semindex.Entry{
				ItemKey: it.Key,
				Version: it.Version,
				Title:   it.Data.Title,
				Vector:  v,
			})
		}
	}

	idx := &semindex.Index{
		Header: semindex.Header{
			SchemaVersion: semindex.SchemaVersion,
			LibraryID:     libraryID,
			Model:         model,
			Dim:           dim,
			BuiltAt:       time.Now().UTC(),
		},
		Entries: entries,
	}
	return idx, skipped, nil
}

func newIndexCmd() *cobra.Command {
	indexCmd := &cobra.Command{
		Use:   "index",
		Short: "Manage the local semantic search index",
		Annotations: map[string]string{
			"args": "none — use a subcommand: build, update, status",
		},
	}

	buildCmd := &cobra.Command{
		Use:   "build",
		Short: "Build the semantic index from all library items (title + abstract)",
		Annotations: map[string]string{
			"args": "none",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			client := zotero.NewClient(cfg.APIKey, cfg.UserID)
			emb := resolveEmbedder(cfg)

			idx, skipped, err := buildIndex(cmd.Context(), client, emb, cfg.UserID, embedModel(cfg), embedBatchSize)
			if err != nil {
				return &CLIError{Code: ErrCodeAPIError, Message: err.Error(), Suggestion: "check that your embedding server is running at " + embedBaseURL(cfg)}
			}
			path := indexPath()
			if err := idx.Save(path); err != nil {
				return &CLIError{Code: ErrCodeIOError, Message: err.Error()}
			}

			if isJSON() {
				return printJSON(map[string]any{
					"indexed": len(idx.Entries),
					"skipped": skipped,
					"model":   idx.Header.Model,
					"dim":     idx.Header.Dim,
					"path":    path,
				})
			}
			printf := cmd.OutOrStdout()
			_, _ = printf.Write([]byte("Indexed " + itoa(len(idx.Entries)) + " items (skipped " + itoa(skipped) + ") -> " + path + "\n"))
			return nil
		},
	}

	indexCmd.AddCommand(buildCmd)
	return indexCmd
}
```

Add a small `itoa` helper if the file has no int-to-string yet (or use `strconv.Itoa` and import `strconv`; prefer `strconv.Itoa` and drop `itoa`). Use `fmt.Fprintf(cmd.OutOrStdout(), ...)` if you prefer — match the surrounding code's style (the rest of the CLI uses `fmt.Printf`; using `fmt.Fprintf(os.Stdout, ...)` is fine here too). Keep progress/log lines on stderr.

> Implementation note: prefer `fmt.Fprintf(os.Stdout, "Indexed %d items (skipped %d) -> %s\n", len(idx.Entries), skipped, path)` and `import "fmt"`/`"os"` to avoid a custom `itoa`. Adjust imports accordingly.

- [ ] **Step 4: Register the command**

In `cmd/zotero-cli/main.go`, add `indexCmd := newIndexCmd()` next to `versionCmd`/`schemaCmd`, and add `indexCmd` to the `rootCmd.AddCommand(...)` call.

- [ ] **Step 5: Run tests + build to verify**

Run: `go -C <repo> test -race ./cmd/zotero-cli/ -run TestBuildIndex`
Expected: PASS.
Run: `go -C <repo> build ./...`
Expected: no errors.

- [ ] **Step 6: gofmt + commit**

```bash
gofmt -w cmd/zotero-cli/index.go cmd/zotero-cli/index_test.go cmd/zotero-cli/main.go
git -C <repo> add cmd/zotero-cli/index.go cmd/zotero-cli/index_test.go cmd/zotero-cli/main.go
git -C <repo> commit -m "feat(cli): index build command

Refs #27"
```

---

### Task 8: CLI `index status` command

**Files:**
- Modify: `cmd/zotero-cli/index.go` (add `statusCmd` under `index`)
- Test: `cmd/zotero-cli/index_test.go` (add a status test using a temp HOME)

**Interfaces:**
- Consumes: `semindex.Load`, `indexPath`, `Config`.
- Produces: `statusCmd` wired under `newIndexCmd()`; uses existing `semindex` types.

- [ ] **Step 1: Write the failing test**

Add to `cmd/zotero-cli/index_test.go`:

```go
import (
	"path/filepath"
	"github.com/yk0817/zotero-cli/semindex"
	// (context, testing already imported)
)

// Contract: index status reads the on-disk index and reports its metadata with
// no network call. A missing index reports NOT_FOUND (so the user is told to
// build), not a crash or a generic error.
func TestIndexStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// No index yet -> NOT_FOUND.
	err := runIndexStatus()
	assertCLIErrorCode(t, err, ErrCodeNotFound)

	// Write an index, then status should succeed and see 1 entry.
	idx := &semindex.Index{
		Header:  semindex.Header{SchemaVersion: semindex.SchemaVersion, LibraryID: "u", Model: "m", Dim: 1},
		Entries: []semindex.Entry{{ItemKey: "K", Version: 1, Vector: []float32{1}}},
	}
	if err := idx.Save(filepath.Join(home, ".config", "zotero-cli", "index", "items.gob")); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := runIndexStatus(); err != nil {
		t.Fatalf("status after build: %v", err)
	}
}
```

> This test calls a small `runIndexStatus()` function (the RunE body extracted so it is testable without cobra plumbing). It relies on `configDir()` deriving from `$HOME`; confirm `configDir()` uses `os.UserHomeDir()` (it does) so `t.Setenv("HOME", ...)` redirects it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go -C <repo> test ./cmd/zotero-cli/ -run TestIndexStatus`
Expected: FAIL — undefined `runIndexStatus`.

- [ ] **Step 3: Write the implementation**

In `cmd/zotero-cli/index.go`, add the extracted function and wire a subcommand:

```go
// runIndexStatus loads the index and reports its metadata. Extracted from the
// cobra RunE so it is unit-testable. A missing index maps to NOT_FOUND with a
// build suggestion.
func runIndexStatus() error {
	path := indexPath()
	idx, err := semindex.Load(path)
	if err != nil {
		if errors.Is(err, semindex.ErrIndexNotFound) {
			return &CLIError{Code: ErrCodeNotFound, Message: "no semantic index found", Suggestion: "Run 'zotero-cli index build' first"}
		}
		return &CLIError{Code: ErrCodeIOError, Message: err.Error()}
	}
	if isJSON() {
		return printJSON(map[string]any{
			"path":    path,
			"indexed": len(idx.Entries),
			"model":   idx.Header.Model,
			"dim":     idx.Header.Dim,
			"builtAt": idx.Header.BuiltAt,
		})
	}
	fmt.Fprintf(os.Stdout, "Index: %s\nItems: %d\nModel: %s (dim %d)\nBuilt: %s\n",
		path, len(idx.Entries), idx.Header.Model, idx.Header.Dim, idx.Header.BuiltAt.Format(time.RFC3339))
	return nil
}
```

Add `"errors"`, `"fmt"`, `"os"` to the imports as needed. Then, inside `newIndexCmd()`, add:

```go
statusCmd := &cobra.Command{
	Use:         "status",
	Short:       "Show semantic index metadata (path, item count, model)",
	Annotations: map[string]string{"args": "none"},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runIndexStatus()
	},
}
indexCmd.AddCommand(statusCmd)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go -C <repo> test -race ./cmd/zotero-cli/ -run TestIndexStatus`
Expected: PASS.

- [ ] **Step 5: gofmt + commit**

```bash
gofmt -w cmd/zotero-cli/index.go cmd/zotero-cli/index_test.go
git -C <repo> add cmd/zotero-cli/index.go cmd/zotero-cli/index_test.go
git -C <repo> commit -m "feat(cli): index status command

Refs #27"
```

---

### Task 9: CLI `index update` command (incremental)

**Files:**
- Modify: `cmd/zotero-cli/index.go` (add `updateIndex` + `updateCmd`)
- Test: `cmd/zotero-cli/index_test.go`

**Interfaces:**
- Consumes: `semindex.Index/Entry/ItemRef/Diff` (Tasks 2/4); `embed.Embedder`; `itemLister`, `embedText` (Tasks 6/7).
- Produces:
  - `type updateStats struct { Added, Updated, Removed, Unchanged, Total int }`
  - `func updateIndex(ctx context.Context, lister itemLister, emb embed.Embedder, existing *semindex.Index, batchSize int) (*semindex.Index, updateStats, error)`

- [ ] **Step 1: Write the failing test**

Add to `cmd/zotero-cli/index_test.go`:

```go
// Contract: updateIndex re-embeds only new/changed items, keeps unchanged
// entries without an embed call, drops removed items, and reports accurate
// counts. This is the cost-saving contract of incremental update; a wrong bucket
// means stale vectors or needless API calls.
func TestUpdateIndexIncremental(t *testing.T) {
	existing := &semindex.Index{
		Header: semindex.Header{SchemaVersion: semindex.SchemaVersion, LibraryID: "u", Model: "m", Dim: 1},
		Entries: []semindex.Entry{
			{ItemKey: "KEEP", Version: 1, Title: "keep", Vector: []float32{1}},
			{ItemKey: "CHANGED", Version: 1, Title: "old", Vector: []float32{1}},
			{ItemKey: "GONE", Version: 1, Title: "gone", Vector: []float32{1}},
		},
	}
	lister := fakeLister{items: []zotero.Item{
		item("KEEP", 1, "keep", "a"),
		item("CHANGED", 2, "changed", "a"),
		item("NEW", 1, "new", "a"),
	}}
	emb := &fakeEmbedder{}

	idx, stats, err := updateIndex(context.Background(), lister, emb, existing, 64)

	if err != nil {
		t.Fatalf("updateIndex: %v", err)
	}
	if emb.embedded != 2 {
		t.Errorf("embedded %d, want 2 (CHANGED, NEW only)", emb.embedded)
	}
	if stats.Added != 1 || stats.Updated != 1 || stats.Removed != 1 || stats.Unchanged != 1 {
		t.Errorf("stats = %+v, want added1 updated1 removed1 unchanged1", stats)
	}
	if stats.Total != 3 || len(idx.Entries) != 3 {
		t.Errorf("total=%d entries=%d, want 3/3 (GONE dropped)", stats.Total, len(idx.Entries))
	}
	// The changed entry must carry the new version and refreshed title.
	for _, e := range idx.Entries {
		if e.ItemKey == "CHANGED" && (e.Version != 2 || e.Title != "changed") {
			t.Errorf("CHANGED entry not refreshed: %+v", e)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go -C <repo> test ./cmd/zotero-cli/ -run TestUpdateIndexIncremental`
Expected: FAIL — undefined `updateIndex`.

- [ ] **Step 3: Write the implementation**

Add to `cmd/zotero-cli/index.go`:

```go
type updateStats struct {
	Added     int
	Updated   int
	Removed   int
	Unchanged int
	Total     int
}

// updateIndex re-embeds only new/changed items (by version), keeps unchanged
// entries, drops removed ones, and returns the merged index plus counts. The
// header is carried over from the existing index (same library/model/dim).
func updateIndex(ctx context.Context, lister itemLister, emb embed.Embedder, existing *semindex.Index, batchSize int) (*semindex.Index, updateStats, error) {
	items, err := lister.ListAllItems()
	if err != nil {
		return nil, updateStats{}, err
	}

	byKey := make(map[string]zotero.Item, len(items))
	refs := make([]semindex.ItemRef, 0, len(items))
	for _, it := range items {
		byKey[it.Key] = it
		refs = append(refs, semindex.ItemRef{Key: it.Key, Version: it.Version})
	}
	oldKeys := make(map[string]bool, len(existing.Entries))
	for _, e := range existing.Entries {
		oldKeys[e.ItemKey] = true
	}

	toEmbed, keep, removed := existing.Diff(refs)

	// Embed only items that have text; a changed item that lost its text is
	// effectively removed from the index.
	var texts []string
	var embItems []zotero.Item
	added, updated := 0, 0
	for _, key := range toEmbed {
		it := byKey[key]
		if embedText(it) == "" {
			continue
		}
		texts = append(texts, embedText(it))
		embItems = append(embItems, it)
		if oldKeys[key] {
			updated++
		} else {
			added++
		}
	}

	newEntries := make([]semindex.Entry, 0, len(embItems))
	for start := 0; start < len(texts); start += batchSize {
		end := start + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := emb.Embed(ctx, texts[start:end])
		if err != nil {
			return nil, updateStats{}, err
		}
		for j, v := range vecs {
			it := embItems[start+j]
			newEntries = append(newEntries, semindex.Entry{ItemKey: it.Key, Version: it.Version, Title: it.Data.Title, Vector: v})
		}
	}

	merged := append(keep, newEntries...)
	idx := &semindex.Index{Header: existing.Header, Entries: merged}
	idx.Header.BuiltAt = time.Now().UTC()

	stats := updateStats{
		Added:     added,
		Updated:   updated,
		Removed:   len(removed),
		Unchanged: len(keep),
		Total:     len(merged),
	}
	return idx, stats, nil
}
```

Then add the `update` subcommand inside `newIndexCmd()`:

```go
updateCmd := &cobra.Command{
	Use:         "update",
	Short:       "Incrementally update the semantic index (only changed items)",
	Annotations: map[string]string{"args": "none"},
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		existing, err := semindex.Load(indexPath())
		if err != nil {
			if errors.Is(err, semindex.ErrIndexNotFound) {
				return &CLIError{Code: ErrCodeNotFound, Message: "no semantic index found", Suggestion: "Run 'zotero-cli index build' first"}
			}
			return &CLIError{Code: ErrCodeIOError, Message: err.Error()}
		}
		if err := existing.CompatibleWith(cfg.UserID, embedModel(cfg)); err != nil {
			return &CLIError{Code: ErrCodeValidation, Message: err.Error()}
		}
		client := zotero.NewClient(cfg.APIKey, cfg.UserID)
		emb := resolveEmbedder(cfg)

		idx, stats, err := updateIndex(cmd.Context(), client, emb, existing, embedBatchSize)
		if err != nil {
			return &CLIError{Code: ErrCodeAPIError, Message: err.Error(), Suggestion: "check that your embedding server is running at " + embedBaseURL(cfg)}
		}
		if err := idx.Save(indexPath()); err != nil {
			return &CLIError{Code: ErrCodeIOError, Message: err.Error()}
		}
		if isJSON() {
			return printJSON(map[string]any{
				"added": stats.Added, "updated": stats.Updated, "removed": stats.Removed,
				"unchanged": stats.Unchanged, "total": stats.Total,
			})
		}
		fmt.Fprintf(os.Stdout, "Updated index: +%d ~%d -%d (unchanged %d, total %d)\n",
			stats.Added, stats.Updated, stats.Removed, stats.Unchanged, stats.Total)
		return nil
	},
}
indexCmd.AddCommand(updateCmd)
```

- [ ] **Step 4: Run tests + build**

Run: `go -C <repo> test -race ./cmd/zotero-cli/ -run TestUpdateIndexIncremental`
Expected: PASS.
Run: `go -C <repo> build ./...`
Expected: no errors.

- [ ] **Step 5: gofmt + commit**

```bash
gofmt -w cmd/zotero-cli/index.go cmd/zotero-cli/index_test.go
git -C <repo> add cmd/zotero-cli/index.go cmd/zotero-cli/index_test.go
git -C <repo> commit -m "feat(cli): index update command (incremental by version)

Refs #27"
```

---

### Task 10: CLI `search --semantic`

**Files:**
- Modify: `cmd/zotero-cli/main.go` (add `--semantic` and `--limit` flags to `searchCmd`; branch into the semantic path)
- Modify: `cmd/zotero-cli/semantic.go` (add `searchSemantic` orchestration)
- Test: `cmd/zotero-cli/semantic_test.go`

**Interfaces:**
- Consumes: `semindex.Index/Result/Search` (Tasks 2/3); `embed.Embedder`.
- Produces: `func searchSemantic(ctx context.Context, idx *semindex.Index, emb embed.Embedder, query string, limit int) ([]semindex.Result, error)`

- [ ] **Step 1: Write the failing test**

Add to `cmd/zotero-cli/semantic_test.go`:

```go
import (
	"context"
	"github.com/yk0817/zotero-cli/semindex"
)

// queryEmbedder returns a fixed query vector regardless of input, so the test
// controls which index entry ranks first.
type queryEmbedder struct{ vec []float32 }

func (q queryEmbedder) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	out := make([][]float32, len(inputs))
	for i := range inputs {
		out[i] = q.vec
	}
	return out, nil
}

// Contract: searchSemantic embeds the query once and returns index entries
// ranked by similarity, truncated to limit. This is the user-visible semantic
// ranking; wrong order or wrong count defeats the feature.
func TestSearchSemanticRanks(t *testing.T) {
	idx := &semindex.Index{Entries: []semindex.Entry{
		{ItemKey: "FAR", Title: "far", Vector: []float32{0, 1}},
		{ItemKey: "NEAR", Title: "near", Vector: []float32{1, 0}},
	}}
	emb := queryEmbedder{vec: []float32{1, 0}}

	got, err := searchSemantic(context.Background(), idx, emb, "anything", 10)

	if err != nil {
		t.Fatalf("searchSemantic: %v", err)
	}
	if len(got) != 2 || got[0].ItemKey != "NEAR" {
		t.Fatalf("ranking = %+v, want NEAR first", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go -C <repo> test ./cmd/zotero-cli/ -run TestSearchSemanticRanks`
Expected: FAIL — undefined `searchSemantic`.

- [ ] **Step 3: Write the orchestration**

Add to `cmd/zotero-cli/semantic.go`:

```go
import (
	"context"
	"fmt"

	"github.com/yk0817/zotero-cli/semindex"
)

// searchSemantic embeds the query and returns index entries ranked by cosine
// similarity, truncated to limit (limit <= 0 returns all).
func searchSemantic(ctx context.Context, idx *semindex.Index, emb embed.Embedder, query string, limit int) ([]semindex.Result, error) {
	vecs, err := emb.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("embedder returned %d vectors for the query, want 1", len(vecs))
	}
	return idx.Search(vecs[0], limit), nil
}
```

(Merge these imports with the existing import block in `semantic.go`.)

- [ ] **Step 4: Wire the flags into `searchCmd`**

In `cmd/zotero-cli/main.go`, add flag vars and flags to `searchCmd`:

```go
var searchSemanticFlag bool
var searchLimit int
// ... after existing flag definitions:
searchCmd.Flags().BoolVar(&searchSemanticFlag, "semantic", false, "Semantic (embedding) search over the local index instead of keyword search")
searchCmd.Flags().IntVar(&searchLimit, "limit", 10, "Max results (semantic search)")
```

At the top of `searchCmd.RunE`, before the existing keyword logic, branch:

```go
query := strings.Join(args, " ")
if err := sanitizeInput(query); err != nil {
	return err
}
if searchSemanticFlag {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	idx, err := semindex.Load(indexPath())
	if err != nil {
		if errors.Is(err, semindex.ErrIndexNotFound) {
			return &CLIError{Code: ErrCodeNotFound, Message: "no semantic index found", Suggestion: "Run 'zotero-cli index build' first"}
		}
		return &CLIError{Code: ErrCodeIOError, Message: err.Error()}
	}
	if err := idx.CompatibleWith(cfg.UserID, embedModel(cfg)); err != nil {
		return &CLIError{Code: ErrCodeValidation, Message: err.Error()}
	}
	results, err := searchSemantic(cmd.Context(), idx, resolveEmbedder(cfg), query, searchLimit)
	if err != nil {
		return &CLIError{Code: ErrCodeAPIError, Message: err.Error(), Suggestion: "check that your embedding server is running at " + embedBaseURL(cfg)}
	}
	if isJSON() {
		if results == nil {
			results = []semindex.Result{}
		}
		return printJSON(results)
	}
	if len(idx.Entries) == 0 {
		fmt.Println("No results — the index is empty, run 'zotero-cli index build'")
		return nil
	}
	printSemanticResults(results)
	return nil
}
// (existing keyword search continues below, reusing `query`)
```

> Refactor note: the existing `searchCmd.RunE` already computes `query` and calls `sanitizeInput`. Do not duplicate those — move the semantic branch to just after the existing `sanitizeInput(query)` call and let the keyword path continue unchanged. Remove the now-duplicated `query :=`/`sanitizeInput` lines from the snippet above when integrating.

Add `printSemanticResults` near the other `print*Table` helpers in `main.go`:

```go
// printSemanticResults renders ranked semantic hits as a table.
func printSemanticResults(results []semindex.Result) {
	if len(results) == 0 {
		fmt.Println("No results found")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "RANK\tSCORE\tKEY\tTITLE")
	fmt.Fprintln(w, "----\t-----\t---\t-----")
	for i, r := range results {
		fmt.Fprintf(w, "%d\t%.3f\t%s\t%s\n", i+1, r.Score, r.ItemKey, zotero.Truncate(r.Title, 60))
	}
	w.Flush()
}
```

Add `"github.com/yk0817/zotero-cli/semindex"` and `"errors"` to `main.go` imports if not present.

- [ ] **Step 5: Run tests + build**

Run: `go -C <repo> test -race ./cmd/zotero-cli/ -run TestSearchSemanticRanks`
Expected: PASS.
Run: `go -C <repo> build ./...` and `go -C <repo> test -race ./...`
Expected: all green.

- [ ] **Step 6: Manually verify the CLI wiring (no network)**

Run: `go -C <repo> run ./cmd/zotero-cli search --semantic "test" --output json`
Expected: a JSON error envelope with code `NOT_FOUND` (no index built) and the build suggestion — proving the flag is wired and the error path is correct.
Run: `go -C <repo> run ./cmd/zotero-cli index --output json` (no subcommand) shows help; `index build`/`update`/`status` appear.

- [ ] **Step 7: gofmt + commit**

```bash
gofmt -w cmd/zotero-cli/main.go cmd/zotero-cli/semantic.go cmd/zotero-cli/semantic_test.go
git -C <repo> add cmd/zotero-cli/main.go cmd/zotero-cli/semantic.go cmd/zotero-cli/semantic_test.go
git -C <repo> commit -m "feat(cli): search --semantic over the local index

Refs #27"
```

---

### Task 11: Docs — README regen, semantic-search section, qa-perspectives

**Files:**
- Modify: `README.md` (auto-generated command table + a new "Semantic search" prose section)
- Modify: `knowledge-base/qa-perspectives.md` (record the new P1 perspectives)

**Interfaces:** none (docs only).

- [ ] **Step 1: Regenerate the command table**

Run: `bash <repo>/scripts/gen-readme.sh`
Then verify: `bash <repo>/scripts/gen-readme.sh --check`
Expected: "README.md is up to date." and the new `index` and (already-present) `search` rows reflect the new commands/flags.

- [ ] **Step 2: Add a "Semantic search" prose section to README**

Insert after the existing search/AI-agent section (bilingual, English-first). Content to include verbatim:

```markdown
### Semantic search (local, optional)

`search --semantic` ranks library items by meaning, not keywords, using a local
vector index built from each item's title + abstract.

```bash
# One-time (and after adding papers) — build / refresh the index:
zotero-cli index build
zotero-cli index update     # incremental: only re-embeds changed items
zotero-cli index status     # show index metadata

# Search by concept:
zotero-cli search --semantic "temporal edges in dynamic knowledge graphs" --limit 10 --output json
```

Embeddings come from an OpenAI-compatible endpoint. **The default is local**
(Ollama at `http://localhost:11434/v1`, model `nomic-embed-text`) — no key or
cloud needed:

```bash
ollama pull nomic-embed-text   # once
```

To use a different or remote backend, set `embed_base_url` / `embed_model` via
`zotero-cli config`, and (for a remote API) export `ZOTERO_EMBED_API_KEY`. The key
is read only from the environment and never written to `config.json`.

Semantic results are `{itemKey, title, score}` ranked by cosine similarity; use
`zotero-cli get <itemKey>` for full details.

意味検索（ローカル・任意）: `search --semantic` は title+abstract から作った
ローカルのベクトルインデックスで、キーワードでなく意味で並べ替える。既定はローカル
（Ollama）で鍵不要。`index build/update/status` でインデックスを管理する。
```

(Adjust the surrounding heading level to match the README's existing structure.)

- [ ] **Step 3: Update qa-perspectives.md**

In `knowledge-base/qa-perspectives.md`, under "P1: API 境界" pagination bullet, and in the "過去バグ由来の観点" table, add rows tying the new behaviour to tests:

```markdown
| 全ライブラリ列挙のページ切り捨て | `ListItems` は1ページ（≤100）しか返さず、意味検索インデックスが部分ライブラリで構築される恐れ（#27） | `TestListAllItemsPaginates` |
| 意味検索の出力契約 | `search --semantic` は `{itemKey,title,score}` エンベロープを守り、空インデックスは明示メッセージ（#27） | `TestSearchSemanticRanks` / CLI 空インデックス表示 |
```

Also add a one-line perspective under "P1: 出力契約": semantic search results MUST be ranked descending and truncated to `--limit`.

- [ ] **Step 4: Verify docs build clean**

Run: `bash <repo>/scripts/gen-readme.sh --check`
Expected: up to date.
Run: `go -C <repo> test -race ./...`
Expected: all green (no code change here, but confirm nothing drifted).

- [ ] **Step 5: Commit**

```bash
git -C <repo> add README.md knowledge-base/qa-perspectives.md
git -C <repo> commit -m "docs: semantic search usage + qa perspectives

Refs #27"
```

---

## Final verification (before opening the PR)

- [ ] `go -C <repo> build ./...` — clean.
- [ ] `go -C <repo> vet ./...` — clean.
- [ ] `go -C <repo> test -race ./...` — all packages green.
- [ ] `bash <repo>/scripts/gen-readme.sh --check` — README up to date.
- [ ] `gofmt -l embed semindex cmd zotero` — no output.
- [ ] Manual smoke: `search --semantic "x" --output json` → `NOT_FOUND` envelope with build suggestion; `index status --output json` → `NOT_FOUND` before build; `schema --output json | jq '.[].name'` includes `index` and shows `search`'s new `--semantic`/`--limit` flags.
- [ ] Self-review via `go-reviewer` on the full branch diff; fix CONFIRMED findings.
- [ ] Open the draft PR via `scripts/issue-pr.sh` with `Closes #27` in the body (Slice B is tracked separately in #38).

## Spec coverage check

| Spec requirement | Task |
|---|---|
| `Embedder` + OpenAI-compatible HTTP, local default | 1 |
| Flat-file gob index, atomic save, header identity | 2 |
| Brute-force cosine + ranked search | 3 |
| Incremental diff by version | 4 |
| Paginating `ListAllItems` (no truncation) | 5 |
| Config fields + env-only key + index path + embed text | 6 |
| `index build` (all items, batched, skip empty) | 7 |
| `index status` (no network, NOT_FOUND when missing) | 8 |
| `index update` (incremental, counts) | 9 |
| `search --semantic` (ranked, JSON envelope, empty message) | 10 |
| README (schema + prose) + qa-perspectives | 11 |
