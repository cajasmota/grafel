// Package registry manages the global grafel registry.
//
// The registry lives at ~/.grafel/registry.json and lists every
// installed group along with the path to its per-group config. Per
// ADR-0004 + ADR-0008 the registry is the single source of truth for
// the MCP router and the CLI; per-group config files live under XDG
// (~/.config/grafel/<group>.fleet.json) when XDG_CONFIG_HOME is
// available, else under ~/.grafel/groups/<group>/config.json.
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

	"github.com/cajasmota/grafel/internal/atomicfile"
	"github.com/cajasmota/grafel/internal/pathboundary"

	"github.com/cajasmota/grafel/internal/safeio"
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
// the registry under ~/.grafel/groups/<name>/.
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
		// Watchers controls the per-repo watcher UNIT — the launchd
		// LaunchAgent / systemd --user service / scheduled task that runs
		// `grafel watch <repo>` (internal/install/watchers).
		//
		// Setting it to false does NOT, by itself, stop a watcher that is
		// already installed (#6192). The unit belongs to the OS service
		// manager; no grafel process keeps it alive, so nothing about writing
		// a new value here reaches it. What each reader does with it:
		//
		//   - internal/install/install.go: `grafel install` writes a unit only
		//     when true.
		//   - internal/install/watchersync.go: ReconcileWatcherUnits rewrites
		//     and re-registers a unit only when true. For a group with it false
		//     it installs nothing — but it does still retire units under the
		//     superseded pre-#6183 and pre-normalisation labels, unloading and
		//     removing them.
		//   - internal/cli/watcher_fleet.go: `grafel start` re-activates only
		//     the units of groups with it true. `grafel stop` is deliberately
		//     ungated — a unit on disk is running whatever the config says.
		//   - internal/cli/wizard.go (applyGroupConfig) and
		//     internal/dashboard/v2_group_settings.go (PATCH .../features):
		//     these DO tear the units down when the resulting value is false.
		//     Both are synchronous, user-initiated statements about how the
		//     group should be set up, which a fleet.json edit — the path that
		//     runs no grafel code at all — is not.
		//   - cmd/grafel/daemon.go (daemonWorktreeParents): a group opts into
		//     linked-worktree tracking when track_worktrees OR this is true.
		//     That read IS retroactive: it is re-evaluated on every call, so
		//     for a group with track_worktrees off, turning this off also stops
		//     worktree discovery. Nothing to do with units.
		//   - internal/dashboard/v2_group_settings.go also reports the value on
		//     GET/PATCH, and derives the group doctor's watcher check from the
		//     machine rather than from this field.
		//
		// So a unit installed under an earlier `true` outlives the flag going
		// false through any path other than the two teardown call sites above.
		// `grafel status` names any such unit it finds RUNNING, and the group
		// doctor reports the same thing keyed on the same predicate.
		Watchers bool `json:"watchers"`
		GitHooks bool `json:"git_hooks"`
		// AutoInjectAgentsMD, when true, causes grafel to append (or
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
		// AgentHooks, when true, installs the OPT-IN Claude Code PreToolUse
		// grep-interceptor hook into each repo's .claude/settings.json. The
		// hook is advisory-only (never blocks) and nudges the agent toward
		// grafel MCP tools when it is about to run a STRUCTURAL grep.
		//
		// This is CLAUDE CODE ONLY reinforcement — no other agent host
		// exposes a PreToolUse surface — and it COMPLEMENTS, not replaces,
		// the cross-host rules block. Default false: it is opt-in to avoid
		// nagging users who don't want it (#4273).
		AgentHooks bool `json:"agent_hooks,omitempty"`
		// ChangeDetection selects which change detector observes this group's
		// working trees (#6932). One of:
		//
		//   "fsnotify" (default) — the fs watcher. Costs one inotify watch
		//       descriptor per DIRECTORY on Linux, recursively: 976 per
		//       worktree measured on this repo, and up to ~10,700 for one lane
		//       at GRAFEL_MAX_WORKTREES_PER_REPO=10.
		//   "poll"     — the descriptor-free ChangePoller (hybrid B of #6932):
		//       `git status --porcelain -unormal` for discovery plus a
		//       stat-sweep of the index manifest's own key set for the change
		//       decision. Zero watch descriptors. This is the container lane:
		//       fs.inotify.max_user_watches is per-UID, host-level and NOT
		//       namespaced, so every container running as the same UID draws
		//       from one pool and none of them can raise it.
		//   "auto"     — reserved for #6932 arm B, which will project the
		//       inotify cost before subscribing and switch on the BUDGET (not
		//       on repo size), ANNOUNCING the switch in `grafel status` and
		//       /diagnostics. Arm A accepts the value and resolves it to
		//       "fsnotify"; see ChangeDetectionMode / PollingEnabled.
		//
		// An unrecognised value resolves to "fsnotify" rather than failing the
		// group: a typo must not leave a group with no detector at all.
		//
		// Example fleet JSON:
		//   "features": { "change_detection": "poll" }
		ChangeDetection string `json:"change_detection,omitempty"`
		// ChangePollIntervalSeconds is the poll cadence for
		// change_detection="poll". Zero or negative selects
		// DefaultChangePollInterval (30 s).
		//
		// A cycle costs ~60 ms per worktree (macOS/APFS; #6932's table is
		// unvalidated on Linux/overlayfs), so 2 s is ~3% of a core per
		// worktree and 30 s is ~0.2% (~2% across ten worktrees). Immediacy is
		// not the point: checkout/merge/rebase are already covered by git
		// hooks and GitHeadPoller's 2 s .git/HEAD poll, and grafel already
		// documents a bounded-staleness contract at 2 s (reloadBeforeCall).
		ChangePollIntervalSeconds int `json:"change_poll_interval_seconds,omitempty"`
	} `json:"features"`
	// Tools is the set of AI coding tools this group's install targets,
	// identified by ToolAdapter ID (e.g. "claude", "cursor", "copilot").
	// It gates which per-tool artifacts (rules files, MCP entries, skills)
	// `grafel install` writes.
	//
	// Back-compat: when absent or empty the effective set is the historical
	// default — every supported tool, i.e. Claude (full: MCP + skills +
	// rules + opt-in hooks) plus all rules-file conventions. Resolve the
	// effective set with DefaultEnabledTools / EnabledTools rather than
	// reading this field directly.
	//
	// Example fleet JSON:
	//   "tools": ["claude", "cursor"]
	Tools []string `json:"tools,omitempty"`
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

// Manifest is the committed teammate file: <repo>/.grafel/group.json.
type Manifest struct {
	Group string `json:"group"`
	Repos []struct {
		Slug     string `json:"slug"`
		CloneURL string `json:"clone_url,omitempty"`
		Stack    string `json:"stack,omitempty"`
	} `json:"repos"`
}

var mu sync.Mutex

// HomeDir returns the grafel home (~/.grafel) honoring overrides.
func HomeDir() (string, error) {
	if override := os.Getenv("GRAFEL_HOME"); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".grafel"), nil
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
// Falls back to ~/.grafel/groups/<name>/ when XDG_CONFIG_HOME and
// the user home are unavailable in the same arrangement.
func ConfigDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "grafel"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "grafel"), nil
}

// ConfigPathFor returns the standard config-path for a group name.
//
// This is a READ-safe derivation: it is also used to resolve the config path
// of an already-registered group (docgen, doctor, export, the CLI group
// list, ...), including one written before ValidateGroupName existed or
// whose name is otherwise grandfathered-invalid. It intentionally does NOT
// validate name — see ValidateGroupName's doc comment for why gating reads
// would be worse than the bug it prevents. Callers about to CREATE or
// OVERWRITE a config file for a name that is not yet known-registered MUST
// use ConfigPathForNew instead.
func ConfigPathFor(name string) (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, name+".fleet.json"), nil
}

// ConfigPathForNew is the write-side counterpart of ConfigPathFor: it
// validates name before deriving the path.
//
// #6186 R1 (found on review): the F6 fix added a ValidateGroupName call
// ahead of SaveGroupConfig at AddGroup's four known callers, enumerated by
// hand. A fifth writer — the archive-import handler in
// internal/dashboard/handlers_v2_graph_export.go, which takes its group name
// from an uploaded ZIP's manifest.json or a request query parameter — used
// ConfigPathFor + SaveGroupConfig with no validation anywhere on that path,
// because it was never on the enumerated list. filepath.Join collapses
// "..", so a manifest group of "../../../../tmp/pwn" made SaveGroupConfig
// write a file outside the config directory before AddGroup's (too-late)
// rejection ever ran.
//
// The fix to that shape, not just that site: a validated derivation that is
// structurally distinct from the read path, so a NEW writer reaches into
// this function by construction rather than by remembering to call
// ValidateGroupName itself. Use this (not ConfigPathFor) at any call site
// that is about to create or overwrite a group's config file for a name
// that is not already a trusted, registered group.
func ConfigPathForNew(name string) (string, error) {
	if err := ValidateGroupName(name); err != nil {
		return "", err
	}
	return ConfigPathFor(name)
}

// StateDirFor returns the per-group state directory under HomeDir.
//
// Like ConfigPathFor this is a READ-safe derivation and intentionally does NOT
// validate name, so a grandfathered-invalid registry entry stays resolvable.
// Callers that are about to DELETE the returned path must use
// StateDirForExisting instead — see #6194 and its doc comment.
func StateDirFor(name string) (string, error) {
	h, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "groups", name), nil
}

// StateRootDir is the single directory that contains every per-group state
// directory. It exists so the delete-side containment assertion has one named
// root instead of each call site re-deriving its own idea of "inside".
func StateRootDir() (string, error) {
	h, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "groups"), nil
}

// StateDirForExisting is the delete-side counterpart of StateDirFor: it
// derives the same path and then asserts that the result is genuinely inside
// the state root before handing it back (#6194).
//
// Why a containment assertion and not ValidateGroupName. The two purge sites
// (install.Uninstall and the daemon's DeleteGroup) ran
// os.RemoveAll(StateDirFor(group)) on a name taken straight from registry.json.
// filepath.Join collapses "..", so a name like "../../../work" resolves outside
// the state root and RemoveAll follows it. Reachability is narrow and is the
// direct, accepted consequence of the read/write split above: no NEW invalid
// name can be created because every write path validates, but entries that
// predate ValidateGroupName exist by design and must keep loading. Rejecting
// such a name here would resurrect exactly the "registry becomes unusable"
// problem that split was written to avoid — a grandfathered group would become
// un-uninstallable. So the gate is on the derived PATH, not on the name: a
// weird-but-contained name like "my/group" still purges; only an escape is
// refused.
//
// It returns no path alongside its error, so a caller that ignores the error
// still cannot delete anything.
func StateDirForExisting(name string) (string, error) {
	root, err := StateRootDir()
	if err != nil {
		return "", err
	}
	dir, err := StateDirFor(name)
	if err != nil {
		return "", err
	}
	if !PathContainedUnder(root, dir) {
		return "", fmt.Errorf("group %q resolves to state directory %q, which is outside the state root %q; "+
			"refusing to derive a deletable path (#6194)", name, filepath.Clean(dir), root)
	}
	return dir, nil
}

// PathContainedUnder reports whether p is a strict descendant of root.
//
// Three things this deliberately does NOT do, each of which is a real trap:
//
//   - It does not use strings.HasPrefix. "/home/u/.grafel-evil" has
//     "/home/u/.grafel" as a literal prefix but is a sibling, not a child.
//     filepath.Rel gives "../.grafel-evil" for that pair, which is rejected on
//     the ".." boundary, so the separator boundary is enforced by construction.
//
//   - It does not compare unresolved strings. If the state root is reached
//     through a symlink (~/.grafel on another volume is a supported layout) or
//     an intermediate component inside it is a symlink pointing back out, a
//     lexical comparison is meaningless in both directions: it rejects the
//     legitimate symlinked root, and it accepts "esc/x" where <root>/esc links
//     elsewhere — which os.RemoveAll happily follows. This is the same trap as
//     #6187 in the watcher reaper. Both sides are therefore resolved through
//     the deepest existing ancestor before comparing.
//
//   - It does not resolve p's own final component. os.RemoveAll does not
//     follow a symlink at the leaf — it unlinks the link itself — so a state
//     directory that is a symlink to another volume is not an escape, and
//     refusing it would break that layout for no safety gain. Only the
//     traversed part (p's parent chain) is resolved.
//
// Equality is not containment: p == root returns false, so an empty group name
// cannot resolve to the state root itself and take every group with it.
func PathContainedUnder(root, p string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pAbs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	rootReal := resolveDeepestExisting(rootAbs)
	pReal := filepath.Join(resolveDeepestExisting(filepath.Dir(pAbs)), filepath.Base(pAbs))

	rel, err := filepath.Rel(rootReal, pReal)
	if err != nil {
		// Different volumes on Windows, or otherwise incomparable. Not
		// contained, and unknowable is the same as unsafe here.
		return false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// resolveDeepestExisting returns p with its longest existing ancestor replaced
// by that ancestor's symlink-resolved form, leaving the not-yet-existing tail
// appended.
//
// filepath.EvalSymlinks fails outright if any component is missing, but a
// state directory legitimately may not exist yet (or any more) at the moment
// containment is checked. Walking up until something resolves keeps the
// comparison meaningful in that case instead of silently falling back to a
// lexical check on one side only — which would make the two sides
// incomparable and produce false verdicts in both directions.
//
// If nothing at all resolves, p is returned unchanged; both sides then get the
// same lexical treatment, which is still enough to catch a "..".
//
// The ascent runs through pathboundary.Climb (#6548): it used to be a bare
// `for { parent := filepath.Dir(cur) ... }` with the filesystem root as its
// only stop, one filepath.EvalSymlinks per level, reached from
// PathContainedUnder on every registry path validation in the daemon —
// unprompted, and with nothing bounding it. It now stops at $HOME when p is
// inside it, refuses a protected directory, and has a depth backstop. When the
// boundary is reached before anything resolves, that is the "nothing resolved"
// case the contract above already describes.
func resolveDeepestExisting(p string) string {
	var tail []string
	out := p
	resolvedAny := pathboundary.Climb(p, func(cur string) bool {
		resolved, err := filepath.EvalSymlinks(cur)
		if err != nil {
			tail = append(tail, filepath.Base(cur))
			return false
		}
		for i := len(tail) - 1; i >= 0; i-- {
			resolved = filepath.Join(resolved, tail[i])
		}
		out = resolved
		return true
	})
	if !resolvedAny {
		return p
	}
	return out
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
	guardResolvedConfigPath(path, "registry.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	sort.Slice(r.Groups, func(i, j int) bool { return r.Groups[i].Name < r.Groups[j].Name })
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	// #6018: unique temp name. The registry is written by BOTH the CLI and the
	// daemon with no cross-process lock, so a deterministic path+".tmp" is
	// shared by concurrent writers and a torn registry is a broken install.
	return atomicfile.WriteFile(path, b, 0o644)
}

// maxGroupNameBytes bounds how long a group name may be.
//
// ConfigPathFor appends ".fleet.json" (11 bytes) to the name to form a
// filename; StateDirFor uses the name as a bare directory component. Most
// filesystems this ships on (APFS, ext4, NTFS's practical limit) cap a single
// path component at 255 bytes. 100 leaves generous headroom for the
// ".fleet.json" suffix and any future per-name derived filenames, without
// being so tight it constrains a real (if unusually long) project name.
const maxGroupNameBytes = 100

// ValidateGroupName rejects group names that are unsafe as a filesystem path
// segment (#6186, widened by #6186 F5).
//
// Group is interpolated raw into Unit.Label(), which is both the watcher
// plist's filename and launchd's job identity (internal/install/watchers).
// A group containing a path separator — e.g. "g/h" — turns
// "com.grafel.watcher.g/h.<repo>.plist" into a path with an intermediate
// directory component; watchers.Write then os.MkdirAll's that directory
// inside ~/Library/LaunchAgents, which launchd does not scan, so the watcher
// is silently never registered. A name like "../escape" or "." escapes (or
// collides with) that directory entirely.
//
// Also rejected, per review of the first cut of this fix:
//   - control characters / NUL: "g\n[Service]\nExecStart=evil" reaches
//     Description= in the rendered systemd unit (see
//     internal/install/watchers' validateUnitFields, which independently
//     guards the write path — this is defense at the earlier, more visible
//     boundary) and "g\x00h" makes every later ConfigPathFor/StateDirFor/
//     plist path operation fail EINVAL, leaving a registry entry that can
//     never be materialised.
//   - all-whitespace: produces a Label launchd will not accept — the exact
//     silent-non-registration failure mode #6186 exists to prevent.
//   - over-length: risks exceeding NAME_MAX on the derived path.
//
// This intentionally does NOT reuse slugify's full alnum-only character
// class: group names are commonly derived from a directory basename
// (defaultGroupName in internal/cli/wizard.go), which routinely contains
// spaces, dots, and underscores — none of those are unsafe as a single path
// segment, and rejecting them would be a regression for existing onboarding
// flows. Only characters/shapes that can actually create/traverse a
// directory, corrupt a derived path, or break the watcher formats are
// blocked.
//
// This check runs at group-creation time (AddGroup, and — per #6186 F6,
// callers must invoke it before ever calling SaveGroupConfig — see
// internal/cli/wizard.go, internal/cli/onboard.go, internal/install/
// install.go, internal/dashboard/store.go), not at Load/Groups(): registries
// that already contain an invalid name (written before this fix, or edited
// by hand) must keep loading normally. Rejecting on load would turn a
// silently-broken watcher into a hard failure to even read the registry,
// which is worse than the bug being fixed.
func ValidateGroupName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("group name required")
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("group name %q must not contain a path separator", name)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("group name %q is not a valid path segment", name)
	}
	if hasControlByte(name) {
		return fmt.Errorf("group name %q must not contain a control character", name)
	}
	if len(name) > maxGroupNameBytes {
		return fmt.Errorf("group name %q is %d bytes, want at most %d", name, len(name), maxGroupNameBytes)
	}
	return nil
}

// hasControlByte reports whether s contains an ASCII control byte (0x00-0x1F
// or 0x7F), including NUL, CR and LF. Mirrors
// internal/install/watchers.hasControlByte; kept as an independent,
// unexported copy rather than a shared dependency between the two packages
// for what is a five-line check with different callers and different
// rationale (path-segment safety here vs. unit-format injection there).
func hasControlByte(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

// AddGroup adds a group to the registry and persists. Idempotent: if the
// group already exists it is updated in place. The config file must exist
// at the target path; otherwise an error is returned.
func AddGroup(name, configPath string) error {
	if err := ValidateGroupName(name); err != nil {
		return err
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
	guardResolvedConfigPath(path, "fleet config")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// #6018: unique temp name — same cross-process CLI/daemon exposure as the
	// registry itself.
	return atomicfile.WriteFile(path, b, 0o644)
}

// LoadManifest reads a committed teammate manifest from
// <repo>/.grafel/group.json.
func LoadManifest(repoOrManifest string) (*Manifest, error) {
	p := repoOrManifest
	if fi, err := os.Stat(p); err == nil && fi.IsDir() {
		p = filepath.Join(p, ".grafel", "group.json")
	}
	b, err := safeio.ReadFileReported(p, safeio.FollowSymlinks, safeio.MaxConfigFileBytes)
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
