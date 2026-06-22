// Package lsp contains protocol-adjacent models and a small client seam for
// optional semantic navigation features. The core app can use these types
// without depending on a concrete language-server implementation.
package lsp

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Language describes one configured language server command.
type Language struct {
	Name       string
	Extensions []string
	Command    []string
}

// Config maps file extensions to language server commands.
type Config struct {
	Languages []Language
}

// DefaultConfig mirrors the documented starter configuration. A concrete LSP
// worker can decide whether to use it automatically; the unavailable core
// client does not force these dependencies.
func DefaultConfig() Config {
	return Config{Languages: []Language{
		{Name: "go", Extensions: []string{".go"}, Command: []string{"gopls"}},
		{Name: "rust", Extensions: []string{".rs"}, Command: []string{"rust-analyzer"}},
		{
			Name:       "cpp",
			Extensions: []string{".c", ".cc", ".cpp", ".cxx", ".c++", ".hh", ".hpp", ".hxx", ".h++", ".h", ".inl", ".ipp", ".tpp"},
			Command:    []string{"clangd"},
		},
	}}
}

func (l Language) languageID(path string) string {
	if l.Name == "cpp" && filepath.Ext(path) == ".c" {
		return "c"
	}
	return l.Name
}

// LanguageForPath returns the first configured language whose extension matches
// path.
func (c Config) LanguageForPath(path string) (Language, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return Language{}, false
	}
	for _, lang := range c.Languages {
		for _, candidate := range lang.Extensions {
			if strings.ToLower(candidate) == ext {
				return lang, true
			}
		}
	}
	return Language{}, false
}

// ParseConfig parses the intentionally small TOML subset used for explicit
// language-server configurations:
//
//	[languages.go]
//	extensions = [".go"]
//	command = ["gopls"]
func ParseConfig(data []byte) (Config, error) {
	var cfg Config
	var current *Language
	lines := strings.Split(string(data), "\n")
	for lineNo, raw := range lines {
		line := stripComment(strings.TrimSpace(raw))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name, ok := languageHeader(line)
			if !ok {
				return Config{}, fmt.Errorf("line %d: unsupported section %q", lineNo+1, line)
			}
			cfg.Languages = append(cfg.Languages, Language{Name: name})
			current = &cfg.Languages[len(cfg.Languages)-1]
			continue
		}
		if current == nil {
			return Config{}, fmt.Errorf("line %d: language property outside [languages.*]", lineNo+1)
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return Config{}, fmt.Errorf("line %d: expected key = value", lineNo+1)
		}
		key = strings.TrimSpace(key)
		items, err := parseStringArray(strings.TrimSpace(value))
		if err != nil {
			return Config{}, fmt.Errorf("line %d: %w", lineNo+1, err)
		}
		switch key {
		case "extensions":
			current.Extensions = items
		case "command":
			current.Command = items
		default:
			return Config{}, fmt.Errorf("line %d: unsupported key %q", lineNo+1, key)
		}
	}
	return cfg, nil
}

func languageHeader(line string) (string, bool) {
	name := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
	if !strings.HasPrefix(name, "languages.") {
		return "", false
	}
	lang := strings.TrimPrefix(name, "languages.")
	return lang, lang != "" && !strings.ContainsAny(lang, " \t")
}

func stripComment(line string) string {
	inQuote := false
	escaped := false
	for i, r := range line {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case r == '"':
			inQuote = !inQuote
		case r == '#' && !inQuote:
			return strings.TrimSpace(line[:i])
		}
	}
	return line
}

func parseStringArray(s string) ([]string, error) {
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, errors.New("expected string array")
	}
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "["), "]"))
	if body == "" {
		return nil, nil
	}
	var out []string
	for len(body) > 0 {
		body = strings.TrimSpace(body)
		if !strings.HasPrefix(body, "\"") {
			return nil, errors.New("expected quoted string")
		}
		value, rest, err := parseQuoted(body[1:])
		if err != nil {
			return nil, err
		}
		out = append(out, value)
		body = strings.TrimSpace(rest)
		if body == "" {
			break
		}
		if !strings.HasPrefix(body, ",") {
			return nil, errors.New("expected comma")
		}
		body = body[1:]
	}
	return out, nil
}

func parseQuoted(s string) (value, rest string, err error) {
	var b strings.Builder
	escaped := false
	for i, r := range s {
		switch {
		case escaped:
			switch r {
			case '"', '\\':
				b.WriteRune(r)
			default:
				b.WriteRune('\\')
				b.WriteRune(r)
			}
			escaped = false
		case r == '\\':
			escaped = true
		case r == '"':
			return b.String(), s[i+1:], nil
		default:
			b.WriteRune(r)
		}
	}
	return "", "", errors.New("unterminated quoted string")
}
