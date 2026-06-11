package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func assertCLIErrorCode(t *testing.T, err error, wantCode string) {
	t.Helper()
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T: %v", err, err)
	}
	if cliErr.Code != wantCode {
		t.Errorf("expected code %s, got %s", wantCode, cliErr.Code)
	}
}

// Contract: the CLI wrapper delegates to zotero.ValidateItemKey but converts
// failures into the CLIError envelope (code VALIDATION_ERROR + suggestion)
// that --output json consumers and the documented error-code table rely on.
func TestValidateItemKeyCLI(t *testing.T) {
	if err := validateItemKey("ABCD1234"); err != nil {
		t.Errorf("expected valid key to pass, got %v", err)
	}

	err := validateItemKey("bad")
	if err == nil {
		t.Fatal("expected error for invalid key, got nil")
	}
	assertCLIErrorCode(t, err, ErrCodeValidation)
}

// Contract: user-supplied strings (file paths, note bodies from flags) must
// not smuggle null bytes, path traversal, or control characters into API
// payloads or filesystem access; ordinary text including newlines/tabs passes.
func TestSanitizeInput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "plain text ok", input: "hello world", wantErr: false},
		{name: "newline and tab ok", input: "line1\n\tline2", wantErr: false},
		{name: "null byte rejected", input: "a\x00b", wantErr: true},
		{name: "path traversal rejected", input: "../etc/passwd", wantErr: true},
		{name: "control char rejected", input: "a\x07b", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeInput(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("sanitizeInput(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

// Contract: upload paths must point at an existing regular file — a missing
// path or a directory is rejected up front with VALIDATION_ERROR instead of
// failing later mid-upload with an opaque IO error.
func TestValidateFilePath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "exists.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := validateFilePath(file); err != nil {
		t.Errorf("expected existing file to pass, got %v", err)
	}

	err := validateFilePath(filepath.Join(dir, "missing.txt"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	assertCLIErrorCode(t, err, ErrCodeValidation)

	err = validateFilePath(dir)
	if err == nil {
		t.Fatal("expected error for directory path, got nil")
	}
	assertCLIErrorCode(t, err, ErrCodeValidation)
}
