package zotero

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Contract: CreateAttachment builds an imported_file attachment item; when
// no title is given the filename stands in, so Zotero never shows a blank row.
func TestCreateAttachmentDefaultsTitleToFilename(t *testing.T) {
	client, rt := newRecordingClient(http.StatusOK, `{"successful":{"0":{"key":"ATTACH01"}},"failed":{}}`)

	key, err := client.CreateAttachment("ITEM0001", "paper.pdf", "", "application/pdf", nil)

	if err != nil {
		t.Fatalf("CreateAttachment returned error: %v", err)
	}
	if key != "ATTACH01" {
		t.Errorf("expected key ATTACH01, got %s", key)
	}
	item := decodeNotePayload(t, rt.lastBody)
	if item["title"] != "paper.pdf" {
		t.Errorf("expected title to default to filename, got %v", item["title"])
	}
	if item["parentItem"] != "ITEM0001" {
		t.Errorf("expected parentItem ITEM0001, got %v", item["parentItem"])
	}
	if item["linkMode"] != "imported_file" {
		t.Errorf("expected linkMode imported_file, got %v", item["linkMode"])
	}
}

// Contract: without a parent key the attachment is created top-level —
// sending an empty parentItem field would make the API reject the item.
func TestCreateAttachmentTopLevelOmitsParent(t *testing.T) {
	client, rt := newRecordingClient(http.StatusOK, `{"successful":{"0":{"key":"ATTACH01"}},"failed":{}}`)

	_, err := client.CreateAttachment("", "paper.pdf", "My Paper", "application/pdf", nil)

	if err != nil {
		t.Fatalf("CreateAttachment returned error: %v", err)
	}
	item := decodeNotePayload(t, rt.lastBody)
	if _, exists := item["parentItem"]; exists {
		t.Errorf("expected parentItem to be omitted for top-level attachment, got %v", item["parentItem"])
	}
	if item["title"] != "My Paper" {
		t.Errorf("expected explicit title kept, got %v", item["title"])
	}
}

// Contract: upload step 1 — request authorization with the file's md5/size/
// mtime as form data; the returned uploadKey/URL drive the next two steps.
func TestGetUploadAuthorization(t *testing.T) {
	client, rt := newRecordingClient(http.StatusOK,
		`{"url":"https://upload.example.com","contentType":"multipart/form-data","prefix":"PRE","suffix":"SUF","uploadKey":"UPKEY123"}`)

	auth, err := client.GetUploadAuthorization("ATTACH01", "paper.pdf", 1024, "abc123", 1700000000)

	if err != nil {
		t.Fatalf("GetUploadAuthorization returned error: %v", err)
	}
	if auth.UploadKey != "UPKEY123" || auth.URL != "https://upload.example.com" {
		t.Errorf("unexpected authorization: %+v", auth)
	}
	form, err := url.ParseQuery(string(rt.lastBody))
	if err != nil {
		t.Fatalf("request body is not form-encoded: %v", err)
	}
	if form.Get("md5") != "abc123" || form.Get("filesize") != "1024" {
		t.Errorf("unexpected form data: %s", rt.lastBody)
	}
}

// Contract: upload step 2 — the file bytes are sent sandwiched between the
// prefix/suffix Zotero supplied (its S3 multipart framing); dropping either
// would corrupt the stored file.
func TestUploadFileContentWrapsWithPrefixAndSuffix(t *testing.T) {
	client, rt := newRecordingClient(http.StatusCreated, "")
	auth := &UploadAuthorization{
		URL:         "https://upload.example.com/file",
		ContentType: "multipart/form-data",
		Prefix:      "PRE-",
		Suffix:      "-SUF",
	}

	err := client.UploadFileContent(auth, []byte("FILEDATA"))

	if err != nil {
		t.Fatalf("UploadFileContent returned error: %v", err)
	}
	if string(rt.lastBody) != "PRE-FILEDATA-SUF" {
		t.Errorf("expected body wrapped with prefix/suffix, got %q", rt.lastBody)
	}
}

// Contract: a non-2xx upload response is an error including the status code,
// so a failed transfer is never registered as complete in step 3.
func TestUploadFileContentErrorStatus(t *testing.T) {
	client, _ := newRecordingClient(http.StatusForbidden, "denied")
	auth := &UploadAuthorization{URL: "https://upload.example.com/file"}

	err := client.UploadFileContent(auth, []byte("x"))

	if err == nil {
		t.Fatal("expected error for HTTP 403, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected status code in error, got %v", err)
	}
}

// Contract: upload step 3 — registering the uploadKey against the item's
// /file endpoint is what makes Zotero acknowledge the new file.
func TestRegisterUpload(t *testing.T) {
	client, rt := newRecordingClient(http.StatusNoContent, "")

	err := client.RegisterUpload("ATTACH01", "UPKEY123")

	if err != nil {
		t.Fatalf("RegisterUpload returned error: %v", err)
	}
	form, err := url.ParseQuery(string(rt.lastBody))
	if err != nil {
		t.Fatalf("request body is not form-encoded: %v", err)
	}
	if form.Get("upload") != "UPKEY123" {
		t.Errorf("expected upload=UPKEY123, got %s", rt.lastBody)
	}
	if rt.lastPath != "/users/12345/items/ATTACH01/file" {
		t.Errorf("expected file endpoint, got %s", rt.lastPath)
	}
}
