// neo4j_activegraph.go — custom extractor for activegraph / neo4j.rb
// (the Neo4j Ruby OGM) graph-schema mappings. #3614, epic #3606.
//
// activegraph (formerly neo4j.rb / Neo4j::ActiveNode) maps Ruby classes to
// Neo4j graph nodes and relationships through a decorator-style DSL, NOT through
// Cypher query strings — so it is the Ruby analogue of the Python neomodel
// OGM (#3670) and the Java Spring-Data-Neo4j template (#3663), and the closest
// model to mirror:
//
//	class Person
//	  include ActiveGraph::Node           # (or legacy: Neo4j::ActiveNode)
//	  property :name
//	  has_many :out, :movies, type: :ACTED_IN, model_class: 'Movie'
//	  has_one  :in,  :studio, type: :OWNS,     model_class: 'Studio'
//	end
//
// A class that includes ActiveGraph::Node (or Neo4j::ActiveNode) is the graph
// "node" (the analogue of a SQL table / a neomodel StructuredNode); the node
// label is the class name. Each has_many / has_one declares a typed, directed
// edge to another node label:
//
//	has_many :out, ...  → direction OUTGOING
//	has_many :in,  ...  → direction INCOMING
//	type:    :ACTED_IN  → the Neo4j relationship type
//	model_class: 'Movie'→ the target node label
//
// Extracted entities
// ------------------
//
//	SCOPE.Schema / node          — one per ActiveGraph::Node class
//	SCOPE.Schema / property      — one per `property :name` declaration
//	SCOPE.Component / relationship — one per has_many / has_one association
//
// GRAPH_RELATES topology (#3614, epic #3606)
// ------------------------------------------
// Mirroring the neomodel template (#3670) and the Java SDN template (#3663),
// every association whose model_class resolves to a same-file ActiveGraph::Node
// class also emits a traversable GRAPH_RELATES edge that hangs off the owner
// node entity:
//
//	Person ──GRAPH_RELATES(rel_type=ACTED_IN, direction=OUTGOING)──▶ Class:Movie
//
// `:out` → direction OUTGOING; `:in` → direction INCOMING. The rel_type is the
// `type:` symbol/string; the target label is `model_class:`. The ToID uses the
// "Class:<Label>" convention so the intra-repo resolver's byName index links it
// to the target node entity (same convention as neomodel and the Java SDN
// template). Cross-file / dynamic model_class targets are honest-partial: no
// edge is emitted, the topology stays only as a `target_node` prop on the
// relationship Component.
//
// Registration key: "custom_ruby_neo4j_activegraph"
package ruby

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_ruby_neo4j_activegraph", &neo4jActiveGraphExtractor{})
}

// neo4jActiveGraphExtractor emits graph-schema entities and GRAPH_RELATES
// topology for activegraph / neo4j.rb ActiveGraph::Node classes.
type neo4jActiveGraphExtractor struct{}

func (e *neo4jActiveGraphExtractor) Language() string {
	return "custom_ruby_neo4j_activegraph"
}

var (
	// Gate: the file must include the OGM node mixin (current activegraph or the
	// legacy neo4j.rb name).
	reAGNodeMixin = regexp.MustCompile(
		`(?m)^\s*include\s+(?:ActiveGraph::Node|Neo4j::ActiveNode)\b`)

	// class Person / class Person < Foo — captures group 1 = class name.
	reAGClass = regexp.MustCompile(
		`(?m)^[ \t]*class\s+([A-Z][A-Za-z0-9_]*)`)

	// end of a class body (de-dent `end` at column-ish). Used only to scope
	// node-membership: a class is a node iff a node-mixin include appears between
	// its `class` line and the next `class` line.

	// property :name  /  property :name, type: Integer
	reAGProperty = regexp.MustCompile(
		`(?m)^[ \t]+property\s+:([a-z_][A-Za-z0-9_]*)`)

	// has_many / has_one association. activegraph requires the direction symbol
	// (:out / :in) as the first argument and the association name as the second:
	//
	//   has_many :out, :movies, type: :ACTED_IN, model_class: 'Movie'
	//   has_one  :in,  :studio, type: 'OWNS',     model_class: 'Studio'
	//
	// Captures: 1 macro (has_many|has_one), 2 direction (out|in), 3 assoc name.
	// type: and model_class: are pulled out of the trailing options separately so
	// their order does not matter.
	reAGAssociation = regexp.MustCompile(
		`(?m)^[ \t]+(has_many|has_one)\s+:(out|in)\s*,\s*:([a-z_][A-Za-z0-9_]*)([^\n]*)`)

	// type: :ACTED_IN  /  type: 'ACTED_IN'  /  type: "ACTED_IN"
	reAGRelType = regexp.MustCompile(`type:\s*['":]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)

	// model_class: 'Movie'  /  model_class: "Movie"  /  model_class: Movie
	reAGModelClass = regexp.MustCompile(`model_class:\s*['"]?([A-Za-z_][A-Za-z0-9_:]*)['"]?`)
)

// agLabel returns the short node label for a possibly-namespaced model_class
// reference such as 'Graph::Movie' → 'Movie'.
func agLabel(ref string) string {
	if i := strings.LastIndex(ref, "::"); i >= 0 {
		return ref[i+2:]
	}
	return ref
}

func (e *neo4jActiveGraphExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/ruby")
	_, span := tracer.Start(ctx, "indexer.ruby_neo4j_activegraph_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "activegraph"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if file.Language != "ruby" || len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	if !reAGNodeMixin.MatchString(src) {
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

	// --- ActiveGraph::Node classes (the graph "nodes") ---
	// A class is a node iff a node-mixin include falls in its class body, i.e.
	// between its `class` offset and the offset of the next `class`.
	type nodeInfo struct {
		name   string
		offset int
		idx    int // index into out of the node entity
	}
	var nodes []nodeInfo
	knownNodes := make(map[string]bool)

	classMatches := reAGClass.FindAllStringSubmatchIndex(src, -1)
	mixinMatches := reAGNodeMixin.FindAllStringIndex(src, -1)
	classHasMixin := func(start, end int) bool {
		for _, mm := range mixinMatches {
			if mm[0] > start && (end < 0 || mm[0] < end) {
				return true
			}
		}
		return false
	}

	for ci, cm := range classMatches {
		className := src[cm[2]:cm[3]]
		classStart := cm[0]
		classEnd := -1
		if ci+1 < len(classMatches) {
			classEnd = classMatches[ci+1][0]
		}
		if !classHasMixin(classStart, classEnd) {
			continue
		}
		ent := makeEntity(className, "SCOPE.Schema", "node", file.Path, file.Language, lineOf(src, classStart))
		setProps(&ent,
			"framework", "activegraph",
			"node_label", className,
			"provenance", "INFERRED_FROM_NEO4J_ACTIVEGRAPH_NODE",
		)
		idx := add(ent)
		if idx >= 0 {
			nodes = append(nodes, nodeInfo{name: className, offset: classStart, idx: idx})
			knownNodes[className] = true
		}
	}

	if len(nodes) == 0 {
		span.SetAttributes(attribute.Int("entity_count", len(out)))
		return out, nil
	}

	// owningNode / owningNodeIdx resolve the ActiveGraph::Node class whose
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

	// --- property declarations (schema_extraction — properties) ---
	for _, m := range reAGProperty.FindAllStringSubmatchIndex(src, -1) {
		propName := src[m[2]:m[3]]
		owner := owningNode(m[0])
		if owner == "" {
			continue
		}
		ent := makeEntity(owner+"."+propName, "SCOPE.Schema", "property", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "activegraph",
			"property_name", propName,
			"owner_node", owner,
			"provenance", "INFERRED_FROM_NEO4J_ACTIVEGRAPH_PROPERTY",
		)
		add(ent)
	}

	// --- has_many / has_one associations (relationship_extraction + topology) ---
	for _, m := range reAGAssociation.FindAllStringSubmatchIndex(src, -1) {
		macro := src[m[2]:m[3]]     // has_many | has_one
		dirSym := src[m[4]:m[5]]    // out | in
		assocName := src[m[6]:m[7]] // movies
		opts := src[m[8]:m[9]]      // ", type: :ACTED_IN, model_class: 'Movie'"

		relType := ""
		if rm := reAGRelType.FindStringSubmatch(opts); rm != nil {
			relType = rm[1]
		}
		targetRef := ""
		if mc := reAGModelClass.FindStringSubmatch(opts); mc != nil {
			targetRef = mc[1]
		}
		targetLabel := agLabel(targetRef)

		direction := "OUTGOING"
		if dirSym == "in" {
			direction = "INCOMING"
		}

		owner := owningNode(m[0])
		if owner == "" {
			continue
		}
		name := relType
		if name == "" {
			name = assocName
		}
		name = owner + "." + name

		relEnt := makeEntity(name, "SCOPE.Component", "relationship", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&relEnt,
			"framework", "activegraph",
			"relation_type", relType,
			"direction", direction,
			"macro", macro,
			"field_name", assocName,
			"owner_node", owner,
			"target_node", targetLabel,
			"provenance", "INFERRED_FROM_NEO4J_ACTIVEGRAPH_RELATIONSHIP",
		)
		add(relEnt)

		// --- GRAPH_RELATES edge (#3614, epic #3606) ---
		// Emit the domain graph schema as a traversable edge owner-node
		// ──GRAPH_RELATES──▶ target-node carrying rel_type + direction, instead
		// of leaving the topology encoded only as the `target_node` string prop.
		// Only when model_class resolves to a same-file ActiveGraph::Node class —
		// cross-file / dynamic targets stay honest-partial (props only).
		ownerIdx := owningNodeIdx(m[0])
		if ownerIdx >= 0 && knownNodes[targetLabel] {
			out[ownerIdx].Relationships = append(out[ownerIdx].Relationships,
				types.RelationshipRecord{
					ToID: "Class:" + targetLabel,
					Kind: string(types.RelationshipKindGraphRelates),
					Properties: types.Props{
						{K: "direction", V: direction},
						{K: "field_name", V: assocName},
						{K: "framework", V: "activegraph"},
						{K: "provenance", V: "INFERRED_FROM_NEO4J_ACTIVEGRAPH_RELATIONSHIP"},
						{K: "rel_type", V: relType},
					},
				})
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
