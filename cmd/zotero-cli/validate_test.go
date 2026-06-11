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
