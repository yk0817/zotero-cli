# Semantic search — Phase 1 (Slice A) design

Design doc for [#27](https://github.com/yk0817/zotero-cli/issues/27), Slice A. This
covers **item-level semantic search only**; RAG chunk retrieval and MCP tools
(Phase 2) are split into a separate follow-up issue.

Status: approved (design), pre-implementation.

## 日本語 TL;DR

`search --semantic <query>` を追加する。ライブラリの各アイテムを **title+abstract
から 1 ベクトル**にして OpenAI 互換の埋め込み API（**既定はローカル**の Ollama 等、
リモートも同一コード）で埋め込み、`~/.config/zotero-cli/index/items.gob` に
フラットファイルとして保存。検索は **pure Go の総当たり cosine**（個人規模で十分）。
`index build`（全再構築）/ `index update`（`version` 差分で変更分だけ再埋め込み）/
`index status`。全文・注釈のチャンク化と RAG・MCP は Phase 2（別 Issue）に先送り。

## Goals / non-goals

### In scope (Slice A)

- An `Embedder` abstraction over an OpenAI-compatible `POST /v1/embeddings` HTTP
  endpoint; **local default** (Ollama / llama.cpp / LM Studio), remote (OpenAI /
  Gemini-compatible) via the same code path with a different base URL + key.
- A local, flat-file vector index over **items** (one vector per item, built from
  `title + abstract`).
- Commands: `index build`, `index update`, `index status`, and `search --semantic`.
- Brute-force cosine search in pure Go (no ANN, no cgo — keeps the binary light).

### Out of scope (→ Phase 2 follow-up issue)

- Full-text and annotation/note embedding; chunk-level retrieval.
- RAG retrieval tool returning top-k chunks with citations.
- MCP tools (`zotero_semantic_search`, `zotero_rag_query`).
- Approximate-nearest-neighbour indexes (HNSW etc.).

### Constraints

- Keep the core binary light: no cgo, no bundled model, no heavy vector DB.
- English-first bilingual docs, comments, and issue text (repo convention).
- Hermetic, deterministic tests; every source is injectable so tests run offline.
- Honour the `{"ok":true,"data":...}` JSON envelope; empty results are explicit,
  never an empty string.

## Package layout

New sibling packages, matching the existing `oapdf` / `resolve` / `scholar` style
(small, focused, injectable HTTP):

| Package | Responsibility |
|---|---|
| `embed/` | `Embedder` interface + `HTTPEmbedder` (OpenAI-compatible `/v1/embeddings`). |
| `semindex/` | Flat-file vector index: types, gob load/save, cosine search, incremental diff. |
| `zotero/` (extend) | `ListAllItems()` — paginating full-library enumerator. |
| `cmd/zotero-cli/` | `index.go` (`index build/update/status`) + `--semantic` flag on `search`. |

### `embed` package

```go
// Embedder turns text into vectors. One method, batched, injectable — so the
// index builder and the search path both depend on the interface, and tests use
// a deterministic fake.
type Embedder interface {
    Embed(ctx context.Context, inputs []string) ([][]float32, error)
}

// HTTPEmbedder speaks the OpenAI-compatible POST {BaseURL}/embeddings shape.
// The same struct talks to a local server (Ollama/llama.cpp/LM Studio) or a
// remote API (OpenAI / Gemini-compatible); only BaseURL/APIKey differ.
type HTTPEmbedder struct {
    BaseURL    string       // e.g. http://localhost:11434/v1
    Model      string       // e.g. nomic-embed-text
    APIKey     string       // optional; sent as Authorization: Bearer when non-empty
    HTTPClient *http.Client // injectable; defaults to a bounded-timeout client
}
```

- Request body: `{"model": Model, "input": inputs}`. Response: `{"data":[{"embedding":[...]}]}`
  in input order. Parse defensively; a length/order mismatch is an error.
- `Authorization: Bearer <APIKey>` header **only when a key is set** (local needs none).
- Bounded HTTP timeout. Batching: callers pass a slice; `HTTPEmbedder` sends it in
  one request per call (the index builder chunks large libraries into fixed-size
  batches, e.g. 64, before calling).
- Errors are wrapped with `%w`; the API key must never appear in an error string.
- **No SSRF dial guard** here, unlike `oapdf`: the embedder's base URL comes from
  the user's own config (not from remote data), and the *default is `localhost`* —
  the very target `oapdf`'s guard blocks. Do not copy that guard into `embed`.

### `semindex` package

```go
const schemaVersion = 1

// Header binds an index file to the library + model it was built with, so a
// model/dimension/account change is detected on load instead of silently mixing
// incompatible vectors.
type Header struct {
    SchemaVersion int
    LibraryID     string // Zotero UserID
    Model         string
    Dim           int
    BuiltAt       time.Time // display metadata only; never asserted in tests
}

// Entry is one item's vector plus the minimum needed to rank and display it and
// to decide incremental re-embedding.
type Entry struct {
    ItemKey string
    Version int     // Zotero item version — drives incremental update
    Title   string  // shown in results without a second API call
    Vector  []float32
}

type Index struct {
    Header  Header
    Entries []Entry
}
```

Behaviour:

- `Load(path) (*Index, error)` — gob-decode; missing file is a distinct sentinel
  error (`ErrIndexNotFound`) so the CLI can suggest `index build`.
- `(*Index) Save(path) error` — **atomic**: encode to `path.tmp`, then rename over
  `path`. A failed build never leaves a half-written index.
- `(*Index) Search(query []float32, k int) []Result` — brute-force cosine over all
  entries, sorted by score desc, top `k`. `Result{ItemKey, Title, Score float64}`.
- `Diff(current []zotero.Item, idx *Index) (toEmbed []zotero.Item, keep []Entry, removed []string)`
  — pure function for incremental update: an item absent from the index or with a
  changed `Version` goes to `toEmbed`; unchanged entries are kept as-is; index
  entries whose key is absent from `current` are `removed`.
- `CompatibleWith(header Header, libraryID, model string) error` — returns a
  descriptive error when the on-disk header does not match the active library or
  model (caller maps it to `VALIDATION_ERROR` + a rebuild hint).
- Cosine helper handles zero vectors without NaN (returns 0 similarity).

### `zotero.ListAllItems`

`ListItems` sends a single `limit` and returns one page (≤100). A whole-library
build must not stop at one page. Add:

```go
// ListAllItems enumerates every top-level item in the library (excluding
// attachments and notes), following Zotero pagination via `start` until fewer
// than a full page is returned (or Total-Results is reached).
func (c *Client) ListAllItems() ([]Item, error)
```

- Uses `itemType=-attachment || note`, a fixed page size of 100, and advances
  `start` by the page size until exhausted.
- This is the pagination P1 from `qa-perspectives.md` ("no silent truncation at
  25/100"): a contract test asserts it fetches across multiple pages.

## CLI surface

All commands honour `--output json` and appear in `schema` + the auto-generated
README table (`scripts/gen-readme.sh`).

### `index build`

Full rebuild. `ListAllItems` → embed `title + "\n" + abstract` in batches → write
the index atomically. Progress goes to **stderr** (`Embedding 128/540...`) so it
never pollutes captured stdout / JSON. On success:

```json
{"ok": true, "data": {"indexed": 540, "model": "nomic-embed-text", "dim": 768, "path": "/…/items.gob"}}
```

An item with an empty `title` and empty `abstract` is skipped and counted under
`skipped` (nothing to embed).

### `index update`

Incremental. `ListAllItems` → `semindex.Diff` → embed only new/changed items →
merge with kept entries → drop removed → atomic write.

```json
{"ok": true, "data": {"added": 3, "updated": 1, "removed": 2, "unchanged": 534, "total": 536}}
```

If no index exists yet, return `NOT_FOUND` with a suggestion to run `index build`
(update does not implicitly full-build).

### `index status`

No network. Reports the header + count from the on-disk index:

```json
{"ok": true, "data": {"path": "/…/items.gob", "indexed": 540, "model": "nomic-embed-text", "dim": 768, "builtAt": "2026-07-18T…Z"}}
```

Missing index → `NOT_FOUND` + build suggestion.

### `search --semantic <query> [--limit N]`

Load index → validate header against active config → embed the query (1 call) →
cosine top-k. Default `--limit` = 10. Query is run through the same `sanitizeInput`
guard as keyword `search`.

```json
{"ok": true, "data": [{"itemKey": "ABCD1234", "title": "Attention Is All You Need", "score": 0.83}]}
```

- Text mode: a ranked table (`RANK  SCORE  KEY  TITLE`).
- Empty index or zero entries → explicit message (`No results — the index is empty,
  run 'zotero-cli index build'`), never an empty string / bare `[]` with no context
  in text mode. JSON mode returns `data: []`.
- The shape intentionally differs from keyword `search` (which returns full
  `[]Item`): semantic search is a ranked operation, so it returns `{itemKey, title,
  score}` and leaves full detail to `get <key>`. Documented in the README.

## Configuration & secrets

- `config.json` gains two **non-secret** fields:
  - `embed_base_url` (default `http://localhost:11434/v1`, Ollama's OpenAI-compatible endpoint)
  - `embed_model` (default `nomic-embed-text`)
  - Added as optional prompts to the interactive `config` command; blank keeps the default.
- **API key via environment only**: `ZOTERO_EMBED_API_KEY`. Never written to
  `config.json`, never printed to stdout/stderr. Local backends need no key. This
  follows the repo's secret-handling rules (same reasoning as not logging secrets).
- Resolution: config value if set, else the built-in default; the env var supplies
  only the key. No per-command flags for base-URL/model in v1 (YAGNI — switching
  models means rebuilding, and the header records which model built the index).

## Error handling

| Situation | Code | Message / suggestion |
|---|---|---|
| Index file missing (`search`/`update`/`status`) | `NOT_FOUND` | "run `zotero-cli index build`" |
| Header library/model/dim mismatch | `VALIDATION_ERROR` | "index was built for a different model/account; run `index build`" |
| Embedding server unreachable / non-2xx | `API_ERROR` | "is your embedding server running at `<base_url>`?" |
| Empty query | `INVALID_ARGUMENT` | "provide a search query" |

`index build` is all-or-nothing: any embedding failure aborts the build and leaves
the previous index untouched (atomic write). Reusing existing error codes keeps the
documented error-code table unchanged (no new codes for v1).

## Testing

Hermetic and deterministic; every test carries a `// Contract:` comment (hook-enforced).
Read `knowledge-base/qa-perspectives.md` and `knowledge-base/test-style-guide.md`
before writing tests; map each P1 below to at least one test.

- **`embed`** (stub `http.RoundTripper`, oapdf pattern):
  - request body carries `model` + `input`; response embeddings map back in input order.
  - batch of N inputs → N vectors; a length mismatch is an error.
  - `Authorization` header present iff a key is configured.
  - non-2xx / transport error propagates (status in the message), key never in the error.
- **`semindex`** (fake `Embedder`, temp dir):
  - cosine: orthogonal → 0, identical → 1, zero vector → 0 (no NaN).
  - `Search` returns entries in descending score with correct top-k truncation.
  - `Diff`: new key → embed; changed `Version` → embed; missing key → removed;
    unchanged → kept without re-embedding.
  - gob round-trip preserves header + entries; `Load` of a missing file → `ErrIndexNotFound`.
  - `CompatibleWith` rejects a mismatched model/library/dim.
  - `Save` is atomic (no `.tmp` left; original intact if encode fails).
- **`zotero.ListAllItems`** (recording transport): follows pagination across >1 page
  (P1: no truncation); excludes attachments/notes; empty library → empty slice, not error.
- **CLI** (temp index dir + fake embedder + stubbed client):
  - `search --semantic --output json` obeys the envelope; empty index → explicit message.
  - query is sanitized before use; missing index → `NOT_FOUND`.
  - new commands appear in `schema` output (discoverability P2).

`qa-perspectives.md` is updated per the issue AC: the semantic-search output-contract
and the `ListAllItems` pagination perspective are recorded.

## Acceptance criteria (Slice A)

- [ ] `index build` embeds every library item (title+abstract) into a local index,
      following pagination (no truncation), writing atomically.
- [ ] `index update` re-embeds only new/changed items (by `Version`) and drops deleted ones.
- [ ] `index status` reports the index metadata without network access.
- [ ] `search --semantic` returns items ranked by cosine similarity as a JSON envelope.
- [ ] Embedding backend is configurable (local default via config; remote via base-URL + env key).
- [ ] New commands appear in `schema` and the auto-generated README table.
- [ ] Contract tests cover the P1 perspectives above; `qa-perspectives.md` updated.

## Follow-up (Phase 2, separate issue)

Full-text + annotation chunking, a `rag`/retrieval command returning top-k chunks
with citations (item key, title, locator), and read-only MCP tools
`zotero_semantic_search` / `zotero_rag_query`. Filed as a new issue linked to #27.
