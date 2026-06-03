// Value-asserting parity fixtures for the pekko-http trailing-sibling Substrate
// cells that the #3990 wave missed: constant_propagation, env_fallback_recognition,
// import_resolution_quality, and schema_drift_detection. (parity-grind-scala,
// epic #3872.)
//
// The four Scala substrate passes exercised here all register on the "scala"
// language slug and gate ONLY on file content — they are framework-agnostic and
// fire on any .scala source dispatched via LanguageForPath:
//
//	constant_propagation / env_fallback_recognition / import_resolution_quality
//	    -> sniffScala            (internal/substrate/scala.go)
//	schema_drift_detection
//	    -> sniffPayloadShapesScala (internal/substrate/payload_shapes_scala.go)
//
// pekko-http is org.apache.pekko.* Scala source (language=scala), so it receives
// the identical substrate treatment the other nine scala frameworks already
// carry. Each test drives the real sniffer on a real pekko-http idiom and
// asserts the SPECIFIC artifact (exact resolved literal, exact env-var name,
// exact import source, exact payload field set) — never len>0.
package substrate

import (
	"reflect"
	"testing"
)

// TestScalaPekko_ConstantPropagation proves the Scala constant sniffer resolves
// a top-level string-literal binding to its EXACT value on a pekko-http config
// object — the constant_propagation capability.
func TestScalaPekko_ConstantPropagation(t *testing.T) {
	const src = `package com.acme.pekkoapp
import org.apache.pekko.http.scaladsl.server.Directives._

object PekkoConfig {
  val API_BASE = "https://api.acme.test/v1"
  final val SERVICE_NAME: String = "user-service"
}
`
	by := bindMap(sniffScala(src))
	if got := by["API_BASE"]; got.Value != "https://api.acme.test/v1" || got.Provenance != ProvenanceLiteral {
		t.Errorf("API_BASE: want value=https://api.acme.test/v1 provenance=literal; got %+v", got)
	}
	if got := by["SERVICE_NAME"]; got.Value != "user-service" || got.Provenance != ProvenanceLiteral {
		t.Errorf("SERVICE_NAME: want value=user-service provenance=literal; got %+v", got)
	}
}

// TestScalaPekko_EnvFallbackRecognition proves the sniffer captures the EXACT
// env-var name and default literal of a sys.env.getOrElse fallback in a
// pekko-http server config — the env_fallback_recognition capability.
func TestScalaPekko_EnvFallbackRecognition(t *testing.T) {
	const src = `package com.acme.pekkoapp
import org.apache.pekko.actor.ActorSystem

object PekkoServer {
  val PORT = sys.env.getOrElse("PEKKO_HTTP_PORT", "8558")
}
`
	by := bindMap(sniffScala(src))
	got := by["PORT"]
	if got.EnvVar != "PEKKO_HTTP_PORT" {
		t.Errorf("PORT env-var: want PEKKO_HTTP_PORT; got %q (%+v)", got.EnvVar, got)
	}
	if got.Value != "8558" {
		t.Errorf("PORT default: want 8558; got %q", got.Value)
	}
	if got.Provenance != ProvenanceEnvFallback {
		t.Errorf("PORT provenance: want env_fallback; got %q", got.Provenance)
	}
}

// TestScalaPekko_ImportResolutionQuality proves the sniffer resolves plain,
// braced, and rebinding imports to their EXACT import source on a pekko-http
// directives file — the import_resolution_quality capability.
func TestScalaPekko_ImportResolutionQuality(t *testing.T) {
	const src = `package com.acme.pekkoapp
import org.apache.pekko.http.scaladsl.Http
import org.apache.pekko.http.scaladsl.server.{Directives, Route => PekkoRoute}
`
	by := bindMap(sniffScala(src))
	if got := by["Http"]; got.ImportSource != "org.apache.pekko.http.scaladsl" || got.Provenance != ProvenanceCrossFile {
		t.Errorf("Http import: want source=org.apache.pekko.http.scaladsl provenance=cross_file; got %+v", got)
	}
	if got := by["Directives"]; got.ImportSource != "org.apache.pekko.http.scaladsl.server.Directives" {
		t.Errorf("Directives braced import: want source ...server.Directives; got %+v", got)
	}
	// Rebinding: `Route => PekkoRoute` binds local PekkoRoute to remote ...server.Route.
	if got := by["PekkoRoute"]; got.ImportSource != "org.apache.pekko.http.scaladsl.server.Route" {
		t.Errorf("PekkoRoute rebinding import: want source ...server.Route; got %+v", got)
	}
}

// TestScalaPekko_SchemaDriftDetection proves the payload-shape sniffer (the
// same pass that powers payload_drift / schema-drift comparison) extracts the
// EXACT producer request and response field sets from a pekko-http handler —
// the schema_drift_detection capability (drift is computed by comparing these
// producer/consumer shapes).
func TestScalaPekko_SchemaDriftDetection(t *testing.T) {
	const src = `package com.acme.pekkoapp
case class CreateUser(name: String, email: String, age: Option[Int])
object UserRoutes {
  def create(req: Request): Future[Response] = {
    val dto = req.as[CreateUser]
    complete(Json.obj("id" -> 1, "status" -> "created"))
  }
}
`
	shapes := sniffPayloadShapesScala(src)
	reqS := findShape(shapes, "create", PayloadDirectionRequest, PayloadSideProducer)
	if reqS == nil {
		t.Fatalf("expected pekko-http req.as[T] producer request shape; got %+v", shapes)
	}
	if got := sortedNames(reqS.Fields); !reflect.DeepEqual(got, []string{"age", "email", "name"}) {
		t.Errorf("pekko request fields: want [age email name]; got %v", got)
	}
	// age is Option[Int] — optionality is the drift-relevant signal.
	for _, f := range reqS.Fields {
		if f.Name == "age" && !f.Optional {
			t.Errorf("age should be Optional (drift-relevant nullability)")
		}
	}
	respS := findShape(shapes, "create", PayloadDirectionResponse, PayloadSideProducer)
	if respS == nil {
		t.Fatalf("expected pekko-http Json.obj producer response shape; got %+v", shapes)
	}
	if got := sortedNames(respS.Fields); !reflect.DeepEqual(got, []string{"id", "status"}) {
		t.Errorf("pekko response fields: want [id status]; got %v", got)
	}
}

// TestScalaPekko_TemplatePatternCatalog proves the Scala template-pattern
// sniffer (sniffTemplatePatternsScala, RegisterTemplatePatternSniffer("scala"))
// catalogues the EXACT i18n key, log literal/level, and SQL verb on a pekko-http
// handler — the template_pattern_catalog capability. Framework-agnostic, fires
// on any .scala file.
func TestScalaPekko_TemplatePatternCatalog(t *testing.T) {
	const src = `package com.acme.pekkoapp
object UserRoutes {
  def handle(): Unit = {
    val msg = Messages("user.created")
    logger.warn("user creation slow")
    val q = "SELECT id FROM users WHERE active = true"
  }
}
`
	pats := sniffTemplatePatternsScala(src)
	var i18n, log, sql *TemplatePattern
	for i := range pats {
		switch {
		case pats[i].Kind == TemplateKindI18n:
			i18n = &pats[i]
		case pats[i].Kind == TemplateKindLog && pats[i].Tag == "logger.warn":
			log = &pats[i]
		case pats[i].Kind == TemplateKindSQL:
			sql = &pats[i]
		}
	}
	if i18n == nil || i18n.Literal != "user.created" || i18n.Tag != "Messages" {
		t.Errorf("i18n: want literal=user.created tag=Messages; got %+v", i18n)
	}
	if log == nil || log.Literal != "user creation slow" {
		t.Errorf("log: want literal='user creation slow' tag=logger.warn; got %+v", log)
	}
	if sql == nil || sql.Literal != "SELECT id FROM users WHERE active = true" || sql.Tag != "sql.literal" {
		t.Errorf("sql: want the exact SELECT literal tag=sql.literal; got %+v", sql)
	}
}

// ===========================================================================
// sttp / tapir / caliban trailing-sibling substrate parity.
//
// The same four framework-agnostic Scala sniffers (sniffScala,
// sniffPayloadShapesScala, sniffTemplatePatternsScala) fire on these
// frameworks' real .scala idioms exactly as they do for pekko-http. Each test
// asserts the SPECIFIC resolved artifact on the framework's own source.
// ===========================================================================

// --- sttp (an HTTP client) --------------------------------------------------

func TestScalaSttp_ConstantAndEnvAndImport(t *testing.T) {
	const src = `package com.acme.sttpclient
import sttp.client3.{SttpBackend, basicRequest}
object ApiClient {
  val BASE_URL = "https://upstream.acme.test"
  val TIMEOUT = sys.env.getOrElse("STTP_TIMEOUT_MS", "5000")
}
`
	by := bindMap(sniffScala(src))
	if got := by["BASE_URL"]; got.Value != "https://upstream.acme.test" || got.Provenance != ProvenanceLiteral {
		t.Errorf("BASE_URL: %+v", got)
	}
	if got := by["TIMEOUT"]; got.EnvVar != "STTP_TIMEOUT_MS" || got.Value != "5000" || got.Provenance != ProvenanceEnvFallback {
		t.Errorf("TIMEOUT: %+v", got)
	}
	if got := by["SttpBackend"]; got.ImportSource != "sttp.client3.SttpBackend" {
		t.Errorf("SttpBackend braced import: %+v", got)
	}
}

func TestScalaSttp_SchemaDrift(t *testing.T) {
	const src = `package com.acme.sttpclient
object ApiClient {
  def createUser(): Unit = {
    basicRequest.post(uri"https://api/users").body(Json.obj("name" -> "x", "email" -> "y"))
  }
}
`
	shapes := sniffPayloadShapesScala(src)
	cs := findShape(shapes, "createUser", PayloadDirectionRequest, PayloadSideConsumer)
	if cs == nil {
		t.Fatalf("expected sttp consumer request shape; got %+v", shapes)
	}
	if got := sortedNames(cs.Fields); !reflect.DeepEqual(got, []string{"email", "name"}) {
		t.Errorf("sttp consumer fields: want [email name]; got %v", got)
	}
}

func TestScalaSttp_TemplatePattern(t *testing.T) {
	const src = `package com.acme.sttpclient
object ApiClient {
  def call(): Unit = {
    logger.error("sttp request failed")
  }
}
`
	pats := sniffTemplatePatternsScala(src)
	var found bool
	for _, p := range pats {
		if p.Kind == TemplateKindLog && p.Tag == "logger.error" && p.Literal == "sttp request failed" {
			found = true
		}
	}
	if !found {
		t.Errorf("sttp: expected log template literal 'sttp request failed' tag logger.error; got %+v", pats)
	}
}

// --- tapir (endpoint DSL) ---------------------------------------------------

func TestScalaTapir_ConstantAndEnvAndImport(t *testing.T) {
	const src = `package com.acme.tapirapp
import sttp.tapir.{endpoint, PublicEndpoint}
object Endpoints {
  val API_VERSION = "v2"
  val HOST = sys.env.getOrElse("TAPIR_HOST", "0.0.0.0")
}
`
	by := bindMap(sniffScala(src))
	if got := by["API_VERSION"]; got.Value != "v2" || got.Provenance != ProvenanceLiteral {
		t.Errorf("API_VERSION: %+v", got)
	}
	if got := by["HOST"]; got.EnvVar != "TAPIR_HOST" || got.Value != "0.0.0.0" {
		t.Errorf("HOST: %+v", got)
	}
	if got := by["endpoint"]; got.ImportSource != "sttp.tapir.endpoint" {
		t.Errorf("endpoint braced import: %+v", got)
	}
}

func TestScalaTapir_SchemaDrift(t *testing.T) {
	const src = `package com.acme.tapirapp
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
		t.Fatalf("expected tapir producer request shape; got %+v", shapes)
	}
	if got := sortedNames(reqS.Fields); !reflect.DeepEqual(got, []string{"note", "qty", "sku"}) {
		t.Errorf("tapir request fields: want [note qty sku]; got %v", got)
	}
	respS := findShape(shapes, "place", PayloadDirectionResponse, PayloadSideProducer)
	if respS == nil {
		t.Fatalf("expected tapir producer response shape; got %+v", shapes)
	}
	if got := sortedNames(respS.Fields); !reflect.DeepEqual(got, []string{"orderId", "status"}) {
		t.Errorf("tapir response fields: want [orderId status]; got %v", got)
	}
}

func TestScalaTapir_TemplatePattern(t *testing.T) {
	const src = `package com.acme.tapirapp
object Logic {
  def run(): Unit = {
    val q = "SELECT * FROM orders WHERE sku = ?"
  }
}
`
	pats := sniffTemplatePatternsScala(src)
	var found bool
	for _, p := range pats {
		if p.Kind == TemplateKindSQL && p.Literal == "SELECT * FROM orders WHERE sku = ?" {
			found = true
		}
	}
	if !found {
		t.Errorf("tapir: expected SQL template literal; got %+v", pats)
	}
}

// --- caliban (GraphQL) ------------------------------------------------------

func TestScalaCaliban_ConstantAndEnv(t *testing.T) {
	const src = `package com.acme.gql
import caliban.GraphQL
object Resolvers {
  val SCHEMA_NAME = "acme-graph"
  val PORT = sys.env.getOrElse("CALIBAN_PORT", "8088")
}
`
	by := bindMap(sniffScala(src))
	if got := by["SCHEMA_NAME"]; got.Value != "acme-graph" || got.Provenance != ProvenanceLiteral {
		t.Errorf("SCHEMA_NAME: %+v", got)
	}
	if got := by["PORT"]; got.EnvVar != "CALIBAN_PORT" || got.Value != "8088" {
		t.Errorf("PORT: %+v", got)
	}
	if got := by["GraphQL"]; got.ImportSource != "caliban" {
		t.Errorf("GraphQL import: %+v", got)
	}
}

func TestScalaCaliban_SchemaDrift(t *testing.T) {
	const src = `package com.acme.gql
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
		t.Fatalf("expected caliban Json.obj producer response shape; got %+v", shapes)
	}
	if got := sortedNames(resp.Fields); !reflect.DeepEqual(got, []string{"id", "name"}) {
		t.Errorf("caliban response fields: want [id name]; got %v", got)
	}
}

func TestScalaCaliban_TemplatePattern(t *testing.T) {
	const src = `package com.acme.gql
object Q {
  def resolve(): Unit = {
    val m = Messages("gql.user.notfound")
  }
}
`
	pats := sniffTemplatePatternsScala(src)
	var found bool
	for _, p := range pats {
		if p.Kind == TemplateKindI18n && p.Literal == "gql.user.notfound" && p.Tag == "Messages" {
			found = true
		}
	}
	if !found {
		t.Errorf("caliban: expected i18n template literal gql.user.notfound; got %+v", pats)
	}
}
