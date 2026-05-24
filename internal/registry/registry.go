// Package registry manages the global archigraph registry.
//
// The registry lives at ~/.archigraph/registry.json and lists every
// installed group along with the path to its per-group config. Per
// ADR-0004 + ADR-0008 the registry is the single source of truth for
// the MCP router and the CLI; per-group config files live under XDG
// (~/.config/archigraph/<group>.fleet.json) when XDG_CONFIG_HOME is
// available, else under ~/.archigraph/groups/<group>/config.json.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// StackList is a JSON-polymorphic list of language tags for a repo.
//
// On disk the "stack" field may appear in two shapes produced by different
// versions of the binary:
//
//	{"stack": "go"}             ← single string (old shape)
//	{"stack": ["go","typescript"]} ← array of strings (new shape)
//
// Both forms are accepted on read; the value is always written back as an
// array so new configs are unambiguous. Callers that need a single canonical
// label should call Primary().
type StackList []string

// UnmarshalJSON accepts null/absent, a bare JSON string, or a JSON array of
// strings. Any other shape is returned as a descriptive error.
func (s *StackList) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*s = nil
		return nil
	}
	// Try array first.
	if b[0] == '[' {
		var arr []string
		if err := json.Unmarshal(b, &arr); err != nil {
			return fmt.Errorf("stack: cannot parse array of strings: %w", err)
		}
		*s = arr
		return nil
	}
	// Try bare string.
	if b[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return fmt.Errorf("stack: cannot parse string: %w", err)
		}
		if str == "" {
			*s = nil
		} else {
			*s = StackList{str}
		}
		return nil
	}
	return fmt.Errorf("stack: expected string or array, got %s", string(b))
}

// MarshalJSON always writes an array (or omits the field when the list is
// empty, relying on the omitempty tag on the containing struct field).
func (s StackList) MarshalJSON() ([]byte, error) {
	if len(s) == 0 {
		return []byte("null"), nil
	}
	return json.Marshal([]string(s))
}

// Primary returns the first element, or "" if the list is empty.
// Use this wherever a single canonical label is needed (display, detect
// fallback, equality checks).
func (s StackList) Primary() string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// String returns a slash-joined representation suitable for display
// (e.g. "go/typescript"). Returns "" for an empty list.
func (s StackList) String() string {
	return strings.Join(s, "/")
}

// IsEmpty reports whether the list contains no elements.
func (s StackList) IsEmpty() bool { return len(s) == 0 }

// GroupRef is a registered group: a name and the absolute path to its
// per-group config file. The group's state directory is colocated with
// the registry under ~/.archigraph/groups/<name>/.
type GroupRef struct {
	Name        string `json:"name"`
	ConfigPath  string `json:"config_path"`
	InstalledAt string `json:"installed_at,omitempty"`
}

// Registry is the on-disk shape persisted to registry.json.
type Registry struct {
	Version int        `json:"version"`
	Groups  []GroupRef `json:"groups"`
}

// Repo describes a single repository inside a group config.
type Repo struct {
	Slug     string    `json:"slug"`
	Path     string    `json:"path"`
	Stack    StackList `json:"stack,omitempty"`
	CloneURL string    `json:"clone_url,omitempty"`
	Modules  []string  `json:"modules,omitempty"`
}

// GroupConfig is the per-group config persisted alongside the registry.
type GroupConfig struct {
	Name      string `json:"name"`
	GroupDocs string `json:"group_docs,omitempty"`
	Repos     []Repo `json:"repos"`
	Features  struct {
		Watchers bool `json:"watchers"`
		GitHooks bool `json:"git_hooks"`
		// AutoInjectAgentsMD, when true, causes archigraph to append (or
		// update) an "Architecture Map" marker block in each repo's AGENTS.md
		// (or CLAUDE.md / GEMINI.md) after every rebuild. The block tells AI
		// coding agents that the repo is indexed, where the dashboard is, and
		// which MCP endpoints to query. Default false — opt-in only.
		AutoInjectAgentsMD bool `json:"auto_inject_agents_md,omitempty"`
		// TrackWorktrees, when true, enables PH3 worktree auto-discovery for
		// this group. The daemon polls `git worktree list` every 5 minutes for
		// each repo in the group and registers linked worktrees as ephemeral
		// children. Default false — opt-in to preserve existing behaviour.
		//
		// Example fleet JSON:
		//   "features": { "track_worktrees": true }
		TrackWorktrees bool `json:"track_worktrees,omitempty"`
	} `json:"features"`
	// ExtraStdlibFilter is a user-extensible map from language tag to a list
	// of bare-name symbols that should be suppressed as if they were stdlib
	// builtins — i.e. no placeholder External entity is emitted for them.
	// Use this to suppress framework stdlibs that are specific to your group
	// (e.g. Django's django.contrib.auth.models when you only care about your
	// own code). Values are loaded via resolve.RegisterExtraStdlibFilter at
	// daemon startup. Issue #1206.
	//
	// Example in fleet JSON:
	//   "extra_stdlib_filter": {
	//     "python": ["authenticate", "login_required", "permission_required"],
	//     "java":   ["doFilter", "doGet", "doPost"]
	//   }
	ExtraStdlibFilter map[string][]string `json:"extra_stdlib_filter,omitempty"`
}

// Manifest is the committed teammate file: <repo>/.archigraph/group.json.
type Manifest struct {
	Group string `json:"group"`
	Repos []struct {
		Slug     string `json:"slug"`
		CloneURL string `json:"clone_url,omitempty"`
		Stack    string `json:"stack,omitempty"`
	} `json:"repos"`
}

var mu sync.Mutex

// HomeDir returns the archigraph home (~/.archigraph) honoring overrides.
func HomeDir() (string, error) {
	if override := os.Getenv("ARCHIGRAPH_HOME"); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".archigraph"), nil
}

// RegistryPath is the canonical path to registry.json.
func RegistryPath() (string, error) {
	h, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "registry.json"), nil
}

// ConfigDir returns the XDG-friendly per-group config directory.
// Falls back to ~/.archigraph/groups/<name>/ when XDG_CONFIG_HOME and
// the user home are unavailable in the same arrangement.
func ConfigDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "archigraph"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "archigraph"), nil
}

// ConfigPathFor returns the standard config-path for a group name.
func ConfigPathFor(name string) (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, name+".fleet.json"), nil
}

// StateDirFor returns the per-group state directory under HomeDir.
func StateDirFor(name string) (string, error) {
	h, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "groups", name), nil
}

// Load reads the registry from disk. A missing file returns an empty
// Registry — never an error — so first-run callers do not have to
// special-case ENOENT.
func Load() (*Registry, error) {
	mu.Lock()
	defer mu.Unlock()
	p, err := RegistryPath()
	if err != nil {
		return nil, err
	}
	return loadFrom(p)
}

func loadFrom(path string) (*Registry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Registry{Version: 1}, nil
		}
		return nil, err
	}
	r := &Registry{}
	if err := json.Unmarshal(b, r); err != nil {
		return nil, fmt.Errorf("registry.json: %w", err)
	}
	if r.Version == 0 {
		r.Version = 1
	}
	return r, nil
}

// Save writes the registry atomically (tmp + rename).
func Save(r *Registry) error {
	mu.Lock()
	defer mu.Unlock()
	p, err := RegistryPath()
	if err != nil {
		return err
	}
	return saveTo(p, r)
}

func saveTo(path string, r *Registry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	sort.Slice(r.Groups, func(i, j int) bool { return r.Groups[i].Name < r.Groups[j].Name })
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// AddGroup adds a group to the registry and persists. Idempotent: if the
// group already exists it is updated in place. The config file must exist
// at the target path; otherwise an error is returned.
func AddGroup(name, configPath string) error {
	if name == "" {
		return errors.New("group name required")
	}
	// Validate that the config file exists.
	if _, err := os.Stat(configPath); err == os.ErrNotExist {
		return fmt.Errorf("config file does not exist: %s", configPath)
	} else if err != nil {
		return fmt.Errorf("cannot access config file: %w", err)
	}
	r, err := Load()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range r.Groups {
		if r.Groups[i].Name == name {
			r.Groups[i].ConfigPath = configPath
			return Save(r)
		}
	}
	r.Groups = append(r.Groups, GroupRef{Name: name, ConfigPath: configPath, InstalledAt: now})
	return Save(r)
}

// RemoveGroup removes a group by name. Returns nil even if the group is
// unknown (idempotent uninstall).
func RemoveGroup(name string) error {
	r, err := Load()
	if err != nil {
		return err
	}
	out := r.Groups[:0]
	for _, g := range r.Groups {
		if g.Name != name {
			out = append(out, g)
		}
	}
	r.Groups = out
	return Save(r)
}

// Groups returns the registered groups, sorted by name.
func Groups() ([]GroupRef, error) {
	r, err := Load()
	if err != nil {
		return nil, err
	}
	return r.Groups, nil
}

// LoadGroupConfig reads a per-group config file.
func LoadGroupConfig(path string) (*GroupConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &GroupConfig{}
	if err := json.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return cfg, nil
}

// SaveGroupConfig writes a per-group config atomically.
func SaveGroupConfig(path string, cfg *GroupConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadManifest reads a committed teammate manifest from
// <repo>/.archigraph/group.json.
func LoadManifest(repoOrManifest string) (*Manifest, error) {
	p := repoOrManifest
	if fi, err := os.Stat(p); err == nil && fi.IsDir() {
		p = filepath.Join(p, ".archigraph", "group.json")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	m := &Manifest{}
	if err := json.Unmarshal(b, m); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(p), err)
	}
	return m, nil
}

// ConfigParseError records a single fleet-config parse failure.
type ConfigParseError struct {
	ConfigPath string
	GroupName  string
	Err        error
}

func (e *ConfigParseError) Error() string {
	return fmt.Sprintf("fleet config %q (group %q): %v", e.ConfigPath, e.GroupName, e.Err)
}

// ValidateFleetConfigs attempts to parse every registered fleet config and
// returns one ConfigParseError per file that fails. A non-nil, non-empty
// slice means at least one config is unreadable — callers should log each
// entry and continue operating on the healthy configs rather than hard-failing
// the whole daemon.
//
// Typical call site: daemon startup, before the first indexer run.
func ValidateFleetConfigs() []*ConfigParseError {
	groups, err := Load()
	if err != nil {
		return []*ConfigParseError{{ConfigPath: "(registry)", Err: err}}
	}
	var errs []*ConfigParseError
	for _, g := range groups.Groups {
		if _, err := LoadGroupConfig(g.ConfigPath); err != nil {
			errs = append(errs, &ConfigParseError{
				ConfigPath: g.ConfigPath,
				GroupName:  g.Name,
				Err:        err,
			})
		}
	}
	return errs
}
