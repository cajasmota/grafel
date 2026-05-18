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
	// Issue #90 — stamp Properties["language"]="python" on every embedded
	// relationship so the resolver's per-language dynamic-pattern dispatch
	// picks the python catalog instead of falling back to the cross-language
	// one. Existing tags are preserved.
	extractor.TagRelationshipsLanguage(entities, "python")
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
				// Issue #526 — class-attribute assignments (DRF ViewSet
				// `serializer_class = ...`, Django Model `title =
				// models.CharField(...)`, SQLAlchemy `id = Column(...)`)
				// are emitted as SCOPE.Schema/field entities whose Name is
				// "<dottedClass>.<attr>" so the resolver's byMember index
				// binds `self.<attr>` references back to them.
				extractClassFields(body, file, childParent, out)
				// Emit CONTAINS edges from the class to every operation entity
				// the walker just appended (methods inside this class).
				// Field entities (#526) are intentionally not given a
				// CONTAINS edge — they exist solely to populate the
				// resolver's byMember index for `self.<attr>` lookups,
				// and an entity-ID-keyed CONTAINS would require the
				// field's ComputeID up front (it's filled in later by
				// buildDocument).
				after := len(*out)
				for k := before; k < after; k++ {
					child := &(*out)[k]
					if child.Kind != "SCOPE.Operation" {
						continue
					}
					// Issue #144 — emit a structural-ref (Format A) keyed on
					// the source file so the resolver disambiguates by
					// location when two classes in different files declare
					// methods with the same Name. FromID empty → buildDocument
					// substitutes the parent (class) entity ID at emit time.
					toID := extractor.BuildOperationStructuralRef("python", file.Path, child.Name)
					(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
						types.RelationshipRecord{
							ToID: toID,
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
					// Issue #526 — see the bare class_definition branch.
					extractClassFields(body, file, childParent, out)
					after := len(*out)
					for k := before; k < after; k++ {
						child := &(*out)[k]
						if child.Kind != "SCOPE.Operation" {
							continue
						}
						// Issue #144 — structural-ref (Format A) keyed on
						// file path; same rationale as the bare class
						// branch above.
						toID := extractor.BuildOperationStructuralRef("python", file.Path, child.Name)
						(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
							types.RelationshipRecord{
								ToID: toID,
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
//
// Issue #93 — every IMPORTS relationship now carries the metadata the
// cross-file resolver needs to bind a bare CALLS target back to its real
// entity:
//
//	Properties["local_name"]    — the identifier as referenced inside the
//	                              importing file (alias when present, else
//	                              the imported leaf name; for `import a.b`
//	                              this is the top-level package "a").
//	Properties["source_module"] — the dotted module path the symbol was
//	                              imported from. For `import x.y` this is
//	                              "x.y"; for `from x import y` this is "x".
//	Properties["imported_name"] — the original (pre-alias) leaf identifier
//	                              inside the source module. Equal to
//	                              local_name when no alias is present.
//	Properties["wildcard"]      — "1" when the import is `from x import *`.
func extractImports(root *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	if root == nil {
		return nil
	}
	var out []types.EntityRecord
	for _, n := range findAll(root, "import_statement") {
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			path, alias := dottedNameAndAlias(ch, file.Content)
			if path == "" {
				continue
			}
			localName := alias
			if localName == "" {
				if dot := strings.IndexByte(path, '.'); dot > 0 {
					localName = path[:dot]
				} else {
					localName = path
				}
			}
			props := map[string]string{
				"local_name":    localName,
				"source_module": path,
				"imported_name": path,
			}
			out = append(out, importRecord(path, file, props))
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
			name, alias := dottedNameAndAlias(ch, file.Content)
			if name == "" {
				continue
			}
			if name == "*" {
				out = append(out, importRecord(modPath, file, map[string]string{
					"source_module": modPath,
					"wildcard":      "1",
				}))
				emittedAny = true
				continue
			}
			localName := alias
			if localName == "" {
				localName = name
			}
			props := map[string]string{
				"local_name":    localName,
				"source_module": modPath,
				"imported_name": name,
			}
			out = append(out, importRecord(modPath+"."+name, file, props))
			emittedAny = true
		}
		if !emittedAny {
			out = append(out, importRecord(modPath, file, map[string]string{
				"source_module": modPath,
			}))
		}
	}
	return out
}

// dottedNameAndAlias returns (path, alias) for an import-list child node.
// path is the dotted import path stripped of any "as <alias>" suffix; alias
// is the binding identifier introduced by `as` (or "" when not present).
// Wildcards return ("*", ""). Unrecognised shapes return ("", "").
func dottedNameAndAlias(node *sitter.Node, src []byte) (string, string) {
	if node == nil {
		return "", ""
	}
	if node.Type() != "aliased_import" {
		return dottedNamePath(node, src), ""
	}
	var path, alias string
	if name := node.ChildByFieldName("name"); name != nil {
		path = dottedNamePath(name, src)
	}
	if a := node.ChildByFieldName("alias"); a != nil {
		alias = strings.TrimSpace(nodeText(a, src))
	}
	if path == "" && node.NamedChildCount() > 0 {
		path = dottedNamePath(node.NamedChild(0), src)
	}
	return path, alias
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
// path with one embedded IMPORTS relationship. Properties on the IMPORTS
// edge carry the import-binding metadata the cross-file resolver consumes
// (issue #93): local_name, source_module, imported_name, and wildcard.
func importRecord(modulePath string, file extractor.FileInput, props map[string]string) types.EntityRecord {
	return types.EntityRecord{
		Name:       modulePath,
		Kind:       "SCOPE.Component",
		Subtype:    "module",
		SourceFile: file.Path,
		Language:   "python",
		Relationships: []types.RelationshipRecord{
			{
				FromID:     file.Path,
				ToID:       modulePath,
				Kind:       "IMPORTS",
				Properties: props,
			},
		},
	}
}

// extractClassFields walks the immediate children of a class body and emits
// one SCOPE.Schema/field entity per class-attribute assignment. Issue #526.
//
// Recognised shapes (all at class body scope, NOT inside a def):
//
//	serializer_class = ArticleSerializer          # DRF ViewSet
//	queryset         = Article.objects.all()      # DRF
//	model            = User                       # Django ModelForm
//	fields           = ['title', 'body']          # DRF Serializer.Meta
//	permission_classes = [IsAuthenticated]        # DRF
//	id = Column(Integer, primary_key=True)        # SQLAlchemy declarative
//	title = models.CharField(max_length=200)      # Django models
//
// Also handles annotated assignments (PEP 526):
//
//	serializer_class: type[Serializer] = ArticleSerializer
//	count: int = 0
//
// Multi-target assignments (`a = b = expr`, `a, b = (1, 2)`) emit one entity
// per left-hand `identifier`. Tuple/list/subscript/attribute targets that
// aren't a bare class-scope name are skipped — those don't correspond to a
// new attribute declaration on the class.
//
// Dunder names (`__slots__`, `__qualname__`, etc.) and underscore-only names
// are skipped: they don't appear as `self.<name>` references in user code.
//
// The Name field is emitted as "<dottedClass>.<attr>" so the resolver's
// byMember[<file>][<class>][<attr>] index picks the entity up directly,
// binding CALLS edges like `self.serializer_class(...)` → this field.
//
// Field declarations are only emitted at the immediate class-body scope.
// Walker recursion into method bodies, nested classes, and conditional
// blocks (`if X: y = ...`) is intentionally skipped — those are not
// stable class attributes a resolver can rely on, and emitting them would
// risk over-eager binding.
func extractClassFields(
	body *sitter.Node,
	file extractor.FileInput,
	parentClass string,
	out *[]types.EntityRecord,
) {
	if body == nil || parentClass == "" {
		return
	}
	seen := make(map[string]bool)
	for i := 0; i < int(body.ChildCount()); i++ {
		stmt := body.Child(i)
		if stmt == nil {
			continue
		}
		if stmt.Type() != "expression_statement" {
			continue
		}
		// expression_statement → first named child is the assignment.
		for j := 0; j < int(stmt.NamedChildCount()); j++ {
			expr := stmt.NamedChild(j)
			if expr == nil {
				continue
			}
			var lhs *sitter.Node
			switch expr.Type() {
			case "assignment":
				lhs = expr.ChildByFieldName("left")
			case "augmented_assignment":
				// `count += 1` at class scope doesn't introduce a new
				// attribute — skip.
				continue
			default:
				continue
			}
			for _, name := range classAttrLHSNames(lhs, file.Content) {
				if name == "" || seen[name] {
					continue
				}
				if skipClassAttrName(name) {
					continue
				}
				seen[name] = true
				*out = append(*out, types.EntityRecord{
					Name:       parentClass + "." + name,
					Kind:       "SCOPE.Schema",
					Subtype:    "field",
					Language:   "python",
					SourceFile: file.Path,
					StartLine:  int(stmt.StartPoint().Row) + 1,
					EndLine:    int(stmt.EndPoint().Row) + 1,
					Signature:  name,
				})
			}
		}
	}
}

// classAttrLHSNames extracts the set of bare-identifier targets on the
// left-hand side of a class-scope assignment. Recognised shapes:
//
//	x              → ["x"]                          (simple)
//	x: int         → ["x"]                          (PEP 526 annotated; LHS is
//	                                                 typed_default_parameter or
//	                                                 "identifier" depending on
//	                                                 grammar version — both
//	                                                 reduce to the leaf name)
//	a = b = expr   → ["a"] then resolver recurses   (chained assignments at
//	                                                 the tree-sitter grammar
//	                                                 level appear as nested
//	                                                 `assignment` nodes inside
//	                                                 the right field; handled
//	                                                 by the caller's
//	                                                 ChildByFieldName loop on
//	                                                 the outer assignment)
//	a, b           → ["a", "b"]                     (tuple/list pattern)
//	self.x         → []                             (attribute LHS — not a
//	                                                 class attribute)
//	x[0]           → []                             (subscript LHS)
func classAttrLHSNames(lhs *sitter.Node, src []byte) []string {
	if lhs == nil {
		return nil
	}
	switch lhs.Type() {
	case "identifier":
		return []string{nodeText(lhs, src)}
	case "pattern_list", "tuple_pattern", "list_pattern":
		var names []string
		for i := 0; i < int(lhs.NamedChildCount()); i++ {
			ch := lhs.NamedChild(i)
			if ch != nil && ch.Type() == "identifier" {
				names = append(names, nodeText(ch, src))
			}
		}
		return names
	}
	return nil
}

// skipClassAttrName filters dunder / private-by-convention names that should
// not be emitted as class-attribute entities. These either have special
// runtime semantics (`__slots__`, `__qualname__`, `__doc__`) or are pure
// implementation noise (`_`, `__`) that no resolver should bind to.
func skipClassAttrName(name string) bool {
	if name == "" || name == "_" || name == "__" {
		return true
	}
	if strings.HasPrefix(name, "__") && strings.HasSuffix(name, "__") {
		return true
	}
	return false
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
