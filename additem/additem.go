// Package additem resolves an external identifier (DOI, arXiv ID, ISBN, or URL)
// into a Zotero item and creates it, with duplicate handling. It is shared by
// the CLI `add` command and the MCP zotero_add_item tool so both behave
// identically; callers supply the resolver and library (small interfaces) and
// map the returned errors to their own surface (CLI error codes / MCP errors).
package additem

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/yk0817/zotero-cli/resolve"
	"github.com/yk0817/zotero-cli/zotero"
)

// identifier kinds.
const (
	KindDOI   = "doi"
	KindArxiv = "arxiv"
	KindISBN  = "isbn"
	KindURL   = "url"
)

// --if-exists modes (what to do when an item with the same identifier exists).
const (
	IfExistsSkip      = "skip"
	IfExistsUpdate    = "update"
	IfExistsDuplicate = "duplicate"
)

// Result.Action values.
const (
	ActionCreated = "created"
	ActionSkipped = "skipped"
	ActionUpdated = "updated"
)

// internal actions decided from a duplicate + mode.
const (
	actCreate = "create"
	actSkip   = "skip"
	actUpdate = "update"
)

// dedupSearchLimit bounds the quick-search used to find an existing item with
// the same identifier before creating a new one.
const dedupSearchLimit = 50

// Resolver is the subset of *resolve.Client that additem uses.
type Resolver interface {
	ResolveDOI(ctx context.Context, doi string) (zotero.ItemData, error)
	ResolveArXiv(ctx context.Context, id string) (zotero.ItemData, error)
	ResolveISBN(ctx context.Context, isbn string) (zotero.ItemData, error)
	ResolveURL(ctx context.Context, url string) (zotero.ItemData, error)
}

// Library is the subset of *zotero.Client that additem uses.
type Library interface {
	FullTextSearch(query, tag string, limit int) ([]zotero.Item, error)
	CreateItem(data zotero.ItemData) (string, error)
	UpdateItem(itemKey string, version int, fields map[string]interface{}) error
}

// Result describes what Run did. The json tags are the CLI's --output json
// shape (also reused by the MCP tool).
type Result struct {
	Action         string `json:"action"` // created | skipped | updated
	ItemKey        string `json:"itemKey"`
	ItemType       string `json:"itemType"`
	Title          string `json:"title"`
	Identifier     string `json:"identifier"`
	IdentifierKind string `json:"identifierKind"`
	Duplicate      bool   `json:"duplicate"`
}

// SelectIdentifier returns the single identifier kind/value provided, erroring
// unless exactly one of the four is non-empty.
func SelectIdentifier(doi, arxiv, isbn, url string) (string, string, error) {
	candidates := []struct{ kind, value string }{
		{KindDOI, strings.TrimSpace(doi)},
		{KindArxiv, strings.TrimSpace(arxiv)},
		{KindISBN, strings.TrimSpace(isbn)},
		{KindURL, strings.TrimSpace(url)},
	}
	var chosen []struct{ kind, value string }
	for _, c := range candidates {
		if c.value != "" {
			chosen = append(chosen, c)
		}
	}
	switch len(chosen) {
	case 1:
		return chosen[0].kind, chosen[0].value, nil
	case 0:
		return "", "", fmt.Errorf("no identifier given")
	default:
		return "", "", fmt.Errorf("multiple identifiers given")
	}
}

// ValidateIfExists checks the --if-exists mode.
func ValidateIfExists(mode string) error {
	switch mode {
	case IfExistsSkip, IfExistsUpdate, IfExistsDuplicate:
		return nil
	default:
		return fmt.Errorf("invalid if-exists %q", mode)
	}
}

// Resolve dispatches to the resolver for the chosen identifier kind.
func Resolve(ctx context.Context, r Resolver, kind, value string) (zotero.ItemData, error) {
	switch kind {
	case KindDOI:
		return r.ResolveDOI(ctx, value)
	case KindArxiv:
		return r.ResolveArXiv(ctx, value)
	case KindISBN:
		return r.ResolveISBN(ctx, value)
	case KindURL:
		return r.ResolveURL(ctx, value)
	default:
		return zotero.ItemData{}, fmt.Errorf("unknown identifier kind %q", kind)
	}
}

// ApplyOptions merges user tags (deduplicated against tags the resolver already
// set) and a collection into the resolved metadata, returning a copy so the
// input is not mutated.
func ApplyOptions(data zotero.ItemData, tags []string, collection string) zotero.ItemData {
	merged := append([]zotero.Tag(nil), data.Tags...)
	present := map[string]bool{}
	for _, t := range merged {
		present[t.Tag] = true
	}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" || present[t] {
			continue
		}
		present[t] = true
		merged = append(merged, zotero.Tag{Tag: t})
	}
	data.Tags = merged
	if collection != "" {
		data.Collections = []string{collection}
	}
	return data
}

// Run finds an existing item with the same identifier and, per the mode,
// creates a new item / skips / updates the existing one.
func Run(lib Library, data zotero.ItemData, kind, value, ifExists string) (Result, error) {
	match := DedupValue(kind, value, data)
	candidates, err := lib.FullTextSearch(match, "", dedupSearchLimit)
	if err != nil {
		return Result{}, err
	}
	dup := FindDuplicate(candidates, kind, match)

	action, err := duplicateAction(dup, ifExists)
	if err != nil {
		return Result{}, err
	}

	result := Result{
		ItemType:       data.ItemType,
		Title:          data.Title,
		Identifier:     value,
		IdentifierKind: kind,
		Duplicate:      dup != nil,
	}
	switch action {
	case actSkip:
		result.Action = ActionSkipped
		result.ItemKey = dup.Key
	case actUpdate:
		if err := lib.UpdateItem(dup.Key, dup.Version, UpdatePayload(data)); err != nil {
			return Result{}, err
		}
		result.Action = ActionUpdated
		result.ItemKey = dup.Key
	default: // actCreate
		key, err := lib.CreateItem(data)
		if err != nil {
			return Result{}, err
		}
		result.Action = ActionCreated
		result.ItemKey = key
	}
	return result, nil
}

// UpdatePayload builds the PATCH body for --if-exists update. It drops tags and
// collections so updating an existing item's bibliographic fields never
// silently clears the tags or collection memberships it already has.
func UpdatePayload(data zotero.ItemData) map[string]interface{} {
	payload := zotero.BuildItemPayload(data)
	delete(payload, "tags")
	delete(payload, "collections")
	return payload
}

// DedupValue is the identifier value used to detect an existing item: the
// resolved canonical URL for a URL add (so a redirect/tracking-param URL still
// matches on re-run), or the raw identifier for DOI/arXiv/ISBN.
func DedupValue(kind, rawValue string, data zotero.ItemData) string {
	if kind == KindURL && data.URL != "" {
		return data.URL
	}
	return rawValue
}

// FindDuplicate returns the first candidate whose identifier field exactly
// matches the one being added, or nil. The exact match guards against a loose
// quick-search returning near-misses (which would wrongly suppress creation).
func FindDuplicate(items []zotero.Item, kind, value string) *zotero.Item {
	for i := range items {
		if matchesIdentifier(items[i].Data, kind, value) {
			return &items[i]
		}
	}
	return nil
}

// duplicateAction validates the mode and decides what to do given a duplicate
// (nil means none was found).
func duplicateAction(dup *zotero.Item, mode string) (string, error) {
	if err := ValidateIfExists(mode); err != nil {
		return "", err
	}
	if dup == nil {
		return actCreate, nil
	}
	switch mode {
	case IfExistsUpdate:
		return actUpdate, nil
	case IfExistsDuplicate:
		return actCreate, nil
	default: // skip
		return actSkip, nil
	}
}

// arxivVersionSuffix matches a trailing arXiv version (e.g. "v7"), stripped so
// a versioned and unversioned ID of the same paper compare equal.
var arxivVersionSuffix = regexp.MustCompile(`v[0-9]+$`)

func matchesIdentifier(d zotero.ItemData, kind, value string) bool {
	switch kind {
	case KindDOI:
		return d.DOI != "" && strings.EqualFold(d.DOI, value)
	case KindArxiv:
		got := stripArxivVersion(zotero.ExtractArxivID(d))
		return got != "" && got == stripArxivVersion(value)
	case KindISBN:
		return d.ISBN != "" && resolve.NormalizeISBN(d.ISBN) == resolve.NormalizeISBN(value)
	case KindURL:
		return d.URL != "" && strings.EqualFold(d.URL, value)
	default:
		return false
	}
}

func stripArxivVersion(id string) string {
	return arxivVersionSuffix.ReplaceAllString(strings.TrimSpace(id), "")
}
