package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/yk0817/zotero-cli/resolve"
)

// Contract: an unknown identifier (resolve.ErrNotFound) surfaces as the CLI's
// NOT_FOUND code so the user is told the identifier is wrong, while any other
// resolver failure (transport/API) maps to API_ERROR — the two must not be
// conflated, or a network blip would read as "no such paper".
func TestResolveCLIError(t *testing.T) {
	notFound := resolveCLIError(fmt.Errorf("wrap: %w", resolve.ErrNotFound), "doi", "10.1/x")
	var cliNotFound *CLIError
	if !errors.As(notFound, &cliNotFound) || cliNotFound.Code != ErrCodeNotFound {
		t.Fatalf("expected NOT_FOUND CLIError, got %#v", notFound)
	}

	apiErr := resolveCLIError(errors.New("crossref 503"), "doi", "10.1/x")
	var cliAPI *CLIError
	if !errors.As(apiErr, &cliAPI) || cliAPI.Code != ErrCodeAPIError {
		t.Fatalf("expected API_ERROR CLIError, got %#v", apiErr)
	}
}
