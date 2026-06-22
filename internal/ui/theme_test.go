package ui

import (
	"strings"
	"testing"

	"cride/internal/diff"
)

// Not parallel: SetTheme mutates package styles shared with other tests.
func TestThemeSwitchLeaksNoForeignPalette(t *testing.T) {
	defer SetTheme(DarkTheme())

	files := []diff.FileDiff{{
		NewPath: "a.go",
		Status:  diff.FileModified,
		Added:   1,
		Hunks: []diff.Hunk{{
			Header: "@@ -1,1 +1,1 @@",
			Lines: []diff.Line{
				{Kind: diff.LineAdd, Content: "x := 1", NewLine: 1},
				{Kind: diff.LineDelete, Content: "x := 0", OldLine: 1},
			},
		}},
	}}
	render := func() string {
		return Render(files, FlattenFile(files, 0), 0, 0, 0, 90, 18, nil, "HEAD", false)
	}

	SetTheme(LightTheme())
	light := render()
	SetTheme(DarkTheme())
	dark := render()

	// The dark cursor/add/del backgrounds must not appear in the light render
	// and vice versa (RGB triples from theme.go).
	darkSeqs := []string{"48;2;61;63;75", "48;2;20;53;31", "48;2;58;24;24"}
	lightSeqs := []string{"48;2;196;204;212", "48;2;218;251;225", "48;2;255;235;233"}
	for _, seq := range darkSeqs {
		if strings.Contains(light, seq) {
			t.Fatalf("light render leaks dark background %q", seq)
		}
	}
	for _, seq := range lightSeqs {
		if strings.Contains(dark, seq) {
			t.Fatalf("dark render leaks light background %q", seq)
		}
	}

	// Diff semantics stay distinguishable in both palettes.
	for _, theme := range []Theme{DarkTheme(), LightTheme()} {
		set := map[string]bool{}
		for _, c := range []string{string(theme.AddBg), string(theme.DelBg), string(theme.HunkBg), string(theme.CursorBg), string(theme.SearchBg)} {
			if set[c] {
				t.Fatalf("palette dark=%v reuses %q for two diff roles", theme.Dark, c)
			}
			set[c] = true
		}
	}
}
