package graph_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/types"
)

// #6329 — the generated flag must survive into the PERSISTED graph, not just
// live in memory between extraction and the first consumer.
//
// The claim being tested is that Entity.properties in graph.fbs is an
// unfiltered key/value vector, so a new property needs no schema change and no
// FormatVersion bump. That is a claim about the writer's behaviour, and
// "I read the writer and it has no allowlist" is not evidence — the whole
// point of this issue is that a plausible-looking mechanism (#6330's config)
// was never executed and therefore never true. So: write a real graph to a
// real directory and read it back.
func TestGeneratedPropertyRoundTripsThroughPersistedGraph(t *testing.T) {
	e := graph.Entity{
		ID:         "ent-1",
		Name:       "User",
		Kind:       string(types.EntityKindClass),
		SourceFile: "api/v1/user.pb.go",
		StartLine:  17,
	}
	e.PropSet(types.EntityGeneratedProperty, "true")
	e.PropSet(types.EntityGeneratedByProperty, "path:*.pb.go")

	authored := graph.Entity{
		ID:         "ent-2",
		Name:       "UserService",
		Kind:       string(types.EntityKindClass),
		SourceFile: "internal/user/service.go",
		StartLine:  42,
	}

	doc := &graph.Document{Entities: []graph.Entity{e, authored}}

	dir := t.TempDir()
	if _, err := fbwriter.WriteGraphGen(dir, doc); err != nil {
		t.Fatalf("WriteGraphGen: %v", err)
	}

	got, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}

	var loadedGen, loadedAuthored *graph.Entity
	for i := range got.Entities {
		x := &got.Entities[i]
		switch x.ID {
		case "ent-1":
			loadedGen = x
		case "ent-2":
			loadedAuthored = x
		}
	}
	if loadedGen == nil || loadedAuthored == nil {
		t.Fatalf("entities did not survive the round trip (got %d)", len(got.Entities))
	}

	if v := loadedGen.PropGet(types.EntityGeneratedProperty); v != "true" {
		t.Errorf("generated = %q after round trip, want \"true\"", v)
	}
	if v := loadedGen.PropGet(types.EntityGeneratedByProperty); v != "path:*.pb.go" {
		t.Errorf("generated_by = %q after round trip, want the rule that fired", v)
	}
	// The authored entity must come back with neither key — the flag is a
	// positive marking, not a field on every entity.
	if v := loadedGen.PropGet("nonexistent"); v != "" {
		t.Errorf("PropGet of an absent key returned %q", v)
	}
	if v := loadedAuthored.PropGet(types.EntityGeneratedProperty); v != "" {
		t.Errorf("authored entity carries generated=%q after round trip", v)
	}
}
