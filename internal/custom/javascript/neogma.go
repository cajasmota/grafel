package javascript

// neogma.go — custom extractor for neogma (Neo4j JS/TS OGM) graph schema
// mappings.
//
// neogma maps JS/TS model objects to Neo4j graph nodes and relationships via
// ModelFactory:
//
//	import { ModelFactory } from 'neogma';
//
//	const Person = ModelFactory({
//	  label: 'Person',
//	  schema: { name: { type: 'string', required: true } },
//	  relationships: {
//	    actedIn: { model: Movie, name: 'ACTED_IN', direction: 'out' },
//	  },
//	}, neogma);
//
//	const Movie = ModelFactory({ label: 'Movie', schema: { ... } }, neogma);
//
// Each ModelFactory(...) call is a graph "node" (the analogue of a SQL table)
// whose label comes from `label:`; the `relationships:` block declares typed,
// directed edges to other node models.
//
// Extracted entities
// ------------------
//	SCOPE.Schema / node          — one per ModelFactory call (label = node label)
//	SCOPE.Component / relationship — one per relationships.<key> entry
//
// GRAPH_RELATES topology (#3610, epic #3606)
// ------------------------------------------
// Mirroring the Java Spring-Data-Neo4j template (#3663), every relationship
// whose `model:` reference resolves to a same-file ModelFactory binding also
// emits a traversable GRAPH_RELATES edge hanging off the owner node entity:
//
//	Person ──GRAPH_RELATES(rel_type=ACTED_IN, direction=OUTGOING)──▶ Class:Movie
//
// direction 'out' → OUTGOING, 'in' → INCOMING, 'none'/absent → preserved as-is
// (defaulting OUTGOING). The ToID uses the "Class:<Label>" convention so the
// intra-repo resolver's byName index links it to the target node entity (same
// convention as the Java SDN / neomodel templates). Cross-file `model:`
// targets are honest-partial: no edge is emitted; topology stays as the
// `target_model` prop on the relationship Component.

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_js_neogma", &neogmaExtractor{})
}

type neogmaExtractor struct{}

func (e *neogmaExtractor) Language() string { return "custom_js_neogma" }

var (
	// Gate: the file must reference neogma / ModelFactory.
	neogmaMarkerRe = regexp.MustCompile(`\bneogma\b|\bModelFactory\b`)

	// const Person = ModelFactory({ ... }, neogma)
	// Captures group 1 = binding variable (the model handle other models
	// reference via `model:`).
	neogmaModelAssignRe = regexp.MustCompile(
		`(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*(?::\s*[^=]+?)?=\s*ModelFactory\s*\(`)

	// label: 'Person'  (within a ModelFactory config object)
	neogmaLabelRe = regexp.MustCompile(`\blabel\s*:\s*['"]([A-Za-z_][\w]*)['"]`)

	// A relationships.<key> entry. Captures:
	//   group1 relationship key (field name)
	//   group2 target model binding (the `model:` reference)
	//   group3 rel name literal (the `name:`)
	//   group4 direction literal (the `direction:`), optional
	// model / name / direction may appear in any order; we capture name &
	// direction independently below to stay order-tolerant, but the common
	// canonical order is matched here for the model+name pairing.
	neogmaRelKeyRe = regexp.MustCompile(
		`([A-Za-z_$][\w$]*)\s*:\s*\{([^{}]*)\}`)

	neogmaRelModelRe = regexp.MustCompile(`\bmodel\s*:\s*([A-Za-z_$][\w$]*)`)
	neogmaRelNameRe  = regexp.MustCompile(`\bname\s*:\s*['"]([^'"]+)['"]`)
	neogmaRelDirRe   = regexp.MustCompile(`\bdirection\s*:\s*['"](out|in|none)['"]`)

	// relationships: { ... } block locator (to scope rel-key scanning).
	neogmaRelBlockRe = regexp.MustCompile(`\brelationships\s*:\s*\{`)
)

// matchedBrace returns the body between the brace at openIdx (which must point
// at '{') and its matching close brace, plus the index just past the close.
func matchedBrace(s string, openIdx int) (string, int) {
	if openIdx < 0 || openIdx >= len(s) || s[openIdx] != '{' {
		return "", openIdx
	}
	depth := 0
	for i := openIdx; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[openIdx+1 : i], i + 1
			}
		}
	}
	return s[openIdx+1:], len(s)
}

func (e *neogmaExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.neogma_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "neogma"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	lang := strings.ToLower(file.Language)
	if lang != "typescript" && lang != "javascript" {
		return nil, nil
	}
	if !neogmaMarkerRe.MatchString(src) {
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

	// --- ModelFactory node models (the graph "nodes") ---
	type modelInfo struct {
		binding string // the const variable (referenced by `model:`)
		label   string // the Neo4j node label
		body    string // the ModelFactory config object body
		idx     int    // index into out of the node entity
	}
	var models []modelInfo
	// bindingToLabel resolves a `model:` reference (a binding variable) to the
	// target node label so GRAPH_RELATES ToID uses the label, not the var.
	bindingToLabel := make(map[string]string)

	for _, m := range neogmaModelAssignRe.FindAllStringSubmatchIndex(src, -1) {
		binding := src[m[2]:m[3]]
		// The config object starts at the '(' just past the match; find the
		// first '{' after the ModelFactory( and read the balanced object body.
		callOpen := m[1] - 1 // index of the '(' captured by the regex tail
		braceOpen := strings.IndexByte(src[callOpen:], '{')
		if braceOpen < 0 {
			continue
		}
		braceOpen += callOpen
		body, _ := matchedBrace(src, braceOpen)

		label := binding
		if lm := neogmaLabelRe.FindStringSubmatch(body); lm != nil {
			label = lm[1]
		}
		ent := makeEntity(label, "SCOPE.Schema", "node", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "neogma",
			"node_label", label,
			"binding", binding,
			"provenance", "INFERRED_FROM_NEO4J_NEOGMA_NODE")
		idx := add(ent)
		if idx >= 0 {
			models = append(models, modelInfo{binding: binding, label: label, body: body, idx: idx})
			bindingToLabel[binding] = label
		}
	}

	// --- relationships within each ModelFactory ---
	for _, owner := range models {
		// Locate the relationships: { ... } block within this model's body.
		bm := neogmaRelBlockRe.FindStringIndex(owner.body)
		if bm == nil {
			continue
		}
		braceOpen := strings.IndexByte(owner.body[bm[1]-1:], '{')
		if braceOpen < 0 {
			continue
		}
		braceOpen += bm[1] - 1
		relsBody, _ := matchedBrace(owner.body, braceOpen)

		for _, km := range neogmaRelKeyRe.FindAllStringSubmatch(relsBody, -1) {
			fieldName := km[1]
			cfg := km[2]

			targetBinding := ""
			if mm := neogmaRelModelRe.FindStringSubmatch(cfg); mm != nil {
				targetBinding = mm[1]
			}
			relType := ""
			if nm := neogmaRelNameRe.FindStringSubmatch(cfg); nm != nil {
				relType = nm[1]
			}
			direction := "OUTGOING"
			if dm := neogmaRelDirRe.FindStringSubmatch(cfg); dm != nil {
				switch dm[1] {
				case "in":
					direction = "INCOMING"
				case "none":
					direction = "NONE"
				default:
					direction = "OUTGOING"
				}
			}
			// A relationship entry must carry at least a model: reference.
			if targetBinding == "" {
				continue
			}

			name := relType
			if name == "" {
				name = fieldName
			}
			name = owner.label + "." + name

			targetLabel := bindingToLabel[targetBinding] // "" if cross-file
			relEnt := makeEntity(name, "SCOPE.Component", "relationship", file.Path, file.Language, lineOf(src, 0))
			props := []string{
				"framework", "neogma",
				"relation_type", relType,
				"direction", direction,
				"field_name", fieldName,
				"owner_node", owner.label,
				"target_model", targetBinding,
				"provenance", "INFERRED_FROM_NEO4J_NEOGMA_RELATIONSHIP",
			}
			if targetLabel != "" {
				props = append(props, "target_node", targetLabel)
			}
			setProps(&relEnt, props...)
			add(relEnt)

			// --- GRAPH_RELATES edge (#3610, epic #3606) ---
			// Only when the `model:` reference resolves to a same-file
			// ModelFactory binding — cross-file targets are honest-partial.
			if targetLabel != "" {
				out[owner.idx].Relationships = append(out[owner.idx].Relationships,
					types.RelationshipRecord{
						ToID: "Class:" + targetLabel,
						Kind: string(types.RelationshipKindGraphRelates),
						Properties: map[string]string{
							"framework":  "neogma",
							"rel_type":   relType,
							"direction":  direction,
							"field_name": fieldName,
							"provenance": "INFERRED_FROM_NEO4J_NEOGMA_RELATIONSHIP",
						},
					})
			}
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
