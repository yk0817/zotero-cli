package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yk0817/zotero-cli/additem"
	"github.com/yk0817/zotero-cli/oapdf"
	"github.com/yk0817/zotero-cli/zotero"
)

// pdfContentType is the MIME type stored for an attached OA PDF. The fetcher has
// already validated the bytes are a PDF, so this is not guessed from the URL.
const pdfContentType = "application/pdf"

// unpaywallEmailEnv is the env var fallback for the Unpaywall contact email,
// mirroring the SEMANTIC_SCHOLAR_API_KEY precedent for optional identifiers used
// only to talk to a third-party API.
const unpaywallEmailEnv = "ZOTERO_CLI_EMAIL"

// pdfFetcher locates and downloads an OA PDF. *oapdf.Fetcher satisfies it; a
// test can substitute a fake so the attach orchestration is exercised without a
// network.
type pdfFetcher interface {
	Fetch(ctx context.Context, data zotero.ItemData, sourcePageURL string) (*oapdf.Result, error)
}

// pdfUploader is the subset of *zotero.Client used to attach a file — the same
// four-step upload path the `upload` command uses. Kept as an interface so the
// attach flow can be tested with a fake that records the calls.
type pdfUploader interface {
	CreateAttachment(parentKey, filename, title, contentType string, tags []string) (string, error)
	GetUploadAuthorization(itemKey, filename string, filesize int64, md5hex string, mtime int64) (*zotero.UploadAuthorization, error)
	UploadFileContent(auth *zotero.UploadAuthorization, fileContent []byte) error
	RegisterUpload(itemKey, uploadKey string) error
}

// attachOutcome is the result of attempting to attach an OA PDF to a created
// item. On any failure Attached is false and SkipReason explains why — attaching
// is best-effort and never turns into a command failure (issue #30 P1).
type attachOutcome struct {
	Attached      bool
	Source        string
	AttachmentKey string
	SkipReason    string
}

// addOutput is the JSON/text shape of `add --attach-pdf`. It embeds the shared
// additem.Result (so the existing add fields stay identical) and adds the PDF
// outcome. PdfAttached has no omitempty so a consumer can always read whether a
// PDF was attached; the reason/source/key appear only when relevant.
type addOutput struct {
	additem.Result
	PdfAttached   bool   `json:"pdfAttached"`
	PdfSource     string `json:"pdfSource,omitempty"`
	AttachmentKey string `json:"attachmentKey,omitempty"`
	PdfSkipReason string `json:"pdfSkipReason,omitempty"`
}

// shouldAttachPDF reports whether an OA PDF fetch should run. It is gated to
// newly created items and never runs under --dry-run (which must not touch the
// network for a PDF), so a re-add that skipped/updated an existing item does not
// silently fetch and attach a second copy.
func shouldAttachPDF(attachPDF, dryRun bool, action string) bool {
	return attachPDF && !dryRun && action == additem.ActionCreated
}

// resolveUnpaywallEmail picks the Unpaywall contact email by precedence:
// --email flag, then the config file's "email", then $ZOTERO_CLI_EMAIL. The
// email is not a secret (it is a polite-pool identifier), so an env fallback is
// safe and matches how the repo already sources the optional Semantic Scholar
// key.
func resolveUnpaywallEmail(flagEmail, configEmail string) string {
	if e := strings.TrimSpace(flagEmail); e != "" {
		return e
	}
	if e := strings.TrimSpace(configEmail); e != "" {
		return e
	}
	return strings.TrimSpace(os.Getenv(unpaywallEmailEnv))
}

// oaSkipReason renders a fetch error into a human-readable skip reason,
// distinguishing "no OA available" from a concrete download failure.
func oaSkipReason(err error) string {
	if errors.Is(err, oapdf.ErrNoOAPDF) {
		return "no open-access PDF found for this item"
	}
	return err.Error()
}

// attachOAPDF fetches an OA PDF for the created item and uploads it as a child
// attachment via the standard four-step Zotero upload path. Every failure path
// (no OA, download failure, or an upload step failing) returns a non-attached
// outcome with a reason instead of an error: the item was already created
// successfully, so a PDF problem must not fail the command.
//
// If the attachment item is created but a later upload step fails, that orphan
// attachment (metadata without a stored file) is left in place — the same
// outcome the `upload` command produces on a mid-flight failure — and reported
// as the skip reason.
func attachOAPDF(ctx context.Context, up pdfUploader, fetch pdfFetcher, itemKey, title string, data zotero.ItemData, sourcePageURL string) attachOutcome {
	res, err := fetch.Fetch(ctx, data, sourcePageURL)
	if err != nil {
		return attachOutcome{SkipReason: oaSkipReason(err)}
	}

	filename := itemKey + ".pdf"
	attachKey, err := up.CreateAttachment(itemKey, filename, title, pdfContentType, nil)
	if err != nil {
		return attachOutcome{SkipReason: fmt.Sprintf("failed to create attachment: %v", err)}
	}

	sum := md5.Sum(res.Content)
	md5hex := hex.EncodeToString(sum[:])
	mtime := time.Now().UnixMilli()

	auth, err := up.GetUploadAuthorization(attachKey, filename, int64(len(res.Content)), md5hex, mtime)
	if err != nil {
		return attachOutcome{SkipReason: fmt.Sprintf("failed to get upload authorization: %v", err)}
	}
	if err := up.UploadFileContent(auth, res.Content); err != nil {
		return attachOutcome{SkipReason: fmt.Sprintf("failed to upload PDF: %v", err)}
	}
	if err := up.RegisterUpload(attachKey, auth.UploadKey); err != nil {
		return attachOutcome{SkipReason: fmt.Sprintf("failed to register upload: %v", err)}
	}

	return attachOutcome{Attached: true, Source: res.Source, AttachmentKey: attachKey}
}

// runAttach fetches and attaches an OA PDF for a just-created item, returning
// the combined add + PDF output. The Unpaywall email is resolved here (flag >
// config > env); the source page is scraped for citation_pdf_url only on a URL
// add. Any failure is captured in the outcome, never returned as an error.
func runAttach(ctx context.Context, client pdfUploader, result additem.Result, data zotero.ItemData, kind, emailFlag string) addOutput {
	out := addOutput{Result: result}

	cfgEmail := ""
	if cfg, err := loadConfig(); err == nil {
		cfgEmail = cfg.Email
	}
	fetcher := oapdf.NewFetcher(resolveUnpaywallEmail(emailFlag, cfgEmail))

	sourcePageURL := ""
	if kind == additem.KindURL {
		sourcePageURL = data.URL
	}

	o := attachOAPDF(ctx, client, fetcher, result.ItemKey, result.Title, data, sourcePageURL)
	out.PdfAttached = o.Attached
	out.PdfSource = o.Source
	out.AttachmentKey = o.AttachmentKey
	out.PdfSkipReason = o.SkipReason
	return out
}

// printAttachResult reports the PDF outcome for text output, after the item
// line: a success goes to stdout as part of the result, a skip goes to stderr as
// a warning so it is visible without polluting a captured result.
func printAttachResult(out addOutput) {
	if out.PdfAttached {
		fmt.Printf("Attached OA PDF (%s): %s\n", out.PdfSource, out.AttachmentKey)
		return
	}
	fmt.Fprintf(os.Stderr, "Warning: OA PDF not attached: %s\n", out.PdfSkipReason)
}
