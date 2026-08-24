package annotate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ExportName is the agent-facing Markdown review at the repository root.
const ExportName = "review.md"

// SaveMarkdown writes the canonical Markdown review atomically. Agents may read
// review.md while cride is still open, so they must never observe a partial
// rewrite.
func SaveMarkdown(path string, review Review) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ExportName+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	removeTemp := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o644); err != nil {
		removeTemp()
		return err
	}
	if _, err := tmp.Write(ExportMarkdown(review)); err != nil {
		removeTemp()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// ExportMarkdown renders review.md in cr's shape: grouped by file, sorted by
// line, snippets quoted, general comments in their own trailing section.
func ExportMarkdown(review Review) []byte {
	comments := make([]Comment, len(review.Comments))
	copy(comments, review.Comments)
	sortComments(comments)

	var b strings.Builder
	b.WriteString("# Review\n")
	if review.Baseline != "" {
		fmt.Fprintf(&b, "\nBaseline: %s\n", review.Baseline)
	}

	var general []Comment
	currentFile := ""
	for _, c := range comments {
		if c.Anchor == nil {
			general = append(general, c)
			continue
		}
		if c.Anchor.Path != currentFile {
			currentFile = c.Anchor.Path
			fmt.Fprintf(&b, "\n## %s\n", currentFile)
		}
		b.WriteString("\n")
		b.WriteString(anchoredHeading(c))
		if c.Snippet != "" {
			for _, line := range strings.Split(c.Snippet, "\n") {
				fmt.Fprintf(&b, "> %s\n", line)
			}
		}
		writeBody(&b, c.Body)
	}

	if len(general) > 0 {
		b.WriteString("\n## General Comments\n")
		for _, c := range general {
			b.WriteString("\n")
			fmt.Fprintf(&b, "- **[%s]**%s\n", c.Severity, resolvedSuffix(c))
			writeBody(&b, indentBody(c.Body))
		}
	}
	return []byte(b.String())
}

func anchoredHeading(c Comment) string {
	lines := fmt.Sprintf("L%d", c.Anchor.LineStart)
	if c.Anchor.LineEnd > c.Anchor.LineStart {
		lines = fmt.Sprintf("L%d-L%d", c.Anchor.LineStart, c.Anchor.LineEnd)
	}
	side := ""
	if c.Anchor.Side == SideBaseline {
		side = " (baseline)"
	}
	return fmt.Sprintf("### %s [%s]%s%s\n", lines, c.Severity, side, resolvedSuffix(c))
}

func resolvedSuffix(c Comment) string {
	switch c.Status {
	case StatusResolved:
		return " ✓ resolved"
	case StatusUnresolved:
		return " (detached)"
	default:
		return ""
	}
}

func writeBody(b *strings.Builder, body string) {
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return
	}
	b.WriteString(body)
	b.WriteString("\n")
}

func indentBody(body string) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	for i, line := range lines {
		lines[i] = "  " + line
	}
	return strings.Join(lines, "\n")
}

// sortComments orders by file, then line, then creation. General comments
// sort last so saves remain deterministic.
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
