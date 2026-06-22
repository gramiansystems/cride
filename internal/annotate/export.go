package annotate

import (
	"fmt"
	"strings"
)

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
