package mcp

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// #6472 arm 1 — the two readers of Properties["local_scope"] want opposite
// things, and this file asserts BOTH on ONE record.
//
// The key is read in two places:
//
//   - internal/resolve/refs.go isLocalBindingKind — "must not compete for this
//     name in the repository-wide byName index". A React props parameter is a
//     formal parameter of the component callable: TRUE.
//   - internal/mcp/denoise.go classifyNoise — "hide this from grafel_find". A
//     props parameter has a real QualifiedName, a real line span and a
//     resolvable ID: FALSE.
//
// Before this change the resolver got its answer from a hardcoded
// `subtype == "component_prop" && framework == "react"` clause, because the
// emitter did not stamp the key at all. Stamping it — the obvious fix — would
// have deleted every React component's prop surface from grafel_find. So the
// change stamps the key AND carves component_prop out of the noise bucket.
//
// A test that asserted only one half would have passed for either of the two
// broken intermediate states. This is the test that would have caught the issue
// as originally filed, so both halves live here, driven by one Properties map.
// ---------------------------------------------------------------------------

// reactPropProperties is the Properties map that
// internal/extractors/javascript/dataflow_react.go's prop emitter builds for a
// prop, copied from that emitter rather than from what this test wants to be
// true. TestReactPropsCarryLocalScopeStamp_6472 (internal/extractors/javascript)
// is what observes the real emitter; this map only has to stay in step with it.
func reactPropProperties(component, prop string) map[string]string {
	return map[string]string{
		"kind":        "SCOPE.Operation",
		"subtype":     "component_prop",
		"component":   component,
		"prop":        prop,
		"framework":   "react",
		"local_scope": "true",
	}
}

// TestComponentProp_SubtypeInPropertiesOnly_6472 pins that the carve-out reads
// the SAME subtype carrier classifyNoise already computes for #2015, not
// graph.Entity.Subtype directly.
//
// The extractor writes the subtype into BOTH EntityRecord.Subtype and
// Properties["subtype"], and classifyNoise computes a `subtype` local that
// falls back from one to the other precisely because some load/conversion
// paths repopulate only one of them. A carve-out keyed on e.Subtype alone
// therefore hides every prop that arrived through such a path — the exact
// capability deletion this change exists to prevent, and invisible to every
// other test here because they all hand-set the struct field.
//
// Non-vacuity: the control leg asserts a prop carrying the subtype in the
// struct field IS classified noiseNone, so a mutant breaking classification
// outright cannot make this test pass by hiding both.
func TestComponentProp_SubtypeInPropertiesOnly_6472(t *testing.T) {
	props := reactPropProperties("PriceTag", "currencyCode")

	// Control: subtype in the struct field (what every other fixture does).
	both := graph.Entity{
		ID: "p1", Name: "currencyCode", Kind: "SCOPE.Operation",
		Subtype: "component_prop", SourceFile: "src/PriceTag.jsx",
		StartLine: 4, EndLine: 4, QualifiedName: "PriceTag.currencyCode",
	}.WithProperties(props)
	if got := classifyNoise(&both); got != noiseNone {
		t.Fatalf("control: prop with Subtype set classified %v, want noiseNone", got)
	}

	// The case under test: Subtype field EMPTY, subtype only in Properties.
	propsOnly := graph.Entity{
		ID: "p2", Name: "currencyCode", Kind: "SCOPE.Operation",
		SourceFile: "src/PriceTag.jsx",
		StartLine:  4, EndLine: 4, QualifiedName: "PriceTag.currencyCode",
	}.WithProperties(props)
	if propsOnly.Subtype != "" {
		t.Fatalf("premise broken: this case requires an empty Subtype field, got %q", propsOnly.Subtype)
	}
	if propsOnly.PropGet("subtype") != "component_prop" {
		t.Fatalf("premise broken: the subtype must ride in Properties for this case, got %q",
			propsOnly.PropGet("subtype"))
	}
	if got := classifyNoise(&propsOnly); got != noiseNone {
		t.Errorf("classifyNoise = %v, want noiseNone: a component_prop whose subtype arrived in "+
			"Properties rather than the struct field is still a component_prop. classifyNoise "+
			"already computes a #2015 fallback local for exactly this reason; the carve-out must "+
			"use it, or the prop is hidden from grafel_find on every load path that repopulates "+
			"only Properties (#6472)", got)
	}

	// And the bucket still fires for a genuine local binding on that same path,
	// so the fix widened nothing beyond component_prop.
	localOnly := graph.Entity{
		ID: "l1", Name: "subtotal", Kind: "SCOPE.Component",
		SourceFile: "src/PriceTag.jsx", StartLine: 9, EndLine: 9,
		QualifiedName: "PriceTag.subtotal",
	}.WithProperties(map[string]string{
		"kind": "SCOPE.Component", "subtype": "const_destructure", "local_scope": "true",
	})
	if got := classifyNoise(&localOnly); got != noiseLocalScope {
		t.Errorf("classifyNoise(const_destructure with subtype in Properties only) = %v, "+
			"want noiseLocalScope: the carve-out must not have widened past component_prop", got)
	}
}

// TestComponentProp_VisibleToFindAndLocalToResolver_6472 asserts the split
// directly: the SAME prop record is returned by grafel_find with default
// options AND does not capture the repository-wide byName slot.
//
// Note grafel_find has no subtype filter — its parameter set is pinned at
// schema_trim_5386_test.go:135, and only kind_filter exists while every
// component_prop is SCOPE.Operation — so visibility is exercised through the
// ranked path and the kind_filter enumeration, not through a subtype selector.
//
// Non-vacuity, asserted before every claim:
//   - direction A checks a real entity from the same fixture comes back, so an
//     empty result set cannot satisfy the visibility assertion;
//   - direction B checks exactly one IMPORTS edge exists and that a same-named
//     ADDRESSABLE declaration DOES take the slot, so "the prop did not win"
//     cannot be satisfied by resolution having failed outright.
func TestComponentProp_VisibleToFindAndLocalToResolver_6472(t *testing.T) {
	const (
		file      = "src/widgets/PriceTag.jsx"
		component = "PriceTag"
		prop      = "currencyCode"
	)
	props := reactPropProperties(component, prop)

	// ----- Direction A: still returned by grafel_find (denoise carve-out) ----
	propEntity := graph.Entity{
		ID: "p1", Name: prop, Kind: "SCOPE.Operation", Subtype: "component_prop",
		SourceFile: file, StartLine: 4, EndLine: 4,
		QualifiedName: component + "." + prop,
	}.WithProperties(props)

	// Positive control FIRST: the prop entity is in the fixture graph at all,
	// and classifyNoise agrees it is not noise. Without this, every assertion
	// below could be satisfied by a graph that never contained the prop.
	if got := classifyNoise(&propEntity); got != noiseNone {
		t.Fatalf("classifyNoise(component_prop) = %v, want noiseNone: a prop carrying "+
			"local_scope must be carved out of the noise bucket — it has a real "+
			"QualifiedName (%q), a real span (%d) and a resolvable ID, so it fails the "+
			"bucket's own \"not independently inspectable\" test (#6472)",
			got, propEntity.QualifiedName, propEntity.StartLine)
	}

	srv := newTestServer(t, minDoc([]graph.Entity{
		{
			ID: "fn1", Name: component, Kind: "SCOPE.Operation",
			SourceFile: file, StartLine: 3, QualifiedName: component,
		},
		propEntity,
	}, nil))

	def := findNames(t, srv, map[string]any{"query": component})
	if !def[component] {
		t.Fatalf("non-vacuity: default grafel_find returned no entity from the fixture; got %v", def)
	}
	if !def[prop] {
		t.Errorf("grafel_find (default options) must still return the component_prop %q. "+
			"Stamping local_scope on props without the classifyNoise carve-out deletes a "+
			"component's entire prop surface from agent-facing search (#6472); got %v", prop, def)
	}

	kf := findNames(t, srv, map[string]any{"query": component, "kind_filter": "Operation"})
	if !kf[component] {
		t.Fatalf("non-vacuity: kind_filter=Operation returned no entity from the fixture; got %v", kf)
	}
	if !kf[prop] {
		t.Errorf("the kind_filter enumeration re-decides noise on its own (tools.go ~1168) and "+
			"must also return the component_prop %q; got %v", prop, kf)
	}

	// ----- Direction B: still loses the repository-wide byName slot ----------
	// Same Properties map, expressed as the extractor record the resolver sees.
	propRecord := types.EntityRecord{
		ID: "aaaa000000000001", Kind: "SCOPE.Operation", Name: prop,
		QualifiedName: component + "." + prop, Subtype: "component_prop",
		SourceFile: file, Language: "typescript",
		Properties: props,
	}
	placeholder := types.EntityRecord{
		ID: "aaaa00000000000f", Kind: "SCOPE.Component", Name: prop,
		Subtype: "import", SourceFile: "app/importer.ts", Language: "typescript",
		Properties: map[string]string{
			"external_dependency": "true",
			"provenance":          "INFERRED_FROM_IMPORT_STATEMENT",
		},
		Relationships: []types.RelationshipRecord{
			{FromID: "app/importer.ts", ToID: prop, Kind: "IMPORTS"},
		},
	}
	// The non-vacuity control for this direction: an ADDRESSABLE, module-scope
	// declaration of the same name, in another file, carrying no local_scope.
	// It must win the slot. If resolution silently stopped binding anything,
	// this assertion fires and the "the prop did not win" claim below cannot be
	// satisfied by a dead resolver.
	realDecl := types.EntityRecord{
		ID: "aaaa000000000002", Kind: "SCOPE.Operation", Name: prop,
		QualifiedName: prop, SourceFile: "src/lib/currency.ts", Language: "typescript",
		Properties: map[string]string{"kind": "SCOPE.Operation"},
	}

	for _, order := range []struct {
		tag  string
		recs []types.EntityRecord
	}{
		{"placeholder first", []types.EntityRecord{placeholder, propRecord, realDecl}},
		{"prop first", []types.EntityRecord{propRecord, realDecl, placeholder}},
		{"declaration first", []types.EntityRecord{realDecl, propRecord, placeholder}},
	} {
		t.Run("byName/"+order.tag, func(t *testing.T) {
			recs := append([]types.EntityRecord(nil), order.recs...)
			idx := resolve.BuildIndex(recs)
			resolve.ReferencesEmbedded(recs, idx)

			var got string
			seen := 0
			for i := range recs {
				for _, r := range recs[i].Relationships {
					if r.Kind == "IMPORTS" {
						seen++
						got = r.ToID
					}
				}
			}
			if seen != 1 {
				t.Fatalf("fixture is vacuous: found %d IMPORTS edge(s), want 1", seen)
			}
			if got != realDecl.ID {
				t.Errorf("app/importer.ts -IMPORTS-> %q, want the addressable declaration %q: "+
					"the props parameter must not compete for the repository-wide byName slot, "+
					"and a same-named real declaration must still win it (#6472/#6467)",
					got, realDecl.ID)
			}
			if got == propRecord.ID {
				t.Errorf("the component_prop captured the repository-wide byName slot: a binding " +
					"that exists only inside the component callable must never hold it (#6467)")
			}
		})
	}
}
