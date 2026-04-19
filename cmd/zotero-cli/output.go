package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// outputFormat is set by the global --output flag.
var outputFormat string

// Error codes for structured error responses.
const (
	ErrCodeConfigNotFound  = "CONFIG_NOT_FOUND"
	ErrCodeConfigInvalid   = "CONFIG_INVALID"
	ErrCodeValidation      = "VALIDATION_ERROR"
	ErrCodeAPIError        = "API_ERROR"
	ErrCodeNotFound        = "NOT_FOUND"
	ErrCodeInvalidArgument = "INVALID_ARGUMENT"
	ErrCodeIOError         = "IO_ERROR"
)

// JSONResponse is the standard JSON envelope for all CLI output.
type JSONResponse struct {
	OK    bool       `json:"ok"`
	Data  any        `json:"data,omitempty"`
	Error *JSONError `json:"error,omitempty"`
}

// JSONError represents a structured error in JSON output.
type JSONError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

// CLIError is an error type that carries structured error information.
type CLIError struct {
	Code       string
	Message    string
	Suggestion string
}

func (e *CLIError) Error() string {
	return e.Message
}

// isJSON returns true if the output format is JSON.
func isJSON() bool {
	return outputFormat == "json"
}

// printJSON writes a successful JSON response to stdout.
func printJSON(data any) error {
	resp := JSONResponse{OK: true, Data: data}
	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

// printErrorJSON writes an error JSON response to stderr and returns the original error.
func printErrorJSON(code, msg, suggestion string) {
	resp := JSONResponse{
		OK: false,
		Error: &JSONError{
			Code:       code,
			Message:    msg,
			Suggestion: suggestion,
		},
	}
	out, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Fprintln(os.Stderr, string(out))
}

// classifyError converts a generic error into a CLIError with an appropriate code.
func classifyError(err error) *CLIError {
	if cliErr, ok := err.(*CLIError); ok {
		return cliErr
	}
	msg := err.Error()
	switch {
	case contains(msg, "config not found"):
		return &CLIError{Code: ErrCodeConfigNotFound, Message: msg, Suggestion: "Run 'zotero-cli config' to set up"}
	case contains(msg, "API key or user ID not set"):
		return &CLIError{Code: ErrCodeConfigInvalid, Message: msg, Suggestion: "Run 'zotero-cli config' to set up"}
	case contains(msg, "failed to read config"):
		return &CLIError{Code: ErrCodeConfigInvalid, Message: msg, Suggestion: "Check config file format"}
	default:
		return &CLIError{Code: ErrCodeAPIError, Message: msg}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
