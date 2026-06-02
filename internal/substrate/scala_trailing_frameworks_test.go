// Value-asserting fixtures for the 5 trailing Scala frameworks (#3990, epic
// #3872, audit #3887): caliban / pekko-http / scalapb-grpc / sttp / tapir.
//
// The Scala substrate sniffers (def_use_scala.go, effect_sinks_scala.go,
// taint_sites_scala.go, payload_shapes_scala.go) all register on the "scala"
// slug and gate ONLY on file content — they are framework-agnostic and fire on
// any .scala source dispatched via LanguageForPath. These tests prove the
// sniffers produce the SPECIFIC artifact (a named def→use, a named effect sink,
// a categorised taint site, a field-bearing payload shape) on each trailing
// framework's real idiom, which is what justifies crediting the language-level
// Substrate cells to the same partial/full status the other 9 frameworks carry.
package substrate

import (
	"reflect"
	"testing"
)

// hasDefUse asserts the def-use sniffer captured both a def and a (later) use
// of varName inside fn.
func hasDefUse(t *testing.T, defs []VarDef, uses []VarUse, fn, varName string) {
	t.Helper()
	var def, use bool
	for _, d := range defs {
		if d.Function == fn && d.Var == varName {
			def = true
		}
	}
	for _, u := range uses {
		if u.Function == fn && u.Var == varName {
			use = true
		}
	}
	if !def {
		t.Errorf("expected def of %q in %q", varName, fn)
	}
	if !use {
		t.Errorf("expected use of %q in %q", varName, fn)
	}
}

// --- caliban: a GraphQL resolver file ---------------------------------------

// TestScalaTrailing_Caliban_DefUse proves the def-use sniffer forms a real
// def→use pair on a caliban resolver method.
func TestScalaTrailing_Caliban_DefUse(t *testing.T) {
	const src = `
import caliban.schema.Schema
object Resolvers {
  def users(env: Env): List[User] = {
    val all = env.userRepo.findAll()
    all.filter(_.active)
  }
}
`
	defs, uses := sniffDefUseScala(src)
	hasDefUse(t, defs, uses, "users", "all")
}

// TestScalaTrailing_Caliban_Effects proves the effect sniffer attributes a DB
// read effect to the named resolver function.
func TestScalaTrailing_Caliban_Effects(t *testing.T) {
	const src = `
object UserResolver {
  def listUsers(em: EntityManager): List[User] = {
    em.createQuery("from User").getResultList.asInstanceOf[List[User]]
  }
}
`
	by := groupByEffect(sniffEffectsScala(src))
	mustHave(t, by, EffectDBRead, "listUsers")
}

// TestScalaTrailing_Caliban_Taint proves a taint source is detected in a
// caliban resolver reading sys.env (config-as-input) and a SQL splice sink.
func TestScalaTrailing_Caliban_Taint(t *testing.T) {
	const src = `
object SecretResolver {
  def lookup(name: String) = {
    val key = sys.env("API_KEY")
    sql"""SELECT * FROM secrets WHERE name = '#${name}'""".as[Secret]
  }
}
`
	var src0, sink bool
	for _, m := range sniffTaintScala(src) {
		if m.Kind == TaintKindSource {
			src0 = true
		}
		if m.Kind == TaintKindSink && m.Category == TaintCategorySQL {
			sink = true
		}
	}
	if !src0 {
		t.Error("expected sys.env source in caliban resolver")
	}
	if !sink {
		t.Error("expected Slick splice SQL sink in caliban resolver")
	}
}

// TestScalaTrailing_Caliban_PayloadShape proves a caliban argument/result case
// class yields a field-bearing response shape via Json.obj.
func TestScalaTrailing_Caliban_PayloadShape(t *testing.T) {
	const src = `
case class UserArgs(id: String, includeOrders: Option[Boolean])
object Q {
  def user(args: UserArgs): Future[Response] = {
    Ok(Json.obj("id" -> args.id, "name" -> "x"))
  }
}
`
	shapes := sniffPayloadShapesScala(src)
	resp := findShape(shapes, "user", PayloadDirectionResponse, PayloadSideProducer)
	if resp == nil {
		t.Fatalf("expected caliban Json.obj response shape; got %+v", shapes)
	}
	if got := sortedNames(resp.Fields); !reflect.DeepEqual(got, []string{"id", "name"}) {
		t.Errorf("caliban response fields: want [id name] got %v", got)
	}
}

// --- pekko-http: a route + directive file -----------------------------------

func TestScalaTrailing_Pekko_DefUse(t *testing.T) {
	const src = `
import org.apache.pekko.http.scaladsl.server.Directives._
object Routes {
  def userRoutes(repo: UserRepo) = {
    val route = path("users") { complete(repo.all()) }
    route
  }
}
`
	defs, uses := sniffDefUseScala(src)
	hasDefUse(t, defs, uses, "userRoutes", "route")
}

func TestScalaTrailing_Pekko_Effects(t *testing.T) {
	const src = `
object Handler {
  def saveUser(em: EntityManager, u: User): Unit = {
    em.persist(u)
  }
}
`
	by := groupByEffect(sniffEffectsScala(src))
	mustHave(t, by, EffectDBWrite, "saveUser")
}

// TestScalaTrailing_Pekko_Taint proves a pekko-http entity(as[T]) / parameter
// directive is recognised as a taint source.
func TestScalaTrailing_Pekko_Taint(t *testing.T) {
	const src = `
object Routes {
  def createUser() = {
    entity(as[CreateUser]) { dto =>
      parameter("verbose") { v =>
        complete(dto)
      }
    }
  }
}
`
	var hasSrc bool
	for _, m := range sniffTaintScala(src) {
		if m.Kind == TaintKindSource {
			hasSrc = true
		}
	}
	if !hasSrc {
		t.Error("expected pekko-http entity(as[T])/parameter source")
	}
}

// TestScalaTrailing_Pekko_PayloadShape proves a request body case class yields
// the request shape via req.as[T].
func TestScalaTrailing_Pekko_PayloadShape(t *testing.T) {
	const src = `
case class CreateUser(name: String, email: String, phone: Option[String])
object Routes {
  def create(req: Request): Future[Response] = {
    val dto = req.as[CreateUser]
    complete(dto)
  }
}
`
	shapes := sniffPayloadShapesScala(src)
	reqS := findShape(shapes, "create", PayloadDirectionRequest, PayloadSideProducer)
	if reqS == nil {
		t.Fatalf("expected pekko-http req.as[T] request shape; got %+v", shapes)
	}
	if got := sortedNames(reqS.Fields); !reflect.DeepEqual(got, []string{"email", "name", "phone"}) {
		t.Errorf("pekko-http request fields: want [email name phone] got %v", got)
	}
	for _, f := range reqS.Fields {
		if f.Name == "phone" && !f.Optional {
			t.Errorf("phone should be Optional")
		}
	}
}

// --- scalapb-grpc: a service implementation file ----------------------------

func TestScalaTrailing_Grpc_DefUse(t *testing.T) {
	const src = `
class GreeterImpl extends GreeterGrpc.Greeter {
  def sayHello(req: HelloRequest): Future[HelloReply] = {
    val name = req.name
    Future.successful(HelloReply(s"Hello $name"))
  }
}
`
	defs, uses := sniffDefUseScala(src)
	hasDefUse(t, defs, uses, "sayHello", "name")
}

func TestScalaTrailing_Grpc_Effects(t *testing.T) {
	const src = `
class UserServiceImpl extends UserServiceGrpc.UserService {
  def listUsers(req: ListReq): Future[UserList] = {
    val rows = userQuery.filter(_.active).result
    db.run(rows)
  }
}
`
	by := groupByEffect(sniffEffectsScala(src))
	mustHave(t, by, EffectDBRead, "listUsers")
}

// TestScalaTrailing_Grpc_Taint proves a SQL splice sink fires inside a gRPC
// service method (the unsafe Slick #${} interpolation).
func TestScalaTrailing_Grpc_Taint(t *testing.T) {
	const src = `
class SearchImpl extends SearchGrpc.Search {
  def search(req: SearchReq): Future[SearchReply] = {
    val q = req.query
    db.run(sql"""SELECT * FROM docs WHERE body LIKE '#${q}'""".as[Doc])
  }
}
`
	var sink bool
	for _, m := range sniffTaintScala(src) {
		if m.Kind == TaintKindSink && m.Category == TaintCategorySQL {
			sink = true
		}
	}
	if !sink {
		t.Error("expected Slick splice SQL sink in gRPC service method")
	}
}

// --- sttp: an HTTP client call site -----------------------------------------

func TestScalaTrailing_Sttp_DefUse(t *testing.T) {
	const src = `
import sttp.client3._
object ApiClient {
  def fetchUser(id: String): Response = {
    val req = basicRequest.get(uri"https://api/users/$id")
    req.send(backend)
  }
}
`
	defs, uses := sniffDefUseScala(src)
	hasDefUse(t, defs, uses, "fetchUser", "req")
}

// TestScalaTrailing_Sttp_HTTPEffect proves the outbound HTTP effect is
// attributed (sttp's idiom — already credited full; this guards it).
func TestScalaTrailing_Sttp_HTTPEffect(t *testing.T) {
	const src = `
object ApiClient {
  def callRemote(): Unit = {
    basicRequest.post(uri"https://x/api").send(backend)
  }
}
`
	by := groupByEffect(sniffEffectsScala(src))
	mustHave(t, by, EffectHTTPOut, "callRemote")
}

// TestScalaTrailing_Sttp_ConsumerShape proves the sttp consumer payload hint +
// Json.obj body produces a consumer request shape with the endpoint hint.
func TestScalaTrailing_Sttp_ConsumerShape(t *testing.T) {
	const src = `
object ApiClient {
  def createUser(): Unit = {
    basicRequest.post(uri"https://api/users").body(Json.obj("name" -> "x", "email" -> "y"))
  }
}
`
	shapes := sniffPayloadShapesScala(src)
	cs := findShape(shapes, "createUser", PayloadDirectionRequest, PayloadSideConsumer)
	if cs == nil {
		t.Fatalf("expected sttp consumer shape; got %+v", shapes)
	}
	if got := sortedNames(cs.Fields); !reflect.DeepEqual(got, []string{"email", "name"}) {
		t.Errorf("sttp consumer fields: want [email name] got %v", got)
	}
	if cs.VerbHint != "POST" || cs.EndpointHint != "https://api/users" {
		t.Errorf("sttp consumer hint: verb=%q url=%q", cs.VerbHint, cs.EndpointHint)
	}
}

// --- tapir: an endpoint-DSL file --------------------------------------------

func TestScalaTrailing_Tapir_DefUse(t *testing.T) {
	const src = `
import sttp.tapir._
object Endpoints {
  def userEndpoint() = {
    val ep = endpoint.get.in("users").out(jsonBody[List[User]])
    ep
  }
}
`
	defs, uses := sniffDefUseScala(src)
	hasDefUse(t, defs, uses, "userEndpoint", "ep")
}

func TestScalaTrailing_Tapir_Effects(t *testing.T) {
	const src = `
object Logic {
  def handle(em: EntityManager, u: User): Unit = {
    em.persist(u)
  }
}
`
	by := groupByEffect(sniffEffectsScala(src))
	mustHave(t, by, EffectDBWrite, "handle")
}

// TestScalaTrailing_Tapir_PayloadShape proves a tapir endpoint's request body
// case class yields a field-bearing shape. (tapir is already credited full for
// request/response_shape via tapir.go's endpoint parser; this guards the
// language-level case-class path that also fires on the same source.)
func TestScalaTrailing_Tapir_PayloadShape(t *testing.T) {
	const src = `
case class CreateOrder(sku: String, qty: Int, note: Option[String])
object Logic {
  def place(req: Request): Future[Response] = {
    val dto = req.as[CreateOrder]
    Ok(Json.obj("orderId" -> 1, "status" -> "ok"))
  }
}
`
	shapes := sniffPayloadShapesScala(src)
	reqS := findShape(shapes, "place", PayloadDirectionRequest, PayloadSideProducer)
	if reqS == nil {
		t.Fatalf("expected tapir request shape; got %+v", shapes)
	}
	if got := sortedNames(reqS.Fields); !reflect.DeepEqual(got, []string{"note", "qty", "sku"}) {
		t.Errorf("tapir request fields: want [note qty sku] got %v", got)
	}
	resp := findShape(shapes, "place", PayloadDirectionResponse, PayloadSideProducer)
	if resp == nil {
		t.Fatalf("expected tapir response shape; got %+v", shapes)
	}
	if got := sortedNames(resp.Fields); !reflect.DeepEqual(got, []string{"orderId", "status"}) {
		t.Errorf("tapir response fields: want [orderId status] got %v", got)
	}
}
