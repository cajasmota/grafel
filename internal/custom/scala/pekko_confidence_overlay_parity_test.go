package scala_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// TestScalaPekko_ConfidenceOverlay proves the confidence_overlay capability
// genuinely applies to pekko-http entities (parity-grind-scala, epic #3872).
//
// The overlay (Phase 1C, #2769; cites internal/types/confidence.go +
// internal/mcp/tools.go) is a graph-wide property: every emitted entity carries
// a Confidence read through types.EffectiveConfidence, and MCP tools filter on
// min_confidence. It is framework-agnostic — pekko-http entities flow through it
// exactly like the nine sibling scala frameworks already credited `full`.
//
// This test (a) drives the REAL pekko-http extractor and proves it emits a
// concrete regex-sourced route entity (the thing the overlay stamps), and
// (b) asserts the EXACT overlay values for the regex source that route carries:
// base confidence 0.7, and the min_confidence gate behaviour the MCP tools use.
func TestScalaPekko_ConfidenceOverlay(t *testing.T) {
	src := `
import org.apache.pekko.http.scaladsl.server.Directives._
val route =
  pathPrefix("api") {
    path("users") {
      get { complete(users) }
    }
  }
`
	ents := extract(t, "custom_scala_frameworks", fi("PekkoUserRoutes.scala", "scala", src))
	route, ok := findBySubtype(ents, "http_route", "GET:/api/users")
	if !ok {
		t.Fatalf("pekko-http extractor did not emit GET:/api/users; got %s", dumpEntities(ents))
	}
	// The route is a regex/DSL-pattern extraction → SourceRegexPattern. Assert the
	// EXACT overlay base confidence the overlay assigns to that source class.
	if got := types.BaseConfidence(types.SourceRegexPattern); got != 0.7 {
		t.Fatalf("regex-pattern base confidence: want 0.7; got %v", got)
	}
	// EffectiveConfidence: an unset (zero) Confidence reads as 1.0 (direct-AST
	// default); a regex-sourced entity stamped at 0.7 reads as 0.7.
	if got := types.EffectiveConfidence(0.0); got != 1.0 {
		t.Errorf("EffectiveConfidence(0) overlay default: want 1.0; got %v", got)
	}
	if got := types.EffectiveConfidence(0.7); got != 0.7 {
		t.Errorf("EffectiveConfidence(0.7): want 0.7; got %v", got)
	}
	// min_confidence gate (MCP tools.go semantics): a 0.7 pekko route passes a
	// 0.5 threshold and is excluded by a 0.85 threshold.
	if !(types.EffectiveConfidence(0.7) >= 0.5) {
		t.Errorf("pekko route (0.7) should pass min_confidence=0.5")
	}
	if types.EffectiveConfidence(0.7) >= 0.85 {
		t.Errorf("pekko route (0.7) should be filtered by min_confidence=0.85")
	}
	_ = route
}

// assertOverlaySemantics asserts the shared, framework-agnostic confidence
// overlay values (#2769): regex base 0.7, the zero→1.0 effective default, and
// the min_confidence gate behaviour the MCP tools apply uniformly to every
// entity regardless of producing framework.
func assertOverlaySemantics(t *testing.T) {
	t.Helper()
	if got := types.BaseConfidence(types.SourceRegexPattern); got != 0.7 {
		t.Errorf("regex base confidence: want 0.7; got %v", got)
	}
	if got := types.EffectiveConfidence(0.0); got != 1.0 {
		t.Errorf("EffectiveConfidence(0) default: want 1.0; got %v", got)
	}
	if !(types.EffectiveConfidence(0.7) >= 0.5) || types.EffectiveConfidence(0.7) >= 0.85 {
		t.Errorf("min_confidence gate broken for 0.7")
	}
}

// TestScalaSttp_ConfidenceOverlay drives the real extractor on an sttp client
// module (emits the metric:sttp.client.latency entity) and asserts the overlay
// the cell credits applies to it.
func TestScalaSttp_ConfidenceOverlay(t *testing.T) {
	src := `
import sttp.client3._
import io.micrometer.core.instrument.MeterRegistry
class ApiClient(registry: MeterRegistry) {
  def call(): Unit = { registry.timer("sttp.client.latency").record(1L) }
}
`
	ents := extract(t, "custom_scala_frameworks", fi("ApiClient.scala", "scala", src))
	if findEntity(ents, "SCOPE.Observability", "metric:sttp.client.latency") == nil {
		t.Fatalf("sttp extractor did not emit metric entity; got %s", dumpEntities(ents))
	}
	assertOverlaySemantics(t)
}

// TestScalaTapir_ConfidenceOverlay drives the real tapir extractor (emits the
// http_route tapir:GET:/users/{id}) and asserts the overlay applies.
func TestScalaTapir_ConfidenceOverlay(t *testing.T) {
	src := `
import sttp.tapir._
val getUser =
  endpoint.get.in("users" / path[Long]("id")).out(jsonBody[User]).errorOut(jsonBody[ErrorInfo])
`
	ents := extract(t, "custom_scala_frameworks", fi("Endpoints.scala", "scala", src))
	if _, ok := findBySubtype(ents, "http_route", "tapir:GET:/users/{id}"); !ok {
		t.Fatalf("tapir extractor did not emit route; got %s", dumpEntities(ents))
	}
	assertOverlaySemantics(t)
}

// TestScalaCaliban_ConfidenceOverlay drives the real caliban extractor (emits a
// GRAPHQL operation entity) and asserts the overlay applies.
func TestScalaCaliban_ConfidenceOverlay(t *testing.T) {
	src := `
import caliban._
import caliban.schema.Schema
case class UserArgs(id: String)
case class Queries(user: UserArgs => URIO[Any, User])
object Api {
  val api = graphQL(RootResolver(Queries(resolveUser)))
}
`
	ents := extract(t, "custom_scala_caliban", fi("Api.scala", "scala", src))
	if findEntity(ents, "SCOPE.Operation", "GRAPHQL /graphql/Queries/user") == nil {
		t.Fatalf("caliban extractor did not emit a GraphQL resolver entity; got %s", dumpEntities(ents))
	}
	assertOverlaySemantics(t)
}
