// Command gen regenerates docs/keymap.md from keymap.Groups. It runs from
// the internal/keymap package directory via go:generate (or `make docs`).
package main

import (
	"fmt"
	"os"

	"cride/internal/keymap"
)

func main() {
	const out = "../../docs/keymap.md"
	if err := os.WriteFile(out, []byte(keymap.Markdown()), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "gen: %v\n", err)
		os.Exit(1)
	}
}
