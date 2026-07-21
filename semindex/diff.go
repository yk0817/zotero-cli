package semindex

// ItemRef is the minimal library-item identity Diff needs: its key and current
// version. Keeping Diff on refs (not full items) keeps semindex decoupled from
// the zotero package.
type ItemRef struct {
	Key     string
	Version int
}

// Diff partitions current library items against the index:
//   - toEmbed: keys that are new, or whose Version changed (must be re-embedded)
//   - keep:    existing entries still present and unchanged (reused as-is)
//   - removed: index keys no longer in the library (dropped)
func (idx *Index) Diff(current []ItemRef) (toEmbed []string, keep []Entry, removed []string) {
	byKey := make(map[string]Entry, len(idx.Entries))
	for _, e := range idx.Entries {
		byKey[e.ItemKey] = e
	}
	seen := make(map[string]bool, len(current))
	for _, ref := range current {
		seen[ref.Key] = true
		if e, ok := byKey[ref.Key]; ok && e.Version == ref.Version {
			keep = append(keep, e)
		} else {
			toEmbed = append(toEmbed, ref.Key)
		}
	}
	for _, e := range idx.Entries {
		if !seen[e.ItemKey] {
			removed = append(removed, e.ItemKey)
		}
	}
	return toEmbed, keep, removed
}
