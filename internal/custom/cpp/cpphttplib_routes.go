package cpp

// cpphttplib_routes.go — cpp-httplib (yhirose/cpp-httplib) HTTP server
// route/handler extractor.
//
// cpp-httplib is a header-only C++11 HTTP/HTTPS library (~13k★). Servers are
// declared by registering verb-named member functions on an httplib::Server (or
// httplib::SSLServer) instance:
//
//	httplib::Server svr;
//	svr.Get("/hi", handler);
//	svr.Post("/users", handler);
//	svr.Put("/users/(\\d+)", handler);     // regex path
//	svr.Delete("/users/:id", handler);     // (rare, but accepted)
//	svr.Patch("/x", handler);
//	svr.Options("/x", handler);
//
// Each matched route emits one SCOPE.Operation/endpoint entity with provenance
// INFERRED_FROM_CPPHTTPLIB_ROUTE. The handler token (lambda → "<lambda>", named
// function/method → its identifier) is stamped in handler_name to support
// handler_attribution.
//
// Outbound httplib::Client calls (svr is a Client) are NOT route declarations
// and are intentionally not matched here — see follow-up for client http_effect.
//
// Status: partial (regex/heuristic; no AST).

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
	extractor.Register("custom_cpp_cpphttplib", &cppHttplibExtractor{})
}

type cppHttplibExtractor struct{}

func (e *cppHttplibExtractor) Language() string { return "custom_cpp_cpphttplib" }

// <recv>.Get("/path", <handler...>) for the six routable verbs.
//
//	group 1: receiver identifier (server instance, e.g. svr / server)
//	group 2: HTTP verb (Get/Post/Put/Delete/Patch/Options)
//	group 3: route path string literal
//	group 4: handler token start (first token after the comma)
var reCppHttplibRoute = regexp.MustCompile(
	`(?m)\b([A-Za-z_]\w*)\s*\.\s*(Get|Post|Put|Delete|Patch|Options)\s*\(\s*` +
		`(?:R?"[^"(]*\(([^)]*)\)[^"]*"|"((?:[^"\\]|\\.)*)")` +
		`\s*,\s*([A-Za-z_&\[]?[A-Za-z0-9_:&]*)`,
)

// cppHttplibVerb maps a member-function name to its canonical HTTP method.
func cppHttplibVerb(fn string) string {
	return strings.ToUpper(fn)
}

func (e *cppHttplibExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.cpphttplib_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "cpp-httplib"),
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
	// Cheap signal gate: require a cpp-httplib marker so we do not misattribute
	// `obj.Get("...")` calls in unrelated C++ code.
	if !strings.Contains(src, "httplib::") && !strings.Contains(src, "httplib.h") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	for _, m := range reCppHttplibRoute.FindAllStringSubmatchIndex(src, -1) {
		fn := src[m[4]:m[5]]
		verb := cppHttplibVerb(fn)
		// Path comes from either the raw-string group (m[6:7]) or the plain
		// string-literal group (m[8:9]).
		var path string
		if m[6] >= 0 {
			path = src[m[6]:m[7]]
		} else if m[8] >= 0 {
			path = src[m[8]:m[9]]
		}
		path = cppNormalizeRoutePath(path)

		handler := "<lambda>"
		if m[10] >= 0 {
			raw := strings.TrimSpace(src[m[10]:m[11]])
			raw = strings.TrimLeft(raw, "&")
			// A lambda capture `[` or empty token means an inline lambda.
			if raw != "" && !strings.HasPrefix(raw, "[") {
				handler = raw
			}
		}

		name := verb + " " + path
		key := "SCOPE.Operation:" + name
		if seen[key] {
			continue
		}
		seen[key] = true

		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "cpp-httplib",
			"provenance", "INFERRED_FROM_CPPHTTPLIB_ROUTE",
			"http_method", verb,
			"route_path", path,
			"handler_name", handler,
			"dsl", "Server."+fn,
		)
		entities = append(entities, ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
