// Package php implements the tree-sitter–based extractor for PHP source files.
//
// Extracted entities:
//   - class_declaration     → Kind="SCOPE.Component", Subtype="class"
//   - interface_declaration → Kind="SCOPE.Component", Subtype="interface"
//     (+ CONTAINS edges to every method in the body)
//   - trait_declaration     → Kind="SCOPE.Component", Subtype="trait"
//     (+ CONTAINS edges to every method in the body)
//   - enum_declaration      → Kind="SCOPE.Schema", Subtype="enum"  (PHP 8.1+)
//     (backed enum case values stored in Properties)
//   - method_declaration    → Kind="SCOPE.Operation", Subtype="method"
//   - function_definition   → Kind="SCOPE.Operation", Subtype="function"
//   - namespace_definition       → IMPORTS relationship (file → own namespace)
//   - namespace_use_declaration  → IMPORTS relationship (file → imported FQN)
//
// Method/function bodies emit CALLS relationships (#376) for:
//   - function_call_expression  bareFunc()
//   - member_call_expression    $obj->method()
//   - scoped_call_expression    Foo::m() / self::m() / parent::m()
//   - object_creation_expression  new Foo()  (CALLS Foo, constructor edge)
//
// PHP 8.1 enums are extracted as SCOPE.Schema/enum, mirroring the Python and
// TypeScript enum_extraction convention so cross-stack type queries join on
// the same kind/subtype taxonomy.
//
// Backed enums (enum Status: string { case Active = 'active'; }) store case
// values alongside names in Properties["enum_member_values"] (name=value CSV)
// so downstream type queries can surface the wire values.
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package php

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("php", &Extractor{})
}

// Extractor implements extractor.Extractor for PHP.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "php" }

// Extract walks the tree-sitter CST and returns entity records for the PHP file.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if file.TSTree == nil || len(file.Content) == 0 {
		return nil, nil
	}

	var entities []types.EntityRecord
	// Issue #577 — emit file-level SCOPE.Component (subtype="file") so the
	// cross-repo import linker (#566) can map IMPORTS edges back to the
	// originating repo via the resolver's byName index. Generalises the
	// JS/TS fix from #570/#575.
	entities = append(entities, extractor.FileEntity(file))
	root := file.TSTree.RootNode()
	walk(root, file, "", &entities)
	// Issue #3641 (epic #3625) — config-key consumption edges
	// (getenv / $_ENV / Laravel env() / config()) → shared SCOPE.Config nodes.
	emitConfigConsumerEdges(root, file, &entities)
	// View-layer topology (epic #3628) — RENDERS edges from Laravel
	// controller actions to a shared SCOPE.Template node for view('name') /
	// View::make('name') shapes (dynamic / interpolated names are dropped).
	emitTemplateRenderEdges(root, file, &entities)
	// Localization topology (child of epic #3628) — USES_TRANSLATION edges from
	// functions / methods to a shared SCOPE.TranslationKey node for Laravel
	// `__('k')` / `trans('k')` shapes (dynamic / interpolated keys dropped).
	emitTranslationKeyEdges(root, file, &entities)
	// Error-flow topology (epic #3628) — THROWS / CATCHES edges from
	// operations to a shared SCOPE.ExceptionType node for `throw new X()` and
	// typed `catch (X $e)` (incl. PHP 8 union multi-catch). Dynamic / re-throw
	// shapes are dropped (precision-first).
	emitExceptionFlowEdges(root, file, &entities)
	// Constant-collection value-sets (data-model, epic #3628 / #4419) —
	// SCOPE.Enum value-set nodes for `const X = [...]` array maps, class /
	// interface / trait constant groups, PHP 8.1 backed enums, and
	// `define('X', [...])` maps so the member roster + literal values are
	// searchable and diffable. Empty collections emit no node (honest-partial).
	emitConstValueSets(root, file, &entities)
	// Issue #4686 (epic #4615 / #4672) — Pest test-scope owner. Pest specs put
	// their logic in anonymous `it('...', function () {...})` / `test(...)`
	// closures, which walk() (method/function declarations only) never mines, so
	// a Pest spec emitted zero CALLS and the handlers it exercises read untested.
	// This emits one SCOPE.Operation/test_scope per Pest spec file that owns the
	// receiver-typed CALLS edges reachable from those closures. PHPUnit
	// `function test_x()` methods are already mined by walk() (named methods), so
	// this pass touches only the anonymous-closure case (no double-emit).
	emitPHPTestScopeOwner(root, file, &entities)
	// Issue #4854 — in-file base-class EXTENDS for field-membership recursion.
	entities = attachPhpExtends(entities)
	// Issue #90 — language tag for resolver dynamic-pattern dispatch.
	extractor.TagRelationshipsLanguage(entities, "php")
	extractor.TagEntitiesLanguage(entities, "php")
	return entities, nil
}

// walk performs a depth-first traversal of the CST, collecting entities.
// parentClass is the bare name of the immediately-enclosing class (or "" at
// file scope) — methods declared inside a class body are emitted with
// Name="<Class>.<method>" so two classes in the same file declaring a
// same-named method produce distinct entity IDs (issue #145).
func walk(node ts.Node, file extractor.FileInput, parentClass string, out *[]types.EntityRecord) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "class_declaration":
		// Issue #145: emit class CONTAINS edges to every method declared
		// inside the class body. Snapshot the entity slice length before
		// recursing so we can attribute every operation appended during
		// the recursion to this class. Method Names are dotted
		// "<Class>.<method>" — same convention as Java/Go — so two
		// classes with same-named methods have distinct IDs.
		rec, ok := buildComponent(node, file, "class")
		if !ok {
			break
		}
		classIdx := len(*out)
		className := rec.Name
		*out = append(*out, rec)
		body := phpDeclBody(node)
		if body != nil {
			before := len(*out)
			for i := range body.ChildCount() {
				walk(body.Child(int(i)), file, className, out)
			}
			after := len(*out)
			for k := before; k < after; k++ {
				child := &(*out)[k]
				if child.Kind != "SCOPE.Operation" {
					continue
				}
				// Format-A structural-ref keyed on the source file
				// (issue #144 / #145) so the resolver disambiguates
				// by location when two classes in different files
				// declare same-named methods.
				toID := extractor.BuildOperationStructuralRef("php", file.Path, child.Name)
				(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
					types.RelationshipRecord{
						ToID: toID,
						Kind: "CONTAINS",
					})
			}
		}
		// Issue #4854 — general field membership: one SCOPE.Schema/field per
		// typed property + promoted constructor param, plus a class→field
		// CONTAINS edge so a plain data class has field children (dedups by
		// Name with the framework DTO members in #4613).
		fieldEnts, baseName := emitPhpFieldMembers(node, body, file.Content, className, file.Path)
		for _, fe := range fieldEnts {
			toID := extractor.BuildSchemaFieldStructuralRef("php", file.Path, fe.Name)
			(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
				types.RelationshipRecord{ToID: toID, Kind: "CONTAINS"})
		}
		*out = append(*out, fieldEnts...)
		if baseName != "" {
			if (*out)[classIdx].Metadata == nil {
				(*out)[classIdx].Metadata = map[string]interface{}{}
			}
			(*out)[classIdx].Metadata["base_candidate"] = baseName
		}
		return

	case "interface_declaration":
		// interface_extraction: emit entity + CONTAINS edges to every
		// method declared in the interface body so the graph records both
		// the structural type and its contract (method set).
		rec, ok := buildComponent(node, file, "interface")
		if !ok {
			break
		}
		ifaceIdx := len(*out)
		ifaceName := rec.Name
		*out = append(*out, rec)
		body := phpDeclBody(node)
		if body != nil {
			before := len(*out)
			for i := range body.ChildCount() {
				walk(body.Child(int(i)), file, ifaceName, out)
			}
			after := len(*out)
			for k := before; k < after; k++ {
				child := &(*out)[k]
				if child.Kind != "SCOPE.Operation" {
					continue
				}
				toID := extractor.BuildOperationStructuralRef("php", file.Path, child.Name)
				(*out)[ifaceIdx].Relationships = append((*out)[ifaceIdx].Relationships,
					types.RelationshipRecord{
						ToID: toID,
						Kind: "CONTAINS",
					})
			}
		}
		return

	case "trait_declaration":
		// type_extraction: PHP traits are first-class named types that
		// define reusable method sets. Emit as SCOPE.Component/trait +
		// CONTAINS edges to every method, mirroring the class convention.
		rec, ok := buildComponent(node, file, "trait")
		if !ok {
			break
		}
		traitIdx := len(*out)
		traitName := rec.Name
		*out = append(*out, rec)
		body := phpDeclBody(node)
		if body != nil {
			before := len(*out)
			for i := range body.ChildCount() {
				walk(body.Child(int(i)), file, traitName, out)
			}
			after := len(*out)
			for k := before; k < after; k++ {
				child := &(*out)[k]
				if child.Kind != "SCOPE.Operation" {
					continue
				}
				toID := extractor.BuildOperationStructuralRef("php", file.Path, child.Name)
				(*out)[traitIdx].Relationships = append((*out)[traitIdx].Relationships,
					types.RelationshipRecord{
						ToID: toID,
						Kind: "CONTAINS",
					})
			}
		}
		return

	case "enum_declaration":
		// PHP 8.1+ backed and pure enums.
		// Tree-sitter grammar node: enum_declaration with a `name` field.
		// Members are emitted as a comma-separated list in the Properties
		// "enum_members" key so downstream type queries can surface them.
		if rec, ok := buildEnum(node, file); ok {
			*out = append(*out, rec)
		}

	case "method_declaration":
		if rec, ok := buildOperation(node, file, "method"); ok {
			bareName := rec.Name
			if parentClass != "" {
				rec.Name = parentClass + "." + bareName
			}
			body := node.ChildByFieldName("body")
			if body == nil {
				body = findFirstChildOfType(node, "compound_statement")
			}
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(body, file.Content, bareName, parentClass)...)
			*out = append(*out, rec)
		}

	case "function_definition":
		if rec, ok := buildOperation(node, file, "function"); ok {
			body := node.ChildByFieldName("body")
			if body == nil {
				body = findFirstChildOfType(node, "compound_statement")
			}
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(body, file.Content, rec.Name, "")...)
			*out = append(*out, rec)
		}

	case "namespace_definition":
		if rec, ok := buildNamespace(node, file); ok {
			*out = append(*out, rec)
		}

	case "namespace_use_declaration":
		// Issue #102: emit one IMPORTS edge per `use` statement so the
		// synth allowlist (Symfony\, Doctrine\, Twig\, Psr\, ...) can
		// classify the FQN as ExternalKnown via the `\`-separator
		// branch in classifyExternal. Without this every `use Foo\Bar;`
		// would be invisible to the resolver and the bug-rate stays
		// pinned to whatever extractor emitted before #102.
		for _, rec := range buildUseImports(node, file) {
			*out = append(*out, rec)
		}
	}

	for i := range node.ChildCount() {
		walk(node.Child(int(i)), file, parentClass, out)
	}
}

// buildComponent creates a Component entity for class/interface declarations.
// Eloquent / Laravel framework labelling is applied via tagEloquent:
// models, migrations and controllers get framework="laravel" plus a kind
// discriminator in Properties.
func buildComponent(node ts.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}

	rec := types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Component",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "php",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildClassSignature(node, file.Content, name),
		EnrichmentRequired: false,
	}
	tagEloquent(&rec, node, file.Content, file.Path)
	return rec, true
}

// buildEnum creates a SCOPE.Schema/enum entity for PHP 8.1+ enum_declaration
// nodes. Backed enums (enum Status: string) and pure enums are both handled.
//
// Properties emitted:
//
//	"pattern_type"       → "enum"
//	"enum_members"       → comma-separated case names  (e.g. "Active,Inactive")
//	"enum_member_values" → comma-separated name=value pairs for backed enums
//	                       (e.g. "Active='active',Inactive='inactive'").
//	                       Omitted for pure enums (no backing type).
//	"enum_backing_type"  → "string" or "int" when a backing type is declared.
func buildEnum(node ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}

	// Detect backing type: enum Status: string { ... }
	// Tree-sitter PHP grammar exposes the backing type as the child after
	// the `:` token; its node type is "named_type" or "primitive_type".
	backingType := phpEnumBackingType(node, file.Content)

	// Collect enum case names (and values for backed enums) from the body.
	var members []string
	var memberValues []string // name=value pairs
	body := phpDeclBody(node)
	if body != nil {
		for i := range body.ChildCount() {
			ch := body.Child(int(i))
			if ch.Type() != "enum_case" {
				continue
			}
			caseName := childFieldText(ch, "name", file.Content)
			if caseName == "" {
				continue
			}
			members = append(members, caseName)
			// Backed enum: `case Active = 'active';`
			// The value is in the "value" field of the enum_case node.
			val := childFieldText(ch, "value", file.Content)
			if val != "" {
				memberValues = append(memberValues, caseName+"="+val)
			}
		}
	}

	props := map[string]string{
		"pattern_type": "enum",
		"enum_members": strings.Join(members, ","),
	}
	if backingType != "" {
		props["enum_backing_type"] = backingType
	}
	if len(memberValues) > 0 {
		props["enum_member_values"] = strings.Join(memberValues, ",")
	}

	rec := types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Schema",
		Subtype:            "enum",
		SourceFile:         file.Path,
		Language:           "php",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		EnrichmentRequired: false,
		Properties:         props,
	}
	rec.ID = rec.ComputeID()
	return rec, true
}

// phpEnumBackingType returns the backing type text ("string"/"int") of a
// backed PHP 8.1 enum, or "" for pure enums. Tree-sitter PHP grammar
// places the backing type as a child node between `:` and the body.
func phpEnumBackingType(node ts.Node, src []byte) string {
	// Scan direct children for a named_type or primitive_type child that
	// follows the colon token. The colon itself appears as a ":" leaf.
	seenColon := false
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch.Type() == ":" {
			seenColon = true
			continue
		}
		if seenColon {
			t := ch.Type()
			if t == "named_type" || t == "primitive_type" || t == "name" {
				return strings.TrimSpace(string(src[ch.StartByte():ch.EndByte()]))
			}
			// Stop looking once we hit the body.
			if t == "declaration_list" || t == "enum_declaration_list" {
				break
			}
		}
	}
	return ""
}

// phpDeclBody returns the declaration-list body child of a class/interface/
// trait/enum node, trying the named "body" field first and falling back to
// scanning for a declaration_list or enum_declaration_list child. This is
// needed because older grammar revisions may not label the body field.
func phpDeclBody(node ts.Node) ts.Node {
	if body := node.ChildByFieldName("body"); body != nil {
		return body
	}
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		switch ch.Type() {
		case "declaration_list", "enum_declaration_list":
			return ch
		}
	}
	return nil
}

// buildOperation creates an Operation entity for method/function declarations.
func buildOperation(node ts.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "php",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildMethodSignature(node, file.Content),
		EnrichmentRequired: false,
	}, true
}

// buildNamespace emits a Component representing a PHP namespace.
func buildNamespace(node ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		// Fallback: extract text after "namespace " keyword
		raw := strings.TrimSpace(string(file.Content[node.StartByte():node.EndByte()]))
		raw = strings.TrimPrefix(raw, "namespace ")
		if idx := strings.IndexAny(raw, " {;"); idx >= 0 {
			raw = raw[:idx]
		}
		name = strings.TrimSpace(raw)
	}
	if name == "" {
		return types.EntityRecord{}, false
	}

	top := name
	if idx := strings.Index(name, "\\"); idx >= 0 {
		top = name[:idx]
	}

	return types.EntityRecord{
		Name:       top,
		Kind:       "SCOPE.Component",
		SourceFile: file.Path,
		Language:   "php",
		Relationships: []types.RelationshipRecord{
			{
				FromID: file.Path,
				ToID:   name,
				Kind:   "IMPORTS",
			},
		},
	}, true
}

// buildUseImports emits one IMPORTS edge per imported symbol on a
// `namespace_use_declaration` node. Issue #102.
//
// PHP `use` shapes handled:
//   - simple:    use Foo\Bar;                 → IMPORTS Foo\Bar
//   - aliased:   use Foo\Bar as B;            → IMPORTS Foo\Bar (alias dropped)
//   - function:  use function Foo\helper;     → IMPORTS Foo\helper
//   - const:     use const Foo\PI;            → IMPORTS Foo\PI
//   - grouped:   use Foo\Bar\{A, B as C};     → IMPORTS Foo\Bar\A, Foo\Bar\B
//
// Aliases are intentionally stripped: the synth allowlist matches on the
// root namespace segment (Symfony, Doctrine, Twig, ...), so emitting the
// canonical FQN gives the synth `\`-branch a clean root to classify.
func buildUseImports(node ts.Node, file extractor.FileInput) []types.EntityRecord {
	if node == nil {
		return nil
	}

	// Detect grouped use: a child of type "namespace_use_group" preceded
	// by a `namespace_name` prefix. Tree-sitter PHP exposes the prefix
	// directly as a sibling child of the declaration node.
	var prefix string
	for i := range int(node.ChildCount()) {
		ch := node.Child(i)
		switch ch.Type() {
		case "namespace_name":
			prefix = strings.TrimSpace(string(file.Content[ch.StartByte():ch.EndByte()]))
		case "namespace_use_group":
			return buildUseGroup(ch, file, prefix)
		}
	}

	// Simple/aliased/function/const forms — one or more
	// `namespace_use_clause` children. (PHP allows comma-separated
	// clauses like `use Foo, Bar;` though it's rare.)
	var out []types.EntityRecord
	for i := range int(node.ChildCount()) {
		ch := node.Child(i)
		if ch.Type() != "namespace_use_clause" {
			continue
		}
		fqn := useClauseFQN(ch, file.Content)
		if fqn == "" {
			continue
		}
		out = append(out, useImportRecord(fqn, file.Path))
	}
	return out
}

// buildUseGroup expands a `namespace_use_group` node by joining each
// clause's name onto the shared prefix. Issue #102.
func buildUseGroup(group ts.Node, file extractor.FileInput, prefix string) []types.EntityRecord {
	if group == nil || prefix == "" {
		return nil
	}
	var out []types.EntityRecord
	for i := range int(group.ChildCount()) {
		ch := group.Child(i)
		// Tree-sitter PHP uses `namespace_use_group_clause` for grouped
		// imports and `namespace_use_clause` for non-grouped — accept
		// both so the code is robust to grammar revisions.
		if ch.Type() != "namespace_use_group_clause" && ch.Type() != "namespace_use_clause" {
			continue
		}
		tail := useClauseFQN(ch, file.Content)
		if tail == "" {
			continue
		}
		fqn := prefix + "\\" + strings.TrimPrefix(tail, "\\")
		out = append(out, useImportRecord(fqn, file.Path))
	}
	return out
}

// useClauseFQN returns the qualified-name text of a namespace_use_clause,
// stripping any trailing `as Alias` segment. Returns "" when the clause
// has no qualified_name child (defensive — malformed input).
func useClauseFQN(clause ts.Node, src []byte) string {
	for i := range int(clause.ChildCount()) {
		ch := clause.Child(i)
		// `qualified_name` / `name` cover plain `use` clauses;
		// `namespace_name` covers `namespace_use_group_clause` children
		// (group imports), which wrap the trailing segment in a
		// namespace_name even when it's a single name.
		switch ch.Type() {
		case "qualified_name", "name", "namespace_name":
			return strings.TrimSpace(string(src[ch.StartByte():ch.EndByte()]))
		}
	}
	// Fallback: take clause text up to " as ".
	raw := strings.TrimSpace(string(src[clause.StartByte():clause.EndByte()]))
	if idx := strings.Index(raw, " as "); idx > 0 {
		raw = raw[:idx]
	}
	return strings.TrimSpace(raw)
}

// useImportRecord builds a SCOPE.Component placeholder + IMPORTS edge
// for a single PHP use-statement target. The component Name is the top
// namespace segment (Symfony, Doctrine, App, ...) — same convention as
// buildNamespace — so emitting the same `use` from multiple files
// idempotently merges to one Component per top-level namespace.
//
// Issue #113 — IMPORTS edges carry the same Properties contract as
// Python (#93) and Java (#120) so the cross-file resolver can build a
// per-file binding table:
//
//	Properties["local_name"]    — bare leaf identifier introduced into
//	                              the importing file. For `use Foo\Bar`
//	                              this is "Bar"; aliases are intentionally
//	                              dropped at FQN-extraction time so the
//	                              alias is not visible here.
//	Properties["source_module"] — dotted-namespace path with the leaf
//	                              stripped, slashes normalized to dots
//	                              ("Foo\\Bar" → source_module="Foo").
//	                              This matches the form modulesForPHPFile
//	                              produces in the resolver.
//	Properties["imported_name"] — equal to local_name. The shape `use
//	                              function Foo\helper;` and `use const
//	                              Foo\PI;` are treated identically — the
//	                              leaf identifier is the importable
//	                              symbol name regardless of the
//	                              function/const sub-form.
func useImportRecord(fqn, srcPath string) types.EntityRecord {
	// Strip leading '\' (PHP allows fully-qualified `use \Foo\Bar`).
	fqn = strings.TrimPrefix(fqn, "\\")
	top := fqn
	if idx := strings.Index(fqn, "\\"); idx >= 0 {
		top = fqn[:idx]
	}

	// Derive (source_module, local_name) pair. local_name is the leaf
	// (last backslash-separated segment); source_module is the prefix
	// with slashes converted to dots so it matches the resolver's
	// modulesByName index. A FQN without a backslash separator (rare
	// — `use Foo;`) sets source_module = the FQN itself and leaf =
	// the FQN; the resolver will skip it (no leaf separator).
	leaf := fqn
	mod := fqn
	if idx := strings.LastIndex(fqn, "\\"); idx >= 0 {
		leaf = fqn[idx+1:]
		mod = strings.ReplaceAll(fqn[:idx], "\\", ".")
	}
	props := map[string]string{
		"local_name":    leaf,
		"source_module": mod,
		"imported_name": leaf,
	}

	return types.EntityRecord{
		Name:       top,
		Kind:       "SCOPE.Component",
		SourceFile: srcPath,
		Language:   "php",
		Relationships: []types.RelationshipRecord{
			{
				FromID:     srcPath,
				ToID:       fqn,
				Kind:       "IMPORTS",
				Properties: props,
			},
		},
	}
}

// childFieldText extracts the text of a named child field.
func childFieldText(node ts.Node, field string, src []byte) string {
	child := node.ChildByFieldName(field)
	if child == nil {
		return ""
	}
	return string(src[child.StartByte():child.EndByte()])
}

// buildMethodSignature builds a Python-parity method signature.
// Python strips visibility modifiers and return types, keeping only:
//
//	function name(params)
func buildMethodSignature(node ts.Node, src []byte) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "\n"); idx >= 0 {
		raw = raw[:idx]
	}
	raw = strings.TrimSpace(raw)

	// Strip trailing { or body.
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = strings.TrimSpace(raw[:idx])
	}

	// Strip return type annotation ": type" after closing paren.
	if parenIdx := strings.LastIndex(raw, ")"); parenIdx >= 0 {
		afterParen := raw[parenIdx+1:]
		if colonIdx := strings.Index(afterParen, ":"); colonIdx >= 0 {
			raw = raw[:parenIdx+1]
		}
	}

	// Strip visibility modifiers to match Python convention.
	for _, mod := range []string{"public ", "private ", "protected ", "static "} {
		raw = strings.TrimPrefix(raw, mod)
	}
	return strings.TrimSpace(raw)
}

// buildClassSignature constructs a readable signature up to the class body.
func buildClassSignature(node ts.Node, src []byte, name string) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "{"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	if idx := strings.Index(raw, "\n"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	return name
}

// findFirstChildOfType returns the first direct child whose Type() == kind.
// Used as a fallback when ChildByFieldName("body") fails on older grammar
// revisions that didn't label the body field.
func findFirstChildOfType(n ts.Node, kind string) ts.Node {
	if n == nil {
		return nil
	}
	for i := range int(n.ChildCount()) {
		ch := n.Child(i)
		if ch.Type() == kind {
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
		if set[n.Type()] {
			out = append(out, n)
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return out
}

// extractCallRelationships returns one CALLS RelationshipRecord per unique
// callable invocation descendant of body. Issue #376.
//
// Tree-sitter PHP exposes four invocation node types:
//
//	function_call_expression    bareFunc()              — `function` field is the leaf name
//	member_call_expression      $obj->method()          — `object` + `name` fields
//	scoped_call_expression      Foo::m() / self::m()    — `scope` + `name` fields
//	object_creation_expression  new Foo()               — constructor edge
//
// Receiver-type inference (stamped as Properties["receiver_type"] when known
// and used by the resolver to bind same-name methods to the right class):
//
//	$this->m()                       → "<parentClass>.m"
//	self::m() / static::m()          → "<parentClass>.m"
//	Foo::m()                         → "Foo.m" (scope is a `name` node)
//	$x->m() with `$x = new Foo()`    → "Foo.m"
//	$x->m() preceded by /** @var Foo $x */
//	                                 → "Foo.m"
//	$x->m() otherwise                → bare leaf "m"
//	bareFunc()                       → bare leaf "bareFunc"
//	new Foo()                        → "Foo" (constructor)
//
// FromID is left empty so buildDocument substitutes the caller's entity ID
// at emit time. Self-recursion (target leaf == callerName, dotted match
// against parentClass + "." + callerName included) is dropped.
func extractCallRelationships(body ts.Node, src []byte, callerName, parentClass string) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	locals := collectLocalVarTypes(body, src)
	selfQualified := callerName
	if parentClass != "" {
		selfQualified = parentClass + "." + callerName
	}
	calls := findAllNodes(body,
		"function_call_expression",
		"member_call_expression",
		"scoped_call_expression",
		"object_creation_expression",
	)
	if len(calls) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(calls))
	rels := make([]types.RelationshipRecord, 0, len(calls))
	for _, call := range calls {
		target, recvType := phpCallTarget(call, src, parentClass, locals)
		if target == "" {
			continue
		}
		// Self-recursion check on the bare leaf.
		leaf := target
		if dot := strings.LastIndexByte(target, '.'); dot >= 0 {
			leaf = target[dot+1:]
		}
		if leaf == callerName && (target == callerName || target == selfQualified) {
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		r := types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
			Properties: map[string]string{
				"line": strconv.Itoa(int(call.StartPoint().Row) + 1),
			},
		}
		if recvType != "" {
			r.Properties["receiver_type"] = recvType
		}
		rels = append(rels, r)
	}
	return rels
}

// phpCallTarget resolves the callee target from one of the four PHP
// invocation nodes. Returns (target, receiverType). receiverType is "" when
// no static type for the receiver is determinable. Issue #376.
func phpCallTarget(call ts.Node, src []byte, parentClass string, locals map[string]string) (string, string) {
	switch call.Type() {
	case "function_call_expression":
		fn := call.ChildByFieldName("function")
		if fn == nil {
			return "", ""
		}
		// Only emit for plain `name` callees. Variable-function calls
		// (`$callable($x)`) and chained call results are too dynamic
		// for static binding; the resolver would just route them to
		// bug-resolver, so skip.
		if fn.Type() != "name" {
			return "", ""
		}
		return string(src[fn.StartByte():fn.EndByte()]), ""

	case "member_call_expression":
		nameNode := call.ChildByFieldName("name")
		if nameNode == nil || nameNode.Type() != "name" {
			return "", ""
		}
		method := string(src[nameNode.StartByte():nameNode.EndByte()])
		obj := call.ChildByFieldName("object")
		if obj == nil {
			return method, ""
		}
		// $this->m() — receiver is the enclosing class.
		if obj.Type() == "variable_name" {
			vname := string(src[obj.StartByte():obj.EndByte()])
			if vname == "$this" && parentClass != "" {
				return parentClass + "." + method, parentClass
			}
			if t, ok := locals[vname]; ok && t != "" {
				return t + "." + method, t
			}
		}
		return method, ""

	case "scoped_call_expression":
		nameNode := call.ChildByFieldName("name")
		if nameNode == nil || nameNode.Type() != "name" {
			return "", ""
		}
		method := string(src[nameNode.StartByte():nameNode.EndByte()])
		scope := call.ChildByFieldName("scope")
		if scope == nil {
			return method, ""
		}
		switch scope.Type() {
		case "name":
			// Foo::m() — explicit type name.
			t := string(src[scope.StartByte():scope.EndByte()])
			return t + "." + method, t
		case "qualified_name":
			// \Foo\Bar::m() — leaf segment is the bound type.
			raw := strings.TrimSpace(string(src[scope.StartByte():scope.EndByte()]))
			leaf := raw
			if i := strings.LastIndex(raw, "\\"); i >= 0 {
				leaf = raw[i+1:]
			}
			if leaf == "" {
				return method, ""
			}
			return leaf + "." + method, leaf
		case "relative_scope":
			// self::m() / static::m() / parent::m() — bind to enclosing
			// class when known. parent:: would ideally bind to the
			// declared parent class, but the extractor doesn't track
			// `extends` here; binding to parentClass is a best-effort
			// shape that keeps the receiver_type stamp accurate for the
			// caller-side and lets the resolver fall back to the bare
			// leaf when needed.
			if parentClass == "" {
				return method, ""
			}
			return parentClass + "." + method, parentClass
		}
		return method, ""

	case "object_creation_expression":
		// new Foo() — emit a CALLS edge to "Foo". The resolver treats
		// this as a constructor reference so framework classes
		// (Symfony Request, Doctrine EntityManager, …) appear as
		// outgoing edges from the calling method.
		var typeNode ts.Node
		// `type`/`name` fields are not consistently set across grammar
		// revisions; the type is the first `name` or `qualified_name`
		// child after the `new` keyword.
		for i := range int(call.ChildCount()) {
			ch := call.Child(i)
			if ch.Type() == "name" || ch.Type() == "qualified_name" {
				typeNode = ch
				break
			}
		}
		if typeNode == nil {
			return "", ""
		}
		raw := strings.TrimSpace(string(src[typeNode.StartByte():typeNode.EndByte()]))
		// Take the leaf segment of a qualified name.
		if i := strings.LastIndex(raw, "\\"); i >= 0 {
			raw = raw[i+1:]
		}
		if raw == "" {
			return "", ""
		}
		return raw, ""
	}
	return "", ""
}

// docblockVarRE matches `@var Type $name` inside a /** ... */ docblock.
// PHPDoc allows `@var Foo\Bar $x`, `@var ?Foo $x`, `@var Foo|null $x` —
// we capture the first type token (up to whitespace, `|`, or `&`) and
// strip a leading `?` nullable marker. Union/intersection types after the
// first segment are dropped — receiver-type stamping needs a single class
// name to bind, and downstream synth handles unions via the resolver.
var docblockVarRE = regexp.MustCompile(`@var\s+\??([A-Za-z_\\][A-Za-z0-9_\\]*)\s+\$([A-Za-z_][A-Za-z0-9_]*)`)

// collectLocalVarTypes scans the method/function body for two binding
// patterns and returns a map from `$varname` to bare type leaf.
//
//  1. Assignment with constructor:   $x = new Foo();           → x → Foo
//  2. PHPDoc `@var` immediately before a use:  /** @var Foo $x */
//
// Returned keys carry the leading `$` so callers can match them directly
// against `variable_name` token text.
func collectLocalVarTypes(body ts.Node, src []byte) map[string]string {
	if body == nil {
		return nil
	}
	out := map[string]string{}
	// Pattern 1: assignment_expression where rhs is object_creation_expression.
	for _, n := range findAllNodes(body, "assignment_expression") {
		left := n.ChildByFieldName("left")
		right := n.ChildByFieldName("right")
		if left == nil || right == nil {
			continue
		}
		if left.Type() != "variable_name" {
			continue
		}
		if right.Type() != "object_creation_expression" {
			continue
		}
		// Type leaf — first name/qualified_name child of the new-expr.
		var typeNode ts.Node
		for i := range int(right.ChildCount()) {
			ch := right.Child(i)
			if ch.Type() == "name" || ch.Type() == "qualified_name" {
				typeNode = ch
				break
			}
		}
		if typeNode == nil {
			continue
		}
		raw := strings.TrimSpace(string(src[typeNode.StartByte():typeNode.EndByte()]))
		if i := strings.LastIndex(raw, "\\"); i >= 0 {
			raw = raw[i+1:]
		}
		if raw == "" {
			continue
		}
		vname := string(src[left.StartByte():left.EndByte()])
		out[vname] = raw
	}
	// Pattern 2: PHPDoc `@var Type $name` comments. tree-sitter exposes
	// docblocks as `comment` nodes — scan all and apply the regex.
	for _, c := range findAllNodes(body, "comment") {
		text := string(src[c.StartByte():c.EndByte()])
		if !strings.HasPrefix(text, "/**") {
			continue
		}
		for _, m := range docblockVarRE.FindAllStringSubmatch(text, -1) {
			typ := m[1]
			if i := strings.LastIndex(typ, "\\"); i >= 0 {
				typ = typ[i+1:]
			}
			out["$"+m[2]] = typ
		}
	}
	return out
}
