// Package javascript implements the JS/TS language extractor for archigraph.
//
// A single JSExtractor handles both "javascript" and "typescript" because the
// TypeScript grammar is a strict superset of JavaScript. The OTel span name is
// "extractor.javascript" for both; the Language attribute distinguishes them.
//
// Registered languages: "javascript", "typescript"
//
// Extracted node types:
//   - function_declaration       → Kind="SCOPE.Operation"
//   - arrow_function (via const) → Kind="SCOPE.Operation"
//   - function_expression (via const) → Kind="SCOPE.Operation"
//   - class_declaration          → Kind="SCOPE.Component"
//   - method_definition          → Kind="SCOPE.Operation"
//   - public_field_definition (arrow RHS) → Kind="SCOPE.Operation" (issue #771)
//   - public_field_definition (non-arrow, plain value) → Kind="SCOPE.Schema/field" (issue #679)
//   - interface_declaration (TS) → Kind="SCOPE.Schema" subtype="interface"
//     Properties: fields (comma-sep), generics (comma-sep), extends (comma-sep)
//     Edges: EXTENDS per extends clause (issue #1343)
//   - type_alias_declaration (TS)→ Kind="SCOPE.Schema" subtype="type_alias"
//     Properties: generics (comma-sep), type_body (raw rhs text) (issue #1343)
//   - enum_declaration (TS)      → Kind="SCOPE.Schema" subtype="enum"
//     Properties: members (comma-sep) (issue #1343)
//   - import_statement + require → IMPORTS edge on file entity (issue #742)
//   - top-level const FOO = <primitive> → Kind="SCOPE.Schema" subtype="constant"
//     Properties: value (raw literal, quotes stripped for strings) (issue #1968)
package javascript

import (
	"context"
	"fmt"
	"os"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// emitDestructureDetailEnv is the env var that opts const_destructure /
// const_destructure_call subtype emission back on.
//
// Issue #2338: on the UpVate bench corpus the JS/TS extractor produced
// ~2,633 SCOPE.Component/const_destructure entities and ~1,294
// SCOPE.Operation/const_destructure_call entities (~4,000 total, ~20% of
// the graph) whose consumer landscape was not fully verified. Binding
// entities are still emitted in default-off mode with subtype "const" or
// "function" (same as other const declarations) so per-binding resolution
// continues to work. What is lost is the specific destructure-pattern
// label; set this flag to "1" or "true" to restore it.
//
// Accepted truthy values: "1", "true" (case-sensitive). Anything else,
// including unset, leaves destructure-detail emission disabled.
const emitDestructureDetailEnv = "ARCHIGRAPH_EMIT_DESTRUCTURE_DETAIL"

// emitDestructureDetailEnabled reports whether const_destructure /
// const_destructure_call subtypes should be emitted. Reads the env var on
// every call — cheap, and lets tests toggle via t.Setenv without restarting.
func emitDestructureDetailEnabled() bool {
	v := os.Getenv(emitDestructureDetailEnv)
	return v == "1" || v == "true"
}

// New returns a new JSExtractor. Use this in tests or explicit registrations.
func New() *JSExtractor {
	return &JSExtractor{}
}

// init registers the extractor for both "javascript" and "typescript".
func init() {
	e := New()
	extreg.Register("javascript", e)
	extreg.Register("typescript", e)
}

// JSExtractor extracts entities from JavaScript and TypeScript source files.
// It is safe for concurrent use.
type JSExtractor struct{}

// Language returns the canonical language name (used for registration).
func (e *JSExtractor) Language() string {
	return "javascript"
}

// Extract processes a parsed JS/TS source file and returns entity records.
//
// The tree-sitter parse tree (file.Tree) may be nil for empty files, in which
// case an empty slice is returned. Partial results are returned when individual
// node queries fail; errors are logged but never abort the full extraction.
func (e *JSExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("javascript")
	_, span := tracer.Start(ctx, "extractor.javascript",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file", file.Path),
			attribute.Int("file_size_bytes", len(file.Content)),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Tree == nil {
		span.SetAttributes(
			attribute.Int("entity_count", 0),
			attribute.Int("relationship_count", 0),
		)
		return nil, nil
	}

	root := file.Tree.RootNode()
	if root == nil {
		return nil, nil
	}

	x := &extractor{
		source:   file.Content,
		filePath: file.Path,
		language: file.Language,
		// Issue #842 — use AliasMapForFile so monorepo packages with their
		// own tsconfig.json (e.g. frontend/tsconfig.json) contribute their
		// aliases to files under that subdirectory, rather than only
		// considering the repo-root tsconfig.
		aliases:  AliasMapForFile(file.RepoRoot, file.Path),
		repoRoot: file.RepoRoot,
		// Issue #1616 — derive the dotted module path once so emit() can
		// stamp QualifiedName on every entity.
		module: dottedModuleFromPath(file.Path),
	}

	// Issue #570 — emit a file-level SCOPE.Component (subtype="file")
	// entity per source file so the cross-repo import linker (#566)
	// can map IMPORTS edges back to the originating repo via the
	// resolver's byName index. Before this change every JS/TS IMPORTS
	// edge carried FromID=<file path string>; the linker's
	// `entRepo[edge.FromID]` lookup missed because no entity had that
	// path as its ID, collapsing the candidate cross-repo
	// `method=import` link count to ~0 across the client-fixture
	// group despite #566/#569 making the rest of the pipeline work
	// end-to-end. With the file-level entity present, the resolver's
	// ReferencesEmbeddedWithAllowlist pass rewrites the IMPORTS
	// FromID from the path string to the file entity's stamped hex ID
	// (graph.EntityID(repoTag, "SCOPE.Component", path, path)) via
	// byName, and the linker then matches it back to the source repo.
	// We do NOT pre-stamp the ID here — the extractor doesn't know
	// the indexer's repoTag seed, so any hex we wrote would be
	// short-circuited as already-hex by isHexID in the resolver and
	// the rewrite would never happen.
	fileEntity := types.EntityRecord{
		Name:       file.Path,
		Kind:       "SCOPE.Component",
		SourceFile: file.Path,
		Language:   file.Language,
		Subtype:    "file",
		Properties: map[string]string{
			"kind":    "SCOPE.Component",
			"subtype": "file",
		},
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
	}
	x.entities = append(x.entities, fileEntity)

	// Issue #421 — collect import bindings BEFORE walking the body so
	// receiver-typed CALLS edges materialised inside class methods can
	// look up the imported source file at emission time. Bindings is
	// indexed by the local name introduced into the file scope; the
	// receiver binder consults it when the receiver's declared type
	// matches a binding.
	x.imports = x.collectFileImports(root)
	x.importByLocal = make(map[string]*importBinding, len(x.imports))
	for i := range x.imports {
		b := &x.imports[i]
		if existing, dup := x.importByLocal[b.localName]; dup {
			// Issue #505 — when multiple bindings share localName,
			// they MAY be alias-resolved variants of the same import
			// statement (one binding per candidate target). Detect
			// that shape (same importPath + same importedName +
			// aliasResolved on both) and prefer the first
			// registration silently. Genuine duplicates (different
			// importPath or importedName) still hit the safer-bias
			// "drop both" path.
			if existing.importPath == b.importPath &&
				existing.importedName == b.importedName &&
				existing.aliasResolved && b.aliasResolved {
				continue
			}
			// Last-writer-wins is unsafe; drop both. The receiver
			// binder leaves the bare method name in place when the
			// type lookup misses, which is the conservative bias.
			delete(x.importByLocal, b.localName)
			continue
		}
		x.importByLocal[b.localName] = b
	}

	// Issues #514 / #517 — build the framework-DSL tracker before walk()
	// so that extractCallRelationships can stamp receiver_package on
	// CALLS edges originating from Express-family or NestJS receivers.
	// buildFrameworkDSLTracker is cheap: it iterates importByLocal (already
	// populated above) and does one pass over the AST to find factory-call
	// assignments. It returns nil when no express-family import is present.
	x.frameworkDSL = x.buildFrameworkDSLTracker(root)

	// Issue #44 (TS/JS slice) — build the hook-variable map before walk()
	// so that callTarget can rewrite CALLS-to-hook-result edges.
	x.hookVarToModule = x.buildHookVarToModule(root)

	var extractErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				extractErr = fmt.Errorf("javascript extractor panicked: %v", r)
			}
		}()
		x.walk(root, "", nil)
		// Issue #742 — snapshot length before collectImports so we can
		// identify which entities were added by it (the import-placeholder
		// SCOPE.Component/import entities). After collectImports we call
		// attachImportRelationshipsJS to lift those IMPORTS relationships
		// onto the file entity (entities[0]) and drop the now-redundant
		// wrapper entities. Mirrors Java #681/#694 and Python #693/#715.
		preImportLen := len(x.entities)
		x.collectImports(root)
		if len(x.entities) > preImportLen && len(x.entities) > 0 {
			importSlice := x.entities[preImportLen:]
			kept := attachImportRelationshipsJS(importSlice, &x.entities[0])
			x.entities = append(x.entities[:preImportLen], kept...)
		}
		// Second pass: REFERENCES-edge emission. Runs AFTER walk +
		// collectImports so the file-scope symbol table covers every
		// declared name (functions, methods, consts, destructured
		// bindings). Import-placeholder entities are no longer emitted
		// (issue #742); IMPORTS edges live on the file entity instead.
		// emitReferences is wrapped in the same recover frame so a
		// pathological AST shape can't take down the primary extraction.
		x.emitReferences(root)
	}()

	// Third pass (#713): platform-variant and test-file relationship emission.
	// Detects React Native platform-specific file naming (.ios.tsx,
	// .android.tsx, .tablet.tsx, …) and emits PLATFORM_VARIANT_OF edges.
	// Also detects *.test.tsx / *.spec.tsx files and emits a single
	// file-to-file TESTS edge. Runs after the primary walk so the file
	// entity already exists.
	x.emitPlatformVariantRelationships()

	// Fourth pass (#1726): per-operation TESTS edges. For files identified
	// as JS/TS test files (*.test.{ts,tsx,js,jsx,mjs,cjs},
	// *.spec.{...}, __tests__/, tests/), reclassify each CALLS edge from
	// every Operation entity as ALSO carrying a TESTS edge to the same
	// callee. This is the per-call equivalent of the file-to-file edge
	// emitted above and the JS/TS counterpart to the gain the cross/
	// testmap pass already delivers for Python (iter4: 87 → 459 TESTS
	// edges, but all gain in upvate-core/Python; frontend produced 1 and
	// mobile 0 across ~2500 test entities). emitTestsEdgesForTestFile is
	// a no-op for non-test files (cheap filename check) so the hot path
	// stays cheap.
	x.emitTestsEdgesForTestFile()

	if extractErr != nil {
		span.RecordError(extractErr)
		span.SetStatus(codes.Error, extractErr.Error())
		// Return partial results on panic.
		return x.entities, extractErr
	}

	span.SetAttributes(
		attribute.Int("entity_count", len(x.entities)),
		attribute.Int("relationship_count", len(x.relationships)),
	)

	// Issue #90 — stamp Properties["language"] (e.g. "javascript" or
	// "typescript") on every embedded relationship so the resolver routes
	// to the right per-language dynamic-pattern catalog.
	extreg.TagRelationshipsLanguage(x.entities, file.Language)
	extreg.TagEntitiesLanguage(x.entities, file.Language)

	return x.entities, nil
}

// extractor holds mutable extraction state for a single file.
type extractor struct {
	source        []byte
	filePath      string
	language      string
	entities      []types.EntityRecord
	relationships []types.RelationshipRecord

	// funcDepth tracks how many function/method bodies deep the current
	// walk is. Zero means module scope. Incremented before recursing into
	// a function body, decremented on exit. Used by handleVariableDeclarator
	// to suppress non-addressable local destructure bindings (#1748).
	funcDepth int

	// module — Issue #1616. The dotted module path derived from filePath
	// (e.g. "src/stores/authReducer.js" → "src.stores.authReducer"). Set
	// once in Extract before walk() runs. Used to populate every emitted
	// entity's QualifiedName ("<module>.<name>"), mirroring the Python
	// extractor (#1413). Empty when filePath is empty.
	module string

	// imports / importByLocal — issue #421. Populated once per file
	// before walk() runs. Receiver-typed CALLS emission consults
	// importByLocal[<typeName>] to resolve a typed receiver to the
	// source file declaring the type. importByLocal is nil-safe;
	// callers must check the lookup result.
	imports       []importBinding
	importByLocal map[string]*importBinding

	// aliases — issue #505. The merged per-repo path-alias map loaded
	// from tsconfig.json / vite.config / metro.config / babel.config.
	// Empty for projects without alias declarations; the resolver
	// gracefully no-ops on every lookup in that case.
	aliases AliasMap
	// repoRoot is the absolute path of the repository root, used to
	// filesystem-check alias candidate paths so multi-target aliases
	// (`@/*: ["./*", "./src/*"]`) pick the candidate that actually
	// exists on disk rather than emitting an IMPORTS edge per candidate
	// (which would inflate bug-extractor counts for the wrong ones).
	repoRoot string

	// frameworkDSL — issues #514 / #517. Built once per file after
	// importByLocal is populated; nil when no express-family import is
	// detected in the file (fast-path for non-Express files). When non-nil,
	// extractCallRelationships stamps Properties["receiver_package"] on
	// CALLS edges whose receiver traces to a framework-DSL object.
	frameworkDSL *frameworkDSLTracker

	// hookVarToModule — Issue #44 (TS/JS slice). Built once per file after
	// importByLocal is populated. Maps local variable names assigned from a
	// React-hook call (e.g. `const navigate = useNavigate()`) to the npm
	// package the hook was imported from (e.g. "react-router-dom"). This
	// lets callTarget rewrite CALLS-to-hook-result edges to
	// "ext:<module>" rather than the bare local-variable name, which
	// would otherwise be unresolvable and land in bug-extractor.
	hookVarToModule map[string]string
}

// applyAlias attempts to substitute a path-alias prefix in spec using
// the extractor's per-repo alias map. Returns the repo-relative
// POSIX path the alias resolves to (without an extension; the caller
// runs the same `.ts → .tsx → .js …` candidate-extension loop a
// relative import would), or "" when no alias matches.
//
// Specs that are already relative (`./` / `../`) or absolute (`/`) are
// bypassed unconditionally — alias substitution is a NON-relative-spec
// concern. Bare npm specs (`react`, `@tanstack/react-query`) fall
// through to the alias lookup, which is correct: `@tanstack/...` is
// also the shape an alias map could use, but in practice the alias
// table's prefix-vs-package-name disambiguation is left to the alias
// declaration itself (a project that aliases `@tanstack` shadows the
// npm package by definition).
func (x *extractor) applyAlias(spec string) string {
	if spec == "" {
		return ""
	}
	if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") || strings.HasPrefix(spec, "/") {
		return ""
	}
	return x.aliases.Resolve(spec)
}

// classBindings tracks receiver-typed identifiers visible inside a
// class body — typed property declarations and constructor parameters
// (TypeScript's "parameter property" shape). Map key is the field /
// parameter name; value is the declared leaf type identifier.
//
// Issue #421 — NestJS @Inject() style:
//
//	class UsersController {
//	  constructor(private readonly userService: UserService) {}
//	  list() { return this.userService.findAll(); }
//	}
//
// `userService` is BOTH a constructor parameter and an implicit class
// field; the binder treats them identically.
type classBindings struct {
	// fields maps field/parameter name → declared type identifier (leaf).
	fields map[string]string
	// className is the enclosing class's name. Empty when we're not
	// inside a class body.
	className string
}

// nodeText returns the UTF-8 text of a tree-sitter node.
func (x *extractor) nodeText(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	start := n.StartByte()
	end := n.EndByte()
	if end > uint32(len(x.source)) {
		end = uint32(len(x.source))
	}
	return string(x.source[start:end])
}

// lines returns (startLine, endLine) for a node, 1-indexed.
func lines(n *sitter.Node) (int, int) {
	start := int(n.StartPoint().Row) + 1
	end := int(n.EndPoint().Row) + 1
	return start, end
}

// qualify returns the module-path-qualified name for an emitted entity
// (Issue #1616). Mirrors the Python extractor (#1413): "<module>.<name>",
// falling back to the bare name when the module is empty. name may already
// carry a dotted class path for methods (e.g. "Foo.bar"), in which case the
// result is "<module>.Foo.bar".
func (x *extractor) qualify(name string) string {
	if x.module == "" {
		return name
	}
	return x.module + "." + name
}

// emit appends an entity to the extraction results.
func (x *extractor) emit(name, kind string, n *sitter.Node, subtype string, sig string) {
	if name == "" || name == "?" {
		return
	}
	x.emitWithRels(name, kind, n, subtype, sig, nil)
}

// emitWithRels appends an entity to the extraction results carrying the
// supplied embedded relationships.
func (x *extractor) emitWithRels(name, kind string, n *sitter.Node, subtype string, sig string, rels []types.RelationshipRecord) {
	if name == "" || name == "?" {
		return
	}
	start, end := lines(n)
	e := types.EntityRecord{
		Name:          name,
		QualifiedName: x.qualify(name),
		Kind:          kind,
		SourceFile:    x.filePath,
		StartLine:     start,
		EndLine:       end,
		Language:      x.language,
		Subtype:       subtype,
		Signature:     sig,
		Properties: map[string]string{
			"kind":    kind,
			"subtype": subtype,
		},
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
		Relationships:    rels,
	}
	e.ID = e.ComputeID()
	x.entities = append(x.entities, e)
}

// tagLocalScope stamps Properties["local_scope"]="true" on every entity
// appended to x.entities at index >= from. Called after emitting entities that
// were discovered inside a function/method body (funcDepth > 0) to mark them
// as non-addressable locals. The serving layer (denoise.go) uses this flag to
// hide these entities from archigraph_find results while still allowing the
// resolver to use them for REFERENCES/CALLS binding (#1748).
func (x *extractor) tagLocalScope(from int) {
	for i := from; i < len(x.entities); i++ {
		if x.entities[i].Properties == nil {
			x.entities[i].Properties = map[string]string{}
		}
		x.entities[i].Properties["local_scope"] = "true"
	}
}

// emitWithProps appends an entity to the extraction results using a caller-supplied
// Properties map rather than the default {"kind": ..., "subtype": ...} map.
// Used by handlers that need to store structured metadata (fields, generics, etc.).
func (x *extractor) emitWithProps(name, kind string, n *sitter.Node, subtype string, sig string, props map[string]string, rels []types.RelationshipRecord) {
	if name == "" || name == "?" {
		return
	}
	start, end := lines(n)
	e := types.EntityRecord{
		Name:             name,
		QualifiedName:    x.qualify(name),
		Kind:             kind,
		SourceFile:       x.filePath,
		StartLine:        start,
		EndLine:          end,
		Language:         x.language,
		Subtype:          subtype,
		Signature:        sig,
		Properties:       props,
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
		Relationships:    rels,
	}
	e.ID = e.ComputeID()
	x.entities = append(x.entities, e)
}

// walk performs a depth-first traversal of the CST, dispatching on node type.
// parentClass is non-empty when inside a class body (used to tag methods).
// cb carries the field-type bindings for the enclosing class so receiver-
// typed CALLS edges can resolve `this.<field>.<method>` and `<field>.<method>`
// shapes to the import-declared type (issue #421). cb is nil outside a
// class body.
func (x *extractor) walk(n *sitter.Node, parentClass string, cb *classBindings) {
	if n == nil {
		return
	}

	switch n.Type() {
	case "function_declaration":
		x.handleFunctionDeclaration(n, parentClass, cb)
		return // do not recurse into function body for name extraction

	case "class_declaration":
		x.handleClassDeclaration(n)
		return // recurse is handled inside

	case "method_definition":
		x.handleMethodDefinition(n, parentClass, cb)
		return

	case "public_field_definition", "field_definition":
		// Issue #771 — class-body `name = () => ...` arrow methods.
		// tree-sitter JavaScript grammar: "field_definition"
		// tree-sitter TypeScript grammar:  "public_field_definition"
		// Both have the same structure: name + optional type + value.
		// Only emits when the RHS is an arrow_function; plain value
		// assignments remain unhandled here and fall through to
		// walkChildren so nested constructs still get visited.
		x.handlePublicFieldDefinition(n, parentClass, cb)
		return

	case "interface_declaration":
		x.handleInterfaceDeclaration(n)
		return

	case "type_alias_declaration":
		x.handleTypeAliasDeclaration(n)
		return

	case "enum_declaration":
		x.handleEnumDeclaration(n)
		return

	case "lexical_declaration", "variable_declaration":
		// const/let foo = () => {} or = function() {}
		x.handleVariableDeclaration(n, parentClass, cb)
		// still recurse for nested structures at statement level
		return

	case "export_statement":
		// Recurse into the declaration inside the export.
		x.walkChildren(n, parentClass, cb)
		return
	}

	x.walkChildren(n, parentClass, cb)
}

func (x *extractor) walkChildren(n *sitter.Node, parentClass string, cb *classBindings) {
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		x.walk(n.Child(i), parentClass, cb)
	}
}

// handleFunctionDeclaration handles: function foo(...) { ... }
func (x *extractor) handleFunctionDeclaration(n *sitter.Node, parentClass string, cb *classBindings) {
	nameNode := n.ChildByFieldName("name")
	name := x.nodeText(nameNode)
	if name == "" {
		return
	}
	subtype := "function"
	if parentClass != "" {
		subtype = "method"
	}
	sig := fmt.Sprintf("function %s", name)
	body := n.ChildByFieldName("body")
	// Issue #421 — top-level functions can still take typed parameters
	// the receiver binder needs to consult (e.g. `function callIt(svc:
	// UserService) { svc.findOne(); }`). Build a function-scope binding
	// frame from the params node and pass it as the cb override.
	params := n.ChildByFieldName("parameters")
	frame := x.functionParamFrame(params, cb)
	rels := x.extractCallRelationships(body, name, frame)
	// Issue #610 — for PascalCase function components, scan the body for
	// JSX child elements and emit RENDERS edges so the component-composition
	// graph is complete.
	rels = append(rels, x.extractJSXRendersRelationships(body, name)...)
	x.emitWithRels(name, "SCOPE.Operation", n, subtype, sig, rels)

	// Recurse into the body for nested declarations.
	// Increment funcDepth so handleVariableDeclarator suppresses non-addressable
	// local destructure bindings discovered inside this body (#1748).
	if body != nil {
		x.funcDepth++
		x.walkChildren(body, parentClass, cb)
		x.funcDepth--
	}
}

// handleClassDeclaration handles: class Foo { ... }
//
// Emits one CONTAINS edge per method/operation entity declared directly inside
// the class body. The CONTAINS source is the class entity (FromID empty →
// substituted at emit time); the target is the bare method name.
func (x *extractor) handleClassDeclaration(n *sitter.Node) {
	nameNode := n.ChildByFieldName("name")
	className := x.nodeText(nameNode)
	if className == "" {
		return
	}

	// Snapshot the entity slice length before walking the body so we can
	// attribute every operation appended during the walk to this class.
	classIdx := len(x.entities)
	x.emit(className, "SCOPE.Component", n, "class", fmt.Sprintf("class %s", className))

	body := n.ChildByFieldName("body")
	// Issue #421 — collect the class's typed property declarations and
	// constructor parameter properties so receiver-typed CALLS inside
	// any method body can resolve `this.<field>` to the declared type.
	cb := &classBindings{className: className, fields: map[string]string{}}
	if body != nil {
		x.collectClassFields(body, cb.fields)
	}
	if body != nil {
		before := len(x.entities)
		x.walkChildren(body, className, cb)
		after := len(x.entities)
		for k := before; k < after; k++ {
			child := &x.entities[k]
			if child.Kind != "SCOPE.Operation" {
				continue
			}
			// Issue #144 — emit a structural-ref (Format A) keyed on the
			// source file so the resolver disambiguates by location when
			// two classes in different files declare same-named methods
			// (a common shape in Express/Nest/React-component apps).
			toID := extreg.BuildOperationStructuralRef(x.language, x.filePath, child.Name)
			x.entities[classIdx].Relationships = append(x.entities[classIdx].Relationships,
				types.RelationshipRecord{
					ToID: toID,
					Kind: "CONTAINS",
				})
		}
	}
}

// handleMethodDefinition handles method definitions inside class bodies.
func (x *extractor) handleMethodDefinition(n *sitter.Node, _ string, cb *classBindings) {
	nameNode := n.ChildByFieldName("name")
	name := x.nodeText(nameNode)
	if name == "" || name == "constructor" {
		return
	}
	body := n.ChildByFieldName("body")
	// Issue #421 — merge the class's field bindings with the method's
	// own typed parameters so a method param can shadow / extend the
	// receiver-type lookup scope (parameters win on conflict).
	params := n.ChildByFieldName("parameters")
	frame := x.functionParamFrame(params, cb)
	rels := x.extractCallRelationships(body, name, frame)
	x.emitWithRels(name, "SCOPE.Operation", n, "method", fmt.Sprintf("method %s", name), rels)
}

// handlePublicFieldDefinition handles class-body field assignments whose RHS
// is an arrow function. These are the "class-field arrow method" pattern
// common in AngularJS-style services and Angular components:
//
//	byId = (id) => this.$http.get('/users/' + id);
//	static getAll = async () => [...];
//	private save: (x: T) => void = async (x) => { ... };
//
// Issue #771 — tree-sitter emits these as `public_field_definition` (TS
// grammar) or `field_definition` (JS grammar) nodes. The name field differs:
//   - TypeScript `public_field_definition`: ChildByFieldName("name")
//   - JavaScript `field_definition`:        ChildByFieldName("property")
//
// The `value` field holds the RHS expression. If the value is NOT an
// arrow_function, this handler does nothing (plain value fields stay as
// non-Operation entities and the extractor recurses into their subtrees for
// nested constructs).
//
// The emitted entity subtype is "method" — consistent with how
// handleMethodDefinition classifies class methods.
func (x *extractor) handlePublicFieldDefinition(n *sitter.Node, parentClass string, cb *classBindings) {
	// TypeScript grammar: "name" field; JavaScript grammar: "property" field.
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		nameNode = n.ChildByFieldName("property")
	}
	name := x.nodeText(nameNode)
	if name == "" {
		return
	}

	valueNode := n.ChildByFieldName("value")
	if valueNode == nil || valueNode.Type() != "arrow_function" {
		// Not an arrow method — recurse into children so nested
		// declarations (e.g. arrow inside object literal RHS) still get
		// visited. Issue #679: also emit a SCOPE.Schema/field entity for
		// plain class fields (e.g. `name: string`, `count = 0`) so that
		// `this.<field>` REFERENCES edges have a resolvable target.
		if parentClass != "" {
			emittedName := parentClass + "." + name
			sig := name
			// Include a type annotation in the signature when present.
			if typeNode := n.ChildByFieldName("type"); typeNode != nil {
				sig = name + ": " + x.nodeText(typeNode)
			}
			x.emit(emittedName, "SCOPE.Schema", n, "field", sig)
		}
		x.walkChildren(n, parentClass, cb)
		return
	}

	// Arrow method: emit as SCOPE.Operation subtype=method.
	body := valueNode.ChildByFieldName("body")
	params := valueNode.ChildByFieldName("parameters")
	frame := x.functionParamFrame(params, cb)
	rels := x.extractCallRelationships(body, name, frame)

	// Build a signature that reflects static/async modifiers for readability.
	isStatic := false
	isAsync := false
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "static" {
			isStatic = true
		}
	}
	// async is a child of the arrow_function itself.
	for i := 0; i < int(valueNode.ChildCount()); i++ {
		ch := valueNode.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "async" {
			isAsync = true
		}
	}
	sigParts := ""
	if isStatic {
		sigParts = "static "
	}
	if isAsync {
		sigParts += "async "
	}
	sig := fmt.Sprintf("%s%s = (...) =>", sigParts, name)

	x.emitWithRels(name, "SCOPE.Operation", valueNode, "method", sig, rels)

	// Recurse into the body for nested declarations.
	// Increment funcDepth so nested const declarations inside this arrow
	// class-field method are not emitted as addressable entities (#1748).
	if body != nil {
		x.funcDepth++
		x.walkChildren(body, parentClass, cb)
		x.funcDepth--
	}
}

// handleInterfaceDeclaration handles TypeScript interface declarations.
//
// Emits a SCOPE.Schema entity (subtype="interface") with structured Properties:
//   - "fields"    : comma-separated list of field names declared in the body
//   - "generics"  : comma-separated list of type-parameter names
//   - "extends"   : comma-separated list of base interface names
//
// Also emits one EXTENDS relationship per base interface so the graph
// captures the structural type hierarchy without requiring a resolver pass.
// (issue #1343)
func (x *extractor) handleInterfaceDeclaration(n *sitter.Node) {
	nameNode := n.ChildByFieldName("name")
	name := x.nodeText(nameNode)
	if name == "" {
		return
	}

	props := map[string]string{
		"kind":    "SCOPE.Schema",
		"subtype": "interface",
	}

	// Generic type parameters: <T, U extends Foo>
	var generics []string
	if tpNode := n.ChildByFieldName("type_parameters"); tpNode != nil {
		for i := 0; i < int(tpNode.ChildCount()); i++ {
			ch := tpNode.Child(i)
			if ch == nil {
				continue
			}
			// type_parameter node has a "name" field
			if ch.Type() == "type_parameter" {
				if pn := ch.ChildByFieldName("name"); pn != nil {
					generics = append(generics, x.nodeText(pn))
				}
			}
		}
	}
	if len(generics) > 0 {
		props["generics"] = strings.Join(generics, ", ")
	}

	// Extends clauses: interface Foo extends Bar, Baz
	var extendsList []string
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "extends_type_clause" {
			// Each child of the clause that is a type_identifier or generic_type
			for j := 0; j < int(ch.ChildCount()); j++ {
				item := ch.Child(j)
				if item == nil {
					continue
				}
				switch item.Type() {
				case "type_identifier", "identifier":
					extendsList = append(extendsList, x.nodeText(item))
				case "generic_type":
					// e.g. Base<T> — use only the name part
					if nn := item.ChildByFieldName("name"); nn != nil {
						extendsList = append(extendsList, x.nodeText(nn))
					}
				}
			}
		}
	}
	if len(extendsList) > 0 {
		props["extends"] = strings.Join(extendsList, ", ")
	}

	// Body fields: collect property_signature and method_signature names
	var fields []string
	if body := n.ChildByFieldName("body"); body != nil {
		for i := 0; i < int(body.ChildCount()); i++ {
			member := body.Child(i)
			if member == nil {
				continue
			}
			switch member.Type() {
			case "property_signature", "method_signature", "index_signature":
				if fn := member.ChildByFieldName("name"); fn != nil {
					fields = append(fields, x.nodeText(fn))
				}
			}
		}
	}
	if len(fields) > 0 {
		props["fields"] = strings.Join(fields, ", ")
	}

	// Build EXTENDS edges for each base interface
	var rels []types.RelationshipRecord
	for _, base := range extendsList {
		rels = append(rels, types.RelationshipRecord{
			ToID: base,
			Kind: "EXTENDS",
		})
	}

	sig := fmt.Sprintf("interface %s", name)
	if len(generics) > 0 {
		sig = fmt.Sprintf("interface %s<%s>", name, strings.Join(generics, ", "))
	}

	start, end := lines(n)
	e := types.EntityRecord{
		Name:             name,
		QualifiedName:    x.qualify(name),
		Kind:             "SCOPE.Schema",
		SourceFile:       x.filePath,
		StartLine:        start,
		EndLine:          end,
		Language:         x.language,
		Subtype:          "interface",
		Signature:        sig,
		Properties:       props,
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
		Relationships:    rels,
	}
	e.ID = e.ComputeID()
	x.entities = append(x.entities, e)
}

// handleTypeAliasDeclaration handles TypeScript type aliases: type Foo = ...
//
// Emits a SCOPE.Schema entity (subtype="type_alias") with Properties:
//   - "generics"   : comma-separated type parameter names
//   - "type_body"  : raw text of the right-hand-side type expression
//
// (issue #1343)
func (x *extractor) handleTypeAliasDeclaration(n *sitter.Node) {
	nameNode := n.ChildByFieldName("name")
	name := x.nodeText(nameNode)
	if name == "" {
		return
	}

	props := map[string]string{
		"kind":    "SCOPE.Schema",
		"subtype": "type_alias",
	}

	// Generic type parameters
	var generics []string
	if tpNode := n.ChildByFieldName("type_parameters"); tpNode != nil {
		for i := 0; i < int(tpNode.ChildCount()); i++ {
			ch := tpNode.Child(i)
			if ch == nil {
				continue
			}
			if ch.Type() == "type_parameter" {
				if pn := ch.ChildByFieldName("name"); pn != nil {
					generics = append(generics, x.nodeText(pn))
				}
			}
		}
	}
	if len(generics) > 0 {
		props["generics"] = strings.Join(generics, ", ")
	}

	// RHS type body — capture raw text for union/intersection visibility
	if valueNode := n.ChildByFieldName("value"); valueNode != nil {
		body := x.nodeText(valueNode)
		if body != "" && len(body) <= 512 {
			props["type_body"] = body
		}
	}

	sig := fmt.Sprintf("type %s", name)
	if len(generics) > 0 {
		sig = fmt.Sprintf("type %s<%s>", name, strings.Join(generics, ", "))
	}

	start, end := lines(n)
	e := types.EntityRecord{
		Name:             name,
		QualifiedName:    x.qualify(name),
		Kind:             "SCOPE.Schema",
		SourceFile:       x.filePath,
		StartLine:        start,
		EndLine:          end,
		Language:         x.language,
		Subtype:          "type_alias",
		Signature:        sig,
		Properties:       props,
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
	}
	e.ID = e.ComputeID()
	x.entities = append(x.entities, e)
}

// handleEnumDeclaration handles TypeScript enum declarations: enum Direction { Up, Down }
//
// Emits a SCOPE.Schema entity (subtype="enum") with Properties:
//   - "members" : comma-separated list of enum member names
//
// (issue #1343)
func (x *extractor) handleEnumDeclaration(n *sitter.Node) {
	nameNode := n.ChildByFieldName("name")
	name := x.nodeText(nameNode)
	if name == "" {
		return
	}

	props := map[string]string{
		"kind":    "SCOPE.Schema",
		"subtype": "enum",
	}

	// Collect enum member names from the enum_body
	var members []string
	if body := n.ChildByFieldName("body"); body != nil {
		for i := 0; i < int(body.ChildCount()); i++ {
			member := body.Child(i)
			if member == nil {
				continue
			}
			if member.Type() == "enum_assignment" || member.Type() == "property_identifier" || member.Type() == "identifier" {
				members = append(members, x.nodeText(member))
			} else if member.Type() == "enum_member" {
				// Some grammars wrap in enum_member
				if mn := member.ChildByFieldName("name"); mn != nil {
					members = append(members, x.nodeText(mn))
				} else if mn2 := member.Child(0); mn2 != nil {
					members = append(members, x.nodeText(mn2))
				}
			}
		}
	}
	if len(members) > 0 {
		props["members"] = strings.Join(members, ", ")
	}

	x.emitWithProps(name, "SCOPE.Schema", n, "enum", fmt.Sprintf("enum %s", name), props, nil)
}

// handleVariableDeclaration handles: const/let foo = (...) => {...} or = function(...) {...}
func (x *extractor) handleVariableDeclaration(n *sitter.Node, parentClass string, cb *classBindings) {
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child.Type() == "variable_declarator" {
			x.handleVariableDeclarator(child, parentClass, cb)
		}
	}
}

// handleVariableDeclarator processes a single variable_declarator node.
func (x *extractor) handleVariableDeclarator(n *sitter.Node, parentClass string, cb *classBindings) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	valueNode := n.ChildByFieldName("value")

	// Issue #584 — destructure-rename lift. Patterns like
	//   const { mutate: createAddress } = useCreateAlternateAddress();
	//   const { data, isLoading } = useFooQuery();
	//   const [error, setError] = useState();
	// previously produced no entity (nameNode is object_pattern /
	// array_pattern, not identifier). Downstream files using
	// `createAddress(...)`, `setError(...)` therefore landed in
	// bug-extractor on the resolver. Walk the LHS pattern and emit one
	// entity per local binding so the resolver can bind same-file and
	// cross-file CALLS to a real entity. Mutation-style hooks
	// (useMutation / useCreateX / useDeleteX / ...) classify lifted
	// callables as SCOPE.Operation; everything else as SCOPE.Component
	// (mirrors the wrapper-call vs plain-value split in the default
	// branch below).
	if nameNode.Type() == "object_pattern" || nameNode.Type() == "array_pattern" {
		opLift := isMutationStyleHookCall(x, valueNode)
		// Issue #513 — for state hooks returning [value, setter] tuples
		// (useState, useReducer, useTransition, etc.), emit the setter
		// elements (index ≥ 1 in the array pattern) with subtype
		// "state_setter" so the resolver binds setX() calls to a real
		// entity instead of landing in bug-extractor.
		stateHook := nameNode.Type() == "array_pattern" && isStateHookCall(x, valueNode)
		// Issue #1748 — inside a function body (funcDepth > 0) and not a
		// hook-result binding, tag newly emitted entities as local_scope=true
		// so the serving layer can filter them from archigraph_find results.
		// We still emit the entities so the resolver can bind same-file
		// REFERENCES/CALLS edges.
		before := len(x.entities)
		x.emitDestructuredEntities(nameNode, valueNode, opLift, stateHook, parentClass, cb)
		if x.funcDepth > 0 && !opLift && !stateHook {
			x.tagLocalScope(before)
		}
		if valueNode != nil {
			x.walkChildren(valueNode, parentClass, cb)
		}
		return
	}

	name := x.nodeText(nameNode)
	if name == "" {
		return
	}

	if valueNode == nil {
		return
	}

	switch valueNode.Type() {
	case "arrow_function":
		subtype := "function"
		if parentClass != "" {
			subtype = "method"
		}
		body := valueNode.ChildByFieldName("body")
		params := valueNode.ChildByFieldName("parameters")
		frame := x.functionParamFrame(params, cb)
		rels := x.extractCallRelationships(body, name, frame)
		// Issue #610 — PascalCase arrow components emit RENDERS edges.
		rels = append(rels, x.extractJSXRendersRelationships(body, name)...)
		x.emitWithRels(name, "SCOPE.Operation", valueNode, subtype, fmt.Sprintf("const %s = (...) =>", name), rels)
		if body != nil {
			// Increment funcDepth so nested const declarations inside this
			// arrow body are not emitted as addressable entities (#1748).
			x.funcDepth++
			x.walkChildren(body, parentClass, cb)
			x.funcDepth--
		}

	case "function", "function_expression":
		subtype := "function"
		if parentClass != "" {
			subtype = "method"
		}
		body := valueNode.ChildByFieldName("body")
		params := valueNode.ChildByFieldName("parameters")
		frame := x.functionParamFrame(params, cb)
		rels := x.extractCallRelationships(body, name, frame)
		// Issue #610 — PascalCase function-expression components emit RENDERS edges.
		rels = append(rels, x.extractJSXRendersRelationships(body, name)...)
		x.emitWithRels(name, "SCOPE.Operation", valueNode, subtype, fmt.Sprintf("const %s = function", name), rels)
		if body != nil {
			// Increment funcDepth so nested const declarations inside this
			// function-expression body are not emitted as addressable entities (#1748).
			x.funcDepth++
			x.walkChildren(body, parentClass, cb)
			x.funcDepth--
		}

	default:
		// Issue #562 — PR #522 emitted const_* entities for every `const X = <expr>`
		// shape, producing 2,464+ orphans in client-fixture-c (2,448 of them orphans
		// with no inbound or outbound edges). These entities are synthetic resolver
		// state, not queryable graph structure. Instead of emitting them as standalone
		// entities:
		//
		// Only emit semantically meaningful const declarations:
		//   1. React/MobX/Redux wrapper-call values (forwardRef, memo, observer,
		//      styled, withRouter, connect, createSelector, useCallback, useMemo, etc.)
		//      — classified as SCOPE.Operation so existing function-targeted resolver
		//      paths apply. These ARE semantically significant graph nodes.
		//   2. Context-factory calls (createContext, etc.) — classified as
		//      SCOPE.Component subtype="context" for Provider/Consumer relationships.
		//   3. Type-annotated const declarations (issue #709) — TS `const x: MyType = ...`
		//      carries a type annotation where type-position uses need to resolve back to
		//      the const. Emit as SCOPE.Component so the references walker can attribute
		//      type-position REFERENCES edges to the const entity.
		//
		// Plain values without type annotations (objects, arrays, primitives, instances,
		// alias assignments, call results that aren't wrappers/contexts) are NOT emitted
		// as separate entities. The resolver's structural-ref mechanism (used by same-file
		// and cross-file REFERENCES/IMPORTS edges) resolves these WITHOUT requiring
		// entity materialization — it uses `scope:<kind>:<sub>:<lang>:<file>:<name>`
		// lookups that work on the symbol table alone.
		//
		// We always recurse into the value so nested function/class declarations
		// inside the value (object methods, callbacks, JSX children, etc.) still
		// get emitted.
		if x.isContextFactory(valueNode) {
			// Issue #611 — createContext() and similar context-factory calls
			// return a Context object (with .Provider / .Consumer / .displayName
			// properties), NOT a callable function. Emit as SCOPE.Component with
			// subtype="context" so Provider/Consumer relationships can attach and
			// the entity is not confused with a regular callable.
			before := len(x.entities)
			x.emit(name, "SCOPE.Component", valueNode, "context", fmt.Sprintf("const %s = createContext()", name))
			// Issue #1748 — context factories inside function bodies are
			// unusual/non-addressable; tag as local_scope so find hides them.
			if x.funcDepth > 0 {
				x.tagLocalScope(before)
			}
		} else if x.isFunctionWrapperCall(valueNode) {
			subtype := "function"
			if parentClass != "" {
				subtype = "method"
			}
			// Find an inner arrow/function expression to attribute
			// CALLS to. Fall back to walking the entire value node
			// when the inner shape isn't a literal function (e.g.
			// `forwardRef(someExternalRef)`).
			inner := x.findInnerFunctionBody(valueNode)
			frame := x.functionParamFrame(nil, cb)
			rels := x.extractCallRelationships(valueNode, name, frame)
			_ = inner
			before := len(x.entities)
			x.emitWithRels(name, "SCOPE.Operation", valueNode, subtype, fmt.Sprintf("const %s = <wrapper>", name), rels)
			// Issue #1748 — wrapper calls (forwardRef, memo, etc.) inside a
			// function body are non-addressable; tag as local_scope.
			if x.funcDepth > 0 {
				x.tagLocalScope(before)
			}
		} else if n.ChildByFieldName("type") != nil {
			// Issue #709 — TS `const x: MyType = ...` has a type annotation.
			// We need to emit it as an entity so type-position REFERENCES edges
			// can be attributed to it. This applies only when there's an explicit
			// type annotation on the declarator.
			before := len(x.entities)
			// Has a type annotation; emit as SCOPE.Component
			x.emit(name, "SCOPE.Component", valueNode, "const", fmt.Sprintf("const %s: Type", name))
			// Issue #1748 — type-annotated locals inside function bodies are
			// non-addressable; tag as local_scope.
			if x.funcDepth > 0 {
				x.tagLocalScope(before)
			}
		} else {
			// Issue #1968 — top-level primitive const declarations must be
			// emitted as SCOPE.Schema subtype="constant" so that constants like
			//   export const PROPOSAL_COUNTS_QUERY_KEY = "proposalCounts"
			// appear in the graph with the right kind/name rather than being
			// swallowed into the file entity or incorrectly classified.
			// Only applies at module scope (funcDepth==0) to avoid flooding the
			// graph with transient locals. Object/array/call RHS shapes are NOT
			// classified here — those remain handled by other extractors or
			// deliberately un-emitted to avoid the orphan-count regression (#562).
			// Type-annotated consts are handled by the branch above (issue #709).
			if x.funcDepth == 0 && isPrimitiveLiteralNode(valueNode) {
				props := map[string]string{
					"kind":    "SCOPE.Schema",
					"subtype": "constant",
				}
				if lit := x.primitiveNodeValue(valueNode); lit != "" {
					props["value"] = lit
				}
				sig := fmt.Sprintf("const %s", name)
				x.emitWithProps(name, "SCOPE.Schema", valueNode, "constant", sig, props, nil)
			}
		}
		// Recurse so nested function/class declarations inside the value
		// (object methods, callbacks, JSX children, …) still get emitted.
		x.walkChildren(valueNode, parentClass, cb)
	}
}

// isFunctionWrapperCall returns true when valueNode is a call_expression
// whose callee is one of the well-known React / MobX / Redux / Recoil /
// styled-components / MobX-react function wrappers. We treat the bound
// name as a function (SCOPE.Operation) in that case so the resolver's
// function-targeted edges apply.
//
// The match is intentionally conservative — we only recognise wrappers
// whose semantic IS "this value is a function" (forwardRef returns a
// component, memo returns a component, observer returns a component,
// styled.* returns a component, withRouter wraps a component, connect()
// returns a component, createSlice().reducer is a function, etc.). For
// the dotted shapes (`styled.div`, `createSlice(...).reducer`,
// `Animated.createAnimatedComponent(...)`) we walk down the function
// child to find the leaf identifier.
func (x *extractor) isFunctionWrapperCall(n *sitter.Node) bool {
	if n == nil || n.Type() != "call_expression" {
		return false
	}
	fn := n.ChildByFieldName("function")
	if fn == nil {
		return false
	}
	leaf := ""
	switch fn.Type() {
	case "identifier", "type_identifier":
		leaf = x.nodeText(fn)
	case "member_expression":
		if prop := fn.ChildByFieldName("property"); prop != nil {
			leaf = x.nodeText(prop)
		}
	case "call_expression":
		// e.g. styled(Foo)`...` → recurse on inner call
		return x.isFunctionWrapperCall(fn)
	}
	switch leaf {
	case
		// React
		"forwardRef", "memo", "lazy",
		// MobX-react / MobX
		"observer",
		// styled-components / emotion
		"styled", "css", "keyframes",
		// React Router HOCs
		"withRouter", "withTranslation", "withTheme", "withStyles",
		// Redux / Recoil / Zustand selectors
		"connect", "createSelector", "createStructuredSelector",
		// React Native Animated
		"createAnimatedComponent",
		// HOC-shape utilities
		"compose", "pipe",
		// React Query / TanStack
		"createMutation", "createQuery",
		// antd-style v5 — `const useStyle = createStyles(({css, token}) =>
		// ({...}))` is the canonical antd-style hook-factory shape. The
		// returned value is a hook (function), not a value, so SCOPE.Operation
		// is correct.
		"createStyles",
		// Wave-8 (#567 chain-fix A) — React hook wrappers whose
		// argument is a function/callback. Lifting `const handleX
		// = useCallback((...) => {...}, [...])` and `const value
		// = useMemo(() => ..., [...])` to SCOPE.Operation lets
		// the resolver bind same-file CALLS targets through the
		// existing function-targeted path (same shape as the
		// arrow_function case above). Without this, useCallback/
		// useMemo handlers fall into the default branch and emit
		// SCOPE.Component subtype="const_call", which produces
		// bug-resolver ambiguity for any caller in another file
		// that uses the same handler name. Conservative selection:
		// only hook wrappers whose canonical first arg is a
		// function (useCallback, useMemo). useEffect / useLayoutEffect
		// also take a function but are imperative side-effects,
		// not values bound to a name — the `const cleanup =
		// useEffect(...)` shape is not idiomatic.
		"useCallback", "useMemo":
		return true
	}
	return false
}

// isContextFactory returns true when valueNode is a call_expression whose
// callee is one of the React context-factory functions. These return a Context
// object (with .Provider / .Consumer) — NOT a callable — so the bound const
// should be emitted as SCOPE.Component subtype="context" (issue #611).
//
// Recognised factories: createContext (React), createNamedContext (common
// utility wrapper shape), createOptionalContext (pattern from some React libs).
func (x *extractor) isContextFactory(n *sitter.Node) bool {
	if n == nil || n.Type() != "call_expression" {
		return false
	}
	fn := n.ChildByFieldName("function")
	if fn == nil {
		return false
	}
	var leaf string
	switch fn.Type() {
	case "identifier", "type_identifier":
		leaf = x.nodeText(fn)
	case "member_expression":
		if prop := fn.ChildByFieldName("property"); prop != nil {
			leaf = x.nodeText(prop)
		}
	}
	switch leaf {
	case "createContext", "createNamedContext", "createOptionalContext":
		return true
	}
	return false
}

// findInnerFunctionBody returns the innermost arrow_function /
// function_expression body inside a wrapper-call value, or nil. Used
// to attribute CALLS to the wrapped function rather than the wrapper
// call expression as a whole.
func (x *extractor) findInnerFunctionBody(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if n.Type() == "arrow_function" || n.Type() == "function_expression" || n.Type() == "function" {
		return n.ChildByFieldName("body")
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if b := x.findInnerFunctionBody(n.Child(i)); b != nil {
			return b
		}
	}
	return nil
}

// emitDestructuredEntities walks an object_pattern or array_pattern LHS
// of a variable declarator and emits one entity per local binding name.
// See #584 for the rationale; this is the destructure-rename twin of the
// #522 const-export lift.
//
// Naming rules:
//   - `{ foo }` shorthand_property_identifier_pattern → local name "foo"
//   - `{ foo: bar }` pair_pattern → local name "bar" (the value-side,
//     not the property key)
//   - `{ foo: { y } }` nested pair_pattern → recurse into the value
//     pattern (the local binding is "y", not "foo")
//   - `[a, b, c]` array_pattern → one entity per identifier child
//   - `[, b]` array_pattern with elisions → skipped (no identifier)
//   - `{ ...rest }` rest_pattern → emit the rest binding name
//
// Classification:
//   - opLift=true → SCOPE.Operation (mutation hooks return callables)
//   - opLift=false → SCOPE.Component
//   - stateHook=true (issue #513) → array elements at index≥1 get subtype
//     "state_setter" so setX() calls resolve to a real entity
//
// Issue #1616 — each binding is anchored to ITS OWN identifier node for
// line numbers, not the shared valueNode. Previously every binding in a
// multi-line destructure (e.g. `export const { setToken, getToken, ... } =
// authSlice.actions;`) was pinned to the single line of the RHS member
// expression, producing a cluster of entities all reporting the same
// start_line. Anchoring on the bound identifier attributes each entity to
// its real declaration line.
func (x *extractor) emitDestructuredEntities(pattern, valueNode *sitter.Node, opLift bool, stateHook bool, parentClass string, cb *classBindings) {
	if pattern == nil {
		return
	}
	// fallbackAnchor is used only when a binding node is somehow nil; the
	// per-binding identifier node is preferred for line attribution (#1616).
	fallbackAnchor := valueNode
	if fallbackAnchor == nil {
		fallbackAnchor = pattern
	}
	// anchorFor returns the node to use for line numbers for a given
	// binding: the binding's own identifier node when available, else the
	// shared fallback (#1616).
	anchorFor := func(bind *sitter.Node) *sitter.Node {
		if bind != nil {
			return bind
		}
		return fallbackAnchor
	}
	kind := "SCOPE.Component"
	subtype := "const_destructure"
	sigPrefix := "const"
	if opLift {
		kind = "SCOPE.Operation"
		subtype = "const_destructure_call"
		sigPrefix = "const"
	}
	// Issue #2338 — gate destructure-detail subtypes behind the env var.
	// Default-off: use the plain "const" subtype (same as other const
	// declarations) so the binding entities are still emitted and the
	// resolver can bind same-file REFERENCES / CALLS edges, but the
	// const_destructure / const_destructure_call label is not stamped
	// and the entity count stays low. The kind (Component vs Operation)
	// is preserved so mutation-hook callables remain SCOPE.Operation.
	if !emitDestructureDetailEnabled() {
		subtype = "const"
	}

	var walk func(p *sitter.Node, arrayIdx int)
	walk = func(p *sitter.Node, arrayIdx int) {
		if p == nil {
			return
		}
		switch p.Type() {
		case "object_pattern":
			for i := 0; i < int(p.ChildCount()); i++ {
				walk(p.Child(i), -1)
			}
		case "array_pattern":
			elemIdx := 0
			for i := 0; i < int(p.ChildCount()); i++ {
				ch := p.Child(i)
				if ch == nil {
					continue
				}
				switch ch.Type() {
				case "identifier":
					name := x.nodeText(ch)
					// Issue #513 — setters in state-hook array patterns
					// (index ≥ 1) get subtype "state_setter" so the
					// resolver can bind setX() CALLS to this entity.
					elemSubtype := subtype
					if stateHook && elemIdx >= 1 {
						elemSubtype = "state_setter"
					}
					x.emit(name, kind, anchorFor(ch), elemSubtype, fmt.Sprintf("%s [%s, ...]", sigPrefix, name))
					elemIdx++
				case "object_pattern", "array_pattern":
					walk(ch, elemIdx)
					elemIdx++
				case "rest_pattern":
					if id := firstIdentifierChild(ch); id != nil {
						name := x.nodeText(id)
						x.emit(name, kind, anchorFor(id), subtype, fmt.Sprintf("%s [...%s]", sigPrefix, name))
					}
					elemIdx++
				case "assignment_pattern":
					// e.g. [a = 1] — the binding name is the LHS identifier.
					if left := ch.ChildByFieldName("left"); left != nil {
						if left.Type() == "identifier" {
							name := x.nodeText(left)
							x.emit(name, kind, anchorFor(left), subtype, fmt.Sprintf("%s [%s = ...]", sigPrefix, name))
						} else {
							walk(left, -1)
						}
					}
					elemIdx++
				}
			}
		case "shorthand_property_identifier_pattern":
			name := x.nodeText(p)
			x.emit(name, kind, anchorFor(p), subtype, fmt.Sprintf("%s { %s }", sigPrefix, name))
		case "object_assignment_pattern":
			// `{ foo = defaultValue }` — tree-sitter wraps the
			// shorthand identifier in an object_assignment_pattern when
			// a default value is present. The bound local is the LHS;
			// we ignore the RHS default-value expression for entity
			// emission (#710 — destructure-with-defaults).
			if left := p.ChildByFieldName("left"); left != nil {
				switch left.Type() {
				case "shorthand_property_identifier_pattern", "identifier":
					name := x.nodeText(left)
					x.emit(name, kind, anchorFor(left), subtype, fmt.Sprintf("%s { %s = ... }", sigPrefix, name))
				default:
					walk(left, -1)
				}
			}
		case "pair_pattern":
			// `{ key: value }` — value can be identifier, nested object_pattern,
			// array_pattern, or assignment_pattern (default value).
			value := p.ChildByFieldName("value")
			if value == nil {
				return
			}
			switch value.Type() {
			case "identifier":
				name := x.nodeText(value)
				x.emit(name, kind, anchorFor(value), subtype, fmt.Sprintf("%s { ...: %s }", sigPrefix, name))
			case "object_pattern", "array_pattern":
				walk(value, -1)
			case "assignment_pattern":
				if left := value.ChildByFieldName("left"); left != nil {
					if left.Type() == "identifier" {
						name := x.nodeText(left)
						x.emit(name, kind, anchorFor(left), subtype, fmt.Sprintf("%s { ...: %s = ... }", sigPrefix, name))
					} else {
						walk(left, -1)
					}
				}
			}
		case "rest_pattern":
			if id := firstIdentifierChild(p); id != nil {
				name := x.nodeText(id)
				x.emit(name, kind, anchorFor(id), subtype, fmt.Sprintf("%s { ...%s }", sigPrefix, name))
			}
		case "assignment_pattern":
			if left := p.ChildByFieldName("left"); left != nil {
				if left.Type() == "identifier" {
					name := x.nodeText(left)
					x.emit(name, kind, anchorFor(left), subtype, fmt.Sprintf("%s %s = ...", sigPrefix, name))
				} else {
					walk(left, -1)
				}
			}
		case "identifier":
			name := x.nodeText(p)
			x.emit(name, kind, anchorFor(p), subtype, fmt.Sprintf("%s %s", sigPrefix, name))
		}
	}
	walk(pattern, -1)
}

// firstIdentifierChild returns the first identifier-typed child of n, or nil.
// Used to dig out the bound name from a rest_pattern wrapper.
func firstIdentifierChild(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if ch != nil && ch.Type() == "identifier" {
			return ch
		}
	}
	return nil
}

// isPrimitiveLiteralNode returns true when n is a tree-sitter node whose type
// represents a JS/TS primitive literal (string, number, boolean, null, undefined,
// template literal). Used by handleVariableDeclarator (#1968) to decide whether
// a top-level const declaration should be emitted as SCOPE.Schema/constant.
func isPrimitiveLiteralNode(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	switch n.Type() {
	case "string", "number", "true", "false", "null", "undefined",
		"template_string", "template_literal",
		// TypeScript grammar names for the same literals
		"string_fragment", "escape_sequence":
		return true
	// unary_expression covers negative numbers: -1, -3.14
	case "unary_expression":
		return true
	}
	return false
}

// primitiveNodeValue returns the raw text of a primitive literal node, trimmed
// of surrounding quotes for string nodes so the stored value is the bare string
// content. Returns "" for non-string nodes or when the text is empty.
func (x *extractor) primitiveNodeValue(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	raw := x.nodeText(n)
	if raw == "" {
		return ""
	}
	// Strip surrounding single/double/backtick quotes from string literals.
	if n.Type() == "string" || n.Type() == "template_string" || n.Type() == "template_literal" {
		if len(raw) >= 2 {
			first, last := raw[0], raw[len(raw)-1]
			if (first == '"' && last == '"') ||
				(first == '\'' && last == '\'') ||
				(first == '`' && last == '`') {
				return raw[1 : len(raw)-1]
			}
		}
	}
	return raw
}

// isStateHookCall returns true when the RHS is a call to one of the built-in
// React state hooks that return a [value, setter] tuple. Used by #513 to tag
// array-pattern setters with subtype="state_setter".
func isStateHookCall(x *extractor, valueNode *sitter.Node) bool {
	if valueNode == nil || valueNode.Type() != "call_expression" {
		return false
	}
	fn := valueNode.ChildByFieldName("function")
	if fn == nil {
		return false
	}
	leaf := ""
	switch fn.Type() {
	case "identifier":
		leaf = x.nodeText(fn)
	case "member_expression":
		if prop := fn.ChildByFieldName("property"); prop != nil {
			leaf = x.nodeText(prop)
		}
	}
	return isStateHookName(leaf)
}

// isMutationStyleHookCall returns true when the RHS of a destructuring
// declaration is a call to a React/data-fetching hook whose canonical
// destructured leaf is a callable (a mutation trigger, dispatcher,
// setter, modal opener, etc.). Used by #584 to classify lifted
// destructure-rename bindings as SCOPE.Operation rather than
// SCOPE.Component.
//
// We match on the *callee identifier* (or member-expression trailing
// property), gated to call_expression values. The set is the union of:
//
//   - React core: useState, useReducer
//   - React Query / TanStack: useMutation + every `useXxxMutation`
//     convention (matched via a "Mutation" suffix)
//   - SWR mutate hooks: useSWRMutation
//   - React Hook Form: useForm
//   - antd v5 hooks that return callable triples (useModal, useMessage,
//     useNotification, useApp)
//   - Generic convention: any identifier matching
//     `^use(Create|Update|Delete|Patch|Post|Put|Remove|Add|Toggle|Open|Close|Save|Submit)[A-Z]`
//     which covers the dominant naming pattern for custom mutation
//     hooks observed in real client codebases (e.g.
//     useCreateAlternateAddress, useDeleteUser, useToggleFavorite).
//
// When this returns true, ALL leaves of the destructure pattern are
// lifted as SCOPE.Operation — the broader bias is intentional. Real
// data values like `{ data, isLoading }` from useQuery still get
// classified as Operation under this scheme, but the cost is low: the
// resolver only consults Operation entities for CALLS edges, so a
// non-callable bound name produces no false positives, only a slightly
// wider candidate set for legitimate callable leaves like `mutate`,
// `refetch`, `setError`.
func isMutationStyleHookCall(x *extractor, valueNode *sitter.Node) bool {
	if valueNode == nil || valueNode.Type() != "call_expression" {
		return false
	}
	fn := valueNode.ChildByFieldName("function")
	if fn == nil {
		return false
	}
	leaf := ""
	switch fn.Type() {
	case "identifier":
		leaf = x.nodeText(fn)
	case "member_expression":
		if prop := fn.ChildByFieldName("property"); prop != nil {
			leaf = x.nodeText(prop)
		}
	}
	if leaf == "" {
		return false
	}
	return isMutationStyleHookName(leaf)
}

// isStateHookName returns true when the hook name is one of the built-in
// React hooks that return a [value, setter] tuple. Issue #513 — setters from
// these hooks (e.g. setIsOpen, setActive) must be lifted as SCOPE.Operation
// with subtype="state_setter" so the resolver can bind same-file CALLS edges
// instead of routing them to bug-extractor.
func isStateHookName(leaf string) bool {
	switch leaf {
	case "useState", "useReducer", "useTransition", "useOptimistic",
		"useActionState", "useFormState":
		return true
	}
	return false
}

// isMutationStyleHookName encodes the name-shape rule documented on
// isMutationStyleHookCall. Pure function, exported via test seam.
func isMutationStyleHookName(leaf string) bool {
	// State hooks are a subset of mutation-style — they return callables.
	if isStateHookName(leaf) {
		return true
	}
	switch leaf {
	case
		"useMutation", "useSWRMutation",
		"useForm",
		"useModal", "useMessage", "useNotification", "useApp",
		"useQuery", "useInfiniteQuery", "useSWR", "useSWRImmutable",
		"useDispatch", "useNavigate", "useLocation", "useParams",
		"useDisclosure":
		return true
	}
	// `useXxxMutation` convention (React Query custom mutation hooks).
	if strings.HasPrefix(leaf, "use") && strings.HasSuffix(leaf, "Mutation") && len(leaf) > len("use")+len("Mutation") {
		return true
	}
	// `use{Create|Update|Delete|Patch|Post|Put|Remove|Add|Toggle|Open|Close|Save|Submit}{Xxx}`
	// custom mutation-hook naming convention.
	if strings.HasPrefix(leaf, "use") && len(leaf) > 3 {
		rest := leaf[3:]
		for _, verb := range []string{
			"Create", "Update", "Delete", "Patch", "Post", "Put",
			"Remove", "Add", "Toggle", "Open", "Close", "Save", "Submit",
			"Fetch", "Send", "Upload", "Download",
		} {
			if strings.HasPrefix(rest, verb) && len(rest) > len(verb) {
				next := rest[len(verb)]
				if next >= 'A' && next <= 'Z' {
					return true
				}
			}
		}
	}
	return false
}

// extractCallRelationships returns one CALLS RelationshipRecord per unique
// call_expression / new_expression descendant of body. The target name is
// computed from the function child of the call:
//
//	identifier               → bare name      (e.g. "foo")
//	member_expression a.b.c  → trailing prop  (e.g. "c"), or — when the
//	                           receiver chain types via class fields /
//	                           function parameters AND the type was
//	                           imported from a relative path — a Format A
//	                           structural-ref keyed on the imported file
//	                           (issue #421).
//	parenthesized_expression → inner target   (best-effort)
//
// FromID is left empty so buildDocument substitutes the caller's entity ID
// at emit time. ToID is either a bare callee name OR a structural-ref
// stub. Self-recursion is dropped on the BARE form only — a structural-
// ref to the same name in another file is a legitimate cross-file CALLS
// edge and must survive.
//
// frame carries the receiver-type bindings visible in the caller's scope:
// merged class fields (from the enclosing class body) + the caller's own
// typed parameters. nil means "no typed receiver lookup possible".
func (x *extractor) extractCallRelationships(body *sitter.Node, callerName string, frame *classBindings) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	calls := findAllNodes(body, "call_expression", "new_expression")
	if len(calls) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(calls))
	rels := make([]types.RelationshipRecord, 0, len(calls))
	for _, call := range calls {
		target := x.callTarget(call, frame)
		if target == "" || target == "require" {
			continue
		}
		// Self-recursion drop applies to the bare leaf only. A
		// structural-ref dotted target whose leaf matches callerName
		// would still be a legitimate cross-file binding (the leaf
		// happens to share a name across files), so we keep it.
		if !strings.Contains(target, ":") && target == callerName {
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		// Issues #514 / #517 — stamp receiver_package when the call's
		// receiver was bound to an Express-family or NestJS application
		// object. The resolver checks this property before
		// classifyDispositionLang to route the edge to DispositionDynamic.
		rel := types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
		}
		if pkg := x.frameworkDSL.receiverPackageForCall(x, call); pkg != "" {
			rel.Properties = map[string]string{
				PropReceiverPackage: pkg,
			}
		}
		rels = append(rels, rel)
	}
	return rels
}

// builtinMethodNames is the set of JS/TS built-in prototype methods on
// Array, String, Object, Promise, Map, Set, and Number that the extractor
// must NOT emit as user-defined CALLS targets (Issue #1616). A bare
// `<expr>.map(...)` whose receiver did not type-resolve to a user class is
// almost always a built-in iteration/transform, not a call into the user's
// own code; emitting it produces unresolvable bug-extractor edges and
// spurious SCOPE.Process flow steps. The list is deliberately limited to
// unambiguous, high-frequency built-ins.
var builtinMethodNames = map[string]bool{
	// Array iteration / transformation
	"map": true, "filter": true, "reduce": true, "reduceRight": true,
	"forEach": true, "some": true, "every": true, "find": true,
	"findIndex": true, "findLast": true, "findLastIndex": true,
	"flat": true, "flatMap": true, "sort": true, "reverse": true,
	"fill": true, "copyWithin": true, "entries": true, "keys": true,
	"values": true, "indexOf": true, "lastIndexOf": true,
	"includes": true, "push": true, "pop": true, "shift": true,
	"unshift": true, "splice": true,
	// Array + String shared
	"slice": true, "concat": true, "join": true,
	// String
	"trim": true, "trimStart": true, "trimEnd": true, "split": true,
	"replace": true, "replaceAll": true, "toLowerCase": true,
	"toUpperCase": true, "padStart": true, "padEnd": true,
	"startsWith": true, "endsWith": true, "charAt": true,
	"charCodeAt": true, "codePointAt": true, "substring": true,
	"substr": true, "repeat": true, "match": true, "matchAll": true,
	"search": true, "normalize": true, "localeCompare": true,
	// Promise instance
	"then": true, "catch": true, "finally": true,
	// Number / common formatting
	"toFixed": true, "toString": true, "toPrecision": true,
	"valueOf": true, "hasOwnProperty": true,
}

// isBuiltinMethodName reports whether method is a JS/TS built-in prototype
// method that should not be modeled as a user-defined CALLS target (#1616).
func isBuiltinMethodName(method string) bool {
	return builtinMethodNames[method]
}

// callTarget resolves the callee name from a call_expression / new_expression.
// Returns the trailing identifier of the function expression, or "" when the
// callee is an unsupported expression form (numeric literal, complex
// destructuring, etc.).
//
// Issue #421 — when the function child is a member_expression of the form
// `<receiver>.<method>` AND the receiver types via the supplied frame to
// a relatively-imported class, callTarget returns a Format A structural-
// ref ("scope:operation:method:<lang>:<resolved_file>:<method>") instead
// of the bare trailing identifier. This lets the resolver bind the call
// to the imported class's method without going through bare-name lookup.
func (x *extractor) callTarget(call *sitter.Node, frame *classBindings) string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		// new_expression uses "constructor" field.
		fn = call.ChildByFieldName("constructor")
	}
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier", "type_identifier", "property_identifier":
		name := x.nodeText(fn)
		// Refs #44 — bare named-import shape: `import { join } from
		// "path"` then `join(...)`. The leaf identifier binds to a
		// Node.js stdlib import; route the call to the matching
		// `ext:node:<module>` placeholder via the cross-language
		// `:external:` synth path. Miss falls through to bare name.
		if id := x.classifyBareNodeStdlibCall(name); id != "" {
			return id
		}
		// Issue #44 (TS/JS slice) — hook-variable rewrite: if `name` is a
		// local variable assigned from a React/framework hook call (e.g.
		// `const navigate = useNavigate()`), rewrite to "ext:<module>" so
		// the external synthesiser handles it correctly rather than
		// producing an unresolvable bare-name CALLS edge (bug-extractor).
		if mod, ok := x.hookVarToModule[name]; ok && mod != "" {
			return "ext:" + mod
		}
		return name
	case "member_expression":
		prop := fn.ChildByFieldName("property")
		if prop == nil {
			return ""
		}
		method := x.nodeText(prop)
		// Issue #421 — try receiver typing first. The lookup walks
		// `<recv>.<method>` and `this.<recv>.<method>` shapes; on a
		// hit it returns the structural-ref keyed on the imported
		// source file. On a miss we fall through to the bare method
		// name (current behaviour).
		if id := x.receiverTypedTarget(fn, method, frame); id != "" {
			return id
		}
		// Refs #44 — Node.js stdlib namespace shape: `import * as
		// path from "path"` (or `import fs from "node:fs"`) followed
		// by `path.join(...)` / `fs.readFileSync(...)`. The receiver
		// binds to a Node stdlib import spec; route the call to the
		// matching `ext:node:<module>` placeholder. Miss falls through
		// to the bare method name (existing behaviour preserved).
		if id := x.receiverNodeStdlibTarget(fn, method); id != "" {
			return id
		}
		// Issue #1616 — drop CALLS edges to JS/TS built-in array, string,
		// Object, and Promise prototype methods (.map/.filter/.reduce/
		// .forEach/.some/.every/.find/.join/.split/.trim/.slice/...). These
		// are language built-ins, not user-defined operations: emitting a
		// bare-name CALLS edge to them produces unresolvable targets that
		// the resolver dumps into bug-extractor AND, downstream, causes the
		// process-flow builder to synthesise spurious SCOPE.Process steps
		// (e.g. `Login → map`, `Login → trim`). We only filter when the
		// receiver did NOT type-resolve to a user class and is NOT a Node
		// stdlib namespace (both handled above), so a genuine user method
		// that happens to share a built-in name (resolved via typing) is
		// preserved.
		if isBuiltinMethodName(method) {
			return ""
		}
		return method
	case "parenthesized_expression":
		for i := 0; i < int(fn.ChildCount()); i++ {
			ch := fn.Child(i)
			if ch.Type() == "identifier" {
				return x.nodeText(ch)
			}
			if ch.Type() == "member_expression" {
				if p := ch.ChildByFieldName("property"); p != nil {
					return x.nodeText(p)
				}
			}
		}
	}
	return ""
}

// extractJSXRendersRelationships scans body for JSX elements whose tag name
// begins with an uppercase ASCII letter (= React component convention) and
// emits one RENDERS RelationshipRecord per unique component. Issue #610.
//
// Only emits when callerName is itself PascalCase (isComponentName returns
// true) so hook functions and plain utilities don't pick up spurious RENDERS
// edges from JSX fragments inside non-component functions.
//
// The ToID is the bare PascalCase component name by default. When the tag is
// identified in importByLocal as an external (npm) import (resolvedFile==""),
// the ToID is set to "ext:<module>" so the external synthesiser produces a
// well-formed placeholder and the resolver classifies it as ExternalKnown /
// ExternalUnknown instead of BugExtractor. Issue #44 TS/JS slice.
//
// The cross-file resolver (or the react_props cross-extractor) will bind
// local tags to the declaring entity. Self-renders (caller == tag name) are
// skipped.
func (x *extractor) extractJSXRendersRelationships(body *sitter.Node, callerName string) []types.RelationshipRecord {
	if body == nil || !isComponentName(callerName) {
		return nil
	}
	jsxNodes := findAllNodes(body,
		"jsx_opening_element",
		"jsx_self_closing_element",
	)
	if len(jsxNodes) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(jsxNodes))
	var rels []types.RelationshipRecord
	for _, jx := range jsxNodes {
		nameNode := jx.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		tag := x.nodeText(nameNode)
		// Only PascalCase tags — HTML intrinsics (div, span, …) start lowercase.
		if !isComponentName(tag) {
			continue
		}
		// Skip self-renders.
		if tag == callerName {
			continue
		}
		// Issue #44 (TS/JS resolver slice) — handle member-expression JSX
		// tags of the form "Object.Property" (e.g. AuthContext.Provider,
		// React.Fragment, styled.div).
		//
		// Strategy:
		//   1. Split into objectPart + propertyPart.
		//   2. If propertyPart is "Provider" or "Consumer" — React context
		//      API — use "ext:react" as the toID directly and skip the
		//      normal import lookup. These are React runtime internals, not
		//      separate graph entities.
		//   3. For other member expressions (e.g. styled.Button), take the
		//      property as the effective tag for the import lookup below.
		//   4. If the derived property is not PascalCase (e.g. styled.div),
		//      skip the edge entirely — it's an HTML intrinsic.
		var memberObjectPart string
		if strings.Contains(tag, ".") {
			dot := strings.LastIndex(tag, ".")
			memberObjectPart = tag[:dot]
			tag = tag[dot+1:]
			if !isComponentName(tag) {
				continue
			}
		}
		if seen[tag] {
			continue
		}
		seen[tag] = true

		// Issue #44 (TS/JS resolver slice) — determine toID:
		//   a) React context .Provider / .Consumer patterns → ext:react.
		//   b) External (npm) PascalCase imports → ext:<module>.
		//   c) Everything else keeps the bare component name for the
		//      cross-file resolver to bind at pass2.
		toID := tag
		if memberObjectPart != "" && (tag == "Provider" || tag == "Consumer") {
			// AuthContext.Provider, SomeCtx.Consumer, etc. — always React API.
			toID = "ext:react"
		} else if x.importByLocal != nil {
			if b := x.importByLocal[tag]; b != nil && b.resolvedFile == "" && b.sourceModule != "" {
				toID = "ext:" + b.sourceModule
			}
		}

		rels = append(rels, types.RelationshipRecord{
			ToID: toID,
			Kind: "RENDERS",
		})
	}
	return rels
}

// buildHookVarToModule scans the file AST for variable declarations of the
// form `const localName = hookCall()` where `hookCall` resolves to an
// external (npm) import via importByLocal. It returns a map from localName
// to the npm package the hook was imported from.
//
// This covers the common React pattern:
//
//	const navigate = useNavigate();   // useNavigate ← react-router-dom
//	const dispatch = useDispatch();   // useDispatch ← react-redux
//
// Without this map, extractCallRelationships emits `navigate(...)` as a
// CALLS edge with target "navigate" — a bare local variable with no graph
// entity, which lands in bug-extractor. With the map, callTarget rewrites
// the target to "ext:<module>" so the external synthesiser handles it
// correctly. Issue #44 (TS/JS resolver slice).
func (x *extractor) buildHookVarToModule(root *sitter.Node) map[string]string {
	if root == nil || x.importByLocal == nil {
		return nil
	}
	result := make(map[string]string)
	stack := make([]*sitter.Node, 0, 64)
	stack = append(stack, root)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		if n.Type() == "variable_declarator" {
			// Pattern: `localName = hookCall()` or `localName = hookCall(args)`
			// The LHS must be a simple identifier and the RHS a call_expression
			// whose function resolves to an external import.
			nameNode := n.ChildByFieldName("name")
			valNode := n.ChildByFieldName("value")
			if nameNode != nil && valNode != nil && valNode.Type() == "call_expression" {
				localName := x.nodeText(nameNode)
				// Only handle simple identifiers on the LHS (not destructures).
				if localName != "" && !strings.ContainsAny(localName, "{}[].,") {
					fnNode := valNode.ChildByFieldName("function")
					if fnNode != nil {
						hookName := x.nodeText(fnNode)
						if b, ok := x.importByLocal[hookName]; ok && b != nil && b.resolvedFile == "" && b.sourceModule != "" {
							result[localName] = b.sourceModule
						}
					}
				}
			}
		}
		count := int(n.ChildCount())
		for i := count - 1; i >= 0; i-- {
			stack = append(stack, n.Child(i))
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// isComponentName returns true when name starts with an ASCII uppercase
// letter — the React convention for function component identifiers.
func isComponentName(name string) bool {
	if name == "" {
		return false
	}
	c := name[0]
	return c >= 'A' && c <= 'Z'
}

// findAllNodes returns every descendant of root whose Type() is in kinds.
// Iterative to stay safe on deeply-nested trees.
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

// collectImports scans the tree for ES6 import statements and CommonJS
// require() calls, emitting a SCOPE.Component entity per unique module.
//
// Issue #421 — IMPORTS edges now carry the per-binding property
// contract Python (#93) and Java (#120) emit so the cross-file
// resolver pre-pass can build a per-file binding table:
//
//	Properties["local_name"]    — identifier introduced into the file
//	Properties["source_module"] — canonical dotted module path
//	Properties["imported_name"] — original symbol name (pre-alias)
//	Properties["wildcard"]      — "1" for `import * as ns from "..."`
//
// One IMPORTS edge per BINDING is emitted (so `import { A, B } from
// "mod"` produces two edges); the parent SCOPE.Component entity is
// shared across bindings of the same module so the existing dedup
// shape is preserved.
func (x *extractor) collectImports(root *sitter.Node) {
	seen := make(map[string]bool)
	// Group import bindings by module spec so we can attach all
	// bindings as separate IMPORTS edges on a single import entity.
	bindingsByModule := map[string][]*importBinding{}
	for i := range x.imports {
		b := &x.imports[i]
		bindingsByModule[b.importPath] = append(bindingsByModule[b.importPath], b)
	}
	x.collectImportsNode(root, seen, bindingsByModule)
}

func (x *extractor) collectImportsNode(n *sitter.Node, seen map[string]bool, bindingsByModule map[string][]*importBinding) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "import_statement":
		// ES6: import foo from "module" / import { x } from "module"
		// The string node is a direct child.
		count := int(n.ChildCount())
		for i := 0; i < count; i++ {
			child := n.Child(i)
			if child.Type() == "string" {
				raw := x.nodeText(child)
				module := strings.Trim(raw, `"'`+"`")
				if module != "" && !seen[module] {
					seen[module] = true
					x.emitImport(module, n, bindingsByModule[module])
				}
			}
		}
		return // do not recurse

	case "call_expression":
		// CommonJS: require("module")
		funcNode := n.ChildByFieldName("function")
		if funcNode != nil && x.nodeText(funcNode) == "require" {
			argsNode := n.ChildByFieldName("arguments")
			if argsNode != nil {
				count := int(argsNode.ChildCount())
				for i := 0; i < count; i++ {
					arg := argsNode.Child(i)
					if arg.Type() == "string" {
						raw := x.nodeText(arg)
						module := strings.Trim(raw, `"'`+"`")
						if module != "" && !seen[module] {
							seen[module] = true
							x.emitImport(module, n, nil)
						}
						break
					}
				}
			}
		}
	}

	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		x.collectImportsNode(n.Child(i), seen, bindingsByModule)
	}
}

// emitImport emits a SCOPE.Component entity for an imported module.
//
// Issue #421 — when the module's import_statement introduced one or
// more named/default/namespace bindings, every binding becomes its own
// IMPORTS RelationshipRecord on the entity, carrying the property
// contract the cross-file resolver pre-pass consumes. Side-effect-only
// imports (`import "./polyfills";`) and CommonJS require() calls
// without destructuring fall back to a single IMPORTS edge with no
// per-binding properties so existing downstream consumers still see
// at least one edge per module.
func (x *extractor) emitImport(module string, n *sitter.Node, bindings []*importBinding) {
	// Use the full module path as the entity name for parity with Python indexer.
	start, end := lines(n)
	// Issue #570 — FromID is the importing file's path. The extractor
	// also emits a file-level SCOPE.Component (subtype="file") entity
	// with Name == file path at the top of Extract; the resolver's
	// byName index then rewrites this path-shaped FromID to the file
	// entity's stamped hex ID, and the cross-repo import linker
	// (#566) can map the edge back to its originating repo.
	fromID := x.filePath
	rels := make([]types.RelationshipRecord, 0, max1(len(bindings)))
	if len(bindings) == 0 {
		rels = append(rels, types.RelationshipRecord{
			FromID: fromID,
			ToID:   module,
			Kind:   "IMPORTS",
		})
	} else {
		for _, b := range bindings {
			props := map[string]string{
				"local_name":    b.localName,
				"source_module": b.sourceModule,
				"imported_name": b.importedName,
				"import_path":   b.importPath,
			}
			if b.wildcard {
				props["wildcard"] = "1"
			}
			if b.resolvedFile != "" {
				props["resolved_file"] = b.resolvedFile
			}
			// Issue #505 — for ALIAS-resolved imports, switch the
			// IMPORTS ToID from the raw alias spec (`@/src/store/foo`,
			// which the resolver can't bind because it lacks the
			// dotted-module shape ResolveDottedImportTarget requires)
			// to the dotted-module + leaf form
			// (`src.store.foo.<importedName>`). The resolver splits on
			// the last dot and looks up (module, leaf) against the
			// per-module reverse index. Plain relative imports keep the
			// legacy raw-spec ToID so pre-#505 disposition shapes on
			// the existing TS/JS corpora (express, nestjs, etc.) are
			// preserved bit-for-bit.
			toID := module
			if b.aliasResolved && b.sourceModule != "" && b.importedName != "" {
				toID = b.sourceModule + "." + b.importedName
			}
			rels = append(rels, types.RelationshipRecord{
				FromID:     fromID,
				ToID:       toID,
				Kind:       "IMPORTS",
				Properties: props,
			})
		}
	}
	e := types.EntityRecord{
		Name:       module,
		Kind:       "SCOPE.Component",
		SourceFile: x.filePath,
		StartLine:  start,
		EndLine:    end,
		Language:   x.language,
		Subtype:    "import",
		Properties: map[string]string{
			"kind":     "SCOPE.Component",
			"subtype":  "import",
			"module":   module,
			"is_local": boolStr(strings.HasPrefix(module, ".")),
		},
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
		Relationships:    rels,
	}
	e.ID = e.ComputeID()
	x.entities = append(x.entities, e)
}

// max1 returns the larger of n and 1. Used to pre-size relationship
// slices without ever zero-allocating when n is 0.
func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// boolStr returns "true" or "false" as a string.
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
