package annotate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// StoreName is the review file at the repo root, agent-findable by design.
const StoreName = ".crreview"

// DefaultPath returns the .crreview location for a repo root.
func DefaultPath(root string) string {
	return filepath.Join(root, StoreName)
}

// Load reads a review file. A missing file yields an empty review; a corrupt
// or future-versioned file returns an error so the caller can surface it
// without discarding the bytes on disk.
func Load(path string) (Review, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Review{FormatVersion: FormatVersion}, nil
	}
	if err != nil {
		return Review{FormatVersion: FormatVersion}, err
	}
	var review Review
	if err := json.Unmarshal(data, &review); err != nil {
		return Review{FormatVersion: FormatVersion}, fmt.Errorf("parse %s: %w", path, err)
	}
	if review.FormatVersion > FormatVersion {
		return Review{FormatVersion: FormatVersion}, fmt.Errorf("%s: format version %d is newer than this cride", path, review.FormatVersion)
	}
	if review.FormatVersion == 0 {
		review.FormatVersion = FormatVersion
	}
	return review, nil
}

// Save writes the review atomically (temp + rename) so a crash mid-write
// never corrupts the store.
func Save(path string, review Review) error {
	review.FormatVersion = FormatVersion
	sortComments(review.Comments)
	data, err := json.MarshalIndent(review, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), StoreName+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// sortComments orders by file, then line, then creation, keeping the store
// and export deterministic. General comments sort last.
func sortComments(comments []Comment) {
	sort.SliceStable(comments, func(i, j int) bool {
		a, b := comments[i], comments[j]
		switch {
		case a.Anchor == nil && b.Anchor == nil:
			return a.Created.Before(b.Created)
		case a.Anchor == nil:
			return false
		case b.Anchor == nil:
			return true
		}
		if a.Anchor.Path != b.Anchor.Path {
			return a.Anchor.Path < b.Anchor.Path
		}
		if a.Anchor.LineStart != b.Anchor.LineStart {
			return a.Anchor.LineStart < b.Anchor.LineStart
		}
		return a.Created.Before(b.Created)
	})
}
