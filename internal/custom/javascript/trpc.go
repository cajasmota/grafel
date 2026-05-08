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
	extreg.Register("custom_js_trpc", &trpcExtractor{})
}

type trpcExtractor struct{}

func (e *trpcExtractor) Language() string { return "custom_js_trpc" }

var (
	// router({...}) / createRouter({...}) / t.router({...}) / appRouter
	reTRPCRouter = regexp.MustCompile(
		`(?:const|let|var)\s+(\w+)\s*=\s*(?:t\.|router\.|createRouter\.)?\s*router\s*\(\s*\{|` +
			`(?:const|let|var)\s+(\w+)\s*=\s*(?:t\.)?\s*router\s*\(\s*\{`,
	)
	// procedure.query(...) / procedure.mutation(...) / procedure.subscription(...)
	// Standalone: const getPosts = t.procedure.query(...)
	// Handles: t.procedure.query, t.procedure.input(...).query, publicProcedure.query, etc.
	reTRPCProcedureStandalone = regexp.MustCompile(
		`(?:const|let|var)\s+(\w+)\s*=\s*(?:t\.procedure\.|publicProcedure\.|protectedProcedure\.|procedure\.)` +
			`(?:\w+\([^)]*\)\s*\.)*` +
			`(query|mutation|subscription)\s*\(`,
	)
	// Inline: methodName: t.procedure.query(...)
	reTRPCProcedureInline = regexp.MustCompile(
		`(\w+)\s*:\s*(?:t\.procedure\.|publicProcedure\.|protectedProcedure\.|procedure\.)` +
			`(?:\w+\([^)]*\)\s*\.)*` +
			`(query|mutation|subscription)\s*\(`,
	)
	// createTRPCRouter / initTRPC
	reTRPCInit = regexp.MustCompile(
		`initTRPC\s*(?:\.[A-Za-z<>]*?)?\s*\.create\s*\(|createTRPCRouter\s*\(`,
	)
	// Context: createContext / createInnerTRPCContext
	reTRPCContext = regexp.MustCompile(
		`(?:export\s+)?(?:async\s+)?function\s+(create(?:Inner)?(?:TRPC)?Context)\s*\(`,
	)
	// Middleware: t.middleware(...)
	reTRPCMiddleware = regexp.MustCompile(
		`(?:t\.|router\.)middleware\s*\(`,
	)
)

func (e *trpcExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.trpc_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "trpc"),
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

	// initTRPC / createTRPCRouter
	for _, m := range reTRPCInit.FindAllStringIndex(src, -1) {
		ent := makeEntity("trpc_init", "SCOPE.Service", "trpc_instance", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "trpc", "provenance", "INFERRED_FROM_TRPC_INIT")
		addEntity(ent)
	}

	// Router declarations
	for _, m := range reTRPCRouter.FindAllStringSubmatchIndex(src, -1) {
		var name string
		if m[2] >= 0 {
			name = src[m[2]:m[3]]
		} else if m[4] >= 0 {
			name = src[m[4]:m[5]]
		}
		if name == "" {
			continue
		}
		ent := makeEntity(name, "SCOPE.Component", "trpc_router", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "trpc", "provenance", "INFERRED_FROM_TRPC_ROUTER")
		addEntity(ent)
	}

	// Standalone procedures
	for _, m := range reTRPCProcedureStandalone.FindAllStringSubmatchIndex(src, -1) {
		procName := src[m[2]:m[3]]
		procType := src[m[4]:m[5]]
		ent := makeEntity(procName, "SCOPE.Operation", "trpc_procedure", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "trpc", "procedure_type", procType,
			"provenance", "INFERRED_FROM_TRPC_PROCEDURE")
		addEntity(ent)
	}

	// Inline procedures
	for _, m := range reTRPCProcedureInline.FindAllStringSubmatchIndex(src, -1) {
		procName := src[m[2]:m[3]]
		procType := src[m[4]:m[5]]
		ent := makeEntity(procName, "SCOPE.Operation", "trpc_procedure", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "trpc", "procedure_type", procType,
			"provenance", "INFERRED_FROM_TRPC_PROCEDURE")
		addEntity(ent)
	}

	// Context functions
	for _, m := range reTRPCContext.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "context", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "trpc", "provenance", "INFERRED_FROM_TRPC_CONTEXT")
		addEntity(ent)
	}

	// Middleware
	for _, m := range reTRPCMiddleware.FindAllStringIndex(src, -1) {
		ent := makeEntity("trpc_middleware", "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "trpc", "provenance", "INFERRED_FROM_TRPC_MIDDLEWARE")
		addEntity(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
