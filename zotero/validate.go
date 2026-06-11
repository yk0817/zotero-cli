package zotero

import (
	"fmt"
	"unicode"
)

const itemKeyLength = 8

// ValidateItemKey checks that a key is exactly 8 alphanumeric characters.
func ValidateItemKey(key string) error {
	if len(key) != itemKeyLength {
		return fmt.Errorf("invalid item key %q: must be exactly %d characters", key, itemKeyLength)
	}
	for _, r := range key {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return fmt.Errorf("invalid item key %q: contains non-alphanumeric character", key)
		}
	}
	return nil
}
