// Package scala implements the tree-sitter–based extractor for Scala source files.
//
// Extracted entities:
//   - function_definition    → Kind="SCOPE.Operation", Subtype="function"
//   - function_declaration   → Kind="SCOPE.Operation", Subtype="function"
//   - class_definition       → Kind="SCOPE.Component", Subtype="class"
//   - trait_definition       → Kind="SCOPE.Component", Subtype="trait"
//   - object_definition      → Kind="SCOPE.Component", Subtype="object"
//   - case_class_definition  → Kind="SCOPE.Component", Subtype="case_class"
//   - import_declaration     → IMPORTS relationships
//
// Issue #379 — relationship parity with java/kotlin:
//
//   - IMPORTS edges carry the same Properties contract Python emits
//     (#93): local_name, source_module, imported_name, wildcard. The
//     dotted package separator follows Scala convention (`.`).
//   - CALLS edges are emitted per call_expression descendant of every
//     function body. When the call's receiver (field_expression `obj.m`)
//     resolves to a known type via the enclosing class's val/var/class
//     parameters or the enclosing function's parameters, the target is
//     emitted as the dotted "<Type>.<method>" form and Properties
//     carries `receiver_type=<Type>`.
//   - CONTAINS edges are attached from each class/object/trait component
//     to every function declared in its template_body, using the
//     canonical Format A structural-ref
//     (`scope:operation:method:scala:<file>:<name>`).
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package scala

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("scala", &Extractor{})
}

// Extractor implements extractor.Extractor for Scala.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "scala" }

// Extract walks the tree-sitter CST and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if file.Tree == nil || len(file.Content) == 0 {
		return nil, nil
	}

	var entities []types.EntityRecord
	// Issue #501 — Twirl templates (*.scala.html) are Scala+HTML templates
	// compiled by the Twirl plugin. Emit a file entity with subtype="twirl"
	// to distinguish them from regular Scala source files, then still walk
	// the CST so embedded Scala constructs (import statements, class
	// definitions in template companion objects) are captured.
	if isTwirlTemplate(file.Path) {
		fe := extractor.FileEntity(file)
		fe.Subtype = "twirl"
		fe.Properties["subtype"] = "twirl"
		entities = append(entities, fe)
	} else {
		// Issue #577 — emit file-level SCOPE.Component (subtype="file") so the
		// cross-repo import linker (#566) can map IMPORTS edges back to the
		// originating repo via the resolver's byName index. Generalises the
		// JS/TS fix from #570/#575.
		entities = append(entities, extractor.FileEntity(file))
	}
	walkNode(file.Tree.RootNode(), file, nil, &entities)
	// Issue #90 — language tag for resolver dynamic-pattern dispatch.
	extractor.TagRelationshipsLanguage(entities, "scala")
	return entities, nil
}

// isTwirlTemplate reports whether the given file path is a Twirl template.
// Twirl templates use a compound extension: *.scala.html (HTML with embedded
// Scala), *.scala.xml, *.scala.js, *.scala.txt. The most common is .scala.html.
//
// Issue #501 — proper detection so Twirl files are not misclassified as
// plain HTML or plain Scala.
func isTwirlTemplate(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".scala.html") ||
		strings.HasSuffix(lower, ".scala.xml") ||
		strings.HasSuffix(lower, ".scala.js") ||
		strings.HasSuffix(lower, ".scala.txt")
}

// classCtx carries the lexical scope used by extractCallRelationships to
// resolve receiver types. fields holds value/variable/parameter members
// declared on the enclosing class/object/trait (val, var, class
// parameter); their declared leaf type lets a `repo.find()` call emit
// "Repo.find" instead of bare "find". When nil the call resolver still
// emits bare-name CALLS edges for unqualified invocations.
type classCtx struct {
	fields map[string]string
}

// walkNode performs a depth-first traversal.
//
// Issue #379: class/object/trait declarations attach CONTAINS edges per
// function declared inside their template_body, and every function body
// is scanned for call_expression descendants that yield CALLS edges.
func walkNode(node *sitter.Node, file extractor.FileInput, cc *classCtx, out *[]types.EntityRecord) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "class_definition", "case_class_definition":
		subtype := "class"
		if node.Type() == "case_class_definition" {
			subtype = "case_class"
		} else {
			// Detect case class by checking raw source text.
			raw := string(file.Content[node.StartByte():node.EndByte()])
			if strings.HasPrefix(strings.TrimSpace(raw), "case class ") {
				subtype = "case_class"
			}
		}
		emitContainerWithMembers(node, file, subtype, out)
		return

	case "trait_definition":
		emitContainerWithMembers(node, file, "trait", out)
		return

	case "object_definition":
		emitContainerWithMembers(node, file, "object", out)
		return

	case "function_definition", "function_declaration":
		if rec, ok := buildOperation(node, file, "function"); ok {
			body := findFunctionBody(node)
			paramTypes := collectParamTypes(node, file.Content)
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(body, file.Content, rec.Name, cc, paramTypes)...)
			*out = append(*out, rec)
		}
		return

	case "import_declaration":
		recs := buildImports(node, file)
		*out = append(*out, recs...)
		return
	}

	for i := range node.ChildCount() {
		walkNode(node.Child(int(i)), file, cc, out)
	}
}

// emitContainerWithMembers emits a class/object/trait component entity,
// recurses into its template_body to collect contained operations, and
// attaches a CONTAINS edge from the container to each emitted
// SCOPE.Operation child via the canonical structural-ref. The container's
// fields/parameters are passed down via classCtx so member functions can
// resolve receiver types on call_expression nodes.
func emitContainerWithMembers(
	node *sitter.Node,
	file extractor.FileInput,
	subtype string,
	out *[]types.EntityRecord,
) {
	rec, ok := buildComponent(node, file, subtype)
	if !ok {
		// Still recurse so nested types/imports below this node are
		// captured even when the declaration itself is malformed.
		for i := range node.ChildCount() {
			walkNode(node.Child(int(i)), file, nil, out)
		}
		return
	}
	classIdx := len(*out)
	*out = append(*out, rec)

	// Build the per-container scope: class parameters + val/var members.
	localCtx := &classCtx{fields: collectContainerFieldTypes(node, file.Content)}

	body := findTemplateBody(node)
	if body == nil {
		// Even without a body, emit case class parameter fields.
		emitScalaCaseClassFields(node, file, rec.Name, classIdx, out)
		return
	}
	before := len(*out)
	for i := range body.ChildCount() {
		ch := body.Child(int(i))
		// Issue #690 — intercept val/var definition children so we can
		// qualify the name with the enclosing class name. walkNode does not
		// carry parentType, so emitting through it would produce bare names
		// that don't match the CONTAINS stub's byLocation key.
		switch ch.Type() {
		case "val_definition", "var_definition":
			if fieldRec, ok := buildScalaField(ch, file, rec.Name); ok {
				*out = append(*out, fieldRec)
			}
			continue
		}
		walkNode(ch, file, localCtx, out)
	}
	after := len(*out)
	for k := before; k < after; k++ {
		child := &(*out)[k]
		var toID string
		switch {
		case child.Kind == "SCOPE.Operation":
			toID = extractor.BuildOperationStructuralRef("scala", file.Path, child.Name)
		case child.Kind == "SCOPE.Schema" && child.Subtype == "field":
			// Issue #690 — CONTAINS for class/object/trait val/var fields,
			// mirroring the Python fix from #689.
			toID = extractor.BuildSchemaFieldStructuralRef("scala", file.Path, child.Name)
		default:
			continue
		}
		(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
			types.RelationshipRecord{
				ToID: toID,
				Kind: "CONTAINS",
			})
	}
	// Issue #690 — also emit SCOPE.Schema/field for case class parameters
	// (class_parameters child of the class_definition node). These are
	// structural fields, not just constructor arguments.
	emitScalaCaseClassFields(node, file, rec.Name, classIdx, out)
}

// findTemplateBody returns the template_body child of a class/object/
// trait declaration, or nil when the declaration has no body.
func findTemplateBody(node *sitter.Node) *sitter.Node {
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch.Type() == "template_body" {
			return ch
		}
	}
	return nil
}

// findFunctionBody returns the block child of a function_definition that
// holds the call expressions, or nil when the function is abstract
// (function_declaration) / expression-body without a block.
func findFunctionBody(node *sitter.Node) *sitter.Node {
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch.Type() == "block" {
			return ch
		}
	}
	// Expression-body functions (`def f() = expr`) can hold a single
	// call_expression directly as a sibling of "=". Return the node
	// itself so extractCallRelationships scans its descendants.
	return node
}

// collectContainerFieldTypes walks a class/object/trait declaration and
// returns a map of member-name → declared leaf type for every val_
// definition, var_definition and class_parameter. Generic parameters
// are stripped so `List[Owner]` yields "List" — matching the java
// resolver's index shape.
func collectContainerFieldTypes(node *sitter.Node, src []byte) map[string]string {
	out := map[string]string{}

	// Class parameters (`class C(val repo: Repo, x: Int)`).
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch.Type() != "class_parameters" {
			continue
		}
		for j := 0; j < int(ch.ChildCount()); j++ {
			p := ch.Child(j)
			if p.Type() != "class_parameter" {
				continue
			}
			name, typ := extractNamedTypePair(p, src)
			if name != "" && typ != "" {
				out[name] = typ
			}
		}
	}

	// Template-body val/var members.
	body := findTemplateBody(node)
	if body == nil {
		return out
	}
	for i := 0; i < int(body.ChildCount()); i++ {
		ch := body.Child(i)
		switch ch.Type() {
		case "val_definition", "var_definition", "val_declaration", "var_declaration":
			name, typ := extractNamedTypePair(ch, src)
			if name != "" && typ != "" {
				if _, exists := out[name]; !exists {
					out[name] = typ
				}
			}
		}
	}
	return out
}

// extractNamedTypePair returns (name, leafType) for a node that has a
// direct identifier child followed (optionally) by a type_identifier or
// generic_type child. Returns ("", "") when either is missing.
func extractNamedTypePair(node *sitter.Node, src []byte) (string, string) {
	var name, typ string
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		switch ch.Type() {
		case "identifier":
			if name == "" {
				name = string(src[ch.StartByte():ch.EndByte()])
			}
		case "type_identifier":
			if typ == "" {
				typ = string(src[ch.StartByte():ch.EndByte()])
			}
		case "generic_type":
			if typ == "" {
				// Leaf is the first type_identifier child.
				for j := 0; j < int(ch.ChildCount()); j++ {
					gc := ch.Child(j)
					if gc.Type() == "type_identifier" {
						typ = string(src[gc.StartByte():gc.EndByte()])
						break
					}
				}
			}
		}
	}
	return name, typ
}

// collectParamTypes returns a map of parameter-name → declared leaf
// type for every parameter declared on a function_definition /
// function_declaration node. Used by the receiver binder so a call like
// `p.go()` inside `def f(p: Param)` resolves to "Param.go".
func collectParamTypes(node *sitter.Node, src []byte) map[string]string {
	out := map[string]string{}
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch.Type() != "parameters" {
			continue
		}
		for j := 0; j < int(ch.ChildCount()); j++ {
			p := ch.Child(j)
			if p.Type() != "parameter" {
				continue
			}
			name, typ := extractNamedTypePair(p, src)
			if name != "" && typ != "" {
				out[name] = typ
			}
		}
	}
	return out
}

// scalaKeywordStop lists Scala keywords / special identifiers that the
// parser surfaces as call_expression heads but are not real call
// targets. Mirrors the kotlin extractor's drop list (#106).
var scalaKeywordStop = map[string]bool{
	"this":  true,
	"super": true,
	"new":   true,
}

// extractCallRelationships returns one CALLS RelationshipRecord per
// unique call_expression descendant of body. The target name is the
// trailing identifier of the call's `function` (or `field` of a
// field_expression). When the receiver of a field_expression resolves
// to a known type via cc/paramTypes, the target is emitted as
// "<Type>.<method>" and Properties carries `receiver_type=<Type>`.
// Self-recursion is dropped to match the java/kotlin extractor dedup
// semantics.
func extractCallRelationships(
	body *sitter.Node,
	src []byte,
	callerName string,
	cc *classCtx,
	paramTypes map[string]string,
) []types.RelationshipRecord {
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
		target, recv := scalaCallTarget(call, src, cc, paramTypes)
		if target == "" {
			continue
		}
		if scalaKeywordStop[target] {
			continue
		}
		// Self-recursion check on the bare leaf name.
		leaf := target
		if dot := strings.LastIndexByte(target, '.'); dot >= 0 {
			leaf = target[dot+1:]
		}
		if leaf == callerName {
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		rel := types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
		}
		if recv != "" {
			rel.Properties = map[string]string{"receiver_type": recv}
		}
		rels = append(rels, rel)
	}
	return rels
}

// scalaCallTarget resolves the callee target of a call_expression. The
// first child is either:
//
//   - identifier            → bare-name call (helper())
//   - field_expression      → obj.method() — the field_expression's
//     trailing identifier is the method, the leading identifier is the
//     receiver. Receiver type lookup walks cc.fields then paramTypes.
//   - call_expression       → curried call (`f(a)(b)`); recurse into
//     the inner call to find the leaf method name.
//
// Returns (target, receiverType). receiverType is non-empty only when a
// field_expression receiver bound to a known type.
func scalaCallTarget(
	call *sitter.Node,
	src []byte,
	cc *classCtx,
	paramTypes map[string]string,
) (string, string) {
	if call.ChildCount() == 0 {
		return "", ""
	}
	first := call.Child(0)
	switch first.Type() {
	case "identifier":
		return string(src[first.StartByte():first.EndByte()]), ""
	case "field_expression":
		// field_expression has children: identifier "." identifier ...
		// Method = last identifier; receiver = first identifier.
		var method, receiver string
		var idents []*sitter.Node
		for i := 0; i < int(first.ChildCount()); i++ {
			ch := first.Child(i)
			if ch.Type() == "identifier" {
				idents = append(idents, ch)
			}
		}
		if len(idents) < 2 {
			return "", ""
		}
		receiver = string(src[idents[0].StartByte():idents[0].EndByte()])
		methodNode := idents[len(idents)-1]
		method = string(src[methodNode.StartByte():methodNode.EndByte()])
		if method == "" {
			return "", ""
		}
		// Resolve receiver type.
		recvType := ""
		if cc != nil {
			if t, ok := cc.fields[receiver]; ok && t != "" {
				recvType = t
			}
		}
		if recvType == "" {
			if t, ok := paramTypes[receiver]; ok && t != "" {
				recvType = t
			}
		}
		// PascalCase static-call shape: `Module.method`.
		if recvType == "" && isPascalCase(receiver) {
			recvType = receiver
		}
		if recvType != "" {
			return recvType + "." + method, recvType
		}
		return method, ""
	case "call_expression":
		// Curried call — recurse.
		return scalaCallTarget(first, src, cc, paramTypes)
	}
	return "", ""
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

// findAllNodes returns every descendant of root whose Type() is in kinds.
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

// buildScalaField creates a SCOPE.Schema/field entity for a Scala val_definition
// or var_definition node. The name is extracted via extractName and qualified
// with parentType as "<Class>.<field>" so the CONTAINS stub's byLocation key
// matches.
//
// Issue #690 — closes the Scala analog of the Python field orphan gap (#689).
func buildScalaField(node *sitter.Node, file extractor.FileInput, parentType string) (types.EntityRecord, bool) {
	name := extractName(node, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	emittedName := name
	if parentType != "" {
		emittedName = parentType + "." + name
	}
	return types.EntityRecord{
		Name:       emittedName,
		Kind:       "SCOPE.Schema",
		Subtype:    "field",
		SourceFile: file.Path,
		Language:   "scala",
		StartLine:  int(node.StartPoint().Row) + 1,
		EndLine:    int(node.EndPoint().Row) + 1,
	}, true
}

// emitScalaCaseClassFields scans a class_definition / case_class_definition
// node for a class_parameters child and emits a SCOPE.Schema/field entity for
// each class_parameter, appending CONTAINS edges on the parent class entity at
// classIdx.
//
// Issue #690 — case class pattern:
//
//	case class Order(id: Int, total: BigDecimal)
//
// Every positional parameter is a structural field accessible as `order.id`.
// Unlike Kotlin, all Scala case class parameters are structural properties
// regardless of whether they carry a `val`/`var` modifier (case classes
// generate accessors by default).
func emitScalaCaseClassFields(
	classNode *sitter.Node,
	file extractor.FileInput,
	className string,
	classIdx int,
	out *[]types.EntityRecord,
) {
	if className == "" {
		return
	}
	for i := 0; i < int(classNode.ChildCount()); i++ {
		ch := classNode.Child(i)
		if ch.Type() != "class_parameters" {
			continue
		}
		for j := 0; j < int(ch.ChildCount()); j++ {
			param := ch.Child(j)
			if param.Type() != "class_parameter" {
				continue
			}
			name := extractName(param, file.Content)
			if name == "" {
				continue
			}
			emittedName := className + "." + name
			rec := types.EntityRecord{
				Name:       emittedName,
				Kind:       "SCOPE.Schema",
				Subtype:    "field",
				SourceFile: file.Path,
				Language:   "scala",
				StartLine:  int(param.StartPoint().Row) + 1,
				EndLine:    int(param.EndPoint().Row) + 1,
			}
			*out = append(*out, rec)
			toID := extractor.BuildSchemaFieldStructuralRef("scala", file.Path, emittedName)
			(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
				types.RelationshipRecord{
					ToID: toID,
					Kind: "CONTAINS",
				})
		}
		break // only one class_parameters block
	}
}

// buildComponent creates a SCOPE.Component entity.
func buildComponent(node *sitter.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := extractName(node, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Component",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "scala",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          classSignature(file.Content, node),
		EnrichmentRequired: false,
	}, true
}

// buildOperation creates a SCOPE.Operation entity for function definitions.
func buildOperation(node *sitter.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := extractName(node, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "scala",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          methodSignature(file.Content, node),
		EnrichmentRequired: false,
	}, true
}

// buildImports creates SCOPE.Component entities with IMPORTS relationships.
//
// Issue #379: each emitted IMPORTS edge carries the same Properties
// contract Python (#93) and Java (#120) emit so the cross-file resolver
// can build a per-file binding table:
//
//	Properties["local_name"]    — the simple identifier introduced by
//	                              the import (last dotted segment, or
//	                              the selected name in `{A, B}`).
//	                              Omitted for wildcard imports.
//	Properties["source_module"] — the dotted package path the leaf was
//	                              selected from.
//	Properties["imported_name"] — equal to local_name for plain imports.
//	Properties["wildcard"]      — "1" when the import ends with `._`.
//
// In smacker/go-tree-sitter/scala, import_declaration children are:
// "import" identifier "." identifier ... [namespace_selectors |
// namespace_wildcard | identifier]. We reconstruct the dotted base
// path from the direct children.
func buildImports(node *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	var pathParts []string
	var selectors []string
	hasWildcard := false

	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		t := ch.Type()
		switch t {
		case "import", ".":
			// skip keyword and dots
		case "identifier":
			text := string(file.Content[ch.StartByte():ch.EndByte()])
			pathParts = append(pathParts, text)
		case "namespace_selectors":
			// children: "{" identifier "," identifier ... "}"
			for j := range ch.ChildCount() {
				sel := ch.Child(int(j))
				if sel.Type() == "identifier" {
					selectors = append(selectors, string(file.Content[sel.StartByte():sel.EndByte()]))
				}
			}
		case "namespace_wildcard":
			hasWildcard = true
		case "stable_identifier", "import_expression", "import_selectors":
			// fallback for other grammar versions
			pathParts = append(pathParts, string(file.Content[ch.StartByte():ch.EndByte()]))
		}
	}

	base := strings.Join(pathParts, ".")
	if base == "" {
		return nil
	}

	type importEdge struct {
		toID  string
		props map[string]string
	}

	var edges []importEdge
	switch {
	case hasWildcard:
		// `import scala.collection.mutable._`. ToID drops the wildcard.
		edges = append(edges, importEdge{
			toID: base,
			props: map[string]string{
				"source_module": base,
				"wildcard":      "1",
			},
		})
	case len(selectors) > 0:
		for _, sel := range selectors {
			if sel == "_" {
				edges = append(edges, importEdge{
					toID: base,
					props: map[string]string{
						"source_module": base,
						"wildcard":      "1",
					},
				})
				continue
			}
			edges = append(edges, importEdge{
				toID: base + "." + sel,
				props: map[string]string{
					"local_name":    sel,
					"source_module": base,
					"imported_name": sel,
				},
			})
		}
	default:
		// Plain `import a.b.c`: leaf=c, source_module=a.b.
		leaf := base
		mod := base
		if dot := strings.LastIndexByte(base, '.'); dot > 0 {
			leaf = base[dot+1:]
			mod = base[:dot]
		}
		edges = append(edges, importEdge{
			toID: base,
			props: map[string]string{
				"local_name":    leaf,
				"source_module": mod,
				"imported_name": leaf,
			},
		})
	}

	out := make([]types.EntityRecord, 0, len(edges))
	for _, e := range edges {
		top := e.toID
		if idx := strings.Index(e.toID, "."); idx >= 0 {
			top = e.toID[:idx]
		}
		out = append(out, types.EntityRecord{
			Name:       top,
			Kind:       "SCOPE.Component",
			SourceFile: file.Path,
			Language:   "scala",
			Relationships: []types.RelationshipRecord{
				{
					FromID:     file.Path,
					ToID:       e.toID,
					Kind:       "IMPORTS",
					Properties: e.props,
				},
			},
		})
	}
	return out
}

// extractName finds the name of a declaration node.
func extractName(node *sitter.Node, src []byte) string {
	if child := node.ChildByFieldName("name"); child != nil {
		return string(src[child.StartByte():child.EndByte()])
	}
	keywords := map[string]bool{
		"class": true, "trait": true, "object": true, "case": true,
		"def": true, "val": true, "var": true, "extends": true,
		"abstract": true, "sealed": true, "final": true, "override": true,
		"private": true, "protected": true, "implicit": true,
	}
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		t := ch.Type()
		if t == "identifier" || t == "type_identifier" {
			name := string(src[ch.StartByte():ch.EndByte()])
			if !keywords[name] {
				return name
			}
		}
	}
	return ""
}

// firstLine returns the first line of the node's source text.
func firstLine(src []byte, node *sitter.Node) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "\n"); idx >= 0 {
		raw = raw[:idx]
	}
	return strings.TrimSpace(raw)
}

// methodSignature extracts a clean method signature, truncating at body.
// Matches Python's behavior: "def name(params): ReturnType" without body.
func methodSignature(src []byte, node *sitter.Node) string {
	raw := firstLine(src, node)
	// Remove "override " prefix for cleaner parity
	raw = strings.TrimPrefix(raw, "override ")
	// Truncate at " = " or " = {" or " {"
	for _, sep := range []string{" = {", " = ", " {"} {
		if idx := strings.Index(raw, sep); idx >= 0 {
			raw = raw[:idx]
		}
	}
	return strings.TrimSpace(raw)
}

// classSignature extracts a clean class/trait signature without body.
// Strips extends/with clauses and type parameters to match Python convention.
func classSignature(src []byte, node *sitter.Node) string {
	raw := firstLine(src, node)
	// Truncate at opening brace or opening paren for case classes with params.
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = raw[:idx]
	}
	// Strip extends clause.
	if idx := strings.Index(raw, " extends "); idx >= 0 {
		raw = raw[:idx]
	}
	// Strip type parameters: Name[T, ID] -> Name
	if idx := strings.Index(raw, "["); idx >= 0 {
		raw = raw[:idx]
	}
	return strings.TrimSpace(raw)
}
