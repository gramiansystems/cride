package diff

import (
	"fmt"
	"strings"

	godiff "github.com/sourcegraph/go-diff/diff"
)

const devNull = "/dev/null"

// ParseReview parses unified `git diff` output into per-file review diffs.
// Empty input yields a nil slice (no error).
func ParseReview(unified []byte) ([]FileDiff, error) {
	if strings.TrimSpace(string(unified)) == "" {
		return nil, nil
	}
	fds, err := godiff.ParseMultiFileDiff(unified)
	if err != nil {
		return nil, fmt.Errorf("parse diff: %w", err)
	}
	out := make([]FileDiff, 0, len(fds))
	for _, fd := range fds {
		out = append(out, convertFile(fd))
	}
	return out, nil
}

func convertFile(fd *godiff.FileDiff) FileDiff {
	f := FileDiff{
		OldPath: stripPrefix(fd.OrigName),
		NewPath: stripPrefix(fd.NewName),
	}
	f.Status = classify(fd, f.OldPath, f.NewPath)
	for _, e := range fd.Extended {
		if strings.HasPrefix(e, "Binary files") || strings.HasPrefix(e, "GIT binary patch") {
			f.Binary = true
		}
	}
	for _, h := range fd.Hunks {
		f.Hunks = append(f.Hunks, convertHunk(h, &f))
	}
	return f
}

func classify(fd *godiff.FileDiff, oldP, newP string) FileStatus {
	switch {
	case oldP == devNull || oldP == "":
		return FileAdded
	case newP == devNull || newP == "":
		return FileDeleted
	}
	for _, e := range fd.Extended {
		if strings.HasPrefix(e, "rename ") || strings.HasPrefix(e, "copy ") {
			return FileRenamed
		}
	}
	return FileModified
}

func convertHunk(h *godiff.Hunk, f *FileDiff) Hunk {
	hunk := Hunk{
		OldStart: int(h.OrigStartLine),
		OldLines: int(h.OrigLines),
		NewStart: int(h.NewStartLine),
		NewLines: int(h.NewLines),
		Header:   fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.OrigStartLine, h.OrigLines, h.NewStartLine, h.NewLines),
	}
	if sec := strings.TrimRight(h.Section, "\n"); sec != "" {
		hunk.Header += " " + sec
	}

	oldLn := int(h.OrigStartLine)
	newLn := int(h.NewStartLine)
	for _, raw := range splitLines(string(h.Body)) {
		if raw == "" {
			hunk.Lines = append(hunk.Lines, Line{Kind: LineContext, OldLine: oldLn, NewLine: newLn})
			oldLn++
			newLn++
			continue
		}
		switch raw[0] {
		case '+':
			hunk.Lines = append(hunk.Lines, Line{Kind: LineAdd, Content: raw[1:], NewLine: newLn})
			newLn++
			f.Added++
		case '-':
			hunk.Lines = append(hunk.Lines, Line{Kind: LineDelete, Content: raw[1:], OldLine: oldLn})
			oldLn++
			f.Deleted++
		case ' ':
			hunk.Lines = append(hunk.Lines, Line{Kind: LineContext, Content: raw[1:], OldLine: oldLn, NewLine: newLn})
			oldLn++
			newLn++
		case '\\':
			// "\ No newline at end of file" — metadata, not a content row.
		}
	}
	return hunk
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// stripPrefix removes git's a/ or b/ path prefix, leaving /dev/null intact.
func stripPrefix(name string) string {
	if name == "" || name == devNull {
		return name
	}
	if len(name) > 2 && (name[:2] == "a/" || name[:2] == "b/") {
		return name[2:]
	}
	return name
}
