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

// =============================================================================
// Issue #710 — Same-file destructure binding REFERENCES.
//
// Destructured bindings (const { foo } = useHook()) emit a const_destructure
// SCOPE.Component entity per local name. Same-file uses of that name must
// produce a REFERENCES edge from the enclosing function frame to the
// destructure entity, including when the use is in call position (CALLS
// edges target Operation-kinded callees only, so the const_destructure
// entity stays orphaned without an additive REFERENCES edge).
// =============================================================================

// anySameFileReferenceTo reports whether any entity in the slice has a
// REFERENCES edge whose ToID trails with `:targetName`. Used when the
// emitter attributes the edge to whichever same-file frame (parent
// component or nested arrow-bound const) wraps the usage — what we care
// about for orphan reduction is that SOMETHING in the file references
// the destructure binding entity.
func anySameFileReferenceTo(ents []types.EntityRecord, targetName string) bool {
	suffix := ":" + targetName
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == "REFERENCES" && strings.HasSuffix(r.ToID, suffix) {
				return true
			}
		}
	}
	return false
}

// TestReferences_DestructureBinding_SimpleObject — `const { foo } = obj`
// then `foo()` inside the same function emits REFERENCES to foo. The
// attribution lands on the nearest enclosing function-like frame; nested
// arrow bound to a const wins over the outer component.
func TestReferences_DestructureBinding_SimpleObject(t *testing.T) {
	src := `function ReviewTabContent() {
  const { onConfirmLeave } = useReviewLeaveGuard();
  const handleClose = () => onConfirmLeave();
  return handleClose;
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !anySameFileReferenceTo(ents, "onConfirmLeave") {
		t.Errorf("expected SOME same-file REFERENCES edge to onConfirmLeave; got from ReviewTabContent=%v, from handleClose=%v",
			referencesFrom(ents, "ReviewTabContent"),
			referencesFrom(ents, "handleClose"))
	}
}

// TestReferences_DestructureBinding_Renamed — `const { foo: bar } = obj`
// renames the property; the local binding name is bar. Same-file uses of
// bar must REFERENCES the bar entity (not foo).
func TestReferences_DestructureBinding_Renamed(t *testing.T) {
	src := `function comp() {
  const { mutate: createAddress } = useCreateAddress();
  const onSave = () => createAddress();
  return onSave;
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasReferencesTo(ents, "comp", "createAddress") {
		t.Errorf("expected REFERENCES comp->createAddress; got %v",
			referencesFrom(ents, "comp"))
	}
	// Negative: should NOT bind to the property key 'mutate'.
	for _, id := range referencesFrom(ents, "comp") {
		if strings.HasSuffix(id, ":mutate") {
			t.Errorf("unexpected REFERENCES to property key 'mutate': %s", id)
		}
	}
}

// TestReferences_DestructureBinding_WithDefault — `const { foo = "x" } = obj`
// still binds foo; default value should not prevent REFERENCES emission.
func TestReferences_DestructureBinding_WithDefault(t *testing.T) {
	src := `function comp() {
  const { onConfirm = noop } = props;
  const fire = () => onConfirm();
  return fire;
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !anySameFileReferenceTo(ents, "onConfirm") {
		t.Errorf("expected SOME same-file REFERENCES edge to onConfirm; got from comp=%v, from fire=%v",
			referencesFrom(ents, "comp"), referencesFrom(ents, "fire"))
	}
}

// TestReferences_DestructureBinding_ArrayPattern — `const [first, second] = arr`
// emits one entity per identifier; same-file uses must REFERENCES the
// binding entity.
func TestReferences_DestructureBinding_ArrayPattern(t *testing.T) {
	src := `function comp() {
  const [first, second] = useState(0);
  const show = () => first + second;
  return show;
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasReferencesTo(ents, "comp", "first") {
		t.Errorf("expected REFERENCES comp->first; got %v",
			referencesFrom(ents, "comp"))
	}
	if !hasReferencesTo(ents, "comp", "second") {
		t.Errorf("expected REFERENCES comp->second; got %v",
			referencesFrom(ents, "comp"))
	}
}

// TestReferences_DestructureBinding_CallPosition — the destructured
// binding is invoked as a function. CALLS would target an Operation
// (which a SCOPE.Component const_destructure is not); REFERENCES must
// still fire so the binding entity has an inbound edge.
func TestReferences_DestructureBinding_CallPosition(t *testing.T) {
	src := `function ReviewTabContent() {
  const { onConfirmLeave } = useReviewLeaveGuard();
  return <Foo onConfirm={() => onConfirmLeave(clearChecks)} />;
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasReferencesTo(ents, "ReviewTabContent", "onConfirmLeave") {
		t.Errorf("expected REFERENCES ReviewTabContent->onConfirmLeave (call position); got %v",
			referencesFrom(ents, "ReviewTabContent"))
	}
}

// =============================================================================
// Issue #711 — Define-then-export icon pattern.
//
// `const X = createIcon(...); export { X };` produces a const_call entity
// for X but no inbound edge (X is never referenced inside the file; the
// export clause is a separate statement). Emit REFERENCES from the file
// entity to each named specifier whose binding is a same-file symbol.
// =============================================================================

// TestReferences_ExportClause_DefineThenExport — define-then-export
// emits REFERENCES from the file entity to the named const.
func TestReferences_ExportClause_DefineThenExport(t *testing.T) {
	src := `const FlamingoIcon = createIcon({ viewBox: "0 0 24 24" });
const HippoIcon = createIcon({ viewBox: "0 0 24 24" });
export { FlamingoIcon, HippoIcon };
`
	tree := parseJSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "icons.tsx")

	// File entity carries the REFERENCES edges for export clauses.
	file := findByNameRel(ents, "icons.tsx")
	if file == nil {
		t.Fatalf("file entity not found")
	}
	got := map[string]bool{}
	for _, r := range file.Relationships {
		if r.Kind == "REFERENCES" {
			for _, name := range []string{"FlamingoIcon", "HippoIcon"} {
				if strings.HasSuffix(r.ToID, ":"+name) {
					got[name] = true
				}
			}
		}
	}
	if !got["FlamingoIcon"] {
		t.Errorf("expected REFERENCES file->FlamingoIcon; got %+v", file.Relationships)
	}
	if !got["HippoIcon"] {
		t.Errorf("expected REFERENCES file->HippoIcon; got %+v", file.Relationships)
	}
}

// TestReferences_ExportClause_RenamedExport — `export { X as Y }` binds
// to the local name X (the `name` field of export_specifier).
func TestReferences_ExportClause_RenamedExport(t *testing.T) {
	src := `const FlamingoIcon = createIcon({});
export { FlamingoIcon as Flamingo };
`
	tree := parseJSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "icons.tsx")

	file := findByNameRel(ents, "icons.tsx")
	if file == nil {
		t.Fatalf("file entity not found")
	}
	found := false
	for _, r := range file.Relationships {
		if r.Kind == "REFERENCES" && strings.HasSuffix(r.ToID, ":FlamingoIcon") {
			found = true
		}
		if r.Kind == "REFERENCES" && strings.HasSuffix(r.ToID, ":Flamingo") {
			t.Errorf("unexpected REFERENCES to alias name Flamingo: %s", r.ToID)
		}
	}
	if !found {
		t.Errorf("expected REFERENCES file->FlamingoIcon for `export { FlamingoIcon as Flamingo }`; got %+v", file.Relationships)
	}
}

// TestReferences_ExportClause_MultipleSpecifiers — a single export
// statement with N specifiers emits REFERENCES to each named const.
func TestReferences_ExportClause_MultipleSpecifiers(t *testing.T) {
	src := `const A = createIcon({});
const B = createIcon({});
const C = createIcon({});
export { A, B, C };
`
	tree := parseJSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "icons.tsx")

	file := findByNameRel(ents, "icons.tsx")
	if file == nil {
		t.Fatalf("file entity not found")
	}
	for _, name := range []string{"A", "B", "C"} {
		found := false
		for _, r := range file.Relationships {
			if r.Kind == "REFERENCES" && strings.HasSuffix(r.ToID, ":"+name) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected REFERENCES file->%s; got %+v", name, file.Relationships)
		}
	}
}

// TestReferences_ExportClause_ReExportSkipped — `export { X } from './baz'`
// is a re-export forwarding from another module; X is NOT a same-file
// declaration and MUST NOT receive a REFERENCES edge from the file.
func TestReferences_ExportClause_ReExportSkipped(t *testing.T) {
	src := `export { Foo } from './baz';
`
	tree := parseJSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "barrel.ts")

	file := findByNameRel(ents, "barrel.ts")
	if file == nil {
		t.Fatalf("file entity not found")
	}
	for _, r := range file.Relationships {
		if r.Kind == "REFERENCES" && strings.HasSuffix(r.ToID, ":Foo") {
			t.Errorf("unexpected REFERENCES edge for re-export `export { Foo } from './baz'`: %s", r.ToID)
		}
	}
}

// TestReferences_ThisAttrJS — Issue #679.
// A JS class method that accesses `this.<field>` should emit a REFERENCES
// edge to the class field entity (SCOPE.Schema/field, Name=ClassName.field).
func TestReferences_ThisAttrJS(t *testing.T) {
	src := `class Counter {
  count = 0;
  increment() {
    this.count = this.count + 1;
  }
  reset() {
    this.count = 0;
  }
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	// The field entity must exist.
	field := findByNameRel(ents, "Counter.count")
	if field == nil {
		t.Fatalf("expected SCOPE.Schema/field entity Counter.count; got nil")
	}
	if field.Kind != "SCOPE.Schema" || field.Subtype != "field" {
		t.Errorf("field entity: want Kind=SCOPE.Schema/field, got %s/%s", field.Kind, field.Subtype)
	}

	// increment() must reference Counter.count.
	if !hasReferencesTo(ents, "increment", "Counter.count") {
		t.Errorf("expected REFERENCES increment->Counter.count; got %v", referencesFrom(ents, "increment"))
	}
	// reset() must reference Counter.count.
	if !hasReferencesTo(ents, "reset", "Counter.count") {
		t.Errorf("expected REFERENCES reset->Counter.count; got %v", referencesFrom(ents, "reset"))
	}
}

// TestReferences_ThisAttrTS — Issue #679 TypeScript variant.
// A TS class method that accesses `this.<field>` (typed field) should emit
// a REFERENCES edge to the class field entity.
func TestReferences_ThisAttrTS(t *testing.T) {
	src := `class Service {
  private baseUrl: string;
  private timeout: number;
  fetch(path: string): string {
    return this.baseUrl + path;
  }
  getTimeout(): number {
    return this.timeout;
  }
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	// Both typed fields must be emitted as SCOPE.Schema/field.
	baseUrlField := findByNameRel(ents, "Service.baseUrl")
	if baseUrlField == nil {
		t.Fatalf("expected SCOPE.Schema/field entity Service.baseUrl; got nil")
	}
	timeoutField := findByNameRel(ents, "Service.timeout")
	if timeoutField == nil {
		t.Fatalf("expected SCOPE.Schema/field entity Service.timeout; got nil")
	}

	// fetch() must reference Service.baseUrl.
	if !hasReferencesTo(ents, "fetch", "Service.baseUrl") {
		t.Errorf("expected REFERENCES fetch->Service.baseUrl; got %v", referencesFrom(ents, "fetch"))
	}
	// getTimeout() must reference Service.timeout.
	if !hasReferencesTo(ents, "getTimeout", "Service.timeout") {
		t.Errorf("expected REFERENCES getTimeout->Service.timeout; got %v", referencesFrom(ents, "getTimeout"))
	}
}

// TestReferences_ThisAttrCallNotDoubleEmitted — Issue #679.
// `this.method()` calls are CALLS-owned; no REFERENCES edge for the callee.
// The object `this` itself is not a project entity — no edge for `this`.
func TestReferences_ThisAttrCallNotDoubleEmitted(t *testing.T) {
	src := `class Processor {
  data = [];
  process() {
    this.helper();
  }
  helper() {
    return this.data;
  }
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	// process() calls helper() — CALLS owns that edge; no REFERENCES to helper.
	for _, id := range referencesFrom(ents, "process") {
		if strings.HasSuffix(id, ":helper") {
			t.Errorf("process() should not emit REFERENCES to helper (CALLS owns it); got %s", id)
		}
	}

	// helper() accesses this.data — REFERENCES to Processor.data expected.
	if !hasReferencesTo(ents, "helper", "Processor.data") {
		t.Errorf("expected REFERENCES helper->Processor.data; got %v", referencesFrom(ents, "helper"))
	}
}
