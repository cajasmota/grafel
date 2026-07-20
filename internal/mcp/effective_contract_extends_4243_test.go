package mcp

// effective_contract_extends_4243_test.go — regression coverage for #4243.
//
// THE BUG (verified live on acme-core): grafel_effective_contract is
// normally called with an entity_id. When that entity_id resolved to a DRF
// ViewSet that the Python extractor emits as Kind="View" with an EMPTY subtype
// (so isClassEntity is false for it), resolveEffectiveContractTarget matched
// neither the router-route branch nor the class-entity branch and fell through
// to the raw-leaf fallback. For a hex / "<repo>::<id>" entity_id that leaf is
// the id itself (no dot to split), so wantVS never matched any route's ViewSet
// name and the tool returned groups:null with a misleading "reindex may help"
// note — even though the router-expanded routes (and the ViewSet's EXTENDS edge
// to ModelViewSet) were present in the index all along.
//
// These tests reproduce that null result on the pre-fix resolver and assert the
// post-fix tool returns the per-verb contract for ALL THREE target forms a
// caller might pass: the bare class name, the LOCAL entity id, and the
// "<repo>::<localid>" prefixed entity id.

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// drfViewKindViewSetDoc mirrors the live acme-core shape: a JurisdictionViewSet
// emitted as Kind="View", Subtype="" (NOT a SCOPE.Component/class), an EXTENDS
// edge to viewsets.ModelViewSet (base_name carries the dotted attribute form the
// external synthesiser rewrites the ToID away from), and a couple of
// router-expanded routes attributed to it via drf_view_method.
func drfViewKindViewSetDoc() *graph.Document {
	route := func(id string, props map[string]string) graph.Entity {
		p := map[string]string{"pattern_type": "drf_router_expanded", "framework": "django"}
		for k, v := range props {
			p[k] = v
		}
		return graph.Entity{ID: id, Name: id, Kind: "http_endpoint", Language: "python"}.WithProperties(p)
	}
	return &graph.Document{
		Repo: "acme-core",
		Entities: []graph.Entity{
			// The ViewSet — Kind="View", empty subtype, exactly as the Python
			// extractor emits it on acme-core. isClassEntity is FALSE for it.
			{ID: "vs1", Name: "JurisdictionViewSet",
				QualifiedName: "core.views.jurisdiction_viewset.JurisdictionViewSet",
				Kind:          "View", Subtype: "", SourceFile: "core/views/jurisdiction_viewset.py",
				StartLine: 22, EndLine: 22, Language: "python"},
			// The pack-recognised base, rewritten to an ext: placeholder named
			// after the import module root — the live "ext:viewsets" shape.
			{ID: "ext:viewsets", Name: "viewsets", QualifiedName: "viewsets",
				Kind: "SCOPE.External", Language: "python"},
			// INHERITED create — the #278 contract, stamped by the engine.
			route("http:create", map[string]string{
				"verb": "POST", "path": "/api/v1/jurisdictions",
				"drf_view_method":          "JurisdictionViewSet.create",
				"effective_kind":           "inherited",
				"effective_source_class":   "CreateModelMixin",
				"effective_status":         "201",
				"effective_error_statuses": "400",
			}),
			// INHERITED list.
			route("http:list", map[string]string{
				"verb": "GET", "path": "/api/v1/jurisdictions",
				"drf_view_method":        "JurisdictionViewSet.list",
				"effective_kind":         "inherited",
				"effective_source_class": "ListModelMixin",
				"effective_status":       "200",
			}),
		},
		Relationships: []graph.Relationship{
			// The ViewSet EXTENDS edge — base_name is the dotted "viewsets.ModelViewSet"
			// the hierarchy extractor records (its leaf, ModelViewSet, is what the
			// baseknowledge pack matches), while ToID points at the ext: placeholder.
			graph.Relationship{FromID: "vs1", ToID: "ext:viewsets", Kind: "EXTENDS"}.WithProperties(map[string]string{"language": "python", "base_name": "viewsets.ModelViewSet"}),
		},
	}
}

// TestEffectiveContract_4243_ResolvesByEntityIDForms asserts the tool returns the
// per-verb contract for a Kind="View" DRF ViewSet whether the caller passes the
// bare class name, the local entity id, or the "<repo>::<id>" prefixed id.
//
// NON-VACUOUS: on the pre-fix resolver the two id forms returned groups:null
// (see TestEffectiveContract_4243_PreFixResolverReturnedNull below, which
// asserts the OLD raw-leaf behaviour that this fix removes).
func TestEffectiveContract_4243_ResolvesByEntityIDForms(t *testing.T) {
	srv := newTestServer(t, drfViewKindViewSetDoc())

	for _, target := range []string{
		"JurisdictionViewSet", // bare class name (already worked pre-fix)
		"vs1",                 // LOCAL entity id (returned null pre-fix)
		"acme-core::vs1",      // prefixed entity id (returned null pre-fix)
	} {
		t.Run(target, func(t *testing.T) {
			out := callEffectiveContract(t, srv, target)
			if len(out.Groups) != 1 {
				t.Fatalf("target %q: expected 1 group, got %d (note=%q)", target, len(out.Groups), out.Note)
			}
			g := out.Groups[0]
			if g.Class != "JurisdictionViewSet" {
				t.Errorf("target %q: group class = %q; want JurisdictionViewSet", target, g.Class)
			}
			if len(g.Handlers) != 2 {
				t.Fatalf("target %q: expected 2 per-verb handlers, got %d: %+v", target, len(g.Handlers), g.Handlers)
			}
			create := handlerByVerbPath(t, g, "POST", "/api/v1/jurisdictions")
			if create.Kind != "inherited" || create.SourceClass != "CreateModelMixin" || create.DefaultStatus != 201 {
				t.Errorf("target %q: create contract = {kind:%q source:%q status:%d}; want {inherited CreateModelMixin 201}",
					target, create.Kind, create.SourceClass, create.DefaultStatus)
			}
			if len(create.ErrorStatuses) != 1 || create.ErrorStatuses[0] != 400 {
				t.Errorf("target %q: create error_statuses = %v; want [400]", target, create.ErrorStatuses)
			}
			list := handlerByVerbPath(t, g, "GET", "/api/v1/jurisdictions")
			if list.Kind != "inherited" || list.DefaultStatus != 200 {
				t.Errorf("target %q: list contract = {kind:%q status:%d}; want {inherited 200}", target, list.Kind, list.DefaultStatus)
			}
		})
	}
}

// TestEffectiveContract_4243_PreFixResolverReturnedNull proves the bug was real
// and the fix is non-vacuous: it reconstructs the OLD resolution semantics
// (router-route OR class-entity branch only, then raw-leaf fallback) and shows
// that a Kind="View" ViewSet's local id resolved to a wantVS that matches NO
// route — the live groups:null. The current resolver (asserted above) fixes it.
func TestEffectiveContract_4243_PreFixResolverReturnedNull(t *testing.T) {
	doc := drfViewKindViewSetDoc()
	srv := newTestServer(t, doc)
	lg := srv.State.groups["test"]
	r := lg.Repos["acme-core"]

	// OLD resolveEffectiveContractTarget: only the router-route and class-entity
	// branches returned; everything else fell to the raw leaf. A Kind="View"
	// ViewSet matched neither branch.
	oldResolve := func(arg string) string {
		for _, e := range r.LabelIndex.LookupAll(arg) {
			if isRouterExpandedRoute(e) {
				if vs := viewSetNameForRoute(e); vs != "" {
					return strings.ToLower(vs)
				}
			}
			if isClassEntity(e) {
				return strings.ToLower(leafAfterDot(e.Name))
			}
		}
		return strings.ToLower(leafAfterDot(arg)) // raw-leaf fallback
	}

	wantVS := oldResolve("vs1")
	if wantVS == "jurisdictionviewset" {
		t.Fatalf("pre-fix resolver should NOT have resolved the View-kind ViewSet id to its name; got %q", wantVS)
	}
	// And that broken wantVS matches none of the routes — the null result.
	matched := 0
	for i := range r.Doc.Entities {
		e := &r.Doc.Entities[i]
		if isRouterExpandedRoute(e) && strings.ToLower(viewSetNameForRoute(e)) == wantVS {
			matched++
		}
	}
	if matched != 0 {
		t.Fatalf("pre-fix wantVS %q unexpectedly matched %d routes; the bug requires 0", wantVS, matched)
	}
}
