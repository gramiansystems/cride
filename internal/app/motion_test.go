package app

import "testing"

func TestNextWordStart(t *testing.T) {
	t.Parallel()

	cases := []struct {
		line  string
		col   int
		want  int
		found bool
	}{
		{"foo bar baz", 0, 4, true},
		{"foo bar baz", 4, 8, true},
		{"foo bar baz", 8, 0, false},
		{"foo.bar", 0, 3, true},  // punct run is its own word
		{"foo.bar", 3, 4, true},  // from the dot to the identifier
		{"  indent", 0, 2, true}, // leading space skips to the word
		{"", 0, 0, false},
		{"x", 0, 0, false},
		{"a := b", 1, 2, true}, // from the space onto :=
		{"a := b", 2, 5, true}, // := then b
	}
	for _, tc := range cases {
		got, found := nextWordStart([]rune(tc.line), tc.col)
		if found != tc.found || (found && got != tc.want) {
			t.Errorf("nextWordStart(%q, %d) = %d,%v want %d,%v", tc.line, tc.col, got, found, tc.want, tc.found)
		}
	}
}

func TestPrevWordStart(t *testing.T) {
	t.Parallel()

	cases := []struct {
		line  string
		col   int
		want  int
		found bool
	}{
		{"foo bar baz", 8, 4, true},
		{"foo bar baz", 4, 0, true},
		{"foo bar baz", 0, 0, false},
		{"foo bar baz", 6, 4, true}, // mid-word goes to its start
		{"foo.bar", 4, 3, true},     // from b to the dot run
		{"  x", 2, 0, false},
		{"", 0, 0, false},
	}
	for _, tc := range cases {
		got, found := prevWordStart([]rune(tc.line), tc.col)
		if found != tc.found || (found && got != tc.want) {
			t.Errorf("prevWordStart(%q, %d) = %d,%v want %d,%v", tc.line, tc.col, got, found, tc.want, tc.found)
		}
	}
}

func TestWordEndAfter(t *testing.T) {
	t.Parallel()

	cases := []struct {
		line  string
		col   int
		want  int
		found bool
	}{
		{"foo bar", 0, 2, true}, // inside foo to its end
		{"foo bar", 2, 6, true}, // from foo's end to bar's end
		{"foo bar", 6, 0, false},
		{"a.b", 0, 1, true}, // onto the dot
	}
	for _, tc := range cases {
		got, found := wordEndAfter([]rune(tc.line), tc.col)
		if found != tc.found || (found && got != tc.want) {
			t.Errorf("wordEndAfter(%q, %d) = %d,%v want %d,%v", tc.line, tc.col, got, found, tc.want, tc.found)
		}
	}
}

func TestFindCharOnLine(t *testing.T) {
	t.Parallel()

	line := []rune("abcabc")
	if got, ok := findCharOnLine(line, 0, 'c', 1, false); !ok || got != 2 {
		t.Errorf("f c from 0 = %d,%v want 2,true", got, ok)
	}
	if got, ok := findCharOnLine(line, 2, 'c', 1, false); !ok || got != 5 {
		t.Errorf("f c from 2 = %d,%v want 5,true (skips cursor)", got, ok)
	}
	if got, ok := findCharOnLine(line, 5, 'a', -1, false); !ok || got != 3 {
		t.Errorf("F a from 5 = %d,%v want 3,true", got, ok)
	}
	if got, ok := findCharOnLine(line, 0, 'c', 1, true); !ok || got != 1 {
		t.Errorf("t c from 0 = %d,%v want 1,true", got, ok)
	}
	// till landing equal to the cursor is skipped so ; progresses.
	if got, ok := findCharOnLine(line, 1, 'c', 1, true); !ok || got != 4 {
		t.Errorf("t c from 1 = %d,%v want 4,true", got, ok)
	}
	if _, ok := findCharOnLine(line, 0, 'z', 1, false); ok {
		t.Error("f z found a match on a line without z")
	}
}

func TestBracketHelpers(t *testing.T) {
	t.Parallel()

	if match, forward, ok := bracketFor('('); !ok || match != ')' || !forward {
		t.Errorf("bracketFor('(') = %c,%v,%v", match, forward, ok)
	}
	if match, forward, ok := bracketFor('}'); !ok || match != '{' || forward {
		t.Errorf("bracketFor('}') = %c,%v,%v", match, forward, ok)
	}
	if _, _, ok := bracketFor('x'); ok {
		t.Error("bracketFor('x') reported a bracket")
	}
	if got, ok := firstBracketFrom([]rune("ab(cd)"), 0); !ok || got != 2 {
		t.Errorf("firstBracketFrom = %d,%v want 2,true", got, ok)
	}
	if _, ok := firstBracketFrom([]rune("abcd"), 0); ok {
		t.Error("firstBracketFrom found a bracket in plain text")
	}
}

func TestRuneByteColumnConversions(t *testing.T) {
	t.Parallel()

	// "héllo" — é is two bytes.
	content := "héllo"
	if col, ok := byteColumnAtRune(content, 2); !ok || col != 4 {
		t.Errorf("byteColumnAtRune(2) = %d,%v want 4,true", col, ok)
	}
	if _, ok := byteColumnAtRune(content, 10); ok {
		t.Error("byteColumnAtRune past end reported ok")
	}
	if idx, ok := runeIndexAtByteColumn(content, 4); !ok || idx != 2 {
		t.Errorf("runeIndexAtByteColumn(4) = %d,%v want 2,true", idx, ok)
	}
	// Mid-rune byte column resolves to the covering rune.
	if idx, ok := runeIndexAtByteColumn(content, 3); !ok || idx != 1 {
		t.Errorf("runeIndexAtByteColumn(3) = %d,%v want 1,true", idx, ok)
	}
	if _, ok := runeIndexAtByteColumn(content, 100); ok {
		t.Error("runeIndexAtByteColumn past end reported ok")
	}
}
