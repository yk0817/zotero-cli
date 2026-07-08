package zotero

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// LibraryTag is one entry from the library tag list (GET /tags). Meta carries
// the usage count Zotero reports, which the closed-vocabulary UI shows so a
// human (or agent) can tell established tags apart from one-off ones.
type LibraryTag struct {
	Tag  string `json:"tag"`
	Meta struct {
		NumItems int `json:"numItems"`
	} `json:"meta"`
}

// tagsPageSize is the Zotero API maximum page size for the tags endpoint.
// Without an explicit limit the API returns only 25 tags, which would make the
// "closed vocabulary" callers choose from silently incomplete.
const tagsPageSize = 100

// ListTags returns every tag in the user's library, following pagination so a
// library with more than one page of tags is not silently truncated. The
// result is the full closed vocabulary callers choose from.
func (c *Client) ListTags() ([]LibraryTag, error) {
	var all []LibraryTag
	for start := 0; ; start += tagsPageSize {
		params := url.Values{}
		params.Set("limit", fmt.Sprintf("%d", tagsPageSize))
		params.Set("start", fmt.Sprintf("%d", start))

		body, err := c.doRequest("/tags", params, "application/json")
		if err != nil {
			return nil, err
		}

		var page []LibraryTag
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
		all = append(all, page...)
		if len(page) < tagsPageSize {
			return all, nil
		}
	}
}

// ApplyTagDelta returns a new tag set with `remove` tags dropped and `add`
// tags appended, both matched case-insensitively. It never mutates `current`,
// never introduces duplicates, and preserves existing order (new tags are
// appended in the order given). A tag present in both add and remove ends up
// added, since removals are applied before additions.
func ApplyTagDelta(current []Tag, add, remove []string) []Tag {
	removeSet := make(map[string]bool, len(remove))
	for _, r := range remove {
		removeSet[strings.ToLower(strings.TrimSpace(r))] = true
	}

	result := []Tag{}
	present := make(map[string]bool)
	for _, t := range current {
		if removeSet[strings.ToLower(t.Tag)] {
			continue
		}
		result = append(result, Tag{Tag: t.Tag, Type: t.Type})
		present[strings.ToLower(t.Tag)] = true
	}

	for _, a := range add {
		a = strings.TrimSpace(a)
		key := strings.ToLower(a)
		if a == "" || present[key] {
			continue
		}
		result = append(result, Tag{Tag: a})
		present[key] = true
	}
	return result
}

// UpdateItemTags applies the add/remove deltas to an already-fetched item's
// tag set and PATCHes the result back. It uses the item's version via
// If-Unmodified-Since-Version (optimistic concurrency), so an item edited
// since it was read is rejected (HTTP 412) instead of having a concurrent tag
// change clobbered. Callers should pass an item read shortly before for this
// to be meaningful. Returns the resulting tag set.
func (c *Client) UpdateItemTags(item *Item, add, remove []string) ([]Tag, error) {
	newTags := ApplyTagDelta(item.Data.Tags, add, remove)
	if err := c.doPatchRequest(fmt.Sprintf("/items/%s", item.Key), item.Version, map[string]interface{}{"tags": newTags}); err != nil {
		return nil, err
	}
	return newTags, nil
}
