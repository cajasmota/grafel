package scala_test

import (
	"testing"
)

// ---------------------------------------------------------------------------
// tapir — endpoint-DSL routing + DTO extraction (#3507).
//
// Value-asserting: each test pins the exact http_route name (verb + canonical
// path) and the request/response/error DTO type names + handler the extractor
// produces from the endpoint chain. A "≥1 route exists" check is NOT used.
// ---------------------------------------------------------------------------

func TestTapirEndpointRouteAndDTOs(t *testing.T) {
	src := `
import sttp.tapir._
import sttp.tapir.json.circe._

val getUser =
  endpoint
    .get
    .in("users" / path[Long]("id"))
    .in(query[String]("q"))
    .out(jsonBody[User])
    .errorOut(jsonBody[ErrorInfo])
    .serverLogic(handleGetUser)
`
	ents := extract(t, "custom_scala_frameworks", fi("UserEndpoints.scala", "scala", src))
	got := dumpEntities(ents)

	e, ok := findBySubtype(ents, "http_route", "tapir:GET:/users/{id}")
	if !ok {
		t.Fatalf("expected http_route tapir:GET:/users/{id}; got:%s", got)
	}
	if e.Props["http_method"] != "GET" {
		t.Errorf("http_method = %q, want GET", e.Props["http_method"])
	}
	if e.Props["http_path"] != "/users/{id}" {
		t.Errorf("http_path = %q, want /users/{id}", e.Props["http_path"])
	}
	if e.Props["request_dto"] != "" {
		// request body came from .in(jsonBody[...]) only; this endpoint has none.
		t.Errorf("request_dto = %q, want empty (no .in(jsonBody))", e.Props["request_dto"])
	}
	if e.Props["response_dto"] != "User" {
		t.Errorf("response_dto = %q, want User", e.Props["response_dto"])
	}
	if e.Props["error_dto"] != "ErrorInfo" {
		t.Errorf("error_dto = %q, want ErrorInfo", e.Props["error_dto"])
	}
	if e.Props["query_params"] != "q" {
		t.Errorf("query_params = %q, want q", e.Props["query_params"])
	}
	if e.Props["handler"] != "handleGetUser" {
		t.Errorf("handler = %q, want handleGetUser", e.Props["handler"])
	}

	// DTO ref entities for response + error.
	if _, ok := findBySubtype(ents, "dto_ref", "tapir_dto:response:User:tapir:GET:/users/{id}"); !ok {
		t.Errorf("expected response dto_ref for User; got:%s", got)
	}
	if _, ok := findBySubtype(ents, "dto_ref", "tapir_dto:error:ErrorInfo:tapir:GET:/users/{id}"); !ok {
		t.Errorf("expected error dto_ref for ErrorInfo; got:%s", got)
	}
}

func TestTapirPostRequestBodyDTO(t *testing.T) {
	src := `
import sttp.tapir._

val createUser =
  endpoint
    .post
    .in("users")
    .in(jsonBody[CreateUserRequest])
    .out(jsonBody[User])
    .serverLogicSuccess(createUserHandler)
`
	ents := extract(t, "custom_scala_frameworks", fi("Create.scala", "scala", src))
	got := dumpEntities(ents)

	e, ok := findBySubtype(ents, "http_route", "tapir:POST:/users")
	if !ok {
		t.Fatalf("expected http_route tapir:POST:/users; got:%s", got)
	}
	if e.Props["request_dto"] != "CreateUserRequest" {
		t.Errorf("request_dto = %q, want CreateUserRequest", e.Props["request_dto"])
	}
	if e.Props["response_dto"] != "User" {
		t.Errorf("response_dto = %q, want User", e.Props["response_dto"])
	}
	if e.Props["handler"] != "createUserHandler" {
		t.Errorf("handler = %q, want createUserHandler", e.Props["handler"])
	}
	if _, ok := findBySubtype(ents, "dto_ref", "tapir_dto:request:CreateUserRequest:tapir:POST:/users"); !ok {
		t.Errorf("expected request dto_ref for CreateUserRequest; got:%s", got)
	}
}

func TestTapirMethodExplicitForm(t *testing.T) {
	src := `
import sttp.tapir._
val delUser = endpoint.method(Method.DELETE).in("users" / path[Long]("id"))
`
	ents := extract(t, "custom_scala_frameworks", fi("Del.scala", "scala", src))
	got := dumpEntities(ents)
	if _, ok := findBySubtype(ents, "http_route", "tapir:DELETE:/users/{id}"); !ok {
		t.Errorf("expected http_route tapir:DELETE:/users/{id}; got:%s", got)
	}
}

func TestTapirNamedPathParam(t *testing.T) {
	src := `
import sttp.tapir._
val getBook = endpoint.get.in("books" / path[String]("isbn")).out(jsonBody[Book])
`
	ents := extract(t, "custom_scala_frameworks", fi("Book.scala", "scala", src))
	got := dumpEntities(ents)
	if _, ok := findBySubtype(ents, "http_route", "tapir:GET:/books/{isbn}"); !ok {
		t.Errorf("expected http_route tapir:GET:/books/{isbn}; got:%s", got)
	}
}

// A non-tapir Scala file must NOT produce tapir routes (no-op signal gate).
func TestTapirNoSignalNoOp(t *testing.T) {
	src := `
import org.http4s._
val routes = HttpRoutes.of[IO] { case GET -> Root / "ping" => Ok("pong") }
`
	ents := extract(t, "custom_scala_frameworks", fi("Http4s.scala", "scala", src))
	for _, e := range ents {
		if e.Props["framework"] == "tapir" {
			t.Errorf("expected no tapir entities for an http4s file; got %s", dumpEntities(ents))
		}
	}
}

// ---------------------------------------------------------------------------
// Apache Pekko (pekko-http) — Apache fork of akka-http. Same routing DSL,
// package org.apache.pekko.*. Must be detected as its own framework and the
// akka path canonicaliser reused, producing combined verb+path routes.
// ---------------------------------------------------------------------------

func TestPekkoHttpRoute(t *testing.T) {
	src := `
import org.apache.pekko.http.scaladsl.server.Directives._
val route =
  pathPrefix("api") {
    path("users" / LongNumber) { id =>
      get { complete(users) } ~
      post { entity(as[User]) { u => complete(u) } }
    }
  }
`
	ents := extract(t, "custom_scala_frameworks", fi("UserRoutes.scala", "scala", src))
	got := dumpEntities(ents)

	e, ok := findBySubtype(ents, "http_route", "GET:/api/users/{id}")
	if !ok {
		t.Fatalf("expected http_route GET:/api/users/{id} from pekko-http; got:%s", got)
	}
	if e.Props["framework"] != "pekko-http" {
		t.Errorf("framework = %q, want pekko-http", e.Props["framework"])
	}
	if _, ok := findBySubtype(ents, "http_route", "POST:/api/users/{id}"); !ok {
		t.Errorf("expected http_route POST:/api/users/{id} from pekko-http; got:%s", got)
	}
}

func TestPekkoHttpAuthDirective(t *testing.T) {
	src := `
import org.apache.pekko.http.scaladsl.server.Directives._
val route =
  authenticateBasic(realm = "secure", myAuth) { user =>
    path("admin") { get { complete("ok") } }
  }
`
	ents := extract(t, "custom_scala_frameworks", fi("Admin.scala", "scala", src))
	got := dumpEntities(ents)
	found := false
	for _, e := range ents {
		if e.Subtype == "auth_check" && e.Props["framework"] == "pekko-http" &&
			e.Props["auth_method"] == "basic" && e.Props["realm"] == "secure" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected pekko-http basic auth_check with realm=secure; got:%s", got)
	}
}
