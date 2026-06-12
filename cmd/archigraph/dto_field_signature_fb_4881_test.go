package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// TestDTOFieldSignatureSurvivesFBRoundTrip is the end-to-end regression test
// for #4881: a NestJS DTO with bare class-field declarations
// (`id: number;`, `type: string | null;`) must round-trip its field-type
// SIGNATURE all the way through the binary graph.fb persistence path the
// daemon MCP + dashboard read. Before the fix the FlatBuffers Entity table had
// no `signature` slot, so every entity (field, function, class, …) came back
// from graph.fb with Signature="" — which made the dashboard shape API render
// every DTO field with an empty type. This test fails on the unpatched
// writer/schema and passes once the signature slot is persisted + restored.
func TestDTOFieldSignatureSurvivesFBRoundTrip(t *testing.T) {
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", t.TempDir())
	repo := t.TempDir()
	srcDir := filepath.Join(repo, "src", "modules", "equipment-types", "dto", "response")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dto := `import { EquipmentType } from '../../models/equipment-type.entity';
import { EquipmentCategory } from '../../models/equipment-category.enum';

export class EquipmentTypeResponse {
  id: number;
  category: EquipmentCategory;
  type: string | null;
  alias: string | null;

  constructor(id: number, category: EquipmentCategory, type: string | null, alias: string | null) {
    this.id = id;
    this.category = category;
    this.type = type;
    this.alias = alias;
  }

  static from(entity: EquipmentType): EquipmentTypeResponse {
    return new EquipmentTypeResponse(entity.id, entity.category, entity.type, entity.alias);
  }
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "equipment-type.response.dto.ts"), []byte(dto), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInitCommit(t, repo)

	out := filepath.Join(repo, ".ag", "graph.json")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Index(repo, out, "dto-test", nil, true, false); err != nil {
		t.Fatalf("index: %v", err)
	}

	// LoadGraphFromDir reads graph.fb first (the binary fast-path the daemon
	// MCP + dashboard use) — exactly the path that dropped the signature.
	doc, err := graph.LoadGraphFromDir(filepath.Dir(out))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	want := map[string]string{
		"EquipmentTypeResponse.id":       "id: number",
		"EquipmentTypeResponse.category": "category: EquipmentCategory",
		"EquipmentTypeResponse.type":     "type: string | null",
		"EquipmentTypeResponse.alias":    "alias: string | null",
	}
	got := map[string]string{}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" && strings.HasPrefix(e.Name, "EquipmentTypeResponse.") {
			got[e.Name] = e.Signature
		}
	}
	for name, wantSig := range want {
		if got[name] != wantSig {
			t.Errorf("field %s: signature=%q after graph.fb round-trip, want %q (the field TYPE was dropped by the binary persistence path)", name, got[name], wantSig)
		}
	}
}
