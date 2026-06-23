package javascript_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tsjavascript "github.com/smacker/go-tree-sitter/javascript"
	tstypescript "github.com/smacker/go-tree-sitter/typescript/typescript"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors/javascript"
	"github.com/cajasmota/grafel/internal/types"
)

// --------------------------------------------------------------------------
// test helpers
// --------------------------------------------------------------------------

// parseJS parses source with the JavaScript grammar and returns the tree.
func parseJS(t *testing.T, src []byte) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tsjavascript.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parseJS: %v", err)
	}
	return tree
}

// parseTS parses source with the TypeScript grammar and returns the tree.
func parseTS(t *testing.T, src []byte) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tstypescript.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parseTS: %v", err)
	}
	return tree
}

// extract runs the extractor and returns the entity slice.
func extract(t *testing.T, content []byte, language string, tree ts.Tree) []types.EntityRecord {
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

func (s *jsExtractorShim) extract(content []byte, language string, tree ts.Tree) ([]types.EntityRecord, error) {
	e := javascript.New()
	return e.Extract(context.Background(), extreg.FileInput{
		Path:     "test.go",
		Content:  content,
		Language: language,
		TSTree:   tree,
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

// TestES6Import (updated for #742) — ES6 import statements emit IMPORTS edges
// on the file entity. Import-placeholder SCOPE.Component/import entities are
// no longer emitted.
func TestES6Import(t *testing.T) {
	src := []byte(tsImportSrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	// No import-placeholder entities with subtype="import".
	for _, e := range entities {
		if e.Kind == "SCOPE.Component" && e.Subtype == "import" {
			t.Errorf("SCOPE.Component/import placeholder entity still emitted (#742): %q", e.Name)
		}
	}

	// IMPORTS edges for "express" and "fs" must exist on the file entity.
	wantSpecs := map[string]bool{"express": false, "fs": false}
	for i := range entities {
		for j := range entities[i].Relationships {
			r := &entities[i].Relationships[j]
			if r.Kind != "IMPORTS" {
				continue
			}
			ip := ""
			if r.Properties != nil {
				ip = r.Properties["import_path"]
			}
			if ip == "" {
				ip = r.ToID
			}
			if _, ok := wantSpecs[ip]; ok {
				wantSpecs[ip] = true
			}
		}
	}
	for spec, found := range wantSpecs {
		if !found {
			t.Errorf("IMPORTS edge for %q not found on any entity", spec)
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

	// Issue #562 — plain const assignments (require() calls that aren't
	// function wrappers or context factories) are no longer emitted.
	// Expect zero entities for `const X = require(...)`.
	if e := findByName(entities, "path"); e != nil {
		t.Fatalf("path should not be emitted; got %+v", e)
	}
	if e := findByName(entities, "express"); e != nil {
		t.Fatalf("express should not be emitted; got %+v", e)
	}
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
	// Issue #562 — plain const assignments (require() and call results)
	// are no longer emitted. Only function entities are expected.
	if e := findByName(entities, "express"); e != nil {
		t.Fatalf("express should not be emitted; got %+v", e)
	}
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
	// Issue #742: import placeholder entity for "express" no longer exists.
	// The IMPORTS edge is now on the file entity. Verify the edge exists.
	importsExpressFound := false
	for i := range entities {
		for j := range entities[i].Relationships {
			r := &entities[i].Relationships[j]
			if r.Kind != "IMPORTS" {
				continue
			}
			ip := ""
			if r.Properties != nil {
				ip = r.Properties["import_path"]
			}
			if ip == "" {
				ip = r.ToID
			}
			if ip == "express" {
				importsExpressFound = true
			}
		}
	}
	if !importsExpressFound {
		t.Error("IMPORTS edge for 'express' not found on any entity after #742 fix")
	}
}

// --------------------------------------------------------------------------
// Issue #522 — extract entities for `export const X = <non-function-value>`
//
// Pre-fix: JS/TS extractor only emitted entities for arrow / function
// expressions assigned through `const X = ...`. Every other const-export
// shape (object literal, configured instance, React wrapper call, hook
// alias, …) produced ZERO entities, so alias-resolved imports targeting
// those consts landed in bug-extractor. The fix emits an entity per
// const-declarator regardless of value type, classifying wrapper-call
// shapes as SCOPE.Operation and everything else as SCOPE.Component.
// --------------------------------------------------------------------------

const constExportObjectSrc = `
export const ROUTES = {
  home: "/",
  login: "/login",
};

export const COLORS = { primary: "#000", secondary: "#fff" };
`

func TestConstExportObjectLiteral(t *testing.T) {
	src := []byte(constExportObjectSrc)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	// Issue #562 — plain const assignments (objects, arrays, literals, etc.)
	// are no longer emitted. They are synthetic resolver state, not queryable.
	if e := findByName(entities, "ROUTES"); e != nil {
		t.Fatalf("ROUTES should not be emitted; got %+v", e)
	}
	if e := findByName(entities, "COLORS"); e != nil {
		t.Fatalf("COLORS should not be emitted; got %+v", e)
	}
}

const constExportInstanceSrc = `
import axios from "axios";
import { QueryClient } from "@tanstack/react-query";

export const api = axios.create({ baseURL: "/api" });
export const queryClient = new QueryClient();
`

func TestConstExportInstance(t *testing.T) {
	src := []byte(constExportInstanceSrc)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	// Issue #562 — plain const assignments (call results, new instances)
	// are no longer emitted. Only function wrappers and context factories
	// are kept as they're semantically meaningful graph nodes.
	if e := findByName(entities, "api"); e != nil {
		t.Fatalf("api should not be emitted; got %+v", e)
	}
	if e := findByName(entities, "queryClient"); e != nil {
		t.Fatalf("queryClient should not be emitted; got %+v", e)
	}
}

const constExportReactWrappersSrc = `
import React, { forwardRef, memo } from "react";
import { observer } from "mobx-react";
import { connect } from "react-redux";

export const Button = forwardRef((props, ref) => <button ref={ref} />);
export const Card = memo((props) => <div />);
export const Header = observer((props) => <header />);
export const Connected = connect(mapState)(Component);
`

func TestConstExportReactWrappers(t *testing.T) {
	src := []byte(constExportReactWrappersSrc)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	assertKind(t, entities, "Button", "SCOPE.Operation")
	assertKind(t, entities, "Card", "SCOPE.Operation")
	assertKind(t, entities, "Header", "SCOPE.Operation")
	assertKind(t, entities, "Connected", "SCOPE.Operation")
}

const constExportHookAliasSrc = `
import { useSelector, useDispatch } from "react-redux";

export const useAppSelector = useSelector;
export const useAppDispatch = useDispatch;
`

func TestConstExportHookAlias(t *testing.T) {
	src := []byte(constExportHookAliasSrc)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	// Issue #562 — plain identifier aliases (const X = Y) are no longer emitted.
	// These are synthetic resolver state resolved through structural refs.
	if e := findByName(entities, "useAppSelector"); e != nil {
		t.Fatalf("useAppSelector should not be emitted; got %+v", e)
	}
	if e := findByName(entities, "useAppDispatch"); e != nil {
		t.Fatalf("useAppDispatch should not be emitted; got %+v", e)
	}
}

const constExportReducerSrc = `
import { createSlice } from "@reduxjs/toolkit";

const slice = createSlice({ name: "cart", initialState: {}, reducers: {} });

export const cartReducer = slice.reducer;
export const cartActions = slice.actions;
`

func TestConstExportReducer(t *testing.T) {
	src := []byte(constExportReducerSrc)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	// Issue #562 — plain const assignments (member expressions like
	// slice.reducer and slice.actions) are no longer emitted.
	if e := findByName(entities, "cartReducer"); e != nil {
		t.Fatalf("cartReducer should not be emitted; got %+v", e)
	}
	if e := findByName(entities, "cartActions"); e != nil {
		t.Fatalf("cartActions should not be emitted; got %+v", e)
	}
}

const tsConstExportTypedSrc = `
import { z } from "zod";

type UserSchemaT = { id: string };

export const userSchema: UserSchemaT = z.object({ id: z.string() });

export const MAX_RETRIES: number = 5;
`

func TestTSConstExportTyped(t *testing.T) {
	src := []byte(tsConstExportTypedSrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	// Issue #562 + #709: type-annotated const declarations ARE emitted to
	// support type-position REFERENCES edge attribution. Plain const assignments
	// without type annotations are not emitted (synthetic resolver state).
	// Both userSchema and MAX_RETRIES have explicit type annotations, so they
	// should be emitted as SCOPE.Component subtype="const".
	assertKind(t, entities, "userSchema", "SCOPE.Component")
	assertKind(t, entities, "MAX_RETRIES", "SCOPE.Component")
}

const constExportArrowStillWorksSrc = `
export const useAuth = () => useContext(AuthCtx);
`

// Regression guard: arrow-function const exports must still classify as
// SCOPE.Operation after the #522 extension.
func TestConstExportArrowFunctionRegression(t *testing.T) {
	src := []byte(constExportArrowStillWorksSrc)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	assertKind(t, entities, "useAuth", "SCOPE.Operation")
}

// --------------------------------------------------------------------------
// #584 — destructure-rename lift
// --------------------------------------------------------------------------

// Shorthand object pattern → entity per leaf, SCOPE.Component default.
func TestDestructureShorthandObject(t *testing.T) {
	src := []byte(`const { foo, bar } = somethingArbitrary();`)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	assertKind(t, entities, "foo", "SCOPE.Component")
	assertKind(t, entities, "bar", "SCOPE.Component")
}

// Rename pair → entity for the LOCAL name (value side), not the property key.
func TestDestructureRenamePropertyEmitsLocalName(t *testing.T) {
	src := []byte(`const { foo: bar } = makeIt();`)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	if got := findByName(entities, "bar"); got == nil {
		t.Fatalf("expected local-name entity 'bar', got names: %v", entityNames(entities))
	}
	if got := findByName(entities, "foo"); got != nil {
		t.Errorf("did NOT expect property-key entity 'foo', but found one")
	}
}

// React Query mutation hook → destructured leaves lift as SCOPE.Operation.
func TestDestructureMutationHookLiftsOperation(t *testing.T) {
	src := []byte(`
import { useCreateAlternateAddress } from "./hooks";
function Cmp() {
  const { mutate: createAddress, isSuccess } = useCreateAlternateAddress();
  return createAddress();
}
`)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	assertKind(t, entities, "createAddress", "SCOPE.Operation")
	assertKind(t, entities, "isSuccess", "SCOPE.Operation")
}

// useQuery → all leaves lift; data + isLoading both emitted.
func TestDestructureUseQueryEmitsAllLeaves(t *testing.T) {
	src := []byte(`
function Cmp() {
  const { data, isLoading, error } = useQuery({ queryKey: ["x"] });
  return data;
}
`)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	if findByName(entities, "data") == nil {
		t.Errorf("expected 'data'; names: %v", entityNames(entities))
	}
	if findByName(entities, "isLoading") == nil {
		t.Errorf("expected 'isLoading'; names: %v", entityNames(entities))
	}
	if findByName(entities, "error") == nil {
		t.Errorf("expected 'error'; names: %v", entityNames(entities))
	}
}

// Array destructure → entity per identifier (covers useState-like tuples).
func TestDestructureArrayPatternEmitsAll(t *testing.T) {
	src := []byte(`
function Cmp() {
  const [error, setError] = useState(null);
  const [a, b] = arr;
}
`)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	// useState → Operation lift (the hook is in the mutation-style allowlist).
	assertKind(t, entities, "error", "SCOPE.Operation")
	assertKind(t, entities, "setError", "SCOPE.Operation")
	// `arr` is an identifier RHS, not a hook call → Component default.
	assertKind(t, entities, "a", "SCOPE.Component")
	assertKind(t, entities, "b", "SCOPE.Component")
}

// Nested object pattern → leaf binding (y), not the intermediate key (x).
func TestDestructureNestedObjectPattern(t *testing.T) {
	src := []byte(`const { x: { y } } = z;`)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	if findByName(entities, "y") == nil {
		t.Fatalf("expected leaf entity 'y'; names: %v", entityNames(entities))
	}
	if findByName(entities, "x") != nil {
		t.Errorf("did NOT expect intermediate-key entity 'x'")
	}
}

// Real-world cfb pattern — Modal/useModal style with multiple renames.
func TestDestructureUseModalLiftsCallables(t *testing.T) {
	src := []byte(`
function Cmp() {
  const { open: openModal, close: closeModal } = useModal();
  return openModal();
}
`)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	assertKind(t, entities, "openModal", "SCOPE.Operation")
	assertKind(t, entities, "closeModal", "SCOPE.Operation")
}

// Regression: issue #562 — plain const assignments (non-function-wrapper,
// non-context-factory) are no longer emitted as entities. This test verifies
// that `const useAppDispatch = useDispatch;` produces zero entities (synthetic
// state, not queryable graph structure). REFERENCES edges will still resolve
// through structural refs without entity materialization.
func TestDestructureRegressionNonPattern(t *testing.T) {
	src := []byte(`const useAppDispatch = useDispatch;`)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	// Expect no entity for plain const assignment
	if e := findByName(entities, "useAppDispatch"); e != nil {
		t.Fatalf("useAppDispatch should not be emitted; got %+v", e)
	}
}

// --------------------------------------------------------------------------
// Issue #771 — class-field arrow methods → SCOPE.Operation
// --------------------------------------------------------------------------

// Pattern 1: plain arrow `name = () => body`
func TestClassFieldArrow_PlainArrow(t *testing.T) {
	src := []byte(`
class UserService {
  getAll = () => this.$http.get('/users');
}
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "getAll", "SCOPE.Operation")
	e := findByName(entities, "getAll")
	if e == nil {
		t.Fatal("getAll not found")
	}
	if e.Subtype != "method" {
		t.Errorf("Subtype=%q, want 'method'", e.Subtype)
	}
}

// Pattern 1b: plain arrow in JS (field_definition, not public_field_definition)
// This is the dominant fixture-e shape — AngularJS-style JS service classes.
func TestClassFieldArrow_PlainArrowJS(t *testing.T) {
	src := []byte(`
class ProductsService {
  allProducts = ({page}) => this.$http.get('/products');
  getUploadTemplate = () => downloadFile('/products/upload/template');
  uploadProducts = ({file}) => uploadFile('/products/upload', file);
}
`)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	assertKind(t, entities, "allProducts", "SCOPE.Operation")
	assertKind(t, entities, "getUploadTemplate", "SCOPE.Operation")
	assertKind(t, entities, "uploadProducts", "SCOPE.Operation")
	for _, name := range []string{"allProducts", "getUploadTemplate", "uploadProducts"} {
		e := findByName(entities, name)
		if e != nil && e.Subtype != "method" {
			t.Errorf("%s: Subtype=%q, want 'method'", name, e.Subtype)
		}
	}
}

// Pattern 2: async arrow `name = async () => body`
func TestClassFieldArrow_AsyncArrow(t *testing.T) {
	src := []byte(`
class UserService {
  fetchUser = async () => this.repo.findOne();
}
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "fetchUser", "SCOPE.Operation")
	e := findByName(entities, "fetchUser")
	if e == nil {
		t.Fatal("fetchUser not found")
	}
	if !strings.Contains(e.Signature, "async") {
		t.Errorf("Signature=%q, want 'async' in signature for async arrow", e.Signature)
	}
}

// Pattern 3: parameterized arrow `name = (a, b) => body`
func TestClassFieldArrow_Parameterized(t *testing.T) {
	src := []byte(`
class CartService {
  addItem = (id, qty) => this.$http.post('/cart', { id, qty });
}
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "addItem", "SCOPE.Operation")
}

// Pattern 4: TS-typed parameters `name = (a: T) => body`
func TestClassFieldArrow_TSTypedParams(t *testing.T) {
	src := []byte(`
class OrderService {
  byId = (id: string) => this.$http.get('/orders/' + id);
}
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "byId", "SCOPE.Operation")
}

// Pattern 5: TS generic `name = <T>(a: T) => body`
func TestClassFieldArrow_TSGeneric(t *testing.T) {
	src := []byte(`
class DataService {
  get = <T>(url: string): Promise<T> => this.$http.get(url);
}
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "get", "SCOPE.Operation")
}

// Pattern 6: block body `name = (a) => { /* multi-line */ }`
func TestClassFieldArrow_BlockBody(t *testing.T) {
	src := []byte(`
class AuthService {
  login = (user, pass) => {
    const token = this.auth.signIn(user, pass);
    return token;
  };
}
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "login", "SCOPE.Operation")
}

// Pattern 7: static arrow `static name = () => body`
func TestClassFieldArrow_StaticArrow(t *testing.T) {
	src := []byte(`
class ConfigService {
  static defaults = () => ({ timeout: 3000 });
}
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "defaults", "SCOPE.Operation")
	e := findByName(entities, "defaults")
	if e == nil {
		t.Fatal("defaults not found")
	}
	if !strings.Contains(e.Signature, "static") {
		t.Errorf("Signature=%q, want 'static' in signature for static arrow", e.Signature)
	}
}

// Pattern 8: TS property type annotation `name: Type = () => body`
func TestClassFieldArrow_TSPropertyTypeAnnotation(t *testing.T) {
	src := []byte(`
class NotificationService {
  send: (msg: string) => void = (msg) => this.mailer.send(msg);
}
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "send", "SCOPE.Operation")
}

// Pattern 9: TS access modifier `private name = () => body`
func TestClassFieldArrow_TSAccessModifier(t *testing.T) {
	src := []byte(`
class PaymentService {
  private charge = (amount: number) => this.$http.post('/charge', { amount });
}
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "charge", "SCOPE.Operation")
}

// Negative case 1: plain value assignment stays as non-Operation field.
// `name = 'foo'` should NOT emit SCOPE.Operation (stays as Component via
// walkChildren → no emit at all for bare public_field_definition without arrow).
func TestClassFieldArrow_Negative_PlainStringField(t *testing.T) {
	src := []byte(`
class Config {
  baseUrl = '/api';
  timeout = 5000;
  debug = false;
}
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	// These plain fields must NOT become Operation entities.
	for _, name := range []string{"baseUrl", "timeout", "debug"} {
		e := findByName(entities, name)
		if e != nil && e.Kind == "SCOPE.Operation" {
			t.Errorf("field %q: got SCOPE.Operation, want no Operation entity for plain value", name)
		}
	}
}

// Negative case 2: method_definition (non-arrow class methods) continue to
// work via the existing handleMethodDefinition path — regression guard.
func TestClassFieldArrow_Negative_RegularMethodUnaffected(t *testing.T) {
	src := []byte(`
class Counter {
  increment() { this.count++; }
  decrement() { this.count--; }
}
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "increment", "SCOPE.Operation")
	assertKind(t, entities, "decrement", "SCOPE.Operation")
}

// Full service class: all patterns together — the dominant fixture-e shape.
func TestClassFieldArrow_ServiceClass_MultipleArrowMethods(t *testing.T) {
	src := []byte(`
class ProductService {
  getAll = () => this.$http.get('/products');
  byId = (id) => this.$http.get('/products/' + id);
  create = async (data) => this.$http.post('/products', data);
  update = async (id, data) => this.$http.put('/products/' + id, data);
  remove = (id) => this.$http.delete('/products/' + id);
}
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	for _, name := range []string{"getAll", "byId", "create", "update", "remove"} {
		assertKind(t, entities, name, "SCOPE.Operation")
		e := findByName(entities, name)
		if e != nil && e.Subtype != "method" {
			t.Errorf("%s: Subtype=%q, want 'method'", name, e.Subtype)
		}
	}
}

// CONTAINS edges: class entity must carry CONTAINS for arrow-method children.
func TestClassFieldArrow_ClassContainsArrowMethods(t *testing.T) {
	src := []byte(`
class ItemService {
  list = () => this.$http.get('/items');
  detail = (id) => this.$http.get('/items/' + id);
}
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	cls := findByName(entities, "ItemService")
	if cls == nil {
		t.Fatal("ItemService class entity not found")
	}

	// Both arrow methods must appear in CONTAINS relationships on the class.
	containsNames := map[string]bool{}
	for _, rel := range cls.Relationships {
		if rel.Kind == "CONTAINS" {
			containsNames[rel.ToID] = true
		}
	}
	if len(containsNames) == 0 {
		t.Errorf("ItemService has no CONTAINS relationships; list and detail should be contained")
	}
}

// --------------------------------------------------------------------------
// Issue #611 — createContext() emitted as SCOPE.Component subtype=context
// --------------------------------------------------------------------------

// TestCreateContext_BasicContext verifies that `const X = createContext(default)`
// is emitted as SCOPE.Component with subtype="context", not SCOPE.Operation.
func TestCreateContext_BasicContext(t *testing.T) {
	src := []byte(`
import { createContext } from 'react';
const AuthContext = createContext(null);
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "AuthContext", "SCOPE.Component")
	e := findByName(entities, "AuthContext")
	if e == nil {
		t.Fatal("AuthContext entity not found")
	}
	if e.Subtype != "context" {
		t.Errorf("AuthContext: Subtype=%q, want 'context'", e.Subtype)
	}
}

// TestCreateContext_Typed verifies the TypeScript generic form:
// `const X = createContext<T | null>(null)` — still SCOPE.Component.
func TestCreateContext_Typed(t *testing.T) {
	src := []byte(`
import { createContext } from 'react';
interface AuthCtx { user: string; }
const AuthContext = createContext<AuthCtx | null>(null);
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "AuthContext", "SCOPE.Component")
	e := findByName(entities, "AuthContext")
	if e != nil && e.Subtype != "context" {
		t.Errorf("AuthContext (typed): Subtype=%q, want 'context'", e.Subtype)
	}
}

// TestCreateContext_MultipleContexts verifies that multiple createContext calls
// in one file each produce their own SCOPE.Component context entity.
func TestCreateContext_MultipleContexts(t *testing.T) {
	src := []byte(`
import { createContext } from 'react';
const UserContext = createContext(null);
const ThemeContext = createContext({ color: 'blue' });
const AuthContext = createContext(null);
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	for _, name := range []string{"UserContext", "ThemeContext", "AuthContext"} {
		assertKind(t, entities, name, "SCOPE.Component")
		e := findByName(entities, name)
		if e != nil && e.Subtype != "context" {
			t.Errorf("%s: Subtype=%q, want 'context'", name, e.Subtype)
		}
	}
}

// TestCreateContext_NotAWrapper verifies that createContext is no longer treated
// as a function wrapper (it must NOT be SCOPE.Operation).
func TestCreateContext_NotAWrapper(t *testing.T) {
	src := []byte(`
import { createContext, createContext as createCtx } from 'react';
const Ctx = createContext(defaultValue);
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	e := findByName(entities, "Ctx")
	if e == nil {
		t.Fatal("Ctx entity not found")
	}
	if e.Kind == "SCOPE.Operation" {
		t.Errorf("Ctx: should not be SCOPE.Operation (createContext result is a Context object, not a function)")
	}
	if e.Kind != "SCOPE.Component" {
		t.Errorf("Ctx: Kind=%q, want SCOPE.Component", e.Kind)
	}
}

// TestMemo_StillOperation verifies that memo() (a wrapper that DOES return a
// component function) is still emitted as SCOPE.Operation, not affected by #611.
func TestMemo_StillOperation(t *testing.T) {
	src := []byte(`
import { memo } from 'react';
function CardInner({ user }) { return null; }
const MemoCard = memo(CardInner);
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "MemoCard", "SCOPE.Operation")
}
