package graph_test

import (
	"sort"
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

// TestGeneratedPropertyKeysAreTheLiteralWireContract kills mutant M8.
//
// M8 renamed types.EntityGeneratedProperty from "generated" to "zgenerated"
// and internal/graph, internal/mcp and internal/extractors all stayed GREEN.
// Every test used the constant on both sides of the comparison, so the whole
// suite was tautological with respect to the one thing that is actually a
// contract: the key string itself.
//
// It is a contract in two independent ways, and both are asserted here.
//
//  1. IT IS A CROSS-COMPONENT KEY. entity.go argues this property is read by
//     ranking, docgen, the quality benchmark and the security audit. Those are
//     separate packages that must agree on a string, exactly like the JSON
//     wire key in serializeHits (which IS pinned, as a literal m["generated"]).
//
//  2. IT PERSISTS ON DISK WITH NO FormatVersion BUMP. That is stated as a
//     FEATURE of the design — Entity.properties is an unfiltered key/value
//     vector, so no schema edit and no forced reindex were needed. The cost of
//     that freedom is that every graph already written to disk carries the
//     literal bytes "generated". A rename is therefore not a refactor: it
//     silently stops reading every graph on every user's machine, and no
//     amount of constant-consistency can detect it.
//
// The second direction below is the load-bearing one. It writes the property
// under the LITERAL key — simulating a graph persisted by an older build —
// and then reads it back through the CONSTANT. A rename breaks that lookup
// even though both sides of the mutated code still agree with each other.
func TestGeneratedPropertyKeysAreTheLiteralWireContract(t *testing.T) {
	// Direction 1 — what we write must land on disk under the literal keys.
	e := graph.Entity{
		ID:         "ent-lit-1",
		Name:       "User",
		Kind:       string(types.EntityKindClass),
		SourceFile: "api/v1/user.pb.go",
		StartLine:  17,
	}
	e.PropSet(types.EntityGeneratedProperty, "true")
	e.PropSet(types.EntityGeneratedByProperty, "path:*.pb.go")

	// Direction 2 — a graph written by an OLDER build, which knew only the
	// literal bytes. No FormatVersion distinguishes it from one we write now.
	prior := graph.Entity{
		ID:         "ent-lit-2",
		Name:       "Order",
		Kind:       string(types.EntityKindClass),
		SourceFile: "api/v1/order.pb.go",
		StartLine:  9,
	}
	prior.PropSet("generated", "true")
	prior.PropSet("generated_by", "marker:go-do-not-edit")

	dir := t.TempDir()
	doc := &graph.Document{Entities: []graph.Entity{e, prior}}
	if _, err := fbwriter.WriteGraphGen(dir, doc); err != nil {
		t.Fatalf("WriteGraphGen: %v", err)
	}
	got, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	byID := map[string]*graph.Entity{}
	for i := range got.Entities {
		byID[got.Entities[i].ID] = &got.Entities[i]
	}
	loaded, priorLoaded := byID["ent-lit-1"], byID["ent-lit-2"]
	if loaded == nil || priorLoaded == nil {
		t.Fatalf("entities did not survive the round trip (got %d)", len(got.Entities))
	}

	// Direction 1: the persisted key bytes, read WITHOUT the constant.
	props := loaded.PropsSnapshot()
	if v := props["generated"]; v != "true" {
		t.Errorf(`persisted property map has ["generated"] = %q, want "true"; `+
			`the on-disk key is a cross-component contract and cannot be renamed `+
			`without a FormatVersion bump and a migration (keys present: %v)`, v, keysOf(props))
	}
	if v := props["generated_by"]; v != "path:*.pb.go" {
		t.Errorf(`persisted property map has ["generated_by"] = %q, want the rule that fired `+
			`(keys present: %v)`, v, keysOf(props))
	}

	// Direction 2: a graph written under the literal keys by an older build
	// must still be readable through the constants.
	if v := priorLoaded.PropGet(types.EntityGeneratedProperty); v != "true" {
		t.Errorf("a graph persisted under the literal key \"generated\" reads back as %q "+
			"through EntityGeneratedProperty; renaming the constant orphans every "+
			"graph already on disk", v)
	}
	if v := priorLoaded.PropGet(types.EntityGeneratedByProperty); v != "marker:go-do-not-edit" {
		t.Errorf("a graph persisted under the literal key \"generated_by\" reads back as %q "+
			"through EntityGeneratedByProperty", v)
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
