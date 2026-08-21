// incremental_django_compose_6461_test.go — PERMISSIVE-DIRECTION ratchets for
// the #6461 cross-file composition pass.
//
// The end-to-end gate in cmd/grafel (TestMountParity_6461_Django_
// RoutePathRename_PathA_EndpointCensus) proves the ghost is corrected. It does
// NOT constrain how much the pass is allowed to touch on its way there, and a
// recompose-and-reconcile pass fails in exactly that direction: prune a wider
// set than it owns and live endpoints from other passes disappear; re-add
// entities the graph already holds and every endpoint doubles on every tick.
// Both mutants leave the ghost fixed, so the census gate stays green for them.
//
// These two tests are therefore the permissive half, asserted at the seam where
// the decision is made rather than through the whole daemon.
package extractors

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/graph"
)

const (
	dcRepoTag      = "test-repo"
	dcEndpointKind = "http_endpoint_definition"
)

// dcWriteRepo lays down the minimal Django two-file URLconf: a mount file that
// includes a route file, and the view the route names.
func dcWriteRepo(t *testing.T) (absRepo string, allFiles []string) {
	t.Helper()
	absRepo = t.TempDir()
	files := map[string]string{
		"mpproj/urls.py": `from django.urls import include, path

urlpatterns = [
    path("network/", include("mpsite.urls")),
]
`,
		"mpsite/urls.py": `from django.urls import path

from . import views

urlpatterns = [
    path("conditions/", views.mp_terms_view, name="mp_terms"),
]
`,
		"mpsite/views.py": `def mp_terms_view(request):
    return {"ok": 1}
`,
	}
	for rel, body := range files {
		abs := filepath.Join(absRepo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
		allFiles = append(allFiles, rel)
	}
	return absRepo, allFiles
}

// dcEntity builds a graph.Entity with the deterministic id the pass computes.
func dcEntity(kind, name, file string, props map[string]string) graph.Entity {
	return graph.Entity{
		ID:         graph.EntityID(dcRepoTag, kind, name, file),
		Kind:       kind,
		Name:       name,
		SourceFile: file,
		StartLine:  1,
		EndLine:    2,
		Language:   "python",
	}.WithProperties(props)
}

func dcSilentLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// dcDiskReader is the same allow-listed, memoising reader recomposeDjangoURLConf
// builds, exposed so a test can drive engine.ApplyDjangoNestedURLConf and
// drfUnknownComposedPaths over the same bytes the pass would see.
func dcDiskReader(absRepo string, allFiles []string) engine.NestedURLConfFileReader {
	allowed := make(map[string]struct{}, len(allFiles))
	for _, f := range allFiles {
		allowed[f] = struct{}{}
	}
	cache := map[string][]byte{}
	return func(rel string) []byte {
		if b, ok := cache[rel]; ok {
			return b
		}
		if _, ok := allowed[rel]; !ok {
			cache[rel] = nil
			return nil
		}
		b, err := os.ReadFile(filepath.Join(absRepo, filepath.FromSlash(rel)))
		if err != nil {
			b = nil
		}
		cache[rel] = b
		return b
	}
}

// TestDjangoComposeResult_PrunesRelBothDirections pins the edge-prune rule.
//
// Mutant this kills: testing only `removedIDs[rel.FromID]`. That mutant is
// invisible to every test that inspects the ENTITY set — including the
// end-to-end census gate, which was measured GREEN against it — because the
// ghost entity is still removed. What survives is its INBOUND `IMPLEMENTS` row,
// now pointing at an id no entity has: an orphan edge that lives until the next
// full reindex.
func TestDjangoComposeResult_PrunesRelBothDirections(t *testing.T) {
	const ghostID = "ent:ghost"
	res := djangoComposeResult{removedIDs: map[string]bool{ghostID: true}}

	cases := []struct {
		name string
		rel  graph.Relationship
		want bool
	}{
		{"outbound — ghost is the SOURCE (ENTRY_POINT_OF)",
			graph.Relationship{FromID: ghostID, ToID: "ent:process", Kind: "ENTRY_POINT_OF"}, true},
		{"inbound — ghost is the TARGET (IMPLEMENTS from the handler)",
			graph.Relationship{FromID: "ent:handler", ToID: ghostID, Kind: "IMPLEMENTS"}, true},
		{"unrelated — neither endpoint was pruned",
			graph.Relationship{FromID: "ent:handler", ToID: "ent:module", Kind: "CONTAINS"}, false},
	}
	for _, tc := range cases {
		if got := res.prunesRel(&tc.rel); got != tc.want {
			t.Errorf("#6461 %s: prunesRel(%s→%s :%s) = %t, want %t. A composed endpoint is the "+
				"target of the handler's IMPLEMENTS edge and the source of its ENTRY_POINT_OF edge, "+
				"so pruning it must drop rows in BOTH directions or the graph keeps an edge "+
				"referencing an id no entity has.",
				tc.name, tc.rel.FromID, tc.rel.ToID, tc.rel.Kind, got, tc.want)
		}
	}

	// An empty reconciliation must never drop anything.
	empty := djangoComposeResult{}
	if empty.prunesRel(&graph.Relationship{FromID: "ent:a", ToID: "ent:b", Kind: "CALLS"}) {
		t.Errorf("#6461: a reconciliation that pruned nothing must not drop any edge")
	}
}

// TestRecomposeDjangoURLConf_PrunesOnlyEndpointsItOwns pins the prune predicate.
//
// The pass may delete a composed endpoint the recomputation no longer produces
// — that is the whole fix. It may NOT delete an endpoint some other pass
// produced, because it cannot re-derive one and the daemon would simply lose it
// until the next full reindex.
//
// Mutant this kills: widening isComposedDjangoEndpoint to accept any endpoint
// entity regardless of `pattern_type`. That mutant still prunes the ghost, so
// the end-to-end census gate stays GREEN for it; here the foreign endpoint
// vanishes and the test fails.
func TestRecomposeDjangoURLConf_PrunesOnlyEndpointsItOwns(t *testing.T) {
	absRepo, allFiles := dcWriteRepo(t)

	ghost := dcEntity(dcEndpointKind, "http:ANY:/network/terms", "mpsite/views.py", map[string]string{
		"pattern_type": composedNestedPatternType,
		"path":         "/network/terms",
		"verb":         "ANY",
		"framework":    "django",
	})
	// Emitted by Pass 2.5's per-file synthesis, NOT by the composition pass.
	// Nothing in this pass can re-create it.
	foreign := dcEntity(dcEndpointKind, "http:GET:/foreign", "mpsite/views.py", map[string]string{
		"pattern_type": "http_endpoint_synthesis",
		"path":         "/foreign",
		"verb":         "GET",
	})
	// #6528 review — the HARD case, and the one M3 never covered: a FOREIGN
	// producer of the SAME pattern_type. fastapiMountPointSynthetics
	// (internal/engine/http_endpoint_synthesis.go, #6385) stamps the
	// byte-identical `url_mount_point` on the byte-identical entity kind for
	// FastAPI's `include_router(prefix=)`. Ownership by pattern_type alone
	// claims it, and this pass cannot re-derive a FastAPI mount, so it would
	// DELETE it — reachable in any Django+FastAPI monorepo, because the gate
	// only asks that SOME *urls.py exists, never that a given mount is Django's.
	fastapiMount := dcEntity(dcEndpointKind, "http:ANY:/api/v2:mount", "svc/main.py", map[string]string{
		"pattern_type": composedMountPatternType,
		"path":         "/api/v2",
		"verb":         "ANY",
		"framework":    "fastapi",
	})
	// #6530 — the case NEITHER of the two above covers, and the one a review
	// mutant slipped through: a claimed `pattern_type` with NO `framework`
	// property at all. FastAPI is the collision we already know about; an
	// UNLABELLED mount is what a future pass, or a third-party producer, emits
	// before anyone thinks to stamp a framework — and there is no second
	// property to fall back on, so the predicate's third arm is the only thing
	// standing between it and deletion. Without this row the predicate's
	// `ok &&` and the sentence justifying it in isComposedDjangoEndpoint rest on
	// nothing: a mutant returning true on absence survives the whole package.
	unlabelled := dcEntity(dcEndpointKind, "http:ANY:/legacy:mount", "thirdparty/wiring.py",
		map[string]string{
			"pattern_type": composedMountPatternType,
			"path":         "/legacy",
			"verb":         "ANY",
		})
	handler := dcEntity("SCOPE.Operation", "mp_terms_view", "mpsite/views.py", nil)

	doc := &graph.Document{
		Repo:     dcRepoTag,
		Entities: []graph.Entity{ghost, foreign, fastapiMount, unlabelled, handler},
	}

	res := recomposeDjangoURLConf(absRepo, allFiles, []string{"mpsite/urls.py"}, doc, nil, dcSilentLogger())

	if !res.ran {
		t.Fatalf("#6461: the pass declined to run on a Django repo with a *urls.py and a changed .py file; "+
			"gate inputs were allFiles=%v changed=[mpsite/urls.py]", allFiles)
	}
	if !res.removedIDs[ghost.ID] {
		t.Errorf("#6461: the stale composed endpoint %s|%s@%s was NOT pruned; the recomputation no "+
			"longer produces it (the route is now /network/conditions), so leaving it in the graph "+
			"is the ghost this pass exists to remove",
			ghost.Kind, ghost.Name, ghost.SourceFile)
	}
	if res.removedIDs[foreign.ID] {
		t.Errorf("#6461: the pass pruned %s|%s@%s (pattern_type=http_endpoint_synthesis), which it "+
			"does NOT own and cannot re-derive. Prune only entities carrying pattern_type %q or %q — "+
			"anything else is a live endpoint from another pass and deleting it loses it until the "+
			"next full reindex.",
			foreign.Kind, foreign.Name, foreign.SourceFile,
			composedNestedPatternType, composedMountPatternType)
	}
	if res.removedIDs[fastapiMount.ID] {
		t.Errorf("#6528: the pass pruned %s|%s@%s — a FastAPI mount synthetic (framework=fastapi) "+
			"carrying the SAME pattern_type %q this pass emits. Two producers stamp that string "+
			"(fastapiMountPointSynthetics, #6385, and ApplyDjangoNestedURLConf), so pattern_type "+
			"alone is not ownership: `framework` is. This pass cannot re-derive a FastAPI mount, "+
			"so pruning it deletes it until the next full reindex.",
			fastapiMount.Kind, fastapiMount.Name, fastapiMount.SourceFile, composedMountPatternType)
	}
	if res.removedIDs[unlabelled.ID] {
		t.Errorf("#6530: the pass pruned %s|%s@%s — pattern_type %q with NO `framework` property. "+
			"This pass ALWAYS stamps one, so its absence means a different producer, and there is "+
			"no other property left to tell them apart. Absence must therefore read as NOT OURS: "+
			"the cost of declining is a stale endpoint for one tick, the cost of claiming is a "+
			"deletion nothing on this path re-derives.",
			unlabelled.Kind, unlabelled.Name, unlabelled.SourceFile, composedMountPatternType)
	}
	if res.removedIDs[handler.ID] {
		t.Errorf("#6461: the pass pruned the HANDLER entity %s|%s@%s; it owns endpoint entities only",
			handler.Kind, handler.Name, handler.SourceFile)
	}

	var names []string
	for _, e := range res.added {
		names = append(names, e.Name)
	}
	found := false
	for _, n := range names {
		if n == "http:ANY:/network/conditions" {
			found = true
		}
	}
	if !found {
		t.Errorf("#6461: the recomputation did not produce the composed endpoint "+
			"http:ANY:/network/conditions for path(\"network/\", include(\"mpsite.urls\")) + "+
			"path(\"conditions/\", views.mp_terms_view); added=%v", names)
	}
}

// TestRecomposeDjangoURLConf_ReadFailureSkipsThePrune pins the failure policy
// (#6528 review).
//
// The file reader's contract is "nil means unavailable", and
// ApplyDjangoNestedURLConf reads nil as "this file declares no routes". Those
// two are indistinguishable at the seam, so one transient read failure on a
// root urls.py produces an EMPTY composition while the graph still holds every
// endpoint composed under it — and a reconciliation that trusts absence then
// deletes all of them.
//
// The failure is injected the way the daemon would actually meet it: a path
// that walkSourceFiles reported but that cannot be read when the pass gets to
// it. Deterministic, no chmod, no root-vs-user divergence.
//
// The prune must stand down. The ADDS are unaffected on purpose — they came
// from files that did read, so they are real either way.
func TestRecomposeDjangoURLConf_ReadFailureSkipsThePrune(t *testing.T) {
	absRepo, allFiles := dcWriteRepo(t)
	// Reported by the walk, unreadable by the time the pass asks for it.
	// `*urls.py`, so ApplyDjangoNestedURLConf scans it as a root.
	allFiles = append(allFiles, "mpsite/vanished_urls.py")

	ghost := dcEntity(dcEndpointKind, "http:ANY:/network/terms", "mpsite/views.py", map[string]string{
		"pattern_type": composedNestedPatternType,
		"path":         "/network/terms",
		"verb":         "ANY",
		"framework":    "django",
	})
	handler := dcEntity("SCOPE.Operation", "mp_terms_view", "mpsite/views.py", nil)

	doc := &graph.Document{
		Repo:     dcRepoTag,
		Entities: []graph.Entity{ghost, handler},
	}

	res := recomposeDjangoURLConf(absRepo, allFiles, []string{"mpsite/urls.py"}, doc, nil, dcSilentLogger())

	if !res.ran {
		t.Fatalf("#6528: the pass declined to run; allFiles=%v", allFiles)
	}
	if len(res.removedIDs) != 0 {
		var got []string
		for id := range res.removedIDs {
			got = append(got, id)
		}
		t.Errorf("#6528: a source read FAILED during this pass, so the recomputed composition is "+
			"not authoritative and the prune must be skipped entirely; it removed %d entity/entities "+
			"%v. Deleting real graph because a file was briefly unreadable is strictly worse than "+
			"carrying a stale endpoint for one tick — the stale one is corrected on the next pass, "+
			"the deleted one is not, until a full reindex.",
			len(res.removedIDs), got)
	}
	// The sibling test proves this same ghost IS pruned when every read
	// succeeds, so the assertion above is a policy difference and not a pass
	// that simply never prunes anything.
}

// TestRecomposeDjangoURLConf_ReAddsNothingTheGraphAlreadyHolds pins the add
// predicate.
//
// Reconciliation is idempotent by contract: when the graph already holds
// exactly what the recomputation produces, the pass must return an EMPTY add
// set. It runs on every daemon tick that touches a .py file, so an add that
// ignores what is already there duplicates every composed endpoint once per
// tick — the #6033 shape, one level up.
//
// Mutant this kills: dropping the `present[...]` guard in the add loop.
func TestRecomposeDjangoURLConf_ReAddsNothingTheGraphAlreadyHolds(t *testing.T) {
	absRepo, allFiles := dcWriteRepo(t)

	composed := dcEntity(dcEndpointKind, "http:ANY:/network/conditions", "mpsite/views.py", map[string]string{
		"pattern_type": composedNestedPatternType,
		"path":         "/network/conditions",
		"verb":         "ANY",
		"framework":    "django",
	})
	mount := dcEntity(dcEndpointKind, "http:ANY:/network:mount", "mpproj/urls.py", map[string]string{
		"pattern_type": composedMountPatternType,
		"path":         "/network",
		"verb":         "ANY",
		"framework":    "django",
	})
	handler := dcEntity("SCOPE.Operation", "mp_terms_view", "mpsite/views.py", nil)

	doc := &graph.Document{
		Repo:     dcRepoTag,
		Entities: []graph.Entity{composed, mount, handler},
	}

	res := recomposeDjangoURLConf(absRepo, allFiles, []string{"mpsite/urls.py"}, doc, nil, dcSilentLogger())

	if !res.ran {
		t.Fatalf("#6461: the pass declined to run; allFiles=%v", allFiles)
	}
	if len(res.added) != 0 {
		var got []string
		for _, e := range res.added {
			got = append(got, e.Kind+"|"+e.Name+"@"+e.SourceFile)
		}
		t.Errorf("#6461: the graph already holds every entity the recomputation produces, so the add "+
			"set must be EMPTY; got %d: %v. This pass runs on every daemon tick that touches a .py "+
			"file, so an unguarded add duplicates each composed endpoint once per tick.",
			len(res.added), got)
	}
	if len(res.removedIDs) != 0 {
		var got []string
		for id := range res.removedIDs {
			got = append(got, id)
		}
		t.Errorf("#6461: nothing is stale in this graph, so the prune set must be EMPTY; got %v", got)
	}
	if len(res.addedRels) != 0 {
		t.Errorf("#6461: no entity was added, so no IMPLEMENTS bridge should be emitted; got %d edge(s). "+
			"An edge whose target already exists is already in the graph and re-adding it duplicates the row.",
			len(res.addedRels))
	}
}

// dcScopeRepo lays down TWO mounted apps that share a last route segment:
//
//	mpproj/urls.py   network/ -> mpsite.urls      api/ -> apiapp.urls
//	mpsite/urls.py   path("terms/", views.mp_terms_view)       -> /network/terms
//	apiapp/urls.py   router.register(r"terms",  TermViewSet)   -> /api/terms
//	                 router.register(r"legacy", TermViewSet)   -> /api/legacy
//
// The composed paths share last segments ACROSS mounts. That is the whole
// point: `users`, `items`, `orders`, `terms` collide constantly between apps in
// a monorepo.
//
// TWO registrations, not one, so a scope bug is caught on BOTH sides:
// `/network/terms` exercises the ADD side (an unrelated route that must still
// be composed) and a stale `/network/legacy` exercises the PRUNE side (an
// unrelated ghost that must still be removed).
func dcScopeRepo(t *testing.T) (absRepo string, allFiles []string) {
	t.Helper()
	absRepo = t.TempDir()
	files := map[string]string{
		"mpproj/urls.py": "from django.urls import include, path\n\n" +
			"urlpatterns = [\n" +
			"    path(\"network/\", include(\"mpsite.urls\")),\n" +
			"    path(\"api/\", include(\"apiapp.urls\")),\n" +
			"]\n",
		"mpsite/urls.py": "from django.urls import path\n\n" +
			"from . import views\n\n" +
			"urlpatterns = [\n    path(\"terms/\", views.mp_terms_view),\n]\n",
		"mpsite/views.py": "def mp_terms_view(request):\n    return {\"ok\": 1}\n",
		"apiapp/urls.py": "from rest_framework import routers, viewsets\n\n\n" +
			"class TermViewSet(viewsets.ModelViewSet):\n    queryset = []\n\n\n" +
			"router = routers.DefaultRouter()\n" +
			"router.register(r\"terms\", TermViewSet)\n" +
			"router.register(r\"legacy\", TermViewSet)\n\n" +
			"urlpatterns = router.urls\n",
	}
	for rel, body := range files {
		abs := filepath.Join(absRepo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
		allFiles = append(allFiles, rel)
	}
	sort.Strings(allFiles)
	return absRepo, allFiles
}

// TestDRFUnknownComposedPaths_ScopedToTheRegisteringMount pins the SCOPE axis
// of the #6528 abstention (#6530 review).
//
// The first version of this collected the bare registered segment (`/terms`)
// and matched it as a suffix repo-wide, so a registration in `apiapp/` silently
// suppressed an unrelated `path("terms/", ...)` in `mpsite/` mounted at
// `/network/` — re-creating the #6461 ghost on a last-segment name collision.
// Containment was never the problem; SCOPE was, and the old containment test
// could not see it because both axes were being described as one "narrowness"
// claim.
//
// The set must contain the registering mount's composed path and NOTHING else.
func TestDRFUnknownComposedPaths_ScopedToTheRegisteringMount(t *testing.T) {
	absRepo, allFiles := dcScopeRepo(t)
	reader := dcDiskReader(absRepo, allFiles)

	fresh := engine.ApplyDjangoNestedURLConf(allFiles, reader)
	var composed []string
	for i := range fresh {
		if p := composedRecordPath(&fresh[i]); p != "" {
			composed = append(composed, p)
		}
	}
	sort.Strings(composed)
	// Guard the fixture itself: if the composition stopped producing both
	// paths, the assertions below would pass vacuously.
	if len(composed) != 3 ||
		composed[0] != "/api/legacy" || composed[1] != "/api/terms" || composed[2] != "/network/terms" {
		t.Fatalf("#6530 fixture: expected the tree to compose exactly "+
			"[/api/legacy /api/terms /network/terms], got %v — the scope assertions below would "+
			"prove nothing otherwise", composed)
	}

	unknown := drfUnknownComposedPaths(allFiles, []string{"mpsite/urls.py", "apiapp/urls.py"}, fresh, reader)

	if !unknown["/api/terms"] {
		t.Errorf("#6528: /api/terms is composed from router.register(\"terms\") in apiapp/urls.py, "+
			"which changed this tick, so its drf_router_expanded coverage was pruned and is "+
			"UNKNOWN; it must be in the abstention set. Got %v", unknown)
	}
	if unknown["/network/terms"] {
		t.Errorf("#6530: /network/terms is composed from a plain path() in mpsite/urls.py and has "+
			"NOTHING to do with apiapp's registration — it only shares a last segment. Marking it "+
			"unknown suppresses both its add and its prune, which re-creates the #6461 ghost this "+
			"pass exists to remove. The set must be keyed on the FULL COMPOSED path, not on a bare "+
			"/segment matched repo-wide. Got %v", unknown)
	}
	if !unknown["/api/legacy"] {
		t.Errorf("#6528: /api/legacy is composed from the second registration in the same changed "+
			"file and must be in the abstention set too. Got %v", unknown)
	}
	if len(unknown) != 2 {
		t.Errorf("#6530: expected exactly the 2 registering mount's paths, got %d: %v",
			len(unknown), unknown)
	}

	// NARROWNESS on the other axis, unchanged from #6528: a changed set with no
	// registration in it abstains from nothing, and neither does an empty one.
	if n := len(drfUnknownComposedPaths(allFiles, []string{"mpsite/urls.py"}, fresh, reader)); n != 0 {
		t.Errorf("#6528: only mpsite/urls.py changed and it declares no router.register(); "+
			"%d abstention path(s) produced, want 0", n)
	}
	if n := len(drfUnknownComposedPaths(allFiles, nil, fresh, reader)); n != 0 {
		t.Errorf("#6528: an empty changed set produced %d abstention path(s); coverage is only "+
			"unknown for files whose entities were pruned THIS tick", n)
	}
}

// TestDRFRouterRegisterRe_MatchesEngineSpellings pins the copied regex against
// engine's djangoRouterRegisterRe. The two must recognise the same
// registrations: engine's decides which routes get COMPOSED, this one decides
// which composed routes must ABSTAIN. A spelling engine matches and this one
// does not is the #6528 hole reopened silently.
func TestDRFRouterRegisterRe_MatchesEngineSpellings(t *testing.T) {
	shouldMatch := []string{
		`router.register(r"widgets", WidgetViewSet)`,
		`api_router.register("gadgets", GadgetViewSet)`,
		`v2_router.register(r'things', ThingViewSet)`,
		`router_v3.register("doohickeys", DooViewSet)`,
		`my_router.register(r"sprockets", SprocketViewSet)`,
	}
	for _, src := range shouldMatch {
		if !drfRouterRegisterRe.MatchString(src) {
			t.Errorf("#6528: drfRouterRegisterRe does not match %q, but engine's "+
				"djangoRouterRegisterRe does — a route composed from it would be added with no "+
				"abstention", src)
		}
	}
	shouldNotMatch := []string{
		`urlpatterns = [path("terms/", views.terms)]`,
		`registry.register("not-a-router", X)`,
	}
	for _, src := range shouldNotMatch {
		if drfRouterRegisterRe.MatchString(src) {
			t.Errorf("#6528: drfRouterRegisterRe matches %q, which is not a DRF router "+
				"registration; abstaining on it suppresses composition for no reason", src)
		}
	}
}

// TestPathIsUnknownCoverage_ExactPathNotSuffix pins the matcher's shape after
// #6530. The set now carries FULL COMPOSED paths, so the match is equality: a
// suffix or containment test here would re-introduce the repo-wide scope bug
// one level down, no matter how the set was computed.
func TestPathIsUnknownCoverage_ExactPathNotSuffix(t *testing.T) {
	unknown := map[string]bool{"/api/terms": true}
	cases := []struct {
		path string
		want bool
	}{
		{"/api/terms", true},
		{"/network/terms", false}, // same last segment, DIFFERENT mount — #6530
		{"/terms", false},
		{"/api/terms/detail", false},
		{"/api/terms-archive", false},
	}
	for _, tc := range cases {
		e := dcEntity(dcEndpointKind, "http:ANY:"+tc.path, "x/urls.py", map[string]string{"path": tc.path})
		if got := pathIsUnknownCoverage(&e, unknown); got != tc.want {
			t.Errorf("#6530: pathIsUnknownCoverage(%q, %v) = %t, want %t — the abstention set holds "+
				"full composed paths, so the match must be EQUALITY", tc.path, unknown, got, tc.want)
		}
	}
	if pathIsUnknownCoverage(&graph.Entity{}, unknown) {
		t.Errorf("#6530: an entity with no `path` property must not match")
	}
	e := dcEntity(dcEndpointKind, "http:ANY:/api/terms", "x/urls.py", map[string]string{"path": "/api/terms"})
	if pathIsUnknownCoverage(&e, nil) {
		t.Errorf("#6530: with an empty abstention set, nothing may match")
	}
}

// TestRecomposeDjangoURLConf_UnrelatedRouteStillPrunesAcrossMounts is the
// #6530 review's probe, at the seam it was run against.
//
// Two registering-or-not files change together, and an unrelated app's stale
// composed route shares a last segment with the registration. Before the fix,
// `removed=map[]` — the ghost survived, which is #6461 verbatim. It must prune.
func TestRecomposeDjangoURLConf_UnrelatedRouteStillPrunesAcrossMounts(t *testing.T) {
	absRepo, allFiles := dcScopeRepo(t)

	// Stale composition on the UNCHANGED handler file: the route under
	// /network/ used to be `legacy/` and is now `terms/`, so this is the #6461
	// ghost. Its last segment DELIBERATELY collides with apiapp's
	// `router.register(r"legacy")`, which is what a bare-segment abstention
	// matches on — so a scope bug shows up here as the ghost SURVIVING.
	ghost := dcEntity(dcEndpointKind, "http:ANY:/network/legacy", "mpsite/views.py",
		map[string]string{
			"pattern_type": composedNestedPatternType,
			"path":         "/network/legacy",
			"verb":         "ANY",
			"framework":    "django",
		})
	handler := dcEntity("SCOPE.Operation", "mp_terms_view", "mpsite/views.py", nil)
	doc := &graph.Document{Repo: dcRepoTag, Entities: []graph.Entity{ghost, handler}}

	// BOTH files change: the plain-path app AND the registering app.
	res := recomposeDjangoURLConf(absRepo, allFiles,
		[]string{"mpsite/urls.py", "apiapp/urls.py"}, doc, nil, dcSilentLogger())

	if !res.ran {
		t.Fatalf("#6530: the pass declined to run; allFiles=%v", allFiles)
	}
	if !res.removedIDs[ghost.ID] {
		t.Errorf("#6530: the ghost %s|%s@%s was NOT pruned. apiapp/urls.py registers `legacy` "+
			"under the /api mount, and an abstention keyed on that BARE SEGMENT matches "+
			"/network/legacy too — a different mount, a different app, an unrelated plain "+
			"path(). Suppressing its prune re-creates the exact #6461 ghost this pass exists to "+
			"remove. The set must hold FULL COMPOSED paths. removedIDs=%v",
			ghost.Kind, ghost.Name, ghost.SourceFile, res.removedIDs)
	}
	// The abstention must still hold where it IS warranted, or this test would
	// pass just as well against a fix that deleted the feature.
	for _, e := range res.added {
		if p, _ := e.PropLookup("path"); p == "/api/terms" {
			t.Errorf("#6528: /api/terms was ADDED even though apiapp/urls.py's registration " +
				"changed this tick and its coverage is unknown; the abstention must still apply " +
				"to the registering mount's own routes")
		}
	}
	// ...and the unrelated route must be composed normally.
	sawNetworkTerms := false
	for _, e := range res.added {
		if p, _ := e.PropLookup("path"); p == "/network/terms" {
			sawNetworkTerms = true
		}
	}
	if !sawNetworkTerms {
		var got []string
		for _, e := range res.added {
			p, _ := e.PropLookup("path")
			got = append(got, p)
		}
		t.Errorf("#6530: /network/terms was not composed. It comes from a plain path() in a file "+
			"with no registration, so nothing about apiapp's DRF router may suppress it. added=%v", got)
	}
}

// TestRecomposeDjangoURLConf_UnknownDRFCoverageAlsoBlocksThePrune pins the
// OTHER half of the #6528 abstention, which is not symmetry for its own sake.
//
// Suppressing the ADD keeps those records out of `res.added`, but they are
// still in `freshEnts`, so on its own the prune is unaffected. The hazard is
// the case where the graph's copy of a suppressed route has a DIFFERENT id from
// the one this pass computes — the full path handler-resolves a composed
// endpoint onto the handler's file (#2678), and the two need not agree once the
// DRF entities that carried that handler have been pruned. The graph's copy is
// then absent from `freshIDs` and reads as stale, so an abstention covering
// only the add side would DELETE the very route it just declined to re-derive:
// suppressed on one side, deleted on the other, net loss.
//
// Mutant this kills: applying the abstention to the add loop only.
func TestRecomposeDjangoURLConf_UnknownDRFCoverageAlsoBlocksThePrune(t *testing.T) {
	absRepo := t.TempDir()
	files := map[string]string{
		"mpproj/urls.py": "from django.urls import include, path\n\n" +
			"urlpatterns = [\n    path(\"network/\", include(\"mpsite.urls\")),\n]\n",
		"mpsite/urls.py": "from rest_framework import routers, viewsets\n\n\n" +
			"class WidgetViewSet(viewsets.ModelViewSet):\n    queryset = []\n\n\n" +
			"router = routers.DefaultRouter()\n" +
			"router.register(r\"widgets\", WidgetViewSet)\n\n" +
			"urlpatterns = router.urls\n",
	}
	var allFiles []string
	for rel, body := range files {
		abs := filepath.Join(absRepo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
		allFiles = append(allFiles, rel)
	}

	// The graph's copy of the composed route, attributed to a DIFFERENT file
	// from the one this pass's recomputation lands it on. Different SourceFile
	// therefore different graph.EntityID, so it is absent from freshIDs and
	// reads as stale.
	//
	// HONEST ABOUT ITS OWN EVIDENCE (#6530 review): the divergence here is
	// CONSTRUCTED by attribution, not OBSERVED from a real resolution flip. The
	// file is a real one in this fixture, and the shape is the one a full index
	// leaves behind — bridgeEndpointToHandler rebinds a composed endpoint onto
	// the handler's file (#2678), so the graph's copy and this pass's copy can
	// legitimately disagree once the DRF entities that carried that handler have
	// been pruned. What is not demonstrated is an end-to-end tick where they
	// actually do. The argument is structural rather than measured, and the
	// worst case of keeping the prune-side abstention is one stale tick.
	existing := dcEntity(dcEndpointKind, "http:ANY:/network/widgets", "mpsite/urls.py",
		map[string]string{
			"pattern_type": composedNestedPatternType,
			"path":         "/network/widgets",
			"verb":         "ANY",
			"framework":    "django",
		})
	doc := &graph.Document{Repo: dcRepoTag, Entities: []graph.Entity{existing}}

	res := recomposeDjangoURLConf(absRepo, allFiles, []string{"mpsite/urls.py"}, doc, nil, dcSilentLogger())

	if !res.ran {
		t.Fatalf("#6528: the pass declined to run; allFiles=%v", allFiles)
	}
	if res.removedIDs[existing.ID] {
		t.Errorf("#6528: the pass PRUNED %s|%s@%s while abstaining from re-deriving it. "+
			"mpsite/urls.py declares a router.register for widgets, so coverage under /widgets is "+
			"UNKNOWN this tick and the add was suppressed; suppressing the add while allowing the "+
			"prune deletes the route outright. The abstention must cover BOTH sides.",
			existing.Kind, existing.Name, existing.SourceFile)
	}
	for _, e := range res.added {
		if p, _ := e.PropLookup("path"); p == "/network/widgets" {
			t.Errorf("#6528: the pass ADDED %s@%s under an unknown-coverage prefix; "+
				"the add must be suppressed", e.Name, e.SourceFile)
		}
	}
}

// dcTwoRouterRepo lays down a monorepo where TWO apps register DRF routers
// under two different mounts. It is the fixture the changed-files scoping of
// drfUnknownComposedPaths is actually observable on: with only ONE app's
// urls.py in the changed set, the OTHER app's registration must keep composing.
//
// dcScopeRepo cannot see that axis — it has exactly one registering file and
// that file is always in the changed set, so "strip the changed files" and
// "strip every file" are byte-identical there.
func dcTwoRouterRepo(t *testing.T) (absRepo string, allFiles []string) {
	t.Helper()
	absRepo = t.TempDir()
	viewset := func(name string) string {
		return "from rest_framework import routers, viewsets\n\n\n" +
			"class " + name + "ViewSet(viewsets.ModelViewSet):\n    queryset = []\n\n\n" +
			"router = routers.DefaultRouter()\n"
	}
	files := map[string]string{
		"mpproj/urls.py": "from django.urls import include, path\n\n" +
			"urlpatterns = [\n" +
			"    path(\"api/\", include(\"apiapp.urls\")),\n" +
			"    path(\"other/\", include(\"otherapp.urls\")),\n" +
			"]\n",
		"apiapp/urls.py": viewset("Gadget") +
			"router.register(r\"gadgets\", GadgetViewSet)\n\n" +
			"urlpatterns = router.urls\n",
		"otherapp/urls.py": viewset("Widget") +
			"router.register(r\"widgets\", WidgetViewSet)\n\n" +
			"urlpatterns = router.urls\n",
	}
	for rel, body := range files {
		abs := filepath.Join(absRepo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
		allFiles = append(allFiles, rel)
	}
	sort.Strings(allFiles)
	return absRepo, allFiles
}

// TestDRFUnknownComposedPaths_StripsOnlyTheChangedFiles pins the CHANGED-FILES
// scoping of the differential counterfactual (#6528 round-4 mutant P1).
//
// The counterfactual composition is run with `router.register(...)` removed
// from the CHANGED files only. Drop the `!registering[relPath] ||` guard in
// strippedReader and it is removed from EVERY file instead: `survives`
// collapses to the routes no router produced, every DRF-composed path in the
// repo falls into `unknown`, and the pass abstains from pruning composed paths
// whose DRF coverage was never in question. Abstaining too much is exactly how
// the #6461 ghost is left in place, which is the defect this pass exists to
// remove.
//
// Both the doc comment and the commit message state the scoping; before this
// test nothing observed it, and the mutant was measured SURVIVING both
// ./internal/extractors/ and the cmd/grafel Django arc.
func TestDRFUnknownComposedPaths_StripsOnlyTheChangedFiles(t *testing.T) {
	absRepo, allFiles := dcTwoRouterRepo(t)
	reader := dcDiskReader(absRepo, allFiles)

	fresh := engine.ApplyDjangoNestedURLConf(allFiles, reader)
	var composed []string
	for i := range fresh {
		if p := composedRecordPath(&fresh[i]); p != "" {
			composed = append(composed, p)
		}
	}
	sort.Strings(composed)
	// Guard the fixture: without BOTH router-composed paths the assertions
	// below would hold vacuously.
	if len(composed) != 2 || composed[0] != "/api/gadgets" || composed[1] != "/other/widgets" {
		t.Fatalf("#6528 fixture: expected the tree to compose exactly "+
			"[/api/gadgets /other/widgets], got %v — the scoping assertions below would prove "+
			"nothing otherwise", composed)
	}

	// Only apiapp/urls.py changed. otherapp/urls.py is untouched, so its
	// drf_router_expanded entities were never pruned and its coverage is
	// perfectly observable.
	unknown := drfUnknownComposedPaths(allFiles, []string{"apiapp/urls.py"}, fresh, reader)

	if !unknown["/api/gadgets"] {
		t.Errorf("#6528: /api/gadgets is composed from the registration in the CHANGED "+
			"apiapp/urls.py, so its coverage is unknown this tick and it must be in the "+
			"abstention set. Got %v", unknown)
	}
	if unknown["/other/widgets"] {
		t.Errorf("#6528: /other/widgets is composed from a registration in otherapp/urls.py, "+
			"which did NOT change — its drf_router_expanded coverage was never pruned and is not "+
			"in question. The counterfactual must strip router.register() from the CHANGED files "+
			"only; stripping it repo-wide marks every DRF-composed path unknown and suppresses "+
			"the prune that removes the #6461 ghost. Got %v", unknown)
	}
	if len(unknown) != 1 {
		t.Errorf("#6528: expected exactly the changed app's 1 composed path, got %d: %v",
			len(unknown), unknown)
	}

	// Symmetric control: change the OTHER app instead and the abstention moves
	// with it, so the assertion above is about scoping and not about which
	// mount happens to sort first.
	flipped := drfUnknownComposedPaths(allFiles, []string{"otherapp/urls.py"}, fresh, reader)
	if !flipped["/other/widgets"] || flipped["/api/gadgets"] || len(flipped) != 1 {
		t.Errorf("#6528: with only otherapp/urls.py changed the abstention set must be exactly "+
			"{/other/widgets}; got %v", flipped)
	}
}

// TestRecomposeDjangoURLConf_UnchangedRouterAppStillReconciles is the same
// mutant seen through the whole pass rather than at the helper's seam.
//
// A ghost under the UNCHANGED router app must still be pruned, and that app's
// live composed route must still be added. Under the repo-wide strip both are
// suppressed: the route is neither re-derived nor its ghost removed, which is
// #6461 verbatim on an app the edit never touched.
func TestRecomposeDjangoURLConf_UnchangedRouterAppStillReconciles(t *testing.T) {
	absRepo, allFiles := dcTwoRouterRepo(t)

	// The graph's copy of /other/widgets, attributed to a different file from
	// the one this pass's recomputation lands it on — the shape #2678's handler
	// re-attribution leaves behind. Different SourceFile, therefore a different
	// graph.EntityID, therefore absent from freshIDs and stale.
	ghost := dcEntity(dcEndpointKind, "http:ANY:/other/widgets", "otherapp/views.py",
		map[string]string{
			"pattern_type": composedNestedPatternType,
			"path":         "/other/widgets",
			"verb":         "ANY",
			"framework":    "django",
		})
	doc := &graph.Document{Repo: dcRepoTag, Entities: []graph.Entity{ghost}}

	res := recomposeDjangoURLConf(absRepo, allFiles,
		[]string{"apiapp/urls.py"}, doc, nil, dcSilentLogger())

	if !res.ran {
		t.Fatalf("#6528: the pass declined to run; allFiles=%v", allFiles)
	}
	if !res.removedIDs[ghost.ID] {
		t.Errorf("#6528: the stale composed endpoint %s|%s@%s was NOT pruned. It belongs to "+
			"otherapp, whose urls.py did not change, so its DRF coverage is observable and the "+
			"abstention must not cover it. Stripping router.register() repo-wide in the "+
			"counterfactual marks /other/widgets unknown and leaves the #6461 ghost in place. "+
			"removedIDs=%v", ghost.Kind, ghost.Name, ghost.SourceFile, res.removedIDs)
	}
	sawOtherWidgets := false
	for _, e := range res.added {
		if p, _ := e.PropLookup("path"); p == "/other/widgets" {
			sawOtherWidgets = true
		}
	}
	if !sawOtherWidgets {
		var got []string
		for _, e := range res.added {
			p, _ := e.PropLookup("path")
			got = append(got, p)
		}
		t.Errorf("#6528: /other/widgets was not re-composed. otherapp/urls.py did not change, so "+
			"nothing about apiapp's registration may suppress its add. added=%v", got)
	}
	// The abstention must still hold where it IS warranted, or this test would
	// pass against a fix that simply deleted the feature.
	for _, e := range res.added {
		if p, _ := e.PropLookup("path"); p == "/api/gadgets" {
			t.Errorf("#6528: /api/gadgets was ADDED even though apiapp/urls.py changed this tick " +
				"and its drf_router_expanded coverage was pruned; the abstention must still apply " +
				"to the registering app's own routes")
		}
	}
}
