package swift

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
	extractor.Register("custom_swift_vapor", &vaporExtractor{})
}

type vaporExtractor struct{}

func (e *vaporExtractor) Language() string { return "custom_swift_vapor" }

var (
	reVaporRoute = regexp.MustCompile(
		`(?m)(?:app|routes|grouped)\.(get|post|put|delete|patch|options)\s*\(\s*"([^"]+)"`,
	)
	reVaporGrouped = regexp.MustCompile(
		`(?m)(?:app|routes)\.grouped\s*\(\s*"([^"]+)"`,
	)
	reVaporRouteCollection = regexp.MustCompile(
		`(?m)(?:struct|class)\s+(\w+)\s*:\s*RouteCollection\b`,
	)
	reVaporFluentModel = regexp.MustCompile(
		`(?m)final\s+class\s+(\w+)\s*:\s*Model\b`,
	)
	reVaporFluentField = regexp.MustCompile(
		`@Field\s*\(\s*key:\s*"([^"]+)"\s*\)`,
	)
	reVaporMiddleware = regexp.MustCompile(
		`(?m)(?:struct|class|final\s+class)\s+(\w+)\s*:\s*Middleware\b`,
	)
	reVaporLeafRender = regexp.MustCompile(
		`req\.view\.render\s*\(\s*"([^"]+)"`,
	)
)

func (e *vaporExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/swift")
	_, span := tracer.Start(ctx, "indexer.vapor_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "vapor"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "swift" {
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

	// 1. HTTP routes -> SCOPE.Operation/endpoint
	for _, m := range reVaporRoute.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToUpper(src[m[2]:m[3]])
		path := src[m[4]:m[5]]
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_ROUTE",
			"http_method", method, "route_path", path)
		add(ent)
	}

	// 2. .grouped("/prefix") -> SCOPE.Component
	for _, m := range reVaporGrouped.FindAllStringSubmatchIndex(src, -1) {
		prefix := src[m[2]:m[3]]
		ent := makeEntity("group:"+prefix, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_GROUPED",
			"route_prefix", prefix)
		add(ent)
	}

	// 3. RouteCollection structs -> SCOPE.Component/controller
	for _, m := range reVaporRouteCollection.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "controller", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_ROUTE_COLLECTION")
		add(ent)
	}

	// 4. Fluent @Model classes -> SCOPE.Schema/model
	for _, m := range reVaporFluentModel.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "model", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_FLUENT_MODEL")
		add(ent)
	}

	// 5. @Field(key:) -> SCOPE.Schema
	for _, m := range reVaporFluentField.FindAllStringSubmatchIndex(src, -1) {
		fieldKey := src[m[2]:m[3]]
		ent := makeEntity("field:"+fieldKey, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_FLUENT_FIELD",
			"field_key", fieldKey)
		add(ent)
	}

	// 6. Middleware protocol conformance -> SCOPE.Pattern/middleware
	for _, m := range reVaporMiddleware.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_MIDDLEWARE")
		add(ent)
	}

	// 7. Leaf template renders -> SCOPE.Component
	for _, m := range reVaporLeafRender.FindAllStringSubmatchIndex(src, -1) {
		templateName := src[m[2]:m[3]]
		ent := makeEntity("template:"+templateName, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_LEAF_TEMPLATE",
			"template_name", templateName)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
