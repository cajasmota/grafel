package scala_test

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Framework extractor tests
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
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "http_route" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one http_route entity from akka-http")
	}
}

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
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "http_route" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one http_route entity from http4s")
	}
}

func TestFrameworksScalatraRoute(t *testing.T) {
	src := `
import org.scalatra._
class UserServlet extends ScalatraServlet {
  get("/users") { "all users" }
  post("/users") { "create user" }
}
`
	ents := extract(t, "custom_scala_frameworks", fi("UserServlet.scala", "scala", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "http_route" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one http_route entity from scalatra")
	}
}

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
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "http_route" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one http_route entity from cask")
	}
}

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
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "http_route" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one http_route entity from finatra")
	}
}

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
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "lagom_service_call" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one lagom_service_call entity")
	}
}

func TestFrameworksPlayRoute(t *testing.T) {
	src := `GET     /users                  controllers.UserController.list
POST    /users                  controllers.UserController.create
GET     /users/:id              controllers.UserController.get(id: Long)
`
	ents := extract(t, "custom_scala_frameworks", fi("routes", "scala", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "http_route" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one http_route entity from play routes file")
	}
}

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
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "http_route" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one http_route entity from zio-http")
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
