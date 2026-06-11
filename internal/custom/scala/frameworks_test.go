package scala_test

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Framework routing tests — specific path+method assertions.
//
// Each test asserts exact entity names the extractor actually produces for the
// given fixture.  A vacuous "≥1 http_route exists" check is NOT sufficient to
// prove the extractor correctly parses the DSL.
// ---------------------------------------------------------------------------

// containsRouteEntity checks that entities contain an http_route (or lagom_service_call)
// with the given exact Name.
func containsRouteEntity(ents []entitySummary, subtype, name string) bool {
	for _, e := range ents {
		if e.Subtype == subtype && e.Name == name {
			return true
		}
	}
	return false
}

// dumpEntities formats entities for diagnostic failure messages.
func dumpEntities(ents []entitySummary) string {
	out := ""
	for _, e := range ents {
		out += "\n  {Kind:" + e.Kind + " Subtype:" + e.Subtype + " Name:" + e.Name + "}"
	}
	return out
}

// ---------------------------------------------------------------------------
// Akka-HTTP
//
// Fixture: pathPrefix("api") { path("users") { get { ... } ~ post { ... } } }
//
// The extractor uses positional context (nearest preceding pathPrefix + path)
// to combine nested directives.  Both methods produce fully combined entities:
//   GET:/api/users
//   POST:/api/users
// ---------------------------------------------------------------------------

func TestFrameworksAkkaHttpRoute(t *testing.T) {
	src := `
import akka.http.scaladsl.server.Directives._
val route =
  pathPrefix("api") {
    path("users") {
      get { complete(users) } ~
      post { entity(as[User]) { u => complete(u) } }
    }
  }
`
	ents := extract(t, "custom_scala_frameworks", fi("UserRoutes.scala", "scala", src))
	got := dumpEntities(ents)
	if !containsRouteEntity(ents, "http_route", "GET:/api/users") {
		t.Errorf("expected http_route GET:/api/users from akka-http; got:%s", got)
	}
	if !containsRouteEntity(ents, "http_route", "POST:/api/users") {
		t.Errorf("expected http_route POST:/api/users from akka-http; got:%s", got)
	}
}

// ---------------------------------------------------------------------------
// http4s
//
// Fixture: HttpRoutes.of[IO] {
//   case GET  -> Root / "health" => Ok("ok")
//   case POST -> Root / "users"  => Ok("created")
// }
//
// The extractor parses method + Root path-segment chain, producing:
//   route:GET:/health
//   route:POST:/users
// ---------------------------------------------------------------------------

func TestFrameworksHttp4sRoute(t *testing.T) {
	src := `
import org.http4s._
import org.http4s.dsl.io._
val routes = HttpRoutes.of[IO] {
  case GET -> Root / "health" => Ok("ok")
  case POST -> Root / "users" => Ok("created")
}
`
	ents := extract(t, "custom_scala_frameworks", fi("Routes.scala", "scala", src))
	got := dumpEntities(ents)
	if !containsRouteEntity(ents, "http_route", "route:GET:/health") {
		t.Errorf("expected http_route route:GET:/health from http4s; got:%s", got)
	}
	if !containsRouteEntity(ents, "http_route", "route:POST:/users") {
		t.Errorf("expected http_route route:POST:/users from http4s; got:%s", got)
	}
}

// ---------------------------------------------------------------------------
// Scalatra
//
// Fixture: get("/users") { ... }  /  post("/users") { ... }
//
// The extractor produces method:path combined names:
//   get:/users
//   post:/users
// ---------------------------------------------------------------------------

func TestFrameworksScalatraRoute(t *testing.T) {
	src := `
import org.scalatra._
class UserServlet extends ScalatraServlet {
  get("/users") { "all users" }
  post("/users") { "create user" }
}
`
	ents := extract(t, "custom_scala_frameworks", fi("UserServlet.scala", "scala", src))
	got := dumpEntities(ents)
	if !containsRouteEntity(ents, "http_route", "get:/users") {
		t.Errorf("expected http_route get:/users from scalatra; got:%s", got)
	}
	if !containsRouteEntity(ents, "http_route", "post:/users") {
		t.Errorf("expected http_route post:/users from scalatra; got:%s", got)
	}
}

// ---------------------------------------------------------------------------
// Cask
//
// Fixture: @cask.get("/api/users") / @cask.post("/api/users") annotations
//
// The extractor reads method + path from annotations:
//   get:/api/users
//   post:/api/users
// ---------------------------------------------------------------------------

func TestFrameworksCaskRoute(t *testing.T) {
	src := `
import cask._
object Main extends cask.MainRoutes {
  @cask.get("/api/users")
  def getUsers() = "users"
  @cask.post("/api/users")
  def createUser(request: cask.Request) = "created"
}
`
	ents := extract(t, "custom_scala_frameworks", fi("Main.scala", "scala", src))
	got := dumpEntities(ents)
	if !containsRouteEntity(ents, "http_route", "get:/api/users") {
		t.Errorf("expected http_route get:/api/users from cask; got:%s", got)
	}
	if !containsRouteEntity(ents, "http_route", "post:/api/users") {
		t.Errorf("expected http_route post:/api/users from cask; got:%s", got)
	}
}

// ---------------------------------------------------------------------------
// Finatra
//
// Fixture: @Get("/api/users") / @Post("/api/users") Java-style annotations
//
// The extractor reads the annotation method + path (preserving original case):
//   Get:/api/users
//   Post:/api/users
// ---------------------------------------------------------------------------

func TestFrameworksFinatraRoute(t *testing.T) {
	src := `
import com.twitter.finatra.http._
class UserController extends HttpController {
  @Get("/api/users")
  def getUsers(request: Request): Response = ???
  @Post("/api/users")
  def createUser(request: UserRequest): Response = ???
}
`
	ents := extract(t, "custom_scala_frameworks", fi("UserController.scala", "scala", src))
	got := dumpEntities(ents)
	if !containsRouteEntity(ents, "http_route", "Get:/api/users") {
		t.Errorf("expected http_route Get:/api/users from finatra; got:%s", got)
	}
	if !containsRouteEntity(ents, "http_route", "Post:/api/users") {
		t.Errorf("expected http_route Post:/api/users from finatra; got:%s", got)
	}
}

// ---------------------------------------------------------------------------
// Lagom
//
// Fixture: pathCall("/api/users/:id", getUser _) / namedCall("/api/users", createUser _)
//
// The extractor produces lagom_service_call entities (subtype differs from http_route):
//   lagom:/api/users/:id
//   lagom:/api/users
// ---------------------------------------------------------------------------

func TestFrameworksLagomServiceCall(t *testing.T) {
	src := `
import com.lightbend.lagom.scaladsl.api._
trait UserService extends Service {
  def getUser(id: String): ServiceCall[NotUsed, User]
  override def descriptor = {
    import Service._
    named("user-service").withCalls(
      pathCall("/api/users/:id", getUser _),
      namedCall("/api/users", createUser _)
    )
  }
}
`
	ents := extract(t, "custom_scala_frameworks", fi("UserService.scala", "scala", src))
	got := dumpEntities(ents)
	if !containsRouteEntity(ents, "lagom_service_call", "lagom:/api/users/{id}") {
		t.Errorf("expected lagom_service_call lagom:/api/users/{id} from lagom; got:%s", got)
	}
	if !containsRouteEntity(ents, "lagom_service_call", "lagom:/api/users") {
		t.Errorf("expected lagom_service_call lagom:/api/users from lagom; got:%s", got)
	}
}

// ---------------------------------------------------------------------------
// Play
//
// Fixture: standard conf/routes file
//   GET  /users       controllers.UserController.list
//   POST /users       controllers.UserController.create
//   GET  /users/:id   controllers.UserController.get(id: Long)
//
// The extractor reads method:path pairs:
//   GET:/users
//   POST:/users
//   GET:/users/:id
// ---------------------------------------------------------------------------

func TestFrameworksPlayRoute(t *testing.T) {
	src := `GET     /users                  controllers.UserController.list
POST    /users                  controllers.UserController.create
GET     /users/:id              controllers.UserController.get(id: Long)
`
	ents := extract(t, "custom_scala_frameworks", fi("routes", "scala", src))
	got := dumpEntities(ents)
	if !containsRouteEntity(ents, "http_route", "GET:/users") {
		t.Errorf("expected http_route GET:/users from play; got:%s", got)
	}
	if !containsRouteEntity(ents, "http_route", "POST:/users") {
		t.Errorf("expected http_route POST:/users from play; got:%s", got)
	}
	if !containsRouteEntity(ents, "http_route", "GET:/users/{id}") {
		t.Errorf("expected http_route GET:/users/{id} from play; got:%s", got)
	}
}

// ---------------------------------------------------------------------------
// Observability tests (unchanged — not routing)
// ---------------------------------------------------------------------------

func TestFrameworksObservabilityLogging(t *testing.T) {
	src := `
import org.slf4j.LoggerFactory
class UserService {
  val logger = LoggerFactory.getLogger(classOf[UserService])
  def findUser(id: Long) = {
    logger.info(s"Finding user $id")
    logger.warn("User not found")
  }
}
`
	ents := extract(t, "custom_scala_frameworks", fi("UserService.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Observability", "UserService.scala") {
		// Look for any log entity
		found := false
		for _, e := range ents {
			if e.Kind == "SCOPE.Observability" && e.Subtype == "log_statement" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected log_statement entity")
		}
	}
}

func TestFrameworksObservabilityMetrics(t *testing.T) {
	src := `
import io.micrometer.core.instrument.{Counter, MeterRegistry}
class MetricsService(registry: MeterRegistry) {
  val requestCounter = Counter.builder("http.requests").register(registry)
}
`
	ents := extract(t, "custom_scala_frameworks", fi("Metrics.scala", "scala", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Observability" && e.Subtype == "metric" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected metric entity")
	}
}

// TestFrameworksObservabilityMetricNamesMicrometer is value-asserting: it proves
// the SPECIFIC literal metric name is captured for Micrometer builders and
// MeterRegistry call sites. This is what justifies metric_extraction = full.
func TestFrameworksObservabilityMetricNamesMicrometer(t *testing.T) {
	src := `
import io.micrometer.core.instrument.{Counter, Timer, MeterRegistry}
class MetricsService(registry: MeterRegistry) {
  val requestCounter = Counter.builder("http.server.requests").register(registry)
  val latencyTimer = Timer.builder("http.server.latency").register(registry)
  def hit(): Unit = registry.counter("cache.hits").increment()
}
`
	ents := extract(t, "custom_scala_frameworks", fi("Metrics.scala", "scala", src))
	want := map[string]string{
		"metric:http.server.requests": "builder",
		"metric:http.server.latency":  "builder",
		"metric:cache.hits":           "counter",
	}
	for name, instrument := range want {
		e := findEntity(ents, "SCOPE.Observability", name)
		if e == nil {
			t.Fatalf("expected metric entity %q", name)
		}
		if e.Subtype != "metric" {
			t.Errorf("%s: subtype = %q, want metric", name, e.Subtype)
		}
		gotName := strings.TrimPrefix(name, "metric:")
		if e.Props["metric_name"] != gotName {
			t.Errorf("%s: metric_name = %q, want %q", name, e.Props["metric_name"], gotName)
		}
		if e.Props["instrument"] != instrument {
			t.Errorf("%s: instrument = %q, want %q", name, e.Props["instrument"], instrument)
		}
		if e.Props["provenance"] != "SCALA_METRIC_NAMED" {
			t.Errorf("%s: provenance = %q, want SCALA_METRIC_NAMED", name, e.Props["provenance"])
		}
	}
}

// TestFrameworksObservabilityMetricNamesKamonDropwizard proves literal metric
// name capture for Kamon and Dropwizard instrument call sites.
func TestFrameworksObservabilityMetricNamesKamonDropwizard(t *testing.T) {
	src := `
import kamon.Kamon
class Svc(metrics: MetricRegistry) {
  val orders = Kamon.counter("orders.placed")
  val gauge = Kamon.gauge("queue.depth")
  val hist = Kamon.histogram("payload.size")
  val meter = metrics.meter("requests.rate")
}
`
	ents := extract(t, "custom_scala_frameworks", fi("Svc.scala", "scala", src))
	for _, name := range []string{
		"metric:orders.placed",
		"metric:queue.depth",
		"metric:payload.size",
		"metric:requests.rate",
	} {
		e := findEntity(ents, "SCOPE.Observability", name)
		if e == nil {
			t.Fatalf("expected metric entity %q", name)
		}
		if e.Props["metric_name"] != strings.TrimPrefix(name, "metric:") {
			t.Errorf("%s: metric_name = %q", name, e.Props["metric_name"])
		}
	}
}

// TestFrameworksObservabilityTraceNames is value-asserting: it proves the
// SPECIFIC literal span name is captured for Kamon, OpenTelemetry, and natchez
// span call sites. This is what justifies trace_extraction = full.
func TestFrameworksObservabilityTraceNames(t *testing.T) {
	src := `
import kamon.Kamon
class Svc(tracer: Tracer) {
  def a() = Kamon.span("place-order") { doWork() }
  def b() = tracer.spanBuilder("http.request").startSpan()
  def c[F[_]: Trace] = Trace[F].span("db.query")(run)
}
`
	ents := extract(t, "custom_scala_frameworks", fi("Svc.scala", "scala", src))
	for _, name := range []string{
		"span:place-order",
		"span:http.request",
		"span:db.query",
	} {
		e := findEntity(ents, "SCOPE.Observability", name)
		if e == nil {
			t.Fatalf("expected trace entity %q", name)
		}
		if e.Subtype != "trace" {
			t.Errorf("%s: subtype = %q, want trace", name, e.Subtype)
		}
		if e.Props["span_name"] != strings.TrimPrefix(name, "span:") {
			t.Errorf("%s: span_name = %q, want %q", name, e.Props["span_name"], strings.TrimPrefix(name, "span:"))
		}
		if e.Props["provenance"] != "SCALA_TRACE_NAMED" {
			t.Errorf("%s: provenance = %q, want SCALA_TRACE_NAMED", name, e.Props["provenance"])
		}
	}
}

// TestFrameworksObservabilityUnnamedFallback proves that metric/trace usage
// WITHOUT a literal name still yields a file-local fallback entity (honest
// partial: dynamic name not resolvable without cross-file dataflow).
func TestFrameworksObservabilityUnnamedFallback(t *testing.T) {
	src := `
class Svc(registry: MeterRegistry, name: String) {
  val c = registry.counter(name)
  def trace() = Kamon.span(dynamicName) { run() }
}
`
	ents := extract(t, "custom_scala_frameworks", fi("Svc.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Observability", "metrics:Svc.scala") {
		t.Error("expected file-local metric fallback entity")
	}
	if !containsEntity(ents, "SCOPE.Observability", "trace:Svc.scala") {
		t.Error("expected file-local trace fallback entity")
	}
}

func TestFrameworksTestingLinkage(t *testing.T) {
	src := `
import org.scalatest._
class UserServiceSpec extends AnyFlatSpec {
  "UserService" should "find a user by id" in {
    val service = new UserService(mockRepo)
    service.findById(1L) shouldBe Some(testUser)
  }
}
`
	ents := extract(t, "custom_scala_frameworks", fi("UserServiceSpec.scala", "scala", src))
	// #4360 — the per-file SCOPE.Test/test_suite orphan is no longer emitted
	// from this extractor. It carried no edges and had no consumer. Subject-aware
	// test→SUT TESTS edges now come solely from the deep testmap linker
	// (detectScalaTest), and route-hit e2e edges from tests_route_e2e.go.
	for _, e := range ents {
		if e.Kind == "SCOPE.Test" && e.Subtype == "test_suite" {
			t.Errorf("scaffolding orphan test_suite must not be emitted (#4360); got %q", e.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// ZIO-HTTP
//
// Fixture: Http.collect[Request] {
//   case Method.GET  -> Root / "users" => Response.text("users")
//   case Method.POST -> Root / "users" => Response.ok
// }
//
// The extractor parses method + Root path-segment chain, producing:
//   route:GET:/users
//   route:POST:/users
// ---------------------------------------------------------------------------

func TestFrameworksZioHttpRoute(t *testing.T) {
	src := `
import zio._
import zio.http._
val app = Http.collect[Request] {
  case Method.GET -> Root / "users" => Response.text("users")
  case Method.POST -> Root / "users" => Response.ok
}
`
	ents := extract(t, "custom_scala_frameworks", fi("App.scala", "scala", src))
	got := dumpEntities(ents)
	if !containsRouteEntity(ents, "http_route", "route:GET:/users") {
		t.Errorf("expected http_route route:GET:/users from zio-http; got:%s", got)
	}
	if !containsRouteEntity(ents, "http_route", "route:POST:/users") {
		t.Errorf("expected http_route route:POST:/users from zio-http; got:%s", got)
	}
}

// ===========================================================================
// Deep-grind path-parameter normalisation + prefix composition (#3452).
//
// Every Scala framework's native path-parameter syntax must normalise to the
// project-wide canonical `{name}` form so routes bucket identically with their
// cross-stack peers. These tests assert EXACT (verb, canonical-path) pairs.
// ===========================================================================

// akka-http: pathPrefix("api") { path("users" / LongNumber) { get/post } }
// PathMatcher LongNumber → {id}; prefix composed with segment.
func TestFrameworksAkkaHttpPathParam(t *testing.T) {
	src := `
import akka.http.scaladsl.server.Directives._
val route =
  pathPrefix("api") {
    path("users" / LongNumber) {
      get { complete(user) } ~
      delete { complete("ok") }
    }
  }
`
	ents := extract(t, "custom_scala_frameworks", fi("UserRoutes.scala", "scala", src))
	got := dumpEntities(ents)
	if !containsRouteEntity(ents, "http_route", "GET:/api/users/{id}") {
		t.Errorf("expected http_route GET:/api/users/{id} from akka-http LongNumber; got:%s", got)
	}
	if !containsRouteEntity(ents, "http_route", "DELETE:/api/users/{id}") {
		t.Errorf("expected http_route DELETE:/api/users/{id} from akka-http LongNumber; got:%s", got)
	}
}

// akka-http: path("users" / Segment / "posts") → /users/{segment}/posts.
func TestFrameworksAkkaHttpSegmentMatcher(t *testing.T) {
	src := `
import akka.http.scaladsl.server.Directives._
val route = path("users" / Segment / "posts") { get { complete("posts") } }
`
	ents := extract(t, "custom_scala_frameworks", fi("Routes.scala", "scala", src))
	got := dumpEntities(ents)
	if !containsRouteEntity(ents, "http_route", "GET:/users/{segment}/posts") {
		t.Errorf("expected http_route GET:/users/{segment}/posts from akka-http Segment; got:%s", got)
	}
}

// http4s: case GET -> Root / "users" / LongVar(id) => ... → /users/{id}.
func TestFrameworksHttp4sPathParam(t *testing.T) {
	src := `
import org.http4s._
import org.http4s.dsl.io._
val routes = HttpRoutes.of[IO] {
  case GET -> Root / "users" / LongVar(id) => Ok(user(id))
  case GET -> Root / "users" / id @ UUIDVar(_) => Ok(byUuid(id))
}
`
	ents := extract(t, "custom_scala_frameworks", fi("Routes.scala", "scala", src))
	got := dumpEntities(ents)
	if !containsRouteEntity(ents, "http_route", "route:GET:/users/{id}") {
		t.Errorf("expected http_route route:GET:/users/{id} from http4s LongVar; got:%s", got)
	}
}

// zio-http (collect DSL): case Method.GET -> Root / "users" / int("id") => ...
func TestFrameworksZioHttpPathParam(t *testing.T) {
	src := `
import zio._
import zio.http._
val app = Http.collect[Request] {
  case Method.GET -> Root / "users" / int("id") => Response.ok
}
`
	ents := extract(t, "custom_scala_frameworks", fi("App.scala", "scala", src))
	got := dumpEntities(ents)
	if !containsRouteEntity(ents, "http_route", "route:GET:/users/{id}") {
		t.Errorf("expected http_route route:GET:/users/{id} from zio-http int(); got:%s", got)
	}
}

// zio-http (Scala-3 Routes DSL): Method.GET / "users" / int("id") -> handler.
func TestFrameworksZioRoutesDSLPathParam(t *testing.T) {
	src := `
import zio.http._
val routes = Routes(
  Method.GET / "users" / int("id") -> handler { (id: Int, req: Request) => Response.ok },
  Method.POST / "users" -> handler(Response.ok)
)
`
	ents := extract(t, "custom_scala_frameworks", fi("Routes.scala", "scala", src))
	got := dumpEntities(ents)
	if !containsRouteEntity(ents, "http_route", "route:GET:/users/{id}") {
		t.Errorf("expected http_route route:GET:/users/{id} from zio Routes DSL; got:%s", got)
	}
	if !containsRouteEntity(ents, "http_route", "route:POST:/users") {
		t.Errorf("expected http_route route:POST:/users from zio Routes DSL; got:%s", got)
	}
}

// scalatra: get("/users/:id") → /users/{id}.
func TestFrameworksScalatraPathParam(t *testing.T) {
	src := `
import org.scalatra._
class UserServlet extends ScalatraServlet {
  get("/users/:id") { params("id") }
}
`
	ents := extract(t, "custom_scala_frameworks", fi("UserServlet.scala", "scala", src))
	got := dumpEntities(ents)
	if !containsRouteEntity(ents, "http_route", "get:/users/{id}") {
		t.Errorf("expected http_route get:/users/{id} from scalatra :id; got:%s", got)
	}
}

// cask: @cask.get("/users/:id") → /users/{id}.
func TestFrameworksCaskPathParam(t *testing.T) {
	src := `
import cask._
object Main extends cask.MainRoutes {
  @cask.get("/users/:id")
  def getUser(id: String) = id
}
`
	ents := extract(t, "custom_scala_frameworks", fi("Main.scala", "scala", src))
	got := dumpEntities(ents)
	if !containsRouteEntity(ents, "http_route", "get:/users/{id}") {
		t.Errorf("expected http_route get:/users/{id} from cask :id; got:%s", got)
	}
}

// finatra: @Get("/users/:id") → /users/{id}.
func TestFrameworksFinatraPathParam(t *testing.T) {
	src := `
import com.twitter.finatra.http._
class UserController extends HttpController {
  @Get("/users/:id")
  def getUser(request: Request): Response = ???
}
`
	ents := extract(t, "custom_scala_frameworks", fi("UserController.scala", "scala", src))
	got := dumpEntities(ents)
	if !containsRouteEntity(ents, "http_route", "Get:/users/{id}") {
		t.Errorf("expected http_route Get:/users/{id} from finatra :id; got:%s", got)
	}
}

// play: $id<regex> dollar-param form normalises to {id}.
func TestFrameworksPlayDollarParam(t *testing.T) {
	src := `GET     /users/$id<[0-9]+>      controllers.UserController.get(id: Long)
`
	ents := extract(t, "custom_scala_frameworks", fi("routes", "scala", src))
	got := dumpEntities(ents)
	if !containsRouteEntity(ents, "http_route", "GET:/users/{id}") {
		t.Errorf("expected http_route GET:/users/{id} from play $id<regex>; got:%s", got)
	}
}

func TestFrameworksDTOExtraction(t *testing.T) {
	src := `
import akka.http.scaladsl.server.Directives._
case class CreateUserRequest(name: String, email: String, age: Int)
case class UserResponse(id: Long, name: String)
`
	ents := extract(t, "custom_scala_frameworks", fi("Dto.scala", "scala", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Type" && e.Subtype == "dto" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected dto entity from case class")
	}
}

// ---------------------------------------------------------------------------
// dto_extraction — VALUE-ASSERTING: fields, types, Option nullability, circe
// codec attribution, and @JsonKey wire-name overrides (issue #3454).
// ---------------------------------------------------------------------------

func TestFrameworksDTOFieldsAndNullability(t *testing.T) {
	src := `
import io.circe.generic.semiauto._
case class CreateUserRequest(
  name: String,
  email: String,
  age: Int,
  nickname: Option[String]
)
object CreateUserRequest {
  implicit val dec = deriveDecoder[CreateUserRequest]
}
`
	ents := extract(t, "custom_scala_frameworks", fi("Dto.scala", "scala", src))
	got := dumpEntities(ents)

	// Parent DTO carries the full field shape, nullability, and codec.
	dto, ok := findBySubtype(ents, "dto", "CreateUserRequest")
	if !ok {
		t.Fatalf("expected dto CreateUserRequest; got:%s", got)
	}
	if dto.Props["field_count"] != "4" {
		t.Errorf("expected field_count=4, got %q", dto.Props["field_count"])
	}
	if dto.Props["nullable_fields"] != "nickname" {
		t.Errorf("expected nullable_fields=nickname, got %q", dto.Props["nullable_fields"])
	}
	if dto.Props["codec"] != "circe" {
		t.Errorf("expected codec=circe (deriveDecoder), got %q", dto.Props["codec"])
	}

	// A specific field entity records its exact type + nullability.
	nick, ok := findBySubtype(ents, "dto_field", "dto_field:CreateUserRequest.nickname")
	if !ok {
		t.Fatalf("expected dto_field for nickname; got:%s", got)
	}
	if nick.Props["field_type"] != "Option[String]" {
		t.Errorf("expected nickname field_type=Option[String], got %q", nick.Props["field_type"])
	}
	if nick.Props["nullable"] != "true" {
		t.Errorf("expected nickname nullable=true, got %q", nick.Props["nullable"])
	}

	age, ok := findBySubtype(ents, "dto_field", "dto_field:CreateUserRequest.age")
	if !ok {
		t.Fatalf("expected dto_field for age; got:%s", got)
	}
	if age.Props["field_type"] != "Int" {
		t.Errorf("expected age field_type=Int, got %q", age.Props["field_type"])
	}
	if age.Props["nullable"] != "false" {
		t.Errorf("expected age nullable=false, got %q", age.Props["nullable"])
	}
}

func TestFrameworksDTOWireNameAndPlayCodec(t *testing.T) {
	src := `
import play.api.libs.json._
case class Account(
  @JsonKey("user_name") userName: String,
  balance: Long
)
object Account { implicit val fmt = Json.format[Account] }
`
	ents := extract(t, "custom_scala_frameworks", fi("Account.scala", "scala", src))
	got := dumpEntities(ents)

	dto, ok := findBySubtype(ents, "dto", "Account")
	if !ok {
		t.Fatalf("expected dto Account; got:%s", got)
	}
	if dto.Props["codec"] != "play-json" {
		t.Errorf("expected codec=play-json (Json.format[Account]), got %q", dto.Props["codec"])
	}
	if dto.Props["wire_overrides"] != "userName=user_name" {
		t.Errorf("expected wire_overrides=userName=user_name, got %q", dto.Props["wire_overrides"])
	}

	field, ok := findBySubtype(ents, "dto_field", "dto_field:Account.userName")
	if !ok {
		t.Fatalf("expected dto_field for userName; got:%s", got)
	}
	if field.Props["wire_name"] != "user_name" {
		t.Errorf("expected userName wire_name=user_name, got %q", field.Props["wire_name"])
	}
}

// ---------------------------------------------------------------------------
// request_validation — VALUE-ASSERTING: refined, cats Validated, accord,
// octopus. Each asserts the SPECIFIC field + constraint (issue #3454).
// ---------------------------------------------------------------------------

func TestFrameworksValidationRefined(t *testing.T) {
	src := `
import eu.timepit.refined._
import eu.timepit.refined.string.MatchesRegex
case class SignupRequest(
  email: String Refined MatchesRegex["^.+@.+$"],
  age: Int Refined Positive,
  bio: Refined[String, NonEmpty]
)
`
	ents := extract(t, "custom_scala_frameworks", fi("Signup.scala", "scala", src))
	got := dumpEntities(ents)

	email, ok := findBySubtype(ents, "request_validation", "validate:refined:email")
	if !ok {
		t.Fatalf("expected refined validation for email; got:%s", got)
	}
	if !strings.HasPrefix(email.Props["constraint"], "MatchesRegex") {
		t.Errorf("expected email constraint MatchesRegex..., got %q", email.Props["constraint"])
	}

	age, ok := findBySubtype(ents, "request_validation", "validate:refined:age")
	if !ok {
		t.Fatalf("expected refined validation for age; got:%s", got)
	}
	if age.Props["constraint"] != "Positive" {
		t.Errorf("expected age constraint Positive, got %q", age.Props["constraint"])
	}

	bio, ok := findBySubtype(ents, "request_validation", "validate:refined:bio")
	if !ok {
		t.Fatalf("expected refined validation for bio; got:%s", got)
	}
	if bio.Props["constraint"] != "NonEmpty" {
		t.Errorf("expected bio constraint NonEmpty, got %q", bio.Props["constraint"])
	}
}

func TestFrameworksValidationAccord(t *testing.T) {
	src := `
import com.wix.accord._
import com.wix.accord.dsl._
implicit val userValidator = validator[User] { user =>
  user.name is notEmpty
  user.age should be >= 18
}
`
	ents := extract(t, "custom_scala_frameworks", fi("UserValidator.scala", "scala", src))
	got := dumpEntities(ents)

	name, ok := findBySubtype(ents, "request_validation", "validate:accord:name")
	if !ok {
		t.Fatalf("expected accord validation for name; got:%s", got)
	}
	if name.Props["constraint"] != "notEmpty" {
		t.Errorf("expected name constraint notEmpty, got %q", name.Props["constraint"])
	}
	if name.Props["dto"] != "User" {
		t.Errorf("expected accord dto=User, got %q", name.Props["dto"])
	}

	age, ok := findBySubtype(ents, "request_validation", "validate:accord:age")
	if !ok {
		t.Fatalf("expected accord validation for age; got:%s", got)
	}
	if !strings.Contains(age.Props["constraint"], ">= 18") {
		t.Errorf("expected age constraint to contain '>= 18', got %q", age.Props["constraint"])
	}
}

func TestFrameworksValidationCatsValidated(t *testing.T) {
	src := `
import cats.data.ValidatedNel
import cats.syntax.validated._
def validateEmail(s: String): ValidatedNel[String, String] = ???
def validateAge(a: Int): Validated[String, Int] = ???
`
	ents := extract(t, "custom_scala_frameworks", fi("Validators.scala", "scala", src))
	got := dumpEntities(ents)

	email, ok := findBySubtype(ents, "request_validation", "validate:cats-validated:email")
	if !ok {
		t.Fatalf("expected cats-validated validation for email; got:%s", got)
	}
	if email.Props["validator_fn"] != "validateEmail" {
		t.Errorf("expected validator_fn=validateEmail, got %q", email.Props["validator_fn"])
	}
	if _, ok := findBySubtype(ents, "request_validation", "validate:cats-validated:age"); !ok {
		t.Errorf("expected cats-validated validation for age; got:%s", got)
	}
}

func TestFrameworksNoMatchNonScala(t *testing.T) {
	src := `get("/users") { "all users" }`
	ents := extract(t, "custom_scala_frameworks", fi("app.rb", "ruby", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for ruby file, got %d", len(ents))
	}
}
