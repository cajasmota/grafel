// Package python implements the tree-sitter–based extractor for Python source files.
//
// Extracted entities (maps to Python indexer scope_mapping.py):
//   - function_definition  → Kind="SCOPE.Operation", Subtype="function"
//   - decorated_definition wrapping function_definition → same kind (decorators are
//     not emitted by the base extractor; framework extractors handle those separately)
//   - class_definition     → Kind="SCOPE.Component", Subtype="class"
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

	// Secondary pass: error-handling patterns.
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
// parentClass is "" when outside a class body, or the dotted class path when
// inside (e.g. "Outer" for a top-level class, "Outer.Inner" for a nested one).
// Issue #68 — multi-level nesting is preserved by appending each enclosing
// class name with a "." separator as the walker descends.
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
			// For nested classes the parent path accumulates: "Outer" → "Outer.Inner".
			childParent := rec.Name
			if parentClass != "" {
				childParent = parentClass + "." + rec.Name
			}
			body := node.ChildByFieldName("body")
			if body != nil {
				before := len(*out)
				for i := range int(body.ChildCount()) {
					walkNode(body.Child(i), file, childParent, out, funcCount, classCount)
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
			// Self-recursion is identified by the bare function name, not the
			// class-qualified Name. nameNode is the leaf identifier.
			selfName := rec.Name
			if nameNode := node.ChildByFieldName("name"); nameNode != nil {
				selfName = nodeText(nameNode, file.Content)
			}
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(node.ChildByFieldName("body"), file.Content, selfName, parentClass)...)
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
				selfName := rec.Name
				if nameNode := inner.ChildByFieldName("name"); nameNode != nil {
					selfName = nodeText(nameNode, file.Content)
				}
				rec.Relationships = append(rec.Relationships,
					extractCallRelationships(inner.ChildByFieldName("body"), file.Content, selfName, parentClass)...)
				*out = append(*out, rec)
				*funcCount++
			}
		case "class_definition":
			rec := buildClass(inner, file)
			if rec.Name != "" {
				classIdx := len(*out)
				*out = append(*out, rec)
				*classCount++
				childParent := rec.Name
				if parentClass != "" {
					childParent = parentClass + "." + rec.Name
				}
				body := inner.ChildByFieldName("body")
				if body != nil {
					before := len(*out)
					for i := range int(body.ChildCount()) {
						walkNode(body.Child(i), file, childParent, out, funcCount, classCount)
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
		Subtype:            "class",
		Language:           "python",
		SourceFile:         file.Path,
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "class " + name,
		EnrichmentRequired: false,
	}
}

// buildFunction constructs a SCOPE.Operation EntityRecord for a function_definition.
// parentClass is "" for module-level functions, or the dotted class path for
// methods (e.g. "Foo" for a top-level class method, "Outer.Inner" for a method
// defined on a nested class — issue #68).
//
// Methods are emitted with Name="<dotted.class.path>.<method>" (issue #45 +
// issue #68) so two classes declaring a same-named method in the same file
// produce distinct entity IDs via ComputeID(SourceFile+Kind+Name). The dotted
// form is the same encoding used by Format B structural references and is
// indexed natively by resolve.Index.byMember, which splits Name on the LAST
// '.' to preserve multi-level scopes.
func buildFunction(node *sitter.Node, file extractor.FileInput, parentClass string) types.EntityRecord {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return types.EntityRecord{}
	}
	name := nodeText(nameNode, file.Content)

	subtype := "function"
	emittedName := name
	if parentClass != "" {
		subtype = "method"
		emittedName = parentClass + "." + name
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
		Name:               emittedName,
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
// at emit time. ToID is the bare callee name when no receiver type can be
// inferred — the resolver rewrites it to a deterministic ID when an
// unambiguous same-named entity exists in the merged index.
//
// Issue #69 — when the call has an inferable receiver type the target is
// emitted in dotted "<Class>.<method>" form so the resolver can bind the
// edge to the correct method entity even when multiple classes in the same
// file declare a same-named method:
//
//	self.foo()       → "<parentClass>.foo"   (true self-recursion drops)
//	ClassName().foo()→ "ClassName.foo"
//	obj.foo()        → "foo" + properties{disposition_hint: "ambiguous"}
//
// parentClass is the dotted enclosing class path of the caller ("" for
// module-level functions). It is used to qualify `self.X` calls and to
// detect when an inferred class-qualified target is in fact self-recursion.
func extractCallRelationships(body *sitter.Node, src []byte, callerName, parentClass string) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	calls := findAll(body, "call")
	if len(calls) == 0 {
		return nil
	}
	// Self-target in dotted form, used to drop true self-recursion when the
	// receiver resolves back to the caller's own class.
	selfQualified := callerName
	if parentClass != "" {
		selfQualified = parentClass + "." + callerName
	}
	seen := make(map[string]bool, len(calls))
	rels := make([]types.RelationshipRecord, 0, len(calls))
	for _, call := range calls {
		target, ambiguous := callTarget(call, src, parentClass)
		if target == "" {
			continue
		}
		// Drop self-recursion. The bare-name match preserves prior behavior
		// for module-level recursion (parentClass == ""); the dotted match
		// catches `self.foo()` inside the owning class.
		if target == callerName || target == selfQualified {
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		r := types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
		}
		if ambiguous {
			r.Properties = map[string]string{"disposition_hint": "ambiguous"}
		}
		rels = append(rels, r)
	}
	return rels
}

// callTarget resolves the callee name from a tree-sitter Python `call` node.
//
// Return values:
//
//	target     — dotted "<Class>.<method>" when the receiver type can be
//	             inferred from the call expression itself, or the bare leaf
//	             identifier otherwise. Empty when the call node has no
//	             resolvable function child.
//	ambiguous  — true when the leaf is a method call on a receiver whose
//	             type cannot be inferred from local syntax (e.g. `obj.foo()`
//	             where `obj` is a parameter or a name bound elsewhere). The
//	             resolver consumes this hint via the relationship's
//	             properties.disposition_hint to classify the edge.
//
// Receiver-type inference is purely syntactic and intentionally narrow:
//
//	self.foo()         — receiver = parentClass     (qualified if non-empty)
//	ClassName().foo()  — receiver = ClassName       (constructor-call result)
//	obj.foo()          — receiver unknown           (ambiguous=true, bare leaf)
//	foo()              — bare identifier            (no receiver, unchanged)
//	a.b.c()            — trailing identifier "c"    (chain receiver unknown)
//
// parentClass is the dotted enclosing class path of the caller, or "" when
// the caller is module-level. It is consulted only for `self.<x>` resolution.
func callTarget(call *sitter.Node, src []byte, parentClass string) (string, bool) {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return "", false
	}
	switch fn.Type() {
	case "identifier":
		return nodeText(fn, src), false
	case "attribute":
		// Trailing attribute identifier — the leaf method name.
		attr := fn.ChildByFieldName("attribute")
		if attr == nil {
			return "", false
		}
		leaf := nodeText(attr, src)
		// Inspect the receiver (`object` field) to qualify the leaf when we
		// can. Anything we don't recognise falls back to the bare leaf with
		// the ambiguous hint so the resolver can downgrade it appropriately.
		recv := fn.ChildByFieldName("object")
		if recv == nil {
			return leaf, true
		}
		if cls := receiverClass(recv, src, parentClass); cls != "" {
			return cls + "." + leaf, false
		}
		return leaf, true
	case "parenthesized_expression":
		for i := 0; i < int(fn.ChildCount()); i++ {
			ch := fn.Child(i)
			if ch.Type() == "identifier" {
				return nodeText(ch, src), false
			}
			if ch.Type() == "attribute" {
				if a := ch.ChildByFieldName("attribute"); a != nil {
					return nodeText(a, src), false
				}
			}
		}
	}
	return "", false
}

// receiverClass infers the class name of a method-call receiver when the
// receiver expression makes the type locally evident. Returns "" when no
// reliable inference is possible — the caller will then emit a bare-name
// edge tagged with disposition_hint=ambiguous.
//
// Recognised shapes:
//
//	self                 — caller's enclosing class (parentClass)
//	ClassName()          — constructor call result; receiver type = ClassName
//	(expr)               — unwrap parenthesised expressions and retry
//
// Anything else (free identifiers, attribute chains, subscripts, etc.) is
// deliberately left unresolved here: making a guess would risk binding edges
// to the wrong class. The resolver's name-collision logic is a safer place
// to disambiguate those cases.
func receiverClass(recv *sitter.Node, src []byte, parentClass string) string {
	if recv == nil {
		return ""
	}
	switch recv.Type() {
	case "identifier":
		if nodeText(recv, src) == "self" {
			return parentClass
		}
		return ""
	case "call":
		// e.g. B().foo() — function child is the constructor identifier.
		ctor := recv.ChildByFieldName("function")
		if ctor != nil && ctor.Type() == "identifier" {
			return nodeText(ctor, src)
		}
		return ""
	case "parenthesized_expression":
		for i := 0; i < int(recv.ChildCount()); i++ {
			ch := recv.Child(i)
			if ch.IsNamed() {
				return receiverClass(ch, src, parentClass)
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
