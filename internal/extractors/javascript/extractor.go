// Package javascript implements the JS/TS language extractor for archigraph.
//
// A single JSExtractor handles both "javascript" and "typescript" because the
// TypeScript grammar is a strict superset of JavaScript. The OTel span name is
// "extractor.javascript" for both; the Language attribute distinguishes them.
//
// Registered languages: "javascript", "typescript"
//
// Extracted node types:
//   - function_declaration       → Kind="SCOPE.Operation"
//   - arrow_function (via const) → Kind="SCOPE.Operation"
//   - function_expression (via const) → Kind="SCOPE.Operation"
//   - class_declaration          → Kind="SCOPE.Component"
//   - method_definition          → Kind="SCOPE.Operation"
//   - interface_declaration (TS) → Kind="SCOPE.Schema"
//   - type_alias_declaration (TS)→ Kind="SCOPE.Schema"
//   - import_statement + require → Kind="SCOPE.Component"
package javascript

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// New returns a new JSExtractor. Use this in tests or explicit registrations.
func New() *JSExtractor {
	return &JSExtractor{}
}

// init registers the extractor for both "javascript" and "typescript".
func init() {
	e := New()
	extreg.Register("javascript", e)
	extreg.Register("typescript", e)
}

// JSExtractor extracts entities from JavaScript and TypeScript source files.
// It is safe for concurrent use.
type JSExtractor struct{}

// Language returns the canonical language name (used for registration).
func (e *JSExtractor) Language() string {
	return "javascript"
}

// Extract processes a parsed JS/TS source file and returns entity records.
//
// The tree-sitter parse tree (file.Tree) may be nil for empty files, in which
// case an empty slice is returned. Partial results are returned when individual
// node queries fail; errors are logged but never abort the full extraction.
func (e *JSExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("javascript")
	_, span := tracer.Start(ctx, "extractor.javascript",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file", file.Path),
			attribute.Int("file_size_bytes", len(file.Content)),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Tree == nil {
		span.SetAttributes(
			attribute.Int("entity_count", 0),
			attribute.Int("relationship_count", 0),
		)
		return nil, nil
	}

	root := file.Tree.RootNode()
	if root == nil {
		return nil, nil
	}

	x := &extractor{
		source:   file.Content,
		filePath: file.Path,
		language: file.Language,
		aliases:  AliasMapFor(file.RepoRoot),
		repoRoot: file.RepoRoot,
	}

	// Issue #421 — collect import bindings BEFORE walking the body so
	// receiver-typed CALLS edges materialised inside class methods can
	// look up the imported source file at emission time. Bindings is
	// indexed by the local name introduced into the file scope; the
	// receiver binder consults it when the receiver's declared type
	// matches a binding.
	x.imports = x.collectFileImports(root)
	x.importByLocal = make(map[string]*importBinding, len(x.imports))
	for i := range x.imports {
		b := &x.imports[i]
		if existing, dup := x.importByLocal[b.localName]; dup {
			// Issue #505 — when multiple bindings share localName,
			// they MAY be alias-resolved variants of the same import
			// statement (one binding per candidate target). Detect
			// that shape (same importPath + same importedName +
			// aliasResolved on both) and prefer the first
			// registration silently. Genuine duplicates (different
			// importPath or importedName) still hit the safer-bias
			// "drop both" path.
			if existing.importPath == b.importPath &&
				existing.importedName == b.importedName &&
				existing.aliasResolved && b.aliasResolved {
				continue
			}
			// Last-writer-wins is unsafe; drop both. The receiver
			// binder leaves the bare method name in place when the
			// type lookup misses, which is the conservative bias.
			delete(x.importByLocal, b.localName)
			continue
		}
		x.importByLocal[b.localName] = b
	}

	var extractErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				extractErr = fmt.Errorf("javascript extractor panicked: %v", r)
			}
		}()
		x.walk(root, "", nil)
		x.collectImports(root)
	}()

	// Secondary pass: error-handling patterns.
	// Runs after the primary extraction so a detection failure here
	// cannot abort the primary entity output — extractErrorHandlingPatterns
	// recovers panics internally and returns partial results.
	errorPatterns := extractErrorHandlingPatterns(root, file.Path, file.Language)
	x.entities = append(x.entities, errorPatterns...)

	if extractErr != nil {
		span.RecordError(extractErr)
		span.SetStatus(codes.Error, extractErr.Error())
		// Return partial results on panic.
		return x.entities, extractErr
	}

	span.SetAttributes(
		attribute.Int("entity_count", len(x.entities)),
		attribute.Int("relationship_count", len(x.relationships)),
		attribute.Int("error_pattern_count", len(errorPatterns)),
	)

	// Issue #90 — stamp Properties["language"] (e.g. "javascript" or
	// "typescript") on every embedded relationship so the resolver routes
	// to the right per-language dynamic-pattern catalog.
	extreg.TagRelationshipsLanguage(x.entities, file.Language)

	return x.entities, nil
}

// extractor holds mutable extraction state for a single file.
type extractor struct {
	source        []byte
	filePath      string
	language      string
	entities      []types.EntityRecord
	relationships []types.RelationshipRecord

	// imports / importByLocal — issue #421. Populated once per file
	// before walk() runs. Receiver-typed CALLS emission consults
	// importByLocal[<typeName>] to resolve a typed receiver to the
	// source file declaring the type. importByLocal is nil-safe;
	// callers must check the lookup result.
	imports       []importBinding
	importByLocal map[string]*importBinding

	// aliases — issue #505. The merged per-repo path-alias map loaded
	// from tsconfig.json / vite.config / metro.config / babel.config.
	// Empty for projects without alias declarations; the resolver
	// gracefully no-ops on every lookup in that case.
	aliases AliasMap
	// repoRoot is the absolute path of the repository root, used to
	// filesystem-check alias candidate paths so multi-target aliases
	// (`@/*: ["./*", "./src/*"]`) pick the candidate that actually
	// exists on disk rather than emitting an IMPORTS edge per candidate
	// (which would inflate bug-extractor counts for the wrong ones).
	repoRoot string
}

// applyAlias attempts to substitute a path-alias prefix in spec using
// the extractor's per-repo alias map. Returns the repo-relative
// POSIX path the alias resolves to (without an extension; the caller
// runs the same `.ts → .tsx → .js …` candidate-extension loop a
// relative import would), or "" when no alias matches.
//
// Specs that are already relative (`./` / `../`) or absolute (`/`) are
// bypassed unconditionally — alias substitution is a NON-relative-spec
// concern. Bare npm specs (`react`, `@tanstack/react-query`) fall
// through to the alias lookup, which is correct: `@tanstack/...` is
// also the shape an alias map could use, but in practice the alias
// table's prefix-vs-package-name disambiguation is left to the alias
// declaration itself (a project that aliases `@tanstack` shadows the
// npm package by definition).
func (x *extractor) applyAlias(spec string) string {
	if spec == "" {
		return ""
	}
	if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") || strings.HasPrefix(spec, "/") {
		return ""
	}
	return x.aliases.Resolve(spec)
}

// classBindings tracks receiver-typed identifiers visible inside a
// class body — typed property declarations and constructor parameters
// (TypeScript's "parameter property" shape). Map key is the field /
// parameter name; value is the declared leaf type identifier.
//
// Issue #421 — NestJS @Inject() style:
//
//	class UsersController {
//	  constructor(private readonly userService: UserService) {}
//	  list() { return this.userService.findAll(); }
//	}
//
// `userService` is BOTH a constructor parameter and an implicit class
// field; the binder treats them identically.
type classBindings struct {
	// fields maps field/parameter name → declared type identifier (leaf).
	fields map[string]string
	// className is the enclosing class's name. Empty when we're not
	// inside a class body.
	className string
}

// nodeText returns the UTF-8 text of a tree-sitter node.
func (x *extractor) nodeText(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	start := n.StartByte()
	end := n.EndByte()
	if end > uint32(len(x.source)) {
		end = uint32(len(x.source))
	}
	return string(x.source[start:end])
}

// lines returns (startLine, endLine) for a node, 1-indexed.
func lines(n *sitter.Node) (int, int) {
	start := int(n.StartPoint().Row) + 1
	end := int(n.EndPoint().Row) + 1
	return start, end
}

// emit appends an entity to the extraction results.
func (x *extractor) emit(name, kind string, n *sitter.Node, subtype string, sig string) {
	if name == "" || name == "?" {
		return
	}
	x.emitWithRels(name, kind, n, subtype, sig, nil)
}

// emitWithRels appends an entity to the extraction results carrying the
// supplied embedded relationships.
func (x *extractor) emitWithRels(name, kind string, n *sitter.Node, subtype string, sig string, rels []types.RelationshipRecord) {
	if name == "" || name == "?" {
		return
	}
	start, end := lines(n)
	e := types.EntityRecord{
		Name:       name,
		Kind:       kind,
		SourceFile: x.filePath,
		StartLine:  start,
		EndLine:    end,
		Language:   x.language,
		Subtype:    subtype,
		Signature:  sig,
		Properties: map[string]string{
			"kind":    kind,
			"subtype": subtype,
		},
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
		Relationships:    rels,
	}
	e.ID = e.ComputeID()
	x.entities = append(x.entities, e)
}

// walk performs a depth-first traversal of the CST, dispatching on node type.
// parentClass is non-empty when inside a class body (used to tag methods).
// cb carries the field-type bindings for the enclosing class so receiver-
// typed CALLS edges can resolve `this.<field>.<method>` and `<field>.<method>`
// shapes to the import-declared type (issue #421). cb is nil outside a
// class body.
func (x *extractor) walk(n *sitter.Node, parentClass string, cb *classBindings) {
	if n == nil {
		return
	}

	switch n.Type() {
	case "function_declaration":
		x.handleFunctionDeclaration(n, parentClass, cb)
		return // do not recurse into function body for name extraction

	case "class_declaration":
		x.handleClassDeclaration(n)
		return // recurse is handled inside

	case "method_definition":
		x.handleMethodDefinition(n, parentClass, cb)
		return

	case "interface_declaration":
		x.handleInterfaceDeclaration(n)
		return

	case "type_alias_declaration":
		x.handleTypeAliasDeclaration(n)
		return

	case "lexical_declaration", "variable_declaration":
		// const/let foo = () => {} or = function() {}
		x.handleVariableDeclaration(n, parentClass, cb)
		// still recurse for nested structures at statement level
		return

	case "export_statement":
		// Recurse into the declaration inside the export.
		x.walkChildren(n, parentClass, cb)
		return
	}

	x.walkChildren(n, parentClass, cb)
}

func (x *extractor) walkChildren(n *sitter.Node, parentClass string, cb *classBindings) {
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		x.walk(n.Child(i), parentClass, cb)
	}
}

// handleFunctionDeclaration handles: function foo(...) { ... }
func (x *extractor) handleFunctionDeclaration(n *sitter.Node, parentClass string, cb *classBindings) {
	nameNode := n.ChildByFieldName("name")
	name := x.nodeText(nameNode)
	if name == "" {
		return
	}
	subtype := "function"
	if parentClass != "" {
		subtype = "method"
	}
	sig := fmt.Sprintf("function %s", name)
	body := n.ChildByFieldName("body")
	// Issue #421 — top-level functions can still take typed parameters
	// the receiver binder needs to consult (e.g. `function callIt(svc:
	// UserService) { svc.findOne(); }`). Build a function-scope binding
	// frame from the params node and pass it as the cb override.
	params := n.ChildByFieldName("parameters")
	frame := x.functionParamFrame(params, cb)
	rels := x.extractCallRelationships(body, name, frame)
	x.emitWithRels(name, "SCOPE.Operation", n, subtype, sig, rels)

	// Recurse into the body for nested declarations.
	if body != nil {
		x.walkChildren(body, parentClass, cb)
	}
}

// handleClassDeclaration handles: class Foo { ... }
//
// Emits one CONTAINS edge per method/operation entity declared directly inside
// the class body. The CONTAINS source is the class entity (FromID empty →
// substituted at emit time); the target is the bare method name.
func (x *extractor) handleClassDeclaration(n *sitter.Node) {
	nameNode := n.ChildByFieldName("name")
	className := x.nodeText(nameNode)
	if className == "" {
		return
	}

	// Snapshot the entity slice length before walking the body so we can
	// attribute every operation appended during the walk to this class.
	classIdx := len(x.entities)
	x.emit(className, "SCOPE.Component", n, "class", fmt.Sprintf("class %s", className))

	body := n.ChildByFieldName("body")
	// Issue #421 — collect the class's typed property declarations and
	// constructor parameter properties so receiver-typed CALLS inside
	// any method body can resolve `this.<field>` to the declared type.
	cb := &classBindings{className: className, fields: map[string]string{}}
	if body != nil {
		x.collectClassFields(body, cb.fields)
	}
	if body != nil {
		before := len(x.entities)
		x.walkChildren(body, className, cb)
		after := len(x.entities)
		for k := before; k < after; k++ {
			child := &x.entities[k]
			if child.Kind != "SCOPE.Operation" {
				continue
			}
			// Issue #144 — emit a structural-ref (Format A) keyed on the
			// source file so the resolver disambiguates by location when
			// two classes in different files declare same-named methods
			// (a common shape in Express/Nest/React-component apps).
			toID := extreg.BuildOperationStructuralRef(x.language, x.filePath, child.Name)
			x.entities[classIdx].Relationships = append(x.entities[classIdx].Relationships,
				types.RelationshipRecord{
					ToID: toID,
					Kind: "CONTAINS",
				})
		}
	}
}

// handleMethodDefinition handles method definitions inside class bodies.
func (x *extractor) handleMethodDefinition(n *sitter.Node, _ string, cb *classBindings) {
	nameNode := n.ChildByFieldName("name")
	name := x.nodeText(nameNode)
	if name == "" || name == "constructor" {
		return
	}
	body := n.ChildByFieldName("body")
	// Issue #421 — merge the class's field bindings with the method's
	// own typed parameters so a method param can shadow / extend the
	// receiver-type lookup scope (parameters win on conflict).
	params := n.ChildByFieldName("parameters")
	frame := x.functionParamFrame(params, cb)
	rels := x.extractCallRelationships(body, name, frame)
	x.emitWithRels(name, "SCOPE.Operation", n, "method", fmt.Sprintf("method %s", name), rels)
}

// handleInterfaceDeclaration handles TypeScript interface declarations.
func (x *extractor) handleInterfaceDeclaration(n *sitter.Node) {
	nameNode := n.ChildByFieldName("name")
	name := x.nodeText(nameNode)
	if name == "" {
		return
	}
	x.emit(name, "SCOPE.Schema", n, "interface", fmt.Sprintf("interface %s", name))
}

// handleTypeAliasDeclaration handles TypeScript type aliases: type Foo = ...
func (x *extractor) handleTypeAliasDeclaration(n *sitter.Node) {
	nameNode := n.ChildByFieldName("name")
	name := x.nodeText(nameNode)
	if name == "" {
		return
	}
	x.emit(name, "SCOPE.Schema", n, "type_alias", fmt.Sprintf("type %s", name))
}

// handleVariableDeclaration handles: const/let foo = (...) => {...} or = function(...) {...}
func (x *extractor) handleVariableDeclaration(n *sitter.Node, parentClass string, cb *classBindings) {
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child.Type() == "variable_declarator" {
			x.handleVariableDeclarator(child, parentClass, cb)
		}
	}
}

// handleVariableDeclarator processes a single variable_declarator node.
func (x *extractor) handleVariableDeclarator(n *sitter.Node, parentClass string, cb *classBindings) {
	nameNode := n.ChildByFieldName("name")
	name := x.nodeText(nameNode)
	if name == "" {
		return
	}

	valueNode := n.ChildByFieldName("value")
	if valueNode == nil {
		return
	}

	switch valueNode.Type() {
	case "arrow_function":
		subtype := "function"
		if parentClass != "" {
			subtype = "method"
		}
		body := valueNode.ChildByFieldName("body")
		params := valueNode.ChildByFieldName("parameters")
		frame := x.functionParamFrame(params, cb)
		rels := x.extractCallRelationships(body, name, frame)
		x.emitWithRels(name, "SCOPE.Operation", valueNode, subtype, fmt.Sprintf("const %s = (...) =>", name), rels)
		if body != nil {
			x.walkChildren(body, parentClass, cb)
		}

	case "function", "function_expression":
		subtype := "function"
		if parentClass != "" {
			subtype = "method"
		}
		body := valueNode.ChildByFieldName("body")
		params := valueNode.ChildByFieldName("parameters")
		frame := x.functionParamFrame(params, cb)
		rels := x.extractCallRelationships(body, name, frame)
		x.emitWithRels(name, "SCOPE.Operation", valueNode, subtype, fmt.Sprintf("const %s = function", name), rels)
		if body != nil {
			x.walkChildren(body, parentClass, cb)
		}

	default:
		// Issue #522 — every other `const X = <expr>` shape currently
		// produces no entity, so alias-resolved imports targeting these
		// consts land in bug-extractor. Emit a value-export entity so the
		// resolver has something to bind to.
		//
		// Two refinements on top of the bare emit:
		//   1. React/MobX/Redux wrapper-call values that wrap a function
		//      (forwardRef, memo, observer, styled.x``, withRouter, …)
		//      get classified as SCOPE.Operation so existing
		//      function-targeted resolver paths apply. The wrapper's
		//      inner function body is walked for CALLS so the const's
		//      relationships mirror what `export function X` would emit.
		//   2. Plain values (objects, primitives, instances) become
		//      SCOPE.Component subtype="const" — the same shape the
		//      import-resolver expects for module-level value bindings.
		//
		// We always recurse into the value so nested function expressions
		// (e.g. inside `createSlice({ reducers: { add(state) {...} }})`)
		// still get walked.
		if x.isFunctionWrapperCall(valueNode) {
			subtype := "function"
			if parentClass != "" {
				subtype = "method"
			}
			// Find an inner arrow/function expression to attribute
			// CALLS to. Fall back to walking the entire value node
			// when the inner shape isn't a literal function (e.g.
			// `forwardRef(someExternalRef)`).
			inner := x.findInnerFunctionBody(valueNode)
			frame := x.functionParamFrame(nil, cb)
			rels := x.extractCallRelationships(valueNode, name, frame)
			_ = inner
			x.emitWithRels(name, "SCOPE.Operation", valueNode, subtype, fmt.Sprintf("const %s = <wrapper>", name), rels)
		} else {
			subtype := constValueSubtype(valueNode.Type())
			x.emit(name, "SCOPE.Component", valueNode, subtype, fmt.Sprintf("const %s", name))
		}
		// Recurse so nested function/class declarations inside the value
		// (object methods, callbacks, JSX children, …) still get emitted.
		x.walkChildren(valueNode, parentClass, cb)
	}
}

// isFunctionWrapperCall returns true when valueNode is a call_expression
// whose callee is one of the well-known React / MobX / Redux / Recoil /
// styled-components / MobX-react function wrappers. We treat the bound
// name as a function (SCOPE.Operation) in that case so the resolver's
// function-targeted edges apply.
//
// The match is intentionally conservative — we only recognise wrappers
// whose semantic IS "this value is a function" (forwardRef returns a
// component, memo returns a component, observer returns a component,
// styled.* returns a component, withRouter wraps a component, connect()
// returns a component, createSlice().reducer is a function, etc.). For
// the dotted shapes (`styled.div`, `createSlice(...).reducer`,
// `Animated.createAnimatedComponent(...)`) we walk down the function
// child to find the leaf identifier.
func (x *extractor) isFunctionWrapperCall(n *sitter.Node) bool {
	if n == nil || n.Type() != "call_expression" {
		return false
	}
	fn := n.ChildByFieldName("function")
	if fn == nil {
		return false
	}
	leaf := ""
	switch fn.Type() {
	case "identifier", "type_identifier":
		leaf = x.nodeText(fn)
	case "member_expression":
		if prop := fn.ChildByFieldName("property"); prop != nil {
			leaf = x.nodeText(prop)
		}
	case "call_expression":
		// e.g. styled(Foo)`...` → recurse on inner call
		return x.isFunctionWrapperCall(fn)
	}
	switch leaf {
	case
		// React
		"forwardRef", "memo", "lazy", "createContext",
		// MobX-react / MobX
		"observer",
		// styled-components / emotion
		"styled", "css", "keyframes",
		// React Router HOCs
		"withRouter", "withTranslation", "withTheme", "withStyles",
		// Redux / Recoil / Zustand selectors
		"connect", "createSelector", "createStructuredSelector",
		// React Native Animated
		"createAnimatedComponent",
		// HOC-shape utilities
		"compose", "pipe",
		// React Query / TanStack
		"createMutation", "createQuery":
		return true
	}
	return false
}

// findInnerFunctionBody returns the innermost arrow_function /
// function_expression body inside a wrapper-call value, or nil. Used
// to attribute CALLS to the wrapped function rather than the wrapper
// call expression as a whole.
func (x *extractor) findInnerFunctionBody(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if n.Type() == "arrow_function" || n.Type() == "function_expression" || n.Type() == "function" {
		return n.ChildByFieldName("body")
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if b := x.findInnerFunctionBody(n.Child(i)); b != nil {
			return b
		}
	}
	return nil
}

// constValueSubtype maps a tree-sitter value-node type to a stable
// subtype string for `export const X = <value>` entity emission. The
// subtype is informational — the resolver keys on Kind + Name + file,
// not on subtype — but a stable string keeps debugging tractable.
func constValueSubtype(nodeType string) string {
	switch nodeType {
	case "object":
		return "const_object"
	case "array":
		return "const_array"
	case "string", "template_string", "number", "true", "false", "null", "undefined":
		return "const_literal"
	case "new_expression":
		return "const_instance"
	case "call_expression":
		return "const_call"
	case "jsx_element", "jsx_self_closing_element":
		return "const_jsx"
	case "member_expression", "subscript_expression":
		return "const_reference"
	case "identifier":
		return "const_alias"
	}
	return "const"
}

// extractCallRelationships returns one CALLS RelationshipRecord per unique
// call_expression / new_expression descendant of body. The target name is
// computed from the function child of the call:
//
//	identifier               → bare name      (e.g. "foo")
//	member_expression a.b.c  → trailing prop  (e.g. "c"), or — when the
//	                           receiver chain types via class fields /
//	                           function parameters AND the type was
//	                           imported from a relative path — a Format A
//	                           structural-ref keyed on the imported file
//	                           (issue #421).
//	parenthesized_expression → inner target   (best-effort)
//
// FromID is left empty so buildDocument substitutes the caller's entity ID
// at emit time. ToID is either a bare callee name OR a structural-ref
// stub. Self-recursion is dropped on the BARE form only — a structural-
// ref to the same name in another file is a legitimate cross-file CALLS
// edge and must survive.
//
// frame carries the receiver-type bindings visible in the caller's scope:
// merged class fields (from the enclosing class body) + the caller's own
// typed parameters. nil means "no typed receiver lookup possible".
func (x *extractor) extractCallRelationships(body *sitter.Node, callerName string, frame *classBindings) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	calls := findAllNodes(body, "call_expression", "new_expression")
	if len(calls) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(calls))
	rels := make([]types.RelationshipRecord, 0, len(calls))
	for _, call := range calls {
		target := x.callTarget(call, frame)
		if target == "" || target == "require" {
			continue
		}
		// Self-recursion drop applies to the bare leaf only. A
		// structural-ref dotted target whose leaf matches callerName
		// would still be a legitimate cross-file binding (the leaf
		// happens to share a name across files), so we keep it.
		if !strings.Contains(target, ":") && target == callerName {
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		rels = append(rels, types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
		})
	}
	return rels
}

// callTarget resolves the callee name from a call_expression / new_expression.
// Returns the trailing identifier of the function expression, or "" when the
// callee is an unsupported expression form (numeric literal, complex
// destructuring, etc.).
//
// Issue #421 — when the function child is a member_expression of the form
// `<receiver>.<method>` AND the receiver types via the supplied frame to
// a relatively-imported class, callTarget returns a Format A structural-
// ref ("scope:operation:method:<lang>:<resolved_file>:<method>") instead
// of the bare trailing identifier. This lets the resolver bind the call
// to the imported class's method without going through bare-name lookup.
func (x *extractor) callTarget(call *sitter.Node, frame *classBindings) string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		// new_expression uses "constructor" field.
		fn = call.ChildByFieldName("constructor")
	}
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier", "type_identifier", "property_identifier":
		name := x.nodeText(fn)
		// Refs #44 — bare named-import shape: `import { join } from
		// "path"` then `join(...)`. The leaf identifier binds to a
		// Node.js stdlib import; route the call to the matching
		// `ext:node:<module>` placeholder via the cross-language
		// `:external:` synth path. Miss falls through to bare name.
		if id := x.classifyBareNodeStdlibCall(name); id != "" {
			return id
		}
		return name
	case "member_expression":
		prop := fn.ChildByFieldName("property")
		if prop == nil {
			return ""
		}
		method := x.nodeText(prop)
		// Issue #421 — try receiver typing first. The lookup walks
		// `<recv>.<method>` and `this.<recv>.<method>` shapes; on a
		// hit it returns the structural-ref keyed on the imported
		// source file. On a miss we fall through to the bare method
		// name (current behaviour).
		if id := x.receiverTypedTarget(fn, method, frame); id != "" {
			return id
		}
		// Refs #44 — Node.js stdlib namespace shape: `import * as
		// path from "path"` (or `import fs from "node:fs"`) followed
		// by `path.join(...)` / `fs.readFileSync(...)`. The receiver
		// binds to a Node stdlib import spec; route the call to the
		// matching `ext:node:<module>` placeholder. Miss falls through
		// to the bare method name (existing behaviour preserved).
		if id := x.receiverNodeStdlibTarget(fn, method); id != "" {
			return id
		}
		return method
	case "parenthesized_expression":
		for i := 0; i < int(fn.ChildCount()); i++ {
			ch := fn.Child(i)
			if ch.Type() == "identifier" {
				return x.nodeText(ch)
			}
			if ch.Type() == "member_expression" {
				if p := ch.ChildByFieldName("property"); p != nil {
					return x.nodeText(p)
				}
			}
		}
	}
	return ""
}

// findAllNodes returns every descendant of root whose Type() is in kinds.
// Iterative to stay safe on deeply-nested trees.
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

// collectImports scans the tree for ES6 import statements and CommonJS
// require() calls, emitting a SCOPE.Component entity per unique module.
//
// Issue #421 — IMPORTS edges now carry the per-binding property
// contract Python (#93) and Java (#120) emit so the cross-file
// resolver pre-pass can build a per-file binding table:
//
//	Properties["local_name"]    — identifier introduced into the file
//	Properties["source_module"] — canonical dotted module path
//	Properties["imported_name"] — original symbol name (pre-alias)
//	Properties["wildcard"]      — "1" for `import * as ns from "..."`
//
// One IMPORTS edge per BINDING is emitted (so `import { A, B } from
// "mod"` produces two edges); the parent SCOPE.Component entity is
// shared across bindings of the same module so the existing dedup
// shape is preserved.
func (x *extractor) collectImports(root *sitter.Node) {
	seen := make(map[string]bool)
	// Group import bindings by module spec so we can attach all
	// bindings as separate IMPORTS edges on a single import entity.
	bindingsByModule := map[string][]*importBinding{}
	for i := range x.imports {
		b := &x.imports[i]
		bindingsByModule[b.importPath] = append(bindingsByModule[b.importPath], b)
	}
	x.collectImportsNode(root, seen, bindingsByModule)
}

func (x *extractor) collectImportsNode(n *sitter.Node, seen map[string]bool, bindingsByModule map[string][]*importBinding) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "import_statement":
		// ES6: import foo from "module" / import { x } from "module"
		// The string node is a direct child.
		count := int(n.ChildCount())
		for i := 0; i < count; i++ {
			child := n.Child(i)
			if child.Type() == "string" {
				raw := x.nodeText(child)
				module := strings.Trim(raw, `"'`+"`")
				if module != "" && !seen[module] {
					seen[module] = true
					x.emitImport(module, n, bindingsByModule[module])
				}
			}
		}
		return // do not recurse

	case "call_expression":
		// CommonJS: require("module")
		funcNode := n.ChildByFieldName("function")
		if funcNode != nil && x.nodeText(funcNode) == "require" {
			argsNode := n.ChildByFieldName("arguments")
			if argsNode != nil {
				count := int(argsNode.ChildCount())
				for i := 0; i < count; i++ {
					arg := argsNode.Child(i)
					if arg.Type() == "string" {
						raw := x.nodeText(arg)
						module := strings.Trim(raw, `"'`+"`")
						if module != "" && !seen[module] {
							seen[module] = true
							x.emitImport(module, n, nil)
						}
						break
					}
				}
			}
		}
	}

	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		x.collectImportsNode(n.Child(i), seen, bindingsByModule)
	}
}

// emitImport emits a SCOPE.Component entity for an imported module.
//
// Issue #421 — when the module's import_statement introduced one or
// more named/default/namespace bindings, every binding becomes its own
// IMPORTS RelationshipRecord on the entity, carrying the property
// contract the cross-file resolver pre-pass consumes. Side-effect-only
// imports (`import "./polyfills";`) and CommonJS require() calls
// without destructuring fall back to a single IMPORTS edge with no
// per-binding properties so existing downstream consumers still see
// at least one edge per module.
func (x *extractor) emitImport(module string, n *sitter.Node, bindings []*importBinding) {
	// Use the full module path as the entity name for parity with Python indexer.
	start, end := lines(n)
	rels := make([]types.RelationshipRecord, 0, max1(len(bindings)))
	if len(bindings) == 0 {
		rels = append(rels, types.RelationshipRecord{
			FromID: x.filePath,
			ToID:   module,
			Kind:   "IMPORTS",
		})
	} else {
		for _, b := range bindings {
			props := map[string]string{
				"local_name":    b.localName,
				"source_module": b.sourceModule,
				"imported_name": b.importedName,
				"import_path":   b.importPath,
			}
			if b.wildcard {
				props["wildcard"] = "1"
			}
			if b.resolvedFile != "" {
				props["resolved_file"] = b.resolvedFile
			}
			// Issue #505 — for ALIAS-resolved imports, switch the
			// IMPORTS ToID from the raw alias spec (`@/src/store/foo`,
			// which the resolver can't bind because it lacks the
			// dotted-module shape ResolveDottedImportTarget requires)
			// to the dotted-module + leaf form
			// (`src.store.foo.<importedName>`). The resolver splits on
			// the last dot and looks up (module, leaf) against the
			// per-module reverse index. Plain relative imports keep the
			// legacy raw-spec ToID so pre-#505 disposition shapes on
			// the existing TS/JS corpora (express, nestjs, etc.) are
			// preserved bit-for-bit.
			toID := module
			if b.aliasResolved && b.sourceModule != "" && b.importedName != "" {
				toID = b.sourceModule + "." + b.importedName
			}
			rels = append(rels, types.RelationshipRecord{
				FromID:     x.filePath,
				ToID:       toID,
				Kind:       "IMPORTS",
				Properties: props,
			})
		}
	}
	e := types.EntityRecord{
		Name:       module,
		Kind:       "SCOPE.Component",
		SourceFile: x.filePath,
		StartLine:  start,
		EndLine:    end,
		Language:   x.language,
		Subtype:    "import",
		Properties: map[string]string{
			"kind":     "SCOPE.Component",
			"subtype":  "import",
			"module":   module,
			"is_local": boolStr(strings.HasPrefix(module, ".")),
		},
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
		Relationships:    rels,
	}
	e.ID = e.ComputeID()
	x.entities = append(x.entities, e)
}

// max1 returns the larger of n and 1. Used to pre-size relationship
// slices without ever zero-allocating when n is 0.
func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// boolStr returns "true" or "false" as a string.
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
