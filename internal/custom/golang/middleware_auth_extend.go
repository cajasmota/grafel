package golang

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

// middleware_auth_extend.go — a framework-agnostic middleware + auth scanner
// (issue #3213, cluster 2) that extends the shared `.Use(...)` detector
// (helpers.go) beyond the four well-templated frameworks (gin/echo/fiber/chi,
// which call the helpers from their own extractors) to the remaining Go HTTP
// frameworks that register middleware/filters/interceptors:
//
//   - buffalo     : app.Use(mw) / app.Middleware.Use(mw)        (.Use chain)
//   - iris        : app.Use(mw) / .UseRouter / .UseGlobal       (.Use chain)
//   - hertz       : h.Use(mw)                                    (.Use chain)
//   - gorilla-mux : r.Use(mw)                                    (.Use chain)
//   - beego       : web/beego.InsertFilter("/p", pos, FilterFn)  (filter API)
//   - revel       : revel.InterceptFunc/Method(fn, revel.BEFORE) (interceptors)
//
// Each emitted entity is a SCOPE.Pattern (pattern_kind=middleware), with a
// dedicated auth SCOPE.Pattern (pattern_kind=auth) for any expression that
// classifies as auth middleware via the shared classifyAuthMiddleware catalog
// (jwt/oauth/basic/session/rbac/api_key/…). All emission funnels through the
// shared helpers (parseMiddlewareChain/emitMiddlewareChain for .Use chains;
// classifyAuthMiddleware for filter/interceptor expressions) so the property
// shape is identical across every framework.
//
// Honesty (registry coverage status):
//
//	partial — for every framework above. Detection is a heuristic regex /
//	substring match on source text. It does NOT perform import-resolution or
//	data-flow analysis to confirm a value actually enforces authentication, and
//	it does not bind the middleware to a specific route. The auth classifier is
//	a substring match on the expression.
//
//	not_applicable — for raw net/http and fasthttp. Neither the Go standard
//	library router (net/http) nor fasthttp / fasthttp/router exposes a
//	first-class middleware-registration primitive: middleware in both is plain
//	manual handler wrapping (`func(next http.Handler) http.Handler` /
//	`func(h fasthttp.RequestHandler) fasthttp.RequestHandler`) with no
//	`.Use(...)`-style chain to extract. There is therefore no
//	registration-surface to catalog, and we honestly mark those cells NA rather
//	than fabricate one. (Auth in those frameworks is likewise inline checks in
//	handlers, not a registered middleware.)
//
// Framework attribution: the scanner runs on every Go file (registry key
// custom_go_middleware_auth matches the custom_go_ dispatch prefix). It infers
// the framework from framework-specific engine/context markers and stamps it
// on each emitted entity. A file with no recognised marker emits nothing.
// gin/echo/fiber/chi are intentionally NOT handled here — their own extractors
// already call the shared helpers — so we never double-emit for them.

func init() {
	extractor.Register("custom_go_middleware_auth", &middlewareAuthExtractor{})
}

type middlewareAuthExtractor struct{}

func (e *middlewareAuthExtractor) Language() string { return "custom_go_middleware_auth" }

// mwExtendFramework is one framework this agnostic pass attributes + scans.
// mode selects the registration-surface parser:
//
//	"use"        — balanced .Use(...) chain (shared helpers)
//	"insertfilter" — beego web/beego.InsertFilter("/p", pos, fn)
//	"intercept"  — revel.InterceptFunc/Method(fn, …)
type mwExtendFramework struct {
	name   string
	marker *regexp.Regexp
	mode   string
}

// mwExtendFrameworks lists the frameworks handled by this pass, in priority
// order. The marker is a framework-specific engine/context construct that is
// unambiguous and present in any router/handler file. First match wins, so the
// list is ordered to avoid cross-framework confusion (hertz before buffalo
// etc. is irrelevant because the markers are disjoint).
//
// gin/echo/fiber/chi are deliberately absent (their own extractors emit
// middleware/auth). net/http and fasthttp are absent because they have no
// middleware-registration primitive (see file-level honesty note → NA cells).
var mwExtendFrameworks = []mwExtendFramework{
	{"buffalo", regexp.MustCompile(`\bbuffalo\.(?:New|App|Options|Context)\b`), "use"},
	{"iris", regexp.MustCompile(`\biris\.(?:New|Default|Application|Context|Party)\b`), "use"},
	{"hertz", regexp.MustCompile(`\bserver\.(?:Default|New|Hertz)\b|\bapp\.RequestContext\b`), "use"},
	{"gorilla-mux", regexp.MustCompile(`\bmux\.(?:NewRouter|Router|Vars)\b`), "use"},
	{"beego", regexp.MustCompile(`\b(?:beego|web)\.(?:Router|NewNamespace|Run|InsertFilter|AutoRouter)\b`), "insertfilter"},
	{"revel", regexp.MustCompile(`\brevel\.(?:Result|Controller|Intercept(?:Func|Method)|BEFORE|AFTER)\b`), "intercept"},
}

// detectMWExtendFramework returns the framework + scan mode the file belongs
// to, or ("","") when no recognised marker is present.
func detectMWExtendFramework(src string) (string, string) {
	for _, f := range mwExtendFrameworks {
		if f.marker.MatchString(src) {
			return f.name, f.mode
		}
	}
	return "", ""
}

// reBeegoInsertFilter matches Beego's filter-registration API:
//
//	web.InsertFilter("/api/*", web.BeforeRouter, AuthFilter)
//	beego.InsertFilter("/*", beego.BeforeExec, JWTFilter, web.WithReturnOnOutput(true))
//
// Capture groups: 1=path pattern, 2=position const (e.g. BeforeRouter),
// 3=filter function expression.
var reBeegoInsertFilter = regexp.MustCompile(
	`(?:beego|web)\.InsertFilter\s*\(\s*"([^"]*)"\s*,\s*([\w.]+)\s*,\s*([\w.]+)`,
)

// reRevelExtendIntercept matches Revel interceptor registration:
//
//	revel.InterceptFunc(checkUser, revel.BEFORE, &App{})
//	revel.InterceptMethod(App.checkUser, revel.BEFORE)
//
// Capture group 1 is the interceptor function/method expression.
var reRevelExtendIntercept = regexp.MustCompile(
	`revel\.Intercept(?:Func|Method)\s*\(\s*([\w.]+)`,
)

func (e *middlewareAuthExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.middleware_auth_extend_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "go" {
		return nil, nil
	}

	src := string(file.Content)
	framework, mode := detectMWExtendFramework(src)
	if framework == "" {
		span.SetAttributes(attribute.String("framework", ""))
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	mwProv := "INFERRED_FROM_" + provToken(framework) + "_MIDDLEWARE"
	authProv := "INFERRED_FROM_" + provToken(framework) + "_AUTH"

	switch mode {
	case "use":
		// Balanced .Use(...) chains via the shared helpers — one ordered
		// SCOPE.Pattern per middleware + a dedicated auth pattern for any
		// entry that classifies as auth.
		for _, uc := range findUseCalls(src) {
			chain := parseMiddlewareChain(uc.Args)
			emitMiddlewareChain(add, chain, framework, mwProv, authProv,
				file.Path, file.Language, uc.Line)
		}

	case "insertfilter":
		// Beego filter API: the 3rd argument is the filter function; the
		// filter position (e.g. BeforeRouter) is recorded for ordering context.
		for _, m := range reBeegoInsertFilter.FindAllStringSubmatchIndex(src, -1) {
			pathPat := submatch(src, m, 2)
			position := submatch(src, m, 4)
			filterFn := submatch(src, m, 6)
			if filterFn == "" {
				continue
			}
			emitFilterMiddleware(add, framework, mwProv, authProv,
				filterFn, file.Path, file.Language, lineOf(src, m[0]),
				"filter_position", position, "filter_path", pathPat)
		}

	case "intercept":
		// Revel interceptors (before/after filters ~ middleware).
		for _, m := range reRevelExtendIntercept.FindAllStringSubmatchIndex(src, -1) {
			fn := submatch(src, m, 2)
			if fn == "" {
				continue
			}
			emitFilterMiddleware(add, framework, mwProv, authProv,
				fn, file.Path, file.Language, lineOf(src, m[0]),
				"middleware_form", "interceptor")
		}
	}

	span.SetAttributes(
		attribute.String("framework", framework),
		attribute.Int("entity_count", len(entities)),
	)
	return entities, nil
}

// emitFilterMiddleware emits a single non-.Use middleware registration (a beego
// filter or a revel interceptor) as a SCOPE.Pattern (pattern_kind=middleware),
// plus a dedicated auth SCOPE.Pattern when the expression classifies as auth.
// It mirrors emitMiddlewareChain's property shape (minus mw_order, which is
// undefined for these single-registration APIs). extraProps are appended as
// key/value pairs onto the middleware entity.
func emitFilterMiddleware(
	add func(types.EntityRecord),
	framework, mwProvenance, authProvenance, expr, filePath, language string,
	line int,
	extraProps ...string,
) {
	head := reMiddlewareCallHead.FindString(expr)
	if head == "" {
		head = expr
	}
	authKind := classifyAuthMiddleware(expr)

	mw := makeEntity(expr, "SCOPE.Pattern", "", filePath, language, line)
	setProps(&mw, "framework", framework, "provenance", mwProvenance,
		"pattern_kind", "middleware", "middleware_name", head)
	if len(extraProps) > 0 {
		setProps(&mw, extraProps...)
	}
	if authKind != "" {
		setProps(&mw, "is_auth", "true", "auth_kind", authKind)
	}
	add(mw)

	if authKind != "" {
		au := makeEntity("auth:"+head, "SCOPE.Pattern", "", filePath, language, line)
		setProps(&au, "framework", framework, "provenance", authProvenance,
			"pattern_kind", "auth", "auth_kind", authKind,
			"middleware_name", head, "middleware_expr", expr)
		add(au)
	}
}

// provToken normalises a framework name into the UPPER_SNAKE token used in the
// INFERRED_FROM_* provenance tags (e.g. "gorilla-mux" -> "GORILLA_MUX").
func provToken(framework string) string {
	r := strings.NewReplacer("-", "_", "/", "_", ".", "_")
	return strings.ToUpper(r.Replace(framework))
}
