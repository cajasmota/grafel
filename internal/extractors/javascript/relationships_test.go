package javascript_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tsjavascript "github.com/smacker/go-tree-sitter/javascript"
	tstypescript "github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
	// Blank import to trigger init() registration.
	_ "github.com/cajasmota/grafel/internal/extractors/javascript"
)

// parseJSRel parses JS source for relationship tests.
func parseJSRel(t *testing.T, src []byte) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tsjavascript.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tree
}

// parseTSRel parses TS source for relationship tests.
func parseTSRel(t *testing.T, src []byte) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tstypescript.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tree
}

func runJS(t *testing.T, src string, language string, tree ts.Tree) []types.EntityRecord {
	t.Helper()
	return runJSPath(t, src, language, tree, "test."+extOf(language))
}

// runJSPath is runJS with an explicit file path — used by tests that
// exercise relative-import path resolution (issue #421).
func runJSPath(t *testing.T, src string, language string, tree ts.Tree, path string) []types.EntityRecord {
	t.Helper()
	ext, _ := extractor.Get(language)
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: language,
		TSTree:   tree,
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

// TestExtract_ImportsES6 (#41, updated for #742) — file with M import
// statements emits M IMPORTS relationships on the file entity. Import-
// placeholder SCOPE.Component/import entities are no longer emitted (#742).
func TestExtract_ImportsES6(t *testing.T) {
	src := `import { Foo } from "./foo";
import bar from "bar";
const lodash = require("lodash");
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	// No import-placeholder SCOPE.Component/import entities.
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "import" {
			t.Errorf("SCOPE.Component/import placeholder entity still emitted (#742): %q", e.Name)
		}
	}

	// IMPORTS edges must exist on the file entity (any entity post-#742).
	wantModules := map[string]bool{"./foo": false, "bar": false, "lodash": false}
	for i := range ents {
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind != "IMPORTS" {
				continue
			}
			// Match by import_path property (preferred) or raw ToID.
			if r.Properties != nil {
				if ip := r.Properties["import_path"]; ip != "" {
					if _, ok := wantModules[ip]; ok {
						wantModules[ip] = true
					}
				}
			}
			// Fallback: ToID for side-effect / require shapes with no properties.
			if _, ok := wantModules[r.ToID]; ok {
				wantModules[r.ToID] = true
			}
		}
	}
	for mod, found := range wantModules {
		if !found {
			t.Errorf("expected IMPORTS edge for module %q on any entity", mod)
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
	// Issue #742 — import placeholder entities are no longer emitted.
	// Verify the IMPORTS edge for "./x" exists on the file entity instead.
	importEdgeFound := false
	for i := range ents {
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind == "IMPORTS" {
				if r.Properties != nil && r.Properties["import_path"] == "./x" {
					importEdgeFound = true
				}
				if r.ToID == "./x" {
					importEdgeFound = true
				}
			}
		}
	}
	if !importEdgeFound {
		t.Errorf("expected IMPORTS edge for ./x on the file entity (post-#742)")
	}
	// Verify no SCOPE.Component/import placeholder entity for "./x" exists.
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "import" {
			t.Errorf("SCOPE.Component/import placeholder entity still emitted (#742): %q", e.Name)
		}
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
// import_path property == module AND whose local_name == localName.
// Issue #742: IMPORTS edges now live on the file entity (SCOPE.Component/file)
// rather than standalone SCOPE.Component/import placeholder entities. This
// helper searches ALL entities so tests are independent of the carrier kind.
func findImportProps(ents []types.EntityRecord, module, localName string) map[string]string {
	for i := range ents {
		e := &ents[i]
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" || r.Properties == nil {
				continue
			}
			// Match on import_path (preferred — exact spec string) or source_module.
			importPath := r.Properties["import_path"]
			sourceMod := r.Properties["source_module"]
			if importPath != module && sourceMod != module {
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

// TestExtract_FileLevelEntityEnablesImportsFromIDRewrite (#570) —
// the cross-repo import linker (#566) consults a per-entity-id →
// repo index; a file-path-shaped from_id isn't in that index, so the
// linker silently skipped every JS/TS import edge — collapsing the
// candidate `method=import` link count to near-zero on shared
// packages like react / axios / react-native between sibling repos.
//
// The fix has two parts that this test pins:
//
//  1. The extractor emits a file-level SCOPE.Component (subtype="file")
//     entity whose Name == the file path. The indexer stamps it with
//     graph.EntityID(repoTag, ...), and the resolver's byName index
//     then maps the file-path string to the file-entity's hex ID.
//
//  2. Every IMPORTS edge keeps FromID == the file path string at
//     extract time. The path is the file-entity's Name, so the
//     resolver's ReferencesEmbeddedWithAllowlist pass rewrites the
//     FromID to the file-entity's stamped hex ID before the link
//     pipeline reads the graph. We do NOT pre-stamp a hex in the
//     extractor because the extractor doesn't know the indexer's
//     repoTag seed — a pre-stamped hex would short-circuit isHexID
//     in ReferencesEmbedded and prevent the rewrite.
//
// This test asserts the extractor-side contract (file entity exists,
// IMPORTS FromID matches its Name). The end-to-end hex-rewrite is
// verified by re-indexing client-fixture-{a,b,c} and inspecting the
// saved graph.json — see PR description.
func TestExtract_FileLevelEntityEnablesImportsFromIDRewrite(t *testing.T) {
	src := `import { UserService } from "./user/user.service";
import express from "express";
import * as fs from "fs";
import "./side-effect";
const lodash = require("lodash");
`
	tree := parseTSRel(t, []byte(src))
	const path = "src/app/app.module.ts"
	ents := runJSPath(t, src, "typescript", tree, path)

	// 1. A file-level SCOPE.Component subtype="file" must exist for
	//    the source path, so the indexer's byName index can map the
	//    path-shaped FromID to the file entity's stamped hex ID.
	var fileEntFound bool
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "file" && e.Name == path {
			fileEntFound = true
			break
		}
	}
	if !fileEntFound {
		t.Fatalf("expected file-level SCOPE.Component subtype=file entity for %s; ents=%+v", path, ents)
	}

	// No SCOPE.Component/import placeholder entities (issue #742).
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "import" {
			t.Errorf("SCOPE.Component/import placeholder entity still emitted (#742): %q", e.Name)
		}
	}

	// 2. Every IMPORTS edge across ALL entities must carry FromID == path.
	// Issue #742: IMPORTS edges now live on the file entity, not on standalone
	// import-placeholder entities. The resolver rewrites this to the stamped
	// hex ID at index-time.
	var importEdges int
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			importEdges++
			if r.FromID != path {
				t.Errorf("IMPORTS edge on entity %q (subtype=%q) has FromID=%q; want file path %q",
					e.Name, e.Subtype, r.FromID, path)
			}
		}
	}
	// Five distinct import shapes: named, default, namespace, side-effect, require.
	if importEdges < 5 {
		t.Errorf("expected at least 5 IMPORTS edges across shapes, got %d", importEdges)
	}
}
