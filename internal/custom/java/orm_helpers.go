package java

import (
	"github.com/cajasmota/archigraph/internal/types"
)

// javaClassRef returns the resolvable stub naming a Java class entity. The
// `Class:<Name>` form binds through the resolver's byName fallback to the base
// tree-sitter SCOPE.Component class node (Java classes are extracted as
// SCOPE.Component/class), mirroring the JS/Python/Ruby/Go ORM field-membership
// convention (#4328/#4366/#4367).
func javaClassRef(className string) string { return "Class:" + className }

// containsFieldRel builds the structural CONTAINS membership edge from an
// owning entity/DTO class to one of its column / association / validation field
// entities. Issue #4367 (JVM generalization of #4328/#4366): JPA/Hibernate
// column + association fields and Bean Validation DTO fields were emitted as
// standalone `<Owner>.<field>` graph nodes with no owning-class membership,
// leaving them as degree-0 orphans on the graph.
//
// The edge is hung off the FIELD entity (SourceRef = fieldRef, the carrier) but
// its source resolves to the OWNING CLASS via FromName=`Class:<owner>` — the Java
// dispatch maps FromName onto the edge's FromID, which ReferencesEmbedded
// rewrites by name to the real (separately-extracted) class entity.
func containsFieldRel(ownerClass, fieldRef, fieldName, framework string) Relationship {
	return Relationship{
		SourceRef:        fieldRef,
		TargetRef:        fieldRef,
		FromName:         javaClassRef(ownerClass),
		RelationshipType: string(types.RelationshipKindContains),
		Properties: map[string]string{
			"framework":  framework,
			"member":     "field",
			"field_name": fieldName,
			"provenance": "INFERRED_FROM_MODEL_FIELD_MEMBERSHIP",
		},
	}
}

// referencesClassRel builds a REFERENCES edge from a relation/validation field
// entity to the class it points at — a JPA @ManyToOne/@OneToMany/@OneToOne/
// @ManyToMany/@Embedded target type (the generic element type for collection
// relations, e.g. List<Item> -> Item) or a Bean Validation @Valid nested DTO
// type. Issue #4367: that target type is the field's only outbound semantic
// edge; without it the related entity / nested DTO rings.
//
// The edge is hung off the field entity (SourceRef = fieldRef) and its ToID is
// `Class:<target>`, which the resolver binds to the real class entity by name.
func referencesClassRel(fieldRef, targetClass, fieldName, framework string) Relationship {
	return Relationship{
		SourceRef:        fieldRef,
		TargetRef:        javaClassRef(targetClass),
		RelationshipType: string(types.RelationshipKindReferences),
		Properties: map[string]string{
			"framework":   framework,
			"ref_kind":    "field_target_type",
			"field_name":  fieldName,
			"target_type": targetClass,
			"provenance":  "INFERRED_FROM_MODEL_FIELD_TARGET",
		},
	}
}

// makeEntity builds a minimal EntityRecord with all required fields set.
// Mirrors the helper used by the registered JS/Kotlin custom extractors so the
// Java ORM extractors (Ebean / EclipseLink / MyBatis) emit registry-shaped
// records via the custom_java_* dispatch path.
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

// setProps merges extra key-value pairs into entity.Properties.
func setProps(e *types.EntityRecord, kv ...string) {
	if len(kv)%2 != 0 {
		return
	}
	for i := 0; i < len(kv); i += 2 {
		e.Properties[kv[i]] = kv[i+1]
	}
}
