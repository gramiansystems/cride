// Package config loads cride's optional user configuration from
// $XDG_CONFIG_HOME/cride/config (a plain "key = value" file). Missing or
// malformed config is never fatal: defaults apply. See README.md's
// "Configuration and state" section.
package config

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	// Theme is auto|dark|light. Empty and unknown values mean auto.
	Theme string
	// ChromaStyle overrides the syntax-highlighting style by Chroma name.
	ChromaStyle string
}

// Path returns the config file location honoring XDG_CONFIG_HOME.
func Path() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "cride", "config")
}

// Load reads the config file; a missing file yields the zero Config.
func Load() Config {
	path := Path()
	if path == "" {
		return Config{}
	}
	f, err := os.Open(path)
	if err != nil {
		return Config{}
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads "key = value" lines; '#' starts a comment, unknown keys are
// ignored so older binaries tolerate newer configs.
func Parse(r io.Reader) Config {
	var cfg Config
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "theme":
			cfg.Theme = strings.ToLower(value)
		case "chroma_style", "chroma-style":
			cfg.ChromaStyle = value
		}
	}
	return cfg
}

// WantsDark resolves the theme setting to a dark/light decision, given the
// terminal's detected background for the auto case.
func (c Config) WantsDark(detectedDark bool) bool {
	switch c.Theme {
	case "dark":
		return true
	case "light":
		return false
	default:
		return detectedDark
	}
}
