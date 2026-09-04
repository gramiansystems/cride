// Package terminal contains narrowly-scoped terminal capability setup that is
// not provided by the Bubble Tea version used by cride.
package terminal

import (
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

const (
	kittyShift = 1 << iota
	kittyAlt
	kittyCtrl
	kittySuper
	kittyHyper
	kittyMeta
	kittyCapsLock
	kittyNumLock
)

// EnableKeyboardEnhancements asks terminals implementing the Kitty keyboard
// protocol to report standalone modifier presses. The returned reader
// downgrades the enhanced key encodings to the legacy sequences understood by
// Bubble Tea v1, leaving Shift events intact for the app to recognize.
//
// Unsupported terminals ignore the control sequence and continue to work
// through the pass-through reader. The restore function is idempotent.
func EnableKeyboardEnhancements(input, output *os.File) (io.Reader, func()) {
	if input == nil || output == nil || !term.IsTerminal(input.Fd()) || !term.IsTerminal(output.Fd()) {
		return nil, func() {}
	}
	flags := ansi.KittyReportEventTypes | ansi.KittyReportAllKeysAsEscapeCodes | ansi.KittyReportAssociatedKeys
	if _, err := io.WriteString(output, ansi.PushKittyKeyboard(flags)); err != nil {
		return nil, func() {}
	}

	var once sync.Once
	restore := func() {
		once.Do(func() {
			_, _ = io.WriteString(output, ansi.PopKittyKeyboard(1))
		})
	}
	return &keyboardReader{file: input}, restore
}

// keyboardReader retains the terminal file methods so Bubble Tea can still
// put stdin in raw mode and its cancelable input reader can poll the same fd.
type keyboardReader struct {
	file    *os.File
	pending []byte
	err     error
}

func (r *keyboardReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		if len(r.pending) > 0 {
			n := copy(p, r.pending)
			r.pending = r.pending[n:]
			return n, nil
		}
		if r.err != nil {
			err := r.err
			r.err = nil
			return 0, err
		}

		buf := make([]byte, max(256, len(p)))
		n, err := r.file.Read(buf)
		translated := downgradeKeyboardInput(buf[:n])
		if len(translated) > 0 {
			written := copy(p, translated)
			r.pending = append(r.pending, translated[written:]...)
			r.err = err
			return written, nil
		}
		if err != nil {
			return 0, err
		}
	}
}

func (r *keyboardReader) Write(p []byte) (int, error) { return r.file.Write(p) }
func (r *keyboardReader) Close() error                { return nil }
func (r *keyboardReader) Fd() uintptr                 { return r.file.Fd() }
func (r *keyboardReader) Name() string                { return r.file.Name() }

func downgradeKeyboardInput(input []byte) []byte {
	out := make([]byte, 0, len(input))
	for i := 0; i < len(input); {
		if input[i] != '\x1b' || i+1 >= len(input) || input[i+1] != '[' {
			out = append(out, input[i])
			i++
			continue
		}

		end := -1
		for j := i + 2; j < len(input); j++ {
			if input[j] >= 0x40 && input[j] <= 0x7e {
				end = j + 1
				break
			}
		}
		if end < 0 {
			out = append(out, input[i:]...)
			break
		}

		sequence := input[i:end]
		if replacement, handled := downgradeCSI(sequence); handled {
			out = append(out, replacement...)
		} else {
			out = append(out, sequence...)
		}
		i = end
	}
	return out
}

func downgradeCSI(sequence []byte) ([]byte, bool) {
	if len(sequence) < 4 || sequence[0] != '\x1b' || sequence[1] != '[' {
		return nil, false
	}
	final := sequence[len(sequence)-1]
	if final == 'u' {
		return downgradeKittyKey(sequence)
	}
	return downgradeLegacyKeyEvent(sequence)
}

func downgradeKittyKey(sequence []byte) ([]byte, bool) {
	body := string(sequence[2 : len(sequence)-1])
	params := strings.Split(body, ";")
	if len(params) == 0 {
		return nil, false
	}
	codeParts := strings.Split(params[0], ":")
	code, err := strconv.Atoi(codeParts[0])
	if err != nil {
		return nil, false
	}

	modifier, eventType := 1, 1
	if len(params) > 1 && params[1] != "" {
		modifierParts := strings.Split(params[1], ":")
		if modifierParts[0] != "" {
			modifier, err = strconv.Atoi(modifierParts[0])
			if err != nil {
				return nil, false
			}
		}
		if len(modifierParts) > 1 && modifierParts[1] != "" {
			eventType, err = strconv.Atoi(modifierParts[1])
			if err != nil {
				return nil, false
			}
		}
	}

	// Preserve only distinct Shift presses. The app consumes this private-CSI
	// Bubble Tea message; repeats and releases must not look like a second tap.
	if code == 57441 || code == 57447 {
		if eventType == 1 {
			return sequence, true
		}
		return nil, true
	}
	if code >= 57442 && code <= 57454 {
		return nil, true // other standalone modifiers
	}
	if eventType == 3 {
		return nil, true
	}

	modifierMask := max(0, modifier-1)
	if legacy, ok := legacySpecialKey(code, modifierMask); ok {
		return legacy, true
	}

	text := kittyAssociatedText(params)
	if modifierMask&kittyCtrl != 0 {
		if control, ok := legacyControlByte(rune(code)); ok {
			if modifierMask&kittyAlt != 0 {
				return []byte{'\x1b', control}, true
			}
			return []byte{control}, true
		}
	}
	if text == "" && utf8.ValidRune(rune(code)) && unicode.IsPrint(rune(code)) {
		r := rune(code)
		if modifierMask&(kittyShift|kittyCapsLock) != 0 {
			r = unicode.ToUpper(r)
		}
		text = string(r)
	}
	if text == "" {
		return nil, true
	}
	if modifierMask&kittyAlt != 0 {
		return append([]byte{'\x1b'}, []byte(text)...), true
	}
	return []byte(text), true
}

func kittyAssociatedText(params []string) string {
	if len(params) < 3 || params[2] == "" {
		return ""
	}
	var b strings.Builder
	for _, value := range strings.Split(params[2], ":") {
		codepoint, err := strconv.Atoi(value)
		if err != nil || !utf8.ValidRune(rune(codepoint)) {
			return ""
		}
		b.WriteRune(rune(codepoint))
	}
	return b.String()
}

func legacyControlByte(r rune) (byte, bool) {
	r = unicode.ToLower(r)
	switch {
	case r >= 'a' && r <= 'z':
		return byte(r - 'a' + 1), true
	case r == ' ' || r == '2' || r == '@':
		return 0, true
	case r == '3' || r == '[':
		return 27, true
	case r == '4' || r == '\\':
		return 28, true
	case r == '5' || r == ']':
		return 29, true
	case r == '6' || r == '^' || r == '~':
		return 30, true
	case r == '7' || r == '/' || r == '_':
		return 31, true
	case r == '8' || r == '?':
		return 127, true
	default:
		return 0, false
	}
}

func legacySpecialKey(code, modifierMask int) ([]byte, bool) {
	modifier := modifierMask&7 + 1 // legacy xterm supports shift/alt/ctrl
	switch code {
	case 27, 57344:
		return withLegacyAlt([]byte{'\x1b'}, modifierMask), true
	case 13, 57345, 57414:
		return withLegacyAlt([]byte{'\r'}, modifierMask), true
	case 9, 57346:
		if modifierMask&kittyShift != 0 {
			return []byte("\x1b[Z"), true
		}
		return withLegacyAlt([]byte{'\t'}, modifierMask), true
	case 127, 57347:
		return withLegacyAlt([]byte{127}, modifierMask), true
	case 57348, 57425:
		return legacyTildeKey(2, modifier), true
	case 57349, 57426:
		return legacyTildeKey(3, modifier), true
	case 57350, 57417:
		return legacyLetterKey('D', modifier), true
	case 57351, 57418:
		return legacyLetterKey('C', modifier), true
	case 57352, 57419:
		return legacyLetterKey('A', modifier), true
	case 57353, 57420:
		return legacyLetterKey('B', modifier), true
	case 57354, 57421:
		return legacyTildeKey(5, modifier), true
	case 57355, 57422:
		return legacyTildeKey(6, modifier), true
	case 57356, 57423:
		return legacyLetterKey('H', modifier), true
	case 57357, 57424:
		return legacyLetterKey('F', modifier), true
	case 57364, 57365, 57366, 57367:
		final := byte('P' + code - 57364)
		if modifier == 1 {
			return []byte{'\x1b', 'O', final}, true
		}
		return []byte("\x1b[1;" + strconv.Itoa(modifier) + string(final)), true
	case 57368, 57369, 57370, 57371, 57372, 57373, 57374, 57375:
		numbers := [...]int{15, 17, 18, 19, 20, 21, 23, 24}
		return legacyTildeKey(numbers[code-57368], modifier), true
	case 57399, 57400, 57401, 57402, 57403, 57404, 57405, 57406, 57407, 57408:
		return []byte{byte('0' + code - 57399)}, true
	case 57409:
		return []byte{'.'}, true
	case 57410:
		return []byte{'/'}, true
	case 57411:
		return []byte{'*'}, true
	case 57412:
		return []byte{'-'}, true
	case 57413:
		return []byte{'+'}, true
	case 57415:
		return []byte{'='}, true
	case 57416:
		return []byte{','}, true
	default:
		return nil, false
	}
}

func withLegacyAlt(key []byte, modifierMask int) []byte {
	if modifierMask&kittyAlt == 0 {
		return key
	}
	return append([]byte{'\x1b'}, key...)
}

func legacyLetterKey(final byte, modifier int) []byte {
	if modifier == 1 {
		return []byte{'\x1b', '[', final}
	}
	return []byte("\x1b[1;" + strconv.Itoa(modifier) + string(final))
}

func legacyTildeKey(number, modifier int) []byte {
	if modifier == 1 {
		return []byte("\x1b[" + strconv.Itoa(number) + "~")
	}
	return []byte("\x1b[" + strconv.Itoa(number) + ";" + strconv.Itoa(modifier) + "~")
}

// Some functional keys retain their legacy CSI final while adding an event
// type sub-parameter. Remove it for presses/repeats and discard releases.
func downgradeLegacyKeyEvent(sequence []byte) ([]byte, bool) {
	body := string(sequence[2 : len(sequence)-1])
	parts := strings.Split(body, ";")
	if len(parts) < 2 {
		return nil, false
	}
	last := strings.Split(parts[len(parts)-1], ":")
	if len(last) != 2 {
		return nil, false
	}
	eventType, err := strconv.Atoi(last[1])
	if err != nil || eventType < 1 || eventType > 3 {
		return nil, false
	}
	if eventType == 3 {
		return nil, true
	}
	parts[len(parts)-1] = last[0]
	final := sequence[len(sequence)-1]
	if last[0] == "1" && len(parts) == 2 {
		switch final {
		case 'P', 'Q', 'R', 'S':
			return []byte{'\x1b', 'O', final}, true
		case 'A', 'B', 'C', 'D', 'H', 'F':
			return []byte{'\x1b', '[', final}, true
		case '~':
			return []byte("\x1b[" + parts[0] + "~"), true
		}
	}
	return []byte("\x1b[" + strings.Join(parts, ";") + string(final)), true
}
