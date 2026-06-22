package lsp

import (
	"strings"
	"unicode"
)

// FormatHover keeps hover content bounded and renders markdown code fences as
// plain text. It intentionally supports only lightweight cleanup; rich markdown
// rendering belongs in the UI layer later.
func FormatHover(contents string, maxLines, maxWidth int) []string {
	contents = normalizeLineEndings(strings.TrimSpace(contents))
	if contents == "" || maxLines <= 0 {
		return nil
	}
	if maxWidth < 8 {
		maxWidth = 8
	}

	var lines []string
	inFence := false
	for _, raw := range strings.Split(contents, "\n") {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if !inFence {
			line = stripMarkdown(line)
		}
		for _, wrapped := range wrapPlain(line, maxWidth) {
			lines = append(lines, wrapped)
		}
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[len(lines)-1] = trimToWidth(lines[len(lines)-1], maxWidth-1) + "…"
	}
	return lines
}

func normalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

func stripMarkdown(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "> ")
	s = strings.TrimLeft(s, "#")
	s = strings.TrimSpace(s)
	replacer := strings.NewReplacer("**", "", "__", "", "`", "")
	return replacer.Replace(s)
}

func wrapPlain(s string, width int) []string {
	if s == "" {
		return []string{""}
	}
	var out []string
	rest := s
	for len([]rune(rest)) > width {
		cut := runeCut(rest, width)
		breakAt := lastSpaceBefore(rest[:cut])
		if breakAt <= 0 {
			breakAt = cut
		}
		out = append(out, strings.TrimRightFunc(rest[:breakAt], unicode.IsSpace))
		rest = strings.TrimLeftFunc(rest[breakAt:], unicode.IsSpace)
	}
	out = append(out, rest)
	return out
}

func runeCut(s string, width int) int {
	if width <= 0 {
		return 0
	}
	count := 0
	for i := range s {
		if count == width {
			return i
		}
		count++
	}
	return len(s)
}

func lastSpaceBefore(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if unicode.IsSpace(rune(s[i])) {
			return i
		}
	}
	return -1
}

func trimToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if len([]rune(s)) <= width {
		return s
	}
	return string([]rune(s)[:width])
}
