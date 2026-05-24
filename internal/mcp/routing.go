package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/gitmeta"
)

// resolveGroup implements the ADR-0008 cascade (#1746):
//
//  1. explicit `group` argument
//  2. CWD inference via .archigraph/group.json marker (walk upward)
//  3. Registry-based CWD inference: match cwd against registered repo paths
//  4. Singleton-group fallback (only one group registered)
//
// Returns the chosen group name, the source ("explicit"/"cwd"/"cwd_registry"/
// "singleton"), or an error when the group cannot be determined.
//
// Error cases:
//   - cwd is inside repos registered to multiple groups → "ambiguous group"
//     error listing only the matching candidate groups.
//   - cwd is not inside any registered repo AND multiple groups exist →
//     "ambiguous group" error listing all registered groups.
//   - registry is empty → distinct error.
func resolveGroup(s *State, explicit, cwd string) (string, string, error) {
	if explicit != "" {
		return explicit, "explicit", nil
	}
	if g := groupFromCWD(cwd); g != "" {
		// only honor it if the registry knows about it
		if _, ok := s.registry.Groups[g]; ok {
			return g, "cwd", nil
		}
	}
	// Registry-based cwd inference (#1650 / #1746): walk the registry and pick
	// the group whose repo path is a prefix of cwd. groupFromRegistryWithCandidates
	// returns the single matched group or "" + the distinct matching groups for the
	// error message when multiple groups cover the cwd.
	g, candidates := groupFromRegistryWithCandidates(s, cwd)
	if g != "" {
		return g, "cwd_registry", nil
	}
	if len(candidates) > 1 {
		// cwd is under repos in multiple groups — genuinely ambiguous.
		sort.Strings(candidates)
		return "", "", errors.New("ambiguous group; pass `group=<name>`. candidate groups for your cwd: " + strings.Join(candidates, ", "))
	}
	if len(s.registry.Groups) == 1 {
		for g := range s.registry.Groups {
			return g, "singleton", nil
		}
	}
	if len(s.registry.Groups) == 0 {
		return "", "", errors.New("no groups registered (registry is empty)")
	}
	known := make([]string, 0, len(s.registry.Groups))
	for g := range s.registry.Groups {
		known = append(known, g)
	}
	sort.Strings(known)
	return "", "", errors.New("ambiguous group; pass `group=<name>`. registered groups: " + strings.Join(known, ", "))
}

// groupFromRegistry returns the registered group whose repo path is an
// ancestor of cwd. Returns "" when cwd is empty, no registered repo path
// covers cwd, or multiple groups cover it (ambiguous). See
// groupFromRegistryWithCandidates for the richer variant that also returns
// the matching candidate group names.
func groupFromRegistry(s *State, cwd string) string {
	g, _ := groupFromRegistryWithCandidates(s, cwd)
	return g
}

// groupFromRegistryWithCandidates is the core registry-cwd matcher (#1746).
// It walks the registry and collects all groups whose repo path is an ancestor
// of cwd. Returns:
//   - (group, nil) when exactly one group's repos cover cwd (unambiguous).
//   - ("", candidates) when multiple distinct groups cover cwd; candidates
//     lists those group names so the caller can surface a targeted error.
//   - ("", nil) when cwd is empty, the registry is empty/nil, or no repo
//     covers cwd.
//
// When multiple repos from the SAME group cover cwd, the longest (most
// specific) repo path is preferred — that is unambiguous.
func groupFromRegistryWithCandidates(s *State, cwd string) (string, []string) {
	if cwd == "" || s == nil || s.registry == nil {
		return "", nil
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	abs = filepath.Clean(abs)
	type hit struct {
		group string
		path  string
	}
	var hits []hit
	for gname, gentry := range s.registry.Groups {
		for _, repo := range gentry.Repos {
			if repo.Path == "" {
				continue
			}
			rp := filepath.Clean(repo.Path)
			if pathContains(rp, abs) {
				hits = append(hits, hit{group: gname, path: rp})
			}
		}
	}
	if len(hits) == 0 {
		return "", nil
	}
	// Collect distinct matched groups.
	groupSet := make(map[string]string) // group → longest matching path
	for _, h := range hits {
		if prev, ok := groupSet[h.group]; !ok || len(h.path) > len(prev) {
			groupSet[h.group] = h.path
		}
	}
	if len(groupSet) == 1 {
		// Unambiguous: all hits belong to the same group.
		for g := range groupSet {
			return g, nil
		}
	}
	// Multiple distinct groups cover cwd — return candidates for error reporting.
	candidates := make([]string, 0, len(groupSet))
	for g := range groupSet {
		candidates = append(candidates, g)
	}
	return "", candidates
}

// pathContains reports whether ancestor is an ancestor (or equal to) child.
// Both paths must already be absolute + clean.
func pathContains(ancestor, child string) bool {
	if ancestor == child {
		return true
	}
	sep := string(os.PathSeparator)
	if !strings.HasSuffix(ancestor, sep) {
		ancestor += sep
	}
	return strings.HasPrefix(child+sep, ancestor)
}

// groupFromCWD walks dir upward looking for .archigraph/group.json which
// encodes {"group": "<name>"}.
func groupFromCWD(dir string) string {
	if dir == "" {
		return ""
	}
	cur := dir
	for {
		marker := filepath.Join(cur, ".archigraph", "group.json")
		if data, err := os.ReadFile(marker); err == nil {
			var doc struct {
				Group string `json:"group"`
			}
			if err := json.Unmarshal(data, &doc); err == nil && doc.Group != "" {
				return doc.Group
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

// repoFromCWD walks dir upward looking for the repo's .archigraph dir; the
// repo's directory name is returned if found.
func repoFromCWD(dir string) string {
	if dir == "" {
		return ""
	}
	cur := dir
	for {
		if _, err := os.Stat(filepath.Join(cur, ".archigraph")); err == nil {
			return filepath.Base(cur)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

// CWDResolution carries the result of resolving a cwd to a (group, repo, ref)
// triple. It is the canonical output of ResolveCWD (PH1c, epic #2087).
type CWDResolution struct {
	// Group is the archigraph group name, or "" if unresolved.
	Group string
	// RepoSlug is the slug of the repo within the group whose path contains cwd.
	RepoSlug string
	// ModuleSlug is the relative sub-path of the module within a monorepo repo
	// when cwd is inside a declared Module sub-path. Empty when not in a module
	// (standalone repo or monorepo root). Populated by moduleSlugForCWD (M3 #2180).
	ModuleSlug string
	// Ref is the git HEAD ref of the repo (or worktree). Empty string means
	// detached HEAD or non-git directory.
	Ref string
	// SHA is the abbreviated (12-char) commit hash at the resolved HEAD.
	SHA string
	// IsWorktree is true when cwd is inside a linked git worktree (not the
	// main checkout). The Ref then belongs to the worktree's HEAD, not the
	// parent's HEAD.
	IsWorktree bool
	// ParentRepoPath is the parent repo path when IsWorktree is true.
	ParentRepoPath string
	// Source is "worktree_registry" (PH3 ephemeral child), "cwd_registry",
	// "worktree" (PH1c sibling match), "cwd", "singleton", "explicit", or "none".
	Source string
}

// fleetRepoEntry is the minimal fleet-config repo shape needed for module
// resolution. It mirrors registry.Repo but avoids an import cycle.
type fleetRepoEntry struct {
	Slug    string   `json:"slug"`
	Path    string   `json:"path"`
	Modules []string `json:"modules,omitempty"`
}

// fleetGroupConfig is the minimal fleet-config shape for module resolution.
type fleetGroupConfig struct {
	Name  string           `json:"name"`
	Repos []fleetRepoEntry `json:"repos"`
}

// loadFleetConfigForGroup reads the per-group fleet config JSON and returns
// the parsed config. It is used by moduleSlugForCWD to access Module
// declarations that are not stored in the in-memory mcp.Registry (#2180).
// Returns nil on any error (config missing, malformed) — callers treat nil as
// "no module info available".
func loadFleetConfigForGroup(s *State, groupName string) *fleetGroupConfig {
	if s == nil || s.registry == nil {
		return nil
	}
	// Walk the raw registry.json to find the config_path for this group.
	// The mcp.Registry is already a parsed view from registry.json.
	// We re-read the raw file to get the config_path field which is only
	// present in the CLI array format.
	rawData, err := os.ReadFile(s.registry.Path)
	if err != nil {
		return nil
	}
	var raw struct {
		Version int `json:"version"`
		Groups  []struct {
			Name       string `json:"name"`
			ConfigPath string `json:"config_path"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(rawData, &raw); err != nil || len(raw.Groups) == 0 {
		return nil
	}
	var configPath string
	for _, g := range raw.Groups {
		if g.Name == groupName {
			configPath = g.ConfigPath
			break
		}
	}
	if configPath == "" {
		return nil
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}
	var cfg fleetGroupConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return &cfg
}

// moduleSlugForCWD returns the registered Module sub-path that contains abs
// within the given repoPath. It walks the fleet config for the group, finds the
// repo, and performs longest-prefix matching among its Module entries. Returns
// "" when abs is not inside any declared module sub-path, or when the repo has
// no modules (M3 #2180).
//
// Example: repoPath=/repos/platform, Module="payments/svc", abs=/repos/platform/payments/svc/api
// → returns "payments/svc".
func moduleSlugForCWD(s *State, groupName, repoSlug, repoPath, abs string) string {
	cfg := loadFleetConfigForGroup(s, groupName)
	if cfg == nil {
		return ""
	}
	// Find the specific repo entry.
	var modules []string
	for _, r := range cfg.Repos {
		if r.Slug == repoSlug {
			modules = r.Modules
			break
		}
	}
	if len(modules) == 0 {
		return ""
	}
	repoClean := filepath.Clean(repoPath)
	// Find the longest module sub-path that is an ancestor of abs.
	best := ""
	for _, mod := range modules {
		modAbs := filepath.Join(repoClean, mod)
		if pathContains(modAbs, abs) && len(mod) > len(best) {
			best = mod
		}
	}
	return best
}

// ResolveCWD is the PH1c entry point that maps a cwd to a full
// (group, repo, ref) triple. It extends groupFromRegistryWithCandidates
// with worktree-sibling resolution:
//
//  1. First it tries direct path containment (today's behaviour).
//  2. If no registered repo path contains cwd, it runs gitmeta.Capture
//     to find the git toplevel. If that toplevel is itself a linked
//     worktree, it walks every registered repo and checks whether the
//     worktree's parent repo (git-common-dir) matches a registered path.
//     On a match it returns (group, repo, worktree-HEAD-ref) so MCP
//     tools query the per-ref graph for that worktree.
//
// If cwd is empty or nothing matches the function returns a zero-value
// CWDResolution with Source "none".
func ResolveCWD(s *State, cwd string) CWDResolution {
	if cwd == "" || s == nil {
		return CWDResolution{Source: "none"}
	}

	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	abs = filepath.Clean(abs)

	// Step 0 (PH3 #2091): check the ephemeral worktree-child registry first.
	// If cwd is inside a registered worktree entry, return that entry directly
	// so the caller queries the per-worktree ref graph rather than the parent's
	// default ref.
	s.mu.Lock()
	wl := s.worktreeLookup
	s.mu.Unlock()
	if wl != nil {
		// Walk up to the git toplevel for this cwd, then look up that path.
		wtMeta := gitmeta.Capture(abs)
		if wtMeta.IsWorktree && wtMeta.TopLevel != "" {
			wtTop := filepath.Clean(wtMeta.TopLevel)
			if gname, slug, branch := wl.LookupPath(wtTop); gname != "" {
				return CWDResolution{
					Group:      gname,
					RepoSlug:   slug,
					Ref:        branch,
					SHA:        wtMeta.SHA,
					IsWorktree: true,
					Source:     "worktree_registry",
				}
			}
		}
	}

	// Step 1: direct registry containment (existing behaviour).
	group, _ := groupFromRegistryWithCandidates(s, abs)
	if group != "" {
		// Determine which repo slug contains cwd (longest-prefix match).
		slug, repoPath := repoSlugForCWD(s, group, abs)
		meta := gitmeta.Capture(repoPath)
		// M3 (#2180): if the matched repo has declared modules, determine which
		// module sub-path cwd is inside.
		modSlug := moduleSlugForCWD(s, group, slug, repoPath, abs)
		return CWDResolution{
			Group:      group,
			RepoSlug:   slug,
			ModuleSlug: modSlug,
			Ref:        meta.Ref,
			SHA:        meta.SHA,
			IsWorktree: meta.IsWorktree,
			Source:     "cwd_registry",
		}
	}

	// Step 2: worktree-sibling resolution.
	// Ask git for the toplevel of whatever repo cwd is inside.
	meta := gitmeta.Capture(abs)
	if meta.TopLevel == "" || !meta.IsWorktree {
		// Not inside a git repo at all, or not a linked worktree.
		return CWDResolution{Source: "none"}
	}

	wtTopLevel := filepath.Clean(meta.TopLevel)

	// Find a registered repo whose worktrees include wtTopLevel.
	// We do this by looking up each registered repo's git-common-dir and
	// comparing it against the worktree's git-common-dir (they are equal
	// when the worktree belongs to that repo).
	wtCommonDir := gitCommonDir(wtTopLevel)
	if wtCommonDir == "" {
		return CWDResolution{Source: "none"}
	}

	for gname, gentry := range s.registry.Groups {
		for rname, rentry := range gentry.Repos {
			if rentry.Path == "" {
				continue
			}
			parentCommon := gitCommonDir(rentry.Path)
			if parentCommon == "" {
				continue
			}
			if filepath.Clean(parentCommon) != filepath.Clean(wtCommonDir) {
				continue
			}
			// Match: wtTopLevel is a worktree of rentry.Path.
			return CWDResolution{
				Group:          gname,
				RepoSlug:       rname,
				Ref:            meta.Ref,
				SHA:            meta.SHA,
				IsWorktree:     true,
				ParentRepoPath: rentry.Path,
				Source:         "worktree",
			}
		}
	}

	return CWDResolution{Source: "none"}
}

// repoSlugForCWD returns the slug and path of the repo in group whose path
// is the longest prefix of abs. Returns ("", "") when no match.
func repoSlugForCWD(s *State, group, abs string) (slug, repoPath string) {
	gentry, ok := s.registry.Groups[group]
	if !ok {
		return "", ""
	}
	best := ""
	for rname, rentry := range gentry.Repos {
		if rentry.Path == "" {
			continue
		}
		rp := filepath.Clean(rentry.Path)
		if !pathContains(rp, abs) {
			continue
		}
		if len(rp) > len(best) {
			best = rp
			slug = rname
			repoPath = rentry.Path
		}
	}
	return slug, repoPath
}

// gitCommonDir runs `git rev-parse --git-common-dir` in dir and returns the
// absolute, cleaned, symlink-resolved result. Returns "" on any error
// (non-git dir, timeout, …).
// The returned path is made absolute relative to dir when git outputs a
// relative path (which it does for the main checkout: ".git").
// Symlinks are resolved so that /var/folders/… and /private/var/folders/…
// (macOS) compare equal.
func gitCommonDir(dir string) string {
	out := gitmeta.RunGit(dir, "rev-parse", "--git-common-dir")
	if out == "" {
		return ""
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(dir, out)
	}
	out = filepath.Clean(out)
	if resolved, err := filepath.EvalSymlinks(out); err == nil {
		out = resolved
	}
	return out
}
