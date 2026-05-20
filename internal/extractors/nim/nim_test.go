package nim_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/nim"
	"github.com/cajasmota/archigraph/internal/types"
)

// runNim runs the extractor on raw source and returns entity records.
func runNim(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("nim")
	if !ok {
		t.Fatal("nim extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "nim",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func nimFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func nimHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
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

// TestNim_Registered verifies the extractor is in the registry.
func TestNim_Registered(t *testing.T) {
	_, ok := extractor.Get("nim")
	if !ok {
		t.Fatal("nim extractor not registered")
	}
}

// TestNim_EmptyInput returns zero entities for empty content.
func TestNim_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("nim")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.nim",
		Content:  []byte{},
		Language: "nim",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ents) != 0 {
		t.Errorf("expected 0 entities, got %d", len(ents))
	}
}

// TestNim_ProcDiscovery — proc/func/method/template/macro extracted as SCOPE.Operation.
func TestNim_ProcDiscovery(t *testing.T) {
	src := `import strutils

proc greet(name: string): string =
  result = "Hello, " & name

func add(a, b: int): int =
  result = a + b

proc privateHelper() =
  discard

template withLock(body: untyped) =
  acquire()
  body
  release()

macro debugMacro(x: untyped): untyped =
  result = x
`
	ents := runNim(t, src, "app.nim")

	ops := make(map[string]bool)
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			ops[e.Name] = true
			if e.Language != "nim" {
				t.Errorf("entity %q: expected Language=nim, got %q", e.Name, e.Language)
			}
		}
	}

	for _, want := range []string{"greet", "add", "privateHelper", "withLock", "debugMacro"} {
		if !ops[want] {
			t.Errorf("expected proc/func/template/macro %q to be extracted", want)
		}
	}
}

// TestNim_ExportMarkerStripped — trailing `*` export marker is stripped from names.
func TestNim_ExportMarkerStripped(t *testing.T) {
	src := `proc publicProc*(x: int): int =
  x * 2

type MyObj* = object
  value: int
`
	ents := runNim(t, src, "export.nim")

	if nimFind(ents, "publicProc", "SCOPE.Operation") == nil {
		t.Error("expected 'publicProc' (without *) to be extracted")
	}
	if nimFind(ents, "MyObj", "SCOPE.Component") == nil {
		t.Error("expected 'MyObj' (without *) to be extracted")
	}
}

// TestNim_TypeDiscovery — object/enum/tuple types extracted as SCOPE.Component.
func TestNim_TypeDiscovery(t *testing.T) {
	src := `type
  Person = object
    name: string
    age: int

  Direction = enum
    North, South, East, West

  Point = tuple
    x, y: float

  UserId = distinct int
`
	ents := runNim(t, src, "types.nim")

	comps := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype != "" {
			comps[e.Name] = e.Subtype
		}
	}

	checks := map[string]string{
		"Person":    "object",
		"Direction": "enum",
		"Point":     "tuple",
	}
	for name, wantSubtype := range checks {
		got, ok := comps[name]
		if !ok {
			t.Errorf("expected type %q to be extracted", name)
		} else if got != wantSubtype {
			t.Errorf("type %q: expected subtype=%q, got %q", name, wantSubtype, got)
		}
	}
}

// TestNim_ImportEdges — import/from-import statements emit IMPORTS edges.
func TestNim_ImportEdges(t *testing.T) {
	src := `import strutils, sequtils
import std/asyncdispatch
from tables import initTable, newTable

proc main() =
  discard
`
	ents := runNim(t, src, "main.nim")

	wantImports := map[string]bool{
		"strutils":         false,
		"sequtils":         false,
		"std/asyncdispatch": false,
		"tables":           false,
	}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				if _, ok := wantImports[r.ToID]; ok {
					wantImports[r.ToID] = true
					if r.FromID != "main.nim" {
						t.Errorf("IMPORTS %q: expected FromID=main.nim, got %q", r.ToID, r.FromID)
					}
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

// TestNim_CallsEdges — proc invocations emit CALLS edges.
func TestNim_CallsEdges(t *testing.T) {
	src := `import strutils

proc helper(x: int): int =
  x * 2

proc caller(n: int): int =
  let h = helper(n)
  let s = $h
  result = parseInt(s)
`
	ents := runNim(t, src, "calls.nim")

	if !nimHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Error("expected CALLS caller→helper")
	}
	if !nimHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "parseInt") {
		t.Error("expected CALLS caller→parseInt")
	}
}

// TestNim_SelfRecursionExcluded — self-recursive calls are not emitted.
func TestNim_SelfRecursionExcluded(t *testing.T) {
	src := `proc fib(n: int): int =
  if n <= 1:
    return n
  return fib(n-1) + fib(n-2)
`
	ents := runNim(t, src, "fib.nim")
	if nimHasRel(ents, "fib", "SCOPE.Operation", "CALLS", "fib") {
		t.Error("self-recursion CALLS should be filtered")
	}
}

// TestNim_CallsDeduped — duplicate call targets are emitted once.
func TestNim_CallsDeduped(t *testing.T) {
	src := `proc worker(x: int) = discard x

proc runner() =
  worker(1)
  worker(2)
  worker(3)
`
	ents := runNim(t, src, "dedup.nim")
	count := 0
	for _, e := range ents {
		if e.Name == "runner" && e.Kind == "SCOPE.Operation" {
			for _, r := range e.Relationships {
				if r.Kind == "CALLS" && r.ToID == "worker" {
					count++
				}
			}
		}
	}
	if count != 1 {
		t.Errorf("expected 1 CALLS runner→worker (deduped), got %d", count)
	}
}

// TestNim_ContainsEdges — type with methods in its parameters gets CONTAINS edges.
func TestNim_ContainsEdges(t *testing.T) {
	src := `type Animal = object
  name: string
  sound: string

proc speak(a: Animal): string =
  a.name & " says " & a.sound

proc rename(a: var Animal, newName: string) =
  a.name = newName
`
	ents := runNim(t, src, "animal.nim")

	animal := nimFind(ents, "Animal", "SCOPE.Component")
	if animal == nil {
		t.Fatal("expected Animal component")
	}

	wantContains := []string{"speak", "rename"}
	for _, methodName := range wantContains {
		wantRef := "scope:operation:method:nim:animal.nim:" + methodName
		found := false
		for _, r := range animal.Relationships {
			if r.Kind == "CONTAINS" && r.ToID == wantRef {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected Animal CONTAINS %s (ref=%s)", methodName, wantRef)
		}
	}
}

// TestNim_LanguageTagged — all relationships carry language=nim.
func TestNim_LanguageTagged(t *testing.T) {
	src := `import strutils

type Node = object
  val: int

proc process(n: Node): string =
  $n.val
`
	ents := runNim(t, src, "tag.nim")
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Properties == nil || r.Properties["language"] != "nim" {
				t.Errorf("rel %s→%s missing language=nim (got %v)", r.Kind, r.ToID, r.Properties)
			}
		}
	}
}

// TestNim_JesterWebServer — synthetic Jester-style fixture for entity recall.
func TestNim_JesterWebServer(t *testing.T) {
	src := `import jester, asyncdispatch, strutils, json

type
  AppConfig* = object
    host*: string
    port*: int

  ApiError* = object of CatchableError
    code*: int
    message*: string

proc newConfig*(host: string, port: int): AppConfig =
  result = AppConfig(host: host, port: port)

proc handleRequest(req: Request): Future[void] {.async.} =
  let body = req.body
  discard parseJson(body)

proc startServer*(cfg: AppConfig) {.async.} =
  let settings = newSettings(port = Port(cfg.port), bindAddr = cfg.host)
  var jester = initJester(handleRequest, settings = settings)
  jester.serve()

proc main() =
  let cfg = newConfig("0.0.0.0", 8080)
  waitFor startServer(cfg)

when isMainModule:
  main()
`
	ents := runNim(t, src, "server.nim")

	// Measure entity recall against expected entities.
	wantOps := []string{"newConfig", "handleRequest", "startServer", "main"}
	wantComps := []string{"AppConfig", "ApiError"}
	wantImports := []string{"jester", "asyncdispatch", "strutils", "json"}

	foundOps := make(map[string]bool)
	foundComps := make(map[string]bool)
	foundImports := make(map[string]bool)

	for _, e := range ents {
		switch e.Kind {
		case "SCOPE.Operation":
			foundOps[e.Name] = true
		case "SCOPE.Component":
			foundComps[e.Name] = true
			for _, r := range e.Relationships {
				if r.Kind == "IMPORTS" {
					foundImports[r.ToID] = true
				}
			}
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
	compHits := 0
	for _, name := range wantComps {
		if foundComps[name] {
			compHits++
		} else {
			t.Logf("missing component: %s", name)
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

	totalWant := len(wantOps) + len(wantComps) + len(wantImports)
	totalFound := opHits + compHits + importHits
	recall := float64(totalFound) / float64(totalWant) * 100

	t.Logf("Jester fixture recall: %d/%d (%.0f%%): ops=%d/%d comps=%d/%d imports=%d/%d",
		totalFound, totalWant, recall,
		opHits, len(wantOps),
		compHits, len(wantComps),
		importHits, len(wantImports))

	if recall < 80.0 {
		t.Errorf("entity recall %.0f%% below 80%% threshold (%d/%d found)",
			recall, totalFound, totalWant)
	}
}

// TestNim_AsyncChronosFixture — async Nim with chronos-style patterns.
func TestNim_AsyncChronosFixture(t *testing.T) {
	src := `import chronos, strutils

type HttpClient* = object
  baseUrl*: string
  timeout*: Duration

proc newHttpClient*(url: string): HttpClient =
  HttpClient(baseUrl: url, timeout: seconds(30))

proc get*(client: HttpClient, path: string): Future[string] {.async.} =
  let url = client.baseUrl & path
  let resp = await fetch(url)
  return resp.body

proc post*(client: HttpClient, path: string, body: string): Future[string] {.async.} =
  let url = client.baseUrl & path
  let resp = await send(url, body)
  return resp.body
`
	ents := runNim(t, src, "httpclient.nim")

	ops := make(map[string]bool)
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			ops[e.Name] = true
		}
	}

	for _, want := range []string{"newHttpClient", "get", "post"} {
		if !ops[want] {
			t.Errorf("expected proc %q to be extracted", want)
		}
	}

	client := nimFind(ents, "HttpClient", "SCOPE.Component")
	if client == nil {
		t.Error("expected HttpClient component")
	}
}
