// Package dashboard implements the `archigraph dashboard serve` subcommand:
// a Go-native HTTP server that exposes a small read/write REST surface over
// the local registry plus an embedded placeholder SPA. Designed to run on a
// developer workstation only; bind defaults to loopback and auth defaults
// off (loopback-only). See ADR-0011 (dashboard architecture) for context.
package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cajasmota/archigraph/internal/registry"
)

// PortRange is the inclusive [Min, Max] window the server picks a random
// free port from at startup. Both ends are clamped to valid TCP ports.
type PortRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// AuthConfig is reserved for future bearer-token gating. For now Enabled
// defaults to false and the server binds to loopback, which is the only
// access control on the wire.
type AuthConfig struct {
	Enabled bool   `json:"enabled"`
	Token   string `json:"token,omitempty"`
}

// Config is the on-disk shape of ~/.archigraph/dashboard.json. Every field
// has a zero-value default that produces a usable loopback server, so the
// file does not need to exist for `dashboard serve` to work.
type Config struct {
	PortRange PortRange  `json:"port_range"`
	Bind      string     `json:"bind"`
	Auth      AuthConfig `json:"auth"`
}

// DefaultConfig returns the loopback-only defaults used when the config
// file is absent or partially specified.
func DefaultConfig() Config {
	return Config{
		PortRange: PortRange{Min: 31000, Max: 31999},
		Bind:      "127.0.0.1",
		Auth:      AuthConfig{Enabled: false},
	}
}

// ConfigPath returns the canonical path to dashboard.json under HomeDir.
func ConfigPath() (string, error) {
	h, err := registry.HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "dashboard.json"), nil
}

// LoadConfig reads dashboard.json, falling back to DefaultConfig when the
// file is missing. Partial files are merged onto the defaults so a user
// can override only the fields they care about.
func LoadConfig() (Config, error) {
	cfg := DefaultConfig()
	p, err := ConfigPath()
	if err != nil {
		return cfg, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	// Decode into a sparse struct so unset fields keep their defaults.
	var raw struct {
		PortRange *PortRange  `json:"port_range,omitempty"`
		Bind      *string     `json:"bind,omitempty"`
		Auth      *AuthConfig `json:"auth,omitempty"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return cfg, fmt.Errorf("dashboard.json: %w", err)
	}
	if raw.PortRange != nil {
		if raw.PortRange.Min != 0 {
			cfg.PortRange.Min = raw.PortRange.Min
		}
		if raw.PortRange.Max != 0 {
			cfg.PortRange.Max = raw.PortRange.Max
		}
	}
	if raw.Bind != nil && *raw.Bind != "" {
		cfg.Bind = *raw.Bind
	}
	if raw.Auth != nil {
		cfg.Auth = *raw.Auth
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Validate rejects port ranges outside the legal TCP window.
func (c Config) Validate() error {
	if c.PortRange.Min < 1 || c.PortRange.Min > 65535 {
		return fmt.Errorf("port_range.min out of range: %d", c.PortRange.Min)
	}
	if c.PortRange.Max < 1 || c.PortRange.Max > 65535 {
		return fmt.Errorf("port_range.max out of range: %d", c.PortRange.Max)
	}
	if c.PortRange.Max < c.PortRange.Min {
		return fmt.Errorf("port_range.max (%d) < min (%d)", c.PortRange.Max, c.PortRange.Min)
	}
	return nil
}
