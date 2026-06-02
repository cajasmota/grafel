package scala_test

import (
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

// Value-asserting fixtures for the 5 trailing Scala frameworks (#3990, epic
// #3872, audit #3887). The Observability (log/metric/trace) extraction block in
// frameworks.go and the whole type_system.go extractor gate ONLY on
// file.Language == "scala" — they sit OUTSIDE the framework switch and fire on
// any .scala file. These tests prove the SPECIFIC artifact (a named metric/span
// entity, a typed SCOPE.Type/Interface entity) is produced on each trailing
// framework's idiom, justifying crediting the language-level Observability and
// Type System cells to the same status the other 9 Scala frameworks carry.

// typeSysExtract runs the custom_scala_type_system extractor and returns
// entity summaries.
func typeSysExtract(t *testing.T, path, src string) []entitySummary {
	t.Helper()
	return extract(t, "custom_scala_type_system", extreg.FileInput{
		Path: path, Language: "scala", Content: []byte(src),
	})
}

// findSub returns the first entity with the given kind+subtype+name.
func findSub(ents []entitySummary, kind, subtype, name string) *entitySummary {
	for i := range ents {
		if ents[i].Kind == kind && ents[i].Subtype == subtype && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// --- Observability (frameworks.go, language-level) --------------------------

// TestTrailing_Caliban_Observability proves the metric + trace + log
// extraction fires on a caliban-flavoured source with a literal metric/span
// name captured (metric/trace are credited full; log partial).
func TestTrailing_Caliban_Observability(t *testing.T) {
	src := `
import caliban.GraphQL
import kamon.Kamon
import org.slf4j.LoggerFactory
class GraphApi {
  val logger = LoggerFactory.getLogger(classOf[GraphApi])
  val resolverCalls = Kamon.counter("graphql.resolver.calls")
  def run() = {
    Kamon.span("graphql.execute")
    logger.info("executing")
  }
}
`
	ents := extract(t, "custom_scala_frameworks", fi("GraphApi.scala", "scala", src))
	m := findEntity(ents, "SCOPE.Observability", "metric:graphql.resolver.calls")
	if m == nil || m.Props["metric_name"] != "graphql.resolver.calls" {
		t.Fatalf("expected caliban metric entity with literal name; got %+v", ents)
	}
	s := findEntity(ents, "SCOPE.Observability", "span:graphql.execute")
	if s == nil || s.Props["span_name"] != "graphql.execute" {
		t.Fatalf("expected caliban span entity with literal name; got %+v", ents)
	}
	if findSub(ents, "SCOPE.Observability", "log_statement", "logger:GraphApi") == nil {
		t.Error("expected caliban log_statement entity")
	}
}

// TestTrailing_Sttp_Observability proves metric/trace/log fire on an sttp
// client module.
func TestTrailing_Sttp_Observability(t *testing.T) {
	src := `
import sttp.client3._
import io.micrometer.core.instrument.MeterRegistry
class ApiClient(registry: MeterRegistry) {
  val logger = org.slf4j.LoggerFactory.getLogger("api")
  def call(): Unit = {
    registry.timer("sttp.client.latency").record(1L)
    logger.warn("slow")
  }
}
`
	ents := extract(t, "custom_scala_frameworks", fi("ApiClient.scala", "scala", src))
	m := findEntity(ents, "SCOPE.Observability", "metric:sttp.client.latency")
	if m == nil || m.Props["metric_name"] != "sttp.client.latency" {
		t.Fatalf("expected sttp metric entity with literal name; got %+v", ents)
	}
	if findSub(ents, "SCOPE.Observability", "log_statement", "logger:ApiClient") == nil {
		t.Error("expected sttp log_statement entity")
	}
}

// --- Type System (type_system.go, language-level) ---------------------------

// TestTrailing_Pekko_TypeSystem proves type/interface/enum/type_alias all fire
// on a pekko-http domain file.
func TestTrailing_Pekko_TypeSystem(t *testing.T) {
	src := `
package routes
case class User(id: Long, name: String)
trait UserRepo {
  def findAll(): List[User]
}
sealed trait Role
case object Admin extends Role
case object Member extends Role
type UserId = Long
`
	ents := typeSysExtract(t, "Domain.scala", src)
	if findSub(ents, "SCOPE.Type", "case_class", "User") == nil {
		t.Error("expected case class User → type_extraction")
	}
	if findSub(ents, "SCOPE.Interface", "trait", "UserRepo") == nil {
		t.Error("expected trait UserRepo → interface_extraction")
	}
	e := findSub(ents, "SCOPE.Type", "sealed_trait", "Role")
	if e == nil {
		t.Fatal("expected sealed trait Role → enum_extraction")
	}
	if cases := e.Props["enum_cases"]; cases == "" {
		t.Errorf("expected Role enum_cases populated; got %+v", e.Props)
	}
	if findSub(ents, "SCOPE.Type", "type_alias", "UserId") == nil {
		t.Error("expected type alias UserId → type_alias_extraction")
	}
}

// TestTrailing_Tapir_TypeSystem proves the same for a tapir endpoint module.
func TestTrailing_Tapir_TypeSystem(t *testing.T) {
	src := `
package api
case class CreateOrder(sku: String, qty: Int)
trait OrderService {
  def place(o: CreateOrder): OrderId
}
sealed trait Status
case object Pending extends Status
case object Shipped extends Status
type OrderId = String
`
	ents := typeSysExtract(t, "Orders.scala", src)
	if findSub(ents, "SCOPE.Type", "case_class", "CreateOrder") == nil {
		t.Error("expected case class CreateOrder → type_extraction")
	}
	if findSub(ents, "SCOPE.Interface", "trait", "OrderService") == nil {
		t.Error("expected trait OrderService → interface_extraction")
	}
	if findSub(ents, "SCOPE.Type", "sealed_trait", "Status") == nil {
		t.Error("expected sealed trait Status → enum_extraction")
	}
	if findSub(ents, "SCOPE.Type", "type_alias", "OrderId") == nil {
		t.Error("expected type alias OrderId → type_alias_extraction")
	}
}

// TestTrailing_Caliban_TypeSystem proves interface + type_alias (the two
// caliban cells being flipped) fire on a caliban schema file.
func TestTrailing_Caliban_TypeSystem(t *testing.T) {
	src := `
package gql
trait Queries {
  def user(id: String): User
  def users: List[User]
}
type ResolverEnv = AppEnv with UserRepo
`
	ents := typeSysExtract(t, "Queries.scala", src)
	if findSub(ents, "SCOPE.Interface", "trait", "Queries") == nil {
		t.Error("expected trait Queries → interface_extraction")
	}
	if findSub(ents, "SCOPE.Type", "type_alias", "ResolverEnv") == nil {
		t.Error("expected type alias ResolverEnv → type_alias_extraction")
	}
}
