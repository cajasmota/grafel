package docgen

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// #7105 — configEntryFromEntity (llm_bundle.go) fills ModuleConfigEntry.Subtype
// from props["subtype"] ONLY; it never consults the canonical Entity.Subtype.
// So an entity that carries its subtype in the canonical field alone is
// subtype-less in the LLM bundle, which is what docgen hands the model.
//
// Graded through the emitted artefact — the ModuleConfigEntry that goes into
// the bundle — after a round trip through the production write path and the
// loader, not by inspecting a hand-built Properties map.
func docgenRoundTrip7105(t *testing.T, ents []graph.Entity) map[string]*graph.Entity {
	t.Helper()
	dir := t.TempDir()
	doc := &graph.Document{Version: 2, Repo: "repo7105", Entities: ents}
	if _, _, err := fbwriter.WriteGraphGenReport(dir, doc); err != nil {
		t.Fatalf("#7105: production write path failed: %v", err)
	}
	loaded, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("#7105: load failed: %v", err)
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

func TestDocgenConfigEntry_SeesCanonicalSubtype_7105(t *testing.T) {
	gap := graph.Entity{
		ID: "d-gap", Name: "go.mod", Kind: "SCOPE.Config",
		Subtype: "go_module", SourceFile: "go.mod", Language: "go",
		StartLine: 1, EndLine: 1,
	}
	agree := graph.Entity{
		ID: "d-agree", Name: "pyproject.toml", Kind: "SCOPE.Config",
		Subtype: "python_project", SourceFile: "pyproject.toml", Language: "python",
		StartLine: 1, EndLine: 1,
	}
	agree.PropsReplace(map[string]string{"subtype": "python_project", "format": "toml"})
	empty := graph.Entity{
		ID: "d-empty", Name: "Makefile", Kind: "SCOPE.Config",
		Subtype: "", SourceFile: "Makefile", Language: "make",
		StartLine: 1, EndLine: 1,
	}
	empty.PropsReplace(map[string]string{"format": "makefile"})

	loaded := docgenRoundTrip7105(t, []graph.Entity{gap, agree, empty})
	entryFor := func(t *testing.T, id string) ModuleConfigEntry {
		t.Helper()
		e, ok := loaded[id]
		if !ok {
			t.Fatalf("#7105: entity %s missing after round-trip", id)
		}
		return configEntryFromEntity(e)
	}

	t.Run("gap_becomes_visible", func(t *testing.T) {
		if got := entryFor(t, "d-gap").Subtype; got != "go_module" {
			t.Fatalf("#7105: ModuleConfigEntry.Subtype = %q, want %q; docgen reads props[\"subtype\"] only and cannot see Entity.Subtype", got, "go_module")
		}
	})

	t.Run("agreement_preserved", func(t *testing.T) {
		entry := entryFor(t, "d-agree")
		if entry.Subtype != "python_project" {
			t.Fatalf("#7105: dual-stamped ModuleConfigEntry.Subtype = %q, want the pre-existing %q unchanged", entry.Subtype, "python_project")
		}
		if entry.Format != "toml" {
			t.Fatalf("#7105: unrelated Format = %q, want %q", entry.Format, "toml")
		}
	})

	// NOTE on gradability, narrowed to what is actually ungraded:
	// ModuleConfigEntry conflates an ABSENT subtype property with an
	// empty-string one — both arrive as "" — so THIS reader cannot distinguish
	// "gained nothing" from "gained `subtype: \"\"`", and removing the
	// empty-Subtype guard leaves this subtest green. That is a limitation of
	// configEntryFromEntity, NOT of docgen: tier0.renderSection emits a
	// `**Properties:**` block listing every `k = v`, which can tell the two
	// apart, and grades the empty direction in
	// TestDocgenRenderSection_SubtypeWidening_7105 below. The direction is also
	// graded on the dashboard wire map. What remains ungraded here, and only
	// here, is configEntryFromEntity itself; this subtest pins that it keeps
	// reporting no subtype for a subtype-less entity.
	t.Run("forbidden_empty_subtype_stays_empty", func(t *testing.T) {
		entry := entryFor(t, "d-empty")
		if entry.Subtype != "" {
			t.Fatalf("#7105 FORBIDDEN: an entity whose Entity.Subtype is EMPTY produced ModuleConfigEntry.Subtype = %q", entry.Subtype)
		}
		if entry.Format != "makefile" {
			t.Fatalf("#7105: unrelated Format = %q, want %q", entry.Format, "makefile")
		}
	})
}

// #7105 — the other docgen surface. tier0.renderSection renders a
// `**Properties:**` block gated on PropLen() > 0, listing every `k = v`, so
// unlike configEntryFromEntity it CAN distinguish an absent subtype property
// from `subtype = `. Two things are graded here:
//
//   - the gap closes at this surface too, and
//   - the widening does not reach rows that have nothing to say — an entity
//     with an empty Subtype must not gain a `subtype = ` line, and one with no
//     properties at all must not gain a `**Properties:**` block it never had.
//
// Disclosed rather than hidden: the 796 rows that gain the derived key DO now
// render a `**Properties:**` block in tier-0 output where they previously
// rendered none. That is a real change to rendered documentation, it is the
// intended consequence of making the subtype visible to Properties-only
// readers, and `gap_renders_the_derived_property` is the row that pins it.
func TestDocgenRenderSection_SubtypeWidening_7105(t *testing.T) {
	gap := graph.Entity{
		ID: "r-gap", Name: "PaymentGateway", Kind: "SCOPE.Component",
		Subtype: "interface", SourceFile: "pay/gateway.java", Language: "java",
		StartLine: 3, EndLine: 9,
	}
	emptyWithProps := graph.Entity{
		ID: "r-empty-props", Name: "Makefile", Kind: "SCOPE.Config",
		Subtype: "", SourceFile: "Makefile", Language: "make",
		StartLine: 1, EndLine: 1,
	}
	emptyWithProps.PropsReplace(map[string]string{"format": "makefile"})
	emptyBare := graph.Entity{
		ID: "r-empty-bare", Name: "refund", Kind: "SCOPE.Function",
		Subtype: "", SourceFile: "pay/impl.java", Language: "java",
		StartLine: 20, EndLine: 26,
	}

	loaded := docgenRoundTrip7105(t, []graph.Entity{gap, emptyWithProps, emptyBare})
	render := func(t *testing.T, id string) string {
		t.Helper()
		e, ok := loaded[id]
		if !ok {
			t.Fatalf("#7105: entity %s missing after round-trip", id)
		}
		return renderSection("overview", e, nil)
	}

	t.Run("gap_renders_the_derived_property", func(t *testing.T) {
		out := render(t, "r-gap")
		if !strings.Contains(out, "**Properties:**") {
			t.Fatalf("#7105: tier-0 rendered no Properties block for an entity whose Subtype is %q; the block is gated on PropLen() > 0 and the derived key is what makes it non-empty.\n---\n%s", "interface", out)
		}
		if !strings.Contains(out, "subtype = interface") {
			t.Fatalf("#7105: tier-0 Properties block does not list `subtype = interface`.\n---\n%s", out)
		}
	})

	t.Run("forbidden_empty_subtype_renders_no_subtype_line", func(t *testing.T) {
		out := render(t, "r-empty-props")
		if strings.Contains(out, "subtype =") {
			t.Fatalf("#7105 FORBIDDEN: an entity whose Entity.Subtype is EMPTY rendered a `subtype =` line in tier-0 output. This surface CAN tell an absent key from an empty one, which is why the empty direction is graded here.\n---\n%s", out)
		}
		if !strings.Contains(out, "format = makefile") {
			t.Fatalf("#7105: unrelated property lost from the Properties block.\n---\n%s", out)
		}
	})

	t.Run("forbidden_property_less_entity_renders_no_properties_block", func(t *testing.T) {
		out := render(t, "r-empty-bare")
		if strings.Contains(out, "**Properties:**") {
			t.Fatalf("#7105 FORBIDDEN: an entity with an empty Subtype and no properties gained a `**Properties:**` block it never had.\n---\n%s", out)
		}
	})
}
