// Package javascript provides regex-based custom extractors for JavaScript and
// TypeScript source files. Each extractor targets a specific framework or
// library and supplements the YAML-driven engine rules with logic that
// requires multi-pass or context-aware regex matching.
//
// All extractors implement extractor.Extractor and register via init().
package javascript

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// lineOf returns the 1-indexed line number for a byte offset within source.
func lineOf(source string, offset int) int {
	return strings.Count(source[:offset], "\n") + 1
}

// makeEntity builds a minimal EntityRecord with all required fields set.
func makeEntity(name, kind, subtype, filePath, language string, lineNum int) types.EntityRecord {
	e := types.EntityRecord{
		Name:             name,
		Kind:             kind,
		Subtype:          subtype,
		SourceFile:       filePath,
		StartLine:        lineNum,
		EndLine:          lineNum,
		Language:         language,
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
		Properties: map[string]string{
			"kind":    kind,
			"subtype": subtype,
		},
	}
	e.ID = e.ComputeID()
	return e
}

// containsFieldEdge builds the structural CONTAINS membership edge from an
// owning class (DTO / entity / schema) to one of its decorated field/property
// entities. Issue #4328: decorated DTO properties and entity columns were
// emitted as standalone nodes with no owner, leaving them as orphans on the
// graph. The edge is hung off the owner model node; FromID names the owner
// class (`Class:<owner>`) so the resolver binds it to the real class entity,
// ToID is the concrete member entity ID.
func containsFieldEdge(ownerClass, memberID, fieldName, framework string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: "Class:" + ownerClass,
		ToID:   memberID,
		Kind:   string(types.RelationshipKindContains),
		Properties: map[string]string{
			"framework":  framework,
			"member":     "field",
			"field_name": fieldName,
			"provenance": "INFERRED_FROM_DECORATED_FIELD_MEMBERSHIP",
		},
	}
}

// referencesClassEdge builds a REFERENCES edge from a field/property entity to
// the class named in a decorator thunk — `@ManyToOne(() => Role)`,
// `@Type(() => AddressDto)`, `@Prop({ type: () => X })`. Issue #4328: the thunk
// target type carries the field's only outbound semantic edge; without it the
// nested DTO / related entity rings. ToID is the `Class:<target>` stub the
// resolver binds to the real class entity (same-file or symbol-table wide).
func referencesClassEdge(fromID, targetClass, framework, fieldName string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: fromID,
		ToID:   "Class:" + targetClass,
		Kind:   string(types.RelationshipKindReferences),
		Properties: map[string]string{
			"framework":   framework,
			"ref_kind":    "field_target_type",
			"field_name":  fieldName,
			"target_type": targetClass,
			"provenance":  "INFERRED_FROM_DECORATOR_THUNK_TARGET",
		},
	}
}

// setProps merges extra key-value pairs into entity.Properties.
func setProps(e *types.EntityRecord, kv ...string) {
	if len(kv)%2 != 0 {
		return
	}
	for i := 0; i < len(kv); i += 2 {
		e.Properties[kv[i]] = kv[i+1]
	}
}

// lineStart returns the byte offset of the start of the line containing offset.
func lineStart(s string, offset int) int {
	if offset > len(s) {
		offset = len(s)
	}
	if i := strings.LastIndexByte(s[:offset], '\n'); i >= 0 {
		return i + 1
	}
	return 0
}

// lineEnd returns the byte offset just past the end of the line containing
// offset (exclusive of the newline, or len(s) at EOF).
func lineEnd(s string, offset int) int {
	if offset > len(s) {
		return len(s)
	}
	if i := strings.IndexByte(s[offset:], '\n'); i >= 0 {
		return offset + i
	}
	return len(s)
}

// isQuoteOrSpace returns true for characters that commonly surround string literals
// in JavaScript/TypeScript source (quotes, backtick, space, tab).
func isQuoteOrSpace(r rune) bool {
	return r == '\'' || r == '"' || r == '`' || r == ' ' || r == '\t'
}
