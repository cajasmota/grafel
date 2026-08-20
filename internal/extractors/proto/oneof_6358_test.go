package proto_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// #6358 — oneof members were silently dropped.
//
// buildMessage scanned message_body's DIRECT children for `field` / `map_field`
// only. The grammar nests a oneof's members one level deeper:
//
//	message_body
//	  field                ← seen
//	  oneof
//	    oneof_field        ← NEVER visited
//	    oneof_field
//
// so a message whose fields live inside a oneof indexed as though those fields
// did not exist: no entity, no CONTAINS edge, no REFERENCES edge, and no skip
// counter or warning anywhere — on a file that parses at error_ratio 0.0000.
// The word "oneof" appeared nowhere in the package.
//
// Scope: MEMBERS ONLY. No entity is emitted for the oneof group itself, so the
// mutual-exclusivity semantics of the tagged union are still unmodelled. That
// is tracked separately; it needs a new 4-part ID form and reshapes every
// per-message CONTAINS count, which a data-loss fix must not smuggle in.
// TestProto_Oneof_GroupIsNotAnEntity below pins that boundary so the follow-up
// is a deliberate change rather than an accident.
// ---------------------------------------------------------------------------

const oneofSrc = `syntax = "proto3";

message Order { string id = 1; }

message Event {
  string id = 1;
  oneof payload {
    string text = 2;
    Order order = 3;
    Status status = 4;
  }
  int32 seq = 5;
}

enum Status { STATUS_UNSPECIFIED = 0; }
`

// TestProto_Oneof_MembersBecomeFieldEntities is the direct data-loss assertion:
// every member of the oneof must exist as a SCOPE.Schema/field entity carrying
// its resolved type, exactly as a top-level field does. Before #6358 the three
// oneof members were absent and only Event.id / Event.seq existed.
func TestProto_Oneof_MembersBecomeFieldEntities(t *testing.T) {
	entities := extract(t, "ev.proto", oneofSrc)

	got := make(map[string]string)
	for _, e := range entities {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" {
			got[e.Name] = e.Properties["type"]
		}
	}
	want := map[string]string{
		"Order.id":     "string",
		"Event.id":     "string",
		"Event.text":   "string", // oneof member, scalar
		"Event.order":  "Order",  // oneof member, named message type
		"Event.status": "Status", // oneof member, named enum type
		"Event.seq":    "int32",
	}
	for n, wt := range want {
		gt, ok := got[n]
		if !ok {
			t.Errorf("no SCOPE.Schema/field entity for %q — oneof members are being dropped", n)
			continue
		}
		if gt != wt {
			t.Errorf("field %q Properties[\"type\"] = %q, want %q", n, gt, wt)
		}
	}
	if len(got) != len(want) {
		t.Errorf("emitted %d field entities (%v), want exactly %d", len(got), got, len(want))
	}

	// A oneof member carries no proto label: `repeated`/`optional` are illegal
	// inside a oneof, so a non-empty label would mean the label reader latched
	// onto the wrong token.
	for _, e := range entities {
		switch e.Name {
		case "Event.text", "Event.order", "Event.status":
			if lbl := e.Properties["label"]; lbl != "" {
				t.Errorf("oneof member %q label = %q, want empty", e.Name, lbl)
			}
			if e.StartLine <= 0 {
				t.Errorf("oneof member %q StartLine = %d, want > 0", e.Name, e.StartLine)
			}
			if e.SourceFile != "ev.proto" || e.Language != "protobuf" {
				t.Errorf("oneof member %q SourceFile=%q Language=%q", e.Name, e.SourceFile, e.Language)
			}
		}
	}
}

// TestProto_Oneof_MembersAreContainedAndReferenced pins the two edge families a
// oneof member must participate in: the message→member CONTAINS edge (same
// Format-B member ref as a plain field) and the message→type REFERENCES edge
// for a named member type. The REFERENCES half is what makes a oneof-only
// message reachable in the graph at all.
func TestProto_Oneof_MembersAreContainedAndReferenced(t *testing.T) {
	entities := extract(t, "ev.proto", oneofSrc)

	var event *types.EntityRecord
	for i := range entities {
		if entities[i].Subtype == "message" && entities[i].Name == "Event" {
			event = &entities[i]
		}
	}
	if event == nil {
		t.Fatal("message Event not found")
	}

	contains := make(map[string]bool)
	refs := make(map[string]string) // ToID → via_field
	for _, r := range event.Relationships {
		switch r.Kind {
		case "CONTAINS":
			contains[r.ToID] = true
		case "REFERENCES":
			refs[r.ToID] = r.Properties.Get("via_field")
		}
	}

	for _, f := range []string{"id", "text", "order", "status", "seq"} {
		want := "scope:schema:column:proto:ev.proto:Event#" + f
		if !contains[want] {
			t.Errorf("message Event missing CONTAINS edge to %q", want)
		}
	}

	orderRef := msgTypeRef("ev.proto", "Order")
	statusRef := msgTypeRef("ev.proto", "Status")
	if via, ok := refs[orderRef]; !ok {
		t.Errorf("missing REFERENCES edge Event → Order (%q) from a oneof member", orderRef)
	} else if via != "order" {
		t.Errorf("Event → Order via_field = %q, want order", via)
	}
	if via, ok := refs[statusRef]; !ok {
		t.Errorf("missing REFERENCES edge Event → Status (%q) from a oneof member", statusRef)
	} else if via != "status" {
		t.Errorf("Event → Status via_field = %q, want status", via)
	}
	// The scalar oneof member must NOT mint a REFERENCES edge.
	for to := range refs {
		if to != orderRef && to != statusRef {
			t.Errorf("unexpected REFERENCES edge from Event to %q", to)
		}
	}
}

// TestProto_Oneof_GroupIsNotAnEntity pins the deliberate scope line of #6358:
// members are recovered, the oneof GROUP is not modelled. If a later change
// starts emitting a group entity (or a group-addressed CONTAINS edge) this
// fails, forcing that design change — a new ID form, and a reshaping of every
// per-message CONTAINS count — to be made on purpose.
func TestProto_Oneof_GroupIsNotAnEntity(t *testing.T) {
	entities := extract(t, "ev.proto", oneofSrc)
	for _, e := range entities {
		if e.Subtype == "oneof" || e.Name == "Event.payload" || e.Name == "payload" {
			t.Errorf("unexpected oneof-group entity %+v — #6358 is members-only", e)
		}
		for _, r := range e.Relationships {
			if r.ToID == "scope:schema:column:proto:ev.proto:Event#payload" {
				t.Errorf("unexpected edge addressing the oneof group itself: %+v", r)
			}
		}
	}
}

// TestProto_Oneof_MemberDedupeIsPerMessage pins the dedupe scope chosen in
// #6358: `seen` is keyed per MESSAGE, not per oneof block.
//
// proto3 requires field names to be unique across the WHOLE message, so the
// fixture below (`v` in two sibling oneofs) is invalid proto. The Format-B
// member ref is likewise per-message (…:<file>:Dup#v), so two entities named
// Dup.v would collide at one address and resolve/refs.go would un-bind the
// pair as ambiguous. Emitting the first and dropping the second keeps the
// dedupe and the address space in agreement; this test says so out loud rather
// than letting a future reader assume per-oneof scoping was intended.
func TestProto_Oneof_MemberDedupeIsPerMessage(t *testing.T) {
	src := `syntax = "proto3";
message Dup {
  oneof a {
    string v = 1;
  }
  oneof b {
    int32 v = 2;
  }
}
`
	entities := extract(t, "dup.proto", src)
	n := 0
	var typ string
	for _, e := range entities {
		if e.Subtype == "field" && e.Name == "Dup.v" {
			n++
			typ = e.Properties["type"]
		}
	}
	if n != 1 {
		t.Fatalf("emitted %d entities named Dup.v, want exactly 1 (per-message dedupe)", n)
	}
	if typ != "string" {
		t.Errorf("Dup.v type = %q, want string (the FIRST occurrence wins)", typ)
	}
	// And exactly one CONTAINS edge to the shared address, so the edge count
	// matches the entity count.
	edges := 0
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == "CONTAINS" && r.ToID == "scope:schema:column:proto:dup.proto:Dup#v" {
				edges++
			}
		}
	}
	if edges != 1 {
		t.Errorf("%d CONTAINS edges to Dup#v, want 1", edges)
	}
}
