package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yk0817/zotero-cli/additem"
	"github.com/yk0817/zotero-cli/resolve"
	"github.com/yk0817/zotero-cli/zotero"
)

func newAddCmd() *cobra.Command {
	var doi, arxiv, isbn, pageURL, collection, ifExists, email string
	var tags []string
	var dryRun, attachPDF bool

	cmd := &cobra.Command{
		Use:   "add (--doi <doi> | --arxiv <id> | --isbn <isbn> | --url <url>)",
		Short: "Add a library item resolved from a DOI, arXiv ID, ISBN, or URL",
		Args:  cobra.NoArgs,
		Annotations: map[string]string{
			"args": "none — supply exactly one of --doi, --arxiv, --isbn, --url",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, value, err := additem.SelectIdentifier(doi, arxiv, isbn, pageURL)
			if err != nil {
				return &CLIError{
					Code:       ErrCodeInvalidArgument,
					Message:    err.Error(),
					Suggestion: "Provide exactly one of --doi, --arxiv, --isbn, or --url",
				}
			}
			if err := additem.ValidateIfExists(ifExists); err != nil {
				return &CLIError{Code: ErrCodeInvalidArgument, Message: err.Error(), Suggestion: "Use one of: skip, update, duplicate"}
			}
			if collection != "" {
				if err := validateItemKey(collection); err != nil {
					return err
				}
			}

			data, err := additem.Resolve(cmd.Context(), resolve.NewClient(), kind, value)
			if err != nil {
				return resolveCLIError(err, kind, value)
			}
			data = additem.ApplyOptions(data, tags, collection)

			if dryRun {
				return printAddDryRun(kind, value, data, attachPDF)
			}

			client, err := newClient()
			if err != nil {
				return err
			}
			result, err := additem.Run(client, data, kind, value, ifExists)
			if err != nil {
				return &CLIError{Code: ErrCodeAPIError, Message: err.Error()}
			}

			// Without --attach-pdf the output is exactly as before.
			if !attachPDF {
				if isJSON() {
					return printJSON(result)
				}
				printAddResult(result)
				return nil
			}

			// --attach-pdf: fetch and attach an OA PDF, but only for a newly
			// created item. A partial failure (no OA / download / upload) is
			// reported in the output and never fails the command.
			out := addOutput{Result: result}
			if shouldAttachPDF(attachPDF, dryRun, result.Action) {
				out = runAttach(cmd.Context(), client, result, data, kind, email)
			} else {
				out.PdfSkipReason = fmt.Sprintf("item was %s (not newly created); a PDF is attached only to newly created items", result.Action)
			}

			if isJSON() {
				return printJSON(out)
			}
			printAddResult(result)
			printAttachResult(out)
			return nil
		},
	}

	cmd.Flags().StringVar(&doi, "doi", "", "DOI to resolve via Crossref")
	cmd.Flags().StringVar(&arxiv, "arxiv", "", "arXiv ID to resolve via the arXiv API")
	cmd.Flags().StringVar(&isbn, "isbn", "", "ISBN to resolve via OpenLibrary")
	cmd.Flags().StringVar(&pageURL, "url", "", "URL to resolve from embedded page metadata")
	cmd.Flags().StringVar(&collection, "collection", "", "Collection key to file the new item into")
	cmd.Flags().StringSliceVar(&tags, "tags", nil, "Comma-separated tags to add to the item")
	cmd.Flags().StringVar(&ifExists, "if-exists", additem.IfExistsSkip, "On a duplicate (matched by identifier): skip, update, or duplicate")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show the resolved item payload without creating anything")
	cmd.Flags().BoolVar(&attachPDF, "attach-pdf", false, "After creating the item, fetch and attach an open-access PDF (arXiv / Unpaywall / citation_pdf_url)")
	cmd.Flags().StringVar(&email, "email", "", "Contact email for the Unpaywall OA lookup (falls back to config \"email\" or $ZOTERO_CLI_EMAIL)")
	return cmd
}

// resolveCLIError maps a resolver error to the right CLI error code: an unknown
// identifier is NOT_FOUND, anything else (transport/API) is API_ERROR.
func resolveCLIError(err error, kind, value string) error {
	if errors.Is(err, resolve.ErrNotFound) {
		return &CLIError{
			Code:       ErrCodeNotFound,
			Message:    fmt.Sprintf("could not resolve %s %q", kind, value),
			Suggestion: "Check the identifier is correct and known to the source (Crossref/arXiv/OpenLibrary)",
		}
	}
	return &CLIError{Code: ErrCodeAPIError, Message: err.Error()}
}

func printAddDryRun(kind, value string, data zotero.ItemData, attachPDF bool) error {
	payload := zotero.BuildItemPayload(data)
	if isJSON() {
		return printJSON(map[string]any{"dryRun": true, "payload": payload, "attachPdf": attachPDF})
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
	if attachPDF {
		fmt.Println("PDF attach: requested (no PDF is fetched or attached in a dry run)")
	}
	return nil
}

func printAddResult(r additem.Result) {
	switch r.Action {
	case additem.ActionSkipped:
		fmt.Printf("Already in library (skipped): %s [%s]\n", r.Title, r.ItemKey)
		fmt.Println("Use --if-exists update to refresh it, or --if-exists duplicate to add anyway.")
	case additem.ActionUpdated:
		fmt.Printf("Updated existing item: %s [%s]\n", r.Title, r.ItemKey)
	default:
		fmt.Printf("Created item: %s [%s]\n", r.Title, r.ItemKey)
	}
}
