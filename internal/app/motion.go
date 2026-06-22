package app

// Vim-style character motions over a single line's content. All positions
// are rune indices; display columns are only computed at render time
// (cursorSpan). Row-crossing motion logic lives in cursor.go — these helpers
// are pure so they can be tested without a Model.

import "unicode"

type charClass int

const (
	classSpace charClass = iota
	classWord
	classPunct
)

func classOf(r rune) charClass {
	switch {
	case unicode.IsSpace(r):
		return classSpace
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return classWord
	default:
		return classPunct
	}
}

// nextWordStart returns the rune index of the first word start strictly after
// col, vim-w style: a word is a run of word runes or a run of punctuation.
func nextWordStart(runes []rune, col int) (int, bool) {
	n := len(runes)
	if n == 0 || col >= n {
		return 0, false
	}
	i := max(col, 0)
	if start := classOf(runes[i]); start != classSpace {
		for i < n && classOf(runes[i]) == start {
			i++
		}
	}
	for i < n && classOf(runes[i]) == classSpace {
		i++
	}
	if i < n && i > col {
		return i, true
	}
	return 0, false
}

// prevWordStart returns the rune index of the word start at or before col-1:
// the start of the previous word, or of the current one when the cursor sits
// past its first rune (vim b).
func prevWordStart(runes []rune, col int) (int, bool) {
	i := min(col, len(runes)) - 1
	for i >= 0 && classOf(runes[i]) == classSpace {
		i--
	}
	if i < 0 {
		return 0, false
	}
	cls := classOf(runes[i])
	for i >= 0 && classOf(runes[i]) == cls {
		i--
	}
	return i + 1, true
}

// wordEndAfter returns the rune index of the next word end strictly after col
// (vim e).
func wordEndAfter(runes []rune, col int) (int, bool) {
	n := len(runes)
	i := col + 1
	for i < n && classOf(runes[i]) == classSpace {
		i++
	}
	if i >= n {
		return 0, false
	}
	cls := classOf(runes[i])
	for i+1 < n && classOf(runes[i+1]) == cls {
		i++
	}
	return i, true
}

// firstNonBlank returns the rune index of the first non-space rune, or 0 when
// the line is blank or empty.
func firstNonBlank(runes []rune) int {
	for i, r := range runes {
		if !unicode.IsSpace(r) {
			return i
		}
	}
	return 0
}

// findCharOnLine locates target from col in dir (+1/-1), never matching the
// cursor position itself. till lands one rune short of the target (t/T); a
// landing that would not move the cursor is skipped so ;/, always progress.
func findCharOnLine(runes []rune, col int, target rune, dir int, till bool) (int, bool) {
	for i := col + dir; i >= 0 && i < len(runes); i += dir {
		if runes[i] != target {
			continue
		}
		landing := i
		if till {
			landing = i - dir
		}
		if landing == col || landing < 0 || landing >= len(runes) {
			continue
		}
		return landing, true
	}
	return 0, false
}

var bracketPairs = [...]struct{ open, close rune }{
	{'(', ')'},
	{'[', ']'},
	{'{', '}'},
}

// bracketFor returns the partner of a bracket rune and whether the partner
// lies forward of it (i.e. r is an opening bracket).
func bracketFor(r rune) (match rune, forward bool, ok bool) {
	for _, p := range bracketPairs {
		if r == p.open {
			return p.close, true, true
		}
		if r == p.close {
			return p.open, false, true
		}
	}
	return 0, false, false
}

// firstBracketFrom scans forward from col for the first bracket rune on the
// line, matching vim %'s "look ahead on the current line" behavior.
func firstBracketFrom(runes []rune, col int) (int, bool) {
	for i := max(col, 0); i < len(runes); i++ {
		if _, _, ok := bracketFor(runes[i]); ok {
			return i, true
		}
	}
	return 0, false
}
