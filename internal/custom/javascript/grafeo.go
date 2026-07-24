package javascript

// grafeo.go — custom extractor for grafeo-ogm (Neo4j TS OGM) graph-schema
// mappings.
//
// grafeo-ogm (https://github.com/neomodular/grafeo-ogm) is a type-safe
// Object-Graph Mapper for Neo4j driven by GraphQL SDL. Unlike its sibling OGMs
// neomodel (Python classes) and neogma (JS ModelFactory calls), grafeo declares
// the entire graph model in GraphQL Schema Definition Language — either in a
// standalone `.graphql` file or inline in a TS template literal passed as
// `typeDefs` to `new OGM({ typeDefs, driver })`:
//
//	type Author @node implements Entity {
//	  id: ID! @id @unique
//	  name: String!
//	  books: [Book!]! @relationship(type: "WRITTEN_BY", direction: IN, properties: "WrittenBy")
//	}
//
//	type Book @node {
//	  id: ID! @id @unique
//	  title: String!
//	  author: Author! @relationship(type: "WRITTEN_BY", direction: OUT)
//	}
//
//	type WrittenBy @relationshipProperties {
//	  role: String
//	}
//
// A `type`/`interface` carrying the `@node` directive is the graph "node" (the
// analogue of a SQL table); its Neo4j label is the type name (or the first
// label in `@node(labels: [...])`). Each field carrying a `@relationship(type,
// direction)` directive declares a typed, directed edge whose TARGET node is the
// field's GraphQL type (the named, possibly list/non-null, output type).
//
// A `type ... @relationshipProperties` block is an EDGE-property type, NOT a
// node, and is deliberately excluded (negative case) — matching grafeo's own
// schema-parser which separates `nodes` from `relationshipProperties`.
//
// Extracted entities
// ------------------
//	SCOPE.Schema / node           — one per @node type/interface (label = label)
//	SCOPE.Component / relationship — one per @relationship field
//
// GRAPH_RELATES topology (epic #3606, sibling #3609 neomodel / #3610 neogma)
// -------------------------------------------------------------------------
// Mirroring the neomodel/neogma/Java-SDN templates, every @relationship field
// whose target GraphQL type resolves to a same-document @node type also emits a
// traversable GRAPH_RELATES edge hanging off the owner node entity:
//
//	Book ──GRAPH_RELATES(rel_type=WRITTEN_BY, direction=OUTGOING)──▶ Class:Author
//
// direction OUT → OUTGOING, IN → INCOMING (grafeo has no NONE direction). The
// ToID uses the "Class:<Label>" convention so the intra-repo resolver's byName
// index links it to the target node entity (same convention as neomodel/neogma
// and the Java SDN template). A relationship whose target GraphQL type is NOT a
// declared @node type in the same document (e.g. a cross-file / external type)
// is honest-partial: no edge is emitted, the topology stays only as the
// `target_node` prop on the relationship Component.

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extreg.Register("custom_js_grafeo", &grafeoExtractor{})
}

type grafeoExtractor struct{}

func (e *grafeoExtractor) Language() string { return "custom_js_grafeo" }

var (
	// Gate: the source must carry grafeo SDL signal — the @node directive plus a
	// @relationship directive, OR an explicit grafeo-ogm import/marker. This keeps
	// the extractor from firing on unrelated GraphQL SDL (e.g. Lighthouse) and on
	// TS files that merely mention graphql.
	grafeoNodeDirectiveRe = regexp.MustCompile(`@node\b`)
	grafeoRelDirectiveRe  = regexp.MustCompile(`@relationship\b`)
	grafeoMarkerRe        = regexp.MustCompile(`\bgrafeo-ogm\b|\bnew\s+OGM\s*\(`)

	// type|interface <Name> ... { — the type-system definition header. Captures
	// group1 = the keyword (type/interface), group2 = type name, group3 = the
	// directives + implements clause up to the opening brace.
	grafeoTypeHeaderRe = regexp.MustCompile(
		`(?m)^[ \t]*(type|interface)\s+([A-Za-z_][\w]*)\b([^{]*)\{`)

	// @node(labels: ["Entity", "User"]) — optional explicit label list. First
	// label is the primary Neo4j node label.
	grafeoNodeLabelsRe = regexp.MustCompile(`@node\s*\(\s*labels\s*:\s*\[\s*['"]([A-Za-z_][\w]*)['"]`)

	// @relationshipProperties — marks an edge-property type (NOT a node).
	grafeoRelPropsDirectiveRe = regexp.MustCompile(`@relationshipProperties\b`)

	// A field line carrying a @relationship directive. Captures:
	//   group1 field name
	//   group2 the GraphQL output type (e.g. `[Book!]!`, `Author!`, `Author`)
	// The @relationship(...) args are parsed separately (order-tolerant).
	grafeoRelFieldRe = regexp.MustCompile(
		`(?m)^[ \t]*([A-Za-z_][\w]*)\s*:\s*([\[\]A-Za-z_!][\w!\[\] ]*?)\s+@relationship\b`)

	grafeoRelTypeRe     = regexp.MustCompile(`@relationship\s*\([^)]*\btype\s*:\s*['"]([^'"]+)['"]`)
	grafeoRelDirRe      = regexp.MustCompile(`@relationship\s*\([^)]*\bdirection\s*:\s*(OUT|IN)\b`)
	grafeoRelPropsArgRe = regexp.MustCompile(`@relationship\s*\([^)]*\bproperties\s*:\s*['"]([^'"]+)['"]`)
)

// grafeoNamedType strips GraphQL list/non-null wrappers from a field output
// type, returning the underlying named type: `[Book!]!` → `Book`, `Author!` →
// `Author`, `Author` → `Author`.
func grafeoNamedType(t string) string {
	t = strings.TrimSpace(t)
	t = strings.ReplaceAll(t, "[", "")
	t = strings.ReplaceAll(t, "]", "")
	t = strings.ReplaceAll(t, "!", "")
	t = strings.TrimSpace(t)
	// In case of trailing whitespace-separated tokens, keep the first.
	if sp := strings.IndexAny(t, " \t"); sp >= 0 {
		t = t[:sp]
	}
	return t
}

func (e *grafeoExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.grafeo_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "grafeo-ogm"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	lang := strings.ToLower(file.Language)
	switch lang {
	case "typescript", "javascript", "graphql":
	default:
		return nil, nil
	}

	// Gate: require grafeo SDL signal. The schema must declare at least one
	// @node and one @relationship (the OGM's defining surface), or carry an
	// explicit grafeo-ogm marker alongside @node. This avoids firing on the
	// Lighthouse PHP GraphQL schemas (which use @all/@paginate, never @node).
	hasNode := grafeoNodeDirectiveRe.MatchString(src)
	if !hasNode {
		return nil, nil
	}
	if !grafeoRelDirectiveRe.MatchString(src) && !grafeoMarkerRe.MatchString(src) {
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

	// --- Pass 1: discover @node types (the graph "nodes") ---
	type nodeInfo struct {
		typeName string // GraphQL type name (referenced by relationship targets)
		label    string // Neo4j node label (typeName or @node(labels:[...]) first)
		bodyOff  int    // byte offset of the '{' opening the type body
		idx      int    // index into out of the node entity
	}
	var nodes []nodeInfo
	// typeToLabel resolves a target GraphQL type name to its node label so the
	// GRAPH_RELATES ToID uses the label, not the raw type name (they differ only
	// when @node(labels:[...]) overrides). Only @node types are registered, so a
	// relationship targeting a non-node (e.g. @relationshipProperties type or an
	// external type) is honest-partial.
	typeToLabel := make(map[string]string)

	headers := grafeoTypeHeaderRe.FindAllStringSubmatchIndex(src, -1)
	for _, h := range headers {
		typeName := src[h[4]:h[5]]
		directives := src[h[6]:h[7]]
		braceOpen := h[1] - 1 // the matched '{'

		// @relationshipProperties types are edge-property types, NOT nodes.
		if grafeoRelPropsDirectiveRe.MatchString(directives) {
			continue
		}
		// Only @node types/interfaces are graph nodes.
		if !grafeoNodeDirectiveRe.MatchString(directives) {
			continue
		}

		label := typeName
		if lm := grafeoNodeLabelsRe.FindStringSubmatch(directives); lm != nil {
			label = lm[1]
		}

		ent := makeEntity(label, "SCOPE.Schema", "node", file.Path, file.Language, lineOf(src, h[0]))
		setProps(&ent, "framework", "grafeo-ogm",
			"node_label", label,
			"type_name", typeName,
			"provenance", "INFERRED_FROM_NEO4J_GRAFEO_NODE")
		idx := add(ent)
		if idx >= 0 {
			nodes = append(nodes, nodeInfo{typeName: typeName, label: label, bodyOff: braceOpen, idx: idx})
			typeToLabel[typeName] = label
		}
	}

	// --- Pass 2: relationships within each @node type body ---
	for _, owner := range nodes {
		body, _ := matchedBrace(src, owner.bodyOff)

		for _, fm := range grafeoRelFieldRe.FindAllStringSubmatch(body, -1) {
			fieldName := fm[1]
			targetType := grafeoNamedType(fm[2])

			// Locate the @relationship(...) args for this field. Scan from the
			// field-name occurrence in the body.
			relType := ""
			direction := "OUTGOING"
			relProps := ""

			// Find the directive substring on this field's line.
			fieldIdx := strings.Index(body, fm[0])
			lineSeg := fm[0]
			if fieldIdx >= 0 {
				// Extend the segment to the end of the @relationship(...) args.
				rest := body[fieldIdx:]
				if end := strings.IndexByte(rest, '\n'); end >= 0 {
					lineSeg = rest[:end]
				} else {
					lineSeg = rest
				}
			}
			if tm := grafeoRelTypeRe.FindStringSubmatch(lineSeg); tm != nil {
				relType = tm[1]
			}
			if dm := grafeoRelDirRe.FindStringSubmatch(lineSeg); dm != nil {
				if dm[1] == "IN" {
					direction = "INCOMING"
				} else {
					direction = "OUTGOING"
				}
			}
			if pm := grafeoRelPropsArgRe.FindStringSubmatch(lineSeg); pm != nil {
				relProps = pm[1]
			}

			name := relType
			if name == "" {
				name = fieldName
			}
			name = owner.label + "." + name

			targetLabel := typeToLabel[targetType] // "" if target is not a @node type

			relEnt := makeEntity(name, "SCOPE.Component", "relationship", file.Path, file.Language, lineOf(src, owner.bodyOff))
			props := []string{
				"framework", "grafeo-ogm",
				"relation_type", relType,
				"direction", direction,
				"field_name", fieldName,
				"owner_node", owner.label,
				"target_type", targetType,
				"provenance", "INFERRED_FROM_NEO4J_GRAFEO_RELATIONSHIP",
			}
			if relProps != "" {
				props = append(props, "relationship_properties", relProps)
			}
			if targetLabel != "" {
				props = append(props, "target_node", targetLabel)
			}
			setProps(&relEnt, props...)
			add(relEnt)

			// --- GRAPH_RELATES edge (epic #3606) ---
			// Only when the target GraphQL type resolves to a same-document @node
			// type — cross-file / non-node targets are honest-partial.
			if targetLabel != "" {
				edgeProps := types.Props{
					{K: "direction", V: direction},
					{K: "field_name", V: fieldName},
					{K: "framework", V: "grafeo-ogm"},
					{K: "provenance", V: "INFERRED_FROM_NEO4J_GRAFEO_RELATIONSHIP"},
					{K: "rel_type", V: relType},
				}
				if relProps != "" {
					edgeProps.Set("relationship_properties", relProps)
				}
				out[owner.idx].Relationships = append(out[owner.idx].Relationships,
					types.RelationshipRecord{
						ToID:       "Class:" + targetLabel,
						Kind:       string(types.RelationshipKindGraphRelates),
						Properties: edgeProps,
					})
			}
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
