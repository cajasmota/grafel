package csharp

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_csharp_aspnet_reqresp", &aspnetReqRespExtractor{})
}

type aspnetReqRespExtractor struct{}

func (e *aspnetReqRespExtractor) Language() string { return "custom_csharp_aspnet_reqresp" }

var (
	reFromBody = regexp.MustCompile(
		`\[FromBody\]\s+(\w+(?:<[^>]+>)?)\s+\w+`,
	)
	reReturnType = regexp.MustCompile(
		`(?m)public\s+(?:async\s+)?(?:Task<)?(?:ActionResult<([^>]+)>|IActionResult|([A-Z][A-Za-z0-9_]*))\s*>?\s+\w+\s*\(`,
	)
	reActionResultUnwrap = regexp.MustCompile(
		`ActionResult<([^>]+)>`,
	)
	// Action-method signature: captures the return-type clause and the action
	// method name so endpoint→DTO edges (#3629) can anchor on the action.
	reActionMethod = regexp.MustCompile(
		`(?m)public\s+(?:async\s+)?(?:Task<)?(ActionResult<[^>]+>|IActionResult|[A-Z][A-Za-z0-9_]*)\s*>?\s+(\w+)\s*\(`,
	)
)

func (e *aspnetReqRespExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.aspnet_request_response_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "aspnet_core"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "csharp" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// 1. [FromBody] parameter types -> SCOPE.Component (request DTO)
	for _, m := range reFromBody.FindAllStringSubmatchIndex(src, -1) {
		dtoType := src[m[2]:m[3]]
		if csharpPrimitives[dtoType] {
			continue
		}
		ent := makeEntity(dtoType, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_FROM_BODY",
			"dto_kind", "request")
		add(ent)
	}

	// 2. ActionResult<T> return types -> SCOPE.Schema (response type)
	for _, m := range reReturnType.FindAllStringSubmatchIndex(src, -1) {
		var typeName string
		if m[2] >= 0 {
			typeName = src[m[2]:m[3]]
		} else if m[4] >= 0 {
			typeName = src[m[4]:m[5]]
		}
		if typeName == "" || csharpPrimitives[typeName] {
			continue
		}
		// Unwrap ActionResult<T>
		if um := reActionResultUnwrap.FindStringSubmatch(typeName); um != nil {
			typeName = um[1]
		}
		if csharpPrimitives[typeName] {
			continue
		}
		ent := makeEntity(typeName, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_RETURN_TYPE",
			"dto_kind", "response")
		add(ent)
	}

	// 3. Endpoint→DTO edges (#3629). Anchor an action operation entity per
	//    action method and emit:
	//      ACCEPTS_INPUT : action -> [FromBody] request DTO
	//      RETURNS       : action -> ActionResult<T> / concrete response DTO
	//    Mirrors the Java Spring extractor so expand/traces/payload_drift can
	//    traverse endpoint→DTO. FromID is set explicitly to the action entity
	//    ID (C# computes entity IDs eagerly); ToID is a Class:<Name> structural
	//    ref the intra-repo resolver binds to the real DTO class by name.
	for _, m := range reActionMethod.FindAllStringSubmatchIndex(src, -1) {
		returnClause := src[m[2]:m[3]]
		methodName := src[m[4]:m[5]]
		line := lineOf(src, m[0])

		// Balanced-paren parameter block for this action.
		params, _ := aspnetParamsBlock(src, m[1])

		var rels []types.RelationshipRecord

		// ACCEPTS_INPUT: [FromBody] Dto param.
		if bm := reFromBody.FindStringSubmatch(params); bm != nil {
			dtoType := aspnetUnwrapGeneric(bm[1])
			if dtoType != "" && !csharpPrimitives[dtoType] {
				rels = append(rels, types.RelationshipRecord{
					ToID:       "Class:" + dtoType,
					Kind:       string(types.RelationshipKindAcceptsInput),
					Properties: map[string]string{"framework": "aspnet_core", "match_source": "from_body_param", "dto_type": dtoType},
				})
			}
		}

		// RETURNS: ActionResult<T> or concrete type return clause.
		if um := reActionResultUnwrap.FindStringSubmatch(returnClause); um != nil {
			returnClause = um[1]
		}
		retType := aspnetUnwrapGeneric(returnClause)
		if retType != "" && !csharpPrimitives[retType] {
			rels = append(rels, types.RelationshipRecord{
				ToID:       "Class:" + retType,
				Kind:       string(types.RelationshipKindReturns),
				Properties: map[string]string{"framework": "aspnet_core", "match_source": "action_return_type", "dto_type": retType},
			})
		}

		if len(rels) == 0 {
			continue
		}
		action := makeEntity(methodName, "SCOPE.Operation", "endpoint", file.Path, file.Language, line)
		setProps(&action, "framework", "aspnet_core", "pattern_type", "action_endpoint")
		action.Relationships = rels
		for i := range action.Relationships {
			action.Relationships[i].FromID = action.ID
		}
		add(action)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// aspnetParamsBlock returns the text between the opening paren (whose end
// offset is openParenEnd) and its matching close paren.
func aspnetParamsBlock(src string, openParenEnd int) (string, int) {
	depth := 1
	i := openParenEnd
	for i < len(src) && depth > 0 {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		i++
	}
	if depth != 0 {
		return "", openParenEnd
	}
	return src[openParenEnd : i-1], i - 1
}

// aspnetUnwrapGeneric strips a single generic wrapper (e.g. List<Order> ->
// Order, IEnumerable<Dto> -> Dto) and returns the base type name. Returns the
// bare type when there is no wrapper. Returns "" when the inner type is empty.
func aspnetUnwrapGeneric(raw string) string {
	raw = strings.TrimSpace(raw)
	if lt := strings.IndexByte(raw, '<'); lt >= 0 {
		base := strings.TrimSpace(raw[:lt])
		gt := strings.LastIndexByte(raw, '>')
		if gt > lt {
			inner := strings.TrimSpace(raw[lt+1 : gt])
			// Collection/wrapper bases unwrap to their element type.
			if csharpPrimitives[base] {
				return aspnetUnwrapGeneric(inner)
			}
			return base
		}
		return base
	}
	return raw
}
