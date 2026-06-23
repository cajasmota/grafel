// Package groovy implements the tree-sitter–based extractor for Groovy source files.
//
// Extracted entities:
//   - class_declaration    → Kind="SCOPE.Component", Subtype="class"
//   - method_declaration   → Kind="SCOPE.Operation", Subtype="method"
//   - function_definition  → Kind="SCOPE.Operation", Subtype="function"
//   - Gradle apply plugin  → Kind="SCOPE.Component", Subtype="plugin_id"
//   - Gradle task def      → Kind="SCOPE.Operation", Subtype="task"
//   - groovy_import        → IMPORTS relationships
//
// Issue #372 — relationship parity with java/kotlin/scala:
//
//   - IMPORTS edges carry the same Properties contract Python emits
//     (#93): local_name, source_module, imported_name, wildcard. Aliased
//     imports (`import foo.Bar as Baz`) preserve the original
//     imported_name while local_name reflects the alias. Static imports
//     are tagged with import_kind="static".
//   - CALLS edges are emitted per function_call descendant of every
//     method/function body. When the call target uses a dotted_identifier
//     receiver and the receiver's first segment is PascalCase, the target
//     is emitted as the dotted "<Type>.<method>" form and Properties
//     carries `receiver_type=<Type>`. Self-recursion is dropped, matching
//     java/scala/kotlin dedup semantics.
//   - CONTAINS edges are attached from each class component to every
//     method declared in its body, using the canonical Format A
//     structural-ref (`scope:operation:method:groovy:<file>:<name>`).
//
// Uses the groovy grammar from smacker/go-tree-sitter.
// Registers itself via init() and is imported by registry_gen.go.
package groovy

import (
	"context"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
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
	if file.TSTree == nil || len(file.Content) == 0 {
		return nil, nil
	}

	root := file.TSTree.RootNode()
	imports := collectImports(root, file.Content)
	var entities []types.EntityRecord
	// Issue #372: emit IMPORTS edges as standalone SCOPE.Component records
	// matching the scala/elixir extractor pattern.
	entities = append(entities, buildImportRecords(root, file)...)
	walkGroovy(root, file, imports, &entities)
	// Issue #90 — tag every relationship with language="groovy".
	extractor.TagRelationshipsLanguage(entities, "groovy")
	extractor.TagEntitiesLanguage(entities, "groovy")
	return entities, nil
}

// walkGroovy performs a depth-first traversal collecting entity nodes.
// inClass tracks whether we're inside a class body (for method vs function distinction).
func walkGroovy(node ts.Node, file extractor.FileInput, imports []string, out *[]types.EntityRecord) {
	walkGroovyWithContext(node, file, imports, out, false)
}

func walkGroovyWithContext(node ts.Node, file extractor.FileInput, imports []string, out *[]types.EntityRecord, inClass bool) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "class_declaration", "class_definition":
		// smacker grammar uses class_definition; Python tree-sitter uses class_declaration.
		classIdx := -1
		if rec, ok := buildClass(node, file, imports); ok {
			classIdx = len(*out)
			*out = append(*out, rec)
		}
		// Walk ALL children with inClass=true (all nested definitions are methods).
		before := len(*out)
		for i := range node.ChildCount() {
			walkGroovyWithContext(node.Child(int(i)), file, imports, out, true)
		}
		after := len(*out)
		// Issue #372: attach CONTAINS edges from this class to every
		// SCOPE.Operation emitted from its body.
		if classIdx >= 0 {
			for k := before; k < after; k++ {
				child := &(*out)[k]
				if child.Kind != "SCOPE.Operation" {
					continue
				}
				toID := extractor.BuildOperationStructuralRef("groovy", file.Path, child.Name)
				(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
					types.RelationshipRecord{
						ToID: toID,
						Kind: "CONTAINS",
					})
			}
		}
		return
	case "method_declaration":
		if rec, ok := buildMethod(node, file, imports); ok {
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(findFunctionBody(node), file.Content, rec.Name)...)
			*out = append(*out, rec)
		}
	case "function_definition":
		if inClass {
			// Inside a class, treat as method (Groovy's `def name()` inside class = method).
			if rec, ok := buildFunctionAsMethod(node, file, imports); ok {
				rec.Relationships = append(rec.Relationships,
					extractCallRelationships(findFunctionBody(node), file.Content, rec.Name)...)
				*out = append(*out, rec)
			}
		} else {
			if rec, ok := buildFunction(node, file, imports); ok {
				rec.Relationships = append(rec.Relationships,
					extractCallRelationships(findFunctionBody(node), file.Content, rec.Name)...)
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

	// #4914 — Groovy enum value-set. The grammar has no dedicated enum node:
	// `enum X {…}` parses as an `enum`/`X` header pair whose body is the
	// following `closure` sibling. Two header shapes are accepted:
	//   - legacy smacker: a single `declaration[ identifier(enum), identifier(X) ]`
	//   - current official (regenerated grammar): two bare `identifier`
	//     siblings `enum` then `X` directly under the container.
	// Detect enum headers among THIS node's children (where the
	// header/closure sibling pair lives) and emit one SCOPE.Enum value-set
	// each. Done here rather than in the `declaration` case because the case
	// handler only receives the header node, not its parent/sibling context.
	// See types.go.
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch == nil {
			continue
		}
		// Legacy: declaration-wrapped header. Official: a bare `enum`
		// identifier whose name + body follow as siblings.
		isEnumHead := ch.Type() == "declaration" ||
			(ch.Type() == "identifier" && nodeText(ch, file.Content) == "enum")
		if !isEnumHead {
			continue
		}
		if rec, ok := buildGroovyEnumValueSet(node, i, file); ok {
			*out = append(*out, rec)
		}
	}

	// Gradle task DSL — official grammar shape. `task clean { … }` and
	// `task compileGroovy(type: X) { … }` now parse as a bare `identifier`
	// ("task") followed by a sibling `juxt_function_call`/`function_call`
	// whose `function` child is the task name (the legacy smacker grammar
	// instead nested everything under one declaration/juxt_function_call,
	// handled by the case clauses above). Pair the `task` keyword with its
	// following call sibling here, where sibling context is available.
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch == nil || ch.Type() != "identifier" || nodeText(ch, file.Content) != "task" {
			continue
		}
		if rec, ok := buildGradleTaskSibling(node, i, file, imports); ok {
			*out = append(*out, rec)
		}
	}

	for i := range node.ChildCount() {
		walkGroovyWithContext(node.Child(int(i)), file, imports, out, inClass)
	}
}

// buildGradleTaskSibling pairs a bare `task` identifier at taskIdx with the
// following `juxt_function_call`/`function_call` sibling whose `function`
// child names the task (the official grammar shape). Returns ok=false when no
// such call sibling is found before the next statement boundary.
func buildGradleTaskSibling(parent ts.Node, taskIdx int, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
	for i := taskIdx + 1; i < int(parent.ChildCount()); i++ {
		ch := parent.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "juxt_function_call", "function_call":
			fn := ch.ChildByFieldName("function")
			if fn == nil {
				fn = childByType(ch, "identifier")
			}
			if fn == nil {
				return types.EntityRecord{}, false
			}
			name := nodeText(fn, file.Content)
			if name == "" || name == "task" {
				return types.EntityRecord{}, false
			}
			return types.EntityRecord{
				Name:               name,
				Kind:               "SCOPE.Operation",
				Subtype:            "task",
				SourceFile:         file.Path,
				Language:           "groovy",
				StartLine:          int(parent.Child(taskIdx).StartPoint().Row) + 1,
				EndLine:            int(ch.EndPoint().Row) + 1,
				Signature:          "task " + name,
				EnrichmentRequired: false,
				Properties: map[string]string{
					"imports":    strings.Join(imports, ","),
					"gradle_dsl": "task",
				},
			}, true
		case "identifier", "closure":
			// `task` directly followed by its name/body without a call wrapper
			// is not the shape we handle here; stop scanning.
			return types.EntityRecord{}, false
		}
	}
	return types.EntityRecord{}, false
}

func nodeText(node ts.Node, src []byte) string {
	if node == nil {
		return ""
	}
	return string(src[node.StartByte():node.EndByte()])
}

func childByType(node ts.Node, types_ ...string) ts.Node {
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

func buildClass(node ts.Node, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
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

func buildMethod(node ts.Node, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
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
func buildFunctionAsMethod(node ts.Node, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
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
func extractGroovySignature(node ts.Node, src []byte) string {
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

func buildFunction(node ts.Node, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
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
func extractMethodSignature(node ts.Node, src []byte) (string, string) {
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
func collectImports(root ts.Node, src []byte) []string {
	var imports []string
	walkForImports(root, src, &imports)
	return imports
}

func walkForImports(node ts.Node, src []byte, out *[]string) {
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
func buildGradleTaskDeclaration(node ts.Node, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
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
func buildGradleApplyPlugin(node ts.Node, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
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
func extractPluginIDFromArgList(argList ts.Node, src []byte) string {
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
func extractPluginIDFromMapItem(mapItem ts.Node, src []byte) string {
	var keyNode, valNode ts.Node
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
func buildGradleTaskJuxt(node ts.Node, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
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
	// Walk argument_list for the task name. The smacker grammar nested it
	// in a `function_call` (`task foo(type: X)`); the official grammar's
	// no-paren form (`task clean { … }`) puts the name as a bare
	// `identifier` argument (the body closure is a detached sibling).
	for i := range argList.ChildCount() {
		child := argList.Child(int(i))
		if child == nil {
			continue
		}
		var nameNode ts.Node
		switch child.Type() {
		case "function_call":
			nameNode = childByType(child, "identifier")
		case "identifier":
			nameNode = child
		}
		if nameNode != nil {
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

// groovyKeywordStop lists groovy keywords / special identifiers that the
// parser surfaces as call_expression heads but are not real call targets.
// Mirrors the kotlin/scala extractors' drop list (#106, #379).
var groovyKeywordStop = map[string]bool{
	"this":  true,
	"super": true,
	"new":   true,
}

// findFunctionBody returns the closure child of a function_definition /
// method_declaration that holds the call expressions, or nil when the
// declaration has no body (abstract/interface method).
func findFunctionBody(node ts.Node) ts.Node {
	if node == nil {
		return nil
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch == nil {
			continue
		}
		// smacker groovy grammar wraps the body in a "closure" node;
		// some grammars use "block".
		if ch.Type() == "closure" || ch.Type() == "block" {
			return ch
		}
	}
	return nil
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
		if n == nil {
			continue
		}
		if set[n.Type()] {
			out = append(out, n)
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return out
}

// extractCallRelationships returns one CALLS RelationshipRecord per
// unique function_call descendant of body. Self-recursion is dropped.
//
// In smacker/go-tree-sitter/groovy, function_call has either:
//
//   - identifier         → bare-name call (helper())
//   - dotted_identifier  → obj.method() — the trailing identifier is the
//     method, the leading identifier is the receiver. When the receiver
//     is PascalCase, the target is emitted as "<Type>.<method>" with
//     Properties[receiver_type]=<Type>.
//   - function_call      → curried call (`f()(x)`); recurse to find the
//     leaf method name.
func extractCallRelationships(body ts.Node, src []byte, callerName string) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	calls := findAllNodes(body, "function_call")
	if len(calls) == 0 {
		return nil
	}
	// #4749 (Groovy slice of the coverage-linkage tail epic #4749/#4615; the JVM
	// analog of Java #4682). Build a local-variable → constructed-type map from
	// `def c = new FooController(...)` / `FooController c = new FooController()`
	// declarations in this body so a later `c.index()` receiver call resolves to
	// the class method `FooController.index` (test→CALLS crediting) instead of
	// degrading to the bare leaf `index`. Only a DIRECT `new ClassName(...)`
	// initialiser types the local — a factory/builder RHS (`def c =
	// MyFactory.create()`) leaves the local UNtyped (no fabricated edge),
	// mirroring the Java #4682 conservatism.
	localVarTypes := collectGroovyLocalVarTypes(body, src)
	// #4749 — the `function_call` that is the operand of a `new` unary_op is a
	// CONSTRUCTOR (`new FooController()`), not an outbound method call; emitting
	// it as a bare `FooController` CALLS edge is a phantom. Collect those nodes
	// so they can be skipped (the type is already lifted into localVarTypes).
	ctorCalls := groovyConstructorCalls(body)
	seen := make(map[string]bool, len(calls))
	rels := make([]types.RelationshipRecord, 0, len(calls))
	for _, call := range calls {
		if ctorCalls[spanOf(call)] {
			continue
		}
		target, recv := groovyCallTargetWithLocals(call, src, localVarTypes)
		if target == "" {
			continue
		}
		if groovyKeywordStop[target] {
			continue
		}
		// Self-recursion check: skip bare-name targets that match the
		// caller's own leaf name (e.g. `process()` calling itself without
		// a receiver). Dotted targets (e.g. "OrderService.process") are
		// cross-type calls and MUST NOT be filtered even when the leaf
		// matches the caller's name — "OrderController.process" calling
		// "OrderService.process" is a legitimate outbound call, not
		// recursion (#2114). The previous check applied the leaf match
		// to all dotted targets, which incorrectly dropped every CALLS
		// edge where the callee method shared its name with the enclosing
		// method.
		if strings.IndexByte(target, '.') < 0 && target == callerName {
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		rel := types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
			Properties: map[string]string{
				"line": strconv.Itoa(int(call.StartPoint().Row) + 1),
			},
		}
		if recv != "" {
			rel.Properties["receiver_type"] = recv
		}
		rels = append(rels, rel)
	}
	return rels
}

// groovyCallTarget resolves the callee target of a function_call node.
// Returns (target, receiverType). receiverType is non-empty only when a
// dotted_identifier receiver looks like a PascalCase type. This is the
// no-locals convenience wrapper retained for callers that don't carry a
// local-variable type table.
func groovyCallTarget(call ts.Node, src []byte) (string, string) {
	return groovyCallTargetWithLocals(call, src, nil)
}

// groovyCallTargetWithLocals is groovyCallTarget extended with a
// local-variable → type table (#4749). When the receiver of an `obj.method()`
// call is a local whose type was inferred from a `new ClassName(...)`
// initialiser, the target is resolved to `<Type>.<method>` (with
// receiver_type=<Type>), exactly as a PascalCase static-receiver call is. The
// table may be nil.
func groovyCallTargetWithLocals(call ts.Node, src []byte, localVarTypes map[string]string) (string, string) {
	if call == nil || call.ChildCount() == 0 {
		return "", ""
	}
	first := call.Child(0)
	if first == nil {
		return "", ""
	}
	switch first.Type() {
	case "identifier":
		return nodeText(first, src), ""
	case "dotted_identifier":
		// dotted_identifier children: identifier "." identifier ...
		// The trailing identifier is the method, the leading identifier
		// is the receiver. Nested function_call children (`Service().run`)
		// indicate chained-call receivers — fall back to bare leaf.
		var idents []ts.Node
		hasNestedCall := false
		for i := 0; i < int(first.ChildCount()); i++ {
			ch := first.Child(i)
			if ch == nil {
				continue
			}
			switch ch.Type() {
			case "identifier":
				idents = append(idents, ch)
			case "function_call", "dotted_identifier":
				hasNestedCall = true
			}
		}
		if len(idents) == 0 {
			return "", ""
		}
		methodNode := idents[len(idents)-1]
		method := nodeText(methodNode, src)
		if method == "" {
			return "", ""
		}
		if hasNestedCall || len(idents) < 2 {
			return method, ""
		}
		receiver := nodeText(idents[0], src)
		// PascalCase static-call shape: `Module.method`.
		if isPascalCase(receiver) {
			return receiver + "." + method, receiver
		}
		// #4749 — local-variable receiver typing: `def c = new FooController();
		// c.index()` → FooController.index. Only fires when the local was typed
		// from a DIRECT `new ClassName(...)` initialiser (factory/builder RHS
		// stays untyped), so no class edge is ever forged.
		if typ, ok := localVarTypes[receiver]; ok && typ != "" {
			return typ + "." + method, typ
		}
		return method, ""
	case "function_call":
		// Curried call — recurse.
		return groovyCallTargetWithLocals(first, src, localVarTypes)
	}
	return "", ""
}

// groovyConstructorCalls returns the set of function_call nodes that are the
// operand of a `new` unary_op (i.e. constructor calls `new ClassName(...)`).
// These are object instantiations, not outbound method CALLS, so they are
// excluded from the CALLS edge set (#4749).
// nodeSpan is a stable identity key for a CST node within one parse tree:
// its (start, end) byte offsets. Required because the official tree-sitter
// binding returns a fresh wrapper struct per accessor call, so `ts.Node`
// values are not usable as comparable map keys (B2 #5418).
type nodeSpan struct{ start, end uint32 }

func spanOf(n ts.Node) nodeSpan { return nodeSpan{n.StartByte(), n.EndByte()} }

func groovyConstructorCalls(body ts.Node) map[nodeSpan]bool {
	if body == nil {
		return nil
	}
	out := map[nodeSpan]bool{}
	for _, uo := range findAllNodes(body, "unary_op") {
		hasNew := false
		for j := 0; j < int(uo.ChildCount()); j++ {
			if c := uo.Child(j); c != nil && c.Type() == "new" {
				hasNew = true
				break
			}
		}
		if !hasNew {
			continue
		}
		for j := 0; j < int(uo.ChildCount()); j++ {
			if c := uo.Child(j); c != nil && c.Type() == "function_call" {
				out[spanOf(c)] = true
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// collectGroovyLocalVarTypes scans a method/function body for local-variable
// declarations whose right-hand side is a DIRECT constructor call
// (`new ClassName(...)`) and returns a varName → ClassName map (#4749).
//
// Two declaration shapes are recognised (both confirmed against the smacker
// groovy grammar):
//
//	def c = new FooController(svc)         → declaration[def, id(c), =, unary_op[new, function_call[id(FooController)]]]
//	FooController c = new FooController()  → declaration[id(FooController), id(c), =, unary_op[new, function_call[id(FooController)]]]
//
// The variable name is the `identifier` immediately preceding the `=`; the type
// is the leading identifier of the `function_call` inside the `new` unary_op.
// A declaration with no `new` unary_op (factory/builder RHS) types nothing —
// the local is left out of the map, so its later receiver calls degrade to the
// bare leaf and no spurious `<Class>.method` edge is forged (the Java #4682
// negative-case guarantee).
func collectGroovyLocalVarTypes(body ts.Node, src []byte) map[string]string {
	if body == nil {
		return nil
	}
	out := map[string]string{}
	for _, decl := range findAllNodes(body, "declaration") {
		varName, typeName := groovyDeclVarAndNewType(decl, src)
		if varName == "" || typeName == "" {
			continue
		}
		// PascalCase-only: a constructed type is a class name. Guards against
		// edge-cases where the RHS leading identifier is not a type.
		if !isPascalCase(typeName) {
			continue
		}
		out[varName] = typeName
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// groovyDeclVarAndNewType returns (varName, ClassName) for a `declaration` node
// whose RHS is a `new ClassName(...)` constructor, or ("","") otherwise.
func groovyDeclVarAndNewType(decl ts.Node, src []byte) (string, string) {
	// Find the `=` position; the var name is the identifier just before it.
	eqIdx := -1
	for i := 0; i < int(decl.ChildCount()); i++ {
		if ch := decl.Child(i); ch != nil && ch.Type() == "=" {
			eqIdx = i
			break
		}
	}
	if eqIdx <= 0 {
		return "", ""
	}
	var varNode ts.Node
	for i := eqIdx - 1; i >= 0; i-- {
		if ch := decl.Child(i); ch != nil && ch.Type() == "identifier" {
			varNode = ch
			break
		}
	}
	if varNode == nil {
		return "", ""
	}
	varName := nodeText(varNode, src)
	if varName == "" {
		return "", ""
	}
	// The RHS (child after `=`) must be a `new` unary_op wrapping the
	// constructor function_call.
	typeName := groovyNewTypeFromRHS(decl, eqIdx)
	if typeName == nil {
		return "", ""
	}
	return varName, nodeText(typeName, src)
}

// groovyNewTypeFromRHS returns the type-name identifier node of a
// `new ClassName(...)` RHS that follows the `=` at eqIdx in decl, or nil when
// the RHS is not a direct constructor call.
func groovyNewTypeFromRHS(decl ts.Node, eqIdx int) ts.Node {
	for i := eqIdx + 1; i < int(decl.ChildCount()); i++ {
		ch := decl.Child(i)
		if ch == nil || ch.Type() != "unary_op" {
			continue
		}
		hasNew := false
		var ctorCall ts.Node
		for j := 0; j < int(ch.ChildCount()); j++ {
			c := ch.Child(j)
			if c == nil {
				continue
			}
			switch c.Type() {
			case "new":
				hasNew = true
			case "function_call":
				ctorCall = c
			}
		}
		if !hasNew || ctorCall == nil {
			return nil
		}
		// The constructor's leading identifier is the class name.
		for j := 0; j < int(ctorCall.ChildCount()); j++ {
			if c := ctorCall.Child(j); c != nil && c.Type() == "identifier" {
				return c
			}
		}
		return nil
	}
	return nil
}

// isPascalCase reports whether s starts with an uppercase ASCII letter
// followed by at least one more character.
func isPascalCase(s string) bool {
	if len(s) < 2 {
		return false
	}
	c := s[0]
	return c >= 'A' && c <= 'Z'
}

// buildImportRecords walks the source for groovy_import nodes and emits
// one SCOPE.Component entity per import edge with an embedded IMPORTS
// relationship. The edge Properties carry the same contract Python (#93),
// Java (#120), and Scala (#379) emit so the cross-file resolver can
// build a per-file binding table.
//
// Groovy import shapes handled:
//
//	import foo.Bar              → ToID=foo.Bar,  local_name=Bar
//	import foo.Bar as Baz       → ToID=foo.Bar,  local_name=Baz, imported_name=Bar
//	import foo.something.*      → ToID=foo.something, wildcard=1
//	import static foo.Util.helper → ToID=foo.Util.helper, import_kind=static
//	import static foo.Util.*    → ToID=foo.Util,  wildcard=1, import_kind=static
func buildImportRecords(root ts.Node, file extractor.FileInput) []types.EntityRecord {
	imports := findAllNodes(root, "groovy_import", "import_declaration")
	if len(imports) == 0 {
		return nil
	}
	out := make([]types.EntityRecord, 0, len(imports))
	for _, imp := range imports {
		rec, ok := buildImportRecord(imp, file)
		if !ok {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// buildImportRecord builds a single import edge from a groovy_import node.
func buildImportRecord(node ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	var path string
	var alias string
	hasWildcard := false
	isStatic := false
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "qualified_name":
			path = nodeText(ch, file.Content)
		case "wildcard_import":
			hasWildcard = true
		case "modifier":
			// `import static ...`
			if strings.Contains(nodeText(ch, file.Content), "static") {
				isStatic = true
			}
		case "static":
			isStatic = true
		case "as":
			// next identifier is the alias.
		case "identifier":
			// Trailing identifier: either the alias (after "as") or the
			// final segment of a non-wildcard static import (when the
			// grammar surfaces it separately from qualified_name).
			alias = nodeText(ch, file.Content)
		}
	}
	if path == "" {
		// Fallback: parse from raw text for grammars that don't expose
		// qualified_name directly.
		raw := strings.TrimSpace(nodeText(node, file.Content))
		raw = strings.TrimPrefix(raw, "import")
		raw = strings.TrimSpace(raw)
		raw = strings.TrimPrefix(raw, "static")
		raw = strings.TrimSpace(raw)
		raw = strings.TrimSuffix(raw, ";")
		if asIdx := strings.Index(raw, " as "); asIdx >= 0 {
			alias = strings.TrimSpace(raw[asIdx+4:])
			raw = strings.TrimSpace(raw[:asIdx])
		}
		if strings.HasSuffix(raw, ".*") {
			hasWildcard = true
			raw = strings.TrimSuffix(raw, ".*")
		}
		path = raw
	}
	if path == "" {
		return types.EntityRecord{}, false
	}

	// Determine ToID and properties.
	var toID string
	props := map[string]string{}
	if isStatic {
		props["import_kind"] = "static"
	}
	if hasWildcard {
		toID = path
		props["source_module"] = path
		props["wildcard"] = "1"
	} else {
		// Plain import (with optional alias). Static non-wildcard imports
		// also have shape `static foo.Util.helper` where the leaf is the
		// member; we treat path as the full ToID and split off the leaf.
		toID = path
		leaf := path
		mod := path
		if dot := strings.LastIndexByte(path, '.'); dot > 0 {
			leaf = path[dot+1:]
			mod = path[:dot]
		}
		importedName := leaf
		localName := leaf
		if alias != "" {
			localName = alias
		}
		props["local_name"] = localName
		props["source_module"] = mod
		props["imported_name"] = importedName
	}

	top := toID
	if idx := strings.Index(toID, "."); idx >= 0 {
		top = toID[:idx]
	}
	return types.EntityRecord{
		Name:       top,
		Kind:       "SCOPE.Component",
		SourceFile: file.Path,
		Language:   "groovy",
		Relationships: []types.RelationshipRecord{
			{
				FromID:     file.Path,
				ToID:       toID,
				Kind:       "IMPORTS",
				Properties: props,
			},
		},
	}, true
}
