// manifest_xref.go — package.json declared-dependency cross-reference pass
// (#5526, milestone 0.1.5).
//
// The _cross_manifest extractor (internal/extractors/cross/manifest) emits one
// SCOPE.Component(subtype=external_dependency) record per declared package in a
// package.json (all four sections: prod/dev/peer/optional) AND, separately, one
// per resolved package in the sibling lockfile (package-lock.json / yarn.lock /
// pnpm-lock.yaml). Those two record sets are produced by independent per-file
// Extract calls, so on their own a declared dep carries only its manifest
// version RANGE and there is no used-vs-unused signal.
//
// This whole-graph pass — run AFTER external.Synthesize so the import-derived
// `ext:<pkg>` External nodes already exist — joins the three views per repo:
//
//	declared (package.json)  +  resolved (lockfile)  +  imported (import graph)
//
// and stamps each declared npm dependency with:
//
//	version_range              — the manifest range (e.g. "^4.17.21")
//	version                    — the lockfile-resolved exact version when
//	                             available, else the range (already set by the
//	                             extractor; only overwritten when the lockfile
//	                             has a better value)
//	imported                   — "true" if some indexed JS/TS file imports it
//	dead_dependency_candidate  — "true" when declared but NOT imported
//
// The matching import-derived `ext:<pkg>` External node is also enriched with
// the resolved `version` (+ dep_section / declared) so a version surfaces on the
// node an agent actually navigates to.
//
// Scope: JS/TS (package.json + npm/yarn/pnpm lockfiles) only. Other ecosystems
// (requirements.txt/pyproject, go.mod, Cargo.toml, Gemfile, pom.xml) are
// follow-ups (same join shape, different name-normalisation).
package external

import (
	"path"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// jsPackageManagers is the set of package_manager values whose declared
// dependencies are reconciled against the JS/TS import graph.
var jsPackageManagers = map[string]bool{
	"npm":  true,
	"yarn": true,
	"pnpm": true,
}

// ManifestXrefStats summarises a CrossReferenceManifests run.
type ManifestXrefStats struct {
	// DeclaredReconciled is the number of declared package.json deps that were
	// stamped with used/unused + resolved-version attributes.
	DeclaredReconciled int
	// DeadCandidates is how many of those were flagged
	// dead_dependency_candidate=true (declared but never imported).
	DeadCandidates int
	// VersionsResolved counts declared deps whose `version` was upgraded from a
	// manifest range to a lockfile-resolved exact version.
	VersionsResolved int
	// ExternalNodesEnriched counts import-derived ext:<pkg> External nodes that
	// received a version (and dep_section/declared) from a declared dep.
	ExternalNodesEnriched int
}

// CrossReferenceManifests performs the package.json declared-dependency
// cross-reference described in the package doc. Idempotent and safe on a
// nil/empty document; a no-op when no npm manifests are present.
//
// MUST run AFTER external.Synthesize (the ext:<pkg> nodes must already exist).
func CrossReferenceManifests(doc *graph.Document) ManifestXrefStats {
	var stats ManifestXrefStats
	if doc == nil || len(doc.Entities) == 0 {
		return stats
	}

	// 1. Set of imported npm package ROOTS. Two complementary sources:
	//    (a) every synthesised External node ID `ext:<root>` (JS imports
	//        resolve to ext:react, ext:@scope/name, ext:react-router-dom),
	//    (b) every IMPORTS edge's source_module, normalised to its package
	//        root — covers subpath imports (lodash/fp → lodash) and any import
	//        whose External node was folded elsewhere.
	imported := make(map[string]bool, 64)
	for k := range doc.Entities {
		e := &doc.Entities[k]
		if e.Kind != KindExternal {
			continue
		}
		if root := npmRootFromExtID(e.ID); root != "" {
			imported[root] = true
		}
	}
	for k := range doc.Relationships {
		rel := &doc.Relationships[k]
		if rel.Kind != string(types.RelationshipKindImports) {
			continue
		}
		spec := rel.PropGet("source_module")
		if root := npmPackageRoot(spec); root != "" {
			imported[root] = true
		}
	}

	// 2. Lockfile-resolved versions, indexed by (manifest dir, package name).
	//    A lockfile-derived record is dependency_kind=locked; its SourceFile is
	//    the lockfile path, so its directory identifies the repo/workspace the
	//    resolution belongs to. We also keep a dir-agnostic fallback for the
	//    common single-lockfile repo.
	resolvedByDirName := make(map[string]string)
	resolvedByName := make(map[string]string)
	for k := range doc.Entities {
		e := &doc.Entities[k]
		if e.Subtype != "external_dependency" {
			continue
		}
		if e.PropGet("dependency_kind") != "locked" {
			continue
		}
		if !jsPackageManagers[e.PropGet("package_manager")] {
			continue
		}
		ver := e.PropGet("version")
		if ver == "" {
			continue
		}
		dir := path.Dir(e.SourceFile)
		resolvedByDirName[dir+"\x00"+e.Name] = ver
		if _, ok := resolvedByName[e.Name]; !ok {
			resolvedByName[e.Name] = ver
		}
	}

	// 3. Index import-derived External nodes by package root so a declared dep
	//    can enrich the node an agent navigates to.
	extByRoot := make(map[string]*graph.Entity, len(imported))
	for k := range doc.Entities {
		e := &doc.Entities[k]
		if e.Kind != KindExternal {
			continue
		}
		if root := npmRootFromExtID(e.ID); root != "" {
			extByRoot[root] = e
		}
	}

	// 4. Reconcile each DECLARED npm dependency.
	for k := range doc.Entities {
		e := &doc.Entities[k]
		if e.Subtype != "external_dependency" {
			continue
		}
		if e.PropLen() == 0 || e.PropGet("declared") != "true" {
			continue
		}
		if !jsPackageManagers[e.PropGet("package_manager")] {
			continue
		}

		name := e.Name
		manifestRange := e.PropGet("version")

		// version_range always records the manifest declaration verbatim.
		if _, ok := e.PropLookup("version_range"); !ok {
			e.PropSet("version_range", manifestRange)
		}

		// Resolve the exact version from the sibling lockfile: prefer the
		// same-dir match, then any lockfile in the graph, then the range.
		dir := path.Dir(e.SourceFile)
		resolved := resolvedByDirName[dir+"\x00"+name]
		if resolved == "" {
			resolved = resolvedByName[name]
		}
		if resolved != "" && resolved != e.PropGet("version") {
			e.PropSet("version", resolved)
			stats.VersionsResolved++
		}

		// Used vs unused against the import graph.
		isImported := imported[npmPackageRoot(name)]
		if isImported {
			e.PropSet("imported", "true")
		} else {
			e.PropSet("imported", "false")
			e.PropSet("dead_dependency_candidate", "true")
			stats.DeadCandidates++
		}
		stats.DeclaredReconciled++

		// Enrich the import-derived ext:<root> node with the resolved version
		// + section so a version surfaces on the navigable External node.
		// Peer/optional deps and unimported deps have no such node — skip.
		if ext := extByRoot[npmPackageRoot(name)]; ext != nil {
			finalVer := e.PropGet("version")
			if enrichExternalVersion(ext, finalVer, e.PropGet("dep_section")) {
				stats.ExternalNodesEnriched++
			}
		}
	}

	return stats
}

// enrichExternalVersion stamps version / dep_section / declared on an
// import-derived External node. Returns true if anything changed. Idempotent.
func enrichExternalVersion(ext *graph.Entity, version, section string) bool {
	if ext.PropLen() == 0 {
		ext.PropsReplace(make(map[string]string, 3))
	}
	changed := false
	if version != "" && ext.PropGet("version") != version {
		ext.PropSet("version", version)
		changed = true
	}
	if section != "" && ext.PropGet("dep_section") != section {
		ext.PropSet("dep_section", section)
		changed = true
	}
	if ext.PropGet("declared") != "true" {
		ext.PropSet("declared", "true")
		changed = true
	}
	return changed
}

// npmRootFromExtID extracts the npm package root from an `ext:<...>` External
// node ID. Returns "" for non-ext IDs and for ids that don't look like an npm
// package (Go module paths, db.<table>, node:<builtin> stay out — they carry a
// scheme or path shape an npm package name never has). Subpaths are collapsed
// to the package root (ext:lodash/fp → lodash).
func npmRootFromExtID(id string) string {
	if !strings.HasPrefix(id, ExtIDPrefix) {
		return ""
	}
	return npmPackageRoot(id[len(ExtIDPrefix):])
}

// npmPackageRoot normalises an npm import specifier or package name to its
// package root, the unit a package.json dependency is keyed by:
//
//	react                  → react
//	lodash/fp              → lodash
//	@scope/name            → @scope/name
//	@scope/name/sub        → @scope/name
//
// Returns "" for things that are NOT npm packages: relative ("./x", "../x"),
// absolute ("/x"), Go-style module paths ("github.com/...", "golang.org/..."),
// other-scheme refs ("node:fs", "db.users", "ext:..."), and empties. The
// goal is precision — only collapse specifiers an npm package.json could
// actually declare.
func npmPackageRoot(spec string) string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return ""
	}
	// Relative / absolute project paths are never npm packages.
	if strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "/") {
		return ""
	}
	// Any scheme-qualified ref (node:fs, db.users, ext:react already stripped
	// by the caller, http(s)://...) is not a bare npm package name.
	if i := strings.IndexByte(spec, ':'); i >= 0 {
		return ""
	}
	// Go-style module paths contain a dotted-domain first segment with a slash
	// (github.com/go-chi/chi). npm scoped packages start with '@'; unscoped
	// npm names never contain a '.' in the first path segment.
	first := spec
	if i := strings.IndexByte(spec, '/'); i >= 0 {
		first = spec[:i]
	}
	if !strings.HasPrefix(spec, "@") && strings.Contains(first, ".") {
		return ""
	}

	if strings.HasPrefix(spec, "@") {
		// Scoped: keep the first TWO segments (@scope/name).
		parts := strings.SplitN(spec, "/", 3)
		if len(parts) < 2 || parts[1] == "" {
			return ""
		}
		return parts[0] + "/" + parts[1]
	}
	// Unscoped: keep the first segment.
	if i := strings.IndexByte(spec, '/'); i >= 0 {
		return spec[:i]
	}
	return spec
}
