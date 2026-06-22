package lsp

import (
	"fmt"
	"strconv"
	"strings"

	"cride/internal/diff"
	"cride/internal/source"
)

// ServerState is the lifecycle state surfaced in the footer and request panels.
type ServerState int

const (
	StateDisabled ServerState = iota
	StateUnavailable
	StateStarting
	StateRunning
	StateCrashed
)

func (s ServerState) String() string {
	switch s {
	case StateDisabled:
		return "disabled"
	case StateUnavailable:
		return "unavailable"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateCrashed:
		return "crashed"
	default:
		return "unknown"
	}
}

// Status is a compact server status snapshot.
type Status struct {
	Language string
	Command  []string
	State    ServerState
	Message  string
}

func (s Status) Enabled() bool {
	return s.Language != "" || len(s.Command) > 0 || s.State != StateDisabled || s.Message != ""
}

func (s Status) Key() string {
	if s.Language != "" {
		return s.Language
	}
	if len(s.Command) > 0 {
		return s.Command[0]
	}
	return s.State.String()
}

func (s Status) Label() string {
	if !s.Enabled() {
		return ""
	}
	name := s.Language
	if name == "" && len(s.Command) > 0 {
		name = s.Command[0]
	}
	if name == "" {
		name = "lsp"
	}
	marker := "○"
	switch s.State {
	case StateStarting:
		marker = "◐"
	case StateRunning:
		marker = "●"
	case StateCrashed:
		marker = "×"
	}
	if s.Message == "" {
		return name + " " + marker + " " + s.State.String()
	}
	return name + " " + marker + " " + s.Message
}

// DiagnosticSeverity follows the LSP severity ordering.
type DiagnosticSeverity int

const (
	DiagnosticError DiagnosticSeverity = iota + 1
	DiagnosticWarning
	DiagnosticInformation
	DiagnosticHint
)

func (s DiagnosticSeverity) String() string {
	switch s {
	case DiagnosticError:
		return "error"
	case DiagnosticWarning:
		return "warning"
	case DiagnosticInformation:
		return "info"
	case DiagnosticHint:
		return "hint"
	default:
		return "diagnostic"
	}
}

func (s DiagnosticSeverity) Marker() string {
	switch s {
	case DiagnosticError:
		return "E"
	case DiagnosticWarning:
		return "W"
	case DiagnosticInformation:
		return "I"
	case DiagnosticHint:
		return "H"
	default:
		return "!"
	}
}

type Diagnostic struct {
	Range    source.Range
	Severity DiagnosticSeverity
	Message  string
	Source   string
	Code     string
	Score    int
	Review   diff.ReviewMarkers
}

func (d Diagnostic) Location() source.Location {
	loc := d.Range.Start
	if loc.Line < 1 {
		loc.Line = 1
	}
	if loc.Column < 1 {
		loc.Column = 1
	}
	return loc
}

func (d Diagnostic) CoversLine(path string, line int) bool {
	if path == "" || line < 1 || d.Range.Start.Path != path {
		return false
	}
	start := d.Range.Start.Line
	if start < 1 {
		start = 1
	}
	end := d.Range.End.Line
	if end < start {
		end = start
	}
	return line >= start && line <= end
}

func DiagnosticLabel(d Diagnostic) string {
	loc := d.Location()
	parts := []string{d.Severity.Marker(), loc.Path + ":" + strconv.Itoa(loc.Line) + ":" + strconv.Itoa(loc.Column)}
	if d.Source != "" {
		parts = append(parts, d.Source)
	}
	return "[" + parts[0] + "] " + strings.Join(parts[1:], " ")
}

type Hover struct {
	Location source.Location
	Contents string
}

type SymbolKind int

const (
	SymbolUnknown SymbolKind = iota
	SymbolFile
	SymbolModule
	SymbolNamespace
	SymbolPackage
	SymbolClass
	SymbolMethod
	SymbolProperty
	SymbolField
	SymbolConstructor
	SymbolEnum
	SymbolInterface
	SymbolFunction
	SymbolVariable
	SymbolConstant
	SymbolString
	SymbolNumber
	SymbolBoolean
	SymbolArray
	SymbolObject
	SymbolKey
	SymbolNull
	SymbolEnumMember
	SymbolStruct
	SymbolEvent
	SymbolOperator
	SymbolTypeParameter
	SymbolTest
)

func (k SymbolKind) String() string {
	switch k {
	case SymbolFile:
		return "file"
	case SymbolModule:
		return "module"
	case SymbolNamespace:
		return "namespace"
	case SymbolPackage:
		return "package"
	case SymbolClass:
		return "class"
	case SymbolMethod:
		return "method"
	case SymbolProperty:
		return "property"
	case SymbolField:
		return "field"
	case SymbolConstructor:
		return "constructor"
	case SymbolEnum:
		return "enum"
	case SymbolInterface:
		return "interface"
	case SymbolFunction:
		return "function"
	case SymbolVariable:
		return "variable"
	case SymbolConstant:
		return "constant"
	case SymbolString:
		return "string"
	case SymbolNumber:
		return "number"
	case SymbolBoolean:
		return "boolean"
	case SymbolArray:
		return "array"
	case SymbolObject:
		return "object"
	case SymbolKey:
		return "key"
	case SymbolNull:
		return "null"
	case SymbolEnumMember:
		return "enum-member"
	case SymbolStruct:
		return "struct"
	case SymbolEvent:
		return "event"
	case SymbolOperator:
		return "operator"
	case SymbolTypeParameter:
		return "type-parameter"
	case SymbolTest:
		return "test"
	default:
		return "symbol"
	}
}

type DocumentSymbol struct {
	Name           string
	Detail         string
	Kind           SymbolKind
	Range          source.Range
	SelectionRange source.Range
	Children       []DocumentSymbol
}

type WorkspaceSymbol struct {
	Name          string
	Kind          SymbolKind
	Location      source.Location
	ContainerName string
	Score         int
	Review        diff.ReviewMarkers
}

type CallDirection int

const (
	CallIncoming CallDirection = iota
	CallOutgoing
)

func (d CallDirection) String() string {
	if d == CallOutgoing {
		return "outgoing"
	}
	return "incoming"
}

type CallHierarchyCall struct {
	Name     string
	Kind     SymbolKind
	Location source.Location
	Preview  string
	Score    int
	Review   diff.ReviewMarkers
}

func DocumentSymbolLabel(symbol DocumentSymbol) string {
	loc := symbol.SelectionRange.Start
	if loc.Line < 1 {
		loc = symbol.Range.Start
	}
	label := symbol.Name
	if symbol.Detail != "" {
		label += " " + symbol.Detail
	}
	if loc.Line > 0 {
		label += fmt.Sprintf("  %d:%d", loc.Line, max(1, loc.Column))
	}
	return "[" + symbol.Kind.String() + "] " + label
}

func WorkspaceSymbolLabel(symbol WorkspaceSymbol) string {
	label := "[" + symbol.Kind.String() + "] " + symbol.Name
	if symbol.ContainerName != "" {
		label += " · " + symbol.ContainerName
	}
	if symbol.Location.Path != "" {
		label += fmt.Sprintf("  %s:%d:%d", symbol.Location.Path, max(1, symbol.Location.Line), max(1, symbol.Location.Column))
	}
	return label
}

func CallLabel(call CallHierarchyCall) string {
	label := "[" + call.Kind.String() + "] " + call.Name
	if call.Location.Path != "" {
		label += fmt.Sprintf("  %s:%d:%d", call.Location.Path, max(1, call.Location.Line), max(1, call.Location.Column))
	}
	return label
}

func FlattenDocumentSymbols(symbols []DocumentSymbol) []DocumentSymbol {
	var out []DocumentSymbol
	var walk func([]DocumentSymbol)
	walk = func(items []DocumentSymbol) {
		for _, item := range items {
			children := item.Children
			item.Children = nil
			out = append(out, item)
			walk(children)
		}
	}
	walk(symbols)
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
