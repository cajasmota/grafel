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
	extractor.Register("custom_csharp_aspnet_core", &aspnetCoreExtractor{})
}

type aspnetCoreExtractor struct{}

func (e *aspnetCoreExtractor) Language() string { return "custom_csharp_aspnet_core" }

var (
	reAspNetRouteAttr = regexp.MustCompile(
		`\[Route\s*\(\s*["']([^"']+)["']`,
	)
	reAspNetHTTPMethod = regexp.MustCompile(
		`\[(Http(?:Get|Post|Put|Delete|Patch|Head|Options))(?:\s*\(\s*["']([^"']*)['"]\s*\))?`,
	)
	reAspNetMinimalAPI = regexp.MustCompile(
		`(?m)app\.Map(Get|Post|Put|Delete|Patch)\s*\(\s*["']([^"']+)["']`,
	)
	reAspNetDIRegister = regexp.MustCompile(
		`services\.Add(Scoped|Transient|Singleton|HostedService)\s*(?:<([^>]+)>)?`,
	)
	reAspNetMiddleware = regexp.MustCompile(
		`app\.Use(?:Middleware<([^>]+)>|When\s*\(|)\s*\(`,
	)
	reAspNetSignalRHub = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+:\s+Hub\b`,
	)
	reAspNetMapHub = regexp.MustCompile(
		`app\.MapHub\s*<\s*(\w+)\s*>`,
	)
	reAspNetGRPC = regexp.MustCompile(
		`app\.MapGrpcService\s*<\s*(\w+)\s*>`,
	)
	reAspNetHealthCheck = regexp.MustCompile(
		`app\.MapHealthChecks\s*\(\s*["']([^"']+)["']`,
	)
	reAspNetBackground = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+:\s+(?:IHostedService|BackgroundService)\b`,
	)
	reAspNetController = regexp.MustCompile(
		`(?m)class\s+(\w+(?:Controller))\s+:\s+(?:Controller|ControllerBase|ApiController)\b`,
	)
)

func (e *aspnetCoreExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.aspnet_core_extractor.extract",
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

	// 1. [HttpGet/Post...] attribute routes -> SCOPE.Operation/endpoint
	// Track controller-level route prefix
	controllerPrefix := ""
	if m := reAspNetRouteAttr.FindStringSubmatch(src); m != nil {
		controllerPrefix = m[1]
	}
	for _, m := range reAspNetHTTPMethod.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		verb = strings.TrimPrefix(verb, "Http")
		verb = strings.ToUpper(verb)
		actionPath := ""
		if m[4] >= 0 {
			actionPath = src[m[4]:m[5]]
		}
		fullPath := controllerPrefix
		if actionPath != "" {
			if fullPath != "" && !strings.HasSuffix(fullPath, "/") {
				fullPath += "/"
			}
			fullPath += actionPath
		}
		if fullPath == "" {
			fullPath = "/"
		}
		name := verb + " " + fullPath
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_ROUTE",
			"http_method", verb, "route_path", fullPath)
		add(ent)
	}

	// 2. Minimal API app.MapGet/Post/etc -> SCOPE.Operation/endpoint
	for _, m := range reAspNetMinimalAPI.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToUpper(src[m[2]:m[3]])
		path := src[m[4]:m[5]]
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_MINIMAL_API",
			"http_method", method, "route_path", path)
		add(ent)
	}

	// 3. Controller declarations -> SCOPE.Component
	for _, m := range reAspNetController.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_CONTROLLER")
		add(ent)
	}

	// 4. DI registrations -> SCOPE.Pattern
	for _, m := range reAspNetDIRegister.FindAllStringSubmatchIndex(src, -1) {
		lifetime := src[m[2]:m[3]]
		serviceType := ""
		if m[4] >= 0 {
			serviceType = src[m[4]:m[5]]
		}
		name := "di:" + lifetime
		if serviceType != "" {
			name += ":" + serviceType
		}
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_DI",
			"lifetime", lifetime, "service_type", serviceType)
		add(ent)
	}

	// 5. Middleware -> SCOPE.Pattern
	for _, m := range reAspNetMiddleware.FindAllStringSubmatchIndex(src, -1) {
		mwType := ""
		if m[2] >= 0 {
			mwType = src[m[2]:m[3]]
		}
		name := "middleware:" + mwType
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_MIDDLEWARE",
			"middleware_type", mwType)
		add(ent)
	}

	// 6. SignalR Hub -> SCOPE.Operation/endpoint
	for _, m := range reAspNetSignalRHub.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_SIGNALR",
			"hub_class", name)
		add(ent)
	}
	for _, m := range reAspNetMapHub.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("hub:"+name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_SIGNALR",
			"hub_class", name)
		add(ent)
	}

	// 7. gRPC services -> SCOPE.Service
	for _, m := range reAspNetGRPC.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_GRPC")
		add(ent)
	}

	// 8. Health check endpoints -> SCOPE.Operation/endpoint
	for _, m := range reAspNetHealthCheck.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ent := makeEntity("GET "+path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_HEALTH_CHECK",
			"route_path", path)
		add(ent)
	}

	// 9. Background services -> SCOPE.Service
	for _, m := range reAspNetBackground.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "aspnet_core", "provenance", "INFERRED_FROM_ASPNET_BACKGROUND_SERVICE")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
