package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/yk0817/zotero-cli/scholar"
	"github.com/yk0817/zotero-cli/zotero"
)

// citation directions.
const (
	dirBackward = "backward"
	dirForward  = "forward"
	dirBoth     = "both"
)

// arxivIDPattern extracts an arXiv identifier from a URL such as
// https://arxiv.org/abs/2301.01234v2 or .../pdf/2301.01234. Zotero has no
// dedicated arXiv field, so the URL is the most reliable place to find it when
// a DOI is absent.
var arxivIDPattern = regexp.MustCompile(`arxiv\.org/(?:abs|pdf)/([0-9]{4}\.[0-9]{4,5}(?:v[0-9]+)?)`)

// extractArxivID returns the arXiv ID for an item, or "" if none is found. It
// looks at the URL and at an arXiv-style DOI (10.48550/arXiv.<id>).
func extractArxivID(d zotero.ItemData) string {
	if m := arxivIDPattern.FindStringSubmatch(d.URL); m != nil {
		return m[1]
	}
	if id := strings.TrimPrefix(d.DOI, "10.48550/arXiv."); id != d.DOI {
		return id
	}
	return ""
}

// citationsData is the JSON payload shape (inside the {"ok":true,"data":...}
// envelope). backward/forward are always non-nil so an empty network renders as
// [] rather than null.
type citationsData struct {
	ItemKey   string             `json:"itemKey"`
	PaperID   string             `json:"paperId"`
	Title     string             `json:"title"`
	Direction string             `json:"direction"`
	Backward  []scholar.PaperRef `json:"backward"`
	Forward   []scholar.PaperRef `json:"forward"`
}

func newCitationsCmd() *cobra.Command {
	var direction string
	var limit int
	cmd := &cobra.Command{
		Use:   "citations <itemKey>",
		Short: "List a paper's references and citations via Semantic Scholar",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"args": "itemKey: 8-character alphanumeric item key (required)",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateItemKey(args[0]); err != nil {
				return err
			}
			dir := strings.ToLower(direction)
			if dir != dirBackward && dir != dirForward && dir != dirBoth {
				return &CLIError{
					Code:       ErrCodeInvalidArgument,
					Message:    fmt.Sprintf("invalid --direction %q", direction),
					Suggestion: "Use one of: backward, forward, both",
				}
			}

			client, err := newClient()
			if err != nil {
				return err
			}
			item, err := client.GetItem(args[0])
			if err != nil {
				return err
			}

			sc := scholar.NewClient()
			ctx := context.Background()
			paperID, err := sc.ResolvePaperID(ctx, item.Data.DOI, extractArxivID(item.Data), item.Data.Title)
			if err != nil {
				if errors.Is(err, scholar.ErrPaperNotFound) {
					return &CLIError{
						Code:       ErrCodeNotFound,
						Message:    fmt.Sprintf("could not identify %q on Semantic Scholar (no DOI, arXiv ID, or title match)", item.Data.Title),
						Suggestion: "Ensure the item has a DOI or arXiv ID, or a precise title",
					}
				}
				return &CLIError{Code: ErrCodeAPIError, Message: err.Error()}
			}

			data := citationsData{
				ItemKey:   args[0],
				PaperID:   paperID,
				Title:     item.Data.Title,
				Direction: dir,
				Backward:  []scholar.PaperRef{},
				Forward:   []scholar.PaperRef{},
			}

			if dir == dirBackward || dir == dirBoth {
				refs, err := sc.References(ctx, paperID, limit)
				if err != nil {
					return &CLIError{Code: ErrCodeAPIError, Message: err.Error()}
				}
				data.Backward = refs
			}
			if dir == dirForward || dir == dirBoth {
				cites, err := sc.Citations(ctx, paperID, limit)
				if err != nil {
					return &CLIError{Code: ErrCodeAPIError, Message: err.Error()}
				}
				// Forward edges are most useful highest-impact-first.
				sort.SliceStable(cites, func(i, j int) bool {
					return cites[i].CitationCount > cites[j].CitationCount
				})
				data.Forward = cites
			}

			if isJSON() {
				return printJSON(data)
			}
			printCitations(data, dir)
			return nil
		},
	}
	cmd.Flags().StringVar(&direction, "direction", dirBoth, "Citation direction: backward (references), forward (cited by), or both")
	cmd.Flags().IntVar(&limit, "limit", scholar.DefaultLimit, "Max papers per direction")
	return cmd
}

// printCitations renders the citation network as human/LLM-readable tables.
func printCitations(data citationsData, dir string) {
	fmt.Printf("=== CITATION NETWORK: %s ===\n", data.Title)
	fmt.Printf("Resolved paper: %s\n", data.PaperID)

	if dir == dirBackward || dir == dirBoth {
		fmt.Printf("\n=== REFERENCES (backward, %d) — papers this work cites ===\n", len(data.Backward))
		printCitationTable(data.Backward)
	}
	if dir == dirForward || dir == dirBoth {
		fmt.Printf("\n=== CITED BY (forward, %d, by citation count) — papers citing this work ===\n", len(data.Forward))
		printCitationTable(data.Forward)
	}
}

// printCitationTable prints one PaperRef list, or a clear empty-state line so an
// LLM never mistakes a blank section for a render failure.
func printCitationTable(refs []scholar.PaperRef) {
	if len(refs) == 0 {
		fmt.Println("(none found via Semantic Scholar)")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "YEAR\tTITLE\tAUTHORS\tCITED")
	fmt.Fprintln(w, "----\t-----\t-------\t-----")
	for _, r := range refs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\n",
			formatYear(r.Year),
			zotero.Truncate(orDash(r.Title), 60),
			zotero.Truncate(formatScholarAuthors(r.Authors), 30),
			r.CitationCount,
		)
	}
	w.Flush()
}

func formatYear(year int) string {
	if year == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", year)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// formatScholarAuthors renders author names, truncating to "et al." past three.
func formatScholarAuthors(authors []scholar.Author) string {
	names := []string{}
	for _, a := range authors {
		if a.Name != "" {
			names = append(names, a.Name)
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
