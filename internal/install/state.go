// Package install provides the install/uninstall/update logic for the
// archigraph CLI, daemon, skills, and MCP registration. This file owns
// the ~/ .archigraph/install.json schema and read/write helpers used by
// `archigraph install` (COPY mode) and future `archigraph doctor` (#2211).
package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// StateSchemaVersion is the current schema version written into install.json.
// Bump this when the shape of State changes in a backward-incompatible way.
const StateSchemaVersion = 1

// InstallMode describes which install mode was used.
type InstallMode string

const (
	// ModeCopy is the default COPY mode: skills are copied, not symlinked.
	ModeCopy InstallMode = "copy"
	// ModeDev is the dev/contributor mode: skills are symlinked (issue #2212).
	ModeDev InstallMode = "dev"
)

// CLIRecord holds information about the running binary that performed
// the install.
type CLIRecord struct {
	// Path is the absolute path to the archigraph binary.
	Path string `json:"path"`
	// SHA256 is the hex-encoded SHA-256 hash of the binary at Path.
	SHA256 string `json:"sha256"`
}

// SkillRecord holds the per-file SHA-256 manifest for one installed skill.
type SkillRecord struct {
	// Files maps relative-path-within-skill → hex SHA-256 of that file.
	// Populated in COPY mode; empty (or nil) in DEV mode (skills are live symlinks).
	Files map[string]string `json:"files,omitempty"`

	// DevTarget is the absolute path of the repo skill directory that the
	// symlink/junction points to.  Only set when install_mode = "dev".
	// Doctor uses this to verify the symlink target still matches.
	DevTarget string `json:"dev_target,omitempty"`
}

// MCPRecord holds the MCP registration state.
type MCPRecord struct {
	// Name is the mcpServers key used in .claude.json (always "archigraph").
	Name string `json:"name"`
	// RegisteredPaths is the list of .claude.json paths we wrote to.
	RegisteredPaths []string `json:"registered_paths,omitempty"`
}

// GitignoreRecord records which git repos we appended .archigraph/ to.
type GitignoreRecord struct {
	// Repos lists the absolute repo roots where we added the .gitignore entry.
	Repos []string `json:"repos,omitempty"`
}

// State is the schema of ~/.archigraph/install.json.
// It is the authoritative record of an install transaction. Future
// `archigraph doctor` (#2211) reads from this file.
type State struct {
	// SchemaVersion must equal StateSchemaVersion; future tooling can
	// refuse to read older/newer schemas.
	SchemaVersion int `json:"schema_version"`

	// InstalledAt is the RFC3339 timestamp of the install transaction.
	InstalledAt string `json:"installed_at"`

	// InstallMode is the mode used ("copy" or "dev").
	InstallMode InstallMode `json:"install_mode"`

	// CLI is the binary that ran this install.
	CLI CLIRecord `json:"cli"`

	// Skills maps skill name → per-file SHA manifest.
	// Populated after step 2 completes.
	Skills map[string]SkillRecord `json:"skills,omitempty"`

	// MCP holds the MCP registration state.
	// Populated after step 3 completes.
	MCP MCPRecord `json:"mcp,omitempty"`

	// DaemonVersion is the version string reported by /healthz after restart.
	// Populated after step 4 completes.
	DaemonVersion string `json:"daemon_version,omitempty"`

	// Gitignore records .gitignore mutations.
	// Populated after step 5 completes.
	Gitignore GitignoreRecord `json:"gitignore,omitempty"`

	// RollbackFromStep is non-zero when install partially completed but
	// was rolled back. It records the step number from which rollback began,
	// so the user can understand exactly how far things got.
	RollbackFromStep int `json:"rollback_from_step,omitempty"`

	// PartialInstall is true when the state reflects a partial/rolled-back
	// install. A truthy value causes future `archigraph install` runs
	// (without --force) to refuse and direct the user to --force or uninstall.
	PartialInstall bool `json:"partial_install,omitempty"`
}

// DefaultStatePath returns the canonical path for install.json.
// It honours HOME on all platforms (so tests can redirect via t.Setenv).
func DefaultStatePath() (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".archigraph", "install.json"), nil
}

// userHomeDir returns the user's home directory, preferring the HOME
// environment variable so tests can override it portably.
func userHomeDir() (string, error) {
	if h := os.Getenv("HOME"); h != "" {
		return h, nil
	}
	return os.UserHomeDir()
}

// ReadState reads and parses the install state from path.
// Returns (nil, nil) when the file does not exist.
func ReadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read install state %s: %w", path, err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse install state %s: %w", path, err)
	}
	return &s, nil
}

// WriteState atomically writes state to path (parent dirs are created
// as needed). An atomic rename is used so the file is never left in a
// half-written state.
func WriteState(path string, state *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal install state: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write install state tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename install state: %w", err)
	}
	return nil
}

// NewState returns a fresh State pre-populated with the current time,
// schema version, and install mode.
func NewState(mode InstallMode) *State {
	return &State{
		SchemaVersion: StateSchemaVersion,
		InstalledAt:   time.Now().UTC().Format(time.RFC3339),
		InstallMode:   mode,
		Skills:        make(map[string]SkillRecord),
	}
}
