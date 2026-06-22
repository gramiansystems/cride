package lsp

import (
	"errors"
	"strings"

	"cride/internal/source"
)

var ErrUnavailable = errors.New("language server unavailable")

// Client is the app-facing seam for optional LSP enrichments. Implementations
// may run real language servers, or may simply report unavailable.
type Client interface {
	Status(path string) Status
	Definition(loc source.Location) ([]source.Location, Status, error)
	References(loc source.Location, includeDeclaration bool) ([]source.Location, Status, error)
	Diagnostics(path string) ([]Diagnostic, Status, error)
	WorkspaceDiagnostics(paths []string) ([]Diagnostic, Status, error)
	Hover(symbol string, loc source.Location) (Hover, Status, error)
	DocumentSymbols(path string) ([]DocumentSymbol, Status, error)
	WorkspaceSymbols(query string) ([]WorkspaceSymbol, Status, error)
	CallHierarchy(symbol string, loc source.Location, direction CallDirection) ([]CallHierarchyCall, Status, error)
}

// UnavailableClient keeps the core app dependency-free while preserving the
// non-fatal behavior of semantic commands when no LSP worker is installed.
type UnavailableClient struct {
	Config Config
}

func NewUnavailableClient(cfg Config) UnavailableClient {
	return UnavailableClient{Config: cfg}
}

func (c UnavailableClient) Status(path string) Status {
	lang, ok := c.Config.LanguageForPath(path)
	if !ok {
		return Status{State: StateDisabled, Message: "no language server configured"}
	}
	return Status{
		Language: lang.Name,
		Command:  lang.Command,
		State:    StateUnavailable,
		Message:  "unavailable",
	}
}

func (c UnavailableClient) Definition(loc source.Location) ([]source.Location, Status, error) {
	status := c.Status(loc.Path)
	return nil, status, unavailableError(status)
}

func (c UnavailableClient) References(loc source.Location, _ bool) ([]source.Location, Status, error) {
	status := c.Status(loc.Path)
	return nil, status, unavailableError(status)
}

func (c UnavailableClient) Diagnostics(path string) ([]Diagnostic, Status, error) {
	status := c.Status(path)
	return nil, status, unavailableError(status)
}

func (c UnavailableClient) WorkspaceDiagnostics(paths []string) ([]Diagnostic, Status, error) {
	for _, path := range paths {
		if status := c.Status(path); status.State != StateDisabled {
			return nil, status, unavailableError(status)
		}
	}
	return nil, Status{State: StateDisabled, Message: "no language server configured"}, ErrUnavailable
}

func (c UnavailableClient) Hover(_ string, loc source.Location) (Hover, Status, error) {
	status := c.Status(loc.Path)
	return Hover{Location: loc}, status, unavailableError(status)
}

func (c UnavailableClient) DocumentSymbols(path string) ([]DocumentSymbol, Status, error) {
	status := c.Status(path)
	return nil, status, unavailableError(status)
}

func (c UnavailableClient) WorkspaceSymbols(query string) ([]WorkspaceSymbol, Status, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, Status{State: StateDisabled, Message: "empty query"}, nil
	}
	return nil, Status{State: StateDisabled, Message: "no language server configured"}, ErrUnavailable
}

func (c UnavailableClient) CallHierarchy(_ string, loc source.Location, _ CallDirection) ([]CallHierarchyCall, Status, error) {
	status := c.Status(loc.Path)
	return nil, status, unavailableError(status)
}

func unavailableError(status Status) error {
	if status.State == StateDisabled {
		return errors.New("no language server configured")
	}
	return ErrUnavailable
}
