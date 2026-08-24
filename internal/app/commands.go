package app

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/search"
	"cride/internal/source"
	"cride/internal/ui"
)

// CommandContext is the state supplied to a Command when it runs. Keeping the
// model and invocation details explicit lets keyboard shortcuts and the
// command palette execute exactly the same action.
type CommandContext struct {
	Model    *Model
	Count    int
	HasCount bool
}

// Command is one user-facing action. Categorized actions appear in the
// command palette; low-level motions remain Command objects used by shortcuts.
type Command struct {
	ID             string
	Name           string
	Keys           string
	Description    string
	Category       CommandCategory
	Execute        func(CommandContext) tea.Cmd
	preserveScroll bool
}

// CommandCategory is a browsable tab in the command palette. An empty
// category deliberately keeps keystroke-level actions (for example, moving
// one row) out of the palette without bypassing Command dispatch.
type CommandCategory string

const (
	CommandCategoryReview    CommandCategory = "Review"
	CommandCategoryFiles     CommandCategory = "Files"
	CommandCategoryCode      CommandCategory = "Code"
	CommandCategoryView      CommandCategory = "View"
	CommandCategoryEdit      CommandCategory = "Edit"
	CommandCategoryOperators CommandCategory = "Operators"
	CommandCategoryGeneral   CommandCategory = "General"
)

var commandPaletteCategories = []CommandCategory{
	CommandCategoryCode,
	CommandCategoryReview,
	CommandCategoryFiles,
	CommandCategoryView,
	CommandCategoryEdit,
	CommandCategoryOperators,
	CommandCategoryGeneral,
}

const (
	commandCallsIncoming         = "calls.incoming"
	commandCallsOutgoing         = "calls.outgoing"
	commandChangeLine            = "edit.change-line"
	commandChangeToLineEnd       = "edit.change-to-line-end"
	commandCloseActivePanel      = "app.close-active-panel"
	commandClearContextAll       = "view.clear-context-all"
	commandClearSearch           = "search.clear-current"
	commandCollapseContext       = "view.collapse-context"
	commandCollapseDirectory     = "files.collapse-directory"
	commandCommentCurrent        = "comments.current"
	commandCommentGeneral        = "comments.general"
	commandCursorDown            = "cursor.down"
	commandCursorFirstNonBlank   = "cursor.first-non-blank"
	commandCursorLeft            = "cursor.left"
	commandCursorLineEnd         = "cursor.line-end"
	commandCursorLineStart       = "cursor.line-start"
	commandCursorRight           = "cursor.right"
	commandCursorUp              = "cursor.up"
	commandCursorWordBackward    = "cursor.word-backward"
	commandCursorWordEnd         = "cursor.word-end"
	commandCursorWordForward     = "cursor.word-forward"
	commandDeleteCharacter       = "edit.delete-character"
	commandDeleteLine            = "edit.delete-line"
	commandDeleteToLineEnd       = "edit.delete-to-line-end"
	commandDiagnosticsCurrent    = "diagnostics.current"
	commandDiagnosticsWorkspace  = "diagnostics.workspace"
	commandDiscardEdits          = "edit.discard"
	commandDocumentSymbols       = "symbols.document"
	commandEditAppend            = "edit.append"
	commandEditAppendLineEnd     = "edit.append-line-end"
	commandEditChange            = "edit.change"
	commandEditDelete            = "edit.delete"
	commandEditInsert            = "edit.insert"
	commandEditInsertLineStart   = "edit.insert-line-start"
	commandEditOpenLineAbove     = "edit.open-line-above"
	commandEditOpenLineBelow     = "edit.open-line-below"
	commandEditPasteAfter        = "edit.paste-after"
	commandEditPasteBefore       = "edit.paste-before"
	commandEditRedo              = "edit.redo"
	commandEditReplace           = "edit.replace"
	commandEditSubstitute        = "edit.substitute"
	commandEditSubstituteLine    = "edit.substitute-line"
	commandEditUndo              = "edit.undo"
	commandEditYank              = "edit.yank"
	commandExitEditMode          = "edit.exit"
	commandExpandContext         = "view.expand-context"
	commandExpandContextAll      = "view.expand-context-all"
	commandExpandDirectory       = "files.expand-directory"
	commandExportReview          = "comments.export"
	commandFindBackward          = "cursor.find-backward"
	commandFindForward           = "cursor.find-forward"
	commandFocusChangeList       = "focus.change-list"
	commandFocusDiff             = "focus.diff"
	commandGoToDefinition        = "navigation.definition"
	commandGoToFileBottom        = "cursor.file-bottom"
	commandGoToFileTop           = "cursor.file-top"
	commandHover                 = "navigation.hover"
	commandImpact                = "navigation.impact"
	commandJoinLines             = "edit.join-lines"
	commandJumpBack              = "navigation.back"
	commandJumpForward           = "navigation.forward"
	commandJumpMatchingBracket   = "cursor.match-bracket"
	commandJumpSourceLine        = "cursor.source-line"
	commandMarkAllRead           = "unread.mark-all-read"
	commandMarkCurrentRead       = "unread.mark-current-read"
	commandMarkCurrentUnread     = "unread.mark-current-unread"
	commandMoveViewportBottom    = "cursor.viewport-bottom"
	commandMoveViewportTop       = "cursor.viewport-top"
	commandNextAnnotation        = "comments.next"
	commandNextFile              = "files.next"
	commandNextHunk              = "hunks.next"
	commandNextUnreadOrMatch     = "navigation.next-unread-or-match"
	commandOpenFile              = "files.open"
	commandOpenListSelection     = "files.open-selection"
	commandOpenPalette           = "app.command-palette"
	commandOutlineChanges        = "symbols.changed-outline"
	commandPreviousAnnotation    = "comments.previous"
	commandPreviousFile          = "files.previous"
	commandPreviousHunk          = "hunks.previous"
	commandPreviousUnreadOrMatch = "navigation.previous-unread-or-match"
	commandProjectSearch         = "search.project"
	commandQuit                  = "app.quit"
	commandReferences            = "navigation.references"
	commandReferencesChanged     = "navigation.references-changed"
	commandReload                = "app.reload"
	commandRepeatFindBackward    = "cursor.repeat-find-backward"
	commandRepeatFindForward     = "cursor.repeat-find-forward"
	commandSaveEdits             = "edit.save"
	commandScrollHalfPageDown    = "cursor.half-page-down"
	commandScrollHalfPageUp      = "cursor.half-page-up"
	commandScrollPageDown        = "cursor.page-down"
	commandScrollPageUp          = "cursor.page-up"
	commandSearchCurrentFile     = "search.current-file"
	commandTillBackward          = "cursor.till-backward"
	commandTillForward           = "cursor.till-forward"
	commandToggleCommentResolved = "comments.toggle-resolved"
	commandToggleFileListOrder   = "files.toggle-order"
	commandToggleFullFile        = "view.toggle-full-file"
	commandToggleOutlineScope    = "symbols.toggle-outline-scope"
	commandToggleResultOrder     = "navigation.toggle-result-order"
	commandToggleSideBySide      = "view.toggle-side-by-side"
	commandWorkspaceSymbols      = "symbols.workspace"
	commandYankLine              = "edit.yank-line"
)

var commandCatalog = []Command{
	command(commandEditAppend, "Append after cursor", "a", "Enter insert mode after the cursor.", func(c CommandContext) tea.Cmd {
		if c.Model.mode == modeEdit {
			return c.Model.startInsert(insertAfterCursor)
		}
		return c.Model.enterEditMode('a')
	}),
	command(commandEditAppendLineEnd, "Append at line end", "A", "Enter insert mode at the end of the line.", editOnly(func(c CommandContext) tea.Cmd {
		return c.Model.startInsert(insertAtLineEnd)
	})),
	command(commandCallsIncoming, "Calls: show incoming", "gI", "Show callers of the symbol under the cursor.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.openCallHierarchyPanel(enrichmentPanelCallIncoming)
	})),
	command(commandCallsOutgoing, "Calls: show outgoing", "gO", "Show calls made by the symbol under the cursor.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.openCallHierarchyPanel(enrichmentPanelCallOutgoing)
	})),
	command(commandChangeLine, "Change line", "cc", "Delete the current line and enter insert mode.", editOnly(func(c CommandContext) tea.Cmd {
		return c.Model.applyOperator('c', "c", commandCount(c))
	})),
	command(commandChangeToLineEnd, "Change to line end", "C", "Delete to the end of the line and enter insert mode.", editOnly(func(c CommandContext) tea.Cmd {
		return c.Model.applyOperator('c', "$", commandCount(c))
	})),
	command(commandEditChange, "Change with motion", "c + motion", "Arm the change operator for a following motion.", editOnly(func(c CommandContext) tea.Cmd {
		c.Model.pendingOp = 'c'
		c.Model.pendingOpCount = commandCount(c)
		return nil
	})),
	command(commandClearContextAll, "Clear all expanded context", "zC", "Remove local context expansion from every hunk in the file.", reviewOnly(func(c CommandContext) tea.Cmd {
		c.Model.clearLocalExpansions()
		return nil
	})),
	command(commandClearSearch, "Clear current-file search", "esc", "Clear the active in-file search.", reviewOnly(func(c CommandContext) tea.Cmd {
		c.Model.clearInFileSearch()
		return nil
	})),
	command(commandCloseActivePanel, "Close active panel", "esc", "Close the active results panel or return focus to the diff.", reviewOnly(func(c CommandContext) tea.Cmd {
		switch {
		case c.Model.enrichmentPanel.Open:
			c.Model.enrichmentPanel = enrichmentPanelState{}
		case c.Model.referencePanel.Open:
			c.Model.referencePanel = referencePanelState{}
		case c.Model.focus == paneList:
			c.Model.focusDiff()
		case c.Model.search.active:
			c.Model.clearInFileSearch()
		}
		return nil
	})),
	command(commandCollapseContext, "Collapse context around hunk", "zc", "Reduce context around the current hunk.", reviewOnly(func(c CommandContext) tea.Cmd {
		c.Model.expandCurrentHunk(-commandCount(c) * localExpansionStep)
		return nil
	})),
	command(commandCollapseDirectory, "Collapse directory", "h / left", "Collapse the selected change-list directory.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.collapseSelectedDirectory()
	})),
	command(commandCommentCurrent, "Comment on current line", "c", "Compose a review comment anchored to the current line.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.openComposer(false)
	})),
	command(commandCommentGeneral, "Comment: add general", "C", "Compose a review-wide comment.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.openComposer(true)
	})),
	command(commandCursorDown, "Cursor: move down", "j / down", "Move the cursor down.", func(c CommandContext) tea.Cmd {
		if c.Model.focus == paneList && c.Model.mode == modeReview {
			c.Model.moveChangeListCursor(commandCount(c))
			return nil
		}
		c.Model.move(commandCount(c))
		return nil
	}),
	command(commandCursorFirstNonBlank, "Cursor: move to first non-blank", "^", "Move to the first non-blank character.", func(c CommandContext) tea.Cmd {
		c.Model.cursorFirstNonBlank()
		return nil
	}),
	command(commandCursorLeft, "Cursor: move left", "h / left", "Move the cursor left.", func(c CommandContext) tea.Cmd {
		c.Model.moveCursorCol(-commandCount(c))
		return nil
	}),
	command(commandCursorLineEnd, "Cursor: move to line end", "$", "Move to the end of the line.", func(c CommandContext) tea.Cmd {
		c.Model.cursorLineEnd()
		return nil
	}),
	command(commandCursorLineStart, "Cursor: move to line start", "0", "Move to the start of the line.", func(c CommandContext) tea.Cmd {
		c.Model.cursorLineStart()
		return nil
	}),
	command(commandCursorRight, "Cursor: move right", "l / right", "Move the cursor right.", func(c CommandContext) tea.Cmd {
		c.Model.moveCursorCol(commandCount(c))
		return nil
	}),
	command(commandCursorUp, "Cursor: move up", "k / up", "Move the cursor up.", func(c CommandContext) tea.Cmd {
		if c.Model.focus == paneList && c.Model.mode == modeReview {
			c.Model.moveChangeListCursor(-commandCount(c))
			return nil
		}
		c.Model.move(-commandCount(c))
		return nil
	}),
	command(commandCursorWordBackward, "Cursor: previous word", "b", "Move to the previous word.", func(c CommandContext) tea.Cmd {
		c.Model.cursorWordBackward(commandCount(c))
		return nil
	}),
	command(commandCursorWordEnd, "Cursor: word end", "e", "Move to the end of the next word in edit mode.", editOnly(func(c CommandContext) tea.Cmd {
		c.Model.cursorWordEnd(commandCount(c))
		return nil
	})),
	command(commandCursorWordForward, "Cursor: next word", "w", "Move to the next word.", func(c CommandContext) tea.Cmd {
		c.Model.cursorWordForward(commandCount(c))
		return nil
	}),
	command(commandDeleteCharacter, "Delete character", "x", "Delete characters at the cursor in edit mode.", editOnly(func(c CommandContext) tea.Cmd {
		return c.Model.applyOperator('d', "l", commandCount(c))
	})),
	command(commandDeleteLine, "Delete line", "dd", "Delete lines into the edit register.", editOnly(func(c CommandContext) tea.Cmd {
		return c.Model.applyOperator('d', "d", commandCount(c))
	})),
	command(commandDeleteToLineEnd, "Delete to line end", "D", "Delete from the cursor to the end of the line.", editOnly(func(c CommandContext) tea.Cmd {
		return c.Model.applyOperator('d', "$", commandCount(c))
	})),
	command(commandEditDelete, "Delete with motion", "d + motion", "Arm the delete operator for a following motion.", editOnly(func(c CommandContext) tea.Cmd {
		c.Model.pendingOp = 'd'
		c.Model.pendingOpCount = commandCount(c)
		return nil
	})),
	command(commandDiagnosticsCurrent, "Diagnostics: current file", "ge", "Show diagnostics for the current file.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.openDiagnosticsPanel(false)
	})),
	command(commandDiagnosticsWorkspace, "Diagnostics: workspace", "gE", "Show diagnostics across changed files.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.openDiagnosticsPanel(true)
	})),
	command(commandDiscardEdits, "Discard edits and exit", "ZQ", "Discard the edit buffer and return to review mode.", editOnly(func(c CommandContext) tea.Cmd {
		return c.Model.discardEditBufferAndExit()
	})),
	command(commandDocumentSymbols, "Document symbols", "gs", "Show symbols in the current document.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.openDocumentSymbolsPanel()
	})),
	command(commandExitEditMode, "Exit edit mode", "esc", "Return to review mode when there are no unsaved edits.", editOnly(func(c CommandContext) tea.Cmd {
		if c.Model.editDirty {
			return c.Model.notify(ui.ToastWarn, "unsaved edits — ZZ saves, ZQ discards")
		}
		return c.Model.exitEditMode()
	})),
	command(commandExpandContextAll, "Expand context around all hunks", "zO", "Add context around every hunk in the file.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.expandAllHunks(commandCount(c) * localExpansionStep)
	})),
	command(commandExpandContext, "Expand context around hunk", "zo", "Add context around the current hunk.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.expandCurrentHunk(commandCount(c) * localExpansionStep)
	})),
	command(commandExpandDirectory, "Expand directory", "l / right", "Expand the selected change-list directory.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.expandSelectedDirectory()
	})),
	command(commandExportReview, "Save review", "ctrl+s / e", "Save the review comments and refresh review.md without leaving cride.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.exportReviewCmd()
	})),
	command(commandFindBackward, "Find character backward", "F", "Wait for a character, then find it backward on the line.", func(c CommandContext) tea.Cmd {
		c.Model.pendingFind = 'F'
		return nil
	}),
	command(commandFindForward, "Find character forward", "f", "Wait for a character, then find it forward on the line.", func(c CommandContext) tea.Cmd {
		c.Model.pendingFind = 'f'
		return nil
	}),
	command(commandFocusChangeList, "Focus change list", "ctrl+h", "Move keyboard focus to the change list.", reviewOnly(func(c CommandContext) tea.Cmd {
		c.Model.focusChangeList()
		return nil
	})),
	command(commandFocusDiff, "Focus diff", "ctrl+l", "Move keyboard focus to the diff.", reviewOnly(func(c CommandContext) tea.Cmd {
		c.Model.focusDiff()
		return nil
	})),
	command(commandGoToDefinition, "Go to definition", "gd", "Jump to the definition of the symbol under the cursor.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.openReferencesPanel(referenceRequestDefinition, false)
	})),
	command(commandGoToFileBottom, "Go to file bottom", "G / end", "Move to the final row in the file.", func(c CommandContext) tea.Cmd {
		c.Model.cursor = len(c.Model.currentRows()) - 1
		return nil
	}),
	command(commandGoToFileTop, "Go to file top", "gg / home", "Move to the first row in the file.", func(c CommandContext) tea.Cmd {
		c.Model.cursor = 0
		return nil
	}),
	command(commandHover, "Hover documentation", "K", "Show documentation for the symbol under the cursor.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.openHoverPanel()
	})),
	command(commandImpact, "Impact: changed symbol", "gi", "Show references affected by the changed symbol under the cursor.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.openReferencesPanel(referenceRequestImpact, false)
	})),
	command(commandEditInsert, "Insert at cursor", "i", "Enter insert mode at the cursor.", func(c CommandContext) tea.Cmd {
		if c.Model.mode == modeEdit {
			return c.Model.startInsert(insertAtCursor)
		}
		return c.Model.enterEditMode('i')
	}),
	command(commandEditInsertLineStart, "Insert at first non-blank", "I", "Enter insert mode at the first non-blank character.", func(c CommandContext) tea.Cmd {
		if c.Model.mode == modeEdit {
			c.Model.cursorFirstNonBlank()
			return c.Model.startInsert(insertAtCursor)
		}
		return c.Model.enterEditMode('I')
	}),
	command(commandJoinLines, "Join lines", "J", "Join the current line with following lines in edit mode.", editOnly(func(c CommandContext) tea.Cmd {
		count := 2
		if c.HasCount {
			count = commandCount(c)
		}
		return c.Model.joinLines(count)
	})),
	command(commandJumpBack, "Jump history: back", "ctrl+o", "Jump to the previous navigation location.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.jumpBack()
	})),
	command(commandJumpForward, "Jump history: forward", "ctrl+]", "Jump to the next navigation location.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.jumpForward()
	})),
	command(commandJumpMatchingBracket, "Jump to matching bracket", "%", "Move to the matching bracket on the current file.", func(c CommandContext) tea.Cmd {
		c.Model.cursorMatchBracket()
		return nil
	}),
	command(commandJumpSourceLine, "Jump to source line", "{count}G", "Jump to the source line supplied by the numeric count.", func(c CommandContext) tea.Cmd {
		c.Model.jumpSourceLine(commandCount(c))
		return nil
	}),
	command(commandMarkAllRead, "Mark all files read", "A", "Mark every changed file as read.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.markAllRead()
	})),
	command(commandMarkCurrentRead, "Mark current file read and advance", "R", "Mark the current file read, then open the next unread file.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.markCurrentFileReadAndAdvance()
	})),
	command(commandMarkCurrentUnread, "Mark current file unread", "U", "Mark the current file as unread.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.markCurrentFileUnread()
	})),
	command(commandMoveViewportBottom, "Move to viewport bottom", "L", "Move to the bottom visible row.", func(c CommandContext) tea.Cmd {
		c.Model.moveViewportEdge(1)
		return nil
	}),
	command(commandMoveViewportTop, "Move to viewport top", "H", "Move to the top visible row.", func(c CommandContext) tea.Cmd {
		c.Model.moveViewportEdge(-1)
		return nil
	}),
	command(commandNextAnnotation, "Next comment", "]a", "Jump to the next review comment.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.stepAnnotation(1)
	})),
	command(commandNextFile, "Next file", "} / J / ]]", "Open the next changed file.", reviewOnly(func(c CommandContext) tea.Cmd {
		c.Model.switchFileN(1, commandCount(c))
		return c.Model.ensureCurrentFileContentCmd()
	})),
	preserveCommandScroll(command(commandNextHunk, "Next hunk", "]c", "Jump to the next diff hunk.", reviewOnly(func(c CommandContext) tea.Cmd {
		c.Model.jumpHunkKeepingScreenRow(1, commandCount(c))
		return nil
	}))),
	command(commandNextUnreadOrMatch, "Next unread file or search match", "n", "Step to the next search match, or the next unread file when no search is active.", reviewOnly(func(c CommandContext) tea.Cmd {
		if c.Model.search.active {
			return c.Model.stepSearchMatch(1, commandCount(c))
		}
		return c.Model.stepUnreadFile(1, commandCount(c))
	})),
	command(commandEditOpenLineAbove, "Open line above", "O", "Open a new line above and enter insert mode.", editOnly(func(c CommandContext) tea.Cmd {
		return c.Model.openLine(true)
	})),
	command(commandEditOpenLineBelow, "Open line below", "o", "Open a new line below and enter insert mode.", editOnly(func(c CommandContext) tea.Cmd {
		return c.Model.openLine(false)
	})),
	command(commandOpenFile, "Open file", "ctrl+p", "Open a project file by fuzzy name.", reviewOnly(func(c CommandContext) tea.Cmd {
		c.Model.openFileOverlay()
		return c.Model.loadProjectFilesCmd()
	})),
	command(commandOpenListSelection, "Open selected change-list item", "enter", "Open the selected file or toggle the selected directory.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.openSelectedChangeListItem()
	})),
	command(commandOpenPalette, "Open command palette", "? / f1 / g?", "Find and run any command.", func(c CommandContext) tea.Cmd {
		c.Model.startCommandPalette()
		return nil
	}),
	command(commandOutlineChanges, "Outline changed symbols", "gy", "Show changed symbols in the current file or review.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.openOutlinePanel()
	})),
	command(commandEditPasteAfter, "Paste after cursor", "p", "Paste the edit register after the cursor.", editOnly(func(c CommandContext) tea.Cmd {
		return c.Model.pasteRegister(false, commandCount(c))
	})),
	command(commandEditPasteBefore, "Paste before cursor", "P", "Paste the edit register before the cursor.", editOnly(func(c CommandContext) tea.Cmd {
		return c.Model.pasteRegister(true, commandCount(c))
	})),
	command(commandPreviousAnnotation, "Previous comment", "[a", "Jump to the previous review comment.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.stepAnnotation(-1)
	})),
	command(commandPreviousFile, "Previous file", "{ / [[", "Open the previous changed file.", reviewOnly(func(c CommandContext) tea.Cmd {
		c.Model.switchFileN(-1, commandCount(c))
		return c.Model.ensureCurrentFileContentCmd()
	})),
	preserveCommandScroll(command(commandPreviousHunk, "Previous hunk", "[c", "Jump to the previous diff hunk.", reviewOnly(func(c CommandContext) tea.Cmd {
		c.Model.jumpHunkKeepingScreenRow(-1, commandCount(c))
		return nil
	}))),
	command(commandPreviousUnreadOrMatch, "Previous unread file or search match", "N / shift+tab", "Step to the previous search match, or previous unread file when no search is active.", reviewOnly(func(c CommandContext) tea.Cmd {
		if c.Model.search.active {
			return c.Model.stepSearchMatch(-1, commandCount(c))
		}
		return c.Model.stepUnreadFile(-1, commandCount(c))
	})),
	command(commandProjectSearch, "Project search", "g/", "Search text across the project.", reviewOnly(func(c CommandContext) tea.Cmd {
		c.Model.openSearchOverlay()
		return nil
	})),
	command(commandQuit, "Quit", "q / ctrl+c", "Save the session and quit cride.", func(c CommandContext) tea.Cmd {
		if c.Model.mode != modeReview {
			c.Model.releaseEditLock()
		}
		c.Model.stopWatching()
		c.Model.saveSessionNow()
		return tea.Quit
	}),
	command(commandReferences, "References: find", "gr", "Find references to the symbol under the cursor.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.openReferencesPanel(referenceRequestUsages, false)
	})),
	command(commandReferencesChanged, "References: find in changed files", "gR", "Find references limited to changed files.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.openReferencesPanel(referenceRequestUsages, true)
	})),
	command(commandReload, "Reload review", "ctrl+r", "Reload the diff and import comments from review.md.", reviewOnly(func(c CommandContext) tea.Cmd {
		return tea.Batch(c.Model.reload(true), c.Model.loadReviewCmd())
	})),
	command(commandEditRedo, "Redo edit", "ctrl+r", "Redo changes in the edit buffer.", editOnly(func(c CommandContext) tea.Cmd {
		return c.Model.popEditRedo(commandCount(c))
	})),
	command(commandRepeatFindBackward, "Repeat character find backward", ",", "Repeat the last character find in reverse.", func(c CommandContext) tea.Cmd {
		c.Model.repeatFindChar(commandCount(c), true)
		return nil
	}),
	command(commandRepeatFindForward, "Repeat character find forward", ";", "Repeat the last character find.", func(c CommandContext) tea.Cmd {
		c.Model.repeatFindChar(commandCount(c), false)
		return nil
	}),
	command(commandEditReplace, "Replace character", "r", "Wait for a character, then replace at the cursor.", editOnly(func(c CommandContext) tea.Cmd {
		c.Model.pendingReplace = commandCount(c)
		return nil
	})),
	command(commandSaveEdits, "Save edits and exit", "ZZ", "Write the edit buffer and return to review mode.", editOnly(func(c CommandContext) tea.Cmd {
		return c.Model.saveEditBufferAndExit()
	})),
	command(commandScrollHalfPageDown, "Scroll half page down", "ctrl+d", "Move down by half a viewport.", func(c CommandContext) tea.Cmd {
		c.Model.move(commandCount(c) * max(1, c.Model.viewHeight()/2))
		return nil
	}),
	command(commandScrollHalfPageUp, "Scroll half page up", "ctrl+u", "Move up by half a viewport.", func(c CommandContext) tea.Cmd {
		c.Model.move(-commandCount(c) * max(1, c.Model.viewHeight()/2))
		return nil
	}),
	command(commandScrollPageDown, "Scroll page down", "ctrl+f / pgdown", "Scroll down by one viewport.", func(c CommandContext) tea.Cmd {
		c.Model.windowScroll(commandCount(c) * c.Model.viewHeight())
		return nil
	}),
	command(commandScrollPageUp, "Scroll page up", "ctrl+b / pgup", "Scroll up by one viewport.", func(c CommandContext) tea.Cmd {
		c.Model.windowScroll(-commandCount(c) * c.Model.viewHeight())
		return nil
	}),
	command(commandSearchCurrentFile, "Search current file", "/", "Search within the current rendered file.", reviewOnly(func(c CommandContext) tea.Cmd {
		c.Model.startInFileSearch()
		return nil
	})),
	command(commandEditSubstitute, "Substitute character", "s", "Delete characters and enter insert mode.", editOnly(func(c CommandContext) tea.Cmd {
		return c.Model.substituteCharacters(commandCount(c))
	})),
	command(commandEditSubstituteLine, "Substitute line", "S", "Change whole lines and enter insert mode.", editOnly(func(c CommandContext) tea.Cmd {
		return c.Model.applyOperator('c', "c", commandCount(c))
	})),
	command(commandTillBackward, "Till character backward", "T", "Wait for a character, then move just after it searching backward.", func(c CommandContext) tea.Cmd {
		c.Model.pendingFind = 'T'
		return nil
	}),
	command(commandTillForward, "Till character forward", "t", "Wait for a character, then move just before it searching forward.", func(c CommandContext) tea.Cmd {
		c.Model.pendingFind = 't'
		return nil
	}),
	command(commandToggleCommentResolved, "Toggle comment resolved", "x", "Resolve or reopen the comment under the cursor.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.toggleCommentResolved()
	})),
	command(commandToggleFileListOrder, "Toggle file-list order", "o", "Toggle between path and most-recently-changed order.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.toggleChangeListOrder()
	})),
	preserveCommandScroll(command(commandToggleFullFile, "Toggle full-file view", "tab / zf", "Toggle between diff and full-file views.", reviewOnly(func(c CommandContext) tea.Cmd {
		c.Model.toggleViewMode()
		return c.Model.ensureCurrentFileContentCmd()
	}))),
	command(commandToggleOutlineScope, "Toggle outline file/review scope", "s", "Toggle the changed-symbol outline between the current file and whole review.", reviewOnly(func(c CommandContext) tea.Cmd {
		if c.Model.enrichmentPanel.Open && c.Model.enrichmentPanel.Kind == enrichmentPanelOutlineDiff {
			c.Model.outlineWholeReview = !c.Model.outlineWholeReview
			c.Model.refreshOutlinePanel()
		}
		return nil
	})),
	command(commandToggleResultOrder, "Toggle result order", "o / ctrl+o", "Toggle the active results panel between review and source order.", reviewOnly(func(c CommandContext) tea.Cmd {
		switch {
		case c.Model.enrichmentPanel.Open && c.Model.enrichmentPanel.Kind != enrichmentPanelHover:
			c.Model.toggleEnrichmentOrder()
		case c.Model.referencePanel.Open:
			c.Model.toggleReferenceOrder()
		}
		return nil
	})),
	command(commandToggleSideBySide, "Toggle side-by-side diff", "zs", "Toggle side-by-side rendering for the current file.", reviewOnly(func(c CommandContext) tea.Cmd {
		return c.Model.toggleSplitView()
	})),
	command(commandEditUndo, "Undo edit", "u", "Undo changes in the edit buffer.", editOnly(func(c CommandContext) tea.Cmd {
		return c.Model.popEditUndo(commandCount(c))
	})),
	command(commandWorkspaceSymbols, "Workspace symbols", "gS", "Search symbols across the workspace.", reviewOnly(func(c CommandContext) tea.Cmd {
		c.Model.openWorkspaceSymbolOverlay()
		return nil
	})),
	command(commandEditYank, "Yank with motion", "y + motion", "Arm the yank operator for a following motion.", editOnly(func(c CommandContext) tea.Cmd {
		c.Model.pendingOp = 'y'
		c.Model.pendingOpCount = commandCount(c)
		return nil
	})),
	command(commandYankLine, "Yank line", "yy", "Yank lines into the edit register.", editOnly(func(c CommandContext) tea.Cmd {
		return c.Model.applyOperator('y', "y", commandCount(c))
	})),
}

var commandByID = buildCommandIndex(commandCatalog)

func command(id, name, keys, description string, execute func(CommandContext) tea.Cmd) Command {
	return Command{
		ID:          id,
		Name:        name,
		Keys:        keys,
		Description: description,
		Category:    commandPaletteCategory(id),
		Execute:     execute,
	}
}

func commandPaletteCategory(id string) CommandCategory {
	switch id {
	case commandCommentCurrent,
		commandCommentGeneral,
		commandExportReview,
		commandNextAnnotation,
		commandNextHunk,
		commandPreviousAnnotation,
		commandPreviousHunk,
		commandToggleCommentResolved:
		return CommandCategoryReview

	case commandCollapseDirectory,
		commandExpandDirectory,
		commandMarkAllRead,
		commandMarkCurrentRead,
		commandMarkCurrentUnread,
		commandNextFile,
		commandNextUnreadOrMatch,
		commandOpenFile,
		commandOpenListSelection,
		commandPreviousFile,
		commandPreviousUnreadOrMatch,
		commandToggleFileListOrder:
		return CommandCategoryFiles

	case commandCallsIncoming,
		commandCallsOutgoing,
		commandDiagnosticsCurrent,
		commandDiagnosticsWorkspace,
		commandDocumentSymbols,
		commandGoToDefinition,
		commandHover,
		commandImpact,
		commandJumpBack,
		commandJumpForward,
		commandOutlineChanges,
		commandReferences,
		commandReferencesChanged,
		commandToggleOutlineScope,
		commandWorkspaceSymbols:
		return CommandCategoryCode

	case commandClearContextAll,
		commandCollapseContext,
		commandExpandContext,
		commandExpandContextAll,
		commandFocusChangeList,
		commandFocusDiff,
		commandToggleFullFile,
		commandToggleResultOrder,
		commandToggleSideBySide:
		return CommandCategoryView

	case commandDiscardEdits,
		commandEditAppend,
		commandEditAppendLineEnd,
		commandEditInsert,
		commandEditInsertLineStart,
		commandEditOpenLineAbove,
		commandEditOpenLineBelow,
		commandEditPasteAfter,
		commandEditPasteBefore,
		commandEditRedo,
		commandEditReplace,
		commandEditSubstitute,
		commandEditSubstituteLine,
		commandEditUndo,
		commandExitEditMode,
		commandJoinLines,
		commandSaveEdits:
		return CommandCategoryEdit

	case commandChangeLine,
		commandChangeToLineEnd,
		commandDeleteCharacter,
		commandDeleteLine,
		commandDeleteToLineEnd,
		commandEditChange,
		commandEditDelete,
		commandEditYank,
		commandYankLine:
		return CommandCategoryOperators

	case commandClearSearch,
		commandCloseActivePanel,
		commandProjectSearch,
		commandQuit,
		commandReload,
		commandSearchCurrentFile:
		return CommandCategoryGeneral

	default:
		return ""
	}
}

func preserveCommandScroll(command Command) Command {
	command.preserveScroll = true
	return command
}

func buildCommandIndex(commands []Command) map[string]Command {
	index := make(map[string]Command, len(commands))
	for _, command := range commands {
		if command.ID == "" || command.Name == "" || command.Execute == nil {
			panic("app: incomplete command " + command.ID)
		}
		if _, exists := index[command.ID]; exists {
			panic("app: duplicate command " + command.ID)
		}
		index[command.ID] = command
	}
	return index
}

// Commands returns the complete command catalog in alphabetical order. The
// copy prevents callers from mutating the application's registry.
func Commands() []Command {
	commands := append([]Command(nil), commandCatalog...)
	sort.SliceStable(commands, func(i, j int) bool {
		return strings.ToLower(commands[i].Name) < strings.ToLower(commands[j].Name)
	})
	return commands
}

func (m *Model) executeCommand(id string, count int, hasCount bool) tea.Cmd {
	command, ok := commandByID[id]
	if !ok {
		return nil
	}
	cmd := command.Execute(CommandContext{Model: m, Count: max(1, count), HasCount: hasCount})
	if m.overlay.Kind == OverlayCommandPalette && m.overlay.RawResults == nil {
		m.overlay.RawResults = commandPaletteResults(m.overlay.CommandCategory, "")
		m.overlay.Results = commandPaletteResults(m.overlay.CommandCategory, m.overlay.Query)
	}
	if !command.preserveScroll {
		m.clampScroll()
	}
	return cmd
}

func (m *Model) jumpHunkKeepingScreenRow(direction, count int) {
	cursorScreenRow := m.cursorScreenRow()
	if m.jumpHeaderN(direction, count) {
		m.clampScroll()
		m.scrollCursorToScreenRowAllowingEOFSpace(cursorScreenRow)
	}
}

func commandCount(context CommandContext) int {
	return max(1, context.Count)
}

func reviewOnly(execute func(CommandContext) tea.Cmd) func(CommandContext) tea.Cmd {
	return func(context CommandContext) tea.Cmd {
		if context.Model == nil || context.Model.mode != modeReview {
			return nil
		}
		return execute(context)
	}
}

func editOnly(execute func(CommandContext) tea.Cmd) func(CommandContext) tea.Cmd {
	return func(context CommandContext) tea.Cmd {
		if context.Model == nil || context.Model.mode != modeEdit {
			return nil
		}
		return execute(context)
	}
}

func commandPaletteResults(category CommandCategory, query string) []search.Result {
	terms := strings.Fields(strings.ToLower(query))
	results := make([]search.Result, 0, len(commandCatalog))
	for _, command := range Commands() {
		if command.Category == "" || command.Category != category {
			continue
		}
		haystack := strings.ToLower(command.Name + " " + command.Keys + " " + command.Description)
		matches := true
		for _, term := range terms {
			if !strings.Contains(haystack, term) {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		preview := command.Description
		if command.Keys != "" {
			preview = command.Keys + "  ·  " + preview
		}
		// Location.Path is an opaque command ID for palette results. It keeps
		// identity attached to the filtered row without parallel index state.
		results = append(results, search.Result{
			Kind:     search.ResultFile,
			Label:    command.Name,
			Preview:  preview,
			Location: source.Location{Path: command.ID},
		})
	}
	return results
}
