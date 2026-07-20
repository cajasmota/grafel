// Package engine — dependency-hygiene annotation pass (#3640, epic #3625).
//
// This pass promotes the deplinker dependency analysis from a dashboard-only
// query into a PERSISTED graph signal. Previously deplinker.AnalyzeGroup was
// called solely by the dashboard handler (internal/dashboard/handlers_
// dependencies.go); its used/unused/phantom classification never reached the
// graph, so `find`/`neighbors`/agents could not see dependency hygiene.
//
// ApplyDependencyHygiene runs the SAME deplinker.Analyze logic over the
// assembled graph.Document and writes the classification back onto the graph:
//
//   - Each external_dependency entity (emitted by the manifest extractor) gets
//     a usage_status=used|unused property.
//   - The entity's embedded DEPENDS_ON(kind=external_dependency) edge — when
//     present — gets the same usage_status property, so edge-oriented queries
//     (neighbors) surface hygiene without a second entity lookup.
//   - Phantom imports (imported but not declared in any manifest) are surfaced
//     as a doc-level stat. We deliberately do NOT synthesise new entities for
//     phantoms here: the import edges already exist in the graph and inventing
//     dependency entities for them would double-count against the manifest
//     extractor's declared-dep inventory. Phantom visibility remains the
//     dashboard's job; the persisted layer annotates declared deps only.
//
// The dashboard path is unchanged and keeps calling AnalyzeGroup — this pass
// adds the persisted layer without removing the on-demand one (no double
// emit: this mutates existing entities in place, it does not append).
//
// Honest-partial: when manifest data or import edges are incomplete the
// classification is only as good as deplinker's input. A declared dep with no
// observed import edge is marked "unused" exactly as the dashboard reports it;
// callers reading usage_status should treat it with the same confidence as the
// dashboard's dependency panel.
package engine

import (
	"github.com/cajasmota/grafel/internal/extractors/cross/deplinker"
	"github.com/cajasmota/grafel/internal/graph"
)

// DependencyHygieneStats summarises one ApplyDependencyHygiene run.
type DependencyHygieneStats struct {
	// Declared is the number of external_dependency entities considered.
	Declared int
	// Used is the count annotated usage_status=used.
	Used int
	// Unused is the count annotated usage_status=unused.
	Unused int
	// Phantom is the count of imported-but-undeclared packages deplinker
	// detected. Not persisted as entities (see package doc); reported for
	// parity with the dashboard summary.
	Phantom int
	// EntitiesAnnotated is the number of entities that received a
	// usage_status property (== Used + Unused for matched declared deps).
	EntitiesAnnotated int
	// EdgesAnnotated is the number of embedded DEPENDS_ON edges that
	// received a usage_status property.
	EdgesAnnotated int
}

// usageStatusProp is the entity/edge property key carrying the deplinker
// classification. Kept in one place so queries and tests agree on the name.
const usageStatusProp = "usage_status"

// ApplyDependencyHygiene classifies every declared external dependency in doc
// as used or unused (via deplinker.Analyze) and writes usage_status onto the
// dependency entities and their DEPENDS_ON edges in place.
//
// Reuses deplinker.Analyze — the classification logic is NOT reimplemented
// here. Safe to call on a doc with no manifest data (returns zero stats).
func ApplyDependencyHygiene(doc *graph.Document) DependencyHygieneStats {
	stats := DependencyHygieneStats{}
	if doc == nil {
		return stats
	}

	// Reuse the dashboard's analysis logic verbatim.
	report := deplinker.Analyze(doc)
	stats.Declared = report.Declared
	stats.Used = report.Used
	stats.Unused = report.Unused
	stats.Phantom = report.Phantom

	if report.Declared == 0 {
		return stats
	}

	// Index the classification by the canonical "<pm>:<name>" key so we can
	// match it back to entities. deplinker keys declared deps the same way
	// (package_manager + name), so this round-trips exactly.
	statusByKey := make(map[string]deplinker.DependencyStatus, len(report.Packages))
	for _, p := range report.Packages {
		// Phantom packages have no package_manager and no declared entity to
		// annotate; skip them (their import edges already live in the graph).
		if p.Status == deplinker.StatusPhantom {
			continue
		}
		statusByKey[depKey(p.PackageManager, p.Name)] = p.Status
	}

	// Walk entities once, annotating external_dependency entities (and their
	// embedded DEPENDS_ON edges) with the matched status.
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind != "SCOPE.Component" || e.Subtype != "external_dependency" {
			continue
		}
		if e.PropLen() == 0 {
			continue
		}
		pm := e.PropGet("package_manager")
		if pm == "" {
			continue
		}
		status, ok := statusByKey[depKey(pm, e.Name)]
		if !ok {
			continue
		}
		e.PropSet(usageStatusProp, string(status))
		stats.EntitiesAnnotated++
	}

	// Annotate the standalone DEPENDS_ON(kind=external_dependency) edges that
	// the manifest extractor emits. These live in doc.Relationships after the
	// document is assembled. We match on the edge's ToID, which the manifest
	// extractor builds as "scope:component:external_dep:<pm>:<name>".
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.Kind != "DEPENDS_ON" {
			continue
		}
		if r.PropLen() == 0 || r.PropGet("kind") != "external_dependency" {
			continue
		}
		pm := r.PropGet("package_manager")
		name := depNameFromExternalRef(r.ToID)
		if pm == "" || name == "" {
			continue
		}
		status, ok := statusByKey[depKey(pm, name)]
		if !ok {
			continue
		}
		r.PropSet(usageStatusProp, string(status))
		stats.EdgesAnnotated++
	}

	return stats
}

// depKey builds the canonical "<pm>:<name>" lookup key, matching the keying
// deplinker uses internally for declared dependencies.
func depKey(pm, name string) string {
	return pm + ":" + name
}

// externalDepRefPrefix is the ToID prefix the manifest extractor uses for the
// DEPENDS_ON edge target: "scope:component:external_dep:<pm>:<name>".
const externalDepRefPrefix = "scope:component:external_dep:"

// depNameFromExternalRef extracts the bare package name from a manifest
// dependency ref of the form "scope:component:external_dep:<pm>:<name>".
// Returns "" when ref is not in that form. The package name may itself
// contain ':' (e.g. Maven "group:artifact"), so we strip only the prefix and
// the leading "<pm>:" segment.
func depNameFromExternalRef(ref string) string {
	rest, ok := cutPrefix(ref, externalDepRefPrefix)
	if !ok {
		return ""
	}
	// rest == "<pm>:<name>"; drop the first segment (the package manager).
	for i := 0; i < len(rest); i++ {
		if rest[i] == ':' {
			return rest[i+1:]
		}
	}
	return ""
}

// cutPrefix is strings.CutPrefix, inlined to avoid an import churn diff.
func cutPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return "", false
}
