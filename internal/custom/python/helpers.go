// Package python provides regex-based framework extractors for Python code.
//
// Each extractor targets a specific framework (Django, FastAPI, Flask, etc.)
// and registers itself with a key like "python_django". These extractors
// complement the tree-sitter base Python extractor by capturing framework-
// specific patterns (decorators, class-based views, ORM models, etc.) that
// tree-sitter grammars do not model.
package python

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// lineOf returns the 1-indexed line number for a byte offset in source.
func lineOf(source string, offset int) int {
	return strings.Count(source[:offset], "\n") + 1
}

// entity builds an EntityRecord with the common fields pre-filled.
//
// ENDLINE IS ANCHORED TO STARTLINE, NOT LEFT AT ZERO (#6118). This was the one
// EntityRecord construction site in the whole of internal/custom/** that
// populated StartLine and left EndLine at its zero value — every other
// language's helper (csharp, java, javascript, golang, ruby, rust, php, kotlin,
// scala, swift, dart, lua, cpp, elixir, fsharp, crystal, groovy, nim, …) emits
// EndLine == lineNum. A zero EndLine is not "line zero", it is "extent
// unknown": any consumer that slices source by extent (grafel_get_source,
// docgen, the dashboard code panels, entity-size metrics) gets an unanchored
// region rather than a one-line one.
//
// These extractors are regex-based and match a single declaration line, so the
// honest extent IS one line. If a caller knows a wider extent it should widen
// EndLine after calling; MergeWithCustom's span union never narrows it.
func entity(name, kind, subtype, sourceFile string, startLine int, props map[string]string) types.EntityRecord {
	return types.EntityRecord{
		Name:               name,
		Kind:               kind,
		Subtype:            subtype,
		SourceFile:         sourceFile,
		StartLine:          startLine,
		EndLine:            startLine,
		Language:           "python",
		Properties:         props,
		EnrichmentRequired: true,
	}
}

// allMatchesIndex returns all matches with their byte positions.
func allMatchesIndex(re *regexp.Regexp, source string) [][]int {
	return re.FindAllStringSubmatchIndex(source, -1)
}

// pyClassRef returns the structural reference ID used as the FromID/ToID for
// edges targeting a Python class entity (Django/SQLAlchemy/Pydantic model,
// DRF serializer, ...). The "Class:<Name>" form resolves through the resolver's
// byName fallback to the SCOPE.Schema/SCOPE.Component class node the various
// Python extractors emit — the same convention already used by the SQLAlchemy
// GRAPH_RELATES and Django HANDLES_SIGNAL/REGISTERS edges.
func pyClassRef(className string) string { return "Class:" + className }

// containsFieldEdge builds the structural CONTAINS membership edge from an
// owning model/serializer/schema class to one of its field/column/attribute
// entities. Issue #4366 (Python generalization of #4328): Django model fields,
// SQLAlchemy columns/relationships, Pydantic fields and DRF serializer fields
// were emitted as standalone `<Class>.<field>` nodes with no owning-class
// membership, leaving them as orphans on the graph.
//
// FromID names the owner class (`Class:<owner>`) so the resolver binds it to
// the real class entity; ToID is the field entity's qualified Name
// (`<owner>.<field>`), which resolves merge-stably through the byName index
// regardless of when entity IDs are backfilled. The edge is hung off the owner
// model node by the caller (mirroring the JS/TS fix).
func containsFieldEdge(ownerClass, memberName, fieldName, framework string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: pyClassRef(ownerClass),
		ToID:   memberName,
		Kind:   string(types.RelationshipKindContains),
		Properties: types.Props{
			{K: "field_name", V: fieldName},
			{K: "framework", V: framework},
			{K: "language", V: "python"},
			{K: "member", V: "field"},
			{K: "provenance", V: "INFERRED_FROM_MODEL_FIELD_MEMBERSHIP"},
		},
	}
}

// referencesClassEdge builds a REFERENCES edge from a field/column entity to the
// class it points at — a Django ForeignKey/OneToOne/ManyToMany target, a
// SQLAlchemy relationship('Other') target, a DRF nested-serializer target, or a
// Pydantic field whose annotated type is another model. Issue #4366: that target
// type is the field's only outbound semantic edge; without it the related model /
// nested serializer rings.
//
// FromID is the field entity's qualified Name (`<owner>.<field>`); ToID is the
// `Class:<target>` stub the resolver binds to the real class entity (same-file
// or symbol-table wide). The edge is hung off the field entity by the caller.
func referencesClassEdge(memberName, targetClass, framework, fieldName string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: memberName,
		ToID:   pyClassRef(targetClass),
		Kind:   string(types.RelationshipKindReferences),
		Properties: types.Props{
			{K: "field_name", V: fieldName},
			{K: "framework", V: framework},
			{K: "language", V: "python"},
			{K: "provenance", V: "INFERRED_FROM_MODEL_FIELD_TARGET"},
			{K: "ref_kind", V: "field_target_type"},
			{K: "target_type", V: targetClass},
		},
	}
}

// decoratorWindow returns the contiguous block of stacked decorator lines that
// immediately precede the byte offset `at` (the start of a route decorator
// match), plus everything from there up to `end`. It walks backwards over
// consecutive `@…` / comment / blank lines so a sibling decorator such as
// slowapi's `@limiter.limit("5/minute")` — which the route regex cannot include
// in its own match (the regex tail only permits comments before `def`) — is
// still visible to the rate-limit resolver. Used for endpoint-level throttle
// stamping (#3628 rate-limit child).
func decoratorWindow(source string, at, end int) string {
	if at < 0 || at > len(source) || end < at || end > len(source) {
		return ""
	}
	start := at
	// Walk back line-by-line while the preceding line is a decorator, comment,
	// or blank line (the conventional stacked-decorator block).
	for start > 0 {
		// Find the start of the line that ends just before `start`.
		lineEnd := start - 1 // index of the '\n' terminating the previous line
		if lineEnd < 0 || source[lineEnd] != '\n' {
			break
		}
		lineStart := strings.LastIndexByte(source[:lineEnd], '\n') + 1
		line := strings.TrimSpace(source[lineStart:lineEnd])
		if line == "" || strings.HasPrefix(line, "@") || strings.HasPrefix(line, "#") {
			start = lineStart
			continue
		}
		break
	}
	return source[start:end]
}
