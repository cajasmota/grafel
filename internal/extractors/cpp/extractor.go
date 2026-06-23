// Package cpp implements the C and C++ language extractor for grafel.
//
// It extracts functions, structs/classes/unions, namespaces, templates, enums,
// #include directives, and #define macros from C and C++ source files using
// the smacker/go-tree-sitter grammar. The extractor registers itself via init()
// under both "c" and "cpp" language keys.
//
// Entity mapping:
//
//	function_definition / function_declarator → Kind="SCOPE.Operation",   Subtype="function"
//	class_specifier / struct_specifier        → Kind="SCOPE.Component",   Subtype="class"/"struct"
//	union_specifier                           → Kind="SCOPE.Component",   Subtype="union"
//	namespace_definition                      → Kind="SCOPE.Component",   Subtype="namespace"
//	template_declaration                      → Kind="SCOPE.Schema",      Subtype="template"
//	enum_specifier                            → Kind="SCOPE.Schema",       Subtype="enum"
//	preproc_include                           → Kind="SCOPE.Component",   Subtype="import"
//	preproc_def                               → Kind="SCOPE.Pattern",     Subtype="macro"
//
// Issue #367 (PORT-RELS-CPP) — emits the same three relationship kinds the
// other ported extractors emit:
//
//   - IMPORTS: every `#include` directive and `using` declaration carries
//     Properties{local_name, source_module, imported_name} matching the Java
//     contract (#120) and the Python schema (#93).
//   - CALLS: every `call_expression` inside a function body emits one CALLS
//     edge per unique target. Bare `foo()` → ToID="foo". Member access
//     `obj.method()` / `ptr->method()` → ToID="method". Qualified
//     `Foo::method()` → ToID="method". Self-recursion is dropped.
//   - CONTAINS: class/struct/union/namespace declarations attach one
//     CONTAINS edge per method/function declared inside the body, with the
//     structural-ref shape `scope:operation:method:cpp:<file>:<name>`
//     (Format A, #144). Out-of-line definitions `void Foo::bar()` are also
//     attached to the Foo entity when it exists in the same file.
//
// OTel span: "indexer.extract.cpp" with attributes language, file_line_count, entity_count.
package cpp

import (
	"bytes"
	"context"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("c", &CppExtractor{lang: "c"})
	extractor.Register("cpp", &CppExtractor{lang: "cpp"})
}

// CppExtractor extracts C/C++ language entities using tree-sitter.
type CppExtractor struct {
	lang string
}

// Language implements extractor.Extractor.
func (e *CppExtractor) Language() string { return e.lang }

// Extract implements extractor.Extractor.
// Returns partial results on node failures — never panics.
func (e *CppExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor.cpp")
	ctx, span := tracer.Start(ctx, "indexer.extract.cpp")
	defer span.End()

	lang := file.Language
	if lang == "" {
		lang = e.lang
	}

	lineCount := bytes.Count(file.Content, []byte{'\n'}) + 1

	// Fast-path: empty content or nil tree.
	if len(file.Content) == 0 {
		span.SetAttributes(
			attribute.String("language", lang),
			attribute.Int("file_line_count", 0),
			attribute.Int("entity_count", 0),
		)
		return nil, nil
	}

	tree := file.TSTree
	if tree == nil {
		grammar := cppGrammar()
		if lang == "c" {
			grammar = cGrammar()
		}
		parser, perr := cppAdapter.NewParser(grammar)
		if perr != nil {
			return nil, perr
		}
		defer parser.Close()
		var err error
		tree, err = parser.Parse(file.Content)
		if err != nil {
			return nil, err
		}
	}

	root := tree.RootNode()
	if root == nil {
		span.SetAttributes(
			attribute.String("language", lang),
			attribute.Int("file_line_count", lineCount),
			attribute.Int("entity_count", 0),
		)
		return nil, nil
	}

	var records []types.EntityRecord

	// Issue #577 — emit file-level SCOPE.Component (subtype="file") so the
	// cross-repo import linker (#566) can map IMPORTS edges back to the
	// originating repo via the resolver's byName index. Generalises the
	// JS/TS fix from #570/#575.
	records = append(records, extractor.FileEntity(file))

	// Structural walk: container nodes (class/struct/union/namespace) are
	// handled specially so we can emit CONTAINS edges for their direct
	// method/function children; everything else is collected via flat
	// recursion. Function bodies are scanned for call_expression nodes
	// to emit CALLS edges.
	walkStructural(root, file.Content, file.Path, lang, "", &records)

	// Out-of-line method definitions like `void Foo::bar()` declared at
	// file/namespace scope attach CONTAINS on the matching Foo entity in
	// the same file. We do this in a second pass because the class entity
	// may have been declared above or below the definition.
	attachOutOfLineContains(&records, file.Path, lang)

	// Issue #4854 — class→field CONTAINS + in-file base-class EXTENDS for the
	// SCOPE.Schema/field members emitted in the structural walk.
	records = attachCppFieldMembership(records, file.Path, lang)

	// Issue #3628 — error-flow: scan throw / typed-catch sites and emit
	// THROWS / CATCHES edges to a shared SCOPE.ExceptionType convergence node
	// (records[0] is the file entity required by EmitExceptionEdges).
	emitExceptionFlowEdges(root, file.Content, &records)

	span.SetAttributes(
		attribute.String("language", lang),
		attribute.Int("file_line_count", lineCount),
		attribute.Int("entity_count", len(records)),
	)
	// Issue #90 — language tag for resolver dynamic-pattern dispatch.
	extractor.TagRelationshipsLanguage(records, lang)
	extractor.TagEntitiesLanguage(records, lang)
	return records, nil
}

// ----------------------------------------------------------------
// Structural walker
// ----------------------------------------------------------------

// walkStructural traverses the tree handling container nodes specially.
// `container` is the name of the enclosing class/struct/union (NOT
// namespace) when we're inside its body — used so methods attach CONTAINS
// to that container and self-recursion is detected for CALLS dedup.
func walkStructural(n ts.Node, src []byte, path, lang, container string, out *[]types.EntityRecord) {
	if n == nil {
		return
	}

	switch n.Type() {
	case "class_specifier", "struct_specifier", "union_specifier":
		subtype := "class"
		switch n.Type() {
		case "struct_specifier":
			subtype = "struct"
		case "union_specifier":
			subtype = "union"
		}
		rec, ok := extractClassLike(n, src, path, lang, subtype)
		if !ok {
			// Anonymous — recurse into body to still pick up nested entities.
			for i := 0; i < int(n.ChildCount()); i++ {
				walkStructural(n.Child(i), src, path, lang, container, out)
			}
			return
		}
		idx := len(*out)
		*out = append(*out, rec)
		// Walk the field_declaration_list (body) to collect methods +
		// nested entities, and attach CONTAINS for each Operation that
		// belongs to this class.
		body := findClassBody(n)
		if body != nil {
			before := len(*out)
			for i := 0; i < int(body.ChildCount()); i++ {
				walkStructural(body.Child(i), src, path, lang, rec.Name, out)
			}
			after := len(*out)
			for k := before; k < after; k++ {
				child := &(*out)[k]
				if child.Kind != "SCOPE.Operation" {
					continue
				}
				// Only attach for direct same-class methods (out-of-line
				// methods are handled by attachOutOfLineContains).
				if child.SourceFile != path {
					continue
				}
				toID := extractor.BuildOperationStructuralRef(lang, path, child.Name)
				(*out)[idx].Relationships = append((*out)[idx].Relationships,
					types.RelationshipRecord{
						ToID: toID,
						Kind: "CONTAINS",
					})
			}
			// Issue #4854 — emit one SCOPE.Schema/field per data member so a
			// plain C++ data class has field children (class→field CONTAINS is
			// wired in a post-pass once every in-file type name is known).
			fieldEnts := emitClassFieldMembers(body, src, rec.Name, path, lang)
			*out = append(*out, fieldEnts...)
		}
		// Issue #4854 — stash candidate base-class names on the owner so the
		// post-pass can emit EXTENDS edges restricted to bases declared in
		// this same file (mirrors the Go embedded-field EXTENDS policy).
		if bases := cppBaseClasses(n, src); len(bases) > 0 {
			var baseNames []string
			for _, b := range bases {
				if nm, _ := b["name"].(string); nm != "" {
					baseNames = append(baseNames, nm)
				}
			}
			if len(baseNames) > 0 {
				if (*out)[idx].Metadata == nil {
					(*out)[idx].Metadata = map[string]interface{}{}
				}
				(*out)[idx].Metadata["base_candidates"] = baseNames
			}
		}
		return

	case "namespace_definition":
		rec, ok := extractNamespace(n, src, path, lang)
		if !ok {
			for i := 0; i < int(n.ChildCount()); i++ {
				walkStructural(n.Child(i), src, path, lang, container, out)
			}
			return
		}
		idx := len(*out)
		*out = append(*out, rec)
		// Recurse into the namespace body. Methods declared directly in
		// the namespace (not in a nested class) attach CONTAINS to the
		// namespace.
		body := findNamespaceBody(n)
		if body != nil {
			before := len(*out)
			for i := 0; i < int(body.ChildCount()); i++ {
				walkStructural(body.Child(i), src, path, lang, "", out)
			}
			after := len(*out)
			for k := before; k < after; k++ {
				child := &(*out)[k]
				if child.Kind != "SCOPE.Operation" {
					continue
				}
				if child.SourceFile != path {
					continue
				}
				// Skip methods that are out-of-line definitions of a
				// member of some class — those will attach CONTAINS to
				// their class via attachOutOfLineContains and shouldn't
				// also be attributed to the namespace.
				if hasQualifiedScope(child) {
					continue
				}
				toID := extractor.BuildOperationStructuralRef(lang, path, child.Name)
				(*out)[idx].Relationships = append((*out)[idx].Relationships,
					types.RelationshipRecord{
						ToID: toID,
						Kind: "CONTAINS",
					})
			}
		}
		return

	case "function_definition":
		if r, ok := extractFunction(n, src, path, lang); ok {
			// CALLS: scan body for call_expression descendants.
			body := n.ChildByFieldName("body")
			rels := extractCallRelationships(body, src, r.Name)
			r.Relationships = append(r.Relationships, rels...)
			*out = append(*out, r)
		}
		return

	case "template_declaration":
		// C++20 concept: `template<...> concept Name = <constraint>;`
		// The template wraps a concept_definition child. A concept is the
		// C++ analogue of an interface/type-class constraint, so emit it as
		// a SCOPE.Schema/concept entity rather than a generic template.
		if cn := cppFirstChildOfType(n, "concept_definition"); cn != nil {
			if r, ok := extractConcept(n, cn, src, path, lang); ok {
				*out = append(*out, r)
			}
			return
		}
		if r, ok := extractTemplate(n, src, path, lang); ok {
			// Template's inner declaration may be a function — collect
			// its calls. Find function_definition or class_specifier
			// within and attach calls / nested operations.
			for i := 0; i < int(n.ChildCount()); i++ {
				inner := n.Child(i)
				switch inner.Type() {
				case "function_definition":
					body := inner.ChildByFieldName("body")
					rels := extractCallRelationships(body, src, r.Name)
					r.Relationships = append(r.Relationships, rels...)
				}
			}
			*out = append(*out, r)
			// Also recurse into the template's inner class/struct so
			// nested class members get emitted.
			for i := 0; i < int(n.ChildCount()); i++ {
				inner := n.Child(i)
				switch inner.Type() {
				case "class_specifier", "struct_specifier", "union_specifier":
					// Walk the body of the templated class so its methods
					// are emitted as Operations (skip emitting the class
					// itself again — the template entity already
					// represents it).
					body := findClassBody(inner)
					if body != nil {
						for j := 0; j < int(body.ChildCount()); j++ {
							walkStructural(body.Child(j), src, path, lang, r.Name, out)
						}
					}
				}
			}
		}
		return

	case "enum_specifier":
		if r, ok := extractEnum(n, src, path, lang); ok {
			*out = append(*out, r)
		}
		return

	case "preproc_include":
		if r, ok := extractInclude(n, src, path, lang); ok {
			*out = append(*out, r)
		}
		return

	case "preproc_def":
		if r, ok := extractMacro(n, src, path, lang); ok {
			*out = append(*out, r)
		}
		return

	case "using_declaration":
		// `using std::cout;` — emit IMPORTS edge.
		if r, ok := extractUsing(n, src, path, lang); ok {
			*out = append(*out, r)
		}
		return
	}

	// Default: recurse into children.
	for i := 0; i < int(n.ChildCount()); i++ {
		walkStructural(n.Child(i), src, path, lang, container, out)
	}
}

// findClassBody returns the field_declaration_list child of a class/struct/
// union specifier, or nil when no body is present (forward declarations).
func findClassBody(n ts.Node) ts.Node {
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if ch.Type() == "field_declaration_list" {
			return ch
		}
	}
	return nil
}

// findNamespaceBody returns the declaration_list child of a namespace
// definition, or nil for namespace alias / anonymous declarations.
func findNamespaceBody(n ts.Node) ts.Node {
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if ch.Type() == "declaration_list" {
			return ch
		}
	}
	return nil
}

// ----------------------------------------------------------------
// Out-of-line CONTAINS attach
// ----------------------------------------------------------------

// attachOutOfLineContains scans Operation entities whose Metadata carries
// a `qualified_scope` (set when the function was defined as
// `void Foo::bar()` outside the class body) and attaches a CONTAINS edge
// from the matching class entity in the same file. Cross-file out-of-line
// definitions are left to the resolver.
func attachOutOfLineContains(records *[]types.EntityRecord, path, lang string) {
	for i := range *records {
		op := &(*records)[i]
		if op.Kind != "SCOPE.Operation" {
			continue
		}
		scope, _ := op.Metadata["qualified_scope"].(string)
		if scope == "" {
			continue
		}
		// Find the matching class/struct/union/namespace entity in same file.
		for j := range *records {
			cont := &(*records)[j]
			if cont.SourceFile != path {
				continue
			}
			if cont.Kind != "SCOPE.Component" {
				continue
			}
			if cont.Name != scope {
				continue
			}
			toID := extractor.BuildOperationStructuralRef(lang, path, op.Name)
			cont.Relationships = append(cont.Relationships,
				types.RelationshipRecord{
					ToID: toID,
					Kind: "CONTAINS",
				})
			break
		}
	}
}

// hasQualifiedScope reports whether an Operation entity was defined
// out-of-line (i.e. `Foo::bar`). Such entities are tagged in
// extractFunction via Metadata["qualified_scope"].
func hasQualifiedScope(op *types.EntityRecord) bool {
	if op == nil || op.Metadata == nil {
		return false
	}
	_, ok := op.Metadata["qualified_scope"].(string)
	return ok
}

// ----------------------------------------------------------------
// CALLS extraction
// ----------------------------------------------------------------

// cppKeywordStop lists C++ keywords / pseudo-identifiers that the parser
// surfaces as the head of a call_expression but are not real call targets.
// Mirrors the keyword filters in kotlin/swift.
var cppKeywordStop = map[string]bool{
	"sizeof":           true,
	"alignof":          true,
	"static_cast":      true,
	"dynamic_cast":     true,
	"reinterpret_cast": true,
	"const_cast":       true,
	"typeid":           true,
	"new":              true,
	"delete":           true,
	"this":             true,
}

// extractCallRelationships returns one CALLS RelationshipRecord per unique
// call_expression descendant of body. The target name is the callee
// identifier — the trailing simple identifier when the call is on a
// member access (`obj.method`, `ptr->method`) or qualified
// (`Foo::method`). Self-recursion is dropped.
func extractCallRelationships(body ts.Node, src []byte, callerName string) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	calls := findAllNodes(body, "call_expression")
	if len(calls) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(calls))
	rels := make([]types.RelationshipRecord, 0, len(calls))
	for _, call := range calls {
		target := cppCallTarget(call, src)
		if target == "" || target == callerName {
			continue
		}
		if cppKeywordStop[target] {
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

// cppCallTarget resolves the callee name from a call_expression.
//
// Tree-sitter-cpp shapes a call_expression with two children: the
// function expression and the argument_list. The function expression may
// be:
//   - identifier               → return text
//   - field_expression         → `obj.method` / `ptr->method` — return the
//     trailing field_identifier
//   - qualified_identifier     → `Foo::method` — return the trailing name
//   - template_function        → `foo<int>()` — return the inner name
//   - parenthesized_expression → recurse into inner
func cppCallTarget(call ts.Node, src []byte) string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		// Fallback: first non-argument_list child.
		for i := 0; i < int(call.ChildCount()); i++ {
			ch := call.Child(i)
			if ch.Type() != "argument_list" && ch.Type() != "(" && ch.Type() != ")" {
				fn = ch
				break
			}
		}
	}
	return resolveCallee(fn, src)
}

// resolveCallee unpacks a function-expression node to its trailing
// identifier name. Returns "" when the shape is unrecognised.
func resolveCallee(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "identifier", "field_identifier", "type_identifier":
		return nodeText(n, src)
	case "field_expression":
		// `obj.method` / `ptr->method`. The trailing field is the field
		// child.
		f := n.ChildByFieldName("field")
		if f != nil {
			return nodeText(f, src)
		}
		// Fallback: last child that's a field_identifier.
		for i := int(n.ChildCount()) - 1; i >= 0; i-- {
			ch := n.Child(i)
			if ch.Type() == "field_identifier" {
				return nodeText(ch, src)
			}
		}
	case "qualified_identifier":
		// `Foo::bar` — the rightmost name child is the leaf.
		name := n.ChildByFieldName("name")
		if name != nil {
			return resolveCallee(name, src)
		}
		// Fallback: last identifier-like child.
		for i := int(n.ChildCount()) - 1; i >= 0; i-- {
			ch := n.Child(i)
			t := ch.Type()
			if t == "identifier" || t == "field_identifier" || t == "type_identifier" || t == "qualified_identifier" {
				return resolveCallee(ch, src)
			}
		}
	case "template_function":
		// `foo<int>` — name child is the function template name.
		name := n.ChildByFieldName("name")
		if name != nil {
			return resolveCallee(name, src)
		}
	case "parenthesized_expression":
		for i := 0; i < int(n.ChildCount()); i++ {
			ch := n.Child(i)
			if ch.Type() != "(" && ch.Type() != ")" {
				if t := resolveCallee(ch, src); t != "" {
					return t
				}
			}
		}
	}
	return ""
}

// findAllNodes returns every descendant of root whose Type() is in kinds.
func findAllNodes(root ts.Node, kinds ...string) []ts.Node {
	if root == nil {
		return nil
	}
	set := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		set[k] = true
	}
	var out []ts.Node
	stack := []ts.Node{root}
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

// ----------------------------------------------------------------
// Tree helpers (legacy walk retained for findFirst usage)
// ----------------------------------------------------------------

// findFirst returns the first descendant node (depth-first) matching any of the given types.
func findFirst(root ts.Node, types ...string) ts.Node {
	if root == nil {
		return nil
	}
	typeSet := make(map[string]bool, len(types))
	for _, t := range types {
		typeSet[t] = true
	}
	stack := []ts.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if typeSet[n.Type()] {
			return n
		}
		count := int(n.ChildCount())
		for i := count - 1; i >= 0; i-- {
			stack = append(stack, n.Child(i))
		}
	}
	return nil
}

// ----------------------------------------------------------------
// Text helpers
// ----------------------------------------------------------------

func nodeText(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	s := n.StartByte()
	e := n.EndByte()
	if int(e) > len(src) {
		e = uint32(len(src))
	}
	return string(src[s:e])
}

func nodeLines(n ts.Node) (int, int) {
	return int(n.StartPoint().Row) + 1, int(n.EndPoint().Row) + 1
}

// ----------------------------------------------------------------
// Entity extractors
// ----------------------------------------------------------------

// extractFunction extracts a function_definition node.
// The declarator field gives us the function name via function_declarator → identifier.
// Out-of-line definitions like `void Foo::bar()` capture the qualifier
// in Metadata["qualified_scope"] = "Foo" so attachOutOfLineContains can
// later wire a CONTAINS edge from the Foo entity.
func extractFunction(n ts.Node, src []byte, path, lang string) (types.EntityRecord, bool) {
	decl := n.ChildByFieldName("declarator")
	if decl == nil {
		return types.EntityRecord{}, false
	}
	name := resolveFunctionName(decl, src)
	if name == "" {
		return types.EntityRecord{}, false
	}
	scope := resolveQualifiedScope(decl, src)
	start, end := nodeLines(n)
	meta := map[string]interface{}{"subtype": "function"}
	if scope != "" {
		meta["qualified_scope"] = scope
	}
	return types.EntityRecord{
		Name:         name,
		Kind:         "SCOPE.Operation",
		Subtype:      "function",
		SourceFile:   path,
		StartLine:    start,
		EndLine:      end,
		Language:     lang,
		QualityScore: 0.9,
		Metadata:     meta,
	}, true
}

// resolveFunctionName drills through pointer/ref declarators to the
// function_declarator and returns its identifier.
func resolveFunctionName(decl ts.Node, src []byte) string {
	if decl == nil {
		return ""
	}
	switch decl.Type() {
	case "function_declarator":
		inner := decl.ChildByFieldName("declarator")
		return resolveIdentifier(inner, src)
	case "pointer_declarator", "reference_declarator":
		inner := decl.ChildByFieldName("declarator")
		return resolveFunctionName(inner, src)
	case "identifier", "field_identifier", "type_identifier":
		return nodeText(decl, src)
	case "destructor_name", "operator_name":
		return nodeText(decl, src)
	default:
		// Try the declarator field recursively.
		inner := decl.ChildByFieldName("declarator")
		if inner != nil {
			return resolveFunctionName(inner, src)
		}
		// Fall back: look for function_declarator anywhere below.
		fd := findFirst(decl, "function_declarator")
		if fd != nil {
			inner2 := fd.ChildByFieldName("declarator")
			return resolveIdentifier(inner2, src)
		}
	}
	return ""
}

// resolveQualifiedScope returns the class/namespace qualifier of an
// out-of-line function definition (`void Foo::bar() {}` → "Foo"). For
// in-class or unqualified definitions returns "". Walks through pointer/
// reference / function declarators to find a qualified_identifier whose
// scope child names the qualifier.
func resolveQualifiedScope(decl ts.Node, src []byte) string {
	if decl == nil {
		return ""
	}
	switch decl.Type() {
	case "function_declarator":
		return resolveQualifiedScope(decl.ChildByFieldName("declarator"), src)
	case "pointer_declarator", "reference_declarator":
		return resolveQualifiedScope(decl.ChildByFieldName("declarator"), src)
	case "qualified_identifier":
		// scope child is the qualifier (could itself be qualified for
		// `A::B::method` — return the immediate parent A::B's leaf).
		scope := decl.ChildByFieldName("scope")
		if scope != nil {
			// For nested qualifiers, pick the rightmost identifier in
			// the scope chain (matching the immediate parent class).
			return rightmostIdentifier(scope, src)
		}
	}
	return ""
}

// rightmostIdentifier returns the rightmost identifier name in a chain of
// qualified_identifier / namespace_identifier nodes.
func rightmostIdentifier(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "namespace_identifier", "identifier", "type_identifier":
		return nodeText(n, src)
	case "qualified_identifier":
		name := n.ChildByFieldName("name")
		if name != nil {
			return rightmostIdentifier(name, src)
		}
	}
	// Fallback: scan children right-to-left.
	for i := int(n.ChildCount()) - 1; i >= 0; i-- {
		ch := n.Child(i)
		t := ch.Type()
		if t == "namespace_identifier" || t == "identifier" || t == "type_identifier" {
			return nodeText(ch, src)
		}
	}
	return ""
}

// resolveIdentifier extracts the name text from identifier-like nodes,
// including qualified names (scope_resolution).
func resolveIdentifier(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "identifier", "type_identifier", "field_identifier":
		return nodeText(n, src)
	case "qualified_identifier":
		// e.g. MyClass::method — return last part (method)
		name := n.ChildByFieldName("name")
		if name != nil {
			return resolveIdentifier(name, src)
		}
		return nodeText(n, src)
	case "destructor_name", "operator_name":
		return nodeText(n, src)
	default:
		// Fall through: check for nested identifier
		if id := findFirst(n, "identifier"); id != nil {
			return nodeText(id, src)
		}
	}
	return ""
}

// extractClassLike handles class_specifier, struct_specifier, union_specifier.
func extractClassLike(n ts.Node, src []byte, path, lang, subtype string) (types.EntityRecord, bool) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		// Anonymous struct/class — skip.
		return types.EntityRecord{}, false
	}
	name := nodeText(nameNode, src)
	if name == "" {
		return types.EntityRecord{}, false
	}
	start, end := nodeLines(n)

	meta := map[string]interface{}{"subtype": subtype}

	// Inheritance: base_class_clause → [access_specifier? type_identifier]+.
	if bases := cppBaseClasses(n, src); len(bases) > 0 {
		meta["bases"] = bases
	}

	// Member fields + access specifiers + abstract (pure-virtual) detection.
	// Methods are emitted as separate SCOPE.Operation entities via the
	// structural walk; here we record the data-member shape so a `type`
	// (class/struct/union) entity carries its field schema.
	body := findClassBody(n)
	if body != nil {
		fields, isAbstract := cppClassMembers(body, src)
		if len(fields) > 0 {
			meta["fields"] = fields
			meta["field_count"] = len(fields)
		}
		if isAbstract {
			// An abstract class with pure-virtual methods is the C++
			// analogue of an interface.
			meta["abstract"] = true
		}
	}

	return types.EntityRecord{
		Name:         name,
		Kind:         "SCOPE.Component",
		Subtype:      subtype,
		SourceFile:   path,
		StartLine:    start,
		EndLine:      end,
		Language:     lang,
		QualityScore: 0.9,
		Metadata:     meta,
	}, true
}

// cppBaseClasses returns the inherited base classes of a class/struct
// specifier as []map{"name","access"} where access is the explicit
// access-specifier ("public"/"protected"/"private") when present.
func cppBaseClasses(n ts.Node, src []byte) []map[string]interface{} {
	var clause ts.Node
	for i := 0; i < int(n.ChildCount()); i++ {
		if n.Child(i).Type() == "base_class_clause" {
			clause = n.Child(i)
			break
		}
	}
	if clause == nil {
		return nil
	}
	var out []map[string]interface{}
	access := ""
	for i := 0; i < int(clause.ChildCount()); i++ {
		ch := clause.Child(i)
		switch ch.Type() {
		case "access_specifier":
			access = strings.TrimSpace(nodeText(ch, src))
		case "type_identifier", "qualified_identifier", "template_type":
			base := map[string]interface{}{"name": nodeText(ch, src)}
			if access != "" {
				base["access"] = access
			}
			out = append(out, base)
			access = ""
		case ",":
			access = ""
		}
	}
	return out
}

// cppClassMembers walks a field_declaration_list returning the data members
// (name + type + current access section) and whether the class is abstract
// (has at least one pure-virtual method, i.e. a `pure_virtual_clause`).
func cppClassMembers(body ts.Node, src []byte) (fields []map[string]interface{}, abstract bool) {
	access := "private" // class default; struct/union default is public but
	// access is tracked relative to the explicit specifiers we see.
	for i := 0; i < int(body.ChildCount()); i++ {
		ch := body.Child(i)
		switch ch.Type() {
		case "access_specifier":
			access = strings.TrimSpace(nodeText(ch, src))
		case "field_declaration":
			// Distinguish a data member from an inline method declaration.
			// A function_declarator child ⇒ it's a method, not a field.
			if cppFirstChildOfType(ch, "function_declarator") != nil {
				// Pure-virtual method (`virtual T f() = 0;`) ⇒ abstract class.
				// The current tree-sitter-cpp grammar represents the pure
				// specifier as a bare `= number_literal(0)` inside the
				// field_declaration (no dedicated pure_virtual_clause node);
				// older grammars used a pure_virtual_clause child. Accept both.
				if cppIsPureVirtualField(ch, src) {
					abstract = true
				}
				continue
			}
			typeNode := ch.ChildByFieldName("type")
			fname := ""
			for j := 0; j < int(ch.ChildCount()); j++ {
				cj := ch.Child(j)
				if cj.Type() == "field_identifier" {
					fname = nodeText(cj, src)
					break
				}
			}
			if fname == "" {
				continue
			}
			f := map[string]interface{}{"name": fname, "access": access}
			if typeNode != nil {
				f["type"] = strings.TrimSpace(nodeText(typeNode, src))
			}
			fields = append(fields, f)
		case "function_definition", "declaration":
			// Pure-virtual method ⇒ abstract class. The pure_virtual_clause
			// appears as a child of the function_definition.
			if cppFirstChildOfType(ch, "pure_virtual_clause") != nil {
				abstract = true
			}
		}
	}
	return fields, abstract
}

// cppIsPureVirtualField reports whether a method-bearing field_declaration is
// a pure-virtual declaration (`virtual T f() = 0;`). It accepts both the
// legacy `pure_virtual_clause` child and the current grammar's bare
// `= number_literal(0)` representation. A leading `virtual` specifier is not
// required (out-of-class `= 0` is not legal C++, so the `= 0` after a method
// declarator is unambiguous).
func cppIsPureVirtualField(field ts.Node, src []byte) bool {
	if cppFirstChildOfType(field, "pure_virtual_clause") != nil {
		return true
	}
	sawAssign := false
	for i := 0; i < int(field.ChildCount()); i++ {
		ch := field.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "=":
			sawAssign = true
		case "number_literal":
			if sawAssign && strings.TrimSpace(nodeText(ch, src)) == "0" {
				return true
			}
		}
	}
	return false
}

// cppFirstChildOfType returns the first direct child of n with the given
// node type, or nil.
func cppFirstChildOfType(n ts.Node, typ string) ts.Node {
	for i := 0; i < int(n.ChildCount()); i++ {
		if n.Child(i).Type() == typ {
			return n.Child(i)
		}
	}
	return nil
}

// extractNamespace extracts a namespace_definition node.
func extractNamespace(n ts.Node, src []byte, path, lang string) (types.EntityRecord, bool) {
	nameNode := n.ChildByFieldName("name")
	name := ""
	if nameNode != nil {
		name = nodeText(nameNode, src)
	}
	if name == "" {
		// Anonymous namespace — use synthetic name.
		name = "(anonymous)"
	}
	start, end := nodeLines(n)
	return types.EntityRecord{
		Name:         name,
		Kind:         "SCOPE.Component",
		Subtype:      "namespace",
		SourceFile:   path,
		StartLine:    start,
		EndLine:      end,
		Language:     lang,
		QualityScore: 0.9,
		Metadata:     map[string]interface{}{"subtype": "namespace"},
	}, true
}

// extractTemplate extracts a template_declaration node.
// Tries to get the name from the inner declaration (class/function).
func extractTemplate(n ts.Node, src []byte, path, lang string) (types.EntityRecord, bool) {
	// The body is the last child (after template parameter list).
	// It can be a function_definition, class_specifier, alias_declaration, etc.
	name := ""
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		child := n.Child(i)
		switch child.Type() {
		case "function_definition":
			decl := child.ChildByFieldName("declarator")
			name = resolveFunctionName(decl, src)
		case "class_specifier", "struct_specifier":
			nameNode := child.ChildByFieldName("name")
			if nameNode != nil {
				name = nodeText(nameNode, src)
			}
		case "declaration":
			decl := child.ChildByFieldName("declarator")
			if decl != nil {
				name = resolveFunctionName(decl, src)
			}
		}
		if name != "" {
			break
		}
	}
	if name == "" {
		return types.EntityRecord{}, false
	}
	start, end := nodeLines(n)
	return types.EntityRecord{
		Name:         name,
		Kind:         "SCOPE.Schema",
		Subtype:      "template",
		SourceFile:   path,
		StartLine:    start,
		EndLine:      end,
		Language:     lang,
		QualityScore: 0.9,
		Metadata:     map[string]interface{}{"subtype": "template"},
	}, true
}

// extractEnum extracts an enum_specifier node.
//
// Captures the enum's depth so TypeSystem coverage is real, not just a name:
//   - scoped:          true for `enum class` / `enum struct` (C++11)
//   - underlying_type: the `: <type>` fixed underlying type when present
//   - enumerators:     ordered list of {name, value?} for each enumerator,
//     where value is the explicit `= <expr>` text when given
//   - enumerator_count: convenience count
func extractEnum(n ts.Node, src []byte, path, lang string) (types.EntityRecord, bool) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return types.EntityRecord{}, false
	}
	name := nodeText(nameNode, src)
	if name == "" {
		return types.EntityRecord{}, false
	}
	start, end := nodeLines(n)

	meta := map[string]interface{}{"subtype": "enum"}

	// Scoped enums (`enum class` / `enum struct`) carry a `class`/`struct`
	// keyword child before the name. The fixed underlying type, when
	// present, follows a `:` token.
	scoped := false
	underlying := ""
	sawColon := false
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		switch ch.Type() {
		case "class", "struct":
			scoped = true
		case ":":
			sawColon = true
		case "primitive_type", "type_identifier", "sized_type_specifier", "qualified_identifier":
			if sawColon && underlying == "" {
				underlying = nodeText(ch, src)
			}
		}
	}
	meta["scoped"] = scoped
	if underlying != "" {
		meta["underlying_type"] = underlying
	}

	enumerators := cppEnumerators(n, src)
	if len(enumerators) > 0 {
		meta["enumerators"] = enumerators
		meta["enumerator_count"] = len(enumerators)
	}

	return types.EntityRecord{
		Name:         name,
		Kind:         "SCOPE.Schema",
		Subtype:      "enum",
		SourceFile:   path,
		StartLine:    start,
		EndLine:      end,
		Language:     lang,
		QualityScore: 0.9,
		Metadata:     meta,
	}, true
}

// cppEnumerators returns the ordered enumerators of an enum_specifier as
// []map{"name","value"} where value is the explicit initialiser text (or
// absent when the enumerator has no `= <expr>`).
func cppEnumerators(n ts.Node, src []byte) []map[string]interface{} {
	var list ts.Node
	for i := 0; i < int(n.ChildCount()); i++ {
		if n.Child(i).Type() == "enumerator_list" {
			list = n.Child(i)
			break
		}
	}
	if list == nil {
		return nil
	}
	var out []map[string]interface{}
	for i := 0; i < int(list.ChildCount()); i++ {
		ch := list.Child(i)
		if ch.Type() != "enumerator" {
			continue
		}
		nameNode := ch.ChildByFieldName("name")
		ename := ""
		if nameNode != nil {
			ename = nodeText(nameNode, src)
		} else {
			// Fallback: first identifier child.
			for j := 0; j < int(ch.ChildCount()); j++ {
				if ch.Child(j).Type() == "identifier" {
					ename = nodeText(ch.Child(j), src)
					break
				}
			}
		}
		if ename == "" {
			continue
		}
		e := map[string]interface{}{"name": ename}
		if valNode := ch.ChildByFieldName("value"); valNode != nil {
			e["value"] = strings.TrimSpace(nodeText(valNode, src))
		} else {
			// Fallback: text after the `=` token, if any.
			sawEq := false
			for j := 0; j < int(ch.ChildCount()); j++ {
				cj := ch.Child(j)
				if cj.Type() == "=" {
					sawEq = true
					continue
				}
				if sawEq {
					e["value"] = strings.TrimSpace(nodeText(cj, src))
					break
				}
			}
		}
		out = append(out, e)
	}
	return out
}

// extractConcept extracts a C++20 concept_definition (wrapped in a
// template_declaration). Emits a SCOPE.Schema/concept entity — the C++
// analogue of an interface constraint.
//
//	tmpl is the enclosing template_declaration (for span/template params),
//	cn   is the concept_definition node carrying the concept name.
func extractConcept(tmpl, cn ts.Node, src []byte, path, lang string) (types.EntityRecord, bool) {
	name := ""
	if nameNode := cn.ChildByFieldName("name"); nameNode != nil {
		name = nodeText(nameNode, src)
	} else {
		// Fallback: first identifier child after the `concept` keyword.
		for i := 0; i < int(cn.ChildCount()); i++ {
			if cn.Child(i).Type() == "identifier" {
				name = nodeText(cn.Child(i), src)
				break
			}
		}
	}
	if name == "" {
		return types.EntityRecord{}, false
	}
	start, end := nodeLines(tmpl)
	meta := map[string]interface{}{"subtype": "concept"}
	if params := cppTemplateParams(tmpl, src); len(params) > 0 {
		meta["template_params"] = params
	}
	return types.EntityRecord{
		Name:         name,
		Kind:         "SCOPE.Schema",
		Subtype:      "concept",
		SourceFile:   path,
		StartLine:    start,
		EndLine:      end,
		Language:     lang,
		QualityScore: 0.9,
		Metadata:     meta,
	}, true
}

// cppTemplateParams returns the names of a template_declaration's parameters
// (e.g. `template<class T, int N>` → ["T","N"]).
func cppTemplateParams(tmpl ts.Node, src []byte) []string {
	list := cppFirstChildOfType(tmpl, "template_parameter_list")
	if list == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(list.ChildCount()); i++ {
		ch := list.Child(i)
		switch ch.Type() {
		case "type_parameter_declaration", "parameter_declaration",
			"optional_type_parameter_declaration", "variadic_type_parameter_declaration":
			for j := 0; j < int(ch.ChildCount()); j++ {
				cj := ch.Child(j)
				if cj.Type() == "type_identifier" || cj.Type() == "identifier" {
					out = append(out, nodeText(cj, src))
				}
			}
		}
	}
	return out
}

// extractInclude extracts a preproc_include node and emits an IMPORTS edge
// carrying the Properties contract shared with java/swift/scala (#120, #93):
//
//	Properties["local_name"]    — leaf identifier (e.g. "vector",
//	                              "stdio.h", "myheader.h"). For paths
//	                              like "foo/bar.h" the leaf is "bar.h".
//	Properties["source_module"] — the dotted/slash prefix, or equal to
//	                              local_name when the include has no
//	                              prefix.
//	Properties["imported_name"] — equal to local_name.
func extractInclude(n ts.Node, src []byte, path, lang string) (types.EntityRecord, bool) {
	// preproc_include children: '#include' keyword + path node
	// path node types: string_literal, system_lib_string
	var pathNode ts.Node
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		child := n.Child(i)
		t := child.Type()
		if t == "string_literal" || t == "system_lib_string" {
			pathNode = child
			break
		}
	}
	if pathNode == nil {
		return types.EntityRecord{}, false
	}
	raw := nodeText(pathNode, src)
	includePath := strings.Trim(raw, `"<>`)
	if includePath == "" {
		return types.EntityRecord{}, false
	}
	leaf := includePath
	mod := includePath
	if slash := strings.LastIndexAny(includePath, "/\\"); slash >= 0 {
		leaf = includePath[slash+1:]
		mod = includePath[:slash]
	}
	props := map[string]string{
		"local_name":    leaf,
		"source_module": mod,
		"imported_name": leaf,
	}
	start, _ := nodeLines(n)
	return types.EntityRecord{
		Name:         includePath,
		Kind:         "SCOPE.Component",
		Subtype:      "import",
		SourceFile:   path,
		StartLine:    start,
		EndLine:      start,
		Language:     lang,
		QualityScore: 1.0,
		Metadata:     map[string]interface{}{"subtype": "import"},
		Relationships: []types.RelationshipRecord{
			{
				FromID:     path,
				ToID:       includePath,
				Kind:       "IMPORTS",
				Properties: props,
			},
		},
	}, true
}

// extractUsing extracts a `using std::cout;` declaration as an IMPORTS edge.
// Plain `using namespace std;` is also captured. Returns a SCOPE.Component
// entity whose Name is the imported leaf. The same Properties contract used
// for #include is applied (`::` is normalised to `.` for source_module).
func extractUsing(n ts.Node, src []byte, path, lang string) (types.EntityRecord, bool) {
	raw := strings.TrimSpace(nodeText(n, src))
	// Strip the leading "using" keyword and trailing ";".
	raw = strings.TrimPrefix(raw, "using")
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, ";")
	raw = strings.TrimSpace(raw)
	// Strip optional "namespace" qualifier.
	raw = strings.TrimPrefix(raw, "namespace")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return types.EntityRecord{}, false
	}
	// Normalise C++ `::` separators to `.` for the property contract.
	dotted := strings.ReplaceAll(raw, "::", ".")
	leaf := dotted
	mod := dotted
	if dot := strings.LastIndexByte(dotted, '.'); dot > 0 {
		leaf = dotted[dot+1:]
		mod = dotted[:dot]
	}
	props := map[string]string{
		"local_name":    leaf,
		"source_module": mod,
		"imported_name": leaf,
	}
	start, _ := nodeLines(n)
	return types.EntityRecord{
		Name:         raw,
		Kind:         "SCOPE.Component",
		Subtype:      "import",
		SourceFile:   path,
		StartLine:    start,
		EndLine:      start,
		Language:     lang,
		QualityScore: 0.9,
		Metadata:     map[string]interface{}{"subtype": "import"},
		Relationships: []types.RelationshipRecord{
			{
				FromID:     path,
				ToID:       raw,
				Kind:       "IMPORTS",
				Properties: props,
			},
		},
	}, true
}

// extractMacro extracts a preproc_def node.
// Name is the macro identifier.
func extractMacro(n ts.Node, src []byte, path, lang string) (types.EntityRecord, bool) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return types.EntityRecord{}, false
	}
	name := nodeText(nameNode, src)
	if name == "" {
		return types.EntityRecord{}, false
	}
	start, _ := nodeLines(n)
	return types.EntityRecord{
		Name:         name,
		Kind:         "SCOPE.Pattern",
		Subtype:      "macro",
		SourceFile:   path,
		StartLine:    start,
		EndLine:      start,
		Language:     lang,
		QualityScore: 0.8,
		Metadata:     map[string]interface{}{"subtype": "macro"},
	}, true
}
