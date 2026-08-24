package annotate

// review.md is the editable source of truth. Its visible structure keeps the
// fields a reviewer is likely to edit by hand. Parsing matches comments back
// to existing anchors so in-memory IDs and timestamps survive ordinary
// body/severity edits.

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	markdownAnchorHeading  = regexp.MustCompile(`^### L([1-9][0-9]*)(?:-L([1-9][0-9]*))? \[([^]]+)\](?: (\(baseline\)))?(?: (✓ resolved|\(detached\)))?$`)
	markdownGeneralHeading = regexp.MustCompile(`^- \*\*\[([^]]+)\]\*\*(?: (✓ resolved|\(detached\)))?$`)
)

// LoadMarkdown reads and parses an editable review.md export.
func LoadMarkdown(path string, existing Review) (Review, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Review{}, err
	}
	return ParseMarkdown(data, existing)
}

// ParseMarkdown imports the Markdown shape produced by ExportMarkdown.
// Prose outside a structured comment is retained as a general nit comment,
// making small hand-written additions useful without requiring exact syntax.
func ParseMarkdown(data []byte, existing Review) (Review, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	review := Review{
		Baseline: existing.Baseline,
		Comments: make([]Comment, 0),
	}
	section := ""
	loose := make([]string, 0)
	flushLoose := func() {
		body := trimMarkdownBody(loose)
		loose = loose[:0]
		if strings.TrimSpace(body) == "" {
			return
		}
		review.Comments = append(review.Comments, Comment{
			Body:     body,
			Severity: SeverityNit,
			Status:   StatusOpen,
		})
	}

	for i := 0; i < len(lines); {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			if len(loose) > 0 {
				loose = append(loose, "")
			}
			i++

		case section == "" && trimmed == "# Review":
			i++

		case section == "" && strings.HasPrefix(trimmed, "Baseline:"):
			review.Baseline = strings.TrimSpace(strings.TrimPrefix(trimmed, "Baseline:"))
			i++

		case strings.HasPrefix(line, "## "):
			flushLoose()
			section = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			i++

		case strings.HasPrefix(line, "### "):
			flushLoose()
			if section == "" || section == "General Comments" {
				return Review{}, fmt.Errorf("review.md line %d: anchored comment has no file section", i+1)
			}
			comment, err := parseAnchoredMarkdownHeading(line, section)
			if err != nil {
				return Review{}, fmt.Errorf("review.md line %d: %w", i+1, err)
			}
			i++
			var snippet []string
			for i < len(lines) && strings.HasPrefix(lines[i], ">") {
				quoted := strings.TrimPrefix(lines[i], ">")
				quoted = strings.TrimPrefix(quoted, " ")
				snippet = append(snippet, quoted)
				i++
			}
			bodyStart := i
			for i < len(lines) && !strings.HasPrefix(lines[i], "## ") && !strings.HasPrefix(lines[i], "### ") {
				i++
			}
			comment.Snippet = strings.Join(snippet, "\n")
			comment.Body = trimMarkdownBody(lines[bodyStart:i])
			review.Comments = append(review.Comments, comment)

		case section == "General Comments" && strings.HasPrefix(line, "- **["):
			flushLoose()
			comment, err := parseGeneralMarkdownHeading(line)
			if err != nil {
				return Review{}, fmt.Errorf("review.md line %d: %w", i+1, err)
			}
			i++
			bodyStart := i
			for i < len(lines) && !strings.HasPrefix(lines[i], "## ") && !strings.HasPrefix(lines[i], "- **[") {
				i++
			}
			bodyLines := append([]string(nil), lines[bodyStart:i]...)
			for j := range bodyLines {
				bodyLines[j] = strings.TrimPrefix(bodyLines[j], "  ")
			}
			comment.Body = trimMarkdownBody(bodyLines)
			review.Comments = append(review.Comments, comment)

		default:
			loose = append(loose, line)
			i++
		}
	}
	flushLoose()
	restoreMarkdownIdentity(review.Comments, existing.Comments)
	return review, nil
}

func parseAnchoredMarkdownHeading(heading, path string) (Comment, error) {
	match := markdownAnchorHeading.FindStringSubmatch(heading)
	if match == nil {
		return Comment{}, fmt.Errorf("invalid comment heading %q", heading)
	}
	start, _ := strconv.Atoi(match[1])
	end := start
	if match[2] != "" {
		end, _ = strconv.Atoi(match[2])
		if end < start {
			return Comment{}, fmt.Errorf("line range ends before it starts")
		}
	}
	severity, err := parseMarkdownSeverity(match[3])
	if err != nil {
		return Comment{}, err
	}
	side := SideCurrent
	if match[4] != "" {
		side = SideBaseline
	}
	return Comment{
		Severity: severity,
		Status:   parseMarkdownStatus(match[5]),
		Anchor: &Anchor{
			Path:      path,
			Side:      side,
			LineStart: start,
			LineEnd:   end,
		},
	}, nil
}

func parseGeneralMarkdownHeading(heading string) (Comment, error) {
	match := markdownGeneralHeading.FindStringSubmatch(heading)
	if match == nil {
		return Comment{}, fmt.Errorf("invalid general comment heading %q", heading)
	}
	severity, err := parseMarkdownSeverity(match[1])
	if err != nil {
		return Comment{}, err
	}
	return Comment{Severity: severity, Status: parseMarkdownStatus(match[2])}, nil
}

func parseMarkdownSeverity(value string) (Severity, error) {
	severity := Severity(value)
	switch severity {
	case SeverityNit, SeverityQuestion, SeverityMustFix:
		return severity, nil
	default:
		return "", fmt.Errorf("unknown severity %q", value)
	}
}

func parseMarkdownStatus(suffix string) Status {
	switch suffix {
	case "✓ resolved":
		return StatusResolved
	case "(detached)":
		return StatusUnresolved
	default:
		return StatusOpen
	}
}

func trimMarkdownBody(lines []string) string {
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

func restoreMarkdownIdentity(comments, existing []Comment) {
	candidates := append([]Comment(nil), existing...)
	sortComments(candidates)
	used := make([]bool, len(candidates))
	// Preserve exact body matches first so inserting a new general comment
	// before an existing one does not transfer the existing comment's ID.
	for i := range comments {
		for j := range candidates {
			if used[j] || comments[i].Body != candidates[j].Body || !sameMarkdownAnchor(comments[i].Anchor, candidates[j].Anchor) {
				continue
			}
			comments[i].ID = candidates[j].ID
			comments[i].Created = candidates[j].Created
			used[j] = true
			break
		}
	}
	for i := range comments {
		if comments[i].ID != "" {
			continue
		}
		for j := range candidates {
			if used[j] || !sameMarkdownAnchor(comments[i].Anchor, candidates[j].Anchor) {
				continue
			}
			comments[i].ID = candidates[j].ID
			comments[i].Created = candidates[j].Created
			used[j] = true
			break
		}
		if comments[i].ID == "" {
			comments[i].ID = NewID()
		}
		if comments[i].Created.IsZero() {
			comments[i].Created = time.Now()
		}
	}
}

func sameMarkdownAnchor(a, b *Anchor) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
