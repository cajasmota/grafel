// Package deplinker implements the dependency-linker analysis pass.
//
// It operates on a complete entity+relationship snapshot (e.g. a loaded
// graph.Document) and annotates each external_dependency entity with one
// of three usage statuses:
//
//   - "used"    — the package name appears in at least one IMPORTS edge target
//   - "unused"  — declared in the manifest but never imported (dead dependency)
//   - "phantom" — imported in source but not declared in any manifest
//
// The package is intentionally a pure analysis library (no extractor
// registration, no HTTP coupling) so it can be called from the dashboard
// handler, CLI tooling, and tests without pulling in the full indexer.
package deplinker

import (
	"fmt"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
)

// DependencyStatus captures the usage classification for one declared dep.
type DependencyStatus string

const (
	StatusUsed    DependencyStatus = "used"
	StatusUnused  DependencyStatus = "unused"
	StatusPhantom DependencyStatus = "phantom"
)

// PackageEntry describes one declared or phantom external dependency.
type PackageEntry struct {
	// Name is the package name as declared in the manifest.
	Name string `json:"name"`
	// PackageManager is e.g. "npm", "go_modules", "cargo", "pip", "bundler".
	PackageManager string `json:"package_manager"`
	// Version is the declared version string (empty for phantom deps).
	Version string `json:"version,omitempty"`
	// DependencyKind is "runtime", "dev", or "peer".
	DependencyKind string `json:"dependency_kind"`
	// Status is one of "used", "unused", or "phantom".
	Status DependencyStatus `json:"status"`
	// SourceFile is the manifest file that declared this dependency.
	SourceFile string `json:"source_file,omitempty"`
	// Importers lists the source files that import this package.
	Importers []string `json:"importers,omitempty"`
}

// Report is the full dependency analysis result for one repo.
type Report struct {
	// PackageManager is the primary detected package manager.
	PackageManager string `json:"package_manager"`
	// Declared is the total number of declared dependencies.
	Declared int `json:"declared"`
	// Used is the count of declared deps that are also imported.
	Used int `json:"used"`
	// Unused is the count of declared deps with no matching imports.
	Unused int `json:"unused"`
	// Phantom is the count of packages imported but not declared.
	Phantom int `json:"phantom"`
	// Packages is the full per-package breakdown.
	Packages []PackageEntry `json:"packages"`
}

// Analyze scans a loaded graph.Document and returns the dependency report.
//
// It reads:
//   - All entities of Kind=="SCOPE.Component" / Subtype=="external_dependency"
//     (emitted by the manifest extractor) as declared deps.
//   - All relationships of Kind=="DEPENDS_ON" / Properties["kind"]=="import"
//     (emitted by the imports cross extractor) as import edges.
//
// From those two sets it computes used, unused, and phantom deps.
func Analyze(doc *graph.Document) Report {
	if doc == nil {
		return Report{}
	}
	return analyzeEntities(doc.Entities, doc.Relationships)
}

// analyzeEntities is the pure core: testable without a real graph.Document.
func analyzeEntities(entities []graph.Entity, rels []graph.Relationship) Report {
	// ---- Step 1: collect declared deps ----------------------------------
	type declared struct {
		pkg        string
		pm         string
		version    string
		kind       string
		sourceFile string
	}
	// Key: "<pm>:<pkg>" → entry
	declaredMap := map[string]*declared{}
	primaryPM := ""

	for i := range entities {
		e := &entities[i]
		if e.Kind != "SCOPE.Component" || e.Subtype != "external_dependency" {
			continue
		}
		pm := e.PropGet("package_manager")
		if pm == "" {
			continue
		}
		if primaryPM == "" {
			primaryPM = pm
		}
		key := pm + ":" + e.Name
		declaredMap[key] = &declared{
			pkg:        e.Name,
			pm:         pm,
			version:    e.PropGet("version"),
			kind:       e.PropGet("dependency_kind"),
			sourceFile: e.SourceFile,
		}
	}

	// ---- Step 2: collect IMPORTS edges ----------------------------------
	// importers[key] is the list of files that import "<pm>:<pkg>".
	importers := map[string][]string{} // key → []sourceFile

	for i := range rels {
		r := &rels[i]
		if r.Kind != "DEPENDS_ON" {
			continue
		}
		if r.PropGet("kind") != "import" {
			continue
		}
		// ToID is either:
		//   scope:component:import:external:<pkg>  (from cross/imports)
		//   <bare import path>
		var importedName string
		if after, ok := strings.CutPrefix(r.ToID, "scope:component:import:external:"); ok {
			importedName = after
		} else if after2, ok2 := strings.CutPrefix(r.ToID, "scope:component:external_dep:"); ok2 {
			// already in structured form: "<pm>:<pkg>"
			importedName = after2
		} else {
			importedName = r.ToID
		}

		// Match against declared packages (exact and prefix for Go modules).
		for key, d := range declaredMap {
			pkg := d.pkg
			if importedName == pkg || strings.HasPrefix(importedName, pkg+"/") ||
				strings.HasSuffix(key, ":"+importedName) {
				importers[key] = appendUnique(importers[key], r.FromID)
			}
		}
	}

	// ---- Step 3: classify declared deps ---------------------------------
	var packages []PackageEntry
	used := 0
	unused := 0

	for key, d := range declaredMap {
		imps := importers[key]
		status := StatusUnused
		if len(imps) > 0 {
			status = StatusUsed
			used++
		} else {
			unused++
		}
		depKind := d.kind
		if depKind == "" {
			depKind = "runtime"
		}
		packages = append(packages, PackageEntry{
			Name:           d.pkg,
			PackageManager: d.pm,
			Version:        d.version,
			DependencyKind: depKind,
			Status:         status,
			SourceFile:     d.sourceFile,
			Importers:      imps,
		})
	}

	// ---- Step 4: detect phantom imports ---------------------------------
	// A phantom is an import whose ToID resolves to an external package
	// that does NOT appear in declaredMap.
	phantom := 0
	seenPhantom := map[string]bool{}

	for i := range rels {
		r := &rels[i]
		if r.Kind != "DEPENDS_ON" || r.PropGet("kind") != "import" {
			continue
		}
		after, ok := strings.CutPrefix(r.ToID, "scope:component:import:external:")
		if !ok {
			continue
		}
		importedName := after
		// Check whether this matches any declared package.
		matched := false
		for _, d := range declaredMap {
			if importedName == d.pkg || strings.HasPrefix(importedName, d.pkg+"/") {
				matched = true
				break
			}
		}
		if matched || seenPhantom[importedName] {
			continue
		}
		// Phantom: imported but not declared. We don't know the PM here,
		// so we omit it (empty string signals unknown).
		seenPhantom[importedName] = true
		phantom++
		packages = append(packages, PackageEntry{
			Name:           importedName,
			PackageManager: "",
			DependencyKind: "runtime",
			Status:         StatusPhantom,
		})
	}

	return Report{
		PackageManager: primaryPM,
		Declared:       len(declaredMap),
		Used:           used,
		Unused:         unused,
		Phantom:        phantom,
		Packages:       packages,
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// appendUnique appends s to slice only when it is not already present.
func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

// GroupReport is the dependency analysis result for a whole group (all repos).
type GroupReport struct {
	// Group is the group name.
	Group string `json:"group"`
	// ByRepo is per-repo breakdown, keyed by repo slug.
	ByRepo map[string]Report `json:"by_repo"`
	// Summary rolls up declared/used/unused/phantom across all repos.
	Summary GroupSummary `json:"summary"`
}

// GroupSummary is the group-level roll-up.
type GroupSummary struct {
	Declared int `json:"declared"`
	Used     int `json:"used"`
	Unused   int `json:"unused"`
	Phantom  int `json:"phantom"`
}

// AnalyzeGroup runs Analyze across all repos in a DashGroup and returns
// an aggregated GroupReport.  dashGroupRepos accepts any slice of (slug,
// doc) pairs so the caller can pass dashboard.DashGroup data without a
// direct import cycle.
func AnalyzeGroup(group string, repos []RepoDoc) GroupReport {
	byRepo := map[string]Report{}
	var total GroupSummary
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		rep := Analyze(r.Doc)
		byRepo[r.Slug] = rep
		total.Declared += rep.Declared
		total.Used += rep.Used
		total.Unused += rep.Unused
		total.Phantom += rep.Phantom
	}
	return GroupReport{
		Group:   group,
		ByRepo:  byRepo,
		Summary: total,
	}
}

// RepoDoc is the minimal (slug, graph.Document) pair that AnalyzeGroup
// accepts. It avoids a direct import of internal/dashboard.
type RepoDoc struct {
	Slug string
	Doc  *graph.Document
}

// pkgKey returns the canonical map key for a declared package.
func pkgKey(pm, pkg string) string {
	return fmt.Sprintf("%s:%s", pm, pkg)
}

var _ = pkgKey // keep exported for potential callers
