// Pattern configuration: tunable thresholds documented in ADR-0018 (the
// "Configuration" table) and surfaced via `archigraph patterns config`.
//
// Config lives at <group>/.archigraph/patterns-config.json alongside
// patterns.json. Defaults are returned when the file does not exist; the
// daemon decay scheduler, the MCP convergence pass, and the CLI all read
// through this package so a single source of truth governs every consumer.
package agentpatterns

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Default thresholds per ADR-0018 "Configuration" table.
const (
	DefaultPerSubagentThreshold       = 2
	DefaultConvergenceThreshold       = 3
	DefaultClusterSimilarityThreshold = 0.8
	DefaultCandidateDecayDays         = 90

	DefaultSilentApplyProcess      = 0.8
	DefaultSilentApplyArchitecture = 0.8
	DefaultSilentApplyCode         = 0.65
	DefaultSilentApplyTooling      = 0.65
	DefaultSilentApplyTeam         = 0.5
)

// Config holds the tunable thresholds that govern pattern discovery,
// promotion, decay, and silent-apply behaviour. Per-category silent-apply
// thresholds are keyed by the Category string ("code", "process", ...).
type Config struct {
	PerSubagentThreshold       int                `json:"per_subagent_threshold"`
	ConvergenceThreshold       int                `json:"convergence_threshold"`
	ClusterSimilarityThreshold float64            `json:"cluster_similarity_threshold"`
	CandidateDecayDays         int                `json:"candidate_decay_days"`
	SilentApplyThreshold       map[string]float64 `json:"silent_apply_threshold,omitempty"`
}

// DefaultConfig returns a Config populated with ADR-0018 defaults.
func DefaultConfig() Config {
	return Config{
		PerSubagentThreshold:       DefaultPerSubagentThreshold,
		ConvergenceThreshold:       DefaultConvergenceThreshold,
		ClusterSimilarityThreshold: DefaultClusterSimilarityThreshold,
		CandidateDecayDays:         DefaultCandidateDecayDays,
		SilentApplyThreshold: map[string]float64{
			string(CategoryProcess):      DefaultSilentApplyProcess,
			string(CategoryArchitecture): DefaultSilentApplyArchitecture,
			string(CategoryCode):         DefaultSilentApplyCode,
			string(CategoryTooling):      DefaultSilentApplyTooling,
			string(CategoryTeam):         DefaultSilentApplyTeam,
		},
	}
}

// ConfigPath returns the canonical path to patterns-config.json for the
// supplied <group>/.archigraph/ directory.
func ConfigPath(groupArchigraphDir string) string {
	return filepath.Join(groupArchigraphDir, "patterns-config.json")
}

// LoadConfig reads the config file or returns defaults if absent. Any
// fields missing from the on-disk file are filled in from DefaultConfig
// so partial configs still behave correctly.
func LoadConfig(groupArchigraphDir string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(ConfigPath(groupArchigraphDir))
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("agentpatterns: read config: %w", err)
	}
	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		return cfg, fmt.Errorf("agentpatterns: parse config: %w", err)
	}
	mergeConfig(&cfg, loaded)
	return cfg, nil
}

// SaveConfig persists the config atomically (tmp + rename).
func SaveConfig(groupArchigraphDir string, cfg Config) error {
	if err := os.MkdirAll(groupArchigraphDir, 0o755); err != nil {
		return fmt.Errorf("agentpatterns: mkdir config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("agentpatterns: marshal config: %w", err)
	}
	path := ConfigPath(groupArchigraphDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("agentpatterns: write config tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("agentpatterns: rename config: %w", err)
	}
	return nil
}

// SetConfigKey applies a single `key=value` mutation in the form accepted
// by the `archigraph patterns config` CLI verb. Recognised keys:
//
//	per_subagent_threshold              (int)
//	convergence_threshold               (int)
//	cluster_similarity_threshold        (float)
//	candidate_decay_days                (int)
//	silent_apply_threshold.<category>   (float)
//
// Returns the updated config; the caller is responsible for persisting it.
func SetConfigKey(cfg Config, key, value string) (Config, error) {
	switch key {
	case "per_subagent_threshold":
		n, err := parseInt(value)
		if err != nil {
			return cfg, err
		}
		cfg.PerSubagentThreshold = n
	case "convergence_threshold":
		n, err := parseInt(value)
		if err != nil {
			return cfg, err
		}
		cfg.ConvergenceThreshold = n
	case "cluster_similarity_threshold":
		f, err := parseFloat(value)
		if err != nil {
			return cfg, err
		}
		cfg.ClusterSimilarityThreshold = f
	case "candidate_decay_days":
		n, err := parseInt(value)
		if err != nil {
			return cfg, err
		}
		cfg.CandidateDecayDays = n
	default:
		const prefix = "silent_apply_threshold."
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			cat := key[len(prefix):]
			f, err := parseFloat(value)
			if err != nil {
				return cfg, err
			}
			if cfg.SilentApplyThreshold == nil {
				cfg.SilentApplyThreshold = map[string]float64{}
			}
			cfg.SilentApplyThreshold[cat] = f
			return cfg, nil
		}
		return cfg, fmt.Errorf("unknown config key: %s", key)
	}
	return cfg, nil
}

// SilentApplyThresholdFor returns the configured silent-apply threshold for
// the named category, falling back to the ADR-0018 default for unknown
// categories.
func (c Config) SilentApplyThresholdFor(cat Category) float64 {
	if c.SilentApplyThreshold != nil {
		if v, ok := c.SilentApplyThreshold[string(cat)]; ok {
			return v
		}
	}
	switch cat {
	case CategoryProcess, CategoryArchitecture:
		return DefaultSilentApplyProcess
	case CategoryCode, CategoryTooling:
		return DefaultSilentApplyCode
	case CategoryTeam:
		return DefaultSilentApplyTeam
	}
	return DefaultSilentApplyCode
}

func mergeConfig(base *Config, override Config) {
	if override.PerSubagentThreshold != 0 {
		base.PerSubagentThreshold = override.PerSubagentThreshold
	}
	if override.ConvergenceThreshold != 0 {
		base.ConvergenceThreshold = override.ConvergenceThreshold
	}
	if override.ClusterSimilarityThreshold != 0 {
		base.ClusterSimilarityThreshold = override.ClusterSimilarityThreshold
	}
	if override.CandidateDecayDays != 0 {
		base.CandidateDecayDays = override.CandidateDecayDays
	}
	if override.SilentApplyThreshold != nil {
		if base.SilentApplyThreshold == nil {
			base.SilentApplyThreshold = map[string]float64{}
		}
		for k, v := range override.SilentApplyThreshold {
			base.SilentApplyThreshold[k] = v
		}
	}
}

func parseInt(s string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, fmt.Errorf("expected integer, got %q: %w", s, err)
	}
	return n, nil
}

func parseFloat(s string) (float64, error) {
	var f float64
	if _, err := fmt.Sscanf(s, "%g", &f); err != nil {
		return 0, fmt.Errorf("expected float, got %q: %w", s, err)
	}
	return f, nil
}
