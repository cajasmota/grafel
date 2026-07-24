package pony_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// hasTag reports whether e carries tag.
func hasTag(e *types.EntityRecord, tag string) bool {
	if e == nil {
		return false
	}
	for _, t := range e.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

const actorSrc = `use "collections"

actor Worker
  let _env: Env
  new create(env: Env) =>
    _env = env
  be run(payload: String) =>
    _env.out.print(payload)
  be stop() =>
    None

actor Main
  new create(env: Env) =>
    let w: Worker = Worker(env)
    w.run("hello")
    w.stop()
`

func TestActorTopology_TagsActorsAndBehaviours(t *testing.T) {
	ents := runPony(t, actorSrc, "/proj/main.pony")

	worker := ponyFind(ents, "Worker", "SCOPE.Component")
	if worker == nil {
		t.Fatal("Worker actor not extracted")
	}
	if !hasTag(worker, "actor") {
		t.Errorf("Worker should be tagged 'actor', tags=%v", worker.Tags)
	}

	run := ponyFind(ents, "Worker.run", "SCOPE.Operation")
	if run == nil {
		t.Fatal("Worker.run behaviour not extracted")
	}
	if !hasTag(run, "pony_behaviour") {
		t.Errorf("Worker.run should be tagged 'pony_behaviour', tags=%v", run.Tags)
	}
	if run.Properties["actor"] != "Worker" {
		t.Errorf("Worker.run actor property = %q, want Worker", run.Properties["actor"])
	}
}

func TestActorTopology_MessageSendEnrichment(t *testing.T) {
	ents := runPony(t, actorSrc, "/proj/main.pony")

	// Main.create sends `w.run(...)` and `w.stop()` — both behaviour sends.
	mainCtor := ponyFind(ents, "Main.create", "SCOPE.Operation")
	if mainCtor == nil {
		t.Fatal("Main.create not extracted")
	}

	foundRun := false
	for _, rel := range mainCtor.Relationships {
		if rel.Kind != "CALLS" || rel.ToID != "run" {
			continue
		}
		foundRun = true
		if rel.Properties.Get("pony_msg_send") != "true" {
			t.Errorf("run CALLS edge not marked pony_msg_send, props=%v", rel.Properties)
		}
		if rel.Properties.Get("pony_msg_receiver") != "w" {
			t.Errorf("run receiver = %q, want w", rel.Properties.Get("pony_msg_receiver"))
		}
		if rel.Properties.Get("pony_msg_behaviour") != "run" {
			t.Errorf("run behaviour = %q, want run", rel.Properties.Get("pony_msg_behaviour"))
		}
		if rel.Properties.Get("pony_msg_actor") != "Worker" {
			t.Errorf("run target actor = %q, want Worker", rel.Properties.Get("pony_msg_actor"))
		}
	}
	if !foundRun {
		t.Error("expected a CALLS edge to behaviour 'run' from Main.create")
	}
	if !hasTag(mainCtor, "pony_msg_out:run") {
		t.Errorf("Main.create should be tagged pony_msg_out:run, tags=%v", mainCtor.Tags)
	}
}

// A file with no actors must not gain any topology tags/props (no-op path).
func TestActorTopology_NoActorsNoOp(t *testing.T) {
	src := `class Greeter
  fun greet(name: String): String =>
    "hi " + name
`
	ents := runPony(t, src, "/proj/greeter.pony")
	for i := range ents {
		e := &ents[i]
		if hasTag(e, "actor") || hasTag(e, "pony_behaviour") {
			t.Errorf("non-actor file gained topology tag on %s: %v", e.Name, e.Tags)
		}
		for _, rel := range e.Relationships {
			if rel.Properties.Get("pony_msg_send") == "true" {
				t.Errorf("non-actor file gained a pony_msg_send edge on %s", e.Name)
			}
		}
	}
}

// A non-behaviour synchronous fun call must NOT be marked as a message send.
func TestActorTopology_SyncFunNotMessage(t *testing.T) {
	src := `actor Worker
  be run() =>
    None
  fun helper(): None =>
    None

actor Main
  new create() =>
    let w: Worker = Worker
    w.helper()
`
	ents := runPony(t, src, "/proj/sync.pony")
	mainCtor := ponyFind(ents, "Main.create", "SCOPE.Operation")
	if mainCtor == nil {
		t.Fatal("Main.create not extracted")
	}
	for _, rel := range mainCtor.Relationships {
		if rel.ToID == "helper" && rel.Properties.Get("pony_msg_send") == "true" {
			t.Error("synchronous fun call 'helper' must not be marked pony_msg_send")
		}
	}
}
