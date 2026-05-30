package cpp

// restinio_middleware.go — RESTinio C++ middleware / handler-chaining extractor.
//
// RESTinio ≥0.6 middleware surfaces:
//
//  1. non_matched_request_handler:
//       server_settings.non_matched_request_handler(handler_fn)
//       — a catch-all handler that runs when no route matches (used for
//         global error handling, 404 responses, logging, etc.).
//
//  2. request_handler chaining via make_chain<H1,H2,...>() or
//       make_sync_chain / make_easy_parser_dispatcher:
//       restinio::router::make_chain<H1,H2,H3>()
//
//  3. server_settings.request_handler(handler) — custom root handler
//     that replaces the router and can implement middleware logic.
//
//  4. Logging handler: set_logger / restinio::null_logger_t / spdlog logger
//     configured on server settings.
//
// Status: partial — heuristic regex; no AST; covers common middleware
// patterns but does not resolve handler types across files.

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
	extractor.Register("custom_cpp_restinio_mw", &restinioMwExtractor{})
}

type restinioMwExtractor struct{}

func (e *restinioMwExtractor) Language() string { return "custom_cpp_restinio_mw" }

var (
	// Gate: must look like a restinio file.
	reRestinioGate = regexp.MustCompile(`(?:#\s*include\s+[<"]restinio/|restinio::|http_server_run\s*\()`)

	// non_matched_request_handler(handler)
	reRestinioNonMatched = regexp.MustCompile(
		`\b(?:\w+)\s*(?:->|\.)\s*non_matched_request_handler\s*\(([^)]+)\)`,
	)

	// make_chain<H1, H2, ...>() / make_sync_chain<...>()
	reRestinioMakeChain = regexp.MustCompile(
		`restinio\s*::\s*(?:router\s*::)?make_(?:sync_)?chain\s*<([^>]+)>`,
	)

	// server_settings.request_handler(handler) — custom root handler
	reRestinioRequestHandler = regexp.MustCompile(
		`\b(?:\w+)\s*(?:->|\.)\s*request_handler\s*\(\s*(\w[^)]*)\)`,
	)

	// Logger setup on server settings
	reRestinioLogger = regexp.MustCompile(
		`\b(?:\w+)\s*(?:->|\.)\s*(?:logger|set_logger)\s*\(`,
	)
)

func (e *restinioMwExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.restinio_mw_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "restinio"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "cpp" {
		return nil, nil
	}

	src := string(file.Content)
	if !reRestinioGate.MatchString(src) {
		return nil, nil
	}

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

	// non_matched_request_handler
	for _, m := range reRestinioNonMatched.FindAllStringSubmatchIndex(src, -1) {
		handler := strings.TrimSpace(src[m[2]:m[3]])
		if len(handler) > 60 {
			handler = handler[:60]
		}
		name := "restinio:non_matched_request_handler:" + handler
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "restinio", "provenance", "INFERRED_FROM_RESTINIO_NON_MATCHED",
			"middleware_kind", "non_matched_request_handler", "handler_expr", handler)
		add(ent)
	}

	// make_chain<H1, H2, ...>
	for _, m := range reRestinioMakeChain.FindAllStringSubmatchIndex(src, -1) {
		chainTypes := strings.TrimSpace(src[m[2]:m[3]])
		if len(chainTypes) > 100 {
			chainTypes = chainTypes[:100]
		}
		name := "restinio:make_chain:" + chainTypes
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "restinio", "provenance", "INFERRED_FROM_RESTINIO_MAKE_CHAIN",
			"middleware_kind", "handler_chain", "chain_types", chainTypes)
		add(ent)
	}

	// Custom root request_handler (can implement middleware)
	for _, m := range reRestinioRequestHandler.FindAllStringSubmatchIndex(src, -1) {
		handler := strings.TrimSpace(src[m[2]:m[3]])
		if len(handler) > 60 {
			handler = handler[:60]
		}
		name := "restinio:request_handler:" + handler
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "restinio", "provenance", "INFERRED_FROM_RESTINIO_REQUEST_HANDLER",
			"middleware_kind", "request_handler", "handler_expr", handler)
		add(ent)
	}

	// Logger middleware
	if reRestinioLogger.MatchString(src) {
		ent := makeEntity("restinio:logger_middleware", "SCOPE.Pattern", "", file.Path, file.Language, 1)
		setProps(&ent, "framework", "restinio", "provenance", "INFERRED_FROM_RESTINIO_LOGGER",
			"middleware_kind", "logger")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
