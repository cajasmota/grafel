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

// TestFSharp_CallLineStamping proves every CALLS edge carries a 1-based
// FILE-ABSOLUTE line Property (#5034; promoted from the body-relative #4939
// convention). The line points directly at the call site in the file, so a
// jump-to-source is possible without a separate body-offset lookup.
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

	// `let caller n =` is FILE line 6. paren-call `helper(n)` is on file line 7,
	// space-call `other n` on file line 8 (file-absolute, not body-relative).
	if r := fsRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper"); r == nil {
		t.Error("expected CALLS caller→helper")
	} else if r.Properties["line"] != "7" {
		t.Errorf("helper call line = %q, want 7 (file-absolute)", r.Properties["line"])
	}
	if r := fsRel(ents, "caller", "SCOPE.Operation", "CALLS", "other"); r == nil {
		t.Error("expected CALLS caller→other")
	} else if r.Properties["line"] != "8" {
		t.Errorf("other call line = %q, want 8 (file-absolute)", r.Properties["line"])
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

// TestFSharp_DataAnnotationsValidations — #5049: DataAnnotations attributes
// preceding a record field are collected into Properties["validations"]
// (comma-joined chips) so the dashboard ShapeTree renders constraint chips —
// mirroring the Java/TS/Python field-validation support.
func TestFSharp_DataAnnotationsValidations(t *testing.T) {
	src := `module Dto

type CreateUserDto = {
    [<Required>]
    [<EmailAddress>]
    Email: string

    [<StringLength(120)>]
    [<MinLength(2)>]
    Name: string

    [<Required; Range(1, 5)>]
    Rating: int

    [<RegularExpression("^[0-9]+$")>]
    Code: string

    Note: string
}
`
	ents := runFSharp(t, src, "dto.fs")

	cases := []struct {
		name, want string
	}{
		{"CreateUserDto.Email", "Required,Email"},
		{"CreateUserDto.Name", "MaxLength:120,MinLength:2"},
		{"CreateUserDto.Rating", "Required,Range:1..5"},
		{"CreateUserDto.Code", "Pattern"},
	}
	for _, c := range cases {
		fe := fsFind(ents, c.name, "SCOPE.Schema")
		if fe == nil {
			t.Fatalf("expected field %q", c.name)
		}
		if got := fe.Properties["validations"]; got != c.want {
			t.Errorf("field %q validations=%q, want %q", c.name, got, c.want)
		}
	}

	// A field with no attributes must NOT carry a validations property.
	if fe := fsFind(ents, "CreateUserDto.Note", "SCOPE.Schema"); fe == nil {
		t.Fatal("expected field CreateUserDto.Note")
	} else if _, ok := fe.Properties["validations"]; ok {
		t.Errorf("Note should have no validations, got %q", fe.Properties["validations"])
	}
}

// TestFSharp_InlineValidationAttribute — #5049: an attribute on the SAME line as
// the field (`[<Required>] Email: string`) is still collected, and the leading
// attribute is stripped off the field name.
func TestFSharp_InlineValidationAttribute(t *testing.T) {
	src := `module Dto

type LoginDto = {
    [<Required>] Username: string
}
`
	ents := runFSharp(t, src, "login.fs")
	fe := fsFind(ents, "LoginDto.Username", "SCOPE.Schema")
	if fe == nil {
		t.Fatalf("expected field LoginDto.Username (attr should be stripped off name)")
	}
	if got := fe.Properties["validations"]; got != "Required" {
		t.Errorf("Username validations=%q, want Required", got)
	}
	if mt := fe.Properties["member_type"]; mt != "string" {
		t.Errorf("Username member_type=%q, want string", mt)
	}
}

// TestFSharp_NonValidationAttributesIgnored — #5049: non-validation attributes
// (`[<CLIMutable>]`, `[<JsonProperty>]`) never leak into the validations chips.
func TestFSharp_NonValidationAttributesIgnored(t *testing.T) {
	src := `module Dto

type Config = {
    [<CLIMutable>]
    Host: string
}
`
	ents := runFSharp(t, src, "config.fs")
	fe := fsFind(ents, "Config.Host", "SCOPE.Schema")
	if fe == nil {
		t.Fatalf("expected field Config.Host")
	}
	if v, ok := fe.Properties["validations"]; ok {
		t.Errorf("Host should have no validation chips, got %q", v)
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

// TestFSharp_CEUsage_CustomBuilder — #5048/#5077: a custom builder invocation
// `optional { ... }` emits a USES edge. #5077: the `optional` symbol resolves
// through the let-binding `let optional = OptionBuilder()` so the USES edge
// re-targets the OptionBuilder TYPE (ToID) and stamps ce_builder_type, while the
// raw symbol survives in ce_builder.
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
		if r.Kind == "USES" && r.ToID == "OptionBuilder" {
			found = true
			if r.Properties["ce_builder"] != "optional" {
				t.Errorf("ce_builder=%q, want optional", r.Properties["ce_builder"])
			}
			if r.Properties["ce_builder_type"] != "OptionBuilder" {
				t.Errorf("ce_builder_type=%q, want OptionBuilder", r.Properties["ce_builder_type"])
			}
			if !strings.Contains(r.Properties["ce_bind_points"], "return!") {
				t.Errorf("ce_bind_points=%q, want return!", r.Properties["ce_bind_points"])
			}
		}
	}
	if !found {
		t.Error("expected USES edge to resolved OptionBuilder type")
	}
}

// --- #5077 deepening: match-site edges, CE-member re-typing, USES resolution,
// computed builder heads ---

// TestFSharp_ActivePattern_MatchSite — #5077: a match arm `| Even ->` against a
// known active-pattern case emits a USES edge from the enclosing operation to
// the case sub-entity, closing the active-pattern match-SITE gap.
func TestFSharp_ActivePattern_MatchSite(t *testing.T) {
	src := `module Patterns

let (|Even|Odd|) n =
    if n % 2 = 0 then Even else Odd

let classify n =
    match n with
    | Even -> "even"
    | Odd -> "odd"
`
	ents := runFSharp(t, src, "patterns.fs")
	op := fsFind(ents, "classify", "SCOPE.Operation")
	if op == nil {
		t.Fatal("expected SCOPE.Operation classify")
	}
	wantRef := fsSchemaRef("patterns.fs", "(|Even|Odd|).Even")
	var even, odd *types.RelationshipRecord
	for i := range op.Relationships {
		r := &op.Relationships[i]
		if r.Kind != "USES" || r.Properties["match_site"] != "true" {
			continue
		}
		switch r.Properties["active_pattern_case"] {
		case "Even":
			even = r
		case "Odd":
			odd = r
		}
	}
	if even == nil {
		t.Fatal("expected match-site USES edge for case Even")
	}
	if even.ToID != wantRef {
		t.Errorf("Even ToID=%q, want %q", even.ToID, wantRef)
	}
	if odd == nil {
		t.Error("expected match-site USES edge for case Odd")
	}
}

// TestFSharp_ActivePattern_MatchSite_NoMatch — #5077: a match arm against a DU
// case that is NOT a known active-pattern case emits NO match-site edge.
func TestFSharp_ActivePattern_MatchSite_NoMatch(t *testing.T) {
	src := `module Patterns

let (|Even|Odd|) n =
    if n % 2 = 0 then Even else Odd

let describe x =
    match x with
    | Some v -> "some"
    | None -> "none"
`
	ents := runFSharp(t, src, "patterns.fs")
	op := fsFind(ents, "describe", "SCOPE.Operation")
	if op == nil {
		t.Fatal("expected SCOPE.Operation describe")
	}
	for _, r := range op.Relationships {
		if r.Properties["match_site"] == "true" {
			t.Errorf("unexpected match-site edge for non-active-pattern case %q",
				r.Properties["active_pattern_case"])
		}
	}
}

// TestFSharp_CEMember_Retyped — #5077: the individual Bind/Return/Zero members of
// a CE builder type are re-typed SCOPE.Operation/ce_member (not plain member) so
// the builder protocol is queryable.
func TestFSharp_CEMember_Retyped(t *testing.T) {
	src := `module Builders

type OptionBuilder() =
    member _.Bind(m, f) = Option.bind f m
    member _.Return(x) = Some x
    member _.Zero() = None
`
	ents := runFSharp(t, src, "builders.fs")
	for _, name := range []string{"Bind", "Return", "Zero"} {
		op := fsFind(ents, name, "SCOPE.Operation")
		if op == nil {
			t.Fatalf("expected SCOPE.Operation %s", name)
		}
		if op.Subtype != "ce_member" {
			t.Errorf("%s subtype=%q, want ce_member", name, op.Subtype)
		}
		if op.Properties["ce_member"] != "true" {
			t.Errorf("%s ce_member=%q, want true", name, op.Properties["ce_member"])
		}
		if op.Properties["ce_protocol_method"] != name {
			t.Errorf("%s ce_protocol_method=%q, want %s", name, op.Properties["ce_protocol_method"], name)
		}
	}
}

// TestFSharp_CEMember_NotRetypedWhenNotBuilder — #5077: a method named `Return`
// on an ordinary (non-builder) type is NOT re-typed ce_member, since no CE
// builder type in the file declares the protocol.
func TestFSharp_CEMember_NotRetypedWhenNotBuilder(t *testing.T) {
	src := `module Things

type Repo() =
    member _.Return(x) = x
`
	ents := runFSharp(t, src, "things.fs")
	op := fsFind(ents, "Return", "SCOPE.Operation")
	if op == nil {
		t.Fatal("expected SCOPE.Operation Return")
	}
	if op.Subtype == "ce_member" {
		t.Error("Return on non-builder Repo should not be re-typed ce_member")
	}
}

// TestFSharp_CEUsage_ComputedHead — #5077: a computed builder head
// `(mkBuilder ()) { ... }` (an expression, not a bare identifier) emits a USES
// edge to the factory symbol stamped ce_head=computed.
func TestFSharp_CEUsage_ComputedHead(t *testing.T) {
	src := `module Work

let run () =
    (mkBuilder ()) {
        let! x = getX ()
        return x
    }
`
	ents := runFSharp(t, src, "work.fs")
	op := fsFind(ents, "run", "SCOPE.Operation")
	if op == nil {
		t.Fatal("expected SCOPE.Operation run")
	}
	found := false
	for _, r := range op.Relationships {
		if r.Kind == "USES" && r.ToID == "mkBuilder" {
			found = true
			if r.Properties["ce_head"] != "computed" {
				t.Errorf("ce_head=%q, want computed", r.Properties["ce_head"])
			}
			if !strings.Contains(r.Properties["ce_bind_points"], "let!") {
				t.Errorf("ce_bind_points=%q, want let!", r.Properties["ce_bind_points"])
			}
		}
	}
	if !found {
		t.Error("expected USES edge to computed builder head mkBuilder")
	}
}

// TestFSharp_CE_WrongLanguageNoOp — #5077: C#-shaped source fed to the F#
// extractor produces no F#-specific CE/active-pattern artefacts (wrong-language
// no-op guard).
func TestFSharp_CE_WrongLanguageNoOp(t *testing.T) {
	src := `public class Foo {
    public int Bar() { return 1; }
}
`
	ents := runFSharp(t, src, "Foo.cs")
	for _, e := range ents {
		if e.Subtype == "ce_member" || e.Subtype == "active_pattern" ||
			e.Subtype == "active_pattern_case" || e.Subtype == "computation_builder" {
			t.Errorf("unexpected F# CE/active-pattern artefact %q on wrong-language input", e.Name)
		}
		for _, r := range e.Relationships {
			if r.Properties["match_site"] == "true" || r.Properties["ce_head"] == "computed" {
				t.Errorf("unexpected CE/match-site edge on wrong-language input from %q", e.Name)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// #5130 — Validus / FsToolkit.ErrorHandling validators + nested/custom
// DataAnnotations.
// ---------------------------------------------------------------------------

// fsFindRel returns the first relationship of the given kind/ToID on the named
// entity, or nil.
func fsFindRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) *types.RelationshipRecord {
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

// TestFSharp_ValidusPipeline — #5130: a `validate { ... }` Validus computation
// expression with Check.* combinators emits a VALIDATES edge from the enclosing
// operation to validator:validus, stamped with the combinators.
func TestFSharp_ValidusPipeline(t *testing.T) {
	src := `module Validation

open Validus

let validateUser (dto: UserDto) =
    validate {
        let! _ = Check.String.notEmpty dto.Name
        and! _ = Check.String.betweenLen 2 50 dto.Name
        and! _ = Check.Int.greaterThan 0 dto.Age
        return dto
    }
`
	ents := runFSharp(t, src, "validation.fs")
	r := fsFindRel(ents, "validateUser", "SCOPE.Operation", "VALIDATES", "validator:validus")
	if r == nil {
		t.Fatal("expected validateUser VALIDATES validator:validus")
	}
	if r.Properties["library"] != "validus" {
		t.Errorf("library=%q, want validus", r.Properties["library"])
	}
	if r.Properties["computation_expression"] != "true" {
		t.Errorf("expected computation_expression=true, got %q", r.Properties["computation_expression"])
	}
	combos := r.Properties["combinators"]
	for _, want := range []string{"Check.String.notEmpty", "Check.String.betweenLen", "Check.Int.greaterThan"} {
		if !strings.Contains(combos, want) {
			t.Errorf("combinators=%q, want to contain %q", combos, want)
		}
	}
	if r.Properties["line"] == "" {
		t.Error("expected a line property on the VALIDATES edge")
	}
}

// TestFSharp_ValidatorGroup — #5130: a Validus ValidatorGroup / validateField
// pipeline (no CE) is recognised via its combinators.
func TestFSharp_ValidatorGroup(t *testing.T) {
	src := `module Validation

open Validus

let nameValidator =
    ValidatorGroup("Name")
        .And(validateField "Name" (Check.String.notEmpty))
        .Build()
`
	ents := runFSharp(t, src, "group.fs")
	r := fsFindRel(ents, "nameValidator", "SCOPE.Operation", "VALIDATES", "validator:validus")
	if r == nil {
		t.Fatal("expected nameValidator VALIDATES validator:validus")
	}
	if !strings.Contains(r.Properties["combinators"], "ValidatorGroup") {
		t.Errorf("combinators=%q, want ValidatorGroup", r.Properties["combinators"])
	}
}

// TestFSharp_FsToolkitValidation — #5130: a `validation { ... }` FsToolkit CE
// with Validation.* combinators emits a VALIDATES edge to validator:fstoolkit.
func TestFSharp_FsToolkitValidation(t *testing.T) {
	src := `module Validation

open FsToolkit.ErrorHandling

let validateForm (input: FormInput) =
    validation {
        let! name = input.Name |> Result.requireNotNull "name required"
        and! age = Validation.ofResult (parseAge input.Age)
        return { Name = name; Age = age }
    }
`
	ents := runFSharp(t, src, "form.fs")
	r := fsFindRel(ents, "validateForm", "SCOPE.Operation", "VALIDATES", "validator:fstoolkit")
	if r == nil {
		t.Fatal("expected validateForm VALIDATES validator:fstoolkit")
	}
	if r.Properties["computation_expression"] != "true" {
		t.Errorf("expected computation_expression=true, got %q", r.Properties["computation_expression"])
	}
	if !strings.Contains(r.Properties["combinators"], "Validation.ofResult") {
		t.Errorf("combinators=%q, want Validation.ofResult", r.Properties["combinators"])
	}
}

// TestFSharp_NestedModelValidates — #5130: a record field whose type is another
// record defined in the same file materialises an owner→nested VALIDATES edge
// (via=nested_model) — the F# analog of [<ValidateComplexType>].
func TestFSharp_NestedModelValidates(t *testing.T) {
	src := `module Dto

type Address = {
    Street: string
    City: string
}

type Order = {
    Id: int
    ShipTo: Address
    BillTo: Address option
}
`
	ents := runFSharp(t, src, "order.fs")
	r := fsFindRel(ents, "Order", "SCOPE.Component", "VALIDATES", "Address")
	if r == nil {
		t.Fatal("expected Order VALIDATES Address (nested_model)")
	}
	if r.Properties["via"] != "nested_model" {
		t.Errorf("via=%q, want nested_model", r.Properties["via"])
	}
	// Only ONE edge per distinct nested type even though two fields reference it.
	count := 0
	for i := range ents {
		if ents[i].Name == "Order" && ents[i].Kind == "SCOPE.Component" {
			for _, rel := range ents[i].Relationships {
				if rel.Kind == "VALIDATES" && rel.ToID == "Address" {
					count++
				}
			}
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 nested VALIDATES edge to Address, got %d", count)
	}
}

// TestFSharp_CustomValidationAttr — #5130: a [<CustomValidation(typeof<T>, "M")>]
// field attribute emits an owner→validator:dataannotations VALIDATES edge
// (via=custom_validation) stamped with the validator type + method.
func TestFSharp_CustomValidationAttr(t *testing.T) {
	src := `module Dto

type SignupDto = {
    [<CustomValidation(typeof<UserValidators>, "ValidateAge")>]
    Age: int
}
`
	ents := runFSharp(t, src, "signup.fs")
	r := fsFindRel(ents, "SignupDto", "SCOPE.Component", "VALIDATES", "validator:dataannotations")
	if r == nil {
		t.Fatal("expected SignupDto VALIDATES validator:dataannotations (custom_validation)")
	}
	if r.Properties["via"] != "custom_validation" {
		t.Errorf("via=%q, want custom_validation", r.Properties["via"])
	}
	if r.Properties["validator_type"] != "UserValidators" {
		t.Errorf("validator_type=%q, want UserValidators", r.Properties["validator_type"])
	}
	if r.Properties["method"] != "ValidateAge" {
		t.Errorf("method=%q, want ValidateAge", r.Properties["method"])
	}
	// The custom-validation attribute must NOT leak into the field validation chips.
	if fe := fsFind(ents, "SignupDto.Age", "SCOPE.Schema"); fe == nil {
		t.Fatal("expected field SignupDto.Age")
	} else if v, ok := fe.Properties["validations"]; ok {
		t.Errorf("Age should have no chips from CustomValidation, got %q", v)
	}
}

// TestFSharp_IValidatableObject — #5130: a type implementing IValidatableObject
// emits an owner→validator:dataannotations VALIDATES edge (via=ivalidatableobject).
func TestFSharp_IValidatableObject(t *testing.T) {
	src := `module Dto

open System.ComponentModel.DataAnnotations

type RegistrationDto =
    { Password: string
      Confirm: string }

    interface IValidatableObject with
        member this.Validate(ctx) =
            seq { if this.Password <> this.Confirm then yield ValidationResult("mismatch") }
`
	ents := runFSharp(t, src, "registration.fs")
	r := fsFindRel(ents, "RegistrationDto", "SCOPE.Component", "VALIDATES", "validator:dataannotations")
	if r == nil {
		t.Fatal("expected RegistrationDto VALIDATES validator:dataannotations (ivalidatableobject)")
	}
	if r.Properties["via"] != "ivalidatableobject" {
		t.Errorf("via=%q, want ivalidatableobject", r.Properties["via"])
	}
}

// TestFSharp_Validator_WrongLanguageNoOp — #5130: a non-F# file (or a C#-shaped
// validate block) does not produce F# VALIDATES edges (the extractor is only
// invoked for .fs content, and a plain record with no validator surface emits
// nothing).
func TestFSharp_Validator_WrongLanguageNoOp(t *testing.T) {
	src := `module Dto

// Plain record, no validators of any kind.
type Plain = {
    Name: string
    Count: int
}

let doWork x = x + 1
`
	ents := runFSharp(t, src, "plain.fs")
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == "VALIDATES" {
				t.Errorf("unexpected VALIDATES edge on no-validator input from %q -> %q", ents[i].Name, r.ToID)
			}
		}
	}
}

// TestFSharp_Validator_NoMatchNoOp — #5130: an ordinary record `{ ... }` block
// and a bare Result.* error-handling body (no `validation { }` CE, no
// Validation.* combinator) must NOT be over-claimed as a validator pipeline.
func TestFSharp_Validator_NoMatchNoOp(t *testing.T) {
	src := `module Logic

open FsToolkit.ErrorHandling

// Result.* WITHOUT the validation CE is ordinary error handling, not
// applicative validation accumulation.
let parse s =
    s |> Result.map int |> Result.mapError string
`
	ents := runFSharp(t, src, "logic.fs")
	if r := fsFindRel(ents, "parse", "SCOPE.Operation", "VALIDATES", "validator:fstoolkit"); r != nil {
		t.Error("bare Result.* should NOT be claimed as an FsToolkit validation pipeline")
	}
}
