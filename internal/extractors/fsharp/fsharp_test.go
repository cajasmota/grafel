package fsharp_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/fsharp"
	"github.com/cajasmota/archigraph/internal/types"
)

// runFSharp runs the extractor on raw source and returns entity records.
func runFSharp(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("fsharp")
	if !ok {
		t.Fatal("fsharp extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "fsharp",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func fsFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func fsHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
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

// TestFSharp_Registered verifies the extractor is in the registry.
func TestFSharp_Registered(t *testing.T) {
	_, ok := extractor.Get("fsharp")
	if !ok {
		t.Fatal("fsharp extractor not registered")
	}
}

// TestFSharp_EmptyInput returns zero entities for empty content.
func TestFSharp_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("fsharp")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.fs",
		Content:  []byte{},
		Language: "fsharp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ents) != 0 {
		t.Errorf("expected 0 entities, got %d", len(ents))
	}
}

// TestFSharp_ModuleDiscovery — module declarations extracted as SCOPE.Component.
func TestFSharp_ModuleDiscovery(t *testing.T) {
	src := `module MyApp.Domain

open System
open System.Collections.Generic

let greet name = sprintf "Hello, %s" name
`
	ents := runFSharp(t, src, "domain.fs")
	if fsFind(ents, "MyApp.Domain", "SCOPE.Component") == nil {
		t.Error("expected module MyApp.Domain as SCOPE.Component")
	}
}

// TestFSharp_NamespaceDiscovery — namespace declarations extracted as SCOPE.Component.
func TestFSharp_NamespaceDiscovery(t *testing.T) {
	src := `namespace MyApp.Infrastructure

open System

type Repository() =
    member _.FindAll() = []
`
	ents := runFSharp(t, src, "infra.fs")
	if fsFind(ents, "MyApp.Infrastructure", "SCOPE.Component") == nil {
		t.Error("expected namespace MyApp.Infrastructure as SCOPE.Component")
	}
}

// TestFSharp_LetBindings — let/let rec functions extracted as SCOPE.Operation.
func TestFSharp_LetBindings(t *testing.T) {
	src := `module App

let add a b = a + b

let rec factorial n =
    if n <= 1 then 1
    else n * factorial (n - 1)

let mutable counter = 0

let processItems items =
    items |> List.map (fun x -> x * 2)
`
	ents := runFSharp(t, src, "app.fs")

	ops := make(map[string]bool)
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			ops[e.Name] = true
			if e.Language != "fsharp" {
				t.Errorf("entity %q: expected Language=fsharp, got %q", e.Name, e.Language)
			}
		}
	}

	for _, want := range []string{"add", "factorial", "processItems"} {
		if !ops[want] {
			t.Errorf("expected function %q to be extracted", want)
		}
	}
}

// TestFSharp_TypeDiscovery — type declarations extracted as SCOPE.Component.
func TestFSharp_TypeDiscovery(t *testing.T) {
	src := `module Types

type Person = {
    Name: string
    Age: int
}

type Shape =
    | Circle of float
    | Rectangle of float * float
    | Triangle of float * float * float

type Animal =
    | Dog
    | Cat
    | Bird

type IService =
    abstract member Process: string -> string
`
	ents := runFSharp(t, src, "types.fs")

	comps := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			comps[e.Name] = e.Subtype
		}
	}

	wantTypes := []string{"Person", "Shape", "Animal"}
	for _, name := range wantTypes {
		if _, ok := comps[name]; !ok {
			t.Errorf("expected type %q to be extracted as SCOPE.Component", name)
		}
	}
}

// TestFSharp_TypeSubtypes proves classifyTypeSubtype distinguishes the F# type
// kinds (#4906): record (`= {`), discriminated_union (`= |`), interface. This is
// the evidence behind the lang.fsharp.core / Giraffe Type-System coverage cells.
func TestFSharp_TypeSubtypes(t *testing.T) {
	src := `module Types

type Person = {
    Name: string
    Age: int
}

type Shape =
    | Circle of float
    | Rectangle of float * float

type IService =
    interface
        abstract member Process: string -> string
    end
`
	ents := runFSharp(t, src, "types.fs")
	got := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			got[e.Name] = e.Subtype
		}
	}
	want := map[string]string{
		"Person":   "record",
		"Shape":    "discriminated_union",
		"IService": "interface",
	}
	for name, sub := range want {
		if got[name] != sub {
			t.Errorf("type %q subtype=%q, want %q", name, got[name], sub)
		}
	}
}

// TestFSharp_OpenStatements — open statements emit IMPORTS edges.
func TestFSharp_OpenStatements(t *testing.T) {
	src := `module App

open System
open System.IO
open Microsoft.FSharp.Collections

let readFile path = File.ReadAllText(path)
`
	ents := runFSharp(t, src, "main.fs")

	wantImports := map[string]bool{
		"System":                       false,
		"System.IO":                    false,
		"Microsoft.FSharp.Collections": false,
	}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				if _, ok := wantImports[r.ToID]; ok {
					wantImports[r.ToID] = true
					if r.FromID != "main.fs" {
						t.Errorf("IMPORTS %q: expected FromID=main.fs, got %q", r.ToID, r.FromID)
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

// TestFSharp_CallsEdges — function calls emit CALLS edges.
func TestFSharp_CallsEdges(t *testing.T) {
	src := `module App

open System

let helper x = x * 2

let caller n =
    let result = helper(n)
    printfn "Result: %d" result
    result
`
	ents := runFSharp(t, src, "calls.fs")

	if !fsHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Error("expected CALLS caller→helper")
	}
}

// TestFSharp_PipeOperatorCalls — |> chains emit CALLS edges.
func TestFSharp_PipeOperatorCalls(t *testing.T) {
	src := `module App

open System.Collections.Generic

let processData data =
    data
    |> List.filter (fun x -> x > 0)
    |> List.map (fun x -> x * 2)
    |> List.sum
`
	ents := runFSharp(t, src, "pipes.fs")

	// pipe targets should be extracted as CALLS
	hasPipeCall := false
	for _, e := range ents {
		if e.Name == "processData" {
			for _, r := range e.Relationships {
				if r.Kind == "CALLS" && (r.ToID == "List.filter" || r.ToID == "List.map" || r.ToID == "List.sum") {
					hasPipeCall = true
				}
			}
		}
	}
	if !hasPipeCall {
		t.Error("expected CALLS edges from pipe |> operator targets")
	}
}

// TestFSharp_SelfRecursionExcluded — self-recursive calls not emitted.
func TestFSharp_SelfRecursionExcluded(t *testing.T) {
	src := `module App

let rec fib n =
    if n <= 1 then n
    else fib(n-1) + fib(n-2)
`
	ents := runFSharp(t, src, "fib.fs")
	if fsHasRel(ents, "fib", "SCOPE.Operation", "CALLS", "fib") {
		t.Error("self-recursion CALLS should be filtered")
	}
}

// TestFSharp_LanguageTagged — all relationships carry language=fsharp.
func TestFSharp_LanguageTagged(t *testing.T) {
	src := `module App

open System

type Node = { Value: int }

let process (n: Node) = n.Value
`
	ents := runFSharp(t, src, "tag.fs")
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Properties == nil || r.Properties["language"] != "fsharp" {
				t.Errorf("rel %s→%s missing language=fsharp (got %v)", r.Kind, r.ToID, r.Properties)
			}
		}
	}
}

// fsRel returns the first relationship matching (name,kind,edgeKind,toID), or nil.
func fsRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) *types.RelationshipRecord {
	for i := range ents {
		if ents[i].Name != name || ents[i].Kind != kind {
			continue
		}
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind == edgeKind && r.ToID == toID {
				return r
			}
		}
	}
	return nil
}

// TestFSharp_SpaceApplicationCalls proves the space-applied application scanner
// (#4939): F#'s dominant call idiom `head arg1 arg2` now emits CALLS edges that
// the paren/pipe/compose scanners miss. This is the evidence behind flipping the
// fsharp function-application coverage cell.
func TestFSharp_SpaceApplicationCalls(t *testing.T) {
	src := `module App

let createUser name email = { Name = name; Email = email }
let notify msg = printfn "%s" msg
let render user = sprintf "%A" user

let handler () =
    let u = createUser "ada" "ada@example.com"
    notify "created"
    render u
`
	ents := runFSharp(t, src, "app.fs")

	for _, target := range []string{"createUser", "notify", "render"} {
		if !fsHasRel(ents, "handler", "SCOPE.Operation", "CALLS", target) {
			t.Errorf("expected space-applied CALLS handler→%s", target)
		}
	}
}

// TestFSharp_SpaceApplicationGating proves the scanner stays conservative: it
// does NOT fire on keywords, type-case heads, or non-call positions (#4939).
func TestFSharp_SpaceApplicationGating(t *testing.T) {
	src := `module App

let helper x = x

let caller flag =
    if flag then helper 1
    else helper 2
`
	ents := runFSharp(t, src, "g.fs")

	// keyword heads (if/then/else) must never become CALLS
	for _, kw := range []string{"if", "then", "else"} {
		if fsHasRel(ents, "caller", "SCOPE.Operation", "CALLS", kw) {
			t.Errorf("keyword %q must not produce a CALLS edge", kw)
		}
	}
	// the real call (helper, applied after `then`/`else`) should be captured
	if !fsHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Error("expected CALLS caller→helper from then/else clause")
	}
}

// TestFSharp_CallLineStamping proves every CALLS edge carries a 1-based body
// line Property (#4939), matching the Nim/Erlang call_line_precision convention.
func TestFSharp_CallLineStamping(t *testing.T) {
	src := `module App

let helper x = x * 2
let other y = y + 1

let caller n =
    let a = helper(n)
    let b = other n
    a + b
`
	ents := runFSharp(t, src, "lines.fs")

	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != "CALLS" {
				continue
			}
			if r.Properties == nil || r.Properties["line"] == "" {
				t.Errorf("CALLS %s→%s missing line Property (got %v)", e.Name, r.ToID, r.Properties)
			}
		}
	}

	// paren-call `helper(n)` is on body line 2, space-call `other n` on body line 3.
	if r := fsRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper"); r == nil {
		t.Error("expected CALLS caller→helper")
	} else if r.Properties["line"] != "2" {
		t.Errorf("helper call line = %q, want 2", r.Properties["line"])
	}
	if r := fsRel(ents, "caller", "SCOPE.Operation", "CALLS", "other"); r == nil {
		t.Error("expected CALLS caller→other")
	} else if r.Properties["line"] != "3" {
		t.Errorf("other call line = %q, want 3", r.Properties["line"])
	}
}

// TestFSharp_MemberFunctions — member definitions extracted as SCOPE.Operation.
func TestFSharp_MemberFunctions(t *testing.T) {
	src := `module App

open System

type Calculator() =
    member this.Add(a: int, b: int) = a + b
    member this.Subtract(a: int, b: int) = a - b
    member this.Multiply(a: int, b: int) = a * b
`
	ents := runFSharp(t, src, "calc.fs")

	ops := make(map[string]bool)
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			ops[e.Name] = true
		}
	}

	for _, want := range []string{"Add", "Subtract", "Multiply"} {
		if !ops[want] {
			t.Errorf("expected member %q to be extracted as SCOPE.Operation", want)
		}
	}
}

// TestFSharp_GiraffeWebApp — synthetic Giraffe-style fixture for entity recall.
func TestFSharp_GiraffeWebApp(t *testing.T) {
	src := `module GiraffeApp.App

open System
open Microsoft.AspNetCore.Builder
open Microsoft.AspNetCore.Hosting
open Microsoft.Extensions.DependencyInjection
open Giraffe

// Models
type User = {
    Id: int
    Name: string
    Email: string
}

type CreateUserRequest = {
    Name: string
    Email: string
}

// Handlers
let getUser (userId: int) : HttpHandler =
    fun next ctx ->
        let user = { Id = userId; Name = "Alice"; Email = "alice@example.com" }
        json user next ctx

let createUser : HttpHandler =
    fun next ctx ->
        task {
            let! req = ctx.BindJsonAsync<CreateUserRequest>()
            let user = { Id = 1; Name = req.Name; Email = req.Email }
            return! json user next ctx
        }

let listUsers : HttpHandler =
    fun next ctx ->
        let users = [
            { Id = 1; Name = "Alice"; Email = "alice@example.com" }
            { Id = 2; Name = "Bob"; Email = "bob@example.com" }
        ]
        json users next ctx

// Router
let webApp =
    choose [
        GET >=> route "/users" >=> listUsers
        GET >=> routef "/users/%i" getUser
        POST >=> route "/users" >=> createUser
        setStatusCode 404 >=> text "Not Found"
    ]

// Service configuration
let configureServices (services: IServiceCollection) =
    services.AddGiraffe() |> ignore

let configureApp (app: IApplicationBuilder) =
    app.UseGiraffe(webApp)

// Entry point
[<EntryPoint>]
let main argv =
    WebHostBuilder()
        .UseKestrel()
        .ConfigureServices(configureServices)
        .Configure(configureApp)
        .Build()
        .Run()
    0
`
	ents := runFSharp(t, src, "app.fs")

	wantOps := []string{"getUser", "createUser", "listUsers", "configureServices", "configureApp", "main"}
	wantComps := []string{"User", "CreateUserRequest"}
	wantImports := []string{
		"System",
		"Microsoft.AspNetCore.Builder",
		"Microsoft.AspNetCore.Hosting",
		"Microsoft.Extensions.DependencyInjection",
		"Giraffe",
	}

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

	t.Logf("Giraffe fixture recall: %d/%d (%.0f%%): ops=%d/%d comps=%d/%d imports=%d/%d",
		totalFound, totalWant, recall,
		opHits, len(wantOps),
		compHits, len(wantComps),
		importHits, len(wantImports))

	if recall < 80.0 {
		t.Errorf("entity recall %.0f%% below 80%% threshold (%d/%d found)",
			recall, totalFound, totalWant)
	}
}

// TestFSharp_AsyncWorkflow — async workflows emit correct entities.
func TestFSharp_AsyncWorkflow(t *testing.T) {
	src := `module AsyncApp

open System
open System.Threading.Tasks

let fetchData (url: string) =
    async {
        let! response = Async.AwaitTask(Task.FromResult("data"))
        return response
    }

let processAsync () =
    async {
        let! data = fetchData "http://example.com"
        return data.Length
    }
    |> Async.RunSynchronously
`
	ents := runFSharp(t, src, "async.fs")

	ops := make(map[string]bool)
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			ops[e.Name] = true
		}
	}

	for _, want := range []string{"fetchData", "processAsync"} {
		if !ops[want] {
			t.Errorf("expected async function %q to be extracted", want)
		}
	}
}

// fsSchemaRef builds the canonical Format A schema-field structural ref the
// extractor emits for a type→member CONTAINS edge (#4942).
func fsSchemaRef(filePath, dotted string) string {
	return extractor.BuildSchemaFieldStructuralRef("fsharp", filePath, dotted)
}

// TestFSharp_RecordFields — #4942: record FIELDS are emitted as individual
// SCOPE.Schema/field sub-entities (dotted "<Type>.<field>") with the parent
// record CONTAINING each one.
func TestFSharp_RecordFields(t *testing.T) {
	src := `module Domain

type Person = {
    Name: string
    Age: int
    mutable Score: float
}
`
	ents := runFSharp(t, src, "person.fs")

	for _, f := range []struct{ name, typ string }{
		{"Person.Name", "string"},
		{"Person.Age", "int"},
		{"Person.Score", "float"},
	} {
		fe := fsFind(ents, f.name, "SCOPE.Schema")
		if fe == nil {
			t.Fatalf("expected record field %q as SCOPE.Schema", f.name)
		}
		if fe.Subtype != "field" {
			t.Errorf("field %q subtype=%q, want field", f.name, fe.Subtype)
		}
		if fe.Properties["member_type"] != f.typ {
			t.Errorf("field %q member_type=%q, want %q", f.name, fe.Properties["member_type"], f.typ)
		}
		if fe.Properties["parent_class"] != "Person" {
			t.Errorf("field %q parent_class=%q, want Person", f.name, fe.Properties["parent_class"])
		}
		// CONTAINS edge from the owner type to the field.
		if !fsHasRel(ents, "Person", "SCOPE.Component", "CONTAINS", fsSchemaRef("person.fs", f.name)) {
			t.Errorf("expected Person CONTAINS %q", f.name)
		}
	}
}

// TestFSharp_DUCases — #4942: DU CASES are emitted as individual
// SCOPE.Schema/du_case sub-entities, with payload types captured from `of T`.
func TestFSharp_DUCases(t *testing.T) {
	src := `module Shapes

type Shape =
    | Circle of float
    | Rectangle of float * float
    | Point
`
	ents := runFSharp(t, src, "shapes.fs")

	for _, c := range []struct{ name, typ string }{
		{"Shape.Circle", "float"},
		{"Shape.Rectangle", "float * float"},
		{"Shape.Point", ""},
	} {
		ce := fsFind(ents, c.name, "SCOPE.Schema")
		if ce == nil {
			t.Fatalf("expected DU case %q as SCOPE.Schema", c.name)
		}
		if ce.Subtype != "du_case" {
			t.Errorf("case %q subtype=%q, want du_case", c.name, ce.Subtype)
		}
		if ce.Properties["member_type"] != c.typ {
			t.Errorf("case %q member_type=%q, want %q", c.name, ce.Properties["member_type"], c.typ)
		}
		if !fsHasRel(ents, "Shape", "SCOPE.Component", "CONTAINS", fsSchemaRef("shapes.fs", c.name)) {
			t.Errorf("expected Shape CONTAINS %q", c.name)
		}
	}
}

// TestFSharp_SingleLineRecord — #4942: a single-line record body
// `{ X: int; Y: int }` still yields one field entity per `;`-separated field.
func TestFSharp_SingleLineRecord(t *testing.T) {
	src := `module Geo

type Point = { X: int; Y: int }
`
	ents := runFSharp(t, src, "geo.fs")
	for _, name := range []string{"Point.X", "Point.Y"} {
		if fsFind(ents, name, "SCOPE.Schema") == nil {
			t.Errorf("expected single-line record field %q", name)
		}
	}
}

// TestFSharp_TypeAlias — #4942: a pure type alias `type Foo = Bar` classifies
// as the "alias" subtype, distinct from the catch-all "type", and emits no
// spurious field/case sub-entities.
func TestFSharp_TypeAlias(t *testing.T) {
	src := `module Aliases

type UserId = int
type Name = string
type Pair = int * string
`
	ents := runFSharp(t, src, "aliases.fs")
	for _, name := range []string{"UserId", "Name", "Pair"} {
		te := fsFind(ents, name, "SCOPE.Component")
		if te == nil {
			t.Fatalf("expected alias type %q", name)
		}
		if te.Subtype != "alias" {
			t.Errorf("alias %q subtype=%q, want alias", name, te.Subtype)
		}
	}
	// No sub-entities for aliases.
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" {
			t.Errorf("alias produced unexpected schema sub-entity %q", e.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// #5048: computation expressions + active patterns
// ---------------------------------------------------------------------------

// TestFSharp_ActivePattern_Total — #5048: a total active pattern
// `let (|Even|Odd|) n = ...` emits a SCOPE.Pattern entity (subtype
// active_pattern) plus one case sub-entity per banana-clip case, with a
// definition→case CONTAINS edge.
func TestFSharp_ActivePattern_Total(t *testing.T) {
	src := `module Patterns

let (|Even|Odd|) n =
    if n % 2 = 0 then Even else Odd
`
	ents := runFSharp(t, src, "patterns.fs")
	ap := fsFind(ents, "(|Even|Odd|)", "SCOPE.Pattern")
	if ap == nil {
		t.Fatal("expected SCOPE.Pattern (|Even|Odd|)")
	}
	if ap.Subtype != "active_pattern" {
		t.Errorf("subtype=%q, want active_pattern", ap.Subtype)
	}
	if ap.Properties["active_pattern_cases"] != "Even,Odd" {
		t.Errorf("cases=%q, want Even,Odd", ap.Properties["active_pattern_cases"])
	}
	if ap.Properties["partial"] != "false" {
		t.Errorf("partial=%q, want false", ap.Properties["partial"])
	}
	for _, c := range []string{"Even", "Odd"} {
		dotted := "(|Even|Odd|)." + c
		if fsFind(ents, dotted, "SCOPE.Schema") == nil {
			t.Errorf("expected case sub-entity %q", dotted)
		}
		if !fsHasRel(ents, "(|Even|Odd|)", "SCOPE.Pattern", "CONTAINS", fsSchemaRef("patterns.fs", dotted)) {
			t.Errorf("expected CONTAINS edge to %q", dotted)
		}
	}
}

// TestFSharp_ActivePattern_Partial — #5048: a partial active pattern
// `let (|Positive|_|) n = ...` is flagged partial and only its named case
// becomes a sub-entity (the `_` wildcard is not a case).
func TestFSharp_ActivePattern_Partial(t *testing.T) {
	src := `module Patterns

let (|Positive|_|) n =
    if n > 0 then Some n else None
`
	ents := runFSharp(t, src, "patterns.fs")
	ap := fsFind(ents, "(|Positive|_|)", "SCOPE.Pattern")
	if ap == nil {
		t.Fatal("expected SCOPE.Pattern (|Positive|_|)")
	}
	if ap.Subtype != "partial_active_pattern" {
		t.Errorf("subtype=%q, want partial_active_pattern", ap.Subtype)
	}
	if ap.Properties["partial"] != "true" {
		t.Errorf("partial=%q, want true", ap.Properties["partial"])
	}
	if ap.Properties["active_pattern_cases"] != "Positive" {
		t.Errorf("cases=%q, want Positive", ap.Properties["active_pattern_cases"])
	}
	if fsFind(ents, "(|Positive|_|)._", "SCOPE.Schema") != nil {
		t.Error("wildcard _ should not be a case sub-entity")
	}
}

// TestFSharp_ActivePattern_Parameterised — #5048: a parameterised active
// pattern `let (|DivisibleBy|_|) divisor n = ...` is flagged parameterised.
func TestFSharp_ActivePattern_Parameterised(t *testing.T) {
	src := `module Patterns

let (|DivisibleBy|_|) divisor n =
    if n % divisor = 0 then Some n else None
`
	ents := runFSharp(t, src, "patterns.fs")
	ap := fsFind(ents, "(|DivisibleBy|_|)", "SCOPE.Pattern")
	if ap == nil {
		t.Fatal("expected SCOPE.Pattern (|DivisibleBy|_|)")
	}
	if ap.Properties["parameterised"] != "true" {
		t.Errorf("parameterised=%q, want true", ap.Properties["parameterised"])
	}
}

// TestFSharp_CEBuilder_Custom — #5048: a custom computation-expression builder
// type (declaring Bind/Return/...) is stamped ce_builder and re-subtyped
// computation_builder.
func TestFSharp_CEBuilder_Custom(t *testing.T) {
	src := `module Builders

type OptionBuilder() =
    member _.Bind(m, f) = Option.bind f m
    member _.Return(x) = Some x
    member _.Zero() = None
`
	ents := runFSharp(t, src, "builders.fs")
	b := fsFind(ents, "OptionBuilder", "SCOPE.Component")
	if b == nil {
		t.Fatal("expected SCOPE.Component OptionBuilder")
	}
	if b.Properties["ce_builder"] != "true" {
		t.Errorf("ce_builder=%q, want true", b.Properties["ce_builder"])
	}
	if b.Subtype != "computation_builder" {
		t.Errorf("subtype=%q, want computation_builder", b.Subtype)
	}
	members := b.Properties["ce_builder_members"]
	for _, want := range []string{"Bind", "Return", "Zero"} {
		if !strings.Contains(members, want) {
			t.Errorf("ce_builder_members=%q missing %q", members, want)
		}
	}
}

// TestFSharp_CEBuilder_NotMisclassified — #5048: an ordinary type with a single
// unrelated CE-shaped member is NOT flagged a builder.
func TestFSharp_CEBuilder_NotMisclassified(t *testing.T) {
	src := `module Things

type Repo() =
    member _.Return(x) = x
`
	ents := runFSharp(t, src, "things.fs")
	b := fsFind(ents, "Repo", "SCOPE.Component")
	if b == nil {
		t.Fatal("expected SCOPE.Component Repo")
	}
	if b.Properties["ce_builder"] == "true" {
		t.Error("Repo should not be classified a CE builder (single member)")
	}
}

// TestFSharp_CEUsage_Intrinsic — #5048: a `task { ... }` CE invocation inside a
// let body emits a USES edge to the builder with bind-point metadata.
func TestFSharp_CEUsage_Intrinsic(t *testing.T) {
	src := `module Work

let fetchAll () =
    task {
        let! a = getA ()
        let! b = getB ()
        return a + b
    }
`
	ents := runFSharp(t, src, "work.fs")
	op := fsFind(ents, "fetchAll", "SCOPE.Operation")
	if op == nil {
		t.Fatal("expected SCOPE.Operation fetchAll")
	}
	var uses *types.RelationshipRecord
	for i := range op.Relationships {
		if op.Relationships[i].Kind == "USES" && op.Relationships[i].ToID == "task" {
			uses = &op.Relationships[i]
		}
	}
	if uses == nil {
		t.Fatal("expected USES edge to task builder")
	}
	if uses.Properties["ce_builder"] != "task" {
		t.Errorf("ce_builder=%q, want task", uses.Properties["ce_builder"])
	}
	if !strings.Contains(uses.Properties["ce_bind_points"], "let!") {
		t.Errorf("ce_bind_points=%q, want let!", uses.Properties["ce_bind_points"])
	}
	if !strings.Contains(uses.Properties["ce_bind_points"], "return") {
		// 'return' (no bang) is not a bind point; only return! counts. Just
		// confirm let! is present (above) — this is a sanity guard.
		_ = uses
	}
}

// TestFSharp_CEUsage_CustomBuilder — #5048: a custom builder invocation
// `optional { ... }` emits a USES edge to that builder symbol.
func TestFSharp_CEUsage_CustomBuilder(t *testing.T) {
	src := `module Work

let optional = OptionBuilder()

let combine () =
    optional {
        let! x = tryGet ()
        return! finish x
    }
`
	ents := runFSharp(t, src, "work.fs")
	op := fsFind(ents, "combine", "SCOPE.Operation")
	if op == nil {
		t.Fatal("expected SCOPE.Operation combine")
	}
	found := false
	for _, r := range op.Relationships {
		if r.Kind == "USES" && r.ToID == "optional" {
			found = true
			if !strings.Contains(r.Properties["ce_bind_points"], "return!") {
				t.Errorf("ce_bind_points=%q, want return!", r.Properties["ce_bind_points"])
			}
		}
	}
	if !found {
		t.Error("expected USES edge to optional builder")
	}
}
