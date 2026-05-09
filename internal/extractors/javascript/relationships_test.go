package javascript_test

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsjavascript "github.com/smacker/go-tree-sitter/javascript"
	tstypescript "github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
	// Blank import to trigger init() registration.
	_ "github.com/cajasmota/archigraph/internal/extractors/javascript"
)

// parseJSRel parses JS source for relationship tests.
func parseJSRel(t *testing.T, src []byte) *sitter.Tree {
	t.Helper()
	p := sitter.NewParser()
	p.SetLanguage(tsjavascript.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tree
}

// parseTSRel parses TS source for relationship tests.
func parseTSRel(t *testing.T, src []byte) *sitter.Tree {
	t.Helper()
	p := sitter.NewParser()
	p.SetLanguage(tstypescript.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tree
}

func runJS(t *testing.T, src string, language string, tree *sitter.Tree) []types.EntityRecord {
	t.Helper()
	return runJSPath(t, src, language, tree, "test."+extOf(language))
}

// runJSPath is runJS with an explicit file path — used by tests that
// exercise relative-import path resolution (issue #421).
func runJSPath(t *testing.T, src string, language string, tree *sitter.Tree, path string) []types.EntityRecord {
	t.Helper()
	ext, _ := extractor.Get(language)
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: language,
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func extOf(language string) string {
	if language == "typescript" {
		return "ts"
	}
	return "js"
}

func findByNameRel(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func hasRelEdge(ents []types.EntityRecord, fromName, kind, toID string) bool {
	src := findByNameRel(ents, fromName)
	if src == nil {
		return false
	}
	for _, r := range src.Relationships {
		if r.Kind == kind && r.ToID == toID {
			return true
		}
	}
	return false
}

func countRelByKind(ents []types.EntityRecord, fromName, kind string) int {
	src := findByNameRel(ents, fromName)
	if src == nil {
		return 0
	}
	n := 0
	for _, r := range src.Relationships {
		if r.Kind == kind {
			n++
		}
	}
	return n
}

// TestExtract_ContainsClassMethods (#41) — class with N methods produces N
// CONTAINS edges from the class to each method.
func TestExtract_ContainsClassMethods(t *testing.T) {
	src := `class Foo {
  a() {}
  b() {}
  c() {}
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	if c := countRelByKind(ents, "Foo", "CONTAINS"); c != 3 {
		t.Errorf("expected 3 CONTAINS edges, got %d", c)
	}
	// Issue #144 — CONTAINS targets are structural-ref stubs (Format A)
	// keyed on the source file so the resolver disambiguates by location.
	for _, m := range []string{"a", "b", "c"} {
		want := "scope:operation:method:javascript:test.js:" + m
		if !hasRelEdge(ents, "Foo", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Foo→%s", want)
		}
	}
}

// TestExtract_CallsBareName (#41) — function calling another function emits
// a CALLS edge with stub to_id; duplicate call sites collapse to one edge.
func TestExtract_CallsBareName(t *testing.T) {
	src := `function helper() { return 1; }
function caller() {
  helper();
  helper();
  console.log("x");
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	if !hasRelEdge(ents, "caller", "CALLS", "helper") {
		t.Errorf("expected CALLS caller→helper")
	}
	if !hasRelEdge(ents, "caller", "CALLS", "log") {
		t.Errorf("expected CALLS caller→log (member trailing)")
	}
	n := 0
	for _, r := range findByNameRel(ents, "caller").Relationships {
		if r.Kind == "CALLS" && r.ToID == "helper" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected 1 CALLS caller→helper after dedup, got %d", n)
	}
}

// TestExtract_ImportsES6 (#41) — file with M import statements emits M
// IMPORTS relationships on module entities.
func TestExtract_ImportsES6(t *testing.T) {
	src := `import { Foo } from "./foo";
import bar from "bar";
const lodash = require("lodash");
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	want := map[string]bool{"./foo": false, "bar": false, "lodash": false}
	for _, e := range ents {
		if e.Subtype != "import" {
			continue
		}
		if _, ok := want[e.Name]; ok {
			want[e.Name] = true
		}
		if len(e.Relationships) != 1 || e.Relationships[0].Kind != "IMPORTS" {
			t.Errorf("import entity %q missing IMPORTS edge: %+v", e.Name, e.Relationships)
		}
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("expected import entity for %q", k)
		}
	}
}

// TestExtract_TypeScript covers the same shape against the TS grammar to
// guarantee parity (single extractor, two languages).
func TestExtract_TypeScript(t *testing.T) {
	src := `import { X } from "./x";
class A {
  foo() { this.bar(); helper(); }
  bar() { return 1; }
}
function helper() {}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if c := countRelByKind(ents, "A", "CONTAINS"); c != 2 {
		t.Errorf("expected 2 CONTAINS from A, got %d", c)
	}
	// Issue #144 — TS goes through the same JS extractor; CONTAINS targets
	// must be structural-ref stubs prefixed with the "typescript" segment.
	for _, m := range []string{"foo", "bar"} {
		want := "scope:operation:method:typescript:test.ts:" + m
		if !hasRelEdge(ents, "A", "CONTAINS", want) {
			t.Errorf("expected CONTAINS A→%s", want)
		}
	}
	if !hasRelEdge(ents, "foo", "CALLS", "bar") {
		t.Errorf("expected CALLS foo→bar")
	}
	if !hasRelEdge(ents, "foo", "CALLS", "helper") {
		t.Errorf("expected CALLS foo→helper")
	}
	importFound := false
	for _, e := range ents {
		if e.Subtype == "import" && e.Name == "./x" && len(e.Relationships) == 1 {
			importFound = true
		}
	}
	if !importFound {
		t.Errorf("expected ./x import entity with IMPORTS relationship")
	}
}

// TestExtract_ImportsContractProperties (#421) — ES6 import statements
// must emit IMPORTS edges carrying the same Properties contract Python
// (#93) and Java (#120) emit so the cross-file resolver pre-pass can
// build a per-file binding table:
//
//	Properties["local_name"]    — identifier introduced by the import
//	Properties["source_module"] — dotted module path the symbol came from
//	Properties["imported_name"] — original name (pre-alias) of the symbol
//
// For relative imports the source_module is the importer-relative path
// resolved to the canonical dotted form; for non-relative (npm) imports
// the spec is used verbatim with slashes → dots.
func TestExtract_ImportsContractProperties(t *testing.T) {
	src := `import { UserService } from "./user/user.service";
import express from "express";
import * as fs from "fs";
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/app/app.module.ts")

	// Named import — local_name=UserService, imported_name=UserService,
	// source_module = relative path resolved against the importer dir
	// then dotted ("src.app.user.user.service") plus the source-root-
	// stripped form ("app.user.user.service" — see modulesForJSFile).
	gotProps := findImportProps(ents, "./user/user.service", "UserService")
	if gotProps == nil {
		t.Fatalf("expected IMPORTS edge for UserService from ./user/user.service; ents=%+v", ents)
	}
	if gotProps["local_name"] != "UserService" {
		t.Errorf("local_name=%q want UserService", gotProps["local_name"])
	}
	if gotProps["imported_name"] != "UserService" {
		t.Errorf("imported_name=%q want UserService", gotProps["imported_name"])
	}
	if got := gotProps["source_module"]; got != "src.app.user.user.service" {
		t.Errorf("source_module=%q want src.app.user.user.service", got)
	}

	// Default import — local_name=express, imported_name=default,
	// source_module=express (npm spec verbatim).
	defProps := findImportProps(ents, "express", "express")
	if defProps == nil {
		t.Fatalf("expected default-import IMPORTS edge for express; ents=%+v", ents)
	}
	if defProps["local_name"] != "express" || defProps["source_module"] != "express" {
		t.Errorf("default import props=%v", defProps)
	}

	// Namespace import — local_name=fs, source_module=fs, wildcard=1.
	nsProps := findImportProps(ents, "fs", "fs")
	if nsProps == nil {
		t.Fatalf("expected namespace IMPORTS edge for fs; ents=%+v", ents)
	}
	if nsProps["wildcard"] != "1" {
		t.Errorf("namespace import expected wildcard=1, got %v", nsProps)
	}
}

// findImportProps returns the Properties of the IMPORTS edge whose
// import-entity Name == module AND whose local_name == localName.
// Returns nil when no such edge exists.
func findImportProps(ents []types.EntityRecord, module, localName string) map[string]string {
	for i := range ents {
		e := &ents[i]
		if e.Subtype != "import" || e.Name != module {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" || r.Properties == nil {
				continue
			}
			if r.Properties["local_name"] == localName {
				return r.Properties
			}
		}
	}
	return nil
}

// TestExtract_ReceiverTypedCallsCrossFile (#421) — TS analogue of Java
// #120. A method invocation `this.userService.findOne(...)` where the
// receiver is a constructor-injected typed field should bind to the
// findOne method declared in the imported UserService class. The
// extractor emits a structural-ref Format A stub keyed on the resolved
// import file path so the resolver's byLocation index binds the call
// to the cross-file target without going through bare-name lookup
// (which would collide with every other findOne in the corpus).
func TestExtract_ReceiverTypedCallsCrossFile(t *testing.T) {
	src := `import { UserService } from "./services/user.service";

class UsersController {
  constructor(private readonly userService: UserService) {}
  findOne(id: string) {
    return this.userService.findOne(id);
  }
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/users/users.controller.ts")

	// Expected structural-ref target for the receiver-typed call.
	// The receiver `this.userService` has declared type `UserService`,
	// imported from "./services/user.service" → resolved file path
	// "src/services/user.service.ts".
	want := "scope:operation:method:typescript:src/users/services/user.service.ts:findOne"

	// "findOne" is also the method name in the controller. Disambiguate
	// by SourceFile + class context: the controller's method lives in
	// the same file as the import statement.
	var callerEntity *types.EntityRecord
	for i := range ents {
		e := &ents[i]
		if e.Name == "findOne" && e.Kind == "SCOPE.Operation" {
			callerEntity = e
			break
		}
	}
	if callerEntity == nil {
		t.Fatalf("expected findOne method entity; got ents=%+v", ents)
	}
	found := false
	for _, r := range callerEntity.Relationships {
		if r.Kind == "CALLS" && r.ToID == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected CALLS findOne -> %q; got rels=%+v", want, callerEntity.Relationships)
	}
}

// TestExtract_ReceiverTypedCallsConstructorParam (#421) — same shape but
// the typed receiver comes from a constructor parameter (NestJS @Inject
// style: `constructor(private userService: UserService)`). The
// parameter declaration introduces both the parameter and an implicit
// class field of the same name+type.
func TestExtract_ReceiverTypedCallsConstructorParam(t *testing.T) {
	src := `import { UserService } from "./services/user.service";

class UsersController {
  constructor(private userService: UserService) {}
  list() {
    return this.userService.findAll();
  }
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/users/users.controller.ts")

	want := "scope:operation:method:typescript:src/users/services/user.service.ts:findAll"
	if !hasRelEdge(ents, "list", "CALLS", want) {
		caller := findByNameRel(ents, "list")
		if caller == nil {
			t.Fatalf("list entity missing; ents=%+v", ents)
		}
		t.Fatalf("expected CALLS list -> %q; got %+v", want, caller.Relationships)
	}
}

// TestExtract_ReceiverTypedCallsBareReceiver (#421) — receiver is a
// bare identifier (no `this.` prefix) bound to a typed parameter:
// `userService.findOne(id)` where `userService` is a parameter typed
// `UserService`. The extractor should still emit a structural-ref
// keyed on the imported source file.
func TestExtract_ReceiverTypedCallsBareReceiver(t *testing.T) {
	src := `import { UserService } from "./services/user.service";

function callIt(userService: UserService, id: string) {
  return userService.findOne(id);
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/users/handler.ts")

	want := "scope:operation:method:typescript:src/users/services/user.service.ts:findOne"
	if !hasRelEdge(ents, "callIt", "CALLS", want) {
		caller := findByNameRel(ents, "callIt")
		t.Fatalf("expected CALLS callIt -> %q; got %+v", want, caller.Relationships)
	}
}

// TestExtract_ReceiverTypedCallsExternalImportFallsBack (#421) —
// receiver is typed by an external (non-relative) import. We can't
// resolve a project-internal file path, so the extractor falls back
// to the bare method name (current behaviour preserved).
func TestExtract_ReceiverTypedCallsExternalImportFallsBack(t *testing.T) {
	src := `import { Repository } from "typeorm";

function run(repo: Repository) {
  return repo.findOne();
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/users/handler.ts")

	if !hasRelEdge(ents, "run", "CALLS", "findOne") {
		caller := findByNameRel(ents, "run")
		t.Fatalf("expected bare CALLS run -> findOne for external receiver; got %+v",
			caller.Relationships)
	}
}
