// Package java implements the tree-sitter–based extractor for Java source files.
//
// Extracted entities:
//   - class_declaration       → Kind="SCOPE.Component", Subtype="class"
//   - interface_declaration   → Kind="SCOPE.Component", Subtype="interface"
//   - method_declaration      → Kind="SCOPE.Operation", Subtype="method"
//   - constructor_declaration → Kind="SCOPE.Operation", Subtype="constructor"
//   - import_declaration      → IMPORTS relationship on file entity (issue #681)
//
// Issue #120 — cross-file receiver binding. method_invocation nodes
// whose receiver (object) is a field/parameter of a known type emit
// CALLS edges with target "<ReceiverType>.<method>" instead of the
// bare leaf name. The receiver-type lookup walks:
//
//  1. Field declarations on the enclosing class (covers the dominant
//     Spring DI shape: `@Autowired private OwnerRepository owners;`
//     followed by `owners.findById(...)`).
//  2. Method parameters of the enclosing operation.
//  3. PascalCase static-call shape: `Helpers.compute()` → keep dotted
//     even without a direct binding so the resolver's byKind/byName
//     index can pick it up cross-file (issue #65 emits methods as
//     "<EnclosingType>.<member>", so the dotted target binds).
//
// IMPORTS edges now carry the same Properties contract Python emits
// (issue #93) — local_name / source_module / imported_name / wildcard
// — so the cross-file resolver pre-pass (internal/resolve/imports.go)
// can build a per-file binding table for Java just like Python.
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package java

import (
	"context"
	"strconv"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/txscope"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("java", &Extractor{})
}

// javaMethodTxStamp inspects a method_declaration's `modifiers` child (where
// tree-sitter Java places annotations) for @Transactional and returns the
// resulting transaction stamp. Scanning only the modifiers — not the whole
// method body — avoids false positives from an @Transactional token appearing
// inside a string literal or comment in the body. Returns a zero stamp when no
// @Transactional annotation is present.
func javaMethodTxStamp(methodNode *sitter.Node, src []byte) txscope.Stamp {
	mods := methodModifiersText(methodNode, src)
	if mods == "" {
		return txscope.Stamp{}
	}
	return txscope.DetectJava(mods)
}

// methodModifiersText returns the source text of a declaration's `modifiers`
// child (annotations + visibility keywords), or "" when absent.
func methodModifiersText(node *sitter.Node, src []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch != nil && ch.Type() == "modifiers" {
			return nodeText(ch, src)
		}
	}
	return ""
}

// stampClassLevelTransactional walks every class/interface/enum/record body
// whose own `modifiers` carry @Transactional, and stamps every enclosed method
// operation entity that is not already transactional with the class-level
// stamp. This realises Spring's class-level @Transactional → all-methods
// semantics. A method with its own @Transactional (already stamped during the
// primary walk) keeps its own — more specific — propagation/isolation.
func stampClassLevelTransactional(root *sitter.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil {
		return
	}
	walkClassTx(root, "", file, entities)
}

func walkClassTx(n *sitter.Node, pkgQualifier string, file extractor.FileInput, entities *[]types.EntityRecord) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "class_declaration", "interface_declaration", "enum_declaration", "record_declaration":
		className := childFieldText(n, "name", file.Content)
		classStamp := txscope.DetectJava(methodModifiersText(n, file.Content))
		if classStamp.Transactional && className != "" {
			if body := n.ChildByFieldName("body"); body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					ch := body.Child(i)
					if ch == nil || ch.Type() != "method_declaration" {
						continue
					}
					methodName := childFieldText(ch, "name", file.Content)
					if methodName == "" {
						continue
					}
					op := findJavaOp(*entities, file.Path, className+"."+methodName)
					if op == nil || op.Properties["transactional"] == "true" {
						// Already stamped by its own method-level annotation, or
						// not found — leave the more-specific stamp intact.
						continue
					}
					op.Properties = classStamp.Apply(op.Properties)
				}
			}
		}
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		walkClassTx(n.Child(i), pkgQualifier, file, entities)
	}
}

// findJavaOp returns the SCOPE.Operation entity with the given file + emitted
// (Class.method) name, or nil.
func findJavaOp(entities []types.EntityRecord, filePath, emittedName string) *types.EntityRecord {
	for i := range entities {
		e := &entities[i]
		if e.Kind == "SCOPE.Operation" && e.SourceFile == filePath && e.Name == emittedName {
			return e
		}
	}
	return nil
}

// Extractor implements extractor.Extractor for Java.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "java" }

// Extract walks the tree-sitter CST and returns entity records for the Java file.
//
// OTel span "extractor.java" carries attributes: file, entity_count,
// error_pattern_count.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor.java")
	_, span := tracer.Start(ctx, "extractor.java")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if file.Tree == nil || len(file.Content) == 0 {
		span.SetAttributes(
			attribute.Int("entity_count", 0),
			attribute.Int("error_pattern_count", 0),
		)
		return nil, nil
	}

	var entities []types.EntityRecord
	// Issue #577 — emit file-level SCOPE.Component (subtype="file") so the
	// cross-repo import linker (#566) can map IMPORTS edges back to the
	// originating repo via the resolver's byName index. Generalises the
	// JS/TS fix from #570/#575.
	entities = append(entities, extractor.FileEntity(file))
	root := file.Tree.RootNode()
	imports := collectImportNames(root, file.Content)
	// Issue #1917 — extract the file's package declaration so QualifiedName
	// can be set to "<package>.<ClassName>" and "<package>.<Class>.<method>".
	pkgName := collectPackageName(root, file.Content)
	walk(root, file, "", nil, imports, pkgName, &entities)

	// #3628 — class-level @Transactional propagation. A class annotated
	// @Transactional makes all of its (public) methods transactional in Spring;
	// stamp each method operation that was not already stamped by its own
	// method-level annotation. Runs after walk so all method operations exist.
	func() {
		defer func() { _ = recover() }()
		stampClassLevelTransactional(root, file, &entities)
	}()

	// Issue #681 — attach IMPORTS relationships directly to the file-level
	// entity instead of emitting a separate SCOPE.Component placeholder
	// per import_declaration. Placeholder entities were dangling (zero
	// inbound edges) because REFERENCES edges point at the real external
	// entity (ext:java:List), not the placeholder. Eliminating the
	// placeholder entities drops ~1205 orphans on client-fixture-d
	// (-25 to -35pp on the orphan rate).
	//
	// entities[0] is always the file entity (appended first above).
	attachImportRelationships(root, file, &entities[0])

	// Issue #818 — PanacheQuery / PanacheUpdate DSL builder method synthesis.
	// Emit synthetic interface + method entities for every DSL method on the
	// PanacheQuery / PanacheUpdate / ReactivePanacheQuery interfaces so that
	// chained calls like `q.list()`, `q.page(0,20)`, `q.count()`, etc. resolve
	// to a synthesized entity rather than landing as bug-extractor unresolved
	// edges. Called once per FILE (not per class) to avoid per-class duplication;
	// the indexer dedup layer collapses identical Name+Kind entries across files.
	rawImportsForDSL := collectRawImports(file.Content)
	entities = append(entities, synthesizePanacheDSLEntities(file.Path, rawImportsForDSL)...)

	// Track A (analog of #641/#650 for Java) — REFERENCES-edge emission.
	// Runs after every primary-pass entity is in place so the file-
	// scope symbol table covers methods, classes, fields, and import
	// bindings. Failures here recover internally to partial results —
	// never aborts primary output.
	func() {
		defer func() { _ = recover() }()
		emitReferences(root, file, &entities)
	}()

	// Config-consumption topology (issue #3641, epic #3625) —
	// DEPENDS_ON_CONFIG edges from Spring/MicroProfile beans that read a
	// config key (@Value("${...}"), @ConfigurationProperties, env.getProperty)
	// to a shared config-key entity, so config:<key>'s inbound edges form the
	// config-change blast radius. Runs after primary entities are in place so
	// edges attach to the right enclosing class/method.
	func() {
		defer func() { _ = recover() }()
		emitConfigConsumerEdges(root, file, &entities)
	}()

	// Error-flow topology (epic #3628) — THROWS / CATCHES edges from
	// methods/constructors to a shared SCOPE.ExceptionType node for
	// `throw new X()`, the `throws` clause, and typed/multi `catch (X | Y e)`
	// shapes. Java's checked-exception model makes these highly reliable.
	func() {
		defer func() { _ = recover() }()
		emitExceptionFlowEdges(root, file, &entities)
	}()

	// View-layer topology (epic #3628) — RENDERS edges from Spring MVC
	// controller methods that return a static view name to a shared
	// SCOPE.Template node. Honest REST-vs-MVC boundary: @RestController /
	// @ResponseBody methods are skipped (String return is a body, not a view),
	// and dynamic view names are dropped.
	func() {
		defer func() { _ = recover() }()
		emitTemplateRenderEdges(root, file, &entities)
	}()

	// Track B (analog of #642/#650 for Java) — IMPORTS ToID rewrite.
	// Rewrites IMPORTS edges whose source_module's longest dotted
	// prefix matches a known external JVM package to an
	// `ext:<prefix>[:<name>]` ToID so the resolver's external-
	// disposition gate classifies them ExternalKnown directly.
	// In-tree imports are untouched — the existing
	// ResolveDottedImportTarget path binds them via source_module /
	// imported_name properties.
	resolveImportToIDs(entities)

	// Issue #1994 — final safety net for line-bound emission. Every entity
	// MUST carry non-zero start_line + end_line so the docgen source_window
	// helper has a usable anchor. Class-scoped synthesizers (Lombok, Panache)
	// are stamped per-class above; this pass catches any file-level
	// synthesized entity (Panache DSL interfaces, top-level helpers) that
	// the per-class pass cannot reach. We use line 1 / file end as a
	// conservative fallback — the bundle-side by-name fallback (#1987) will
	// still rebind to the real source location when needed, but a non-zero
	// sentinel keeps downstream rendering safe.
	fileEnd := int(root.EndPoint().Row) + 1
	if fileEnd < 1 {
		fileEnd = 1
	}
	for i := range entities {
		if entities[i].StartLine == 0 {
			entities[i].StartLine = 1
		}
		if entities[i].EndLine == 0 {
			entities[i].EndLine = fileEnd
		}
	}

	span.SetAttributes(
		attribute.Int("entity_count", len(entities)),
	)
	// Issue #90 — tag every embedded relationship with language="java" so
	// the resolver routes to the JVM dynamic-pattern catalog.
	extractor.TagRelationshipsLanguage(entities, "java")
	extractor.TagEntitiesLanguage(entities, "java")
	return entities, nil
}

// walk performs a depth-first traversal of the CST, collecting entities.
//
// PORT-2-FIX-2-ALL (#41): class/interface declarations attach a CONTAINS
// edge per method/constructor declared inside the body, and every method
// or constructor body is scanned for method_invocation / object_creation
// nodes that yield CALLS edges with stub `to_id` (resolver rewrites
// cross-file refs in pass 5).
//
// Issue #65: methods/constructors declared inside a class, interface, or
// enum body are emitted with Name="<EnclosingType>.<member>" so that
// EntityRecord.ComputeID(SourceFile+Kind+Name) produces distinct IDs for
// same-named members on sibling types. Module-level constructs and
// methods inside anonymous classes (which lack a stable enclosing-type
// name) stay bare. Nested types carry only their immediate parent — the
// nested class/interface/enum itself stays bare, but its members are
// qualified by it (multi-dot fully-qualified IDs are out of scope here).
// classCtx carries the resolution context for cross-file receiver
// binding (issue #120). fields maps a declared field name to its
// declared type identifier (the leaf type, not generic parameters).
// For nested classes the outer class's fields are NOT inherited — the
// walker rebuilds the map at every class entry.
type classCtx struct {
	fields map[string]string
}

func walk(
	node *sitter.Node,
	file extractor.FileInput,
	parentType string,
	cc *classCtx,
	imports map[string]bool,
	pkgName string,
	out *[]types.EntityRecord,
) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "class_declaration", "interface_declaration", "enum_declaration", "record_declaration":
		subtype := "class"
		switch node.Type() {
		case "interface_declaration":
			subtype = "interface"
		case "enum_declaration":
			subtype = "enum"
		case "record_declaration":
			// Refs #1935 Phase 1 — Java records emit a Class entity so the
			// ShapeTree subtree resolver treats them identically to POJOs.
			// Header parameters become field entities so the dashboard sees
			// them as CONTAINS children with type metadata.
			subtype = "record"
		}
		if node.Type() == "enum_declaration" {
			// Value-carrying SCOPE.Enum value-set node (data-model #3628).
			if vs, vok := buildJavaEnumValueSet(node, file); vok {
				*out = append(*out, vs)
			}
		}
		// #4430 — index constant COLLECTIONS (static-final Map.of/ImmutableMap
		// maps & arrays, interface constant groups) as queryable SCOPE.Enum
		// value-sets, the Java arm of the #4420/#4429 cross-language model.
		if tn := childFieldText(node, "name", file.Content); tn != "" {
			if body := node.ChildByFieldName("body"); body != nil {
				*out = append(*out, buildJavaConstCollections(tn, body, file)...)
			}
		}
		rec, ok := buildComponent(node, file, subtype, pkgName)
		if ok {
			// Issue #1996 — emit EXTENDS / IMPLEMENTS edges so the docgen
			// ClassManifest can populate `bases` and `interfaces`. The
			// tree-sitter Java grammar exposes the parent class via a
			// `superclass` named child (with a single nested
			// type_identifier) and implemented interfaces via
			// `super_interfaces` → `type_list` → many type_identifiers.
			// Both shapes are best-effort: malformed source still emits
			// the class entity, just without these structural edges.
			for _, base := range javaSuperclassNames(node, file.Content) {
				rec.Relationships = append(rec.Relationships,
					types.RelationshipRecord{ToID: base, Kind: "EXTENDS"})
			}
			for _, iface := range javaSuperInterfaceNames(node, file.Content) {
				rec.Relationships = append(rec.Relationships,
					types.RelationshipRecord{ToID: iface, Kind: "IMPLEMENTS"})
			}
			// Issue #1997 — emit a REFERENCES edge from the class entity
			// to every type appearing on an @Inject-annotated field. This
			// matches the cross-language convention that "consumers of X"
			// queries walk REFERENCES edges; previously Java DI fields
			// only produced a SCOPE.Schema child with a CONTAINS edge,
			// which made find-consumers traversals miss them. The Schema
			// child is preserved (extracted separately in field_declaration)
			// for source-level symmetry — this is option B from #1997, the
			// safer choice for downstream consumers.
			if body := node.ChildByFieldName("body"); body != nil {
				for _, injectedType := range javaInjectFieldTypes(body, file.Content) {
					rec.Relationships = append(rec.Relationships,
						types.RelationshipRecord{ToID: injectedType, Kind: "REFERENCES"})
				}
			}
		}
		if !ok {
			// Still recurse so nested types/imports below this node are
			// captured even when the class itself is malformed.
			for i := range node.ChildCount() {
				walk(node.Child(int(i)), file, parentType, cc, imports, pkgName, out)
			}
			return
		}
		classIdx := len(*out)
		*out = append(*out, rec)

		// Refs #1935 Phase 1 — Java record header parameters
		// (e.g. `record TransferRequest(String id, BigDecimal qty)`)
		// emit as SCOPE.Schema field entities so the dashboard
		// ShapeTree can render them as CONTAINS children of the
		// record class. The tree-sitter grammar exposes the
		// parameters via a `formal_parameters` child on
		// record_declaration; each child is a `formal_parameter`
		// with `type` + `name` named children.
		if node.Type() == "record_declaration" {
			if params := node.ChildByFieldName("parameters"); params != nil {
				for i := range params.ChildCount() {
					p := params.Child(int(i))
					if p == nil || p.Type() != "formal_parameter" {
						continue
					}
					nameNode := p.ChildByFieldName("name")
					typeNode := p.ChildByFieldName("type")
					if nameNode == nil || typeNode == nil {
						continue
					}
					fieldName := nodeText(nameNode, file.Content)
					typeName := nodeText(typeNode, file.Content)
					if fieldName == "" || typeName == "" {
						continue
					}
					emittedName := rec.Name + "." + fieldName
					// Preserve any annotations on the record
					// component by replaying the raw source span.
					raw := strings.TrimSpace(string(file.Content[p.StartByte():p.EndByte()]))
					raw = strings.Join(strings.Fields(raw), " ")
					sig := raw
					if sig == "" {
						sig = typeName + " " + fieldName
					}
					fieldRec := types.EntityRecord{
						Name:       emittedName,
						Kind:       "SCOPE.Schema",
						Subtype:    "field",
						SourceFile: file.Path,
						Language:   "java",
						StartLine:  int(p.StartPoint().Row) + 1,
						EndLine:    int(p.EndPoint().Row) + 1,
						Signature:  sig,
					}
					*out = append(*out, fieldRec)
					(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
						types.RelationshipRecord{
							ToID: extractor.BuildSchemaFieldStructuralRef("java", file.Path, emittedName),
							Kind: "CONTAINS",
						})
				}
			}
		}

		body := node.ChildByFieldName("body")
		if body != nil {
			// Issue #120 — pre-pass: collect this class's field types so
			// method invocations like `owners.findById(...)` can be
			// rewritten to `OwnerRepository.findById` at emit time. Field
			// scope is per-class only; we do NOT inherit from an outer
			// class because Java field resolution at a call site uses the
			// member-type rules, not lexical scope.
			localCtx := &classCtx{fields: collectFieldTypes(body, file.Content)}
			before := len(*out)
			for i := range body.ChildCount() {
				// Members of this type are qualified by rec.Name (the
				// immediate enclosing type), regardless of any outer
				// type we may currently be nested under. Enum bodies wrap
				// methods/constructors in an extra `enum_body_declarations`
				// node — descend through it so those members still receive
				// the enclosing-enum qualification.
				child := body.Child(int(i))
				if child != nil && child.Type() == "enum_body_declarations" {
					for j := range child.ChildCount() {
						walk(child.Child(int(j)), file, rec.Name, localCtx, imports, pkgName, out)
					}
					continue
				}
				walk(child, file, rec.Name, localCtx, imports, pkgName, out)
			}
			after := len(*out)
			for k := before; k < after; k++ {
				child := &(*out)[k]
				var toID string
				switch {
				case child.Kind == "SCOPE.Operation":
					// Issue #144 — emit a structural-ref (Format A) keyed on
					// the source file. child.Name is dotted "Outer.method" for
					// nested types (issue #65); the same string is the entity
					// Name indexed by byLocation, so the resolver matches.
					toID = extractor.BuildOperationStructuralRef("java", file.Path, child.Name)
				case child.Kind == "SCOPE.Schema" && child.Subtype == "field":
					// Issue #690 — emit CONTAINS for class fields, mirroring
					// the Python fix from #689. child.Name is "<Class>.<field>"
					// (qualified in buildField), matching the byLocation index
					// the resolver uses to bind the stub.
					toID = extractor.BuildSchemaFieldStructuralRef("java", file.Path, child.Name)
				default:
					continue
				}
				(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
					types.RelationshipRecord{
						ToID: toID,
						Kind: "CONTAINS",
					})
			}
		}

		// Issue #793 — Lombok annotation-driven entity synthesis.
		// Synthesize SCOPE.Operation / SCOPE.Component entities for every
		// method Lombok generates at compile time (@Builder, @Data, @Value,
		// @Getter, @Setter, @*Constructor, @With, @Accessors, @Singular).
		// We pass the raw source text of the class declaration (annotations +
		// declaration tokens, excluding the body) and the body text separately
		// so detectedAnnotations can scan the header and collectLombokFields
		// can scan the body.
		//
		// Issue #820 — emit CONTAINS edges from the class entity to every
		// synthesized SCOPE.Operation and SCOPE.Component entity. Without these
		// edges the synthesized entities have zero inbound edges and appear
		// orphaned. The CONTAINS relationship correctly models the fact that the
		// class "contains" its annotation-generated methods even though the
		// method bodies don't appear in source. We emit structural refs (Format A)
		// exactly as for real extracted children so the resolver can rebind them.
		if node.Type() == "class_declaration" {
			var classDeclSrc string
			if body != nil {
				// Declaration text = everything before the body's opening brace.
				classDeclSrc = string(file.Content[node.StartByte():body.StartByte()])
			} else {
				classDeclSrc = string(file.Content[node.StartByte():node.EndByte()])
			}
			var classBodySrc string
			if body != nil {
				classBodySrc = string(file.Content[body.StartByte():body.EndByte()])
			}
			// Class-level Lombok synthesis.
			lombokSynth := synthesizeLombokEntities(rec.Name, classDeclSrc, classBodySrc, file.Path)
			*out = append(*out, lombokSynth...)
			// Field-level @Getter / @Setter / @With synthesis (supplements class-level).
			lombokFieldSynth := synthesizeFieldLevelLombok(rec.Name, classBodySrc, file.Path)
			*out = append(*out, lombokFieldSynth...)
			// Issue #804 — Quarkus Panache static-method synthesizer.
			// Synthesize SCOPE.Operation entities for every method that Panache
			// provides at runtime for classes extending PanacheEntity / PanacheEntityBase,
			// implementing PanacheRepository<T>, or their Mongo/Reactive variants.
			// rawImports is derived from the full file content so import-package
			// detection determines which Panache flavour (SQL/Reactive/MongoDB) to use.
			rawImports := collectRawImports(file.Content)
			panacheSynth := synthesizePanacheEntities(rec.Name, classDeclSrc, classBodySrc, file.Path, rawImports)
			*out = append(*out, panacheSynth...)

			// Issue #820 — emit CONTAINS edges from the class entity to every
			// synthesized entity. This gives each synthesized method/component
			// at least one inbound edge (from its containing class) so it is no
			// longer orphaned. Use Format A structural refs matching the kind:
			//   SCOPE.Operation  → extractor.BuildOperationStructuralRef
			//   SCOPE.Component  → scope:component:ref:java:<file>:<name>
			//   SCOPE.Schema     → not emitted by synthesizers, skip
			// Issue #1994 — stamp StartLine/EndLine on every synthesized entity
			// (Lombok / Panache / @NamedQuery) with the class node's source
			// range. Synthesized entities have no AST node of their own, so the
			// next-best anchor is the declaring class — this guarantees that
			// docgen's source_window helper and the bundle-side fallback always
			// see non-zero line bounds and can emit useful excerpts.
			classStart := int(node.StartPoint().Row) + 1
			classEnd := int(node.EndPoint().Row) + 1
			stampSynthLines := func(slice []types.EntityRecord) {
				for i := range slice {
					if slice[i].StartLine == 0 {
						slice[i].StartLine = classStart
					}
					if slice[i].EndLine == 0 {
						slice[i].EndLine = classEnd
					}
				}
			}
			// The class-scoped synthesizers were appended directly to *out
			// above; mutate the trailing region of the slice in place. Each
			// append set begins at the recorded len(*out) before it ran, but
			// for simplicity we mutate the full lombok+panache block by
			// stamping the local slices and re-using the post-append region.
			stampSynthLines(lombokSynth)
			stampSynthLines(lombokFieldSynth)
			stampSynthLines(panacheSynth)
			// Issue #1887 — stamp QualifiedName on synthesized entities. The
			// per-kind synthesizer helpers (synthOp/synthComp/synthConstructor
			// in lombok.go, the panache helpers in panache.go) don't know the
			// file's package, so they emit entities with an empty
			// QualifiedName. Mirror the build{Component,Operation} convention:
			// for non-empty pkgName, QualifiedName = "<pkg>.<Name>". Name is
			// already qualified by parent class ("<Class>.<method>") for
			// synthesized members, so concatenation produces the full
			// "<pkg>.<Class>.<method>" form expected by inspect consumers.
			stampSynthQN := func(slice []types.EntityRecord) {
				if pkgName == "" {
					return
				}
				for i := range slice {
					if slice[i].QualifiedName == "" && slice[i].Name != "" {
						slice[i].QualifiedName = pkgName + "." + slice[i].Name
					}
				}
			}
			stampSynthQN(lombokSynth)
			stampSynthQN(lombokFieldSynth)
			stampSynthQN(panacheSynth)
			// Re-walk the *out tail to apply line numbers to entities that
			// were appended-by-value (their stamped versions live only in the
			// local slices above). We seek the matching name/kind in the tail
			// and stamp in place.
			tailStart := classIdx + 1
			for i := tailStart; i < len(*out); i++ {
				if (*out)[i].StartLine == 0 {
					(*out)[i].StartLine = classStart
				}
				if (*out)[i].EndLine == 0 {
					(*out)[i].EndLine = classEnd
				}
				// Issue #1887 — same QualifiedName stamping for the tail.
				if pkgName != "" && (*out)[i].QualifiedName == "" && (*out)[i].Name != "" {
					(*out)[i].QualifiedName = pkgName + "." + (*out)[i].Name
				}
			}

			allSynth := append(lombokSynth, lombokFieldSynth...)
			allSynth = append(allSynth, panacheSynth...)
			for _, s := range allSynth {
				var toID string
				switch s.Kind {
				case "SCOPE.Operation":
					toID = extractor.BuildOperationStructuralRef("java", file.Path, s.Name)
				case "SCOPE.Component":
					// Builder class entities (e.g. OrderBuilder). Use the
					// component structural ref form mirroring buildComponent.
					toID = extractor.BuildComponentStructuralRef("java", file.Path, s.Name)
				default:
					continue
				}
				(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
					types.RelationshipRecord{
						ToID: toID,
						Kind: "CONTAINS",
					})
			}
		}
		return

	case "method_declaration":
		if rec, ok := buildOperation(node, file, "method", parentType, pkgName); ok {
			// Self-recursion is detected by the bare callee identifier;
			// extractCallRelationships compares against the caller name.
			selfName := rec.Name
			if nameNode := node.ChildByFieldName("name"); nameNode != nil {
				selfName = nodeText(nameNode, file.Content)
			}
			paramTypes := collectParamTypes(node, file.Content)
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(
					node.ChildByFieldName("body"),
					file.Content, selfName, cc, paramTypes, imports,
				)...)
			// Issue #3689 — OpenTelemetry span instrumentation: @WithSpan
			// annotations and spanBuilder(...).startSpan() chains.
			rec.Relationships = append(rec.Relationships,
				javaTracingSpanEdges(node, selfName, file.Content)...)
			// Issue #3856 — non-OTel-tracing observability: Micrometer /
			// Dropwizard metrics, Spring Sleuth / Brave spans, SLF4J fluent
			// structured logging. Same INSTRUMENTS edge contract.
			rec.Relationships = append(rec.Relationships,
				javaObsEdges(node, selfName, file.Content)...)
			// #3628 — transaction-boundary stamping. Mark the method
			// transactional when it carries @Transactional (Spring or JTA),
			// capturing propagation / isolation / readOnly. Class-level
			// @Transactional is propagated to enclosing methods in a post-pass
			// (stampClassLevelTransactional).
			rec.Properties = javaMethodTxStamp(node, file.Content).Apply(rec.Properties)
			*out = append(*out, rec)
		}
		return

	case "constructor_declaration":
		if rec, ok := buildOperation(node, file, "constructor", parentType, pkgName); ok {
			selfName := rec.Name
			if nameNode := node.ChildByFieldName("name"); nameNode != nil {
				selfName = nodeText(nameNode, file.Content)
			}
			paramTypes := collectParamTypes(node, file.Content)
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(
					node.ChildByFieldName("body"),
					file.Content, selfName, cc, paramTypes, imports,
				)...)
			// Issue #3856 — observability instrumentation registered in a
			// constructor (common for Micrometer Counter/Timer fields).
			rec.Relationships = append(rec.Relationships,
				javaObsEdges(node, selfName, file.Content)...)
			*out = append(*out, rec)
		}
		return

	case "field_declaration":
		// Issue #690 — pass parentType so the field name is qualified as
		// "<Class>.<field>", matching the CONTAINS stub's byLocation key.
		// Fields at module scope (parentType="") keep a bare name.
		if rec, ok := buildField(node, file, parentType); ok {
			*out = append(*out, rec)
		}

		// import_declaration is handled by attachImportRelationships (issue #681)
		// which attaches IMPORTS edges to the file-level entity instead of
		// emitting a separate SCOPE.Component placeholder per import. The
		// placeholder entities were dangling (zero inbound edges) and
		// contributed ~1205 orphans on client-fixture-d.
	}

	// Default recursion. parentType / cc do NOT propagate through unrelated
	// nodes (e.g. method bodies, anonymous-class bodies) — methods nested
	// inside a method body or anonymous class are emitted bare because
	// they have no stable enclosing-type identifier, and their receiver
	// resolution starts from a fresh scope.
	for i := range node.ChildCount() {
		walk(node.Child(int(i)), file, "", nil, imports, pkgName, out)
	}
}

// extractCallRelationships returns one CALLS RelationshipRecord per unique
// method_invocation / object_creation_expression descendant of body.
//
// Issue #120 — receiver-aware target resolution. For a method_invocation
// `<obj>.<m>(...)` we attempt to type the receiver before falling back
// to the bare leaf name:
//
//   - `<obj>` is a field of the enclosing class with declared type T
//     → emit "T.m"
//   - `this.<obj>` where <obj> is such a field → "T.m"
//   - `<obj>` is a parameter of the enclosing method with type T → "T.m"
//   - `<obj>` is a PascalCase identifier (likely a Type) — including
//     when the file has imported a class by that simple name → "obj.m"
//     (static-call shape; the resolver's byKind/byName picks it up
//     because Java methods are emitted with Name="<EnclosingType>.m")
//
// All other shapes fall through to the bare leaf name.
//
// FromID is left empty so buildDocument substitutes the caller's entity
// ID at emit time. Self-recursion is skipped (compared against the
// caller's bare name regardless of the callee's dotted form).
func extractCallRelationships(
	body *sitter.Node,
	src []byte,
	callerName string,
	cc *classCtx,
	paramTypes map[string]string,
	imports map[string]bool,
) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	// Issue #120 — local variables typed via explicit declarations
	// (`Owner owner = new Owner()`, `LocalDate today = LocalDate.now()`)
	// are bound to their declared leaf type so a follow-up
	// `owner.setName(...)` resolves to "Owner.setName". Locals are
	// merged with paramTypes — declared params are visible in the same
	// lookup scope as locals — but param types take precedence so a
	// loop-local that shadows a parameter doesn't change the param's
	// type for the rest of the method (Java forbids name-shadowing of
	// parameters in the top-level method scope, so this only matters
	// for nested blocks; conservative bias, no harm).
	locals := collectLocalVarTypes(body, src)
	merged := paramTypes
	if len(locals) > 0 {
		merged = make(map[string]string, len(paramTypes)+len(locals))
		for k, v := range locals {
			merged[k] = v
		}
		for k, v := range paramTypes {
			merged[k] = v // params win over locals
		}
	}
	calls := findAllNodes(body, "method_invocation", "object_creation_expression")
	if len(calls) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(calls))
	rels := make([]types.RelationshipRecord, 0, len(calls))
	for _, call := range calls {
		target := javaCallTarget(call, src, cc, merged, imports)
		if target == "" {
			continue
		}
		// Self-recursion check: skip bare-name targets that match the
		// caller's own leaf name (e.g. `create()` calling itself without
		// a receiver). Dotted targets (e.g. "UsersService.create") are
		// cross-type calls and MUST NOT be filtered even when the leaf
		// matches the caller's name — "UsersController.create" calling
		// "UsersService.create" is a legitimate outbound call, not
		// recursion (#2111). The previous check applied the leaf match
		// to all dotted targets, which incorrectly dropped every CALLS
		// edge where the callee method shared its name with the enclosing
		// JAX-RS / REST controller method (create, update, delete, …).
		if strings.IndexByte(target, '.') < 0 && target == callerName {
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		// Line is 1-based: tree-sitter StartPoint().Row is 0-based.
		callLine := strconv.Itoa(int(call.StartPoint().Row) + 1)
		rels = append(rels, types.RelationshipRecord{
			ToID:       target,
			Kind:       "CALLS",
			Properties: map[string]string{"line": callLine},
		})
	}
	return rels
}

// javaCallTarget resolves the callee target from a method_invocation or
// object_creation_expression node. Issue #120 — for method_invocation
// the receiver (object field) is consulted to produce a dotted
// "<Type>.<method>" target whenever the receiver's type is statically
// determinable from field declarations, parameter types, or PascalCase
// shape. Falls back to the bare leaf name when no receiver type is
// known.
func javaCallTarget(
	call *sitter.Node,
	src []byte,
	cc *classCtx,
	paramTypes map[string]string,
	imports map[string]bool,
) string {
	switch call.Type() {
	case "method_invocation":
		nameNode := call.ChildByFieldName("name")
		if nameNode == nil {
			return ""
		}
		method := string(src[nameNode.StartByte():nameNode.EndByte()])
		obj := call.ChildByFieldName("object")
		if obj == nil {
			// No receiver — bare-name call (helper(); foo();).
			return method
		}
		recv := receiverTypeName(obj, src, cc, paramTypes, imports)
		if recv == "" {
			return method
		}
		return recv + "." + method
	case "object_creation_expression":
		typ := call.ChildByFieldName("type")
		if typ == nil {
			return ""
		}
		// Issue #2062 — constructor binding. `new ClassName(args)` was
		// previously emitted as a CALLS stub "ClassName", which bound to
		// the class entity (SCOPE.Component) instead of the constructor.
		// As a result every Lombok-synthesized constructor (Name shape
		// "ClassName.ClassName" from synthConstructor) ended up orphaned —
		// no inbound CALLS edge ever pointed at it. Emit the qualified
		// constructor form so the resolver's byName / byMember indexes
		// route the edge to the synthesized (or extracted) constructor
		// entity. The class entity still receives EXTENDS / IMPLEMENTS
		// edges through their own code paths, so no class-level signal
		// is lost.
		//
		// Falls back to the bare class name when the rightmost
		// type_identifier could not be located (defensive — keeps the
		// previous behaviour for malformed parses).
		var className string
		ids := findAllNodes(typ, "type_identifier")
		if len(ids) > 0 {
			n := ids[len(ids)-1]
			className = string(src[n.StartByte():n.EndByte()])
		} else {
			className = string(src[typ.StartByte():typ.EndByte()])
		}
		if className == "" {
			return ""
		}
		return className + "." + className
	}
	return ""
}

// panacheQueryReturningMethods is the set of method names that return a
// PanacheQuery object when called on a Panache entity class or repository.
// Used by receiverTypeName to type chained calls like
// `Order.find(...).list()` → `PanacheQuery.list`.
//
// Issue #818 — method-return-type tracking for Panache DSL chains.
var panacheQueryReturningMethods = map[string]bool{
	"find":    true,
	"findAll": true,
}

// panacheQueryDSLChainMethods is the set of PanacheQuery instance methods
// that return PanacheQuery<T> (i.e. chainable). Methods that are terminal
// (returning List, T, Optional, long, etc.) are NOT listed here, but they
// still bind to PanacheQuery.* when the receiver is a panache chain.
var panacheQueryDSLChainMethods = map[string]bool{
	"page":         true,
	"nextPage":     true,
	"previousPage": true,
	"firstPage":    true,
	"lastPage":     true,
	"range":        true,
	"withHint":     true,
	"withLock":     true,
	"project":      true,
	"filter":       true,
}

// receiverTypeName returns the declared type of a method_invocation's
// `object` field when statically determinable, or "" otherwise.
//
// Resolution order:
//
//  1. Receiver is `this.<id>` field_access → look up <id> in cc.fields.
//  2. Receiver is a bare identifier matching a known field → field type.
//  3. Receiver is a bare identifier matching a known parameter → param type.
//  4. Receiver is a bare identifier whose first rune is uppercase
//     (PascalCase) — treat as a Type identifier (static-call shape) and
//     return it verbatim. Imports[<id>] presence is a stronger signal
//     but not required: most Java conventions use PascalCase for type
//     names and lowerCamelCase for fields/locals, so the case
//     heuristic alone is reliable enough to catch JDK constants like
//     `Math.max`, `Integer.parseInt`, `String.format` etc.
//  5. Receiver is a method_invocation whose callee is a Panache
//     query-returning method (find, findAll, or a DSL chain method like
//     page, withHint, etc.) → return "PanacheQuery" so the chained DSL
//     method binds to the PanacheQuery.* synthesized entity (#818).
//  6. Anything else — return "" so the caller falls back to the bare
//     method name.
func receiverTypeName(
	obj *sitter.Node,
	src []byte,
	cc *classCtx,
	paramTypes map[string]string,
	imports map[string]bool,
) string {
	if obj == nil {
		return ""
	}
	switch obj.Type() {
	case "identifier":
		ident := string(src[obj.StartByte():obj.EndByte()])
		if cc != nil {
			if t, ok := cc.fields[ident]; ok && t != "" {
				return t
			}
		}
		if t, ok := paramTypes[ident]; ok && t != "" {
			return t
		}
		// PascalCase static-call shape. Java identifiers that begin
		// with an uppercase letter are types by overwhelming
		// convention; using the identifier verbatim preserves the
		// "<Type>.<method>" form the resolver's byKind index needs to
		// rebind cross-file.
		if isPascalCase(ident) {
			return ident
		}
		_ = imports // imports presence reserved for future tightening
		return ""
	case "field_access":
		// `this.<field>` shape — field is the rightmost identifier.
		// Other field_access forms (`a.b.c.method`) are deeper
		// chains we don't currently type.
		objChild := obj.ChildByFieldName("object")
		fieldChild := obj.ChildByFieldName("field")
		if objChild == nil || fieldChild == nil {
			return ""
		}
		if objChild.Type() != "this" {
			return ""
		}
		ident := string(src[fieldChild.StartByte():fieldChild.EndByte()])
		if cc != nil {
			if t, ok := cc.fields[ident]; ok && t != "" {
				return t
			}
		}
		return ""
	case "method_invocation":
		// Issue #818 — PanacheQuery chain detection.
		//
		// Pattern: `EntityClass.find(...).list()` — the outer call's receiver
		// is a method_invocation. If that inner call's method is a known
		// Panache query-returning method (find, findAll) OR a PanacheQuery DSL
		// chain method (page, withHint, etc.), we return "PanacheQuery" so the
		// outer call target becomes "PanacheQuery.<method>".
		//
		// This handles both:
		//   Entity.find(...).list()         → PanacheQuery.list
		//   Entity.find(...).page(0,20).list() → PanacheQuery.list (via recursive typing)
		innerName := obj.ChildByFieldName("name")
		if innerName == nil {
			return ""
		}
		callee := string(src[innerName.StartByte():innerName.EndByte()])
		if panacheQueryReturningMethods[callee] || panacheQueryDSLChainMethods[callee] {
			return "PanacheQuery"
		}
		return ""
	}
	return ""
}

// isPascalCase reports whether s starts with an uppercase ASCII letter
// followed by at least one more character. Conservative — we don't
// fold Unicode case classes here because Java type identifiers are
// almost universally ASCII PascalCase, and a wider definition risks
// false positives on locale-specific lower-case identifiers.
func isPascalCase(s string) bool {
	if len(s) < 2 {
		return false
	}
	c := s[0]
	return c >= 'A' && c <= 'Z'
}

// collectFieldTypes walks the immediate children of a class/interface/
// enum body and returns a map of field-name → declared-type-leaf for
// every `field_declaration`. Generic parameters and array suffixes are
// stripped — the leaf type identifier is what the resolver indexes
// against (`List<Owner>` → "List", `Owner[]` → "Owner").
//
// Multi-declarator fields (`int x, y, z;`) bind every variable to the
// same declared type. Fields without a parseable type are dropped.
func collectFieldTypes(body *sitter.Node, src []byte) map[string]string {
	if body == nil {
		return nil
	}
	out := make(map[string]string)
	for i := 0; i < int(body.ChildCount()); i++ {
		ch := body.Child(i)
		if ch == nil || ch.Type() != "field_declaration" {
			continue
		}
		typ := leafTypeName(ch.ChildByFieldName("type"), src)
		if typ == "" {
			continue
		}
		for j := 0; j < int(ch.ChildCount()); j++ {
			d := ch.Child(j)
			if d == nil || d.Type() != "variable_declarator" {
				continue
			}
			name := childFieldText(d, "name", src)
			if name == "" {
				continue
			}
			// First declaration wins — Java disallows shadowing a
			// field within the same class anyway, so this is
			// effectively a no-collision insert.
			if _, ok := out[name]; !ok {
				out[name] = typ
			}
		}
	}
	return out
}

// collectParamTypes returns a map of parameter-name → leaf-type for
// every formal_parameter on a method_declaration / constructor_
// declaration node. Variadic parameters ("Type... args") strip the
// "..." and bind args to the leaf type.
func collectParamTypes(node *sitter.Node, src []byte) map[string]string {
	if node == nil {
		return nil
	}
	params := node.ChildByFieldName("parameters")
	if params == nil {
		return nil
	}
	out := make(map[string]string)
	for i := 0; i < int(params.ChildCount()); i++ {
		p := params.Child(i)
		if p == nil {
			continue
		}
		switch p.Type() {
		case "formal_parameter", "spread_parameter":
			typ := leafTypeName(p.ChildByFieldName("type"), src)
			if typ == "" {
				continue
			}
			name := childFieldText(p, "name", src)
			if name == "" {
				// spread_parameter shape (`Type... args`) wraps a
				// variable_declarator; pick its name field.
				for j := 0; j < int(p.ChildCount()); j++ {
					ch := p.Child(j)
					if ch != nil && ch.Type() == "variable_declarator" {
						name = childFieldText(ch, "name", src)
						break
					}
				}
			}
			if name == "" {
				continue
			}
			out[name] = typ
		}
	}
	return out
}

// collectLocalVarTypes walks the descendants of a method/constructor
// body and returns a map of local-variable-name → declared leaf type
// for every local_variable_declaration node. Used by the receiver
// binder so calls like `Owner owner = new Owner(); owner.setId(...)`
// resolve to "Owner.setId".
//
// Variable declarations using `var` (Java 10+) are not typed here —
// inferring the type would require chasing the initialiser expression,
// which is out of scope. Multi-declarator declarations bind every
// variable to the declared type. Re-declarations within nested blocks
// produce a last-writer-wins shape; Java forbids re-declaring a name
// already in the enclosing block, so the only collisions in practice
// are loop-local rebinds in different sibling blocks — both bind to
// the same type in idiomatic code, and the conservative pick still
// matches.
func collectLocalVarTypes(body *sitter.Node, src []byte) map[string]string {
	if body == nil {
		return nil
	}
	out := map[string]string{}
	for _, decl := range findAllNodes(body, "local_variable_declaration") {
		typ := leafTypeName(decl.ChildByFieldName("type"), src)
		if typ == "" {
			continue
		}
		for i := 0; i < int(decl.ChildCount()); i++ {
			ch := decl.Child(i)
			if ch == nil || ch.Type() != "variable_declarator" {
				continue
			}
			name := childFieldText(ch, "name", src)
			if name == "" {
				continue
			}
			out[name] = typ
		}
	}
	// `enhanced_for_statement` (`for (Owner o : owners) { ... }`) — bind
	// the loop variable to its declared type so calls inside the body
	// can be receiver-typed.
	for _, fr := range findAllNodes(body, "enhanced_for_statement") {
		typ := leafTypeName(fr.ChildByFieldName("type"), src)
		if typ == "" {
			continue
		}
		name := childFieldText(fr, "name", src)
		if name != "" {
			out[name] = typ
		}
	}
	return out
}

// leafTypeName returns the leaf type identifier of a Java type node,
// stripping generic parameters and array suffixes. `List<Owner>`
// yields "List"; `Map<String, Owner>` yields "Map"; `Owner[]` yields
// "Owner"; `int` yields "int". Returns "" for type nodes the function
// can't characterise.
func leafTypeName(typ *sitter.Node, src []byte) string {
	if typ == nil {
		return ""
	}
	switch typ.Type() {
	case "type_identifier", "void_type", "integral_type",
		"floating_point_type", "boolean_type":
		return strings.TrimSpace(string(src[typ.StartByte():typ.EndByte()]))
	case "generic_type":
		// First child is the underlying type_identifier or scoped type.
		if first := typ.NamedChild(0); first != nil {
			return leafTypeName(first, src)
		}
	case "array_type":
		if elem := typ.ChildByFieldName("element"); elem != nil {
			return leafTypeName(elem, src)
		}
		// Some grammars expose the element as the first named child.
		if first := typ.NamedChild(0); first != nil {
			return leafTypeName(first, src)
		}
	case "scoped_type_identifier":
		// `com.foo.Bar` — leaf is the rightmost type_identifier.
		ids := findAllNodes(typ, "type_identifier")
		if len(ids) > 0 {
			n := ids[len(ids)-1]
			return strings.TrimSpace(string(src[n.StartByte():n.EndByte()]))
		}
	}
	return ""
}

// collectImportNames scans the file for top-level import_declaration
// nodes and returns a set of locally-bound simple names introduced by
// non-wildcard, non-static imports. `import com.foo.Bar;` adds "Bar".
// Wildcard imports (`import com.foo.*;`) and static imports of static
// fields/methods are not included; the receiver-binder uses this set
// only to confirm a PascalCase identifier was imported (a future
// tightening — for now the case heuristic alone gates emission).
func collectImportNames(root *sitter.Node, src []byte) map[string]bool {
	if root == nil {
		return nil
	}
	out := make(map[string]bool)
	for _, n := range findAllNodes(root, "import_declaration") {
		raw := strings.TrimSpace(string(src[n.StartByte():n.EndByte()]))
		raw = strings.TrimPrefix(raw, "import ")
		isStatic := strings.HasPrefix(raw, "static ")
		raw = strings.TrimPrefix(raw, "static ")
		raw = strings.TrimSuffix(raw, ";")
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasSuffix(raw, ".*") {
			continue
		}
		leaf := raw
		if dot := strings.LastIndexByte(raw, '.'); dot > 0 {
			leaf = raw[dot+1:]
		}
		if isStatic {
			// `import static X.Y.Z;` introduces Z at top level — not a
			// type binding, but we record it anyway so a future
			// improvement can disambiguate.
			out[leaf] = true
			continue
		}
		out[leaf] = true
	}
	return out
}

// collectPackageName extracts the dotted package name from the file's
// package_declaration node (issue #1917). Returns "" when no package
// declaration is present (default package).
//
// Example: `package com.example.users.controllers;` → "com.example.users.controllers"
func collectPackageName(root *sitter.Node, src []byte) string {
	if root == nil {
		return ""
	}
	for i := range root.ChildCount() {
		child := root.Child(int(i))
		if child == nil || child.Type() != "package_declaration" {
			continue
		}
		// The package name is the entire text between "package" keyword and ";",
		// captured by the scoped_identifier / identifier children. Using the raw
		// node text minus the leading keyword and trailing semicolon is the most
		// robust approach across grammar versions.
		raw := string(src[child.StartByte():child.EndByte()])
		raw = strings.TrimSpace(raw)
		raw = strings.TrimPrefix(raw, "package ")
		raw = strings.TrimSuffix(raw, ";")
		raw = strings.TrimSpace(raw)
		if raw != "" {
			return raw
		}
	}
	return ""
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

// buildComponent creates a Component entity for class/interface declarations.
//
// Issue #1917 — QualifiedName is set to "<package>.<ClassName>" when pkgName
// is non-empty, giving inspect consumers a fully-qualified type reference.
func buildComponent(node *sitter.Node, file extractor.FileInput, subtype, pkgName string) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}

	qn := name
	if pkgName != "" {
		qn = pkgName + "." + name
	}

	return types.EntityRecord{
		Name:               name,
		QualifiedName:      qn,
		Kind:               "SCOPE.Component",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "java",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildClassSignature(node, file.Content, name),
		EnrichmentRequired: false,
	}, true
}

// buildOperation creates an Operation entity for method/constructor declarations.
//
// Issue #65: when parentType is non-empty (member of a class/interface/enum),
// Name is emitted as "<parentType>.<member>" so two sibling types declaring
// same-named methods produce distinct ComputeID(SourceFile+Kind+Name) values.
// The dotted form is the encoding consumed by resolve.Index.byMember, which
// splits on the first '.'.
//
// Issue #1917 — QualifiedName is set to "<package>.<emittedName>" when pkgName
// is non-empty, giving inspect consumers a fully-qualified method reference.
func buildOperation(node *sitter.Node, file extractor.FileInput, subtype, parentType, pkgName string) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}

	emittedName := name
	if parentType != "" {
		emittedName = parentType + "." + name
	}

	qn := emittedName
	if pkgName != "" {
		qn = pkgName + "." + emittedName
	}

	return types.EntityRecord{
		Name:               emittedName,
		QualifiedName:      qn,
		Kind:               "SCOPE.Operation",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "java",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildMethodSignature(node, file.Content),
		EnrichmentRequired: false,
	}, true
}

// buildField creates a Schema entity for field declarations.
//
// Issue #690 — parentType qualifies the field name as "<Class>.<field>"
// when non-empty, matching the pattern used for methods (issue #65) so
// the resolver's byLocation index can bind CONTAINS stubs to field entities
// the same way it binds class→method CONTAINS edges.
func buildField(node *sitter.Node, file extractor.FileInput, parentType string) (types.EntityRecord, bool) {
	// Field declarations have a "declarator" child containing the variable name.
	name := ""
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch.Type() == "variable_declarator" {
			name = childFieldText(ch, "name", file.Content)
			break
		}
	}
	if name == "" {
		return types.EntityRecord{}, false
	}

	emittedName := name
	if parentType != "" {
		emittedName = parentType + "." + name
	}

	// Build field signature: "Type name" (strip visibility).
	fieldSig := buildFieldSignature(node, file.Content, name)

	return types.EntityRecord{
		Name:       emittedName,
		Kind:       "SCOPE.Schema",
		Subtype:    "field",
		SourceFile: file.Path,
		Language:   "java",
		StartLine:  int(node.StartPoint().Row) + 1,
		EndLine:    int(node.EndPoint().Row) + 1,
		Signature:  fieldSig,
	}, true
}

// buildFieldSignature produces "Type name" for a Java field, stripping visibility.
func buildFieldSignature(node *sitter.Node, src []byte, name string) string {
	raw := strings.TrimSpace(string(src[node.StartByte():node.EndByte()]))
	// Remove everything after '=' (initializer).
	if idx := strings.Index(raw, "="); idx >= 0 {
		raw = strings.TrimSpace(raw[:idx])
	}
	// Remove trailing ';'.
	raw = strings.TrimSuffix(raw, ";")
	raw = strings.TrimSpace(raw)
	// Strip visibility modifiers.
	for _, mod := range []string{"public ", "private ", "protected ", "static ", "final "} {
		raw = strings.ReplaceAll(raw, mod, "")
	}
	return strings.TrimSpace(raw)
}

// nodeText returns the source text covered by node.
func nodeText(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	return string(src[node.StartByte():node.EndByte()])
}

// childFieldText extracts the text of a named child field (e.g. "name").
func childFieldText(node *sitter.Node, field string, src []byte) string {
	child := node.ChildByFieldName(field)
	if child == nil {
		return ""
	}
	return string(src[child.StartByte():child.EndByte()])
}

// buildMethodSignature builds a Python-parity method signature.
// Captures annotations + return type + name + parameters, collapsing
// multi-line declarations into a single line (up to the opening brace).
// Strips visibility modifiers and annotation arguments.
func buildMethodSignature(node *sitter.Node, src []byte) string {
	raw := string(src[node.StartByte():node.EndByte()])
	// Strip annotation arguments FIRST to remove braces inside annotation args
	// like @DeleteMapping("/{id}") → @DeleteMapping, so the body-brace search
	// doesn't get confused by braces in annotation strings.
	raw = stripAnnotationArgs(raw)
	// Trim at opening brace (body start).
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = raw[:idx]
	}
	// Collapse newlines + whitespace into single spaces.
	raw = strings.Join(strings.Fields(raw), " ")
	// Strip visibility modifiers to match Python convention.
	for _, mod := range []string{"public ", "private ", "protected ", "static "} {
		raw = strings.ReplaceAll(raw, mod, "")
	}
	return strings.TrimSpace(raw)
}

// buildClassSignature constructs a readable signature up to the opening brace.
// Strips visibility modifiers and annotation arguments to match Python convention.
func buildClassSignature(node *sitter.Node, src []byte, name string) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = raw[:idx]
	}
	// Collapse newlines + whitespace into single spaces.
	raw = strings.Join(strings.Fields(raw), " ")
	// Strip visibility modifiers.
	for _, mod := range []string{"public ", "private ", "protected ", "static "} {
		raw = strings.ReplaceAll(raw, mod, "")
	}
	// Strip annotation arguments: @Foo("bar") -> @Foo
	raw = stripAnnotationArgs(raw)
	return strings.TrimSpace(raw)
}

// javaSuperclassNames extracts the parent class name from a class_declaration
// node's `superclass` child. Returns a slice (always 0 or 1 elements) for
// uniform call-site iteration with javaSuperInterfaceNames. Generics are
// stripped — `extends List<Owner>` yields "List".
//
// Issue #1996 — required input for the docgen ClassManifest `bases` field.
func javaSuperclassNames(node *sitter.Node, src []byte) []string {
	if node == nil {
		return nil
	}
	sc := node.ChildByFieldName("superclass")
	if sc == nil {
		return nil
	}
	// `superclass` wraps either a type_identifier, a generic_type, or a
	// scoped_type_identifier. leafTypeName covers all three.
	for i := 0; i < int(sc.NamedChildCount()); i++ {
		ch := sc.NamedChild(i)
		if ch == nil {
			continue
		}
		if name := leafTypeName(ch, src); name != "" {
			return []string{name}
		}
	}
	return nil
}

// javaSuperInterfaceNames extracts the implemented-interface names from a
// class_declaration node's `interfaces` child (`super_interfaces` in the
// grammar, exposed via the `interfaces` field). The interface list is a
// `type_list` of type_identifier (or generic_type) nodes; each is
// reduced to its leaf type identifier.
//
// Issue #1996 — required input for the docgen ClassManifest `interfaces`
// field.
func javaSuperInterfaceNames(node *sitter.Node, src []byte) []string {
	if node == nil {
		return nil
	}
	si := node.ChildByFieldName("interfaces")
	if si == nil {
		// Fallback: scan named children for super_interfaces (the field
		// name varies between grammar versions).
		for i := 0; i < int(node.NamedChildCount()); i++ {
			ch := node.NamedChild(i)
			if ch != nil && ch.Type() == "super_interfaces" {
				si = ch
				break
			}
		}
	}
	if si == nil {
		return nil
	}
	var out []string
	// si may directly be a type_list, or wrap one.
	var list *sitter.Node
	if si.Type() == "type_list" {
		list = si
	} else {
		for i := 0; i < int(si.NamedChildCount()); i++ {
			ch := si.NamedChild(i)
			if ch != nil && ch.Type() == "type_list" {
				list = ch
				break
			}
		}
	}
	if list == nil {
		return nil
	}
	for i := 0; i < int(list.NamedChildCount()); i++ {
		ch := list.NamedChild(i)
		if ch == nil {
			continue
		}
		if name := leafTypeName(ch, src); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// javaInjectFieldTypes returns the declared leaf type of every field on a
// class body whose `modifiers` block contains an @Inject (or @Autowired)
// annotation. The match is case-sensitive and accepts both
// `marker_annotation` (`@Inject`) and `annotation` (`@Inject(qualifier=...)`).
//
// Issue #1997 — cross-language DI consistency. Java extractor emits
// REFERENCES edges from the containing class entity to every injected
// type so "find consumers of UsersService" queries walk consistently with
// Python (which already uses REFERENCES for the same shape).
//
// The Schema/CONTAINS edge for the field itself is still emitted by the
// regular field_declaration case in walk(); this function does not
// suppress it.
func javaInjectFieldTypes(body *sitter.Node, src []byte) []string {
	if body == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for i := 0; i < int(body.NamedChildCount()); i++ {
		ch := body.NamedChild(i)
		if ch == nil || ch.Type() != "field_declaration" {
			continue
		}
		if !javaFieldHasInjectAnnotation(ch, src) {
			continue
		}
		typ := leafTypeName(ch.ChildByFieldName("type"), src)
		if typ == "" || seen[typ] {
			continue
		}
		seen[typ] = true
		out = append(out, typ)
	}
	return out
}

// javaFieldHasInjectAnnotation reports whether a field_declaration node
// carries an @Inject or @Autowired annotation in its `modifiers` child.
func javaFieldHasInjectAnnotation(field *sitter.Node, src []byte) bool {
	if field == nil {
		return false
	}
	for i := 0; i < int(field.NamedChildCount()); i++ {
		ch := field.NamedChild(i)
		if ch == nil || ch.Type() != "modifiers" {
			continue
		}
		for j := 0; j < int(ch.NamedChildCount()); j++ {
			ann := ch.NamedChild(j)
			if ann == nil {
				continue
			}
			if ann.Type() != "marker_annotation" && ann.Type() != "annotation" {
				continue
			}
			nameNode := ann.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			name := string(src[nameNode.StartByte():nameNode.EndByte()])
			// Accept the simple name as well as fully-qualified forms
			// (`javax.inject.Inject` / `jakarta.inject.Inject` /
			// `org.springframework.beans.factory.annotation.Autowired`).
			if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
				name = name[dot+1:]
			}
			if name == "Inject" || name == "Autowired" {
				return true
			}
		}
	}
	return false
}

// stripAnnotationArgs removes parenthesised arguments from Java annotations.
// @RequestMapping("/api/users") -> @RequestMapping
// Only strips args immediately following an @Identifier — does not affect
// method parameter parens.
func stripAnnotationArgs(s string) string {
	var result strings.Builder
	depth := 0
	// expectAnnotationParen: true right after @AnnotationName, before a space or (.
	expectAnnotationParen := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '@':
			expectAnnotationParen = true
			result.WriteByte(ch)
		case expectAnnotationParen && (ch == '_' || (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')):
			// Still in annotation identifier name.
			result.WriteByte(ch)
		case expectAnnotationParen && ch == '(':
			// Annotation args start — eat until matching ')'.
			depth = 1
			expectAnnotationParen = false
			for i++; i < len(s) && depth > 0; i++ {
				switch s[i] {
				case '(':
					depth++
				case ')':
					depth--
				}
			}
			i-- // outer loop will i++
		case expectAnnotationParen:
			// Non-identifier char after @Name — annotation has no args.
			expectAnnotationParen = false
			result.WriteByte(ch)
		default:
			result.WriteByte(ch)
		}
	}
	return result.String()
}
