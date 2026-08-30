// Package diffsource defines the transport-oriented seam for obtaining a
// review diff. The local working tree is the first implementation; ssh/docker/PR
// sources slot in behind this same interface. See DESIGN.md's "Diff sources"
// section.
package diffsource

import (
	"errors"

	"cride/internal/search"
)

const (
	// MaxContentBytes caps on-demand file reads for full-file views and
	// navigation features. Larger files should be surfaced as too large instead
	// of being pulled into the TUI.
	MaxContentBytes = 2 * 1024 * 1024
)

var (
	ErrFileTooLarge = errors.New("file too large")
	ErrNotRegular   = errors.New("not a regular file")
)

// Fingerprinter is an optional Source capability: a cheap snapshot of the
// current side that changes whenever the review diff could have changed.
// Sources with an immutable current side return a constant.
type Fingerprinter interface {
	Fingerprint() (string, error)
}

// Watcher is an optional Source capability: onChange fires (debounced, from
// an arbitrary goroutine) whenever the current side may have changed. stop
// releases the watch. Sources that cannot watch return an error; callers
// fall back to polling the Fingerprinter.
type Watcher interface {
	Watch(onChange func()) (stop func(), err error)
}

// TextSearcher is the optional user-facing text-search capability. Unlike
// Source.Search, whose query is a regular expression used by lexical code
// intelligence, SearchText treats the query literally and applies smart-case
// matching (lowercase queries ignore case; uppercase queries match exactly).
// Sources that do not implement it fall back to an escaped Source.Search.
type TextSearcher interface {
	SearchText(query string) ([]search.Result, error)
}

// Source yields the review diff (baseline → current) for a repository.
type Source interface {
	// Diff returns the unified review diff for the whole change.
	Diff() ([]byte, error)
	// Baseline returns a short human identifier for the review baseline or
	// immutable comparison being reviewed.
	Baseline() string
	// Root returns the absolute repository root.
	Root() string
	// CurrentContent returns the current-side bytes for path.
	CurrentContent(path string) ([]byte, error)
	// BaselineContent returns the pinned baseline bytes for path.
	BaselineContent(path string) ([]byte, error)
	// ChangedPaths returns repository-relative paths changed against baseline.
	ChangedPaths() ([]string, error)
	// ProjectFiles returns repository-relative files visible on the current side
	// of the review.
	ProjectFiles() ([]string, error)
	// Search returns project text matches for a regular-expression query.
	Search(query string) ([]search.Result, error)
	// SearchWord returns project text matches for a whole word.
	SearchWord(word string) ([]search.Result, error)
}
