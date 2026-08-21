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
//
//   - It does not run the DRF router-expansion pass (`ApplyDjangoDRFRoutes`),
//     which would mean reading every Python file in the repo on every daemon
//     tick. The full path runs it only to feed `DeduplicateNestedURLConfDRF`,
//     whose entire effect is "drop a composed ANY endpoint when a per-verb
//     `drf_router_expanded` entry already covers that path". Those per-verb
//     entries are already IN the graph, so the same verdict is reached here by
//     consulting the graph instead — see dropDRFCovered, without the I/O.
//
//     THAT SUBSTITUTION HAS ONE HOLE, and it is CLOSED BY ABSTENTION rather
//     than claimed away. Reading the graph is only as good as what the graph
//     still holds. `ApplyDjangoDRFRoutes` has exactly one caller
//     (cmd/grafel/index.go), so `drf_router_expanded` entities are never
//     re-derived incrementally — and when the ViewSet is declared IN the edited
//     urls.py they are attributed to that file, Step 5 prunes them, and
//     coverage reads as ABSENT when it is merely INVISIBLE. Left alone, this
//     pass then ADDED a composed ANY endpoint the full path deduplicates away.
//
//     So `drfUnknownCoveragePaths` marks the prefix of every
//     `router.register()` in a file that changed this tick, and routes under
//     those prefixes are NEITHER added NOR pruned for that tick. Keyed on the
//     registered prefix, not on "a DRF file changed": a urls.py mixing
//     `router.register("widgets")` with a plain `path("terms/", ...)` still
//     recomposes /…/terms normally.
//
//     MEASURED end-to-end, both layouts, edit confined to the DRF urls.py
//     (TestMountParity_6461_DRFCoverage_PathA_BothLayouts):
//
//     ORDINARY, ViewSet in views.py — before: lost=0 invented=0;
//     after: lost=0 invented=0.
//     SAME-FILE, ViewSet in the edited urls.py — before: lost=6 invented=1
//     (`http:ANY:/api/widgets`); after: lost=6 invented=0.
//
//     The ordinary layout never needed the abstention — the handler resolver
//     re-attributes each `drf_router_expanded` entity onto the ViewSet's file,
//     which the edit does not touch, so the coverage survives and
//     dropDRFCovered reaches the right verdict on its own. The `lost=6` is a
//     SEPARATE, pre-existing defect (#6529: DRF entities are never re-derived
//     incrementally); it is identical with this pass disabled and is
//     deliberately not papered over here.
//
//   - It does not touch the second defect the #6461 thread flags — entities
//     LOST on a file that DID change, because `TryIncremental` runs no CROSS
//     extractors. That is a different root cause and is still live.
package extractors

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
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
	// djangoFramework is the `framework` value ApplyDjangoNestedURLConf stamps
	// on every record it emits. It is what separates this pass's mount points
	// from FastAPI's identically-typed ones — see isComposedDjangoEndpoint.
	djangoFramework = "django"
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
//
// THE `framework` CHECK IS LOAD-BEARING, NOT DEFENSIVE PADDING.
// `pattern_type == "url_mount_point"` has TWO producers on the same entity
// kind: this pass, and `fastapiMountPointSynthetics`
// (internal/engine/http_endpoint_synthesis.go, #6385), which stamps the
// byte-identical string. Ownership by `pattern_type` alone therefore claims
// FastAPI's `include_router(prefix=)` mounts as well — and this pass cannot
// re-derive one, so it would DELETE them. The gate above only asks that the
// repo contains SOME `*urls.py`; it never asks that a given mount is Django's,
// so a Django+FastAPI monorepo hits this on any `.py` edit, silently and
// permanently until the next full reindex.
//
// Both producers stamp `framework`, so that is the discriminator. An entity
// with no `framework` property is NOT claimed: this pass always writes one, so
// its absence means some other producer, and the cost of declining is a stale
// endpoint for one tick versus a deletion nothing re-derives.
func isComposedDjangoEndpoint(e *graph.Entity) bool {
	if e.Kind != endpointSyntheticKind && e.Kind != string(types.HTTPEndpointKindLegacy) {
		return false
	}
	pt, ok := e.PropLookup("pattern_type")
	if !ok {
		return false
	}
	if pt != composedNestedPatternType && pt != composedMountPatternType {
		return false
	}
	fw, ok := e.PropLookup("framework")
	return ok && fw == djangoFramework
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
	//
	// A READ FAILURE IS NOT AN ANSWER. The reader's contract is "nil means not
	// available", which ApplyDjangoNestedURLConf reads as "this file declares no
	// routes" — indistinguishable from "the file is briefly unreadable". Left
	// undistinguished, one transient EIO/ENOENT on a root urls.py yields an
	// EMPTY `fresh` while the graph still holds its compositions, and the
	// reconciliation below then prunes EVERY composed endpoint under it. The
	// flag makes the two cases distinct so the prune can stand down; the adds
	// are still safe (whatever DID parse is real), and a stale endpoint for one
	// tick is a far better outcome than deleting live graph nothing re-derives.
	readFailed := false
	cache := make(map[string][]byte, 8)
	reader := func(relPath string) []byte {
		if b, ok := cache[relPath]; ok {
			return b
		}
		if _, ok := allowed[relPath]; !ok {
			// NOT a read failure: the path is outside the walked set, which is
			// a definite "no such source file" and the security guard's whole
			// point. Treating it as a failure would let a bogus
			// `include("does.not.exist")` disable the prune permanently.
			cache[relPath] = nil
			return nil
		}
		b, err := os.ReadFile(filepath.Join(absRepo, filepath.FromSlash(relPath)))
		if err != nil {
			readFailed = true
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
	// Paths whose DRF coverage this pass CANNOT observe — see
	// drfUnknownComposedPaths. Neither added nor pruned below. Computed from
	// the RAW composition, BEFORE dropDRFCovered, because that call compacts
	// over `fresh`'s own backing array.
	drfUnknown := drfUnknownComposedPaths(pyFiles, changed, fresh, reader)

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

	// `readFailed` is checked HERE and not at the top of the function on
	// purpose: the adds computed above are still sound (they came from files
	// that DID read), and only the prune depends on `fresh` being the COMPLETE
	// composition. Absence of evidence is not evidence of absence — an empty
	// `fresh` caused by an unreadable urls.py must not authorise a deletion.
	present := make(map[string]bool, len(doc.Entities)+len(newEntities))
	res.removedIDs = make(map[string]bool)
	for i := range doc.Entities {
		e := &doc.Entities[i]
		present[e.ID] = true
		if readFailed {
			continue
		}
		if !isComposedDjangoEndpoint(e) || freshIDs[e.ID] {
			continue
		}
		// Same abstention as the add side, for the same reason and in the
		// opposite direction: if this pass cannot see whether DRF covers the
		// path, its absence from `fresh` may be an artefact of the missing
		// coverage rather than a fact about the tree, so it is not evidence
		// for a deletion either.
		if pathIsUnknownCoverage(e, drfUnknown) {
			continue
		}
		res.removedIDs[e.ID] = true
	}
	if readFailed && logger != nil {
		logger.Printf("incremental: django-urlconf recompose — a source read failed; " +
			"prune SKIPPED this pass so an unreadable urls.py cannot delete live composed " +
			"endpoints (#6461)")
	}
	for i := range newEntities {
		present[newEntities[i].ID] = true
	}

	drfSuppressed := 0
	for i := range freshEnts {
		if present[freshEnts[i].ID] {
			continue
		}
		if pathIsUnknownCoverage(&freshEnts[i], drfUnknown) {
			drfSuppressed++
			continue
		}
		res.added = append(res.added, freshEnts[i])
	}
	if drfSuppressed > 0 && logger != nil {
		logger.Printf("incremental: django-urlconf recompose — %d composed endpoint(s) NOT added: "+
			"a changed file declares router.register() and its drf_router_expanded coverage was "+
			"pruned this tick, so DRF coverage is UNKNOWN (#6461)", drfSuppressed)
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

// drfRouterRegisterRe mirrors engine's djangoRouterRegisterRe
// (internal/engine/django_urlconf_nested.go) — the same shapes, because it must
// recognise exactly the registrations that pass composes routes from. It is
// duplicated rather than exported: the engine constant is unexported, this is
// the only other consumer, and the two are pinned together by
// TestDRFUnknownComposedPaths_ScopedToTheRegisteringMount. If you widen one,
// widen the other.
var drfRouterRegisterRe = regexp.MustCompile(
	`(?:[\w]*[Rr]outer|api_router|v\d+_router|router_v\d+)\.register\s*\(\s*r?["']([^"']*)["']`)

// drfUnknownComposedPaths returns the FULL COMPOSED paths whose DRF coverage
// this pass cannot observe this tick.
//
// WHY THIS EXISTS (#6528 review, measured).
// dropDRFCovered decides "is this composed ANY endpoint superseded by per-verb
// drf_router_expanded entries?" by reading those entries out of the GRAPH,
// because ApplyDjangoDRFRoutes has exactly one caller (cmd/grafel/index.go) and
// re-running it here would mean reading every .py file on every tick. That
// substitution is sound only while the entries are still IN the graph.
//
// They are not, in one layout. When the ViewSet is declared in the edited
// urls.py, the drf_router_expanded entities are attributed to that file, Step 5
// prunes them by SourceFile, and nothing on this path re-derives them. Coverage
// then reads as ABSENT when it is merely INVISIBLE, and the composed ANY
// endpoint the full path deduplicates away gets ADDED. Measured on a
// `router.register` + ModelViewSet-in-urls.py fixture: invented went 0 -> 1
// (`http:ANY:/api/widgets`) purely from this pass running.
//
// The answer is abstention, not re-derivation: routes composed out of a
// registration in a changed file are neither added nor pruned for that tick.
//
// HOW THE SET IS COMPUTED, AND WHY NOT BY NAME (#6530 review).
// The first version collected the bare registered segment (`/widgets`) and
// matched it as a SUFFIX against every composed path in the repo. That is
// narrow with respect to CONTAINMENT and unscoped with respect to the MOUNT: a
// `router.register(r"conditions")` in `apiapp/urls.py` suppressed pruning of an
// unrelated `path("conditions/", ...)` in `mpsite/urls.py` mounted at
// `/network/`, because `/network/conditions` ends in `/conditions`. That
// RE-CREATED the very #6461 ghost this pass exists to remove, on nothing but a
// last-segment name collision — and `users`, `items`, `orders` are the
// realistic collisions in any monorepo.
//
// So the set is computed DIFFERENTIALLY instead of by name. The composition is
// re-run over the same file list with every `router.register(...)` call site in
// the CHANGED files textually removed; whatever composed path disappears
// between the two runs is, by construction, a path that came from one of those
// registrations — mount prefix and all. No module-path resolution to duplicate,
// no name matching to get wrong, and the mount scoping is inherited from the
// composition itself rather than re-derived.
//
// Exact in both directions the previous version got wrong:
//
//   - `/network/conditions` composed from a plain `path()` in another app
//     survives the stripped run, so it is NOT in the set and prunes normally.
//   - a urls.py mixing `router.register("widgets")` with `path("terms/", ...)`
//     loses only `/.../widgets` in the stripped run, so `/.../terms` still
//     recomposes.
//
// COST: one extra composition over ALREADY-CACHED content (the reader memoises,
// so there is no second read), and only when a changed `.py` actually declares
// a registration. Note that the scan reads every changed `.py`, not only
// `*urls.py` — a registration can live in `routers.py` or any module an
// `include()` reaches — but it stays bounded by the changed set, which the
// too-many-changed gate caps well below the repo.
func drfUnknownComposedPaths(
	pyFiles, changed []string,
	fresh []types.EntityRecord,
	reader engine.NestedURLConfFileReader,
) map[string]bool {
	// Which changed files declare a registration at all. Nothing else here runs
	// when the answer is "none", which is the overwhelming majority of ticks.
	registering := map[string]bool{}
	for _, rel := range changed {
		if !isPythonPath(rel) {
			continue
		}
		content := reader(rel)
		if len(content) == 0 {
			continue
		}
		if drfRouterRegisterRe.Match(content) {
			registering[rel] = true
		}
	}
	if len(registering) == 0 {
		return nil
	}

	// The counterfactual: the same tree with those registrations deleted.
	// Removing the matched text leaves `, WidgetViewSet)` behind, which no
	// route-recognising regex in the composition matches — deliberately, so the
	// rest of the file keeps composing exactly as it did.
	strippedReader := func(relPath string) []byte {
		content := reader(relPath)
		if !registering[relPath] || len(content) == 0 {
			return content
		}
		return drfRouterRegisterRe.ReplaceAll(content, nil)
	}
	without := engine.ApplyDjangoNestedURLConf(pyFiles, strippedReader)

	survives := make(map[string]bool, len(without))
	for i := range without {
		if p := composedRecordPath(&without[i]); p != "" {
			survives[p] = true
		}
	}
	unknown := map[string]bool{}
	for i := range fresh {
		p := composedRecordPath(&fresh[i])
		if p != "" && !survives[p] {
			unknown[p] = true
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	return unknown
}

// composedRecordPath returns a composed NESTED record's path, or "" for
// anything else. Mount points are excluded on purpose: they are not what
// DeduplicateNestedURLConfDRF removes, so their coverage is never in question.
func composedRecordPath(r *types.EntityRecord) string {
	if r.Properties == nil || r.Properties["pattern_type"] != composedNestedPatternType {
		return ""
	}
	return r.Properties["path"]
}

// pathIsUnknownCoverage reports whether the entity's `path` property is one of
// the composed paths whose DRF coverage is unknown this tick.
//
// EXACT MATCH on the full composed path — see drfUnknownComposedPaths for why a
// suffix test on the bare registered segment was wrong. The set already carries
// the mount prefix, so no scoping is left for this function to get right.
func pathIsUnknownCoverage(e *graph.Entity, unknown map[string]bool) bool {
	if len(unknown) == 0 {
		return false
	}
	p, ok := e.PropLookup("path")
	if !ok || p == "" {
		return false
	}
	return unknown[p]
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
