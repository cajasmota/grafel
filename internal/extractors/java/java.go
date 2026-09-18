// Package java implements the tree-sitter–based extractor for Java source files.
//
// Extracted entities:
//   - class_declaration       → Kind="SCOPE.Component", Subtype="class"
//   - interface_declaration   → Kind="SCOPE.Component", Subtype="interface"
//   - annotation_type_declaration          → Kind="SCOPE.Component", Subtype="annotation" (#7073)
//   - annotation_type_element_declaration  → Kind="SCOPE.Schema",    Subtype="field"      (#7073)
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

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/txscope"
	"github.com/cajasmota/grafel/internal/types"
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
func javaMethodTxStamp(methodNode ts.Node, src []byte) txscope.Stamp {
	mods := methodModifiersText(methodNode, src)
	if mods == "" {
		return txscope.Stamp{}
	}
	return txscope.DetectJava(mods)
}

// methodModifiersText returns the source text of a declaration's `modifiers`
// child (annotations + visibility keywords), or "" when absent.
func methodModifiersText(node ts.Node, src []byte) string {
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
func stampClassLevelTransactional(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil {
		return
	}
	walkClassTx(root, "", file, entities)
}

func walkClassTx(n ts.Node, pkgQualifier string, file extractor.FileInput, entities *[]types.EntityRecord) {
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

	if file.TSTree == nil || len(file.Content) == 0 {
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
	root := file.TSTree.RootNode()
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

	// Issue #6912 arm E — field -> declared-type REFERENCES edges, same-file
	// targets only. See field_type_refs.go for the full ruling.
	//
	// It must run after walk, which is where every record for THIS file is
	// produced: the classes, the enum value-sets, Lombok's synthesized
	// builders and the nosql `schema` model node all reach `entities` from
	// inside it, and the allow-list and the ambiguity rule have to weigh all
	// of them. The position is LATER than that but the extra distance is NOT
	// load-bearing today, and saying so is more useful than implying it is.
	//
	// An equivalence argument is only as good as its enumeration, so here is
	// the complete list of the SEVEN passes between walk's return and this
	// call, and what each does to `entities`:
	//
	//	stampClassLevelTransactional  takes &entities, but only mutates
	//	                              op.Properties via findJavaOp
	//	attachImportRelationships     handed &entities[0]; cannot append
	//	synthesizePanacheDSLEntities  the ONLY appender — and it stamps
	//	                              panacheDSLSyntheticSourceFile, so its
	//	                              records never share this file's path
	//	emitReferences                relationships only
	//	emitConfigConsumerEdges       relationships only
	//	emitExceptionFlowEdges        relationships only
	//	emitTemplateRenderEdges       relationships only
	//
	// The first two were missing from an earlier revision of this comment,
	// and stampClassLevelTransactional is the only other pass that takes the
	// slice BY POINTER — exactly the one an incomplete enumeration should not
	// have dropped. So no record for this file exists here that did not exist
	// at walk's return: moving this call up changes no output, a mutant that
	// does so is alive under the suite AND produces a byte-identical 147/147
	// corpus result, and it sits here only so a future pass that DOES append a
	// same-file entity is seen by default rather than by luck.
	attachJavaFieldTypeRefs(entities, file.Path)

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
	node ts.Node,
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
	case "class_declaration", "interface_declaration", "enum_declaration", "record_declaration",
		"annotation_type_declaration":
		subtype := "class"
		switch node.Type() {
		case "interface_declaration":
			subtype = "interface"
		case "annotation_type_declaration":
			// Issue #7073 — a Java annotation type (`public @interface Audited`)
			// previously minted NO entity at all, so a project's own annotations
			// were structurally invisible: nothing to find, nothing to point an
			// edge at.
			//
			// Kind is SCOPE.Component with its OWN subtype rather than reusing
			// "interface", staying inside the existing Kind vocabulary (no
			// SCOPE.* bump) exactly as enum/record already do.
			//
			// TWO REASONS THAT WERE FIRST GIVEN HERE AND ARE NOT TRUE, recorded
			// so they are not re-derived:
			//
			//   - "it would pollute who-implements-this-interface answers". It
			//     would not. Those answers are EDGE-driven (docgen/llm_bundle.go,
			//     mcp/mro.go), and an `@interface` emits neither EXTENDS nor
			//     IMPLEMENTS, so it contributes zero rows under either subtype.
			//   - "a distinct subtype keeps `list this project's annotations`
			//     answerable". It does not, today: Subtype is invisible to MCP —
			//     serializeEntity and grafel_find hit rows emit no `subtype` key,
			//     no tool filters on one, and Java does not dual-stamp
			//     Properties["subtype"]. That query is not answerable over MCP at
			//     all right now, under any subtype.
			//
			// The reasons that ARE real are downstream subtype tables, and they
			// cut both ways — stated with their costs rather than only their
			// benefits:
			//
			//   - engine.ClassLikeComponentSubtypes (classfold.go): "annotation"
			//     is absent, so an annotation is never a class-fold SOURCE. Inert
			//     today (there is no typed survivor for it to fold into) but it
			//     is a real exclusion, not a neutral one.
			//   - links/sameas_pass.go modelSubtypes: absent, and isDomainModelKind
			//     returns modelSubtypes[sub], so an annotation is excluded from
			//     cross-repo SAME_AS. Deliberate — an annotation is not a shared
			//     domain model — but it IS a cost if two repos ever share one.
			//   - feedback/report.go isFieldExtractionCandidate: "annotation" is
			//     NOT in nonClassSubtypes, so an annotation IS admitted to the
			//     field-bearing denominator. Verified by reading the predicate,
			//     not assumed. That is the correct side for it to land on: with
			//     elements emitted as Subtype "field" children, an annotation
			//     genuinely is field-bearing and can pass.
			//
			// Reusing "interface" would have put annotations into the first two
			// tables and left the third unchanged. Choosing a new subtype trades
			// the class-fold and SAME_AS rows (worth nothing here) for a subtype
			// that names what the thing is.
			subtype = "annotation"
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
		// Issue #4872 — collect the AST node carrying each field's
		// annotations (record component or class field_declaration) so the
		// Bean Validation pass can stamp Properties["validations"] without
		// re-traversing the tree.
		recBefore := len(*out)
		fieldNodes := map[string]ts.Node{}
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
					fieldNodes[fieldName] = p
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
					// Issue #6912 arm E — stash the record component's
					// declared type so attachJavaFieldTypeRefs can emit the
					// field -> type REFERENCES edge once the file's full
					// record set exists. typeNode is the same node typeName
					// was read from; the AST is passed rather than the string
					// because a flat type string cannot be unwrapped safely
					// (arm C's ruling, and swift's lossy field_type property
					// is the live counter-example).
					stashJavaFieldTypeRefs(&fieldRec, typeNode, file.Content, rec.Name)
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
			// Issue #4872 — record each class field_declaration's node by leaf
			// name so the Bean Validation pass can read its annotations.
			for i := range body.ChildCount() {
				ch := body.Child(int(i))
				if ch == nil || ch.Type() != "field_declaration" {
					continue
				}
				for j := range ch.ChildCount() {
					vd := ch.Child(int(j))
					if vd == nil || vd.Type() != "variable_declarator" {
						continue
					}
					if fn := childFieldText(vd, "name", file.Content); fn != "" {
						fieldNodes[fn] = ch
					}
				}
			}
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

		// Issue #4872 — route Bean Validation annotations (javax.* + jakarta.*)
		// on this type's fields/record-components into Properties["validations"]
		// so the dashboard ShapeTree renders them as constraint chips, matching
		// TS (#4858) and Python (#4871). Covers the whole [recBefore, now)
		// window — both record components and class field_declarations.
		emitJavaFieldValidations(rec.Name, recBefore, len(*out), fieldNodes, file.Content, out)

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
			// Issue #4283 — Spring Data NoSQL schema/model extraction.
			// Emit a SCOPE.Schema model entity for @Document (Mongo) /
			// @Table (Cassandra) / @RedisHash (Redis) annotated classes,
			// CONTAINS-wired to the field children already emitted into the
			// [classIdx+1, len(*out)) region above. Runs before the Lombok /
			// Panache synthesizers so the field region holds only the real
			// extracted children, not synthesized members.
			emitNoSQLModel(node, file, rec.Name, classDeclSrc, classBodySrc,
				pkgName, classIdx+1, len(*out), out)
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

	case "annotation_type_element_declaration":
		// Issue #7073 — an annotation ELEMENT (`String value() default "";`)
		// parses as its own node kind, not as method_declaration, which is why
		// nothing downstream ever saw one. It is modelled as SCOPE.Schema/field,
		// the same shape this extractor already gives a record component
		// (#1935): a named, typed, defaultable attribute that carries data.
		//
		// It is deliberately NOT a SCOPE.Operation, but NOT for the reason the
		// first version of this change gave. That reason — "an annotation element
		// is never a call target" — is FALSE, and reading it back is the ordinary
		// reflection idiom:
		//
		//	Audited a = (Audited) c.getAnnotation(Audited.class);
		//	return a.value();
		//
		// The local-variable-receiver machinery (#4682) types `a` and emits
		// `Audited.value` as a CALLS target, which now BINDS onto this
		// SCOPE.Schema/field. On main that stub was harmlessly unmatched because
		// no entity of that name existed; minting the declaration is what turns
		// it into a real edge. That edge is accepted, not suppressed — it is the
		// only thing that answers "who reads @Audited's value" — and it is pinned
		// by TestJavaAnnotationType_7073_ElementIsACallTarget, which asserts the
		// edge, its binding, AND the kind it lands on.
		//
		// The real reason for Schema over Operation is the dead-code surface:
		// internal/mcp/dead_code.go's isLiveCodeKind excludes "schema" and admits
		// "operation", keyed on Kind with no subtype escape hatch. Most annotation
		// elements are read only by frameworks via reflection, invisible to this
		// graph — so as operations they would be reported as dead code almost
		// universally.
		//
		// The grammar exposes `name` and `type` fields on this node exactly as
		// interface_declaration exposes `name` / `body`, so buildField's sibling
		// helpers apply unchanged. The enclosing CONTAINS edge is emitted by the
		// generic SCOPE.Schema/field arm of the body loop above.
		if rec, ok := buildAnnotationElement(node, file, parentType); ok {
			*out = append(*out, rec)
		}

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
	body ts.Node,
	src []byte,
	callerName string,
	cc *classCtx,
	paramTypes map[string]string,
	imports map[string]bool,
) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	seen := make(map[string]bool)
	var rels []types.RelationshipRecord
	javaScopeCalls(body, src, callerName, cc, paramTypes, nil, imports, seen, &rels)
	if len(rels) == 0 {
		return nil
	}
	return rels
}

// javaScopeCalls emits the CALLS edges for ONE class scope rooted at
// scopeRoot, then recurses into each nested class scope with its own ledger
// (#7109).
//
// BEFORE #7109 this was one flat pass: `collectLocalVarTypes(body)` over the
// whole method body, merged with the method's parameters, consulted by every
// call site in it. A local or anonymous CLASS body is a descendant of that
// body but a DIFFERENT class scope, and its calls are attributed to the
// enclosing method entity, so the flat pass both (a) let a nested member's
// binder collide with an outer local — refusing BOTH under #7094's ledger, the
// recall cost TestJava7094_ClassBodyShadowingRecallRecoveredBy7109 recorded
// before this change recovered it — and (b)
// resolved a call INSIDE the nested body against the OUTER ledger, which is
// the wrong-receiver bug #7109 reports: a nested `formal_parameter` was in no
// ledger arm at all, so a sibling `Order o` owned the name and `o.b()` in the
// nested body came out `Order.b` — a WRONG dotted receiver on a real same-file
// type, which BINDS and which bind/orphan/dangle all score as a success
// (#7056).
//
// The ledger for a scope is, in increasing precedence:
//
//	inherited   the ENCLOSING scope's resolved ledger. Java capture: an
//	            effectively-final local of the enclosing method IS visible by
//	            bare name inside a local/anonymous class body, so dropping it
//	            would be a recall regression, not a fix
//	            (TestJava7109_CapturedOuterLocalStillBindsInside).
//	locals      this scope's own #7094/#7097/#7099/#7100 ledger, now walked
//	            with scopedFindNodes so it stops at the class boundary.
//	params      this scope's member's formal parameters. Params win over
//	            locals, unchanged from the flat form.
//
// Shadowing is therefore an OVERLAY rather than a collision: the outer name is
// not refused, it is replaced for the duration of the inner scope, which is
// what the language says happens.
//
// `seen` is shared across the whole recursion, as the single flat map was, so
// a target is still emitted once per caller entity.
//
// NOT FIXED HERE, recorded rather than claimed: receiverTypeName consults
// cc.fields BEFORE the ledger, so an ENCLOSING-class field still outranks a
// nested body's own local of the same name. That is the same precedence the
// flat form had, it is wrong for the same reason, and it is a separate change
// with its own grading obligation.
func javaScopeCalls(
	scopeRoot ts.Node,
	src []byte,
	callerName string,
	cc *classCtx,
	params, inherited map[string]string,
	imports map[string]bool,
	seen map[string]bool,
	rels *[]types.RelationshipRecord,
) {
	if scopeRoot == nil {
		return
	}
	// Issue #120 — local variables typed via explicit declarations
	// (`Owner owner = new Owner()`, `LocalDate today = LocalDate.now()`)
	// are bound to their declared leaf type so a follow-up
	// `owner.setName(...)` resolves to "Owner.setName".
	ledger := javaOverlayLedger(inherited, collectLocalVarTypes(scopeRoot, src), params)
	for _, call := range scopedFindNodes(scopeRoot, "method_invocation", "object_creation_expression") {
		target := javaCallTarget(call, src, cc, ledger, imports)
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
		*rels = append(*rels, types.RelationshipRecord{
			ToID:       target,
			Kind:       "CALLS",
			Properties: types.Props{{K: "line", V: callLine}},
		})
	}
	for _, cb := range scopedFindNodes(scopeRoot,
		"class_body", "interface_body", "enum_body") {
		javaClassBodyCalls(cb, src, callerName, cc, ledger, imports, seen, rels)
	}
}

// javaClassBodyCalls resolves the calls inside ONE nested class-scope body
// against that class's own ledger (#7109).
//
// The class scope contributes two binder families of its own, and BOTH are
// load-bearing: without them the inherited ledger still carries the outer
// local under the same name and the wrong dotted receiver survives the
// boundary cut.
//
//	field_declaration   TYPED, via the collectFieldTypes machinery the
//	                    enclosing class already uses. A field of an anonymous
//	                    class is bare-reachable from its methods and shadows
//	                    the captured local. MEASURED at 83004cbef: a
//	                    `Runnable(){ Cust o = new Cust(); public void run(){
//	                    o.b(); } }` beside an outer `Order o` emitted
//	                    `Order.b`.
//	enum_constant       POISONED (empty type). This is the `enum_constant`
//	                    entry the #7100 enumeration named and deferred to
//	                    "the anonymous/local-class-member gap", i.e. to here.
//	                    A local `enum E { Cust; void go(){ Cust.b(); } }`
//	                    beside an outer `Order Cust` emitted `Order.b` at
//	                    83004cbef. The constant's type IS the enum, but that
//	                    enum is a local type with no entity (walk returns at
//	                    method_declaration and never emits members of a
//	                    method-local class), so typing it would fabricate a
//	                    target; "" refuses instead, and the call falls back
//	                    through receiverTypeName's empty mask. An enum
//	                    constant starts with an uppercase letter by
//	                    convention, so the PascalCase static-call arm then
//	                    yields `Cust.b` — a target that DANGLES rather than
//	                    binding to the wrong real type, which is the
//	                    direction #7094 chose explicitly.
func javaClassBodyCalls(
	classBody ts.Node,
	src []byte,
	callerName string,
	cc *classCtx,
	inherited map[string]string,
	imports map[string]bool,
	seen map[string]bool,
	rels *[]types.RelationshipRecord,
) {
	if classBody == nil {
		return
	}
	own := map[string]string{}
	for name, typ := range collectFieldTypes(classBody, src) {
		own[name] = typ
	}
	// RECORD COMPONENTS are the one class-scope binder that does not live
	// inside the body at all: the grammar hangs them off the
	// record_declaration as `parameters: formal_parameters`, a SIBLING of the
	// body. They are bare-reachable from every member. MEASURED at 9fe0b2be5,
	// with the class boundary already in place: `record R(Cust o) { void go()
	// { o.b(); } }` beside an outer `Order o` still emitted `Order.b`, and so
	// did the compact-constructor form `record R(Cust o) { R { o.b(); } }` —
	// the boundary cut alone does not reach a name bound outside the body.
	if parent := classBody.Parent(); parent != nil && parent.Type() == "record_declaration" {
		for name, typ := range collectParamTypes(parent, src) {
			own[name] = typ
		}
	}
	for i := 0; i < int(classBody.ChildCount()); i++ {
		ch := classBody.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "constant_declaration":
			// An INTERFACE field is a `constant_declaration`, NOT a
			// `field_declaration` — a different node with the same
			// type/variable_declarator shape, so collectFieldTypes cannot see
			// it. DERIVED from a parse dump, and MEASURED at 9fe0b2be5:
			// `interface I { Cust o = new Cust(); default void go() {
			// o.b(); } }` beside an outer `Order o` still emitted `Order.b`.
			for name, typ := range javaDeclaratorTypes(ch, src) {
				own[name] = typ
			}
		case "enum_constant":
			if name := childFieldText(ch, "name", src); name != "" {
				own[name] = ""
			}
		case "enum_body_declarations":
			// An enum's methods and fields sit one level deeper, in
			// `enum_body_declarations`, but belong to the SAME class scope
			// as the constants.
			for name, typ := range collectFieldTypes(ch, src) {
				own[name] = typ
			}
		}
	}
	ledger := javaOverlayLedger(inherited, own, nil)
	javaClassMemberCalls(classBody, src, callerName, cc, ledger, imports, seen, rels)
}

// javaClassMemberCalls walks the members of one class scope, giving each
// method / constructor its own parameter frame on top of the class ledger.
// `enum_body_declarations` is transparent: its children are members of the
// same class scope, not a nested one (#7109).
func javaClassMemberCalls(
	container ts.Node,
	src []byte,
	callerName string,
	cc *classCtx,
	ledger map[string]string,
	imports map[string]bool,
	seen map[string]bool,
	rels *[]types.RelationshipRecord,
) {
	for i := 0; i < int(container.ChildCount()); i++ {
		m := container.Child(i)
		if m == nil {
			continue
		}
		switch m.Type() {
		case "method_declaration", "constructor_declaration", "compact_constructor_declaration":
			javaScopeCalls(m.ChildByFieldName("body"), src, callerName, cc,
				collectParamTypes(m, src), ledger, imports, seen, rels)
		case "enum_body_declarations":
			javaClassMemberCalls(m, src, callerName, cc, ledger, imports, seen, rels)
		default:
			// Field initialisers, instance/static initialiser blocks,
			// enum-constant arguments and constant-specific bodies, and
			// member types nested one level further down. No parameter
			// frame of their own.
			javaScopeCalls(m, src, callerName, cc, nil, ledger, imports, seen, rels)
		}
	}
}

// javaDeclaratorTypes reads a declaration node of the shape
// `type: <type>, declarator: variable_declarator{name}+` — an interface
// `constant_declaration`, whose node kind differs from `field_declaration`
// even though the shape is identical — and returns name → leaf type for every
// declarator. Multi-declarator constants (`Cust p = …, q = …;`) bind every
// name (#7109).
func javaDeclaratorTypes(decl ts.Node, src []byte) map[string]string {
	if decl == nil {
		return nil
	}
	typ := leafTypeName(decl.ChildByFieldName("type"), src)
	if typ == "" {
		return nil
	}
	var out map[string]string
	for i := 0; i < int(decl.ChildCount()); i++ {
		d := decl.Child(i)
		if d == nil || d.Type() != "variable_declarator" {
			continue
		}
		name := childFieldText(d, "name", src)
		if name == "" {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[name] = typ
	}
	return out
}

// javaOverlayLedger layers three name→type maps in increasing precedence and
// returns the result. It never mutates its arguments, and returns `base`
// itself when both overlays are empty so the common no-locals no-params case
// allocates nothing — the same shortcut the pre-#7109 flat merge had.
func javaOverlayLedger(base, mid, top map[string]string) map[string]string {
	if len(mid) == 0 && len(top) == 0 {
		return base
	}
	out := make(map[string]string, len(base)+len(mid)+len(top))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range mid {
		out[k] = v
	}
	// params win over locals — a loop-local that shadows a parameter must not
	// change the parameter's type for the rest of the method.
	for k, v := range top {
		out[k] = v
	}
	return out
}

// javaCallTarget resolves the callee target from a method_invocation or
// object_creation_expression node. Issue #120 — for method_invocation
// the receiver (object field) is consulted to produce a dotted
// "<Type>.<method>" target whenever the receiver's type is statically
// determinable from field declarations, parameter types, or PascalCase
// shape. Falls back to the bare leaf name when no receiver type is
// known.
func javaCallTarget(
	call ts.Node,
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
		// The LEAF type identifier is taken, so `new com.x.XController()`
		// emits "XController.XController" and not "com.com" (#7096). The
		// leaf is read structurally by leafTypeName — descending the
		// generic / array / scoped shapes by the node's own children —
		// rather than by indexing a flattened findAllNodes result, whose
		// ordering is an implementation artefact of that walk.
		//
		// Falls back to the raw type text when leafTypeName cannot
		// characterise the node (defensive — keeps the previous behaviour
		// for malformed parses).
		className := leafTypeName(typ, src)
		if className == "" {
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
	obj ts.Node,
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
func collectFieldTypes(body ts.Node, src []byte) map[string]string {
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
func collectParamTypes(node ts.Node, src []byte) map[string]string {
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
// body and returns a map of local-variable-name → declared leaf type.
// Used by the receiver binder so calls like
// `Owner owner = new Owner(); owner.setId(...)` resolve to "Owner.setId".
//
// TWO of the binders it sees are TYPED — `local_variable_declaration` and
// `enhanced_for_statement`. Every OTHER binder is recorded with an empty type,
// which poisons its name rather than typing it; the arms are listed where they
// are implemented below, and the COMPLETE enumeration of Java's name-binding
// nodes — including the ones that need no arm and the one gap deliberately
// left open — sits with the pattern arms at the end of this function (#7100).
// This paragraph must not claim a shorter ledger than the code has: the
// previous revision named only these two long after #7097 added three more.
//
// Variable declarations using `var` (Java 10+) are typed only when the
// initialiser is a direct `new ClassName(...)`; anything else leaves the
// name untyped (see the declarator loop). Multi-declarator declarations
// bind every variable to the declared type.
//
// AMBIGUOUS NAMES ARE REFUSED, NOT GUESSED (#7094). findAllNodes is a FLAT
// descendant walk and this map is keyed by BARE NAME, so the pass has no
// model of block scope at all. Two locals of the same name in SIBLING blocks
//
//	{ Order o = new Order();       o.a(); }
//	{ Customer o = new Customer(); o.b(); }
//
// both write `o`, and before this change one of them simply won — MEASURED as
// the first-in-source declarator, because findAllNodes is a stack DFS that
// pops siblings right-to-left, so the earlier declaration overwrites the later
// one. Which one wins is an artefact of the traversal, not a decision. The
// loser's call then carried a receiver type that name never had at that site:
// `o.b()` in the second block came out as `Order.b`. Because the winner is a
// REAL same-file type the dotted edge BINDS, so bind rate, orphan rate and
// dangle count all score it as a success — the #7056 signature, invisible to
// every metric that would otherwise catch it.
//
// The rule is therefore the one the C# twin adopted in #7072: when a name maps
// to two DIFFERENT types, drop the name entirely and let its calls fall back
// to their bare leaf. A dotted target leaves the resolver's bare-name class
// and is scored as a confident bind (#7071), so a WRONG dotted receiver is
// that same hazard rather than a lesser one — a fabricated `var.b` at least
// dangles, while `Order.b` on a real Order does not.
//
// AN UNTYPED DECLARATION POISONS THE NAME TOO. "We could not type this
// declaration" is not "no declaration happened here". A `var` whose initialiser
// is not a direct `new` (a factory call) produces no type, and it used to skip
// the name outright — MEASURED emitting `Order.b` on the sibling's type, in
// BOTH source orders. It now enters the ledger instead.
//
// C#'s twin had a SECOND shape here: a declared type leafTypeName has no case
// for (a `ref` local). NO JAVA INSTANCE OF THAT SHAPE WAS FOUND. leafTypeName's
// switch covers every `_unannotated_type` the grammar produces, and a leading
// annotation is hoisted into the declaration's modifiers rather than producing
// an `annotated_type` type field — `@NonNull Customer o`, `final Customer o`,
// `Customer @NonNull [] o`, `List<@A Customer> o` and `@NonNull var o` were all
// measured reducing to a non-empty leaf. An empty declType is therefore routed
// through the ledger defensively and that arm is UNGRADED: nothing was found
// that reaches it. Recorded rather than claimed.
//
// Such names enter the ledger with an EMPTY type, which disagrees with
// every real type, so the refusal holds in either source order. Publishing an
// empty type is inert on its own: receiverTypeName masks empty lookups
// (`ok && t != ""`) and javaCallTarget falls back to the bare method when the
// receiver comes back empty — which is what TestIssue4682_NegativeFactoryReceiver
// grades. Hence no `typ == ""` arm in the publish loop: it would fire only
// where the real guard already fires and would leave both ungraded.
//
// SAME-NAME/SAME-TYPE IS NOT A COLLISION. Two blocks each declaring `Order o`
// agree on the answer, so refusing them would be pure recall loss for no
// soundness gain; the ledger compares TYPES, not names.
//
// THE ENHANCED-FOR ARM IS IN THE SAME LEDGER, which is where this differs from
// the C# fix. C# has two refusal mechanisms over one map because its `foreach`
// arm already had an ambiguity ledger and a claimed-elsewhere word list of its
// own. Java's loop-variable arm had NEITHER: it was a bare `out[name] = typ`
// running AFTER the declaration walk, so it overwrote unconditionally —
// MEASURED, `for (Customer o : cs)` beside a local `Order o` bound BOTH sites
// to Customer. One ledger fed by both arms is therefore the right shape here,
// and it is also simpler: nothing reads `out` mid-walk any more.
//
// THE TWO-STAGE FORM IS LOAD-BEARING. A one-stage delete-on-collision against
// `out` cannot work, because the ambiguous SET is the only thing that
// remembers a name ONCE disagreed. Deleting the entry destroys that memory, so
// a third declarator agreeing with the first finds no entry, sees no conflict
// and RESURRECTS the wrong binding for the middle one. That is graded, not
// asserted: TestJava7094_AmbiguityIsStickyAcrossAnAgreeingRedeclaration kills
// the one-stage form.
//
// THE WALK IS SCOPED AT CLASS BOUNDARIES (#7109) — scopedFindNodes, not
// findAllNodes. Everything above describes ONE class scope, and that is now
// what this function sees: it does not descend into a `class_body` /
// `interface_body` / `enum_body` / `annotation_type_body`, so a name bound
// inside a local or anonymous class is not in this ledger at all.
// (`annotation_type_body` is not a boundary — javac rejects an annotation-type
// declaration anywhere under a method body, so nothing can reach it.)
//
// WHY, since it was deliberately not done for two rounds. An earlier revision
// of this comment said Java "forbids an inner block from redeclaring a name
// already in scope, so a compilable program cannot contain that case". That is
// FALSE, and it was load-bearing — it is how a reader concludes there is
// nothing here to grade. JLS §6.4 restricts redeclaration only within the
// DIRECTLY ENCLOSING method, constructor or initializer block; a local or
// anonymous CLASS BODY is a new class scope and may legally shadow. The calls
// inside such a body are attributed to the enclosing method entity, so a flat
// walk reached straight across the class boundary:
//
//	Order o = new Order();
//	o.a();                                   // was bare `a`, now Order.a
//	Runnable r = new Runnable() {
//	  public void run() { Customer o = new Customer(); o.b(); }
//	};
//
// The recall loss that cost (both receivers) was recorded as a FIXTURE rather
// than as prose, and that fixture named this fix and predicted its own red:
// TestJava7094_ClassBodyShadowingRecallRecoveredBy7109 now asserts the
// recovery it anticipated.
//
// REFUSAL WAS NEVER THE GOAL HERE, only the answer available without a scope
// model. Across a class boundary the two declarations do not disagree about
// anything — they are different variables — so refusing them was not the
// #7094 "do not guess between two real types" judgement, it was a flat walk
// mistaking two scopes for one. Inside one class scope that judgement stands
// unchanged, which is why the statement-level binders below still poison.
//
// WHAT THIS IS NOT: a general block-scoped symbol table. Statement scopes
// (block, for, enhanced_for, try-with-resources, catch clause, switch block,
// lambda body) are still one flat map, and #7094's refusal is still how a
// collision between them is answered. A real per-site table would push a frame
// at each of those and resolve every call against the stack LIVE at that
// site's position; nothing here blocks it, since a per-site table simply stops
// consulting this ledger. #7109 deliberately stopped at the class boundary
// because that is where the language stops too.
//
// BOTH DIRECTIONS ARE GRADED BY FIXTURES. A collision guard that never fires
// and one that always fires are indistinguishable on a corpus where the
// incidence is near zero, so the fixtures pin both.
func collectLocalVarTypes(body ts.Node, src []byte) map[string]string {
	if body == nil {
		return nil
	}
	// Two-stage ledger: candidates accumulate here and only unambiguous
	// names are published to `out`. See the stickiness note above for why
	// the ambiguous set cannot be replaced by deleting from `out`.
	cand := map[string]string{}
	ambiguous := map[string]bool{}
	// record notes one binding of name→typ. typ == "" means "bound here,
	// type unknown", which disagrees with every real type. The ambiguity
	// flag is STICKY — a later declarator agreeing with the first must not
	// clear it, since the disagreeing one is still out there.
	record := func(name, typ string) {
		if name == "" {
			return
		}
		if prev, seen := cand[name]; seen && prev != typ {
			ambiguous[name] = true
			return
		}
		cand[name] = typ
	}
	for _, decl := range scopedFindNodes(body, "local_variable_declaration") {
		declType := leafTypeName(decl.ChildByFieldName("type"), src)
		// `var` (Java 10+) carries no declared leaf type. Mirror the TS/JS
		// (#4680) and Python (#4716) local-receiver wins: when the
		// initialiser is a direct `new ClassName(...)` we infer the local's
		// type from the constructed class so a follow-up `localName.method()`
		// in a `@Test` method resolves to the class method (the dominant
		// modern-JUnit idiom `var controller = new XController(mock);`).
		// Any other RHS — a factory/builder call (`MyFactory.create()`), a
		// method chain, a cast, a literal — leaves the `var` local
		// unresolved. An empty declType (a type node leafTypeName has no
		// case for, e.g. an `annotated_type`) takes the same route.
		//
		// READ "unresolved" AT METHOD SCOPE, NOT DECLARATION SCOPE (#7094):
		// an unresolved declarator still reaches the ledger below, so it
		// poisons its name rather than ceding it to a same-name sibling.
		isVar := declType == "var" || declType == ""
		for i := 0; i < int(decl.ChildCount()); i++ {
			ch := decl.Child(i)
			if ch == nil || ch.Type() != "variable_declarator" {
				continue
			}
			name := childFieldText(ch, "name", src)
			if name == "" {
				continue
			}
			typ := declType
			if isVar {
				// May be "" when the RHS defeats inference. That empty
				// result must NOT skip the ledger — see above.
				typ = newExprClassName(ch.ChildByFieldName("value"), src)
			}
			record(name, typ)
		}
	}
	// `enhanced_for_statement` (`for (Owner o : owners) { ... }`) — bind
	// the loop variable to its declared type so calls inside the body can
	// be receiver-typed. Same ledger as the declaration walk: this arm used
	// to overwrite `out` unconditionally.
	for _, fr := range scopedFindNodes(body, "enhanced_for_statement") {
		record(childFieldText(fr, "name", src),
			leafTypeName(fr.ChildByFieldName("type"), src))
	}
	// THREE MORE BINDERS, LEDGER-ONLY (#7097). Each of the following binds a
	// name in a nested scope, and none of them typed a receiver before this
	// change — so a same-name sibling local silently owned the name and its
	// type was applied to call sites it never covered. MEASURED on compilable
	// Java (the sibling `Order o` lives in its own block, so there is no JLS
	// §6.4 conflict): all three emitted `Order.b` for the inner `o.b()`, a
	// WRONG receiver on a real same-file type, which binds and which every
	// bind/orphan/dangle metric scores as a success (#7056).
	//
	// They are recorded with an EMPTY type — "bound here, type unknown" —
	// which disagrees with every real type and therefore poisons the name,
	// exactly as an untyped `var` declarator does. That is deliberately NOT
	// the same as typing them, and the standalone controls
	// (TestJava7097_*AloneUnchanged) pin that: each construct alone still
	// emits the bare leaf, as it did before. Per construct:
	//
	//	lambda parameter    genuinely untypeable in the dominant form
	//	                    (`o -> o.b()`, and `(o, p) -> …`): the grammar
	//	                    carries no type at all. "" is the only honest
	//	                    entry, so there is nothing to decide here.
	//	catch parameter     a declared type exists, but `catch_type` is a
	//	                    UNION (`catch (A | B e)`) whose leftmost arm is
	//	                    not "the" type; picking one is the guess #7094
	//	                    refused to make, and leafTypeName has no
	//	                    catch_type case. "" until a decision covers the
	//	                    union.
	//	try-with-resources  a single declared leaf type IS derivable here,
	//	                    so this is the one arm where typing is possible
	//	                    rather than a guess. It is still recorded as ""
	//	                    because typing it is a RECALL ADDITION with its
	//	                    own grading obligation, not part of removing a
	//	                    wrong bind, and this diff stays disjoint from
	//	                    #7091/#7096. The cost of that choice is not
	//	                    prose: TestJava7097_TryWithResourcesSameTypeCost
	//	                    OBSERVES the recall a same-type sibling loses to
	//	                    the poison, so the fixture moves when the
	//	                    behaviour does.
	//
	// Node spellings are DERIVED from the grammar by dumping a parse tree,
	// not assumed — a matcher naming a node the grammar lacks is a silent
	// no-op. `go run ./tools/node-type-gate` is the standing check.

	// `try (Customer o = new Customer()) { … }` — resource_specification
	// holds `resource` children with `type`/`name`/`value` fields. A resource
	// that is a plain existing variable (`try (existing) { … }`) has NO
	// `name` field, so childFieldText yields "" and record no-ops: that form
	// binds nothing and must not poison the outer name.
	for _, res := range scopedFindNodes(body, "resource") {
		record(childFieldText(res, "name", src), "")
	}
	// `catch (MyEx o) { … }` — catch_clause holds a catch_formal_parameter
	// whose `name` field is the bound identifier (its type sits in an
	// unnamed `catch_type` child, not a `type` field).
	for _, cfp := range scopedFindNodes(body, "catch_formal_parameter") {
		record(childFieldText(cfp, "name", src), "")
	}
	// `cs.forEach(o -> o.b())` — lambda_expression's `parameters` field is
	// one of three shapes: a bare `identifier` (single inferred parameter),
	// `inferred_parameters` (`(o, p) -> …`), or `formal_parameters`
	// (`(Customer o) -> …` / `(Customer... o) -> …`, the only typed shapes).
	// Every binder each shape can hold is poisoned — which is what #7099's
	// version of this comment claimed and did not do. `formal_parameters`
	// also admits a `spread_parameter` (a VARARGS lambda parameter), whose
	// name does NOT sit in a `name` field: the grammar gives
	// `spread_parameter → _unannotated_type, variable_declarator{name}`, and
	// that variable_declarator is NOT reachable from the
	// `local_variable_declaration` walk above, so the old
	// `p.Type() == "formal_parameter"` guard dropped it entirely. MEASURED at
	// 1a6a134f3, both before and after #7099: `use((Customer... o) ->
	// o.toString())` beside an `Order o` sibling emitted `Order.toString` — a
	// wrong receiver on a real same-file type, which binds (#7056). The
	// receiver is an ARRAY, so only `Object` methods are callable on it, which
	// bounds the harm to overridden `Object` members — and makes a same-type
	// sibling's bind wrong TOO, unlike the three pattern constructs below
	// (TestJava7102_VarargsSameTypeSiblingWasAlsoWrong). The
	// MIXED form `(Customer a, Object... o) -> …` puts a `formal_parameter`
	// and a `spread_parameter` in the SAME node, so both branches of the
	// switch below run for one lambda; it is graded separately from the pure
	// varargs form (#7102). `receiver_parameter` — the third thing
	// `formal_parameters` admits — binds `this`, not a name, so there is
	// nothing to record for it.
	for _, lam := range scopedFindNodes(body, "lambda_expression") {
		params := lam.ChildByFieldName("parameters")
		if params == nil {
			continue
		}
		switch params.Type() {
		case "identifier":
			record(strings.TrimSpace(string(src[params.StartByte():params.EndByte()])), "")
		case "inferred_parameters":
			for i := 0; i < int(params.NamedChildCount()); i++ {
				p := params.NamedChild(i)
				if p != nil && p.Type() == "identifier" {
					record(strings.TrimSpace(string(src[p.StartByte():p.EndByte()])), "")
				}
			}
		case "formal_parameters":
			for i := 0; i < int(params.NamedChildCount()); i++ {
				p := params.NamedChild(i)
				if p == nil {
					continue
				}
				switch p.Type() {
				case "formal_parameter":
					record(childFieldText(p, "name", src), "")
				case "spread_parameter":
					// No `name` field here — the binder is the
					// variable_declarator child's `name` (#7102).
					for j := 0; j < int(p.NamedChildCount()); j++ {
						d := p.NamedChild(j)
						if d != nil && d.Type() == "variable_declarator" {
							record(childFieldText(d, "name", src), "")
						}
					}
				}
			}
		}
	}
	// PATTERN BINDERS, LEDGER-ONLY (#7100). Java's pattern-matching forms bind
	// a name too, and none of them was in the ledger. All three carry a
	// declared type in source, so — like the try-with-resources arm — typing
	// them is POSSIBLE; they are still recorded with an EMPTY type, because
	// typing a binder is a recall addition with its own grading obligation and
	// is not part of removing a wrong bind (#7099's reasoning, applied per
	// construct). MEASURED at 1a6a134f3, each beside a sibling `{ Order o =
	// new Order(); o.a(); }`, with the pattern variable's call INSIDE the
	// binder's own scope: every one emitted `Order.b` — a wrong dotted
	// receiver on a real same-file type, which binds and which bind/orphan/
	// dangle all score as a success (#7056). Each ALONE emits the bare leaf,
	// as the *AloneUnchanged fixtures pin.
	//
	// Spellings DERIVED from a parse dump of compilable Java, not assumed
	// (`go run ./tools/node-type-gate` is the standing check that a matcher
	// does not name a node the grammar lacks — a silent no-op reads as a pass):
	//
	//	instanceof pattern variable   instanceof_expression's `name` FIELD
	//	                              (`x instanceof Customer o`). Optional in
	//	                              the grammar: a plain `x instanceof
	//	                              Customer` has no `name`, so record
	//	                              no-ops and binds nothing.
	//	switch type pattern           switch_label → pattern → type_pattern,
	//	                              binder as an `identifier` CHILD (no
	//	                              field). Covers BOTH label forms — arrow
	//	                              (`case Customer o -> …`, switch_rule)
	//	                              and colon (`case Customer o: …`,
	//	                              switch_block_statement_group) — and the
	//	                              guarded form (`case Customer o when
	//	                              o.ok() -> …`), because the flat walk
	//	                              matches the type_pattern directly rather
	//	                              than routing through the label.
	//	record deconstruction         record_pattern_body →
	//	                              record_pattern_component, binder as an
	//	                              `identifier` CHILD. Reached from BOTH
	//	                              `instanceof` (the `pattern` field, which
	//	                              is the alternative to `name`) and a
	//	                              switch label, and NESTED patterns
	//	                              (`Pair(Point(Customer o, …), …)`) are
	//	                              reached for free because the components
	//	                              are descendants.
	//
	// THE RECORD-PATTERN ARM MUST NOT MATCH `record_pattern` ITSELF: that node
	// carries the record's TYPE name as its own `identifier` child (`Pair`),
	// so poisoning it would refuse the type name, not the binder. Only
	// `record_pattern_component` is matched, and the component's type is
	// always an `_unannotated_type` (type_identifier / scoped_type_identifier
	// / generic_type / array_type, plus `type_identifier "var"` for
	// `Pair(var o, …)`) and NEVER an `identifier` — verified against the
	// grammar's `_unannotated_type` supertype — which is what makes "the
	// identifier child" an unambiguous reading of the binder in both
	// patternBinderName callers.
	// The `var` spelling is not merely asserted: it is the one that decides
	// which child patternBinderName returns, so it is GRADED by
	// TestJava7100_RecordPatternVarComponentCollisionRefuses — were `var` an
	// `identifier`, the binder would go unpoisoned and the sibling would win.
	for _, ie := range scopedFindNodes(body, "instanceof_expression") {
		record(childFieldText(ie, "name", src), "")
	}
	for _, tp := range scopedFindNodes(body, "type_pattern") {
		record(patternBinderName(tp, src), "")
	}
	for _, rc := range scopedFindNodes(body, "record_pattern_component") {
		record(patternBinderName(rc, src), "")
	}
	// ENUMERATED AND DELIBERATELY NOT HERE (#7100's own recommendation was to
	// stop finding these one pair at a time and enumerate the grammar). Every
	// other name-binding node reachable inside a method body, and why it needs
	// no arm:
	//
	//	receiver_parameter        `void m(Svc this)` binds `this`, not a name,
	//	                          and it sits in the method's OWN
	//	                          formal_parameters, which are a sibling of
	//	                          `body` rather than a descendant.
	//	underscore_pattern        the JLS 22 unnamed variable binds nothing
	//	                          referenceable, so it can never be a
	//	                          receiver. (In this grammar version a
	//	                          `case Customer _` label actually yields
	//	                          `identifier "_"`, which the type_pattern arm
	//	                          records; poisoning `_` is inert for the same
	//	                          reason.)
	//	labeled_statement         its `identifier` is a LABEL — a separate
	//	                          namespace that no expression can receive on.
	//	type_parameter            binds a type name, not a variable.
	//	local class / interface / enum / record DECLARATION names — likewise
	//	                          type names.
	//	enum_constant             the one entry in this list that binds a VALUE
	//	                          name rather than a type name, so it is named
	//	                          explicitly rather than left out of a list that
	//	                          claims to be complete. A local `enum E {
	//	                          Customer }` inside a method body declares
	//	                          constants, and inside that enum's OWN body a
	//	                          constant is reachable bare — so `Customer.b()`
	//	                          there could take a sibling local's type. It
	//	                          needs no arm HERE, and now for a reason that is
	//	                          graded rather than deferred: the enum's body is
	//	                          a class scope this walk stops at, and the
	//	                          constant is bound in THAT scope's ledger by
	//	                          javaClassBodyCalls (#7109). From this method's
	//	                          own scope the constant must be qualified
	//	                          (`E.Customer`), and a qualified receiver never
	//	                          reaches this ledger under the bare name.
	//	                          TestJava7109_LocalEnumConstantOwnsItsName.
	//
	// THE GAP THAT USED TO BE RECORDED HERE IS CLOSED (#7109): members of an
	// ANONYMOUS or LOCAL CLASS declared inside this body. A `new Go() { public
	// void go(Customer o) { o.b(); } }` beside a sibling `Order o` emitted
	// `Order.b` at 83004cbef — the #7056 signature — because its
	// `formal_parameter` is a descendant of this body but belongs to a
	// different CLASS scope, and no arm here bound it. It is not an arm here
	// now either: scopedFindNodes stops before it, and javaScopeCalls resolves
	// that body's calls against its own ledger. Nested class members are
	// therefore OUT of this function's remit entirely, which is why no
	// enumeration entry above needs to cover them.
	out := map[string]string{}
	for name, typ := range cand {
		if ambiguous[name] {
			continue
		}
		out[name] = typ
	}
	return out
}

// patternBinderName returns the name a Java pattern node binds: the text of its
// first `identifier` named child. Used by collectLocalVarTypes for `type_pattern`
// (`case Customer o -> …`) and `record_pattern_component`
// (`case Pair(Customer o, …)`), whose binder sits in an UNNAMED child rather
// than a `name` field — so ChildByFieldName("name") returns nil for both and a
// field-based reading is a silent no-op (#7100).
//
// Taking the first `identifier` is unambiguous because the sibling that could
// be confused with it — the component's or pattern's declared type — is always
// an `_unannotated_type` (type_identifier, scoped_type_identifier, generic_type,
// array_type, or a primitive), and `identifier` is not a member of that
// supertype. `var` in a record pattern arrives as `type_identifier "var"`, so it
// does not shift which child is the binder — OBSERVED by
// TestJava7100_RecordPatternVarComponentCollisionRefuses, not asserted.
func patternBinderName(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if c != nil && c.Type() == "identifier" {
			return strings.TrimSpace(string(src[c.StartByte():c.EndByte()]))
		}
	}
	return ""
}

// newExprClassName returns the constructed class name when value is a direct
// `new ClassName(...)` object_creation_expression, or "" for any other
// initialiser shape. Used to type `var` locals (#4682, mirroring TS/JS #4680
// and Python #4716): only a bare construction is trusted; factory/builder
// calls, casts, chains, ternaries and literals stay unresolved so a `var`
// receiver never types to a non-constructed class. The LEAF type identifier
// is taken (so `new com.x.XController(...)` → "XController", matching
// javaCallTarget's object-creation handling), read structurally via
// leafTypeName; taking the last element of a flattened findAllNodes result
// yielded the PACKAGE ROOT, so a qualified `var` typed to "com" (#7096).
func newExprClassName(value ts.Node, src []byte) string {
	if value == nil || value.Type() != "object_creation_expression" {
		return ""
	}
	typ := value.ChildByFieldName("type")
	if typ == nil {
		return ""
	}
	if name := leafTypeName(typ, src); name != "" {
		return name
	}
	return strings.TrimSpace(string(src[typ.StartByte():typ.EndByte()]))
}

// leafTypeName returns the leaf type identifier of a Java type node,
// stripping generic parameters and array suffixes. `List<Owner>`
// yields "List"; `Map<String, Owner>` yields "Map"; `Owner[]` yields
// "Owner"; `int` yields "int". Returns "" for type nodes the function
// can't characterise.
func leafTypeName(typ ts.Node, src []byte) string {
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
		// `com.foo.Bar` — the leaf is the node's LAST NAMED CHILD. The
		// grammar is `seq(qualifier, '.', repeat(annotation), type_identifier)`
		// (scoped_type_identifier declares no fields, so there is no
		// ChildByFieldName to ask), which makes the trailing
		// type_identifier the last named child even when the segment
		// carries annotations — `com.foo.@Ann Bar` still yields "Bar".
		//
		// This used to index a flattened findAllNodes result at
		// len-1 while calling that "the rightmost type_identifier".
		// findAllNodes is a stack DFS that pushes children in index order
		// and pops from the end, so its result is in REVERSE source order
		// and len-1 is the LEFTMOST segment: `com.foo.Bar` reduced to
		// "com", and a qualified receiver was then named after its
		// package root (`com.getCounts`) — a plausible dotted target that
		// binds confidently and wrong (#7096). Nothing pinned that
		// ordering, so the dependency on it is removed rather than
		// inverted.
		//
		// The child's text is read directly, with no test of its kind and
		// no recursion: the grammar puts a type_identifier last in EVERY
		// scoped shape it can produce (a generic or scoped qualifier sits
		// before the dot, annotations before the identifier), so both a
		// kind test and a recursive reduction would be arms nothing can
		// reach — and an arm nothing can reach is an arm no fixture grades.
		if n := int(typ.NamedChildCount()); n > 0 {
			ch := typ.NamedChild(n - 1)
			return strings.TrimSpace(string(src[ch.StartByte():ch.EndByte()]))
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
func collectImportNames(root ts.Node, src []byte) map[string]bool {
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
func collectPackageName(root ts.Node, src []byte) string {
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

// javaClassScopeBody is the set of tree-sitter-java node types that open a
// NEW CLASS SCOPE. A name bound inside one of these is a member (or a local of
// a member) of a DIFFERENT class than the method the node sits in, so it is
// invisible to that method's bare-name lookups and must not enter its ledger
// (#7109).
//
// Reached inside a method body by five source forms, all of which funnel into
// one of these four nodes — DERIVED from parse dumps of compilable Java, not
// assumed:
//
//	new Go() { … }            object_creation_expression → class_body
//	class Inner { … }         class_declaration          → class_body
//	record R(int x) { … }     record_declaration         → class_body
//	interface I { … }         interface_declaration      → interface_body
//	enum E { … }              enum_declaration           → enum_body
//
// `annotation_type_body` is DELIBERATELY ABSENT, and that is a measured claim
// rather than an omission: javac 25.0.3 rejects an annotation-type declaration
// ("annotation interface declaration not allowed here") in all four places
// reachable from a method body — directly in the body, inside a local class,
// inside a local interface, and inside an anonymous class body — so no
// compilable Java can put one under the roots these helpers walk. A boundary
// for it would be code no fixture can reach; it was in the set for one round
// and its mutant was necessarily ALIVE, so it is gone instead of ungraded.
//
// WHAT IS DELIBERATELY *NOT* HERE — these are the permissive direction, and
// they must keep binding exactly as they did before #7109:
//
//	block                  a nested statement block is the SAME class scope;
//	                       JLS §6.4 even forbids redeclaring the method's own
//	                       locals there. #7094's sibling-collision refusal
//	                       lives on this node and must survive.
//	lambda_expression      a lambda body is not a class body; its parameter is
//	                       a #7099 ledger arm and stays one.
//	switch_block / for /   all statement scopes, all in the same class.
//	try / catch_clause
//
// A boundary placed on any of those would stop the walk TOO EARLY and silently
// re-open #7094 / #7097 / #7099 / #7100, which is why each has a fixture.
var javaClassScopeBody = map[string]bool{
	"class_body":     true,
	"interface_body": true,
	"enum_body":      true,
}

// scopedFindNodes is findAllNodes restricted to ONE class scope: it returns
// every descendant of root whose Type() is in kinds, WITHOUT descending into a
// nested class-scope body (#7109). A matching class-scope body is itself
// returned — that is how the caller finds the boundaries to recurse into — but
// its contents are not searched.
//
// root itself is never matched. That is EQUIVALENT to matching it at every
// current call site, and the enumeration is what makes that a fact rather than
// a hope — a mutant that adds `if set[root.Type()] { out = append(out, root) }`
// is ALIVE, so it is recorded here rather than left as a silently untested
// line. The three call sites and the kinds each asks for:
//
//	collectLocalVarTypes(body)   local_variable_declaration,
//	                             enhanced_for_statement, resource,
//	                             catch_formal_parameter, lambda_expression,
//	                             instanceof_expression, type_pattern,
//	                             record_pattern_component
//	javaScopeCalls (calls)       method_invocation, object_creation_expression
//	javaScopeCalls (boundaries)  class_body, interface_body, enum_body
//
// and every root passed in is a method/constructor body (`block` /
// `constructor_body`), a DIRECT CHILD of a class-scope body (a member
// declaration, a field declaration, an initialiser block, an enum constant),
// or an `enum_body_declarations`. None of those is a member of any of the
// three kind sets, so the extra match could never fire.
//
// The non-matching form is kept because it is the SAFE one: were a class-scope
// body ever passed as root, matching it would make javaScopeCalls recurse into
// the scope it is already resolving.
func scopedFindNodes(root ts.Node, kinds ...string) []ts.Node {
	if root == nil {
		return nil
	}
	set := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		set[k] = true
	}
	var out []ts.Node
	var stack []ts.Node
	for i := 0; i < int(root.ChildCount()); i++ {
		stack = append(stack, root.Child(i))
	}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		if set[n.Type()] {
			out = append(out, n)
		}
		if javaClassScopeBody[n.Type()] {
			// New class scope — its names belong to a different ledger.
			continue
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return out
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

// buildComponent creates a Component entity for class/interface declarations.
//
// Issue #1917 — QualifiedName is set to "<package>.<ClassName>" when pkgName
// is non-empty, giving inspect consumers a fully-qualified type reference.
func buildComponent(node ts.Node, file extractor.FileInput, subtype, pkgName string) (types.EntityRecord, bool) {
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
func buildOperation(node ts.Node, file extractor.FileInput, subtype, parentType, pkgName string) (types.EntityRecord, bool) {
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
func buildField(node ts.Node, file extractor.FileInput, parentType string) (types.EntityRecord, bool) {
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

	rec := types.EntityRecord{
		Name:       emittedName,
		Kind:       "SCOPE.Schema",
		Subtype:    "field",
		SourceFile: file.Path,
		Language:   "java",
		StartLine:  int(node.StartPoint().Row) + 1,
		EndLine:    int(node.EndPoint().Row) + 1,
		Signature:  fieldSig,
	}
	// Issue #6912 arm E — stash the declared type's AST node so
	// attachJavaFieldTypeRefs can emit the field -> type REFERENCES edge once
	// the file's full record set exists. The `type` child is the node
	// buildFieldSignature already spans as text; leafTypeName is deliberately
	// NOT reused here (it reduces a type expression to one leaf, which is the
	// lossy shape arm C refused).
	stashJavaFieldTypeRefs(&rec, node.ChildByFieldName("type"), file.Content, parentType)
	return rec, true
}

// buildAnnotationElement creates a SCOPE.Schema/field entity for one
// annotation_type_element_declaration (issue #7073).
//
// The node's `name` field is the element identifier and its `type` field is the
// declared type; both are the same field names interface/class declarations and
// field_declaration use, so the signature and field-type-ref helpers are shared
// rather than duplicated. Like every other member of a Java type, the emitted
// Name is qualified by the immediate enclosing type ("<Annotation>.<element>")
// so ComputeID separates same-named elements on sibling annotations.
func buildAnnotationElement(node ts.Node, file extractor.FileInput, parentType string) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}

	emittedName := name
	if parentType != "" {
		emittedName = parentType + "." + name
	}

	rec := types.EntityRecord{
		Name:       emittedName,
		Kind:       "SCOPE.Schema",
		Subtype:    "field",
		SourceFile: file.Path,
		Language:   "java",
		StartLine:  int(node.StartPoint().Row) + 1,
		EndLine:    int(node.EndPoint().Row) + 1,
		Signature:  buildAnnotationElementSignature(node, file.Content),
	}
	// Issue #6912 arm E — same contract as buildField: stash the declared
	// type's AST node so attachJavaFieldTypeRefs can emit the element -> type
	// REFERENCES edge once the file's full record set exists. This is what makes
	// `Level level();` on an annotation reach the project's own Level enum.
	stashJavaFieldTypeRefs(&rec, node.ChildByFieldName("type"), file.Content, parentType)
	return rec, true
}

// buildAnnotationElementSignature renders an annotation element as
// "Type name()" plus any `default <value>` clause, stripping the modifiers JLS
// 9.6.1 permits on one (`public` and `abstract`, both redundant and both
// legal). The trailing "()" is kept deliberately: it is how the element is
// written at its declaration and how a reader recognises it as an annotation
// element rather than a plain field.
//
// Modifiers are trimmed as a LEADING PREFIX, in a loop, rather than with
// strings.ReplaceAll over the whole span. That is not a style preference. This
// signature deliberately KEEPS the `default <value>` clause — buildFieldSignature
// truncates at `=` and so never has a value in scope, but this one does — and a
// default's value is arbitrary text that may itself contain a modifier keyword.
// A whole-span ReplaceAll reaches inside it and silently corrupts the value:
// `String scope() default "public api";` rendered as
// `String scope() default "api"`. The prefix loop also makes the strip
// order-independent, so `abstract public` is handled as well as
// `public abstract`.
func buildAnnotationElementSignature(node ts.Node, src []byte) string {
	raw := strings.TrimSpace(string(src[node.StartByte():node.EndByte()]))
	raw = strings.TrimSuffix(raw, ";")
	raw = strings.Join(strings.Fields(raw), " ")
	for trimmed := true; trimmed; {
		trimmed = false
		for _, mod := range []string{"public ", "abstract "} {
			if strings.HasPrefix(raw, mod) {
				raw = strings.TrimPrefix(raw, mod)
				trimmed = true
			}
		}
	}
	return strings.TrimSpace(raw)
}

// buildFieldSignature renders a Java field as "<Type> <name>" — e.g.
// `Map<String,String> CACHE`, `int arr[]`.
//
// # Read the parse tree, do not cut up the raw span
//
// Both parts are taken from `field_declaration`'s own children. That is a
// deliberate replacement for the previous implementation, which sliced the
// declaration's RAW SOURCE TEXT and produced three separate filed defects from
// that one decision:
//
//   - #7114 — no whitespace collapse, so a wrapped declaration carried
//     newlines and source indentation into the persisted signature, AND the
//     modifier strip below it silently stopped firing (its patterns each need
//     a trailing SPACE, so "public " never matches "public\n").
//   - #7117 — the initializer was cut at `strings.Index(raw, "=")`, i.e. the
//     FIRST `=` anywhere in the span. An annotation element assignment
//     precedes the type, so `@Deprecated(since = "1.0") private String key =
//     "v";` emitted `"@Deprecated(since"` — TYPE AND NAME BOTH LOST. Same for
//     `@Column(name = "x")`, `@JsonProperty(value = …)`,
//     `@RequestParam(required = false)`: ordinary in JPA/Spring/Jackson code.
//     An `=` inside an annotation's STRING literal did it too
//     (`@Query("a = b")` → `"@Query(\"a"`).
//   - #7116 — the modifier strip was an UNANCHORED strings.ReplaceAll, so
//     `@SuppressWarnings("public static thing") private String key;` emitted
//     `"@SuppressWarnings(\"thing\") String key"` — two words eaten out of a
//     string literal, yielding text that appears in no source file and that a
//     reader cannot recognise as mangled.
//
// Making the `=` search or the strip smarter would reproduce the pattern that
// produced all three. Reading the tree removes the class outright: the only
// nodes read are `type` and `declarator`, so no `=`, no modifier keyword and
// no annotation text — wherever it sits, whatever it contains — can reach the
// output at all.
//
// # Grammar, derived from a real parse tree (executed, not assumed)
//
//	field_declaration
//	  modifiers            <- child BY TYPE. NOT a named field:
//	                          ChildByFieldName("modifiers") returns nil here.
//	                          Holds the annotations AND the bare
//	                          visibility/`static`/`final`/… keyword tokens.
//	                          Never read by this function.
//	  type                 <- named field: the declared type node
//	  declarator           <- named field: the FIRST variable_declarator;
//	                          `int a = 1, b = 2;` has several as children
//	  variable_declarator
//	    name / value / dimensions   <- named fields; `dimensions` is the
//	                                   C-style `int arr[];` suffix
//
// # Annotations are DROPPED, and that is a deliberate break from the siblings
//
// Today they survive: `@SuppressWarnings("thing") String key`. They no longer
// will. buildClassSignature and buildMethodSignature DO keep `@Foo` (with the
// arguments stripped), so this differs from them on purpose, for a reason that
// applies to fields and to no other kind:
//
// A FIELD's Signature is the only one any consumer parses POSITIONALLY.
// docgen's typeHintFromSignature (internal/docgen/llm_bundle.go) is gated on
// `Kind == "SCOPE.Schema" && Subtype == "field"`, and its Java/C# arm takes
// `strings.Fields(sig)[0]` as the declared type. A leading `@Column` makes that
// hint the ANNOTATION rather than the type, on every annotated field — which in
// a JPA entity is most of them. Class and method signatures are never fed to
// it, so keeping annotations there costs nothing.
//
// What this costs, stated precisely rather than as "nothing is lost". The
// annotations any consumer actually acts on do not come from the signature and
// are untouched: javaFieldHasInjectAnnotation walks the `modifiers` child and
// turns @Inject/@Autowired into REFERENCES edges; field_validations.go walks
// the same child and stamps Bean Validation annotations into
// Properties["validations"]; nosql_model.go recognises
// @Id/@Indexed/@Field/@Column, but only on a Mongo/Cassandra document class.
// Everything else — `@Deprecated`, a JPA `@Column` on an entity that is NOT a
// NoSQL document, any third-party marker — used to appear in the signature and
// is now recorded NOWHERE on the entity. That is a real, if narrow, loss and
// not "nothing".
//
// It is narrow because of the #7117 defect itself: an annotation carrying an
// ELEMENT (`@Size(max = 120)`, `@Column(name = "x")`) already destroyed the
// signature from its first `=` onward, so it never reached a reader intact.
// Only a MARKER annotation — no argument list at all, or arguments containing
// no `=` (`@SuppressWarnings("thing")`) — survived pre-fix, so markers are the
// only thing that regresses. If one ever needs to be on the entity, `modifiers`
// is where to read it: a positionally-parsed signature is the wrong carrier for
// it either way.
//
// Note this also widens the modifier strip: previously exactly five keywords
// were removed ("public ", "private ", "protected ", "static ", "final "), so
// `transient`, `volatile` and friends survived into the signature. Not reading
// `modifiers` at all removes every modifier, which is what
// "stripping visibility" always meant.
//
// Whitespace is collapsed per part with strings.Fields, so a wrapped type
// (#7114) and an intra-line whitespace RUN inside one (#7118 —
// `Map<String,   String>`) both normalise to single spaces. Both call sites of
// collapseJavaSpaces are graded separately, and separately per AXIS, because a
// verdict on one does not carry to the other: guarding the HELPER on containing
// a newline was killed only by `spaced`, while guarding the DIMENSIONS CALL
// alone was ALIVE with 0 `--- FAIL` lines until #7118 added `spacedDims`. So
// the type site is graded by `spaced` (intra-line) and `wrapped` (newline), and
// the dimensions site by `spacedDims`/`tabbedDims` (intra-line — JLS 10.2
// permits whitespace BETWEEN the brackets, `int arr[   ];`) and `wrappedDims`
// (a `[` and `]` split across lines). Whitespace is ONE KIND of content that
// run can hold, and the node's start at `[` does not bound it to whitespace:
// `int withComment[/* a comment */];` and a TYPE_USE annotation
// `int withAnno @NN [];` are both javac-clean and both put non-whitespace into
// the dimensions text. Those two are graded by #7161's rows, not by the
// `spacedDims`/`tabbedDims` pair here — the annotation one because it starts the
// dimensions text at `@` rather than `[` and so exposed the missing separator in
// the concatenation below, which no amount of collapsing can restore.
//
// # The three guards below are DEFENSIVE and ungraded on purpose
//
// `type != nil`, `txt != ""` and `decl != ""` are each EQUIVALENT UNDER THE
// CURRENT SUITE: widening any of them to `true` changes no signature and
// produces 0 `--- FAIL` lines in this package. They are recorded here rather
// than left looking ungraded, because they are unreachable, not untested:
//
//   - `type` is a mandatory named field of field_declaration, so it is never
//     nil on a node that reaches this function;
//   - a non-nil node spans at least one byte, so `txt` is empty only when
//     `type` is nil, i.e. only via the arm above;
//   - `decl` is `name + dimensions` and `name` is the entity name buildField
//     already established, so it is never empty.
//
// Demonstrated rather than asserted: the malformed spellings that could
// produce those shapes (`private int ;`, `private ;`, `private x;`, `int;`)
// parse to ERROR nodes and yield ZERO SCOPE.Schema records — buildField never
// runs on them — so no input reachable here can distinguish the guarded form
// from the widened one. Do not "grade" them with a hand-built parse tree; the
// honest record is that they cost nothing and can never fire.
func buildFieldSignature(node ts.Node, src []byte, name string) string {
	var parts []string
	if t := node.ChildByFieldName("type"); t != nil {
		if txt := collapseJavaSpaces(nodeText(t, src)); txt != "" {
			parts = append(parts, txt)
		}
	}
	// The declarator is `name` glued to its dimensions suffix. Gluing is right
	// only while that suffix starts at `[`: JLS 10.2 spells it
	// `{Annotation} [ ]`, so a TYPE_USE annotation belongs to the dimensions
	// node and the text can begin at `@`. The source's separating space then
	// falls BETWEEN the two operands and nothing puts it back —
	// `int withAnno @NN [];` emitted `int withAnno@NN []` (#7161).
	//
	// The policy is to NORMALISE, not to replay: emit EXACTLY ONE space before
	// a dimensions suffix that opens at `@`, whether or not the source had one,
	// and none before one that opens at `[`. So `int a@NN[];` — javac-clean,
	// no space anywhere — emits `int a @NN[]`, a space this code MANUFACTURES
	// rather than restores. That is deliberate: a Signature is a rendered form
	// and this function already collapses every whitespace run in it, so
	// propagating incidental source spacing here would be the inconsistent
	// choice. Graded by `normAnno`; `plainDims`/`plainTwoDims`/`annoNotFirst`
	// grade the other branch, at both dimension counts and with an annotation
	// present but not at position 0.
	//
	// The normalisation is POSITION-0 ONLY, and that is a scope boundary rather
	// than a general rule about the suffix. Only the first character of the
	// dimensions text is examined, so an annotation sitting LATER in the same
	// text with no space in front of it keeps none: `int x[]@NN[];` emits
	// `int x[]@NN[]`, while `int x@NN[];` emits `int x @NN[]`. One construct
	// therefore renders three ways — leading-without-space gains a space,
	// inner-without-space does not, inner-with-space keeps its own. Inner
	// positions are NOT untouched territory, and saying they are unnormalised
	// would be false: an inner whitespace RUN is already collapsed upstream by
	// collapseJavaSpaces (`int x @NN[]   @NN   [];` emits `int x @NN[] @NN []`,
	// measured on review). It is specifically the inner ZERO-space case that is
	// left alone. Filed as #7180 and deliberately NOT fixed here.
	//
	// The predicate reads position 0 rather than searching for `@`, and the
	// first character of `dims` is exactly `@` or `[` — enumerated over 13
	// javac-clean shapes on review, `@Sz(msg="[weird]")` included, where the
	// bracket inside an annotation argument is never at position 0.
	dims := javaDeclaratorDimensions(node, src)
	if dims != "" && !strings.HasPrefix(dims, "[") {
		dims = " " + dims
	}
	if decl := name + dims; decl != "" {
		parts = append(parts, decl)
	}
	return strings.Join(parts, " ")
}

// javaDeclaratorDimensions returns the C-style array suffix JLS 10.2 permits
// after the variable name (`private int arr[];` → "[]"), or "" when there is
// none. It is a `dimensions` child of the variable_declarator, NOT part of the
// `type` node, so reading type+name alone would silently drop it.
//
// The `declarator` field is read rather than the declarator whose `name`
// matches: `int a = 1, b = 2;` does have several variable_declarator children,
// but buildField takes its entity Name from the FIRST one it walks to, which is
// exactly the node `declarator` names (verified on that declaration: the
// `declarator` field is `a = 1`). Matching by name instead would select the
// same node on every input reachable here, so it would be an unreachable guard
// rather than a stricter one.
//
// The `decl == nil` early return is a fourth EQUIVALENT UNDER THE CURRENT
// SUITE: deleting it changes no signature and produces 0 `--- FAIL` lines
// (`declarator` is a mandatory named field, and the malformed declarations that
// lack one yield no SCOPE.Schema record at all — see buildFieldSignature). It
// is kept because childFieldText would panic on a nil node, so the guard is the
// difference between "impossible" and "impossible AND survivable".
func javaDeclaratorDimensions(node ts.Node, src []byte) string {
	decl := node.ChildByFieldName("declarator")
	if decl == nil {
		return ""
	}
	return collapseJavaSpaces(childFieldText(decl, "dimensions", src))
}

// collapseJavaSpaces normalises every whitespace run — newlines included — to a
// single space and trims the ends.
func collapseJavaSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// nodeText returns the source text covered by node.
func nodeText(node ts.Node, src []byte) string {
	if node == nil {
		return ""
	}
	return string(src[node.StartByte():node.EndByte()])
}

// childFieldText extracts the text of a named child field (e.g. "name").
func childFieldText(node ts.Node, field string, src []byte) string {
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
func buildMethodSignature(node ts.Node, src []byte) string {
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
func buildClassSignature(node ts.Node, src []byte, name string) string {
	raw := string(src[node.StartByte():node.EndByte()])
	// Strip annotation arguments FIRST, for the reason buildMethodSignature
	// already documents: a brace inside an annotation argument string
	// (`@Table(name = "{weird}")`) is indistinguishable from the body brace to
	// strings.Index, so cutting first truncated the declaration INSIDE the
	// literal and the emitted signature lost `class Foo` entirely (#7124).
	// stripAnnotationArgs balances PARENTHESES, so it is unaffected by braces
	// at any nesting depth inside the arguments.
	raw = stripAnnotationArgs(raw)
	// Trim at opening brace (body start).
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = raw[:idx]
	}
	// Collapse newlines + whitespace into single spaces.
	raw = strings.Join(strings.Fields(raw), " ")
	// Strip visibility modifiers.
	for _, mod := range []string{"public ", "private ", "protected ", "static "} {
		raw = strings.ReplaceAll(raw, mod, "")
	}
	return strings.TrimSpace(raw)
}

// javaSuperclassNames extracts the parent class name from a class_declaration
// node's `superclass` child. Returns a slice (always 0 or 1 elements) for
// uniform call-site iteration with javaSuperInterfaceNames. Generics are
// stripped — `extends List<Owner>` yields "List".
//
// Issue #1996 — required input for the docgen ClassManifest `bases` field.
func javaSuperclassNames(node ts.Node, src []byte) []string {
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
func javaSuperInterfaceNames(node ts.Node, src []byte) []string {
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
	var list ts.Node
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
func javaInjectFieldTypes(body ts.Node, src []byte) []string {
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
func javaFieldHasInjectAnnotation(field ts.Node, src []byte) bool {
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
