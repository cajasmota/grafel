package python

// neo4j_neomodel.go — custom extractor for neomodel (Neo4j Python OGM) graph
// schema mappings.
//
// neomodel maps Python classes to Neo4j graph nodes and relationships:
//
//	from neomodel import StructuredNode, RelationshipTo, RelationshipFrom
//
//	class Person(StructuredNode):
//	    name   = StringProperty()
//	    movies = RelationshipTo('Movie', 'ACTED_IN')
//
//	class Movie(StructuredNode):
//	    title = StringProperty()
//
// A subclass of StructuredNode is the graph "node" (the analogue of a SQL
// table); each RelationshipTo/RelationshipFrom class attribute declares a
// typed, directed edge to another node label.
//
// Extracted entities
// ------------------
//	SCOPE.Schema / node          — one per StructuredNode subclass
//	SCOPE.Schema / property      — one per *Property() class attribute
//	SCOPE.Component / relationship — one per RelationshipTo/From attribute
//
// GRAPH_RELATES topology (#3609, epic #3606)
// ------------------------------------------
// Mirroring the Java Spring-Data-Neo4j template (#3663), every relationship
// whose target node label resolves to a same-file StructuredNode subclass also
// emits a traversable GRAPH_RELATES edge that hangs off the owner node entity:
//
//	Person ──GRAPH_RELATES(rel_type=ACTED_IN, direction=OUTGOING)──▶ Class:Movie
//
// RelationshipTo → direction OUTGOING; RelationshipFrom → direction INCOMING.
// The target label is the first string argument ('Movie'); the rel_type is the
// second string argument ('ACTED_IN'). The ToID uses the "Class:<Label>"
// convention so the intra-repo resolver's byName index links it to the target
// node entity (same convention as the Django model cross-refs and the Java
// SDN template). Cross-file target labels are honest-partial: no edge is
// emitted, the topology stays only as a `target_node` prop on the relationship
// Component.

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("python_neomodel", &NeomodelExtractor{})
}

// NeomodelExtractor emits graph-schema entities and GRAPH_RELATES topology for
// neomodel StructuredNode classes.
type NeomodelExtractor struct{}

func (e *NeomodelExtractor) Language() string { return "python_neomodel" }

var (
	// Gate: the file must import neomodel.
	neomodelImportRe = regexp.MustCompile(
		`(?m)^\s*(?:from\s+neomodel(?:\.\w+)*\s+import|import\s+neomodel)\b`)

	// class Person(StructuredNode): / class Rel(StructuredRel):
	// Captures group 1 = class name. Only StructuredNode subclasses are nodes.
	neomodelNodeClassRe = regexp.MustCompile(
		`(?m)^[ \t]*class\s+(\w+)\s*\(([^)]*)\)\s*:`)

	// A *Property() class attribute (StringProperty, IntegerProperty, …).
	//   name = StringProperty()
	neomodelPropertyRe = regexp.MustCompile(
		`(?m)^[ \t]+(\w+)\s*=\s*(\w*Property)\s*\(`)

	// RelationshipTo / RelationshipFrom / Relationship attribute.
	//   movies = RelationshipTo('Movie', 'ACTED_IN')
	//   movies = RelationshipFrom("Movie", "ACTED_IN", cardinality=ZeroOrMore)
	// Captures: group1 field name, group2 To|From|"" (plain Relationship),
	// group3 target-label literal, group4 rel-type literal (optional).
	neomodelRelRe = regexp.MustCompile(
		`(?m)^[ \t]+(\w+)\s*=\s*Relationship(To|From|)\s*\(\s*` +
			`['"]([A-Za-z_][\w.]*)['"]` +
			`(?:\s*,\s*['"]([^'"]+)['"])?`)
)

// neomodelLabel returns the short node label for a (possibly dotted) target
// reference such as 'myapp.models.Movie' → 'Movie'.
func neomodelLabel(ref string) string {
	if dot := strings.LastIndex(ref, "."); dot >= 0 {
		return ref[dot+1:]
	}
	return ref
}

func (e *NeomodelExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_neomodel")
	_, span := tracer.Start(ctx, "custom.python_neomodel")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	if !neomodelImportRe.MatchString(src) {
		return nil, nil
	}

	var out []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) int {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return -1
		}
		seen[key] = true
		out = append(out, ent)
		return len(out) - 1
	}

	// --- StructuredNode subclasses (the graph "nodes") ---
	type nodeInfo struct {
		name   string
		offset int
		idx    int // index into out of the node entity
	}
	var nodes []nodeInfo
	knownNodes := make(map[string]bool)

	for _, m := range neomodelNodeClassRe.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		bases := src[m[4]:m[5]]
		// Only StructuredNode subclasses are graph nodes. StructuredRel models
		// the relationship's own properties — not a node — so it is skipped as a
		// node (its props are still captured below as generic properties of
		// nothing; we just don't treat it as an edge endpoint).
		if !strings.Contains(bases, "StructuredNode") {
			continue
		}
		ent := entity(className, "SCOPE.Schema", "node", file.Path, lineOf(src, m[0]),
			map[string]string{
				"framework":  "neomodel",
				"node_label": className,
				"provenance": "INFERRED_FROM_NEO4J_NEOMODEL_NODE",
			})
		idx := add(ent)
		if idx >= 0 {
			nodes = append(nodes, nodeInfo{name: className, offset: m[0], idx: idx})
			knownNodes[className] = true
		}
	}

	// owningNode / owningNodeIdx resolve the StructuredNode whose class
	// declaration most closely precedes a class-body offset.
	owningNode := func(offset int) string {
		best := ""
		for _, n := range nodes {
			if n.offset <= offset {
				best = n.name
			}
		}
		return best
	}
	owningNodeIdx := func(offset int) int {
		best := -1
		for _, n := range nodes {
			if n.offset <= offset {
				best = n.idx
			}
		}
		return best
	}

	// --- *Property() attributes (schema_extraction — properties) ---
	for _, m := range neomodelPropertyRe.FindAllStringSubmatchIndex(src, -1) {
		fieldName := src[m[2]:m[3]]
		propType := src[m[4]:m[5]]
		owner := owningNode(m[0])
		if owner == "" {
			continue
		}
		ent := entity(owner+"."+fieldName, "SCOPE.Schema", "property", file.Path, lineOf(src, m[0]),
			map[string]string{
				"framework":     "neomodel",
				"property_name": fieldName,
				"property_type": propType,
				"owner_node":    owner,
				"provenance":    "INFERRED_FROM_NEO4J_NEOMODEL_PROPERTY",
			})
		add(ent)
	}

	// --- RelationshipTo/From attributes (relationship_extraction + topology) ---
	for _, m := range neomodelRelRe.FindAllStringSubmatchIndex(src, -1) {
		fieldName := src[m[2]:m[3]]
		toFrom := src[m[4]:m[5]] // "To", "From", or ""
		targetRef := src[m[6]:m[7]]
		relType := ""
		if m[8] >= 0 {
			relType = src[m[8]:m[9]]
		}
		targetLabel := neomodelLabel(targetRef)

		direction := "OUTGOING"
		if toFrom == "From" {
			direction = "INCOMING"
		}

		owner := owningNode(m[0])
		if owner == "" {
			continue
		}
		name := relType
		if name == "" {
			name = fieldName
		}
		name = owner + "." + name

		relEnt := entity(name, "SCOPE.Component", "relationship", file.Path, lineOf(src, m[0]),
			map[string]string{
				"framework":     "neomodel",
				"relation_type": relType,
				"direction":     direction,
				"field_name":    fieldName,
				"owner_node":    owner,
				"target_node":   targetLabel,
				"provenance":    "INFERRED_FROM_NEO4J_NEOMODEL_RELATIONSHIP",
			})
		add(relEnt)

		// --- GRAPH_RELATES edge (#3609, epic #3606) ---
		// Emit the domain graph schema as a traversable edge owner-node
		// ──GRAPH_RELATES──▶ target-node carrying rel_type + direction, instead
		// of leaving the topology encoded only as the `target_node` string prop.
		// Only when the target label resolves to a same-file StructuredNode
		// subclass — cross-file targets stay honest-partial (props only).
		ownerIdx := owningNodeIdx(m[0])
		if ownerIdx >= 0 && knownNodes[targetLabel] {
			out[ownerIdx].Relationships = append(out[ownerIdx].Relationships,
				types.RelationshipRecord{
					ToID: "Class:" + targetLabel,
					Kind: string(types.RelationshipKindGraphRelates),
					Properties: map[string]string{
						"framework":  "neomodel",
						"rel_type":   relType,
						"direction":  direction,
						"field_name": fieldName,
						"provenance": "INFERRED_FROM_NEO4J_NEOMODEL_RELATIONSHIP",
					},
				})
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
