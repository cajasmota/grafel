// Package cpp implements the C and C++ language extractor for archigraph.
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
// OTel span: "indexer.extract.cpp" with attributes language, file_line_count, entity_count.
package cpp

import (
	"bytes"
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
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

	tree := file.Tree
	if tree == nil {
		parser := sitter.NewParser()
		if lang == "c" {
			parser.SetLanguage(cGrammar())
		} else {
			parser.SetLanguage(cppGrammar())
		}
		var err error
		tree, err = parser.ParseCtx(ctx, nil, file.Content)
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

	// Walk the AST collecting all target node types.
	walk(root, func(n *sitter.Node) {
		switch n.Type() {
		case "function_definition":
			if r, ok := extractFunction(n, file.Content, file.Path, lang); ok {
				records = append(records, r)
			}
		case "class_specifier":
			if r, ok := extractClassLike(n, file.Content, file.Path, lang, "class"); ok {
				records = append(records, r)
			}
		case "struct_specifier":
			if r, ok := extractClassLike(n, file.Content, file.Path, lang, "struct"); ok {
				records = append(records, r)
			}
		case "union_specifier":
			if r, ok := extractClassLike(n, file.Content, file.Path, lang, "union"); ok {
				records = append(records, r)
			}
		case "namespace_definition":
			if r, ok := extractNamespace(n, file.Content, file.Path, lang); ok {
				records = append(records, r)
			}
		case "template_declaration":
			if r, ok := extractTemplate(n, file.Content, file.Path, lang); ok {
				records = append(records, r)
			}
		case "enum_specifier":
			if r, ok := extractEnum(n, file.Content, file.Path, lang); ok {
				records = append(records, r)
			}
		case "preproc_include":
			if r, ok := extractInclude(n, file.Content, file.Path, lang); ok {
				records = append(records, r)
			}
		case "preproc_def":
			if r, ok := extractMacro(n, file.Content, file.Path, lang); ok {
				records = append(records, r)
			}
		}
	})

	span.SetAttributes(
		attribute.String("language", lang),
		attribute.Int("file_line_count", lineCount),
		attribute.Int("entity_count", len(records)),
	)
	return records, nil
}

// ----------------------------------------------------------------
// Walker
// ----------------------------------------------------------------

// walk performs a depth-first traversal calling fn on every node.
// Iterative to avoid stack overflow on large files.
func walk(root *sitter.Node, fn func(*sitter.Node)) {
	if root == nil {
		return
	}
	stack := []*sitter.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		fn(n)
		count := int(n.ChildCount())
		for i := count - 1; i >= 0; i-- {
			stack = append(stack, n.Child(i))
		}
	}
}

// findFirst returns the first descendant node (depth-first) matching any of the given types.
func findFirst(root *sitter.Node, types ...string) *sitter.Node {
	if root == nil {
		return nil
	}
	typeSet := make(map[string]bool, len(types))
	for _, t := range types {
		typeSet[t] = true
	}
	stack := []*sitter.Node{root}
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

func nodeText(n *sitter.Node, src []byte) string {
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

func nodeLines(n *sitter.Node) (int, int) {
	return int(n.StartPoint().Row) + 1, int(n.EndPoint().Row) + 1
}

// ----------------------------------------------------------------
// Entity extractors
// ----------------------------------------------------------------

// extractFunction extracts a function_definition node.
// The declarator field gives us the function name via function_declarator → identifier.
func extractFunction(n *sitter.Node, src []byte, path, lang string) (types.EntityRecord, bool) {
	decl := n.ChildByFieldName("declarator")
	if decl == nil {
		return types.EntityRecord{}, false
	}
	name := resolveFunctionName(decl, src)
	if name == "" {
		return types.EntityRecord{}, false
	}
	start, end := nodeLines(n)
	return types.EntityRecord{
		Name:         name,
		Kind:         "SCOPE.Operation",
		Subtype:      "function",
		SourceFile:   path,
		StartLine:    start,
		EndLine:      end,
		Language:     lang,
		QualityScore: 0.9,
		Metadata:     map[string]interface{}{"subtype": "function"},
	}, true
}

// resolveFunctionName drills through pointer/ref declarators to the
// function_declarator and returns its identifier.
func resolveFunctionName(decl *sitter.Node, src []byte) string {
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

// resolveIdentifier extracts the name text from identifier-like nodes,
// including qualified names (scope_resolution).
func resolveIdentifier(n *sitter.Node, src []byte) string {
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
			return nodeText(name, src)
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
func extractClassLike(n *sitter.Node, src []byte, path, lang, subtype string) (types.EntityRecord, bool) {
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
	return types.EntityRecord{
		Name:         name,
		Kind:         "SCOPE.Component",
		Subtype:      subtype,
		SourceFile:   path,
		StartLine:    start,
		EndLine:      end,
		Language:     lang,
		QualityScore: 0.9,
		Metadata:     map[string]interface{}{"subtype": subtype},
	}, true
}

// extractNamespace extracts a namespace_definition node.
func extractNamespace(n *sitter.Node, src []byte, path, lang string) (types.EntityRecord, bool) {
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
func extractTemplate(n *sitter.Node, src []byte, path, lang string) (types.EntityRecord, bool) {
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
func extractEnum(n *sitter.Node, src []byte, path, lang string) (types.EntityRecord, bool) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return types.EntityRecord{}, false
	}
	name := nodeText(nameNode, src)
	if name == "" {
		return types.EntityRecord{}, false
	}
	start, end := nodeLines(n)
	return types.EntityRecord{
		Name:         name,
		Kind:         "SCOPE.Schema",
		Subtype:      "enum",
		SourceFile:   path,
		StartLine:    start,
		EndLine:      end,
		Language:     lang,
		QualityScore: 0.9,
		Metadata:     map[string]interface{}{"subtype": "enum"},
	}, true
}

// extractInclude extracts a preproc_include node.
// Name is the included path string (with quotes/angle brackets stripped).
func extractInclude(n *sitter.Node, src []byte, path, lang string) (types.EntityRecord, bool) {
	// preproc_include children: '#include' keyword + path node
	// path node types: string_literal, system_lib_string
	var pathNode *sitter.Node
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
				FromID: path,
				ToID:   includePath,
				Kind:   "IMPORTS",
			},
		},
	}, true
}

// extractMacro extracts a preproc_def node.
// Name is the macro identifier.
func extractMacro(n *sitter.Node, src []byte, path, lang string) (types.EntityRecord, bool) {
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
