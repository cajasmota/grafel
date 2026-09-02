package python_test

// #6767 — dramatiq's two `task_routing` entities stamped
// `edge_kind: "ROUTES_TO"` while the pass emitted zero relationships.
//
// ROUTES_TO is a declared kind, but nothing in the dramatiq path emits it, and
// the hop the property gestures at is ALREADY carried by a real edge of a
// different kind: `internal/engine/scheduled_jobs_edges.go`'s
// synthesizeDramatiqSendEdges emits ENQUEUES from the dispatching function to
// `Function:<actor>` for `.send(...)` and `.send_with_options(...)` — the very
// call site section 7 stamps. Emitting ROUTES_TO beside it would be the
// ADR-0028 §3 double edge #6741 arm 4 refused for exactly this pass.
//
// So the property goes and no edge replaces it. The routing FACT is not lost:
// `queue_name` + `actor` / `actor_ref` still record which queue carries which
// actor. What goes is the claim that an edge models it.

import "testing"

func TestDramatiqTaskRoutingDoesNotClaimAnEdgeKind(t *testing.T) {
	src := fixtureSchema(t, "dramatiq_task_routing.py")
	ents := extract(t, "python_dramatiq", src)

	routing := 0
	for i := range ents {
		if ents[i].Subtype != "task_routing" {
			continue
		}
		routing++
		if v, ok := ents[i].Props["edge_kind"]; ok {
			t.Errorf("dramatiq task_routing %q stamps edge_kind=%q — a property naming a "+
				"relationship kind is not a relationship, and this hop already ships as "+
				"ENQUEUES (#6767)", ents[i].Name, v)
		}
		// The routing fact itself must survive the property's removal.
		if ents[i].Props["queue_name"] == "" {
			t.Errorf("dramatiq task_routing %q lost queue_name", ents[i].Name)
		}
	}
	if routing == 0 {
		t.Fatal("no task_routing entities extracted — this test is measuring nothing")
	}
}
