package csharp

import (
	"context"
	"regexp"

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

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
