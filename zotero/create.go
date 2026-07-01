package zotero

import (
	"encoding/json"
	"fmt"
)

// BuildItemPayload converts resolved item metadata into the field map sent to
// the Zotero write API. It deliberately transmits only fields that are
// populated: an empty optional field (e.g. publicationTitle on a book) would be
// rejected by Zotero as invalid for that item type, so silence is safer than an
// empty string. creators and tags are always emitted as arrays so an item with
// none serializes them as [] rather than null, which the API rejects.
func BuildItemPayload(d ItemData) map[string]interface{} {
	payload := map[string]interface{}{
		"itemType": d.ItemType,
		"creators": creatorsPayload(d.Creators),
		"tags":     nonNilTags(d.Tags),
	}
	setIfNotEmpty(payload, "title", d.Title)
	setIfNotEmpty(payload, "date", d.Date)
	setIfNotEmpty(payload, "abstractNote", d.AbstractNote)
	setIfNotEmpty(payload, "url", d.URL)
	setIfNotEmpty(payload, "DOI", d.DOI)
	setTypedField(payload, d.ItemType, "ISBN", d.ISBN)
	setTypedField(payload, d.ItemType, "publisher", d.Publisher)
	setTypedField(payload, d.ItemType, "publicationTitle", d.PublicationTitle)
	setTypedField(payload, d.ItemType, "proceedingsTitle", d.ProceedingsTitle)
	setTypedField(payload, d.ItemType, "bookTitle", d.BookTitle)
	if len(d.Collections) > 0 {
		payload["collections"] = d.Collections
	}
	return payload
}

// validForType gates the fields that exist only on some Zotero item types.
// Sending a field to a type that lacks it makes Zotero reject the whole item
// (its "failed" map), so BuildItemPayload emits these only for the listed
// types. The lists err toward omission: a missing field merely loses a detail,
// whereas an invalid one fails the write. Fields valid on essentially every
// type (title, date, url, abstractNote, DOI) are not gated here — resolvers do
// not set DOI on a type that lacks it (e.g. webpage).
var validForType = map[string]map[string]bool{
	"ISBN":             {"book": true, "bookSection": true, "conferencePaper": true},
	"publisher":        {"book": true, "bookSection": true, "conferencePaper": true},
	"publicationTitle": {"journalArticle": true, "magazineArticle": true, "newspaperArticle": true},
	"proceedingsTitle": {"conferencePaper": true},
	"bookTitle":        {"bookSection": true},
}

func setIfNotEmpty(m map[string]interface{}, key, value string) {
	if value != "" {
		m[key] = value
	}
}

// setTypedField sets a field only when it is non-empty and valid for the given
// item type (see validForType).
func setTypedField(m map[string]interface{}, itemType, key, value string) {
	if value == "" {
		return
	}
	if allowed := validForType[key]; allowed != nil && !allowed[itemType] {
		return
	}
	m[key] = value
}

// creatorsPayload renders creators as field maps carrying only the populated
// name form: firstName/lastName for a person, or name for a single-field
// (organisation) creator. This avoids sending an empty "name" alongside a
// personal name, which would be an ambiguous creator object to Zotero. The
// result is always a non-nil slice so it serializes as [] rather than null.
func creatorsPayload(creators []Creator) []map[string]string {
	out := make([]map[string]string, 0, len(creators))
	for _, c := range creators {
		creatorType := c.CreatorType
		if creatorType == "" {
			creatorType = "author"
		}
		m := map[string]string{"creatorType": creatorType}
		if c.Name != "" {
			m["name"] = c.Name
		} else {
			m["firstName"] = c.FirstName
			m["lastName"] = c.LastName
		}
		out = append(out, m)
	}
	return out
}

func nonNilTags(t []Tag) []Tag {
	if t == nil {
		return []Tag{}
	}
	return t
}

// CreateItem creates a new top-level library item from resolved metadata and
// returns the key Zotero assigned. A non-empty "failed" map in the response is
// surfaced as an error so a rejected write is never reported as success.
func (c *Client) CreateItem(d ItemData) (string, error) {
	payload := []map[string]interface{}{BuildItemPayload(d)}
	respBody, err := c.doWriteRequest("POST", "/items", payload)
	if err != nil {
		return "", err
	}
	return parseCreatedKey(respBody)
}

// parseCreatedKey extracts the created item's key from a Zotero write
// response, treating any "failed" entry as an error.
func parseCreatedKey(respBody []byte) (string, error) {
	var result struct {
		Successful map[string]Item        `json:"successful"`
		Failed     map[string]interface{} `json:"failed"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}
	if len(result.Failed) > 0 {
		return "", fmt.Errorf("failed to create item: %v", result.Failed)
	}
	for _, item := range result.Successful {
		return item.Key, nil
	}
	return "", fmt.Errorf("unexpected empty response")
}

// UpdateItem PATCHes the given fields onto an existing item, guarded by the
// item's version via If-Unmodified-Since-Version (see doPatchRequest), so an
// item edited elsewhere since it was read is rejected with HTTP 412 rather than
// clobbered — the same lost-update guard the tag and delete paths use.
func (c *Client) UpdateItem(itemKey string, version int, fields map[string]interface{}) error {
	return c.doPatchRequest(fmt.Sprintf("/items/%s", itemKey), version, fields)
}
