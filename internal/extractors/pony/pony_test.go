package pony_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/pony"
	"github.com/cajasmota/grafel/internal/types"
)

// runPony runs the extractor on raw source and returns entity records.
func runPony(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("pony")
	if !ok {
		t.Fatal("pony extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "pony",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func ponyFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func ponyHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	for i := range ents {
		if ents[i].Name != name || ents[i].Kind != kind {
			continue
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == edgeKind && r.ToID == toID {
				return true
			}
		}
	}
	return false
}

// TestPony_Registered verifies the extractor is in the registry.
func TestPony_Registered(t *testing.T) {
	_, ok := extractor.Get("pony")
	if !ok {
		t.Fatal("pony extractor not registered")
	}
}

// TestPony_EmptyInput returns zero entities for empty content.
func TestPony_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("pony")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.pony",
		Content:  []byte{},
		Language: "pony",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ents) != 0 {
		t.Errorf("expected 0 entities, got %d", len(ents))
	}
}

// TestPony_ActorDiscovery — actor declarations extracted as SCOPE.Component(actor).
func TestPony_ActorDiscovery(t *testing.T) {
	src := `
actor Main
  new create(env: Env) =>
    env.out.print("Hello, Pony!")

actor Counter
  var _count: U64 = 0

  new create() =>
    None

  be increment() =>
    _count = _count + 1

  be reset() =>
    _count = 0

  fun count(): U64 =>
    _count
`
	ents := runPony(t, src, "main.pony")

	main := ponyFind(ents, "Main", "SCOPE.Component")
	if main == nil {
		t.Fatal("expected Main actor component")
	}
	if main.Subtype != "actor" {
		t.Errorf("expected subtype=actor, got %q", main.Subtype)
	}

	counter := ponyFind(ents, "Counter", "SCOPE.Component")
	if counter == nil {
		t.Fatal("expected Counter actor component")
	}
}

// TestPony_ClassDiscovery — class declarations extracted as SCOPE.Component(class).
func TestPony_ClassDiscovery(t *testing.T) {
	src := `
class Point
  var x: F64
  var y: F64

  new create(x': F64, y': F64) =>
    x = x'
    y = y'

  fun distance(other: Point): F64 =>
    let dx = x - other.x
    let dy = y - other.y
    (dx * dx + dy * dy).sqrt()

class Rectangle
  var _width: F64
  var _height: F64

  new create(w: F64, h: F64) =>
    _width = w
    _height = h

  fun area(): F64 =>
    _width * _height
`
	ents := runPony(t, src, "geometry.pony")

	comps := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			comps[e.Name] = e.Subtype
		}
	}

	for _, name := range []string{"Point", "Rectangle"} {
		if sub, ok := comps[name]; !ok {
			t.Errorf("expected class %q to be extracted; comps=%v", name, comps)
		} else if sub != "class" {
			t.Errorf("class %q: expected subtype=class, got %q", name, sub)
		}
	}
}

// TestPony_PrimitiveDiscovery — primitive declarations as SCOPE.Component(primitive).
func TestPony_PrimitiveDiscovery(t *testing.T) {
	src := `
primitive None
primitive Bool
primitive U64
`
	ents := runPony(t, src, "primitives.pony")

	comps := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			comps[e.Name] = e.Subtype
		}
	}

	for _, name := range []string{"None", "Bool", "U64"} {
		if sub, ok := comps[name]; !ok {
			t.Errorf("expected primitive %q to be extracted", name)
		} else if sub != "primitive" {
			t.Errorf("primitive %q: expected subtype=primitive, got %q", name, sub)
		}
	}
}

// TestPony_FunDiscovery — fun declarations extracted as SCOPE.Operation(function).
func TestPony_FunDiscovery(t *testing.T) {
	src := `
class Calculator
  fun add(a: I64, b: I64): I64 =>
    a + b

  fun subtract(a: I64, b: I64): I64 =>
    a - b

  fun multiply(a: I64, b: I64): I64 =>
    a * b
`
	ents := runPony(t, src, "calc.pony")

	ops := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			ops[e.Name] = e.Subtype
		}
	}

	for _, name := range []string{"Calculator.add", "Calculator.subtract", "Calculator.multiply"} {
		if sub, ok := ops[name]; !ok {
			t.Errorf("expected function %q to be extracted; ops=%v", name, ops)
		} else if sub != "function" {
			t.Errorf("function %q: expected subtype=function, got %q", name, sub)
		}
	}
}

// TestPony_BehaviorDiscovery — be declarations extracted as SCOPE.Operation(behavior).
func TestPony_BehaviorDiscovery(t *testing.T) {
	src := `
actor Worker
  be process(data: String) =>
    // handle data
    None

  be shutdown() =>
    None
`
	ents := runPony(t, src, "worker.pony")

	ops := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			ops[e.Name] = e.Subtype
		}
	}

	for _, name := range []string{"Worker.process", "Worker.shutdown"} {
		if sub, ok := ops[name]; !ok {
			t.Errorf("expected behavior %q to be extracted; ops=%v", name, ops)
		} else if sub != "behavior" {
			t.Errorf("behavior %q: expected subtype=behavior, got %q", name, sub)
		}
	}
}

// TestPony_ConstructorDiscovery — new declarations extracted as SCOPE.Operation(constructor).
func TestPony_ConstructorDiscovery(t *testing.T) {
	src := `
class MyClass
  new create() =>
    None

  new from_string(s: String) =>
    None
`
	ents := runPony(t, src, "myclass.pony")

	ops := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			ops[e.Name] = e.Subtype
		}
	}

	for _, name := range []string{"MyClass.create", "MyClass.from_string"} {
		if sub, ok := ops[name]; !ok {
			t.Errorf("expected constructor %q to be extracted; ops=%v", name, ops)
		} else if sub != "constructor" {
			t.Errorf("constructor %q: expected subtype=constructor, got %q", name, sub)
		}
	}
}

// TestPony_ImportEdges — use statements emit IMPORTS edges.
func TestPony_ImportEdges(t *testing.T) {
	src := `use "collections"
use "files"
use io = "buffered"

actor Main
  new create(env: Env) =>
    None
`
	ents := runPony(t, src, "main.pony")

	wantImports := map[string]bool{
		"collections": false,
		"files":       false,
		"buffered":    false,
	}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				if _, ok := wantImports[r.ToID]; ok {
					wantImports[r.ToID] = true
				}
			}
		}
	}
	for mod, found := range wantImports {
		if !found {
			t.Errorf("expected IMPORTS edge for %q", mod)
		}
	}
}

// TestPony_LanguageTagged — all relationships carry language=pony.
func TestPony_LanguageTagged(t *testing.T) {
	src := `use "collections"

actor Main
  new create(env: Env) =>
    None
`
	ents := runPony(t, src, "main.pony")
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Properties == nil || r.Properties.Get("language") != "pony" {
				t.Errorf("rel %s→%s missing language=pony (got %v)", r.Kind, r.ToID, r.Properties)
			}
		}
	}
}

// TestPony_ActorMiniFixture — synthetic actor-based chat server fixture for recall.
func TestPony_ActorMiniFixture(t *testing.T) {
	src := `use "collections"
use "net"

primitive Connected
primitive Disconnected

interface Hashable
  fun hash(): USize

actor ChatRoom
  let _name: String
  var _clients: Map[String, TCPConnection tag] = Map[String, TCPConnection tag]

  new create(name: String) =>
    _name = name

  be join(id: String, conn: TCPConnection tag) =>
    _clients(id) = conn
    broadcast("* " + id + " joined " + _name)

  be leave(id: String) =>
    try
      _clients.remove(id)?
    end
    broadcast("* " + id + " left " + _name)

  be send(id: String, msg: String) =>
    broadcast(id + ": " + msg)

  fun broadcast(msg: String) =>
    for conn in _clients.values() do
      conn.write(msg)
    end

actor Client
  let _id: String
  let _room: ChatRoom tag

  new create(id: String, room: ChatRoom tag) =>
    _id = id
    _room = room

  be message(msg: String) =>
    _room.send(_id, msg)

  be disconnect() =>
    _room.leave(_id)

class MessageBuffer
  var _messages: Array[String] = Array[String]

  new create() =>
    None

  fun ref push(msg: String) =>
    _messages.push(msg)

  fun ref pop(): (String | None) =>
    try
      _messages.pop()?
    else
      None
    end
`
	ents := runPony(t, src, "chat.pony")

	wantComps := []string{"ChatRoom", "Client", "MessageBuffer", "Connected", "Disconnected"}
	wantOps := []string{
		"ChatRoom.create", "ChatRoom.join", "ChatRoom.leave", "ChatRoom.send", "ChatRoom.broadcast",
		"Client.create", "Client.message", "Client.disconnect",
		"MessageBuffer.create", "MessageBuffer.push", "MessageBuffer.pop",
	}
	wantImports := []string{"collections", "net"}

	foundComps := make(map[string]bool)
	foundOps := make(map[string]bool)
	foundImports := make(map[string]bool)

	for _, e := range ents {
		switch e.Kind {
		case "SCOPE.Component":
			foundComps[e.Name] = true
		case "SCOPE.Operation":
			foundOps[e.Name] = true
		}
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				foundImports[r.ToID] = true
			}
		}
	}

	compHits := 0
	for _, name := range wantComps {
		if foundComps[name] {
			compHits++
		} else {
			t.Logf("missing component: %s", name)
		}
	}
	opHits := 0
	for _, name := range wantOps {
		if foundOps[name] {
			opHits++
		} else {
			t.Logf("missing operation: %s", name)
		}
	}
	importHits := 0
	for _, mod := range wantImports {
		if foundImports[mod] {
			importHits++
		} else {
			t.Logf("missing import: %s", mod)
		}
	}

	totalWant := len(wantComps) + len(wantOps) + len(wantImports)
	totalFound := compHits + opHits + importHits
	recall := float64(totalFound) / float64(totalWant) * 100

	t.Logf("Pony chat fixture recall: %d/%d (%.0f%%): comps=%d/%d ops=%d/%d imports=%d/%d",
		totalFound, totalWant, recall,
		compHits, len(wantComps),
		opHits, len(wantOps),
		importHits, len(wantImports))

	if recall < 80.0 {
		t.Errorf("entity recall %.0f%% below 80%% threshold (%d/%d found)",
			recall, totalFound, totalWant)
	}
}
