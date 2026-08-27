package mcp

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// ---------------------------------------------------------------------------
// #6472 arm 2 — serving-layer coverage for noiseLocalScope and component_prop.
//
// Before these tests, the ONLY thing observing Properties["local_scope"]=="true"
// was classifyNoise itself (denoise_test.go:131,144,157). Nothing asserted that
// the classification actually WITHHELD anything from grafel_find, and nothing
// asserted that a React `component_prop` stayed reachable. Both are user-facing capabilities
// that a "tidy up the resolver" change could have deleted with a green suite.
//
// Every test here asserts a POSITIVE CONTROL before any absence claim: these
// tests can all pass by finding nothing, and that is exactly the failure mode
// that let the gap persist.
// ---------------------------------------------------------------------------

// localScopeEntity builds a non-addressable function-body local of the shape the
// javascript dataflow extractor stamps (#1748): a destructure binding carrying
// Properties["local_scope"]="true".
func localScopeEntity(id, name, file string, line int) graph.Entity {
	return graph.Entity{
		ID: id, Name: name,
		Kind: "SCOPE.Component", Subtype: "const_destructure",
		SourceFile: file, StartLine: line,
		QualifiedName: "Widget." + name,
	}.WithProperties(map[string]string{
		"kind":        "SCOPE.Component",
		"subtype":     "const_destructure",
		"local_scope": "true",
	})
}

// componentPropEntity mirrors the record built by
// internal/extractors/javascript/dataflow_react.go's prop emitter, Properties
// map included.
//
// #6472 RE-DERIVED THIS FROM THE POST-CHANGE EMITTER: the prop now carries
// "local_scope":"true", stamped so internal/resolve's isLocalBindingKind can
// key on that property alone instead of a framework-name check. Every test
// below that asserts a prop is still returned by grafel_find is therefore
// observing classifyNoise's component_prop carve-out — remove the carve-out and
// they go red. Before this change the map had no local_scope and those
// assertions were free.
func componentPropEntity(id, prop, component, file string, line int) graph.Entity {
	return graph.Entity{
		ID: id, Name: prop,
		Kind: "SCOPE.Operation", Subtype: "component_prop",
		SourceFile: file, StartLine: line,
		QualifiedName: component + "." + prop,
	}.WithProperties(map[string]string{
		"kind":        "SCOPE.Operation",
		"subtype":     "component_prop",
		"component":   component,
		"prop":        prop,
		"framework":   "react",
		"local_scope": "true",
	})
}

// findNames runs grafel_find's structured shape and returns the returned names.
func findNames(t *testing.T, srv *Server, args map[string]any) map[string]bool {
	t.Helper()
	args["group"] = "test"
	args["full"] = true
	out := callDashboardTool(t, srv.handleQueryGraph, args)
	names := map[string]bool{}
	matches, _ := out["matches"].([]any)
	for _, m := range matches {
		obj, _ := m.(map[string]any)
		if n, ok := obj["name"].(string); ok {
			names[n] = true
		}
	}
	return names
}

// TestFind_BM25_LocalScopeEntityIsWithheld pins the de-noise `continue` in
// handleQueryGraph (tools.go ~720-730): a bm25 hit classified noiseLocalScope is
// dropped from the default ranked list and recovered by include_noise:true.
//
// Non-vacuity: the include_noise:true leg runs FIRST and asserts the local entity
// IS returned, so the absence assertion below cannot pass on an empty result set.
func TestFind_BM25_LocalScopeEntityIsWithheld(t *testing.T) {
	const file = "src/checkout/totals.jsx"
	srv := newTestServer(t, minDoc([]graph.Entity{
		// Real, addressable entity — must always be returned (control).
		{
			ID: "op1", Name: "computeCheckoutTotals", Kind: "SCOPE.Operation",
			SourceFile: file, StartLine: 10, QualifiedName: "computeCheckoutTotals",
		},
		// Non-addressable local binding — must be withheld by default.
		localScopeEntity("loc1", "subtotal", file, 48),
	}, nil))

	// Positive control: with include_noise:true BOTH entities are returned.
	// This proves the query textually reaches the local entity at all.
	withNoise := findNames(t, srv, map[string]any{"query": "totals", "include_noise": true})
	if !withNoise["subtotal"] {
		t.Fatalf("positive control failed: include_noise:true must return the local_scope entity, got %v", withNoise)
	}
	if !withNoise["computeCheckoutTotals"] {
		t.Fatalf("positive control failed: include_noise:true must return the real entity, got %v", withNoise)
	}

	// Default (include_noise unset): the local_scope entity is withheld, the
	// real one survives.
	def := findNames(t, srv, map[string]any{"query": "totals"})
	if !def["computeCheckoutTotals"] {
		t.Fatalf("non-vacuity: default find returned nothing real; got %v", def)
	}
	if def["subtotal"] {
		t.Errorf("default find must withhold the local_scope entity, got %v", def)
	}

	// Explicit include_noise:false behaves like the default.
	off := findNames(t, srv, map[string]any{"query": "totals", "include_noise": false})
	if !off["computeCheckoutTotals"] {
		t.Fatalf("non-vacuity: include_noise:false returned nothing real; got %v", off)
	}
	if off["subtotal"] {
		t.Errorf("include_noise:false must withhold the local_scope entity, got %v", off)
	}
}

// TestFind_KindFilter_LocalScopeEntityIsWithheld pins the SECOND, independent
// filter: the `if !includeNoise && isNoise(e) { return true }` skip inside
// enumerateByKind (tools.go ~1168). kind_filter turns find into an enumeration
// that folds in entities bm25 never surfaced, so it re-decides noise on its own
// and the tools.go:720 filter cannot cover it.
//
// The local entity is named so it does NOT textually match the query — it can
// only arrive via the enumeration fold-in, which is the branch under test.
func TestFind_KindFilter_LocalScopeEntityIsWithheld(t *testing.T) {
	srv := newTestServer(t, minDoc([]graph.Entity{
		// bm25-matching seed of the filtered kind, so the ranked list is non-empty.
		{
			ID: "c1", Name: "InvoiceWidget", Kind: "SCOPE.Component",
			SourceFile: "src/billing/invoice.jsx", StartLine: 4,
			QualifiedName: "InvoiceWidget",
		},
		// Same kind, NOT bm25-matching → reachable only through the fold-in.
		// Real entity: must be enumerated (this is the enumeration's control).
		{
			ID: "c2", Name: "ZZTopBanner", Kind: "SCOPE.Component",
			SourceFile: "src/promo/banner.jsx", StartLine: 7,
			QualifiedName: "ZZTopBanner",
		},
		// Same kind, NOT bm25-matching, local_scope → must be dropped by the
		// fold-in's own noise check.
		localScopeEntity("loc1", "QQrowCount", "src/promo/banner.jsx", 22),
	}, nil))

	// Positive control 1: include_noise:true folds in BOTH non-matching entities,
	// proving the enumeration reaches the local entity when noise is allowed.
	withNoise := findNames(t, srv, map[string]any{
		"query": "invoice", "kind_filter": "Component", "include_noise": true,
	})
	if !withNoise["QQrowCount"] {
		t.Fatalf("positive control failed: include_noise:true kind enumeration must fold in the local_scope entity, got %v", withNoise)
	}
	if !withNoise["ZZTopBanner"] {
		t.Fatalf("positive control failed: include_noise:true must fold in the real non-matching entity, got %v", withNoise)
	}

	// Default: the enumeration still folds in the real non-matching entity
	// (non-vacuity — proves the fold-in ran), but withholds the local one.
	def := findNames(t, srv, map[string]any{"query": "invoice", "kind_filter": "Component"})
	if !def["ZZTopBanner"] {
		t.Fatalf("non-vacuity: kind enumeration did not run (real non-matching entity absent); got %v", def)
	}
	if !def["InvoiceWidget"] {
		t.Fatalf("non-vacuity: bm25 seed absent from kind-filtered result; got %v", def)
	}
	if def["QQrowCount"] {
		t.Errorf("kind_filter enumeration must withhold the local_scope entity, got %v", def)
	}
}

// TestFind_ComponentPropIsVisibleByDefault pins the capability the
// local_scope-stamping arm must not delete: a React `component_prop`
// (SCOPE.Operation / Subtype component_prop) is returned by grafel_find with
// DEFAULT options — now WHILE carrying local_scope="true", i.e. this is the
// direct observation of classifyNoise's component_prop carve-out (#6472).
//
// Note grafel_find has no subtype filter — its parameter set is pinned at
// schema_trim_5386_test.go:135 and only kind_filter exists — so both paths are
// exercised via Kind SCOPE.Operation.
//
// The two legs need DIFFERENT props to be distinct. The ranked leg needs a prop
// the query textually reaches; but a prop that bm25-matches is retained by
// enumerateByKind's STEP 1 (keep kind-matching bm25 hits) and never reaches the
// STEP 2 fold-in, so reusing it for the kind_filter leg would make that leg a
// near-duplicate of the first. The fixture therefore carries a second prop, in
// another component and file, whose name and file stem do not match the query:
// it can only arrive via the fold-in, which is what that leg is here to cover.
func TestFind_ComponentPropIsVisibleByDefault(t *testing.T) {
	const file = "src/widgets/PriceTag.jsx"
	srv := newTestServer(t, minDoc([]graph.Entity{
		{
			ID: "fn1", Name: "PriceTag", Kind: "SCOPE.Operation",
			SourceFile: file, StartLine: 3, QualifiedName: "PriceTag",
		},
		// bm25-reachable prop — the ranked leg's subject.
		componentPropEntity("p1", "currencyCode", "PriceTag", file, 4),
		// Fold-in-only prop: different component, different file stem, and a
		// name sharing no token with the query.
		componentPropEntity("p2", "QQlabelText", "PromoBanner", "src/promo/banner.jsx", 9),
	}, nil))

	// Ranked (bm25) path, default options — no include_noise.
	def := findNames(t, srv, map[string]any{"query": "PriceTag"})
	if !def["PriceTag"] {
		t.Fatalf("non-vacuity: default find returned no entity from the fixture; got %v", def)
	}
	if !def["currencyCode"] {
		t.Errorf("component_prop must be reachable through grafel_find with default options, got %v", def)
	}
	// Premise guard for the leg below. If a scoring change ever makes the
	// fold-in-only prop a bm25 hit, that leg silently degrades into a copy of
	// this one. Fail loudly here rather than lose the coverage quietly.
	if def["QQlabelText"] {
		t.Fatalf("premise broken: QQlabelText was expected NOT to bm25-match %q, so the kind_filter "+
			"leg below no longer exercises the enumerateByKind fold-in; rename it. got %v", "PriceTag", def)
	}

	// kind_filter enumeration path, default options.
	kf := findNames(t, srv, map[string]any{"query": "PriceTag", "kind_filter": "Operation"})
	if !kf["PriceTag"] {
		t.Fatalf("non-vacuity: kind_filter=Operation returned no entity from the fixture; got %v", kf)
	}
	if !kf["currencyCode"] {
		t.Errorf("component_prop must survive the kind_filter enumeration with default options, got %v", kf)
	}
	// The load-bearing assertion: this prop is reachable only through the step-2
	// fold-in, which re-decides noise on its own and is exactly where a future
	// local_scope stamp on props would drop them.
	if !kf["QQlabelText"] {
		t.Errorf("a component_prop reachable only via the enumerateByKind fold-in must be returned, got %v", kf)
	}
}

// TestFind_SubstringPathDoesNotHideLocalScope pins a surprising, currently
// undocumented asymmetry. grafel_find routes on search= (core_merges.go:96-101):
//
//	bm25 (default)            → handleQueryGraph    → filters ALL noise classes
//	substring/literal/name    → handleSearchEntities → filters ONLY noiseSchemaField
//
// So the SAME entity is hidden on the default path and visible on the substring
// path. That is the behaviour today; a future change could "fix" it in either
// direction without anything going red. This test pins both halves in one place
// so the divergence is a deliberate decision, not an accident.
func TestFind_SubstringPathDoesNotHideLocalScope(t *testing.T) {
	const file = "src/checkout/totals.jsx"
	entities := []graph.Entity{
		{
			ID: "op1", Name: "computeSubtotalTotals", Kind: "SCOPE.Operation",
			SourceFile: file, StartLine: 10, QualifiedName: "computeSubtotalTotals",
		},
		localScopeEntity("loc1", "subtotal", file, 48),
	}
	srv := newTestServer(t, minDoc(entities, nil))

	// Half 1 (control for the divergence): the default bm25 path HIDES it.
	bm := findNames(t, srv, map[string]any{"query": "totals"})
	if !bm["computeSubtotalTotals"] {
		t.Fatalf("non-vacuity: bm25 path returned nothing; got %v", bm)
	}
	if bm["subtotal"] {
		t.Fatalf("premise broken: bm25 path is expected to hide the local_scope entity, got %v", bm)
	}

	// Half 2: the substring path returns it — routed through the REAL grafel_find
	// dispatcher so the routing itself is under test, not just the sub-handler.
	for _, search := range []string{"substring", "literal", "name"} {
		out := callDashboardTool(t, srv.handleCoreFind, map[string]any{
			"group":  "test",
			"query":  "subtotal",
			"search": search,
		})
		results, _ := out["results"].([]any)
		names := map[string]bool{}
		for _, r := range results {
			obj, _ := r.(map[string]any)
			if n, ok := obj["name"].(string); ok {
				names[n] = true
			}
		}
		// Positive control. "computeSubtotalTotals" also substring-matches
		// "subtotal", so this asserts the search actually ran and returned the
		// real entity — unlike a len(names)==0 guard, which that same match
		// makes unfirable.
		if !names["computeSubtotalTotals"] {
			t.Fatalf("search=%s: non-vacuity — substring search did not return the real entity; got %v", search, names)
		}
		if !names["subtotal"] {
			t.Errorf("search=%s: substring path does NOT filter local_scope; expected the local entity, got %v", search, names)
		}
	}
}
