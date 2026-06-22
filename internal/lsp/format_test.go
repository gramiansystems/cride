package lsp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFormatHoverHandlesMarkdownCodeFencesAndLongOutput(t *testing.T) {
	t.Parallel()

	lines := FormatHover("**Target**\n```go\nfunc Target() string\n```\n"+strings.Repeat("more words ", 20), 4, 28)
	joined := strings.Join(lines, "\n")

	if strings.Contains(joined, "```") {
		t.Fatalf("hover still contains code fence:\n%s", joined)
	}
	if !strings.Contains(joined, "Target") || !strings.Contains(joined, "func Target() string") {
		t.Fatalf("hover missing plain content:\n%s", joined)
	}
	if len(lines) != 4 {
		t.Fatalf("line count = %d, want 4", len(lines))
	}
	if !strings.HasSuffix(lines[len(lines)-1], "…") {
		t.Fatalf("last line = %q, want ellipsis", lines[len(lines)-1])
	}
}

func TestParseLocationsAcceptsClangdLocationLinks(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal([]wireLocationLink{{
		TargetURI:            "file:///repo/include/widget.hpp",
		TargetSelectionRange: wireRange{Start: wirePosition{Line: 6, Character: 10}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	locations := parseLocations("/repo", raw)
	if len(locations) != 1 || locations[0].Path != "include/widget.hpp" || locations[0].Line != 7 || locations[0].Column != 11 {
		t.Fatalf("locations = %+v", locations)
	}
}
