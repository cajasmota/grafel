// django_global_wiring.go — late-binding resolver pass for Django global
// cross-cutting wiring (issue #4379).
//
// The Django extractor emits a synthetic `django_settings` entity that owns one
// USES edge per class bound app-wide via settings (MIDDLEWARE,
// AUTHENTICATION_BACKENDS, REST_FRAMEWORK DEFAULT_*_CLASSES). Each such edge has
// Properties["global"]="true" and Properties["dotted_path"] = the verbatim
// sys.path-rooted dotted path, with the leaf class name in
// Properties["class_name"]. The edge ToID is the dotted path.
//
// The dotted path is the class's module-qualified QualifiedName, so it normally
// resolves directly through the byQualifiedName index during BuildIndex. But a
// referenced class can lose its QualifiedName during MergeWithCustom — e.g. a
// MiddlewareMixin subclass that the Django CBV extractor re-emits as a
// SCOPE.Operation/endpoint replaces the base SCOPE.Component class node and the
// merged node has no QualifiedName (the same merge-replace concern as #4366).
// In that case the QualifiedName probe misses and the edge would dangle.
//
// This pass runs AFTER BuildIndex and rewrites any still-unresolved global
// wiring USES edge to the real entity ID by:
//
//	(1) byQualifiedName / byName via idx.Lookup(dotted_path) — the structural
//	    sys.path-rooted resolution.
//	(2) the unique leaf class name (Properties["class_name"]) — recovers the
//	    merge-dropped-QualifiedName case where the surviving node is still
//	    indexed by bare Name.
//
// Built-in / third-party dotted paths (django.*, rest_framework.*) have no
// in-repo entity and are intentionally left unresolved here; the downstream
// External-synthesis pass materialises them as external nodes, exactly as it
// does for any other unresolved import-shaped reference.
package resolve

import (
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// uniqueID returns the single distinct entity ID present in ids, or "" when ids
// is empty or names two-or-more distinct IDs (ambiguous).
func uniqueID(ids []string) string {
	first := ""
	for _, id := range ids {
		if id == "" {
			continue
		}
		if first == "" {
			first = id
		} else if id != first {
			return ""
		}
	}
	return first
}

// ResolveDjangoGlobalWiringRefs rewrites unresolved Django settings global
// USES edges (MIDDLEWARE / auth / DRF default classes) to the real in-repo
// class entity IDs. Returns the number of edges rewritten.
//
// It is a method on Index because it reads the symbol table. It mutates the
// Relationships slice of affected EntityRecords in place.
func (idx Index) ResolveDjangoGlobalWiringRefs(records []types.EntityRecord) int {
	rewrites, _ := idx.resolveDjangoGlobalWiringRefs(records)
	return rewrites
}

// resolveDjangoGlobalWiringRefs is the implementation. It additionally returns
// the number of times the lazy whole-corpus bare-Name index was materialised,
// which the #5986 tests assert on: 0 when no edge reaches Strategy 3, and
// exactly 1 no matter how many edges do.
func (idx Index) resolveDjangoGlobalWiringRefs(records []types.EntityRecord) (int, int) {
	rewrites := 0
	nameIndexBuilds := 0

	// byNameSrc indexes candidate entities by bare Name → list of (id,
	// sourceFile). Used by the module-qualified leaf fallback (Strategy 3),
	// which disambiguates a leaf name that is ambiguous in the global byName
	// index (e.g. a MiddlewareMixin subclass re-emitted as both a CBV endpoint
	// and a SCOPE.Pattern, dropping its QualifiedName) by matching the dotted
	// path's module against the entity's source-file-derived Python module.
	//
	// #5986: this is a whole-corpus map (~42-75 MB live on a 427k-entity
	// corpus) and it is live concurrently with resolve.BuildIndex, so it adds
	// to the peak heap. It serves ONLY Strategy 3, which requires a global
	// USES edge with a dotted_path, an unresolved non-hex ToID, AND both
	// Strategies 1 and 2 missing — conditions no non-Django repo can meet.
	// So it is built lazily on first Strategy-3 need rather than behind an
	// early-return guard: a guard would have to restate those conditions and
	// could drift out of sync with them, whereas laziness is correct by
	// construction. nameIndex() memoises, so the build is O(entities) ONCE for
	// the whole pass — never per-edge, which would be O(edges × entities).
	type nameCand struct {
		id, src, kind string
	}
	var byNameSrc map[string][]nameCand
	nameIndex := func() map[string][]nameCand {
		if byNameSrc != nil {
			return byNameSrc
		}
		byNameSrc = map[string][]nameCand{}
		nameIndexBuilds++
		for i := range records {
			e := &records[i]
			if e.ID == "" || e.Name == "" || e.SourceFile == "" {
				continue
			}
			byNameSrc[e.Name] = append(byNameSrc[e.Name], nameCand{e.ID, e.SourceFile, e.Kind})
		}
		return byNameSrc
	}

	for k := range records {
		rec := &records[k]
		for j := range rec.Relationships {
			r := &rec.Relationships[j]
			if r.Kind != string(types.RelationshipKindUses) {
				continue
			}
			if r.Properties == nil || r.Properties.Get("global") != "true" {
				continue
			}
			dotted := r.Properties.Get("dotted_path")
			if dotted == "" {
				continue
			}
			// Already resolved to a hex entity ID (BuildIndex matched the
			// QualifiedName): nothing to do.
			if r.ToID == "" || isHexID(r.ToID) {
				continue
			}

			// Strategy 1: full dotted-path resolution (QualifiedName, or bare
			// name when the dotted path is itself a single segment).
			if id, ok := idx.Lookup(dotted); ok && id != "" {
				r.ToID = id
				rewrites++
				continue
			}

			// Strategy 2: unique leaf class name. Recovers the merge-dropped
			// QualifiedName case where the surviving node is still indexed by
			// its bare Name. class_name is stamped by the extractor; fall back
			// to splitting the dotted path if absent.
			leaf := r.Properties.Get("class_name")
			if leaf == "" {
				if i := strings.LastIndexByte(dotted, '.'); i >= 0 && i+1 < len(dotted) {
					leaf = dotted[i+1:]
				}
			}
			if leaf != "" && leaf != dotted {
				if id, ok := idx.Lookup(leaf); ok && id != "" {
					r.ToID = id
					rewrites++
					continue
				}

				// Strategy 3: module-qualified leaf disambiguation. The leaf is
				// ambiguous in the global byName index (so idx.Lookup failed),
				// but the dotted path names the exact module. Match the unique
				// candidate whose source-file-derived Python module + "." + leaf
				// equals the dotted path. wantModule is the dotted path minus
				// its leaf segment.
				wantModule := ""
				if i := strings.LastIndexByte(dotted, '.'); i > 0 {
					wantModule = dotted[:i]
				}
				if wantModule != "" {
					// Collect candidates whose source-file module matches the
					// dotted path exactly. The leaf class can be re-emitted at
					// the same (Name, SourceFile) under several kinds during merge
					// (e.g. a CBV SCOPE.Operation/endpoint plus a synthetic
					// SCOPE.Pattern), so prefer a definition-bearing node and
					// resolve only when a single best candidate exists.
					var defIDs, allIDs []string
					for _, c := range nameIndex()[leaf] {
						matched := false
						for _, mod := range modulesForPythonFile(c.src) {
							if mod == wantModule {
								matched = true
								break
							}
						}
						if !matched {
							continue
						}
						allIDs = append(allIDs, c.id)
						// SCOPE.Pattern nodes are synthetic anchors, not the class
						// definition; bias toward the real definition node.
						if c.kind != "SCOPE.Pattern" {
							defIDs = append(defIDs, c.id)
						}
					}
					if id := uniqueID(defIDs); id != "" {
						r.ToID = id
						rewrites++
						continue
					}
					if id := uniqueID(allIDs); id != "" {
						r.ToID = id
						rewrites++
						continue
					}
				}
			}
		}
	}
	return rewrites, nameIndexBuilds
}
