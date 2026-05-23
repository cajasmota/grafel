package dashboard

// handlers_dependencies.go — GET /api/dependencies/{group}
//
// Returns the dependency tree for a group: for every repo in the group,
// reads the on-disk graph.fb / graph.json and runs the deplinker analysis
// to classify each declared dependency as "used", "unused", or "phantom".
//
// Route registered in server.go:
//
//	GET /api/dependencies/{group}
//
// Query parameters:
//
//	status=used|unused|phantom  — filter to one status (default: all)
//	pm=npm|go_modules|cargo|... — filter by package manager (default: all)
//	kind=runtime|dev|peer       — filter by dependency_kind (default: all)

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractors/cross/deplinker"
)

// ---------------------------------------------------------------------------
// Wire shapes
// ---------------------------------------------------------------------------

// DependenciesReply is the wire shape for GET /api/dependencies/{group}.
type DependenciesReply struct {
	Group   string                    `json:"group"`
	Summary deplinker.GroupSummary    `json:"summary"`
	ByRepo  map[string]RepoDepSummary `json:"by_repo"`
}

// RepoDepSummary is the per-repo dependency breakdown.
type RepoDepSummary struct {
	PackageManager string                   `json:"package_manager"`
	Declared       int                      `json:"declared"`
	Used           int                      `json:"used"`
	Unused         int                      `json:"unused"`
	Phantom        int                      `json:"phantom"`
	Packages       []deplinker.PackageEntry `json:"packages"`
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// handleDependencies handles GET /api/dependencies/{group}.
func (s *Server) handleDependencies(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	// Load the group's graph data from the in-memory cache.
	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q: %v", group, err))
		return
	}

	// Build the repo slice for the deplinker.
	repos := sortedRepos(grp)
	repoInputs := make([]deplinker.RepoDoc, 0, len(repos))
	for _, r := range repos {
		repoInputs = append(repoInputs, deplinker.RepoDoc{
			Slug: r.Slug,
			Doc:  r.Doc,
		})
	}

	// Run the deplinker analysis.
	report := deplinker.AnalyzeGroup(group, repoInputs)

	// Optional query filters.
	filterStatus := r.URL.Query().Get("status")
	filterPM := r.URL.Query().Get("pm")
	filterKind := r.URL.Query().Get("kind")

	// Build per-repo summary with optional filtering.
	byRepo := map[string]RepoDepSummary{}
	for slug, rep := range report.ByRepo {
		pkgs := filterPackages(rep.Packages, filterStatus, filterPM, filterKind)
		sort.Slice(pkgs, func(i, j int) bool {
			if pkgs[i].Status != pkgs[j].Status {
				return statusOrder(pkgs[i].Status) < statusOrder(pkgs[j].Status)
			}
			return pkgs[i].Name < pkgs[j].Name
		})
		byRepo[slug] = RepoDepSummary{
			PackageManager: rep.PackageManager,
			Declared:       rep.Declared,
			Used:           rep.Used,
			Unused:         rep.Unused,
			Phantom:        rep.Phantom,
			Packages:       pkgs,
		}
	}

	reply := DependenciesReply{
		Group:   group,
		Summary: report.Summary,
		ByRepo:  byRepo,
	}
	writeJSON(w, http.StatusOK, reply)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// filterPackages applies optional status/pm/kind filters to a package slice.
// When a filter is empty or "*" all packages pass.
func filterPackages(pkgs []deplinker.PackageEntry, status, pm, kind string) []deplinker.PackageEntry {
	if status == "" && pm == "" && kind == "" {
		out := make([]deplinker.PackageEntry, len(pkgs))
		copy(out, pkgs)
		return out
	}
	var out []deplinker.PackageEntry
	for _, p := range pkgs {
		if status != "" && !strings.EqualFold(string(p.Status), status) {
			continue
		}
		if pm != "" && !strings.EqualFold(p.PackageManager, pm) {
			continue
		}
		if kind != "" && !strings.EqualFold(p.DependencyKind, kind) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// statusOrder returns a sort key so that "unused" and "phantom" sort before "used".
func statusOrder(s deplinker.DependencyStatus) int {
	switch s {
	case deplinker.StatusPhantom:
		return 0
	case deplinker.StatusUnused:
		return 1
	default:
		return 2
	}
}
