package keymap

import (
	"os"
	"testing"
)

// TestCheatsheetInSync fails when docs/keymap.md was not regenerated after a
// keymap change — the committed cheatsheet must match the source of truth.
func TestCheatsheetInSync(t *testing.T) {
	data, err := os.ReadFile("../../docs/keymap.md")
	if err != nil {
		t.Fatalf("read docs/keymap.md: %v (run `make docs`)", err)
	}
	if string(data) != Markdown() {
		t.Fatal("docs/keymap.md is stale; run `make docs` to regenerate it")
	}
}

func TestGroupsHaveContent(t *testing.T) {
	for _, group := range Groups {
		if group.Title == "" {
			t.Fatal("group with empty title")
		}
		if len(group.Bindings) == 0 {
			t.Fatalf("group %q has no bindings", group.Title)
		}
		for _, binding := range group.Bindings {
			if binding.Keys == "" || binding.Desc == "" {
				t.Fatalf("group %q has an incomplete binding: %+v", group.Title, binding)
			}
		}
	}
}
