package javascript

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_js_nestjs", &nestjsExtractor{})
}

type nestjsExtractor struct{}

func (e *nestjsExtractor) Language() string { return "custom_js_nestjs" }

var (
	reNestModule = regexp.MustCompile(
		`@Module\s*\([^@]*?\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	reNestController = regexp.MustCompile(
		`@Controller\s*\([^@]*?\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	reNestInjectable = regexp.MustCompile(
		`@Injectable\s*\([^)]*\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	reNestHTTPMethod = regexp.MustCompile(
		`@(Get|Post|Put|Delete|Patch|Options|Head|All)\s*\(([^)]*)\)\s*(?:async\s+)?(\w+)\s*\(`,
	)
	reNestGuard = regexp.MustCompile(
		`(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)\s+(?:extends\s+\w+\s+)?implements\s+[^{]*\bCanActivate\b`,
	)
	reNestInterceptor = regexp.MustCompile(
		`(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)\s+(?:extends\s+\w+\s+)?implements\s+[^{]*\bNestInterceptor\b`,
	)
	reNestGateway = regexp.MustCompile(
		`@WebSocketGateway\s*\([^@]*?\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	reNestSubscribeMessage = regexp.MustCompile(
		`@SubscribeMessage\s*\(([^)]*)\)\s*(?:async\s+)?(\w+)\s*\(`,
	)
	reNestResolver = regexp.MustCompile(
		`@Resolver\s*\([^@]*?\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	reNestQuery = regexp.MustCompile(
		`@Query\s*\((?:[^()]*|\([^()]*\))*\)\s*(?:async\s+)?(\w+)\s*\(`,
	)
	reNestMutation = regexp.MustCompile(
		`@Mutation\s*\((?:[^()]*|\([^()]*\))*\)\s*(?:async\s+)?(\w+)\s*\(`,
	)
	reNestSubscription = regexp.MustCompile(
		`@Subscription\s*\((?:[^()]*|\([^()]*\))*\)\s*(?:async\s+)?(\w+)\s*\(`,
	)
	reNestPipe = regexp.MustCompile(
		`(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)\s+(?:extends\s+\w+\s+)?implements\s+[^{]*\bPipeTransform\b`,
	)
	reNestMessagePattern = regexp.MustCompile(
		`@MessagePattern\s*\(([^)]*)\)\s*(?:async\s+)?(\w+)\s*\(`,
	)
	reNestEventPattern = regexp.MustCompile(
		`@EventPattern\s*\(([^)]*)\)\s*(?:async\s+)?(\w+)\s*\(`,
	)
	reNestCatch = regexp.MustCompile(
		`@Catch\s*\([^@]*?\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	reNestCron = regexp.MustCompile(
		`@Cron\s*\(([^)]*)\)\s*(?:async\s+)?(\w+)\s*\(`,
	)
	reNestInterval = regexp.MustCompile(
		`@Interval\s*\(([^)]*)\)\s*(?:async\s+)?(\w+)\s*\(`,
	)
	reNestCreateParamDecorator = regexp.MustCompile(
		`(?:export\s+)?const\s+([A-Z][A-Za-z0-9_]*)\s*=\s*createParamDecorator\s*\(`,
	)
	reNestPathString = regexp.MustCompile(`['"]([^'"]*?)['"]`)
)

var nestHTTPVerbMap = map[string]string{
	"Get": "GET", "Post": "POST", "Put": "PUT", "Delete": "DELETE",
	"Patch": "PATCH", "Options": "OPTIONS", "Head": "HEAD", "All": "ALL",
}

func (e *nestjsExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.nestjs_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "nestjs"),
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

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	addEntity := func(ent types.EntityRecord) {
		key := fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Name, ent.Subtype)
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// @Module
	for _, m := range reNestModule.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "module", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_MODULE")
		addEntity(ent)
	}

	// @Controller
	for _, m := range reNestController.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "controller", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_CONTROLLER")
		addEntity(ent)
	}

	// @Injectable
	for _, m := range reNestInjectable.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "service", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_INJECTABLE")
		addEntity(ent)
	}

	// HTTP verb methods
	for _, m := range reNestHTTPMethod.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		pathArg := src[m[4]:m[5]]
		methodName := src[m[6]:m[7]]
		httpMethod := nestHTTPVerbMap[verb]
		routePath := ""
		if pm := reNestPathString.FindStringSubmatch(pathArg); pm != nil {
			routePath = pm[1]
		}
		name := fmt.Sprintf("%s %s", httpMethod, methodName)
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "http_method", httpMethod,
			"route_path", routePath, "method_name", methodName,
			"provenance", "INFERRED_FROM_NESTJS_ROUTE")
		addEntity(ent)
	}

	// Guards
	for _, m := range reNestGuard.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "guard", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_GUARD")
		addEntity(ent)
	}

	// Interceptors
	for _, m := range reNestInterceptor.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "interceptor", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_INTERCEPTOR")
		addEntity(ent)
	}

	// WebSocket gateways
	for _, m := range reNestGateway.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "gateway", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_GATEWAY")
		addEntity(ent)
	}

	// @SubscribeMessage
	for _, m := range reNestSubscribeMessage.FindAllStringSubmatchIndex(src, -1) {
		eventArg := src[m[2]:m[3]]
		methodName := src[m[4]:m[5]]
		event := ""
		if pm := reNestPathString.FindStringSubmatch(eventArg); pm != nil {
			event = pm[1]
		}
		name := fmt.Sprintf("subscribe:%s", methodName)
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "event", event, "method_name", methodName,
			"provenance", "INFERRED_FROM_NESTJS_SUBSCRIBE_MESSAGE")
		addEntity(ent)
	}

	// @Resolver
	for _, m := range reNestResolver.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "resolver", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_RESOLVER")
		addEntity(ent)
	}

	// @Query
	for _, m := range reNestQuery.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_GRAPHQL_QUERY")
		addEntity(ent)
	}

	// @Mutation
	for _, m := range reNestMutation.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "mutation", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_GRAPHQL_MUTATION")
		addEntity(ent)
	}

	// @Subscription (GraphQL)
	for _, m := range reNestSubscription.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "subscription", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_GRAPHQL_SUBSCRIPTION")
		addEntity(ent)
	}

	// PipeTransform
	for _, m := range reNestPipe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "pipe", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_PIPE")
		addEntity(ent)
	}

	// @MessagePattern
	for _, m := range reNestMessagePattern.FindAllStringSubmatchIndex(src, -1) {
		patternArg := src[m[2]:m[3]]
		methodName := src[m[4]:m[5]]
		name := fmt.Sprintf("msg:%s", methodName)
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "pattern_arg", patternArg, "method_name", methodName,
			"provenance", "INFERRED_FROM_NESTJS_MESSAGE_PATTERN")
		addEntity(ent)
	}

	// @EventPattern
	for _, m := range reNestEventPattern.FindAllStringSubmatchIndex(src, -1) {
		patternArg := src[m[2]:m[3]]
		methodName := src[m[4]:m[5]]
		name := fmt.Sprintf("event:%s", methodName)
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "pattern_arg", patternArg, "method_name", methodName,
			"provenance", "INFERRED_FROM_NESTJS_EVENT_PATTERN")
		addEntity(ent)
	}

	// @Catch (exception filter)
	for _, m := range reNestCatch.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "exception_filter", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_CATCH")
		addEntity(ent)
	}

	// @Cron
	for _, m := range reNestCron.FindAllStringSubmatchIndex(src, -1) {
		cronExpr := strings.TrimFunc(src[m[2]:m[3]], isQuoteOrSpace)
		methodName := src[m[4]:m[5]]
		name := fmt.Sprintf("cron:%s", methodName)
		ent := makeEntity(name, "SCOPE.Operation", "job", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "cron_expression", cronExpr, "method_name", methodName,
			"provenance", "INFERRED_FROM_NESTJS_CRON")
		addEntity(ent)
	}

	// @Interval
	for _, m := range reNestInterval.FindAllStringSubmatchIndex(src, -1) {
		intervalArg := strings.TrimSpace(src[m[2]:m[3]])
		methodName := src[m[4]:m[5]]
		name := fmt.Sprintf("interval:%s", methodName)
		ent := makeEntity(name, "SCOPE.Operation", "job", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "interval_ms", intervalArg, "method_name", methodName,
			"provenance", "INFERRED_FROM_NESTJS_INTERVAL")
		addEntity(ent)
	}

	// createParamDecorator
	for _, m := range reNestCreateParamDecorator.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "param_decorator", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_PARAM_DECORATOR")
		addEntity(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
