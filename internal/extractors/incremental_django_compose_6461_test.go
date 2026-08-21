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
	handler := dcEntity("SCOPE.Operation", "mp_terms_view", "mpsite/views.py", nil)

	doc := &graph.Document{
		Repo:     dcRepoTag,
		Entities: []graph.Entity{ghost, foreign, fastapiMount, handler},
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

// TestDRFUnknownCoveragePaths_MatchesRouterRegisterSpellings pins
// drfRouterRegisterRe against the engine regex it is a copy of
// (djangoRouterRegisterRe, internal/engine/django_urlconf_nested.go).
//
// The two MUST recognise the same registrations. engine's decides which routes
// get COMPOSED; this one decides which composed routes must ABSTAIN for want of
// coverage. A spelling engine matches and this one does not is precisely the
// hole the #6528 fix closes, reopened silently — the route would be composed
// and added with no abstention.
//
// It also pins the abstention's NARROWNESS: only Python files, only files that
// actually changed, and nothing at all when no registration is present.
func TestDRFUnknownCoveragePaths_MatchesRouterRegisterSpellings(t *testing.T) {
	files := map[string]string{
		"a/urls.py":     `router.register(r"widgets", WidgetViewSet)`,
		"b/urls.py":     `api_router.register("gadgets", GadgetViewSet)`,
		"c/urls.py":     `v2_router.register(r'/things/', ThingViewSet)`,
		"d/urls.py":     `router_v3.register("doohickeys", DooViewSet)`,
		"e/urls.py":     `my_router.register(r"sprockets", SprocketViewSet)`,
		"plain/urls.py": `urlpatterns = [path("terms/", views.terms)]`,
		"notpy.txt":     `router.register(r"ignored", X)`,
	}
	reader := func(rel string) []byte {
		if b, ok := files[rel]; ok {
			return []byte(b)
		}
		return nil
	}

	var changed []string
	for rel := range files {
		changed = append(changed, rel)
	}
	sort.Strings(changed)

	got := drfUnknownCoveragePaths(changed, reader)
	sort.Strings(got)
	want := []string{"/doohickeys", "/gadgets", "/sprockets", "/things", "/widgets"}
	if len(got) != len(want) {
		t.Fatalf("#6528: drfUnknownCoveragePaths returned %v, want %v. Every spelling engine's "+
			"djangoRouterRegisterRe accepts must be recognised here too, or a composed route "+
			"under it is added with no abstention.", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("#6528: drfUnknownCoveragePaths[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}

	// NARROWNESS. A repo whose changed files declare no registration must
	// abstain from nothing — otherwise the fix suppresses live composition
	// everywhere and the ordinary layout silently loses routes.
	if n := len(drfUnknownCoveragePaths([]string{"plain/urls.py"}, reader)); n != 0 {
		t.Errorf("#6528: a changed file with no router.register() produced %d abstention path(s); "+
			"it must produce none, or the fix suppresses composition it has no reason to doubt", n)
	}
	// A file that did NOT change is not evidence: its drf_router_expanded
	// entities were never pruned, so its coverage is still visible in the graph.
	if n := len(drfUnknownCoveragePaths(nil, reader)); n != 0 {
		t.Errorf("#6528: an empty changed set produced %d abstention path(s); coverage is only "+
			"unknown for files whose entities were pruned THIS tick", n)
	}
}

// TestPathMatchesAnySuffix_IsSuffixNotContainment pins the matcher's shape.
//
// ApplyDjangoNestedURLConf composes the registered prefix as the LAST segment
// of the list route (`/api` + `widgets` -> `/api/widgets`), so a SUFFIX test
// names exactly the routes under that registration. A containment test would
// additionally abstain on `/widgets/detail` and on any unrelated path that
// merely mentions the word — suppressing live composition on evidence that says
// nothing about it.
func TestPathMatchesAnySuffix_IsSuffixNotContainment(t *testing.T) {
	sfx := []string{"/widgets"}
	cases := []struct {
		path string
		want bool
	}{
		{"/api/widgets", true},
		{"/widgets", true},
		{"/api/widgets-archive", false}, // sibling registration, different route
		{"/widgets/detail", false},      // composed under, not the list route
		{"/api/terms", false},
	}
	for _, tc := range cases {
		e := dcEntity(dcEndpointKind, "http:ANY:"+tc.path, "x/urls.py", map[string]string{"path": tc.path})
		if got := pathMatchesAnySuffix(&e, sfx); got != tc.want {
			t.Errorf("#6528: pathMatchesAnySuffix(%q, %v) = %t, want %t — the match must be a "+
				"SUFFIX on the registered prefix, not containment", tc.path, sfx, got, tc.want)
		}
	}
	if pathMatchesAnySuffix(&graph.Entity{}, sfx) {
		t.Errorf("#6528: an entity with no `path` property must not match")
	}
	e := dcEntity(dcEndpointKind, "http:ANY:/api/widgets", "x/urls.py", map[string]string{"path": "/api/widgets"})
	if pathMatchesAnySuffix(&e, nil) {
		t.Errorf("#6528: with no abstention paths, nothing may match")
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

	// The graph's copy of the composed route, attributed the way a FULL index
	// left it — on the handler's file, which is NOT where this pass's
	// recomputation lands it. Different SourceFile therefore different
	// graph.EntityID, so it is absent from freshIDs and reads as stale.
	existing := dcEntity(dcEndpointKind, "http:ANY:/network/widgets", "mpsite/legacy_views.py",
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
