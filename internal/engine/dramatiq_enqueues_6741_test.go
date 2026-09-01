// #6741 arm 4 — the Python queue-dispatch hop gets ONE pair, and it is
// ENQUEUES.
//
// #6490 arm B recorded the divergence this file settles: `python-rq-mini`
// emits ENQUEUES for `queue.enqueue(send_email, …)` while `python-dramatiq-mini`
// emitted only the generic REFERENCES for the structurally identical
// `charge_card.send(…)`. Same language, same producer shape, two different
// answers to "what enqueues onto this handler".
//
// ADR-0028 §3 says to pick one pair for the hop rather than add a third edge
// beside them, and §6 makes the fixtures' nice_to_have rows the specification.
// `python-dramatiq-mini`'s row names ENQUEUES, and RQ — the only other Python
// queue framework in the corpus — already ships it, targeting Function:<name>
// exactly as this does. So dramatiq joins ENQUEUES; no PRODUCES edge is
// emitted for either Python framework (see TestPythonQueueHop_NoProducesBeside-
// Enqueues, which pins that ADR-0028 §3 constraint against a later arm).
package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// `actor.send(...)` links the producer's enclosing function to the consumer
// Function:<actor>, mirroring RQ's target convention.
func TestScheduledJobs_PyDramatiq_SendRef(t *testing.T) {
	src := `import dramatiq
from workers.billing import charge_card

def process_checkout(user_id, amount):
    charge_card.send(user_id, amount)
`
	_, rels := runScheduledDetect(t, "python", "api/checkout.py", src)
	enq := enqueuesByFramework(rels, "dramatiq")
	if len(enq) != 1 {
		t.Fatalf("expected exactly 1 dramatiq ENQUEUES edge, got %d (%v)", len(enq), enq)
	}
	e := enq[0]
	if e.FromID != "SCOPE.Operation:process_checkout" {
		t.Errorf("expected ENQUEUES from process_checkout, got %q", e.FromID)
	}
	if e.ToID != "Function:charge_card" {
		t.Errorf("expected ENQUEUES to Function:charge_card, got %q", e.ToID)
	}
}

// `actor.send_with_options(...)` is the same hop through dramatiq's options
// form. It is also the framework evidence for the file: the method name is
// dramatiq-specific, unlike the bare `.send(`.
func TestScheduledJobs_PyDramatiq_SendWithOptions(t *testing.T) {
	src := `from workers.billing import send_receipt

def process_checkout(user_id, amount):
    send_receipt.send_with_options(args=[user_id, amount], delay=2000)
`
	_, rels := runScheduledDetect(t, "python", "api/checkout.py", src)
	enq := enqueuesByFramework(rels, "dramatiq")
	if len(enq) != 1 {
		t.Fatalf("expected exactly 1 dramatiq ENQUEUES edge, got %d (%v)", len(enq), enq)
	}
	if enq[0].ToID != "Function:send_receipt" {
		t.Errorf("expected ENQUEUES to Function:send_receipt, got %q", enq[0].ToID)
	}
}

// The producing file in `python-dramatiq-mini` does NOT import dramatiq — it
// imports the actors from workers.billing. `send_with_options` in the same file
// is what identifies it as a dramatiq dispatch site, and it must carry the bare
// `.send(` call beside it.
func TestScheduledJobs_PyDramatiq_SendWithOptionsIsEvidenceForBareSend(t *testing.T) {
	src := `from workers.billing import charge_card, send_receipt

def process_checkout(user_id, amount):
    charge_card.send(user_id, amount)
    send_receipt.send_with_options(args=[user_id, amount], delay=2000)
`
	_, rels := runScheduledDetect(t, "python", "api/checkout.py", src)
	enq := enqueuesByFramework(rels, "dramatiq")
	if len(enq) != 2 {
		t.Fatalf("expected 2 dramatiq ENQUEUES edges, got %d (%v)", len(enq), enq)
	}
	want := map[string]bool{"Function:charge_card": false, "Function:send_receipt": false}
	for _, e := range enq {
		if _, ok := want[e.ToID]; !ok {
			t.Errorf("unexpected ENQUEUES target %q", e.ToID)
			continue
		}
		want[e.ToID] = true
	}
	for target, seen := range want {
		if !seen {
			t.Errorf("missing ENQUEUES edge to %s", target)
		}
	}
}

// Negative — the load-bearing one. `.send(` is generic Python (sockets,
// generators, requests, channels). Without dramatiq evidence in the file the
// pass must stay silent rather than fabricate an edge to a same-named function
// somewhere else in the repo.
func TestScheduledJobs_PyDramatiq_NoEvidence_NoEdge(t *testing.T) {
	src := `def relay(sock, payload):
    sock.send(payload)
`
	_, rels := runScheduledDetect(t, "python", "net/relay.py", src)
	if got := enqueuesByFramework(rels, "dramatiq"); len(got) != 0 {
		t.Errorf("expected no dramatiq ENQUEUES edge without framework evidence, got %v", got)
	}
}

// Negative — an rq file that happens to call `.send(` must not be read as
// dramatiq. This pins that the evidence guard names dramatiq specifically
// rather than "some queue import".
func TestScheduledJobs_PyDramatiq_RQImportIsNotEvidence(t *testing.T) {
	src := `from rq import Queue

def relay(sock, payload):
    sock.send(payload)
`
	_, rels := runScheduledDetect(t, "python", "net/relay.py", src)
	if got := enqueuesByFramework(rels, "dramatiq"); len(got) != 0 {
		t.Errorf("expected no dramatiq ENQUEUES edge from an rq import, got %v", got)
	}
}

// A send inside the actor's own def is a self-enqueue: the edge would be a
// self-loop that says nothing. RQ skips it; so does this.
func TestScheduledJobs_PyDramatiq_SelfSend_NoEdge(t *testing.T) {
	src := `import dramatiq

@dramatiq.actor
def retry_me(n):
    retry_me.send(n + 1)
`
	_, rels := runScheduledDetect(t, "python", "workers/retry.py", src)
	if got := enqueuesByFramework(rels, "dramatiq"); len(got) != 0 {
		t.Errorf("expected no dramatiq ENQUEUES edge for a self-send, got %v", got)
	}
}

// Two dispatches of the same actor from the same function are one hop.
func TestScheduledJobs_PyDramatiq_Dedup(t *testing.T) {
	src := `import dramatiq
from workers.billing import charge_card

def process_checkout(user_id, amount):
    charge_card.send(user_id, amount)
    charge_card.send(user_id, amount * 2)
`
	_, rels := runScheduledDetect(t, "python", "api/checkout.py", src)
	if got := enqueuesByFramework(rels, "dramatiq"); len(got) != 1 {
		t.Errorf("expected 1 deduped dramatiq ENQUEUES edge, got %d (%v)", len(got), got)
	}
}

// A module-level `actor.send(...)` has no enclosing operation to be the FROM
// endpoint, so there is no edge to emit. RQ behaves the same way; recorded here
// because it is the honest-partial that keeps the pass from inventing a
// producer node.
func TestScheduledJobs_PyDramatiq_ModuleLevelSend_NoEdge(t *testing.T) {
	src := `import dramatiq
from workers.billing import charge_card

charge_card.send(1, 2.0)
`
	_, rels := runScheduledDetect(t, "python", "api/bootstrap.py", src)
	if got := enqueuesByFramework(rels, "dramatiq"); len(got) != 0 {
		t.Errorf("expected no dramatiq ENQUEUES edge for a module-level send, got %v", got)
	}
}

// ADR-0028 §3 — one hop, one pair. Neither Python queue framework may carry a
// PRODUCES edge beside its ENQUEUES for the SAME dispatch site. This is the
// constraint that decided arm 4: `internal/custom/python/rq.go` sections 1-3
// match byte-for-byte the same three call-site shapes synthesizeRQEnqueueEdges
// already covers, so a PRODUCES edge there would be the double-edge the ADR
// forbids; and dramatiq is settled onto ENQUEUES above rather than given a
// third kind of its own.
func TestPythonQueueHop_NoProducesBesideEnqueues(t *testing.T) {
	cases := []struct{ name, path, src string }{
		{"rq", "api/notifications.py", `from rq import Queue
from workers.email import send_email

def notify_user(uid):
    q.enqueue(send_email, uid)
`},
		{"dramatiq", "api/checkout.py", `import dramatiq
from workers.billing import charge_card

def process_checkout(uid):
    charge_card.send(uid)
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, rels := runScheduledDetect(t, "python", tc.path, tc.src)
			if len(enqueuesEdges(rels)) == 0 {
				t.Fatalf("premise broken: expected an ENQUEUES edge for %s", tc.name)
			}
			for _, r := range rels {
				if r.Kind == string(types.RelationshipKindProduces) {
					t.Errorf("ADR-0028 §3 violated: PRODUCES %s -> %s emitted beside ENQUEUES",
						r.FromID, r.ToID)
				}
			}
		})
	}
}
