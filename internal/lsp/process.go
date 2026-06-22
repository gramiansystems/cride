package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"cride/internal/source"
)

const (
	requestTimeout = 4 * time.Second
	restartBackoff = 2 * time.Second
)

// ProcessClient is a small stdio JSON-RPC LSP client. It is intentionally
// conservative: servers start lazily, all requests run outside Bubble Tea
// Update, and failures are reported as unavailable status instead of fatal app
// errors.
type ProcessClient struct {
	root    string
	config  Config
	mu      sync.Mutex
	servers map[string]*lspServer
}

func NewProcessClient(root string, cfg Config) *ProcessClient {
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err == nil {
		root = abs
	}
	return &ProcessClient{
		root:    root,
		config:  cfg,
		servers: make(map[string]*lspServer),
	}
}

func (c *ProcessClient) Status(path string) Status {
	lang, ok := c.config.LanguageForPath(path)
	if !ok {
		return Status{State: StateDisabled, Message: "no language server configured"}
	}
	c.mu.Lock()
	server := c.servers[lang.Name]
	c.mu.Unlock()
	if server == nil {
		return Status{Language: lang.Name, Command: lang.Command, State: StateUnavailable, Message: "not started"}
	}
	return server.status()
}

func (c *ProcessClient) Definition(loc source.Location) ([]source.Location, Status, error) {
	server, status, err := c.ensureServer(loc.Path)
	if err != nil {
		return nil, status, err
	}
	if err := server.syncFile(loc.Path); err != nil {
		return nil, server.status(), err
	}
	var raw json.RawMessage
	err = server.request("textDocument/definition", map[string]any{
		"textDocument": map[string]string{"uri": c.uri(loc.Path)},
		"position":     wirePositionFromLocation(loc),
	}, &raw)
	if err != nil {
		return nil, server.status(), err
	}
	return parseLocations(c.root, raw), server.status(), nil
}

func (c *ProcessClient) References(loc source.Location, includeDeclaration bool) ([]source.Location, Status, error) {
	server, status, err := c.ensureServer(loc.Path)
	if err != nil {
		return nil, status, err
	}
	if err := server.syncFile(loc.Path); err != nil {
		return nil, server.status(), err
	}
	var locations []wireLocation
	err = server.request("textDocument/references", map[string]any{
		"textDocument": map[string]string{"uri": c.uri(loc.Path)},
		"position":     wirePositionFromLocation(loc),
		"context":      map[string]bool{"includeDeclaration": includeDeclaration},
	}, &locations)
	if err != nil {
		return nil, server.status(), err
	}
	return convertLocations(c.root, locations), server.status(), nil
}

func (c *ProcessClient) Diagnostics(path string) ([]Diagnostic, Status, error) {
	server, status, err := c.ensureServer(path)
	if err != nil {
		return nil, status, err
	}
	if err := server.syncFile(path); err != nil {
		return nil, server.status(), err
	}
	return server.diagnostics(path), server.status(), nil
}

func (c *ProcessClient) WorkspaceDiagnostics(paths []string) ([]Diagnostic, Status, error) {
	var (
		out    []Diagnostic
		status Status
		seen   bool
	)
	for _, path := range paths {
		server, st, err := c.ensureServer(path)
		status = st
		if err != nil {
			if status.State == StateDisabled {
				continue
			}
			if !seen {
				return nil, status, err
			}
			continue
		}
		seen = true
		if err := server.syncFile(path); err != nil {
			return nil, server.status(), err
		}
		out = append(out, server.diagnostics(path)...)
		status = server.status()
	}
	if !seen {
		return nil, Status{State: StateDisabled, Message: "no language server configured"}, ErrUnavailable
	}
	return out, status, nil
}

func (c *ProcessClient) Hover(symbol string, loc source.Location) (Hover, Status, error) {
	server, status, err := c.ensureServer(loc.Path)
	if err != nil {
		return Hover{Location: loc}, status, err
	}
	if err := server.syncFile(loc.Path); err != nil {
		return Hover{Location: loc}, server.status(), err
	}
	var result wireHover
	err = server.request("textDocument/hover", map[string]any{
		"textDocument": map[string]string{"uri": c.uri(loc.Path)},
		"position":     wirePositionFromLocation(loc),
	}, &result)
	if err != nil {
		return Hover{Location: loc}, server.status(), err
	}
	return Hover{Location: loc, Contents: hoverContentsString(result.Contents)}, server.status(), nil
}

func (c *ProcessClient) DocumentSymbols(path string) ([]DocumentSymbol, Status, error) {
	server, status, err := c.ensureServer(path)
	if err != nil {
		return nil, status, err
	}
	if err := server.syncFile(path); err != nil {
		return nil, server.status(), err
	}
	var raw json.RawMessage
	if err := server.request("textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]string{"uri": c.uri(path)},
	}, &raw); err != nil {
		return nil, server.status(), err
	}
	return parseDocumentSymbols(path, raw), server.status(), nil
}

func (c *ProcessClient) WorkspaceSymbols(query string) ([]WorkspaceSymbol, Status, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, Status{State: StateDisabled, Message: "empty query"}, nil
	}

	candidates := c.config.Languages
	if len(candidates) == 0 {
		return nil, Status{State: StateDisabled, Message: "no language server configured"}, ErrUnavailable
	}

	var (
		out    []WorkspaceSymbol
		status Status
		seen   bool
	)
	for _, lang := range candidates {
		server, err := c.ensureLanguageServer(lang)
		status = c.statusForLanguage(lang)
		if err != nil {
			continue
		}
		seen = true
		var raw json.RawMessage
		if err := server.request("workspace/symbol", map[string]string{"query": query}, &raw); err != nil {
			status = server.status()
			continue
		}
		out = append(out, parseWorkspaceSymbols(c.root, raw)...)
		status = server.status()
	}
	if !seen {
		return nil, status, ErrUnavailable
	}
	return out, status, nil
}

func (c *ProcessClient) CallHierarchy(symbol string, loc source.Location, direction CallDirection) ([]CallHierarchyCall, Status, error) {
	server, status, err := c.ensureServer(loc.Path)
	if err != nil {
		return nil, status, err
	}
	if err := server.syncFile(loc.Path); err != nil {
		return nil, server.status(), err
	}
	var items []wireCallHierarchyItem
	err = server.request("textDocument/prepareCallHierarchy", map[string]any{
		"textDocument": map[string]string{"uri": c.uri(loc.Path)},
		"position":     wirePositionFromLocation(loc),
	}, &items)
	if err != nil {
		return nil, server.status(), err
	}
	if len(items) == 0 {
		return nil, server.status(), nil
	}
	item := items[0]
	if symbol != "" {
		for _, candidate := range items {
			if candidate.Name == symbol {
				item = candidate
				break
			}
		}
	}

	if direction == CallOutgoing {
		var calls []wireOutgoingCall
		if err := server.request("callHierarchy/outgoingCalls", map[string]any{"item": item}, &calls); err != nil {
			return nil, server.status(), err
		}
		return outgoingCalls(c.root, calls), server.status(), nil
	}

	var calls []wireIncomingCall
	if err := server.request("callHierarchy/incomingCalls", map[string]any{"item": item}, &calls); err != nil {
		return nil, server.status(), err
	}
	return incomingCalls(c.root, calls), server.status(), nil
}

func (c *ProcessClient) ensureServer(path string) (*lspServer, Status, error) {
	lang, ok := c.config.LanguageForPath(path)
	if !ok {
		status := Status{State: StateDisabled, Message: "no language server configured"}
		return nil, status, errors.New(status.Message)
	}
	server, err := c.ensureLanguageServer(lang)
	if err != nil {
		return nil, c.statusForLanguage(lang), err
	}
	return server, server.status(), nil
}

func (c *ProcessClient) ensureLanguageServer(lang Language) (*lspServer, error) {
	c.mu.Lock()
	server := c.servers[lang.Name]
	if server != nil {
		status := server.status()
		if status.State == StateRunning || status.State == StateStarting {
			c.mu.Unlock()
			return server, nil
		}
		if status.State == StateCrashed && time.Now().Before(server.nextRestart()) {
			c.mu.Unlock()
			return nil, fmt.Errorf("%s crashed; restart pending", lang.Name)
		}
	}
	server = newLSPServer(c.root, lang)
	c.servers[lang.Name] = server
	c.mu.Unlock()

	if err := server.start(); err != nil {
		return nil, err
	}
	return server, nil
}

func (c *ProcessClient) statusForLanguage(lang Language) Status {
	c.mu.Lock()
	server := c.servers[lang.Name]
	c.mu.Unlock()
	if server == nil {
		return Status{Language: lang.Name, Command: lang.Command, State: StateUnavailable, Message: "not started"}
	}
	return server.status()
}

func (c *ProcessClient) uri(path string) string {
	return fileURI(c.root, path)
}

type lspServer struct {
	root    string
	lang    Language
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	reader  *bufio.Reader
	writeMu sync.Mutex

	mu          sync.Mutex
	state       Status
	pending     map[int64]chan rpcResponse
	nextID      int64
	diagnostic  map[string][]Diagnostic
	opened      map[string]int
	restartTime time.Time
}

func newLSPServer(root string, lang Language) *lspServer {
	return &lspServer{
		root:       root,
		lang:       lang,
		state:      Status{Language: lang.Name, Command: lang.Command, State: StateUnavailable, Message: "not started"},
		pending:    make(map[int64]chan rpcResponse),
		diagnostic: make(map[string][]Diagnostic),
		opened:     make(map[string]int),
	}
}

func (s *lspServer) start() error {
	if len(s.lang.Command) == 0 {
		err := errors.New("language server command is empty")
		s.setStatus(StateUnavailable, err.Error())
		return err
	}

	s.setStatus(StateStarting, "starting")
	cmd := exec.Command(s.lang.Command[0], s.lang.Command[1:]...)
	cmd.Dir = s.root
	stdin, err := cmd.StdinPipe()
	if err != nil {
		s.setStatus(StateUnavailable, err.Error())
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.setStatus(StateUnavailable, err.Error())
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		s.setStatus(StateUnavailable, err.Error())
		return err
	}

	s.mu.Lock()
	s.cmd = cmd
	s.stdin = stdin
	s.reader = bufio.NewReader(stdout)
	s.mu.Unlock()

	go s.readLoop()
	go func() {
		_ = cmd.Wait()
	}()

	var initResult json.RawMessage
	if err := s.requestWithTimeout("initialize", s.initializeParams(), &initResult, 8*time.Second); err != nil {
		_ = cmd.Process.Kill()
		s.setStatus(StateUnavailable, err.Error())
		return err
	}
	_ = s.notify("initialized", map[string]any{})
	s.setStatus(StateRunning, "running")
	return nil
}

func (s *lspServer) initializeParams() map[string]any {
	return map[string]any{
		"processId": os.Getpid(),
		"rootUri":   fileURI(s.root, ""),
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"definition":         map[string]any{"dynamicRegistration": false, "linkSupport": true},
				"references":         map[string]any{"dynamicRegistration": false},
				"hover":              map[string]any{"dynamicRegistration": false},
				"documentSymbol":     map[string]any{"dynamicRegistration": false},
				"publishDiagnostics": map[string]any{"relatedInformation": true},
				"callHierarchy":      map[string]any{"dynamicRegistration": false},
			},
			"workspace": map[string]any{
				"symbol": map[string]any{"dynamicRegistration": false},
			},
		},
	}
}

func (s *lspServer) status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *lspServer) setStatus(state ServerState, message string) {
	s.mu.Lock()
	s.state = Status{Language: s.lang.Name, Command: s.lang.Command, State: state, Message: message}
	if state == StateCrashed {
		s.restartTime = time.Now().Add(restartBackoff)
	}
	s.mu.Unlock()
}

func (s *lspServer) nextRestart() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restartTime
}

func (s *lspServer) syncFile(path string) error {
	abs := filepath.Join(s.root, filepath.FromSlash(path))
	content, err := os.ReadFile(abs)
	if err != nil {
		return err
	}

	s.mu.Lock()
	version := s.opened[path] + 1
	opened := s.opened[path] > 0
	s.opened[path] = version
	s.mu.Unlock()

	if !opened {
		return s.notify("textDocument/didOpen", map[string]any{
			"textDocument": map[string]any{
				"uri":        fileURI(s.root, path),
				"languageId": s.lang.languageID(path),
				"version":    version,
				"text":       string(content),
			},
		})
	}
	return s.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{
			"uri":     fileURI(s.root, path),
			"version": version,
		},
		"contentChanges": []map[string]string{{"text": string(content)}},
	})
}

func (s *lspServer) diagnostics(path string) []Diagnostic {
	s.mu.Lock()
	defer s.mu.Unlock()
	diagnostics := s.diagnostic[path]
	out := make([]Diagnostic, len(diagnostics))
	copy(out, diagnostics)
	return out
}

func (s *lspServer) request(method string, params any, result any) error {
	return s.requestWithTimeout(method, params, result, requestTimeout)
}

func (s *lspServer) requestWithTimeout(method string, params any, result any, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	id := s.nextRequestID()
	ch := make(chan rpcResponse, 1)
	s.mu.Lock()
	s.pending[id] = ch
	s.mu.Unlock()

	if err := s.writeMessage(id, method, params); err != nil {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return err
	}

	select {
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return ctx.Err()
	case response := <-ch:
		if response.Error != nil {
			return errors.New(response.Error.Message)
		}
		if result == nil || len(response.Result) == 0 || bytes.Equal(response.Result, []byte("null")) {
			return nil
		}
		return json.Unmarshal(response.Result, result)
	}
}

func (s *lspServer) notify(method string, params any) error {
	return s.writeMessage(0, method, params)
}

func (s *lspServer) nextRequestID() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return s.nextID
}

func (s *lspServer) writeMessage(id int64, method string, params any) error {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if id > 0 {
		msg["id"] = id
	}
	if params != nil {
		msg["params"] = params
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	s.mu.Lock()
	stdin := s.stdin
	s.mu.Unlock()
	if stdin == nil {
		return ErrUnavailable
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = fmt.Fprintf(stdin, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return err
}

func (s *lspServer) readLoop() {
	for {
		body, err := readRPCMessage(s.reader)
		if err != nil {
			s.failPending(err)
			s.setStatus(StateCrashed, "crashed")
			return
		}
		s.handleMessage(body)
	}
}

func (s *lspServer) handleMessage(body []byte) {
	var envelope rpcEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return
	}
	if len(envelope.ID) > 0 && !bytes.Equal(envelope.ID, []byte("null")) {
		id, ok := parseRPCID(envelope.ID)
		if !ok {
			return
		}
		s.mu.Lock()
		ch := s.pending[id]
		delete(s.pending, id)
		s.mu.Unlock()
		if ch != nil {
			ch <- rpcResponse{Result: envelope.Result, Error: envelope.Error}
		}
		return
	}
	if envelope.Method == "textDocument/publishDiagnostics" {
		var params wirePublishDiagnostics
		if err := json.Unmarshal(envelope.Params, &params); err == nil {
			s.storeDiagnostics(params)
		}
	}
}

func (s *lspServer) storeDiagnostics(params wirePublishDiagnostics) {
	path := pathFromURI(s.root, params.URI)
	diagnostics := make([]Diagnostic, 0, len(params.Diagnostics))
	for _, d := range params.Diagnostics {
		diagnostics = append(diagnostics, Diagnostic{
			Range:    source.Range{Start: locationFromWire(path, d.Range.Start), End: locationFromWire(path, d.Range.End)},
			Severity: DiagnosticSeverity(d.Severity),
			Message:  d.Message,
			Source:   d.Source,
			Code:     d.Code.String(),
		})
	}
	s.mu.Lock()
	s.diagnostic[path] = diagnostics
	s.mu.Unlock()
}

func (s *lspServer) failPending(err error) {
	s.mu.Lock()
	pending := s.pending
	s.pending = make(map[int64]chan rpcResponse)
	s.mu.Unlock()
	for _, ch := range pending {
		ch <- rpcResponse{Error: &rpcError{Message: err.Error()}}
	}
}

func readRPCMessage(r *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "Content-Length") {
			contentLength, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
		}
	}
	if contentLength < 0 {
		return nil, errors.New("missing Content-Length")
	}
	body := make([]byte, contentLength)
	_, err := io.ReadFull(r, body)
	return body, err
}

func parseRPCID(raw json.RawMessage) (int64, bool) {
	var id int64
	if err := json.Unmarshal(raw, &id); err == nil {
		return id, true
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, false
	}
	id, err := strconv.ParseInt(text, 10, 64)
	return id, err == nil
}

type rpcEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcResponse struct {
	Result json.RawMessage
	Error  *rpcError
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type wirePosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type wireRange struct {
	Start wirePosition `json:"start"`
	End   wirePosition `json:"end"`
}

type wireDiagnostic struct {
	Range    wireRange      `json:"range"`
	Severity int            `json:"severity"`
	Message  string         `json:"message"`
	Source   string         `json:"source"`
	Code     diagnosticCode `json:"code"`
}

type diagnosticCode struct {
	raw json.RawMessage
}

func (c *diagnosticCode) UnmarshalJSON(data []byte) error {
	c.raw = append(c.raw[:0], data...)
	return nil
}

func (c diagnosticCode) String() string {
	if len(c.raw) == 0 || bytes.Equal(c.raw, []byte("null")) {
		return ""
	}
	var s string
	if err := json.Unmarshal(c.raw, &s); err == nil {
		return s
	}
	var n int
	if err := json.Unmarshal(c.raw, &n); err == nil {
		return strconv.Itoa(n)
	}
	return ""
}

type wirePublishDiagnostics struct {
	URI         string           `json:"uri"`
	Diagnostics []wireDiagnostic `json:"diagnostics"`
}

type wireHover struct {
	Contents json.RawMessage `json:"contents"`
}

type wireDocumentSymbol struct {
	Name           string               `json:"name"`
	Detail         string               `json:"detail"`
	Kind           int                  `json:"kind"`
	Range          wireRange            `json:"range"`
	SelectionRange wireRange            `json:"selectionRange"`
	Children       []wireDocumentSymbol `json:"children"`
}

type wireSymbolInformation struct {
	Name          string       `json:"name"`
	Kind          int          `json:"kind"`
	Location      wireLocation `json:"location"`
	ContainerName string       `json:"containerName"`
}

type wireLocation struct {
	URI   string    `json:"uri"`
	Range wireRange `json:"range"`
}

type wireLocationLink struct {
	TargetURI            string    `json:"targetUri"`
	TargetRange          wireRange `json:"targetRange"`
	TargetSelectionRange wireRange `json:"targetSelectionRange"`
}

type wireCallHierarchyItem struct {
	Name           string    `json:"name"`
	Kind           int       `json:"kind"`
	URI            string    `json:"uri"`
	Range          wireRange `json:"range"`
	SelectionRange wireRange `json:"selectionRange"`
}

type wireIncomingCall struct {
	From       wireCallHierarchyItem `json:"from"`
	FromRanges []wireRange           `json:"fromRanges"`
}

type wireOutgoingCall struct {
	To         wireCallHierarchyItem `json:"to"`
	FromRanges []wireRange           `json:"fromRanges"`
}

func wirePositionFromLocation(loc source.Location) wirePosition {
	return wirePosition{Line: max(1, loc.Line) - 1, Character: max(1, loc.Column) - 1}
}

func locationFromWire(path string, pos wirePosition) source.Location {
	return source.Location{Path: path, Line: pos.Line + 1, Column: pos.Character + 1}
}

func fileURI(root, path string) string {
	abs := filepath.Join(root, filepath.FromSlash(path))
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	return u.String()
}

func pathFromURI(root, raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	path := filepath.FromSlash(u.Path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func hoverContentsString(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var markup struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &markup); err == nil && markup.Value != "" {
		return markup.Value
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var marked []json.RawMessage
	if err := json.Unmarshal(raw, &marked); err == nil {
		var parts []string
		for _, item := range marked {
			part := hoverContentsString(item)
			if part != "" {
				parts = append(parts, part)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func parseDocumentSymbols(path string, raw json.RawMessage) []DocumentSymbol {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	var docs []wireDocumentSymbol
	if err := json.Unmarshal(raw, &docs); err == nil && len(docs) > 0 {
		return convertDocumentSymbols(path, docs)
	}
	var infos []wireSymbolInformation
	if err := json.Unmarshal(raw, &infos); err != nil {
		return nil
	}
	out := make([]DocumentSymbol, 0, len(infos))
	for _, info := range infos {
		out = append(out, DocumentSymbol{
			Name: info.Name,
			Kind: SymbolKind(info.Kind),
			Range: source.Range{
				Start: locationFromWire(path, info.Location.Range.Start),
				End:   locationFromWire(path, info.Location.Range.End),
			},
			SelectionRange: source.Range{Start: locationFromWire(path, info.Location.Range.Start)},
		})
	}
	return out
}

func parseLocations(root string, raw json.RawMessage) []source.Location {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	var locations []wireLocation
	if err := json.Unmarshal(raw, &locations); err == nil {
		if converted := convertLocations(root, locations); len(converted) > 0 {
			return converted
		}
	}
	var location wireLocation
	if err := json.Unmarshal(raw, &location); err == nil && location.URI != "" {
		return convertLocations(root, []wireLocation{location})
	}
	var links []wireLocationLink
	if err := json.Unmarshal(raw, &links); err != nil {
		return nil
	}
	out := make([]source.Location, 0, len(links))
	for _, link := range links {
		if link.TargetURI == "" {
			continue
		}
		selection := link.TargetSelectionRange
		if selection == (wireRange{}) {
			selection = link.TargetRange
		}
		path := pathFromURI(root, link.TargetURI)
		out = append(out, locationFromWire(path, selection.Start))
	}
	return out
}

func convertLocations(root string, locations []wireLocation) []source.Location {
	out := make([]source.Location, 0, len(locations))
	for _, location := range locations {
		if location.URI == "" {
			continue
		}
		path := pathFromURI(root, location.URI)
		out = append(out, locationFromWire(path, location.Range.Start))
	}
	return out
}

func convertDocumentSymbols(path string, docs []wireDocumentSymbol) []DocumentSymbol {
	out := make([]DocumentSymbol, 0, len(docs))
	for _, doc := range docs {
		out = append(out, DocumentSymbol{
			Name:   doc.Name,
			Detail: doc.Detail,
			Kind:   SymbolKind(doc.Kind),
			Range: source.Range{
				Start: locationFromWire(path, doc.Range.Start),
				End:   locationFromWire(path, doc.Range.End),
			},
			SelectionRange: source.Range{
				Start: locationFromWire(path, doc.SelectionRange.Start),
				End:   locationFromWire(path, doc.SelectionRange.End),
			},
			Children: convertDocumentSymbols(path, doc.Children),
		})
	}
	return out
}

func parseWorkspaceSymbols(root string, raw json.RawMessage) []WorkspaceSymbol {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	var infos []wireSymbolInformation
	if err := json.Unmarshal(raw, &infos); err != nil {
		return nil
	}
	out := make([]WorkspaceSymbol, 0, len(infos))
	for _, info := range infos {
		path := pathFromURI(root, info.Location.URI)
		out = append(out, WorkspaceSymbol{
			Name:          info.Name,
			Kind:          SymbolKind(info.Kind),
			Location:      locationFromWire(path, info.Location.Range.Start),
			ContainerName: info.ContainerName,
		})
	}
	return out
}

func incomingCalls(root string, calls []wireIncomingCall) []CallHierarchyCall {
	out := make([]CallHierarchyCall, 0, len(calls))
	for _, call := range calls {
		path := pathFromURI(root, call.From.URI)
		out = append(out, CallHierarchyCall{
			Name:     call.From.Name,
			Kind:     SymbolKind(call.From.Kind),
			Location: locationFromWire(path, call.From.SelectionRange.Start),
		})
	}
	return out
}

func outgoingCalls(root string, calls []wireOutgoingCall) []CallHierarchyCall {
	out := make([]CallHierarchyCall, 0, len(calls))
	for _, call := range calls {
		path := pathFromURI(root, call.To.URI)
		out = append(out, CallHierarchyCall{
			Name:     call.To.Name,
			Kind:     SymbolKind(call.To.Kind),
			Location: locationFromWire(path, call.To.SelectionRange.Start),
		})
	}
	return out
}
