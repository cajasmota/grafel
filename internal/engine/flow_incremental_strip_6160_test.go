package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// #6160 — Path B (cmd/grafel's full-pipeline incremental rebuild) needs the
// same prior-flow strip Path A has had since #5309 layer 3, but it must be able
// to strip ONLY the walkers it is about to re-run: `--skip-pass=process-flow`
// / `--skip-pass=event-flow` mean the corresponding walker will NOT regenerate
// what a strip removed, and stripping it anyway would be a plain deletion of
// user-visible graph rows.
//
// StripFlows is therefore the selectable form; stripFlows(doc) is
// StripFlows(doc, true, true) and keeps Path A's behaviour byte-identical.

func fi6160Doc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "op", Kind: "SCOPE.Operation", Name: "op", SourceFile: "a.py"},
			{ID: "proc", Kind: EntityKindProcess, Name: "p", SourceFile: "a.py"},
			{ID: "ef", Kind: EntityKindEventFlow, Name: "e", SourceFile: "a.py"},
		},
		Relationships: []graph.Relationship{
			{ID: "calls", FromID: "op", ToID: "op", Kind: RelationshipKindCalls},
			{ID: "step", FromID: "proc", ToID: "op", Kind: RelationshipKindStepInProcess},
			{ID: "entry", FromID: "op", ToID: "proc", Kind: RelationshipKindEntryPointOf},
			{ID: "estep", FromID: "ef", ToID: "op", Kind: RelationshipKindStepInEventFlow},
			{ID: "eseed", FromID: "op", ToID: "ef", Kind: RelationshipKindSeedOfEventFlow},
		},
	}
}

func fi6160IDs(d *graph.Document) (ents, rels map[string]bool) {
	ents, rels = map[string]bool{}, map[string]bool{}
	for _, e := range d.Entities {
		ents[e.ID] = true
	}
	for _, r := range d.Relationships {
		rels[r.ID] = true
	}
	return ents, rels
}

func TestStripFlows_ProcessOnly_LeavesEventFlowIntact(t *testing.T) {
	doc := fi6160Doc()
	StripFlows(doc, true, false)
	ents, rels := fi6160IDs(doc)

	if ents["proc"] || rels["step"] || rels["entry"] {
		t.Errorf("process-flow artefacts survived a process strip: ents=%v rels=%v", ents, rels)
	}
	if !ents["ef"] || !rels["estep"] || !rels["eseed"] {
		t.Errorf("event-flow artefacts were deleted by a PROCESS-only strip — the "+
			"event-flow walker is not going to put them back: ents=%v rels=%v", ents, rels)
	}
	if !ents["op"] || !rels["calls"] {
		t.Errorf("non-flow rows were deleted: ents=%v rels=%v", ents, rels)
	}
}

func TestStripFlows_EventFlowOnly_LeavesProcessIntact(t *testing.T) {
	doc := fi6160Doc()
	StripFlows(doc, false, true)
	ents, rels := fi6160IDs(doc)

	if ents["ef"] || rels["estep"] || rels["eseed"] {
		t.Errorf("event-flow artefacts survived an event strip: ents=%v rels=%v", ents, rels)
	}
	if !ents["proc"] || !rels["step"] || !rels["entry"] {
		t.Errorf("process-flow artefacts were deleted by an EVENT-only strip — the "+
			"process-flow walker is not going to put them back: ents=%v rels=%v", ents, rels)
	}
}

func TestStripFlows_NeitherSelected_IsANoOp(t *testing.T) {
	doc := fi6160Doc()
	ents, rels := StripFlows(doc, false, false)
	if ents != 0 || rels != 0 {
		t.Fatalf("StripFlows(doc, false, false) removed %d entities / %d rels, want 0/0", ents, rels)
	}
	if len(doc.Entities) != 3 || len(doc.Relationships) != 5 {
		t.Fatalf("document mutated by a no-op strip: %d entities / %d rels",
			len(doc.Entities), len(doc.Relationships))
	}
}

// stripFlows (Path A's caller) must stay exactly "strip both".
func TestStripFlows_BothSelected_MatchesPathAStrip(t *testing.T) {
	a := fi6160Doc()
	b := fi6160Doc()
	ea, ra := stripFlows(a)
	eb, rb := StripFlows(b, true, true)
	if ea != eb || ra != rb {
		t.Fatalf("stripFlows=%d/%d != StripFlows(both)=%d/%d", ea, ra, eb, rb)
	}
	if ea != 2 || ra != 4 {
		t.Fatalf("stripFlows removed %d entities / %d rels, want 2/4", ea, ra)
	}
}
