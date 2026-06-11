package zotero

import "strings"

// AIGeneratedTag marks notes created by AI frontends (CLI add-note, MCP
// zotero_add_note) so they are distinguishable from human-written notes.
const AIGeneratedTag = "ai-generated"

// NoteTags builds the tag list for an AI-created note: AIGeneratedTag first,
// followed by the extra tags with whitespace trimmed, empties and duplicate
// AIGeneratedTag entries removed.
func NoteTags(extra []string) []string {
	tags := []string{AIGeneratedTag}
	for _, t := range extra {
		t = strings.TrimSpace(t)
		if t != "" && !strings.EqualFold(t, AIGeneratedTag) {
			tags = append(tags, t)
		}
	}
	return tags
}
