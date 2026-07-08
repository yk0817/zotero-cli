package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/yk0817/zotero-cli/additem"
	"github.com/yk0817/zotero-cli/oapdf"
	"github.com/yk0817/zotero-cli/zotero"
)

// fakeFetcher returns a canned fetch outcome and records what it was asked for.
type fakeFetcher struct {
	res              *oapdf.Result
	err              error
	calls            int
	gotSourcePageURL string
}

func (f *fakeFetcher) Fetch(_ context.Context, _ zotero.ItemData, sourcePageURL string) (*oapdf.Result, error) {
	f.calls++
	f.gotSourcePageURL = sourcePageURL
	return f.res, f.err
}

// fakeUploader records each of the four upload steps and fails at a configured
// one, so a test can assert both what was sent and that a mid-flight failure
// stops the sequence.
type fakeUploader struct {
	createErr   error
	authErr     error
	uploadErr   error
	registerErr error

	createCalls        int
	createdParent      string
	createdFilename    string
	createdTitle       string
	createdContentType string

	authCalls    int
	authItemKey  string
	authFilename string
	authFilesize int64
	authMD5      string
	authMtime    int64

	uploadedContent []byte

	registerCalls     int
	registerItemKey   string
	registerUploadKey string
}

func (u *fakeUploader) CreateAttachment(parentKey, filename, title, contentType string, _ []string) (string, error) {
	u.createCalls++
	u.createdParent = parentKey
	u.createdFilename = filename
	u.createdTitle = title
	u.createdContentType = contentType
	if u.createErr != nil {
		return "", u.createErr
	}
	return "PDFATT01", nil
}

func (u *fakeUploader) GetUploadAuthorization(itemKey, filename string, filesize int64, md5hex string, mtime int64) (*zotero.UploadAuthorization, error) {
	u.authCalls++
	u.authItemKey = itemKey
	u.authFilename = filename
	u.authFilesize = filesize
	u.authMD5 = md5hex
	u.authMtime = mtime
	if u.authErr != nil {
		return nil, u.authErr
	}
	return &zotero.UploadAuthorization{UploadKey: "UPKEY123", URL: "https://upload.test"}, nil
}

func (u *fakeUploader) UploadFileContent(_ *zotero.UploadAuthorization, content []byte) error {
	u.uploadedContent = content
	return u.uploadErr
}

func (u *fakeUploader) RegisterUpload(itemKey, uploadKey string) error {
	u.registerCalls++
	u.registerItemKey = itemKey
	u.registerUploadKey = uploadKey
	return u.registerErr
}

// Contract: on a successful fetch the PDF is uploaded as a child of the created
// item via all four upload steps, stored as application/pdf, and the outcome
// reports the source and attachment key. Getting the parent or content type
// wrong would orphan the file or mislabel it in Zotero.
func TestAttachOAPDFSuccess(t *testing.T) {
	fetch := &fakeFetcher{res: &oapdf.Result{Content: []byte("%PDF-1.7 data"), Source: oapdf.SourceArxiv, URL: "https://arxiv.org/pdf/2301.01234"}}
	up := &fakeUploader{}

	out := attachOAPDF(context.Background(), up, fetch, "ITEM0001", "My Paper", zotero.ItemData{}, "")

	if !out.Attached {
		t.Fatalf("expected Attached=true, got %+v", out)
	}
	if out.Source != oapdf.SourceArxiv || out.AttachmentKey != "PDFATT01" {
		t.Errorf("outcome = %+v, want arxiv source + PDFATT01", out)
	}
	if up.createdParent != "ITEM0001" {
		t.Errorf("attachment parent = %q, want ITEM0001 (must be a child of the created item)", up.createdParent)
	}
	if up.createdContentType != "application/pdf" {
		t.Errorf("content type = %q, want application/pdf", up.createdContentType)
	}
	if up.createdFilename != "ITEM0001.pdf" {
		t.Errorf("filename = %q, want ITEM0001.pdf", up.createdFilename)
	}
	if string(up.uploadedContent) != "%PDF-1.7 data" {
		t.Errorf("uploaded content = %q, want the fetched PDF bytes", up.uploadedContent)
	}
	if up.registerCalls != 1 {
		t.Errorf("RegisterUpload calls = %d, want 1 (upload must be finalized)", up.registerCalls)
	}

	// The upload-authorization step must describe the actual file: it is keyed on
	// the attachment item (not the parent), and carries the true byte length and
	// md5 of the fetched PDF. A wrong size/hash makes Zotero reject or mismatch the
	// upload, so assert them rather than only that the call happened.
	sum := md5.Sum([]byte("%PDF-1.7 data"))
	wantMD5 := hex.EncodeToString(sum[:])
	if up.authItemKey != "PDFATT01" {
		t.Errorf("upload-authorization itemKey = %q, want the attachment key PDFATT01", up.authItemKey)
	}
	if up.authFilename != "ITEM0001.pdf" {
		t.Errorf("upload-authorization filename = %q, want ITEM0001.pdf", up.authFilename)
	}
	if up.authFilesize != int64(len("%PDF-1.7 data")) {
		t.Errorf("upload-authorization filesize = %d, want %d (byte length of the PDF)", up.authFilesize, len("%PDF-1.7 data"))
	}
	if up.authMD5 != wantMD5 {
		t.Errorf("upload-authorization md5 = %q, want %q (md5 of the fetched bytes)", up.authMD5, wantMD5)
	}
	if up.authMtime <= 0 {
		t.Errorf("upload-authorization mtime = %d, want a positive timestamp", up.authMtime)
	}
	// Finalization must name the same attachment item and the upload key returned
	// by authorization; swapped or empty args would corrupt the registration.
	if up.registerItemKey != "PDFATT01" || up.registerUploadKey != "UPKEY123" {
		t.Errorf("RegisterUpload(itemKey=%q, uploadKey=%q), want (PDFATT01, UPKEY123)", up.registerItemKey, up.registerUploadKey)
	}
}

// Contract: when no OA PDF exists the command must NOT fail — attachOAPDF
// returns a non-attached outcome with a clear reason and makes zero upload
// calls, so item creation still stands (issue #30 partial-failure policy).
func TestAttachOAPDFNoOASkips(t *testing.T) {
	fetch := &fakeFetcher{err: oapdf.ErrNoOAPDF}
	up := &fakeUploader{}

	out := attachOAPDF(context.Background(), up, fetch, "ITEM0001", "T", zotero.ItemData{}, "")

	if out.Attached {
		t.Fatal("expected Attached=false when no OA PDF")
	}
	if out.SkipReason != "no open-access PDF found for this item" {
		t.Errorf("skip reason = %q", out.SkipReason)
	}
	if up.createCalls != 0 {
		t.Errorf("expected no upload attempt when nothing was fetched, got %d CreateAttachment calls", up.createCalls)
	}
}

// Contract: a download failure (not ErrNoOAPDF) is also non-fatal and its detail
// is surfaced as the skip reason, so the operator can see WHY the PDF was
// skipped without the add command itself failing.
func TestAttachOAPDFDownloadFailureSkips(t *testing.T) {
	fetch := &fakeFetcher{err: errors.New("arxiv: HTTP 500 downloading https://arxiv.org/pdf/x")}
	up := &fakeUploader{}

	out := attachOAPDF(context.Background(), up, fetch, "ITEM0001", "T", zotero.ItemData{}, "")

	if out.Attached {
		t.Fatal("expected Attached=false on download failure")
	}
	if !strings.Contains(out.SkipReason, "500") {
		t.Errorf("skip reason should carry the download error, got %q", out.SkipReason)
	}
	if up.createCalls != 0 {
		t.Errorf("expected no upload attempt after a failed download, got %d", up.createCalls)
	}
}

// Contract: a failure partway through the upload (here, creating the attachment
// item) is caught and reported as a skip, never propagated as a command error —
// the item creation already succeeded.
func TestAttachOAPDFCreateAttachmentFailureSkips(t *testing.T) {
	fetch := &fakeFetcher{res: &oapdf.Result{Content: []byte("%PDF-x"), Source: oapdf.SourceUnpaywall}}
	up := &fakeUploader{createErr: errors.New("boom")}

	out := attachOAPDF(context.Background(), up, fetch, "ITEM0001", "T", zotero.ItemData{}, "")

	if out.Attached {
		t.Fatal("expected Attached=false when the attachment item cannot be created")
	}
	if !strings.Contains(out.SkipReason, "failed to create attachment") || !strings.Contains(out.SkipReason, "boom") {
		t.Errorf("skip reason = %q, want the create-attachment failure", out.SkipReason)
	}
	if up.authCalls != 0 {
		t.Errorf("upload must stop after CreateAttachment fails, but authorization was requested")
	}
}

// Contract: a failure during the file transfer (after auth) is a skip, and the
// upload is NOT finalized (RegisterUpload), so Zotero never acknowledges a file
// that did not actually upload.
func TestAttachOAPDFUploadFailureDoesNotRegister(t *testing.T) {
	fetch := &fakeFetcher{res: &oapdf.Result{Content: []byte("%PDF-x"), Source: oapdf.SourceCitationPDF}}
	up := &fakeUploader{uploadErr: errors.New("transfer reset")}

	out := attachOAPDF(context.Background(), up, fetch, "ITEM0001", "T", zotero.ItemData{}, "")

	if out.Attached {
		t.Fatal("expected Attached=false on transfer failure")
	}
	if !strings.Contains(out.SkipReason, "failed to upload PDF") {
		t.Errorf("skip reason = %q, want the upload failure", out.SkipReason)
	}
	if up.registerCalls != 0 {
		t.Errorf("RegisterUpload must not run after a failed transfer, got %d calls", up.registerCalls)
	}
}

// Contract: attachOAPDF passes the source page URL through to the fetcher so the
// citation_pdf_url source can scrape it (only supplied for URL adds).
func TestAttachOAPDFPassesSourcePageURL(t *testing.T) {
	fetch := &fakeFetcher{err: oapdf.ErrNoOAPDF}

	attachOAPDF(context.Background(), &fakeUploader{}, fetch, "ITEM0001", "T", zotero.ItemData{}, "https://journal.test/article")

	if fetch.gotSourcePageURL != "https://journal.test/article" {
		t.Errorf("source page URL = %q, want it passed through to the fetcher", fetch.gotSourcePageURL)
	}
}

// Contract: the Unpaywall email is chosen flag > config > env. Getting this
// order wrong would ignore an explicit --email override or a per-run env value.
func TestResolveUnpaywallEmailPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		flag       string
		config     string
		env        string
		wantResult string
	}{
		{name: "flag wins over all", flag: "flag@x.com", config: "cfg@x.com", env: "env@x.com", wantResult: "flag@x.com"},
		{name: "config when no flag", config: "cfg@x.com", env: "env@x.com", wantResult: "cfg@x.com"},
		{name: "env when no flag or config", env: "env@x.com", wantResult: "env@x.com"},
		{name: "empty when none set", wantResult: ""},
		{name: "whitespace flag falls through", flag: "   ", config: "cfg@x.com", wantResult: "cfg@x.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(unpaywallEmailEnv, tt.env)

			got := resolveUnpaywallEmail(tt.flag, tt.config)

			if got != tt.wantResult {
				t.Errorf("resolveUnpaywallEmail(%q, %q) with env %q = %q, want %q", tt.flag, tt.config, tt.env, got, tt.wantResult)
			}
		})
	}
}

// Contract: an OA PDF is fetched only for a newly created item and never under
// --dry-run (which must not hit the network for a PDF). A skipped/updated re-add
// must not silently attach a second copy.
func TestShouldAttachPDF(t *testing.T) {
	tests := []struct {
		name      string
		attachPDF bool
		dryRun    bool
		action    string
		want      bool
	}{
		{name: "flag off", attachPDF: false, action: additem.ActionCreated, want: false},
		{name: "created", attachPDF: true, action: additem.ActionCreated, want: true},
		{name: "dry-run never fetches", attachPDF: true, dryRun: true, action: additem.ActionCreated, want: false},
		{name: "skipped existing", attachPDF: true, action: additem.ActionSkipped, want: false},
		{name: "updated existing", attachPDF: true, action: additem.ActionUpdated, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldAttachPDF(tt.attachPDF, tt.dryRun, tt.action); got != tt.want {
				t.Errorf("shouldAttachPDF(%v,%v,%q) = %v, want %v", tt.attachPDF, tt.dryRun, tt.action, got, tt.want)
			}
		})
	}
}

// Contract: the JSON output embeds the existing add fields (so consumers of
// `add` see no change) and always carries pdfAttached; source/key/reason appear
// only when relevant. An AI agent relies on pdfAttached to know whether it must
// fall back to another way of getting the PDF.
func TestAddOutputJSONShape(t *testing.T) {
	attached := addOutput{
		Result:        additem.Result{Action: additem.ActionCreated, ItemKey: "ITEM0001", Title: "T"},
		PdfAttached:   true,
		PdfSource:     oapdf.SourceArxiv,
		AttachmentKey: "PDFATT01",
	}
	m := marshalToMap(t, attached)
	if m["action"] != "created" || m["itemKey"] != "ITEM0001" {
		t.Errorf("embedded add fields missing/flattened wrong: %v", m)
	}
	if m["pdfAttached"] != true {
		t.Errorf("pdfAttached = %v, want true", m["pdfAttached"])
	}
	if m["pdfSource"] != "arxiv" || m["attachmentKey"] != "PDFATT01" {
		t.Errorf("pdfSource/attachmentKey missing: %v", m)
	}
	if _, has := m["pdfSkipReason"]; has {
		t.Errorf("pdfSkipReason must be omitted when attached: %v", m)
	}

	skipped := addOutput{
		Result:        additem.Result{Action: additem.ActionCreated, ItemKey: "ITEM0002"},
		PdfAttached:   false,
		PdfSkipReason: "no open-access PDF found for this item",
	}
	m = marshalToMap(t, skipped)
	if m["pdfAttached"] != false {
		t.Errorf("pdfAttached = %v, want false", m["pdfAttached"])
	}
	if m["pdfSkipReason"] != "no open-access PDF found for this item" {
		t.Errorf("pdfSkipReason = %v", m["pdfSkipReason"])
	}
	if _, has := m["attachmentKey"]; has {
		t.Errorf("attachmentKey must be omitted when not attached: %v", m)
	}
	if _, has := m["pdfSource"]; has {
		t.Errorf("pdfSource must be omitted when not attached: %v", m)
	}
}

func marshalToMap(t *testing.T, v any) map[string]any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}
