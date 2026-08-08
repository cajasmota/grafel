package python_test

import "testing"

// The RQ worker entity's name must reproduce the source syntax including the
// list brackets: `Worker([notification_queue, report_queue])`.
//
// rqWorkerRe (rq.go:39-41) captures the text INSIDE the brackets, so the name
// built at rq.go:152 has to re-add them; the surrounding `Worker(` / `)` are
// already re-added there by hand. Dropping only the brackets makes the name
// disagree with the extractor's own doc comment (rq.go:21, "Worker([queue,
// ...]) instantiation") and with the golden expectation in
// internal/quality/golden/python-rq-mini/expected.json, which
// internal/quality/diff.go matches by exact string on Kind\x00Name.
//
// Assertions here compare the FULL name string. A `strings.Contains(name,
// "Worker")` check passes with or without the brackets and would not have
// caught this.
func TestRQWorkerEntityNameKeepsListBrackets_6260(t *testing.T) {
	// Byte-for-byte the body of internal/quality/golden/python-rq-mini/src/worker_runner.py.
	src := `from rq import Queue, Worker
from redis import Redis

redis_conn = Redis()
notification_queue = Queue("notifications", connection=redis_conn)
report_queue = Queue("reports", connection=redis_conn)

# Consumer: runs jobs from notification and report queues
worker = Worker([notification_queue, report_queue], connection=redis_conn)
worker.work()
`

	const wantName = "Worker([notification_queue, report_queue])"
	const wantKind = "SCOPE.Service"

	ents := extract(t, "python_rq", src)

	var names []string
	found := false
	for _, e := range ents {
		if e.Props["pattern_type"] != "worker" {
			continue
		}
		names = append(names, e.Name)
		if e.Name == wantName {
			found = true
			if e.Kind != wantKind {
				t.Errorf("worker entity kind = %q, want %q", e.Kind, wantKind)
			}
		}
	}
	if !found {
		t.Fatalf("no worker entity named %q; worker entity names were %q", wantName, names)
	}
	if len(names) != 1 {
		t.Fatalf("expected exactly one worker entity, got %d: %q", len(names), names)
	}
}

// A single-queue worker keeps the brackets too — the brackets come from the
// source construct, not from the number of queues.
func TestRQWorkerEntityNameSingleQueue_6260(t *testing.T) {
	src := `from rq import Queue
from redis import Redis

redis_conn = Redis()
q = Queue(connection=redis_conn)
worker = Worker([q], connection=redis_conn)
`
	ents := extract(t, "python_rq", src)
	for _, e := range ents {
		if e.Props["pattern_type"] != "worker" {
			continue
		}
		if e.Name != "Worker([q])" {
			t.Fatalf("worker entity name = %q, want %q", e.Name, "Worker([q])")
		}
		return
	}
	t.Fatal("no worker entity emitted")
}
