// Package kotlin implements the tree-sitter–based extractor for Kotlin source files.
//
// Extracted entities:
//   - class_declaration    → Kind="SCOPE.Component", Subtype="class"
//   - object_declaration   → Kind="SCOPE.Component", Subtype="object"
//   - function_declaration → Kind="SCOPE.Operation", Subtype="function"
//   - import_header        → Kind="SCOPE.Component", Subtype="import"
//     (one entity per import; Name is the FULL dotted path, NOT the
//     leading segment — the historical "ghost org / com / java" hazard
//     came from splitting on '.', which we do NOT do here; mirrors the
//     Python extractor's importRecord shape.)
//
// When a class carries a Spring stereotype annotation (@RestController,
// @Controller, @Service, @Component, @Repository) we additionally emit a
// Kind="SCOPE.Service" entity whose Name is the class name, matching the
// Python indexer's output.
//
// Import headers carry one IMPORTS relationship (FromID=file path,
// ToID=full dotted module path, with `.*` wildcard suffix stripped).
// This unlocks file-import-aware allowlist gating in
// internal/external/synth.go — Ktor server DSL HTTP-verb routing
// (`get("/x") { ... }`, `post(...)`, ...) is the leading residual
// bug-resolver cohort in ktor-samples (#456 / #470), and the synth
// classifier needs the file's import set to gate these collision-prone
// verb names on a genuine Ktor import.
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package kotlin

import (
	"context"
	"strconv"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("kotlin", &Extractor{})
}

// Extractor implements extractor.Extractor for Kotlin.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "kotlin" }

// Extract walks the tree-sitter CST and returns entity records for the Kotlin file.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if file.Tree == nil || len(file.Content) == 0 {
		return nil, nil
	}

	var entities []types.EntityRecord
	// Issue #577 — emit file-level SCOPE.Component (subtype="file") so the
	// cross-repo import linker (#566) can map IMPORTS edges back to the
	// originating repo via the resolver's byName index. Generalises the
	// JS/TS fix from #570/#575.
	entities = append(entities, extractor.FileEntity(file))
	root := file.Tree.RootNode()
	// #4375 — per-file cross-package context (package + imports). Threaded into
	// the call extractor so a qualified `pkg.Type.method()` / imported-fn /
	// `Type.method()` call stamps its resolved (package[, type], leaf) onto the
	// CALLS edge for the resolver's package-keyed bind.
	crossCtx := buildKotlinCrossCtx(root, file.Content)
	walk(root, file, &entities, crossCtx)

	// #4375 — stamp every Kotlin entity with its file's package. Kotlin is
	// one-package-per-file, so a post-pass is exact. The resolver's
	// package-keyed cross-package index is built from these stamps.
	if crossCtx != nil && crossCtx.filePackage != "" {
		for i := range entities {
			e := &entities[i]
			if e.Language != "kotlin" || e.Subtype == "import" || e.Subtype == "file" {
				continue
			}
			if e.Properties == nil {
				e.Properties = map[string]string{}
			}
			if _, ok := e.Properties["kotlin_package"]; !ok {
				e.Properties["kotlin_package"] = crossCtx.filePackage
			}
		}
	}

	// Track A (analog of #641/#650/#670 for Kotlin) — REFERENCES-edge
	// emission. Runs after every primary-pass entity is in place so the
	// file-scope symbol table covers functions, classes, objects, and
	// property bindings. Failures here recover internally to partial
	// results — never aborts primary output.
	func() {
		defer func() { _ = recover() }()
		emitReferences(root, file, &entities)
	}()

	// Error-flow topology (epic #3628) — THROWS / CATCHES edges from functions
	// to a shared SCOPE.ExceptionType node for `throw X(...)`, typed
	// `catch (e: X)`, Spring `@ExceptionHandler(X::class)`
	// (@ControllerAdvice/@RestControllerAdvice), and Ktor StatusPages
	// `exception<X> { ... }`. Cross-language consistent with the Java / Python
	// flagships (same Kind + edge model). Recover-wrapped so a malformed CST
	// never aborts primary output.
	func() {
		defer func() { _ = recover() }()
		emitExceptionFlowEdges(root, file, &entities)
	}()

	// Track B (analog of #642/#650/#670 for Kotlin) — IMPORTS ToID rewrite.
	// Rewrites IMPORTS edges whose dotted path's longest matching prefix
	// is a known external JVM/Kotlin package to an
	// `ext:<prefix>[:<name>]` ToID so the resolver's external-disposition
	// gate classifies them ExternalKnown directly. In-tree imports are
	// untouched — the existing ResolveDottedImportTarget path binds them
	// via source_module / imported_name properties.
	resolveImportToIDs(entities)

	// Issue #90 — language tag for resolver dynamic-pattern dispatch.
	extractor.TagRelationshipsLanguage(entities, "kotlin")
	extractor.TagEntitiesLanguage(entities, "kotlin")
	return entities, nil
}

// walk performs a depth-first traversal of the CST, collecting entities.
//
// PORT-2-FIX-2-ALL (#41): class/object declarations attach a CONTAINS edge
// per function declared inside the body, and every function body is scanned
// for call_expression / call_suffix nodes that yield CALLS edges with stub
// to_id. Imports are still NOT emitted.
func walk(node *sitter.Node, file extractor.FileInput, out *[]types.EntityRecord, ctx *kotlinCrossCtx) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "type_alias":
		// Issue #3275 — typealias Handler = (String) -> Unit
		// tree-sitter-kotlin: [type_alias] → [typealias] [type_identifier <name>] [=] <rhs>
		if rec, ok := buildTypeAlias(node, file); ok {
			*out = append(*out, rec)
		}
		return

	case "property_declaration":
		// #4428: a TOP-LEVEL `val X = mapOf(...)` constant map. (Class/object
		// body properties are intercepted by their declaration case below and
		// never reach this top-level branch.) Emit a value-set when the
		// initialiser is a closed all-literal map; otherwise fall through to
		// the default recursion so nested constructs are still visited.
		if vs, ok := emitMapValueSet(node, file); ok {
			*out = append(*out, vs)
		}
		for i := range node.ChildCount() {
			walk(node.Child(int(i)), file, out, ctx)
		}
		return

	case "class_declaration":
		subtype := classDeclarationSubtype(node, file.Content)
		// #4428: an `enum class` carries a value-set (entries + constructor
		// literal values). Emitted alongside the SCOPE.Component below.
		if subtype == "enum" {
			if vs, ok := emitEnumValueSet(node, file); ok {
				*out = append(*out, vs)
			}
		}
		rec, ok := buildComponent(node, file, subtype)
		if !ok {
			for i := range node.ChildCount() {
				walk(node.Child(int(i)), file, out, ctx)
			}
			return
		}
		classIdx := len(*out)
		*out = append(*out, rec)
		// emit Spring stereotype service entity alongside the class.
		if svc, ok := buildSpringService(node, file, rec.Name); ok {
			*out = append(*out, svc)
		}
		// Issue #690 — emit SCOPE.Schema/field for primary-constructor
		// val/var parameters (data class pattern: `data class Foo(val x: T)`).
		// These are structural properties, not just formal parameters, so they
		// must appear as field entities with CONTAINS edges to the class.
		emitPrimaryConstructorFields(node, file, rec.Name, classIdx, out)
		// CONTAINS: every function AND property declared in the class body.
		body := findClassBody(node)
		if body != nil {
			before := len(*out)
			for i := range body.ChildCount() {
				ch := body.Child(int(i))
				// Issue #690 — intercept property_declaration children so
				// we can qualify the name with the enclosing class name.
				// The generic walk() call below does not carry parentType,
				// so property entities would get bare names and CONTAINS
				// stubs would not resolve. Emit them here instead.
				if ch.Type() == "property_declaration" {
					// #4428: a body-level `val X = mapOf(...)` constant map.
					if vs, ok := emitMapValueSet(ch, file); ok {
						*out = append(*out, vs)
					}
					if propRec, ok := buildProperty(ch, file, rec.Name); ok {
						*out = append(*out, propRec)
					}
					continue
				}
				walk(ch, file, out, ctx)
			}
			// #4428: grouped `const val` string properties form a class-level
			// value-set named after the class.
			if vs, ok := emitConstGroupValueSet(body, file, rec.Name); ok {
				*out = append(*out, vs)
			}
			after := len(*out)
			for k := before; k < after; k++ {
				child := &(*out)[k]
				var toID string
				switch {
				case child.Kind == "SCOPE.Operation":
					// Issue #144 — emit a structural-ref (Format A) keyed on
					// the source file so the resolver disambiguates by location
					// when two Kotlin classes/objects in different files declare
					// same-named functions.
					toID = extractor.BuildOperationStructuralRef("kotlin", file.Path, child.Name)
					// #4375 — stamp the enclosing type so the resolver's
					// package-keyed member index can bind a cross-package
					// `Type.method()` call. Only the DIRECT member is stamped:
					// nested-class members were already stamped by the inner
					// walk (which runs first), so we skip any already set.
					stampKotlinEnclosingType(child, rec.Name)
				case child.Kind == "SCOPE.Schema" && child.Subtype == "field":
					// Issue #690 — CONTAINS for class-body properties,
					// mirroring the Python fix from #689.
					toID = extractor.BuildSchemaFieldStructuralRef("kotlin", file.Path, child.Name)
				default:
					continue
				}
				(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
					types.RelationshipRecord{
						ToID: toID,
						Kind: "CONTAINS",
					})
			}
		}
		return

	case "object_declaration":
		rec, ok := buildComponent(node, file, "object")
		if !ok {
			for i := range node.ChildCount() {
				walk(node.Child(int(i)), file, out, ctx)
			}
			return
		}
		classIdx := len(*out)
		*out = append(*out, rec)
		body := findClassBody(node)
		if body != nil {
			before := len(*out)
			for i := range body.ChildCount() {
				ch := body.Child(int(i))
				// Issue #690 — intercept property_declaration children so
				// we can qualify the name with the enclosing object name.
				if ch.Type() == "property_declaration" {
					// #4428: a body-level `val X = mapOf(...)` constant map.
					if vs, ok := emitMapValueSet(ch, file); ok {
						*out = append(*out, vs)
					}
					if propRec, ok := buildProperty(ch, file, rec.Name); ok {
						*out = append(*out, propRec)
					}
					continue
				}
				walk(ch, file, out, ctx)
			}
			// #4428: grouped `const val` string properties form an
			// object-level value-set named after the object (the common
			// `object Pages { const val CORE_ADMIN = "core-admin" }` shape).
			if vs, ok := emitConstGroupValueSet(body, file, rec.Name); ok {
				*out = append(*out, vs)
			}
			after := len(*out)
			for k := before; k < after; k++ {
				child := &(*out)[k]
				var toID string
				switch {
				case child.Kind == "SCOPE.Operation":
					// Issue #144 — emit a structural-ref (Format A) keyed on
					// the source file so the resolver disambiguates by location
					// when two Kotlin classes/objects in different files declare
					// same-named functions.
					toID = extractor.BuildOperationStructuralRef("kotlin", file.Path, child.Name)
					// #4375 — stamp the enclosing object/companion type for the
					// resolver's package-keyed member index (innermost wins).
					stampKotlinEnclosingType(child, rec.Name)
				case child.Kind == "SCOPE.Schema" && child.Subtype == "field":
					// Issue #690 — CONTAINS for object-body properties.
					toID = extractor.BuildSchemaFieldStructuralRef("kotlin", file.Path, child.Name)
				default:
					continue
				}
				(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
					types.RelationshipRecord{
						ToID: toID,
						Kind: "CONTAINS",
					})
			}
		}
		return

	case "function_declaration":
		if rec, ok := buildOperation(node, file); ok {
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(findFunctionBody(node), file.Content, rec.Name, ctx)...)
			*out = append(*out, rec)
		}
		return

	case "import_header":
		if rec, ok := buildImport(node, file); ok {
			*out = append(*out, rec)
		}
		return
	}

	for i := range node.ChildCount() {
		walk(node.Child(int(i)), file, out, ctx)
	}
}

// findClassBody returns the class_body / object_body / enum_class_body child
// of a class/object declaration, or nil when the declaration has no body.
func findClassBody(node *sitter.Node) *sitter.Node {
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		t := ch.Type()
		if t == "class_body" || t == "object_body" || t == "enum_class_body" {
			return ch
		}
	}
	return nil
}

// findFunctionBody returns the function_body child of a function_declaration,
// or nil when the function is abstract / interface / expression-body without
// a block. Tree-sitter-kotlin uses an unnamed `function_body` child.
func findFunctionBody(node *sitter.Node) *sitter.Node {
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch.Type() == "function_body" {
			return ch
		}
	}
	return nil
}

// kotlinKeywordStop lists Kotlin keywords and special identifiers that
// tree-sitter surfaces inside call_expression nodes but are NOT real
// call targets. Emitting CALLS edges for them sends meaningless
// bare-name stubs to the resolver, where they land in bug-extractor
// because no entity matches. Mirrors the Python extractor's
// `self`/`cls` drop. Issue #106.
var kotlinKeywordStop = map[string]bool{
	"synchronized": true,
	"it":           true,
	"this":         true,
	"super":        true,
	"lateinit":     true,
	"by":           true,
	"where":        true,
}

// extractCallRelationships returns one CALLS RelationshipRecord per unique
// call_expression descendant of body. The target name is the trailing
// simple_identifier of the call's expression. FromID is left empty so
// buildDocument substitutes the caller's entity ID at emit time. Self-recursion
// is dropped to match Python/Go extractor dedup semantics.
//
// When ctx is non-nil (issue #4375), each call is additionally probed for a
// statically-qualified cross-package shape — a fully-qualified
// `com.app.services.OrderService.place()`, an imported top-level function, an
// imported/aliased type member `Orders.place()`, or a same-package
// companion/object member `OrderService.create()`. A resolvable call carries
// `kotlin_call_pkg` (";"-separated candidate packages, most-specific first),
// `kotlin_call_type` (declaring type; absent for a top-level-function call), and
// `call_leaf` (the bare callee name) so the resolver can bind it through the
// package-keyed index instead of the ambiguous bare-name path.
func extractCallRelationships(body *sitter.Node, src []byte, callerName string, ctx *kotlinCrossCtx) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	calls := findAllNodes(body, "call_expression")
	if len(calls) == 0 {
		return nil
	}
	// Instance-receiver guard inputs: names whose head segment marks an
	// instance receiver (`order.place()`) rather than a static type qualifier.
	localNames := kotlinLocalValueNames(body, src)
	seen := make(map[string]bool, len(calls))
	rels := make([]types.RelationshipRecord, 0, len(calls))
	for _, call := range calls {
		target := kotlinCallTarget(call, src)
		if target == "" || target == callerName {
			continue
		}
		if kotlinKeywordStop[target] {
			// Kotlin keywords / special identifiers that the parser
			// surfaces as call_expression heads but are not real call
			// targets. Mirrors how the Python extractor drops `self` /
			// `cls`. Issue #106.
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		// Line is 1-based: tree-sitter StartPoint().Row is 0-based.
		callLine := strconv.Itoa(int(call.StartPoint().Row) + 1)
		props := map[string]string{"line": callLine}
		// #4375 — stamp the cross-package qualifier when statically resolvable.
		if ctx != nil {
			if q := ctx.resolveKotlinQualifiedCall(call, src, localNames); q != nil &&
				q.leaf == target && len(q.pkgCandidates) > 0 {
				props["kotlin_call_pkg"] = strings.Join(q.pkgCandidates, ";")
				props["call_leaf"] = q.leaf
				if q.typ != "" {
					props["kotlin_call_type"] = q.typ
				}
			}
		}
		rels = append(rels, types.RelationshipRecord{
			ToID:       target,
			Kind:       "CALLS",
			Properties: props,
		})
	}
	return rels
}

// stampKotlinEnclosingType records the declaring type name on a member
// Operation entity (issue #4375) so the resolver's package-keyed member index
// can bind a cross-package `Type.method()` call. Idempotent and innermost-wins:
// a nested-class member already stamped by the inner walk is left untouched, so
// the outer class does not overwrite the correct (inner) enclosing type.
func stampKotlinEnclosingType(e *types.EntityRecord, typeName string) {
	if e == nil || typeName == "" {
		return
	}
	if e.Properties == nil {
		e.Properties = map[string]string{}
	}
	if _, ok := e.Properties["kotlin_enclosing_type"]; !ok {
		e.Properties["kotlin_enclosing_type"] = typeName
	}
}

// kotlinLocalValueNames collects the in-scope value names declared inside a
// function body — parameters are not reachable from the body node, so this
// gathers `val`/`var` property_declaration and variable_declaration names. A
// head segment matching one of these marks an INSTANCE receiver, which the
// cross-package qualifier resolver must skip (no false static-qualifier stamp).
func kotlinLocalValueNames(body *sitter.Node, src []byte) map[string]bool {
	names := map[string]bool{}
	for _, vd := range findAllNodes(body, "variable_declaration", "property_declaration") {
		for i := 0; i < int(vd.ChildCount()); i++ {
			ch := vd.Child(i)
			if ch.Type() == "simple_identifier" {
				names[string(src[ch.StartByte():ch.EndByte()])] = true
				break
			}
		}
	}
	return names
}

// kotlinCallTarget resolves the callee name from a call_expression node.
//
// Tree-sitter-kotlin shapes a call as `<expression> <call_suffix>` where
// `<call_suffix>` carries the parenthesized argument list (or trailing
// lambda) that distinguishes a real method invocation from a plain
// property/field reference. We require a `call_suffix` sibling before
// emitting any CALLS edge — without it, the node is not a true call
// site and the resolver cannot bind it. Issue #122.
//
// When the receiver expression is a `simple_identifier` (`foo()`) we
// return that identifier. When it is a `navigation_expression`
// (`a.b.c()`) the actual callee name is the rightmost `simple_identifier`
// of the trailing `navigation_suffix`, NOT the leftmost descendant — the
// previous implementation walked descendants via the stack-based
// findAllNodes (LIFO) which returned the receiver's root identifier
// (e.g. `chat` for `chat.lastMessages.add()`), producing the
// false-positive same-class field-access CALLS that dominated the
// ktor-samples bug-extractor rate. Issue #122.
func kotlinCallTarget(call *sitter.Node, src []byte) string {
	if !hasCallSuffix(call) {
		return ""
	}
	if call.ChildCount() == 0 {
		return ""
	}
	first := call.Child(0)
	switch first.Type() {
	case "simple_identifier":
		return string(src[first.StartByte():first.EndByte()])
	case "navigation_expression":
		// The callee name is the last `simple_identifier` of the
		// trailing navigation_suffix (`.method`).
		var lastSuffix *sitter.Node
		for i := 0; i < int(first.ChildCount()); i++ {
			ch := first.Child(i)
			if ch.Type() == "navigation_suffix" {
				lastSuffix = ch
			}
		}
		if lastSuffix == nil {
			return ""
		}
		for i := int(lastSuffix.ChildCount()) - 1; i >= 0; i-- {
			ch := lastSuffix.Child(i)
			if ch.Type() == "simple_identifier" {
				return string(src[ch.StartByte():ch.EndByte()])
			}
		}
	}
	return ""
}

// hasCallSuffix reports whether a call_expression node has a `call_suffix`
// child. Tree-sitter-kotlin requires this child for a real invocation —
// its presence (parentheses or trailing lambda) is what distinguishes a
// method call from a bare identifier or property reference. Issue #122.
func hasCallSuffix(call *sitter.Node) bool {
	for i := 0; i < int(call.ChildCount()); i++ {
		if call.Child(i).Type() == "call_suffix" {
			return true
		}
	}
	return false
}

// findAllNodes returns every descendant of root whose Type() is in kinds.
func findAllNodes(root *sitter.Node, kinds ...string) []*sitter.Node {
	if root == nil {
		return nil
	}
	set := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		set[k] = true
	}
	var out []*sitter.Node
	stack := []*sitter.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if set[n.Type()] {
			out = append(out, n)
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return out
}

// classDeclarationSubtype determines the subtype for a class_declaration node.
//
// tree-sitter-kotlin maps interface, enum class, and plain class all to
// class_declaration; we discriminate by inspecting direct children:
//
//   - [interface] child → "interface"
//   - [enum] child      → "enum"
//   - raw text contains "data class" → "data_class"
//   - otherwise          → "class"
//
// Issue #3275 — type-system CST extraction.
func classDeclarationSubtype(node *sitter.Node, src []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		switch node.Child(i).Type() {
		case "interface":
			return "interface"
		case "enum":
			return "enum"
		}
	}
	raw := string(src[node.StartByte():node.EndByte()])
	if strings.Contains(raw, "data class ") {
		return "data_class"
	}
	return "class"
}

// buildTypeAlias creates a SCOPE.Schema/type_alias entity for a type_alias node.
//
// tree-sitter-kotlin shape:
//
//	[type_alias] → [typealias] [type_identifier <name>] [=] <rhs>
//
// Mirrors the Go and Python extractors which emit SCOPE.Schema/type_alias for
// language-level type alias declarations (#3275 — type-system CST extraction).
func buildTypeAlias(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	name := firstChildOfType(node, file.Content, "type_identifier")
	if name == "" {
		return types.EntityRecord{}, false
	}
	return types.EntityRecord{
		Name:       name,
		Kind:       "SCOPE.Schema",
		Subtype:    "type_alias",
		SourceFile: file.Path,
		Language:   "kotlin",
		StartLine:  int(node.StartPoint().Row) + 1,
		EndLine:    int(node.EndPoint().Row) + 1,
	}, true
}

// buildComponent creates a Component entity for class/object declarations.
func buildComponent(node *sitter.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	// Kotlin grammar uses type_identifier for class/object names (no "name" field).
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		name = firstChildOfType(node, file.Content, "type_identifier")
	}
	if name == "" {
		return types.EntityRecord{}, false
	}

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Component",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "kotlin",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildClassSignature(node, file.Content, name),
		EnrichmentRequired: false,
	}, true
}

// buildImport creates a SCOPE.Component entity for an import_header node.
//
// The entity Name is the FULL dotted module path (e.g.
// "io.ktor.server.routing"), with the optional trailing `.*` wildcard
// suffix stripped. We intentionally do NOT split on '.' or use the first
// segment as the Name — the historical Kotlin ghost-entity hazard
// (`org`, `com`, `java` ghosts that broke parity verdict classification)
// came from splitting; mirroring the Python extractor's importRecord
// shape avoids it.
//
// One IMPORTS relationship is embedded on the entity:
//
//	FromID = file path  (the importing source file)
//	ToID   = full dotted module path (wildcard suffix stripped)
//	Kind   = "IMPORTS"
//
// The cross-file resolver and the external-synthesis pass both consume
// these edges to gate language-specific allowlists on a real import
// (e.g. Ktor server DSL HTTP-verb routing — `get("/x") { ... }`,
// `post(...)` — must only classify as external when the source file
// imports an `io.ktor.server.*` package, per the same precision model
// the Go chi-router gate uses, #131).
func buildImport(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	// import_header text shape: `import io.ktor.server.routing.*`
	// or `import io.ktor.server.routing.get`. Strip the leading
	// `import` keyword + trailing wildcard, comments, and the optional
	// `as <alias>` rename — we keep only the dotted module path so the
	// resolver can do prefix matches against allowlists.
	raw := strings.TrimSpace(string(file.Content[node.StartByte():node.EndByte()]))
	raw = strings.TrimPrefix(raw, "import ")
	raw = strings.TrimSpace(raw)
	// Drop trailing line comment (`import x.y // foo`).
	if i := strings.Index(raw, "//"); i >= 0 {
		raw = strings.TrimSpace(raw[:i])
	}
	// Drop `as <alias>` rename.
	if i := strings.Index(raw, " as "); i >= 0 {
		raw = strings.TrimSpace(raw[:i])
	}
	// Strip wildcard suffix; track presence for IMPORTS-rewrite (PR #670 analog).
	wildcard := strings.HasSuffix(raw, ".*")
	raw = strings.TrimSuffix(raw, ".*")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return types.EntityRecord{}, false
	}
	props := map[string]string{
		"source_module": raw,
	}
	if wildcard {
		props["wildcard"] = "1"
	} else if dot := strings.LastIndexByte(raw, '.'); dot >= 0 {
		// Imported leaf name (used by the IMPORTS-rewrite pass to build
		// the `ext:<root>:<leaf>` ToID for known-external packages).
		props["imported_name"] = raw[dot+1:]
	} else {
		props["imported_name"] = raw
	}
	return types.EntityRecord{
		Name:       raw,
		Kind:       "SCOPE.Component",
		Subtype:    "import",
		SourceFile: file.Path,
		Language:   "kotlin",
		Relationships: []types.RelationshipRecord{
			{
				FromID:     file.Path,
				ToID:       raw,
				Kind:       "IMPORTS",
				Properties: props,
			},
		},
	}, true
}

// buildOperation creates an Operation entity for function declarations.
func buildOperation(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	// Kotlin grammar uses simple_identifier for function names (no "name" field).
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		name = firstChildOfType(node, file.Content, "simple_identifier")
	}
	if name == "" {
		return types.EntityRecord{}, false
	}

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            "function",
		SourceFile:         file.Path,
		Language:           "kotlin",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildFunSignature(node, file.Content),
		EnrichmentRequired: false,
	}, true
}

// springStereotypes is the set of Spring annotations that promote a Kotlin
// class to a SCOPE.Service entity. Matches the Python indexer.
var springStereotypes = map[string]bool{
	"RestController": true,
	"Controller":     true,
	"Service":        true,
	"Component":      true,
	"Repository":     true,
}

// buildSpringService emits a SCOPE.Service entity for a class declaration
// that carries a Spring stereotype annotation. Returns (_, false) when no
// stereotype is found so the caller can skip the append.
//
// The returned entity shape matches the Python golden:
//
//	name           = class name
//	kind           = "SCOPE.Service"
//	qualified_name = "<source_file>::<class_name>"
//	provenance     = "@<StereotypeName>" (e.g. "@RestController")
//	source_type    = "class"
func buildSpringService(node *sitter.Node, file extractor.FileInput, className string) (types.EntityRecord, bool) {
	if className == "" {
		return types.EntityRecord{}, false
	}
	// Inspect the class declaration's raw text. We scan for an @Stereotype
	// token in the modifiers/annotations that precede the class body.
	raw := string(file.Content[node.StartByte():node.EndByte()])
	classIdx := strings.Index(raw, "class ")
	if classIdx < 0 {
		classIdx = len(raw)
	}
	header := raw[:classIdx]
	stereotype := findSpringStereotype(header)
	if stereotype == "" {
		return types.EntityRecord{}, false
	}
	return types.EntityRecord{
		Name:          className,
		QualifiedName: file.Path + "::" + className,
		Kind:          "SCOPE.Service",
		SourceFile:    file.Path,
		Language:      "kotlin",
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		Properties: map[string]string{
			"provenance":  "@" + stereotype,
			"source_type": "class",
		},
		EnrichmentRequired: false,
	}, true
}

// findSpringStereotype scans a class header (everything before the `class`
// keyword) for the first recognised Spring stereotype annotation token.
// Returns the bare stereotype name (e.g. "RestController") or "".
func findSpringStereotype(header string) string {
	i := 0
	for i < len(header) {
		if header[i] != '@' {
			i++
			continue
		}
		i++
		start := i
		for i < len(header) && (header[i] == '_' || (header[i] >= 'A' && header[i] <= 'Z') || (header[i] >= 'a' && header[i] <= 'z') || (header[i] >= '0' && header[i] <= '9')) {
			i++
		}
		name := header[start:i]
		if springStereotypes[name] {
			return name
		}
	}
	return ""
}

// buildProperty creates a SCOPE.Schema/field entity for a Kotlin
// property_declaration node. The name comes from the variable_declaration's
// first simple_identifier child. When parentType is non-empty the emitted
// name is "<parentType>.<name>", matching the qualified form used by
// BuildSchemaFieldStructuralRef so the resolver's byLocation index can bind
// the CONTAINS stub to the field entity.
//
// Issue #690 — closes the Kotlin analog of the Python field orphan gap (#689).
func buildProperty(node *sitter.Node, file extractor.FileInput, parentType string) (types.EntityRecord, bool) {
	// property_declaration structure:
	//   binding_pattern_kind (val|var)
	//   variable_declaration
	//     simple_identifier  ← property name
	//     ":" type?
	//   ["=" initializer]
	name := ""
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch.Type() != "variable_declaration" {
			continue
		}
		// First simple_identifier inside variable_declaration is the name.
		for j := 0; j < int(ch.ChildCount()); j++ {
			gc := ch.Child(j)
			if gc.Type() == "simple_identifier" {
				name = string(file.Content[gc.StartByte():gc.EndByte()])
				break
			}
		}
		break
	}
	if name == "" {
		return types.EntityRecord{}, false
	}
	emittedName := name
	if parentType != "" {
		emittedName = parentType + "." + name
	}
	return types.EntityRecord{
		Name:       emittedName,
		Kind:       "SCOPE.Schema",
		Subtype:    "field",
		SourceFile: file.Path,
		Language:   "kotlin",
		StartLine:  int(node.StartPoint().Row) + 1,
		EndLine:    int(node.EndPoint().Row) + 1,
	}, true
}

// emitPrimaryConstructorFields scans a class_declaration's primary_constructor
// for class_parameter nodes that carry a binding_pattern_kind (val or var).
// Each such parameter is a structural property of the class — not just a
// constructor argument — so we emit it as a SCOPE.Schema/field entity and
// append a CONTAINS edge on the parent class entity at classIdx.
//
// Issue #690 — data class pattern:
//
//	data class User(val id: Int, val name: String)
//
// Tree: class_declaration → primary_constructor → class_parameter+
//
//	class_parameter carries binding_pattern_kind (val|var) when the
//	parameter is a property; plain parameters have no such child.
func emitPrimaryConstructorFields(
	classNode *sitter.Node,
	file extractor.FileInput,
	className string,
	classIdx int,
	out *[]types.EntityRecord,
) {
	for i := 0; i < int(classNode.ChildCount()); i++ {
		ch := classNode.Child(i)
		if ch.Type() != "primary_constructor" {
			continue
		}
		for j := 0; j < int(ch.ChildCount()); j++ {
			param := ch.Child(j)
			if param.Type() != "class_parameter" {
				continue
			}
			// Only emit a field entity when the parameter has a
			// binding_pattern_kind child (val or var).
			hasBinding := false
			name := ""
			for k := 0; k < int(param.ChildCount()); k++ {
				gc := param.Child(k)
				switch gc.Type() {
				case "binding_pattern_kind":
					hasBinding = true
				case "simple_identifier":
					if name == "" {
						name = string(file.Content[gc.StartByte():gc.EndByte()])
					}
				}
			}
			if !hasBinding || name == "" {
				continue
			}
			emittedName := className + "." + name
			rec := types.EntityRecord{
				Name:       emittedName,
				Kind:       "SCOPE.Schema",
				Subtype:    "field",
				SourceFile: file.Path,
				Language:   "kotlin",
				StartLine:  int(param.StartPoint().Row) + 1,
				EndLine:    int(param.EndPoint().Row) + 1,
			}
			fieldIdx := len(*out)
			*out = append(*out, rec)
			_ = fieldIdx
			toID := extractor.BuildSchemaFieldStructuralRef("kotlin", file.Path, emittedName)
			(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
				types.RelationshipRecord{
					ToID: toID,
					Kind: "CONTAINS",
				})
		}
		break // only one primary_constructor per class
	}
}

// childFieldText extracts the text of a named child field.
func childFieldText(node *sitter.Node, field string, src []byte) string {
	child := node.ChildByFieldName(field)
	if child == nil {
		return ""
	}
	return string(src[child.StartByte():child.EndByte()])
}

// firstChildOfType returns the text of the first direct child with the given node type.
func firstChildOfType(node *sitter.Node, src []byte, nodeType string) string {
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch.Type() == nodeType {
			return string(src[ch.StartByte():ch.EndByte()])
		}
	}
	return ""
}

// buildFunSignature builds a function signature (up to body).
// Strips top-level annotations but keeps parameter annotations (e.g., @RequestBody).
// Python convention: "fun name(@ParamAnnotation param: Type): ReturnType".
func buildFunSignature(node *sitter.Node, src []byte) string {
	raw := string(src[node.StartByte():node.EndByte()])
	// Strip body block
	if idx := strings.Index(raw, " {"); idx >= 0 {
		raw = raw[:idx]
	}
	// Strip one-liner expression body
	if eqIdx := strings.Index(raw, " ="); eqIdx >= 0 {
		raw = raw[:eqIdx]
	}
	// Collapse newlines into spaces.
	raw = strings.Join(strings.Fields(raw), " ")
	// Strip only top-level annotations (before "fun" keyword).
	// Keep parameter annotations intact.
	if funIdx := strings.Index(raw, "fun "); funIdx >= 0 {
		prefix := raw[:funIdx]
		suffix := raw[funIdx:]
		prefix = stripKotlinAnnotations(prefix)
		raw = strings.TrimSpace(prefix + suffix)
	}
	return strings.TrimSpace(raw)
}

// buildClassSignature constructs a readable signature up to the class body.
// Strips annotations to match Python convention: "class Foo" or "data class Foo(...)".
func buildClassSignature(node *sitter.Node, src []byte, name string) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = raw[:idx]
	}
	// Collapse to single line.
	raw = strings.Join(strings.Fields(raw), " ")
	// Strip annotations.
	raw = stripKotlinAnnotations(raw)
	return strings.TrimSpace(raw)
}

// stripKotlinAnnotations removes @Annotation and @Annotation(...) tokens.
func stripKotlinAnnotations(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '@' {
			// Skip @Identifier
			i++
			for i < len(s) && (s[i] == '_' || (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z') || (s[i] >= '0' && s[i] <= '9')) {
				i++
			}
			// Skip optional (args)
			if i < len(s) && s[i] == '(' {
				depth := 1
				i++
				for i < len(s) && depth > 0 {
					switch s[i] {
					case '(':
						depth++
					case ')':
						depth--
					}
					i++
				}
			}
			// Skip trailing space.
			for i < len(s) && s[i] == ' ' {
				i++
			}
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}
