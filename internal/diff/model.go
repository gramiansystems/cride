// Package diff models and parses the review diff (baseline → current). It is
// pure data + parsing; rendering lives in internal/ui. See DESIGN.md's "Review
// model" section.
package diff

// LineKind classifies a line within a hunk.
type LineKind int

const (
	LineContext LineKind = iota
	LineAdd
	LineDelete
)

// Line is one line of a hunk. OldLine/NewLine are 1-based line numbers in the
// old/new file respectively, or 0 when the line does not exist on that side.
type Line struct {
	Kind    LineKind
	Content string // text without the leading +/-/space or trailing newline
	OldLine int
	NewLine int
}

// Hunk is a contiguous changed region from a unified diff.
type Hunk struct {
	Header   string // reconstructed "@@ -a,b +c,d @@ section"
	OldStart int
	OldLines int
	NewStart int
	NewLines int
	Lines    []Line
}

// FileStatus describes how a file changed between baseline and current.
type FileStatus int

const (
	FileModified FileStatus = iota
	FileAdded
	FileDeleted
	FileRenamed
	// FileUnchanged is a current-side project file included for navigation,
	// rather than a member of the review diff.
	FileUnchanged
)

// FileDiff is the review diff for a single file.
type FileDiff struct {
	OldPath string
	NewPath string
	Status  FileStatus
	Hunks   []Hunk
	Binary  bool
	Added   int // added line count (for change-list badges)
	Deleted int // deleted line count
}

// Path returns the display path: the new path, falling back to the old one
// (e.g. for deletions).
func (f FileDiff) Path() string {
	if f.NewPath != "" && f.NewPath != "/dev/null" {
		return f.NewPath
	}
	return f.OldPath
}
