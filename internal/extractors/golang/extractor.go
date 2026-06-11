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
//	method-value ref       → RelationshipRecord{Kind="CALLS", Properties["via_value"]="true"}
//	                         (Function → Method, when method used as value: s.M passed as arg)
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
	"strconv"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/txscope"
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

	// Issue #577 — emit a file-level SCOPE.Component (subtype="file")
	// entity per source file so the cross-repo import linker (#566) can
	// map IMPORTS edges back to the originating repo via the resolver's
	// byName index. Generalises the JS/TS fix from #570/#575.
	records = append(records, extractor.FileEntity(file))

	// ----------------------------------------------------------------
	// 1. Functions and methods
	//
	// Issue #614 — collect struct field types (intra-file) BEFORE function
	// extraction so call_expression resolution inside method bodies can
	// detect interface-field dispatch patterns like `h.Store.List()` and
	// stamp the field's static type onto the CALLS edge. Per-file scope:
	// only field types declared in this file are recorded; cross-file
	// resolution to the implementing struct is the resolver's job
	// (refs.go interface-field dispatch lookup).
	structFields := collectStructFieldTypes(root, file.Content)
	// Issue #4332 — per-file map of in-tree package qualifier → package dir,
	// so cross-package selector calls (`resolve.BuildIndex()`) can be stamped
	// with the importing package directory and bound by the resolver instead
	// of collapsing to an ambiguity-prone bare name.
	inTreeQualifiers := buildGoInTreeQualifiers(root, file.Content, goModuleRoot(file.RepoRoot), goModuleReplaces(file.RepoRoot))
	funcEntities, fCount := extractFunctions(root, file.Content, file.Path, structFields, inTreeQualifiers)
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

	// Value-carrying SCOPE.Enum value-set nodes for const-group enums typed by
	// a same-file named type (data-model, epic #3628).
	records = append(records, extractGoEnums(root, file.Content, file.Path)...)

	// Additional Go constant-COLLECTION value-sets the same-file-named-type path
	// above deliberately skips: untyped/foreign-typed grouped const blocks and
	// package-level const maps (`var X = map[string]string{...}`). #4426.
	records = append(records,
		extractGoConstantSets(root, file.Content, file.Path,
			collectNamedTypes(root, file.Content))...)

	// ----------------------------------------------------------------
	// 3. Import relationships — emitted as standalone SCOPE.Component
	//    EntityRecord entries (one per import path). Not fanned out to
	// every function/type entity.
	// ----------------------------------------------------------------
	importRecords := extractImportEntities(root, file.Content, file.Path, goModuleRoot(file.RepoRoot), goModuleReplaces(file.RepoRoot))
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
	// 4c. Track A (analog of #641/#650/#670 for Go) — REFERENCES-edge
	//     emission. Runs after every primary-pass entity is in place so
	//     the file-scope symbol table covers functions, methods,
	//     structs, interfaces, type aliases, and import placeholders.
	//     Failures here recover internally to partial results — never
	//     aborts primary output.
	// ----------------------------------------------------------------
	func() {
		defer func() { _ = recover() }()
		emitReferences(root, file, &records, structFields)
	}()

	// ----------------------------------------------------------------
	// 4d. Track B (analog of #642/#650/#670 for Go) — IMPORTS ToID
	//     rewrite. Rewrites IMPORTS edges whose import-path prefix is
	//     a known external Go package (stdlib + popular third-party)
	//     to an `ext:<prefix>` ToID so the resolver's external-
	//     disposition gate classifies them ExternalKnown directly.
	//     In-tree imports are untouched.
	// ----------------------------------------------------------------
	resolveImportToIDs(records)

	// ----------------------------------------------------------------
	// 5. Error-handling patterns — secondary pass.
	//    Emits one SCOPE.Pattern entity per `if err != nil { ... }`
	//    occurrence. Runs after the base extraction so a detection
	//    failure here cannot abort the primary entity output.
	// ----------------------------------------------------------------
	errorPatterns := extractErrorHandlingPatterns(root, file.Content, file.Path)
	records = append(records, errorPatterns...)

	// ----------------------------------------------------------------
	// 6b. Config-consumption topology (issue #3641, epic #3625) —
	//     DEPENDS_ON_CONFIG edges from functions/methods that read a
	//     config key (os.Getenv / viper.GetString) to a shared
	//     config-key entity, so config:<key>'s inbound edges form the
	//     config-change blast radius. Runs after function entities are
	//     in place so edges attach to the right enclosing entity.
	// ----------------------------------------------------------------
	func() {
		defer func() { _ = recover() }()
		emitConfigConsumerEdges(root, file.Content, &records)
	}()

	// ----------------------------------------------------------------
	// 5b. Error-flow topology (epic #3628) — THROWS / CATCHES edges from
	//     functions/methods to a shared SCOPE.ExceptionType node for NAMED
	//     sentinel errors: `return ErrX` / wrapped `%w` (THROWS) and
	//     errors.Is/As(err, ErrX) (CATCHES). Anonymous errors.New / bare
	//     fmt.Errorf are dropped — precision over recall.
	// ----------------------------------------------------------------
	func() {
		defer func() { _ = recover() }()
		emitExceptionFlowEdges(root, file.Content, &records)
	}()

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

	// Issue #1811 — stamp Properties["build_tag"] on every entity emitted
	// from this file when the file carries a //go:build constraint. The
	// resolver's BuildIndex uses this property to detect platform-variant
	// pairs (darwin||linux vs windows) and merge them into one logical
	// symbol instead of blanking the byPackageOperation slot with the
	// ambiguity sentinel. Without this stamp the resolver has no way to
	// distinguish a genuine same-package duplicate from a platform split.
	if bt := extractBuildTag(file.Content); bt != "" {
		stampBuildTag(records, bt)
	}

	// Issue #90 — stamp Properties["language"]="go" so the resolver's
	// per-language dynamic-pattern dispatch picks the Go catalog.
	extractor.TagRelationshipsLanguage(records, "go")
	extractor.TagEntitiesLanguage(records, "go")

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
func collectBodyVarTypes(body *sitter.Node, src []byte, ctorReturns map[string]string) map[string]string {
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
		if typ := typeOfExpression(expr, src, ctorReturns); typ != "" {
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
				typ = typeOfExpression(valueNode, src, ctorReturns)
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
func typeOfExpression(expr *sitter.Node, src []byte, ctorReturns map[string]string) string {
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
			return typeOfExpression(op, src, ctorReturns)
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
				fnName := nodeText(fn, src)
				if t, ok := goSamePackageConstructorReturnTypes[fnName]; ok {
					return t
				}
				// Issue #4683 / #4615: file-local user-defined constructors.
				// `svc := NewProposalService(); svc.GetCounts()` — type `svc`
				// from NewProposalService's same-package named return type so
				// the downstream method call gets a receiver_type stamp and
				// the test→CALLS→handler coverage edge is credited.
				if ctorReturns != nil {
					if t, ok := ctorReturns[fnName]; ok {
						return t
					}
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

// collectFileConstructorReturns scans top-level function_declaration nodes and
// returns a (funcName -> canonical return type) map for functions that look
// like constructors: a single result whose type is a same-package named type
// (a bare type_identifier, optionally behind a leading `*`). This generalises
// the hardcoded goSamePackageConstructorReturnTypes allowlist (#364) to ANY
// user-defined `func NewFoo(...) *Foo` / `func MakeBar(...) Bar` declared in
// the file, so `x := NewFoo(); x.Method()` types `x` as `Foo` and the
// downstream method call acquires a receiver_type stamp (issue #4683 / #4615:
// test→CALLS→handler coverage crediting for Go).
//
// Conservatism (mirrors prior-slice rules):
//   - Only a SINGLE, unnamed result is accepted. Functions returning
//     `(T, error)`, multiple values, or named results are skipped — pairing
//     a `:=` LHS with the right tuple slot is out of scope (same limitation
//     as the multi-LHS short_var_declaration skip).
//   - The result type must be a bare same-package named type (type_identifier
//     or pointer_type→type_identifier). Qualified types (`pkg.T`), interface
//     results, slices, maps, generics, funcs and channels are skipped: a
//     constructor returning an interface is ambiguous (the negative case —
//     factory-returning-interface receiver stays bare).
//   - The canonical return type is stored as a BARE type name (pointer
//     stripped) so the resolver's same-package member lookup binds it directly
//     — identical to the goSamePackageConstructorReturnTypes contract.
func collectFileConstructorReturns(nodes []*sitter.Node, src []byte) map[string]string {
	// Only constructors whose declared return type is a same-file STRUCT are
	// accepted. A constructor declared to return an interface
	// (`func NewCounter() Counter`) is intentionally excluded — the concrete
	// type behind the interface is a runtime decision, so `c := NewCounter();
	// c.Method()` must stay bare (the negative case from #4683). Interface and
	// non-struct named types are therefore filtered out here.
	structTypes := collectFileStructTypeNames(nodes, src)
	if len(structTypes) == 0 {
		return nil
	}
	out := map[string]string{}
	for _, n := range nodes {
		if n.Type() != "function_declaration" {
			continue
		}
		nameNode := n.ChildByFieldName("name")
		resultNode := n.ChildByFieldName("result")
		if nameNode == nil || resultNode == nil {
			continue
		}
		name := nodeText(nameNode, src)
		if name == "" {
			continue
		}
		// Accept only a single, unnamed, same-package named result that is a
		// STRUCT declared in this file (interface returns stay bare).
		ret := samePackageNamedResultType(resultNode, src)
		if ret == "" || !structTypes[ret] {
			continue
		}
		if existing, ok := out[name]; ok && existing != ret {
			// Two same-named constructors with different return types in one
			// file (extremely rare) — drop rather than guess.
			delete(out, name)
			continue
		}
		out[name] = ret
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// collectFileStructTypeNames returns the set of type names whose declaration
// body is a struct_type in the file's node slice. Used by
// collectFileConstructorReturns to restrict constructor-return typing to
// concrete structs — interface / alias / func / primitive named types are
// excluded so an interface-returning factory stays bare (#4683 negative).
//
// The input slice is the file's function/method declaration list; type
// declarations are elsewhere in the tree, so we recover the file root from the
// first node's parentage and scan every type_spec once. A type_spec whose
// `type` field is a `struct_type` is recorded; interfaces, aliases, funcs and
// primitives are skipped.
func collectFileStructTypeNames(nodes []*sitter.Node, src []byte) map[string]bool {
	out := map[string]bool{}
	if len(nodes) == 0 {
		return out
	}
	// Recover the file root from any node (walk up to the top).
	root := nodes[0]
	for root.Parent() != nil {
		root = root.Parent()
	}
	for _, spec := range findAll(root, "type_spec") {
		nameNode := spec.ChildByFieldName("name")
		typeNode := spec.ChildByFieldName("type")
		if nameNode == nil || typeNode == nil {
			continue
		}
		if typeNode.Type() != "struct_type" {
			continue
		}
		if name := nodeText(nameNode, src); name != "" {
			out[name] = true
		}
	}
	return out
}

// samePackageNamedResultType returns the canonical bare type name of a
// function `result` node when (and only when) it is a single, unnamed result
// that is a same-package named type: a `type_identifier` or a `pointer_type`
// wrapping one. Anything else (multiple results, named results, qualified
// types, interfaces, slices, maps, generics, funcs, channels) returns "".
//
// The Go tree-sitter `result` field is either the bare type node itself (for
// a single unnamed result like `*Foo`) or a `parameter_list` (for `(T, error)`
// or named results). We only accept the bare-node form, which guarantees a
// single unnamed result.
func samePackageNamedResultType(result *sitter.Node, src []byte) string {
	if result == nil {
		return ""
	}
	switch result.Type() {
	case "type_identifier":
		return nodeText(result, src)
	case "pointer_type":
		for i := 0; i < int(result.ChildCount()); i++ {
			c := result.Child(i)
			if c.Type() == "type_identifier" {
				return nodeText(c, src)
			}
		}
	}
	return ""
}

// collectIntraFileFuncNames returns the set of bare names of all top-level
// functions (function_declaration, NOT method_declaration) in the node slice.
// Only nodes of type "function_declaration" are included; method_declaration
// nodes have a receiver and their entity Name carries the "Recv.method" dotted
// form, so they are excluded — method calls are resolved through the
// receiver_type machinery, not the intraFileFuncs set.
//
// Issue #1806: used by extractFunctions to build the same-file symbol table
// passed into extractCallRelationships. Enables the intra-file / cross-file
// distinction for bare-identifier CALLS edge emission.
func collectIntraFileFuncNames(nodes []*sitter.Node, src []byte) map[string]struct{} {
	funcs := make(map[string]struct{})
	for _, n := range nodes {
		if n.Type() != "function_declaration" {
			continue
		}
		nameNode := n.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		name := nodeText(nameNode, src)
		if name != "" {
			funcs[name] = struct{}{}
		}
	}
	return funcs
}

// extractFunctions extracts function_declaration and method_declaration nodes.
// Returns entity records and the count of function-type entities.
//
// each function/method entity carries a slice of CALLS
// RelationshipRecord values extracted from call_expression nodes inside its
// body. Methods additionally carry a DEPENDS_ON edge to the receiver type
// so the graph can traverse from method back to its owning schema.
func extractFunctions(root *sitter.Node, src []byte, filePath string, structFields map[string]map[string]string, inTreeQualifiers map[string]string) ([]types.EntityRecord, int) {
	importStems := collectImportStems(root, src)
	nodes := findAll(root, "function_declaration", "method_declaration")

	// Issue #1806 — intra-file function symbol table.
	// First pass: collect the bare names of all top-level functions (not
	// methods — methods have a receiver and are named "Recv.Method") declared
	// in this file. The set is passed into extractCallRelationships so that
	// bare-identifier call_expression nodes whose target name matches an
	// intra-file function can be identified as same-file intra-package calls.
	// Cross-file same-package calls (callee defined in a sibling .go file of
	// the same package) are also emitted — the resolver's byPackageOperation
	// index handles those via the structural-ref's caller-file path (v1
	// limitation: only single-file resolution confirmed at extraction time;
	// cross-file resolution depends on byPackageOperation being unambiguous
	// for the target name in the package directory).
	intraFileFuncs := collectIntraFileFuncNames(nodes, src)

	// Issue #4683 / #4615 — file-local constructor return-type table. Lets
	// `x := NewFoo(); x.Method()` type `x` as Foo (Foo declared in this file)
	// so the method call acquires a receiver_type stamp → test→CALLS→handler
	// coverage crediting. Built once per file; nil when no constructor exists.
	ctorReturns := collectFileConstructorReturns(nodes, src)

	// Issue #3628 area #11 — non-OTel observability (ddtrace/Sentry/Prometheus).
	// Build the file-level Prometheus metric registry once so `reqs.Inc()` body
	// calls resolve to the metric declared by `reqs := prometheus.NewCounter(...)`
	// anywhere in the file (package- or function-scope). nil when no metric.
	metricReg := buildGoMetricRegistry(root, src)

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
		paramTypes = mergeVarTypes(paramTypes, collectBodyVarTypes(bodyOrNode, src, ctorReturns))
		relationships := extractCallRelationships(bodyOrNode, src, nameText, recvVarName, receiverType, paramTypes, filePath, structFields, intraFileFuncs, inTreeQualifiers)
		// Rewrite FromID on each CALLS edge to the qualified Name so the
		// edge source matches the entity ID downstream.
		//
		// Refs #44 #472 — top-level `main` and `init` are unqualified across
		// every Go package: any multi-binary repo (e.g. grpc-go-examples
		// with 70 `examples/*/main.go` files) collapses their bare-name
		// byName entries to "ambiguous", forcing every CALLS edge sourced
		// from `main`/`init` into the bug-resolver bucket. The Format A
		// structural-ref form `scope:operation:function:go:<file>:<name>`
		// pins the edge to the file's entity via byLocation[<file>][<name>]
		// regardless of how many sibling files repeat the same bare name.
		// Methods already encode their receiver into Name (issue #66) and
		// resolve via byMember, so this only fires for top-level functions.
		fromID := name
		if receiverType == "" {
			// All top-level functions get a Format A structural-ref FromID.
			// Even `Run`, `Setup`, `Handle`, `New`, etc. collide across
			// packages in any non-trivial multi-binary repo; widening from
			// the narrow `main`/`init` allowlist to every top-level function
			// removes the entire FromID-side ambiguity class for Go without
			// changing the entity Name (so byName lookups for the ToID side
			// behave exactly as before).
			fromID = extractor.BuildOperationStructuralRef("go", filePath, nameText)
		}
		for i := range relationships {
			relationships[i].FromID = fromID
		}

		// DEPENDS_ON edge from each method to its receiver type.
		// Enables graph traversal from method → owning schema without
		// needing qualified-name joins downstream.
		if receiverType != "" {
			relationships = append(relationships, types.RelationshipRecord{
				FromID: fromID,
				ToID:   receiverType,
				Kind:   "DEPENDS_ON",
			})
		}

		// Issue #3689 — OpenTelemetry span-creation sites: emit INSTRUMENTS
		// edges from this operation → synthetic span stubs. nameText is the
		// bare function/method name used to key dynamic-name stubs.
		relationships = append(relationships,
			goTracingSpanEdges(bodyNode, nameText, fromID, src)...)

		// Issue #3628 area #11 — non-OTel observability: ddtrace tracer.StartSpan/
		// StartSpanFromContext, Sentry sentry.StartSpan, Prometheus metric
		// mutations on known metric vars → INSTRUMENTS span:/metric: stubs.
		relationships = append(relationships,
			goObservabilityEdges(bodyNode, nameText, fromID, metricReg, src)...)

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
		// Transaction-boundary stamp (#3628): mark the function transactional
		// when a db.Begin()/BeginTx/GORM-Transaction construct is lexically
		// present in its body. No transitive propagation — a function that
		// merely receives a *sql.Tx is not stamped.
		if tx := txscope.DetectGo(nodeText(bodyOrNode, src)); tx.Transactional {
			rec.Properties = tx.Apply(rec.Properties)
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
//
// Issue #1806 — intra-package bare-function calls.
// The intraFileFuncs set carries the bare names of all top-level functions
// declared in the same source file (not methods). When a bare-identifier
// call_expression targets a name in this set, the CALLS edge ToID is the
// caller-file structural-ref `scope:operation:method:go:<file>:<name>`,
// which resolves directly via byLocation[file][name] in the resolver —
// no cross-file fallback needed. This is the same structural-ref shape used
// for all bare identifier calls (Refs #44/#476) but the intraFileFuncs check
// makes the same-file contract explicit and guarantees the byLocation path
// fires without ambiguity from other-package collisions.
//
// Bare calls to names NOT in intraFileFuncs (cross-file same-package
// functions, builtins, closures passed as values, etc.) also emit the
// structural-ref keyed on the caller's file. The resolver's
// byPackageOperation[pkgDir][name] index handles cross-file same-package
// resolution when the target name is unambiguous in the package. If
// byPackageOperation marks the name as ambiguous (e.g., the same function
// is defined in two platform-specific files under the same package directory),
// the structural-ref stub routes to Dynamic via isHeuristicScopeStub rather
// than bug-resolver — a v1 limitation documented in the comment above
// (single-file confirmation at extraction time; cross-file resolution depends
// on byPackageOperation being unambiguous).
func extractCallRelationships(body *sitter.Node, src []byte, callerName, recvVarName, recvType string, paramTypes map[string]string, filePath string, structFields map[string]map[string]string, intraFileFuncs map[string]struct{}, inTreeQualifiers map[string]string) []types.RelationshipRecord {
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
		// Issue #1789 — method-value references.
		// Detect selector_expression nodes that are NOT the function child of a
		// call_expression — i.e. the method is used as a VALUE (passed as an
		// argument, stored in a variable, etc.) rather than being invoked.
		// For these cases the CALLS path never fires, so `find_callers` sees
		// "no_incoming_edges". We emit a CALLS edge with via_value=true so the
		// resolver binds it like any other CALLS edge, while the property lets
		// consumers distinguish "invoked here" from "passed here".
		//
		// Supported shapes:
		//   s.wrap("name", s.handler)       — method value as argument
		//   var h = obj.Method               — method value in short/var decl
		//   return obj.Method                — method value in return statement
		//
		// Not handled (v1): `m := obj.Method; m(arg)` — the through-a-local
		// indirect call requires tracking local-var method aliases, which is a
		// separate pass. The declaration site DOES emit via_value=true already.
		if t == "selector_expression" && !isSelectorCalleeOf(n) {
			mvTarget, mvRecvMatch, mvOperand := selectorMethodValue(n, src, recvVarName)
			if mvTarget != "" && mvTarget != callerName {
				// Deduplicate with suffix "?mv" to avoid merging with a direct
				// CALLS edge to the same method (they carry different semantics).
				mvKey := mvTarget + "?mv"
				if !seen[mvKey] {
					seen[mvKey] = true
					// Line comes from the selector_expression node (n), which
					// is the method-value reference site. StartPoint().Row is
					// 0-based; +1 converts to 1-based line number.
					mvLine := strconv.Itoa(int(n.StartPoint().Row) + 1)
					mvRec := types.RelationshipRecord{
						FromID:     callerName,
						ToID:       mvTarget,
						Kind:       "CALLS",
						Properties: map[string]string{"via_value": "true", "line": mvLine},
					}
					if mvRecvMatch && recvType != "" {
						mvRec.Properties["receiver_type"] = recvType
					} else if mvOperand != "" {
						if ty := lookup(mvOperand); ty != "" {
							mvRec.Properties["receiver_type"] = ty
						}
					}
					rels = append(rels, mvRec)
				}
			}
		}
		if t == "call_expression" {
			target, isSelfReceiver, operand := callExpressionTargetWithOperand(n, src, recvVarName)
			// Self-recursion suppression: drop a call only when target
			// equals callerName AND the call genuinely dispatches to the
			// enclosing method (self-receiver) or the enclosing scope is a
			// top-level function with no receiver. A method `(h *Foo) List`
			// calling `h.Store.List()` shares the bare name `List` with its
			// caller but the dispatch is on `h.Store` (a different receiver
			// chain), so the edge MUST be emitted — issue #614.
			isSelfCall := target == callerName && (isSelfReceiver || recvType == "")
			if target != "" && !isSelfCall && !seen[target] {
				seen[target] = true
				// Line is the 1-based line of the call_expression node (n).
				// StartPoint().Row is 0-based per tree-sitter convention.
				callLine := strconv.Itoa(int(n.StartPoint().Row) + 1)
				rec := types.RelationshipRecord{
					FromID: callerName,
					ToID:   target,
					Kind:   "CALLS",
				}
				switch {
				case isSelfReceiver && recvType != "":
					rec.Properties = map[string]string{"receiver_type": recvType, "line": callLine}
				case operand != "":
					if ty := lookup(operand); ty != "" {
						rec.Properties = map[string]string{"receiver_type": ty, "line": callLine}
					} else if dir := inTreeQualifiers[operand]; dir != "" {
						// Issue #4332 — cross-package selector call. `operand` is an
						// in-tree imported package qualifier (not a local var or
						// receiver, since lookup(operand) missed), so this is
						// `pkg.Exported(...)`. Stamp the imported package directory
						// and the bare callee leaf so the resolver binds the edge
						// to that package's byPackageOperation entry instead of
						// falling back to an ambiguity-prone global bare-name match.
						rec.Properties = map[string]string{
							"go_call_pkg_dir": dir,
							"call_leaf":       target,
							"line":            callLine,
						}
					} else {
						rec.Properties = map[string]string{"line": callLine}
					}
				default:
					rec.Properties = map[string]string{"line": callLine}
				}
				// Issue #614 — interface-field dispatch detection. When the
				// call is `<recvVarName>.<Field>.<method>(...)` (a 2-level
				// selector whose outermost operand is a selector_expression
				// rooted at the enclosing method's receiver var), look up
				// the field's declared type on the enclosing receiver
				// struct and stamp it onto the edge as
				// `interface_dispatch_type` so the resolver can fan out to
				// any IMPLEMENTS edge targeting that interface name and
				// rebind the bare-name target to the implementing struct's
				// method (`MemoryStore.List`). Per-file scope: structFields
				// only contains fields declared in this file's structs, so
				// the dispatch stamp is emitted ONLY when the receiver type
				// and the interface field are both intra-file.
				if recvVarName != "" && recvType != "" && structFields != nil {
					if fieldName, isFieldDispatch := selectorFieldOnReceiver(n, src, recvVarName); isFieldDispatch {
						if fieldType := structFields[recvType][fieldName]; fieldType != "" {
							if rec.Properties == nil {
								rec.Properties = map[string]string{}
							}
							rec.Properties["interface_dispatch_type"] = fieldType
						}
					}
				}
				// Refs #44 (Refs #476 follow-up) — identifier-form CALLS edges
				// (bare `foo(...)` with no selector operand) collide on the
				// ToID side just like top-level `main`/`init` collide on the
				// FromID side: any multi-binary repo (grpc-go-examples with
				// 14 `func callUnaryEcho` across examples/*/client/main.go)
				// folds the bare-name byName entry to "ambiguous" and forces
				// every such edge into bug-resolver. Rewriting the ToID to a
				// Format A structural-ref keyed on the CALLER's file pins
				// same-file callees (the dominant pattern: a function calling
				// a sibling helper declared in the same .go file) to a unique
				// byLocation entry. Cross-file/cross-package bare-name calls
				// that don't resolve still avoid bug-resolver because
				// isHeuristicScopeStub routes unresolved `scope:operation:`
				// stubs to Dynamic. Selector-form calls (`pkg.X` /
				// `recv.method`) keep their bare ToID — they resolve through
				// byMember / external-package / receiver_type paths that the
				// structural-ref shape doesn't help with.
				// Issue #614 — skip the structural-ref rewrite when the
				// edge is an interface-field dispatch. The resolver's
				// dispatch path keys on the bare member name in r.ToID
				// (no per-file rewrite); the structural-ref would bury
				// the bare name inside a `scope:operation:` stub that
				// the dispatch lookup cannot match.
				// Issue #1806 — intra-package bare-function call resolution.
				// Two sub-cases share the same structural-ref shape:
				//
				// (a) Intra-file call: target is a top-level function declared
				//     in the same source file (confirmed via intraFileFuncs).
				//     The structural-ref scope:operation:method:go:<file>:<name>
				//     resolves directly via byLocation[file][name]; no
				//     cross-file fallback is needed. This is the canonical
				//     "function calling a sibling helper" pattern.
				//
				// (b) Cross-file same-package call: target is defined in a
				//     sibling .go file of the same package (not present in
				//     intraFileFuncs). The same structural-ref shape is used;
				//     the resolver's byPackageOperation[pkgDir][name] index
				//     handles resolution when the target name is unambiguous
				//     in the package directory. If byPackageOperation has an
				//     ambiguity sentinel (e.g., the function is defined in two
				//     platform-conditional files), the stub routes to Dynamic —
				//     a v1 limitation; cross-file resolution is best-effort.
				hasIfaceDispatch := rec.Properties["interface_dispatch_type"] != ""
				if operand == "" && !isSelfReceiver && filePath != "" && !hasIfaceDispatch {
					rec.ToID = extractor.BuildOperationStructuralRef("go", filePath, target)
					// Stamp intra_file=true when the callee is confirmed to be
					// in the same source file. Consumers can use this property
					// to distinguish "same-file helper call" (always resolves
					// via byLocation) from "cross-file same-package call"
					// (resolves via byPackageOperation, best-effort).
					if _, isIntraFile := intraFileFuncs[target]; isIntraFile {
						if rec.Properties == nil {
							rec.Properties = map[string]string{}
						}
						rec.Properties["intra_file"] = "true"
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

// isSelectorCalleeOf reports whether sel is the `function` child of its
// parent `call_expression`. When true, CALLS already owns the edge and the
// method-value path must NOT emit a duplicate via_value edge.
func isSelectorCalleeOf(sel *sitter.Node) bool {
	if sel == nil {
		return false
	}
	parent := sel.Parent()
	if parent == nil || parent.Type() != "call_expression" {
		return false
	}
	return parent.ChildByFieldName("function") == sel
}

// selectorMethodValue extracts the method name from a selector_expression
// that is being used as a value (not invoked). Returns:
//   - methodName: the bare field name (e.g. "handleQueryGraph")
//   - isSelfReceiver: true when the operand matches recvVarName
//   - operandName: the operand identifier text (for type lookup)
//
// Returns ("", false, "") when the selector is not a simple `obj.Method`
// shape, or when the operand is not an identifier (chained selectors,
// index expressions, etc.).
//
// Issue #1789: drives CALLS via_value=true emission for patterns like
//
//	s.wrap("name", s.handleQueryGraph)   — operand == recvVarName
//	var h = obj.Method                   — operand is a known variable
//	register("foo", obj.Method)          — generic argument
func selectorMethodValue(sel *sitter.Node, src []byte, recvVarName string) (string, bool, string) {
	if sel == nil {
		return "", false, ""
	}
	operand := sel.ChildByFieldName("operand")
	field := sel.ChildByFieldName("field")
	if operand == nil || field == nil {
		return "", false, ""
	}
	if operand.Type() != "identifier" {
		// Chained selector (e.g. a.b.c used as value) — not covered in v1.
		return "", false, ""
	}
	opName := nodeText(operand, src)
	methodName := nodeText(field, src)
	if opName == "" || methodName == "" {
		return "", false, ""
	}
	isSelf := recvVarName != "" && opName == recvVarName
	return methodName, isSelf, opName
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
			// Go tree-sitter grammar represents two kinds of type declarations:
			//   type_spec   — `type T U`      (type definition / named type)
			//   type_alias  — `type T = U`    (alias declaration, Go 1.9+)
			// Both carry the same field structure for our purposes (a name
			// node and a type body), but in the type_alias form the first
			// non-punctuation child is the name and the child after "=" is
			// the underlying type. We handle both shapes below.
			specNodeType := typeSpec.Type()
			if specNodeType != "type_spec" && specNodeType != "type_alias" {
				continue
			}

			nameNode := typeSpec.ChildByFieldName("name")
			if nameNode == nil {
				// type_alias nodes may not expose "name" by field name in older
				// grammar versions — fall back to the first type_identifier child.
				specCount0 := int(typeSpec.ChildCount())
				for j := 0; j < specCount0; j++ {
					ch := typeSpec.Child(j)
					if ch.Type() == "type_identifier" {
						nameNode = ch
						break
					}
				}
				if nameNode == nil {
					continue
				}
			}
			name := nodeText(nameNode, src)

			// For a type_alias (`type T = U`), the AST node IS the alias;
			// emit directly as type_alias without further subtype detection.
			if specNodeType == "type_alias" {
				// Find the underlying type node (the child after "=").
				var aliasBody *sitter.Node
				specCount0 := int(typeSpec.ChildCount())
				for j := 0; j < specCount0; j++ {
					ch := typeSpec.Child(j)
					if ch == nameNode || ch.Type() == "=" {
						continue
					}
					aliasBody = ch
					break
				}
				startLine, endLine := nodeLines(typeSpec)
				baseType := ""
				if aliasBody != nil {
					baseType = nodeText(aliasBody, src)
				}
				signature := fmt.Sprintf("type %s = %s", name, baseType)
				rec := types.EntityRecord{
					Name:               name,
					Kind:               "SCOPE.Schema",
					Subtype:            "type_alias",
					SourceFile:         filePath,
					StartLine:          startLine,
					EndLine:            endLine,
					Language:           "go",
					Signature:          signature,
					QualityScore:       1.0,
					Metadata:           map[string]interface{}{"subtype": "type_alias"},
					EnrichmentRequired: false,
				}
				records = append(records, rec)
				continue
			}

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
	// Collect both type_spec (named type) and type_alias (assignment alias)
	// so DEPENDS_ON edges can reference either form.
	for _, spec := range findAll(root, "type_spec", "type_alias") {
		name := spec.ChildByFieldName("name")
		if name == nil {
			// Fallback: first type_identifier child.
			for j := 0; j < int(spec.ChildCount()); j++ {
				ch := spec.Child(j)
				if ch.Type() == "type_identifier" {
					name = ch
					break
				}
			}
		}
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

// collectStructFieldTypes walks every struct_type declaration in the file
// and returns a per-struct map from field name to its declared type string
// (e.g. `Store`, `store.Store`, `*MemoryStore`). Used by extractCallRelationships
// (issue #614) to recognise `<recv>.<Field>.<method>()` interface-field
// dispatch and stamp the field's type on the CALLS edge so the resolver
// can fan out to IMPLEMENTS edges.
//
// Per-file scope: only structs declared in this file are recorded. The
// resolver consumes the stamp across files; the extractor stays local.
//
// Field handling:
//   - Named fields keep their identifier name and the verbatim type text.
//   - Pointer / slice / map wrappers are kept verbatim — the resolver
//     normalises pointer prefixes and package qualifiers.
//   - Embedded fields (no name child) are SKIPPED — `Store` embedded in
//     `UsersHandler` is rare in handler structs and the resolver path
//     for embedded promotion is out of scope for #614.
func collectStructFieldTypes(root *sitter.Node, src []byte) map[string]map[string]string {
	if root == nil {
		return nil
	}
	out := map[string]map[string]string{}
	for _, typeDecl := range findAll(root, "type_declaration") {
		count := int(typeDecl.ChildCount())
		for i := 0; i < count; i++ {
			spec := typeDecl.Child(i)
			if spec.Type() != "type_spec" {
				continue
			}
			nameNode := spec.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			structName := nodeText(nameNode, src)
			// Find struct_type child (skip interface/type_alias).
			var body *sitter.Node
			specCount := int(spec.ChildCount())
			for j := 0; j < specCount; j++ {
				c := spec.Child(j)
				if c.Type() == "struct_type" {
					body = c
					break
				}
			}
			if body == nil {
				continue
			}
			fields := map[string]string{}
			for _, field := range findAll(body, "field_declaration") {
				typeNode := field.ChildByFieldName("type")
				if typeNode == nil {
					continue
				}
				typeText := nodeText(typeNode, src)
				if typeText == "" {
					continue
				}
				// field_declaration may have a `name` field
				// (field_identifier) OR an identifier_list (multiple fields
				// sharing one type: `A, B int`). Walk children to collect
				// every leading identifier before the type.
				for k := 0; k < int(field.ChildCount()); k++ {
					ch := field.Child(k)
					switch ch.Type() {
					case "field_identifier":
						fields[nodeText(ch, src)] = typeText
					}
				}
			}
			if len(fields) > 0 {
				out[structName] = fields
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// selectorFieldOnReceiver tests whether call's function is of the shape
// `<recvVarName>.<Field>.<method>(...)` — a 2-level selector_expression
// whose outermost operand is itself a selector_expression rooted at
// recvVarName. Returns (fieldName, true) when the pattern matches, where
// fieldName is the middle identifier (`Field`). Returns ("", false) for
// any other call shape.
//
// Used by extractCallRelationships to detect interface-field dispatch
// (`h.Store.List()` inside `(h *UsersHandler).List`) so the resolver can
// rebind the bare method target to the implementing struct's method.
func selectorFieldOnReceiver(call *sitter.Node, src []byte, recvVarName string) (string, bool) {
	if call == nil || recvVarName == "" {
		return "", false
	}
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "selector_expression" {
		return "", false
	}
	outerOperand := fn.ChildByFieldName("operand")
	if outerOperand == nil || outerOperand.Type() != "selector_expression" {
		return "", false
	}
	innerOperand := outerOperand.ChildByFieldName("operand")
	innerField := outerOperand.ChildByFieldName("field")
	if innerOperand == nil || innerField == nil {
		return "", false
	}
	if innerOperand.Type() != "identifier" {
		return "", false
	}
	if nodeText(innerOperand, src) != recvVarName {
		return "", false
	}
	return nodeText(innerField, src), true
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
//
// When moduleRoot is non-empty (read from go.mod by the caller), in-tree
// imports are stamped with Properties["go_module_root"] and
// Properties["go_pkg_dir"] so the resolver's ResolveGoInTreeImports pass
// can map them to the correct file-level SCOPE.Component entities. External
// imports are left for the resolveImportToIDs pass to rewrite to ext: form.
func extractImportEntities(root *sitter.Node, src []byte, filePath, moduleRoot string, replaces []goReplace) []types.EntityRecord {
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

		// Build the IMPORTS relationship. For in-tree imports (when
		// moduleRoot is known and the import path starts with it), stamp
		// go_module_root and go_pkg_dir so the resolver can bind the edge
		// to the importing package's file entities rather than leaving it
		// unresolved. External imports are handled by resolveImportToIDs.
		rel := types.RelationshipRecord{
			FromID: filePath,
			ToID:   importPath,
			Kind:   "IMPORTS",
		}
		if moduleRoot != "" && strings.HasPrefix(importPath, moduleRoot+"/") {
			pkgDir := importPath[len(moduleRoot)+1:]
			rel.Properties = map[string]string{
				"go_module_root": moduleRoot,
				"go_pkg_dir":     pkgDir,
			}
		} else if pkgDir, ok := goReplacePkgDir(importPath, replaces); ok {
			// #4705c: a local-path `replace` directive redirects this import
			// to an in-repo directory. Stamp go_pkg_dir so the resolver's
			// ResolveGoInTreeImports pass binds it to the local file entity
			// BEFORE it falls through to external_package.
			rel.Properties = map[string]string{
				"go_module_root": moduleRoot,
				"go_pkg_dir":     pkgDir,
			}
		}

		records = append(records, types.EntityRecord{
			Name:          importPath,
			Kind:          "SCOPE.Component",
			SourceFile:    filePath,
			Language:      "go",
			Metadata:      metadata,
			Relationships: []types.RelationshipRecord{rel},
		})
	}

	return records
}

// logWarning logs a warning to stderr without aborting extraction.
// Called when a tree node is unexpected or a query produces no result.
func logWarning(format string, args ...any) {
	log.Printf("[golang extractor] WARNING: "+format, args...)
}

// extractBuildTag scans the first bytes of a Go source file and returns the
// normalised build-constraint expression from a "//go:build <expr>" directive,
// or "" when none is present. Only the first 4096 bytes are inspected — build
// constraints must appear before the package clause per the Go spec.
//
// Issue #1811: the returned string is stamped as Properties["build_tag"] on
// all entity records emitted from this file by stampBuildTag so the resolver
// can identify platform-variant pairs and merge them into one logical symbol.
func extractBuildTag(content []byte) string {
	if len(content) == 0 {
		return ""
	}
	limit := len(content)
	if limit > 4096 {
		limit = 4096
	}
	snippet := string(content[:limit])
	// Fast-path: no build directive present.
	if !strings.Contains(snippet, "//go:build") && !strings.Contains(snippet, "// +build") {
		return ""
	}
	for _, line := range strings.SplitN(snippet, "\n", 52) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//go:build ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "//go:build "))
		}
	}
	// Legacy fallback — pre-Go-1.17 files may only have "// +build".
	for _, line := range strings.SplitN(snippet, "\n", 52) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "// +build ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "// +build "))
		}
	}
	return ""
}

// stampBuildTag sets Properties["build_tag"] = tag on every EntityRecord in
// records that does not already have a "build_tag" property. Skips records
// with a nil Properties map only to allocate the minimum required.
//
// Issue #1811: called by Extract immediately after all extraction passes so
// the resolver's BuildIndex can read the constraint at index time.
func stampBuildTag(records []types.EntityRecord, tag string) {
	for i := range records {
		r := &records[i]
		if r.Properties == nil {
			r.Properties = map[string]string{"build_tag": tag}
			continue
		}
		if _, ok := r.Properties["build_tag"]; !ok {
			r.Properties["build_tag"] = tag
		}
	}
}
