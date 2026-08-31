package ui

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"

	"cride/internal/diff"
)

// ChangeListRow is one rendered row of the change list: a directory (which
// may be collapsed) or a file.
type ChangeListRow struct {
	Name      string
	Path      string // full slash-separated path from the repo root
	IsDir     bool
	FileIdx   int // index into files; -1 for directories
	Depth     int
	Collapsed bool
	// Aggregate stats: for dirs, summed over all descendant files.
	Added   int
	Deleted int
	Files   int
	// Unread marks unread files; for dirs, UnreadCount aggregates descendants.
	Unread      bool
	UnreadCount int
}

// ChangeListView is the change list's single source of truth: what is
// rendered is exactly what mouse hit-testing and cursor motion see.
type ChangeListView struct {
	Rows     []ChangeListRow
	Top      int // effective scroll after keeping cursor/selection visible
	Cursor   int // row index of the list cursor; -1 when the list is unfocused
	Selected int // row index of the selected file; -1 if hidden or none
	Height   int
	Focused  bool
}

// ChangeListOrder controls the file-list sort. The zero value is the default:
// change order floats files/directories touched most recently.
type ChangeListOrder int

const (
	ChangeListOrderChanged ChangeListOrder = iota
	ChangeListOrderPath
)

const DefaultChangeListOrder = ChangeListOrderChanged

func (o ChangeListOrder) String() string {
	if o == ChangeListOrderChanged {
		return "change order"
	}
	return "path order"
}

func (o ChangeListOrder) ID() string {
	if o == ChangeListOrderChanged {
		return "change"
	}
	return "path"
}

func ParseChangeListOrder(s string) (ChangeListOrder, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return DefaultChangeListOrder, false
	case "change", "changed", "recent":
		return ChangeListOrderChanged, true
	case "path":
		return ChangeListOrderPath, true
	default:
		return DefaultChangeListOrder, false
	}
}

type ChangeListOptions struct {
	Order         ChangeListOrder
	ChangeOrdinal map[string]int
}

// ChangeListRows flattens the change tree, skipping collapsed subtrees.
// unread maps file indexes to unread state for badges and dir aggregates.
func ChangeListRows(files []diff.FileDiff, collapsed map[string]bool, unread map[int]bool) []ChangeListRow {
	return ChangeListRowsWithOptions(files, collapsed, unread, ChangeListOptions{})
}

func ChangeListRowsWithOptions(files []diff.FileDiff, collapsed map[string]bool, unread map[int]bool, opts ChangeListOptions) []ChangeListRow {
	root := buildChangeTree(files, opts)
	var rows []ChangeListRow
	var walk func(n *changeTreeNode, depth int)
	walk = func(n *changeTreeNode, depth int) {
		for _, child := range n.children {
			row := ChangeListRow{
				Name:    child.name,
				Path:    child.path,
				IsDir:   child.isDir,
				FileIdx: child.fileIdx,
				Depth:   depth,
			}
			if child.isDir {
				row.Added, row.Deleted, row.Files = aggregateChangeStats(child, files)
				row.UnreadCount = aggregateUnread(child, unread)
				row.Unread = row.UnreadCount > 0
				row.Collapsed = collapsed[child.path]
			} else if child.fileIdx >= 0 && child.fileIdx < len(files) {
				row.Added = files[child.fileIdx].Added
				row.Deleted = files[child.fileIdx].Deleted
				row.Files = 1
				row.Unread = unread[child.fileIdx]
				if row.Unread {
					row.UnreadCount = 1
				}
			}
			rows = append(rows, row)
			if child.isDir && !row.Collapsed {
				walk(child, depth+1)
			}
		}
	}
	walk(root, 0)
	return rows
}

func aggregateUnread(n *changeTreeNode, unread map[int]bool) int {
	if !n.isDir {
		if unread[n.fileIdx] {
			return 1
		}
		return 0
	}
	count := 0
	for _, child := range n.children {
		count += aggregateUnread(child, unread)
	}
	return count
}

// BuildChangeListView assembles the view, clamping scroll so the cursor
// (when focused) or the selected file (otherwise) stays visible.
func BuildChangeListView(files []diff.FileDiff, collapsed map[string]bool, unread map[int]bool, selectedFile, cursor, top, height int, focused bool) ChangeListView {
	return BuildChangeListViewWithOptions(files, collapsed, unread, selectedFile, cursor, top, height, focused, ChangeListOptions{})
}

func BuildChangeListViewWithOptions(files []diff.FileDiff, collapsed map[string]bool, unread map[int]bool, selectedFile, cursor, top, height int, focused bool, opts ChangeListOptions) ChangeListView {
	rows := ChangeListRowsWithOptions(files, collapsed, unread, opts)
	view := ChangeListView{Rows: rows, Cursor: -1, Selected: -1, Height: max(1, height), Focused: focused}
	for i, row := range rows {
		if !row.IsDir && row.FileIdx == selectedFile {
			view.Selected = i
			break
		}
	}
	if focused && len(rows) > 0 {
		view.Cursor = min(max(cursor, 0), len(rows)-1)
	}

	anchor := view.Selected
	if focused {
		anchor = view.Cursor
	}
	maxTop := max(0, len(rows)-view.Height)
	top = min(max(top, 0), maxTop)
	if anchor >= 0 {
		if anchor < top {
			top = anchor
		}
		if anchor >= top+view.Height {
			top = anchor - view.Height + 1
		}
	}
	view.Top = min(max(top, 0), maxTop)
	return view
}

// RowAt maps a zero-based visible line of the list to a row index, or -1.
func (v ChangeListView) RowAt(line int) int {
	if line < 0 || line >= v.Height {
		return -1
	}
	idx := v.Top + line
	if idx < 0 || idx >= len(v.Rows) {
		return -1
	}
	return idx
}

// ChangeListFileOrder returns file indexes in rendered order, ignoring
// collapse state so file cycling can reach hidden files (which auto-reveal).
func ChangeListFileOrder(files []diff.FileDiff) []int {
	return ChangeListFileOrderWithOptions(files, ChangeListOptions{})
}

func ChangeListFileOrderWithOptions(files []diff.FileDiff, opts ChangeListOptions) []int {
	rows := ChangeListRowsWithOptions(files, nil, nil, opts)
	order := make([]int, 0, len(files))
	for _, row := range rows {
		if !row.IsDir {
			order = append(order, row.FileIdx)
		}
	}
	return order
}

// ChangeListAncestorDirs returns the directory paths containing path.
func ChangeListAncestorDirs(path string) []string {
	path = filepath.ToSlash(path)
	var dirs []string
	for i, r := range path {
		if r == '/' {
			dirs = append(dirs, path[:i])
		}
	}
	return dirs
}

func aggregateChangeStats(n *changeTreeNode, files []diff.FileDiff) (added, deleted, count int) {
	if !n.isDir {
		if n.fileIdx >= 0 && n.fileIdx < len(files) {
			return files[n.fileIdx].Added, files[n.fileIdx].Deleted, 1
		}
		return 0, 0, 0
	}
	for _, child := range n.children {
		a, d, c := aggregateChangeStats(child, files)
		added += a
		deleted += d
		count += c
	}
	return added, deleted, count
}

type changeTreeNode struct {
	name     string
	path     string
	isDir    bool
	fileIdx  int
	change   int
	children []*changeTreeNode
}

func buildChangeTree(files []diff.FileDiff, opts ChangeListOptions) *changeTreeNode {
	root := &changeTreeNode{isDir: true, fileIdx: -1}
	for i, f := range files {
		parts := strings.Split(filepath.ToSlash(f.Path()), "/")
		curr := root
		prefix := ""
		for pi, part := range parts {
			if part == "" {
				continue
			}
			if prefix == "" {
				prefix = part
			} else {
				prefix += "/" + part
			}
			isLast := pi == len(parts)-1
			child := findChild(curr, part)
			if child == nil {
				child = &changeTreeNode{
					name:    part,
					path:    prefix,
					isDir:   !isLast,
					fileIdx: -1,
				}
				curr.children = append(curr.children, child)
			}
			if isLast {
				child.isDir = false
				child.fileIdx = i
				if opts.ChangeOrdinal != nil {
					child.change = opts.ChangeOrdinal[f.Path()]
				}
			}
			curr = child
		}
	}
	finalizeChangeTree(root)
	sortChangeTree(root, opts.Order)
	return root
}

func findChild(n *changeTreeNode, name string) *changeTreeNode {
	for _, child := range n.children {
		if child.name == name {
			return child
		}
	}
	return nil
}

func finalizeChangeTree(n *changeTreeNode) int {
	if !n.isDir {
		return n.change
	}
	maxChange := 0
	for _, child := range n.children {
		maxChange = max(maxChange, finalizeChangeTree(child))
	}
	n.change = maxChange
	return maxChange
}

func sortChangeTree(n *changeTreeNode, order ChangeListOrder) {
	sort.Slice(n.children, func(i, j int) bool {
		a, b := n.children[i], n.children[j]
		if order == ChangeListOrderChanged && a.change != b.change {
			return a.change > b.change
		}
		if a.isDir != b.isDir {
			return a.isDir
		}
		return strings.ToLower(a.name) < strings.ToLower(b.name)
	})
	for _, child := range n.children {
		if child.isDir {
			sortChangeTree(child, order)
		}
	}
}

func changeListLines(view ChangeListView, files []diff.FileDiff, width int) []string {
	height := view.Height
	if height <= 0 {
		return nil
	}
	if len(view.Rows) == 0 {
		return []string{dimStyle.Render(" (clean)")}
	}

	scrollbar := len(view.Rows) > height
	rowWidth := width
	if scrollbar {
		rowWidth = max(1, width-1)
	}

	out := make([]string, 0, min(height, len(view.Rows)))
	for i := view.Top; i < len(view.Rows) && len(out) < height; i++ {
		label := changeListRowLabel(view.Rows[i], files, rowWidth)
		switch {
		case view.Focused && i == view.Cursor:
			label = selectedFileStyle.Width(rowWidth).MaxWidth(rowWidth).Render(padRight(label, rowWidth))
		case i == view.Selected:
			style := selectedFileStyle
			if view.Focused {
				style = hunkBgStyle // selection stays visible but yields to the cursor
			}
			label = style.Width(rowWidth).MaxWidth(rowWidth).Render(padRight(label, rowWidth))
		default:
			label = normalFileStyle.Width(rowWidth).MaxWidth(rowWidth).Render(padRight(label, rowWidth))
		}
		if scrollbar {
			label += changeListScrollbarCell(view, len(out))
		}
		out = append(out, label)
	}
	return out
}

// changeListScrollbarCell renders one cell of the slim scrollbar column: the
// thumb marks which slice of the list is visible.
func changeListScrollbarCell(view ChangeListView, line int) string {
	total := len(view.Rows)
	height := view.Height
	thumbLen := max(1, height*height/total)
	maxTop := max(1, total-height)
	thumbStart := (height - thumbLen) * view.Top / maxTop
	if line >= thumbStart && line < thumbStart+thumbLen {
		return borderStyle.Render("█")
	}
	return dimStyle.Render("░")
}

func changeListRowLabel(row ChangeListRow, files []diff.FileDiff, width int) string {
	indent := strings.Repeat("  ", row.Depth)
	if row.IsDir {
		if row.Collapsed {
			label := "  " + indent + "▸ " + row.Name + "/"
			stat := " " + changeStat(row.Added, row.Deleted) + dimStyle.Render("  ("+strconv.Itoa(row.Files)+" files)")
			if row.UnreadCount > 0 {
				stat += " " + unreadBadgeStyle.Render(strconv.Itoa(row.UnreadCount)+"●")
			}
			return truncate.String(label, uint(max(1, width-lipgloss.Width(stat)))) + stat
		}
		return truncate.String("  "+indent+"▾ "+row.Name+"/", uint(max(1, width)))
	}

	f := files[row.FileIdx]
	stat := ""
	if f.Status != diff.FileUnchanged {
		stat = " " + changeStat(f.Added, f.Deleted)
	}
	badge := "  "
	if row.Unread {
		badge = unreadBadgeStyle.Render("● ")
	}
	prefix := statusLetter(f.Status) + " " + indent + badge
	budget := max(1, width-lipgloss.Width(prefix)-lipgloss.Width(stat))
	name := truncate.String(row.Name, uint(budget))
	return prefix + name + stat
}
