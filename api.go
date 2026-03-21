package main

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

type ZoteroClient struct {
	APIKey     string
	UserID    string
	HTTPClient *http.Client
}

func NewZoteroClient(cfg *Config) *ZoteroClient {
	return &ZoteroClient{
		APIKey:     cfg.APIKey,
		UserID:    cfg.UserID,
		HTTPClient: &http.Client{},
	}
}

func (c *ZoteroClient) doRequest(endpoint string, params url.Values, accept string) ([]byte, error) {
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
		return nil, fmt.Errorf("APIリクエストに失敗: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("レスポンスの読み込みに失敗: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("APIエラー (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// Item represents a Zotero item
type Item struct {
	Key  string   `json:"key"`
	Data ItemData `json:"data"`
}

type ItemData struct {
	ItemType         string    `json:"itemType"`
	Title            string    `json:"title"`
	Creators         []Creator `json:"creators"`
	Date             string    `json:"date"`
	AbstractNote     string    `json:"abstractNote"`
	URL              string    `json:"url"`
	DOI              string    `json:"DOI"`
	Tags             []Tag     `json:"tags"`
	PublicationTitle string    `json:"publicationTitle"`
	Note             string    `json:"note,omitempty"`
	ContentType      string    `json:"contentType,omitempty"`
	Filename         string    `json:"filename,omitempty"`
	ParentItem       string    `json:"parentItem,omitempty"`
}

// FullTextResponse represents the response from the fulltext endpoint
type FullTextResponse struct {
	Content      string `json:"content"`
	IndexedPages int    `json:"indexedPages"`
	TotalPages   int    `json:"totalPages"`
}

// ContextBundle holds all information about an item for the context command
type ContextBundle struct {
	Item        *Item             `json:"item"`
	FullText    *FullTextResponse `json:"fullText,omitempty"`
	Notes       []Item            `json:"notes,omitempty"`
	Attachments []Item            `json:"attachments,omitempty"`
}

type Creator struct {
	CreatorType string `json:"creatorType"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	Name        string `json:"name"`
}

type Tag struct {
	Tag string `json:"tag"`
}

type Collection struct {
	Key  string         `json:"key"`
	Data CollectionData `json:"data"`
}

type CollectionData struct {
	Key              string `json:"key"`
	Name             string `json:"name"`
	ParentCollection interface{} `json:"parentCollection"`
	NumItems         int    `json:"numItems"`
}

func (c *ZoteroClient) doWriteRequest(method, endpoint string, body interface{}) ([]byte, error) {
	jsonData, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("JSONの作成に失敗: %w", err)
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
		return nil, fmt.Errorf("APIリクエストに失敗: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("レスポンスの読み込みに失敗: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("APIエラー (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func (c *ZoteroClient) SearchItems(query string, tag string) ([]Item, error) {
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
		return nil, fmt.Errorf("JSONのパースに失敗: %w", err)
	}
	return items, nil
}

func (c *ZoteroClient) ListItems(collectionKey string, limit int) ([]Item, error) {
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
		return nil, fmt.Errorf("JSONのパースに失敗: %w", err)
	}
	return items, nil
}

func (c *ZoteroClient) GetItem(itemKey string) (*Item, error) {
	body, err := c.doRequest(fmt.Sprintf("/items/%s", itemKey), nil, "application/json")
	if err != nil {
		return nil, err
	}

	var item Item
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("JSONのパースに失敗: %w", err)
	}
	return &item, nil
}

func (c *ZoteroClient) GetBibTeX(query string, collectionKey string, all bool) (string, error) {
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

func (c *ZoteroClient) ListCollections() ([]Collection, error) {
	body, err := c.doRequest("/collections", nil, "application/json")
	if err != nil {
		return nil, err
	}

	var collections []Collection
	if err := json.Unmarshal(body, &collections); err != nil {
		return nil, fmt.Errorf("JSONのパースに失敗: %w", err)
	}
	return collections, nil
}

func (c *ZoteroClient) GetFullText(itemKey string) (*FullTextResponse, error) {
	body, err := c.doRequest(fmt.Sprintf("/items/%s/fulltext", itemKey), nil, "application/json")
	if err != nil {
		return nil, err
	}

	var ft FullTextResponse
	if err := json.Unmarshal(body, &ft); err != nil {
		return nil, fmt.Errorf("JSONのパースに失敗: %w", err)
	}
	return &ft, nil
}

func (c *ZoteroClient) FullTextSearch(query string, tag string, limit int) ([]Item, error) {
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
		return nil, fmt.Errorf("JSONのパースに失敗: %w", err)
	}
	return items, nil
}

func (c *ZoteroClient) GetChildren(itemKey string) ([]Item, error) {
	body, err := c.doRequest(fmt.Sprintf("/items/%s/children", itemKey), nil, "application/json")
	if err != nil {
		return nil, err
	}

	var items []Item
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("JSONのパースに失敗: %w", err)
	}
	return items, nil
}

func (c *ZoteroClient) CreateNote(parentKey, content string, tags []string) (string, error) {
	// Wrap in <p> if not already HTML
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
		Successful map[string]Item `json:"successful"`
		Failed     map[string]interface{} `json:"failed"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("レスポンスのパースに失敗: %w", err)
	}

	if len(result.Failed) > 0 {
		return "", fmt.Errorf("ノートの作成に失敗: %v", result.Failed)
	}

	for _, item := range result.Successful {
		return item.Key, nil
	}
	return "", fmt.Errorf("ノートの作成結果が不明です")
}

func (c *ZoteroClient) ListItemsByTag(tag string, limit int) ([]Item, error) {
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
		return nil, fmt.Errorf("JSONのパースに失敗: %w", err)
	}
	return items, nil
}

func (c *ZoteroClient) GetItemsByKeys(keys []string) ([]Item, error) {
	params := url.Values{}
	params.Set("itemKey", strings.Join(keys, ","))
	params.Set("limit", fmt.Sprintf("%d", len(keys)))

	body, err := c.doRequest("/items", params, "application/json")
	if err != nil {
		return nil, err
	}

	var items []Item
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("JSONのパースに失敗: %w", err)
	}
	return items, nil
}

// Helper functions for display
func formatAuthors(creators []Creator) string {
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

func formatTags(tags []Tag) string {
	var t []string
	for _, tag := range tags {
		t = append(t, tag.Tag)
	}
	if len(t) == 0 {
		return "-"
	}
	return strings.Join(t, ", ")
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
