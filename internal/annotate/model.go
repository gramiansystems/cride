// Package annotate holds review comments: the thread model (minus replies),
// the .crreview store, and the review.md export. See DESIGN.md's "Review
// annotations" section; the
// current v0 uses plain line anchors, with fingerprint remapping still planned.
package annotate

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// FormatVersion is written from the first byte so the v1 fingerprint fields
// can slot in without a format break.
const FormatVersion = 1

type Severity string

const (
	SeverityNit      Severity = "nit"
	SeverityQuestion Severity = "question"
	SeverityMustFix  Severity = "must-fix"
)

// NextSeverity cycles the composer's severity key.
func NextSeverity(s Severity) Severity {
	switch s {
	case SeverityNit:
		return SeverityQuestion
	case SeverityQuestion:
		return SeverityMustFix
	default:
		return SeverityNit
	}
}

type Status string

const (
	StatusOpen     Status = "open"
	StatusResolved Status = "resolved"
	// StatusUnresolved marks detached comments whose anchor no longer
	// matches the tree. They are never discarded.
	StatusUnresolved Status = "unresolved"
)

type Side string

const (
	SideCurrent  Side = "current"
	SideBaseline Side = "baseline"
)

// Anchor pins a comment to a line range. v0 anchors are plain line ranges;
// content fingerprints (v1) extend this struct without a format break.
type Anchor struct {
	Path      string `json:"path"`
	Side      Side   `json:"side"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
}

// Comment is one review annotation. A nil Anchor is a general comment.
type Comment struct {
	ID       string    `json:"id"`
	Body     string    `json:"body"`
	Severity Severity  `json:"severity"`
	Created  time.Time `json:"created"`
	Anchor   *Anchor   `json:"anchor,omitempty"`
	Status   Status    `json:"status"`
	// Snippet quotes the anchored code at comment time, both for the export
	// and to detect drift until real fingerprints land.
	Snippet string `json:"snippet,omitempty"`
}

// Review is the .crreview document.
type Review struct {
	FormatVersion int       `json:"format_version"`
	Baseline      string    `json:"baseline,omitempty"`
	Comments      []Comment `json:"comments"`
}

// NewID returns a random comment id.
func NewID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b[:])
}

// Resolved reports whether the comment is settled.
func (c Comment) Resolved() bool { return c.Status == StatusResolved }
