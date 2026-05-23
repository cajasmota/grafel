package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
