package java

// neo4j.go — custom extractor for Spring Data Neo4j entity mappings.
//
// Spring Data Neo4j (SDN) maps Java classes to graph database nodes and
// relationships via annotations from org.springframework.data.neo4j.core.schema.*
// and the legacy Neo4j OGM annotations (org.neo4j.ogm.annotation.*).
//
// Extracted entities
// ------------------
//   SCOPE.Schema / node         — one per @Node-annotated class
//   SCOPE.Schema / property     — one per @Property-annotated field
//   SCOPE.Schema / id_field     — one per @Id-annotated field
//   SCOPE.Component / relationship — one per @Relationship-annotated field
//
// Capability cells (issue #3098)
// --------------------------------
//   schema_extraction        — partial (@Node/@Property/@Id scanning)
//   association_extraction   — partial (@Relationship type/direction scanning)
//   relationship_extraction  — partial (@Relationship type/direction scanning)
//   foreign_key_extraction   — not_applicable (graph DB; no FK concept)
//   lazy_loading_recognition — not_applicable (graph DB; no lazy-loading concept)
//   migration_parsing        — not_applicable (graph DB; no migration files)
//
// Gate signals
// ------------
//   import org.springframework.data.neo4j
//   import org.neo4j.ogm.annotation
//   @Node / @NodeEntity / @Relationship / @RelationshipEntity / @Id + Neo4j import

import (
	"context"
	"regexp"
	"strings"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_java_neo4j", &neo4jExtractor{})
}

type neo4jExtractor struct{}

func (e *neo4jExtractor) Language() string { return "custom_java_neo4j" }

var (
	// Gate: Spring Data Neo4j or Neo4j OGM import.
	neo4jMarkerRE = regexp.MustCompile(
		`import\s+org\.springframework\.data\.neo4j\.|import\s+org\.neo4j\.(driver|ogm)\.|` +
			`@NodeEntity\b|@RelationshipEntity\b|Neo4jRepository\b`)

	// @Node("Label") or @Node — captures optional label value.
	neo4jNodeAnnoRE = regexp.MustCompile(
		`@Node\s*(?:\(\s*(?:value\s*=\s*)?"([^"]*)"[^)]*\))?`)

	// class declaration immediately following the @Node annotation.
	neo4jNodeClassRE = regexp.MustCompile(
		`(?s)@Node\s*(?:\([^)]*\)\s*)?` +
			`(?:@\w+(?:\s*\([^)]*\))?\s*)*` +
			`(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)

	// @NodeEntity (Neo4j OGM).
	neo4jNodeEntityRE = regexp.MustCompile(
		`(?s)@NodeEntity\s*(?:\([^)]*\)\s*)?` +
			`(?:@\w+(?:\s*\([^)]*\))?\s*)*` +
			`(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)

	// @Property("name") field annotation — captures property name.
	neo4jPropertyRE = regexp.MustCompile(
		`@Property\s*(?:\(\s*(?:value\s*=\s*)?"([^"]*)"[^)]*\))?`)

	// @Id annotation (marks the identifier field).
	neo4jIdFieldRE = regexp.MustCompile(`@Id\b`)

	// Field declaration after @Property or @Id:
	// "private String title;" / "private Long id;"
	neo4jFieldDeclRE = regexp.MustCompile(
		`(?:private|protected|public|)\s+(?:final\s+)?(?:\w+(?:<[^>]*>)?)\s+(\w+)\s*;`)

	// @Relationship(type = "ACTED_IN", direction = Direction.INCOMING)
	neo4jRelationshipRE = regexp.MustCompile(
		`@Relationship\s*\(([^)]*)\)`)

	// Extract type= from @Relationship attributes.
	neo4jRelTypeRE = regexp.MustCompile(`\btype\s*=\s*"([^"]+)"`)

	// Extract direction= from @Relationship attributes.
	neo4jRelDirRE = regexp.MustCompile(`\bdirection\s*=\s*(?:Direction\.)?(\w+)`)

	// Field declaration following @Relationship.
	neo4jRelFieldRE = regexp.MustCompile(
		`(?:private|protected|public|)\s+(?:(?:final|transient)\s+)*` +
			`(?:\w+(?:<(\w+)>)?|(\w+))\s+(\w+)\s*;`)
)

func (e *neo4jExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	if strings.ToLower(file.Language) != "java" {
		return nil, nil
	}
	src := string(file.Content)
	if !neo4jMarkerRE.MatchString(src) {
		return nil, nil
	}
	fp := file.Path

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// --- @Node classes (schema_extraction) ---
	type nodeInfo struct {
		name   string
		label  string
		offset int
	}
	var nodes []nodeInfo

	emitNode := func(className, label string, offset int) {
		if label == "" {
			label = className
		}
		ent := makeEntity(className, "SCOPE.Schema", "node", fp, file.Language, lineOf(src, offset))
		setProps(&ent, "framework", "neo4j",
			"node_label", label,
			"provenance", "INFERRED_FROM_NEO4J_NODE")
		add(ent)
		nodes = append(nodes, nodeInfo{name: className, label: label, offset: offset})
	}

	// Spring Data Neo4j @Node.
	for _, m := range neo4jNodeClassRE.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		// Try to extract the label from the @Node annotation in the same vicinity.
		label := ""
		snippet := src[max(0, m[0]-10) : m[0]+min(len(src)-m[0], 200)]
		if lm := neo4jNodeAnnoRE.FindStringSubmatch(snippet); lm != nil && lm[1] != "" {
			label = lm[1]
		}
		emitNode(className, label, m[0])
	}

	// Neo4j OGM @NodeEntity.
	for _, m := range neo4jNodeEntityRE.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		emitNode(className, "", m[0])
	}

	// owningNode returns the node class name whose declaration precedes offset.
	owningNode := func(offset int) string {
		var best string
		for _, n := range nodes {
			if n.offset <= offset {
				best = n.name
			}
		}
		return best
	}

	// --- @Id fields (schema_extraction — identifier) ---
	for _, m := range neo4jIdFieldRE.FindAllStringSubmatchIndex(src, -1) {
		// Find the field declaration following @Id.
		rest := src[m[1]:]
		fm := neo4jFieldDeclRE.FindStringSubmatchIndex(rest)
		if fm == nil {
			continue
		}
		fieldName := rest[fm[2]:fm[3]]
		owner := owningNode(m[0])
		name := fieldName
		if owner != "" {
			name = owner + "." + fieldName
		}
		ent := makeEntity(name, "SCOPE.Schema", "id_field", fp, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "neo4j",
			"field_name", fieldName,
			"owner_node", owner,
			"provenance", "INFERRED_FROM_NEO4J_ID")
		add(ent)
	}

	// --- @Property fields (schema_extraction — properties) ---
	for _, m := range neo4jPropertyRE.FindAllStringSubmatchIndex(src, -1) {
		propName := ""
		if m[2] >= 0 {
			propName = src[m[2]:m[3]]
		}
		// Find the field declaration following @Property.
		rest := src[m[1]:]
		fm := neo4jFieldDeclRE.FindStringSubmatchIndex(rest)
		if fm == nil {
			continue
		}
		fieldName := rest[fm[2]:fm[3]]
		if propName == "" {
			propName = fieldName
		}
		owner := owningNode(m[0])
		name := propName
		if owner != "" {
			name = owner + "." + propName
		}
		ent := makeEntity(name, "SCOPE.Schema", "property", fp, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "neo4j",
			"property_name", propName,
			"field_name", fieldName,
			"owner_node", owner,
			"provenance", "INFERRED_FROM_NEO4J_PROPERTY")
		add(ent)
	}

	// --- @Relationship fields (association_extraction + relationship_extraction) ---
	for _, m := range neo4jRelationshipRE.FindAllStringSubmatchIndex(src, -1) {
		attrs := src[m[2]:m[3]]
		relType := ""
		if tm := neo4jRelTypeRE.FindStringSubmatch(attrs); tm != nil {
			relType = tm[1]
		}
		direction := "OUTGOING"
		if dm := neo4jRelDirRE.FindStringSubmatch(attrs); dm != nil {
			direction = dm[1]
		}

		// Find the field declaration following @Relationship.
		rest := src[m[1]:]
		fm := neo4jRelFieldRE.FindStringSubmatchIndex(rest)
		if fm == nil {
			continue
		}
		// Capture generic type param or plain type, and field name.
		var targetType, fieldName string
		if fm[2] >= 0 {
			targetType = rest[fm[2]:fm[3]]
		} else if fm[4] >= 0 {
			targetType = rest[fm[4]:fm[5]]
		}
		fieldName = rest[fm[6]:fm[7]]

		owner := owningNode(m[0])
		name := relType
		if name == "" {
			name = fieldName
		}
		if owner != "" {
			name = owner + "." + name
		}

		ent := makeEntity(name, "SCOPE.Component", "relationship", fp, file.Language, lineOf(src, m[0]))
		kvs := []string{
			"framework", "neo4j",
			"relation_type", relType,
			"direction", direction,
			"field_name", fieldName,
			"owner_node", owner,
			"provenance", "INFERRED_FROM_NEO4J_RELATIONSHIP",
		}
		if targetType != "" {
			kvs = append(kvs, "target_node", targetType)
		}
		setProps(&ent, kvs...)
		add(ent)
	}

	return entities, nil
}
