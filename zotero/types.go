package zotero

// Item represents a Zotero library item.
type Item struct {
	Key  string   `json:"key"`
	Data ItemData `json:"data"`
}

// ItemData holds the metadata fields of a Zotero item.
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

// Creator represents an author or contributor.
type Creator struct {
	CreatorType string `json:"creatorType"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	Name        string `json:"name"`
}

// Tag represents a Zotero tag.
type Tag struct {
	Tag string `json:"tag"`
}

// Collection represents a Zotero collection.
type Collection struct {
	Key  string         `json:"key"`
	Data CollectionData `json:"data"`
}

// CollectionData holds the metadata fields of a collection.
type CollectionData struct {
	Key              string      `json:"key"`
	Name             string      `json:"name"`
	ParentCollection interface{} `json:"parentCollection"`
	NumItems         int         `json:"numItems"`
}

// FullTextResponse represents the response from the fulltext endpoint.
type FullTextResponse struct {
	Content      string `json:"content"`
	IndexedPages int    `json:"indexedPages"`
	TotalPages   int    `json:"totalPages"`
}

// ContextBundle holds all information about an item for the context command.
type ContextBundle struct {
	Item        *Item             `json:"item"`
	FullText    *FullTextResponse `json:"fullText,omitempty"`
	Notes       []Item            `json:"notes,omitempty"`
	Attachments []Item            `json:"attachments,omitempty"`
}
