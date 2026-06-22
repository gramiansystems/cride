// Package source contains source-coordinate types shared by navigation,
// search, diagnostics, and review annotations.
package source

// Location identifies a position in a repository-relative source file.
//
// Lines and columns are 1-based. Column is a byte column, not a rune or display
// cell column; convert only at boundaries that require a different convention.
type Location struct {
	Path   string
	Line   int
	Column int
}

// Range identifies a half-open span in source coordinates.
type Range struct {
	Start Location
	End   Location
}
