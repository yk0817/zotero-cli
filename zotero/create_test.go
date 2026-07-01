package zotero

import (
	"encoding/json"
	"net/http"
	"testing"
)

const createItemSuccess = `{"successful":{"0":{"key":"ITEM9999"}},"failed":{}}`

// decodeItemPayload decodes the POST body of a create-item request (a JSON
// array of one item object) and returns that object, so tests assert on the
// parsed payload rather than raw-string matching the request body.
func decodeItemPayload(t *testing.T, body []byte) map[string]interface{} {
	t.Helper()
	var payload []map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("request body is not a JSON array: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("expected 1 item in payload, got %d", len(payload))
	}
	return payload[0]
}

// Contract: BuildItemPayload always emits creators and tags as arrays, so a
// resolved item with no authors or tags serializes them as [] rather than
// null — the Zotero API rejects a null creators/tags field, and downstream
// tooling relies on the "empty is []" convention (qa-perspectives: 未指定スライ
// スが JSON null 化).
func TestBuildItemPayloadCreatorsAndTagsAreAlwaysArrays(t *testing.T) {
	payload := BuildItemPayload(ItemData{ItemType: "journalArticle", Title: "T"})

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	creators, ok := decoded["creators"].([]interface{})
	if !ok {
		t.Fatalf("creators is not a JSON array: %T (%v)", decoded["creators"], decoded["creators"])
	}
	if len(creators) != 0 {
		t.Errorf("expected empty creators array, got %v", creators)
	}
	tags, ok := decoded["tags"].([]interface{})
	if !ok {
		t.Fatalf("tags is not a JSON array: %T (%v)", decoded["tags"], decoded["tags"])
	}
	if len(tags) != 0 {
		t.Errorf("expected empty tags array, got %v", tags)
	}
}

// Contract: BuildItemPayload omits optional bibliographic fields that are
// empty, so a book (which has no publicationTitle) does not send an empty
// publicationTitle that Zotero would reject as an invalid field for that item
// type. Only fields actually populated by the resolver are transmitted.
func TestBuildItemPayloadOmitsEmptyOptionalFields(t *testing.T) {
	payload := BuildItemPayload(ItemData{ItemType: "book", Title: "Only a title"})

	for _, key := range []string{"date", "abstractNote", "url", "DOI", "ISBN", "publisher", "publicationTitle", "collections"} {
		if _, present := payload[key]; present {
			t.Errorf("expected %q to be omitted when empty, but it was present: %v", key, payload[key])
		}
	}
	if payload["itemType"] != "book" {
		t.Errorf("expected itemType book, got %v", payload["itemType"])
	}
	if payload["title"] != "Only a title" {
		t.Errorf("expected title, got %v", payload["title"])
	}
}

// Contract: a person creator (first/last name) is transmitted without an empty
// "name" field, so Zotero stores it as a two-field personal name rather than
// seeing an ambiguous object carrying both name and firstName/lastName. An
// organisation creator, conversely, is sent under "name" only.
func TestBuildItemPayloadPersonCreatorOmitsEmptyName(t *testing.T) {
	payload := BuildItemPayload(ItemData{
		ItemType: "journalArticle",
		Creators: []Creator{
			{CreatorType: "author", FirstName: "Ada", LastName: "Lovelace"},
			{CreatorType: "author", Name: "OpenAI"},
		},
	})

	raw, err := json.Marshal(payload["creators"])
	if err != nil {
		t.Fatalf("marshal creators: %v", err)
	}
	var creators []map[string]interface{}
	if err := json.Unmarshal(raw, &creators); err != nil {
		t.Fatalf("unmarshal creators: %v", err)
	}
	if len(creators) != 2 {
		t.Fatalf("expected 2 creators, got %d", len(creators))
	}

	person := creators[0]
	if _, has := person["name"]; has {
		t.Errorf("person creator should not carry a name field: %v", person)
	}
	if person["firstName"] != "Ada" || person["lastName"] != "Lovelace" {
		t.Errorf("person creator = %v, want Ada Lovelace", person)
	}

	org := creators[1]
	if org["name"] != "OpenAI" {
		t.Errorf("organisation creator = %v, want name OpenAI", org)
	}
	if _, has := org["firstName"]; has {
		t.Errorf("organisation creator should not carry firstName: %v", org)
	}
}

// Contract: BuildItemPayload transmits DOI and ISBN under Zotero's exact field
// names (uppercase "DOI" and "ISBN"). A lowercase key would be dropped by the
// API as an unknown field, silently losing the identifier and breaking later
// duplicate detection that matches on it.
func TestBuildItemPayloadUsesUppercaseIdentifierKeys(t *testing.T) {
	payload := BuildItemPayload(ItemData{
		ItemType: "journalArticle",
		DOI:      "10.1234/abc",
		ISBN:     "9780262033848",
	})

	if payload["DOI"] != "10.1234/abc" {
		t.Errorf("expected DOI under key \"DOI\", got %v", payload["DOI"])
	}
	if _, present := payload["doi"]; present {
		t.Error("DOI must not be sent under lowercase key \"doi\"")
	}
	if payload["ISBN"] != "9780262033848" {
		t.Errorf("expected ISBN under key \"ISBN\", got %v", payload["ISBN"])
	}
}

// Contract: a requested collection is included in the item payload as a
// collections array, so `add --collection KEY` files the new item into that
// collection atomically on creation instead of leaving it unfiled.
func TestBuildItemPayloadIncludesCollectionsWhenSet(t *testing.T) {
	payload := BuildItemPayload(ItemData{ItemType: "journalArticle", Collections: []string{"ABCD1234"}})

	cols, ok := payload["collections"].([]string)
	if !ok {
		t.Fatalf("collections is not []string: %T (%v)", payload["collections"], payload["collections"])
	}
	if len(cols) != 1 || cols[0] != "ABCD1234" {
		t.Errorf("expected collections [ABCD1234], got %v", cols)
	}
}

// Contract: CreateItem POSTs to /items and returns the key Zotero assigned to
// the new item, so `add` can report and later reference the created record.
func TestCreateItemReturnsKey(t *testing.T) {
	client, rt := newRecordingClient(http.StatusOK, createItemSuccess)

	key, err := client.CreateItem(ItemData{ItemType: "journalArticle", Title: "Attention Is All You Need"})

	if err != nil {
		t.Fatalf("CreateItem returned error: %v", err)
	}
	if key != "ITEM9999" {
		t.Errorf("expected key ITEM9999, got %s", key)
	}
	if rt.lastMethod != http.MethodPost || rt.lastPath != "/users/12345/items" {
		t.Errorf("expected POST /users/12345/items, got %s %s", rt.lastMethod, rt.lastPath)
	}
}

// Contract: the resolved metadata is transmitted in the create payload with
// the right field names and item type, so the created Zotero item actually
// carries the title/DOI/itemType we resolved (not a blank record).
func TestCreateItemSendsResolvedMetadata(t *testing.T) {
	client, rt := newRecordingClient(http.StatusOK, createItemSuccess)

	_, err := client.CreateItem(ItemData{
		ItemType: "journalArticle",
		Title:    "Attention Is All You Need",
		DOI:      "10.5555/3295222.3295349",
		Creators: []Creator{{CreatorType: "author", FirstName: "Ashish", LastName: "Vaswani"}},
	})

	if err != nil {
		t.Fatalf("CreateItem returned error: %v", err)
	}
	item := decodeItemPayload(t, rt.lastBody)
	if item["itemType"] != "journalArticle" {
		t.Errorf("expected itemType journalArticle, got %v", item["itemType"])
	}
	if item["title"] != "Attention Is All You Need" {
		t.Errorf("expected title, got %v", item["title"])
	}
	if item["DOI"] != "10.5555/3295222.3295349" {
		t.Errorf("expected DOI, got %v", item["DOI"])
	}
	creators, ok := item["creators"].([]interface{})
	if !ok || len(creators) != 1 {
		t.Fatalf("expected 1 creator, got %v", item["creators"])
	}
}

// Contract: when Zotero accepts the request but reports the item under
// "failed" (e.g. an invalid field for the item type), CreateItem surfaces an
// error instead of returning an empty key — a rejected write must never be
// reported as success (qa-perspectives: 失敗の握りつぶし禁止).
func TestCreateItemFailedResponse(t *testing.T) {
	client, _ := newRecordingClient(http.StatusOK, `{"successful":{},"failed":{"0":{"code":400,"message":"invalid field"}}}`)

	_, err := client.CreateItem(ItemData{ItemType: "journalArticle", Title: "T"})

	if err == nil {
		t.Fatal("expected error for failed response, got nil")
	}
}

// Contract: a non-2xx response (auth failure, rate limit) becomes an error so
// a create is never reported as successful on an HTTP error.
func TestCreateItemAPIError(t *testing.T) {
	client, _ := newRecordingClient(http.StatusForbidden, "forbidden")

	_, err := client.CreateItem(ItemData{ItemType: "journalArticle", Title: "T"})

	if err == nil {
		t.Fatal("expected error for HTTP 403, got nil")
	}
}

// Contract: UpdateItem PATCHes the given fields to /items/<key> and echoes the
// item's version in If-Unmodified-Since-Version, so an item edited elsewhere
// since we read it is rejected (HTTP 412) rather than silently clobbered —
// the same lost-update guard the delete path uses.
func TestUpdateItemPatchesWithVersionHeader(t *testing.T) {
	client, rt := newRecordingClient(http.StatusNoContent, "")

	err := client.UpdateItem("ITEM9999", 42, map[string]interface{}{"title": "New Title"})

	if err != nil {
		t.Fatalf("UpdateItem returned error: %v", err)
	}
	if rt.lastMethod != http.MethodPatch || rt.lastPath != "/users/12345/items/ITEM9999" {
		t.Errorf("expected PATCH /users/12345/items/ITEM9999, got %s %s", rt.lastMethod, rt.lastPath)
	}
	if got := rt.lastHeader.Get("If-Unmodified-Since-Version"); got != "42" {
		t.Errorf("expected If-Unmodified-Since-Version 42, got %q", got)
	}
	var sent map[string]interface{}
	if err := json.Unmarshal(rt.lastBody, &sent); err != nil {
		t.Fatalf("request body is not a JSON object: %v", err)
	}
	if sent["title"] != "New Title" {
		t.Errorf("expected patched title, got %v", sent["title"])
	}
}

// Contract: a non-2xx PATCH response (including a 412 version conflict)
// becomes an error, so `add --if-exists update` never reports a failed update
// as success.
func TestUpdateItemAPIError(t *testing.T) {
	client, _ := newRecordingClient(http.StatusPreconditionFailed, "version conflict")

	err := client.UpdateItem("ITEM9999", 42, map[string]interface{}{"title": "x"})

	if err == nil {
		t.Fatal("expected error for HTTP 412, got nil")
	}
}
