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
	extreg.Register("custom_js_fastify", &fastifyExtractor{})
}

type fastifyExtractor struct{}

func (e *fastifyExtractor) Language() string { return "custom_js_fastify" }

var (
	reFastifyRoute = regexp.MustCompile(
		`(?i)(?:fastify|app|\w+)\s*\.\s*(get|post|put|delete|patch|options|head|all)\s*\(\s*['` + "`" + `"]([^'"` + "`" + ` ]+)['` + "`" + `"]`,
	)
	reFastifyRegister = regexp.MustCompile(
		`(?:fastify|app|\w+)\s*\.\s*register\s*\(\s*(\w+)`,
	)
	reFastifyPlugin = regexp.MustCompile(
		`fastify\.register\s*\(\s*require\s*\(\s*['` + "`" + `"]([^'"` + "`" + `]+)['` + "`" + `"]\s*\)`,
	)
	reFastifyDecorate = regexp.MustCompile(
		`(?:fastify|app|\w+)\s*\.\s*decorate(?:Request|Reply)?\s*\(\s*['` + "`" + `"](\w+)['` + "`" + `"]`,
	)
	reFastifyAddHook = regexp.MustCompile(
		`(?:fastify|app|\w+)\s*\.\s*addHook\s*\(\s*['` + "`" + `"](onRequest|preHandler|preValidation|onSend|onResponse|onError|onClose|onReady|onRoute|onRegister|preParsing|preSerializer|onTimeout)['"` + "`" + `]`,
	)
	reFastifySchema = regexp.MustCompile(
		`schema\s*:\s*\{`,
	)
	reFastifyInstance = regexp.MustCompile(
		`(?:const|let|var)\s+(\w+)\s*=\s*(?:fastify|Fastify)\s*\(`,
	)
)

func (e *fastifyExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.fastify_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "fastify"),
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
		key := fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Name, ent.SourceFile)
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// Fastify instance
	for _, m := range reFastifyInstance.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Service", "fastify_instance", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "fastify", "provenance", "INFERRED_FROM_FASTIFY_INSTANCE")
		addEntity(ent)
	}

	// Route handlers
	for _, m := range reFastifyRoute.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToUpper(src[m[2]:m[3]])
		path := src[m[4]:m[5]]
		// Check if this route has schema validation nearby (within 200 chars forward)
		hasSchema := false
		end := m[1]
		if end+500 < len(src) {
			segment := src[m[0] : end+500]
			hasSchema = reFastifySchema.MatchString(segment)
		}
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		schemaStr := "false"
		if hasSchema {
			schemaStr = "true"
		}
		setProps(&ent, "framework", "fastify", "http_method", method, "route_path", path,
			"has_schema_validation", schemaStr, "provenance", "INFERRED_FROM_FASTIFY_ROUTE")
		addEntity(ent)
	}

	// Plugin registration
	for _, m := range reFastifyPlugin.FindAllStringSubmatchIndex(src, -1) {
		pluginName := src[m[2]:m[3]]
		name := "plugin:" + pluginName
		ent := makeEntity(name, "SCOPE.Component", "plugin", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "fastify", "plugin_name", pluginName,
			"provenance", "INFERRED_FROM_FASTIFY_PLUGIN")
		addEntity(ent)
	}

	// Generic register calls (non-require)
	for _, m := range reFastifyRegister.FindAllStringSubmatchIndex(src, -1) {
		pluginVar := src[m[2]:m[3]]
		if strings.HasPrefix(pluginVar, "require") {
			continue
		}
		name := "register:" + pluginVar
		ent := makeEntity(name, "SCOPE.Component", "plugin", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "fastify", "plugin_var", pluginVar,
			"provenance", "INFERRED_FROM_FASTIFY_REGISTER")
		addEntity(ent)
	}

	// Decorators
	for _, m := range reFastifyDecorate.FindAllStringSubmatchIndex(src, -1) {
		decorName := src[m[2]:m[3]]
		name := "decorate:" + decorName
		ent := makeEntity(name, "SCOPE.Pattern", "decorator", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "fastify", "decorator_name", decorName,
			"provenance", "INFERRED_FROM_FASTIFY_DECORATOR")
		addEntity(ent)
	}

	// Lifecycle hooks
	for _, m := range reFastifyAddHook.FindAllStringSubmatchIndex(src, -1) {
		hookName := src[m[2]:m[3]]
		name := "hook:" + hookName
		ent := makeEntity(name, "SCOPE.Pattern", "lifecycle_hook", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "fastify", "hook_name", hookName,
			"provenance", "INFERRED_FROM_FASTIFY_HOOK")
		addEntity(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
