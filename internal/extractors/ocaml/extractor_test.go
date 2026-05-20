package ocaml_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/ocaml"
	"github.com/cajasmota/archigraph/internal/types"
)

// runOCaml runs the extractor on raw source and returns entity records.
func runOCaml(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("ocaml")
	if !ok {
		t.Fatal("ocaml extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "ocaml",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func mlFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func mlHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
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

// TestOCaml_Registered verifies the extractor is in the registry.
func TestOCaml_Registered(t *testing.T) {
	_, ok := extractor.Get("ocaml")
	if !ok {
		t.Fatal("ocaml extractor not registered")
	}
}

// TestOCaml_EmptyInput returns zero entities for empty content.
func TestOCaml_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("ocaml")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.ml",
		Content:  []byte{},
		Language: "ocaml",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ents) != 0 {
		t.Errorf("expected 0 entities, got %d", len(ents))
	}
}

// TestOCaml_ModuleDeclaration — explicit module declarations → SCOPE.Component(module).
func TestOCaml_ModuleDeclaration(t *testing.T) {
	src := `module StringMap = Map.Make(String)

module Utils = struct
  let greet name = "Hello, " ^ name
  let double x = x * 2
end
`
	ents := runOCaml(t, src, "utils.ml")

	mod := mlFind(ents, "StringMap", "SCOPE.Component")
	if mod == nil {
		t.Error("expected StringMap module component")
	} else if mod.Subtype != "module" {
		t.Errorf("StringMap: expected subtype=module, got %q", mod.Subtype)
	}

	utils := mlFind(ents, "Utils", "SCOPE.Component")
	if utils == nil {
		t.Error("expected Utils module component")
	} else if utils.Subtype != "module" {
		t.Errorf("Utils: expected subtype=module, got %q", utils.Subtype)
	}
}

// TestOCaml_FunctionDiscovery — let/let rec at top-level → SCOPE.Operation(function).
func TestOCaml_FunctionDiscovery(t *testing.T) {
	src := `let add x y = x + y

let rec factorial n =
  if n <= 1 then 1
  else n * factorial (n - 1)

let greet name =
  Printf.printf "Hello, %s!\n" name
`
	ents := runOCaml(t, src, "funcs.ml")

	ops := make(map[string]bool)
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			ops[e.Name] = true
			if e.Language != "ocaml" {
				t.Errorf("entity %q: expected Language=ocaml, got %q", e.Name, e.Language)
			}
		}
	}

	for _, want := range []string{"add", "factorial", "greet"} {
		if !ops[want] {
			t.Errorf("expected function %q to be extracted, got ops=%v", want, ops)
		}
	}
}

// TestOCaml_TypeDeclarations — type declarations → SCOPE.Component(type).
func TestOCaml_TypeDeclarations(t *testing.T) {
	src := `type color = Red | Green | Blue

type 'a option = None | Some of 'a

type point = {
  x: float;
  y: float;
}

type result = Ok of int | Error of string
`
	ents := runOCaml(t, src, "types.ml")

	comps := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			comps[e.Name] = e.Subtype
		}
	}

	for _, name := range []string{"color", "option", "point", "result"} {
		if subtype, ok := comps[name]; !ok {
			t.Errorf("expected type %q to be extracted; got comps=%v", name, comps)
		} else if subtype != "type" {
			t.Errorf("type %q: expected subtype=type, got %q", name, subtype)
		}
	}
}

// TestOCaml_OpenImports — open statements emit IMPORTS edges.
func TestOCaml_OpenImports(t *testing.T) {
	src := `open Printf
open List
open Hashtbl

let main () =
  printf "Hello\n"
`
	ents := runOCaml(t, src, "main.ml")

	wantImports := map[string]bool{
		"Printf":  false,
		"List":    false,
		"Hashtbl": false,
	}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				if _, ok := wantImports[r.ToID]; ok {
					wantImports[r.ToID] = true
					if r.FromID != "main.ml" {
						t.Errorf("IMPORTS %q: expected FromID=main.ml, got %q", r.ToID, r.FromID)
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

// TestOCaml_CallsEdges — function calls in bodies emit CALLS edges.
func TestOCaml_CallsEdges(t *testing.T) {
	src := `let helper x = x + 1

let process lst =
  List.map helper (List.filter (fun x -> x > 0) lst)
`
	ents := runOCaml(t, src, "calls.ml")

	if !mlHasRel(ents, "process", "SCOPE.Operation", "CALLS", "helper") {
		t.Error("expected CALLS process→helper")
	}
	if !mlHasRel(ents, "process", "SCOPE.Operation", "CALLS", "List.map") {
		t.Error("expected CALLS process→List.map")
	}
	if !mlHasRel(ents, "process", "SCOPE.Operation", "CALLS", "List.filter") {
		t.Error("expected CALLS process→List.filter")
	}
}

// TestOCaml_CallsDeduped — duplicate call targets are emitted once.
func TestOCaml_CallsDeduped(t *testing.T) {
	src := `let worker x = x + 1

let runner () =
  let a = worker 1 in
  let b = worker 2 in
  let c = worker 3 in
  Printf.printf "%d %d %d\n" a b c
`
	ents := runOCaml(t, src, "dedup.ml")
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

// TestOCaml_LanguageTagged — all relationships carry language=ocaml.
func TestOCaml_LanguageTagged(t *testing.T) {
	src := `open List

type node = Leaf | Node of int * node * node

let size t =
  let rec aux acc = function
    | Leaf -> acc
    | Node (_, l, r) -> aux (aux (acc + 1) l) r
  in
  aux 0 t
`
	ents := runOCaml(t, src, "tagged.ml")
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Properties == nil || r.Properties["language"] != "ocaml" {
				t.Errorf("rel %s→%s missing language=ocaml (got %v)", r.Kind, r.ToID, r.Properties)
			}
		}
	}
}

// TestOCaml_LwtMiniFixture — synthetic Lwt async CLI fixture for recall.
// This is the acceptance fixture: must achieve ≥80% entity recall.
func TestOCaml_LwtMiniFixture(t *testing.T) {
	src := `(* OCaml Lwt async CLI tool — mini fixture *)
open Printf
open Lwt
open Lwt_io

(* Configuration type *)
type config = {
  host: string;
  port: int;
  verbose: bool;
}

(* Request/response types *)
type request = {
  method_: string;
  path: string;
  body: string option;
}

type response = {
  status: int;
  body: string;
}

(* Hashtable for caching responses *)
let cache : (string, response) Hashtbl.t = Hashtbl.create 16

(* Build the default config *)
let default_config () = {
  host = "localhost";
  port = 8080;
  verbose = false;
}

(* Format a request for display *)
let format_request req =
  sprintf "%s %s" req.method_ req.path

(* Look up a cached response *)
let cache_get key =
  match Hashtbl.find_opt cache key with
  | Some resp -> Lwt.return (Some resp)
  | None -> Lwt.return None

(* Store a response in the cache *)
let cache_put key resp =
  Hashtbl.add cache key resp;
  Lwt.return ()

(* Parse CLI arguments into a config *)
let parse_args argv =
  let host = ref "localhost" in
  let port = ref 8080 in
  let verbose = ref false in
  Array.iter (fun arg ->
    if String.length arg > 7 && String.sub arg 0 7 = "--host=" then
      host := String.sub arg 7 (String.length arg - 7)
    else if arg = "--verbose" then
      verbose := true
    else
      port := int_of_string arg
  ) argv;
  { host = !host; port = !port; verbose = !verbose }

(* Make an HTTP-like request via Lwt *)
let make_request cfg req =
  let key = format_request req in
  cache_get key >>= function
  | Some cached ->
    if cfg.verbose then printf "Cache hit: %s\n%!" key;
    Lwt.return cached
  | None ->
    (* Simulate async work *)
    Lwt_unix.sleep 0.01 >>= fun () ->
    let resp = { status = 200; body = "OK" } in
    cache_put key resp >>= fun () ->
    Lwt.return resp

(* Process a list of requests concurrently *)
let process_requests cfg reqs =
  Lwt_list.map_p (make_request cfg) reqs

(* Print a response summary *)
let print_response resp =
  printf "Status: %d, Body: %s\n%!" resp.status resp.body

(* Application entry point *)
let main () =
  let cfg = parse_args Sys.argv in
  let reqs = [
    { method_ = "GET"; path = "/health"; body = None };
    { method_ = "GET"; path = "/status"; body = None };
  ] in
  Lwt_main.run (
    process_requests cfg reqs >>= fun responses ->
    List.iter print_response responses;
    Lwt.return ()
  )

let () = main ()
`
	ents := runOCaml(t, src, "cli.ml")

	wantOps := []string{
		"default_config", "format_request", "cache_get", "cache_put",
		"parse_args", "make_request", "process_requests", "print_response", "main",
	}
	wantComps := []string{"config", "request", "response"}
	wantImports := []string{"Printf", "Lwt", "Lwt_io"}

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

	t.Logf("Lwt-mini fixture recall: %d/%d (%.0f%%): ops=%d/%d comps=%d/%d imports=%d/%d",
		totalFound, totalWant, recall,
		opHits, len(wantOps),
		compHits, len(wantComps),
		importHits, len(wantImports))

	if recall < 80.0 {
		t.Errorf("entity recall %.0f%% below 80%% threshold (%d/%d found)",
			recall, totalFound, totalWant)
	}
}

// TestOCaml_NoFalsePositives — keywords and noise tokens are not emitted as CALLS edges.
func TestOCaml_NoFalsePositives(t *testing.T) {
	src := `let process lst =
  if List.length lst = 0 then
    []
  else
    let filtered = List.filter (fun x -> x > 0) lst in
    List.map (fun x -> x * 2) filtered
`
	ents := runOCaml(t, src, "nofp.ml")

	ocamlKeywordCalls := map[string]bool{
		"if": true, "then": true, "else": true, "let": true, "in": true,
		"fun": true, "match": true, "with": true, "type": true, "open": true,
	}

	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" && ocamlKeywordCalls[r.ToID] {
				t.Errorf("false positive CALLS edge: %s→%s (keyword should be excluded)", e.Name, r.ToID)
			}
		}
	}
}

// TestOCaml_MliFile — .mli signature files are parsed (type/val declarations).
func TestOCaml_MliFile(t *testing.T) {
	src := `(* Module signature *)
type t

type 'a result = Ok of 'a | Error of string

val create : string -> t

val process : t -> int -> 'a result
`
	ents := runOCaml(t, src, "mymodule.mli")

	comps := make(map[string]bool)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			comps[e.Name] = true
		}
	}

	for _, name := range []string{"t", "result"} {
		if !comps[name] {
			t.Errorf("expected type %q to be extracted from .mli; got comps=%v", name, comps)
		}
	}
}
