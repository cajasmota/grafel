// Package rust implements the tree-sitter–based extractor for Rust source files.
//
// Extracted entities:
//   - struct_item     → Kind="SCOPE.Component", Subtype="struct"
//     (Properties: fields, generics, derives)
//   - enum_item       → Kind="SCOPE.Component", Subtype="enum"
//     (Properties: variants, generics, derives)
//   - trait_item      → Kind="SCOPE.Component", Subtype="trait"
//     (Properties: methods, supertraits, generics; EXTENDS edges per supertrait)
//   - type_item       → Kind="SCOPE.Component", Subtype="type_alias"
//     (Properties: aliased_type, generics)
//   - impl_item       → Kind="SCOPE.Component", Subtype="impl"
//   - function_item   → Kind="SCOPE.Operation", Subtype="function"
//   - use_declaration → IMPORTS relationship
//
// The struct/enum/trait/type_alias Properties realise the Type System deep
// extraction bar (issue #3411), mirroring the JS/TS interface/enum emission.
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package rust

import (
	"context"
	"strconv"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("rust", &Extractor{})
}

// Extractor implements extractor.Extractor for Rust.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "rust" }

// Extract walks the tree-sitter CST and returns entity records for the Rust file.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if file.Tree == nil || len(file.Content) == 0 {
		return nil, nil
	}

	var entities []types.EntityRecord
	// Issue #577 — emit file-level SCOPE.Component (subtype="file") so the
	// cross-repo import linker (#566) can map IMPORTS edges back to the
	// originating repo via the resolver's byName index. Generalises the
	// JS/TS fix from #570/#575.
	entities = append(entities, extractor.FileEntity(file))
	// Issue #4373 — per-file cross-module call-path resolution context, built
	// once from the file path + `use` declarations and threaded into call
	// extraction so cross-module CALLS carry a stamped module qualifier.
	crossCtx := buildRustCrossCtx(file.Tree.RootNode(), file.Content, file.Path)
	walk(file.Tree.RootNode(), file, &entities, crossCtx)
	// Epic #3628 — error-flow topology: typed THROWS / CATCHES edges to the
	// shared SCOPE.ExceptionType convergence node. Runs after walk so the
	// host SCOPE.Operation entities (including impl-qualified method names)
	// already exist for FromName attachment.
	emitExceptionFlowEdges(file.Tree.RootNode(), file.Content, &entities)
	// Ticket #4431 — index const/static constant collections (const slice maps,
	// phf_map!/lazy_static! maps, module constant groups) and data-enums as
	// queryable SCOPE.Enum value-sets, reusing the shared cross-language builder
	// (extends #4420/#4429). Append-only supplemental pass: it never replaces the
	// struct/enum Component entities the walk already emitted.
	emitRustConstValueSets(file.Tree.RootNode(), file.Content, file.Path, &entities)
	// Issue #5020 — config-consumption topology: literal env/config-crate key
	// reads (env::var / dotenvy::var / figment Env::prefixed) become shared
	// SCOPE.Config config-key nodes + DEPENDS_ON_CONFIG edges from the reading
	// function, the config-change blast radius (parity with go/java/php/python).
	// Append-only supplemental pass; entities[0] is the file entity.
	emitConfigConsumerEdges(file.Tree.RootNode(), file.Content, &entities)
	// Issue #90 — language tag for resolver dynamic-pattern dispatch.
	extractor.TagRelationshipsLanguage(entities, "rust")
	extractor.TagEntitiesLanguage(entities, "rust")
	return entities, nil
}

// walk performs a depth-first traversal of the CST, collecting entities.
//
// PORT-2-FIX-2-ALL (#41): trait_item and impl_item attach a CONTAINS edge
// per function_item declared inside their declaration_list, every
// function_item body emits CALLS edges with stub to_id, and use_declaration
// nodes already emit IMPORTS (untouched).
func walk(node *sitter.Node, file extractor.FileInput, out *[]types.EntityRecord, crossCtx *rustCrossCtx) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "struct_item":
		if rec, ok := buildComponent(node, file, "struct"); ok {
			idx := len(*out)
			*out = append(*out, rec)
			// Issue #4854 — general field membership: one SCOPE.Schema/field
			// per named struct field (serde wire name / skip honoured) + a
			// struct→field CONTAINS edge, so a plain data struct has field
			// children (dedups by Name with the serde DTO members in #4635).
			fieldEnts := emitRustStructFields(node, file, rec.Name)
			attachRustFieldContains(*out, idx, file.Path, fieldEnts)
			*out = append(*out, fieldEnts...)
		}

	case "enum_item":
		if rec, ok := buildComponent(node, file, "enum"); ok {
			idx := len(*out)
			*out = append(*out, rec)
			// Issue #4854 — named fields of struct-style enum variants become
			// "<Enum>.<Variant>.<field>" field members.
			fieldEnts := emitRustEnumVariantFields(node, file, rec.Name)
			attachRustFieldContains(*out, idx, file.Path, fieldEnts)
			*out = append(*out, fieldEnts...)
		}

	case "type_item":
		// Issue #3269 — type X = Y; alias declarations.
		// tree-sitter Rust grammar: type_item has a "name" field (type_identifier)
		// and a "type" field holding the aliased type expression.
		if rec, ok := buildTypeAlias(node, file); ok {
			*out = append(*out, rec)
		}

	case "trait_item":
		rec, ok := buildComponent(node, file, "trait")
		if !ok {
			for i := range node.ChildCount() {
				walk(node.Child(int(i)), file, out, crossCtx)
			}
			return
		}
		traitIdx := len(*out)
		*out = append(*out, rec)
		body := findRustDeclList(node)
		if body != nil {
			before := len(*out)
			for i := range body.ChildCount() {
				walk(body.Child(int(i)), file, out, crossCtx)
			}
			after := len(*out)
			for k := before; k < after; k++ {
				child := &(*out)[k]
				if child.Kind != "SCOPE.Operation" {
					continue
				}
				// Issue #144 — structural-ref (Format A) keyed on file path
				// so trait→method CONTAINS edges disambiguate by location.
				toID := extractor.BuildOperationStructuralRef("rust", file.Path, child.Name)
				(*out)[traitIdx].Relationships = append((*out)[traitIdx].Relationships,
					types.RelationshipRecord{
						ToID: toID,
						Kind: "CONTAINS",
					})
			}
		}
		return

	case "impl_item":
		rec, ok := buildImpl(node, file)
		if !ok {
			for i := range node.ChildCount() {
				walk(node.Child(int(i)), file, out, crossCtx)
			}
			return
		}
		implIdx := len(*out)
		implName := rec.Name
		*out = append(*out, rec)
		body := findRustDeclList(node)
		if body != nil {
			before := len(*out)
			for i := range body.ChildCount() {
				ch := body.Child(int(i))
				if ch.Type() == "function_item" {
					// Issue #615 — emit impl methods qualified as "TypeName.fnName"
					// so they are traceable back to their owner type.
					if fnRec, ok2 := buildOperation(ch, file); ok2 {
						paramTypes := collectRustParamTypes(ch, file.Content)
						// Issue #616 — resolve self.method() and dyn-param calls.
						fnRec.Relationships = append(fnRec.Relationships,
							extractCallRelationships(ch.ChildByFieldName("body"), file.Content, fnRec.Name, implName, paramTypes, crossCtx)...)
						// Qualify the name with the impl type owner.
						fnRec.Name = implName + "." + fnRec.Name
						*out = append(*out, fnRec)
					}
				} else {
					walk(ch, file, out, crossCtx)
				}
			}
			after := len(*out)
			for k := before; k < after; k++ {
				child := &(*out)[k]
				if child.Kind != "SCOPE.Operation" {
					continue
				}
				// Issue #144 — structural-ref (Format A) keyed on file path
				// so impl→method CONTAINS edges disambiguate when two files
				// each define an `impl Foo { fn new() }` shape.
				// Use the already-qualified name (e.g. "Foo.bar") for the ref.
				toID := extractor.BuildOperationStructuralRef("rust", file.Path, child.Name)
				(*out)[implIdx].Relationships = append((*out)[implIdx].Relationships,
					types.RelationshipRecord{
						ToID: toID,
						Kind: "CONTAINS",
					})
			}
		}
		return

	case "function_item":
		if rec, ok := buildOperation(node, file); ok {
			paramTypes := collectRustParamTypes(node, file.Content)
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(node.ChildByFieldName("body"), file.Content, rec.Name, "", paramTypes, crossCtx)...)
			*out = append(*out, rec)
		}
		return

	case "use_declaration":
		if rec, ok := buildImport(node, file); ok {
			*out = append(*out, rec)
		}
	}

	for i := range node.ChildCount() {
		walk(node.Child(int(i)), file, out, crossCtx)
	}
}

// findRustDeclList returns the declaration_list child of a trait_item or
// impl_item, or nil when the body is missing.
func findRustDeclList(node *sitter.Node) *sitter.Node {
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch.Type() == "declaration_list" {
			return ch
		}
	}
	return nil
}

// extractCallRelationships returns one CALLS RelationshipRecord per unique
// call_expression / macro_invocation descendant of body. Targets resolve to
// the rightmost identifier in the function expression; FromID is left empty
// so buildDocument substitutes the caller's entity ID at emit time.
//
// Issue #616 — ownerName is the impl type this function belongs to (e.g.
// "Foo"); it enables `self.method()` calls to resolve to "Foo.method".
// paramTypes maps parameter names to their declared types so that calls
// through typed receivers (e.g. `r: &dyn Repo`) resolve to "Repo.method".
func extractCallRelationships(body *sitter.Node, src []byte, callerName, ownerName string, paramTypes map[string]string, crossCtx *rustCrossCtx) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	calls := findAllNodes(body, "call_expression", "macro_invocation")
	if len(calls) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(calls))
	rels := make([]types.RelationshipRecord, 0, len(calls))
	for _, call := range calls {
		target, pathSegs := rustCallTarget(call, src, ownerName, paramTypes)
		if target == "" || target == callerName {
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		// Line is 1-based: tree-sitter StartPoint().Row is 0-based.
		callLine := strconv.Itoa(int(call.StartPoint().Row) + 1)
		props := map[string]string{"line": callLine}
		// Issue #4373 — stamp the resolved module/crate path qualifier so the
		// resolver can bind a cross-module CALL to the exact callee module's
		// entity instead of collapsing to an ambiguity-prone bare leaf. Only
		// fires for path-qualified calls (`a::b::leaf`) where the qualifier
		// maps to in-crate module directories; bare and receiver calls are
		// untouched.
		if crossCtx != nil && len(pathSegs) >= 2 {
			if dirs, scope := crossCtx.resolveCallPath(pathSegs); len(dirs) > 0 {
				props["rust_call_pkg_dirs"] = strings.Join(dirs, ";")
				props["call_leaf"] = pathSegs[len(pathSegs)-1]
				if scope != "" {
					props["rust_call_scope"] = scope
				}
			}
		}
		rels = append(rels, types.RelationshipRecord{
			ToID:       target,
			Kind:       "CALLS",
			Properties: props,
		})
	}
	return rels
}

// rustCallTarget resolves the callee identifier from a Rust call_expression
// or macro_invocation. For call_expression the function is the first child;
// for scoped_identifier / field_expression we use the rightmost identifier.
//
// Issue #616 — ownerName and paramTypes enable receiver-qualified targets:
//   - "self.method()" inside impl Foo → "Foo.method"
//   - "repo.find()" where repo: &dyn Repo → "Repo.find"
// The second return value (issue #4373) is the FULL `::`-separated path of a
// scoped_identifier callee (e.g. ["crate","services","order","place_order"] or
// ["OrderService","new"]), or nil for bare / receiver / macro calls. The
// caller uses it to stamp a cross-module qualifier so the resolver can bind to
// the exact callee module instead of the ambiguity-prone bare leaf.
func rustCallTarget(call *sitter.Node, src []byte, ownerName string, paramTypes map[string]string) (string, []string) {
	switch call.Type() {
	case "call_expression":
		fn := call.ChildByFieldName("function")
		if fn == nil && call.ChildCount() > 0 {
			fn = call.Child(0)
		}
		if fn == nil {
			return "", nil
		}
		switch fn.Type() {
		case "identifier":
			return string(src[fn.StartByte():fn.EndByte()]), nil
		case "scoped_identifier":
			if name := fn.ChildByFieldName("name"); name != nil {
				return string(src[name.StartByte():name.EndByte()]),
					rustScopedPathSegments(fn, src)
			}
		case "field_expression":
			method := ""
			if name := fn.ChildByFieldName("field"); name != nil {
				method = string(src[name.StartByte():name.EndByte()])
			}
			if method == "" {
				return "", nil
			}
			// Issue #616 — resolve receiver type for self and typed params.
			recv := ""
			if value := fn.ChildByFieldName("value"); value != nil {
				recv = string(src[value.StartByte():value.EndByte()])
			}
			if recv == "self" && ownerName != "" {
				return ownerName + "." + method, nil
			}
			if recv != "" && len(paramTypes) > 0 {
				if recvType, ok := paramTypes[recv]; ok && recvType != "" {
					return recvType + "." + method, nil
				}
			}
			return method, nil
		case "generic_function":
			if path := fn.ChildByFieldName("function"); path != nil {
				switch path.Type() {
				case "identifier":
					return string(src[path.StartByte():path.EndByte()]), nil
				case "scoped_identifier":
					if name := path.ChildByFieldName("name"); name != nil {
						return string(src[name.StartByte():name.EndByte()]),
							rustScopedPathSegments(path, src)
					}
				case "field_expression":
					if name := path.ChildByFieldName("field"); name != nil {
						return string(src[name.StartByte():name.EndByte()]), nil
					}
				}
			}
		}
	case "macro_invocation":
		if m := call.ChildByFieldName("macro"); m != nil {
			t := m.Type()
			if t == "identifier" {
				return string(src[m.StartByte():m.EndByte()]), nil
			}
			if t == "scoped_identifier" {
				if name := m.ChildByFieldName("name"); name != nil {
					return string(src[name.StartByte():name.EndByte()]),
						rustScopedPathSegments(m, src)
				}
			}
		}
	}
	return "", nil
}

// rustScopedPathSegments returns the `::`-separated identifier segments of a
// scoped_identifier node, in source order including the trailing name. The
// tree-sitter Rust grammar nests scoped_identifier left-associatively:
// `a::b::c` is scoped_identifier(path=scoped_identifier(path=a, name=b),
// name=c). We flatten by reading the literal source text of the node and
// splitting on `::`, which is robust to the nesting and to crate/self/super
// path keywords (which appear as crate/self/super nodes). Turbofish generics
// are stripped per-segment by splitRustPath. Returns nil when the path cannot
// be cleanly segmented (e.g. contains a non-identifier element).
func rustScopedPathSegments(node *sitter.Node, src []byte) []string {
	raw := strings.TrimSpace(string(src[node.StartByte():node.EndByte()]))
	if raw == "" {
		return nil
	}
	return splitRustPath(raw)
}

// collectRustParamTypes scans a function_item node's parameters child and
// returns a map of parameter-name → declared leaf type. Type references are
// normalised by stripping leading `&`, `&mut`, `Box<dyn `, `dyn `, `Arc<`,
// `Rc<`, and trailing `>` so that `r: &dyn Repo` → {"r": "Repo"}.
//
// Issue #616 — used by extractCallRelationships to qualify dyn-receiver
// CALLS edges (e.g. `r.find(1)` → "Repo.find").
func collectRustParamTypes(node *sitter.Node, src []byte) map[string]string {
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
			nameNode := p.ChildByFieldName("pattern")
			typeNode := p.ChildByFieldName("type")
			if nameNode == nil || typeNode == nil {
				continue
			}
			name := string(src[nameNode.StartByte():nameNode.EndByte()])
			if name == "self" || name == "&self" || name == "&mut self" {
				continue
			}
			typ := string(src[typeNode.StartByte():typeNode.EndByte()])
			typ = normalizeRustType(typ)
			if name != "" && typ != "" {
				out[name] = typ
			}
		}
		break
	}
	return out
}

// normalizeRustType strips common Rust type wrappers to extract the bare
// type name for receiver binding. Examples:
//
//	"&dyn Repo"        → "Repo"
//	"Box<dyn Repo>"    → "Repo"
//	"Arc<MyService>"   → "MyService"
//	"&mut Writer"      → "Writer"
func normalizeRustType(typ string) string {
	// Strip leading reference/mut.
	for _, prefix := range []string{"&mut ", "&"} {
		if strings.HasPrefix(typ, prefix) {
			typ = strings.TrimPrefix(typ, prefix)
			break
		}
	}
	// Strip wrapper types.
	for _, wrap := range []string{"Box<dyn ", "Box<", "Arc<dyn ", "Arc<", "Rc<dyn ", "Rc<", "dyn "} {
		if strings.HasPrefix(typ, wrap) {
			typ = strings.TrimPrefix(typ, wrap)
			typ = strings.TrimSuffix(typ, ">")
			break
		}
	}
	return strings.TrimSpace(typ)
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

// buildComponent creates a Component entity for struct/enum/trait items.
//
// Issue #3411 — Type System deep extraction. Beyond the bare name, structured
// Properties capture the type's internal shape (mirroring the JS/TS bar in
// handleInterfaceDeclaration / handleEnumDeclaration):
//
//	struct → "fields" (named field idents or "0,1,.." for tuple structs),
//	          "generics", "derives"
//	enum   → "variants" (variant idents), "generics", "derives"
//	trait  → "methods" (signature + default-body fn idents),
//	          "supertraits", "generics", plus EXTENDS edges per supertrait
func buildComponent(node *sitter.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}

	rec := types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Component",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "rust",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildTypeSignature(node, file.Content, name),
		EnrichmentRequired: false,
	}

	props := map[string]string{}
	if g := rustGenerics(node, file.Content); g != "" {
		props["generics"] = g
	}
	switch subtype {
	case "struct":
		if f := rustStructFields(node, file.Content); f != "" {
			props["fields"] = f
		}
		if d := rustDerives(node, file.Content); d != "" {
			props["derives"] = d
		}
	case "enum":
		if v := rustEnumVariants(node, file.Content); v != "" {
			props["variants"] = v
		}
		if d := rustDerives(node, file.Content); d != "" {
			props["derives"] = d
		}
	case "trait":
		if m := rustTraitMethods(node, file.Content); m != "" {
			props["methods"] = m
		}
		supers := rustSupertraits(node, file.Content)
		if len(supers) > 0 {
			props["supertraits"] = strings.Join(supers, ", ")
			for _, s := range supers {
				rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
					ToID: s,
					Kind: "EXTENDS",
				})
			}
		}
	}
	if len(props) > 0 {
		rec.Properties = props
	}
	return rec, true
}

// rustGenerics returns a comma-separated list of generic type-parameter names
// declared on a struct/enum/trait/type item (the `type_parameters` field).
// Lifetime params (`'a`) and const params are included as written.
func rustGenerics(node *sitter.Node, src []byte) string {
	tp := node.ChildByFieldName("type_parameters")
	if tp == nil {
		return ""
	}
	var out []string
	for i := 0; i < int(tp.ChildCount()); i++ {
		ch := tp.Child(i)
		switch ch.Type() {
		case "type_identifier", "lifetime", "constrained_type_parameter":
			out = append(out, strings.TrimSpace(string(src[ch.StartByte():ch.EndByte()])))
		case "const_parameter", "optional_type_parameter":
			out = append(out, strings.TrimSpace(string(src[ch.StartByte():ch.EndByte()])))
		}
	}
	return strings.Join(out, ", ")
}

// rustStructFields returns the field names of a struct_item. For a named-field
// struct it returns the field identifiers; for a tuple struct it returns the
// positional indices ("0, 1, ..."); for a unit struct it returns "".
func rustStructFields(node *sitter.Node, src []byte) string {
	body := node.ChildByFieldName("body")
	if body == nil {
		// Tuple/unit structs place the field list in an unnamed child.
		for i := 0; i < int(node.ChildCount()); i++ {
			ch := node.Child(i)
			if ch.Type() == "ordered_field_declaration_list" {
				body = ch
				break
			}
		}
	}
	if body == nil {
		return ""
	}
	switch body.Type() {
	case "field_declaration_list":
		var out []string
		for i := 0; i < int(body.ChildCount()); i++ {
			ch := body.Child(i)
			if ch.Type() == "field_declaration" {
				if nm := ch.ChildByFieldName("name"); nm != nil {
					out = append(out, string(src[nm.StartByte():nm.EndByte()]))
				}
			}
		}
		return strings.Join(out, ", ")
	case "ordered_field_declaration_list":
		var out []string
		idx := 0
		for i := 0; i < int(body.ChildCount()); i++ {
			if body.Child(i).ChildByFieldName("type") != nil ||
				body.Child(i).Type() == "primitive_type" ||
				isRustTypeNode(body.Child(i)) {
				out = append(out, strconv.Itoa(idx))
				idx++
			}
		}
		return strings.Join(out, ", ")
	}
	return ""
}

// isRustTypeNode reports whether a node represents a type expression that would
// occupy a positional slot in a tuple struct's ordered field list.
func isRustTypeNode(n *sitter.Node) bool {
	switch n.Type() {
	case "(", ")", ",", "visibility_modifier":
		return false
	}
	return true
}

// rustEnumVariants returns a comma-separated list of variant names declared in
// an enum_item's enum_variant_list. Tuple (`Foo(i32)`), struct
// (`Bar { x: u8 }`), and discriminant (`Baz = 1`) variants all contribute their
// leading identifier.
func rustEnumVariants(node *sitter.Node, src []byte) string {
	body := node.ChildByFieldName("body")
	if body == nil {
		return ""
	}
	var out []string
	for i := 0; i < int(body.ChildCount()); i++ {
		ch := body.Child(i)
		if ch.Type() != "enum_variant" {
			continue
		}
		if nm := ch.ChildByFieldName("name"); nm != nil {
			out = append(out, string(src[nm.StartByte():nm.EndByte()]))
		}
	}
	return strings.Join(out, ", ")
}

// rustTraitMethods returns a comma-separated list of method names declared in a
// trait's declaration_list — both required signatures (function_signature_item)
// and provided/default methods (function_item).
func rustTraitMethods(node *sitter.Node, src []byte) string {
	body := findRustDeclList(node)
	if body == nil {
		return ""
	}
	var out []string
	for i := 0; i < int(body.ChildCount()); i++ {
		ch := body.Child(i)
		if ch.Type() == "function_signature_item" || ch.Type() == "function_item" {
			if nm := ch.ChildByFieldName("name"); nm != nil {
				out = append(out, string(src[nm.StartByte():nm.EndByte()]))
			}
		}
	}
	return strings.Join(out, ", ")
}

// rustSupertraits returns the supertrait names from a trait_item's `bounds`
// field (e.g. `trait A: B + C` → ["B", "C"]). Lifetime bounds are skipped.
func rustSupertraits(node *sitter.Node, src []byte) []string {
	bounds := node.ChildByFieldName("bounds")
	if bounds == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(bounds.ChildCount()); i++ {
		ch := bounds.Child(i)
		switch ch.Type() {
		case "type_identifier":
			out = append(out, string(src[ch.StartByte():ch.EndByte()]))
		case "generic_type", "scoped_type_identifier":
			if nm := ch.ChildByFieldName("name"); nm != nil {
				out = append(out, string(src[nm.StartByte():nm.EndByte()]))
			} else {
				out = append(out, string(src[ch.StartByte():ch.EndByte()]))
			}
		}
	}
	return out
}

// rustDerives returns a comma-separated list of derive-macro names attached to
// a type via `#[derive(...)]`. Derive attributes are emitted by the grammar as
// `attribute_item` siblings immediately preceding the type item, so we scan
// backwards over the previous siblings (skipping other attributes / comments).
func rustDerives(node *sitter.Node, src []byte) string {
	var out []string
	for prev := node.PrevSibling(); prev != nil; prev = prev.PrevSibling() {
		t := prev.Type()
		if t == "line_comment" || t == "block_comment" {
			continue
		}
		if t != "attribute_item" {
			break
		}
		out = append(rustParseDerive(prev, src), out...)
	}
	return strings.Join(out, ", ")
}

// rustParseDerive extracts the derive names from a single attribute_item node
// when it is a `#[derive(...)]`; returns nil for non-derive attributes.
func rustParseDerive(attr *sitter.Node, src []byte) []string {
	var inner *sitter.Node
	for i := 0; i < int(attr.ChildCount()); i++ {
		if attr.Child(i).Type() == "attribute" {
			inner = attr.Child(i)
			break
		}
	}
	if inner == nil {
		return nil
	}
	// First child identifier must be "derive".
	id := inner.Child(0)
	if id == nil || id.Type() != "identifier" ||
		string(src[id.StartByte():id.EndByte()]) != "derive" {
		return nil
	}
	args := inner.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(args.ChildCount()); i++ {
		ch := args.Child(i)
		switch ch.Type() {
		case "identifier", "scoped_identifier", "type_identifier":
			out = append(out, string(src[ch.StartByte():ch.EndByte()]))
		}
	}
	return out
}

// buildImpl creates a Component entity for impl blocks.
// impl_item uses "type" field (impl Foo) or "trait" + "type" (impl Trait for Foo).
func buildImpl(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	// "type" field holds the implementing type.
	name := childFieldText(node, "type", file.Content)
	if name == "" {
		// Fallback: scan for type_identifier or generic_type child.
		for i := range node.ChildCount() {
			ch := node.Child(int(i))
			t := ch.Type()
			if t == "type_identifier" || t == "generic_type" {
				name = string(file.Content[ch.StartByte():ch.EndByte()])
				break
			}
		}
	}
	if name == "" {
		return types.EntityRecord{}, false
	}

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Component",
		Subtype:            "impl",
		SourceFile:         file.Path,
		Language:           "rust",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildTypeSignature(node, file.Content, name),
		EnrichmentRequired: false,
	}, true
}

// buildOperation creates an Operation entity for function items.
func buildOperation(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}

	sig := buildFnSignature(node, file.Content)
	// Strip "async " prefix to match Python parity
	sig = strings.TrimPrefix(sig, "async ")
	// Strip "pub " prefix for cleaner signatures
	sig = strings.TrimPrefix(sig, "pub ")
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            "function",
		SourceFile:         file.Path,
		Language:           "rust",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          sig,
		EnrichmentRequired: false,
	}, true
}

// buildTypeAlias creates a Component entity for type alias declarations
// (`type X = Y;`). The aliased type is captured in the "aliased_type" property.
//
// Issue #3269 — type_alias_extraction capability.
func buildTypeAlias(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	aliasedType := childFieldText(node, "type", file.Content)

	rec := types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Component",
		Subtype:            "type_alias",
		SourceFile:         file.Path,
		Language:           "rust",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildTypeSignature(node, file.Content, name),
		EnrichmentRequired: false,
	}
	props := map[string]string{}
	if aliasedType != "" {
		props["aliased_type"] = aliasedType
	}
	if g := rustGenerics(node, file.Content); g != "" {
		props["generics"] = g
	}
	if len(props) > 0 {
		rec.Properties = props
	}
	rec.ID = rec.ComputeID()
	return rec, true
}

// buildImport creates a Component entity for use declarations.
//
// Issue #101: pub-modifier and intra-crate prefixes are stripped here so
// the synthesised stub reaches the resolver in the canonical
// "<crate>::<path>" shape that synth.go's `::` branch matches against
// the external-crate allowlist. Without this:
//   - `pub use client::Foo` left the literal "pub" prefix on the stub
//     and never matched anything.
//   - `crate::module::Item` / `self::sibling` / `super::parent` are
//     intra-crate references; emitting them as IMPORTS guarantees a
//     bug-extractor since they cannot be on any external allowlist.
//     We drop them entirely.
func buildImport(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	raw := strings.TrimSpace(string(file.Content[node.StartByte():node.EndByte()]))
	// Visibility modifiers — `pub use ...`, `pub(crate) use ...`,
	// `pub(super) use ...`. Strip the modifier before the `use` token.
	raw = stripRustVisibility(raw)
	raw = strings.TrimPrefix(raw, "use ")
	raw = strings.TrimSuffix(raw, ";")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return types.EntityRecord{}, false
	}

	// Intra-crate paths are not external imports; emitting them as
	// IMPORTS would force a bug-extractor classification. The resolver
	// has no machinery to bind `crate::Foo` to a specific entity in
	// the same crate from this layer (Issue #101).
	if strings.HasPrefix(raw, "crate::") || raw == "crate" ||
		strings.HasPrefix(raw, "self::") || raw == "self" ||
		strings.HasPrefix(raw, "super::") || raw == "super" {
		return types.EntityRecord{}, false
	}

	top := raw
	if idx := strings.Index(raw, "::"); idx >= 0 {
		top = raw[:idx]
	}

	// #4783 — expand the `use` path into one IMPORTS edge per imported symbol,
	// stamping the `imported_name`/`local_name`(/`wildcard`) contract that the
	// per-symbol external-node synthesis (#4515) reads to mint a stable
	// `ext:<crate>:<Symbol>` node. Brace groups (`use a::{B, C as D}`) fan out;
	// a glob (`use a::*`) stamps wildcard with the namespace local. A plain
	// `use a::B` keeps its historical single-edge ToID `a::B`.
	syms := parseRustUsePath(raw)
	rels := make([]types.RelationshipRecord, 0, len(syms))
	for _, s := range syms {
		props := map[string]string{
			"import_path":   s.path,
			"source_module": s.path,
		}
		if s.wildcard {
			props["wildcard"] = "1"
			// The namespace local is the last concrete path segment (`use a::b::*`
			// → namespace local `b`); the per-symbol synth resolves `b.<member>`.
			if s.local != "" {
				props["local_name"] = s.local
			}
		} else {
			props["imported_name"] = s.imported
			props["local_name"] = s.local
		}
		rels = append(rels, types.RelationshipRecord{
			FromID:     file.Path,
			ToID:       s.path,
			Kind:       "IMPORTS",
			Properties: props,
		})
	}
	if len(rels) == 0 {
		rels = []types.RelationshipRecord{{FromID: file.Path, ToID: raw, Kind: "IMPORTS"}}
	}

	return types.EntityRecord{
		Name:          top,
		Kind:          "SCOPE.Component",
		SourceFile:    file.Path,
		Language:      "rust",
		Relationships: rels,
	}, true
}

// rustUseSym is one resolved symbol from a `use` declaration: its full
// `crate::path::Leaf` reference path (the IMPORTS ToID, which keeps the crate
// root as its leading `::`-segment so rustExternalPackageRoot resolves it), the
// imported (source) name, the local (possibly `as`-aliased) name, and whether
// it is a glob `*` namespace import.
type rustUseSym struct {
	path     string
	imported string
	local    string
	wildcard bool
}

// parseRustUsePath parses the text AFTER `use ` (and trailing `;`) into one
// entry per imported symbol. It handles the common static shapes:
//
//	a::b::C            → {path:a::b::C, imported:C, local:C}
//	a::b::C as D       → {path:a::b::C, imported:C, local:D}
//	a::b::{C, D as E}  → two entries, prefixed with `a::b::`
//	a::b::*            → {path:a::b, wildcard, local:b}
//	a                  → {path:a, imported:a, local:a}  (crate root binding)
//
// Nested brace groups are flattened recursively. Whitespace and `self` group
// members (`a::{self, B}` — the `self` re-exports the module `a` itself) are
// handled: `self` binds the trailing path segment as its local name.
func parseRustUsePath(raw string) []rustUseSym {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// Find a top-level `{ ... }` group (brace not nested inside an earlier one).
	open := strings.IndexByte(raw, '{')
	if open < 0 {
		return []rustUseSym{rustUseLeaf(raw)}
	}
	prefix := strings.TrimSpace(raw[:open])
	prefix = strings.TrimSuffix(prefix, "::")
	// Locate the matching close brace for this group.
	close := rustMatchBrace(raw, open)
	if close < 0 {
		return []rustUseSym{rustUseLeaf(strings.TrimSuffix(raw, "{"))}
	}
	inner := raw[open+1 : close]
	var out []rustUseSym
	for _, member := range splitRustTopLevel(inner) {
		member = strings.TrimSpace(member)
		if member == "" {
			continue
		}
		full := member
		if prefix != "" {
			if member == "self" {
				full = prefix // `a::{self}` re-binds module `a` itself.
			} else {
				full = prefix + "::" + member
			}
		}
		out = append(out, parseRustUsePath(full)...)
	}
	return out
}

// rustUseLeaf resolves a brace-free `use` path fragment into a single symbol.
func rustUseLeaf(path string) rustUseSym {
	path = strings.TrimSpace(path)
	// `as` alias: `a::b::C as D`.
	imported, local := path, ""
	if i := strings.Index(path, " as "); i >= 0 {
		imported = strings.TrimSpace(path[:i])
		local = strings.TrimSpace(path[i+4:])
		path = imported
	}
	// Glob namespace import: `a::b::*`.
	if strings.HasSuffix(path, "::*") || path == "*" {
		ns := strings.TrimSuffix(path, "::*")
		ns = strings.TrimSuffix(ns, "*")
		ns = strings.TrimSuffix(ns, "::")
		nsLeaf := ns
		if i := strings.LastIndex(ns, "::"); i >= 0 {
			nsLeaf = ns[i+2:]
		}
		return rustUseSym{path: ns, wildcard: true, local: strings.TrimSpace(nsLeaf)}
	}
	leaf := imported
	if i := strings.LastIndex(imported, "::"); i >= 0 {
		leaf = imported[i+2:]
	}
	if local == "" {
		local = leaf
	}
	return rustUseSym{path: path, imported: leaf, local: local}
}

// rustMatchBrace returns the index of the `}` matching the `{` at open, or -1.
func rustMatchBrace(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitRustTopLevel splits a brace-group body on top-level commas (commas not
// nested inside a deeper `{ ... }`).
func splitRustTopLevel(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

// stripRustVisibility removes a leading Rust visibility modifier from a
// declaration's source text. Handles `pub `, `pub(crate) `,
// `pub(super) `, `pub(in path::to::mod) `. Anything else is returned
// unchanged. Issue #101.
func stripRustVisibility(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "pub") {
		return s
	}
	rest := s[3:]
	if rest == "" {
		return s
	}
	// Plain `pub <decl>`.
	if rest[0] == ' ' || rest[0] == '\t' {
		return strings.TrimSpace(rest)
	}
	// Restricted vis: `pub(...) <decl>`.
	if rest[0] == '(' {
		if closeIdx := strings.IndexByte(rest, ')'); closeIdx >= 0 {
			return strings.TrimSpace(rest[closeIdx+1:])
		}
	}
	return s
}

// childFieldText extracts the text of a named child field.
func childFieldText(node *sitter.Node, field string, src []byte) string {
	child := node.ChildByFieldName(field)
	if child == nil {
		return ""
	}
	return string(src[child.StartByte():child.EndByte()])
}

// buildFnSignature builds the function signature (up to the body block).
func buildFnSignature(node *sitter.Node, src []byte) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, " {"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	if idx := strings.Index(raw, "\n"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	return strings.TrimSpace(raw)
}

// buildTypeSignature constructs a readable signature for struct/enum/trait/impl.
func buildTypeSignature(node *sitter.Node, src []byte, name string) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "{"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	if idx := strings.Index(raw, ";"); idx >= 0 {
		return strings.TrimSpace(raw[:idx+1])
	}
	if idx := strings.Index(raw, "\n"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	return name
}
