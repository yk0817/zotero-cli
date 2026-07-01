package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yk0817/zotero-cli/resolve"
	"github.com/yk0817/zotero-cli/zotero"
)

// identifier kinds selected by the --doi/--arxiv/--isbn/--url flags.
const (
	kindDOI   = "doi"
	kindArxiv = "arxiv"
	kindISBN  = "isbn"
	kindURL   = "url"
)

// --if-exists modes (what to do when an item with the same identifier exists).
const (
	ifExistsSkip      = "skip"
	ifExistsUpdate    = "update"
	ifExistsDuplicate = "duplicate"
)

// resolved actions taken for a duplicate.
const (
	actionCreate = "create"
	actionSkip   = "skip"
	actionUpdate = "update"
)

// dedupSearchLimit bounds the quick-search used to find an existing item with
// the same identifier before creating a new one.
const dedupSearchLimit = 50

// addResult is the JSON payload (inside the {"ok":true,"data":...} envelope)
// describing what add did.
type addResult struct {
	Action         string `json:"action"` // created | skipped | updated
	ItemKey        string `json:"itemKey"`
	ItemType       string `json:"itemType"`
	Title          string `json:"title"`
	Identifier     string `json:"identifier"`
	IdentifierKind string `json:"identifierKind"`
	Duplicate      bool   `json:"duplicate"`
}

func newAddCmd() *cobra.Command {
	var doi, arxiv, isbn, pageURL, collection, ifExists string
	var tags []string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "add (--doi <doi> | --arxiv <id> | --isbn <isbn> | --url <url>)",
		Short: "Add a library item resolved from a DOI, arXiv ID, ISBN, or URL",
		Args:  cobra.NoArgs,
		Annotations: map[string]string{
			"args": "none — supply exactly one of --doi, --arxiv, --isbn, --url",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, value, err := selectIdentifier(doi, arxiv, isbn, pageURL)
			if err != nil {
				return err
			}
			// Validate the mode up front so a typo fails before any network call.
			if _, err := duplicateAction(nil, ifExists); err != nil {
				return err
			}
			if collection != "" {
				if err := validateItemKey(collection); err != nil {
					return err
				}
			}

			data, err := resolveByKind(cmd, kind, value)
			if err != nil {
				if errors.Is(err, resolve.ErrNotFound) {
					return &CLIError{
						Code:       ErrCodeNotFound,
						Message:    fmt.Sprintf("could not resolve %s %q", kind, value),
						Suggestion: "Check the identifier is correct and known to the source (Crossref/arXiv/OpenLibrary)",
					}
				}
				return &CLIError{Code: ErrCodeAPIError, Message: err.Error()}
			}
			data = applyAddOptions(data, tags, collection)

			if dryRun {
				return printAddDryRun(kind, value, data)
			}

			return runAdd(kind, value, data, ifExists)
		},
	}

	cmd.Flags().StringVar(&doi, "doi", "", "DOI to resolve via Crossref")
	cmd.Flags().StringVar(&arxiv, "arxiv", "", "arXiv ID to resolve via the arXiv API")
	cmd.Flags().StringVar(&isbn, "isbn", "", "ISBN to resolve via OpenLibrary")
	cmd.Flags().StringVar(&pageURL, "url", "", "URL to resolve from embedded page metadata")
	cmd.Flags().StringVar(&collection, "collection", "", "Collection key to file the new item into")
	cmd.Flags().StringSliceVar(&tags, "tags", nil, "Comma-separated tags to add to the item")
	cmd.Flags().StringVar(&ifExists, "if-exists", ifExistsSkip, "On a duplicate (matched by identifier): skip, update, or duplicate")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show the resolved item payload without creating anything")
	return cmd
}

// resolveByKind dispatches to the resolver for the chosen identifier kind.
func resolveByKind(cmd *cobra.Command, kind, value string) (zotero.ItemData, error) {
	r := resolve.NewClient()
	ctx := cmd.Context()
	switch kind {
	case kindDOI:
		return r.ResolveDOI(ctx, value)
	case kindArxiv:
		return r.ResolveArXiv(ctx, value)
	case kindISBN:
		return r.ResolveISBN(ctx, value)
	case kindURL:
		return r.ResolveURL(ctx, value)
	default:
		return zotero.ItemData{}, fmt.Errorf("unknown identifier kind %q", kind)
	}
}

// runAdd performs the (non-dry-run) create/skip/update against the library.
func runAdd(kind, value string, data zotero.ItemData, ifExists string) error {
	client, err := newClient()
	if err != nil {
		return err
	}

	// Dedupe against the resolved identifier: for a URL that is the canonical
	// og:url (which may differ from what the user typed), for everything else
	// the identifier value itself.
	match := dedupValue(kind, value, data)
	candidates, err := client.FullTextSearch(match, "", dedupSearchLimit)
	if err != nil {
		return &CLIError{Code: ErrCodeAPIError, Message: err.Error()}
	}
	dup := findDuplicate(candidates, kind, match)

	action, err := duplicateAction(dup, ifExists)
	if err != nil {
		return err
	}

	result := addResult{
		ItemType:       data.ItemType,
		Title:          data.Title,
		Identifier:     value,
		IdentifierKind: kind,
		Duplicate:      dup != nil,
	}
	switch action {
	case actionSkip:
		result.Action = "skipped"
		result.ItemKey = dup.Key
	case actionUpdate:
		if err := client.UpdateItem(dup.Key, dup.Version, updatePayload(data)); err != nil {
			return &CLIError{Code: ErrCodeAPIError, Message: err.Error()}
		}
		result.Action = "updated"
		result.ItemKey = dup.Key
	default: // actionCreate
		key, err := client.CreateItem(data)
		if err != nil {
			return &CLIError{Code: ErrCodeAPIError, Message: err.Error()}
		}
		result.Action = "created"
		result.ItemKey = key
	}

	if isJSON() {
		return printJSON(result)
	}
	printAddResult(result)
	return nil
}

// updatePayload builds the PATCH body for --if-exists update. It drops tags and
// collections so updating an existing item's bibliographic fields never
// silently clears the tags or collection memberships the item already has.
func updatePayload(data zotero.ItemData) map[string]interface{} {
	payload := zotero.BuildItemPayload(data)
	delete(payload, "tags")
	delete(payload, "collections")
	return payload
}

// selectIdentifier returns the single identifier kind/value chosen by the
// flags, erroring unless exactly one of the four is provided.
func selectIdentifier(doi, arxiv, isbn, url string) (string, string, error) {
	candidates := []struct{ kind, value string }{
		{kindDOI, strings.TrimSpace(doi)},
		{kindArxiv, strings.TrimSpace(arxiv)},
		{kindISBN, strings.TrimSpace(isbn)},
		{kindURL, strings.TrimSpace(url)},
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
		return "", "", &CLIError{
			Code:       ErrCodeInvalidArgument,
			Message:    "no identifier given",
			Suggestion: "Provide exactly one of --doi, --arxiv, --isbn, or --url",
		}
	default:
		return "", "", &CLIError{
			Code:       ErrCodeInvalidArgument,
			Message:    "multiple identifiers given",
			Suggestion: "Provide exactly one of --doi, --arxiv, --isbn, or --url",
		}
	}
}

// duplicateAction validates the mode and decides what to do given a duplicate
// (nil means none was found).
func duplicateAction(dup *zotero.Item, mode string) (string, error) {
	switch mode {
	case ifExistsSkip, ifExistsUpdate, ifExistsDuplicate:
	default:
		return "", &CLIError{
			Code:       ErrCodeInvalidArgument,
			Message:    fmt.Sprintf("invalid --if-exists %q", mode),
			Suggestion: "Use one of: skip, update, duplicate",
		}
	}
	if dup == nil {
		return actionCreate, nil
	}
	switch mode {
	case ifExistsUpdate:
		return actionUpdate, nil
	case ifExistsDuplicate:
		return actionCreate, nil
	default: // skip
		return actionSkip, nil
	}
}

// dedupValue is the identifier value used to detect an existing item: the
// resolved canonical URL for a URL add (so a redirect/tracking-param URL still
// matches on re-run), or the raw identifier for DOI/arXiv/ISBN.
func dedupValue(kind, rawValue string, data zotero.ItemData) string {
	if kind == kindURL && data.URL != "" {
		return data.URL
	}
	return rawValue
}

// findDuplicate returns the first candidate whose identifier field exactly
// matches the one being added, or nil. The exact match guards against a loose
// quick-search returning near-misses (which would wrongly suppress creation).
func findDuplicate(items []zotero.Item, kind, value string) *zotero.Item {
	for i := range items {
		if matchesIdentifier(items[i].Data, kind, value) {
			return &items[i]
		}
	}
	return nil
}

var arxivVersionSuffix = regexp.MustCompile(`v[0-9]+$`)

func matchesIdentifier(d zotero.ItemData, kind, value string) bool {
	switch kind {
	case kindDOI:
		return d.DOI != "" && strings.EqualFold(d.DOI, value)
	case kindArxiv:
		got := stripArxivVersion(extractArxivID(d))
		return got != "" && got == stripArxivVersion(value)
	case kindISBN:
		return d.ISBN != "" && resolve.NormalizeISBN(d.ISBN) == resolve.NormalizeISBN(value)
	case kindURL:
		return d.URL != "" && strings.EqualFold(d.URL, value)
	default:
		return false
	}
}

func stripArxivVersion(id string) string {
	return arxivVersionSuffix.ReplaceAllString(strings.TrimSpace(id), "")
}

// applyAddOptions merges user --tags (deduplicated against tags the resolver
// already set) and --collection into the resolved metadata, returning a copy so
// the input is not mutated.
func applyAddOptions(data zotero.ItemData, tags []string, collection string) zotero.ItemData {
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

func printAddDryRun(kind, value string, data zotero.ItemData) error {
	payload := zotero.BuildItemPayload(data)
	if isJSON() {
		return printJSON(map[string]any{"dryRun": true, "payload": payload})
	}
	pretty, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println("=== DRY RUN (no item will be created) ===")
	fmt.Printf("Identifier: %s %s\n", kind, value)
	fmt.Printf("Item type:  %s\n", data.ItemType)
	fmt.Printf("Title:      %s\n", data.Title)
	fmt.Printf("Payload:\n%s\n", string(pretty))
	return nil
}

func printAddResult(r addResult) {
	switch r.Action {
	case "skipped":
		fmt.Printf("Already in library (skipped): %s [%s]\n", r.Title, r.ItemKey)
		fmt.Println("Use --if-exists update to refresh it, or --if-exists duplicate to add anyway.")
	case "updated":
		fmt.Printf("Updated existing item: %s [%s]\n", r.Title, r.ItemKey)
	default:
		fmt.Printf("Created item: %s [%s]\n", r.Title, r.ItemKey)
	}
}
