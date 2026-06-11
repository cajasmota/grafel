package scala

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

func runScalaRouteE2E(t *testing.T, path, src string) (string, string) {
	t.Helper()
	e := &scalaTestRouteE2EExtractor{}
	ents, err := e.Extract(context.Background(), extractor.FileInput{
		Path: path, Language: "scala", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(ents) == 0 {
		return "", ""
	}
	if ents[0].Subtype != "test_suite" {
		t.Fatalf("expected test_suite, got %q", ents[0].Subtype)
	}
	return ents[0].Properties["e2e_route_calls"], ents[0].Properties["framework"]
}

func TestScalaRouteE2E_PlayFakeRequest(t *testing.T) {
	src := `package controllers
import play.api.test._
class UserControllerSpec extends PlaySpec {
  "create" should "work" in {
    val req = FakeRequest(POST, "/api/v1/users")
    route(app, FakeRequest(GET, "/api/v1/users/123")).get
  }
}`
	got, fw := runScalaRouteE2E(t, "test/controllers/UserControllerSpec.scala", src)
	if !strings.Contains(got, "POST /api/v1/users") || !strings.Contains(got, "GET /api/v1/users/123") {
		t.Fatalf("Play routes not captured: %q", got)
	}
	if fw != "play" {
		t.Fatalf("framework=%q, want play", fw)
	}
}

func TestScalaRouteE2E_AkkaHTTPRouteTest(t *testing.T) {
	src := `package routes
import akka.http.scaladsl.testkit.ScalatestRouteTest
class UserRoutesSpec extends AnyWordSpec with ScalatestRouteTest {
  "the route" should {
    "return users" in {
      Get("/api/v1/users") ~> route ~> check { ok }
    }
    "create a user" in {
      Post("/api/v1/users", entity) ~> Route.seal(route) ~> check { ok }
    }
  }
}`
	got, fw := runScalaRouteE2E(t, "src/test/scala/routes/UserRoutesSpec.scala", src)
	if !strings.Contains(got, "GET /api/v1/users") || !strings.Contains(got, "POST /api/v1/users") {
		t.Fatalf("Akka HTTP routes not captured: %q", got)
	}
	if fw != "akka-http" {
		t.Fatalf("framework=%q, want akka-http", fw)
	}
}

func TestScalaRouteE2E_Http4sClient(t *testing.T) {
	src := `package routes
import org.http4s._
class UserRoutesSuite extends munit.FunSuite {
  test("GET users") {
    val req = Request[IO](method = Method.GET, uri = uri"/api/v1/users")
    client.run(req)
  }
  test("POST user") {
    POST(body, uri"/api/v1/users")
  }
}`
	got, _ := runScalaRouteE2E(t, "src/test/scala/routes/UserRoutesSuite.scala", src)
	if !strings.Contains(got, "GET /api/v1/users") || !strings.Contains(got, "POST /api/v1/users") {
		t.Fatalf("http4s routes not captured: %q", got)
	}
}

// Negative: a non-test production controller file is not turned into a route
// test suite even if it mentions a FakeRequest-like token.
func TestScalaRouteE2E_NonTestFileSkipped(t *testing.T) {
	src := `package controllers
class UserController {
  def list() = Action { Get("/api/v1/users") ~> route }
}`
	got, _ := runScalaRouteE2E(t, "app/controllers/UserController.scala", src)
	if got != "" {
		t.Fatalf("production controller should not yield route calls, got %q", got)
	}
}

// Negative: a bare outbound client `Get("/x")` with no `~>` route-test sink is
// not captured as an Akka route test (avoids crediting outbound HTTP clients).
func TestScalaRouteE2E_OutboundClientNotCaptured(t *testing.T) {
	src := `package clients
class ApiClientSpec extends AnyFlatSpec {
  "client" should "fetch" in {
    val resp = Get("/external/data")
    resp.foreach(println)
  }
}`
	got, _ := runScalaRouteE2E(t, "src/test/scala/clients/ApiClientSpec.scala", src)
	if strings.Contains(got, "/external/data") {
		t.Fatalf("bare outbound Get without ~> should not be a route test, got %q", got)
	}
}
