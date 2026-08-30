package worktree

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"cride/internal/diffsource"
	"cride/internal/search"
)

const nullPath = "/dev/null"

var _ diffsource.Source = (*Source)(nil)

// Source is the local git DiffSource. By default it diffs a pinned baseline SHA
// against the current working tree (tracked changes + untracked files). When
// head is set, it diffs two immutable git objects instead.
type Source struct {
	git             *git
	baseline        string // resolved SHA, or the empty-tree object for a fresh/root repo
	head            string // resolved SHA for immutable commit/range mode; empty means worktree
	baselineDisplay string
}

// Open creates a Source rooted at dir. baseline is a git ref (default HEAD);
// it is resolved to a stable SHA so later commits/amends don't move it.
func Open(dir, baseline string) (*Source, error) {
	g, err := newGit(dir)
	if err != nil {
		return nil, err
	}
	ref := baseline
	if ref == "" {
		ref = "HEAD"
	}
	sha, err := g.run("rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		if baseline == "" {
			// Fresh repo with no commits: review against the empty tree.
			return &Source{git: g, baseline: emptyTree, baselineDisplay: shortRef(emptyTree)}, nil
		}
		return nil, err
	}
	resolved := strings.TrimSpace(sha)
	return &Source{git: g, baseline: resolved, baselineDisplay: shortRef(resolved)}, nil
}

// OpenCommit creates a Source for a single commit, shown as first-parent parent
// to commit. Root commits are reviewed against git's empty tree.
func OpenCommit(dir, commit string) (*Source, error) {
	if strings.TrimSpace(commit) == "" {
		return nil, errors.New("commit ref is required")
	}
	g, err := newGit(dir)
	if err != nil {
		return nil, err
	}
	head, err := resolveCommit(g, commit)
	if err != nil {
		return nil, err
	}
	baseline, err := resolveCommit(g, head+"^")
	if err != nil {
		baseline = emptyTree
	}
	return &Source{
		git:             g,
		baseline:        baseline,
		head:            head,
		baselineDisplay: shortRef(baseline) + ".." + shortRef(head),
	}, nil
}

// OpenRange creates a Source for a git revision range. Two-dot ranges compare
// BASE to HEAD. Three-dot ranges compare merge-base(BASE, HEAD) to HEAD, matching
// `git diff BASE...HEAD`.
func OpenRange(dir, revRange string) (*Source, error) {
	if strings.TrimSpace(revRange) == "" {
		return nil, errors.New("range is required")
	}
	g, err := newGit(dir)
	if err != nil {
		return nil, err
	}
	baseline, head, sep, err := resolveRange(g, revRange)
	if err != nil {
		return nil, err
	}
	return &Source{
		git:             g,
		baseline:        baseline,
		head:            head,
		baselineDisplay: shortRef(baseline) + sep + shortRef(head),
	}, nil
}

func (s *Source) Root() string { return s.git.root }

func (s *Source) Baseline() string {
	if s.baselineDisplay != "" {
		return s.baselineDisplay
	}
	return shortRef(s.baseline)
}

// Diff returns the unified review diff. Worktree mode includes untracked,
// non-ignored files; immutable mode compares the two selected git objects.
func (s *Source) Diff() ([]byte, error) {
	if s.head != "" {
		out, err := s.git.run("diff", "--no-color", "-M", s.baseline, s.head)
		return []byte(out), err
	}

	var b strings.Builder

	tracked, err := s.git.run("diff", "--no-color", "-M", s.baseline)
	if err != nil {
		return nil, err
	}
	b.WriteString(tracked)

	others, err := s.git.run("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, p := range splitZ(others) {
		// --no-index exits 1 on differences (always, for a new file); runCode
		// treats that as success. Skip anything that genuinely errors.
		d, _, err := s.git.runCode("diff", "--no-color", "--no-index", "--", nullPath, p)
		if err != nil {
			continue
		}
		b.WriteString(d)
	}
	return []byte(b.String()), nil
}

// CurrentContent returns the current-side bytes for path, bounded by
// diffsource.MaxContentBytes.
func (s *Source) CurrentContent(path string) ([]byte, error) {
	if s.head != "" {
		return s.objectContent(s.head, path)
	}

	clean, err := cleanRepoPath(path)
	if err != nil {
		return nil, err
	}
	full := filepath.Join(s.git.root, filepath.FromSlash(clean))

	info, err := os.Stat(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", clean, fs.ErrNotExist)
		}
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", clean, diffsource.ErrNotRegular)
	}
	if info.Size() > diffsource.MaxContentBytes {
		return nil, fmt.Errorf("%s: %w", clean, diffsource.ErrFileTooLarge)
	}

	return readBounded(full)
}

// BaselineContent returns the pinned baseline bytes for path, bounded by
// diffsource.MaxContentBytes.
func (s *Source) BaselineContent(path string) ([]byte, error) {
	return s.objectContent(s.baseline, path)
}

func (s *Source) objectContent(ref, path string) ([]byte, error) {
	clean, err := cleanRepoPath(path)
	if err != nil {
		return nil, err
	}
	spec := ref + ":" + clean

	typeOut, _, err := s.git.runCode("cat-file", "-t", spec)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", clean, fs.ErrNotExist)
	}
	if strings.TrimSpace(typeOut) != "blob" {
		return nil, fmt.Errorf("%s: %w", clean, diffsource.ErrNotRegular)
	}

	sizeOut, _, err := s.git.runCode("cat-file", "-s", spec)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", clean, fs.ErrNotExist)
	}
	size, err := strconv.ParseInt(strings.TrimSpace(sizeOut), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse baseline size for %s: %w", clean, err)
	}
	if size > diffsource.MaxContentBytes {
		return nil, fmt.Errorf("%s: %w", clean, diffsource.ErrFileTooLarge)
	}

	out, _, err := s.git.runCode("show", spec)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", clean, fs.ErrNotExist)
	}
	return []byte(out), nil
}

// ChangedPaths returns paths changed in the review diff.
func (s *Source) ChangedPaths() ([]string, error) {
	if s.head != "" {
		out, err := s.git.run("diff", "--name-only", "-z", "-M", s.baseline, s.head)
		if err != nil {
			return nil, err
		}
		return splitZ(out), nil
	}

	seen := map[string]bool{}
	var paths []string
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		paths = append(paths, p)
	}

	tracked, err := s.git.run("diff", "--name-only", "-z", "-M", s.baseline)
	if err != nil {
		return nil, err
	}
	for _, p := range splitZ(tracked) {
		add(p)
	}

	others, err := s.git.run("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, p := range splitZ(others) {
		add(p)
	}
	return paths, nil
}

// ProjectFiles returns files visible on the current side of the review.
func (s *Source) ProjectFiles() ([]string, error) {
	if s.head != "" {
		out, err := s.git.run("ls-tree", "-r", "-z", "--name-only", s.head)
		if err != nil {
			return nil, err
		}
		return splitZ(out), nil
	}

	out, err := s.git.run("ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	paths := splitZ(out)
	for i, path := range paths {
		paths[i] = filepath.ToSlash(path)
	}
	return paths, nil
}

// Search returns parsed regular-expression matches from both sides of the review.
func (s *Source) Search(query string) ([]search.Result, error) {
	if query == "" {
		return nil, nil
	}
	return s.searchBothSides(query, searchOptions{})
}

// SearchText runs a literal, smart-case search across both sides of the
// review. This is the interactive project-search path; Search remains regex
// based for definition and other lexical lookups.
func (s *Source) SearchText(query string) ([]search.Result, error) {
	if query == "" {
		return nil, nil
	}
	return s.searchBothSides(query, searchOptions{
		literal:    true,
		ignoreCase: !strings.ContainsFunc(query, unicode.IsUpper),
	})
}

// SearchWord runs whole-word search across both sides of the review.
func (s *Source) SearchWord(word string) ([]search.Result, error) {
	if word == "" {
		return nil, nil
	}
	return s.searchBothSides(word, searchOptions{word: true})
}

type searchOptions struct {
	word       bool
	literal    bool
	ignoreCase bool
}

func (s *Source) searchBothSides(query string, options searchOptions) ([]search.Result, error) {
	current, err := s.searchCurrent(query, options)
	if err != nil {
		return nil, err
	}
	if s.baseline == "" || s.baseline == emptyTree {
		return current, nil
	}
	baseline, err := s.gitGrepRef(s.baseline, query, options, search.ResultSideBaseline)
	if err != nil {
		return nil, err
	}
	return mergeSearchResults(current, baseline), nil
}

func (s *Source) searchCurrent(query string, options searchOptions) ([]search.Result, error) {
	if s.head != "" {
		return s.gitGrepRef(s.head, query, options, search.ResultSideCurrent)
	}
	return s.searchWorktree(query, options)
}

func (s *Source) searchWorktree(query string, options searchOptions) ([]search.Result, error) {
	rg, err := exec.LookPath("rg")
	if err != nil {
		return s.gitGrepWorktree(query, options)
	}

	args := []string{"--line-number", "--column", "--no-heading", "--color", "never"}
	if options.word {
		args = append(args, "--word-regexp")
	}
	if options.literal {
		args = append(args, "--fixed-strings")
	}
	if options.ignoreCase {
		args = append(args, "--ignore-case")
	}
	args = append(args, "--", query, s.git.root)
	cmd := exec.Command(rg, args...)
	cmd.Dir = s.git.root
	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err = cmd.Run()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return nil, nil
		}
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("rg search failed: %s", msg)
	}

	results := search.ParseRipgrepOutput(outb.Bytes())
	for i := range results {
		results[i].Location.Path = s.cleanSearchPath(results[i].Location.Path)
		results[i].Label = fmt.Sprintf("%s:%d:%d", results[i].Location.Path, results[i].Location.Line, results[i].Location.Column)
		results[i].Side = search.ResultSideCurrent
	}
	return results, nil
}

func (s *Source) gitGrepWorktree(query string, options searchOptions) ([]search.Result, error) {
	args := []string{"grep", "--line-number", "--column", "--no-color", "-I", "--untracked"}
	if options.literal {
		args = append(args, "--fixed-strings")
	} else {
		args = append(args, "--extended-regexp")
	}
	if options.word {
		args = append(args, "--word-regexp")
	}
	if options.ignoreCase {
		args = append(args, "--ignore-case")
	}
	args = append(args, "-e", query, "--")
	out, code, err := s.git.runCode(args...)
	if err != nil {
		return nil, err
	}
	if code == 1 || strings.TrimSpace(out) == "" {
		return nil, nil
	}

	results := search.ParseRipgrepOutput([]byte(out))
	for i := range results {
		results[i].Location.Path = s.cleanSearchPath(results[i].Location.Path)
		results[i].Label = fmt.Sprintf("%s:%d:%d", results[i].Location.Path, results[i].Location.Line, results[i].Location.Column)
		results[i].Side = search.ResultSideCurrent
	}
	return results, nil
}

func (s *Source) gitGrepRef(ref, query string, options searchOptions, side search.ResultSide) ([]search.Result, error) {
	if ref == "" || ref == emptyTree {
		return nil, nil
	}
	args := []string{"grep", "--line-number", "--column", "--no-color", "-I"}
	if options.literal {
		args = append(args, "--fixed-strings")
	} else {
		args = append(args, "--extended-regexp")
	}
	if options.word {
		args = append(args, "--word-regexp")
	}
	if options.ignoreCase {
		args = append(args, "--ignore-case")
	}
	args = append(args, "-e", query, ref, "--")
	out, code, err := s.git.runCode(args...)
	if err != nil {
		return nil, err
	}
	if code == 1 || strings.TrimSpace(out) == "" {
		return nil, nil
	}

	prefix := ref + ":"
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, prefix)
	}
	results := search.ParseRipgrepOutput([]byte(strings.Join(lines, "\n")))
	for i := range results {
		results[i].Location.Path = s.cleanSearchPath(results[i].Location.Path)
		results[i].Label = fmt.Sprintf("%s:%d:%d", results[i].Location.Path, results[i].Location.Line, results[i].Location.Column)
		results[i].Side = side
	}
	return results, nil
}

func mergeSearchResults(current, baseline []search.Result) []search.Result {
	out := make([]search.Result, 0, len(current)+len(baseline))
	seen := map[searchResultKey]int{}
	for _, result := range current {
		key := makeSearchResultKey(result)
		if _, ok := seen[key]; !ok {
			seen[key] = len(out)
		}
		out = append(out, result)
	}
	for _, result := range baseline {
		key := makeSearchResultKey(result)
		if idx, ok := seen[key]; ok {
			if out[idx].Side == search.ResultSideCurrent || out[idx].Side == search.ResultSideBaseline {
				out[idx].Side = search.ResultSideBoth
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, result)
	}
	return out
}

type searchResultKey struct {
	kind    search.ResultKind
	path    string
	line    int
	column  int
	preview string
}

func makeSearchResultKey(result search.Result) searchResultKey {
	return searchResultKey{
		kind:    result.Kind,
		path:    result.Location.Path,
		line:    result.Location.Line,
		column:  result.Location.Column,
		preview: result.Preview,
	}
}

func (s *Source) cleanSearchPath(path string) string {
	path = strings.TrimPrefix(filepath.ToSlash(path), "./")
	if filepath.IsAbs(path) {
		if rel, err := filepath.Rel(s.git.root, filepath.FromSlash(path)); err == nil {
			path = rel
		}
	}
	return filepath.ToSlash(strings.TrimPrefix(path, "./"))
}

func resolveCommit(g *git, ref string) (string, error) {
	sha, err := g.run("rev-parse", "--verify", strings.TrimSpace(ref)+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(sha), nil
}

func resolveRange(g *git, revRange string) (string, string, string, error) {
	revRange = strings.TrimSpace(revRange)
	if strings.Contains(revRange, "...") {
		left, right, ok := strings.Cut(revRange, "...")
		if !ok || left == "" || right == "" || strings.Contains(right, "...") {
			return "", "", "", fmt.Errorf("range must be BASE..HEAD or BASE...HEAD")
		}
		head, err := resolveCommit(g, right)
		if err != nil {
			return "", "", "", err
		}
		base, code, err := g.runCode("merge-base", left, right)
		if err != nil {
			return "", "", "", err
		}
		if code != 0 || strings.TrimSpace(base) == "" {
			return "", "", "", fmt.Errorf("no merge base for %s and %s", left, right)
		}
		return strings.TrimSpace(base), head, "...", nil
	}

	left, right, ok := strings.Cut(revRange, "..")
	if !ok || left == "" || right == "" || strings.Contains(right, "..") {
		return "", "", "", fmt.Errorf("range must be BASE..HEAD or BASE...HEAD")
	}
	baseline, err := resolveCommit(g, left)
	if err != nil {
		return "", "", "", err
	}
	head, err := resolveCommit(g, right)
	if err != nil {
		return "", "", "", err
	}
	return baseline, head, "..", nil
}

func shortRef(ref string) string {
	if len(ref) > 12 {
		return ref[:12]
	}
	return ref
}

func cleanRepoPath(path string) (string, error) {
	path = filepath.ToSlash(path)
	if path == "" || filepath.IsAbs(path) {
		return "", fmt.Errorf("%s: %w", path, fs.ErrNotExist)
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%s: %w", path, fs.ErrNotExist)
	}
	return filepath.ToSlash(clean), nil
}

func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fs.ErrNotExist
		}
		return nil, err
	}
	defer f.Close()

	b, err := io.ReadAll(io.LimitReader(f, diffsource.MaxContentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > diffsource.MaxContentBytes {
		return nil, diffsource.ErrFileTooLarge
	}
	return b, nil
}

func splitZ(s string) []string {
	s = strings.Trim(s, "\x00")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x00")
}
