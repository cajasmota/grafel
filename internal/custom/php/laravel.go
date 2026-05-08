package php

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_php_laravel", &laravelExtractor{})
}

type laravelExtractor struct{}

func (e *laravelExtractor) Language() string { return "custom_php_laravel" }

var (
	reLaravelHTTPRoute = regexp.MustCompile(
		`(?m)Route::(get|post|put|patch|delete|options|any)\s*\(\s*['"]([^'"]+)['"]`,
	)
	reLaravelResource = regexp.MustCompile(
		`(?m)Route::(?:api)?[Rr]esource\s*\(\s*['"]([^'"]+)['"]`,
	)
	reLaravelGroup = regexp.MustCompile(
		`(?m)Route::(?:group|prefix)\s*\(\s*\[?[^)]*['"]prefix['"]\s*=>\s*['"]([^'"]+)['"]`,
	)
	reLaravelBind = regexp.MustCompile(
		`(?m)\$this->app->(bind|singleton|instance)\s*\(\s*['"]([^'"]+)['"]`,
	)
	reLaravelJobClass = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+(?:extends\s+\w+\s+)?implements\s+ShouldQueue\b`,
	)
	reLaravelJobHandle = regexp.MustCompile(
		`(?m)public\s+function\s+(handle)\s*\(`,
	)
	reLaravelObserver = regexp.MustCompile(
		`(?m)public\s+function\s+(creating|created|updating|updated|deleting|deleted|saving|saved|restoring|restored)\s*\(`,
	)
	reLaravelPolicy = regexp.MustCompile(
		`(?m)public\s+function\s+(view|viewAny|create|update|delete|restore|forceDelete)\s*\(`,
	)
	reLaravelNotification = regexp.MustCompile(
		`(?m)public\s+function\s+(via|toMail|toDatabase|toBroadcast|toNexmo|toSlack)\s*\(`,
	)
	reLaravelArtisanHandle = regexp.MustCompile(
		`(?m)protected\s+\$signature\s*=\s*['"]([^'"]+)['"]`,
	)
	reLaravelFormRequest = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+FormRequest\b`,
	)
	reLaravelBladeComponent = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+(?:Component|View\\Component)\b`,
	)
)

// laravelCRUDRoutes for Route::resource
var laravelCRUDRoutes = []struct{ method, suffix string }{
	{"GET", ""},
	{"POST", ""},
	{"GET", "/create"},
	{"GET", "/{id}"},
	{"GET", "/{id}/edit"},
	{"PUT", "/{id}"},
	{"DELETE", "/{id}"},
}

func (e *laravelExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.laravel_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "laravel"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "php" {
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

	// 1. Explicit HTTP routes -> SCOPE.Operation/endpoint
	for _, m := range reLaravelHTTPRoute.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToUpper(src[m[2]:m[3]])
		path := src[m[4]:m[5]]
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel", "provenance", "INFERRED_FROM_LARAVEL_ROUTE",
			"http_method", method, "route_path", path)
		add(ent)
	}

	// 2. Route::resource -> expand CRUD
	for _, m := range reLaravelResource.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ln := lineOf(src, m[0])
		for _, cr := range laravelCRUDRoutes {
			path := "/" + name + cr.suffix
			routeName := cr.method + " " + path
			ent := makeEntity(routeName, "SCOPE.Operation", "endpoint", file.Path, file.Language, ln)
			setProps(&ent, "framework", "laravel", "provenance", "INFERRED_FROM_LARAVEL_RESOURCE",
				"http_method", cr.method, "route_path", path, "resource", name)
			add(ent)
		}
	}

	// 3. Route::group prefix -> SCOPE.Pattern
	for _, m := range reLaravelGroup.FindAllStringSubmatchIndex(src, -1) {
		prefix := src[m[2]:m[3]]
		name := "group:" + prefix
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel", "provenance", "INFERRED_FROM_LARAVEL_GROUP",
			"prefix", prefix)
		add(ent)
	}

	// 4. Service container bindings -> SCOPE.Pattern
	for _, m := range reLaravelBind.FindAllStringSubmatchIndex(src, -1) {
		bindType := src[m[2]:m[3]]
		abstract := src[m[4]:m[5]]
		name := fmt.Sprintf("bind:%s:%s", bindType, abstract)
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel", "provenance", "INFERRED_FROM_LARAVEL_BINDING",
			"binding_type", bindType, "abstract", abstract)
		add(ent)
	}

	// 5. Job classes -> SCOPE.Service
	for _, m := range reLaravelJobClass.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel", "provenance", "INFERRED_FROM_LARAVEL_JOB")
		add(ent)
	}

	// 6. Job handle() -> SCOPE.Operation/function
	for _, m := range reLaravelJobHandle.FindAllStringSubmatchIndex(src, -1) {
		ent := makeEntity("handle", "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel", "provenance", "INFERRED_FROM_LARAVEL_JOB")
		add(ent)
	}

	// 7. Observer hooks -> SCOPE.Pattern
	for _, m := range reLaravelObserver.FindAllStringSubmatchIndex(src, -1) {
		hook := src[m[2]:m[3]]
		ent := makeEntity(hook, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel", "provenance", "INFERRED_FROM_LARAVEL_OBSERVER",
			"hook_type", hook)
		add(ent)
	}

	// 8. Policy methods -> SCOPE.Pattern
	for _, m := range reLaravelPolicy.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[2]:m[3]]
		ent := makeEntity("policy:"+method, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel", "provenance", "INFERRED_FROM_LARAVEL_POLICY",
			"policy_method", method)
		add(ent)
	}

	// 9. Notification channels -> SCOPE.Operation/function
	for _, m := range reLaravelNotification.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[2]:m[3]]
		ent := makeEntity("notification:"+method, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel", "provenance", "INFERRED_FROM_LARAVEL_NOTIFICATION",
			"channel_method", method)
		add(ent)
	}

	// 10. Artisan command signature -> SCOPE.Operation/function
	for _, m := range reLaravelArtisanHandle.FindAllStringSubmatchIndex(src, -1) {
		sig := src[m[2]:m[3]]
		ent := makeEntity("artisan:"+sig, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel", "provenance", "INFERRED_FROM_LARAVEL_ARTISAN",
			"command_signature", sig)
		add(ent)
	}

	// 11. FormRequest subclasses -> SCOPE.Component
	for _, m := range reLaravelFormRequest.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel", "provenance", "INFERRED_FROM_LARAVEL_FORM_REQUEST")
		add(ent)
	}

	// 12. Blade component classes -> SCOPE.UIComponent
	for _, m := range reLaravelBladeComponent.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "laravel", "provenance", "INFERRED_FROM_LARAVEL_BLADE_COMPONENT")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
