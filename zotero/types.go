package zotero

import "strings"

// Item represents a Zotero library item.
type Item struct {
	Key string `json:"key"`
	// Version is the item's object version. The Zotero API uses it for
	// optimistic concurrency: a DELETE must echo it back in the
	// If-Unmodified-Since-Version header, so an item modified after we read
	// it is rejected (HTTP 412) instead of being clobbered.
	Version int      `json:"version"`
	Data    ItemData `json:"data"`
}

// HasTag reports whether the item carries tag (case-insensitive). Used by the
// delete guardrails to tell AI-created notes (tagged AIGeneratedTag) apart
// from human-written ones.
func (it *Item) HasTag(tag string) bool {
	for _, t := range it.Data.Tags {
		if strings.EqualFold(t.Tag, tag) {
			return true
		}
	}
	return false
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

	// annotation fields (populated only for itemType == "annotation")
	AnnotationType       string `json:"annotationType,omitempty"`
	AnnotationText       string `json:"annotationText,omitempty"`
	AnnotationComment    string `json:"annotationComment,omitempty"`
	AnnotationColor      string `json:"annotationColor,omitempty"`
	AnnotationPageLabel  string `json:"annotationPageLabel,omitempty"`
	AnnotationSortIndex  string `json:"annotationSortIndex,omitempty"`
	AnnotationPosition   string `json:"annotationPosition,omitempty"`
	AnnotationAuthorName string `json:"annotationAuthorName,omitempty"`
}

// Creator represents an author or contributor.
type Creator struct {
	CreatorType string `json:"creatorType"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	Name        string `json:"name"`
}

// Tag represents a Zotero tag. Type distinguishes a manually-added tag (0, the
// default) from an automatically-applied one (1). It must be preserved on a
// read-modify-write of an item's tag set: dropping it would make a PATCH
// silently re-send automatic tags as manual (type 0), demoting them.
type Tag struct {
	Tag  string `json:"tag"`
	Type int    `json:"type,omitempty"`
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

// UploadAuthorization holds the response from the file upload authorization endpoint.
type UploadAuthorization struct {
	URL         string `json:"url"`
	UploadKey   string `json:"uploadKey"`
	Prefix      string `json:"prefix"`
	Suffix      string `json:"suffix"`
	ContentType string `json:"contentType"`
}

// ContextBundle holds all information about an item for the context command.
type ContextBundle struct {
	Item        *Item             `json:"item"`
	FullText    *FullTextResponse `json:"fullText,omitempty"`
	Notes       []Item            `json:"notes,omitempty"`
	Attachments []Item            `json:"attachments,omitempty"`
	Annotations []Item            `json:"annotations,omitempty"`
}
