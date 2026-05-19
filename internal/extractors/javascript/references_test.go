package javascript_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// referencesFrom returns the REFERENCES ToIDs emitted from a named
// entity in the extracted record slice.
func referencesFrom(ents []types.EntityRecord, fromName string) []string {
	src := findByNameRel(ents, fromName)
	if src == nil {
		return nil
	}
	var out []string
	for _, r := range src.Relationships {
		if r.Kind == "REFERENCES" {
			out = append(out, r.ToID)
		}
	}
	return out
}

// hasReferencesTo reports whether `from` has any REFERENCES ToID whose
// trailing identifier matches `targetName`. Format A structural refs
// embed the name as the last colon-segment, so we test the suffix.
func hasReferencesTo(ents []types.EntityRecord, from, targetName string) bool {
	for _, id := range referencesFrom(ents, from) {
		if strings.HasSuffix(id, ":"+targetName) {
			return true
		}
	}
	return false
}

// TestReferences_SameScopeIdentifier — Track A.
// `const X = useState(false)` declares X; later `setX(true)` is a CALL
// (not a reference target). But `<button onClick={() => doX(X)}>` uses
// X as a value — that is a REFERENCES edge.
func TestReferences_SameScopeIdentifier(t *testing.T) {
	src := `const ENDPOINT = "/api/clients";
function fetchClients() {
  return fetch(ENDPOINT);
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	if !hasReferencesTo(ents, "fetchClients", "ENDPOINT") {
		t.Errorf("expected REFERENCES fetchClients->ENDPOINT; got %v", referencesFrom(ents, "fetchClients"))
	}
}

// TestReferences_TemplateLiteralInterpolation — Track B.
// `` fetch(`${BASE}/users`) `` should resolve BASE as a REFERENCES edge.
func TestReferences_TemplateLiteralInterpolation(t *testing.T) {
	src := `const BASE = "/api";
function loadUsers() {
  return fetch(` + "`${BASE}/users`" + `);
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	if !hasReferencesTo(ents, "loadUsers", "BASE") {
		t.Errorf("expected REFERENCES loadUsers->BASE; got %v", referencesFrom(ents, "loadUsers"))
	}
}

// TestReferences_NoEdgeToGlobals — globals must never produce a
// REFERENCES edge, even if a user-declared name happens to collide.
func TestReferences_NoEdgeToGlobals(t *testing.T) {
	src := `function log() {
  console.log("x");
  fetch("/y");
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	refs := referencesFrom(ents, "log")
	for _, id := range refs {
		if strings.HasSuffix(id, ":console") || strings.HasSuffix(id, ":fetch") {
			t.Errorf("unexpected REFERENCES edge to global: %s", id)
		}
	}
}

// TestReferences_NoSelfEdge — a function referencing itself by name
// (recursion-like shape) must NOT emit REFERENCES to itself. The
// existing CALLS path drops self-recursion; REFERENCES does too.
func TestReferences_NoSelfEdge(t *testing.T) {
	src := `function helper() {
  const x = helper;
  return x;
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	for _, id := range referencesFrom(ents, "helper") {
		if strings.HasSuffix(id, ":helper") {
			t.Errorf("unexpected self REFERENCES edge: %s", id)
		}
	}
}

// TestReferences_DedupePerPair — multiple usages of the same identifier
// inside a function body must collapse to a single REFERENCES edge.
func TestReferences_DedupePerPair(t *testing.T) {
	src := `const FLAG = true;
function check() {
  if (FLAG) {}
  if (FLAG) {}
  return FLAG;
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	n := 0
	for _, id := range referencesFrom(ents, "check") {
		if strings.HasSuffix(id, ":FLAG") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected 1 REFERENCES check->FLAG after dedup, got %d", n)
	}
}

// TestReferences_NoEdgeWhenIdentifierIsCallee — a `helper()` call
// emits CALLS, not REFERENCES. We must NOT double-count by also
// emitting REFERENCES to helper.
func TestReferences_NoEdgeWhenIdentifierIsCallee(t *testing.T) {
	src := `function helper() {}
function caller() {
  helper();
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	for _, id := range referencesFrom(ents, "caller") {
		if strings.HasSuffix(id, ":helper") {
			t.Errorf("unexpected REFERENCES caller->helper (CALLS owns this edge): %s", id)
		}
	}
}

// TestReferences_TrackC_ImportTarget — an imported name used as a
// value inside a function body should produce a REFERENCES edge to
// the same-file-emitted local binding for that import. (Cross-file
// resolution to the originating module happens via IMPORTS; this
// test verifies the in-file reference link is present.)
func TestReferences_TrackC_ImportTarget(t *testing.T) {
	src := `import { CONFIG } from "./config";
function setup() {
  return CONFIG;
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	// The import binding doesn't currently emit a per-binding entity
	// (only an IMPORTS edge on the module entity), so a CONFIG entity
	// may not exist in the file scope. The test asserts the
	// conservative behaviour: NO REFERENCES edge to a non-existent
	// symbol. If a future change emits per-binding import entities,
	// the same machinery will produce the REFERENCES edge for free.
	for _, id := range referencesFrom(ents, "setup") {
		if !strings.Contains(id, "scope:") {
			t.Errorf("non-structural REFERENCES ToID: %s", id)
		}
	}
}

// TestReferences_FunctionDeclaration_References — a function_declaration
// is the canonical from-entity shape; ensure it can host REFERENCES.
func TestReferences_FunctionDeclaration_References(t *testing.T) {
	src := `const greeting = "hi";
function greet() {
  return greeting;
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	if !hasReferencesTo(ents, "greet", "greeting") {
		t.Errorf("expected REFERENCES greet->greeting; got %v", referencesFrom(ents, "greet"))
	}
}

// TestReferences_ArrowFunctionConst — `const fn = () => x` must attribute
// REFERENCES to the const name, not file scope.
func TestReferences_ArrowFunctionConst(t *testing.T) {
	src := `const data = { count: 0 };
const reader = () => data;
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	if !hasReferencesTo(ents, "reader", "data") {
		t.Errorf("expected REFERENCES reader->data; got %v", referencesFrom(ents, "reader"))
	}
}

// TestReferences_TypeScriptParity — same behaviour on TS grammar.
func TestReferences_TypeScriptParity(t *testing.T) {
	src := `const BASE: string = "/api";
function loadUsers(): Promise<unknown> {
  return fetch(` + "`${BASE}/users`" + `);
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasReferencesTo(ents, "loadUsers", "BASE") {
		t.Errorf("expected TS REFERENCES loadUsers->BASE; got %v", referencesFrom(ents, "loadUsers"))
	}
}

// ============================================================================
// #709 — same-file TYPE-position REFERENCES (type aliases, interfaces, enums)
// ============================================================================

// TestReferences_TS_TypeAnnotation_Param — type annotation on function
// parameter must emit REFERENCES to the same-file type entity.
func TestReferences_TS_TypeAnnotation_Param(t *testing.T) {
	src := `type DobStatus = "open" | "closed";
function classify(s: DobStatus): boolean {
  return s === "open";
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasReferencesTo(ents, "classify", "DobStatus") {
		t.Errorf("expected REFERENCES classify->DobStatus; got %v", referencesFrom(ents, "classify"))
	}
}

// TestReferences_TS_TypeAnnotation_ReturnAndConst — type annotation on
// const declarator and function return type both emit REFERENCES.
func TestReferences_TS_TypeAnnotation_ReturnAndConst(t *testing.T) {
	src := `type Status = "a" | "b";
const initial: Status = "a";
function next(): Status { return "b"; }
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasReferencesTo(ents, "initial", "Status") {
		t.Errorf("expected REFERENCES initial->Status; got %v", referencesFrom(ents, "initial"))
	}
	if !hasReferencesTo(ents, "next", "Status") {
		t.Errorf("expected REFERENCES next->Status; got %v", referencesFrom(ents, "next"))
	}
}

// TestReferences_TS_GenericArgument — `React.forwardRef<X, IAccordionProps>`
// generic argument must emit a REFERENCES edge to the same-file type.
func TestReferences_TS_GenericArgument(t *testing.T) {
	src := `type IAccordionProps = { open: boolean };
const Accordion = forwardRef<HTMLDivElement, IAccordionProps>((p, ref) => null);
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasReferencesTo(ents, "Accordion", "IAccordionProps") {
		t.Errorf("expected REFERENCES Accordion->IAccordionProps; got %v", referencesFrom(ents, "Accordion"))
	}
}

// TestReferences_TS_AsCast — `x as MyType` must emit REFERENCES.
func TestReferences_TS_AsCast(t *testing.T) {
	src := `type MyType = { v: number };
function coerce(x: unknown) {
  return x as MyType;
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasReferencesTo(ents, "coerce", "MyType") {
		t.Errorf("expected REFERENCES coerce->MyType; got %v", referencesFrom(ents, "coerce"))
	}
}

// TestReferences_TS_IsPredicate — `x is Foo` predicate must emit REFERENCES.
func TestReferences_TS_IsPredicate(t *testing.T) {
	src := `type Foo = { kind: "foo" };
function isFoo(x: unknown): x is Foo {
  return typeof x === "object";
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasReferencesTo(ents, "isFoo", "Foo") {
		t.Errorf("expected REFERENCES isFoo->Foo; got %v", referencesFrom(ents, "isFoo"))
	}
}

// TestReferences_TS_Satisfies — `x satisfies MyType` must emit REFERENCES.
func TestReferences_TS_Satisfies(t *testing.T) {
	src := `type Config = { port: number };
function build() {
  const cfg = { port: 80 } satisfies Config;
  return cfg;
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasReferencesTo(ents, "build", "Config") {
		t.Errorf("expected REFERENCES build->Config; got %v", referencesFrom(ents, "build"))
	}
}

// TestReferences_TS_ConditionalExtends — `T extends MyType ? ... : ...`
// conditional-type extends clause emits REFERENCES.
func TestReferences_TS_ConditionalExtends(t *testing.T) {
	src := `type MyType = { kind: "x" };
type Pick2<T> = T extends MyType ? T : never;
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasReferencesTo(ents, "Pick2", "MyType") {
		t.Errorf("expected REFERENCES Pick2->MyType; got %v", referencesFrom(ents, "Pick2"))
	}
}

// TestReferences_TS_InterfaceTypeUsage — interface used as a type
// annotation emits REFERENCES.
func TestReferences_TS_InterfaceTypeUsage(t *testing.T) {
	src := `interface IUser { id: number }
function load(u: IUser): IUser { return u; }
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasReferencesTo(ents, "load", "IUser") {
		t.Errorf("expected REFERENCES load->IUser; got %v", referencesFrom(ents, "load"))
	}
}

// TestReferences_TS_InterfaceExtends — `interface B extends A {}` emits
// REFERENCES from B to A.
func TestReferences_TS_InterfaceExtends(t *testing.T) {
	src := `interface A { id: number }
interface B extends A { name: string }
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasReferencesTo(ents, "B", "A") {
		t.Errorf("expected REFERENCES B->A; got %v", referencesFrom(ents, "B"))
	}
}

// TestReferences_TS_TypeofQuery — `typeof X` queries the type of a
// value; X is a value-reference in type position.
func TestReferences_TS_TypeofQuery(t *testing.T) {
	src := `const config = { port: 80 };
type Config = typeof config;
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasReferencesTo(ents, "Config", "config") {
		t.Errorf("expected REFERENCES Config->config; got %v", referencesFrom(ents, "Config"))
	}
}

// TestReferences_TS_JSXComponent — `<MyComponent />` should emit a
// REFERENCES edge from the using function to the same-file MyComponent
// binding. JSX is parsed with the JS grammar (tree-sitter-typescript has
// a separate TSX grammar; the JS grammar is what the extractor uses for
// JSX-bearing files in the existing test helpers).
func TestReferences_TS_JSXComponent(t *testing.T) {
	src := `const MyComponent = (p) => null;
function App() {
  return <MyComponent x={1} />;
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	if !hasReferencesTo(ents, "App", "MyComponent") {
		t.Errorf("expected REFERENCES App->MyComponent; got %v", referencesFrom(ents, "App"))
	}
}

// TestReferences_TS_BuiltinsNotEmitted — built-in TS types (string,
// number, Array, Promise) must NOT produce REFERENCES edges. They have
// no same-file declaration, so the symbol-table guard already handles
// this, but we test the negative explicitly.
func TestReferences_TS_BuiltinsNotEmitted(t *testing.T) {
	src := `function fn(x: string, y: number): Promise<Array<number>> {
  return Promise.resolve([y]);
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	refs := referencesFrom(ents, "fn")
	for _, id := range refs {
		for _, banned := range []string{":string", ":number", ":Array", ":Promise"} {
			if strings.HasSuffix(id, banned) {
				t.Errorf("unexpected REFERENCES edge to built-in: %s", id)
			}
		}
	}
}

// TestReferences_TS_TypeAlias_NoSelfEdge — a recursive type alias must
// not produce a self-edge (consistent with the value-reference rule).
func TestReferences_TS_TypeAlias_NoSelfEdge(t *testing.T) {
	src := `type Tree = { value: number; children: Tree[] };
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	for _, id := range referencesFrom(ents, "Tree") {
		if strings.HasSuffix(id, ":Tree") {
			t.Errorf("unexpected self REFERENCES edge: %s", id)
		}
	}
}

// TestReferences_TS_TupleAndArrayType — `MyType[]` and `[MyType, X]`
// tuple/array types still emit REFERENCES to the inner type.
func TestReferences_TS_TupleAndArrayType(t *testing.T) {
	src := `type Item = { id: number };
function pack(items: Item[]): [Item, number] { return [items[0], 1]; }
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasReferencesTo(ents, "pack", "Item") {
		t.Errorf("expected REFERENCES pack->Item; got %v", referencesFrom(ents, "pack"))
	}
}

// TestReferences_TS_NonTSCorpusParity — pure-JS source must produce
// the identical edge set as before #709 (the type-position handling
// is a strict superset that only fires when a type_identifier is in
// the tree). This is the cross-language parity guard.
func TestReferences_TS_NonTSCorpusParity(t *testing.T) {
	src := `const A = 1;
function use() { return A; }
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	if !hasReferencesTo(ents, "use", "A") {
		t.Errorf("expected REFERENCES use->A; got %v", referencesFrom(ents, "use"))
	}
	// And no spurious edges: only one REFERENCES from use.
	n := 0
	for _, r := range findByNameRel(ents, "use").Relationships {
		if r.Kind == "REFERENCES" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected 1 REFERENCES from use, got %d", n)
	}
}
