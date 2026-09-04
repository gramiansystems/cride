// Command cride is a terminal IDE for reading a diff a third party (an agent)
// is producing: it pins a review baseline, watches the working tree, marks
// newly-arrived changes unread, and round-trips region-anchored review
// comments through review.md. See DESIGN.md and README.md.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"cride/internal/app"
	"cride/internal/config"
	"cride/internal/diffsource/worktree"
	"cride/internal/highlight"
	"cride/internal/lsp"
	crideterminal "cride/internal/terminal"
	"cride/internal/ui"
)

func main() {
	baseline := flag.String("baseline", "", "review baseline ref (default: HEAD at session start)")
	flag.StringVar(baseline, "b", "", "shorthand for --baseline")
	commit := flag.String("commit", "", "review a single commit (first parent to commit)")
	revRange := flag.String("range", "", "review a commit range (BASE..HEAD or BASE...HEAD)")
	fresh := flag.Bool("fresh", false, "ignore any stored session state")
	version := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `cride — a terminal IDE for reviewing a diff an agent is producing

usage: cride [--baseline ref | --range BASE..HEAD | --commit ref] [path]

Run inside a git repo (or pass its path). cride pins a review baseline
(default: HEAD at first start), watches the working tree, and marks
newly-arrived changes unread. Press ? inside the app for the command palette;
docs/keymap.md documents the keyboard shortcuts.

flags:
`)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
files:
  %s
      user config ("key = value"): theme = auto|dark|light,
      chroma_style = <Chroma style name>
  $XDG_STATE_HOME/cride/<repo-id>/session.json
      per-review session state (cursor, read/unread, view prefs);
      --fresh ignores it
  review.md (repo root)
      canonical editable review comments for the reviewer and agent;
      add it to .gitignore so it stays out of the diff
`, config.Path())
	}
	flag.Parse()

	if *version {
		fmt.Println("cride " + buildVersion())
		return
	}

	dir := "."
	if args := flag.Args(); len(args) > 0 {
		dir = args[0]
	}

	selectedModes := 0
	for _, value := range []string{*baseline, *commit, *revRange} {
		if value != "" {
			selectedModes++
		}
	}
	if selectedModes > 1 {
		fmt.Fprint(os.Stderr, "cride: --baseline, --range, and --commit are mutually exclusive\n")
		os.Exit(2)
	}

	var (
		src *worktree.Source
		err error
	)
	switch {
	case *commit != "":
		src, err = worktree.OpenCommit(dir, *commit)
	case *revRange != "":
		src, err = worktree.OpenRange(dir, *revRange)
	default:
		src, err = worktree.Open(dir, *baseline)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "cride: %v\n", err)
		os.Exit(1)
	}

	// Theme selection: config override, else the terminal background decides.
	// Detection queries the terminal, so it must run before bubbletea starts.
	cfg := config.Load()
	dark := cfg.WantsDark(lipgloss.HasDarkBackground())
	ui.SetTheme(ui.NewTheme(dark))
	hl := highlight.NewWithOptions(highlight.Options{
		Style:     cfg.ChromaStyle,
		Dark:      dark,
		TrueColor: terminalSupportsTrueColor(),
		Disabled:  os.Getenv("NO_COLOR") != "",
	})
	keyboardInput, restoreKeyboard := crideterminal.EnableKeyboardEnhancements(os.Stdin, os.Stdout)
	defer restoreKeyboard()

	programOptions := []tea.ProgramOption{
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	}
	if keyboardInput != nil {
		programOptions = append(programOptions, tea.WithInput(keyboardInput))
	}
	p := tea.NewProgram(app.NewWithOptions(src, app.Options{
		LSP:          lsp.NewProcessClient(src.Root(), lsp.DefaultConfig()),
		Highlighter:  hl,
		FreshSession: *fresh,
	}), programOptions...)
	_, runErr := p.Run()
	restoreKeyboard()
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "cride: %v\n", runErr)
		os.Exit(1)
	}
}

func terminalSupportsTrueColor() bool {
	colorterm := strings.ToLower(os.Getenv("COLORTERM"))
	return strings.Contains(colorterm, "truecolor") || strings.Contains(colorterm, "24bit")
}

func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(unknown)"
}
