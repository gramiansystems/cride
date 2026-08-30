// Package app is the bubbletea root: the Model, Update, and View that wire the
// DiffSource, diff parsing, highlighting, and rendering together. All heavy
// work runs in commands; Update only mutates state. See DESIGN.md's "Data
// flow" section.
package app

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/annotate"
	"cride/internal/diff"
	"cride/internal/diffsource"
	"cride/internal/highlight"
	"cride/internal/lsp"
	"cride/internal/outline"
	navsearch "cride/internal/search"
	"cride/internal/session"
	"cride/internal/source"
	"cride/internal/ui"
)

type ViewMode int

const (
	ViewDiff ViewMode = iota
	ViewFile
)

type OverlayKind int

const (
	OverlayNone OverlayKind = iota
	OverlayFileOpen
	OverlaySearch
	OverlayWorkspaceSymbol
	OverlaySymbolChoice
	OverlayCommandPalette
)

type referenceRequestKind int

const (
	referenceRequestUsages referenceRequestKind = iota
	referenceRequestDefinition
	referenceRequestImpact
)

type enrichmentPanelKind int

const (
	enrichmentPanelNone enrichmentPanelKind = iota
	enrichmentPanelDiagnosticsCurrent
	enrichmentPanelDiagnosticsWorkspace
	enrichmentPanelHover
	enrichmentPanelDocumentSymbols
	enrichmentPanelOutlineDiff
	enrichmentPanelCallIncoming
	enrichmentPanelCallOutgoing
)

type paneResizeKind int

const (
	resizeNone paneResizeKind = iota
	resizeChangeList
	resizeResultHeight
	resizeResultWidth
)

const (
	searchDebounceDelay = 150 * time.Millisecond
	overlayResultLimit  = 80
	localExpansionStep  = 10
	commandNameWidth    = 34
)

// Model holds all UI state. Messages are the only way to mutate it.
type Model struct {
	source diffsource.Source
	hl     *highlight.Highlighter
	lsp    lsp.Client

	files        []diff.FileDiff
	changedPaths map[string]bool

	viewMode ViewMode

	selectedFile int
	cursor       int
	col          int // rune index into the cursor row's active-side content
	desiredCol   int // sticky column re-applied after vertical motion; desiredEOL pins to line ends
	top          int // first (possibly partially) visible row
	topWrap      int // screen lines of that row scrolled off above
	width        int
	height       int

	loading bool
	err     error

	fileStates        map[fileStateKey]fileState
	fileContents      map[string]fileContentState
	localExpansions   map[string]map[int]int
	diffViewOrigins   map[string]diffViewPosition
	fileViewAnchors   map[string]fileViewAnchor
	pendingViewJump   viewJumpAnchor
	contentGeneration int
	rowsVersion       int
	wrap              *wrapCacheState

	projectFiles        []string
	projectFilesLoading bool
	projectFilesErr     error
	recentPaths         []string

	overlay          overlayState
	searchGeneration int
	projectSearch    projectSearchMemo

	referencePanel      referencePanelState
	referenceGeneration int

	enrichmentPanel      enrichmentPanelState
	enrichmentGeneration int
	diagnostics          map[string][]lsp.Diagnostic
	lspStatuses          map[string]lsp.Status
	outlineExtractor     outline.Extractor
	outlineChanges       []outline.SymbolChange
	outlineCurrent       map[string][]lsp.DocumentSymbol
	outlineBaseline      map[string][]lsp.DocumentSymbol
	outlineGeneration    int
	outlineLoading       bool
	outlineLoaded        bool
	outlineWholeReview   bool

	pendingLocation    source.Location
	hasPendingLocation bool

	jumplist  []jumpEntry
	jumpIndex int // position in jumplist; == len(jumplist) when at the live end

	countBuf       string
	pendingG       bool
	pendingZ       bool
	pendingBracket int  // +1 after ], -1 after [; 0 when idle
	pendingFind    byte // f/F/t/T awaiting a target rune; 0 when idle
	lastFind       findMotion

	mode            editorMode
	editPrevView    ViewMode // view restored when editing ends
	editDirty       bool
	editOriginal    []string // entry-state buffer; dirty becomes false when undo restores it
	editUndo        []editSnapshot
	editRedo        []editSnapshot
	editRegister    editRegister
	pendingOp       byte // d/c/y awaiting a motion target; 0 when idle
	pendingOpCount  int  // count typed before pendingOp; multiplied by a motion count
	pendingReplace  int  // r count awaiting the replacement rune; 0 when idle
	pendingZUpper   bool // Z pressed in EDIT mode, awaiting Z (save) / Q (discard)
	pendingEditKind byte // i/a/I pressed before the buffer loaded
	pendingEditLine int
	pendingEditCol  int
	pendingEditRow  int    // cursor's viewport row before edit entry
	pendingSeenPath string // path whose seen hash re-stamps after a save reload
	editLockHeld    bool

	status           statusState
	statusGeneration int
	spinnerFrame     int
	spinnerActive    bool
	reloadRequested  bool

	search       searchViewState
	searchKey    searchMatchKey
	fileSearches map[string]searchMemo

	focus           paneID
	listCursor      int
	listTop         int
	changeListWidth int
	collapsedDirs   map[string]bool
	changeOrder     ui.ChangeListOrder
	changeClock     int
	changeOrdinal   map[string]int
	changeHashes    map[string]string

	resultPanelPlacement ui.PanelPlacement
	resultPanelHeight    int
	resultPanelWidth     int
	resizingPane         paneResizeKind

	splitFiles      map[string]bool // per-file side-by-side toggle (zs)
	splitActiveLeft bool            // which pair column symbol lookups use

	loadSeq           int    // guards stale diff loads
	loadedFingerprint string // tree fingerprint captured with the loaded diff
	treeChanged       bool   // poll fallback noticed drift; ^R reloads
	watcherActive     bool
	watchCh           chan struct{}
	watchStop         func()

	seen map[string]string // path → diff hash at last mark-read (unread = current != seen)

	review   annotate.Review
	composer composerState

	freshSession       bool
	sessionFiles       map[string]session.FileState // source-coordinate mirror of fileStates
	pendingSession     *session.State
	sessionApplied     bool
	sessionDirty       bool
	sessionSavePending bool
}

type overlayState struct {
	Kind                    OverlayKind
	CommandCategory         CommandCategory
	Query                   string
	Cursor                  int
	Top                     int
	Results                 []navsearch.Result
	RawResults              []navsearch.Result
	Loading                 bool
	Err                     error
	Generation              int
	Order                   diff.ResultOrder
	SymbolQueries           []navsearch.SymbolQuery
	PendingReferenceKind    referenceRequestKind
	PendingReferenceChanged bool
	SearchRegex             bool
	QuerySelected           bool
}

type projectSearchMemo struct {
	Query  string
	Cursor int
	Top    int
	Order  diff.ResultOrder
	Regex  bool
}

type referencePanelState struct {
	Open        bool
	Kind        referenceRequestKind
	Query       navsearch.SymbolQuery
	Cursor      int
	Top         int
	Results     []navsearch.ReferenceResult
	RawResults  []navsearch.ReferenceResult
	Loading     bool
	Err         error
	Source      navsearch.ResultSource
	Generation  int
	Order       diff.ResultOrder
	ChangedOnly bool
}

type enrichmentPanelState struct {
	Open       bool
	Kind       enrichmentPanelKind
	Title      string
	Query      string
	Cursor     int
	Top        int
	Results    []enrichmentResult
	RawResults []enrichmentResult
	Loading    bool
	Err        error
	Status     lsp.Status
	Generation int
	Order      diff.ResultOrder
}

type enrichmentResult struct {
	Location source.Location
	Side     navsearch.ResultSide
	Label    string
	Preview  string
	Score    int
	Review   diff.ReviewMarkers
}

type fileStateKey struct {
	path string
	mode ViewMode
}

type fileState struct {
	cursor  int
	col     int
	top     int
	topWrap int
}

// diffViewPosition identifies the diff cursor left behind when full-file view
// is collapsed. If it is unchanged on the next expansion, the remembered
// full-file cursor wins; otherwise the moved diff cursor becomes the anchor.
type diffViewPosition struct {
	valid        bool
	cursor       int
	col          int
	kind         ui.RowKind
	hunkIdx      int
	currentLine  int
	baselineLine int
}

// fileViewAnchor is a source-coordinate cursor target captured in diff view.
// Anchors remain pending while the full file is loaded asynchronously.
type fileViewAnchor struct {
	line      int
	col       int
	screenRow int
}

// viewJumpAnchor makes the first full/compact toggle after a result jump use
// the jump destination instead of the other view's cursor saved before the
// jump. Ordinary cursor movement still keeps the two view positions separate.
type viewJumpAnchor struct {
	path     string
	location source.Location
	pending  bool
}

type fileContentState struct {
	lines   []string
	err     error
	loading bool
	loaded  bool
}

// Options carry optional injected components for the model.
type Options struct {
	// LSP is the semantic client; nil keeps enrichments unavailable.
	LSP lsp.Client
	// Highlighter overrides the default (dark, 256-color) highlighter.
	Highlighter *highlight.Highlighter
	// Outline overrides the best-effort lexical structure extractor.
	Outline outline.Extractor
	// FreshSession ignores any stored session state (--fresh).
	FreshSession bool
}

// New returns the initial model for the given diff source.
func New(src diffsource.Source) Model {
	return NewWithOptions(src, Options{})
}

// NewWithLSP returns an initial model with an injected semantic client. Passing
// nil keeps LSP enrichments unavailable and non-fatal.
func NewWithLSP(src diffsource.Source, client lsp.Client) Model {
	return NewWithOptions(src, Options{LSP: client})
}

// NewWithOptions returns an initial model with injected components.
func NewWithOptions(src diffsource.Source, opts Options) Model {
	if opts.LSP == nil {
		opts.LSP = lsp.NewUnavailableClient(lsp.Config{})
	}
	if opts.Highlighter == nil {
		opts.Highlighter = highlight.New()
	}
	if opts.Outline == nil {
		opts.Outline = outline.LexicalExtractor{}
	}
	if src != nil {
		// A lock left by a crashed session must not deadlock the agent.
		clearStaleEditLock(src.Root())
	}
	return Model{
		source:           src,
		hl:               opts.Highlighter,
		lsp:              opts.LSP,
		outlineExtractor: opts.Outline,
		freshSession:     opts.FreshSession,
		changeOrder:      ui.DefaultChangeListOrder,
		loading:          true,
		fileStates:       make(map[fileStateKey]fileState),
		fileContents:     make(map[string]fileContentState),
		localExpansions:  make(map[string]map[int]int),
		diffViewOrigins:  make(map[string]diffViewPosition),
		fileViewAnchors:  make(map[string]fileViewAnchor),
		diagnostics:      make(map[string][]lsp.Diagnostic),
		lspStatuses:      make(map[string]lsp.Status),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadCmdSeq(m.loadSeq), m.startWatchCmd(), m.loadReviewCmd(), m.loadSessionCmd())
}

type diffLoadedMsg struct {
	seq         int
	files       []diff.FileDiff
	fingerprint string
	err         error
}

type fileContentLoadedMsg struct {
	path       string
	generation int
	lines      []string
	err        error
}

type projectFilesLoadedMsg struct {
	files []string
	err   error
}

type searchDebounceMsg struct {
	generation int
	query      string
	regex      bool
}

type searchLoadedMsg struct {
	generation int
	query      string
	regex      bool
	results    []navsearch.Result
	err        error
}

type referenceLoadedMsg struct {
	generation int
	kind       referenceRequestKind
	query      navsearch.SymbolQuery
	source     navsearch.ResultSource
	status     lsp.Status
	results    []navsearch.ReferenceResult
	err        error
}

type enrichmentLoadedMsg struct {
	generation  int
	kind        enrichmentPanelKind
	title       string
	query       string
	path        string
	paths       []string
	results     []enrichmentResult
	diagnostics []lsp.Diagnostic
	status      lsp.Status
	err         error
}

type workspaceSymbolDebounceMsg struct {
	generation int
	query      string
}

type workspaceSymbolsLoadedMsg struct {
	generation int
	query      string
	results    []lsp.WorkspaceSymbol
	status     lsp.Status
	err        error
}

// loadCmdSeq computes and parses the review diff off the UI goroutine. The
// tree fingerprint is captured alongside so drift detection compares against
// what was actually loaded.
func (m Model) loadCmdSeq(seq int) tea.Cmd {
	src := m.source
	return func() tea.Msg {
		raw, err := src.Diff()
		if err != nil {
			return diffLoadedMsg{seq: seq, err: err}
		}
		files, err := diff.ParseReview(raw)
		fingerprint := ""
		if fp, ok := src.(diffsource.Fingerprinter); ok && err == nil {
			fingerprint, _ = fp.Fingerprint()
		}
		return diffLoadedMsg{seq: seq, files: files, fingerprint: fingerprint, err: err}
	}
}

func (m *Model) ensureCurrentFileContentCmd() tea.Cmd {
	if !m.currentFileNeedsContent() || m.source == nil || m.selectedFile < 0 || m.selectedFile >= len(m.files) {
		return nil
	}
	f := m.files[m.selectedFile]
	if f.Binary {
		return nil
	}
	path := f.Path()
	if path == "" {
		return nil
	}
	if m.fileContents == nil {
		m.fileContents = make(map[string]fileContentState)
	}
	if state, ok := m.fileContents[path]; ok && (state.loading || state.loaded || state.err != nil) {
		return nil
	}
	generation := m.contentGeneration
	src := m.source
	m.fileContents[path] = fileContentState{loading: true}
	return func() tea.Msg {
		content, err := src.CurrentContent(path)
		if err != nil {
			return fileContentLoadedMsg{path: path, generation: generation, err: err}
		}
		return fileContentLoadedMsg{path: path, generation: generation, lines: splitContentLines(content)}
	}
}

func (m *Model) loadProjectFilesCmd() tea.Cmd {
	if m.source == nil || m.projectFilesLoading || m.projectFiles != nil {
		return nil
	}
	src := m.source
	m.projectFilesLoading = true
	m.projectFilesErr = nil
	if m.overlay.Kind == OverlayFileOpen {
		m.overlay.Loading = true
		m.overlay.Err = nil
	}
	return func() tea.Msg {
		files, err := src.ProjectFiles()
		return projectFilesLoadedMsg{files: files, err: err}
	}
}

func debounceSearchCmd(generation int, query string, regex bool) tea.Cmd {
	return tea.Tick(searchDebounceDelay, func(time.Time) tea.Msg {
		return searchDebounceMsg{generation: generation, query: query, regex: regex}
	})
}

func searchCmd(src diffsource.Source, generation int, query string, regex bool) tea.Cmd {
	if src == nil {
		return nil
	}
	return func() tea.Msg {
		var results []navsearch.Result
		var err error
		if regex {
			results, err = src.Search(query)
		} else if textSearcher, ok := src.(diffsource.TextSearcher); ok {
			results, err = textSearcher.SearchText(query)
		} else {
			results, err = src.Search(regexp.QuoteMeta(query))
		}
		return searchLoadedMsg{generation: generation, query: query, regex: regex, results: results, err: err}
	}
}

func referenceSearchCmd(src diffsource.Source, client lsp.Client, generation int, kind referenceRequestKind, query navsearch.SymbolQuery) tea.Cmd {
	if src == nil {
		return func() tea.Msg {
			return referenceLoadedMsg{generation: generation, kind: kind, query: query, source: navsearch.ResultSourceLexical, err: errors.New("source unavailable")}
		}
	}
	if client == nil {
		client = lsp.NewUnavailableClient(lsp.Config{})
	}
	return func() tea.Msg {
		var (
			locations []source.Location
			status    lsp.Status
			lspErr    error
		)
		if query.Side != navsearch.ResultSideBaseline {
			if kind == referenceRequestDefinition {
				locations, status, lspErr = client.Definition(query.Location)
			} else {
				locations, status, lspErr = client.References(query.Location, true)
			}
		}
		if lspErr == nil && len(locations) > 0 {
			results := referenceResultsFromLocations(src, query.Symbol, locations, kind == referenceRequestDefinition)
			if kind != referenceRequestDefinition {
				results = expandVTableReferences(src, query.Symbol, results)
			}
			if len(results) > 0 {
				return referenceLoadedMsg{
					generation: generation,
					kind:       kind,
					query:      query,
					source:     navsearch.ResultSourceLSP,
					status:     status,
					results:    results,
				}
			}
		}

		var (
			results []navsearch.Result
			err     error
		)
		switch kind {
		case referenceRequestDefinition:
			results, err = src.Search(navsearch.DefinitionSearchPattern(query.Symbol, query.Location.Path))
			if err == nil {
				return referenceLoadedMsg{
					generation: generation,
					kind:       kind,
					query:      query,
					source:     navsearch.ResultSourceLexical,
					status:     status,
					results:    navsearch.DefinitionResultsFromTextResults(query.Symbol, results, navsearch.ResultSourceLexical),
				}
			}
		default:
			results, err = src.SearchWord(query.Symbol)
			if err == nil {
				references := navsearch.ReferenceResultsFromTextResults(query.Symbol, results, navsearch.ResultSourceLexical)
				references = expandVTableReferences(src, query.Symbol, references)
				return referenceLoadedMsg{
					generation: generation,
					kind:       kind,
					query:      query,
					source:     navsearch.ResultSourceLexical,
					status:     status,
					results:    references,
				}
			}
		}
		return referenceLoadedMsg{generation: generation, kind: kind, query: query, source: navsearch.ResultSourceLexical, status: status, err: err}
	}
}

func expandVTableReferences(src diffsource.Source, symbol string, results []navsearch.ReferenceResult) []navsearch.ReferenceResult {
	slots := navsearch.VTableSlotsForImplementation(symbol, results)
	if len(slots) == 0 {
		return results
	}
	type lineKey struct {
		path     string
		line     int
		baseline bool
	}
	seen := make(map[lineKey]bool, len(results))
	for _, result := range results {
		seen[lineKey{path: result.Location.Path, line: result.Location.Line, baseline: result.Side == navsearch.ResultSideBaseline}] = true
	}
	for _, slot := range slots {
		matches, err := src.SearchWord(slot)
		if err != nil {
			continue
		}
		for _, result := range navsearch.ReferenceResultsFromTextResults(slot, matches, navsearch.ResultSourceLexical) {
			key := lineKey{path: result.Location.Path, line: result.Location.Line, baseline: result.Side == navsearch.ResultSideBaseline}
			if seen[key] {
				continue
			}
			seen[key] = true
			results = append(results, result)
		}
	}
	return results
}

func referenceResultsFromLocations(src diffsource.Source, symbol string, locations []source.Location, definition bool) []navsearch.ReferenceResult {
	contents := make(map[string][]string)
	loaded := make(map[string]bool)
	unavailable := make(map[string]bool)
	seen := make(map[source.Location]bool)
	out := make([]navsearch.ReferenceResult, 0, len(locations))
	for _, loc := range locations {
		if loc.Path == "" || loc.Path == ".." || strings.HasPrefix(loc.Path, "../") || strings.HasPrefix(loc.Path, "/") || seen[loc] {
			continue
		}
		seen[loc] = true
		if !loaded[loc.Path] {
			loaded[loc.Path] = true
			if content, err := src.CurrentContent(loc.Path); err == nil {
				contents[loc.Path] = splitContentLines(content)
			} else {
				unavailable[loc.Path] = true
			}
		}
		if unavailable[loc.Path] {
			continue
		}
		preview := ""
		if lines := contents[loc.Path]; loc.Line >= 1 && loc.Line <= len(lines) {
			preview = lines[loc.Line-1]
		}
		kind := navsearch.ReferenceDefinition
		if !definition {
			kind = navsearch.ClassifyReferenceKind(preview, loc.Path, symbol)
		}
		out = append(out, navsearch.ReferenceResult{
			Location: loc,
			Preview:  preview,
			Kind:     kind,
			Source:   navsearch.ResultSourceLSP,
			Side:     navsearch.ResultSideCurrent,
		})
	}
	return out
}

func diagnosticsCmd(client lsp.Client, generation int, kind enrichmentPanelKind, path string, paths []string) tea.Cmd {
	if client == nil {
		client = lsp.NewUnavailableClient(lsp.Config{})
	}
	return func() tea.Msg {
		var (
			diagnostics []lsp.Diagnostic
			status      lsp.Status
			err         error
		)
		if kind == enrichmentPanelDiagnosticsWorkspace {
			diagnostics, status, err = client.WorkspaceDiagnostics(paths)
		} else {
			diagnostics, status, err = client.Diagnostics(path)
		}
		title := "Diagnostics"
		if kind == enrichmentPanelDiagnosticsWorkspace {
			title = "Workspace diagnostics"
		}
		return enrichmentLoadedMsg{
			generation:  generation,
			kind:        kind,
			title:       title,
			path:        path,
			paths:       paths,
			diagnostics: diagnostics,
			status:      status,
			err:         err,
		}
	}
}

func hoverCmd(client lsp.Client, generation int, query navsearch.SymbolQuery, width int) tea.Cmd {
	if client == nil {
		client = lsp.NewUnavailableClient(lsp.Config{})
	}
	return func() tea.Msg {
		hover, status, err := client.Hover(query.Symbol, query.Location)
		lines := lsp.FormatHover(hover.Contents, 12, max(24, width-8))
		results := make([]enrichmentResult, 0, len(lines))
		for _, line := range lines {
			results = append(results, enrichmentResult{Location: hover.Location, Label: line})
		}
		return enrichmentLoadedMsg{
			generation: generation,
			kind:       enrichmentPanelHover,
			title:      "Hover: " + query.Symbol,
			query:      query.Symbol,
			results:    results,
			status:     status,
			err:        err,
		}
	}
}

func documentSymbolsCmd(client lsp.Client, extractor outline.Extractor, src diffsource.Source, generation int, path string, reviewFiles ...diff.FileDiff) tea.Cmd {
	if client == nil {
		client = lsp.NewUnavailableClient(lsp.Config{})
	}
	if extractor == nil {
		extractor = outline.LexicalExtractor{}
	}
	return func() tea.Msg {
		symbols, status, err := client.DocumentSymbols(path)
		if (err != nil || len(symbols) == 0) && src != nil {
			content, contentErr := src.CurrentContent(path)
			if contentErr == nil {
				lexical, lexicalErr := extractor.Symbols(path, content)
				if lexicalErr == nil {
					symbols, status, err = lexical, lsp.Status{}, nil
				} else {
					err = lexicalErr
				}
			} else if err == nil {
				err = contentErr
			}
		}
		setSymbolPath(symbols, path)
		changes := documentOutlineChanges(src, extractor, path, symbols, reviewFiles)
		flat := lsp.FlattenDocumentSymbols(symbols)
		results := make([]enrichmentResult, 0, len(flat))
		for _, symbol := range flat {
			loc := symbol.SelectionRange.Start
			if loc.Line < 1 {
				loc = symbol.Range.Start
			}
			results = append(results, enrichmentResult{
				Location: loc,
				Label:    lsp.DocumentSymbolLabel(symbol),
				Preview:  symbol.Detail,
				Review:   symbolRangeReview(symbol, changes),
			})
		}
		return enrichmentLoadedMsg{
			generation: generation,
			kind:       enrichmentPanelDocumentSymbols,
			title:      "Document symbols",
			results:    results,
			status:     status,
			err:        err,
		}
	}
}

func documentOutlineChanges(src diffsource.Source, extractor outline.Extractor, path string, symbols []lsp.DocumentSymbol, files []diff.FileDiff) []outline.SymbolChange {
	if src == nil || len(files) == 0 {
		return nil
	}
	var reviewFile *diff.FileDiff
	for i := range files {
		_, newPath := reviewSidePaths(files[i])
		if newPath == path {
			reviewFile = &files[i]
			break
		}
	}
	if reviewFile == nil {
		return nil
	}
	oldPath, newPath := reviewSidePaths(*reviewFile)
	var beforeContent, afterContent []byte
	var before []lsp.DocumentSymbol
	if oldPath != "" {
		beforeContent, _ = src.BaselineContent(oldPath)
		if len(beforeContent) > 0 {
			before, _ = extractor.Symbols(oldPath, beforeContent)
			setSymbolPath(before, oldPath)
		}
	}
	if newPath != "" {
		afterContent, _ = src.CurrentContent(newPath)
	}
	return outline.DiffOutlines(before, symbols, beforeContent, afterContent, oldPath, newPath, []diff.FileDiff{*reviewFile})
}

func symbolRangeReview(symbol lsp.DocumentSymbol, changes []outline.SymbolChange) diff.ReviewMarkers {
	for _, change := range changes {
		if change.After == nil || !sameDocumentSymbol(*change.After, symbol) {
			continue
		}
		return diff.ReviewMarkers{
			ContainsAddition: change.ContainsAddition,
			ContainsDeletion: change.ContainsDeletion,
			EntireAddition:   change.Type == outline.SymbolAdded && change.ContainsAddition,
			EntireDeletion:   change.Type == outline.SymbolRemoved && change.ContainsDeletion,
		}
	}
	return diff.ReviewMarkers{}
}

func sameDocumentSymbol(a, b lsp.DocumentSymbol) bool {
	if a.Name != b.Name || a.Kind != b.Kind {
		return false
	}
	aLoc, bLoc := a.SelectionRange.Start, b.SelectionRange.Start
	if aLoc.Line < 1 {
		aLoc = a.Range.Start
	}
	if bLoc.Line < 1 {
		bLoc = b.Range.Start
	}
	return aLoc.Path == bLoc.Path && aLoc.Line == bLoc.Line && aLoc.Column == bLoc.Column
}

func workspaceSymbolsCmd(client lsp.Client, generation int, query string) tea.Cmd {
	if client == nil {
		client = lsp.NewUnavailableClient(lsp.Config{})
	}
	return func() tea.Msg {
		results, status, err := client.WorkspaceSymbols(query)
		return workspaceSymbolsLoadedMsg{
			generation: generation,
			query:      query,
			results:    results,
			status:     status,
			err:        err,
		}
	}
}

func callHierarchyCmd(client lsp.Client, generation int, kind enrichmentPanelKind, query navsearch.SymbolQuery) tea.Cmd {
	if client == nil {
		client = lsp.NewUnavailableClient(lsp.Config{})
	}
	return func() tea.Msg {
		direction := lsp.CallIncoming
		title := "Incoming calls"
		if kind == enrichmentPanelCallOutgoing {
			direction = lsp.CallOutgoing
			title = "Outgoing calls"
		}
		calls, status, err := client.CallHierarchy(query.Symbol, query.Location, direction)
		results := make([]enrichmentResult, 0, len(calls))
		for _, call := range calls {
			results = append(results, enrichmentResult{
				Location: call.Location,
				Label:    lsp.CallLabel(call),
				Preview:  call.Preview,
			})
		}
		return enrichmentLoadedMsg{
			generation: generation,
			kind:       kind,
			title:      title + ": " + query.Symbol,
			query:      query.Symbol,
			results:    results,
			status:     status,
			err:        err,
		}
	}
}

// Update wraps update so any message that leaves async work in flight keeps
// the spinner ticking without every call site having to remember it.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	nm, ok := next.(Model)
	if !ok {
		return next, cmd
	}
	nm.refreshSearchMatchesIfStale()
	if _, isKey := msg.(tea.KeyMsg); isKey {
		if dirtyCmd := nm.markSessionDirty(); dirtyCmd != nil {
			cmd = tea.Batch(cmd, dirtyCmd)
		}
	}
	if nm.anythingLoading() && !nm.spinnerActive {
		nm.spinnerActive = true
		cmd = tea.Batch(cmd, spinnerTickCmd())
	}
	return nm, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampScroll()
		return m, nil

	case toastExpiredMsg:
		if msg.generation == m.status.generation && !m.status.sticky {
			m.clearToast()
		}
		return m, nil

	case spinnerTickMsg:
		if !m.spinnerActive {
			return m, nil
		}
		if !m.anythingLoading() {
			m.spinnerActive = false
			return m, nil
		}
		m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
		return m, spinnerTickCmd()

	case diffLoadedMsg:
		if msg.seq != m.loadSeq {
			return m, nil // a newer load is in flight
		}
		reloadRequested := m.reloadRequested
		m.reloadRequested = false
		m.loading = false
		m.err = msg.err
		if msg.err != nil {
			return m, nil
		}
		var toastCmd tea.Cmd
		if reloadRequested && !m.status.sticky {
			added, removed, changed := diffDelta(m.files, msg.files)
			toastCmd = m.notify(ui.ToastInfo, reloadToastText(added, removed, changed))
		}
		hadNoFiles := len(m.files) == 0
		pathLost := m.applyReloadedDiff(msg.files)
		m.loadedFingerprint = msg.fingerprint
		m.treeChanged = false
		if m.pendingSeenPath != "" {
			// The reviewer's own save must not flash the file unread.
			if idx := findFileIndexByPath(m.files, m.pendingSeenPath); idx >= 0 {
				if m.seen == nil {
					m.seen = make(map[string]string)
				}
				m.seen[m.pendingSeenPath] = fileDiffHash(m.files[idx])
			}
			m.pendingSeenPath = ""
		}
		m.refreshCommentAnchors()
		sessionSelected := false
		if m.pendingSession != nil && !m.sessionApplied {
			state := *m.pendingSession
			m.pendingSession = nil
			m.sessionApplied = true
			sessionSelected = m.applySession(state)
		}
		m.updateChangeOrder(msg.files)
		if (hadNoFiles && !sessionSelected) || pathLost {
			m.selectFirstDisplayedFile()
		}
		if pathLost && !m.status.sticky {
			toastCmd = m.notify(ui.ToastWarn, "current file left the diff")
		}
		cmd := m.ensureCurrentFileContentCmd()
		outlineCmd := m.loadOutlinesCmd()
		m.clampScroll()
		return m, tea.Batch(cmd, outlineCmd, toastCmd)

	case watchStartedMsg:
		if msg.err != nil || msg.stop == nil {
			// Watch registration failed; degrade to the fingerprint poll.
			return m, m.schedulePollCmd()
		}
		m.watcherActive = true
		m.watchCh = msg.ch
		m.watchStop = msg.stop
		return m, waitWatchCmd(m.watchCh)

	case treeChangedMsg:
		// While an edit buffer is open, reloading would wipe it
		// (applyReloadedDiff drops fileContents); defer until exit. The
		// footer shows the pending reload.
		if m.mode != modeReview {
			m.treeChanged = true
			return m, waitWatchCmd(m.watchCh)
		}
		// Auto-reload silently; state preservation keeps the reviewer's
		// place. Re-arm the watcher wait first so no event is lost.
		cmds := []tea.Cmd{waitWatchCmd(m.watchCh), m.reload(false)}
		return m, tea.Batch(cmds...)

	case pollTickMsg:
		if m.watcherActive {
			return m, nil
		}
		return m, tea.Batch(m.fingerprintCmd(), m.schedulePollCmd())

	case fingerprintMsg:
		if msg.err == nil && msg.generation == m.loadSeq && m.loadedFingerprint != "" && msg.value != m.loadedFingerprint {
			m.treeChanged = true
		}
		return m, nil

	case reviewLoadedMsg:
		if msg.err != nil {
			return m, m.notify(ui.ToastError, "review load: "+msg.err.Error())
		}
		m.review = msg.review
		m.refreshCommentAnchors()
		m.rowsVersion++
		m.clampScroll()
		return m, nil

	case reviewSavedMsg:
		if msg.err != nil {
			return m, m.notify(ui.ToastError, "review save failed: "+msg.err.Error())
		}
		return m, nil

	case reviewExportedMsg:
		if msg.err != nil {
			return m, m.notify(ui.ToastError, "review save failed: "+msg.err.Error())
		}
		return m, m.notify(ui.ToastInfo, "saved "+msg.path)

	case sessionLoadedMsg:
		if msg.err != nil {
			// Corrupt or future-versioned session: start fresh, never exit.
			return m, m.notify(ui.ToastWarn, "session state reset")
		}
		if m.loading {
			state := msg.state
			m.pendingSession = &state
			return m, nil
		}
		if !m.sessionApplied {
			m.sessionApplied = true
			m.applySession(msg.state)
			m.updateChangeOrder(m.files)
		}
		return m, m.ensureCurrentFileContentCmd()

	case sessionSaveTickMsg:
		m.sessionSavePending = false
		if !m.sessionDirty {
			return m, nil
		}
		m.sessionDirty = false
		return m, m.saveSessionCmd()

	case sessionSavedMsg:
		if msg.err != nil {
			return m, m.notify(ui.ToastWarn, "session save failed: "+msg.err.Error())
		}
		return m, nil

	case fileContentLoadedMsg:
		if msg.generation != m.contentGeneration {
			return m, nil
		}
		if m.fileContents == nil {
			m.fileContents = make(map[string]fileContentState)
		}
		m.fileContents[msg.path] = fileContentState{lines: msg.lines, err: msg.err, loaded: true}
		m.rowsVersion++
		if m.pendingEditKind != 0 && msg.path == m.currentFilePath() {
			kind, line, col, screenRow := m.pendingEditKind, m.pendingEditLine, m.pendingEditCol, m.pendingEditRow
			m.pendingEditKind = 0
			if msg.err != nil {
				return m, m.notify(ui.ToastError, "cannot edit: "+msg.err.Error())
			}
			return m, m.completeEditEntry(kind, line, col, screenRow)
		}
		centerPendingLocation := false
		if m.hasPendingLocation && m.pendingLocation.Path == msg.path {
			m.positionCursorAtLocation(m.pendingLocation)
			m.hasPendingLocation = false
			centerPendingLocation = true
		}
		anchoredFileView := m.applyPendingFileViewAnchor()
		if !anchoredFileView {
			m.clampScroll()
		}
		if centerPendingLocation && !anchoredFileView {
			m.centerCursorInViewport()
		}
		return m, nil

	case projectFilesLoadedMsg:
		m.projectFilesLoading = false
		m.projectFilesErr = msg.err
		if msg.err == nil {
			m.projectFiles = msg.files
			if m.projectFiles == nil {
				m.projectFiles = []string{}
			}
		}
		if m.overlay.Kind == OverlayFileOpen {
			m.refreshFileOpenResults()
		}
		return m, nil

	case searchDebounceMsg:
		if m.overlay.Kind != OverlaySearch || msg.generation != m.overlay.Generation || msg.query != m.overlay.Query || msg.regex != m.overlay.SearchRegex {
			return m, nil
		}
		return m, searchCmd(m.source, msg.generation, msg.query, msg.regex)

	case searchLoadedMsg:
		if m.overlay.Kind != OverlaySearch || msg.generation != m.overlay.Generation || msg.query != m.overlay.Query || msg.regex != m.overlay.SearchRegex {
			return m, nil
		}
		m.overlay.Loading = false
		m.overlay.Err = msg.err
		if msg.err != nil {
			m.overlay.Results = nil
			m.overlay.RawResults = nil
		} else {
			m.overlay.RawResults = msg.results
			m.overlay.Results = m.rankOverlayResults(msg.results)
		}
		m.clampOverlayCursor()
		return m, nil

	case referenceLoadedMsg:
		if !m.referencePanel.Open || msg.generation != m.referencePanel.Generation || msg.query.Symbol != m.referencePanel.Query.Symbol {
			return m, nil
		}
		m.recordLSPStatus(msg.status)
		m.referencePanel.Loading = false
		m.referencePanel.Err = msg.err
		m.referencePanel.Source = msg.source
		m.referencePanel.Kind = msg.kind
		if msg.err != nil {
			m.referencePanel.Results = nil
			m.referencePanel.RawResults = nil
			m.clampReferenceCursor()
			return m, m.notify(ui.ToastError, referencePanelErrorText(msg.kind)+": "+msg.err.Error())
		}
		m.referencePanel.RawResults = msg.results
		m.referencePanel.Results = m.rankReferenceResults(msg.results)
		m.clampReferenceCursor()
		if msg.kind == referenceRequestDefinition {
			if len(m.referencePanel.Results) > 0 {
				return m, m.jumpToReferenceResult(m.referencePanel.Results[0])
			}
			return m, m.notify(ui.ToastWarn, "no definition found for "+msg.query.Symbol)
		}
		return m, nil

	case enrichmentLoadedMsg:
		m.recordLSPStatus(msg.status)
		rawResults := msg.results
		if msg.kind == enrichmentPanelDiagnosticsCurrent || msg.kind == enrichmentPanelDiagnosticsWorkspace {
			m.updateDiagnostics(msg)
			msg.results = m.diagnosticPanelResults(msg.diagnostics)
			rawResults = msg.results
		} else if msg.kind != enrichmentPanelHover {
			rawResults = msg.results
			msg.results = m.rankEnrichmentResults(rawResults)
		}
		if !m.enrichmentPanel.Open || msg.generation != m.enrichmentPanel.Generation || msg.kind != m.enrichmentPanel.Kind {
			return m, nil
		}
		m.enrichmentPanel.Loading = false
		m.enrichmentPanel.Err = msg.err
		m.enrichmentPanel.Title = msg.title
		m.enrichmentPanel.Query = msg.query
		m.enrichmentPanel.Results = msg.results
		m.enrichmentPanel.RawResults = rawResults
		m.enrichmentPanel.Status = msg.status
		m.clampEnrichmentCursor()
		return m, nil

	case outlineDiffLoadedMsg:
		if msg.generation != m.outlineGeneration {
			return m, nil
		}
		m.outlineLoading = false
		m.outlineLoaded = true
		m.outlineChanges = msg.changes
		m.outlineCurrent = msg.current
		m.outlineBaseline = msg.baseline
		for _, status := range msg.statuses {
			m.recordLSPStatus(status)
		}
		if m.enrichmentPanel.Open && m.enrichmentPanel.Kind == enrichmentPanelOutlineDiff {
			m.refreshOutlinePanel()
		}
		if m.overlay.Kind == OverlayWorkspaceSymbol {
			for i := range m.overlay.RawResults {
				m.overlay.RawResults[i].Review = m.reviewWithOutlineChange(m.overlay.RawResults[i].Location, "", 0, m.overlay.RawResults[i].Review)
			}
			m.overlay.Results = m.rankOverlayResults(m.overlay.RawResults)
			m.clampOverlayCursor()
		}
		m.clampScroll()
		return m, nil

	case workspaceSymbolDebounceMsg:
		if m.overlay.Kind != OverlayWorkspaceSymbol || msg.generation != m.overlay.Generation || msg.query != m.overlay.Query {
			return m, nil
		}
		return m, workspaceSymbolsCmd(m.lsp, msg.generation, msg.query)

	case workspaceSymbolsLoadedMsg:
		m.recordLSPStatus(msg.status)
		if m.overlay.Kind != OverlayWorkspaceSymbol || msg.generation != m.overlay.Generation || msg.query != m.overlay.Query {
			return m, nil
		}
		m.overlay.Loading = false
		m.overlay.Err = msg.err
		if msg.err != nil {
			m.overlay.Results = nil
			m.overlay.RawResults = nil
		} else {
			m.overlay.RawResults = m.workspaceSymbolOverlayRawResults(msg.results)
			m.overlay.Results = m.rankOverlayResults(m.overlay.RawResults)
		}
		m.clampOverlayCursor()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	if m.composer.open {
		// Non-key messages (cursor blink) keep the textarea animated.
		var cmd tea.Cmd
		m.composer.input, cmd = m.composer.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()

	m.clearStickyToast()

	if m.overlay.Kind != OverlayNone {
		return m.handleOverlayKey(msg)
	}

	// EDIT/INSERT own every key: review bindings are suspended so vim's
	// editing vocabulary applies with its canonical meanings.
	if m.mode == modeInsert {
		return m.handleInsertKey(msg)
	}
	if m.mode == modeEdit {
		if k == "?" && m.pendingFind == 0 && m.pendingReplace == 0 && m.pendingOp == 0 && !m.pendingZUpper {
			m.openCommandPalette()
			return m, nil
		}
		return m.handleEditKey(msg)
	}

	if m.composer.open {
		return m.handleComposerKey(msg)
	}

	if m.search.typing {
		return m.handleSearchTypingKey(msg)
	}

	// The armed find-char target must be consumed before any panel handler
	// can claim the key (panels take J/K/o for list navigation).
	if m.pendingFind != 0 {
		kind := m.pendingFind
		m.pendingFind = 0
		if target, ok := findTargetRune(msg); ok {
			count, _ := m.consumeCount()
			m.findChar(kind, target, count)
			m.clampScroll()
		} else {
			m.countBuf = ""
		}
		return m, nil
	}

	if k == "?" {
		m.openCommandPalette()
		return m, nil
	}

	if k == "ctrl+h" && m.focus != paneList {
		return m, m.executeCommand(commandFocusChangeList, 1, false)
	}

	if m.focus == paneList {
		if next, cmd, handled := m.handleChangeListKey(msg); handled {
			return next, cmd
		}
	}

	if m.enrichmentPanel.Open && m.enrichmentPanel.Kind == enrichmentPanelHover && movementKey(k) {
		m.enrichmentPanel = enrichmentPanelState{}
	}

	if m.enrichmentPanel.Open {
		if next, cmd, handled := m.handleEnrichmentPanelKey(msg); handled {
			return next, cmd
		}
	}

	if m.referencePanel.Open {
		if next, cmd, handled := m.handleReferencePanelKey(msg); handled {
			return next, cmd
		}
	}

	if m.pendingG {
		m.pendingG = false
		count, hasCount := m.consumeCount()
		id := map[string]string{
			"/": commandProjectSearch,
			"r": commandReferences,
			"R": commandReferencesChanged,
			"d": commandGoToDefinition,
			"i": commandImpact,
			"s": commandDocumentSymbols,
			"y": commandOutlineChanges,
			"S": commandWorkspaceSymbols,
			"e": commandDiagnosticsCurrent,
			"E": commandDiagnosticsWorkspace,
			"I": commandCallsIncoming,
			"O": commandCallsOutgoing,
			"?": commandOpenPalette,
		}[k]
		if k == "g" {
			id = commandGoToFileTop
			if hasCount {
				id = commandJumpSourceLine
			}
		}
		if id != "" {
			return m, m.executeCommand(id, count, hasCount)
		}
	}

	if m.pendingZ {
		m.pendingZ = false
		count, _ := m.consumeCount()
		id := map[string]string{
			"o": commandExpandContext,
			"c": commandCollapseContext,
			"O": commandExpandContextAll,
			"C": commandClearContextAll,
			"f": commandToggleFullFile,
			"s": commandToggleSideBySide,
		}[k]
		if id != "" {
			return m, m.executeCommand(id, count, true)
		}
	}

	if m.pendingBracket != 0 {
		dir := m.pendingBracket
		m.pendingBracket = 0
		switch k {
		case "c":
			count, _ := m.consumeCount()
			id := commandNextHunk
			if dir < 0 {
				id = commandPreviousHunk
			}
			return m, m.executeCommand(id, count, true)
		case "a":
			m.countBuf = ""
			id := commandNextAnnotation
			if dir < 0 {
				id = commandPreviousAnnotation
			}
			return m, m.executeCommand(id, 1, false)
		case "]", "[":
			if (dir > 0) == (k == "]") {
				count, _ := m.consumeCount()
				id := commandNextFile
				if dir < 0 {
					id = commandPreviousFile
				}
				return m, m.executeCommand(id, count, true)
			}
		}
		// Any other key cancels the prefix and is handled normally below.
	}

	if m.captureCount(k) {
		return m, nil
	}

	if k == "g" {
		m.pendingG = true
		return m, nil
	}

	if k == "z" {
		m.pendingZ = true
		return m, nil
	}

	if k == "]" || k == "[" {
		m.pendingBracket = 1
		if k == "[" {
			m.pendingBracket = -1
		}
		return m, nil
	}

	// Find-char waits for its target; skipped while the change list has focus
	// so a stray f there cannot swallow the next keypress.
	if m.focus != paneList && (k == "f" || k == "F" || k == "t" || k == "T") {
		id := map[string]string{
			"f": commandFindForward,
			"F": commandFindBackward,
			"t": commandTillForward,
			"T": commandTillBackward,
		}[k]
		return m, m.executeCommand(id, 1, false)
	}

	count, hasCount := m.consumeCount()
	id := map[string]string{
		"q":         commandQuit,
		"ctrl+c":    commandQuit,
		"?":         commandOpenPalette,
		"f1":        commandOpenPalette,
		"ctrl+p":    commandOpenFile,
		"/":         commandSearchCurrentFile,
		"esc":       commandClearSearch,
		"K":         commandHover,
		"ctrl+o":    commandJumpBack,
		"ctrl+]":    commandJumpForward,
		"ctrl+s":    commandExportReview,
		"ctrl+r":    commandReload,
		"j":         commandCursorDown,
		"down":      commandCursorDown,
		"k":         commandCursorUp,
		"up":        commandCursorUp,
		"ctrl+d":    commandScrollHalfPageDown,
		"ctrl+u":    commandScrollHalfPageUp,
		"pgdown":    commandScrollPageDown,
		"ctrl+f":    commandScrollPageDown,
		"pgup":      commandScrollPageUp,
		"ctrl+b":    commandScrollPageUp,
		"H":         commandMoveViewportTop,
		"L":         commandMoveViewportBottom,
		"home":      commandGoToFileTop,
		"G":         commandGoToFileBottom,
		"end":       commandGoToFileBottom,
		"tab":       commandToggleFullFile,
		"n":         commandNextUnreadOrMatch,
		"N":         commandPreviousUnreadOrMatch,
		"shift+tab": commandPreviousUnreadOrMatch,
		"o":         commandToggleFileListOrder,
		"R":         commandMarkCurrentRead,
		"U":         commandMarkCurrentUnread,
		"A":         commandMarkAllRead,
		"c":         commandCommentCurrent,
		"C":         commandCommentGeneral,
		"x":         commandToggleCommentResolved,
		"e":         commandExportReview,
		"}":         commandNextFile,
		"J":         commandNextFile,
		"{":         commandPreviousFile,
		"ctrl+l":    commandFocusDiff,
		"h":         commandCursorLeft,
		"left":      commandCursorLeft,
		"l":         commandCursorRight,
		"right":     commandCursorRight,
		"w":         commandCursorWordForward,
		"b":         commandCursorWordBackward,
		"0":         commandCursorLineStart,
		"^":         commandCursorFirstNonBlank,
		"$":         commandCursorLineEnd,
		"%":         commandJumpMatchingBracket,
		";":         commandRepeatFindForward,
		",":         commandRepeatFindBackward,
		"i":         commandEditInsert,
		"I":         commandEditInsertLineStart,
		"a":         commandEditAppend,
	}[k]
	if k == "G" && hasCount {
		id = commandJumpSourceLine
	}
	return m, m.executeCommand(id, count, hasCount)
}

// findTargetRune extracts the literal rune an f/F/t/T motion should seek.
func findTargetRune(msg tea.KeyMsg) (rune, bool) {
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
		return msg.Runes[0], true
	}
	if msg.Type == tea.KeySpace {
		return ' ', true
	}
	return 0, false
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.overlay.Kind != OverlayNone {
		return m.handleOverlayMouse(msg)
	}
	if msg.Action == tea.MouseActionRelease {
		m.resizingPane = resizeNone
		return m, nil
	}
	if m.resizingPane != resizeNone && msg.Action == tea.MouseActionMotion {
		m.resizePaneAt(msg.X, msg.Y)
		return m, nil
	}
	if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && m.startPaneResize(msg.X, msg.Y) {
		return m, nil
	}

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.handleMouseWheel(-3, msg)
		return m, nil
	case tea.MouseButtonWheelDown:
		m.handleMouseWheel(3, msg)
		return m, nil
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		return m.handleMouseClick(msg)
	default:
		return m, nil
	}
}

func (m *Model) startPaneResize(x, y int) bool {
	layout := m.mainLayout()
	if m.enrichmentPanel.Open || m.referencePanel.Open {
		switch m.resultPanelPlacement {
		case ui.PanelRight:
			if y >= layout.ResultPanelY && y < layout.ResultPanelY+layout.ResultPanelHeight &&
				(x == layout.ResultPanelX || x == layout.ResultPanelX-1) {
				m.resizingPane = resizeResultWidth
				return true
			}
		default:
			if x >= layout.ResultPanelX && x < layout.ResultPanelX+layout.ResultPanelWidth &&
				(y == layout.ResultPanelY || y == layout.ResultPanelY-1) {
				m.resizingPane = resizeResultHeight
				return true
			}
		}
	}
	if y >= layout.BodyY && y < layout.BodyY+layout.BodyHeight &&
		(x == layout.LeftOuterWidth-1 || x == layout.LeftOuterWidth) {
		m.resizingPane = resizeChangeList
		return true
	}
	return false
}

func (m *Model) resizePaneAt(x, y int) {
	switch m.resizingPane {
	case resizeChangeList:
		m.changeListWidth = max(2, x+1)
		m.changeListWidth = m.mainLayout().LeftOuterWidth
	case resizeResultHeight:
		// The footer owns the final terminal row.
		m.resultPanelHeight = max(3, m.height-1-y)
		m.resultPanelHeight = m.mainLayout().ResultPanelHeight
	case resizeResultWidth:
		m.resultPanelWidth = max(2, m.width-x)
		m.resultPanelWidth = m.mainLayout().ResultPanelWidth
	}
	m.clampScroll()
	if m.enrichmentPanel.Open {
		m.clampEnrichmentCursor()
	} else if m.referencePanel.Open {
		m.clampReferenceCursor()
	}
}

func (m Model) handleOverlayMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// The inline symbol choice draws no popup, so there is nothing to click.
	if m.overlay.Kind == OverlaySymbolChoice {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.moveSymbolChoiceCursor(-1)
		case tea.MouseButtonWheelDown:
			m.moveSymbolChoiceCursor(1)
		}
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.moveOverlayCursor(-3)
	case tea.MouseButtonWheelDown:
		m.moveOverlayCursor(3)
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		if m.overlay.Kind == OverlayCommandPalette {
			if idx := ui.OverlayTabIndexAt(m.overlayView(), m.width, m.height, msg.X, msg.Y); idx >= 0 {
				m.setCommandPaletteCategory(idx)
				return m, nil
			}
		}
		idx := ui.OverlayResultIndexAt(m.overlayView(), m.width, m.height, msg.X, msg.Y)
		if idx < 0 {
			return m, nil
		}
		// First click selects; clicking the selected row accepts it.
		if idx == m.overlay.Cursor {
			return m, m.acceptOverlayResult()
		}
		m.overlay.Cursor = idx
		m.clampOverlayCursor()
	}
	return m, nil
}

func (m *Model) handleMouseWheel(delta int, msg tea.MouseMsg) {
	layout := m.mainLayout()
	if msg.X >= layout.ResultPanelX && msg.X < layout.ResultPanelX+layout.ResultPanelWidth &&
		msg.Y >= layout.ResultPanelY && msg.Y < layout.ResultPanelY+layout.ResultPanelHeight {
		m.handleBottomPanelWheel(delta)
		return
	}
	m.windowScroll(delta)
	m.clampScroll()
}

func (m *Model) handleBottomPanelWheel(delta int) {
	if m.enrichmentPanel.Open {
		m.moveEnrichmentCursor(delta)
		return
	}
	if m.referencePanel.Open {
		m.moveReferenceCursor(delta)
	}
}

func (m Model) handleMouseClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	layout := m.mainLayout()

	if msg.X >= layout.ResultPanelX && msg.X < layout.ResultPanelX+layout.ResultPanelWidth &&
		msg.Y >= layout.ResultPanelY && msg.Y < layout.ResultPanelY+layout.ResultPanelHeight {
		return m.handleBottomPanelClick(msg.Y - layout.ResultPanelY - 1)
	}

	if msg.Y < layout.ContentY || msg.Y >= layout.ContentY+layout.ContentHeight {
		return m, nil
	}

	if msg.X >= 0 && msg.X < layout.LeftOuterWidth {
		return m.handleChangeListClick(msg.Y - layout.ContentY)
	}

	if msg.X >= layout.DiffContentX && msg.X < layout.DiffContentX+layout.DiffContentWidth &&
		msg.Y >= layout.DiffRowsY && msg.Y < layout.DiffRowsY+layout.DiffRowsHeight {
		rows := m.currentRows()
		if len(rows) == 0 {
			return m, nil
		}
		m.focusDiff()
		l := m.layoutFor(rows)
		screenIdx := m.topScreenLine(l) + msg.Y - layout.DiffRowsY
		if screenIdx < 0 || screenIdx >= l.TotalLines() {
			return m, nil
		}
		m.cursor = l.LineAt(screenIdx).RowIdx
		if m.cursor >= 0 && m.cursor < len(rows) && rows[m.cursor].Kind == ui.RowPair {
			if left, ok := splitSideForClick(msg.X-layout.DiffContentX, layout.DiffContentWidth); ok {
				m.setSplitActiveSide(left)
			}
		}
		m.clampScroll()
	}
	return m, nil
}

// handleChangeListClick resolves a click on the change list: files select and
// open, directory rows toggle collapse.
func (m Model) handleChangeListClick(line int) (tea.Model, tea.Cmd) {
	view := m.changeListView()
	idx := view.RowAt(line)
	if idx < 0 {
		return m, nil
	}
	if m.focus == paneList {
		m.listCursor = idx
		m.syncChangeListScroll()
	}
	row := view.Rows[idx]
	if row.IsDir {
		m.toggleDirCollapsed(row.Path)
		m.syncChangeListScroll()
		return m, nil
	}
	cmd := m.openFileFromList(row.FileIdx)
	return m, cmd
}

// handleBottomPanelClick selects and jumps on a clicked result row.
func (m Model) handleBottomPanelClick(innerY int) (tea.Model, tea.Cmd) {
	panel := m.bottomPanelView()
	if panel == nil {
		return m, nil
	}
	idx := ui.BottomPanelResultIndexAt(*panel, m.height, innerY)
	if idx < 0 {
		return m, nil
	}
	if m.enrichmentPanel.Open {
		if m.enrichmentPanel.Kind == enrichmentPanelHover {
			return m, nil
		}
		m.enrichmentPanel.Cursor = idx
		m.clampEnrichmentCursor()
		return m, m.acceptEnrichmentResult()
	}
	if m.referencePanel.Open {
		m.referencePanel.Cursor = idx
		m.clampReferenceCursor()
		return m, m.acceptReferenceResult()
	}
	return m, nil
}

func (m Model) handleReferencePanelKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		return m, m.executeCommand(commandCloseActivePanel, 1, false), true
	case "enter":
		return m, m.acceptReferenceResult(), true
	case "up", "K", "ctrl+p":
		m.moveReferenceCursor(-1)
		return m, nil, true
	case "down", "J", "ctrl+n":
		m.moveReferenceCursor(1)
		return m, nil, true
	case "pgup":
		m.pageReferenceCursor(-1)
		return m, nil, true
	case "pgdown":
		m.pageReferenceCursor(1)
		return m, nil, true
	case "o":
		return m, m.executeCommand(commandToggleResultOrder, 1, false), true
	case "ctrl+w":
		return m, m.executeCommand(commandToggleResultDock, 1, false), true
	default:
		return m, nil, false
	}
}

func (m Model) handleEnrichmentPanelKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		return m, m.executeCommand(commandCloseActivePanel, 1, false), true
	case "enter":
		if m.enrichmentPanel.Kind == enrichmentPanelHover {
			return m, nil, true
		}
		return m, m.acceptEnrichmentResult(), true
	case "up", "K", "ctrl+p":
		if m.enrichmentPanel.Kind == enrichmentPanelHover {
			return m, nil, false
		}
		m.moveEnrichmentCursor(-1)
		return m, nil, true
	case "down", "J", "ctrl+n":
		if m.enrichmentPanel.Kind == enrichmentPanelHover {
			return m, nil, false
		}
		m.moveEnrichmentCursor(1)
		return m, nil, true
	case "pgup":
		m.pageEnrichmentCursor(-1)
		return m, nil, true
	case "pgdown":
		m.pageEnrichmentCursor(1)
		return m, nil, true
	case "o":
		if m.enrichmentPanel.Kind == enrichmentPanelHover {
			return m, nil, false
		}
		return m, m.executeCommand(commandToggleResultOrder, 1, false), true
	case "ctrl+w":
		return m, m.executeCommand(commandToggleResultDock, 1, false), true
	case "s":
		if m.enrichmentPanel.Kind != enrichmentPanelOutlineDiff {
			return m, nil, false
		}
		return m, m.executeCommand(commandToggleOutlineScope, 1, false), true
	default:
		return m, nil, false
	}
}

func movementKey(k string) bool {
	switch k {
	case "j", "k", "down", "up", "ctrl+d", "ctrl+u", "pgdown", "pgup", "ctrl+f", "ctrl+b", "H", "L", "home", "end", "G", "tab", "n", "N", "shift+tab", "}", "J", "]", "{", "K", "[",
		"h", "l", "left", "right", "w", "b", "0", "^", "$", "%", ";", ",":
		return true
	default:
		return false
	}
}

func (m Model) handleOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if m.overlay.Kind == OverlayCommandPalette {
		switch k {
		case "esc", "?":
			m.closeOverlay()
			return m, nil
		case "ctrl+c":
			return m, m.executeCommand(commandQuit, 1, false)
		case "enter":
			return m, m.acceptOverlayResult()
		case "backspace":
			return m.updateOverlayQuery(dropLastRune(m.overlay.Query))
		case "ctrl+u":
			return m.updateOverlayQuery("")
		case "tab", "right":
			m.stepCommandPaletteCategory(1)
			return m, nil
		case "shift+tab", "left":
			m.stepCommandPaletteCategory(-1)
			return m, nil
		case "up", "ctrl+p":
			m.moveOverlayCursor(-1)
			return m, nil
		case "down", "ctrl+n":
			m.moveOverlayCursor(1)
			return m, nil
		case "pgup":
			m.pageOverlayCursor(-1)
			return m, nil
		case "pgdown":
			m.pageOverlayCursor(1)
			return m, nil
		case "home":
			m.overlay.Cursor = 0
			m.clampOverlayCursor()
			return m, nil
		case "end", "G":
			m.overlay.Cursor = len(m.overlay.Results) - 1
			m.clampOverlayCursor()
			return m, nil
		}
		if msg.Type == tea.KeyRunes {
			return m.updateOverlayQuery(m.overlay.Query + string(msg.Runes))
		}
		return m, nil
	}
	if m.overlay.Kind == OverlaySymbolChoice {
		return m.handleSymbolChoiceKey(msg)
	}
	switch k {
	case "ctrl+c":
		m.stopWatching()
		m.saveSessionNow()
		return m, tea.Quit
	case "esc":
		m.closeOverlay()
		return m, nil
	case "enter":
		if m.overlay.Kind == OverlaySearch && m.overlay.Query != "" && m.overlay.Loading && len(m.overlay.Results) == 0 {
			return m, m.runProjectSearchNow()
		}
		cmd := m.acceptOverlayResult()
		return m, cmd
	case "up", "ctrl+p":
		m.moveOverlayCursor(-1)
		return m, nil
	case "down", "ctrl+n":
		m.moveOverlayCursor(1)
		return m, nil
	case "shift+tab":
		m.moveOverlayCursor(-1)
		return m, nil
	case "tab":
		m.moveOverlayCursor(1)
		return m, nil
	case "pgup":
		m.pageOverlayCursor(-1)
		return m, nil
	case "pgdown":
		m.pageOverlayCursor(1)
		return m, nil
	case "backspace":
		if m.overlay.QuerySelected {
			return m.updateOverlayQuery("")
		}
		return m.updateOverlayQuery(dropLastRune(m.overlay.Query))
	case "ctrl+u":
		return m.updateOverlayQuery("")
	case "ctrl+w":
		if m.overlay.QuerySelected {
			return m.updateOverlayQuery("")
		}
		return m.updateOverlayQuery(dropLastWord(m.overlay.Query))
	case "ctrl+o":
		m.toggleOverlayOrder()
		return m, nil
	case "ctrl+r":
		if m.overlay.Kind == OverlaySearch {
			return m, m.toggleProjectSearchRegex()
		}
		return m, nil
	}

	if msg.Type == tea.KeyRunes {
		if m.overlay.QuerySelected {
			return m.updateOverlayQuery(string(msg.Runes))
		}
		return m.updateOverlayQuery(m.overlay.Query + string(msg.Runes))
	}
	return m, nil
}

func (m Model) View() string {
	switch {
	case m.width == 0:
		return "loading…"
	case m.err != nil:
		return "cride error:\n\n  " + m.err.Error() + "\n\n  press ^R to retry, q to quit"
	case m.loading:
		return m.spinnerFrameString() + " loading diff…"
	}
	listView := m.changeListView()
	// Symbol choice highlights candidates in-line via match spans; it renders
	// no popup even though it captures keys like an overlay. The character
	// cursor goes last so its cell stays visible on top of match styling.
	matches := append(m.uiMatchSpans(), m.symbolChoiceSpans()...)
	matches = append(matches, m.cursorSpan()...)
	out := ui.RenderWithOptions(m.files, m.renderRows(), m.selectedFile, m.cursor, m.top, m.width, m.height, m.hl, m.source.Baseline(), m.viewMode == ViewFile, m.bottomPanelView(), ui.RenderOptions{
		LSPStatus:       m.semanticStatusLine(),
		TopWrap:         m.topWrap,
		Footer:          m.footerView(),
		Matches:         matches,
		ChangeList:      &listView,
		ChangeListWidth: m.changeListWidth,
		Composer:        m.composerView(),
		Breadcrumb:      m.outlineBreadcrumb(),
		ShowBreadcrumb:  m.showOutlineBreadcrumb(),
	})
	if m.overlay.Kind != OverlayNone && m.overlay.Kind != OverlaySymbolChoice {
		out = ui.RenderOverlay(out, m.overlayView(), m.width, m.height)
	}
	return out
}

// --- navigation helpers (pointer receivers; m is addressable in Update) ---

func (m *Model) openFileOverlay() {
	m.countBuf = ""
	m.pendingG = false
	m.overlay = overlayState{Kind: OverlayFileOpen, Order: diff.ResultOrderReview}
	m.refreshFileOpenResults()
}

func (m *Model) openSearchOverlay() tea.Cmd {
	m.countBuf = ""
	m.pendingG = false
	m.searchGeneration++
	memo := m.projectSearch
	m.overlay = overlayState{
		Kind:          OverlaySearch,
		Query:         memo.Query,
		Cursor:        memo.Cursor,
		Top:           memo.Top,
		Generation:    m.searchGeneration,
		Order:         memo.Order,
		SearchRegex:   memo.Regex,
		QuerySelected: memo.Query != "",
	}
	if memo.Query == "" {
		return nil
	}
	m.overlay.Loading = true
	return searchCmd(m.source, m.overlay.Generation, memo.Query, memo.Regex)
}

func (m *Model) openWorkspaceSymbolOverlay() {
	m.countBuf = ""
	m.pendingG = false
	m.searchGeneration++
	m.referencePanel = referencePanelState{}
	m.enrichmentPanel = enrichmentPanelState{}
	m.overlay = overlayState{Kind: OverlayWorkspaceSymbol, Generation: m.searchGeneration, Order: diff.ResultOrderReview}
}

func (m *Model) openCommandPalette() {
	m.startCommandPalette()
	results := commandPaletteResults(m.overlay.CommandCategory, "")
	m.overlay.Results = results
	m.overlay.RawResults = results
}

func (m *Model) startCommandPalette() {
	m.countBuf = ""
	m.pendingG = false
	m.pendingZ = false
	m.pendingBracket = 0
	category := CommandCategoryCode
	if m.mode == modeEdit {
		category = CommandCategoryEdit
	}
	m.overlay = overlayState{Kind: OverlayCommandPalette, CommandCategory: category}
}

func (m *Model) openReferencesPanel(kind referenceRequestKind, changedOnly bool) tea.Cmd {
	m.countBuf = ""
	m.pendingG = false
	m.enrichmentPanel = enrichmentPanelState{}
	queries, ok := m.currentSymbolQueries()
	if !ok {
		return m.openReferencesPanelError(kind, changedOnly)
	}
	if len(queries) > 1 {
		m.openReferenceSymbolChoice(kind, changedOnly, queries)
		m.clampScroll()
		return nil
	}
	return m.openReferencesPanelForQuery(kind, changedOnly, queries[0])
}

func (m *Model) openReferencesPanelError(kind referenceRequestKind, changedOnly bool) tea.Cmd {
	m.referenceGeneration++
	m.referencePanel = referencePanelState{
		Open:        true,
		Kind:        kind,
		Source:      navsearch.ResultSourceLexical,
		Generation:  m.referenceGeneration,
		Order:       diff.ResultOrderReview,
		ChangedOnly: changedOnly,
	}
	m.referencePanel.Err = errors.New("no source symbol on current row")
	m.clampScroll()
	return nil
}

func (m *Model) openReferencesPanelForQuery(kind referenceRequestKind, changedOnly bool, query navsearch.SymbolQuery) tea.Cmd {
	m.referenceGeneration++
	m.referencePanel = referencePanelState{
		Open:        true,
		Kind:        kind,
		Query:       query,
		Source:      navsearch.ResultSourceLexical,
		Generation:  m.referenceGeneration,
		Order:       diff.ResultOrderReview,
		ChangedOnly: changedOnly,
	}
	m.referencePanel.Loading = true
	m.clampScroll()
	return referenceSearchCmd(m.source, m.lsp, m.referencePanel.Generation, kind, query)
}

func (m *Model) openDiagnosticsPanel(workspace bool) tea.Cmd {
	m.countBuf = ""
	m.pendingG = false
	m.referencePanel = referencePanelState{}
	m.enrichmentGeneration++
	kind := enrichmentPanelDiagnosticsCurrent
	title := "Diagnostics"
	path := m.currentFilePath()
	paths := []string{path}
	if workspace {
		kind = enrichmentPanelDiagnosticsWorkspace
		title = "Workspace diagnostics"
		paths = m.changedFilePaths()
	}
	m.enrichmentPanel = enrichmentPanelState{
		Open:       true,
		Kind:       kind,
		Title:      title,
		Loading:    true,
		Generation: m.enrichmentGeneration,
		Order:      diff.ResultOrderReview,
	}
	if !workspace && path == "" {
		m.enrichmentPanel.Loading = false
		m.enrichmentPanel.Err = errors.New("no current file")
		return nil
	}
	if workspace && len(paths) == 0 {
		m.enrichmentPanel.Loading = false
		m.enrichmentPanel.Results = nil
		return nil
	}
	m.clampScroll()
	return diagnosticsCmd(m.lsp, m.enrichmentPanel.Generation, kind, path, paths)
}

func (m *Model) openHoverPanel() tea.Cmd {
	m.countBuf = ""
	m.pendingG = false
	m.referencePanel = referencePanelState{}
	query, ok := m.currentSymbolQuery()
	m.enrichmentGeneration++
	m.enrichmentPanel = enrichmentPanelState{
		Open:       true,
		Kind:       enrichmentPanelHover,
		Title:      "Hover",
		Generation: m.enrichmentGeneration,
		Order:      diff.ResultOrderReview,
	}
	if !ok {
		m.enrichmentPanel.Err = errors.New("no source symbol on current row")
		return nil
	}
	m.enrichmentPanel.Title = "Hover: " + query.Symbol
	m.enrichmentPanel.Query = query.Symbol
	m.enrichmentPanel.Loading = true
	m.clampScroll()
	return hoverCmd(m.lsp, m.enrichmentPanel.Generation, query, m.width)
}

func (m *Model) openDocumentSymbolsPanel() tea.Cmd {
	m.countBuf = ""
	m.pendingG = false
	m.referencePanel = referencePanelState{}
	m.enrichmentGeneration++
	path := m.currentFilePath()
	m.enrichmentPanel = enrichmentPanelState{
		Open:       true,
		Kind:       enrichmentPanelDocumentSymbols,
		Title:      "Document symbols",
		Loading:    true,
		Generation: m.enrichmentGeneration,
		Order:      diff.ResultOrderReview,
	}
	if path == "" {
		m.enrichmentPanel.Loading = false
		m.enrichmentPanel.Err = errors.New("no current file")
		return nil
	}
	m.clampScroll()
	return documentSymbolsCmd(m.lsp, m.outlineExtractor, m.source, m.enrichmentPanel.Generation, path, m.files...)
}

func (m *Model) openCallHierarchyPanel(kind enrichmentPanelKind) tea.Cmd {
	m.countBuf = ""
	m.pendingG = false
	m.referencePanel = referencePanelState{}
	query, ok := m.currentSymbolQuery()
	m.enrichmentGeneration++
	title := "Incoming calls"
	if kind == enrichmentPanelCallOutgoing {
		title = "Outgoing calls"
	}
	m.enrichmentPanel = enrichmentPanelState{
		Open:       true,
		Kind:       kind,
		Title:      title,
		Generation: m.enrichmentGeneration,
		Order:      diff.ResultOrderReview,
	}
	if !ok {
		m.enrichmentPanel.Err = errors.New("no source symbol on current row")
		return nil
	}
	m.enrichmentPanel.Title = title + ": " + query.Symbol
	m.enrichmentPanel.Query = query.Symbol
	m.enrichmentPanel.Loading = true
	m.clampScroll()
	return callHierarchyCmd(m.lsp, m.enrichmentPanel.Generation, kind, query)
}

func (m *Model) closeOverlay() {
	if m.overlay.Kind == OverlaySearch {
		m.projectSearch = projectSearchMemo{
			Query:  m.overlay.Query,
			Cursor: m.overlay.Cursor,
			Top:    m.overlay.Top,
			Order:  m.overlay.Order,
			Regex:  m.overlay.SearchRegex,
		}
	}
	m.overlay = overlayState{}
}

func (m Model) updateOverlayQuery(query string) (tea.Model, tea.Cmd) {
	m.overlay.Query = query
	m.overlay.QuerySelected = false
	m.overlay.Cursor = 0
	m.overlay.Top = 0
	m.overlay.Err = nil
	switch m.overlay.Kind {
	case OverlayFileOpen:
		m.refreshFileOpenResults()
		return m, nil
	case OverlaySearch:
		m.searchGeneration++
		m.overlay.Generation = m.searchGeneration
		if query == "" {
			m.overlay.Loading = false
			m.overlay.Results = nil
			m.overlay.RawResults = nil
			return m, nil
		}
		m.overlay.Loading = true
		m.overlay.Results = nil
		m.overlay.RawResults = nil
		return m, debounceSearchCmd(m.overlay.Generation, query, m.overlay.SearchRegex)
	case OverlayWorkspaceSymbol:
		m.searchGeneration++
		m.overlay.Generation = m.searchGeneration
		if query == "" {
			m.overlay.Loading = false
			m.overlay.Results = nil
			m.overlay.RawResults = nil
			return m, nil
		}
		m.overlay.Loading = true
		m.overlay.Results = nil
		m.overlay.RawResults = nil
		return m, tea.Tick(searchDebounceDelay, func(time.Time) tea.Msg {
			return workspaceSymbolDebounceMsg{generation: m.overlay.Generation, query: query}
		})
	case OverlayCommandPalette:
		m.refreshCommandPaletteResults()
		return m, nil
	default:
		return m, nil
	}
}

func (m *Model) runProjectSearchNow() tea.Cmd {
	if m.overlay.Kind != OverlaySearch || m.overlay.Query == "" {
		return nil
	}
	m.searchGeneration++
	m.overlay.Generation = m.searchGeneration
	m.overlay.Loading = true
	m.overlay.Err = nil
	m.overlay.Results = nil
	m.overlay.RawResults = nil
	return searchCmd(m.source, m.overlay.Generation, m.overlay.Query, m.overlay.SearchRegex)
}

func (m *Model) toggleProjectSearchRegex() tea.Cmd {
	if m.overlay.Kind != OverlaySearch {
		return nil
	}
	m.overlay.SearchRegex = !m.overlay.SearchRegex
	if m.overlay.Query == "" {
		m.overlay.Err = nil
		return nil
	}
	return m.runProjectSearchNow()
}

func (m *Model) refreshFileOpenResults() {
	if m.overlay.Kind != OverlayFileOpen {
		return
	}
	m.overlay.Loading = m.projectFilesLoading
	m.overlay.Err = m.projectFilesErr
	if m.projectFilesErr != nil || m.projectFilesLoading {
		m.overlay.Results = nil
		return
	}
	m.overlay.Results = navsearch.RankFiles(m.projectFiles, m.overlay.Query, m.reviewChangedPaths(), recentPathRanks(m.recentPaths), overlayResultLimit)
	review := m.reviewIndex()
	for i := range m.overlay.Results {
		loc := m.overlay.Results[i].Location
		m.overlay.Results[i].Review = diff.MarkersForIndex(review, loc.Path, loc.Line)
	}
	m.clampOverlayCursor()
}

func (m *Model) refreshCommandPaletteResults() {
	if m.overlay.Kind != OverlayCommandPalette {
		return
	}
	m.overlay.RawResults = commandPaletteResults(m.overlay.CommandCategory, "")
	m.overlay.Results = commandPaletteResults(m.overlay.CommandCategory, m.overlay.Query)
	m.clampOverlayCursor()
}

func (m *Model) stepCommandPaletteCategory(delta int) {
	if m.overlay.Kind != OverlayCommandPalette || len(commandPaletteCategories) == 0 {
		return
	}
	current := 0
	for i, category := range commandPaletteCategories {
		if category == m.overlay.CommandCategory {
			current = i
			break
		}
	}
	next := (current + delta) % len(commandPaletteCategories)
	if next < 0 {
		next += len(commandPaletteCategories)
	}
	m.setCommandPaletteCategory(next)
}

func (m *Model) setCommandPaletteCategory(index int) {
	if m.overlay.Kind != OverlayCommandPalette || index < 0 || index >= len(commandPaletteCategories) {
		return
	}
	m.overlay.CommandCategory = commandPaletteCategories[index]
	m.overlay.Cursor = 0
	m.overlay.Top = 0
	m.refreshCommandPaletteResults()
}

func (m *Model) clampOverlayCursor() {
	if len(m.overlay.Results) == 0 {
		m.overlay.Cursor = 0
		m.overlay.Top = 0
		return
	}
	m.overlay.Cursor = min(max(m.overlay.Cursor, 0), len(m.overlay.Results)-1)
	page := m.overlayPageSize()
	maxTop := max(0, len(m.overlay.Results)-page)
	if m.overlay.Cursor < m.overlay.Top {
		m.overlay.Top = m.overlay.Cursor
	}
	if m.overlay.Cursor >= m.overlay.Top+page {
		m.overlay.Top = m.overlay.Cursor - page + 1
	}
	m.overlay.Top = min(max(m.overlay.Top, 0), maxTop)
}

func (m *Model) moveOverlayCursor(delta int) {
	if len(m.overlay.Results) == 0 {
		m.overlay.Cursor = 0
		m.overlay.Top = 0
		return
	}
	m.overlay.Cursor = min(max(m.overlay.Cursor+delta, 0), len(m.overlay.Results)-1)
	m.clampOverlayCursor()
}

func (m *Model) pageOverlayCursor(delta int) {
	if len(m.overlay.Results) == 0 {
		m.overlay.Cursor = 0
		m.overlay.Top = 0
		return
	}
	page := m.overlayPageSize()
	offset := min(max(m.overlay.Cursor-m.overlay.Top, 0), page-1)
	maxTop := max(0, len(m.overlay.Results)-page)
	m.overlay.Top = min(max(m.overlay.Top+delta*page, 0), maxTop)
	m.overlay.Cursor = min(max(m.overlay.Top+offset, 0), len(m.overlay.Results)-1)
	m.clampOverlayCursor()
}

func (m *Model) clampReferenceCursor() {
	if len(m.referencePanel.Results) == 0 {
		m.referencePanel.Cursor = 0
		m.referencePanel.Top = 0
		return
	}
	m.referencePanel.Cursor = min(max(m.referencePanel.Cursor, 0), len(m.referencePanel.Results)-1)
	page := m.referencePageSize()
	maxTop := max(0, len(m.referencePanel.Results)-page)
	if m.referencePanel.Cursor < m.referencePanel.Top {
		m.referencePanel.Top = m.referencePanel.Cursor
	}
	if m.referencePanel.Cursor >= m.referencePanel.Top+page {
		m.referencePanel.Top = m.referencePanel.Cursor - page + 1
	}
	m.referencePanel.Top = min(max(m.referencePanel.Top, 0), maxTop)
}

func (m *Model) moveReferenceCursor(delta int) {
	if len(m.referencePanel.Results) == 0 {
		m.referencePanel.Cursor = 0
		m.referencePanel.Top = 0
		return
	}
	m.referencePanel.Cursor = min(max(m.referencePanel.Cursor+delta, 0), len(m.referencePanel.Results)-1)
	m.clampReferenceCursor()
}

func (m *Model) pageReferenceCursor(delta int) {
	if len(m.referencePanel.Results) == 0 {
		m.referencePanel.Cursor = 0
		m.referencePanel.Top = 0
		return
	}
	page := m.referencePageSize()
	offset := min(max(m.referencePanel.Cursor-m.referencePanel.Top, 0), page-1)
	maxTop := max(0, len(m.referencePanel.Results)-page)
	m.referencePanel.Top = min(max(m.referencePanel.Top+delta*page, 0), maxTop)
	m.referencePanel.Cursor = min(max(m.referencePanel.Top+offset, 0), len(m.referencePanel.Results)-1)
	m.clampReferenceCursor()
}

func (m *Model) clampEnrichmentCursor() {
	if len(m.enrichmentPanel.Results) == 0 {
		m.enrichmentPanel.Cursor = 0
		m.enrichmentPanel.Top = 0
		return
	}
	m.enrichmentPanel.Cursor = min(max(m.enrichmentPanel.Cursor, 0), len(m.enrichmentPanel.Results)-1)
	page := m.enrichmentPageSize()
	maxTop := max(0, len(m.enrichmentPanel.Results)-page)
	if m.enrichmentPanel.Cursor < m.enrichmentPanel.Top {
		m.enrichmentPanel.Top = m.enrichmentPanel.Cursor
	}
	if m.enrichmentPanel.Cursor >= m.enrichmentPanel.Top+page {
		m.enrichmentPanel.Top = m.enrichmentPanel.Cursor - page + 1
	}
	m.enrichmentPanel.Top = min(max(m.enrichmentPanel.Top, 0), maxTop)
}

func (m *Model) moveEnrichmentCursor(delta int) {
	if len(m.enrichmentPanel.Results) == 0 {
		m.enrichmentPanel.Cursor = 0
		m.enrichmentPanel.Top = 0
		return
	}
	m.enrichmentPanel.Cursor = min(max(m.enrichmentPanel.Cursor+delta, 0), len(m.enrichmentPanel.Results)-1)
	m.clampEnrichmentCursor()
}

func (m *Model) pageEnrichmentCursor(delta int) {
	if len(m.enrichmentPanel.Results) == 0 {
		m.enrichmentPanel.Cursor = 0
		m.enrichmentPanel.Top = 0
		return
	}
	page := m.enrichmentPageSize()
	offset := min(max(m.enrichmentPanel.Cursor-m.enrichmentPanel.Top, 0), page-1)
	maxTop := max(0, len(m.enrichmentPanel.Results)-page)
	m.enrichmentPanel.Top = min(max(m.enrichmentPanel.Top+delta*page, 0), maxTop)
	m.enrichmentPanel.Cursor = min(max(m.enrichmentPanel.Top+offset, 0), len(m.enrichmentPanel.Results)-1)
	m.clampEnrichmentCursor()
}

func (m *Model) toggleOverlayOrder() {
	if m.overlay.Kind != OverlaySearch && m.overlay.Kind != OverlayWorkspaceSymbol {
		return
	}
	m.overlay.Order = nextResultOrder(m.overlay.Order)
	m.overlay.Results = m.rankOverlayResults(m.overlay.RawResults)
	m.clampOverlayCursor()
}

func (m *Model) toggleReferenceOrder() {
	m.referencePanel.Order = nextResultOrder(m.referencePanel.Order)
	m.referencePanel.Results = m.rankReferenceResults(m.referencePanel.RawResults)
	m.clampReferenceCursor()
}

func (m *Model) toggleEnrichmentOrder() {
	m.enrichmentPanel.Order = nextResultOrder(m.enrichmentPanel.Order)
	m.enrichmentPanel.Results = m.rankEnrichmentResults(m.enrichmentPanel.RawResults)
	m.clampEnrichmentCursor()
}

func (m *Model) toggleResultPanelDock() {
	if !m.enrichmentPanel.Open && !m.referencePanel.Open {
		return
	}
	if m.resultPanelPlacement == ui.PanelRight {
		m.resultPanelPlacement = ui.PanelBottom
	} else {
		m.resultPanelPlacement = ui.PanelRight
	}
	m.resizingPane = resizeNone
	m.clampScroll()
	if m.enrichmentPanel.Open {
		m.clampEnrichmentCursor()
	} else {
		m.clampReferenceCursor()
	}
}

func nextResultOrder(order diff.ResultOrder) diff.ResultOrder {
	if order == diff.ResultOrderSource {
		return diff.ResultOrderReview
	}
	return diff.ResultOrderSource
}

func (m Model) rankOverlayResults(results []navsearch.Result) []navsearch.Result {
	if len(results) == 0 {
		return nil
	}
	ranked := navsearch.RankTextResultsWithReview(results, m.currentReviewLocation(), m.reviewIndex(), m.overlay.Order, overlayResultLimit)
	return ranked
}

func (m Model) rankReferenceResults(results []navsearch.ReferenceResult) []navsearch.ReferenceResult {
	if len(results) == 0 {
		return nil
	}
	filtered := results
	if m.referencePanel.ChangedOnly {
		review := m.reviewIndex()
		filtered = make([]navsearch.ReferenceResult, 0, len(results))
		for _, result := range results {
			if review.IsChanged(result.Location.Path) {
				filtered = append(filtered, result)
			}
		}
	}
	return navsearch.RankReferenceResultsWithReview(filtered, m.currentReviewLocation(), m.reviewIndex(), m.referencePanel.Order, overlayResultLimit)
}

func (m Model) rankEnrichmentResults(results []enrichmentResult) []enrichmentResult {
	if len(results) == 0 {
		return nil
	}
	ranked := make([]enrichmentResult, 0, len(results))
	review := m.reviewIndex()
	current := m.currentReviewLocation()
	for i, result := range results {
		result.Score = max(result.Score, max(0, 1000-i))
		containsAddition, containsDeletion := result.Review.ContainsAddition, result.Review.ContainsDeletion
		entireAddition, entireDeletion := result.Review.EntireAddition, result.Review.EntireDeletion
		result.Review = diff.MarkersForIndex(review, result.Location.Path, result.Location.Line)
		result.Review.ContainsAddition = containsAddition
		result.Review.ContainsDeletion = containsDeletion
		result.Review.EntireAddition = entireAddition
		result.Review.EntireDeletion = entireDeletion
		result.Review = m.reviewMarkersForResultSide(result.Location, result.Side, result.Review)
		result.Score += navigationReviewScore(result.Location, current, result.Review, review, false)
		ranked = append(ranked, result)
	}
	if m.enrichmentPanel.Order == diff.ResultOrderSource {
		sort.SliceStable(ranked, func(i, j int) bool {
			return locationLess(ranked[i].Location, ranked[j].Location, ranked[i].Label, ranked[j].Label)
		})
		return trimEnrichmentResults(ranked, overlayResultLimit)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return locationLess(ranked[i].Location, ranked[j].Location, ranked[i].Label, ranked[j].Label)
	})
	return trimEnrichmentResults(ranked, overlayResultLimit)
}

func trimEnrichmentResults(results []enrichmentResult, limit int) []enrichmentResult {
	if limit > 0 && len(results) > limit {
		return results[:limit]
	}
	return results
}

func (m Model) overlayPageSize() int {
	return ui.OverlayResultHeight(m.overlayView(), m.width, m.height)
}

func (m Model) referencePageSize() int {
	panel := m.referencePanelViewValue()
	m.configureResultPanel(&panel)
	return ui.BottomPanelResultHeight(panel, m.width, m.height)
}

func (m Model) enrichmentPageSize() int {
	panel := m.enrichmentPanelViewValue()
	m.configureResultPanel(&panel)
	return ui.BottomPanelResultHeight(panel, m.width, m.height)
}

func (m *Model) acceptOverlayResult() tea.Cmd {
	if m.overlay.Cursor < 0 || m.overlay.Cursor >= len(m.overlay.Results) {
		return nil
	}
	result := m.overlay.Results[m.overlay.Cursor]
	if m.overlay.Kind == OverlayCommandPalette {
		commandID := result.Location.Path
		m.closeOverlay()
		return m.executeCommand(commandID, 1, false)
	}
	m.closeOverlay()
	return m.jumpToSearchResult(result)
}

func (m *Model) acceptReferenceResult() tea.Cmd {
	if m.referencePanel.Cursor < 0 || m.referencePanel.Cursor >= len(m.referencePanel.Results) {
		return nil
	}
	return m.jumpToReferenceResult(m.referencePanel.Results[m.referencePanel.Cursor])
}

func (m *Model) acceptEnrichmentResult() tea.Cmd {
	if m.enrichmentPanel.Cursor < 0 || m.enrichmentPanel.Cursor >= len(m.enrichmentPanel.Results) {
		return nil
	}
	m.pushJump()
	result := m.enrichmentPanel.Results[m.enrichmentPanel.Cursor]
	return m.jumpToLocationSide(result.Location, result.Side)
}

func (m *Model) jumpToSearchResult(result navsearch.Result) tea.Cmd {
	m.pushJump()
	return m.jumpToLocationSide(result.Location, result.Side)
}

func (m *Model) jumpToReferenceResult(result navsearch.ReferenceResult) tea.Cmd {
	m.pushJump()
	return m.jumpToLocationSide(result.Location, result.Side)
}

func (m *Model) jumpToLocationSide(loc source.Location, side navsearch.ResultSide) tea.Cmd {
	if side == navsearch.ResultSideBaseline {
		if cmd, ok := m.jumpToBaselineLocation(loc); ok {
			m.rememberViewJump(loc, side)
			return cmd
		}
	}
	cmd := m.jumpToLocation(loc)
	m.rememberViewJump(loc, navsearch.ResultSideCurrent)
	return cmd
}

func (m *Model) rememberViewJump(loc source.Location, side navsearch.ResultSide) {
	path := m.currentFilePath()
	if path == "" || loc.Path == "" {
		m.pendingViewJump = viewJumpAnchor{}
		return
	}
	if loc.Line < 1 {
		loc.Line = 1
	}
	if loc.Column < 1 {
		loc.Column = 1
	}
	m.pendingViewJump = viewJumpAnchor{
		path:     path,
		location: loc,
		pending:  true,
	}
	if side == navsearch.ResultSideCurrent {
		m.pendingViewJump.location.Path = path
	}
}

// takeViewJumpAnchor consumes the one-shot cross-view anchor. A toggle on a
// different file also expires it so a later return cannot revive an old jump.
func (m *Model) takeViewJumpAnchor(path string) (viewJumpAnchor, bool) {
	anchor := m.pendingViewJump
	m.pendingViewJump = viewJumpAnchor{}
	return anchor, anchor.pending && anchor.path == path
}

func (m *Model) jumpToBaselineLocation(loc source.Location) (tea.Cmd, bool) {
	if loc.Path == "" {
		return nil, true
	}
	if loc.Line < 1 {
		loc.Line = 1
	}
	if loc.Column < 1 {
		loc.Column = 1
	}

	idx := findFileIndexByPathSide(m.files, loc.Path, navsearch.ResultSideBaseline)
	if idx < 0 {
		return nil, false
	}

	m.saveCurrentFileState()
	m.selectedFile = idx
	m.viewMode = ViewDiff
	m.restoreCurrentFileState()
	m.rememberPath(m.currentFilePath())

	if !m.positionCursorAtLocationSide(loc, navsearch.ResultSideBaseline) {
		return nil, false
	}
	m.clampScroll()
	m.centerCursorInViewport()
	return nil, true
}

func (m *Model) jumpToLocation(loc source.Location) tea.Cmd {
	if loc.Path == "" {
		return nil
	}
	if loc.Line < 1 {
		loc.Line = 1
	}
	if loc.Column < 1 {
		loc.Column = 1
	}

	m.saveCurrentFileState()
	m.selectedFile = m.ensureFileIndex(loc.Path)
	m.viewMode = ViewFile
	m.restoreCurrentFileState()
	m.rememberPath(loc.Path)

	positioned := m.positionCursorAtLocation(loc)
	if !positioned {
		m.pendingLocation = loc
		m.hasPendingLocation = true
	}
	cmd := m.ensureCurrentFileContentCmd()
	m.clampScroll()
	if positioned {
		m.centerCursorInViewport()
	}
	return cmd
}

func (m *Model) ensureFileIndex(path string) int {
	if idx := findFileIndexByPath(m.files, path); idx >= 0 {
		return idx
	}
	m.files = append(m.files, diff.FileDiff{OldPath: path, NewPath: path, Status: diff.FileModified})
	return len(m.files) - 1
}

func (m *Model) positionCursorAtLocation(loc source.Location) bool {
	return m.positionCursorAtLocationSide(loc, navsearch.ResultSideCurrent)
}

func (m *Model) positionCursorAtLocationSide(loc source.Location, side navsearch.ResultSide) bool {
	if loc.Path != m.currentFilePath() {
		if side != navsearch.ResultSideBaseline {
			return false
		}
		if m.selectedFile < 0 || m.selectedFile >= len(m.files) {
			return false
		}
	}
	rows := m.currentRows()
	lastLineRow := -1
	if side == navsearch.ResultSideBaseline {
		file := m.files[m.selectedFile]
		for i, row := range rows {
			rowLoc, ok := baselineLocationForRow(file, row)
			if !ok || rowLoc.Path != loc.Path {
				continue
			}
			lastLineRow = i
			if rowLoc.Line >= loc.Line {
				m.cursor = i
				m.alignCursorColToLocation(row, loc, true, rowLoc.Line == loc.Line)
				return true
			}
		}
		if lastLineRow >= 0 {
			m.cursor = lastLineRow
			m.setCursorCol(0)
			return true
		}
		return false
	}
	for i, row := range rows {
		rowLine := sourceLine(row)
		if rowLine == 0 {
			continue
		}
		lastLineRow = i
		if rowLine >= loc.Line {
			m.cursor = i
			m.alignCursorColToLocation(row, loc, false, rowLine == loc.Line)
			return true
		}
	}
	if lastLineRow >= 0 {
		m.cursor = lastLineRow
		m.setCursorCol(0)
		return true
	}
	return false
}

func (m *Model) rememberPath(path string) {
	if path == "" {
		return
	}
	next := []string{path}
	for _, existing := range m.recentPaths {
		if existing != path {
			next = append(next, existing)
		}
		if len(next) >= 32 {
			break
		}
	}
	m.recentPaths = next
}

func dropLastRune(s string) string {
	if s == "" {
		return ""
	}
	rs := []rune(s)
	return string(rs[:len(rs)-1])
}

// dropLastWord mirrors the terminal input convention used by shells: remove
// trailing whitespace, then the preceding run of non-whitespace characters.
func dropLastWord(s string) string {
	rs := []rune(s)
	i := len(rs)
	for i > 0 && isQuerySpace(rs[i-1]) {
		i--
	}
	for i > 0 && !isQuerySpace(rs[i-1]) {
		i--
	}
	return string(rs[:i])
}

func isQuerySpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

func (m Model) overlayView() ui.Overlay {
	overlay := ui.Overlay{
		Query:         m.overlay.Query,
		Cursor:        m.overlay.Cursor,
		Top:           m.overlay.Top,
		Loading:       m.overlay.Loading,
		QuerySelected: m.overlay.QuerySelected,
	}
	switch m.overlay.Kind {
	case OverlayFileOpen:
		overlay.Title = "Open file"
		overlay.Prompt = "^P"
		overlay.Empty = "No matching files"
	case OverlaySearch:
		overlay.Title = "Search project"
		overlay.Prompt = "g/"
		overlay.Empty = "No matches"
		mode := "literal"
		if m.overlay.SearchRegex {
			mode = "regex"
		} else {
			overlay.Match = m.overlay.Query
			overlay.MatchFold = smartCaseFold(m.overlay.Query)
		}
		overlay.Title += " · " + mode + " · " + m.overlay.Order.String() + " · ^R mode · ^O order"
	case OverlayWorkspaceSymbol:
		overlay.Title = "Workspace symbols"
		overlay.Prompt = "gS"
		overlay.Empty = "No symbols"
	case OverlayCommandPalette:
		overlay.Title = "Commands · tab/shift+tab or left/right changes category · enter runs"
		overlay.Prompt = "?"
		overlay.Empty = "No matching commands"
		overlay.LabelWidth = commandNameWidth
		overlay.FullHeight = true
		for _, category := range commandPaletteCategories {
			overlay.Tabs = append(overlay.Tabs, string(category))
			if category == m.overlay.CommandCategory {
				overlay.ActiveTab = len(overlay.Tabs) - 1
			}
		}
	}
	if m.overlay.Kind == OverlayWorkspaceSymbol {
		overlay.Title += " · " + m.overlay.Order.String() + " · ^O toggle"
	}
	if m.overlay.Err != nil {
		overlay.Error = m.overlay.Err.Error()
	}
	for _, result := range m.overlay.Results {
		overlay.Results = append(overlay.Results, ui.OverlayResult{
			Label:       m.overlayResultLabel(result),
			Preview:     result.Preview,
			Tone:        m.resultTone(result.Kind, result.Location, result.Side, result.Review),
			ChangeField: m.overlay.Kind == OverlayWorkspaceSymbol,
		})
	}
	return overlay
}

func (m Model) bottomPanelView() *ui.BottomPanel {
	if m.composer.open {
		// The composer occupies the bottom-panel slot; an open placeholder
		// keeps Layout, viewHeight, and mouse math consistent.
		return &ui.BottomPanel{Open: true}
	}
	if m.enrichmentPanel.Open {
		panel := m.enrichmentPanelViewValue()
		m.configureResultPanel(&panel)
		return &panel
	}
	panel := m.referencePanelView()
	if panel != nil {
		m.configureResultPanel(panel)
	}
	return panel
}

func (m Model) configureResultPanel(panel *ui.BottomPanel) {
	if panel == nil {
		return
	}
	panel.Placement = m.resultPanelPlacement
	if panel.Placement == ui.PanelRight {
		panel.Size = m.resultPanelWidth
	} else {
		panel.Size = m.resultPanelHeight
	}
}

func (m Model) referencePanelView() *ui.BottomPanel {
	if !m.referencePanel.Open {
		return nil
	}
	panel := m.referencePanelViewValue()
	return &panel
}

func (m Model) referencePanelViewValue() ui.BottomPanel {
	title := "References"
	empty := "No references"
	if m.referencePanel.Kind == referenceRequestDefinition {
		title = "Definition"
		empty = "No definitions"
	} else if m.referencePanel.Kind == referenceRequestImpact {
		title = "Impact"
		empty = "No references"
	}
	if m.referencePanel.ChangedOnly {
		title += " in changed files"
	}
	if m.referencePanel.Query.Symbol != "" {
		title += ": " + m.referencePanel.Query.Symbol
	}

	panel := ui.BottomPanel{
		Open:    m.referencePanel.Open,
		Title:   title,
		Summary: referencePanelSummary(m.referencePanel),
		Cursor:  m.referencePanel.Cursor,
		Top:     m.referencePanel.Top,
		Loading: m.referencePanel.Loading,
		Spinner: m.spinnerFrameString(),
		Empty:   empty,
	}
	if m.referencePanel.Err != nil {
		panel.Error = m.referencePanel.Err.Error()
	}

	for _, result := range m.referencePanel.Results {
		panel.Results = append(panel.Results, ui.BottomPanelResult{
			Label:   m.referenceResultLabel(result),
			Preview: result.Preview,
			Tone:    m.resultTone(navsearch.ResultText, result.Location, result.Side, result.Review),
		})
	}
	return panel
}

func (m Model) enrichmentPanelViewValue() ui.BottomPanel {
	panel := ui.BottomPanel{
		Open:    m.enrichmentPanel.Open,
		Title:   m.enrichmentPanel.Title,
		Summary: enrichmentPanelSummary(m.enrichmentPanel),
		Cursor:  m.enrichmentPanel.Cursor,
		Top:     m.enrichmentPanel.Top,
		Loading: m.enrichmentPanel.Loading,
		Spinner: m.spinnerFrameString(),
		Empty:   enrichmentPanelEmpty(m.enrichmentPanel.Kind),
	}
	if m.enrichmentPanel.Err != nil {
		panel.Error = m.enrichmentPanel.Err.Error()
	}
	for _, result := range m.enrichmentPanel.Results {
		panel.Results = append(panel.Results, ui.BottomPanelResult{
			Label:       m.enrichmentResultLabel(result),
			Preview:     result.Preview,
			Tone:        m.resultTone(navsearch.ResultText, result.Location, result.Side, result.Review),
			ChangeField: m.enrichmentPanel.Kind == enrichmentPanelDocumentSymbols || m.enrichmentPanel.Kind == enrichmentPanelOutlineDiff,
		})
	}
	return panel
}

func enrichmentPanelSummary(panel enrichmentPanelState) string {
	if !panel.Open {
		return ""
	}
	if panel.Loading {
		if label := panel.Status.Label(); label != "" {
			return label
		}
		if panel.Kind == enrichmentPanelOutlineDiff {
			return "outline"
		}
		return "lsp"
	}
	count := len(panel.Results)
	status := panel.Status.Label()
	order := panel.Order.String()
	if panel.Kind == enrichmentPanelHover {
		order = ""
	}
	switch {
	case count == 1 && status != "" && order != "":
		return "1 result · " + status + " · " + order
	case count == 1 && status != "":
		return "1 result · " + status
	case count == 1 && order != "":
		return "1 result · " + order
	case count == 1:
		return "1 result"
	case status != "" && order != "":
		return strconv.Itoa(count) + " results · " + status + " · " + order
	case status != "":
		return strconv.Itoa(count) + " results · " + status
	case order != "":
		return strconv.Itoa(count) + " results · " + order
	default:
		return strconv.Itoa(count) + " results"
	}
}

func enrichmentPanelEmpty(kind enrichmentPanelKind) string {
	switch kind {
	case enrichmentPanelDiagnosticsCurrent, enrichmentPanelDiagnosticsWorkspace:
		return "No diagnostics"
	case enrichmentPanelHover:
		return "No hover information"
	case enrichmentPanelDocumentSymbols:
		return "No symbols"
	case enrichmentPanelOutlineDiff:
		return "No changed symbols"
	case enrichmentPanelCallIncoming:
		return "No incoming calls"
	case enrichmentPanelCallOutgoing:
		return "No outgoing calls"
	default:
		return "No results"
	}
}

func referencePanelSummary(panel referencePanelState) string {
	if !panel.Open {
		return ""
	}
	count := len(panel.Results)
	if panel.Loading {
		return panel.Source.String()
	}
	order := panel.Order.String()
	if count == 1 {
		return "1 result · " + panel.Source.String() + " · " + order
	}
	return strconv.Itoa(count) + " results · " + panel.Source.String() + " · " + order
}

func referencePanelErrorText(kind referenceRequestKind) string {
	switch kind {
	case referenceRequestDefinition:
		return "definition lookup failed"
	case referenceRequestImpact:
		return "impact lookup failed"
	default:
		return "reference search failed"
	}
}

func (m Model) referenceResultLabel(result navsearch.ReferenceResult) string {
	var markers []string
	if result.Kind == navsearch.ReferenceDefinition {
		markers = append(markers, "def")
	}
	reviewMarkers := m.reviewMarkersForResultSide(result.Location, result.Side, result.Review)
	markers = append(markers, m.reviewMarkerLabels(result.Location, reviewMarkers)...)
	if m.referencePanel.Kind == referenceRequestImpact && !m.reviewIndex().IsChanged(result.Location.Path) {
		markers = append(markers, "outside-diff")
	}
	label := navsearch.ReferenceResultLabel(result)
	if len(markers) > 0 {
		label = "[" + strings.Join(markers, ",") + "] " + label
	}
	return label
}

func (m *Model) updateDiagnostics(msg enrichmentLoadedMsg) {
	if m.diagnostics == nil {
		m.diagnostics = make(map[string][]lsp.Diagnostic)
	}
	switch msg.kind {
	case enrichmentPanelDiagnosticsCurrent:
		if msg.path != "" {
			m.diagnostics[msg.path] = msg.diagnostics
		}
	case enrichmentPanelDiagnosticsWorkspace:
		for _, path := range msg.paths {
			delete(m.diagnostics, path)
		}
		for _, diagnostic := range msg.diagnostics {
			loc := diagnostic.Location()
			if loc.Path != "" {
				m.diagnostics[loc.Path] = append(m.diagnostics[loc.Path], diagnostic)
			}
		}
	}
}

func (m Model) diagnosticPanelResults(diagnostics []lsp.Diagnostic) []enrichmentResult {
	ranked := lsp.RankDiagnosticsWithReview(diagnostics, m.currentReviewLocation(), m.reviewIndex(), m.enrichmentPanel.Order, overlayResultLimit)
	results := make([]enrichmentResult, 0, len(ranked))
	for _, diagnostic := range ranked {
		loc := diagnostic.Location()
		results = append(results, enrichmentResult{
			Location: loc,
			Label:    m.diagnosticLabel(diagnostic),
			Preview:  diagnostic.Message,
			Score:    diagnostic.Score,
			Review:   diagnostic.Review,
		})
	}
	return results
}

func (m Model) diagnosticLabel(diagnostic lsp.Diagnostic) string {
	label := lsp.DiagnosticLabel(diagnostic)
	loc := diagnostic.Location()
	markers := m.reviewMarkerLabels(loc, diagnostic.Review)
	if len(markers) > 0 {
		label = "[" + strings.Join(markers, ",") + "] " + label
	}
	return label
}

func (m Model) overlayResultLabel(result navsearch.Result) string {
	label := result.Label
	reviewMarkers := m.reviewMarkersForResultSide(result.Location, result.Side, result.Review)
	markers := m.reviewMarkerLabelsWithChangeText(result.Location, reviewMarkers, m.overlay.Kind != OverlayWorkspaceSymbol)
	if len(markers) > 0 {
		label = "[" + strings.Join(markers, ",") + "] " + label
	}
	return label
}

func (m Model) enrichmentResultLabel(result enrichmentResult) string {
	label := result.Label
	showChangeText := m.enrichmentPanel.Kind != enrichmentPanelDocumentSymbols && m.enrichmentPanel.Kind != enrichmentPanelOutlineDiff
	markers := m.reviewMarkerLabelsWithChangeText(result.Location, m.reviewMarkersForResultSide(result.Location, result.Side, result.Review), showChangeText)
	if len(markers) > 0 {
		prefix := "[" + strings.Join(markers, ",") + "] "
		if !strings.HasPrefix(label, prefix) {
			label = prefix + label
		}
	}
	return label
}

func (m Model) reviewMarkerLabels(loc source.Location, markers diff.ReviewMarkers) []string {
	return m.reviewMarkerLabelsWithChangeText(loc, markers, true)
}

func (m Model) reviewMarkerLabelsWithChangeText(loc source.Location, markers diff.ReviewMarkers, showChangeText bool) []string {
	var out []string
	if showChangeText && !markers.ContainsAddition && !markers.ContainsDeletion {
		switch {
		case markers.ChangeKind == diff.ChangeAdded || markers.ChangeKind == diff.ChangeDeleted:
		case markers.Changed:
			out = append(out, "changed-line")
		case markers.ChangeKind == diff.ChangeContext:
			out = append(out, "context")
		case m.reviewIndex().IsChanged(loc.Path):
			out = append(out, "changed-file")
		}
	}
	if markers.Unread {
		out = append(out, "unread")
	}
	if markers.Annotated {
		out = append(out, markers.Annotation.String())
	}
	return out
}

func (m Model) resultTone(kind navsearch.ResultKind, loc source.Location, side navsearch.ResultSide, markers diff.ReviewMarkers) ui.ResultTone {
	if kind != navsearch.ResultText {
		return ui.ResultToneNone
	}
	switch {
	case markers.EntireAddition:
		return ui.ResultToneAddedEntire
	case markers.EntireDeletion:
		return ui.ResultToneDeletedEntire
	case markers.ContainsAddition && markers.ContainsDeletion:
		return ui.ResultToneModified
	case markers.ContainsAddition:
		return ui.ResultToneAdded
	case markers.ContainsDeletion:
		return ui.ResultToneDeleted
	}
	switch side {
	case navsearch.ResultSideBaseline:
		return ui.ResultToneDeleted
	case navsearch.ResultSideCurrent:
		return ui.ResultToneAdded
	case navsearch.ResultSideBoth:
		return ui.ResultToneNone
	}

	reviewMarkers := m.reviewMarkersForResultSide(loc, side, markers)
	return resultToneForChangeKind(reviewMarkers.ChangeKind)
}

func resultToneForChangeKind(kind diff.ChangeKind) ui.ResultTone {
	switch kind {
	case diff.ChangeAdded:
		return ui.ResultToneAdded
	case diff.ChangeDeleted:
		return ui.ResultToneDeleted
	default:
		return ui.ResultToneNone
	}
}

func (m Model) reviewMarkersForResultSide(loc source.Location, side navsearch.ResultSide, markers diff.ReviewMarkers) diff.ReviewMarkers {
	switch side {
	case navsearch.ResultSideBaseline, navsearch.ResultSideCurrent:
	default:
		return markers
	}

	changeKind, matchedFile := m.lineChangeKindForSide(loc, side)
	if !matchedFile {
		return markers
	}
	markers.ChangeKind = changeKind
	markers.Changed = changeKind.Changed()
	return markers
}

func (m Model) lineChangeKindForSide(loc source.Location, side navsearch.ResultSide) (diff.ChangeKind, bool) {
	if loc.Path == "" || loc.Line < 1 {
		return diff.ChangeNone, false
	}
	matchedFile := false
	best := diff.ChangeNone
	for _, file := range m.files {
		sidePath := filePathForResultSide(file, side)
		if sidePath != loc.Path {
			continue
		}
		matchedFile = true
		for _, hunk := range file.Hunks {
			for _, line := range hunk.Lines {
				changeKind, ok := lineChangeKindForResultSide(line, loc.Line, side)
				if ok && changeKindPriority(changeKind) >= changeKindPriority(best) {
					best = changeKind
				}
			}
		}
	}
	return best, matchedFile
}

func filePathForResultSide(file diff.FileDiff, side navsearch.ResultSide) string {
	switch side {
	case navsearch.ResultSideBaseline:
		if isReviewPath(file.OldPath) {
			return file.OldPath
		}
	case navsearch.ResultSideCurrent:
		if isReviewPath(file.NewPath) {
			return file.NewPath
		}
	}
	return ""
}

func lineChangeKindForResultSide(line diff.Line, targetLine int, side navsearch.ResultSide) (diff.ChangeKind, bool) {
	switch side {
	case navsearch.ResultSideBaseline:
		if line.OldLine != targetLine {
			return diff.ChangeNone, false
		}
		switch line.Kind {
		case diff.LineDelete:
			return diff.ChangeDeleted, true
		case diff.LineContext:
			return diff.ChangeContext, true
		}
	case navsearch.ResultSideCurrent:
		if line.NewLine != targetLine {
			return diff.ChangeNone, false
		}
		switch line.Kind {
		case diff.LineAdd:
			return diff.ChangeAdded, true
		case diff.LineContext:
			return diff.ChangeContext, true
		}
	}
	return diff.ChangeNone, false
}

func isReviewPath(path string) bool {
	return path != "" && path != "/dev/null"
}

func changeKindPriority(kind diff.ChangeKind) int {
	switch kind {
	case diff.ChangeAdded, diff.ChangeDeleted:
		return 3
	case diff.ChangeContext:
		return 1
	default:
		return 0
	}
}

func (m Model) workspaceSymbolOverlayRawResults(results []lsp.WorkspaceSymbol) []navsearch.Result {
	out := make([]navsearch.Result, 0, len(results))
	for _, symbol := range results {
		review := m.reviewWithOutlineChange(symbol.Location, symbol.Name, symbol.Kind, symbol.Review)
		out = append(out, navsearch.Result{
			Kind:     navsearch.ResultText,
			Location: symbol.Location,
			Label:    lsp.WorkspaceSymbolLabel(symbol),
			Preview:  symbol.ContainerName,
			Score:    symbol.Score,
			Review:   review,
		})
	}
	return out
}

func (m Model) reviewWithOutlineChange(loc source.Location, name string, kind lsp.SymbolKind, review diff.ReviewMarkers) diff.ReviewMarkers {
	for _, change := range m.outlineChanges {
		if change.After == nil {
			continue
		}
		afterLoc := change.After.SelectionRange.Start
		if afterLoc.Line < 1 {
			afterLoc = change.After.Range.Start
		}
		if !symbolLocationMatches(loc, *change.After, afterLoc) || (name != "" && change.After.Name != name) || (kind != 0 && change.After.Kind != kind) {
			continue
		}
		review.ContainsAddition = change.ContainsAddition
		review.ContainsDeletion = change.ContainsDeletion
		review.EntireAddition = change.Type == outline.SymbolAdded && change.ContainsAddition
		review.EntireDeletion = change.Type == outline.SymbolRemoved && change.ContainsDeletion
		break
	}
	return review
}

func symbolLocationMatches(loc source.Location, symbol lsp.DocumentSymbol, selection source.Location) bool {
	if loc == selection || loc == symbol.Range.Start {
		return true
	}
	start, end := symbol.Range.Start, symbol.Range.End
	if loc.Path == "" || loc.Path != start.Path || loc.Line < start.Line || loc.Line > end.Line {
		return false
	}
	if loc.Line == start.Line && start.Column > 0 && loc.Column < start.Column {
		return false
	}
	return loc.Line != end.Line || end.Column < 1 || loc.Column <= end.Column
}

func (m *Model) recordLSPStatus(status lsp.Status) {
	if !status.Enabled() || status.State == lsp.StateDisabled {
		return
	}
	if m.lspStatuses == nil {
		m.lspStatuses = make(map[string]lsp.Status)
	}
	m.lspStatuses[status.Key()] = status
}

func (m *Model) captureCount(k string) bool {
	if len(k) == 1 && k[0] >= '1' && k[0] <= '9' && m.countBuf == "" {
		m.countBuf = k
		return true
	}
	if len(k) == 1 && k[0] >= '0' && k[0] <= '9' && m.countBuf != "" {
		m.countBuf += k
		return true
	}
	return false
}

func (m *Model) consumeCount() (int, bool) {
	if m.countBuf == "" {
		return 1, false
	}
	count, err := strconv.Atoi(m.countBuf)
	m.countBuf = ""
	if err != nil || count < 1 {
		return 1, true
	}
	return count, true
}

// viewHeight is the exact diff viewport height in screen lines.
func (m *Model) viewHeight() int {
	return max(1, m.mainLayout().DiffRowsHeight)
}

func (m *Model) move(delta int) {
	rows := m.currentRows()
	if len(rows) == 0 {
		return
	}
	m.cursor = min(max(m.cursor+delta, 0), len(rows)-1)
}

func (m *Model) moveViewportEdge(dir int) {
	rows := m.currentRows()
	if len(rows) == 0 {
		return
	}
	l := m.layoutFor(rows)
	topSL := m.topScreenLine(l)
	if dir < 0 {
		sl := l.LineAt(topSL)
		row := sl.RowIdx
		if sl.WrapIdx > 0 && row+1 < len(rows) {
			row++ // top row only partially visible; pick the first full one
		}
		m.cursor = row
		return
	}
	vh := max(1, m.viewHeight())
	bottom := min(topSL+vh-1, l.TotalLines()-1)
	sl := l.LineAt(bottom)
	row := sl.RowIdx
	if l.RowStart(row)+l.RowHeight(row) > topSL+vh && row > 0 {
		row-- // bottom row overflows the viewport; pick the last full one
	}
	m.cursor = min(max(row, 0), len(rows)-1)
}

// windowScroll scrolls the viewport by delta screen lines while preserving the
// cursor's visible offset when possible, matching vim-style Ctrl+f/Ctrl+b.
func (m *Model) windowScroll(delta int) {
	rows := m.currentRows()
	if len(rows) == 0 || delta == 0 {
		return
	}
	vh := max(1, m.viewHeight())
	l := m.layoutFor(rows)
	topSL := m.topScreenLine(l)
	offset := l.RowStart(m.cursor) - topSL
	if offset < 0 {
		offset = 0
	}
	if offset >= vh {
		offset = vh - 1
	}
	maxTop := max(0, l.TotalLines()-vh)
	newTop := min(max(topSL+delta, 0), maxTop)
	m.setTopScreenLine(l, newTop)
	row := l.LineAt(min(newTop+offset, l.TotalLines()-1)).RowIdx
	// A row straddling the new viewport top would force clampScroll to pull
	// the viewport back; prefer the first row that starts inside it.
	if l.RowStart(row) < newTop && row+1 < len(rows) && l.RowStart(row+1) < newTop+vh {
		row++
	}
	m.cursor = row
}

// cursorScreenRow reports which viewport screen line the cursor's first line
// occupies.
func (m *Model) cursorScreenRow() int {
	rows := m.currentRows()
	if len(rows) == 0 {
		return 0
	}
	vh := max(1, m.viewHeight())
	l := m.layoutFor(rows)
	offset := l.RowStart(min(max(m.cursor, 0), len(rows)-1)) - m.topScreenLine(l)
	if offset < 0 {
		return 0
	}
	if offset >= vh {
		return vh - 1
	}
	return offset
}

// scrollCursorToScreenRowAllowingEOFSpace positions the cursor at the requested
// screen row without clamping the viewport to EOF. Bottom hunks can therefore
// keep the reader's eye position stable and leave blank rows below the file.
func (m *Model) scrollCursorToScreenRowAllowingEOFSpace(row int) {
	rows := m.currentRows()
	if len(rows) == 0 {
		return
	}
	vh := max(1, m.viewHeight())
	row = min(max(row, 0), vh-1)
	m.cursor = min(max(m.cursor, 0), len(rows)-1)
	l := m.layoutFor(rows)
	m.setTopScreenLine(l, max(l.RowStart(m.cursor)-row, 0))
}

func (m *Model) centerCursorInViewport() {
	rows := m.currentRows()
	if len(rows) == 0 {
		m.top, m.topWrap = 0, 0
		return
	}
	vh := max(1, m.viewHeight())
	m.cursor = min(max(m.cursor, 0), len(rows)-1)
	// Result jumps should keep the target readable, even near EOF. Allowing
	// top past the EOF clamp lets the renderer leave blank space below the file.
	l := m.layoutFor(rows)
	curStart := l.RowStart(m.cursor)
	m.setTopScreenLine(l, min(max(curStart-vh/2, 0), curStart))
}

func (m *Model) jumpSourceLine(lineNum int) {
	rows := m.currentRows()
	if len(rows) == 0 {
		return
	}
	if lineNum < 1 {
		lineNum = 1
	}

	lastLineRow := -1
	for i, row := range rows {
		rowLine := sourceLine(row)
		if rowLine == 0 {
			continue
		}
		lastLineRow = i
		if rowLine >= lineNum {
			m.cursor = i
			return
		}
	}
	if lastLineRow >= 0 {
		m.cursor = lastLineRow
	}
}

func sourceLine(row ui.Row) int {
	if !row.IsLineRow() {
		return 0
	}
	if row.Line.NewLine != 0 {
		return row.Line.NewLine
	}
	return row.Line.OldLine
}

// jumpHeader moves the cursor to the next/previous hunk in the selected file.
func (m *Model) jumpHeader(dir int) {
	rows := m.currentRows()
	for i := m.cursor + dir; i >= 0 && i < len(rows); i += dir {
		if !rows[i].IsLineRow() {
			m.cursor = i
			return
		}
	}
}

func (m *Model) jumpHeaderN(dir, count int) bool {
	moved := false
	for i := 0; i < count; i++ {
		before := m.cursor
		m.jumpHeader(dir)
		if m.cursor == before {
			return moved
		}
		moved = true
	}
	return moved
}

// switchFile opens the next/previous changed file, preserving each file's
// cursor and scroll position.
func (m *Model) switchFile(dir int) {
	if len(m.files) == 0 || dir == 0 {
		return
	}
	order := ui.ChangeListFileOrderWithOptions(m.files, m.changeListOptions())
	current := fileOrderPosition(order, m.selectedFile)
	if current < 0 {
		return
	}
	targetPos := current + dir
	if targetPos < 0 || targetPos >= len(order) {
		return
	}
	target := order[targetPos]
	m.saveCurrentFileState()
	m.selectedFile = target
	m.restoreCurrentFileState()
	m.rememberPath(m.currentFilePath())
}

func (m *Model) switchFileN(dir, count int) {
	for i := 0; i < count; i++ {
		before := m.selectedFile
		m.switchFile(dir)
		if m.selectedFile == before {
			return
		}
	}
}

func (m *Model) toggleViewMode() {
	path := m.currentFilePath()
	if m.viewMode == ViewFile {
		jumpAnchor, syncJump := m.takeViewJumpAnchor(path)
		screenRow := m.cursorScreenRow()
		m.saveCurrentFileState()
		if m.fileViewAnchors != nil {
			delete(m.fileViewAnchors, path)
		}
		m.viewMode = ViewDiff
		m.restoreCurrentFileState()
		if syncJump && m.positionCursorAtLocation(jumpAnchor.location) {
			m.clampScroll()
			m.scrollCursorToScreenRowAllowingEOFSpace(screenRow)
		}
		m.clampScroll()
		if path != "" {
			if m.diffViewOrigins == nil {
				m.diffViewOrigins = make(map[string]diffViewPosition)
			}
			m.diffViewOrigins[path] = m.captureDiffViewPosition()
		}
		return
	}

	_, syncJump := m.takeViewJumpAnchor(path)
	position := m.captureDiffViewPosition()
	anchor, hasAnchor := m.fileViewAnchorForDiffCursor()
	_, hasSavedFileView := m.fileStates[fileStateKey{path: path, mode: ViewFile}]
	origin, hasOrigin := m.diffViewOrigins[path]
	overrideSaved := syncJump || !hasSavedFileView || !hasOrigin || !sameDiffViewPosition(position, origin)

	m.saveCurrentFileState()
	m.viewMode = ViewFile
	m.restoreCurrentFileState()
	anchoredFileView := false
	if overrideSaved && hasAnchor {
		if m.fileViewAnchors == nil {
			m.fileViewAnchors = make(map[string]fileViewAnchor)
		}
		m.fileViewAnchors[path] = anchor
		anchoredFileView = m.applyPendingFileViewAnchor()
	}
	if !anchoredFileView {
		m.clampScroll()
	}
}

func (m *Model) captureDiffViewPosition() diffViewPosition {
	rows := m.currentRows()
	if m.viewMode != ViewDiff || m.cursor < 0 || m.cursor >= len(rows) {
		return diffViewPosition{}
	}
	row := rows[m.cursor]
	return diffViewPosition{
		valid:        true,
		cursor:       m.cursor,
		col:          m.col,
		kind:         row.Kind,
		hunkIdx:      row.HunkIdx,
		currentLine:  rowLineNumberForSide(row, false),
		baselineLine: rowLineNumberForSide(row, true),
	}
}

func sameDiffViewPosition(a, b diffViewPosition) bool {
	return a.valid == b.valid &&
		a.cursor == b.cursor &&
		a.col == b.col &&
		a.kind == b.kind &&
		a.hunkIdx == b.hunkIdx &&
		a.currentLine == b.currentLine &&
		a.baselineLine == b.baselineLine
}

// fileViewAnchorForDiffCursor maps the compact diff cursor to the current
// file. Current-side lines are exact. Headers and baseline-only deletions use
// their hunk's current insertion point, with a nearby current-side row as a
// final fallback for synthetic rows such as comments.
func (m *Model) fileViewAnchorForDiffCursor() (fileViewAnchor, bool) {
	rows := m.currentRows()
	if m.selectedFile < 0 || m.selectedFile >= len(m.files) || m.cursor < 0 || m.cursor >= len(rows) {
		return fileViewAnchor{}, false
	}
	row := rows[m.cursor]
	if line := rowLineNumberForSide(row, false); line > 0 {
		return fileViewAnchor{line: line, col: m.col, screenRow: m.cursorScreenRow()}, true
	}
	if row.HunkIdx > 0 && row.HunkIdx <= len(m.files[m.selectedFile].Hunks) {
		hunk := m.files[m.selectedFile].Hunks[row.HunkIdx-1]
		line := currentInsertionLineForDiffRow(hunk, row)
		return fileViewAnchor{line: line, screenRow: m.cursorScreenRow()}, true
	}
	for distance := 1; distance < len(rows); distance++ {
		// Prefer the following current line: a deleted block or header is
		// conceptually inserted immediately before it.
		for _, idx := range [2]int{m.cursor + distance, m.cursor - distance} {
			if idx < 0 || idx >= len(rows) {
				continue
			}
			if line := rowLineNumberForSide(rows[idx], false); line > 0 {
				return fileViewAnchor{line: line, screenRow: m.cursorScreenRow()}, true
			}
		}
	}
	return fileViewAnchor{}, false
}

// currentInsertionLineForDiffRow gives baseline-only rows a useful current
// coordinate. It walks the hunk up to the selected old-side line, so a delete
// late in a hunk maps after preceding current lines instead of always jumping
// to the hunk start.
func currentInsertionLineForDiffRow(hunk diff.Hunk, row ui.Row) int {
	baselineLine := rowLineNumberForSide(row, true)
	line := max(1, hunk.NewStart)
	if baselineLine <= 0 {
		return line
	}
	for _, diffLine := range hunk.Lines {
		if diffLine.Kind == diff.LineDelete && diffLine.OldLine == baselineLine {
			return line
		}
		if diffLine.NewLine > 0 {
			line = diffLine.NewLine + 1
		}
	}
	return max(1, hunk.NewStart)
}

// applyPendingFileViewAnchor resolves an anchor once full-file content is
// available. It returns true when it positioned both cursor and viewport.
func (m *Model) applyPendingFileViewAnchor() bool {
	path := m.currentFilePath()
	anchor, ok := m.fileViewAnchors[path]
	if !ok || m.viewMode != ViewFile {
		return false
	}
	state, loaded := m.fileContents[path]
	if !loaded || !state.loaded || state.err != nil {
		return false
	}
	rows := m.currentRows()
	idx, found := rowIndexForNewLine(rows, anchor.line)
	if !found {
		last := -1
		for i, row := range rows {
			line := rowLineNumberForSide(row, false)
			if line <= 0 {
				continue
			}
			last = i
			if line >= anchor.line {
				idx, found = i, true
				break
			}
		}
		if !found && last >= 0 {
			idx, found = last, true
		}
	}
	delete(m.fileViewAnchors, path)
	if !found {
		return false
	}
	m.cursor = idx
	m.setCursorCol(anchor.col)
	m.clampScroll()
	m.scrollCursorToScreenRowAllowingEOFSpace(anchor.screenRow)
	return true
}

func (m *Model) expandCurrentHunk(delta int) tea.Cmd {
	if delta == 0 {
		return nil
	}
	hunkIdx, ok := m.currentHunkIndex()
	if !ok {
		return nil
	}
	path := m.currentFilePath()
	if path == "" {
		return nil
	}
	if m.localExpansions == nil {
		m.localExpansions = make(map[string]map[int]int)
	}
	if m.localExpansions[path] == nil {
		m.localExpansions[path] = make(map[int]int)
	}
	next := m.localExpansions[path][hunkIdx] + delta
	m.rowsVersion++
	if next <= 0 {
		delete(m.localExpansions[path], hunkIdx)
		if len(m.localExpansions[path]) == 0 {
			delete(m.localExpansions, path)
		}
		return nil
	}
	m.localExpansions[path][hunkIdx] = next
	return m.ensureCurrentFileContentCmd()
}

func (m *Model) expandAllHunks(delta int) tea.Cmd {
	if delta <= 0 || m.selectedFile < 0 || m.selectedFile >= len(m.files) {
		return nil
	}
	path := m.currentFilePath()
	if path == "" {
		return nil
	}
	if m.localExpansions == nil {
		m.localExpansions = make(map[string]map[int]int)
	}
	if m.localExpansions[path] == nil {
		m.localExpansions[path] = make(map[int]int)
	}
	for i := range m.files[m.selectedFile].Hunks {
		m.localExpansions[path][i] += delta
	}
	m.rowsVersion++
	return m.ensureCurrentFileContentCmd()
}

func (m *Model) clearLocalExpansions() {
	path := m.currentFilePath()
	if path == "" || m.localExpansions == nil {
		return
	}
	delete(m.localExpansions, path)
	m.rowsVersion++
}

func (m *Model) currentHunkIndex() (int, bool) {
	if m.selectedFile < 0 || m.selectedFile >= len(m.files) {
		return 0, false
	}
	rows := m.currentRows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return 0, false
	}
	file := m.files[m.selectedFile]
	if rows[m.cursor].HunkIdx > 0 && rows[m.cursor].HunkIdx <= len(file.Hunks) {
		return rows[m.cursor].HunkIdx - 1, true
	}
	lineNum := sourceLine(rows[m.cursor])
	if lineNum > 0 {
		for i, h := range file.Hunks {
			if hunkCoversCurrentLine(h, lineNum) {
				return i, true
			}
		}
	}
	for up, down := m.cursor-1, m.cursor+1; up >= 0 || down < len(rows); {
		if up >= 0 {
			if rows[up].HunkIdx > 0 && rows[up].HunkIdx <= len(file.Hunks) {
				return rows[up].HunkIdx - 1, true
			}
			up--
		}
		if down < len(rows) {
			if rows[down].HunkIdx > 0 && rows[down].HunkIdx <= len(file.Hunks) {
				return rows[down].HunkIdx - 1, true
			}
			down++
		}
	}
	return 0, false
}

func hunkCoversCurrentLine(h diff.Hunk, lineNum int) bool {
	if h.NewLines <= 0 {
		return lineNum == h.NewStart
	}
	return lineNum >= h.NewStart && lineNum < h.NewStart+h.NewLines
}

func (m *Model) currentRows() []ui.Row {
	rows := m.currentRowsUnified()
	if m.splitViewActive() {
		rows = ui.PairRows(rows)
	}
	return m.withCommentRows(rows)
}

func (m *Model) currentRowsUnified() []ui.Row {
	if m.selectedFile < 0 || m.selectedFile >= len(m.files) {
		return nil
	}
	f := m.files[m.selectedFile]
	if m.viewMode != ViewFile && !m.currentFileHasLocalExpansion() {
		return ui.FlattenFile(m.files, m.selectedFile)
	}
	if f.Binary {
		return ui.MessageRows(m.selectedFile, "(binary file)")
	}
	path := f.Path()
	state, ok := m.fileContents[path]
	if !ok || state.loading {
		return ui.MessageRows(m.selectedFile, "(loading file...)")
	}
	if state.err != nil {
		return ui.MessageRows(m.selectedFile, "(current file unavailable: "+state.err.Error()+")")
	}
	if len(state.lines) == 0 {
		rows := ui.FlattenReviewFile(m.files, m.selectedFile, state.lines, m.currentLocalExpansions(), m.viewMode == ViewFile)
		if len(rows) > 0 {
			return rows
		}
		return ui.MessageRows(m.selectedFile, "(empty file)")
	}
	return ui.FlattenReviewFile(m.files, m.selectedFile, state.lines, m.currentLocalExpansions(), m.viewMode == ViewFile)
}

func (m *Model) currentFileNeedsContent() bool {
	if m.selectedFile < 0 || m.selectedFile >= len(m.files) {
		return false
	}
	if m.files[m.selectedFile].Binary {
		return false
	}
	return m.viewMode == ViewFile || m.currentFileHasLocalExpansion()
}

func (m *Model) currentFileHasLocalExpansion() bool {
	for _, n := range m.currentLocalExpansions() {
		if n > 0 {
			return true
		}
	}
	return false
}

func (m *Model) currentLocalExpansions() map[int]int {
	path := m.currentFilePath()
	if path == "" || m.localExpansions == nil {
		return nil
	}
	return m.localExpansions[path]
}

func fileOrderPosition(order []int, fileIdx int) int {
	for i, idx := range order {
		if idx == fileIdx {
			return i
		}
	}
	return -1
}

func (m *Model) currentFilePath() string {
	if m.selectedFile < 0 || m.selectedFile >= len(m.files) {
		return ""
	}
	return m.files[m.selectedFile].Path()
}

func (m *Model) saveCurrentFileState() {
	path := m.currentFilePath()
	if path == "" {
		return
	}
	if m.fileStates == nil {
		m.fileStates = make(map[fileStateKey]fileState)
	}
	m.fileStates[fileStateKey{path: path, mode: m.viewMode}] = fileState{cursor: m.cursor, col: m.col, top: m.top, topWrap: m.topWrap}
	m.captureSessionFileState()
}

func (m *Model) restoreCurrentFileState() {
	path := m.currentFilePath()
	if path == "" {
		m.cursor, m.top, m.topWrap = 0, 0, 0
		return
	}
	if fs, ok := m.fileStates[fileStateKey{path: path, mode: m.viewMode}]; ok {
		m.cursor, m.top, m.topWrap = fs.cursor, fs.top, fs.topWrap
		m.setCursorCol(fs.col)
	} else {
		m.cursor, m.top, m.topWrap = 0, 0, 0
		m.setCursorCol(0)
		m.applySessionFileState()
	}
	m.applyPendingFileViewAnchor()
	m.restoreSearchForCurrentFile()
	if m.enrichmentPanel.Open && m.enrichmentPanel.Kind == enrichmentPanelOutlineDiff && !m.outlineWholeReview {
		m.refreshOutlinePanel()
	}
}

func fileIndexByPath(files []diff.FileDiff, path string) int {
	if idx := findFileIndexByPath(files, path); idx >= 0 {
		return idx
	}
	if len(files) == 0 {
		return 0
	}
	return 0
}

func findFileIndexByPath(files []diff.FileDiff, path string) int {
	if path == "" {
		return -1
	}
	for i, f := range files {
		if f.Path() == path {
			return i
		}
	}
	return -1
}

func findFileIndexByPathSide(files []diff.FileDiff, path string, side navsearch.ResultSide) int {
	if path == "" {
		return -1
	}
	if side != navsearch.ResultSideBaseline {
		return findFileIndexByPath(files, path)
	}
	for i, f := range files {
		if f.OldPath == path {
			return i
		}
		if (f.OldPath == "" || f.OldPath == "/dev/null") && f.Path() == path {
			return i
		}
	}
	return -1
}

// clampScroll keeps the cursor row fully visible in screen-line space. A row
// taller than the viewport pins its first screen line to the top.
func (m *Model) clampScroll() {
	vh := m.viewHeight()
	if len(m.files) == 0 {
		m.selectedFile, m.cursor, m.top, m.topWrap = 0, 0, 0, 0
		return
	}
	m.selectedFile = min(max(m.selectedFile, 0), len(m.files)-1)
	m.revealSelectedFile()
	if m.currentFileContentPending() {
		m.cursor = max(m.cursor, 0)
		m.top = max(m.top, 0)
		return
	}
	rows := m.currentRows()
	if len(rows) == 0 {
		m.cursor, m.top, m.topWrap = 0, 0, 0
		m.col = 0
		return
	}
	m.cursor = min(max(m.cursor, 0), len(rows)-1)
	m.clampCursorColWithRows(rows)
	l := m.layoutFor(rows)
	topSL := m.topScreenLine(l)
	curStart := l.RowStart(m.cursor)
	curEnd := curStart + l.RowHeight(m.cursor)
	if curStart < topSL {
		topSL = curStart
	}
	if curEnd > topSL+vh {
		topSL = curEnd - vh
	}
	if l.RowHeight(m.cursor) > vh {
		topSL = curStart
	}
	topSL = min(max(topSL, 0), max(0, l.TotalLines()-vh))
	m.setTopScreenLine(l, topSL)
}

func (m *Model) currentFileContentPending() bool {
	if !m.currentFileNeedsContent() || m.selectedFile < 0 || m.selectedFile >= len(m.files) {
		return false
	}
	if m.files[m.selectedFile].Binary {
		return false
	}
	state, ok := m.fileContents[m.files[m.selectedFile].Path()]
	return !ok || state.loading
}

func (m Model) currentSymbolQuery() (navsearch.SymbolQuery, bool) {
	queries, ok := m.currentSymbolQueries()
	if !ok || len(queries) == 0 {
		return navsearch.SymbolQuery{}, false
	}
	return queries[0], true
}

func (m Model) currentSymbolQueries() ([]navsearch.SymbolQuery, bool) {
	if m.selectedFile < 0 || m.selectedFile >= len(m.files) {
		return nil, false
	}
	rows := m.currentRows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return nil, false
	}
	row := rows[m.cursor]
	file := m.files[m.selectedFile]
	var (
		loc  source.Location
		ok   bool
		side navsearch.ResultSide
	)
	// In side-by-side the active column decides which side symbols resolve on.
	if row.Kind == ui.RowPair && m.splitActiveLeft {
		loc, ok = baselineLocationForRow(file, row)
		side = navsearch.ResultSideBaseline
		if !ok {
			loc, ok = currentLocationForRow(file, row)
			side = navsearch.ResultSideCurrent
		}
	} else {
		loc, ok = currentLocationForRow(file, row)
		side = navsearch.ResultSideCurrent
		if !ok {
			loc, ok = baselineLocationForRow(file, row)
			side = navsearch.ResultSideBaseline
		}
	}
	if !ok {
		return nil, false
	}
	sideLine, _ := rowSideLine(row, side == navsearch.ResultSideBaseline)
	line := sideLine.Content
	// The character cursor resolves the symbol directly when it sits on one,
	// skipping the inline ←/→ candidate choice.
	if byteCol, ok := byteColumnAtRune(line, m.col); ok {
		if symbol, symCol, ok := navsearch.ExtractNonKeywordIdentifier(line, byteCol); ok {
			loc.Column = symCol
			return []navsearch.SymbolQuery{{Symbol: symbol, Location: loc, Side: side}}, true
		}
	}
	var symbol string
	var col int
	var symbolOK bool
	if loc.Column > 1 {
		symbol, col, symbolOK = navsearch.ExtractIdentifier(line, loc.Column)
		if symbolOK {
			loc.Column = col
			return []navsearch.SymbolQuery{{Symbol: symbol, Location: loc, Side: side}}, true
		}
	}
	identifiers := navsearch.NonKeywordIdentifiers(line)
	if len(identifiers) > 0 {
		queries := make([]navsearch.SymbolQuery, 0, len(identifiers))
		for _, identifier := range identifiers {
			queryLoc := loc
			queryLoc.Column = identifier.Column
			queries = append(queries, navsearch.SymbolQuery{Symbol: identifier.Symbol, Location: queryLoc, Side: side})
		}
		return queries, true
	}
	symbol, col, symbolOK = navsearch.FirstNonKeywordIdentifier(line)
	if !symbolOK {
		symbol, col, symbolOK = navsearch.ExtractIdentifier(line, loc.Column)
	}
	if !symbolOK {
		return nil, false
	}
	loc.Column = col
	return []navsearch.SymbolQuery{{Symbol: symbol, Location: loc, Side: side}}, true
}

func splitContentLines(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
}

func changedPathSet(files []diff.FileDiff) map[string]bool {
	paths := map[string]bool{}
	for _, f := range files {
		path := f.Path()
		if path != "" && path != "/dev/null" {
			paths[path] = true
		}
	}
	return paths
}

func (m Model) reviewChangedPaths() map[string]bool {
	if m.changedPaths != nil {
		return m.changedPaths
	}
	return changedPathSet(m.files)
}

func (m Model) reviewIndex() diff.StaticReviewIndex {
	return diff.NewReviewIndex(m.files)
}

func (m Model) currentReviewLocation() source.Location {
	if m.selectedFile < 0 || m.selectedFile >= len(m.files) {
		return source.Location{}
	}
	rows := m.currentRows()
	if m.cursor >= 0 && m.cursor < len(rows) {
		if loc, ok := currentLocationForRow(m.files[m.selectedFile], rows[m.cursor]); ok {
			return loc
		}
	}
	return source.Location{Path: m.currentFilePath()}
}

func navigationReviewScore(loc, current source.Location, markers diff.ReviewMarkers, review diff.ReviewIndex, definition bool) int {
	score := 0
	if loc.Path != "" && loc.Path == current.Path {
		score += 10000
		if current.Line > 0 && loc.Line > 0 {
			delta := loc.Line - current.Line
			if delta < 0 {
				delta = -delta
			}
			score += max(0, 1000-delta)
		}
	}
	if markers.Changed || markers.ContainsAddition || markers.ContainsDeletion {
		score += 8000
		if markers.Unread {
			score += 1500
		}
	}
	if markers.Annotated {
		if markers.Annotation.Open() {
			score += 7000
		} else {
			score += 1200
		}
	}
	if review != nil && review.IsChanged(loc.Path) {
		score += 5000
	}
	if navsearch.IsLikelyTest(loc.Path) {
		score += 2500
	}
	if definition {
		score += 2000
	}
	return score
}

func locationLess(a, b source.Location, aLabel, bLabel string) bool {
	if a.Path != b.Path {
		return a.Path < b.Path
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	if a.Column != b.Column {
		return a.Column < b.Column
	}
	return aLabel < bLabel
}

func changedLineSet(files []diff.FileDiff) map[string]map[int]bool {
	byPath := map[string]map[int]bool{}
	for _, f := range files {
		path := f.Path()
		if path == "" || path == "/dev/null" {
			continue
		}
		for _, h := range f.Hunks {
			for _, ln := range h.Lines {
				if ln.Kind != diff.LineAdd || ln.NewLine < 1 {
					continue
				}
				if byPath[path] == nil {
					byPath[path] = map[int]bool{}
				}
				byPath[path][ln.NewLine] = true
			}
		}
	}
	return byPath
}

func (m Model) changedFilePaths() []string {
	paths := make([]string, 0, len(m.files))
	for _, f := range m.files {
		path := f.Path()
		if path != "" && path != "/dev/null" {
			paths = append(paths, path)
		}
	}
	return paths
}

func (m Model) renderRows() []ui.Row {
	rows := m.currentRows()
	if len(rows) == 0 || len(m.diagnostics) == 0 || m.selectedFile < 0 || m.selectedFile >= len(m.files) {
		return rows
	}
	out := make([]ui.Row, len(rows))
	copy(out, rows)
	file := m.files[m.selectedFile]
	path := file.Path()
	for i, row := range out {
		loc, ok := currentLocationForRow(file, row)
		if !ok {
			continue
		}
		if marker := m.diagnosticMarkerAt(path, loc.Line); marker != "" {
			out[i].DiagnosticMarker = marker
		}
	}
	return out
}

func (m Model) diagnosticMarkerAt(path string, line int) string {
	var best lsp.DiagnosticSeverity
	for _, diagnostic := range m.diagnostics[path] {
		if !diagnostic.CoversLine(path, line) {
			continue
		}
		if best == 0 || diagnostic.Severity < best {
			best = diagnostic.Severity
		}
	}
	if best == 0 {
		return ""
	}
	return best.Marker()
}

func (m Model) semanticStatusLine() string {
	statuses := map[string]lsp.Status{}
	for key, status := range m.lspStatuses {
		if status.Enabled() && status.State != lsp.StateDisabled {
			statuses[key] = status
		}
	}
	if m.lsp != nil {
		status := m.lsp.Status(m.currentFilePath())
		if status.Enabled() && status.State != lsp.StateDisabled {
			statuses[status.Key()] = status
		}
	}
	if len(statuses) == 0 {
		return ""
	}
	keys := make([]string, 0, len(statuses))
	for key := range statuses {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	labels := make([]string, 0, len(keys))
	for _, key := range keys {
		if label := statuses[key].Label(); label != "" {
			labels = append(labels, label)
		}
	}
	return strings.Join(labels, "  ")
}

func recentPathRanks(paths []string) map[string]int {
	if len(paths) == 0 {
		return nil
	}
	ranks := make(map[string]int, len(paths))
	for i, path := range paths {
		ranks[path] = i
	}
	return ranks
}

func currentLocationForRow(file diff.FileDiff, row ui.Row) (source.Location, bool) {
	line, ok := rowSideLine(row, false)
	if !ok || line.NewLine == 0 || line.Kind == diff.LineDelete {
		return source.Location{}, false
	}
	path := file.NewPath
	if path == "" || path == "/dev/null" {
		path = file.Path()
	}
	if path == "" || path == "/dev/null" {
		return source.Location{}, false
	}
	return source.Location{Path: path, Line: line.NewLine, Column: 1}, true
}

func baselineLocationForRow(file diff.FileDiff, row ui.Row) (source.Location, bool) {
	line, ok := rowSideLine(row, true)
	if !ok || line.OldLine == 0 || line.Kind == diff.LineAdd {
		return source.Location{}, false
	}
	path := file.OldPath
	if path == "" || path == "/dev/null" {
		path = file.Path()
	}
	if path == "" || path == "/dev/null" {
		return source.Location{}, false
	}
	return source.Location{Path: path, Line: line.OldLine, Column: 1}, true
}

// rowSideLine picks the requested side of a row: pair rows have explicit
// sides, unified line rows carry one line that serves both.
func rowSideLine(row ui.Row, baseline bool) (diff.Line, bool) {
	switch row.Kind {
	case ui.RowLine:
		return row.Line, true
	case ui.RowPair:
		if baseline {
			if row.Left == nil {
				return diff.Line{}, false
			}
			return *row.Left, true
		}
		if row.Right == nil {
			return diff.Line{}, false
		}
		return *row.Right, true
	default:
		return diff.Line{}, false
	}
}
