// Package python implements the tree-sitter–based extractor for Python source files.
//
// Extracted entities (maps to Python indexer scope_mapping.py):
//   - function_definition  → Kind="SCOPE.Operation", Subtype="function"
//   - decorated_definition wrapping function_definition → same kind (decorators are
//     not emitted by the base extractor; framework extractors handle those separately)
//   - class_definition     → Kind="SCOPE.Component"
//   - methods in class     → Kind="SCOPE.Operation", Subtype="method"
//   - import_statement / import_from_statement → Kind="SCOPE.Component" (module)
//
// Embedded relationships (PORT-2-FIX-2 / issue #25):
//   - CONTAINS:  class    → method   (one per method declared inside a class body)
//   - CALLS:     function → callee   (bare-name target, resolver rewrites cross-file)
//   - IMPORTS:   file     → module   (one per import path)
//
// QualifiedName is left empty (null in JSON) for all entities, matching the
// Python base parser. Framework-specific qualified names are added by later passes.
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package python

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	tspython "github.com/smacker/go-tree-sitter/python"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("python", &Extractor{})
}

// Extractor implements extractors.Extractor for Python.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "python" }

// Extract walks the tree-sitter CST and returns entity records for the Python file.
//
// OTel span "extractor.python" is emitted with attributes: file, entity_count,
// function_count, class_count.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor.python")
	ctx, span := tracer.Start(ctx, "extractor.python")
	defer span.End()

	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		span.SetAttributes(
			attribute.Int("entity_count", 0),
			attribute.Int("function_count", 0),
			attribute.Int("class_count", 0),
		)
		return nil, nil
	}

	// Parse with a fresh parser when tree is nil (e.g. in tests or malformed input).
	tree := file.Tree
	if tree == nil {
		parser := sitter.NewParser()
		parser.SetLanguage(tspython.GetLanguage())
		var parseErr error
		tree, parseErr = parser.ParseCtx(ctx, nil, file.Content)
		if parseErr != nil {
			return nil, fmt.Errorf("python extractor: parse failed: %w", parseErr)
		}
	}

	root := tree.RootNode()

	var (
		entities      []types.EntityRecord
		functionCount int
		classCount    int
	)

	// Walk top-level children.
	walkNode(root, file, "", &entities, &functionCount, &classCount)

	// Secondary pass: error-handling patterns (MX-1047).
	// Runs after the base walker so a failure here cannot abort the
	// primary entity output — extractErrorHandlingPatterns recovers
	// panics internally and returns partial results.
	errorPatterns := extractErrorHandlingPatterns(root, file.Path)
	entities = append(entities, errorPatterns...)

	// Imports — emitted as standalone module entities each carrying a
	// single IMPORTS relationship (file → module). Mirrors the Go
	// extractor's import_spec handling.
	importEnts := extractImports(root, file)
	entities = append(entities, importEnts...)

	span.SetAttributes(
		attribute.Int("entity_count", len(entities)),
		attribute.Int("function_count", functionCount),
		attribute.Int("class_count", classCount),
		attribute.Int("error_pattern_count", len(errorPatterns)),
		attribute.Int("import_count", len(importEnts)),
	)
	return entities, nil
}

// walkNode performs a depth-first traversal of the CST, collecting entities.
// parentClass is "" when outside a class body, or the class name when inside.
func walkNode(
	node *sitter.Node,
	file extractor.FileInput,
	parentClass string,
	out *[]types.EntityRecord,
	funcCount *int,
	classCount *int,
) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "class_definition":
		rec := buildClass(node, file)
		if rec.Name != "" {
			classIdx := len(*out)
			*out = append(*out, rec)
			*classCount++
			// Walk the class body so methods are captured with parentClass set.
			body := node.ChildByFieldName("body")
			if body != nil {
				before := len(*out)
				for i := range int(body.ChildCount()) {
					walkNode(body.Child(i), file, rec.Name, out, funcCount, classCount)
				}
				// Emit CONTAINS edges from the class to every operation entity
				// the walker just appended (methods inside this class).
				after := len(*out)
				for k := before; k < after; k++ {
					child := &(*out)[k]
					if child.Kind != "SCOPE.Operation" {
						continue
					}
					(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
						types.RelationshipRecord{
							// FromID empty → buildDocument substitutes the
							// parent (class) entity ID at emit time.
							ToID: child.Name,
							Kind: "CONTAINS",
						})
				}
			}
		}
		return // body handled above — do not recurse further

	case "function_definition":
		rec := buildFunction(node, file, parentClass)
		if rec.Name != "" {
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(node.ChildByFieldName("body"), file.Content, rec.Name)...)
			*out = append(*out, rec)
			*funcCount++
		}
		return // do not recurse into function body for nested definitions

	case "decorated_definition":
		// A decorated_definition wraps a function_definition or class_definition.
		// The base Python parser does not emit decorator info — framework extractors
		// (FastAPI, Flask, etc.) handle decorator-specific extraction in later passes.
		inner := node.ChildByFieldName("definition")
		if inner == nil {
			return
		}
		switch inner.Type() {
		case "function_definition":
			rec := buildFunction(inner, file, parentClass)
			if rec.Name != "" {
				rec.Relationships = append(rec.Relationships,
					extractCallRelationships(inner.ChildByFieldName("body"), file.Content, rec.Name)...)
				*out = append(*out, rec)
				*funcCount++
			}
		case "class_definition":
			rec := buildClass(inner, file)
			if rec.Name != "" {
				classIdx := len(*out)
				*out = append(*out, rec)
				*classCount++
				body := inner.ChildByFieldName("body")
				if body != nil {
					before := len(*out)
					for i := range int(body.ChildCount()) {
						walkNode(body.Child(i), file, rec.Name, out, funcCount, classCount)
					}
					after := len(*out)
					for k := before; k < after; k++ {
						child := &(*out)[k]
						if child.Kind != "SCOPE.Operation" {
							continue
						}
						(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
							types.RelationshipRecord{
								ToID: child.Name,
								Kind: "CONTAINS",
							})
					}
				}
			}
		}
		return

	default:
		// Recurse into all other node types.
		for i := range int(node.ChildCount()) {
			walkNode(node.Child(i), file, parentClass, out, funcCount, classCount)
		}
	}
}

// buildClass constructs a SCOPE.Component EntityRecord for a class_definition node.
func buildClass(node *sitter.Node, file extractor.FileInput) types.EntityRecord {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return types.EntityRecord{}
	}
	name := nodeText(nameNode, file.Content)

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Component",
		Language:           "python",
		SourceFile:         file.Path,
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "class " + name,
		EnrichmentRequired: false,
	}
}

// buildFunction constructs a SCOPE.Operation EntityRecord for a function_definition.
// parentClass is "" for module-level functions, or the class name for methods.
func buildFunction(node *sitter.Node, file extractor.FileInput, parentClass string) types.EntityRecord {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return types.EntityRecord{}
	}
	name := nodeText(nameNode, file.Content)

	subtype := "function"
	if parentClass != "" {
		subtype = "method"
	}

	params := ""
	paramsNode := node.ChildByFieldName("parameters")
	if paramsNode != nil {
		params = nodeText(paramsNode, file.Content)
	}

	returnType := ""
	retNode := node.ChildByFieldName("return_type")
	if retNode != nil {
		returnType = " -> " + nodeText(retNode, file.Content)
	}

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            subtype,
		Language:           "python",
		SourceFile:         file.Path,
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "def " + name + params + returnType,
		EnrichmentRequired: false,
	}
}

// extractCallRelationships returns one CALLS RelationshipRecord per unique
// `call` node descended from body. The target name is computed from the
// `function` child of the call node:
//
//	identifier               → bare name           (e.g. "open")
//	attribute (a.b.c)        → trailing attribute  (e.g. "c")
//	parenthesized_expression → inner identifier    (best-effort)
//
// FromID is left empty so buildDocument substitutes the caller's entity ID
// at emit time. ToID is the bare callee name, which the resolver rewrites
// to a deterministic ID when an unambiguous same-named entity exists in the
// merged index. Self-recursion is dropped to match the Go extractor and
// the original Python indexer dedup semantics.
func extractCallRelationships(body *sitter.Node, src []byte, callerName string) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	calls := findAll(body, "call")
	if len(calls) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(calls))
	rels := make([]types.RelationshipRecord, 0, len(calls))
	for _, call := range calls {
		target := callTarget(call, src)
		if target == "" || target == callerName {
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

// callTarget resolves the callee name from a tree-sitter Python `call` node.
// Returns the trailing identifier of the function expression, or "" if the
// node has no resolvable function child.
func callTarget(call *sitter.Node, src []byte) string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier":
		return nodeText(fn, src)
	case "attribute":
		// Use the rightmost attribute identifier (mirrors the Go extractor's
		// "fmt.Println" → "Println" rule).
		attr := fn.ChildByFieldName("attribute")
		if attr != nil {
			return nodeText(attr, src)
		}
	case "parenthesized_expression":
		for i := 0; i < int(fn.ChildCount()); i++ {
			ch := fn.Child(i)
			if ch.Type() == "identifier" {
				return nodeText(ch, src)
			}
			if ch.Type() == "attribute" {
				if a := ch.ChildByFieldName("attribute"); a != nil {
					return nodeText(a, src)
				}
			}
		}
	}
	return ""
}

// extractImports walks the parse tree for top-level Python import statements
// and returns one SCOPE.Component entity per imported module path. Each
// entity carries one IMPORTS relationship (FromID=file path, ToID=module).
//
// Two grammar shapes are handled:
//
//	import a, b.c [as alias]                     → import_statement
//	from x.y import a, b [as alias]              → import_from_statement
//
// `from x import a` creates one entity per imported name with module path
// "x.a", matching the symbol-level granularity Python uses for cross-module
// resolution.
func extractImports(root *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	if root == nil {
		return nil
	}
	var out []types.EntityRecord
	for _, n := range findAll(root, "import_statement") {
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			path := dottedNamePath(ch, file.Content)
			if path == "" {
				continue
			}
			out = append(out, importRecord(path, file))
		}
	}
	for _, n := range findAll(root, "import_from_statement") {
		modNode := n.ChildByFieldName("module_name")
		modPath := dottedNamePath(modNode, file.Content)
		if modPath == "" {
			continue
		}
		// Imported names — child types: dotted_name, aliased_import,
		// wildcard_import. We collect each as "<module>.<name>" so the
		// resolver can pick up the leaf symbol.
		emittedAny := false
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			if ch == modNode {
				continue
			}
			name := dottedNamePath(ch, file.Content)
			if name == "" || name == "*" {
				continue
			}
			out = append(out, importRecord(modPath+"."+name, file))
			emittedAny = true
		}
		if !emittedAny {
			out = append(out, importRecord(modPath, file))
		}
	}
	return out
}

// dottedNamePath flattens a dotted_name / identifier / aliased_import node into
// its source-text path. Aliases are stripped (only the underlying name is used).
// Returns "" for unrecognised node shapes.
func dottedNamePath(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	switch node.Type() {
	case "identifier", "dotted_name":
		return strings.TrimSpace(nodeText(node, src))
	case "aliased_import":
		if name := node.ChildByFieldName("name"); name != nil {
			return dottedNamePath(name, src)
		}
		if node.NamedChildCount() > 0 {
			return dottedNamePath(node.NamedChild(0), src)
		}
	case "relative_import":
		// e.g. ".foo" or "..bar" — keep raw text so the resolver can match.
		return strings.TrimSpace(nodeText(node, src))
	case "wildcard_import":
		return "*"
	}
	return ""
}

// importRecord builds a single SCOPE.Component entity for the given module
// path with one embedded IMPORTS relationship.
func importRecord(modulePath string, file extractor.FileInput) types.EntityRecord {
	return types.EntityRecord{
		Name:       modulePath,
		Kind:       "SCOPE.Component",
		Subtype:    "module",
		SourceFile: file.Path,
		Language:   "python",
		Relationships: []types.RelationshipRecord{
			{
				FromID: file.Path,
				ToID:   modulePath,
				Kind:   "IMPORTS",
			},
		},
	}
}

// findAll returns every descendant of root whose Type() matches kind.
// Recursion is iterative to stay safe on deeply-nested trees.
func findAll(root *sitter.Node, kind string) []*sitter.Node {
	if root == nil {
		return nil
	}
	var out []*sitter.Node
	stack := []*sitter.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		if n.Type() == kind {
			out = append(out, n)
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return out
}

// nodeText returns the raw source bytes for a tree-sitter node as a string.
func nodeText(node *sitter.Node, src []byte) string {
	return string(src[node.StartByte():node.EndByte()])
}
