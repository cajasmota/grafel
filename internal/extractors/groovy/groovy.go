// Package groovy implements the tree-sitter–based extractor for Groovy source files.
//
// Extracted entities:
//   - class_declaration    → Kind="SCOPE.Component", Subtype="class"
//   - method_declaration   → Kind="SCOPE.Operation", Subtype="method"
//   - function_definition  → Kind="SCOPE.Operation", Subtype="function"
//   - Gradle apply plugin  → Kind="SCOPE.Component", Subtype="plugin_id"
//   - Gradle task def      → Kind="SCOPE.Operation", Subtype="task"
//
// Uses the groovy grammar from smacker/go-tree-sitter.
// Registers itself via init() and is imported by registry_gen.go.
package groovy

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("groovy", &Extractor{})
}

// Extractor implements extractor.Extractor for Groovy.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "groovy" }

// Extract walks the tree-sitter CST and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if file.Tree == nil || len(file.Content) == 0 {
		return nil, nil
	}

	imports := collectImports(file.Tree.RootNode(), file.Content)
	var entities []types.EntityRecord
	walkGroovy(file.Tree.RootNode(), file, imports, &entities)
	return entities, nil
}

// walkGroovy performs a depth-first traversal collecting entity nodes.
// inClass tracks whether we're inside a class body (for method vs function distinction).
func walkGroovy(node *sitter.Node, file extractor.FileInput, imports []string, out *[]types.EntityRecord) {
	walkGroovyWithContext(node, file, imports, out, false)
}

func walkGroovyWithContext(node *sitter.Node, file extractor.FileInput, imports []string, out *[]types.EntityRecord, inClass bool) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "class_declaration", "class_definition":
		// smacker grammar uses class_definition; Python tree-sitter uses class_declaration.
		if rec, ok := buildClass(node, file, imports); ok {
			*out = append(*out, rec)
		}
		// Walk ALL children with inClass=true (all nested definitions are methods).
		for i := range node.ChildCount() {
			walkGroovyWithContext(node.Child(int(i)), file, imports, out, true)
		}
		return
	case "method_declaration":
		if rec, ok := buildMethod(node, file, imports); ok {
			*out = append(*out, rec)
		}
	case "function_definition":
		if inClass {
			// Inside a class, treat as method (Groovy's `def name()` inside class = method).
			if rec, ok := buildFunctionAsMethod(node, file, imports); ok {
				*out = append(*out, rec)
			}
		} else {
			if rec, ok := buildFunction(node, file, imports); ok {
				*out = append(*out, rec)
			}
		}
	case "declaration":
		// Gradle DSL: `task taskName { ... }` parses as a declaration node whose
		// first identifier is "task" and second identifier is the task name.
		if rec, ok := buildGradleTaskDeclaration(node, file, imports); ok {
			*out = append(*out, rec)
		}
	case "juxt_function_call":
		// Gradle DSL: `apply plugin: 'com.example.id'` parses as juxt_function_call.
		// Gradle DSL: `task taskName(type: X) { ... }` also parses as juxt_function_call.
		if rec, ok := buildGradleApplyPlugin(node, file, imports); ok {
			*out = append(*out, rec)
		}
		if rec, ok := buildGradleTaskJuxt(node, file, imports); ok {
			*out = append(*out, rec)
		}
	}

	for i := range node.ChildCount() {
		walkGroovyWithContext(node.Child(int(i)), file, imports, out, inClass)
	}
}

func nodeText(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	return string(src[node.StartByte():node.EndByte()])
}

func childByType(node *sitter.Node, types_ ...string) *sitter.Node {
	set := make(map[string]bool, len(types_))
	for _, t := range types_ {
		set[t] = true
	}
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch != nil && set[ch.Type()] {
			return ch
		}
	}
	return nil
}

func buildClass(node *sitter.Node, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
	nameNode := childByType(node, "identifier")
	if nameNode == nil {
		return types.EntityRecord{}, false
	}
	name := nodeText(nameNode, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Component",
		Subtype:            "class",
		SourceFile:         file.Path,
		Language:           "groovy",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "class " + name,
		EnrichmentRequired: false,
		Properties: map[string]string{
			"imports": strings.Join(imports, ","),
		},
	}, true
}

func buildMethod(node *sitter.Node, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
	name, sig := extractMethodSignature(node, file.Content)
	if name == "" || name == "?" {
		return types.EntityRecord{}, false
	}
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            "method",
		SourceFile:         file.Path,
		Language:           "groovy",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          sig,
		EnrichmentRequired: false,
		Properties: map[string]string{
			"imports": strings.Join(imports, ","),
		},
	}, true
}

// buildFunctionAsMethod handles function_definition nodes inside a class body,
// treating them as methods with proper signature extraction.
func buildFunctionAsMethod(node *sitter.Node, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
	nameNode := childByType(node, "identifier")
	if nameNode == nil {
		return types.EntityRecord{}, false
	}
	name := nodeText(nameNode, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	sig := normalizeMethodSignature(extractGroovySignature(node, file.Content))
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            "method",
		SourceFile:         file.Path,
		Language:           "groovy",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          sig,
		EnrichmentRequired: false,
		Properties: map[string]string{
			"imports": strings.Join(imports, ","),
		},
	}, true
}

// extractGroovySignature extracts the raw signature from a Groovy function/method node.
func extractGroovySignature(node *sitter.Node, src []byte) string {
	text := nodeText(node, src)
	// Find the opening brace and take everything before it.
	braceIdx := strings.Index(text, "{")
	if braceIdx > 0 {
		text = strings.TrimSpace(text[:braceIdx])
	}
	// If multi-line, take just the first line.
	if nlIdx := strings.Index(text, "\n"); nlIdx > 0 {
		text = strings.TrimSpace(text[:nlIdx])
	}
	return text
}

// normalizeMethodSignature normalizes a Groovy class method signature to match
// Python golden format:
//   - Replaces concrete return types (boolean, int, etc.) with "def"
//   - Appends " def" suffix for untyped methods
func normalizeMethodSignature(sig string) string {
	if strings.HasPrefix(sig, "def ") || strings.Contains(sig, " def ") {
		// Untyped method — add " def" suffix if not already present.
		if !strings.HasSuffix(sig, " def") {
			sig += " def"
		}
	} else {
		// Typed method (e.g., "private boolean validateEmail(...)").
		// Replace the concrete return type with "def" to match Python golden.
		parenIdx := strings.Index(sig, "(")
		if parenIdx > 0 {
			prefix := sig[:parenIdx]
			suffix := sig[parenIdx:]
			parts := strings.Fields(prefix)
			if len(parts) >= 2 {
				parts[len(parts)-2] = "def"
				sig = strings.Join(parts, " ") + suffix
			}
		}
	}
	return sig
}

func buildFunction(node *sitter.Node, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
	nameNode := childByType(node, "identifier")
	if nameNode == nil {
		return types.EntityRecord{}, false
	}
	name := nodeText(nameNode, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	sig := extractGroovySignature(node, file.Content)
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            "function",
		SourceFile:         file.Path,
		Language:           "groovy",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          sig,
		EnrichmentRequired: false,
		Properties: map[string]string{
			"imports": strings.Join(imports, ","),
		},
	}, true
}

// extractMethodSignature builds (name, signature) for a method_declaration node.
func extractMethodSignature(node *sitter.Node, src []byte) (string, string) {
	nameNode := childByType(node, "identifier")
	name := "?"
	if nameNode != nil {
		name = nodeText(nameNode, src)
	}

	paramsNode := childByType(node, "formal_parameters")
	params := "()"
	if paramsNode != nil {
		params = nodeText(paramsNode, src)
	}

	// Return type node.
	typeNode := childByType(node, "type_identifier", "void_type", "generic_type")
	returnType := ""
	if typeNode != nil {
		returnType = " " + nodeText(typeNode, src)
	}

	// Modifiers.
	modifiersNode := childByType(node, "modifiers")
	modifiers := ""
	if modifiersNode != nil {
		var parts []string
		for i := range modifiersNode.ChildCount() {
			ch := modifiersNode.Child(int(i))
			if ch == nil {
				continue
			}
			switch ch.Type() {
			case "public", "private", "protected", "static", "abstract", "final", "synchronized":
				parts = append(parts, nodeText(ch, src))
			}
		}
		if len(parts) > 0 {
			modifiers = strings.Join(parts, " ") + " "
		}
	}

	sig := strings.TrimSpace(modifiers + "def " + name + params + returnType)
	return name, sig
}

// collectImports gathers import_declaration nodes.
func collectImports(root *sitter.Node, src []byte) []string {
	var imports []string
	walkForImports(root, src, &imports)
	return imports
}

func walkForImports(node *sitter.Node, src []byte, out *[]string) {
	if node == nil {
		return
	}
	// smacker Groovy grammar uses "groovy_import"; some grammars use "import_declaration".
	if node.Type() == "import_declaration" || node.Type() == "groovy_import" {
		text := strings.TrimSpace(nodeText(node, src))
		if strings.HasPrefix(text, "import ") {
			*out = append(*out, strings.TrimSuffix(text[7:], ";"))
		}
	}
	for i := range node.ChildCount() {
		walkForImports(node.Child(int(i)), src, out)
	}
}

// buildGradleTaskDeclaration detects `task taskName { ... }` — the smacker
// Groovy grammar parses this as a declaration node whose first identifier
// child is "task" and second identifier child is the task name.
func buildGradleTaskDeclaration(node *sitter.Node, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
	// Collect all direct identifier children.
	var ids []string
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch != nil && ch.Type() == "identifier" {
			ids = append(ids, nodeText(ch, file.Content))
		}
	}
	// We need exactly "task" followed by a task name.
	if len(ids) < 2 || ids[0] != "task" {
		return types.EntityRecord{}, false
	}
	name := ids[1]
	if name == "" {
		return types.EntityRecord{}, false
	}
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            "task",
		SourceFile:         file.Path,
		Language:           "groovy",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "task " + name,
		EnrichmentRequired: false,
		Properties: map[string]string{
			"imports":    strings.Join(imports, ","),
			"gradle_dsl": "task",
		},
	}, true
}

// buildGradleApplyPlugin detects `apply plugin: 'com.example.id'` — the
// smacker grammar parses this as a juxt_function_call whose first identifier
// child is "apply" and whose argument list contains a map_item with key
// "plugin" and a string value.
func buildGradleApplyPlugin(node *sitter.Node, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
	if node.ChildCount() < 2 {
		return types.EntityRecord{}, false
	}
	// First child must be identifier "apply".
	first := node.Child(0)
	if first == nil || first.Type() != "identifier" || nodeText(first, file.Content) != "apply" {
		return types.EntityRecord{}, false
	}
	// Second child should be argument_list.
	argList := node.Child(1)
	if argList == nil || argList.Type() != "argument_list" {
		return types.EntityRecord{}, false
	}
	// Walk argument_list for a map_item with key "plugin".
	pluginID := extractPluginIDFromArgList(argList, file.Content)
	if pluginID == "" {
		return types.EntityRecord{}, false
	}
	return types.EntityRecord{
		Name:               pluginID,
		Kind:               "SCOPE.Component",
		Subtype:            "plugin_id",
		SourceFile:         file.Path,
		Language:           "groovy",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "apply plugin: '" + pluginID + "'",
		EnrichmentRequired: false,
		Properties: map[string]string{
			"imports":    strings.Join(imports, ","),
			"gradle_dsl": "apply_plugin",
		},
	}, true
}

// extractPluginIDFromArgList walks an argument_list node looking for a
// map_item whose key identifier is "plugin" and returns its string value.
func extractPluginIDFromArgList(argList *sitter.Node, src []byte) string {
	for i := range argList.ChildCount() {
		child := argList.Child(int(i))
		if child == nil {
			continue
		}
		if child.Type() == "map_item" {
			pluginID := extractPluginIDFromMapItem(child, src)
			if pluginID != "" {
				return pluginID
			}
		}
	}
	return ""
}

// extractPluginIDFromMapItem checks a map_item node: key must be identifier
// "plugin", value must be a string node — returns the string content.
func extractPluginIDFromMapItem(mapItem *sitter.Node, src []byte) string {
	var keyNode, valNode *sitter.Node
	for i := range mapItem.ChildCount() {
		ch := mapItem.Child(int(i))
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "identifier":
			if keyNode == nil {
				keyNode = ch
			}
		case "string":
			valNode = ch
		}
	}
	if keyNode == nil || nodeText(keyNode, src) != "plugin" {
		return ""
	}
	if valNode == nil {
		return ""
	}
	// Extract string_content child of the string node.
	for i := range valNode.ChildCount() {
		ch := valNode.Child(int(i))
		if ch != nil && ch.Type() == "string_content" {
			return nodeText(ch, src)
		}
	}
	// Fallback: strip surrounding quotes from raw string text.
	raw := nodeText(valNode, src)
	raw = strings.Trim(raw, `'"`)
	return raw
}

// buildGradleTaskJuxt detects `task taskName(type: X) { ... }` — the smacker
// grammar parses this as a juxt_function_call whose first identifier is "task"
// and whose argument_list contains a function_call whose first identifier is
// the task name.
func buildGradleTaskJuxt(node *sitter.Node, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
	if node.ChildCount() < 2 {
		return types.EntityRecord{}, false
	}
	// First child must be identifier "task".
	first := node.Child(0)
	if first == nil || first.Type() != "identifier" || nodeText(first, file.Content) != "task" {
		return types.EntityRecord{}, false
	}
	// Second child should be argument_list containing a function_call.
	argList := node.Child(1)
	if argList == nil || argList.Type() != "argument_list" {
		return types.EntityRecord{}, false
	}
	// Walk argument_list for a function_call whose identifier is the task name.
	for i := range argList.ChildCount() {
		child := argList.Child(int(i))
		if child == nil {
			continue
		}
		if child.Type() == "function_call" {
			nameNode := childByType(child, "identifier")
			if nameNode == nil {
				continue
			}
			name := nodeText(nameNode, file.Content)
			if name == "" || name == "task" {
				continue
			}
			return types.EntityRecord{
				Name:               name,
				Kind:               "SCOPE.Operation",
				Subtype:            "task",
				SourceFile:         file.Path,
				Language:           "groovy",
				StartLine:          int(node.StartPoint().Row) + 1,
				EndLine:            int(node.EndPoint().Row) + 1,
				Signature:          "task " + name,
				EnrichmentRequired: false,
				Properties: map[string]string{
					"imports":    strings.Join(imports, ","),
					"gradle_dsl": "task",
				},
			}, true
		}
	}
	return types.EntityRecord{}, false
}
