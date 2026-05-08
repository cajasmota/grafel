package rust

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
	extractor.Register("custom_rust_rocket", &rocketExtractor{})
}

type rocketExtractor struct{}

func (e *rocketExtractor) Language() string { return "custom_rust_rocket" }

var (
	reRocketRoute = regexp.MustCompile(
		`#\[(get|post|put|delete|patch|head|options)\s*\(\s*"([^"]+)"\s*\)\][\s\S]*?fn\s+(\w+)\s*\(`,
	)
	reRocketCatch = regexp.MustCompile(
		`#\[catch\s*\(\s*(\d+)\s*\)\]`,
	)
	reRocketGuard = regexp.MustCompile(
		`impl\s+(?:<[^>]*>\s+)?FromRequest(?:<[^>]*>)?\s+for\s+(\w+)`,
	)
	reRocketFairing = regexp.MustCompile(
		`impl\s+(?:<[^>]*>\s+)?Fairing\s+for\s+(\w+)`,
	)
	reRocketDataGuard = regexp.MustCompile(
		`(?:Json|Form|Data|MsgPack)\s*<\s*([A-Za-z_]\w*)`,
	)
	reRocketBuild = regexp.MustCompile(
		`rocket::(build|ignite)\s*\(`,
	)
	reRocketState = regexp.MustCompile(
		`State\s*<\s*([A-Za-z_]\w*)`,
	)
)

func (e *rocketExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rocket_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "rocket"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "rust" {
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

	// 1. Route macros -> SCOPE.Operation/endpoint
	for _, m := range reRocketRoute.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToUpper(src[m[2]:m[3]])
		path := src[m[4]:m[5]]
		handler := src[m[6]:m[7]]
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rocket", "provenance", "INFERRED_FROM_ROCKET_ROUTE",
			"http_method", method, "route_pattern", path, "handler_name", handler)
		add(ent)
	}

	// 2. #[catch(N)] catchers -> SCOPE.Pattern
	for _, m := range reRocketCatch.FindAllStringSubmatchIndex(src, -1) {
		statusCode := src[m[2]:m[3]]
		ent := makeEntity("catch:"+statusCode, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rocket", "provenance", "INFERRED_FROM_ROCKET_CATCHER",
			"status_code", statusCode)
		add(ent)
	}

	// 3. impl FromRequest for T -> SCOPE.Pattern (request guard)
	for _, m := range reRocketGuard.FindAllStringSubmatchIndex(src, -1) {
		guardType := src[m[2]:m[3]]
		ent := makeEntity("guard:"+guardType, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rocket", "provenance", "INFERRED_FROM_ROCKET_GUARD",
			"guard_type", guardType)
		add(ent)
	}

	// 4. impl Fairing for T -> SCOPE.Pattern
	for _, m := range reRocketFairing.FindAllStringSubmatchIndex(src, -1) {
		fairingType := src[m[2]:m[3]]
		ent := makeEntity("fairing:"+fairingType, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rocket", "provenance", "INFERRED_FROM_ROCKET_FAIRING",
			"fairing_type", fairingType)
		add(ent)
	}

	// 5. Json<T>/Form<T>/Data<T>/MsgPack<T> -> SCOPE.Schema
	for _, m := range reRocketDataGuard.FindAllStringSubmatchIndex(src, -1) {
		typeParam := src[m[2]:m[3]]
		ent := makeEntity("data:"+typeParam, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rocket", "provenance", "INFERRED_FROM_ROCKET_DATA_GUARD",
			"type_param", typeParam)
		add(ent)
	}

	// 6. rocket::build()/rocket::ignite() -> SCOPE.Service
	for _, m := range reRocketBuild.FindAllStringSubmatchIndex(src, -1) {
		callName := src[m[2]:m[3]]
		ent := makeEntity("rocket::"+callName, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rocket", "provenance", "INFERRED_FROM_ROCKET_LAUNCH")
		add(ent)
	}

	// 7. State<T> -> SCOPE.Pattern
	for _, m := range reRocketState.FindAllStringSubmatchIndex(src, -1) {
		stateType := src[m[2]:m[3]]
		ent := makeEntity("state:"+stateType, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rocket", "provenance", "INFERRED_FROM_ROCKET_STATE",
			"state_type", stateType)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
