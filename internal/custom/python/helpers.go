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

	"github.com/cajasmota/grafel/internal/extractor"
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
// FromID is the field entity's qualified Name (`<owner>.<field>`).
//
// ToID is the STRUCTURAL address
// `scope:component:class:python:<declaring_file>:<Target>`, not the
// `Class:<Target>` bare-name stub this helper shipped until #6986. Measured
// reason, gate-ON on the 16-repo corpus: 917 `field_target_type` edges, 840
// (91.6%) DANGLING, essentially all Python (django 880 edges / 836 dangling).
// `Class:<Target>` reaches the resolver's bare-name tier, which returns
// AMBIGUOUS the moment two entities share the name — and on Django that is the
// ordinary case twice over: a second app declaring `Author`, and, for a
// UNIQUELY declared model, grafel's own untwinned rule-pack `Model` node
// sitting beside the tree-sitter class anchor (#6981). Only ~54% of the loss is
// duplicate names; the other ~40% is a unique class made ambiguous by grafel.
//
// The address is deliberately built from the CONSUMER's file — the file this
// per-file extractor is reading — and NOT from the target's file, which a
// pass-1 extractor cannot know. That is not a compromise: it is the Python
// dialect the resolver already speaks. internal/resolve/refs.go's
// lookupStructural resolves `scope:component:class:python:<file>:<Name>` in two
// tiers — same-file `lookupLocationKind`, then, for `component` scope with
// lang=="python" only, `lookupUniqueRealComponentByName`, which binds when
// exactly one entity in the whole graph declares the name. The Python
// hierarchy extractor has emitted EXTENDS targets in exactly this shape, with
// exactly this consumer's-file convention, since the flask-realworld wave.
//
// So this pass is NOT same-file-only the way the C# port (#6984) had to be:
// a cross-file target still binds when its declaration is globally unique, and
// an ambiguous or absent target still dangles.
//
// EDGE COUNT: the producer emits the SAME NUMBER OF RECORDS as before — one per
// relational field, unchanged — but the GRAPH gains edges, measured 863 -> 949
// on django gate-ON. The 86 are not new references: a field whose own entity is
// an unresolved stub (`Person.friends` exists in four files) used to collapse
// with its namesakes because their `Class:<Target>` ToIDs were byte-identical,
// and a file-qualified ToID no longer collapses. So "never more edges" is FALSE
// under the graph reading and must not be written here; what holds is "never a
// new reference, and never a guessed one".
//
// NEVER A GUESSED ONE is an assertion, not a claim:
// TestPythonFieldTargetType_NeverBindsToANonDeclaringFile requires that no bound
// field_target_type edge points at an entity in a file that does not declare
// that name, and names the case it must refuse — a `shop/models.py` field may
// not reach the `billing/models.py` `Customer`. Recall cannot detect
// over-firing, so that forbidden row is the only thing grading this direction.
func referencesClassEdge(filePath, memberName, targetClass, framework, fieldName string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: memberName,
		ToID:   extractor.BuildComponentStructuralRef("python", filePath, targetClass),
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
