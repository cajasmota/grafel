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
//	framework.akka-http           Auth/auth_coverage              full     (deep)
//	framework.http4s              Auth/auth_coverage              full     (deep)
//	framework.play                Auth/auth_coverage              full     (deep)
//	framework.cask                Auth/auth_coverage              partial
//	framework.finatra             Auth/auth_coverage              partial
//	framework.lagom               Auth/auth_coverage              partial
//	framework.scalatra            Auth/auth_coverage              partial
//	framework.zio-http            Auth/auth_coverage              partial
//
//	framework.akka-http           Middleware/middleware_coverage   full    (deep)
//	framework.http4s              Middleware/middleware_coverage   full    (deep)
//	framework.play                Middleware/middleware_coverage   full    (deep)
//	framework.cask                Middleware/middleware_coverage   partial
//	framework.finatra             Middleware/middleware_coverage   partial
//	framework.lagom               Middleware/middleware_coverage   partial
//	framework.scalatra            Middleware/middleware_coverage   partial
//	framework.zio-http            Middleware/middleware_coverage   partial
//
// Deep auth/middleware (http4s/akka-http/play) stamp the specific auth method
// (basic/oauth2/bearer/jwt) plus named realm/authenticator/action and the named
// middleware/filters/directives with composition order. Other frameworks remain
// regex-detection partial. Cross-file route→middleware binding is unresolved.
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
//	all 9 framework records       Observability/metric_extraction full
//	all 9 framework records       Observability/trace_extraction  full
//
//	metric_extraction / trace_extraction are FULL: reScalaMetricNamed and
//	reScalaTraceNamed capture the LITERAL metric/span name per call site
//	(Kamon counter/gauge/histogram/timer, Micrometer builder/registry, Dropwizard
//	meter; Kamon span(Builder), OTel tracer.spanBuilder, natchez Trace[F].span).
//	The name is a literal string arg — no cross-file resolution. A file-local
//	fallback still emits for dynamic (non-literal) names.
//	log_extraction stays PARTIAL: log statements are detected but the logger
//	identity and message<->logger binding require cross-file dataflow (logger
//	field decl -> call site) — same honest limit as Java/PHP/Rust/Kotlin.
//
//	all 9 framework records       Testing/tests_linkage           partial
//
// Honest limit: all regex-based, file-local. Cross-file route wiring is not
// resolved. Most cells are partial; the deep auth/middleware cells above
// (http4s/akka-http/play) are full.
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
	// Akka-HTTP path directive capturing the FULL path expression (string
	// literals AND positional PathMatcher tokens), e.g.
	//   path("users" / LongNumber / "posts")
	// Group 1 = inner expression up to the matching close paren (paren-free,
	// which is the common case; nested matcher calls are handled by the
	// dedicated matcher walker in routing.go).
	reAkkaPathDirective = regexp.MustCompile(
		`\b(?:path|pathEnd|pathPrefix|rawPathPrefix)\s*\(\s*("(?:[^"]*)"(?:\s*/\s*[^){]+?)?|[A-Za-z_]\w*(?:\s*/\s*[^){]+?)?)\s*\)`)

	// Akka-HTTP positional context: find positions of pathPrefix/path directives and
	// HTTP method directives, then associate each method with nearest enclosing path context.
	// Both capture the full path EXPRESSION (literals + matchers) for canonicalisation.
	reAkkaFwPathPrefix = regexp.MustCompile(
		`\bpathPrefix\s*\(\s*("(?:[^"]*)"(?:\s*/\s*[^){]+?)?|[A-Za-z_]\w*(?:\s*/\s*[^){]+?)?)\s*\)`)

	reAkkaFwPathSeg = regexp.MustCompile(
		`\bpath\s*\(\s*("(?:[^"]*)"(?:\s*/\s*[^){]+?)?|[A-Za-z_]\w*(?:\s*/\s*[^){]+?)?)\s*\)`)

	reAkkaMethodDir = regexp.MustCompile(
		`\b(get|post|put|delete|patch|head|options)\s*\{`)

	// http4s: case GET -> Root / "seg1" / Var(id) / "seg2" => ...
	// Group 1 = method, group 2 = the full segment expression after Root (string
	// literals AND named extractor vars). The expression is canonicalised by
	// canonicalScalaPathExpr which handles LongVar(id) / IntVar / UUIDVar / etc.
	reHttp4sRoute = regexp.MustCompile(
		`\bcase\s+(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*->\s*Root((?:\s*/\s*[^=]+?)?)\s*=>`)

	// Scalatra: get("/path") { ... }
	reScalatraRoute = regexp.MustCompile(
		`\b(get|post|put|delete|patch|head|options)\s*\(\s*"([^"]*)"`)

	// Cask: @cask.get("/path") or @cask.post("/path")
	reCaskRoute = regexp.MustCompile(
		`@cask\.(get|post|put|delete|patch)\s*\(\s*"([^"]*)"`)

	// ZIO-HTTP (collect DSL): case Method.GET -> Root / "seg" / int("id") => ...
	// Group 1 = method, group 2 = full segment expression after Root.
	reZioHTTPRoute = regexp.MustCompile(
		`\bcase\s+Method\.(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*->\s*Root((?:\s*/\s*[^=]+?)?)\s*=>`)

	// ZIO-HTTP (Scala-3 Routes DSL): Method.GET / "users" / int("id") -> handler
	// Group 1 = method, group 2 = full segment expression up to the `->` arrow.
	reZioRoutesDSL = regexp.MustCompile(
		`\bMethod\.(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*((?:/\s*[^-]+?)?)\s*->`)

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
// Deep AUTH extraction — flagship frameworks (http4s / akka-http / play).
//
// These capture the *specific auth method* (basic / oauth2 / bearer-jwt) plus
// the named realm / authenticator / action so a downstream consumer can answer
// "which auth scheme guards this route and what is it named", not merely "auth
// exists somewhere in this file".
// ---------------------------------------------------------------------------

var (
	// akka-http security directives. Capture realm + authenticator name where present.
	//   authenticateBasic(realm = "secure", myAuthenticator)
	//   authenticateBasicAsync(realm = "secure", asyncAuth)
	//   authenticateOAuth2(realm = "api", tokenAuth)
	//   authenticateOAuth2Async("api", tokenAuth)
	reAkkaAuthDirective = regexp.MustCompile(
		`\b(authenticateBasic|authenticateBasicAsync|authenticateBasicPF|authenticateBasicPFAsync|authenticateOAuth2|authenticateOAuth2Async|authenticateOrRejectWithChallenge)\s*\(`)

	// akka-http realm argument: realm = "secure"  OR  positional first string arg
	reAkkaAuthRealm = regexp.MustCompile(
		`\brealm\s*=\s*"([^"]*)"`)

	// akka-http authorization directive: authorize(...) / authorizeAsync(...)
	reAkkaAuthorize = regexp.MustCompile(
		`\b(authorize|authorizeAsync)\s*\(`)

	// http4s AuthMiddleware: AuthMiddleware(authUser)  /  AuthMiddleware(authUser, onFailure)
	reHttp4sAuthMiddleware = regexp.MustCompile(
		`\bAuthMiddleware(?:\.noSpider|\.withFallThrough)?\s*\(\s*([A-Za-z_][\w]*)`)

	// http4s authed routes: AuthedRoutes.of[User, IO] { ... }
	reHttp4sAuthedRoutes = regexp.MustCompile(
		`\bAuthedRoutes\.of\s*(?:\[[^\]]*\])?\s*\{`)

	// http4s credential constructs → infer scheme
	reHttp4sBasicCreds = regexp.MustCompile(`\bBasicCredentials\b`)
	reHttp4sBearer     = regexp.MustCompile(`\b(?:Authorization\s*\(\s*Credentials\.Token\s*\(\s*AuthScheme\.Bearer|AuthScheme\.Bearer|Credentials\.Token)\b`)

	// play auth actions: object/class Foo extends ActionBuilder / ActionFilter / ActionRefiner
	//   class AuthenticatedAction extends ActionBuilder[UserRequest, AnyContent]
	//   object AuthFilter extends ActionFilter[Request]
	// The pre-extends gap allows newlines (constructor params often wrap) but is
	// length-bounded and forbids a second declaration keyword so it cannot leap
	// across an adjacent class/object into the wrong base type.
	rePlayAuthAction = regexp.MustCompile(
		`\b(?:class|object|trait)\s+(\w+)(?:[^{]{0,200}?)\bextends\b[^{\n]{0,120}?\b(ActionBuilder|ActionFilter|ActionRefiner|ActionTransformer)\b`)

	// Cross-framework auth scheme hints (used to refine method classification).
	reSchemeJwt    = regexp.MustCompile(`\b(?:JWT|JwtToken|Jwt\.decode|pdi\.jwt|decodeJwt|JwtClaim|JwtCirce|JwtSprayJson)\b`)
	reSchemeBearer = regexp.MustCompile(`\b(?:[Bb]earer(?:Auth|Token)?|AuthScheme\.Bearer)\b`)
	reSchemeBasic  = regexp.MustCompile(`\b(?:[Bb]asic(?:Auth|Credentials)?|authenticateBasic)\b`)
	reSchemeOAuth2 = regexp.MustCompile(`\b(?:OAuth2|authenticateOAuth2)\b`)
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
// Deep MIDDLEWARE extraction — flagship frameworks (http4s / akka-http / play).
//
// These capture *named* middleware/filters/directives and their composition
// *order*, not just "middleware exists". For http4s the order is the wrapping
// order of mw1(mw2(routes)); for play it is the declared order in the Filters
// chain; for akka-http it is the lexical order of handle*/directive calls.
// ---------------------------------------------------------------------------

var (
	// http4s named built-in middlewares applied to routes/httpApp.
	//   CORS.policy(...)  CORS(routes)  GZip(routes)  Logger.httpApp(...)  AutoSlash(...)  Timeout(...)
	reHttp4sNamedMw = regexp.MustCompile(
		`\b(CORS|GZip|Logger|AutoSlash|Timeout|RequestLogger|ResponseLogger|HSTS|CSRF|ErrorAction|ErrorHandling|EntityLimiter|ConcurrentRequests|FollowRedirect)\b\s*(?:\.(?:httpApp|httpRoutes|policy|apply|default|impl)\b)?\s*\(`)

	// http4s middleware composition: mw1(mw2(routes)) — capture the outer call chain
	// of identifier( identifier( ... so we can record nesting order. We detect a
	// chain of >=2 nested single-arg applications wrapping a routes/app identifier.
	reHttp4sCompose = regexp.MustCompile(
		`\b([A-Z]\w+)\s*\(\s*([A-Z]\w+)\s*\(\s*([A-Za-z_]\w*)\s*\)\s*\)`)

	// akka-http handler directives (rejection/exception handling middleware).
	//   handleRejections(rejectionHandler)  handleExceptions(exceptionHandler)
	reAkkaHandleDir = regexp.MustCompile(
		`\b(handleRejections|handleExceptions)\s*\(\s*([A-Za-z_]\w*)?`)

	// akka-http transform directives commonly used as middleware.
	reAkkaTransformDir = regexp.MustCompile(
		`\b(mapRequest|mapResponse|mapInnerRoute|withRequestTimeout|encodeResponse|decodeRequest|logRequestResult|logRequest|logResult|extractRequestContext)\b`)

	// akka-http CORS (akka-http-cors / pekko-http-cors): cors() / cors(settings)
	reAkkaCors = regexp.MustCompile(`\bcors\s*\(`)

	// play global filter chain. Two shapes:
	//   class Filters @Inject() (...) extends DefaultHttpFilters(a, b, c)
	//   override def filters: Seq[EssentialFilter] = Seq(a, b, c)
	rePlayDefaultFilters = regexp.MustCompile(
		`\bextends\s+(?:DefaultHttpFilters|HttpFilters)\b\s*(?:\(([^)]*)\))?`)
	rePlayFiltersSeq = regexp.MustCompile(
		`\bdef\s+filters\s*(?::\s*Seq\[\s*EssentialFilter\s*\])?\s*=\s*Seq\s*\(([^)]*)\)`)

	// play custom filter definition: class X extends Filter / EssentialFilter
	rePlayFilterDef = regexp.MustCompile(
		`\b(?:class|object)\s+(\w+)[^\n{]*?\bextends\b[^\n{]*?\b(EssentialFilter|Filter)\b`)

	// generic comma-separated identifier splitter for filter chains.
	reIdentList = regexp.MustCompile(`[A-Za-z_]\w*`)
)

// ---------------------------------------------------------------------------
// Validation regexes
// ---------------------------------------------------------------------------

var (
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

	// reScalaMetricNamed captures the LITERAL metric name at the call site.
	// Covers:
	//   Kamon:        Kamon.counter("name") / .gauge("name") / .histogram("name") /
	//                 .timer("name") / .rangeSampler("name")
	//   Micrometer:   Counter.builder("name") / Timer.builder("name") /
	//                 Gauge.builder("name", ...) / DistributionSummary.builder("name");
	//                 registry.counter("name") / .timer("name") / .gauge("name") / .summary("name")
	//   Dropwizard:   metrics.meter("name") / .counter("name") / .timer("name") /
	//                 .histogram("name")
	// Group 1 = instrument kind, group 2 = literal metric name. The name is a
	// literal string arg at the call site — no cross-file resolution needed.
	reScalaMetricNamed = regexp.MustCompile(
		`\b(?:Kamon\s*\.\s*(counter|gauge|histogram|timer|rangeSampler)` +
			`|(?:Counter|Timer|Gauge|DistributionSummary)\s*\.\s*(builder)` +
			`|(?:registry|meterRegistry|metrics|metricRegistry|meters)\s*\.\s*(counter|timer|gauge|summary|meter|histogram))` +
			`\s*\(\s*"([^"]+)"`)

	// trace_extraction: OpenTelemetry, Jaeger, Zipkin, Kamon tracing
	reScalaTrace = regexp.MustCompile(
		`\b(?:Tracer\s*\.\s*start|tracer\s*\.\s*startSpan|Span\s*\.\s*current|GlobalTracer|DDTracer|Kamon\.span|b3Header)\b`)

	// reScalaTraceNamed captures the LITERAL span name at the call site. Covers:
	//   Kamon:        Kamon.span("name") / Kamon.spanBuilder("name") /
	//                 Kamon.serverSpanBuilder("name") / Kamon.clientSpanBuilder("name")
	//   OpenTelemetry:tracer.spanBuilder("name")
	//   natchez:      Trace[F].span("name") / trace.span("name")
	//   generic OTel: tracer.startSpan("name")
	// Group with index 1 = literal span name. Literal arg at the call site.
	reScalaTraceNamed = regexp.MustCompile(
		`\b(?:Kamon\s*\.\s*(?:span|spanBuilder|serverSpanBuilder|clientSpanBuilder)` +
			`|(?:tracer|Tracer)\s*\.\s*(?:spanBuilder|startSpan)` +
			`|Trace\s*\[\s*\w+\s*\]\s*\.\s*span` +
			`|(?:trace|tracing)\s*\.\s*span)` +
			`\s*\(\s*"([^"]+)"`)
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
	case "tapir":
		// tapir endpoint-DSL: routes + request/response/error DTO refs + handler
		// attribution are parsed from each `endpoint`(.get/.post/...).in(...).out(...)
		// chain (backend-agnostic). See tapir.go.
		for _, ent := range extractTapirEndpoints(src, fileMeta{Path: file.Path, Language: file.Language}) {
			add(ent)
		}

	case "akka-http", "pekko-http":
		// Positional combination: associate each HTTP method directive with the nearest
		// preceding pathPrefix and path segment directives (within a 512-char window).
		// This handles any number of method siblings inside the same path block.
		type akkaPathPos struct {
			pos int
			val string
		}
		var prefixPositions []akkaPathPos
		for _, m := range reAkkaFwPathPrefix.FindAllStringSubmatchIndex(src, -1) {
			prefixPositions = append(prefixPositions, akkaPathPos{m[0], src[m[2]:m[3]]})
		}
		var segPositions []akkaPathPos
		for _, m := range reAkkaFwPathSeg.FindAllStringSubmatchIndex(src, -1) {
			segPositions = append(segPositions, akkaPathPos{m[0], src[m[2]:m[3]]})
		}
		nearestBefore := func(positions []akkaPathPos, pos int, window int) string {
			best := ""
			for _, p := range positions {
				if p.pos < pos && pos-p.pos <= window {
					best = p.val
				}
			}
			return best
		}

		combinedPathSeen := make(map[string]bool)
		const window = 512
		for _, m := range reAkkaMethodDir.FindAllStringSubmatchIndex(src, -1) {
			method := src[m[2]:m[3]]
			upperMethod := strings.ToUpper(method)
			seg := nearestBefore(segPositions, m[0], window)
			prefix := nearestBefore(prefixPositions, m[0], window)
			if seg != "" {
				// Canonicalise each path EXPRESSION (string literals + PathMatcher
				// tokens like LongNumber/Segment/JavaUUID → {param}) then compose
				// pathPrefix + path into one canonical path.
				segPath := canonicalScalaPathExpr(seg)
				prefixPath := ""
				if prefix != "" {
					prefixPath = canonicalScalaPathExpr(prefix)
				}
				fullPath := composeScalaPath(prefixPath, segPath)
				name := upperMethod + ":" + fullPath
				if prefix != "" {
					combinedPathSeen[prefix] = true
				}
				combinedPathSeen[seg] = true
				ent := makeEntity(name, "SCOPE.Operation", "http_route", file.Path, file.Language, lineOf(src, m[0]))
				setProps(&ent, "framework", framework, "http_method", upperMethod, "http_path", fullPath, "provenance", "AKKA_HTTP_COMBINED_ROUTE")
				add(ent)
			}
		}
		// Emit individual path-directive entities not covered by combined (no associated methods)
		for _, m := range reAkkaPathDirective.FindAllStringSubmatchIndex(src, -1) {
			expr := src[m[2]:m[3]]
			if combinedPathSeen[expr] {
				continue
			}
			canonical := canonicalScalaPathExpr(expr)
			ent := makeEntity(canonical, "SCOPE.Operation", "http_route", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", framework, "http_path", canonical, "provenance", "AKKA_HTTP_PATH_DIRECTIVE")
			add(ent)
		}

	case "http4s":
		for _, m := range reHttp4sRoute.FindAllStringSubmatchIndex(src, -1) {
			method := src[m[2]:m[3]]
			segChain := ""
			if m[4] >= 0 {
				segChain = src[m[4]:m[5]]
			}
			// Canonicalise the full Root / "seg" / LongVar(id) chain. The leading
			// `/` separators in segChain are split by canonicalScalaPathExpr; named
			// extractor vars (LongVar(id)/IntVar/UUIDVar) normalise to {name}.
			fullPath := canonicalScalaPathExpr(strings.TrimPrefix(strings.TrimSpace(segChain), "/"))
			name := "route:" + method + ":" + fullPath
			ent := makeEntity(name, "SCOPE.Operation", "http_route", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "http4s", "http_method", method, "http_path", fullPath, "provenance", "HTTP4S_HTTPROUTES_DSL")
			add(ent)
		}

	case "scalatra":
		for _, m := range reScalatraRoute.FindAllStringSubmatchIndex(src, -1) {
			method := src[m[2]:m[3]]
			path := ""
			if m[4] >= 0 {
				path = canonicalScalaColonPath(src[m[4]:m[5]])
			}
			ent := makeEntity(method+":"+path, "SCOPE.Operation", "http_route", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "scalatra", "http_method", strings.ToUpper(method), "http_path", path, "provenance", "SCALATRA_ROUTE")
			add(ent)
		}

	case "cask":
		for _, m := range reCaskRoute.FindAllStringSubmatchIndex(src, -1) {
			method := src[m[2]:m[3]]
			path := canonicalScalaColonPath(src[m[4]:m[5]])
			ent := makeEntity(method+":"+path, "SCOPE.Operation", "http_route", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "cask", "http_method", strings.ToUpper(method), "http_path", path, "provenance", "CASK_ANNOTATION_ROUTE")
			add(ent)
		}

	case "zio-http":
		// collect DSL: case Method.GET -> Root / "users" / int("id") => ...
		for _, m := range reZioHTTPRoute.FindAllStringSubmatchIndex(src, -1) {
			method := src[m[2]:m[3]]
			segChain := ""
			if m[4] >= 0 {
				segChain = src[m[4]:m[5]]
			}
			fullPath := canonicalScalaPathExpr(strings.TrimPrefix(strings.TrimSpace(segChain), "/"))
			name := "route:" + method + ":" + fullPath
			ent := makeEntity(name, "SCOPE.Operation", "http_route", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "zio-http", "http_method", method, "http_path", fullPath, "provenance", "ZIO_HTTP_ROUTE")
			add(ent)
		}
		// Scala-3 Routes DSL: Method.GET / "users" / int("id") -> handler(...)
		for _, m := range reZioRoutesDSL.FindAllStringSubmatchIndex(src, -1) {
			method := src[m[2]:m[3]]
			segChain := ""
			if m[4] >= 0 {
				segChain = src[m[4]:m[5]]
			}
			fullPath := canonicalScalaPathExpr(strings.TrimPrefix(strings.TrimSpace(segChain), "/"))
			name := "route:" + method + ":" + fullPath
			ent := makeEntity(name, "SCOPE.Operation", "http_route", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "zio-http", "http_method", method, "http_path", fullPath, "provenance", "ZIO_HTTP_ROUTES_DSL")
			add(ent)
		}

	case "finatra":
		for _, m := range reFinatraRoute.FindAllStringSubmatchIndex(src, -1) {
			method := src[m[2]:m[3]]
			path := canonicalScalaColonPath(src[m[4]:m[5]])
			ent := makeEntity(method+":"+path, "SCOPE.Operation", "http_route", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "finatra", "http_method", strings.ToUpper(method), "http_path", path, "provenance", "FINATRA_ANNOTATION_ROUTE")
			add(ent)
		}

	case "lagom":
		for _, m := range reLagomCall.FindAllStringSubmatchIndex(src, -1) {
			path := canonicalScalaColonPath(src[m[2]:m[3]])
			ent := makeEntity("lagom:"+path, "SCOPE.Operation", "lagom_service_call", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "lagom", "service_descriptor", path, "provenance", "LAGOM_SERVICE_DESCRIPTOR")
			add(ent)
		}

	case "play":
		for _, m := range rePlayRoute.FindAllStringSubmatchIndex(src, -1) {
			method := src[m[2]:m[3]]
			path := canonicalScalaColonPath(src[m[4]:m[5]])
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
	// Deep, value-asserting auth extraction for the flagship frameworks. These
	// stamp the specific auth method (basic / oauth2 / bearer-jwt / authorize)
	// plus the named realm / authenticator / action.
	deepAuthHandled := extractDeepAuth(framework, src, file, add)

	// Generic per-framework auth fallback (non-flagship frameworks, and any
	// flagship file with no deep match — e.g. a manual header check).
	if !deepAuthHandled {
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
	}

	// ---------------------------------------------------------------------------
	// Middleware extraction
	// ---------------------------------------------------------------------------
	// Deep, value-asserting middleware extraction for the flagship frameworks.
	// These stamp named middleware/filters/directives and their composition order.
	deepMwHandled := extractDeepMiddleware(framework, src, file, add)

	if !deepMwHandled {
		mwRe := middlewareReForFramework(framework)
		if mwRe != nil {
			for _, m := range mwRe.FindAllStringSubmatchIndex(src, -1) {
				ent := makeEntity("middleware:"+src[m[0]:min(m[0]+40, len(src))], "SCOPE.Middleware", "middleware", file.Path, file.Language, lineOf(src, m[0]))
				setProps(&ent, "framework", framework, "provenance", "MIDDLEWARE_PATTERN")
				add(ent)
			}
		}
	}

	// ---------------------------------------------------------------------------
	// Validation / DTO extraction
	//
	// Field-level DTO modeling (fields + types + Option nullability + codec +
	// wire-name overrides) and field-level request validation (refined / cats
	// Validated / accord / octopus — specific field + constraint). See
	// validation.go. Issue #3454.
	// ---------------------------------------------------------------------------
	fm := fileMeta{Path: file.Path, Language: file.Language}
	for _, ent := range extractScalaDTOFields(src, framework, fm) {
		add(ent)
	}
	for _, ent := range extractScalaValidation(src, framework, fm) {
		add(ent)
	}

	// Framework-specific request-body extraction directives (entity(as[T]),
	// jsonOf[T], params(), decode[T], ...) remain a coarse file-local signal
	// that a handler reads a request body, complementing the field-level
	// constraint entities above.
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

	// metric_extraction — per-call-site, with the literal metric name captured.
	// Each Kamon/Micrometer/Dropwizard instrument call with a string-literal name
	// becomes its own entity carrying metric_name. The name is a literal arg at
	// the call site, so no cross-file resolution is required.
	metricNameSeen := map[string]bool{}
	for _, m := range reScalaMetricNamed.FindAllStringSubmatchIndex(src, -1) {
		metricName := src[m[8]:m[9]]
		// instrument kind is whichever of the alternation groups matched
		instrument := ""
		for _, gi := range []int{2, 4, 6} {
			if m[gi] >= 0 {
				instrument = src[m[gi]:m[gi+1]]
				break
			}
		}
		key := metricName + "\x00" + instrument
		if metricNameSeen[key] {
			continue
		}
		metricNameSeen[key] = true
		ent := makeEntity("metric:"+metricName, "SCOPE.Observability", "metric", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", framework, "provenance", "SCALA_METRIC_NAMED",
			"metric_name", metricName, "instrument", instrument)
		add(ent)
	}
	// Fallback: metric usage detected but no literal name captured (dynamic name,
	// builder pattern split across lines, MeterRegistry passed around). File-local.
	if len(metricNameSeen) == 0 && reScalaMetric.MatchString(src) {
		ent := makeEntity("metrics:"+fileBaseName(file.Path), "SCOPE.Observability", "metric", file.Path, file.Language, 1)
		setProps(&ent, "framework", framework, "provenance", "SCALA_METRIC")
		add(ent)
	}

	// trace_extraction — per-call-site, with the literal span name captured.
	spanNameSeen := map[string]bool{}
	for _, m := range reScalaTraceNamed.FindAllStringSubmatchIndex(src, -1) {
		spanName := src[m[2]:m[3]]
		if spanNameSeen[spanName] {
			continue
		}
		spanNameSeen[spanName] = true
		ent := makeEntity("span:"+spanName, "SCOPE.Observability", "trace", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", framework, "provenance", "SCALA_TRACE_NAMED",
			"span_name", spanName)
		add(ent)
	}
	// Fallback: trace usage detected but no literal span name captured.
	if len(spanNameSeen) == 0 && reScalaTrace.MatchString(src) {
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

// ---------------------------------------------------------------------------
// Deep AUTH extraction (flagship frameworks)
// ---------------------------------------------------------------------------

// scalaAuthFirstStringArg returns the first quoted-string argument inside the
// parenthesised arg list that begins at openParen (index of '('). It uses a
// quoted-string match so a ')' inside the realm string does not truncate it.
func scalaAuthFirstStringArg(src string, openParen int) string {
	// Bound the search to the current call's argument region (next 200 chars).
	end := min(openParen+200, len(src))
	region := src[openParen:end]
	if m := reAkkaAuthRealm.FindStringSubmatch(region); m != nil {
		return m[1]
	}
	// positional first string literal
	if m := regexp.MustCompile(`"([^"]*)"`).FindStringSubmatch(region); m != nil {
		return m[1]
	}
	return ""
}

// classifyScalaAuthMethod infers the auth scheme from a snippet/window.
func classifyScalaAuthMethod(window string) string {
	switch {
	case reSchemeJwt.MatchString(window):
		return "jwt"
	case reSchemeOAuth2.MatchString(window):
		return "oauth2"
	case reSchemeBearer.MatchString(window):
		return "bearer"
	case reSchemeBasic.MatchString(window):
		return "basic"
	}
	return "custom"
}

// extractDeepAuth handles http4s / akka-http / play with method+name stamping.
// Returns true if it produced (or definitively handled) auth for a flagship
// framework, so the generic fallback can be skipped.
func extractDeepAuth(framework, src string, file extractor.FileInput, add func(types.EntityRecord)) bool {
	switch framework {
	case "akka-http", "pekko-http":
		handled := false
		// Security directives: authenticateBasic / authenticateOAuth2 / ...
		for _, m := range reAkkaAuthDirective.FindAllStringSubmatchIndex(src, -1) {
			directive := src[m[2]:m[3]]
			realm := scalaAuthFirstStringArg(src, m[1]-1)
			method := "custom"
			switch {
			case strings.Contains(directive, "OAuth2"):
				method = "oauth2"
			case strings.Contains(directive, "Basic"):
				method = "basic"
			}
			// Refine basic→jwt when the surrounding window decodes a JWT.
			winEnd := min(m[1]+240, len(src))
			if method == "basic" && reSchemeJwt.MatchString(src[m[0]:winEnd]) {
				method = "jwt"
			}
			name := "auth:akka:" + directive
			if realm != "" {
				name += ":" + realm
			}
			ent := makeEntity(name, "SCOPE.Security", "auth_check", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", framework, "auth_method", method, "directive", directive,
				"realm", realm, "provenance", "AKKA_HTTP_AUTH_DIRECTIVE")
			add(ent)
			handled = true
		}
		// Authorization directives (authorize / authorizeAsync).
		for _, m := range reAkkaAuthorize.FindAllStringSubmatchIndex(src, -1) {
			directive := src[m[2]:m[3]]
			ent := makeEntity("authz:akka:"+directive, "SCOPE.Security", "auth_check", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", framework, "auth_method", "authorize", "directive", directive,
				"provenance", "AKKA_HTTP_AUTHORIZE_DIRECTIVE")
			add(ent)
			handled = true
		}
		return handled

	case "http4s":
		handled := false
		// AuthMiddleware(authUser) — capture the authenticator function name.
		for _, m := range reHttp4sAuthMiddleware.FindAllStringSubmatchIndex(src, -1) {
			authFn := ""
			if m[2] >= 0 {
				authFn = src[m[2]:m[3]]
			}
			winEnd := min(m[1]+240, len(src))
			method := classifyScalaAuthMethod(src[max0(m[0]-120):winEnd])
			name := "auth:http4s:AuthMiddleware"
			if authFn != "" {
				name += ":" + authFn
			}
			ent := makeEntity(name, "SCOPE.Security", "auth_check", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "http4s", "auth_method", method, "authenticator", authFn,
				"construct", "AuthMiddleware", "provenance", "HTTP4S_AUTH_MIDDLEWARE")
			add(ent)
			handled = true
		}
		// AuthedRoutes.of[User, IO] { ... }
		for _, m := range reHttp4sAuthedRoutes.FindAllStringSubmatchIndex(src, -1) {
			ent := makeEntity("auth:http4s:AuthedRoutes", "SCOPE.Security", "auth_check", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "http4s", "auth_method", "authed-routes", "construct", "AuthedRoutes",
				"provenance", "HTTP4S_AUTHED_ROUTES")
			add(ent)
			handled = true
		}
		return handled

	case "play":
		handled := false
		for _, m := range rePlayAuthAction.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			base := src[m[4]:m[5]]
			winEnd := min(m[1]+400, len(src))
			method := classifyScalaAuthMethod(src[m[0]:winEnd])
			ent := makeEntity("auth:play:"+base+":"+name, "SCOPE.Security", "auth_check", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "play", "auth_method", method, "action_kind", base, "action_name", name,
				"provenance", "PLAY_AUTH_ACTION")
			add(ent)
			handled = true
		}
		return handled
	}
	return false
}

// ---------------------------------------------------------------------------
// Deep MIDDLEWARE extraction (flagship frameworks)
// ---------------------------------------------------------------------------

// extractDeepMiddleware handles http4s / akka-http / play with named middleware
// and composition order. Returns true if it definitively handled the framework.
func extractDeepMiddleware(framework, src string, file extractor.FileInput, add func(types.EntityRecord)) bool {
	switch framework {
	case "http4s":
		handled := false
		// Named built-in middlewares (CORS, GZip, Logger, ...).
		for _, m := range reHttp4sNamedMw.FindAllStringSubmatchIndex(src, -1) {
			mw := src[m[2]:m[3]]
			ent := makeEntity("middleware:http4s:"+mw, "SCOPE.Middleware", "middleware", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "http4s", "middleware_name", mw, "provenance", "HTTP4S_NAMED_MIDDLEWARE")
			add(ent)
			handled = true
		}
		// Composition order: mw1(mw2(routes)) — record outer→inner chain.
		for _, m := range reHttp4sCompose.FindAllStringSubmatchIndex(src, -1) {
			outer := src[m[2]:m[3]]
			inner := src[m[4]:m[5]]
			target := src[m[6]:m[7]]
			order := outer + ">" + inner + "(" + target + ")"
			ent := makeEntity("middleware:http4s:compose:"+order, "SCOPE.Middleware", "middleware", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "http4s", "composition_order", order,
				"outer", outer, "inner", inner, "provenance", "HTTP4S_MIDDLEWARE_COMPOSITION")
			add(ent)
			handled = true
		}
		return handled

	case "akka-http", "pekko-http":
		handled := false
		// Rejection / exception handlers.
		for _, m := range reAkkaHandleDir.FindAllStringSubmatchIndex(src, -1) {
			directive := src[m[2]:m[3]]
			handlerName := ""
			if m[4] >= 0 {
				handlerName = src[m[4]:m[5]]
			}
			name := "middleware:akka:" + directive
			if handlerName != "" {
				name += ":" + handlerName
			}
			ent := makeEntity(name, "SCOPE.Middleware", "middleware", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", framework, "middleware_name", directive, "handler", handlerName,
				"provenance", "AKKA_HTTP_HANDLE_DIRECTIVE")
			add(ent)
			handled = true
		}
		// Transform directives (mapRequest/mapResponse/encodeResponse/...).
		for _, m := range reAkkaTransformDir.FindAllStringSubmatchIndex(src, -1) {
			directive := src[m[2]:m[3]]
			ent := makeEntity("middleware:akka:"+directive, "SCOPE.Middleware", "middleware", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", framework, "middleware_name", directive, "provenance", "AKKA_HTTP_TRANSFORM_DIRECTIVE")
			add(ent)
			handled = true
		}
		// CORS directive.
		for _, m := range reAkkaCors.FindAllStringSubmatchIndex(src, -1) {
			ent := makeEntity("middleware:akka:cors", "SCOPE.Middleware", "middleware", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", framework, "middleware_name", "cors", "provenance", "AKKA_HTTP_CORS")
			add(ent)
			handled = true
		}
		return handled

	case "play":
		handled := false
		// Global filter chain via DefaultHttpFilters(a, b, c).
		for _, m := range rePlayDefaultFilters.FindAllStringSubmatchIndex(src, -1) {
			args := ""
			if m[2] >= 0 {
				args = src[m[2]:m[3]]
			}
			filters := reIdentList.FindAllString(args, -1)
			order := strings.Join(filters, ">")
			ent := makeEntity("middleware:play:filters:"+order, "SCOPE.Middleware", "middleware", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "play", "filter_chain", order, "chain_kind", "DefaultHttpFilters",
				"provenance", "PLAY_DEFAULT_HTTP_FILTERS")
			add(ent)
			handled = true
		}
		// Global filter chain via def filters = Seq(a, b, c).
		for _, m := range rePlayFiltersSeq.FindAllStringSubmatchIndex(src, -1) {
			args := src[m[2]:m[3]]
			filters := reIdentList.FindAllString(args, -1)
			order := strings.Join(filters, ">")
			ent := makeEntity("middleware:play:filterSeq:"+order, "SCOPE.Middleware", "middleware", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "play", "filter_chain", order, "chain_kind", "EssentialFilterSeq",
				"provenance", "PLAY_ESSENTIAL_FILTER_SEQ")
			add(ent)
			handled = true
		}
		// Individual custom filter definitions.
		for _, m := range rePlayFilterDef.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			base := src[m[4]:m[5]]
			ent := makeEntity("middleware:play:filterDef:"+name, "SCOPE.Middleware", "middleware", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "play", "middleware_name", name, "filter_base", base,
				"provenance", "PLAY_FILTER_DEFINITION")
			add(ent)
			handled = true
		}
		return handled
	}
	return false
}

func max0(a int) int {
	if a < 0 {
		return 0
	}
	return a
}

// detectScalaFramework returns the dominant framework based on imports/code patterns.
func detectScalaFramework(src string) string {
	switch {
	// tapir is endpoint-DSL: routes + DTOs live in `endpoint` values regardless
	// of the serving backend (it can run ON akka/pekko/http4s/netty). Detect it
	// FIRST so a tapir file backed by akka/pekko/http4s is labelled `tapir`, not
	// the backend, since the route/DTO shape is the tapir endpoint chain.
	case isTapirSource(src):
		return "tapir"
	// Apache Pekko is the Apache fork of Akka; same routing DSL, package
	// org.apache.pekko.* . Detect it as its OWN framework (distinct from akka)
	// so the registry can track Pekko coverage separately. Checked before the
	// akka branch because a pekko file never contains `akka.http`.
	case strings.Contains(src, "org.apache.pekko") || strings.Contains(src, "pekko.http") ||
		strings.Contains(src, "import org.apache.pekko"):
		return "pekko-http"
	case strings.Contains(src, "akka.http"):
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
