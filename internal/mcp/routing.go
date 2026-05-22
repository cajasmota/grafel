package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// resolveGroup implements the ADR-0008 cascade:
//
//  1. explicit `group` argument
//  2. CWD inference (walk up looking for .archigraph/group.json)
//  3. singleton-group fallback
//
// Returns the chosen group name, the source ("explicit"/"cwd"/"singleton"),
// or an error listing the registered groups when ambiguous.
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
	// Registry-based cwd inference (#1650): when no group.json marker is found,
	// walk the registry and pick the group whose repo path is a prefix of cwd.
	// If exactly one group matches we honor it as "cwd_registry"; multiple
	// matches fall through to the ambiguous-group error.
	if g := groupFromRegistry(s, cwd); g != "" {
		return g, "cwd_registry", nil
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
// covers cwd, or multiple groups cover it (ambiguous). The match prefers the
// longest path (most specific repo) when several repos under the SAME group
// could cover cwd; when different groups each cover cwd, "" is returned and
// the caller surfaces the standard ambiguous-group error.
func groupFromRegistry(s *State, cwd string) string {
	if cwd == "" || s == nil || s.registry == nil {
		return ""
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
		return ""
	}
	// All hits same group → unambiguous; pick longest path (most specific).
	first := hits[0].group
	for _, h := range hits[1:] {
		if h.group != first {
			return "" // ambiguous across groups
		}
	}
	return first
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
