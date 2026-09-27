// Package blame loads immutable, baseline-side git blame metadata.
package blame

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// LineInfo describes the commit that last touched one baseline line.
type LineInfo struct {
	SHA        string
	Author     string
	AuthorTime time.Time
	Summary    string
}

// FileBlame is indexed by one-based source line (Lines[0] is source line 1).
type FileBlame struct {
	Path  string
	Lines []LineInfo
}

// Load runs blame against an immutable baseline ref. Whitespace-only changes
// and moves within the file are ignored so the gutter reflects meaningful
// authorship rather than formatting churn.
func Load(repoRoot, ref, path string) (FileBlame, error) {
	if strings.TrimSpace(repoRoot) == "" {
		return FileBlame{}, fmt.Errorf("blame: repository root is empty")
	}
	if strings.TrimSpace(ref) == "" {
		return FileBlame{}, fmt.Errorf("blame: baseline ref is empty")
	}
	if strings.TrimSpace(path) == "" {
		return FileBlame{}, fmt.Errorf("blame: path is empty")
	}

	cmd := exec.Command("git", "-C", repoRoot, "blame", "--porcelain", "-w", "-M", ref, "--", path)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			message := strings.TrimSpace(string(exitErr.Stderr))
			if message != "" {
				return FileBlame{}, fmt.Errorf("git blame %s: %s", path, message)
			}
		}
		return FileBlame{}, fmt.Errorf("git blame %s: %w", path, err)
	}
	return Parse(bytes.NewReader(out), path)
}

// Parse reads git blame --porcelain output. Git emits commit metadata only on
// the first occurrence of a SHA, so cached headers are carried into compacted
// subsequent records.
func Parse(r io.Reader, path string) (FileBlame, error) {
	type header struct {
		info LineInfo
	}

	scanner := bufio.NewScanner(r)
	// A source line can be large even though the file as a whole is bounded.
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	cache := make(map[string]header)
	result := FileBlame{Path: path}

	var current LineInfo
	currentLine := 0
	haveRecord := false
	for scanner.Scan() {
		line := scanner.Text()
		if sha, finalLine, ok := parseRecordHeader(line); ok {
			current = cache[sha].info
			current.SHA = sha
			currentLine = finalLine
			haveRecord = true
			continue
		}
		if !haveRecord {
			continue
		}

		switch {
		case strings.HasPrefix(line, "author "):
			current.Author = strings.TrimPrefix(line, "author ")
		case strings.HasPrefix(line, "author-time "):
			seconds, err := strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64)
			if err != nil {
				return FileBlame{}, fmt.Errorf("parse blame author-time for line %d: %w", currentLine, err)
			}
			current.AuthorTime = time.Unix(seconds, 0)
		case strings.HasPrefix(line, "summary "):
			current.Summary = strings.TrimPrefix(line, "summary ")
		case strings.HasPrefix(line, "\t"):
			if currentLine < 1 {
				return FileBlame{}, fmt.Errorf("parse blame: invalid final line %d", currentLine)
			}
			if len(result.Lines) < currentLine {
				result.Lines = append(result.Lines, make([]LineInfo, currentLine-len(result.Lines))...)
			}
			result.Lines[currentLine-1] = current
			cache[current.SHA] = header{info: current}
			haveRecord = false
		}
	}
	if err := scanner.Err(); err != nil {
		return FileBlame{}, fmt.Errorf("parse blame: %w", err)
	}
	if haveRecord {
		return FileBlame{}, fmt.Errorf("parse blame: truncated record for line %d", currentLine)
	}
	return result, nil
}

func parseRecordHeader(line string) (sha string, finalLine int, ok bool) {
	fields := strings.Fields(line)
	if len(fields) != 3 && len(fields) != 4 {
		return "", 0, false
	}
	sha = strings.TrimPrefix(fields[0], "^")
	if len(sha) < 7 || !isHex(sha) {
		return "", 0, false
	}
	if _, err := strconv.Atoi(fields[1]); err != nil {
		return "", 0, false
	}
	finalLine, err := strconv.Atoi(fields[2])
	if err != nil || finalLine < 1 {
		return "", 0, false
	}
	return sha, finalLine, true
}

func isHex(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') && !(r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// CompactAge formats a stable, narrow age label for the blame margin.
func CompactAge(now, then time.Time) string {
	if then.IsZero() {
		return ""
	}
	d := now.Sub(then)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d < 24*30*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d/(30*24*time.Hour)))
	default:
		return fmt.Sprintf("%dy", int(d/(365*24*time.Hour)))
	}
}
