// handlers_v2_refs.go — GET /api/v2/groups/:group/refs
//
// PH1c of epic #2087 (#2089): exposes the per-ref store layout introduced
// by PH1a as a machine-readable endpoint so the WebUI v2 can display which
// branches / tags have indexed graphs and let users switch between them.
package dashboard

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/registry"
)

// v2RefEntry describes one indexed ref for a repository.
type v2RefEntry struct {
	// Ref is the decoded git branch/tag name (e.g. "main", "feat/x").
	// Empty string means the sentinel "_unknown" ref (detached HEAD or
	// graphs indexed before PH1a).
	Ref string `json:"ref"`
	// RefSafe is the filesystem-safe encoding used in the store path.
	RefSafe string `json:"ref_safe"`
	// Tier is "hot" when the ref is the currently-loaded ref for this repo
	// (i.e. it matches the graph loaded in memory), otherwise "cold".
	Tier string `json:"tier"`
	// IndexedAt is the mtime of graph.fb for this ref. Zero when unknown.
	IndexedAt *time.Time `json:"indexed_at,omitempty"`
	// Entities is the entity count from the graph-stats.json sidecar.
	// Zero when the sidecar is absent or unreadable.
	Entities int `json:"entities,omitempty"`
}

// v2RepoRefs bundles a repo's slug and its available refs.
type v2RepoRefs struct {
	Slug string       `json:"slug"`
	Refs []v2RefEntry `json:"refs"`
}

// handleV2GroupRefs handles GET /api/v2/groups/{group}/refs.
//
// Response shape:
//
//	{
//	  "ok": true,
//	  "data": {
//	    "group": "my-group",
//	    "repos": [
//	      {
//	        "slug": "my-service",
//	        "refs": [
//	          { "ref": "main",      "ref_safe": "main",       "tier": "hot",  "indexed_at": "...", "entities": 6451 },
//	          { "ref": "feat/foo",  "ref_safe": "feat%2Ffoo", "tier": "cold", "indexed_at": "...", "entities": 6392 }
//	        ]
//	      }
//	    ]
//	  }
//	}
func (s *Server) handleV2GroupRefs(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}

	groups, err := registry.Groups()
	if err != nil {
		writeV2Err(w, http.StatusInternalServerError, "registry_error", err.Error())
		return
	}
	var cfgPath string
	for _, g := range groups {
		if g.Name == group {
			cfgPath = g.ConfigPath
			break
		}
	}
	if cfgPath == "" {
		writeV2Err(w, http.StatusNotFound, "not_found", "group not registered: "+group)
		return
	}
	cfg, err := registry.LoadGroupConfig(cfgPath)
	if err != nil {
		writeV2Err(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}

	// Determine the currently-active ref for each repo (the hot ref)
	// by reading the head of its current StateDirForRepo path.
	// We compare it against each discovered ref to set the tier.
	hotRefs := map[string]string{} // slug → current ref
	for _, r := range cfg.Repos {
		hotDir := daemon.StateDirForRepo(r.Path)
		// Extract the ref from the path: the second-to-last component is the
		// ref-safe name inside refs/<ref-safe>/.
		if base := filepath.Base(hotDir); base != "" {
			hotRefs[r.Slug] = base
		}
	}

	repos := make([]v2RepoRefs, 0, len(cfg.Repos))
	for _, r := range cfg.Repos {
		repoBase := daemon.StateDirForRepoRef(r.Path, "") // uses _unknown; we just need the parent
		// The refs/ dir lives one level above _unknown.
		// StateDirForRepoRef returns <base>/refs/_unknown, so parent is <base>/refs.
		refsDir := filepath.Dir(repoBase)
		entries, err := os.ReadDir(refsDir)
		if err != nil {
			// Store dir may not exist yet (repo never indexed). Return empty refs.
			repos = append(repos, v2RepoRefs{Slug: r.Slug, Refs: []v2RefEntry{}})
			continue
		}

		hotRefSafe := hotRefs[r.Slug]
		refs := make([]v2RefEntry, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			refSafe := e.Name()
			ref := daemon.RefSafeDecode(refSafe)

			tier := "cold"
			if refSafe == hotRefSafe {
				tier = "hot"
			}

			// Check for a graph.fb in this ref slot.
			fbPath := filepath.Join(refsDir, refSafe, "graph.fb")
			var indexedAt *time.Time
			var entityCount int
			if fi, ferr := os.Stat(fbPath); ferr == nil {
				t := fi.ModTime()
				indexedAt = &t
				// Try to read entity count from graph-stats.json sidecar.
				statsPath := filepath.Join(refsDir, refSafe, "graph-stats.json")
				if data, serr := os.ReadFile(statsPath); serr == nil {
					var stats struct {
						TotalEntities int `json:"total_entities"`
					}
					if json.Unmarshal(data, &stats) == nil {
						entityCount = stats.TotalEntities
					}
				}
			} else {
				// No graph.fb — slot may exist but graph was never written.
				continue
			}

			refs = append(refs, v2RefEntry{
				Ref:       ref,
				RefSafe:   refSafe,
				Tier:      tier,
				IndexedAt: indexedAt,
				Entities:  entityCount,
			})
		}

		// Sort: hot first, then by ref name alphabetically.
		sort.Slice(refs, func(i, j int) bool {
			if refs[i].Tier != refs[j].Tier {
				return refs[i].Tier == "hot"
			}
			return refs[i].Ref < refs[j].Ref
		})

		repos = append(repos, v2RepoRefs{Slug: r.Slug, Refs: refs})
	}

	// Sort repos by slug for deterministic output.
	sort.Slice(repos, func(i, j int) bool { return repos[i].Slug < repos[j].Slug })

	writeV2JSON(w, http.StatusOK, v2OK(map[string]any{
		"group": group,
		"repos": repos,
	}))
}

