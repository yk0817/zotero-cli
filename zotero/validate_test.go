package zotero

import "testing"

// Contract: an item key is exactly 8 alphanumeric characters. Keys are
// interpolated into API URL paths, so anything else (separators, spaces,
// control chars) must be rejected before a request is built — this validator
// is the shared guard for both the CLI and the MCP server.
func TestValidateItemKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{name: "valid uppercase alphanumeric", key: "ABCD1234", wantErr: false},
		{name: "valid lowercase accepted", key: "abcd1234", wantErr: false},
		{name: "too short", key: "ABC123", wantErr: true},
		{name: "too long", key: "ABCD12345", wantErr: true},
		{name: "empty", key: "", wantErr: true},
		{name: "contains symbol", key: "ABCD-234", wantErr: true},
		{name: "contains space", key: "ABCD 234", wantErr: true},
		{name: "contains control char", key: "ABCD\n234", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateItemKey(tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateItemKey(%q) error = %v, wantErr %v", tt.key, err, tt.wantErr)
			}
		})
	}
}
