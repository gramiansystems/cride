package lsp

import (
	"testing"

	"cride/internal/source"
)

func TestRankDiagnosticsPrefersChangedLines(t *testing.T) {
	t.Parallel()

	diagnostics := []Diagnostic{
		{
			Range:    source.Range{Start: source.Location{Path: "other.go", Line: 1, Column: 1}},
			Severity: DiagnosticError,
			Message:  "other file",
		},
		{
			Range:    source.Range{Start: source.Location{Path: "changed.go", Line: 10, Column: 1}},
			Severity: DiagnosticWarning,
			Message:  "changed line",
		},
		{
			Range:    source.Range{Start: source.Location{Path: "changed.go", Line: 20, Column: 1}},
			Severity: DiagnosticError,
			Message:  "changed file",
		},
	}

	got := RankDiagnostics(diagnostics, map[string]bool{"changed.go": true}, map[string]map[int]bool{"changed.go": {10: true}}, 0)
	if got[0].Message != "changed line" {
		t.Fatalf("first diagnostic = %q, want changed line", got[0].Message)
	}
	if got[1].Message != "changed file" {
		t.Fatalf("second diagnostic = %q, want changed file", got[1].Message)
	}
}
