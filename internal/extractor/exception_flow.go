package extractor

import (
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// exception_flow.go — shared cross-language helpers for the error-flow
// topology (epic #3628). It mirrors the config-key model in config_key.go.
//
// The error-flow capability answers two questions a rewrite needs to keep
// error-contract parity:
//
//   - "what exceptions can this function raise?"  → a function's outbound
//     THROWS edges.
//   - "where is ValidationError handled?"          → an exception type's
//     inbound CATCHES edges.
//
// To make a type raised in one function and caught in another converge on ONE
// graph node, every language extractor emits:
//
//   - one SCOPE.ExceptionType / subtype="exception_type" entity per distinct
//     (normalized, unqualified) type name, with a SYNTHETIC constant
//     SourceFile (ExceptionTypeSourceFile) so EntityRecord.ComputeID
//     (SourceFile+Kind+Name) collapses identical type names — even across
//     files and languages — into a single node. A NotFound raised in
//     services.py and caught in views.py therefore share one node, and that
//     node's outbound-THROWS / inbound-CATCHES sets are the error contract.
//
//   - one THROWS edge (raising function → exception-type node) and/or one
//     CATCHES edge (handler function → exception-type node), each carried as
//     a structural-ref ToID (ExceptionTypeTargetID) that the resolver binds
//     via the byQualifiedName exact-match tier (the entity's QualifiedName is
//     set equal to that ToID).
//
// Precision-first / honest-partial: only TYPED throws/catches are recorded.
// Dynamic-or-computed raise types, bare `except:` / untyped `catch(e){}`, and
// anonymous inline errors (`errors.New("...")`, bare `fmt.Errorf("...")`)
// emit NO edge — a single wrong THROWS/CATCHES edge would mislead
// error-contract analysis. The detection of those shapes lives in each
// language extractor; this file owns only the node/edge construction so the
// convergence invariant is identical everywhere.

// ExceptionTypeSourceFile is the synthetic, constant SourceFile assigned to
// every exception-type entity so identical type names converge to a single
// graph node under EntityRecord.ComputeID (which hashes SourceFile+Kind+Name).
const ExceptionTypeSourceFile = "<exception>"

// ExceptionTypeName returns the canonical entity Name for an exception type.
// The "exception:" prefix namespaces the node (so it never collides with a
// same-named code symbol) and keeps the human-readable type verbatim, e.g.
// "exception:ValidationError", "exception:NotFound", "exception:IOException".
func ExceptionTypeName(typeName string) string {
	return "exception:" + typeName
}

// ExceptionTypeTargetID returns the structural-ref ToID for a THROWS / CATCHES
// edge pointing at an exception-type entity. Shape:
//
//	scope:exceptiontype:<Type>
//
// This value is ALSO stored as the exception-type entity's QualifiedName, so
// the resolver's byQualifiedName exact-match tier (internal/resolve/refs.go)
// binds the edge to that entity without any new linker code. Constant across
// languages so a JS `throw new NotFound()` and a Python `raise NotFound()`
// resolve to the same node.
func ExceptionTypeTargetID(typeName string) string {
	return "scope:exceptiontype:" + typeName
}

// NormalizeExceptionType strips package / module qualification and trailing
// call/generic punctuation from a raw exception-type token, returning the bare
// class name used as the convergence key — or "" if the token is not a usable
// static type name (empty, or contains characters that signal a dynamic /
// computed type the caller must drop).
//
//	"errors.ValidationError" -> "ValidationError"
//	"java.io.IOException"     -> "IOException"
//	"pkg.ErrNotFound"         -> "ErrNotFound"
//	"NotFound"                -> "NotFound"
//	"e.SomeError"             -> "SomeError"   (instanceof RHS)
//
// Returns "" for tokens that are not a single dotted identifier path (e.g.
// containing spaces, parentheses, brackets, operators) so dynamic raises like
// `raise exc_class()` or `throw mk()` never fabricate a node.
func NormalizeExceptionType(raw string) string {
	t := strings.TrimSpace(raw)
	if t == "" {
		return ""
	}
	// Take the last dotted/scoped segment (Foo.Bar.Baz -> Baz, pkg::Err -> Err).
	t = strings.ReplaceAll(t, "::", ".")
	if i := strings.LastIndexByte(t, '.'); i >= 0 {
		t = t[i+1:]
	}
	t = strings.TrimSpace(t)
	if t == "" {
		return ""
	}
	// Must be a single identifier: letters, digits, underscore only, and must
	// start with a letter or underscore. Anything else (spaces, parens, generic
	// angle brackets, operators) signals a dynamic / computed type → drop.
	for i := 0; i < len(t); i++ {
		c := t[i]
		isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isDigit := c >= '0' && c <= '9'
		if c == '_' || isLetter || (isDigit && i > 0) {
			continue
		}
		return ""
	}
	return t
}

// ExceptionTypeEntity builds the SCOPE.ExceptionType / exception_type entity
// for a single normalized type name in the given language. The entity is
// deliberately file-agnostic (synthetic SourceFile) so it is the shared
// error-contract convergence node, and its QualifiedName equals the edge ToID
// so THROWS / CATCHES edges resolve via byQualifiedName.
func ExceptionTypeEntity(typeName, lang string) types.EntityRecord {
	e := types.EntityRecord{
		Name:          ExceptionTypeName(typeName),
		QualifiedName: ExceptionTypeTargetID(typeName),
		Kind:          string(types.EntityKindExceptionType),
		Subtype:       "exception_type",
		Language:      lang,
		SourceFile:    ExceptionTypeSourceFile,
		StartLine:     1,
		EndLine:       1,
		Signature:     ExceptionTypeName(typeName),
		Properties: map[string]string{
			"exception_type": typeName,
		},
	}
	// Pre-compute the deterministic ID so extractors that ID their entities at
	// emit time stay consistent; the synthetic constant SourceFile makes
	// identical type names across files/languages converge to ONE node.
	e.ID = e.ComputeID()
	return e
}

// ExceptionEdge is one resolved typed throw/catch detected by a language
// extractor: the raw exception-type token, the Name of the enclosing function/
// method, and whether it is thrown or caught.
type ExceptionEdge struct {
	Type     string // raw exception-type token (normalized internally)
	FromName string // enclosing function/method Name; "" => file entity
	Catch    bool   // false => THROWS, true => CATCHES
	Pattern  string // detector label, e.g. "throw_new", "raise", "instanceof", "errors_is"
}

// EmitExceptionEdges appends, to *entities, the exception-type entities and
// THROWS / CATCHES edges for the given typed throw/catch detections.
//
// entities[0] MUST be the file entity (every language extractor appends it
// first). Edges whose FromName is "" — or whose FromName has no matching host
// entity — attach to the file entity (index 0) as a conservative fallback so
// the edge is never silently dropped. Identical type names converge to one
// exception-type entity (deduped by name) and one edge per
// (FromName, type, direction) tuple.
//
// Returns the number of THROWS + CATCHES edges emitted. Safe with nil/empty
// input. Tokens that NormalizeExceptionType rejects (dynamic / computed types)
// are skipped — precision over recall.
func EmitExceptionEdges(entities *[]types.EntityRecord, lang string, edges []ExceptionEdge) int {
	if entities == nil || len(*entities) == 0 || len(edges) == 0 {
		return 0
	}

	hostByName := map[string]int{}
	for i := range *entities {
		hostByName[(*entities)[i].Name] = i
	}

	seenEdge := map[string]bool{}
	seenType := map[string]bool{}
	var newEntities []types.EntityRecord
	emitted := 0

	for _, ed := range edges {
		typeName := NormalizeExceptionType(ed.Type)
		if typeName == "" {
			continue // dynamic / computed / unusable type — drop
		}

		hostIdx := 0 // file entity by default
		if ed.FromName != "" {
			if idx, ok := hostByName[ed.FromName]; ok {
				hostIdx = idx
			}
		}

		kind := types.RelationshipKindThrows
		dir := "throws"
		if ed.Catch {
			kind = types.RelationshipKindCatches
			dir = "catches"
		}

		edgeKey := ed.FromName + "\x00" + dir + "\x00" + typeName
		if !seenEdge[edgeKey] {
			seenEdge[edgeKey] = true
			props := types.Props{{K: "exception_type", V: typeName}}
			if ed.Pattern != "" {
				props.Set("pattern", ed.Pattern)
			}
			(*entities)[hostIdx].Relationships = append((*entities)[hostIdx].Relationships,
				types.RelationshipRecord{
					ToID:       ExceptionTypeTargetID(typeName),
					Kind:       string(kind),
					Properties: props,
				})
			emitted++
		}

		if !seenType[typeName] {
			seenType[typeName] = true
			newEntities = append(newEntities, ExceptionTypeEntity(typeName, lang))
		}
	}

	*entities = append(*entities, newEntities...)
	return emitted
}
