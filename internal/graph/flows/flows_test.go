package flows_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/graph/flows"
)

// writeSingleFileGen writes graph.1.fb + points current at it.
func writeSingleFileGen(t *testing.T, dir string) {
	t.Helper()
	doc := bakedDoc()
	if err := fbwriter.WriteAtomic(filepath.Join(dir, graph.GenFileName(1)), doc); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointer(dir, graph.GenFileName(1)); err != nil {
		t.Fatal(err)
	}
}

// bakedDoc models a graph.fb as the index bakes it: one intra-repo SCOPE.Process
// flow + its STEP_IN_PROCESS edges, plus some ordinary entities.
func ent(id, kind, name string, props map[string]string) graph.Entity {
	return graph.Entity{ID: id, Kind: kind, Name: name}.WithProperties(props)
}

func rel(id, from, to, kind string, props map[string]string) graph.Relationship {
	return graph.Relationship{ID: id, FromID: from, ToID: to, Kind: kind}.WithProperties(props)
}

func bakedDoc() *graph.Document {
	return &graph.Document{Repo: "r", Entities: []graph.Entity{
		{ID: "fn1", Kind: "function", Name: "Handler"},
		{ID: "fn2", Kind: "function", Name: "Service"},
		ent("baked-proc", "SCOPE.Process", "Handler -> Service",
			map[string]string{"step_count": "2", "entry_id": "fn1", "cross_stack": "false"}),
	}, Relationships: []graph.Relationship{
		{ID: "c1", FromID: "fn1", ToID: "fn2", Kind: "CALLS"},
		rel("s1", "baked-proc", "fn1", "STEP_IN_PROCESS", map[string]string{"step_index": "0"}),
		rel("s2", "baked-proc", "fn2", "STEP_IN_PROCESS", map[string]string{"step_index": "1"}),
	}}
}

// crossRepoDelta models the phantom-pass output: a re-synthesized cross-repo
// SCOPE.Process (new id, cross_stack=true) + its steps + a phantom CALLS edge.
func crossRepoDelta() ([]graph.Entity, []graph.Relationship) {
	ents := []graph.Entity{
		ent("xrepo-proc", "SCOPE.Process", "Handler -> Service -> RemoteAPI",
			map[string]string{"step_count": "3", "entry_id": "fn1", "cross_stack": "true"}),
	}
	rels := []graph.Relationship{
		rel("xs0", "xrepo-proc", "fn1", "STEP_IN_PROCESS", map[string]string{"step_index": "0"}),
		rel("xs1", "xrepo-proc", "fn2", "STEP_IN_PROCESS", map[string]string{"step_index": "1"}),
		rel("xs2", "xrepo-proc", "remote::ep", "STEP_IN_PROCESS", map[string]string{"step_index": "2"}),
		rel("ph1", "fn2", "remote::ep", "CALLS", map[string]string{
			"cross_repo": "true", "target_repo": "remote", "link_method": "http", "via": "phantom_edge_pass_#769",
		}),
	}
	return ents, rels
}

func TestUpsertRead_SingleFileRoundtrip(t *testing.T) {
	dir := t.TempDir()
	writeSingleFileGen(t, dir)
	ents, rels := crossRepoDelta()
	if err := flows.Upsert(dir, ents, rels); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	sc, ok := flows.Read(dir)
	if !ok {
		t.Fatal("Read returned not-ok for a fresh sidecar")
	}
	if len(sc.Entities) != 1 || sc.Entities[0].ID != "xrepo-proc" {
		t.Errorf("Entities = %+v", sc.Entities)
	}
	if len(sc.Relationships) != 4 {
		t.Errorf("Relationships len = %d, want 4", len(sc.Relationships))
	}
	if sc.SourceKey == "" {
		t.Error("SourceKey should be non-empty for a single-file graph")
	}
}

// TestApply_ReplaceSemantics is the core double-count guard: applying the
// sidecar must SUPPRESS the baked intra flow and its STEP edges, SUBSTITUTE the
// cross-repo flow, and ADD the phantom CALLS edge — never additively double.
func TestApply_ReplaceSemantics(t *testing.T) {
	doc := bakedDoc()
	ents, rels := crossRepoDelta()
	sc := &flows.Sidecar{Entities: ents, Relationships: rels}

	flows.Apply(doc, sc)

	// Exactly ONE SCOPE.Process, and it is the cross-repo one (not the baked).
	var procs []string
	for _, e := range doc.Entities {
		if e.Kind == "SCOPE.Process" {
			procs = append(procs, e.ID)
		}
	}
	if len(procs) != 1 || procs[0] != "xrepo-proc" {
		t.Fatalf("after Apply want exactly [xrepo-proc], got %v (DOUBLE-COUNT or wrong flow)", procs)
	}
	// The baked STEP edges must be gone; only the cross-repo steps remain.
	var stepFroms []string
	var phantom int
	for _, r := range doc.Relationships {
		if r.Kind == "STEP_IN_PROCESS" {
			stepFroms = append(stepFroms, r.FromID)
		}
		if r.Kind == "CALLS" && r.PropGet("cross_repo") == "true" {
			phantom++
		}
	}
	for _, f := range stepFroms {
		if f == "baked-proc" {
			t.Errorf("baked STEP_IN_PROCESS edge survived Apply (should be suppressed): %v", stepFroms)
		}
	}
	if len(stepFroms) != 3 {
		t.Errorf("want 3 cross-repo step edges, got %d: %v", len(stepFroms), stepFroms)
	}
	if phantom != 1 {
		t.Errorf("want exactly 1 phantom CALLS edge added, got %d", phantom)
	}
	// The ordinary CALLS edge and plain entities survive.
	var funcs int
	for _, e := range doc.Entities {
		if e.Kind == "function" {
			funcs++
		}
	}
	if funcs != 2 {
		t.Errorf("ordinary entities must survive Apply, got %d functions", funcs)
	}
}

// TestApply_ReplaceSemantics_EventFlow guards the SCOPE.EventFlow half of the
// REPLACE machinery: a baked SCOPE.EventFlow + its SEED_OF_EVENT_FLOW /
// STEP_IN_EVENT_FLOW edges must be SUPPRESSED (no dangling baked edge) and
// SUBSTITUTED by the sidecar's cross-repo event flow. FAILS if either event-flow
// edge kind is dropped from the strip set (the baked SEED/STEP edge would then
// survive as a dangling edge pointing at the removed baked entity).
func TestApply_ReplaceSemantics_EventFlow(t *testing.T) {
	doc := &graph.Document{Repo: "r", Entities: []graph.Entity{
		{ID: "chan1", Kind: "message_channel", Name: "orders.topic"},
		{ID: "consumer1", Kind: "function", Name: "onOrder"},
		ent("baked-ef", "SCOPE.EventFlow", "orders.topic -> onOrder",
			map[string]string{"channel_count": "1", "cross_stack": "false"}),
	}, Relationships: []graph.Relationship{
		rel("seed-b", "chan1", "baked-ef", "SEED_OF_EVENT_FLOW", map[string]string{}),
		rel("efs-b", "baked-ef", "consumer1", "STEP_IN_EVENT_FLOW", map[string]string{"step_index": "0"}),
	}}
	sc := &flows.Sidecar{
		Entities: []graph.Entity{
			ent("xrepo-ef", "SCOPE.EventFlow", "orders.topic -> onOrder -> remoteSink",
				map[string]string{"channel_count": "2", "cross_stack": "true"}),
		},
		Relationships: []graph.Relationship{
			rel("seed-x", "chan1", "xrepo-ef", "SEED_OF_EVENT_FLOW", map[string]string{}),
			rel("efs-x0", "xrepo-ef", "consumer1", "STEP_IN_EVENT_FLOW", map[string]string{"step_index": "0"}),
			rel("efs-x1", "xrepo-ef", "remote::sink", "STEP_IN_EVENT_FLOW", map[string]string{"step_index": "1"}),
		},
	}

	flows.Apply(doc, sc)

	// Exactly ONE SCOPE.EventFlow, the cross-repo one.
	var efs []string
	for _, e := range doc.Entities {
		if e.Kind == "SCOPE.EventFlow" {
			efs = append(efs, e.ID)
		}
	}
	if len(efs) != 1 || efs[0] != "xrepo-ef" {
		t.Fatalf("event-flow REPLACE violated: want [xrepo-ef], got %v", efs)
	}
	// No baked SEED/STEP edges survive (they would dangle at removed baked-ef).
	for _, r := range doc.Relationships {
		if r.FromID == "baked-ef" || r.ToID == "baked-ef" {
			t.Errorf("dangling baked event-flow edge survived Apply: %s %s->%s", r.Kind, r.FromID, r.ToID)
		}
	}
	// Substituted edges present: 1 seed + 2 steps for xrepo-ef.
	var seed, step int
	for _, r := range doc.Relationships {
		if r.Kind == "SEED_OF_EVENT_FLOW" && r.ToID == "xrepo-ef" {
			seed++
		}
		if r.Kind == "STEP_IN_EVENT_FLOW" && r.FromID == "xrepo-ef" {
			step++
		}
	}
	if seed != 1 || step != 2 {
		t.Errorf("event-flow substitution wrong: seed=%d step=%d, want 1/2", seed, step)
	}
	// The ordinary channel/consumer entities survive.
	var plain int
	for _, e := range doc.Entities {
		if e.Kind == "message_channel" || e.Kind == "function" {
			plain++
		}
	}
	if plain != 2 {
		t.Errorf("ordinary entities must survive Apply, got %d", plain)
	}
}

// TestMergeInto_FreshReplaces: a fresh sidecar drives REPLACE via MergeInto.
func TestMergeInto_FreshReplaces(t *testing.T) {
	dir := t.TempDir()
	writeSingleFileGen(t, dir)
	ents, rels := crossRepoDelta()
	if err := flows.Upsert(dir, ents, rels); err != nil {
		t.Fatal(err)
	}
	doc := bakedDoc()
	if !flows.MergeInto(dir, doc) {
		t.Fatal("MergeInto should report applied for a fresh sidecar")
	}
	var procs int
	for _, e := range doc.Entities {
		if e.Kind == "SCOPE.Process" {
			procs++
		}
	}
	if procs != 1 {
		t.Errorf("want 1 process after MergeInto, got %d", procs)
	}
}

// TestMergeInto_StaleFallsBackToBaked: after a reindex the sidecar is stale;
// MergeInto is a no-op and the baked intra flow survives unchanged (degraded
// but valid — never empty, never doubled).
func TestMergeInto_StaleFallsBackToBaked(t *testing.T) {
	dir := t.TempDir()
	writeSingleFileGen(t, dir)
	ents, rels := crossRepoDelta()
	if err := flows.Upsert(dir, ents, rels); err != nil {
		t.Fatal(err)
	}
	// Reindex: new generation → source_key mismatch.
	if err := fbwriter.WriteAtomic(filepath.Join(dir, graph.GenFileName(2)), bakedDoc()); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointer(dir, graph.GenFileName(2)); err != nil {
		t.Fatal(err)
	}
	doc := bakedDoc()
	if flows.MergeInto(dir, doc) {
		t.Fatal("MergeInto should be a no-op for a stale sidecar")
	}
	// Baked intra flow survives.
	var procs []string
	for _, e := range doc.Entities {
		if e.Kind == "SCOPE.Process" {
			procs = append(procs, e.ID)
		}
	}
	if len(procs) != 1 || procs[0] != "baked-proc" {
		t.Errorf("stale fallback must show the baked intra flow, got %v", procs)
	}
}

// TestMergeInto_AbsentNoOp: no sidecar → baked flows shown exactly as today.
func TestMergeInto_AbsentNoOp(t *testing.T) {
	dir := t.TempDir()
	writeSingleFileGen(t, dir)
	doc := bakedDoc()
	if flows.MergeInto(dir, doc) {
		t.Fatal("MergeInto should be a no-op when no sidecar exists")
	}
	if len(doc.Entities) != 3 {
		t.Errorf("single-repo parity: entity set must be untouched, got %d", len(doc.Entities))
	}
}

func TestRead_Corrupt(t *testing.T) {
	dir := t.TempDir()
	writeSingleFileGen(t, dir)
	if err := os.WriteFile(flows.Path(dir), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if sc, ok := flows.Read(dir); ok || sc != nil {
		t.Errorf("corrupt sidecar: want (nil,false), got (%v,%v)", sc, ok)
	}
}

func TestUpsertRead_SegmentSetRoundtrip(t *testing.T) {
	dir := t.TempDir()
	writeSegmentSet(t, dir, 3)
	ents, rels := crossRepoDelta()
	if err := flows.Upsert(dir, ents, rels); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	sc, ok := flows.Read(dir)
	if !ok {
		t.Fatal("Read not-ok for segment-set sidecar")
	}
	if len(sc.SourceKey) < 4 || sc.SourceKey[:4] != "seg:" {
		t.Errorf("segment-set SourceKey = %q, want seg: prefix", sc.SourceKey)
	}
}

func writeSegmentSet(t *testing.T, dir string, gen uint64) {
	t.Helper()
	genDirName := graph.GenDirName(gen)
	genDir := filepath.Join(dir, genDirName)
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	docs := []*graph.Document{
		{Repo: "seg", Entities: []graph.Entity{{ID: "a", Kind: "function", Name: "A"}}},
		{Repo: "seg", Entities: []graph.Entity{{ID: "m", Kind: "function", Name: "M"}}},
	}
	m := &graph.Manifest{FormatVersion: graph.ManifestFormatVersion}
	for i, doc := range docs {
		name := graph.SegmentFileName(i)
		if err := fbwriter.WriteAtomic(filepath.Join(genDir, name), doc); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		seg := graph.SegmentMeta{File: name, Kind: graph.SegmentEntities, EntityCount: len(doc.Entities)}
		ids := make([]string, 0, len(doc.Entities))
		for _, e := range doc.Entities {
			ids = append(ids, e.ID)
		}
		sort.Strings(ids)
		seg.MinKey, seg.MaxKey = ids[0], ids[len(ids)-1]
		m.Segments = append(m.Segments, seg)
	}
	if err := graph.WriteManifest(genDir, m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := graph.WriteCurrentPointerRaw(dir, genDirName); err != nil {
		t.Fatalf("write current pointer: %v", err)
	}
}

func TestWriteTo_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	ents, rels := crossRepoDelta()
	sc := &flows.Sidecar{Version: 1, SourceKey: "k", Entities: ents, Relationships: rels}
	if err := flows.WriteTo(flows.Path(dir), sc); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(flows.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	var got flows.Sidecar
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("written file is not valid JSON: %v", err)
	}
	if len(got.Entities) != 1 || got.Entities[0].ID != "xrepo-proc" {
		t.Errorf("roundtrip Entities = %+v", got.Entities)
	}
}
