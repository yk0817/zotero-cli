package semindex

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sampleIndex() *Index {
	return &Index{
		Header:  Header{SchemaVersion: SchemaVersion, LibraryID: "u1", Model: "m", Dim: 3, BuiltAt: time.Unix(0, 0).UTC()},
		Entries: []Entry{{ItemKey: "AAAA1111", Version: 5, Title: "T", Vector: []float32{1, 0, 0}}},
	}
}

// Contract: Save then Load round-trips the header and entries exactly. The index
// is the sole source of truth for semantic search; a lossy encode would silently
// change rankings or lose items.
func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "items.gob")
	idx := sampleIndex()

	if err := idx.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Header != idx.Header {
		t.Errorf("header = %+v, want %+v", got.Header, idx.Header)
	}
	if len(got.Entries) != 1 || got.Entries[0].ItemKey != "AAAA1111" || got.Entries[0].Version != 5 {
		t.Errorf("entries = %+v", got.Entries)
	}
}

// Contract: Load of a missing file returns ErrIndexNotFound (not a generic os
// error), so the CLI can tell "no index yet — run build" apart from a real IO
// failure and give the right suggestion.
func TestLoadMissingReturnsSentinel(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.gob"))

	if !errors.Is(err, ErrIndexNotFound) {
		t.Fatalf("want ErrIndexNotFound, got %v", err)
	}
}

// Contract: Save is atomic — it must not leave a .tmp file behind on success,
// so a crashed/failed build never yields a half-written index that Load would
// accept as valid.
func TestSaveLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.gob")

	if err := sampleIndex().Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp file still present: %v", err)
	}
}

// Contract: CompatibleWith rejects an index built for a different library or
// model, so search never ranks a query embedding against vectors from a
// different embedding space (which would return meaningless results).
func TestCompatibleWith(t *testing.T) {
	idx := sampleIndex()

	if err := idx.CompatibleWith("u1", "m"); err != nil {
		t.Errorf("matching header should pass, got %v", err)
	}
	if err := idx.CompatibleWith("u2", "m"); err == nil {
		t.Error("different library should fail")
	}
	if err := idx.CompatibleWith("u1", "other"); err == nil {
		t.Error("different model should fail")
	}
}
