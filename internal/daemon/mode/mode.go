// Package mode defines the three operational modes of the archigraph daemon
// (S7 of #2149). Each mode is a preset of env-var defaults that controls
// memory usage, background activity, and feature activation.
//
// Env vars are resolved with the following precedence (highest wins):
//
//  1. Process env (explicitly set by the operator)
//  2. Mode defaults applied by ApplyDefaults
//  3. Compiled-in Go defaults
//
// The active mode name is persisted in ~/.archigraph/daemon.config.json
// so `archigraph status` can surface it without querying the live process.
package mode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Mode is one of the three operational presets.
type Mode string

const (
	// Background is the default for first-time / open-source installs.
	// It minimises memory and CPU footprint: lazy hydration, no
	// MiniLM embeddings, on-demand algo passes, 60% heap budget.
	Background Mode = "background"

	// Workstation restores the historical defaults: eager algo passes,
	// MiniLM embeddings allowed, no heap cap override.
	Workstation Mode = "workstation"

	// Readonly serves graph queries against the existing graph.fb only.
	// No reindexing, no watcher, no algo passes run.
	Readonly Mode = "readonly"
)

// All returns the full set of valid mode names in display order.
func All() []Mode { return []Mode{Background, Workstation, Readonly} }

// Parse returns the Mode for s, or an error if s is unrecognised.
func Parse(s string) (Mode, error) {
	switch Mode(s) {
	case Background, Workstation, Readonly:
		return Mode(s), nil
	default:
		return "", fmt.Errorf("unknown mode %q: must be one of background, workstation, readonly", s)
	}
}

// Defaults holds the env-var defaults for a mode.
// An empty-string value means "set the var to the empty string" (disables
// the feature). A nil map entry is never written.
type Defaults map[string]string

// ModeDefaults returns the env-var defaults for m.
func ModeDefaults(m Mode) Defaults {
	switch m {
	case Background:
		return Defaults{
			"ARCHIGRAPH_EAGER_ALGO":    "false",
			"ARCHIGRAPH_EMBEDDING_URL": "",
			"ARCHIGRAPH_HEAP_MAX_PCT":  "60",
		}
	case Workstation:
		// Restore current production defaults; don't force-set EMBEDDING_URL
		// so the operator can configure their own endpoint freely.
		return Defaults{
			"ARCHIGRAPH_EAGER_ALGO":   "true",
			"ARCHIGRAPH_HEAP_MAX_PCT": "80",
		}
	case Readonly:
		return Defaults{
			"ARCHIGRAPH_DISABLE_WATCHER": "true",
			"ARCHIGRAPH_DISABLE_REBUILD": "true",
			"ARCHIGRAPH_DISABLE_ALGO":    "true",
		}
	default:
		return Defaults{}
	}
}

// ApplyDefaults sets each env-var from ModeDefaults(m) only when the
// variable is not already present in the process environment.
// This respects the "env vars still override mode" contract from the spec.
func ApplyDefaults(m Mode) {
	for k, v := range ModeDefaults(m) {
		if _, ok := os.LookupEnv(k); !ok {
			os.Setenv(k, v) //nolint:errcheck // os.Setenv only fails on empty key
		}
	}
}

// Config is the on-disk schema for ~/.archigraph/daemon.config.json.
// It persists the active mode plus any operator-supplied env overrides
// (written by `archigraph mode <m>`).
type Config struct {
	// Mode is the active operational mode.
	Mode Mode `json:"mode"`

	// EnvOverrides, if non-nil, are written into the service unit alongside
	// the mode defaults. Operator-supplied; not set by the mode presets.
	EnvOverrides map[string]string `json:"env_overrides,omitempty"`
}

// DefaultConfigPath returns the canonical path for daemon.config.json,
// rooted under root (typically ~/.archigraph).
func DefaultConfigPath(root string) string {
	return filepath.Join(root, "daemon.config.json")
}

// LoadConfig reads and parses daemon.config.json from path.
// Returns (Config{}, nil) when the file does not exist (first run).
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read daemon config %s: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("parse daemon config %s: %w", path, err)
	}
	return c, nil
}

// SaveConfig writes cfg to path atomically (write-to-tmp + rename).
func SaveConfig(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal daemon config: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write daemon config tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename daemon config: %w", err)
	}
	return nil
}
