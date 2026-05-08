package scala

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_scala_akka", &akkaExtractor{})
}

type akkaExtractor struct{}

func (e *akkaExtractor) Language() string { return "custom_scala_akka" }

var (
	reAkkaClassicActor = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+(?:Actor|AbstractActor)\b`,
	)
	reAkkaTypedActor = regexp.MustCompile(
		`(?m)(?:class|object)\s+(\w+)\s+extends\s+(?:AbstractBehavior|Behavior)\b`,
	)
	reAkkaSpawn = regexp.MustCompile(
		`(?m)context\.(?:spawn|actorOf)\s*\(\s*(?:Props\s*\(\s*new\s+)?(\w+)`,
	)
	reAkkaHTTPRoute = regexp.MustCompile(
		`(?m)(get|post|put|delete|patch|head|options)\s*\{`,
	)
	reAkkaPathPrefix = regexp.MustCompile(
		`(?m)pathPrefix\s*\(\s*"([^"]+)"\s*\)`,
	)
	reAkkaPath = regexp.MustCompile(
		`(?m)\bpath\s*\(\s*"([^"]+)"\s*\)`,
	)
	reAkkaSealedTrait = regexp.MustCompile(
		`(?m)sealed\s+trait\s+(\w+)\b`,
	)
	reAkkaCaseClass = regexp.MustCompile(
		`(?m)case\s+class\s+(\w+)\s*(?:\([^)]*\))?\s+extends\s+(\w+)\b`,
	)
	reAkkaReceive = regexp.MustCompile(
		`(?m)case\s+(\w+)(?:\s*\([^)]*\))?\s*=>`,
	)
)

func (e *akkaExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/scala")
	_, span := tracer.Start(ctx, "indexer.akka_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "akka"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "scala" {
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

	// 1. Classic Actor classes -> SCOPE.Service
	for _, m := range reAkkaClassicActor.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "akka", "provenance", "INFERRED_FROM_AKKA_ACTOR",
			"actor_type", "classic")
		add(ent)
	}

	// 2. Typed Actor classes -> SCOPE.Service
	for _, m := range reAkkaTypedActor.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "akka", "provenance", "INFERRED_FROM_AKKA_ACTOR",
			"actor_type", "typed")
		add(ent)
	}

	// 3. context.spawn / context.actorOf -> SCOPE.Component (spawn reference)
	for _, m := range reAkkaSpawn.FindAllStringSubmatchIndex(src, -1) {
		childActor := src[m[2]:m[3]]
		ent := makeEntity("spawn:"+childActor, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "akka", "provenance", "INFERRED_FROM_AKKA_SPAWN",
			"child_actor", childActor)
		add(ent)
	}

	// 4. pathPrefix("/api") -> SCOPE.Pattern
	for _, m := range reAkkaPathPrefix.FindAllStringSubmatchIndex(src, -1) {
		prefix := src[m[2]:m[3]]
		ent := makeEntity("prefix:"+prefix, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "akka", "provenance", "INFERRED_FROM_AKKA_HTTP_PATH_PREFIX",
			"path_prefix", prefix)
		add(ent)
	}

	// 5. path("name") -> SCOPE.Operation/endpoint
	for _, m := range reAkkaPath.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ent := makeEntity(path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "akka", "provenance", "INFERRED_FROM_AKKA_HTTP_ROUTE",
			"route_path", path)
		add(ent)
	}

	// 6. HTTP method directives -> SCOPE.Operation/endpoint
	for _, m := range reAkkaHTTPRoute.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToUpper(src[m[2]:m[3]])
		ent := makeEntity("route:"+method, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "akka", "provenance", "INFERRED_FROM_AKKA_HTTP_ROUTE",
			"http_method", method)
		add(ent)
	}

	// 7. sealed trait (message protocol) -> SCOPE.Pattern
	for _, m := range reAkkaSealedTrait.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "akka", "provenance", "INFERRED_FROM_AKKA_MESSAGE_PROTOCOL",
			"protocol_kind", "sealed_trait")
		add(ent)
	}

	// 8. case class extends Trait -> SCOPE.Pattern (message)
	for _, m := range reAkkaCaseClass.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "akka", "provenance", "INFERRED_FROM_AKKA_MESSAGE_PROTOCOL",
			"protocol_kind", "case_class")
		add(ent)
	}

	// 9. receive pattern match cases -> SCOPE.Operation/function
	for _, m := range reAkkaReceive.FindAllStringSubmatchIndex(src, -1) {
		msgType := src[m[2]:m[3]]
		ent := makeEntity("receive:"+msgType, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "akka", "provenance", "INFERRED_FROM_AKKA_RECEIVE",
			"message_type", msgType)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
