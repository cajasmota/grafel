package references

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ReferenceKind is the EntityRecord.Kind value emitted for every SCOPE
// reference entity by this package.
const ReferenceKind = "SCOPE.Reference"

// DynamicTargetSentinel is written into target_name when the AST node
// carries a non-identifier expression in place of a name (e.g.
// obj[key] or obj[computeKey()]). The entity is still emitted — the
// spec requires this rather than silently dropping the reference.
const DynamicTargetSentinel = "<dynamic>"

// ReferencesRelationshipKind is the RelationshipRecord.Kind value used
// for the usage-site -> declaration edge.
const ReferencesRelationshipKind = "REFERENCES"

// declLookup is the Phase-1 output: name -> (kind, start_line). A single
// entry is stored per name; if a file declares the same name more than
// once, the first occurrence wins (same rule as the Python reference
// extractor). Phase 2 queries the map to resolve target_kind.
type declLookup struct {
	// entries is keyed by the unqualified declaration name. Values are
	// pointers so updates during Phase 1 don't require reassignment.
	entries map[string]*declEntry
}

type declEntry struct {
	kind      string
	startLine int
	// node is stashed so the Phase-2 relationship builder can reach it
	// without re-walking the AST.
	node *sitter.Node
}

func newDeclLookup() *declLookup {
	return &declLookup{entries: make(map[string]*declEntry)}
}

// add inserts a declaration into the lookup map, preserving the first
// occurrence when a name collides.
func (d *declLookup) add(name, kind string, node *sitter.Node) {
	if name == "" || d == nil {
		return
	}
	if _, exists := d.entries[name]; exists {
		return
	}
	d.entries[name] = &declEntry{
		kind:      kind,
		startLine: int(node.StartPoint().Row) + 1,
		node:      node,
	}
}

// resolve returns the declaration entry for a given name or nil if no
// declaration with that name is known in the current file.
func (d *declLookup) resolve(name string) *declEntry {
	if d == nil || name == "" {
		return nil
	}
	return d.entries[name]
}

// ReferenceExtractor is the SCOPE.Reference extractor. It is a
// standalone extractor.Extractor implementation — it can be called on
// its own to emit ONLY reference entities, or wrapped around a
// language extractor via Wrap to produce a merged declaration +
// reference stream.
//
// ReferenceExtractor is safe for concurrent use. State is scoped per
// Extract call.
type ReferenceExtractor struct {
	// GrammarProvider returns the tree-sitter grammar for a canonical
	// language name. It is only consulted when a FileInput arrives
	// without a pre-parsed Tree. Production callers should always pass
	// Tree through the pipeline's shared parser factory — this hook
	// exists for unit tests and the local run-extractor CLI.
	GrammarProvider func(language string) *sitter.Language

	// FrameworkTagger, when non-nil, is consulted for every emitted
	// reference entity. The default FrameworkTagger chain is
	// initialized by NewReferenceExtractor.
	FrameworkTagger FrameworkTagger

	// MaxReferencesPerFile caps the number of entities emitted per
	// single Extract call. Zero means no cap. Protects against
	// pathological generated files.
	MaxReferencesPerFile int

	tracer trace.Tracer

	initOnce sync.Once
}

// NewReferenceExtractor constructs a ReferenceExtractor wired with the
// default OTel tracer and a no-op FrameworkTagger. The returned value
// is ready to call Extract immediately.
func NewReferenceExtractor() *ReferenceExtractor {
	return &ReferenceExtractor{
		FrameworkTagger:      &CompositeTagger{}, // empty chain == no-op
		MaxReferencesPerFile: 0,
	}
}

// Language implements extractor.Extractor. ReferenceExtractor does not
// own a single language — it reports an empty string so the global
// registry dispatch never routes a file here by accident. The pipeline
// is expected to invoke Extract directly (or via Wrap).
func (r *ReferenceExtractor) Language() string { return "" }

func (r *ReferenceExtractor) init() {
	r.initOnce.Do(func() {
		if r.tracer == nil {
			r.tracer = otel.Tracer("extractor.references")
		}
		if r.FrameworkTagger == nil {
			r.FrameworkTagger = &CompositeTagger{}
		}
	})
}

// Extract runs Phase 1 (declaration collection) and Phase 2 (reference
// resolution) inside a single call and returns a slice of
// SCOPE.Reference EntityRecord values.
//
// The function always emits span "indexer.reference_extract" with
// attributes {language, file_path, reference_count}. On panic during
// AST traversal it logs a WARN, marks the span as error, and returns
// whatever reference entities had been collected up to that point. A
// panic never escapes the function.
func (r *ReferenceExtractor) Extract(ctx context.Context, file extractor.FileInput) (records []types.EntityRecord, retErr error) {
	r.init()

	ctx, span := r.tracer.Start(ctx, "indexer.reference_extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[references] WARNING: panic during reference extraction: file=%s language=%s err=%v",
				file.Path, file.Language, rec)
			span.RecordError(fmt.Errorf("panic: %v", rec))
			span.SetStatus(codes.Error, "panic during reference traversal")
			// Fall through — records collected up to the panic point
			// are still returned to the caller per spec.
		}
		span.SetAttributes(attribute.Int("reference_count", len(records)))
		span.End()
	}()

	if len(file.Content) == 0 {
		return nil, nil
	}

	lang := tableFor(file.Language)
	if lang == nil {
		// Rule 8: unsupported languages are not an error, just a no-op.
		return nil, nil
	}

	tree := file.Tree
	if tree == nil {
		if r.GrammarProvider == nil {
			return nil, nil
		}
		grammar := r.GrammarProvider(file.Language)
		if grammar == nil {
			return nil, nil
		}
		parser := sitter.NewParser()
		parser.SetLanguage(grammar)
		parsed, err := parser.ParseCtx(ctx, nil, file.Content)
		if err != nil {
			return nil, fmt.Errorf("references: parse failed for %s: %w", file.Path, err)
		}
		tree = parsed
	}

	root := tree.RootNode()
	if root == nil {
		return nil, nil
	}

	// -------------------- Phase 1 --------------------
	decls := newDeclLookup()
	collectDeclarations(root, file.Content, lang, decls)

	// Run framework detection ONCE per file, using the declarations
	// from Phase 1 plus a scan of the file's imports. The resulting
	// context is passed to every tagger invocation in Phase 2.
	fwCtx := DetectFramework(file, root, decls)

	// -------------------- Phase 2 --------------------
	records = collectReferences(root, file, lang, decls, fwCtx, r.FrameworkTagger, r.MaxReferencesPerFile)
	return records, nil
}

// ------------------------------------------------------------------
// Phase 1 — declaration collection
// ------------------------------------------------------------------

// collectDeclarations walks the AST once and seeds the name->kind map.
// The traversal is iterative to avoid stack overflows on large files.
func collectDeclarations(root *sitter.Node, src []byte, lang *langNodeTypes, out *declLookup) {
	if lang == nil || len(lang.declarations) == 0 {
		return
	}
	stack := []*sitter.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if kind, ok := lang.declarations[n.Type()]; ok {
			if name := declNameOf(n, src); name != "" {
				out.add(name, kind, n)
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			stack = append(stack, n.Child(i))
		}
	}
}

// declNameOf extracts the declared symbol name from a declaration node.
// It first tries the "name" field (which every major grammar exposes)
// and then falls back to scanning children for an identifier-like node.
func declNameOf(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	if nm := n.ChildByFieldName("name"); nm != nil {
		return nodeText(nm, src)
	}
	// Fallback: scan direct children for the first identifier-ish node.
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		switch child.Type() {
		case "identifier", "type_identifier", "simple_identifier",
			"variable_name", "constant", "sym_lit":
			return nodeText(child, src)
		}
	}
	return ""
}

// ------------------------------------------------------------------
// Phase 2 — reference resolution
// ------------------------------------------------------------------

// collectReferences walks the AST a second time and produces
// SCOPE.Reference entity records.
func collectReferences(
	root *sitter.Node,
	file extractor.FileInput,
	lang *langNodeTypes,
	decls *declLookup,
	fwCtx FrameworkContext,
	tagger FrameworkTagger,
	maxRefs int,
) []types.EntityRecord {
	var out []types.EntityRecord

	emit := func(rec types.EntityRecord) bool {
		// Apply framework tagging before emission.
		if tagger != nil {
			tagger.Tag(&rec, fwCtx)
		}
		out = append(out, rec)
		if maxRefs > 0 && len(out) >= maxRefs {
			return false
		}
		return true
	}

	stack := []*sitter.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		nt := n.Type()

		// --- Writes ---
		if _, isWrite := lang.writeTarget[nt]; isWrite {
			if rec, ok := buildWriteReference(n, file, lang, decls); ok {
				if !emit(rec) {
					return out
				}
			}
		}

		// --- Property access ---
		if _, isMember := lang.memberAccess[nt]; isMember {
			if rec, ok := buildMemberReference(n, file, lang, decls); ok {
				if !emit(rec) {
					return out
				}
			}
		}

		// --- Call / method invocation ---
		if _, isCall := lang.callExpression[nt]; isCall {
			recs := buildCallReferences(n, file, lang, decls)
			for _, rec := range recs {
				if !emit(rec) {
					return out
				}
			}
		}

		// --- Type annotations ---
		if _, isType := lang.typeAnnotation[nt]; isType {
			if rec, ok := buildTypeReference(n, file, lang, decls); ok {
				if !emit(rec) {
					return out
				}
			}
		}

		for i := 0; i < int(n.ChildCount()); i++ {
			stack = append(stack, n.Child(i))
		}
	}

	return out
}

// buildWriteReference produces a "write" reference entity from an
// assignment-like node. The LHS identifier becomes the reference name.
func buildWriteReference(n *sitter.Node, file extractor.FileInput, lang *langNodeTypes, decls *declLookup) (types.EntityRecord, bool) {
	lhs := n.ChildByFieldName("left")
	if lhs == nil && n.ChildCount() > 0 {
		lhs = n.Child(0)
	}
	if lhs == nil {
		return types.EntityRecord{}, false
	}
	name := nodeText(lhs, file.Content)
	if name == "" {
		name = DynamicTargetSentinel
	}
	return newReferenceRecord(file, n, name, name, RefWrite, resolveKind(name, decls), decls), true
}

// buildMemberReference produces a "property_access" reference entity.
// The name is "<object>.<property>" and the target_name is the property
// (which is what a user typically searches for).
func buildMemberReference(n *sitter.Node, file extractor.FileInput, lang *langNodeTypes, decls *declLookup) (types.EntityRecord, bool) {
	var (
		objNode  *sitter.Node
		propNode *sitter.Node
	)
	if lang.memberObjField != "" {
		objNode = n.ChildByFieldName(lang.memberObjField)
	}
	if lang.memberNameField != "" {
		propNode = n.ChildByFieldName(lang.memberNameField)
	}
	// Fallback scan when the grammar uses positional children.
	if objNode == nil && n.ChildCount() >= 1 {
		objNode = n.Child(0)
	}
	if propNode == nil && n.ChildCount() >= 3 {
		propNode = n.Child(int(n.ChildCount()) - 1)
	}

	objText := nodeText(objNode, file.Content)
	propText := nodeText(propNode, file.Content)
	if propText == "" {
		propText = DynamicTargetSentinel
	}
	if objText == "" {
		objText = DynamicTargetSentinel
	}
	name := objText + "." + propText
	rec := newReferenceRecord(file, n, name, propText, RefPropertyAccess, resolveKind(propText, decls), decls)
	// Track the object identifier too, so downstream can join on it.
	if rec.Properties == nil {
		rec.Properties = make(map[string]string)
	}
	rec.Properties["receiver"] = objText
	return rec, true
}

// buildCallReferences produces "call" reference entities from a
// call-expression node. It also emits "argument" entities for each
// identifier passed in the argument list (spec rule 4).
func buildCallReferences(n *sitter.Node, file extractor.FileInput, lang *langNodeTypes, decls *declLookup) []types.EntityRecord {
	var out []types.EntityRecord

	// Resolve the callee.
	var fn *sitter.Node
	if lang.callNameField != "" {
		fn = n.ChildByFieldName(lang.callNameField)
	}
	if fn == nil && n.ChildCount() > 0 {
		fn = n.Child(0)
	}
	if fn != nil {
		rawName := nodeText(fn, file.Content)
		// For member-style callees (e.g. obj.setValue), the last segment
		// is the method name; that's what target_name should carry.
		target := lastSegment(rawName)
		if target == "" {
			target = DynamicTargetSentinel
		}
		kind := resolveKind(target, decls)
		if kind == "" {
			// Heuristic for rule 6: getter-shaped method names resolve
			// to SCOPE.Operation even without a local declaration.
			if isGetterName(target) || isSetterName(target) {
				kind = "SCOPE.Operation"
			}
		}
		rec := newReferenceRecord(file, n, rawName, target, RefCall, kind, decls)
		if rec.Properties == nil {
			rec.Properties = make(map[string]string)
		}
		if strings.Contains(rawName, ".") {
			rec.Properties["receiver"] = strings.Split(rawName, ".")[0]
		}
		out = append(out, rec)
	}

	// Argument references — find the argument list child and emit one
	// entity per identifier found inside it.
	args := n.ChildByFieldName("arguments")
	if args == nil {
		// Grammar fallback: scan children for a node with a parens-y name.
		for i := 0; i < int(n.ChildCount()); i++ {
			child := n.Child(i)
			ct := child.Type()
			if ct == "arguments" || ct == "argument_list" || ct == "call_arguments" {
				args = child
				break
			}
		}
	}
	if args != nil {
		for _, id := range findAllOfSet(args, lang.identifier) {
			name := nodeText(id, file.Content)
			if name == "" {
				continue
			}
			out = append(out, newReferenceRecord(file, id, name, name, RefArgument, resolveKind(name, decls), decls))
		}
	}
	return out
}

// buildTypeReference produces a "type" reference entity from a type
// annotation node.
func buildTypeReference(n *sitter.Node, file extractor.FileInput, lang *langNodeTypes, decls *declLookup) (types.EntityRecord, bool) {
	name := nodeText(n, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	// Generic type nodes often look like "List<User>" — also emit the
	// outer form; inner identifiers will be picked up on later iterations
	// of the traversal because type_identifier is declared in the table.
	return newReferenceRecord(file, n, name, name, RefType, resolveKindOrDefault(name, decls, "SCOPE.Component"), decls), true
}

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------

// newReferenceRecord builds an EntityRecord with the common fields set.
// It centralises the places where a reference entity can be constructed
// so that emission rules stay consistent across reference types.
func newReferenceRecord(
	file extractor.FileInput,
	n *sitter.Node,
	name string,
	targetName string,
	refType string,
	targetKind string,
	decls *declLookup,
) types.EntityRecord {
	startLine := int(n.StartPoint().Row) + 1
	endLine := int(n.EndPoint().Row) + 1

	rec := types.EntityRecord{
		Name:         name,
		Kind:         ReferenceKind,
		Subtype:      refType,
		SourceFile:   file.Path,
		StartLine:    startLine,
		EndLine:      endLine,
		Language:     file.Language,
		QualityScore: 1.0,
		Properties: map[string]string{
			"reference_type": refType,
			"target_name":    targetName,
			"target_kind":    targetKind,
		},
		EnrichmentRequired: false,
	}

	// When the target was resolved against the Phase-1 lookup map we
	// attach a REFERENCES relationship to the declaration's line.
	// The FromID / ToID resolution to concrete entity IDs happens in a
	// downstream pass — at this layer we use the symbolic names and
	// let the pipeline's ID resolver fill in the sha256 IDs.
	if entry := decls.resolve(targetName); entry != nil {
		rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
			FromID: fmt.Sprintf("%s:%d:%s", file.Path, startLine, refType),
			ToID:   fmt.Sprintf("%s:%d:%s", file.Path, entry.startLine, entry.kind),
			Kind:   ReferencesRelationshipKind,
			Properties: map[string]string{
				"target_name": targetName,
				"target_kind": entry.kind,
			},
		})
	}
	return rec
}

// resolveKind looks the name up in the declaration lookup map and
// returns the entry's kind, or empty string if no match exists.
func resolveKind(name string, decls *declLookup) string {
	if entry := decls.resolve(name); entry != nil {
		return entry.kind
	}
	return ""
}

// resolveKindOrDefault returns the resolved kind if present, else the
// fallback passed by the caller.
func resolveKindOrDefault(name string, decls *declLookup, fallback string) string {
	if k := resolveKind(name, decls); k != "" {
		return k
	}
	return fallback
}

// nodeText returns the UTF-8 substring covered by a tree-sitter node.
func nodeText(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	start := node.StartByte()
	end := node.EndByte()
	if int(end) > len(src) {
		end = uint32(len(src))
	}
	return string(src[start:end])
}

// findAllOfSet returns the descendants of root whose node.Type() is in
// typeSet. Iterative to avoid stack overflow on large files.
func findAllOfSet(root *sitter.Node, typeSet map[string]struct{}) []*sitter.Node {
	if root == nil || len(typeSet) == 0 {
		return nil
	}
	var out []*sitter.Node
	stack := []*sitter.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := typeSet[n.Type()]; ok {
			out = append(out, n)
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return out
}

// lastSegment returns the final dot-separated segment of a qualified
// name. `obj.setValue` -> `setValue`, `foo` -> `foo`.
func lastSegment(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

// isGetterName reports whether a method name follows the "get*" /
// "is*" / "has*" convention used in rule 6 of the spec.
func isGetterName(name string) bool {
	switch {
	case strings.HasPrefix(name, "get") && len(name) > 3:
		return true
	case strings.HasPrefix(name, "is") && len(name) > 2:
		return true
	case strings.HasPrefix(name, "has") && len(name) > 3:
		return true
	}
	return false
}

// isSetterName reports whether a method name follows the "set*"
// convention. Used by the framework tagger and by rule 6 extrapolation.
func isSetterName(name string) bool {
	return strings.HasPrefix(name, "set") && len(name) > 3
}
