// Package semindex is a local, flat-file vector index over Zotero items: one
// float32 vector per item (built from title+abstract), stored as gob and
// searched by brute-force cosine. It is intentionally dependency-free and sized
// for a personal library (thousands of items), so no ANN/vector DB is needed.
package semindex

import (
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SchemaVersion is bumped when the on-disk layout changes incompatibly.
const SchemaVersion = 1

// ErrIndexNotFound is returned by Load when the index file does not exist, so
// callers can distinguish "not built yet" from a real IO error.
var ErrIndexNotFound = errors.New("semindex: index not found")

// Header binds an index file to the library and model it was built with. A
// mismatch on load means the vectors are from a different embedding space and
// must not be searched.
type Header struct {
	SchemaVersion int
	LibraryID     string
	Model         string
	Dim           int
	BuiltAt       time.Time
}

// Entry is one item's vector plus the minimum needed to rank, display, and
// incrementally re-embed it.
type Entry struct {
	ItemKey string
	Version int
	Title   string
	Vector  []float32
}

// Index is the full on-disk structure.
type Index struct {
	Header  Header
	Entries []Entry
}

// Load reads and gob-decodes the index at path. A missing file yields
// ErrIndexNotFound.
func Load(path string) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrIndexNotFound
		}
		return nil, err
	}
	defer f.Close()

	var idx Index
	if err := gob.NewDecoder(f).Decode(&idx); err != nil {
		return nil, fmt.Errorf("semindex: decode %s: %w", path, err)
	}
	return &idx, nil
}

// Save gob-encodes the index to a temp file and renames it over path, so a
// failed encode never replaces a good index with a partial one.
func (idx *Index) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(f).Encode(idx); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("semindex: encode: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// CompatibleWith returns a descriptive error when the on-disk header does not
// match the active library or embedding model.
func (idx *Index) CompatibleWith(libraryID, model string) error {
	if idx.Header.LibraryID != libraryID {
		return fmt.Errorf("index was built for library %q, current is %q; run 'index build'", idx.Header.LibraryID, libraryID)
	}
	if idx.Header.Model != model {
		return fmt.Errorf("index was built with model %q, current is %q; run 'index build'", idx.Header.Model, model)
	}
	return nil
}
