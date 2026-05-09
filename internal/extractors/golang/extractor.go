// Package golang implements the Go language extractor for archigraph.
//
// It extracts functions, methods (with receiver), structs, and interfaces from
// Go source files using the smacker/go-tree-sitter grammar. The extractor
// registers itself via init() and is auto-discovered by go:generate.
//
// Entity mapping:
//
//	function_declaration  → Kind="SCOPE.Operation",   Subtype="function"
//	method_declaration    → Kind="SCOPE.Operation",   Subtype="method", Metadata["receiver"]=ReceiverType
//	type_spec (struct)    → Kind="SCOPE.Component",   Subtype="struct"
//	type_spec (interface) → Kind="SCOPE.Component",   Subtype="interface"
//	type_spec (alias)     → Kind="SCOPE.Schema",      Subtype="type_alias"
//
// Relationships:
//
//	import_spec            → RelationshipRecord{Kind="IMPORTS"}       (File → Module)
//	call_expression        → RelationshipRecord{Kind="CALLS"}         (Function → Function)
//	method receiver        → RelationshipRecord{Kind="DEPENDS_ON"}    (Method → Component)
//	struct field type      → RelationshipRecord{Kind="DEPENDS_ON"}    (Component → Component)
//	interface satisfaction → RelationshipRecord{Kind="IMPLEMENTS"}    (Component → Component)
//
// All relationship target names use bare entity names (mirroring EntityRecord.Name)
// rule #5. Unknown/external call targets are emitted with bare function
// name rule #6. Malformed nodes are logged and skipped (rule: never
// abort the whole file because relationship extraction panics).
package golang

import (
	"context"
	"fmt"
	"log"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("go", &GoExtractor{})
}

// GoExtractor extracts Go language entities using tree-sitter.
type GoExtractor struct{}

// Language implements extractors.Extractor.
func (g *GoExtractor) Language() string { return "go" }

// Extract implements extractors.Extractor.
// It returns partial results if any individual extraction step fails — it
// never aborts the whole file on a single query failure.
func (g *GoExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor.go")
	ctx, span := tracer.Start(ctx, "extractor.go",
		trace.WithAttributes(
			attribute.String("file", file.Path),
		),
	)
	defer span.End()

	// Fast-path: empty or binary content.
	if len(file.Content) == 0 {
		span.SetAttributes(
			attribute.Int("entity_count", 0),
			attribute.Int("function_count", 0),
			attribute.Int("struct_count", 0),
		)
		return nil, nil
	}

	// If a pre-parsed tree was supplied, reuse it. Otherwise parse inline.
	tree := file.Tree
	if tree == nil {
		parser := sitter.NewParser()
		parser.SetLanguage(goGrammar())
		var err error
		tree, err = parser.ParseCtx(ctx, nil, file.Content)
		if err != nil {
			return nil, fmt.Errorf("golang extractor: parse failed: %w", err)
		}
	}

	root := tree.RootNode()

	var (
		records []types.EntityRecord
		funcs   int
		structs int
	)

	// ----------------------------------------------------------------
	// 1. Functions and methods
	// ----------------------------------------------------------------
	funcEntities, fCount := extractFunctions(root, file.Content, file.Path)
	records = append(records, funcEntities...)
	funcs = fCount

	// ----------------------------------------------------------------
	// 2. Structs and interfaces
	//    Returns typeIndex describing known schemas in the file so the
	//    post-pass can compute IMPLEMENTS edges.
	// ----------------------------------------------------------------
	typeEntities, sCount, typeIdx := extractTypes(root, file.Content, file.Path)
	records = append(records, typeEntities...)
	structs = sCount

	// ----------------------------------------------------------------
	// 3. Import relationships — emitted as standalone SCOPE.Component
	//    EntityRecord entries (one per import path). Not fanned out to
	// every function/type entity.
	// ----------------------------------------------------------------
	importRecords := extractImportEntities(root, file.Content, file.Path)
	records = append(records, importRecords...)

	// ----------------------------------------------------------------
	// 4. Interface satisfaction — intra-file IMPLEMENTS.
	//    Applied after both function and type extraction so method sets
	//    per receiver type are available. Post-processes records in place.
	// ----------------------------------------------------------------
	records = attachImplementsRelationships(records, typeIdx)

	// ----------------------------------------------------------------
	// 4b. Class CONTAINS — issue #145. For every struct/interface
	//     Component, append a CONTAINS edge per method whose receiver
	//     matches the Component name (struct), or per method declared
	//     in the interface body (interface). Target uses Format-A
	//     structural-ref so the resolver can disambiguate same-named
	//     methods declared on different receivers across files.
	// ----------------------------------------------------------------
	records = attachClassContains(records, file.Path)

	// ----------------------------------------------------------------
	// 5. Error-handling patterns — secondary pass.
	//    Emits one SCOPE.Pattern entity per `if err != nil { ... }`
	//    occurrence. Runs after the base extraction so a detection
	//    failure here cannot abort the primary entity output.
	// ----------------------------------------------------------------
	errorPatterns := extractErrorHandlingPatterns(root, file.Content, file.Path)
	records = append(records, errorPatterns...)

	// ----------------------------------------------------------------
	// 6. OTel span attribute relationship_count
	// and error_pattern_count.
	// ----------------------------------------------------------------
	relCount := 0
	for _, r := range records {
		relCount += len(r.Relationships)
	}

	span.SetAttributes(
		attribute.Int("entity_count", len(records)),
		attribute.Int("function_count", funcs),
		attribute.Int("struct_count", structs),
		attribute.Int("relationship_count", relCount),
		attribute.Int("error_pattern_count", len(errorPatterns)),
	)

	// Issue #90 — stamp Properties["language"]="go" so the resolver's
	// per-language dynamic-pattern dispatch picks the Go catalog.
	extractor.TagRelationshipsLanguage(records, "go")

	return records, nil
}

// ----------------------------------------------------------------
// Internal helpers
// ----------------------------------------------------------------

// nodeText returns the UTF-8 text of a node from source bytes.
func nodeText(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	start := node.StartByte()
	end := node.EndByte()
	if int(end) > len(src) {
		end = uint32(len(src))
	}
	return string(src[start:end])
}

// nodeLines returns (start_line, end_line) as 1-based line numbers.
func nodeLines(node *sitter.Node) (int, int) {
	return int(node.StartPoint().Row) + 1, int(node.EndPoint().Row) + 1
}

// findAll performs a depth-first search and returns all nodes of any of the
// specified types. Iterative to avoid stack overflow on large files.
func findAll(root *sitter.Node, types ...string) []*sitter.Node {
	typeSet := make(map[string]bool, len(types))
	for _, t := range types {
		typeSet[t] = true
	}

	var results []*sitter.Node
	stack := []*sitter.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if typeSet[n.Type()] {
			results = append(results, n)
		}
		count := int(n.ChildCount())
		for i := 0; i < count; i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return results
}

// countDecisions counts cyclomatic complexity branch points in a subtree.
var decisionTypes = map[string]bool{
	"if_statement":                true,
	"else_clause":                 true,
	"for_statement":               true,
	"type_switch_statement":       true,
	"expression_switch_statement": true,
	"select_statement":            true,
	"comm_clause":                 true,
	"case":                        true,
}

func countDecisions(body *sitter.Node, src []byte) int {
	if body == nil {
		return 0
	}
	count := 0
	stack := []*sitter.Node{body}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		t := n.Type()
		if decisionTypes[t] {
			count++
		} else if t == "binary_expression" {
			// count && and || as extra branches
			childCount := int(n.ChildCount())
			for i := 0; i < childCount; i++ {
				child := n.Child(i)
				ct := child.Type()
				if ct == "&&" || ct == "||" {
					count++
				}
			}
		}
		childCount := int(n.ChildCount())
		for i := 0; i < childCount; i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return count
}

// hasExternalCalls reports whether the body uses any selector_expression
// whose operand is an import package name.
func hasExternalCalls(body *sitter.Node, importStems map[string]bool, src []byte) bool {
	if body == nil {
		return false
	}
	for _, sel := range findAll(body, "selector_expression") {
		operand := sel.ChildByFieldName("operand")
		if operand == nil {
			continue
		}
		pkg := nodeText(operand, src)
		if importStems[pkg] {
			return true
		}
	}
	return false
}

// collectImportStems returns a set of last-path-segment names from all
// imports in the file (e.g. "net/http" → "http").
func collectImportStems(root *sitter.Node, src []byte) map[string]bool {
	stems := make(map[string]bool)
	for _, spec := range findAll(root, "import_spec") {
		count := int(spec.ChildCount())
		for i := 0; i < count; i++ {
			child := spec.Child(i)
			if child.Type() == "interpreted_string_literal" {
				raw := strings.Trim(nodeText(child, src), `"`)
				parts := strings.Split(raw, "/")
				if len(parts) > 0 {
					stems[parts[len(parts)-1]] = true
				}
			}
		}
	}
	return stems
}

// receiverParamName extracts the bound parameter name from a method's receiver
// list. For `(mx *Mux) Foo()` it returns "mx"; for `(*Mux) Foo()` (anonymous
// receiver — legal in Go but rare) it returns "". Used by issue #148's same-
// package method-dispatch resolver path: when a call expression's operand
// matches this name, the call is resolvable via `<package>.<receiver_type>.<method>`.
func receiverParamName(recv *sitter.Node, src []byte) string {
	if recv == nil {
		return ""
	}
	for _, paramDecl := range findAll(recv, "parameter_declaration") {
		count := int(paramDecl.ChildCount())
		// The parameter name is the first identifier child appearing
		// BEFORE the type node (pointer_type / type_identifier / generic_type).
		for i := 0; i < count; i++ {
			child := paramDecl.Child(i)
			if child.Type() == "identifier" {
				return nodeText(child, src)
			}
			// Hit the type before any identifier → anonymous receiver.
			switch child.Type() {
			case "pointer_type", "type_identifier", "generic_type", "qualified_type":
				return ""
			}
		}
	}
	return ""
}

// collectParamTypes returns a (paramName -> typeLiteral) map built from a
// function/method `parameters` parameter_list AST node. The type literal is
// canonicalised by stripping a single leading `*` so `*http.Request`
// becomes `http.Request` — pointer-vs-value distinctions don't change the
// stdlib-interface methods exposed and folding here lets the synth lookup
// table use a single key per package type. Returns nil for a nil node or a
// node that contains no named parameters (variadic/anonymous-only signatures).
//
// Issue #364: feeds extractCallRelationships so calls like `w.Write(...)` on
// a parameter `w http.ResponseWriter` get a `receiver_type` stamp that the
// synth pass can route to ext:net/http.
func collectParamTypes(params *sitter.Node, src []byte) map[string]string {
	if params == nil {
		return nil
	}
	out := map[string]string{}
	for _, paramDecl := range findAll(params, "parameter_declaration") {
		// Walk children: collect leading identifier(s), then the first
		// non-identifier child is the type. Tree-sitter Go grammar emits
		// parameter_declaration as: identifier (',' identifier)* type.
		count := int(paramDecl.ChildCount())
		var names []string
		typeText := ""
		for i := 0; i < count; i++ {
			child := paramDecl.Child(i)
			t := child.Type()
			if t == "identifier" {
				names = append(names, nodeText(child, src))
				continue
			}
			if t == "," {
				continue
			}
			// First non-identifier, non-comma child is the type node.
			typeText = strings.TrimSpace(nodeText(child, src))
			break
		}
		if typeText == "" || len(names) == 0 {
			continue
		}
		canonical := strings.TrimPrefix(typeText, "*")
		// Strip type-parameter list ("[T]") so generic types collapse.
		if idx := strings.IndexByte(canonical, '['); idx >= 0 {
			canonical = canonical[:idx]
		}
		canonical = strings.TrimSpace(canonical)
		if canonical == "" {
			continue
		}
		for _, n := range names {
			if n == "" || n == "_" {
				continue
			}
			out[n] = canonical
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mergeVarTypes folds a body-derived (varName -> type) map into a param-
// derived map, preferring the outer (param) declaration on a name conflict
// — Go's lexical scoping rule says outer params shadow body short-var
// decls of the same name only inside their own scope; reverse holds in
// the body. The extractor doesn't model scopes, so on a collision it
// MUST drop the binding rather than emit a wrong receiver_type stamp.
// Returns nil only when both inputs are nil/empty.
func mergeVarTypes(outer, inner map[string]string) map[string]string {
	if len(outer) == 0 && len(inner) == 0 {
		return nil
	}
	out := make(map[string]string, len(outer)+len(inner))
	for k, v := range outer {
		out[k] = v
	}
	for k, v := range inner {
		if existing, ok := out[k]; ok && existing != v {
			// Same identifier declared with different types in two scopes
			// (closure shadow, nested block, etc.). Drop the binding so
			// neither call site gets a false stamp.
			delete(out, k)
			continue
		}
		// Only keep inner bindings that don't conflict with an outer one
		// of a different type. Identical-type duplicates are harmless.
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// collectBodyVarTypes walks a function/method body and returns a
// (varName -> typeLiteral) map for short_var_declaration and
// var_declaration nodes whose RHS has a recognisable static type. Names
// declared more than once with different types are dropped (ambiguous).
//
// Recognised RHS shapes (issue #364):
//
//	x := T{...}              composite_literal whose type child is a
//	                         type_identifier or qualified_type
//	x := &T{...}             unary_expression (`&`) wrapping the above
//	var x T                  var_declaration / var_spec with explicit type
//	var x = T{...}           var_declaration with composite_literal RHS
//	x := pkg.Func()          call_expression where the function is a
//	                         qualified_type-style selector recognised as
//	                         a known stdlib/framework constructor (small
//	                         allowlist: chi.NewRouter, bytes.NewBuffer,
//	                         strings.NewReader, ...)
//
// Pointer types are stripped (`*Mux` → `Mux`) and generic type parameter
// lists (`[T]`) are dropped so the synth lookup table can use a single
// key per package type.
func collectBodyVarTypes(body *sitter.Node, src []byte) map[string]string {
	if body == nil {
		return nil
	}
	out := map[string]string{}
	ambiguous := map[string]bool{}
	record := func(name, typ string) {
		if name == "" || name == "_" || typ == "" {
			return
		}
		canonical := canonicalTypeLiteral(typ)
		if canonical == "" {
			return
		}
		if ambiguous[name] {
			return
		}
		if existing, ok := out[name]; ok && existing != canonical {
			delete(out, name)
			ambiguous[name] = true
			return
		}
		out[name] = canonical
	}

	// Closure-param tracking lives in extractCallRelationships' scoped
	// walker (issue #364) — collecting them here would conflate scopes
	// and force a drop on common shadowing patterns (e.g. an outer
	// `r := chi.NewRouter()` shadowed by a closure `r *http.Request`).

	// short_var_declaration: `x := <expr>` (single LHS, single RHS only;
	// multi-LHS forms like `a, b := f()` are skipped because pairing each
	// name with the right RHS requires knowing the call's return tuple).
	for _, decl := range findAll(body, "short_var_declaration") {
		left := decl.ChildByFieldName("left")
		right := decl.ChildByFieldName("right")
		if left == nil || right == nil {
			continue
		}
		// Only handle the "one-name = one-expr" case.
		if !singleChildOfType(left, "identifier") || !singleNamedChild(right) {
			continue
		}
		name := nodeText(firstChildOfType(left, "identifier"), src)
		expr := firstNamedChild(right)
		if typ := typeOfExpression(expr, src); typ != "" {
			record(name, typ)
		}
	}

	// var_declaration: `var x T` or `var x = <expr>` or `var x T = <expr>`.
	for _, decl := range findAll(body, "var_declaration") {
		for _, spec := range findAll(decl, "var_spec") {
			// var_spec has a `name` field (identifier_list) and either a
			// `type` field (declared type) or a `value` field (init expr).
			nameNode := spec.ChildByFieldName("name")
			typeNode := spec.ChildByFieldName("type")
			valueNode := spec.ChildByFieldName("value")
			var names []string
			if nameNode != nil {
				if nameNode.Type() == "identifier" {
					names = []string{nodeText(nameNode, src)}
				} else {
					for i := 0; i < int(nameNode.ChildCount()); i++ {
						ch := nameNode.Child(i)
						if ch.Type() == "identifier" {
							names = append(names, nodeText(ch, src))
						}
					}
				}
			}
			if len(names) != 1 {
				continue
			}
			typ := ""
			if typeNode != nil {
				typ = nodeText(typeNode, src)
			} else if valueNode != nil {
				typ = typeOfExpression(valueNode, src)
			}
			if typ != "" {
				record(names[0], typ)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// typeOfExpression returns a textual type representation for an expression
// AST node when it's recognisable as a static type, or "" otherwise. Used
// by collectBodyVarTypes to type short/var declarations.
func typeOfExpression(expr *sitter.Node, src []byte) string {
	if expr == nil {
		return ""
	}
	switch expr.Type() {
	case "composite_literal":
		// composite_literal has a `type` field — type_identifier (`Foo{}`),
		// qualified_type (`pkg.Foo{}`), or pointer_type / array_type / etc.
		if t := expr.ChildByFieldName("type"); t != nil {
			return nodeText(t, src)
		}
	case "unary_expression":
		// `&Foo{}` or `&pkg.Foo{}` — drill into the operand.
		if op := expr.ChildByFieldName("operand"); op != nil {
			return typeOfExpression(op, src)
		}
	case "type_assertion_expression":
		if t := expr.ChildByFieldName("type"); t != nil {
			return nodeText(t, src)
		}
	case "call_expression":
		// Recognise a small set of stdlib / well-known-framework constructors
		// that return a value of a predictable type. Two shapes are matched:
		//
		//   `<pkg>.<Func>(...)`  — selector_expression: keyed on the dotted
		//                          form (e.g. `chi.NewRouter` → `chi.Mux`).
		//   `<Func>(...)`        — identifier: a same-package call that
		//                          returns the package's primary type. The
		//                          extractor doesn't know the package name
		//                          so the bare-name table maps to a bare
		//                          receiver type (e.g. `NewRouter` → `Mux`),
		//                          which the resolver's same-package member
		//                          lookup picks up directly.
		fn := expr.ChildByFieldName("function")
		if fn != nil {
			switch fn.Type() {
			case "selector_expression":
				operand := fn.ChildByFieldName("operand")
				field := fn.ChildByFieldName("field")
				if operand != nil && operand.Type() == "identifier" && field != nil {
					key := nodeText(operand, src) + "." + nodeText(field, src)
					if t, ok := goConstructorReturnTypes[key]; ok {
						return t
					}
				}
			case "identifier":
				if t, ok := goSamePackageConstructorReturnTypes[nodeText(fn, src)]; ok {
					return t
				}
			}
		}
	}
	return ""
}

// goSamePackageConstructorReturnTypes maps bare-name constructor calls to
// the receiver type they return when the call is unqualified. Unqualified
// calls are in-package (`m := NewRouter()` inside the chi package, where
// `NewRouter` resolves to `chi.NewRouter`). The receiver type is stored
// as a bare type name (no package prefix) so the resolver's same-package
// member lookup picks it up directly without the qualifier-strip retry.
//
// Issue #364: chi internal tests (mux_test.go) drive 30+ unresolved Get
// calls of this shape; without same-package constructor tracking the
// receiver_type stamp is missing and the resolver can't bind the call.
//
// Conservative selection rule (lesson #94): include only constructor names
// extremely unlikely to be redefined as a non-constructor user function.
// `NewRouter`/`NewMux`/`NewServer` are PascalCase factories that return a
// well-known type in their primary package; any user package redefining
// them with a different return type is a vanishingly rare false positive.
var goSamePackageConstructorReturnTypes = map[string]string{
	"NewRouter": "Mux",
	"NewMux":    "Mux",
}

// goConstructorReturnTypes maps `<pkg>.<Func>` calls to the type their
// return value carries when used as the RHS of a short-var declaration.
// Values use the canonical receiver_type form (no leading `*`, generic
// parameter lists dropped) so the synth lookup can match without further
// normalisation. Issue #364: covers the highest-volume Go patterns where
// a short-var-declared identifier is later used as a method-dispatch
// operand (`r := chi.NewRouter(); r.Get(...)` etc.).
var goConstructorReturnTypes = map[string]string{
	// chi router
	"chi.NewRouter": "chi.Mux",
	"chi.NewMux":    "chi.Mux",
	// gin
	"gin.Default": "gin.Engine",
	"gin.New":     "gin.Engine",
	// echo
	"echo.New": "echo.Echo",
	// net/http
	"http.NewRequest":            "http.Request",
	"http.NewRequestWithContext": "http.Request",
	"http.NewServeMux":           "http.ServeMux",
	// bytes / strings / bufio
	"bytes.NewBuffer":     "bytes.Buffer",
	"bytes.NewReader":     "bytes.Reader",
	"strings.NewReader":   "strings.Reader",
	"strings.NewReplacer": "strings.Replacer",
	"bufio.NewReader":     "bufio.Reader",
	"bufio.NewWriter":     "bufio.Writer",
	"bufio.NewScanner":    "bufio.Scanner",
	// context
	"context.Background": "context.Context",
	"context.TODO":       "context.Context",
	// sync (rarely literal-constructed; included for completeness)
	"sync.NewCond": "sync.Cond",
	// errors
	"errors.New": "error",
	"fmt.Errorf": "error",
}

// singleChildOfType reports whether n has exactly one named child, which
// is of the requested type. Helper for collectBodyVarTypes.
func singleChildOfType(n *sitter.Node, typ string) bool {
	if n == nil {
		return false
	}
	if n.NamedChildCount() != 1 {
		return false
	}
	c := n.NamedChild(0)
	return c != nil && c.Type() == typ
}

// singleNamedChild reports whether n has exactly one named child.
func singleNamedChild(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	return n.NamedChildCount() == 1
}

// firstChildOfType returns the first child of n with the given type, or
// nil. Helper for collectBodyVarTypes.
func firstChildOfType(n *sitter.Node, typ string) *sitter.Node {
	if n == nil {
		return nil
	}
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		c := n.Child(i)
		if c.Type() == typ {
			return c
		}
	}
	return nil
}

// firstNamedChild returns the first named child of n, or nil.
func firstNamedChild(n *sitter.Node) *sitter.Node {
	if n == nil || n.NamedChildCount() == 0 {
		return nil
	}
	return n.NamedChild(0)
}

// canonicalTypeLiteral reduces a type literal to the form used as a key
// in goStdlibInterfaceMethods: leading `*` stripped, generic type
// parameter lists dropped. Returns "" for empty input.
func canonicalTypeLiteral(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return ""
	}
	t = strings.TrimPrefix(t, "*")
	if i := strings.IndexByte(t, '['); i >= 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}

// receiverTypeName extracts the base type name from a receiver parameter list.
// The receiver AST is: parameter_list → parameter_declaration → [identifier, pointer_type|type_identifier|generic_type]
// e.g. "(s *UserStore)" → "UserStore", "(u User)" → "User",
// "(s *Set[T])" → "Set", "(c Cache[K, V])" → "Cache".
//
// Issue #79: generic methods on parameterised types must collapse type
// parameter lists. Without stripping `[T]` / `[K, V]`, the qualified
// Name retains the type parameter sub-tree, so `(s *Set[T]) Add(...)`
// emits Name="Set[T].Add" — and resolve.Index.byMember (which splits on
// the first '.') treats every instantiation as a distinct receiver.
// We unwrap generic_type nodes to their first child (the bare type
// identifier) so all instantiations share one canonical entity.
func receiverTypeName(recv *sitter.Node, src []byte) string {
	if recv == nil {
		return ""
	}
	// Walk parameter_list → parameter_declaration → find type node.
	for _, paramDecl := range findAll(recv, "parameter_declaration") {
		count := int(paramDecl.ChildCount())
		for i := 0; i < count; i++ {
			child := paramDecl.Child(i)
			switch child.Type() {
			case "type_identifier":
				return nodeText(child, src)
			case "generic_type":
				// generic_type's first named child is the bare type
				// identifier; subsequent children carry type_arguments
				// like "[T]" — discard them (issue #79).
				if name := unwrapGenericType(child, src); name != "" {
					return name
				}
			case "pointer_type":
				// pointer_type child is the type_identifier or generic_type.
				for j := 0; j < int(child.ChildCount()); j++ {
					gc := child.Child(j)
					if gc.Type() == "type_identifier" {
						return nodeText(gc, src)
					}
					if gc.Type() == "generic_type" {
						if name := unwrapGenericType(gc, src); name != "" {
							return name
						}
					}
				}
				// fallback: strip leading * and any trailing type
				// parameter list "[...]" so generic pointer receivers
				// like "*Set[T]" still collapse to "Set".
				t := strings.TrimPrefix(nodeText(child, src), "*")
				if idx := strings.IndexByte(t, '['); idx >= 0 {
					t = t[:idx]
				}
				return t
			case "qualified_type":
				return nodeText(child, src)
			}
		}
	}
	return ""
}

// unwrapGenericType returns the bare type identifier of a generic_type
// AST node, stripping the type parameter list. Returns "" if no
// type_identifier child is found.
func unwrapGenericType(node *sitter.Node, src []byte) string {
	for j := 0; j < int(node.ChildCount()); j++ {
		gc := node.Child(j)
		if gc.Type() == "type_identifier" {
			return nodeText(gc, src)
		}
	}
	return ""
}

// extractFunctions extracts function_declaration and method_declaration nodes.
// Returns entity records and the count of function-type entities.
//
// each function/method entity carries a slice of CALLS
// RelationshipRecord values extracted from call_expression nodes inside its
// body. Methods additionally carry a DEPENDS_ON edge to the receiver type
// so the graph can traverse from method back to its owning schema.
func extractFunctions(root *sitter.Node, src []byte, filePath string) ([]types.EntityRecord, int) {
	importStems := collectImportStems(root, src)
	nodes := findAll(root, "function_declaration", "method_declaration")

	var records []types.EntityRecord
	funcCount := 0

	for _, n := range nodes {
		var (
			entitySubtype string
			name          string
			signature     string
			receiverType  string
		)

		nameNode := n.ChildByFieldName("name")
		paramsNode := n.ChildByFieldName("parameters")
		resultNode := n.ChildByFieldName("result")
		bodyNode := n.ChildByFieldName("body")

		nameText := ""
		if nameNode != nil {
			nameText = nodeText(nameNode, src)
		} else {
			logWarning("function/method node at line %d has no name child — skipping", n.StartPoint().Row+1)
			continue
		}
		paramsText := "()"
		if paramsNode != nil {
			paramsText = nodeText(paramsNode, src)
		}
		resultText := ""
		if resultNode != nil {
			resultText = " " + nodeText(resultNode, src)
		}

		recvVarName := ""
		if n.Type() == "method_declaration" {
			entitySubtype = "method"
			recvNode := n.ChildByFieldName("receiver")
			receiverType = receiverTypeName(recvNode, src)
			recvVarName = receiverParamName(recvNode, src)
			recvText := ""
			if recvNode != nil {
				recvText = nodeText(recvNode, src)
			}
			signature = strings.TrimSpace(fmt.Sprintf("func %s %s%s%s", recvText, nameText, paramsText, resultText))
			// issue #66: methods are emitted with Name="<Receiver>.<method>"
			// so two structs in the same file declaring a same-named method
			// produce distinct entity IDs (ComputeID hashes Source+Kind+Name).
			// receiverTypeName already strips the pointer/value distinction,
			// so `(*UserStore).Save` and `(UserStore).Save` collapse to the
			// canonical `UserStore.Save`. The dotted form is the same
			// encoding used for Format B references and is indexed natively
			// by resolve.Index.byMember (splits Name on the first '.').
			if receiverType != "" {
				name = receiverType + "." + nameText
			} else {
				name = nameText
			}
		} else {
			entitySubtype = "function"
			signature = strings.TrimSpace(fmt.Sprintf("func %s%s%s", nameText, paramsText, resultText))
			name = nameText
			funcCount++
		}

		startLine, endLine := nodeLines(n)

		bodyOrNode := n
		if bodyNode != nil {
			bodyOrNode = bodyNode
		}

		hasConditionals := len(findAll(bodyOrNode, "if_statement", "expression_switch_statement", "type_switch_statement")) > 0
		hasExt := hasExternalCalls(bodyOrNode, importStems, src)
		cyclo := 1 + countDecisions(bodyOrNode, src)

		metadata := map[string]interface{}{
			"subtype":               entitySubtype,
			"has_conditionals":      hasConditionals,
			"has_external_calls":    hasExt,
			"cyclomatic_complexity": cyclo,
		}
		if receiverType != "" {
			metadata["receiver"] = receiverType
		}

		// QualifiedName is intentionally left empty for Go entities (issue
		// #80). Since issue #66, Name already carries the Receiver.method
		// dotted form for methods, so a separate QualifiedName field would
		// be redundant. Other languages (Python, Razor, HCL, Markdown,
		// Kotlin, YAML, OpenAPI) keep QualifiedName for forms that differ
		// from Name (e.g. package paths, file::class joins).

		// CALLS relationships — one per call_expression in the body.
		// Unknown/external targets are emitted with the bare function name
		// behaviour rule #6. Deduplicated by (source, target).
		// Self-recursion detection compares against the bare identifier
		// (issue #66): a method `(s *Foo) bar()` calling `bar()` should
		// still suppress the self-edge even though the entity Name is now
		// "Foo.bar".
		// Issue #364 — build a (varName -> typeLiteral) map covering both
		// the outer function/method parameter list AND short/var declarations
		// inside the body (`r := chi.NewRouter()` / `var b bytes.Buffer`).
		// Calls of the form `<varName>.<method>(...)` can then be stamped
		// with the var's static type (e.g. `*http.Request`,
		// `http.ResponseWriter`, `*chi.Mux`). The resolver + synth pass use
		// this stamp to route stdlib-interface dispatch like `w.Write(...)`
		// to ext:net/http rather than leaving it as bug-extractor. Names
		// declared in shadowing scopes (closures, nested blocks) are tracked
		// as "ambiguous" — collectVarTypes drops them rather than emitting a
		// guess, preserving the safer-bias rule from #94.
		paramTypes := collectParamTypes(paramsNode, src)
		// Body-derived var types do NOT include closure-param shadowing — the
		// per-call walker below maintains its own scope stack to disambiguate.
		paramTypes = mergeVarTypes(paramTypes, collectBodyVarTypes(bodyOrNode, src))
		relationships := extractCallRelationships(bodyOrNode, src, nameText, recvVarName, receiverType, paramTypes)
		// Rewrite FromID on each CALLS edge to the qualified Name so the
		// edge source matches the entity ID downstream.
		for i := range relationships {
			relationships[i].FromID = name
		}

		// DEPENDS_ON edge from each method to its receiver type.
		// Enables graph traversal from method → owning schema without
		// needing qualified-name joins downstream.
		if receiverType != "" {
			relationships = append(relationships, types.RelationshipRecord{
				FromID: name,
				ToID:   receiverType,
				Kind:   "DEPENDS_ON",
			})
		}

		rec := types.EntityRecord{
			Name:               name,
			QualifiedName:      "",
			Kind:               "SCOPE.Operation",
			Subtype:            entitySubtype,
			SourceFile:         filePath,
			StartLine:          startLine,
			EndLine:            endLine,
			Language:           "go",
			Signature:          signature,
			QualityScore:       1.0,
			Metadata:           metadata,
			Relationships:      relationships,
			EnrichmentRequired: false,
		}
		records = append(records, rec)
	}

	return records, funcCount
}

// extractCallRelationships returns one CALLS RelationshipRecord per unique
// call_expression found in body. The target name is computed from the function
// child of the call_expression node:
//
//	identifier               → bare name     (e.g., "Println")
//	selector_expression      → field name    (e.g., "fmt.Println" → "Println")
//	parenthesized_expression → inner target  (best-effort)
//
// The source (FromID) is always the caller's bare Name field. Deduplication
// happens per (source, target) pair so multiple call sites to the same target
// from within the same function collapse to a single edge — this matches
// Python parser semantics.
//
// rule #6: unknown/external callees are emitted with the bare name
// rather than being dropped.
//
// Issue #148: when callerName belongs to a method, recvVarName is the
// receiver parameter's bound identifier (e.g. "mx" for `(mx *Mux) Foo()`)
// and recvType is the receiver type (e.g. "Mux"). For each call expression
// shaped like `<recvVarName>.<method>(...)`, the resulting CALLS edge is
// stamped with Properties["receiver_type"]=recvType so the resolver can
// bind the bare-name target to the same-package `<recvType>.<method>`
// entity. Calls on other selector operands (e.g. `other.foo()`, package-
// qualified calls) are NOT stamped — the heuristic is intentionally
// conservative to avoid false same-package binds (Refs #94 lesson).
func extractCallRelationships(body *sitter.Node, src []byte, callerName, recvVarName, recvType string, paramTypes map[string]string) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}

	seen := make(map[string]bool)
	var rels []types.RelationshipRecord

	// Recursive walker (issue #364): maintains a scope stack of (varName ->
	// type) maps so a closure parameter `r *http.Request` inside an outer
	// scope where `r := chi.NewRouter()` shadows correctly. The closest
	// enclosing scope wins per Go's lexical rules. The stack is allocated
	// once and pushed/popped on entry/exit of func_literal nodes; non-
	// closure block scopes are not pushed because Go doesn't permit
	// re-declaring parameters at block scope without `:=`, and short-var
	// re-declarations at block scope are a rare edge case the linter
	// catches independently.
	scopes := []map[string]string{paramTypes}
	lookup := func(name string) string {
		if name == "" {
			return ""
		}
		// Walk inner-most scope first.
		for i := len(scopes) - 1; i >= 0; i-- {
			if v, ok := scopes[i][name]; ok {
				return v
			}
		}
		return ""
	}

	var visit func(n *sitter.Node)
	visit = func(n *sitter.Node) {
		if n == nil {
			return
		}
		t := n.Type()
		// Closure boundary — push the closure's own param map. Body-derived
		// short-var decls inside the closure are NOT collected here for
		// performance (they would require another scoped pass); the highest-
		// volume win is closure params shadowing outer short-var decls.
		pushed := false
		if t == "func_literal" {
			params := n.ChildByFieldName("parameters")
			closureParams := collectParamTypes(params, src)
			if closureParams != nil {
				scopes = append(scopes, closureParams)
				pushed = true
			}
		}
		if t == "call_expression" {
			target, isSelfReceiver, operand := callExpressionTargetWithOperand(n, src, recvVarName)
			if target != "" && target != callerName && !seen[target] {
				seen[target] = true
				rec := types.RelationshipRecord{
					FromID: callerName,
					ToID:   target,
					Kind:   "CALLS",
				}
				switch {
				case isSelfReceiver && recvType != "":
					rec.Properties = map[string]string{"receiver_type": recvType}
				case operand != "":
					if ty := lookup(operand); ty != "" {
						rec.Properties = map[string]string{"receiver_type": ty}
					}
				}
				rels = append(rels, rec)
			}
		}
		// Recurse into all named children.
		count := int(n.NamedChildCount())
		for i := 0; i < count; i++ {
			visit(n.NamedChild(i))
		}
		if pushed {
			scopes = scopes[:len(scopes)-1]
		}
	}
	visit(body)
	return rels
}

// callExpressionTarget resolves the callee name from a call_expression node.
// Returns the bare function name, stripping any qualifying package or receiver
// prefix. Returns "" if the call node has no resolvable function child
// (e.g., higher-order call on a literal expression like `f()()`).
func callExpressionTarget(call *sitter.Node, src []byte) string {
	target, _, _ := callExpressionTargetWithOperand(call, src, "")
	return target
}

// callExpressionTargetWithOperand is callExpressionTarget plus a same-receiver
// flag (issue #148) and the operand identifier text (issue #364). When
// recvVarName is non-empty and the call is a selector_expression whose operand
// is the identifier recvVarName, the second return is true — signalling the
// call dispatches to a method on the enclosing method's own receiver.
//
// The third return is the operand identifier text when the call is a
// selector_expression whose operand is a bare identifier (e.g. `w` in
// `w.Write(...)` or `r` in `r.Method`); empty otherwise. Used by
// extractCallRelationships to look up the operand's static type in the
// caller's parameter map and stamp `receiver_type` for stdlib-interface
// dispatch.
func callExpressionTargetWithOperand(call *sitter.Node, src []byte, recvVarName string) (string, bool, string) {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return "", false, ""
	}
	switch fn.Type() {
	case "identifier":
		return nodeText(fn, src), false, ""
	case "selector_expression":
		field := fn.ChildByFieldName("field")
		if field == nil {
			return "", false, ""
		}
		name := nodeText(field, src)
		isSelf := false
		operand := ""
		if op := fn.ChildByFieldName("operand"); op != nil && op.Type() == "identifier" {
			operand = nodeText(op, src)
			if recvVarName != "" && operand == recvVarName {
				isSelf = true
			}
		}
		return name, isSelf, operand
	case "parenthesized_expression":
		// Rare: ((some.Expr))() — drill in one level.
		for i := 0; i < int(fn.ChildCount()); i++ {
			ch := fn.Child(i)
			if ch.Type() == "identifier" {
				return nodeText(ch, src), false, ""
			}
			if ch.Type() == "selector_expression" {
				if f := ch.ChildByFieldName("field"); f != nil {
					return nodeText(f, src), false, ""
				}
			}
		}
	}
	return "", false, ""
}

// typeIndex captures the set of schema entities and their method sets
// needed for intra-file IMPLEMENTS resolution.
type typeIndex struct {
	// structs is the set of struct entity names declared in the file.
	structs map[string]bool
	// interfaces maps interface entity name → set of method names declared.
	interfaces map[string]map[string]bool
	// methodsByReceiver maps receiver type name → set of method names declared.
	// Populated by extractImplementsRelationships from the collected function
	// records, not by extractTypes itself.
	methodsByReceiver map[string]map[string]bool
}

func newTypeIndex() *typeIndex {
	return &typeIndex{
		structs:           make(map[string]bool),
		interfaces:        make(map[string]map[string]bool),
		methodsByReceiver: make(map[string]map[string]bool),
	}
}

// extractTypes extracts struct and interface type declarations.
// Returns entity records, the count of struct-type entities, and a typeIndex
// describing all schemas and their method sets so later passes can compute
// DEPENDS_ON (field types) and IMPLEMENTS (interface satisfaction) edges.
func extractTypes(root *sitter.Node, src []byte, filePath string) ([]types.EntityRecord, int, *typeIndex) {
	structCount := 0
	var records []types.EntityRecord
	idx := newTypeIndex()

	// Pre-pass: collect the set of all type names declared in the file so
	// DEPENDS_ON edges can be restricted to intra-file type references
	// (avoids emitting edges to primitives or unresolved identifiers).
	knownTypeNames := collectDeclaredTypeNames(root, src)

	for _, typeDecl := range findAll(root, "type_declaration") {
		count := int(typeDecl.ChildCount())
		for i := 0; i < count; i++ {
			typeSpec := typeDecl.Child(i)
			if typeSpec.Type() != "type_spec" {
				continue
			}

			nameNode := typeSpec.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			name := nodeText(nameNode, src)

			// Determine entity type: look for struct_type or interface_type child.
			var entitySubtype string
			var typeBody *sitter.Node

			specCount := int(typeSpec.ChildCount())
			for j := 0; j < specCount; j++ {
				child := typeSpec.Child(j)
				switch child.Type() {
				case "struct_type":
					entitySubtype = "struct"
					typeBody = child
				case "interface_type":
					entitySubtype = "interface"
					typeBody = child
				}
				if entitySubtype != "" {
					break
				}
			}
			// If not struct or interface, check for type aliases/definitions
			// (e.g., `type contextKey int`, `type myString string`).
			// Emit as SCOPE.Schema to capture unexported type declarations.
			if entitySubtype == "" {
				// Check for any type_identifier or qualified_type child
				// that indicates a type alias or definition.
				for j := 0; j < specCount; j++ {
					child := typeSpec.Child(j)
					ct := child.Type()
					if ct == "type_identifier" && child != nameNode {
						entitySubtype = "type_alias"
						typeBody = child
						break
					}
					if ct == "qualified_type" || ct == "pointer_type" || ct == "array_type" ||
						ct == "map_type" || ct == "slice_type" || ct == "channel_type" ||
						ct == "function_type" {
						entitySubtype = "type_alias"
						typeBody = child
						break
					}
				}
			}

			if entitySubtype == "" {
				continue
			}

			startLine, endLine := nodeLines(typeSpec)

			// Build signature and collect schema-level relationships.
			var signature string
			var kind string
			var relationships []types.RelationshipRecord
			switch entitySubtype {
			case "struct":
				tags := findAll(typeBody, "raw_string_literal")
				if len(tags) > 0 {
					signature = fmt.Sprintf("type %s struct // tags: %d", name, len(tags))
				} else {
					signature = fmt.Sprintf("type %s struct", name)
				}
				structCount++
				// revert — struct declarations are class-like AST
				// constructs that can carry methods (behavior) → SCOPE.Component.
				// Canonical rule: behavior → Component, shape → Schema.
				kind = "SCOPE.Component"
				idx.structs[name] = true
				// emit DEPENDS_ON edges for each field whose type
				// references another type declared in the same file. Keeps
				// the graph conservative: no edges to primitives, no edges
				// to unresolved identifiers (which could be package-external).
				relationships = extractStructFieldDependencies(typeBody, src, name, knownTypeNames)
			case "interface":
				methodNodes := findAll(typeBody, "method_elem", "method_spec")
				signature = fmt.Sprintf("type %s interface // %d method(s)", name, len(methodNodes))
				// revert — interface declarations define behavioral
				// contracts (method sets) → SCOPE.Component. Canonical rule: behavior → Component.
				kind = "SCOPE.Component"
				// record the interface method set so the post-pass
				// can compute IMPLEMENTS edges for structs whose method set
				// is a superset.
				ifaceMethods := make(map[string]bool, len(methodNodes))
				for _, m := range methodNodes {
					methodName := interfaceMethodName(m, src)
					if methodName != "" {
						ifaceMethods[methodName] = true
					}
				}
				idx.interfaces[name] = ifaceMethods
			case "type_alias":
				baseType := ""
				if typeBody != nil {
					baseType = nodeText(typeBody, src)
				}
				signature = fmt.Sprintf("type %s %s", name, baseType)
				kind = "SCOPE.Schema"
			default:
				// Ambiguous type_spec nodes default to SCOPE.Schema
				// behaviour rule: "If an AST node type is ambiguous, default to
				// SCOPE.Schema and log the node type for review."
				kind = "SCOPE.Schema"
			}
			metadata := map[string]interface{}{
				"subtype": entitySubtype,
			}

			rec := types.EntityRecord{
				Name:               name,
				QualifiedName:      "",
				Kind:               kind,
				Subtype:            entitySubtype,
				SourceFile:         filePath,
				StartLine:          startLine,
				EndLine:            endLine,
				Language:           "go",
				Signature:          signature,
				QualityScore:       1.0,
				Metadata:           metadata,
				Relationships:      relationships,
				EnrichmentRequired: false,
			}
			records = append(records, rec)
		}
	}

	return records, structCount, idx
}

// collectDeclaredTypeNames returns the set of every type name declared in
// the file. Used to constrain DEPENDS_ON edges to intra-file references so
// we avoid emitting edges for primitives and unresolved package-external
// types.
func collectDeclaredTypeNames(root *sitter.Node, src []byte) map[string]bool {
	names := make(map[string]bool)
	for _, spec := range findAll(root, "type_spec") {
		name := spec.ChildByFieldName("name")
		if name == nil {
			continue
		}
		names[nodeText(name, src)] = true
	}
	return names
}

// extractStructFieldDependencies returns DEPENDS_ON edges from the struct
// named ownerName to every field type that references another known type
// in the same file. The receiver of the edge is the struct's bare name; the
// target is the field's type name (type_identifier or the innermost
// identifier of a pointer/array/slice/map wrapper).
//
// Deduplicated by (source, target). Field types resolved via these patterns:
//
//	type_identifier            → direct
//	pointer_type(...)          → inner
//	slice_type(...) / array    → element type
//	map_type(...)              → value type
//
// Edges are only emitted when the resolved target name is in knownTypeNames,
// enforcing intra-file scoping.
func extractStructFieldDependencies(body *sitter.Node, src []byte, ownerName string, knownTypeNames map[string]bool) []types.RelationshipRecord {
	if body == nil {
		return nil
	}
	seen := make(map[string]bool)
	var rels []types.RelationshipRecord

	for _, field := range findAll(body, "field_declaration") {
		typeNode := field.ChildByFieldName("type")
		if typeNode == nil {
			continue
		}
		targets := resolveTypeReferences(typeNode, src)
		for _, t := range targets {
			if t == "" || t == ownerName {
				continue
			}
			if !knownTypeNames[t] {
				continue
			}
			if seen[t] {
				continue
			}
			seen[t] = true
			rels = append(rels, types.RelationshipRecord{
				FromID: ownerName,
				ToID:   t,
				Kind:   "DEPENDS_ON",
			})
		}
	}
	return rels
}

// resolveTypeReferences walks a type AST node and returns every
// type_identifier name found inside, flattening pointer/slice/array/map/
// channel wrappers. Returns an empty slice for primitive types that don't
// carry a type_identifier child (those appear as keywords, not identifiers).
func resolveTypeReferences(node *sitter.Node, src []byte) []string {
	if node == nil {
		return nil
	}
	var names []string
	// Fast path for direct type_identifier nodes.
	if node.Type() == "type_identifier" {
		return []string{nodeText(node, src)}
	}
	// Walk all descendants and pick type_identifier nodes. tree-sitter-go
	// emits type_identifier for any non-primitive single-word type.
	for _, n := range findAll(node, "type_identifier") {
		names = append(names, nodeText(n, src))
	}
	return names
}

// interfaceMethodName extracts the method name from a method_elem or
// method_spec node inside an interface_type. Returns "" if the node has
// no name child (embedded interface — covered separately if needed).
func interfaceMethodName(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	if name := node.ChildByFieldName("name"); name != nil {
		return nodeText(name, src)
	}
	// Fallback: scan children for a field_identifier / identifier.
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch.Type() == "field_identifier" || ch.Type() == "identifier" {
			return nodeText(ch, src)
		}
	}
	return ""
}

// attachImplementsRelationships walks the collected entity records and
// emits IMPLEMENTS edges from every struct whose method set is a superset
// of an interface's method set. Both struct and interface must be declared
// in the same file. The edge is attached to the struct entity (source).
//
// This is the intra-file heuristic calls for — a full inter-package
// type-resolution pass is out of scope and would require a real Go type
// checker, not tree-sitter.
//
// Edges are deduplicated by (source, target).
func attachImplementsRelationships(records []types.EntityRecord, idx *typeIndex) []types.EntityRecord {
	if idx == nil || len(idx.structs) == 0 || len(idx.interfaces) == 0 {
		return records
	}

	// Build methodsByReceiver from the collected function records.
	for _, r := range records {
		if r.Kind != "SCOPE.Operation" {
			continue
		}
		if sub, _ := r.Metadata["subtype"].(string); sub != "method" {
			continue
		}
		recv, _ := r.Metadata["receiver"].(string)
		if recv == "" {
			continue
		}
		// r.Name is now "<Receiver>.<method>" (issue #66). Strip the
		// receiver qualifier so the method-set comparison against
		// interface.method names continues to operate on bare identifiers.
		bareName := r.Name
		if dot := strings.IndexByte(r.Name, '.'); dot > 0 {
			bareName = r.Name[dot+1:]
		}
		if idx.methodsByReceiver[recv] == nil {
			idx.methodsByReceiver[recv] = make(map[string]bool)
		}
		idx.methodsByReceiver[recv][bareName] = true
	}

	// For each struct with methods, compare against every interface's
	// method set and emit IMPLEMENTS edges where the struct's method set
	// is a superset.
	for structName := range idx.structs {
		methodSet := idx.methodsByReceiver[structName]
		if len(methodSet) == 0 {
			continue
		}
		for ifaceName, ifaceMethods := range idx.interfaces {
			if structName == ifaceName {
				continue
			}
			if len(ifaceMethods) == 0 {
				// An empty interface is implemented by every type. Skip
				// to avoid N^2 edges in files containing `interface{}`
				// type aliases.
				continue
			}
			if !isMethodSetSuperset(methodSet, ifaceMethods) {
				continue
			}
			records = appendRelationshipTo(records, structName, types.RelationshipRecord{
				FromID: structName,
				ToID:   ifaceName,
				Kind:   "IMPLEMENTS",
			})
		}
	}
	return records
}

// isMethodSetSuperset reports whether every name in child also appears in
// parent. Used to match struct method sets against interface method sets.
func isMethodSetSuperset(parent, child map[string]bool) bool {
	for name := range child {
		if !parent[name] {
			return false
		}
	}
	return true
}

// attachClassContains emits class CONTAINS edges from each struct
// SCOPE.Component to every receiver method whose Metadata["receiver"]
// matches the struct name. Issue #145.
//
// The CONTAINS target is a Format-A structural-ref keyed on the source
// file (scope:operation:method:go:<file>:<Receiver.method>). Using the
// dotted method Name (issue #66) keeps the ToID unique per receiver
// even when two structs in the same file declare a same-named method
// (`(Foo) Get` vs `(Bar) Get`).
//
// Edges are deduplicated by (from, to, kind) via appendRelationshipTo.
func attachClassContains(records []types.EntityRecord, filePath string) []types.EntityRecord {
	for _, r := range records {
		if r.Kind != "SCOPE.Operation" {
			continue
		}
		if sub, _ := r.Metadata["subtype"].(string); sub != "method" {
			continue
		}
		recv, _ := r.Metadata["receiver"].(string)
		if recv == "" {
			continue
		}
		toID := extractor.BuildOperationStructuralRef("go", filePath, r.Name)
		records = appendRelationshipTo(records, recv, types.RelationshipRecord{
			ToID: toID,
			Kind: "CONTAINS",
		})
	}
	return records
}

// appendRelationshipTo appends rel to the Relationships slice of the first
// schema record whose Name matches target. If no matching record exists
// the function is a no-op — this can happen when attachImplements runs
// against a record slice that has already been filtered.
func appendRelationshipTo(records []types.EntityRecord, target string, rel types.RelationshipRecord) []types.EntityRecord {
	for i := range records {
		if records[i].Kind != "SCOPE.Component" {
			continue
		}
		if records[i].Name != target {
			continue
		}
		// Dedup on (from, to, kind).
		for _, existing := range records[i].Relationships {
			if existing.FromID == rel.FromID && existing.ToID == rel.ToID && existing.Kind == rel.Kind {
				return records
			}
		}
		records[i].Relationships = append(records[i].Relationships, rel)
		return records
	}
	return records
}

// extractImportEntities returns standalone SCOPE.Component EntityRecord entries
// for each import path found in the file. Each record carries a single IMPORTS
// relationship. Import records have no line numbers (they are file-level).
//
// Name is always the full module path exactly as it appears in the import
// statement (quotes stripped) — never truncated to the last path segment. This
// is the contract K-2SO parity enforces against the Python indexer.
//
// The four import styles recognised by the Go grammar are all emitted:
//
//	"path"              → import_type="standard"
//	alias "path"        → import_type="aliased", metadata["alias"]=alias
//	. "path"            → import_type="dot"
//	_ "path"            → import_type="blank"
//
// Malformed import specs (no path child) are logged and skipped — never panic.
func extractImportEntities(root *sitter.Node, src []byte, filePath string) []types.EntityRecord {
	var records []types.EntityRecord

	for _, spec := range findAll(root, "import_spec") {
		// Resolve the path via field accessor first — falls back to a child
		// scan for older tree-sitter-go grammars that don't expose fields.
		pathNode := spec.ChildByFieldName("path")
		if pathNode == nil {
			count := int(spec.ChildCount())
			for i := 0; i < count; i++ {
				ch := spec.Child(i)
				if ch.Type() == "interpreted_string_literal" ||
					ch.Type() == "raw_string_literal" {
					pathNode = ch
					break
				}
			}
		}
		if pathNode == nil {
			logWarning(
				"import_spec at line %d has no path child — skipping (malformed)",
				spec.StartPoint().Row+1,
			)
			continue
		}

		rawPath := nodeText(pathNode, src)
		// Strip surrounding quotes (both " and `).
		importPath := strings.Trim(rawPath, "\"`")
		if importPath == "" {
			logWarning(
				"import_spec at line %d has empty path %q — skipping (malformed)",
				spec.StartPoint().Row+1,
				rawPath,
			)
			continue
		}

		// Resolve the optional name modifier: package_identifier (alias),
		// dot (dot import), or blank_identifier (blank import).
		importType := "standard"
		var alias string
		if nameNode := spec.ChildByFieldName("name"); nameNode != nil {
			switch nameNode.Type() {
			case "package_identifier":
				importType = "aliased"
				alias = nodeText(nameNode, src)
			case "dot":
				importType = "dot"
			case "blank_identifier":
				importType = "blank"
			}
		}

		metadata := map[string]interface{}{
			"import_type": importType,
		}
		if alias != "" {
			metadata["alias"] = alias
		}

		records = append(records, types.EntityRecord{
			Name:       importPath,
			Kind:       "SCOPE.Component",
			SourceFile: filePath,
			Language:   "go",
			Metadata:   metadata,
			Relationships: []types.RelationshipRecord{
				{
					FromID: filePath,
					ToID:   importPath,
					Kind:   "IMPORTS",
				},
			},
		})
	}

	return records
}

// logWarning logs a warning to stderr without aborting extraction.
// Called when a tree node is unexpected or a query produces no result.
func logWarning(format string, args ...any) {
	log.Printf("[golang extractor] WARNING: "+format, args...)
}
