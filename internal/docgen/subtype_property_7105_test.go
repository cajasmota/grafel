package docgen

import (
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
