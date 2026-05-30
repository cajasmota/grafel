package scala_test

import "testing"

// ---------------------------------------------------------------------------
// Caliban (Scala GraphQL)
// ---------------------------------------------------------------------------

func TestCalibanResolverFields(t *testing.T) {
	src := `
import caliban._
import caliban.schema.Schema

case class UserArgs(id: String)

case class Queries(
  user: UserArgs => URIO[Any, User],
  users: () => List[User],
)

case class Mutations(
  createUser: NewUser => Task[User],
)

object Api {
  val api = graphQL(RootResolver(Queries(resolveUser, resolveUsers), Mutations(resolveCreate)))
}
`
	ents := extract(t, "custom_scala_caliban", fi("Api.scala", "scala", src))

	// Each resolver case-class field becomes an addressable GRAPHQL endpoint,
	// rooted by the positional RootResolver argument (Queries=Query,
	// Mutations=Mutation).
	for _, want := range []string{
		"GRAPHQL /graphql/Queries/user",
		"GRAPHQL /graphql/Queries/users",
		"GRAPHQL /graphql/Mutations/createUser",
	} {
		e := findEntity(ents, "SCOPE.Operation", want)
		if e == nil {
			t.Fatalf("expected resolver endpoint %q", want)
		}
		if e.Props["verb"] != "GRAPHQL" {
			t.Errorf("%s: verb = %q, want GRAPHQL", want, e.Props["verb"])
		}
		if e.Props["framework"] != "caliban" {
			t.Errorf("%s: framework = %q, want caliban", want, e.Props["framework"])
		}
	}

	// Query-root field carries the right operation kind + handler.
	if e := findEntity(ents, "SCOPE.Operation", "GRAPHQL /graphql/Queries/user"); e != nil {
		if e.Props["graphql_operation"] != "Query" {
			t.Errorf("user: graphql_operation = %q, want Query", e.Props["graphql_operation"])
		}
		if e.Props["graphql_field"] != "user" {
			t.Errorf("user: graphql_field = %q, want user", e.Props["graphql_field"])
		}
		if e.Props["handler_name"] != "Queries.user" {
			t.Errorf("user: handler_name = %q, want Queries.user", e.Props["handler_name"])
		}
	}

	// Mutation-root field carries the Mutation operation kind.
	if e := findEntity(ents, "SCOPE.Operation", "GRAPHQL /graphql/Mutations/createUser"); e != nil {
		if e.Props["graphql_operation"] != "Mutation" {
			t.Errorf("createUser: graphql_operation = %q, want Mutation", e.Props["graphql_operation"])
		}
	}

	// A schema-root entity captures the wired roots positionally.
	if e := findEntity(ents, "SCOPE.Service", "graphql_schema:Queries,Mutations"); e == nil {
		t.Fatalf("expected graphql_schema root entity for Queries,Mutations")
	} else {
		if e.Props["query_root"] != "Queries" {
			t.Errorf("schema: query_root = %q, want Queries", e.Props["query_root"])
		}
		if e.Props["mutation_root"] != "Mutations" {
			t.Errorf("schema: mutation_root = %q, want Mutations", e.Props["mutation_root"])
		}
	}
}

func TestCalibanSchemaAdapter(t *testing.T) {
	src := `
import caliban._
import caliban.interop.http4s.Http4sAdapter

case class Queries(users: () => List[User])

object Server {
  val api = graphQL(RootResolver(Queries(resolveUsers)))
  val routes = http4sAdapter.makeHttpService(interpreter)
}
`
	ents := extract(t, "custom_scala_caliban", fi("Server.scala", "scala", src))

	e := findEntity(ents, "SCOPE.Service", "graphql_schema:Queries")
	if e == nil {
		t.Fatalf("expected graphql_schema:Queries entity")
	}
	if e.Props["http_adapter"] != "http4sAdapter" {
		t.Errorf("schema: http_adapter = %q, want http4sAdapter", e.Props["http_adapter"])
	}
}

func TestCalibanDTOs(t *testing.T) {
	src := `
import caliban.schema.Annotations._
import caliban.schema.Schema

@GQLDescription("A registered user")
case class User(id: String, name: String)

@GQLInputName("NewUserInput")
case class NewUser(name: String)

@GQLName("Role")
enum Role:
  case Admin, Member

object Schemas {
  implicit val userSchema = Schema.gen[Any, User]
}
`
	ents := extract(t, "custom_scala_caliban", fi("Schemas.scala", "scala", src))

	// Annotated case classes become schema DTOs.
	if e := findEntity(ents, "SCOPE.Schema", "graphql_dto:User"); e == nil {
		t.Fatalf("expected graphql_dto:User")
	} else if e.Props["graphql_dto_role"] != "object" {
		t.Errorf("User: graphql_dto_role = %q, want object", e.Props["graphql_dto_role"])
	}

	if e := findEntity(ents, "SCOPE.Schema", "graphql_dto:NewUser"); e == nil {
		t.Fatalf("expected graphql_dto:NewUser")
	}

	// An annotated enum becomes an enum-role DTO.
	if e := findEntity(ents, "SCOPE.Schema", "graphql_dto:Role"); e == nil {
		t.Fatalf("expected graphql_dto:Role")
	} else if e.Props["graphql_dto_role"] != "enum" {
		t.Errorf("Role: graphql_dto_role = %q, want enum", e.Props["graphql_dto_role"])
	}
}

func TestCalibanNoFalsePositive(t *testing.T) {
	// A plain http4s/tapir Scala file with no Caliban markers must yield nothing.
	src := `
import org.http4s._
import org.http4s.dsl.io._

object Routes {
  val routes = HttpRoutes.of[IO] {
    case GET -> Root / "users" => Ok("users")
  }
}
`
	ents := extract(t, "custom_scala_caliban", fi("Routes.scala", "scala", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities for non-Caliban file, got %d", len(ents))
	}
}
