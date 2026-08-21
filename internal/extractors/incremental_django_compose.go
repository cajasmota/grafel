// incremental_django_compose.go — the daemon's cross-file composition pass
// (#6461).
//
// THE DEFECT
// ──────────
// `TryIncremental` prunes entities ONLY by `SourceFile` and, before this file,
// ran no cross-file composition pass at all. Any entity attributed to a file
// OTHER than the one that produced its content is therefore invisible to both
// halves of the incremental contract: it is not pruned (its file did not
// change) and it is not re-derived (nothing recomposes it).
//
// Django's nested URLconf is the shipped instance. `path("network/",
// include("mpsite.urls"))` in the MOUNT file composes with `path("conditions/",
// views.mp_terms_view)` in the ROUTE file into one `http:ANY:/network/conditions`
// endpoint, which `engine.ApplyDjangoNestedURLConf` attributes to the mount file
// and `bridgeEndpointToHandler` (#2678) then re-attributes to the HANDLER's file
// — `mpsite/views.py`. Edit the route file alone and NONE of those three files
// except the route file changes, so the pre-edit composition survives as a
// GHOST: measured on the #6461 fixture as `http:ANY:/network/terms` sitting in
// the graph where a full rebuild has `http:ANY:/network/conditions`.
//
// THE FIX
// ───────
// Re-run the composition on the daemon path and RECONCILE, rather than trying
// to guess which composed entities an edit invalidates. The composition is a
// pure function of the repo's Python files, so recomputing it yields exactly
// the set a full rebuild would hold; anything the graph carries that is not in
// that set is stale by construction and is pruned, and anything in the set the
// graph lacks is added. No provenance bookkeeping to keep in step, and the
// answer does not depend on WHICH file was edited.
//
// WHAT THIS DELIBERATELY DOES NOT DO
// ──────────────────────────────────
//   - It does not run the DRF router-expansion pass (`ApplyDjangoDRFRoutes`),
//     which would mean reading every Python file in the repo on every daemon
//     tick. The full path runs it only to feed `DeduplicateNestedURLConfDRF`,
//     whose entire effect is "drop a composed ANY endpoint when a per-verb
//     `drf_router_expanded` entry already covers that path". Those per-verb
//     entries are already IN the graph, so the same verdict is reached here by
//     consulting the graph instead — see dropDRFCovered. That keeps the
//     permissive direction closed (this pass can never ADD an endpoint the full
//     path deduplicates away) without the I/O.
//   - It does not touch the second defect the #6461 thread flags — entities
//     LOST on a file that DID change, because `TryIncremental` runs no CROSS
//     extractors. That is a different root cause and is still live.
package extractors

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// Pattern types stamped by engine.ApplyDjangoNestedURLConf on the two kinds of
// record it emits. Together they define the set this pass OWNS: an endpoint
// entity carrying one of these is fully re-derivable from the file tree, so it
// is safe to prune when the recomputation no longer produces it. Endpoints from
// any other pass are left strictly alone.
const (
	composedNestedPatternType = "urlconf_nested_include"
	composedMountPatternType  = "url_mount_point"
	drfExpandedPatternType    = "drf_router_expanded"
)

// implementsEdgeKindIncremental mirrors engine's IMPLEMENTS edge kind. It is
// duplicated rather than exported because the engine constant is unexported and
// this is the only consumer on this path.
const implementsEdgeKindIncremental = "IMPLEMENTS"

// handlerShellKinds are the entity kinds a Django/DRF `source_handler`
// reference can resolve to. Only entities of these kinds are handed to the
// handler resolver below, which keeps the shell slice proportional to the
// repo's function/class count rather than to the whole graph.
//
// The list is the union of resolverKindEquivalents' keys and values in
// internal/engine/http_endpoint_resolve.go plus the plain language-extractor
// spellings. Over-inclusion here is cheap (a few more shells); UNDER-inclusion
// silently drops a composed endpoint, because the corpus-scoped resolver treats
// "no record matches source_handler" as "this endpoint names a handler that
// does not exist" and deletes it.
var handlerShellKinds = map[string]bool{
	"SCOPE.Operation": true,
	"SCOPE.Function":  true,
	"SCOPE.Class":     true,
	"Controller":      true,
	"View":            true,
	"Function":        true,
	"Method":          true,
	"Class":           true,
}

// djangoComposeResult is what the daemon must apply to reconcile the graph with
// a freshly recomputed composition.
type djangoComposeResult struct {
	// added are composed entities the graph does not yet hold, already stamped
	// with their deterministic graph.EntityID.
	added []graph.Entity
	// addedRels are the IMPLEMENTS edges the handler-resolution pass emitted for
	// `added`, with both endpoints already bound to real entity ids.
	addedRels []graph.Relationship
	// removedIDs are the ids of composed entities the graph holds that the
	// recomputation no longer produces — the ghosts.
	removedIDs map[string]bool
	// ran reports whether the recomputation actually happened; false means the
	// cheap gate declined (no Python change, or no Django URLconf file).
	ran bool
}

// isComposedDjangoEndpoint reports whether e is an endpoint entity THIS pass
// owns, i.e. one that engine.ApplyDjangoNestedURLConf produces and can
// therefore re-produce.
func isComposedDjangoEndpoint(e *graph.Entity) bool {
	if e.Kind != endpointSyntheticKind && e.Kind != string(types.HTTPEndpointKindLegacy) {
		return false
	}
	pt, ok := e.PropLookup("pattern_type")
	if !ok {
		return false
	}
	return pt == composedNestedPatternType || pt == composedMountPatternType
}

// prunesRel reports whether a surviving relationship must be dropped because
// one of its endpoints is a composed entity this reconciliation removed.
//
// BOTH directions, and that is not symmetry for its own sake: a composed
// endpoint is the TARGET of the handler's IMPLEMENTS edge and the SOURCE of its
// ENTRY_POINT_OF edge. Testing only FromID leaves the IMPLEMENTS row pointing
// at an id no entity has — an invisible orphan that survives to the next full
// reindex, which is the same shape Step 6a's inbound-dangling prune exists to
// prevent. It is a predicate rather than an inline condition so that the
// both-directions requirement is pinned by a test instead of by a reviewer
// noticing a missing `||`.
func (r djangoComposeResult) prunesRel(rel *graph.Relationship) bool {
	if len(r.removedIDs) == 0 {
		return false
	}
	return r.removedIDs[rel.FromID] || r.removedIDs[rel.ToID]
}

// recomposeDjangoURLConf recomputes the Django cross-file URLconf composition
// over the CURRENT file tree and returns the reconciliation the caller must
// apply. It never mutates doc.
//
// `allFiles` is the repo-relative file list walkSourceFiles produced for THIS
// pass, so it already reflects creations and deletions. `changed` is the
// really-changed set; it is used only as a gate.
func recomposeDjangoURLConf(
	absRepo string,
	allFiles []string,
	changed []string,
	doc *graph.Document,
	newEntities []graph.Entity,
	logger *log.Logger,
) djangoComposeResult {
	var res djangoComposeResult

	// Gate 1 — a non-Python edit cannot change a Python composition. This is
	// what keeps the pass off the hot path for the overwhelming majority of
	// daemon ticks.
	if !anyPythonPath(changed) {
		return res
	}
	// Gate 2 — no Django URLconf file anywhere means ApplyDjangoNestedURLConf
	// has no root to scan and would return nil after walking the list. Checking
	// here avoids building the reader and the shell slice for every Python repo
	// that is not a Django one.
	pyFiles := make([]string, 0, 64)
	hasURLConf := false
	for _, f := range allFiles {
		if !isPythonPath(f) {
			continue
		}
		pyFiles = append(pyFiles, f)
		if strings.HasSuffix(filepath.Base(f), "urls.py") {
			hasURLConf = true
		}
	}
	if !hasURLConf {
		return res
	}
	res.ran = true

	// The reader serves ONLY files in the walked set, so a crafted
	// `include("....secrets")` cannot make the pass read outside the source
	// tree. Contents are cached because a child urls.py is read once per parent
	// that includes it.
	allowed := make(map[string]struct{}, len(allFiles))
	for _, f := range allFiles {
		allowed[f] = struct{}{}
	}
	cache := make(map[string][]byte, 8)
	reader := func(relPath string) []byte {
		if b, ok := cache[relPath]; ok {
			return b
		}
		if _, ok := allowed[relPath]; !ok {
			cache[relPath] = nil
			return nil
		}
		b, err := os.ReadFile(filepath.Join(absRepo, filepath.FromSlash(relPath)))
		if err != nil {
			b = nil
		}
		cache[relPath] = b
		return b
	}

	fresh := engine.ApplyDjangoNestedURLConf(pyFiles, reader)
	if len(fresh) == 0 && !graphHoldsComposed(doc) {
		return res
	}
	fresh = dropDRFCovered(fresh, doc, newEntities)

	// Resolve handlers exactly as the full path does, so the composed records
	// land on the handler's file with the same `registration_source_file` /
	// `attribution` properties and the same IMPLEMENTS bridge. The CORPUS-scoped
	// entry point is the right one: `merged` below carries every handler-shaped
	// entity in the graph, so "source_handler matches nothing" really does mean
	// "no such handler", which is the verdict a full rebuild reaches.
	shells, shellID := handlerShells(doc, newEntities)
	merged := make([]types.EntityRecord, 0, len(fresh)+len(shells))
	merged = append(merged, fresh...)
	merged = append(merged, shells...)
	// Pass 2.7 before Pass 2.8, exactly as both the full path
	// (cmd/grafel/index.go) and the per-file branch of this one order them: the
	// response-shape pass reads `source_handler`, and the resolver DELETES that
	// property once it has bridged the endpoint. Reversing the two silently
	// costs the composed endpoint its response_keys / status_codes.
	engine.ApplyResponseShapesCorpus(merged, nil, engine.CorpusFileReader(reader))
	resolved, _ := engine.ResolveHTTPEndpointHandlersWithRepo(merged, doc.Repo)

	// Split the returned slice back apart. Endpoint records are the composition;
	// everything else is a handler shell that may have gained an embedded
	// IMPLEMENTS edge.
	freshEnts := make([]graph.Entity, 0, len(fresh))
	stubToID := make(map[string]string, len(fresh))
	for i := range resolved {
		r := &resolved[i]
		if !isEndpointRecordKind(r.Kind) {
			continue
		}
		e := entityRecordToGraphEntity(*r, doc.Repo)
		freshEnts = append(freshEnts, e)
		stubToID[r.Kind+":"+r.Name] = e.ID
	}

	// Which composed ids the recomputation produces. Everything the graph holds
	// under this pass's ownership and NOT in here is stale.
	freshIDs := make(map[string]bool, len(freshEnts))
	for i := range freshEnts {
		freshIDs[freshEnts[i].ID] = true
	}

	present := make(map[string]bool, len(doc.Entities)+len(newEntities))
	res.removedIDs = make(map[string]bool)
	for i := range doc.Entities {
		e := &doc.Entities[i]
		present[e.ID] = true
		if isComposedDjangoEndpoint(e) && !freshIDs[e.ID] {
			res.removedIDs[e.ID] = true
		}
	}
	for i := range newEntities {
		present[newEntities[i].ID] = true
	}

	for i := range freshEnts {
		if present[freshEnts[i].ID] {
			continue
		}
		res.added = append(res.added, freshEnts[i])
	}

	// Harvest the IMPLEMENTS bridges the resolver appended to the handler
	// shells, but only those landing on an endpoint we are actually ADDING —
	// an edge to an endpoint that already exists is already in the graph, and
	// re-adding it would duplicate the row.
	addedIDs := make(map[string]bool, len(res.added))
	for i := range res.added {
		addedIDs[res.added[i].ID] = true
	}
	for i := range resolved {
		r := &resolved[i]
		if isEndpointRecordKind(r.Kind) {
			continue
		}
		fromID, ok := shellID[shellKey{r.Kind, r.Name, r.SourceFile}]
		if !ok {
			continue
		}
		for _, rel := range r.Relationships {
			if rel.Kind != implementsEdgeKindIncremental {
				continue
			}
			toID, ok := stubToID[rel.ToID]
			if !ok || !addedIDs[toID] {
				continue
			}
			// Through relRecordToGraphRel so the row gets the same
			// graph.RelationshipID and the same property snapshot every other
			// edge on this path gets — a hand-built graph.Relationship would
			// carry an empty ID and lose the resolver's framework /
			// pattern_type stamp.
			bound := rel
			bound.FromID = fromID
			bound.ToID = toID
			res.addedRels = append(res.addedRels, relRecordToGraphRel(bound, fromID))
		}
	}

	if logger != nil && (len(res.added) > 0 || len(res.removedIDs) > 0) {
		logger.Printf("incremental: django-urlconf recompose composed=%d added=%d stale_pruned=%d rels=%d (#6461)",
			len(freshEnts), len(res.added), len(res.removedIDs), len(res.addedRels))
	}
	return res
}

// graphHoldsComposed reports whether doc carries any entity this pass owns. It
// exists so that a repo which STOPS composing (the last include() deleted) still
// gets its ghosts pruned, instead of the empty recomputation being mistaken for
// "nothing to do".
func graphHoldsComposed(doc *graph.Document) bool {
	for i := range doc.Entities {
		if isComposedDjangoEndpoint(&doc.Entities[i]) {
			return true
		}
	}
	return false
}

// shellKey identifies a handler shell by the triple the resolver indexes on.
type shellKey struct{ kind, name, file string }

// handlerShells projects the graph's handler-shaped entities into the
// EntityRecord form the resolver consumes, and returns the map back to their
// real entity ids so the IMPLEMENTS edges can be bound afterwards.
//
// `newEntities` is consulted as well as `doc.Entities` because a handler in the
// file that was just re-extracted lives there and nowhere else at this point in
// the pass.
func handlerShells(doc *graph.Document, newEntities []graph.Entity) ([]types.EntityRecord, map[shellKey]string) {
	ids := make(map[shellKey]string, 64)
	out := make([]types.EntityRecord, 0, 64)
	add := func(e *graph.Entity) {
		if !handlerShellKinds[e.Kind] {
			return
		}
		k := shellKey{e.Kind, e.Name, e.SourceFile}
		if _, dup := ids[k]; dup {
			return
		}
		ids[k] = e.ID
		out = append(out, types.EntityRecord{
			Kind:          e.Kind,
			Name:          e.Name,
			QualifiedName: e.QualifiedName,
			SourceFile:    e.SourceFile,
			StartLine:     e.StartLine,
			EndLine:       e.EndLine,
			Language:      e.Language,
		})
	}
	for i := range newEntities {
		add(&newEntities[i])
	}
	for i := range doc.Entities {
		add(&doc.Entities[i])
	}
	return out, ids
}

// dropDRFCovered is the graph-backed equivalent of
// engine.DeduplicateNestedURLConfDRF: a composed ANY endpoint is dropped when a
// per-verb `drf_router_expanded` entry already covers the same path. The full
// path reaches this verdict from the DRF pass's fresh output; here the same
// entries are read out of the graph, which is where the DRF pass already put
// them.
func dropDRFCovered(fresh []types.EntityRecord, doc *graph.Document, newEntities []graph.Entity) []types.EntityRecord {
	covered := make(map[string]bool)
	note := func(e *graph.Entity) {
		if pt, ok := e.PropLookup("pattern_type"); !ok || pt != drfExpandedPatternType {
			return
		}
		verb, vok := e.PropLookup("verb")
		p, pok := e.PropLookup("path")
		if !vok || !pok || verb == "" || verb == "ANY" || p == "" {
			return
		}
		covered[p] = true
	}
	for i := range doc.Entities {
		note(&doc.Entities[i])
	}
	for i := range newEntities {
		note(&newEntities[i])
	}
	if len(covered) == 0 {
		return fresh
	}
	out := fresh[:0]
	for _, r := range fresh {
		if r.Properties != nil &&
			r.Properties["pattern_type"] == composedNestedPatternType &&
			covered[r.Properties["path"]] {
			continue
		}
		out = append(out, r)
	}
	return out
}

// isEndpointRecordKind reports whether a record kind is one of the HTTP
// endpoint kinds. The resolver rewrites the legacy `http_endpoint` kind onto
// the #1217 split kinds in-place, so both spellings must be recognised.
func isEndpointRecordKind(kind string) bool {
	return types.IsHTTPEndpointDefinitionKind(kind) ||
		kind == string(types.HTTPEndpointKindLegacy)
}

// isPythonPath reports whether a repo-relative path is a Python source file.
func isPythonPath(rel string) bool {
	return strings.HasSuffix(strings.ToLower(rel), ".py")
}

// anyPythonPath reports whether any path in the set is Python.
func anyPythonPath(paths []string) bool {
	for _, p := range paths {
		if isPythonPath(p) {
			return true
		}
	}
	return false
}
