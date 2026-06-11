package main

import (
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/yk0817/zotero-cli/zotero"
)

// validateItemKey wraps zotero.ValidateItemKey with a CLI error envelope.
func validateItemKey(key string) error {
	if err := zotero.ValidateItemKey(key); err != nil {
		return &CLIError{
			Code:       ErrCodeValidation,
			Message:    err.Error(),
			Suggestion: "Item keys are 8 alphanumeric characters, e.g. ABCD1234",
		}
	}
	return nil
}

// validateFilePath checks that a file path is valid and the file exists.
func validateFilePath(path string) error {
	if err := sanitizeInput(path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &CLIError{
				Code:       ErrCodeValidation,
				Message:    fmt.Sprintf("file not found: %s", path),
				Suggestion: "Check the file path and try again",
			}
		}
		return &CLIError{
			Code:    ErrCodeIOError,
			Message: fmt.Sprintf("cannot access file: %v", err),
		}
	}
	if info.IsDir() {
		return &CLIError{
			Code:       ErrCodeValidation,
			Message:    fmt.Sprintf("path is a directory, not a file: %s", path),
			Suggestion: "Specify a file path, not a directory",
		}
	}
	return nil
}

// sanitizeInput checks for dangerous patterns in user input.
func sanitizeInput(s string) error {
	if strings.Contains(s, "\x00") {
		return &CLIError{
			Code:    ErrCodeValidation,
			Message: "input contains null bytes",
		}
	}
	if strings.Contains(s, "../") {
		return &CLIError{
			Code:    ErrCodeValidation,
			Message: "input contains path traversal sequence",
		}
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return &CLIError{
				Code:    ErrCodeValidation,
				Message: "input contains control characters",
			}
		}
	}
	return nil
}
