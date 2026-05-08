package kotlin

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
	extractor.Register("custom_kotlin_ktor", &ktorExtractor{})
}

type ktorExtractor struct{}

func (e *ktorExtractor) Language() string { return "custom_kotlin_ktor" }

var (
	reKtorHTTPRoute = regexp.MustCompile(
		`(?m)\b(get|post|put|delete|patch|head|options)\s*\(\s*"([^"]+)"`,
	)
	reKtorRouteBlock = regexp.MustCompile(
		`(?m)\broute\s*\(\s*"([^"]+)"`,
	)
	reKtorInstall = regexp.MustCompile(
		`(?m)\binstall\s*\(\s*([\w.]+)`,
	)
	reKtorAuthenticate = regexp.MustCompile(
		`(?m)\bauthenticate\b\s*(?:\(\s*(?:"([^"]*)")?\s*\))?\s*\{`,
	)
	reKtorCallRespond = regexp.MustCompile(
		`(?m)\bcall\.(respond(?:Text|Html|HtmlTemplate|File|Redirect|OutputStream)?)\s*[({]`,
	)
	reKtorAppModule = regexp.MustCompile(
		`(?m)\bfun\s+Application\s*\.\s*(\w+)\s*\(`,
	)
)

func (e *ktorExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.ktor_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "ktor"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "kotlin" {
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

	// 1. Application module extension functions -> SCOPE.Service
	for _, m := range reKtorAppModule.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ktor", "provenance", "INFERRED_FROM_KTOR_MODULE",
			"module_name", name)
		add(ent)
	}

	// 2. install(Plugin) -> SCOPE.Pattern
	for _, m := range reKtorInstall.FindAllStringSubmatchIndex(src, -1) {
		plugin := src[m[2]:m[3]]
		ent := makeEntity(plugin, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ktor", "provenance", "INFERRED_FROM_KTOR_PLUGIN",
			"plugin_name", plugin)
		add(ent)
	}

	// 3. authenticate blocks -> SCOPE.Pattern
	for _, m := range reKtorAuthenticate.FindAllStringSubmatchIndex(src, -1) {
		scheme := "default"
		if m[2] >= 0 {
			scheme = src[m[2]:m[3]]
		}
		name := "authenticate:" + scheme
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ktor", "provenance", "INFERRED_FROM_KTOR_AUTH",
			"auth_scheme", scheme)
		add(ent)
	}

	// 4. route blocks -> SCOPE.Operation/endpoint
	for _, m := range reKtorRouteBlock.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ent := makeEntity(path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ktor", "provenance", "INFERRED_FROM_KTOR_ROUTE_BLOCK",
			"path", path, "route_type", "scope")
		add(ent)
	}

	// 5. HTTP method routes -> SCOPE.Operation/endpoint
	for _, m := range reKtorHTTPRoute.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToUpper(src[m[2]:m[3]])
		path := src[m[4]:m[5]]
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ktor", "provenance", "INFERRED_FROM_KTOR_HTTP_ROUTE",
			"http_method", method, "path", path)
		add(ent)
	}

	// 6. call.respond* -> SCOPE.Pattern
	seenRespondLines := make(map[string]bool)
	for _, m := range reKtorCallRespond.FindAllStringSubmatchIndex(src, -1) {
		callName := src[m[2]:m[3]]
		ln := lineOf(src, m[0])
		bucket := ln / 3
		dk := callName + ":" + strings.Repeat("x", bucket%100)
		if seenRespondLines[dk] {
			continue
		}
		seenRespondLines[dk] = true
		name := "call." + callName
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, ln)
		setProps(&ent, "framework", "ktor", "provenance", "INFERRED_FROM_KTOR_RESPOND",
			"call_name", callName)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
