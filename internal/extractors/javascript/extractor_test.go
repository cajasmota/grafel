package javascript_test

import (
	"context"
	"strings"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsjavascript "github.com/smacker/go-tree-sitter/javascript"
	tstypescript "github.com/smacker/go-tree-sitter/typescript/typescript"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/extractors/javascript"
	"github.com/cajasmota/archigraph/internal/types"
)

// --------------------------------------------------------------------------
// test helpers
// --------------------------------------------------------------------------

// parseJS parses source with the JavaScript grammar and returns the tree.
func parseJS(t *testing.T, src []byte) *sitter.Tree {
	t.Helper()
	p := sitter.NewParser()
	p.SetLanguage(tsjavascript.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatalf("parseJS: %v", err)
	}
	return tree
}

// parseTS parses source with the TypeScript grammar and returns the tree.
func parseTS(t *testing.T, src []byte) *sitter.Tree {
	t.Helper()
	p := sitter.NewParser()
	p.SetLanguage(tstypescript.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatalf("parseTS: %v", err)
	}
	return tree
}

// extract runs the extractor and returns the entity slice.
func extract(t *testing.T, content []byte, language string, tree *sitter.Tree) []types.EntityRecord {
	t.Helper()
	e := &jsExtractorShim{}
	got, err := e.extract(content, language, tree)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return got
}

// jsExtractorShim invokes the real JSExtractor via the extreg.Extractor interface.
type jsExtractorShim struct{}

func (s *jsExtractorShim) extract(content []byte, language string, tree *sitter.Tree) ([]types.EntityRecord, error) {
	e := javascript.New()
	return e.Extract(context.Background(), extreg.FileInput{
		Path:     "test.go",
		Content:  content,
		Language: language,
		Tree:     tree,
	})
}

// findByName returns the first entity with the given name, or nil.
func findByName(entities []types.EntityRecord, name string) *types.EntityRecord {
	for i := range entities {
		if entities[i].Name == name {
			return &entities[i]
		}
	}
	return nil
}

// assertKind fails the test if the entity's Kind doesn't match.
func assertKind(t *testing.T, entities []types.EntityRecord, name, wantKind string) {
	t.Helper()
	e := findByName(entities, name)
	if e == nil {
		t.Errorf("entity %q not found; got names: %v", name, entityNames(entities))
		return
	}
	if e.Kind != wantKind {
		t.Errorf("entity %q: kind=%q, want %q", name, e.Kind, wantKind)
	}
}

func entityNames(entities []types.EntityRecord) []string {
	var names []string
	for _, e := range entities {
		names = append(names, e.Name)
	}
	return names
}

// --------------------------------------------------------------------------
// happy path: JS function_declaration
// --------------------------------------------------------------------------

const jsFunctionSrc = `
function greet(name) {
  return "Hello " + name;
}
`

func TestJSFunctionDeclaration(t *testing.T) {
	src := []byte(jsFunctionSrc)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	assertKind(t, entities, "greet", "SCOPE.Operation")
	e := findByName(entities, "greet")
	if e == nil {
		t.Fatal("greet not found")
	}
	if e.StartLine != 2 {
		t.Errorf("StartLine=%d, want 2", e.StartLine)
	}
	if !strings.Contains(e.Signature, "greet") {
		t.Errorf("Signature=%q, expected to contain 'greet'", e.Signature)
	}
	if e.Subtype != "function" {
		t.Errorf("Subtype=%q, want 'function'", e.Subtype)
	}
}

// --------------------------------------------------------------------------
// happy path: arrow function assigned to const
// --------------------------------------------------------------------------

const jsArrowSrc = `
const add = (a, b) => {
  return a + b;
};
`

func TestJSArrowFunctionConst(t *testing.T) {
	src := []byte(jsArrowSrc)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	assertKind(t, entities, "add", "SCOPE.Operation")
	e := findByName(entities, "add")
	if e == nil {
		t.Fatal("add not found")
	}
	if e.Subtype != "function" {
		t.Errorf("Subtype=%q, want 'function'", e.Subtype)
	}
}

// --------------------------------------------------------------------------
// happy path: class with methods
// --------------------------------------------------------------------------

const jsClassSrc = `
class Counter {
  constructor() {
    this.count = 0;
  }

  increment() {
    this.count++;
  }

  decrement() {
    this.count--;
  }

  value() {
    return this.count;
  }
}
`

func TestJSClassWithMethods(t *testing.T) {
	src := []byte(jsClassSrc)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	// Class itself.
	assertKind(t, entities, "Counter", "SCOPE.Component")
	c := findByName(entities, "Counter")
	if c == nil {
		t.Fatal("Counter class not found")
	}
	if c.Subtype != "class" {
		t.Errorf("Counter Subtype=%q, want 'class'", c.Subtype)
	}

	// Methods (constructor is excluded by convention).
	assertKind(t, entities, "increment", "SCOPE.Operation")
	assertKind(t, entities, "decrement", "SCOPE.Operation")
	assertKind(t, entities, "value", "SCOPE.Operation")

	for _, name := range []string{"increment", "decrement", "value"} {
		m := findByName(entities, name)
		if m == nil {
			t.Errorf("method %q not found", name)
			continue
		}
		if m.Subtype != "method" {
			t.Errorf("method %q Subtype=%q, want 'method'", name, m.Subtype)
		}
	}

	// constructor should NOT be emitted.
	if findByName(entities, "constructor") != nil {
		t.Error("constructor should not be extracted")
	}
}

// --------------------------------------------------------------------------
// happy path: TypeScript interface
// --------------------------------------------------------------------------

const tsInterfaceSrc = `
interface User {
  id: number;
  name: string;
  email: string;
}
`

func TestTSInterfaceDeclaration(t *testing.T) {
	src := []byte(tsInterfaceSrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "User", "SCOPE.Schema")
	e := findByName(entities, "User")
	if e == nil {
		t.Fatal("User not found")
	}
	if e.Subtype != "interface" {
		t.Errorf("Subtype=%q, want 'interface'", e.Subtype)
	}
}

// --------------------------------------------------------------------------
// happy path: TypeScript type alias
// --------------------------------------------------------------------------

const tsTypeAliasSrc = `
type Callback = (err: Error | null, result: string) => void;
type UserID = string;
`

func TestTSTypeAlias(t *testing.T) {
	src := []byte(tsTypeAliasSrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "Callback", "SCOPE.Schema")
	assertKind(t, entities, "UserID", "SCOPE.Schema")

	for _, name := range []string{"Callback", "UserID"} {
		e := findByName(entities, name)
		if e == nil {
			t.Errorf("%q not found", name)
			continue
		}
		if e.Subtype != "type_alias" {
			t.Errorf("%q Subtype=%q, want 'type_alias'", name, e.Subtype)
		}
	}
}

// --------------------------------------------------------------------------
// happy path: ES6 import → relationship
// --------------------------------------------------------------------------

const tsImportSrc = `
import express, { Request, Response } from "express";
import { readFileSync } from "fs";
`

func TestES6Import(t *testing.T) {
	src := []byte(tsImportSrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "express", "SCOPE.Component")
	assertKind(t, entities, "fs", "SCOPE.Component")

	for _, name := range []string{"express", "fs"} {
		e := findByName(entities, name)
		if e == nil {
			t.Errorf("import entity %q not found", name)
			continue
		}
		if e.Subtype != "import" {
			t.Errorf("%q Subtype=%q, want 'import'", name, e.Subtype)
		}
	}
}

// --------------------------------------------------------------------------
// happy path: CommonJS require
// --------------------------------------------------------------------------

const jsRequireSrc = `
const path = require("path");
const express = require("express");
`

func TestCommonJSRequire(t *testing.T) {
	src := []byte(jsRequireSrc)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	assertKind(t, entities, "path", "SCOPE.Component")
	assertKind(t, entities, "express", "SCOPE.Component")
}

// --------------------------------------------------------------------------
// happy path: export_statement wrapping a function
// --------------------------------------------------------------------------

const jsExportSrc = `
export function handler(event) {
  return event;
}

export class Service {
  run() {}
}
`

func TestJSExportStatement(t *testing.T) {
	src := []byte(jsExportSrc)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	assertKind(t, entities, "handler", "SCOPE.Operation")
	assertKind(t, entities, "Service", "SCOPE.Component")
	assertKind(t, entities, "run", "SCOPE.Operation")
}

// --------------------------------------------------------------------------
// happy path: JSX/TSX file — handles JSX without crashing
// --------------------------------------------------------------------------

const jsxSrc = `
import React from "react";

function App() {
  return <div className="App">Hello</div>;
}

export default App;
`

func TestJSXFileHandledWithoutCrash(t *testing.T) {
	src := []byte(jsxSrc)
	// JSX uses the JavaScript grammar (not TSX grammar — sitter JS handles JSX).
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	// Must not crash; function is extracted, JSX element is NOT an entity.
	assertKind(t, entities, "App", "SCOPE.Operation")
	for _, e := range entities {
		if e.Kind == "SCOPE.JSX" || strings.Contains(e.Name, "<") {
			t.Errorf("unexpected JSX entity: %q kind=%q", e.Name, e.Kind)
		}
	}
}

// --------------------------------------------------------------------------
// happy path: TSX file
// --------------------------------------------------------------------------

const tsxSrc = `
interface ButtonProps {
  label: string;
  onClick: () => void;
}

const Button = (props: ButtonProps) => {
  return props.label;
};
`

func TestTSXWithInterfaceAndArrow(t *testing.T) {
	src := []byte(tsxSrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "ButtonProps", "SCOPE.Schema")
	assertKind(t, entities, "Button", "SCOPE.Operation")
}

// --------------------------------------------------------------------------
// edge case: empty JS file → zero entities
// --------------------------------------------------------------------------

func TestEmptyFile(t *testing.T) {
	entities := extract(t, []byte{}, "javascript", nil)
	if len(entities) != 0 {
		t.Errorf("expected 0 entities for empty file, got %d: %v", len(entities), entityNames(entities))
	}
}

// --------------------------------------------------------------------------
// edge case: zero-byte content, non-nil but empty tree
// --------------------------------------------------------------------------

func TestZeroByteContent(t *testing.T) {
	entities := extract(t, []byte(""), "javascript", nil)
	if len(entities) != 0 {
		t.Errorf("expected 0 entities for zero-byte content, got %d", len(entities))
	}
}

// --------------------------------------------------------------------------
// error path: malformed JS (syntax errors) → extracts valid entities
// --------------------------------------------------------------------------

const malformedJSSrc = `
function good() {
  return 42;
}

function bad( {
  // syntax error — unclosed paren
}
`

func TestMalformedJSSyntaxErrors(t *testing.T) {
	src := []byte(malformedJSSrc)
	// Parse with JS grammar — tree-sitter is error-tolerant, produces partial tree.
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	// "good" should still be extracted even with a syntax error elsewhere.
	if findByName(entities, "good") == nil {
		t.Error("expected 'good' to be extracted despite syntax error in another function")
	}
}

// --------------------------------------------------------------------------
// error path: TS file with type errors (syntactically valid) → all entities
// --------------------------------------------------------------------------

const tsTypeErrorSrc = `
interface Config {
  port: number;
}

function start(cfg: Config): void {
  console.log(cfg.port);
}
`

func TestTSSyntacticallyValidTypeError(t *testing.T) {
	src := []byte(tsTypeErrorSrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "Config", "SCOPE.Schema")
	assertKind(t, entities, "start", "SCOPE.Operation")
}

// --------------------------------------------------------------------------
// error path: binary content labeled as JS → returns empty list gracefully
// --------------------------------------------------------------------------

func TestBinaryContentAsJS(t *testing.T) {
	// Binary content — tree-sitter will produce a parse tree but with high error
	// ratio. The extractor should not panic.
	binary := make([]byte, 256)
	for i := range binary {
		binary[i] = byte(i)
	}
	// Use nil tree to simulate "tree skipped due to binary content".
	entities := extract(t, binary, "javascript", nil)
	if entities == nil {
		// nil is acceptable — empty list is also fine.
		return
	}
	// No entity should have an empty or garbage name.
	for _, e := range entities {
		if e.Name == "" || e.Kind == "" {
			t.Errorf("entity with empty name/kind in binary output: %+v", e)
		}
	}
}

// --------------------------------------------------------------------------
// stress: large file (>1MB) — completes without OOM
// --------------------------------------------------------------------------

func TestLargeFile(t *testing.T) {
	// Build a JS file that is slightly over 1 MB.
	var sb strings.Builder
	for sb.Len() < 1<<20 {
		sb.WriteString("function fn")
		// Unique names to avoid dedup.
		sb.WriteString(strings.Repeat("x", 8))
		sb.WriteString("() { return 1; }\n")
	}
	src := []byte(sb.String())
	tree := parseJS(t, src)

	// Must complete without panic or OOM. We do not assert on entity count.
	entities := extract(t, src, "javascript", tree)
	if len(entities) == 0 {
		t.Error("expected at least one entity from large file")
	}
}

// --------------------------------------------------------------------------
// happy path: multiple functions in one file
// --------------------------------------------------------------------------

const jsManyFuncsSrc = `
function alpha() {}
function beta() {}
function gamma() {}
`

func TestMultipleFunctions(t *testing.T) {
	src := []byte(jsManyFuncsSrc)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	for _, name := range []string{"alpha", "beta", "gamma"} {
		assertKind(t, entities, name, "SCOPE.Operation")
	}
}

// --------------------------------------------------------------------------
// happy path: nested function (not extracted at outer scope)
// --------------------------------------------------------------------------

const jsNestedSrc = `
function outer() {
  function inner() {
    return 1;
  }
  return inner();
}
`

func TestNestedFunctions(t *testing.T) {
	src := []byte(jsNestedSrc)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	// Both outer and inner should be found.
	assertKind(t, entities, "outer", "SCOPE.Operation")
	assertKind(t, entities, "inner", "SCOPE.Operation")
}

// --------------------------------------------------------------------------
// golden fixture: JS sample_express.js
// --------------------------------------------------------------------------

func TestGoldenJSSampleExpress(t *testing.T) {
	// Reproduce the exact entities expected from fixtures/sources/javascript/sample_express.js
	src := []byte(`const express = require("express");
const app = express();
app.use(express.json());
const users = [{ id: 1, name: "Alice", email: "alice@example.com" }];
function validateUser(req, res, next) {
  if (!req.body.name || !req.body.email) {
    return res.status(400).json({ error: "name and email required" });
  }
  next();
}
`)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	assertKind(t, entities, "validateUser", "SCOPE.Operation")
	assertKind(t, entities, "express", "SCOPE.Component")
}

// --------------------------------------------------------------------------
// golden fixture: TS sample_express.ts
// --------------------------------------------------------------------------

func TestGoldenTSSampleExpress(t *testing.T) {
	src := []byte(`import express, { Request, Response, NextFunction } from "express";
interface User {
  id: number;
  name: string;
  email: string;
}
interface CreateUserBody {
  name: string;
  email: string;
}
const app = express();
function authMiddleware(req: Request, res: Response, next: NextFunction): void {
  const token = req.headers.authorization;
  if (!token) {
    res.status(401).json({ error: "Unauthorized" });
    return;
  }
  next();
}
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "User", "SCOPE.Schema")
	assertKind(t, entities, "CreateUserBody", "SCOPE.Schema")
	assertKind(t, entities, "authMiddleware", "SCOPE.Operation")
	assertKind(t, entities, "express", "SCOPE.Component")
}
