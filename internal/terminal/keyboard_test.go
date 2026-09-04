package terminal

import "testing"

func TestDowngradeKeyboardInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain text", input: "\x1b[97;1:1;97u", want: "a"},
		{name: "shifted text", input: "\x1b[97;2:1;65u", want: "A"},
		{name: "unicode text", input: "\x1b[20320;1:1;20320u", want: "你"},
		{name: "ctrl shortcut", input: "\x1b[112;5:1u", want: "\x10"},
		{name: "arrow", input: "\x1b[57352;1:1u", want: "\x1b[A"},
		{name: "shift tab", input: "\x1b[9;2:1u", want: "\x1b[Z"},
		{name: "key release", input: "\x1b[97;1:3;97u", want: ""},
		{name: "key repeat acts as press", input: "\x1b[98;1:2;98u", want: "b"},
		{name: "shift press preserved", input: "\x1b[57441;2:1u", want: "\x1b[57441;2:1u"},
		{name: "shift repeat discarded", input: "\x1b[57441;2:2u", want: ""},
		{name: "shift release discarded", input: "\x1b[57441;1:3u", want: ""},
		{name: "legacy arrow press", input: "\x1b[1;1:1A", want: "\x1b[A"},
		{name: "legacy arrow release", input: "\x1b[1;1:3A", want: ""},
		{name: "bracketed paste and text pass through", input: "\x1b[200~some text\x1b[201~", want: "\x1b[200~some text\x1b[201~"},
		{name: "mouse passes through", input: "\x1b[<0;12;7M", want: "\x1b[<0;12;7M"},
		{name: "mixed events", input: "before\x1b[120;1:1;120u\x1b[120;1:3;120uafter", want: "beforexafter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := string(downgradeKeyboardInput([]byte(tt.input))); got != tt.want {
				t.Fatalf("downgradeKeyboardInput(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
