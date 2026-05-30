// Package scala — framework-specific extractors for Scala HTTP frameworks.
//
// Covers (missing → partial):
//
//	Record                        Capability                      Status
//	──────────────────────────────────────────────────────────────────────
//	framework.akka-http           Routing/route_extraction        partial
//	framework.cask                Routing/route_extraction        partial
//	framework.cask                Routing/endpoint_synthesis      partial
//	framework.cask                Routing/handler_attribution     partial
//	framework.finatra             Routing/route_extraction        partial
//	framework.http4s              Routing/route_extraction        partial
//	framework.lagom               Routing/route_extraction        partial
//	framework.scalatra            Routing/route_extraction        partial
//	framework.zio-http            Routing/route_extraction        partial
//	framework.play                Routing/route_extraction        partial
//	framework.play                Routing/router_pattern          partial
//
//	framework.akka-http           Auth/auth_coverage              partial
//	framework.cask                Auth/auth_coverage              partial
//	framework.finatra             Auth/auth_coverage              partial
//	framework.http4s              Auth/auth_coverage              partial
//	framework.lagom               Auth/auth_coverage              partial
//	framework.scalatra            Auth/auth_coverage              partial
//	framework.zio-http            Auth/auth_coverage              partial
//
//	framework.akka-http           Middleware/middleware_coverage   partial
//	framework.cask                Middleware/middleware_coverage   partial
//	framework.finatra             Middleware/middleware_coverage   partial
//	framework.http4s              Middleware/middleware_coverage   partial
//	framework.lagom               Middleware/middleware_coverage   partial
//	framework.scalatra            Middleware/middleware_coverage   partial
//	framework.zio-http            Middleware/middleware_coverage   partial
//
//	framework.akka-http           Validation/dto_extraction       partial
//	framework.akka-http           Validation/request_validation   partial
//	framework.cask                Validation/dto_extraction       partial
//	framework.cask                Validation/request_validation   partial
//	framework.finatra             Validation/dto_extraction       partial
//	framework.finatra             Validation/request_validation   partial
//	framework.http4s              Validation/dto_extraction       partial
//	framework.http4s              Validation/request_validation   partial
//	framework.lagom               Validation/dto_extraction       partial
//	framework.lagom               Validation/request_validation   partial
//	framework.scalatra            Validation/dto_extraction       partial
//	framework.scalatra            Validation/request_validation   partial
//	framework.zio-http            Validation/dto_extraction       partial
//	framework.zio-http            Validation/request_validation   partial
//
//	all 9 framework records       Observability/log_extraction    partial
//	all 9 framework records       Observability/metric_extraction partial
//	all 9 framework records       Observability/trace_extraction  partial
//
//	all 9 framework records       Testing/tests_linkage           partial
//
// Honest limit: all regex-based, file-local. Cross-file route wiring is not
// resolved. Cells are partial.
package scala

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
	extractor.Register("custom_scala_frameworks", &scalaFrameworksExtractor{})
}

type scalaFrameworksExtractor struct{}

func (e *scalaFrameworksExtractor) Language() string { return "custom_scala_frameworks" }

// ---------------------------------------------------------------------------
// Routing regexes
// ---------------------------------------------------------------------------

var (
	// Akka-HTTP / Pekko: path("segment") { get { ... } }
	reAkkaRoute = regexp.MustCompile(
		`(?m)\b(get|post|put|delete|patch|head|options)\s*\{`)

	reAkkaPathDirective = regexp.MustCompile(
		`\b(?:pathPrefix|path|pathEnd)\s*\(\s*"([^"]+)"`)

	// http4s: case GET -> Root / "seg" => ...
	reHttp4sRoute = regexp.MustCompile(
		`\bcase\s+(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*->\s*Root\s*(?:/\s*"[^"]*"\s*)*`)

	// Scalatra: get("/path") { ... }
	reScalatraRoute = regexp.MustCompile(
		`\b(get|post|put|delete|patch|head|options)\s*\(\s*"([^"]*)"`)

	// Cask: @cask.get("/path") or @cask.post("/path")
	reCaskRoute = regexp.MustCompile(
		`@cask\.(get|post|put|delete|patch)\s*\(\s*"([^"]*)"`)

	// ZIO-HTTP: Http.collect { case Method.GET -> Root / "seg" => ... }
	reZioHTTPRoute = regexp.MustCompile(
		`(?:Method\.(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)|Request\.(get|post))\s*->`)

	// Finatra: @Get("/path") or @Post("/path") annotations
	reFinatraRoute = regexp.MustCompile(
		`@(Get|Post|Put|Delete|Patch|Head)\s*\(\s*"([^"]*)"`)

	// Lagom: namedCall("name") or pathCall("/path", ...)
	reLagomCall = regexp.MustCompile(
		`\b(?:namedCall|pathCall|restCall)\s*\(\s*"([^"]*)"`)

	// Play: conf/routes file patterns (GET /path controller.method)
	rePlayRoute = regexp.MustCompile(
		`(?m)^(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s+(/[^\s]*)\s+(\w+(?:\.\w+)+)`)

	// Play router pattern: routes.reverse or Routes.
	rePlayRouterPattern = regexp.MustCompile(
		`\b(?:routes\.|reverse[A-Z]|\bRoutes\.)`)
)

// ---------------------------------------------------------------------------
// Auth regexes
// ---------------------------------------------------------------------------

var (
	// Akka-HTTP: authenticateBasic / authenticateOAuth2 / headerValueByName("Authorization")
	reAkkaAuth = regexp.MustCompile(
		`\b(?:authenticateBasic|authenticateOAuth2|authenticateBasicAsync|headerValueByName\s*\(\s*"Authorization")\b`)

	// http4s: AuthMiddleware / BasicCredentials / KleisliMiddleware
	reHttp4sAuth = regexp.MustCompile(
		`\b(?:AuthMiddleware|BasicCredentials|AuthedRoutes)\b`)

	// Scalatra: BasicAuth / scentry / SecureHello
	reScalatraAuth = regexp.MustCompile(
		`\b(?:ScentrySupport|BasicAuthSupport|SecureHello|ScentryStrategy)\b`)

	// Cask: no built-in auth — detect manual header checking
	reCaskAuth = regexp.MustCompile(
		`request\.headers\s*\.\s*get\s*\(\s*"Authorization"`)

	// ZIO-HTTP: HttpMiddleware.basicAuth / bearerAuth
	reZioAuth = regexp.MustCompile(
		`\b(?:Middleware\.basicAuth|Middleware\.bearerAuth|basicAuth|bearerAuth)\b`)

	// Finatra: @Authenticated / Twitter credentials
	reFinatraAuth = regexp.MustCompile(
		`\b(?:@Authenticated|TwitterServerCredentials|OAuthAuthenticator)\b`)

	// Lagom: ServiceCall.authenticated / Topic
	reLagomAuth = regexp.MustCompile(
		`\b(?:authenticated|ServiceCallFactory|HeaderFilter)\b`)

	// Generic token/JWT check across frameworks
	reGenericAuth = regexp.MustCompile(
		`\b(?:JWT|JwtToken|BearerToken|ApiKey|Authorization)\b`)
)

// ---------------------------------------------------------------------------
// Middleware regexes
// ---------------------------------------------------------------------------

var (
	// Akka-HTTP: mapRequest / mapResponse / DebuggingDirectives
	reAkkaMiddleware = regexp.MustCompile(
		`\b(?:mapRequest|mapResponse|withSettings|DebuggingDirectives|handleExceptions|handleRejections|cors)\b`)

	// http4s: HttpRoutes middleware composition (HttpApp, Kleisli, Middleware)
	reHttp4sMiddleware = regexp.MustCompile(
		`\b(?:Middleware|Logger\.httpRoutes|GZip|AutoSlash|Timeout\.httpRoutes|CORS\.policy)\b`)

	// Scalatra: before/after blocks
	reScalatraMiddleware = regexp.MustCompile(
		`(?m)^\s*(before|after)\s*\{`)

	// Cask: cask.Decorator / annotation-based middleware
	reCaskMiddleware = regexp.MustCompile(
		`\bcask\.Decorator\b|extends\s+cask\.RawDecorator`)

	// ZIO-HTTP: HttpMiddleware / Middleware.runBefore
	reZioMiddleware = regexp.MustCompile(
		`\b(?:HttpMiddleware|Middleware\.runBefore|Middleware\.runAfter|HttpApp\.collectZIO)\b`)

	// Finatra: SimpleFilter / TypeAgnosticFilter
	reFinatraMiddleware = regexp.MustCompile(
		`\b(?:SimpleFilter|TypeAgnosticFilter|Filter\[|HttpFilter)\b`)

	// Lagom: ServiceLocator / CircuitBreaker / ServiceCall.invoke
	reLagomMiddleware = regexp.MustCompile(
		`\b(?:CircuitBreaker|ServiceLocator|HeaderFilter|ServiceCall\.invoke)\b`)
)

// ---------------------------------------------------------------------------
// Validation regexes
// ---------------------------------------------------------------------------

var (
	// case class fields with type annotations (DTO pattern)
	reDTOCaseClass = regexp.MustCompile(
		`(?m)^\s*case\s+class\s+(\w+)\s*\(([^)]+)\)`)

	// request body extraction patterns
	reAkkaValidation = regexp.MustCompile(
		`\b(?:entity\s*\(\s*as\[|validate\s*\(|Validator\.)\b`)

	reHttp4sValidation = regexp.MustCompile(
		`\b(?:jsonOf\[|EntityDecoder|decode\[|as\[)\b`)

	reScalatraValidation = regexp.MustCompile(
		`\b(?:params\(|multiParams\(|parsedBody|request\.body)\b`)

	reCaskValidation = regexp.MustCompile(
		`\b(?:cask\.Request|request\.data|readAll)\b`)

	reZioValidation = regexp.MustCompile(
		`\b(?:Request\.body|body\.asString|ZIO\.fromEither|decode\[)\b`)

	reFinatraValidation = regexp.MustCompile(
		`\b(?:request\.params|request\.contentString|@NotEmpty|@Min|@Max)\b`)

	reLagomValidation = regexp.MustCompile(
		`\b(?:MessageSerializer|JsonSerializer|ExceptionSerializer)\b`)
)

// ---------------------------------------------------------------------------
// Observability regexes — shared across all Scala frameworks
// ---------------------------------------------------------------------------

var (
	// log_extraction: SLF4J (play-logback, akka-slf4j, logback-classic)
	reScalaSlf4j = regexp.MustCompile(
		`\b(?:LoggerFactory|LogManager|Logger)\s*\.\s*getLogger\s*\(`)

	reScalaLogStatement = regexp.MustCompile(
		`\b(?:log|logger|LOG)\s*\.\s*(trace|debug|info|warn|error)\s*\(`)

	// play.api.Logger
	rePlayLogger = regexp.MustCompile(
		`\b(?:play\.api\.Logger|Logger\s*\.\s*(trace|debug|info|warn|error))\b`)

	// Akka/Pekko actor logging
	reAkkaLogging = regexp.MustCompile(
		`\b(?:Logging\s*\(|log\s*\.\s*(debug|info|warning|error)\s*\(|ActorLogging)\b`)

	// Cats Effect / ZIO logging: LogF / ZIO.logInfo
	reEffectLogging = regexp.MustCompile(
		`\b(?:Logger\[IO\]|Slf4jLogger|ZIO\.log(?:Info|Warning|Error|Debug)|log\.info|log\.warn)\b`)

	// metric_extraction: Micrometer, Kamon, Dropwizard Metrics
	reScalaMetric = regexp.MustCompile(
		`\b(?:Counter\s*\.\s*builder|Timer\s*\.\s*builder|Gauge\s*\.\s*builder|MeterRegistry|Kamon\.|metrics\s*\.\s*counter)\b`)

	// trace_extraction: OpenTelemetry, Jaeger, Zipkin, Kamon tracing
	reScalaTrace = regexp.MustCompile(
		`\b(?:Tracer\s*\.\s*start|tracer\s*\.\s*startSpan|Span\s*\.\s*current|GlobalTracer|DDTracer|Kamon\.span|b3Header)\b`)
)

// ---------------------------------------------------------------------------
// Testing regexes
// ---------------------------------------------------------------------------

var (
	// ScalaTest: extends FlatSpec / WordSpec / AnyFlatSpec / FunSuite
	reScalaTest = regexp.MustCompile(
		`\b(?:extends\s+(?:AnyFlatSpec|FlatSpec|WordSpec|AnyWordSpec|FunSpec|AnyFunSpec|FunSuite|AnyFunSuite|FeatureSpec|PropSpec))\b`)

	// Specs2: extends Specification
	reSpecs2 = regexp.MustCompile(
		`\bextends\s+(?:Specification|mutable\.Specification)\b`)

	// MUnit (cats-effect, http4s): extends munit.FunSuite / munit.CatsEffectSuite
	reMUnit = regexp.MustCompile(
		`\bextends\s+(?:munit\.FunSuite|munit\.CatsEffectSuite|CatsEffectSuite)\b`)

	// Akka TestKit: extends TestKit / extends ActorSpec
	reAkkaTestKit = regexp.MustCompile(
		`\bextends\s+(?:TestKit|ActorSpec|AkkaSpec|PekkoSpec|ScalaTestWithActorTestKit)\b`)

	// http4s: Http4sClientDsl / Http4sMunitCirceSuite
	reHttp4sTest = regexp.MustCompile(
		`\bextends\s+(?:Http4sClientDsl|Http4sMunitCirceSuite|Http4sSuite)\b`)

	// Cask: requests.get in test context
	reCaskTest = regexp.MustCompile(
		`\bTestServer\b|requests\.(get|post|put|delete)\s*\(`)

	// ZIO Test: extends ZIOSpec / ZIOSpecDefault
	reZioTest = regexp.MustCompile(
		`\bextends\s+(?:ZIOSpec|ZIOSpecDefault|ZIOSpecAbstract)\b`)

	// Finatra: extends HttpTest / FeatureTest
	reFinatraTest = regexp.MustCompile(
		`\bextends\s+(?:HttpTest|FeatureTest|TwitterServer|EmbeddedTwitterServer)\b`)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *scalaFrameworksExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/scala")
	_, span := tracer.Start(ctx, "indexer.scala_frameworks.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("extractor", "frameworks"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "scala" {
		return nil, nil
	}

	src := string(file.Content)
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

	// Detect framework from imports / patterns
	framework := detectScalaFramework(src)

	// ---------------------------------------------------------------------------
	// Routing extraction
	// ---------------------------------------------------------------------------
	switch framework {
	case "akka-http", "pekko-http":
		for _, m := range reAkkaRoute.FindAllStringSubmatchIndex(src, -1) {
			method := src[m[2]:m[3]]
			ent := makeEntity("route:"+method, "SCOPE.Operation", "http_route", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", framework, "http_method", strings.ToUpper(method), "provenance", "AKKA_HTTP_ROUTE_DSL")
			add(ent)
		}
		for _, m := range reAkkaPathDirective.FindAllStringSubmatchIndex(src, -1) {
			path := src[m[2]:m[3]]
			ent := makeEntity(path, "SCOPE.Operation", "http_route", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", framework, "http_path", path, "provenance", "AKKA_HTTP_PATH_DIRECTIVE")
			add(ent)
		}

	case "http4s":
		for _, m := range reHttp4sRoute.FindAllStringSubmatchIndex(src, -1) {
			method := src[m[2]:m[3]]
			ent := makeEntity("route:"+method, "SCOPE.Operation", "http_route", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "http4s", "http_method", method, "provenance", "HTTP4S_HTTPROUTES_DSL")
			add(ent)
		}

	case "scalatra":
		for _, m := range reScalatraRoute.FindAllStringSubmatchIndex(src, -1) {
			method := src[m[2]:m[3]]
			path := ""
			if m[4] >= 0 {
				path = src[m[4]:m[5]]
			}
			ent := makeEntity(method+":"+path, "SCOPE.Operation", "http_route", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "scalatra", "http_method", strings.ToUpper(method), "http_path", path, "provenance", "SCALATRA_ROUTE")
			add(ent)
		}

	case "cask":
		for _, m := range reCaskRoute.FindAllStringSubmatchIndex(src, -1) {
			method := src[m[2]:m[3]]
			path := src[m[4]:m[5]]
			ent := makeEntity(method+":"+path, "SCOPE.Operation", "http_route", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "cask", "http_method", strings.ToUpper(method), "http_path", path, "provenance", "CASK_ANNOTATION_ROUTE")
			add(ent)
		}

	case "zio-http":
		for _, m := range reZioHTTPRoute.FindAllStringSubmatchIndex(src, -1) {
			method := ""
			if m[2] >= 0 {
				method = src[m[2]:m[3]]
			} else if m[4] >= 0 {
				method = strings.ToUpper(src[m[4]:m[5]])
			}
			ent := makeEntity("route:"+method, "SCOPE.Operation", "http_route", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "zio-http", "http_method", method, "provenance", "ZIO_HTTP_ROUTE")
			add(ent)
		}

	case "finatra":
		for _, m := range reFinatraRoute.FindAllStringSubmatchIndex(src, -1) {
			method := src[m[2]:m[3]]
			path := src[m[4]:m[5]]
			ent := makeEntity(method+":"+path, "SCOPE.Operation", "http_route", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "finatra", "http_method", strings.ToUpper(method), "http_path", path, "provenance", "FINATRA_ANNOTATION_ROUTE")
			add(ent)
		}

	case "lagom":
		for _, m := range reLagomCall.FindAllStringSubmatchIndex(src, -1) {
			path := src[m[2]:m[3]]
			ent := makeEntity("lagom:"+path, "SCOPE.Operation", "lagom_service_call", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "lagom", "service_descriptor", path, "provenance", "LAGOM_SERVICE_DESCRIPTOR")
			add(ent)
		}

	case "play":
		for _, m := range rePlayRoute.FindAllStringSubmatchIndex(src, -1) {
			method := src[m[2]:m[3]]
			path := src[m[4]:m[5]]
			controller := ""
			if m[6] >= 0 {
				controller = src[m[6]:m[7]]
			}
			ent := makeEntity(method+":"+path, "SCOPE.Operation", "http_route", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "play", "http_method", method, "http_path", path, "controller", controller, "provenance", "PLAY_ROUTES_FILE")
			add(ent)
		}
		// router pattern detection
		if rePlayRouterPattern.MatchString(src) {
			ent := makeEntity("PlayRouter", "SCOPE.Router", "play_router", file.Path, file.Language, 1)
			setProps(&ent, "framework", "play", "provenance", "PLAY_ROUTER_PATTERN")
			add(ent)
		}
	}

	// ---------------------------------------------------------------------------
	// Auth extraction
	// ---------------------------------------------------------------------------
	authRe := authReForFramework(framework)
	if authRe != nil {
		for _, m := range authRe.FindAllStringSubmatchIndex(src, -1) {
			ent := makeEntity("auth:"+src[m[0]:m[1]], "SCOPE.Security", "auth_check", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", framework, "provenance", "AUTH_PATTERN")
			add(ent)
		}
	}
	// generic auth fallback
	if reGenericAuth.MatchString(src) {
		ent := makeEntity("auth:generic", "SCOPE.Security", "auth_check", file.Path, file.Language, 1)
		setProps(&ent, "framework", framework, "provenance", "GENERIC_AUTH_PATTERN")
		add(ent)
	}

	// ---------------------------------------------------------------------------
	// Middleware extraction
	// ---------------------------------------------------------------------------
	mwRe := middlewareReForFramework(framework)
	if mwRe != nil {
		for _, m := range mwRe.FindAllStringSubmatchIndex(src, -1) {
			ent := makeEntity("middleware:"+src[m[0]:min(m[0]+40, len(src))], "SCOPE.Middleware", "middleware", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", framework, "provenance", "MIDDLEWARE_PATTERN")
			add(ent)
		}
	}

	// ---------------------------------------------------------------------------
	// Validation / DTO extraction
	// ---------------------------------------------------------------------------
	for _, m := range reDTOCaseClass.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Type", "dto", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", framework, "provenance", "CASE_CLASS_DTO")
		add(ent)
	}
	valRe := validationReForFramework(framework)
	if valRe != nil {
		if valRe.MatchString(src) {
			ent := makeEntity("validation:"+framework, "SCOPE.Operation", "request_validation", file.Path, file.Language, 1)
			setProps(&ent, "framework", framework, "provenance", "VALIDATION_PATTERN")
			add(ent)
		}
	}

	// ---------------------------------------------------------------------------
	// Observability extraction (shared, language-level)
	// ---------------------------------------------------------------------------

	// log_extraction
	if reScalaSlf4j.MatchString(src) || reScalaLogStatement.MatchString(src) ||
		rePlayLogger.MatchString(src) || reAkkaLogging.MatchString(src) || reEffectLogging.MatchString(src) {
		ent := makeEntity("logger:"+strings.TrimSuffix(fileBaseName(file.Path), ".scala"), "SCOPE.Observability", "log_statement", file.Path, file.Language, 1)
		setProps(&ent, "framework", framework, "provenance", "SCALA_LOGGER")
		add(ent)
	}

	// metric_extraction
	if reScalaMetric.MatchString(src) {
		ent := makeEntity("metrics:"+fileBaseName(file.Path), "SCOPE.Observability", "metric", file.Path, file.Language, 1)
		setProps(&ent, "framework", framework, "provenance", "SCALA_METRIC")
		add(ent)
	}

	// trace_extraction
	if reScalaTrace.MatchString(src) {
		ent := makeEntity("trace:"+fileBaseName(file.Path), "SCOPE.Observability", "trace", file.Path, file.Language, 1)
		setProps(&ent, "framework", framework, "provenance", "SCALA_TRACE")
		add(ent)
	}

	// ---------------------------------------------------------------------------
	// Testing linkage
	// ---------------------------------------------------------------------------
	isTest := reScalaTest.MatchString(src) || reSpecs2.MatchString(src) || reMUnit.MatchString(src) ||
		reAkkaTestKit.MatchString(src) || reHttp4sTest.MatchString(src) || reCaskTest.MatchString(src) ||
		reZioTest.MatchString(src) || reFinatraTest.MatchString(src)

	if isTest {
		ent := makeEntity("test:"+fileBaseName(file.Path), "SCOPE.Test", "test_suite", file.Path, file.Language, 1)
		setProps(&ent, "framework", framework, "provenance", "SCALA_TEST_SUITE")
		add(ent)
	}

	return entities, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// detectScalaFramework returns the dominant framework based on imports/code patterns.
func detectScalaFramework(src string) string {
	switch {
	case strings.Contains(src, "akka.http") || strings.Contains(src, "pekko.http"):
		return "akka-http"
	case strings.Contains(src, "org.http4s"):
		return "http4s"
	case strings.Contains(src, "org.scalatra"):
		return "scalatra"
	case strings.Contains(src, "import cask") || strings.Contains(src, "@cask."):
		return "cask"
	case strings.Contains(src, "dev.zio") || strings.Contains(src, "zio.http"):
		return "zio-http"
	case strings.Contains(src, "com.twitter.finatra"):
		return "finatra"
	case strings.Contains(src, "com.lightbend.lagom"):
		return "lagom"
	case strings.Contains(src, "play.api") || strings.Contains(src, "import play.") ||
		// Play conf/routes file: lines like "GET /path controllers.Ctrl.action"
		rePlayRoute.MatchString(src):
		return "play"
	default:
		return "scala"
	}
}

func authReForFramework(fw string) *regexp.Regexp {
	switch fw {
	case "akka-http":
		return reAkkaAuth
	case "http4s":
		return reHttp4sAuth
	case "scalatra":
		return reScalatraAuth
	case "cask":
		return reCaskAuth
	case "zio-http":
		return reZioAuth
	case "finatra":
		return reFinatraAuth
	case "lagom":
		return reLagomAuth
	}
	return nil
}

func middlewareReForFramework(fw string) *regexp.Regexp {
	switch fw {
	case "akka-http":
		return reAkkaMiddleware
	case "http4s":
		return reHttp4sMiddleware
	case "scalatra":
		return reScalatraMiddleware
	case "cask":
		return reCaskMiddleware
	case "zio-http":
		return reZioMiddleware
	case "finatra":
		return reFinatraMiddleware
	case "lagom":
		return reLagomMiddleware
	}
	return nil
}

func validationReForFramework(fw string) *regexp.Regexp {
	switch fw {
	case "akka-http":
		return reAkkaValidation
	case "http4s":
		return reHttp4sValidation
	case "scalatra":
		return reScalatraValidation
	case "cask":
		return reCaskValidation
	case "zio-http":
		return reZioValidation
	case "finatra":
		return reFinatraValidation
	case "lagom":
		return reLagomValidation
	}
	return nil
}

func fileBaseName(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return path
	}
	return parts[len(parts)-1]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
