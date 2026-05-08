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
	}

	var extractErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				extractErr = fmt.Errorf("javascript extractor panicked: %v", r)
			}
		}()
		x.walk(root, "")
		x.collectImports(root)
	}()

	// Secondary pass: error-handling patterns (MX-1047).
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

	return x.entities, nil
}

// extractor holds mutable extraction state for a single file.
type extractor struct {
	source        []byte
	filePath      string
	language      string
	entities      []types.EntityRecord
	relationships []types.RelationshipRecord
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
func (x *extractor) walk(n *sitter.Node, parentClass string) {
	if n == nil {
		return
	}

	switch n.Type() {
	case "function_declaration":
		x.handleFunctionDeclaration(n, parentClass)
		return // do not recurse into function body for name extraction

	case "class_declaration":
		x.handleClassDeclaration(n)
		return // recurse is handled inside

	case "method_definition":
		x.handleMethodDefinition(n, parentClass)
		return

	case "interface_declaration":
		x.handleInterfaceDeclaration(n)
		return

	case "type_alias_declaration":
		x.handleTypeAliasDeclaration(n)
		return

	case "lexical_declaration", "variable_declaration":
		// const/let foo = () => {} or = function() {}
		x.handleVariableDeclaration(n, parentClass)
		// still recurse for nested structures at statement level
		return

	case "export_statement":
		// Recurse into the declaration inside the export.
		x.walkChildren(n, parentClass)
		return
	}

	x.walkChildren(n, parentClass)
}

func (x *extractor) walkChildren(n *sitter.Node, parentClass string) {
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		x.walk(n.Child(i), parentClass)
	}
}

// handleFunctionDeclaration handles: function foo(...) { ... }
func (x *extractor) handleFunctionDeclaration(n *sitter.Node, parentClass string) {
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
	rels := x.extractCallRelationships(body, name)
	x.emitWithRels(name, "SCOPE.Operation", n, subtype, sig, rels)

	// Recurse into the body for nested declarations.
	if body != nil {
		x.walkChildren(body, parentClass)
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
	if body != nil {
		before := len(x.entities)
		x.walkChildren(body, className)
		after := len(x.entities)
		for k := before; k < after; k++ {
			child := &x.entities[k]
			if child.Kind != "SCOPE.Operation" {
				continue
			}
			x.entities[classIdx].Relationships = append(x.entities[classIdx].Relationships,
				types.RelationshipRecord{
					ToID: child.Name,
					Kind: "CONTAINS",
				})
		}
	}
}

// handleMethodDefinition handles method definitions inside class bodies.
func (x *extractor) handleMethodDefinition(n *sitter.Node, _ string) {
	nameNode := n.ChildByFieldName("name")
	name := x.nodeText(nameNode)
	if name == "" || name == "constructor" {
		return
	}
	body := n.ChildByFieldName("body")
	rels := x.extractCallRelationships(body, name)
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
func (x *extractor) handleVariableDeclaration(n *sitter.Node, parentClass string) {
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child.Type() == "variable_declarator" {
			x.handleVariableDeclarator(child, parentClass)
		}
	}
}

// handleVariableDeclarator processes a single variable_declarator node.
func (x *extractor) handleVariableDeclarator(n *sitter.Node, parentClass string) {
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
		rels := x.extractCallRelationships(body, name)
		x.emitWithRels(name, "SCOPE.Operation", valueNode, subtype, fmt.Sprintf("const %s = (...) =>", name), rels)
		if body != nil {
			x.walkChildren(body, parentClass)
		}

	case "function", "function_expression":
		subtype := "function"
		if parentClass != "" {
			subtype = "method"
		}
		body := valueNode.ChildByFieldName("body")
		rels := x.extractCallRelationships(body, name)
		x.emitWithRels(name, "SCOPE.Operation", valueNode, subtype, fmt.Sprintf("const %s = function", name), rels)
		if body != nil {
			x.walkChildren(body, parentClass)
		}
	}
}

// extractCallRelationships returns one CALLS RelationshipRecord per unique
// call_expression / new_expression descendant of body. The target name is
// computed from the function child of the call:
//
//	identifier               → bare name      (e.g. "foo")
//	member_expression a.b.c  → trailing prop  (e.g. "c")
//	parenthesized_expression → inner target   (best-effort)
//
// FromID is left empty so buildDocument substitutes the caller's entity ID
// at emit time. ToID is the bare callee name; the resolver rewrites
// cross-file references in pass 5. Self-recursion is dropped to match the
// Python and Go extractor dedup semantics. The require() call form is
// excluded so it does not double-count as both a call and an import.
func (x *extractor) extractCallRelationships(body *sitter.Node, callerName string) []types.RelationshipRecord {
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
		target := x.callTarget(call)
		if target == "" || target == callerName || target == "require" {
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
func (x *extractor) callTarget(call *sitter.Node) string {
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
		return x.nodeText(fn)
	case "member_expression":
		prop := fn.ChildByFieldName("property")
		if prop != nil {
			return x.nodeText(prop)
		}
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
func (x *extractor) collectImports(root *sitter.Node) {
	seen := make(map[string]bool)
	x.collectImportsNode(root, seen)
}

func (x *extractor) collectImportsNode(n *sitter.Node, seen map[string]bool) {
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
					x.emitImport(module, n)
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
							x.emitImport(module, n)
						}
						break
					}
				}
			}
		}
	}

	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		x.collectImportsNode(n.Child(i), seen)
	}
}

// emitImport emits a SCOPE.Component entity for an imported module.
func (x *extractor) emitImport(module string, n *sitter.Node) {
	// Use the full module path as the entity name for parity with Python indexer.
	start, end := lines(n)
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
		Relationships: []types.RelationshipRecord{
			{
				FromID: x.filePath,
				ToID:   module,
				Kind:   "IMPORTS",
			},
		},
	}
	e.ID = e.ComputeID()
	x.entities = append(x.entities, e)
}

// boolStr returns "true" or "false" as a string.
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
