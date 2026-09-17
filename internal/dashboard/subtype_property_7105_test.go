package dashboard

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// #7105 — Entity.Subtype is the canonical home of the class-vs-interface (and
// every other) sub-distinction, but serializeEntity — the dashboard's REST wire
// shape — emits NO `subtype` field. The only way a subtype reaches the
// dashboard is inside `properties`, which 74.5% of the corpus (796/1069
// entities, measured on the 38 golden fixtures) does not carry.
//
// This test grades the CONSEQUENCE through the emitted artefact: the entity is
// written by the production write path, loaded by the loader the dashboard
// store actually uses (graph.LoadGraphFromDir, store.go:247), and the assertion
// is on serializeEntity's output map — not on the Properties map of a
// hand-built entity, which would grade the stamp rather than what the surface
// can see.
//
// Three directions are graded, because deriving is a WIDENING and a must-have
// assertion is structurally blind to one that fires too broadly:
//
//	gap        — Subtype set, no twin  → the surface must now see it
//	agreement  — both already present  → unchanged (0/211 disagreement on main)
//	forbidden  — Subtype EMPTY         → must NOT acquire an empty-string key
//
// The forbidden direction is the load-bearing one: an empty `subtype: ""` on
// three quarters of the corpus is a real regression, it would make
// `properties` non-empty on every previously property-less entity, and no
// recall-style assertion can see it.
func roundTripThroughProductionWrite(t *testing.T, ents []graph.Entity) map[string]*graph.Entity {
	t.Helper()
	dir := t.TempDir()
	doc := &graph.Document{Version: 2, Repo: "repo7105", Entities: ents}
	if _, _, err := fbwriter.WriteGraphGenReport(dir, doc); err != nil {
		t.Fatalf("#7105: production write path failed: %v", err)
	}
	loaded, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("#7105: load from %s failed: %v", filepath.Base(dir), err)
	}
	out := make(map[string]*graph.Entity, len(loaded.Entities))
	for i := range loaded.Entities {
		out[loaded.Entities[i].ID] = &loaded.Entities[i]
	}
	if len(out) != len(ents) {
		t.Fatalf("#7105: round-trip lost entities: wrote %d, loaded %d", len(ents), len(out))
	}
	return out
}

func TestDashboardSerializeEntity_SeesCanonicalSubtype_7105(t *testing.T) {
	gap := graph.Entity{
		ID: "e-gap", Name: "PaymentGateway", Kind: "SCOPE.Component",
		Subtype: "interface", SourceFile: "pay/gateway.java", Language: "java",
		StartLine: 3, EndLine: 9,
	}
	agree := graph.Entity{
		ID: "e-agree", Name: "PaymentGatewayImpl", Kind: "SCOPE.Component",
		Subtype: "class", SourceFile: "pay/impl.java", Language: "java",
		StartLine: 4, EndLine: 40,
	}
	agree.PropsReplace(map[string]string{"subtype": "class", "module": "pay"})
	emptyWithProps := graph.Entity{
		ID: "e-empty-props", Name: "charge", Kind: "SCOPE.Function",
		Subtype: "", SourceFile: "pay/impl.java", Language: "java",
		StartLine: 12, EndLine: 18,
	}
	emptyWithProps.PropsReplace(map[string]string{"module": "pay"})
	emptyBare := graph.Entity{
		ID: "e-empty-bare", Name: "refund", Kind: "SCOPE.Function",
		Subtype: "", SourceFile: "pay/impl.java", Language: "java",
		StartLine: 20, EndLine: 26,
	}
	// A HYPOTHETICAL disagreement. Measured disagreement on the 38 golden
	// fixtures is 0/211, so no producer emits this shape today — but the
	// derivation makes a choice for it ("never overwrite an existing
	// subtype property"), and an ungraded choice is prose. Without this row
	// deleting the existing-key guard entirely is indistinguishable from
	// keeping it, because every real pair agrees.
	disagree := graph.Entity{
		ID: "e-disagree", Name: "LegacyShim", Kind: "SCOPE.Component",
		Subtype: "class", SourceFile: "pay/shim.java", Language: "java",
		StartLine: 50, EndLine: 60,
	}
	disagree.PropsReplace(map[string]string{"subtype": "interface"})

	loaded := roundTripThroughProductionWrite(t, []graph.Entity{gap, agree, emptyWithProps, emptyBare, disagree})

	propsOf := func(t *testing.T, id string) (map[string]string, bool) {
		t.Helper()
		e, ok := loaded[id]
		if !ok {
			t.Fatalf("#7105: entity %s missing after round-trip", id)
		}
		wire := serializeEntity("repo7105", e)
		raw, present := wire["properties"]
		if !present {
			return nil, false
		}
		props, ok := raw.(map[string]string)
		if !ok {
			t.Fatalf("#7105: serializeEntity emitted properties as %T, want map[string]string", raw)
		}
		return props, true
	}

	t.Run("gap_becomes_visible", func(t *testing.T) {
		props, present := propsOf(t, "e-gap")
		if !present {
			t.Fatalf("#7105: serializeEntity emitted no `properties` at all for an entity whose Subtype is %q; the dashboard cannot see the subtype by any route (there is no top-level `subtype` key in the wire shape)", "interface")
		}
		if got := props["subtype"]; got != "interface" {
			t.Fatalf("#7105: dashboard wire properties[\"subtype\"] = %q, want %q (derived from the canonical Entity.Subtype)", got, "interface")
		}
	})

	t.Run("agreement_preserved", func(t *testing.T) {
		props, present := propsOf(t, "e-agree")
		if !present {
			t.Fatal("#7105: dual-stamped entity lost its properties entirely")
		}
		if got := props["subtype"]; got != "class" {
			t.Fatalf("#7105: dual-stamped properties[\"subtype\"] = %q, want the pre-existing %q unchanged", got, "class")
		}
		if got := props["module"]; got != "pay" {
			t.Fatalf("#7105: unrelated property module = %q, want %q", got, "pay")
		}
		if len(props) != 2 {
			t.Fatalf("#7105: dual-stamped entity gained/lost properties: %v, want exactly {subtype, module}", props)
		}
	})

	t.Run("forbidden_empty_subtype_gains_nothing", func(t *testing.T) {
		props, present := propsOf(t, "e-empty-props")
		if !present {
			t.Fatal("#7105: entity with a module property lost it")
		}
		if v, ok := props["subtype"]; ok {
			t.Fatalf("#7105 FORBIDDEN: an entity whose Entity.Subtype is EMPTY acquired properties[\"subtype\"] = %q. Absent is correct, not empty-string: `subtype: \"\"` is indistinguishable from a real value to every Properties-only consumer (dashboard, docgen), and it fires on the majority of rows", v)
		}
		if len(props) != 1 {
			t.Fatalf("#7105 FORBIDDEN: subtype-less entity's properties = %v, want exactly {module}", props)
		}
	})

	t.Run("forbidden_existing_property_is_never_overwritten", func(t *testing.T) {
		props, present := propsOf(t, "e-disagree")
		if !present {
			t.Fatal("#7105: entity with a pre-existing subtype property lost it")
		}
		if got := props["subtype"]; got != "interface" {
			t.Fatalf("#7105 FORBIDDEN: a pre-existing properties[\"subtype\"] = %q was overwritten with the canonical Entity.Subtype (now %q). Deriving must only FILL an absent key; overwriting changes what Properties-only consumers already see, and it is a merge rather than a derivation", "interface", got)
		}
	})

	t.Run("forbidden_property_less_entity_stays_property_less", func(t *testing.T) {
		props, present := propsOf(t, "e-empty-bare")
		if present {
			t.Fatalf("#7105 FORBIDDEN: an entity with an empty Subtype and no properties gained a properties map %v. serializeEntity gates on PropLen() > 0 and several downstream checks are written as len(props) == 0, so allocating here changes what they see on the majority of the corpus", props)
		}
	})
}
