package scala_test

import (
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
	if !containsRouteEntity(ents, "lagom_service_call", "lagom:/api/users/:id") {
		t.Errorf("expected lagom_service_call lagom:/api/users/:id from lagom; got:%s", got)
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
	if !containsRouteEntity(ents, "http_route", "GET:/users/:id") {
		t.Errorf("expected http_route GET:/users/:id from play; got:%s", got)
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
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Test" && e.Subtype == "test_suite" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected test_suite entity for ScalaTest")
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

func TestFrameworksNoMatchNonScala(t *testing.T) {
	src := `get("/users") { "all users" }`
	ents := extract(t, "custom_scala_frameworks", fi("app.rb", "ruby", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for ruby file, got %d", len(ents))
	}
}
