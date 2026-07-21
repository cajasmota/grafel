// ref_helper.go — shared ?ref= query parameter resolution for dashboard handlers.
//
// All 6 endpoints added by issue #2220 share the same resolution semantics:
//
//   - Missing ?ref= or ?ref=@current → use current HEAD (empty string, same as
//     pre-#2220 behaviour).
//   - ?ref=<name> → load that specific ref's graph. Returns HTTP 400 with an
//     "available" list if the ref has no indexed graph for the requested repo.
//   - ?ref=@all → aggregate across all indexed refs for the repo/group.
//
// resolveRefParam extracts the raw query value and returns (ref, isAll) or
// writes an error response itself and returns (_, true-as-sentinel) — callers
// check ok==false to bail out.
package dashboard

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

// resolveRefParam parses the ?ref= query parameter.
//
// Returns (resolvedRef, isAll, ok):
//   - ok=false means the handler already wrote an error response; caller must return.
//   - resolvedRef="" means "use current HEAD" (default, backward compatible).
//   - isAll=true means ?ref=@all was requested.
func resolveRefParam(w http.ResponseWriter, r *http.Request, groupName string) (ref string, isAll bool, ok bool) {
	raw := r.URL.Query().Get("ref")
	switch raw {
	case "", "@current":
		return "", false, true
	case "@all":
		return "", true, true
	}

	// Named ref — validate it exists for at least one repo in this group.
	known := knownRefsForGroup(groupName)
	for _, k := range known {
		if k == raw {
			return raw, false, true
		}
	}

	// Not found.
	writeJSON(w, http.StatusBadRequest, map[string]any{
		"error":     "invalid ref",
		"available": known,
	})
	return "", false, false
}

// knownRefsForGroup returns all ref names that have an indexed graph for any
// repo in the given group. The list is sorted and deduplicated. Returns nil
// when the store is empty or the group is not registered.
func knownRefsForGroup(groupName string) []string {
	groups, err := registry.Groups()
	if err != nil {
		return nil
	}
	var cfgPath string
	for _, g := range groups {
		if g.Name == groupName {
			cfgPath = g.ConfigPath
			break
		}
	}
	if cfgPath == "" {
		return nil
	}
	cfg, err := registry.LoadGroupConfig(cfgPath)
	if err != nil {
		return nil
	}

	seen := make(map[string]struct{})
	for _, repo := range cfg.Repos {
		// Get the refs/ directory for this repo.
		// StateDirForRepoRef(<path>, "_unknown") → <base>/refs/_unknown
		// so filepath.Dir gives <base>/refs.
		sentinel := daemon.StateDirForRepoRef(repo.Path, "")
		refsDir := filepath.Dir(sentinel)
		entries, err := os.ReadDir(refsDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			ref := daemon.RefSafeDecode(e.Name())
			if ref == "" {
				continue // skip _unknown sentinel
			}
			// Only count refs that have an actual graph. #5891: resolve the
			// active generation (graph.<gen>.fb via the `current` pointer); a
			// fresh repo has no flat graph.fb, so the hardcoded stat dropped the
			// ref → ?ref=<branch> 400s. Mirror allRefsForRepo; keep the graph.json
			// fallback so a json-only ref dir is still included (CurrentGraphPath
			// returns the non-existent flat graph.fb path for a json-only dir).
			if _, ferr := os.Stat(graph.CurrentGraphPath(filepath.Join(refsDir, e.Name()))); ferr != nil {
				if _, ferr2 := os.Stat(filepath.Join(refsDir, e.Name(), "graph.json")); ferr2 != nil {
					continue
				}
			}
			seen[ref] = struct{}{}
		}
	}

	if len(seen) == 0 {
		return nil
	}
	names := make([]string, 0, len(seen))
	for k := range seen {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// allRefsForRepo returns all ref names with an indexed graph for a single repo path.
func allRefsForRepo(repoPath string) []string {
	sentinel := daemon.StateDirForRepoRef(repoPath, "")
	refsDir := filepath.Dir(sentinel)
	entries, err := os.ReadDir(refsDir)
	if err != nil {
		return nil
	}
	var refs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ref := daemon.RefSafeDecode(e.Name())
		if ref == "" {
			continue
		}
		if _, ferr := os.Stat(graph.CurrentGraphPath(filepath.Join(refsDir, e.Name()))); ferr != nil { // #5891
			if _, ferr2 := os.Stat(filepath.Join(refsDir, e.Name(), "graph.json")); ferr2 != nil {
				continue
			}
		}
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}
