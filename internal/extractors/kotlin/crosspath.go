// Cross-package CALLS qualifier reconciliation for Kotlin (issue #4375).
//
// Kotlin reaches a function/method in another package through a qualifier on a
// navigation_expression invocation — a fully-qualified
// `com.app.services.OrderService.place()`, an imported top-level function
// (`import com.app.services.placeOrder; placeOrder()`), an imported type
// (`import com.app.services.Orders; Orders.place()`), an aliased import
// (`import com.app.x.OrderService as Svc; Svc.place()`), or a companion/object
// member (`OrderService.create()`). The base extractor (kotlinCallTarget) only
// returns the trailing simple_identifier of the navigation chain, so a
// multi-segment qualified call collapses to the bare leaf method name
// (`place`). The bare leaf resolves through the global byName index, which goes
// ambiguous the moment two packages define a same-named function/type
// (`OrderService.place` in both com.app.services and com.app.billing) — so the
// CALLS edge drops and the callee package looks falsely uncalled. This is the
// Kotlin analogue of the Go cross-package (#4332), Rust cross-module (#4373),
// and C# cross-namespace (#4374) qualifier drops.
//
// Like C# namespaces (and unlike Go/Rust directories), Kotlin `package`
// declarations are NOT directory-bound: a file's declared package need not
// match its source directory. So the resolver keys on the Kotlin PACKAGE (not
// the source directory). The extractor stamps the resolved (package, type,
// leaf) onto the CALLS edge; the resolver binds it through a package-keyed
// member/operation index built in BuildIndex (ResolveKotlinCrossPackageCalls in
// internal/resolve/imports.go). Kotlin function entities carry a bare Name (not
// a dotted `Type.method`), so unlike the C# pass the index is populated from the
// `kotlin_package` / `kotlin_enclosing_type` properties stamped on each entity
// rather than from a dotted Name — a parallel Kotlin index, not byNamespaceMember.
//
// Conservative by construction: only fires for a navigation invocation whose
// receiver chain is a statically-qualified path the file context can map to a
// concrete (package[, type], leaf). Bare unqualified calls, instance-receiver
// calls, and star-import / extension-receiver chains we cannot statically
// resolve are left to the base extractor — no false stamps.
package kotlin

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// kotlinCrossCtx is the per-file resolution context derived from the file's
// `package` declaration and `import` directives. Built once per file and
// threaded through the call extractor so qualified calls can map a leading path
// qualifier (or an imported leaf) to a concrete Kotlin package.
type kotlinCrossCtx struct {
	// filePackage is the package declared by this file's `package` header,
	// e.g. "com.app.services". A same-package companion/object call
	// `OrderService.create()` (where OrderService is declared in this package)
	// is resolved against it.
	filePackage string

	// importedTypes maps a simple type name brought into scope by
	// `import com.app.services.Orders` (or an `as` alias) to its declaring
	// package, so `Orders.place()` recovers the package. The map key is the
	// in-scope name (the alias when present, else the imported leaf).
	// name -> package (e.g. "Orders" -> "com.app.services").
	importedTypes map[string]string

	// aliasRealType maps an `as` alias to the REAL imported leaf name, so an
	// aliased member call `Svc.place()` (import OrderService as Svc) resolves
	// the declaring type as the underlying `OrderService`, not the alias.
	// alias -> realLeaf (e.g. "Svc" -> "OrderService"). Only populated for
	// aliased imports.
	aliasRealType map[string]string

	// importedFuncs maps a simple function name brought into scope by
	// `import com.app.services.placeOrder` (or an `as` alias) to its declaring
	// package, so a bare `placeOrder()` recovers the package. The map key is
	// the in-scope name (the alias when present, else the imported leaf).
	// name -> package (e.g. "placeOrder" -> "com.app.services").
	importedFuncs map[string]string

	// hasStarImport is true when the file carries any `import com.app.x.*`
	// wildcard. Wildcard imports make resolution non-unique (any of the
	// star-imported packages could define the leaf), so a bare/Type-qualified
	// call under an active star import is conservatively NOT stamped — the
	// star-import case is a documented follow-up (#4334).
	hasStarImport bool

	// classRecvTypes maps a CLASS-LEVEL property name to the concrete class it
	// is statically constructed as — the MockK `@InjectMockKs val controller =
	// XController()` / `val controller = mockk<XController>()` field idiom
	// (#4687, the Kotlin slice of epic #4615). Set transiently while a class
	// body is walked so each test method's CALLS extractor can type a receiver
	// declared as a class field, then restored on exit. The per-method body
	// locals (collected in extractCallRelationships) take precedence over these
	// — a same-named local shadows the field within the method scope.
	classRecvTypes map[string]string
}

// buildKotlinCrossCtx scans the compilation unit for the package header and
// import headers and assembles the per-file Kotlin cross-package context.
//
// An `import` may resolve to either a type or a top-level function; we cannot
// tell from the import alone, so the imported leaf is registered in BOTH
// importedTypes and importedFuncs and the resolver disambiguates by which
// package index actually holds the (type, leaf) vs (leaf) binding.
func buildKotlinCrossCtx(root *sitter.Node, src []byte) *kotlinCrossCtx {
	if root == nil {
		return nil
	}
	ctx := &kotlinCrossCtx{
		importedTypes: map[string]string{},
		importedFuncs: map[string]string{},
		aliasRealType: map[string]string{},
	}
	for _, p := range findAllNodes(root, "package_header") {
		if pkg := kotlinPackageOfHeader(p, src); pkg != "" {
			ctx.filePackage = pkg
			break
		}
	}
	for _, imp := range findAllNodes(root, "import_header") {
		full, alias, star := parseKotlinImport(imp, src)
		if full == "" {
			continue
		}
		if star {
			ctx.hasStarImport = true
			continue
		}
		dot := strings.LastIndexByte(full, '.')
		if dot <= 0 {
			continue
		}
		pkg := full[:dot]
		leaf := full[dot+1:]
		name := leaf
		if alias != "" {
			name = alias
		}
		if name == "" || pkg == "" {
			continue
		}
		// The imported leaf may be a type or a top-level function — register
		// under both; the resolver picks the index that actually binds.
		ctx.importedTypes[name] = pkg
		ctx.importedFuncs[name] = pkg
		if alias != "" {
			// `import com.app.x.OrderService as Svc` — remember the real leaf
			// so an aliased member call `Svc.place()` looks up type=OrderService.
			ctx.aliasRealType[alias] = leaf
		}
	}
	return ctx
}

// kotlinPackageOfHeader returns the dotted package path of a package_header
// node (`package com.app.services`), or "" when absent.
func kotlinPackageOfHeader(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	// tree-sitter-kotlin: package_header → [package] [identifier dotted path].
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if ch.Type() == "package" {
			continue
		}
		txt := strings.TrimSpace(string(src[ch.StartByte():ch.EndByte()]))
		if txt != "" {
			return txt
		}
	}
	return ""
}

// parseKotlinImport returns the dotted import path (wildcard suffix stripped),
// the `as` alias (or ""), and whether the import is a `.*` wildcard. Mirrors
// buildImport's text shaping so the two stay consistent.
func parseKotlinImport(n *sitter.Node, src []byte) (full, alias string, star bool) {
	raw := strings.TrimSpace(string(src[n.StartByte():n.EndByte()]))
	raw = strings.TrimPrefix(raw, "import ")
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "//"); i >= 0 {
		raw = strings.TrimSpace(raw[:i])
	}
	if i := strings.Index(raw, " as "); i >= 0 {
		alias = strings.TrimSpace(raw[i+4:])
		raw = strings.TrimSpace(raw[:i])
	}
	if strings.HasSuffix(raw, ".*") {
		star = true
		raw = strings.TrimSpace(strings.TrimSuffix(raw, ".*"))
	}
	return raw, alias, star
}

// qualifiedKotlinCall describes the (package, type, leaf) a qualified Kotlin
// call resolves to. typ is "" for a top-level-function call
// (`placeOrder()` via `import ...placeOrder`); pkgCandidates carries the
// candidate packages (most-specific first).
type qualifiedKotlinCall struct {
	leaf          string   // function name
	typ           string   // declaring type (empty for top-level fn)
	pkgCandidates []string // candidate packages (most-specific first)
}

// resolveKotlinQualifiedCall inspects a call_expression node and, when its
// receiver chain is a statically-qualified cross-package path, returns the
// (package candidates, type, leaf) binding. Returns nil for shapes that are not
// statically resolvable (instance receivers, bare same-file calls under no
// matching import, star-import uncertainty, unresolvable chains) so the base
// extractor's bare target is used unchanged.
//
// localNames is the set of in-scope value names (parameters, properties, locals)
// whose head segment marks an INSTANCE receiver — those are left to the base
// extractor, never stamped as a cross-package static qualifier.
// recvTypes maps an in-scope value name (local or class field) to the concrete
// class it was statically constructed as (`val c = XController()` /
// `@InjectMockKs val c = XController()` / `val c = mockk<XController>()`). A
// navigation call whose head is such a typed receiver (`c.getCounts()`) is bound
// to the class method via a (package, Type=that class, leaf) stamp — the #4687
// local-variable / MockK receiver-typing path. Empty/nil → no typed-local path.
func (c *kotlinCrossCtx) resolveKotlinQualifiedCall(
	call *sitter.Node,
	src []byte,
	localNames map[string]bool,
	recvTypes map[string]string,
) *qualifiedKotlinCall {
	if c == nil || call == nil || call.ChildCount() == 0 {
		return nil
	}
	first := call.Child(0)

	switch first.Type() {
	case "simple_identifier":
		// Bare `placeOrder()` — only a cross-package call when the name was
		// brought in by a top-level-function import. Same-file / same-package
		// bare calls have no import entry and are left to the base extractor.
		leaf := string(src[first.StartByte():first.EndByte()])
		if leaf == "" {
			return nil
		}
		if c.hasStarImport {
			return nil // star import could supply this leaf — uncertain, skip.
		}
		if pkg, ok := c.importedFuncs[leaf]; ok && pkg != "" {
			return &qualifiedKotlinCall{leaf: leaf, pkgCandidates: []string{pkg}}
		}
		return nil

	case "navigation_expression":
		return c.resolveKotlinNavigationCall(first, src, localNames, recvTypes)
	}
	return nil
}

// resolveKotlinNavigationCall handles a `a.b.c()` navigation invocation. The
// trailing navigation_suffix identifier is the leaf; the segments before it are
// the receiver path. A statically-qualified receiver maps to (package, type).
func (c *kotlinCrossCtx) resolveKotlinNavigationCall(
	nav *sitter.Node,
	src []byte,
	localNames map[string]bool,
	recvTypes map[string]string,
) *qualifiedKotlinCall {
	segs, ok := flattenKotlinNavigation(nav, src)
	if !ok || len(segs) < 2 {
		return nil
	}
	leaf := segs[len(segs)-1]
	recv := segs[:len(segs)-1] // the receiver path segments
	if leaf == "" || len(recv) == 0 {
		return nil
	}
	head := recv[0]
	// #4687 — typed-local / typed-field receiver. A plain `c.method()` whose
	// receiver `c` was statically typed to a concrete class via a constructor
	// call, an explicit type annotation, or a `mockk<T>()` builder resolves to
	// that class's method (the test→CALLS→handler coverage path). Only the
	// single-segment receiver form `c.method()` is bound here — `c.field.m()`
	// would need field-type tracking and falls through. This branch runs BEFORE
	// the instance-receiver guard so a typed local is upgraded rather than
	// dropped; an untyped local (factory/`mockk()` receiver) still falls to the
	// guard and stays bare (honest exclusion).
	if len(recv) == 1 {
		if typ, ok := recvTypes[head]; ok && typ != "" {
			return c.buildTypedReceiverCall(leaf, typ)
		}
	}
	// Instance-receiver guard: a head that is a known local/param/property is
	// an instance call (`order.place()`), owned by the base extractor — not a
	// static cross-package qualifier.
	if localNames[head] {
		return nil
	}
	// The rightmost receiver segment is the declaring Type; the segments before
	// it form the package path. A lone Type segment (`OrderService.place()`)
	// leaves an empty package path → recover via imported-type / file-package
	// candidates.
	typ := recv[len(recv)-1]
	if typ == "" {
		return nil
	}
	pkgPath := strings.Join(recv[:len(recv)-1], ".")

	b := &qualifiedKotlinCall{leaf: leaf, typ: typ}
	if pkgPath != "" {
		// Fully-qualified `com.app.services.OrderService.place()`. If the head
		// is an imported type alias, the path is actually `alias.member` and
		// should be handled as the lone-type case below; only treat as a fully
		// qualified package path when the head is NOT an in-scope imported name.
		if _, aliased := c.importedTypes[head]; !aliased {
			b.pkgCandidates = []string{pkgPath}
			return b
		}
	}
	// `Type.method()` (or `alias.method()`): candidate packages are the
	// imported-type package (incl. aliases) and the file's own package
	// (most-specific first).
	if c.hasStarImport {
		// A wildcard import could also supply this type — conservatively skip
		// the unqualified case to avoid a wrong bind. (#4334 follow-up.)
		if _, known := c.importedTypes[typ]; !known {
			return nil
		}
	}
	if pkg, ok := c.importedTypes[typ]; ok && pkg != "" {
		b.pkgCandidates = appendUniqueKt(b.pkgCandidates, pkg)
	}
	if c.filePackage != "" {
		b.pkgCandidates = appendUniqueKt(b.pkgCandidates, c.filePackage)
	}
	// Resolve an alias `Svc` to its real imported type name `OrderService` for
	// the index lookup (the package mapping is keyed on the alias, but the
	// callee entity's enclosing type is the real name).
	if real, ok := c.aliasRealType[typ]; ok && real != "" {
		b.typ = real
	}
	if len(b.pkgCandidates) == 0 {
		return nil
	}
	return b
}

// kotlinLocalReceiverTypes scans a function/lambda body and returns a map of
// local value name → the concrete class it is statically constructed as, for
// the local-variable receiver-typing path (#4687, the Kotlin slice of epic
// #4615 — the analogue of Java #4682 collectLocalVarTypes/newExprClassName,
// TS/JS #4680, Python #4716, Go #4683). Three trusted shapes:
//
//   - Constructor call (Kotlin has NO `new` keyword):
//     `val c = XController(svc)` → c : XController. The initializer is a
//     call_expression whose callee is a bare PascalCase simple_identifier and
//     which has a call_suffix (real invocation). This is the dominant modern
//     test idiom for the SUT.
//   - Explicit type annotation: `val c: XController = makeIt()` → c : XController
//     (the declared user_type wins regardless of the RHS shape).
//   - MockK local: `val c = mockk<XController>()` → c : XController (the type
//     argument of the `mockk`/`spyk` builder), the Kotlin mirror of Java's
//     Mockito `@InjectMocks` and the C# DI `GetRequiredService<T>` cases.
//
// Honest exclusion (no entry, receiver stays bare): a factory/builder/`make()`
// call (`val c = makeController()`), a method chain, a non-PascalCase callee, a
// `mockk()` with no static type argument, a literal, a cast — anything whose
// class is not statically recoverable. First binding per name wins.
func kotlinLocalReceiverTypes(body *sitter.Node, src []byte) map[string]string {
	if body == nil {
		return nil
	}
	out := map[string]string{}
	for _, decl := range findAllNodes(body, "property_declaration") {
		name, vd := kotlinPropertyVarNameNode(decl, src)
		if name == "" {
			continue
		}
		if _, taken := out[name]; taken {
			continue // first binding wins
		}
		// 1. Explicit declared type (`val c: XController = …`).
		if typ := kotlinUserTypeLeaf(vd, src); typ != "" && isKotlinPascalType(typ) {
			out[name] = typ
			continue
		}
		// 2 / 3. Infer from the initializer call_expression (ctor or mockk<T>()).
		if init := kotlinPropertyInitCall(decl); init != nil {
			if typ := kotlinConstructedOrMockType(init, src); typ != "" {
				out[name] = typ
			}
		}
	}
	return out
}

// kotlinPropertyVarNameNode returns the declared value name and its
// variable_declaration node from a property_declaration
// (`val a: T = …` / `var a = …`). Returns ("", nil) when no variable_declaration
// child is present (multi-declaration / destructuring shapes are skipped).
func kotlinPropertyVarNameNode(decl *sitter.Node, src []byte) (string, *sitter.Node) {
	for i := 0; i < int(decl.ChildCount()); i++ {
		ch := decl.Child(i)
		if ch.Type() != "variable_declaration" {
			continue
		}
		for j := 0; j < int(ch.ChildCount()); j++ {
			id := ch.Child(j)
			if id.Type() == "simple_identifier" {
				return string(src[id.StartByte():id.EndByte()]), ch
			}
		}
	}
	return "", nil
}

// kotlinUserTypeLeaf returns the leaf type_identifier of a variable_declaration's
// explicit type annotation (`a: XController` → "XController"), stripping any
// package/generic wrapping by taking the first type_identifier descendant of the
// user_type. Returns "" when the declaration has no explicit type.
func kotlinUserTypeLeaf(vd *sitter.Node, src []byte) string {
	if vd == nil {
		return ""
	}
	var ut *sitter.Node
	for i := 0; i < int(vd.ChildCount()); i++ {
		if vd.Child(i).Type() == "user_type" {
			ut = vd.Child(i)
			break
		}
	}
	if ut == nil {
		return ""
	}
	ids := findAllNodes(ut, "type_identifier")
	if len(ids) == 0 {
		return ""
	}
	// The declared type is the FIRST type_identifier (`com.x.XController<T>` →
	// the outer type, not a generic argument). findAllNodes is pre-order so the
	// leftmost/outermost user_type's identifier comes first for the common
	// unqualified `XController` shape.
	n := ids[0]
	return string(src[n.StartByte():n.EndByte()])
}

// kotlinPropertyInitCall returns the initializer call_expression of a
// property_declaration (`val c = XController(svc)` → the `XController(svc)`
// call_expression), or nil when the RHS is not a direct call expression.
func kotlinPropertyInitCall(decl *sitter.Node) *sitter.Node {
	// The RHS is the child following the `=` token. Find `=`, then the next
	// non-trivial sibling.
	sawEq := false
	for i := 0; i < int(decl.ChildCount()); i++ {
		ch := decl.Child(i)
		if ch.Type() == "=" {
			sawEq = true
			continue
		}
		if !sawEq {
			continue
		}
		if ch.Type() == "call_expression" {
			return ch
		}
		// Any other RHS node (literal, navigation, when, etc.) — not a ctor/mockk.
		return nil
	}
	return nil
}

// kotlinConstructedOrMockType returns the class a property initializer call
// constructs, for the two trusted shapes:
//   - `XController(...)` — a Kotlin constructor call: callee is a bare PascalCase
//     simple_identifier with a call_suffix. Returns "XController".
//   - `mockk<XController>()` / `spyk<XController>()` — a MockK builder whose
//     single type argument names the mocked class. Returns "XController".
//
// Returns "" for any other call (factory/`make()`, lowercase callee, `mockk()`
// with no type argument, navigation chains) so the receiver stays bare.
func kotlinConstructedOrMockType(call *sitter.Node, src []byte) string {
	if call == nil || call.ChildCount() == 0 {
		return ""
	}
	callee := call.Child(0)
	if callee.Type() != "simple_identifier" {
		return ""
	}
	name := string(src[callee.StartByte():callee.EndByte()])
	// MockK builder: `mockk<T>()` / `spyk<T>()` — read the type argument.
	if name == "mockk" || name == "spyk" {
		if t := kotlinCallSuffixTypeArg(call, src); t != "" && isKotlinPascalType(t) {
			return t
		}
		return ""
	}
	// Constructor call: a PascalCase callee invoked with a call_suffix.
	if isKotlinPascalType(name) && hasCallSuffix(call) {
		return name
	}
	return ""
}

// kotlinCallSuffixTypeArg returns the leaf type_identifier of the first
// type_arguments projection on a call_expression's call_suffix
// (`mockk<XController>()` → "XController"), or "" when absent.
func kotlinCallSuffixTypeArg(call *sitter.Node, src []byte) string {
	for i := 0; i < int(call.ChildCount()); i++ {
		cs := call.Child(i)
		if cs.Type() != "call_suffix" {
			continue
		}
		for j := 0; j < int(cs.ChildCount()); j++ {
			ta := cs.Child(j)
			if ta.Type() != "type_arguments" {
				continue
			}
			ids := findAllNodes(ta, "type_identifier")
			if len(ids) > 0 {
				n := ids[0]
				return string(src[n.StartByte():n.EndByte()])
			}
		}
	}
	return ""
}

// isKotlinPascalType reports whether name is a non-empty identifier whose first
// rune is an uppercase letter — the Kotlin class-name convention. Mirrors Java's
// isPascalCase used by receiverTypeName.
func isKotlinPascalType(name string) bool {
	if name == "" {
		return false
	}
	r := rune(name[0])
	return r >= 'A' && r <= 'Z'
}

// buildTypedReceiverCall constructs the (package candidates, type, leaf) binding
// for a typed-local / typed-field receiver call `c.leaf()` where `c` is known to
// be of class `typ` (#4687). The candidate packages are the imported-type
// package for `typ` (an `import com.x.XController` brings the class into scope
// from another package) and the file's own package (a same-file SUT), most
// specific first — mirroring the lone-`Type.method()` resolution path. An alias
// (`import ... as Svc`) is resolved to the real type name for the index lookup.
// Returns nil when no candidate package can be recovered (then the receiver
// stays bare — honest exclusion).
func (c *kotlinCrossCtx) buildTypedReceiverCall(leaf, typ string) *qualifiedKotlinCall {
	b := &qualifiedKotlinCall{leaf: leaf, typ: typ}
	if pkg, ok := c.importedTypes[typ]; ok && pkg != "" {
		b.pkgCandidates = appendUniqueKt(b.pkgCandidates, pkg)
	}
	if c.filePackage != "" {
		b.pkgCandidates = appendUniqueKt(b.pkgCandidates, c.filePackage)
	}
	if real, ok := c.aliasRealType[typ]; ok && real != "" {
		b.typ = real
	}
	if len(b.pkgCandidates) == 0 {
		return nil
	}
	return b
}

func appendUniqueKt(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// flattenKotlinNavigation flattens a navigation_expression composed solely of
// simple_identifier receivers and navigation_suffix `.name` steps into its
// dotted segments. Returns (nil, false) the moment a non-static node (a `this`
// expression, a call_expression, an index/element access, a string template,
// etc.) appears in the receiver chain — those are instance chains the base
// extractor owns.
func flattenKotlinNavigation(n *sitter.Node, src []byte) ([]string, bool) {
	if n == nil {
		return nil, false
	}
	switch n.Type() {
	case "simple_identifier":
		return []string{string(src[n.StartByte():n.EndByte()])}, true
	case "navigation_expression":
		// navigation_expression → <receiver> navigation_suffix
		var receiver *sitter.Node
		var suffix *sitter.Node
		for i := 0; i < int(n.ChildCount()); i++ {
			ch := n.Child(i)
			switch ch.Type() {
			case "navigation_suffix":
				suffix = ch
			default:
				if receiver == nil {
					receiver = ch
				}
			}
		}
		if receiver == nil || suffix == nil {
			return nil, false
		}
		ls, ok := flattenKotlinNavigation(receiver, src)
		if !ok {
			return nil, false
		}
		// The suffix's trailing simple_identifier is the step name.
		var step string
		for i := int(suffix.ChildCount()) - 1; i >= 0; i-- {
			ch := suffix.Child(i)
			if ch.Type() == "simple_identifier" {
				step = string(src[ch.StartByte():ch.EndByte()])
				break
			}
		}
		if step == "" {
			return nil, false
		}
		return append(ls, step), true
	}
	return nil, false
}
