package zotero

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

const baseURL = "https://api.zotero.org"

// Client is a Zotero Web API client.
type Client struct {
	APIKey     string
	UserID     string
	HTTPClient *http.Client
}

// NewClient creates a new Zotero API client.
func NewClient(apiKey, userID string) *Client {
	return &Client{
		APIKey:     apiKey,
		UserID:     userID,
		HTTPClient: &http.Client{},
	}
}

func (c *Client) doRequest(endpoint string, params url.Values, accept string) ([]byte, error) {
	u := fmt.Sprintf("%s/users/%s%s", baseURL, c.UserID, endpoint)
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Zotero-API-Key", c.APIKey)
	req.Header.Set("Zotero-API-Version", "3")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func (c *Client) doWriteRequest(method, endpoint string, body interface{}) ([]byte, error) {
	jsonData, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	u := fmt.Sprintf("%s/users/%s%s", baseURL, c.UserID, endpoint)
	req, err := http.NewRequest(method, u, bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Zotero-API-Key", c.APIKey)
	req.Header.Set("Zotero-API-Version", "3")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// SearchItems searches for items by query and optional tag filter.
func (c *Client) SearchItems(query string, tag string) ([]Item, error) {
	params := url.Values{}
	if query != "" {
		params.Set("q", query)
	}
	if tag != "" {
		params.Set("tag", tag)
	}
	params.Set("limit", "25")
	params.Set("sort", "date")
	params.Set("direction", "desc")

	body, err := c.doRequest("/items", params, "application/json")
	if err != nil {
		return nil, err
	}

	var items []Item
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	return items, nil
}

// ListItems lists items, optionally filtered by collection.
func (c *Client) ListItems(collectionKey string, limit int) ([]Item, error) {
	endpoint := "/items"
	if collectionKey != "" {
		endpoint = fmt.Sprintf("/collections/%s/items", collectionKey)
	}

	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("sort", "dateModified")
	params.Set("direction", "desc")
	params.Set("itemType", "-attachment || note")

	body, err := c.doRequest(endpoint, params, "application/json")
	if err != nil {
		return nil, err
	}

	var items []Item
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	return items, nil
}

// GetItem retrieves a single item by key.
func (c *Client) GetItem(itemKey string) (*Item, error) {
	body, err := c.doRequest(fmt.Sprintf("/items/%s", itemKey), nil, "application/json")
	if err != nil {
		return nil, err
	}

	var item Item
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	return &item, nil
}

// GetBibTeX exports items as BibTeX.
func (c *Client) GetBibTeX(query string, collectionKey string, all bool) (string, error) {
	endpoint := "/items"
	if collectionKey != "" {
		endpoint = fmt.Sprintf("/collections/%s/items", collectionKey)
	}

	params := url.Values{}
	if query != "" {
		params.Set("q", query)
	}
	if all {
		params.Set("limit", "100")
	} else {
		params.Set("limit", "25")
	}
	params.Set("itemType", "-attachment || note")

	body, err := c.doRequest(endpoint, params, "application/x-bibtex")
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// ListCollections retrieves all collections.
func (c *Client) ListCollections() ([]Collection, error) {
	body, err := c.doRequest("/collections", nil, "application/json")
	if err != nil {
		return nil, err
	}

	var collections []Collection
	if err := json.Unmarshal(body, &collections); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	return collections, nil
}

// GetFullText retrieves the full-text content of an item.
func (c *Client) GetFullText(itemKey string) (*FullTextResponse, error) {
	body, err := c.doRequest(fmt.Sprintf("/items/%s/fulltext", itemKey), nil, "application/json")
	if err != nil {
		return nil, err
	}

	var ft FullTextResponse
	if err := json.Unmarshal(body, &ft); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	return &ft, nil
}

// FullTextSearch searches items including full-text content.
func (c *Client) FullTextSearch(query string, tag string, limit int) ([]Item, error) {
	params := url.Values{}
	if query != "" {
		params.Set("q", query)
		params.Set("qmode", "everything")
	}
	if tag != "" {
		params.Set("tag", tag)
	}
	if limit <= 0 {
		limit = 25
	}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("sort", "date")
	params.Set("direction", "desc")

	body, err := c.doRequest("/items", params, "application/json")
	if err != nil {
		return nil, err
	}

	var items []Item
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	return items, nil
}

// childrenPageSize is the Zotero API maximum page size. Without an explicit
// limit the API returns only 25 items, silently truncating annotation lists.
const childrenPageSize = 100

// GetChildren retrieves all child items (notes, attachments, annotations)
// of an item, following pagination so results beyond one page are not lost.
func (c *Client) GetChildren(itemKey string) ([]Item, error) {
	var all []Item
	for start := 0; ; start += childrenPageSize {
		params := url.Values{}
		params.Set("limit", fmt.Sprintf("%d", childrenPageSize))
		params.Set("start", fmt.Sprintf("%d", start))

		body, err := c.doRequest(fmt.Sprintf("/items/%s/children", itemKey), params, "application/json")
		if err != nil {
			return nil, err
		}

		var page []Item
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
		all = append(all, page...)
		if len(page) < childrenPageSize {
			return all, nil
		}
	}
}

// GetAnnotations returns all annotations under an item's attachments,
// sorted by annotationSortIndex (reading order).
func (c *Client) GetAnnotations(itemKey string) ([]Item, error) {
	children, err := c.GetChildren(itemKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get children of %s: %w", itemKey, err)
	}
	return c.annotationsUnder(children)
}

// annotationsUnder collects annotation items beneath the given children
// (descending into each attachment), sorted by annotationSortIndex.
func (c *Client) annotationsUnder(children []Item) ([]Item, error) {
	var annotations []Item
	for _, child := range children {
		switch child.Data.ItemType {
		case "annotation":
			// the parent was an attachment key; its children are annotations directly
			annotations = append(annotations, child)
		case "attachment":
			grandchildren, err := c.GetChildren(child.Key)
			if err != nil {
				return nil, fmt.Errorf("failed to get annotations of attachment %s: %w", child.Key, err)
			}
			for _, gc := range grandchildren {
				if gc.Data.ItemType == "annotation" {
					annotations = append(annotations, gc)
				}
			}
		}
	}

	// annotationSortIndex is zero-padded, so lexicographic order == reading order
	sort.Slice(annotations, func(i, j int) bool {
		return annotations[i].Data.AnnotationSortIndex < annotations[j].Data.AnnotationSortIndex
	})
	return annotations, nil
}

// FilterAnnotations returns annotations matching the given color and type
// (an empty filter matches all).
func FilterAnnotations(anns []Item, color, annType string) []Item {
	if color == "" && annType == "" {
		return anns
	}
	filtered := []Item{}
	for _, a := range anns {
		if color != "" && !strings.EqualFold(a.Data.AnnotationColor, color) {
			continue
		}
		if annType != "" && !strings.EqualFold(a.Data.AnnotationType, annType) {
			continue
		}
		filtered = append(filtered, a)
	}
	return filtered
}

// htmlNotePattern detects bodies that are already HTML (start with a tag,
// comment, or doctype) as opposed to plain text that merely begins with '<'.
var htmlNotePattern = regexp.MustCompile(`^<[a-zA-Z!/]`)

// PlainTextToNoteHTML converts plain text to Zotero note HTML: special
// characters are escaped and newlines become paragraph boundaries.
// Content that already looks like HTML is returned unchanged.
func PlainTextToNoteHTML(content string) string {
	if htmlNotePattern.MatchString(strings.TrimSpace(content)) {
		return content
	}
	escaped := html.EscapeString(content)
	return "<p>" + strings.ReplaceAll(escaped, "\n", "</p>\n<p>") + "</p>"
}

// CreateNote creates a note attached to a parent item.
func (c *Client) CreateNote(parentKey, content string, tags []string) (string, error) {
	content = PlainTextToNoteHTML(content)

	tagObjs := []Tag{}
	for _, t := range tags {
		tagObjs = append(tagObjs, Tag{Tag: t})
	}

	noteItem := []map[string]interface{}{
		{
			"itemType":   "note",
			"parentItem": parentKey,
			"note":       content,
			"tags":       tagObjs,
		},
	}

	respBody, err := c.doWriteRequest("POST", "/items", noteItem)
	if err != nil {
		return "", err
	}

	var result struct {
		Successful map[string]Item        `json:"successful"`
		Failed     map[string]interface{} `json:"failed"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Failed) > 0 {
		return "", fmt.Errorf("failed to create note: %v", result.Failed)
	}

	for _, item := range result.Successful {
		return item.Key, nil
	}
	return "", fmt.Errorf("unexpected empty response")
}

// doDeleteRequest sends a DELETE to the Zotero API with the optimistic
// concurrency header required for single-item deletion.
//
// Guardrail (lost-update protection): Zotero requires
// If-Unmodified-Since-Version on a single-item DELETE. By echoing the version
// we read moments earlier, a note that was edited in between (by the desktop
// app, sync, or another client) is rejected with HTTP 412 rather than being
// silently destroyed. This is why DeleteNote always re-reads the item right
// before deleting instead of trusting a version passed in from far away.
func (c *Client) doDeleteRequest(endpoint string, version int) error {
	u := fmt.Sprintf("%s/users/%s%s", baseURL, c.UserID, endpoint)
	req, err := http.NewRequest(http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Zotero-API-Key", c.APIKey)
	req.Header.Set("Zotero-API-Version", "3")
	req.Header.Set("If-Unmodified-Since-Version", fmt.Sprintf("%d", version))

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	// A successful single-item delete returns 204 No Content. Anything else
	// (404 gone, 412 version conflict, 403 auth) is surfaced verbatim so the
	// caller never mistakes a non-deletion for success.
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// DeleteNote deletes a single note item, guarded so it can only ever remove
// one note that the caller has positively identified.
//
// The design deliberately makes the *dangerous* directions impossible rather
// than relying on the caller to be careful:
//
//   - Notes only. We re-read the item and refuse anything whose itemType is
//     not "note". A mistyped or stale key therefore cannot delete a paper, a
//     PDF attachment, or a highlight — only a note, the cheapest thing to
//     recreate. This is a structural guard, not a warning the caller can skip.
//   - One key, no bulk. The API surface takes exactly one itemKey and has no
//     "delete by tag / by query / all" path. There is intentionally no way to
//     express "delete every ai-summary note" here; mass deletion would have to
//     be built explicitly by a caller looping over keys it has already listed
//     and shown, which keeps the blast radius of any single call at one item.
//   - Caller-created only (opt-in). When requireAIGenerated is true the note
//     must also carry AIGeneratedTag, so an autonomous caller (the MCP server)
//     can delete notes it produced but never a human's hand-written note. The
//     CLI passes false because a human has already confirmed the specific key.
//   - Lost-update safe. Deletion uses the version from the read above via
//     If-Unmodified-Since-Version (see doDeleteRequest).
//
// On success it returns the deleted note's metadata so the caller can echo
// exactly what was removed.
func (c *Client) DeleteNote(itemKey string, requireAIGenerated bool) (*Item, error) {
	item, err := c.GetItem(itemKey)
	if err != nil {
		return nil, err
	}
	if item.Data.ItemType != "note" {
		return nil, fmt.Errorf("refusing to delete %s: item type is %q, not \"note\" (this operation only deletes notes)", itemKey, item.Data.ItemType)
	}
	if requireAIGenerated && !item.HasTag(AIGeneratedTag) {
		return nil, fmt.Errorf("refusing to delete %s: note lacks the %q tag (only AI-generated notes may be deleted here)", itemKey, AIGeneratedTag)
	}
	if err := c.doDeleteRequest(fmt.Sprintf("/items/%s", itemKey), item.Version); err != nil {
		return nil, err
	}
	return item, nil
}

// ListItemsByTag lists items filtered by tag.
func (c *Client) ListItemsByTag(tag string, limit int) ([]Item, error) {
	params := url.Values{}
	params.Set("tag", tag)
	if limit <= 0 {
		limit = 100
	}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("sort", "date")
	params.Set("direction", "desc")
	params.Set("itemType", "-attachment || note")

	body, err := c.doRequest("/items", params, "application/json")
	if err != nil {
		return nil, err
	}

	var items []Item
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	return items, nil
}

// GetItemsByKeys retrieves multiple items by their keys.
func (c *Client) GetItemsByKeys(keys []string) ([]Item, error) {
	params := url.Values{}
	params.Set("itemKey", strings.Join(keys, ","))
	params.Set("limit", fmt.Sprintf("%d", len(keys)))

	body, err := c.doRequest("/items", params, "application/json")
	if err != nil {
		return nil, err
	}

	var items []Item
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	return items, nil
}

// GetContext retrieves all information about an item (metadata, fulltext, notes, attachments).
// Annotations cost one extra API request per attachment, so they are fetched
// only when withAnnotations is true.
func (c *Client) GetContext(itemKey string, withAnnotations bool) (*ContextBundle, error) {
	item, err := c.GetItem(itemKey)
	if err != nil {
		return nil, err
	}

	ft, _ := c.GetFullText(itemKey)

	children, err := c.GetChildren(itemKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get children of %s: %w", itemKey, err)
	}
	var notes, attachments []Item
	for _, child := range children {
		switch child.Data.ItemType {
		case "note":
			notes = append(notes, child)
		case "attachment":
			attachments = append(attachments, child)
		}
	}

	var annotations []Item
	if withAnnotations {
		annotations, err = c.annotationsUnder(children)
		if err != nil {
			return nil, err
		}
	}

	return &ContextBundle{
		Item:        item,
		FullText:    ft,
		Notes:       notes,
		Attachments: attachments,
		Annotations: annotations,
	}, nil
}

// doFormRequest sends a form-encoded POST request to the Zotero API.
func (c *Client) doFormRequest(endpoint string, formData url.Values, extraHeaders map[string]string) ([]byte, error) {
	u := fmt.Sprintf("%s/users/%s%s", baseURL, c.UserID, endpoint)
	req, err := http.NewRequest("POST", u, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Zotero-API-Key", c.APIKey)
	req.Header.Set("Zotero-API-Version", "3")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// CreateAttachment creates an attachment item in Zotero.
func (c *Client) CreateAttachment(parentKey, filename, title, contentType string, tags []string) (string, error) {
	tagObjs := []Tag{}
	for _, t := range tags {
		tagObjs = append(tagObjs, Tag{Tag: t})
	}

	if title == "" {
		title = filename
	}

	attachItem := []map[string]interface{}{
		{
			"itemType":    "attachment",
			"linkMode":    "imported_file",
			"title":       title,
			"filename":    filename,
			"contentType": contentType,
			"tags":        tagObjs,
		},
	}
	if parentKey != "" {
		attachItem[0]["parentItem"] = parentKey
	}

	respBody, err := c.doWriteRequest("POST", "/items", attachItem)
	if err != nil {
		return "", err
	}

	var result struct {
		Successful map[string]Item        `json:"successful"`
		Failed     map[string]interface{} `json:"failed"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Failed) > 0 {
		return "", fmt.Errorf("failed to create attachment: %v", result.Failed)
	}

	for _, item := range result.Successful {
		return item.Key, nil
	}
	return "", fmt.Errorf("unexpected empty response")
}

// GetUploadAuthorization requests authorization to upload a file to an attachment item.
func (c *Client) GetUploadAuthorization(itemKey, filename string, filesize int64, md5hex string, mtime int64) (*UploadAuthorization, error) {
	formData := url.Values{}
	formData.Set("md5", md5hex)
	formData.Set("filename", filename)
	formData.Set("filesize", fmt.Sprintf("%d", filesize))
	formData.Set("mtime", fmt.Sprintf("%d", mtime))

	extraHeaders := map[string]string{
		"If-None-Match": "*",
	}

	respBody, err := c.doFormRequest(fmt.Sprintf("/items/%s/file", itemKey), formData, extraHeaders)
	if err != nil {
		return nil, err
	}

	var auth UploadAuthorization
	if err := json.Unmarshal(respBody, &auth); err != nil {
		return nil, fmt.Errorf("failed to parse upload authorization: %w", err)
	}
	return &auth, nil
}

// UploadFileContent uploads the file content to the authorized URL.
func (c *Client) UploadFileContent(auth *UploadAuthorization, fileContent []byte) error {
	var buf bytes.Buffer
	buf.WriteString(auth.Prefix)
	buf.Write(fileContent)
	buf.WriteString(auth.Suffix)

	req, err := http.NewRequest("POST", auth.URL, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", auth.ContentType)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// RegisterUpload completes the upload process by registering the upload key.
func (c *Client) RegisterUpload(itemKey, uploadKey string) error {
	formData := url.Values{}
	formData.Set("upload", uploadKey)

	extraHeaders := map[string]string{
		"If-None-Match": "*",
	}

	_, err := c.doFormRequest(fmt.Sprintf("/items/%s/file", itemKey), formData, extraHeaders)
	return err
}

// FormatAuthors formats a list of creators into a display string.
func FormatAuthors(creators []Creator) string {
	var names []string
	for _, c := range creators {
		if c.Name != "" {
			names = append(names, c.Name)
		} else if c.LastName != "" {
			names = append(names, c.LastName+", "+c.FirstName)
		}
	}
	if len(names) == 0 {
		return "-"
	}
	if len(names) > 3 {
		return strings.Join(names[:3], "; ") + " et al."
	}
	return strings.Join(names, "; ")
}

// FormatTags formats a list of tags into a display string.
func FormatTags(tags []Tag) string {
	var t []string
	for _, tag := range tags {
		t = append(t, tag.Tag)
	}
	if len(t) == 0 {
		return "-"
	}
	return strings.Join(t, ", ")
}

// FormatAnnotation renders one annotation as human/LLM-readable text.
func FormatAnnotation(it Item) string {
	d := it.Data
	page := ""
	if d.AnnotationPageLabel != "" {
		page = " p." + d.AnnotationPageLabel
	}

	switch d.AnnotationType {
	case "highlight", "underline":
		color := ""
		if d.AnnotationColor != "" {
			color = " " + d.AnnotationColor
		}
		s := fmt.Sprintf("[%s%s%s] \"%s\"", d.AnnotationType, page, color, d.AnnotationText)
		if d.AnnotationComment != "" {
			s += "\n  ↳ comment: " + d.AnnotationComment
		}
		return s
	case "note":
		return fmt.Sprintf("[note%s] %s", page, d.AnnotationComment)
	case "ink", "image":
		s := fmt.Sprintf("[%s%s — no text]", d.AnnotationType, page)
		if d.AnnotationComment != "" {
			s += "\n  ↳ comment: " + d.AnnotationComment
		}
		return s
	default:
		return fmt.Sprintf("[%s%s] %s %s", d.AnnotationType, page, d.AnnotationText, d.AnnotationComment)
	}
}

// Truncate truncates a string to a maximum number of runes.
func Truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
