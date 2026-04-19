package zotero

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

// GetChildren retrieves child items (notes, attachments) of an item.
func (c *Client) GetChildren(itemKey string) ([]Item, error) {
	body, err := c.doRequest(fmt.Sprintf("/items/%s/children", itemKey), nil, "application/json")
	if err != nil {
		return nil, err
	}

	var items []Item
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	return items, nil
}

// CreateNote creates a note attached to a parent item.
func (c *Client) CreateNote(parentKey, content string, tags []string) (string, error) {
	if !strings.HasPrefix(strings.TrimSpace(content), "<") {
		content = "<p>" + strings.ReplaceAll(content, "\n", "</p>\n<p>") + "</p>"
	}

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
func (c *Client) GetContext(itemKey string) (*ContextBundle, error) {
	item, err := c.GetItem(itemKey)
	if err != nil {
		return nil, err
	}

	ft, _ := c.GetFullText(itemKey)

	children, _ := c.GetChildren(itemKey)
	var notes, attachments []Item
	for _, child := range children {
		switch child.Data.ItemType {
		case "note":
			notes = append(notes, child)
		case "attachment":
			attachments = append(attachments, child)
		}
	}

	return &ContextBundle{
		Item:        item,
		FullText:    ft,
		Notes:       notes,
		Attachments: attachments,
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

// Truncate truncates a string to a maximum number of runes.
func Truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
