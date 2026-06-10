// Package ruby provides regex-based custom extractors for Ruby source files.
// Each extractor targets a specific framework and registers via init().
package ruby

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

func lineOf(source string, offset int) int {
	return strings.Count(source[:offset], "\n") + 1
}

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

func setProps(e *types.EntityRecord, kv ...string) {
	if len(kv)%2 != 0 {
		return
	}
	for i := 0; i < len(kv); i += 2 {
		e.Properties[kv[i]] = kv[i+1]
	}
}

// rbClassRef returns the structural reference used as the FromID/ToID for an
// edge targeting a Ruby class entity (an ActiveRecord/Sequel/ROM model). The
// `Class:<Name>` form resolves through the resolver's byName fallback to the
// SCOPE.Schema/SCOPE.Component model node the various extractors emit — the same
// convention already used by the ActiveRecord GRAPH_RELATES edges.
func rbClassRef(className string) string { return "Class:" + className }

// containsFieldEdge builds the structural CONTAINS membership edge from an
// owning model class to one of its association / foreign-key / validation
// field entities. Issue #4367 (Ruby generalization of #4328/#4366): ActiveRecord
// association declarations (has_many/belongs_to/...) and validation declarations
// were emitted as standalone `<macro>:<name>` SCOPE.Pattern nodes with no
// owning-model membership, leaving them as degree-0 orphans on the graph.
//
// FromID names the owner class (`Class:<owner>`) so the resolver binds it to the
// real model entity; ToID is the member entity's own ID (the caller passes
// memberID). The edge is hung off the owner model node by the caller (mirroring
// the JS/TS and Python fixes).
func containsFieldEdge(ownerClass, memberID, fieldName, framework string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: rbClassRef(ownerClass),
		ToID:   memberID,
		Kind:   string(types.RelationshipKindContains),
		Properties: map[string]string{
			"framework":  framework,
			"language":   "ruby",
			"member":     "field",
			"field_name": fieldName,
			"provenance": "INFERRED_FROM_MODEL_FIELD_MEMBERSHIP",
		},
	}
}

// referencesClassEdge builds a REFERENCES edge from an association field entity
// to the model class it points at — a belongs_to/has_many/has_one target,
// singularized + camelized (`:items` → `Item`) or honoring `class_name:`.
// Issue #4367: that target model is the association field's only outbound
// semantic edge; without it the related model rings.
//
// FromID is the association entity's own ID; ToID is the `Class:<target>` stub
// the resolver binds to the real model entity. The edge is hung off the
// association entity by the caller.
func referencesClassEdge(memberID, targetClass, framework, fieldName string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: memberID,
		ToID:   rbClassRef(targetClass),
		Kind:   string(types.RelationshipKindReferences),
		Properties: map[string]string{
			"framework":   framework,
			"language":    "ruby",
			"ref_kind":    "field_target_type",
			"field_name":  fieldName,
			"target_type": targetClass,
			"provenance":  "INFERRED_FROM_MODEL_FIELD_TARGET",
		},
	}
}

// boolStr converts a bool to its "true"/"false" string representation.
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
