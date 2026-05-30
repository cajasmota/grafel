package swift

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
	extractor.Register("custom_swift_vapor_extended", &vaporExtendedExtractor{})
}

type vaporExtendedExtractor struct{}

func (e *vaporExtendedExtractor) Language() string { return "custom_swift_vapor_extended" }

var (
	// Auth — BearerAuthenticator / BasicAuthenticator / JWTAuthenticator protocol conformance
	reVaporBearerAuth = regexp.MustCompile(
		`(?m)(?:struct|class|final\s+class)\s+(\w+)\s*:\s*(?:[A-Za-z,\s]*\b)?(?:BearerAuthenticator|BasicAuthenticator|CredentialsAuthenticator|JWTAuthenticator|Authenticator)\b`,
	)
	// req.auth.require / req.auth.get usage — signals authentication at handler level
	reVaporAuthRequire = regexp.MustCompile(
		`\breq\s*\.\s*auth\s*\.\s*(?:require|get|login|logout)\s*[(<]`,
	)
	// TokenAuthMiddleware / UserToken.authenticator() / User.authenticator()
	reVaporAuthMiddlewareChain = regexp.MustCompile(
		`(?m)\.\s*(?:grouped|group)\s*\(\s*[^)]*(?:authenticator|Authenticator|TokenAuthMiddleware|UserAuthMiddleware)\b`,
	)

	// Validation — Content + Validatable conformance
	reVaporValidatable = regexp.MustCompile(
		`(?m)(?:struct|class|final\s+class)\s+(\w+)\s*:\s*(?:[A-Za-z,\s]*\b)?(?:Validatable|Content)\b`,
	)
	// validations.add rule calls
	reVaporValidationsAdd = regexp.MustCompile(
		`\bvalidations\s*\.\s*add\s*\(`,
	)
	// req.content.decode(T.self) — already covered in vapor.go for routes, here as DTO signal
	reVaporContentDecode = regexp.MustCompile(
		`\breq\s*\.\s*content\s*\.\s*decode\s*\(\s*(\w+)\s*\.\s*self`,
	)

	// Middleware — inline Middleware protocol in route group
	reVaporMiddlewareGroup = regexp.MustCompile(
		`(?m)\.grouped\s*\(\s*([^)]*Middleware[^)]*)\)`,
	)

	// Testing — XCTestCase + Application.testable() patterns
	reVaporXCTestCase = regexp.MustCompile(
		`(?m)(?:class|final\s+class)\s+(\w+)\s*:\s*(?:[A-Za-z,\s]*\b)?XCTestCase\b`,
	)
	reVaporTestFunc = regexp.MustCompile(
		`(?m)^\s*func\s+(test\w+)\s*\(`,
	)
	reVaporTestable = regexp.MustCompile(
		`\bApplication\s*\.\s*testable\s*\(`,
	)
	reVaporXCTAssert = regexp.MustCompile(
		`\bXCTAssert(?:Equal|NotEqual|Nil|NotNil|True|False|Throws|NoThrow)?\s*\(`,
	)

	// Observability — logging (swift-log / OSLog / Vapor logger)
	reVaporLogCall = regexp.MustCompile(
		`\b(?:req\s*\.\s*logger|app\s*\.\s*logger|logger|Logger\s*\(\s*label\s*:)` +
			`\s*\.\s*(?:debug|info|notice|warning|error|critical|trace)\s*\(`,
	)
	reVaporOSLogCall = regexp.MustCompile(
		`\bos_log\s*\(|os\.Logger\s*\(\s*subsystem\s*:|Logger\s*\(\s*subsystem\s*:`,
	)

	// Observability — metrics (swift-metrics / Prometheus)
	reVaporMetric = regexp.MustCompile(
		`\bCounter\s*\(\s*label\s*:|\bGauge\s*\(\s*label\s*:|\bTimer\s*\(\s*label\s*:|\bHistogram\s*\(\s*label\s*:`,
	)

	// Observability — tracing (swift-distributed-tracing / Vapor instrumentation)
	reVaporTrace = regexp.MustCompile(
		`\bInstrumentationSystem\s*\.\s*tracer\b|\bTracer\s*\.\s*withSpan\s*\(|\bspan\s*\.\s*(?:setAttribute|addEvent|end)\s*\(`,
	)

	// Type System — typealias (extends the tree-sitter extractor which doesn't yet handle it)
	reSwiftTypealias = regexp.MustCompile(
		`(?m)^\s*(?:public\s+|internal\s+|private\s+|fileprivate\s+|open\s+)?typealias\s+([A-Za-z_][\w]*)\s*=\s*([^\n]+)`,
	)
)

func (e *vaporExtendedExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/swift")
	_, span := tracer.Start(ctx, "indexer.vapor_extended_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "vapor"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "swift" {
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

	// ---- Auth ----------------------------------------------------------------

	// BearerAuthenticator / BasicAuthenticator / JWTAuthenticator conformance
	for _, m := range reVaporBearerAuth.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "authenticator", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_AUTHENTICATOR",
			"category", "auth")
		add(ent)
	}

	// req.auth.require / req.auth.get usage — auth guard at call site
	for _, m := range reVaporAuthRequire.FindAllStringIndex(src, -1) {
		ent := makeEntity("auth:guard", "SCOPE.Pattern", "auth_guard", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_AUTH_REQUIRE",
			"category", "auth")
		add(ent)
	}

	// .grouped(SomeAuthMiddleware) chain — middleware-protected route group
	for _, m := range reVaporAuthMiddlewareChain.FindAllStringIndex(src, -1) {
		ent := makeEntity("auth:middleware_chain", "SCOPE.Pattern", "auth_middleware", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_AUTH_MIDDLEWARE_CHAIN",
			"category", "auth")
		add(ent)
	}

	// ---- Validation ----------------------------------------------------------

	// Validatable-conforming types
	for _, m := range reVaporValidatable.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "validatable", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_VALIDATABLE",
			"category", "validation")
		add(ent)
	}

	// validations.add(…) rule site
	for _, m := range reVaporValidationsAdd.FindAllStringIndex(src, -1) {
		ent := makeEntity("validation:rule", "SCOPE.Pattern", "validation_rule", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_VALIDATIONS_ADD",
			"category", "validation")
		add(ent)
	}

	// DTO decode — signals an input DTO
	for _, m := range reVaporContentDecode.FindAllStringSubmatchIndex(src, -1) {
		typeName := src[m[2]:m[3]]
		ent := makeEntity("dto:"+typeName, "SCOPE.Schema", "dto", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_CONTENT_DECODE",
			"category", "validation", "dto_type", typeName)
		add(ent)
	}

	// ---- Middleware ----------------------------------------------------------

	// .grouped(XMiddleware, YMiddleware) — middleware named inside group chain
	for _, m := range reVaporMiddlewareGroup.FindAllStringSubmatchIndex(src, -1) {
		inner := src[m[2]:m[3]]
		for _, part := range strings.Split(inner, ",") {
			name := strings.TrimSpace(part)
			if name == "" {
				continue
			}
			ent := makeEntity("middleware:"+name, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_GROUPED_MIDDLEWARE",
				"middleware_name", name)
			add(ent)
		}
	}

	// ---- Testing -------------------------------------------------------------

	// XCTestCase subclass
	for _, m := range reVaporXCTestCase.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "test_suite", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_XCTESTCASE",
			"category", "testing")
		add(ent)
	}

	// test function declarations
	for _, m := range reVaporTestFunc.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "test_function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_TEST_FUNC",
			"category", "testing")
		add(ent)
	}

	// Application.testable() — Vapor integration test bootstrapper
	for _, m := range reVaporTestable.FindAllStringIndex(src, -1) {
		ent := makeEntity("vapor:testable_app", "SCOPE.Component", "test_app", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_TESTABLE",
			"category", "testing")
		add(ent)
	}

	// XCTAssert calls — signals a test assertion site
	for _, m := range reVaporXCTAssert.FindAllStringIndex(src, -1) {
		ent := makeEntity("xctest:assert", "SCOPE.Pattern", "test_assertion", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_XCTASSERT",
			"category", "testing")
		add(ent)
	}

	// ---- Observability — Logging ---------------------------------------------

	for _, m := range reVaporLogCall.FindAllStringIndex(src, -1) {
		ent := makeEntity("log:call", "SCOPE.Pattern", "log_call", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_LOGGER_CALL",
			"category", "observability_log")
		add(ent)
	}

	for _, m := range reVaporOSLogCall.FindAllStringIndex(src, -1) {
		ent := makeEntity("oslog:call", "SCOPE.Pattern", "log_call", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_OSLOG_CALL",
			"category", "observability_log")
		add(ent)
	}

	// ---- Observability — Metrics ---------------------------------------------

	for _, m := range reVaporMetric.FindAllStringIndex(src, -1) {
		ent := makeEntity("metric:counter", "SCOPE.Pattern", "metric", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_METRIC",
			"category", "observability_metric")
		add(ent)
	}

	// ---- Observability — Tracing ---------------------------------------------

	for _, m := range reVaporTrace.FindAllStringIndex(src, -1) {
		ent := makeEntity("trace:span", "SCOPE.Pattern", "trace_span", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_VAPOR_TRACE",
			"category", "observability_trace")
		add(ent)
	}

	// ---- Type System — typealias ---------------------------------------------

	for _, m := range reSwiftTypealias.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		rhs := strings.TrimSpace(src[m[4]:m[5]])
		ent := makeEntity(name, "SCOPE.Component", "typealias", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vapor", "provenance", "INFERRED_FROM_SWIFT_TYPEALIAS",
			"alias_target", rhs)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
