// Package highlight provides pure-Go (Chroma) syntax highlighting, applied
// per line at render time with a lexer cached per file. No CGO — keeps the
// core a static binary. See DESIGN.md's "Code intelligence" section.
package highlight

import (
	"bytes"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// Highlighter colorizes single source lines. It is safe for concurrent use.
type Highlighter struct {
	style     *chroma.Style
	formatter chroma.Formatter
	enabled   bool

	mu     sync.Mutex
	lexers map[string]chroma.Lexer // keyed by filename

	cache *lineCache
}

// Options select the Chroma style and output fidelity.
type Options struct {
	// Style is a Chroma style name; empty picks the scheme default
	// (monokai on dark, github on light).
	Style string
	// Dark selects the default style when Style is empty.
	Dark bool
	// TrueColor emits 24-bit color instead of quantizing to 256.
	TrueColor bool
	// Disabled turns highlighting off entirely (NO_COLOR).
	Disabled bool
}

// New returns a dark-theme Highlighter using a 256-color formatter.
func New() *Highlighter {
	return NewWithOptions(Options{Dark: true})
}

// NewWithOptions returns a Highlighter for the given style and fidelity.
func NewWithOptions(opts Options) *Highlighter {
	name := opts.Style
	if name == "" {
		if opts.Dark {
			name = "monokai"
		} else {
			name = "github"
		}
	}
	style := styles.Get(name)
	if style == nil {
		style = styles.Fallback
	}
	formatterName := "terminal256"
	if opts.TrueColor {
		formatterName = "terminal16m"
	}
	f := formatters.Get(formatterName)
	if f == nil {
		f = formatters.Fallback
	}
	return &Highlighter{
		style:     style,
		formatter: f,
		enabled:   !opts.Disabled,
		lexers:    make(map[string]chroma.Lexer),
		cache:     newLineCache(lineCacheCap),
	}
}

// StyleName reports the active Chroma style, for status/tests.
func (h *Highlighter) StyleName() string {
	if h.style == nil {
		return ""
	}
	return h.style.Name
}

// Line returns line with ANSI syntax highlighting for the given filename. On
// any failure (or when disabled) it returns the input unchanged.
func (h *Highlighter) Line(filename, line string) string {
	if !h.enabled || strings.TrimSpace(line) == "" {
		return line
	}
	lx := h.lexerFor(filename)
	key := lineKey{lexer: lx.Config().Name, line: line}
	if out, ok := h.cache.get(key); ok {
		return out
	}
	out := h.highlight(lx, line)
	h.cache.put(key, out)
	return out
}

func (h *Highlighter) highlight(lx chroma.Lexer, line string) string {
	it, err := lx.Tokenise(nil, line)
	if err != nil {
		return line
	}
	var buf bytes.Buffer
	if err := h.formatter.Format(&buf, h.style, it); err != nil {
		return line
	}
	return strings.TrimRight(buf.String(), "\n")
}

func (h *Highlighter) lexerFor(filename string) chroma.Lexer {
	h.mu.Lock()
	defer h.mu.Unlock()
	if lx, ok := h.lexers[filename]; ok {
		return lx
	}
	lx := lexers.Match(filename)
	if lx == nil {
		lx = lexers.Fallback
	}
	lx = chroma.Coalesce(lx)
	h.lexers[filename] = lx
	return lx
}
