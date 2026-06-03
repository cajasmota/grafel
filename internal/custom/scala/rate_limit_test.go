package scala_test

import "testing"

// ---------------------------------------------------------------------------
// custom_scala_rate_limit — http4s Throttle + akka/pekko throttle stamping
// (#4105). Value-asserting: each test pins the SPECIFIC amount/per resolved on
// the SPECIFIC throttle idiom and the flat contract props
// (rate_limited / rate_limit / rate_limit_scope / rate_limit_source). A
// "≥1 entity" check is NEVER used. Negatives required.
// ---------------------------------------------------------------------------

const rlKey = "custom_scala_rate_limit"

// findRL returns the first rate_limit marker matching source (and optionally a
// substring of its name), with its props.
func findRLBySource(ents []entitySummary, source string) (entitySummary, bool) {
	for _, e := range ents {
		if e.Subtype == "rate_limit" && e.Props["rate_limit_source"] == source {
			return e, true
		}
	}
	return entitySummary{}, false
}

func TestScalaRateLimit_Http4sThrottle_Apply(t *testing.T) {
	src := `
import org.http4s._
import org.http4s.server.middleware.Throttle
import scala.concurrent.duration._

val routes = HttpRoutes.of[IO] {
  case GET -> Root / "users" => Ok("ok")
}
val throttled = Throttle(100, 1.minute)(routes.orNotFound)
`
	ents := extract(t, rlKey, fi("App.scala", "scala", src))
	e, ok := findRLBySource(ents, "http4s_throttle")
	if !ok {
		t.Fatalf("expected http4s_throttle marker; got: %+v", ents)
	}
	if e.Props["rate_limited"] != "true" {
		t.Errorf("rate_limited = %q, want true", e.Props["rate_limited"])
	}
	if e.Props["rate_limit"] != "100/60s" {
		t.Errorf("rate_limit = %q, want 100/60s", e.Props["rate_limit"])
	}
	if e.Props["limit"] != "100" {
		t.Errorf("limit = %q, want 100", e.Props["limit"])
	}
	if e.Props["period"] != "60" {
		t.Errorf("period = %q, want 60", e.Props["period"])
	}
	if e.Props["rate_limit_scope"] != "app" {
		t.Errorf("rate_limit_scope = %q, want app", e.Props["rate_limit_scope"])
	}
	if e.Props["framework"] != "http4s" {
		t.Errorf("framework = %q, want http4s", e.Props["framework"])
	}
}

func TestScalaRateLimit_Http4sThrottle_HttpApp(t *testing.T) {
	src := `
import org.http4s.server.middleware.Throttle
import scala.concurrent.duration._
val app = Throttle.httpApp[IO](50, 10.seconds)(myApp)
`
	ents := extract(t, rlKey, fi("App.scala", "scala", src))
	e, ok := findRLBySource(ents, "http4s_throttle")
	if !ok {
		t.Fatalf("expected http4s_throttle marker; got: %+v", ents)
	}
	if e.Props["rate_limit"] != "50/10s" {
		t.Errorf("rate_limit = %q, want 50/10s", e.Props["rate_limit"])
	}
	if e.Props["period"] != "10" {
		t.Errorf("period = %q, want 10", e.Props["period"])
	}
}

func TestScalaRateLimit_Http4sThrottle_SubSecondWindow(t *testing.T) {
	// 100.millis window: honest sub-second rate "200/100ms", no whole-second period.
	src := `
import org.http4s.server.middleware.Throttle
import scala.concurrent.duration._
val app = Throttle.httpRoutes(200, 100.millis)(routes)
`
	ents := extract(t, rlKey, fi("App.scala", "scala", src))
	e, ok := findRLBySource(ents, "http4s_throttle")
	if !ok {
		t.Fatalf("expected http4s_throttle marker; got: %+v", ents)
	}
	if e.Props["rate_limit"] != "200/100ms" {
		t.Errorf("rate_limit = %q, want 200/100ms", e.Props["rate_limit"])
	}
	if e.Props["limit"] != "200" {
		t.Errorf("limit = %q, want 200", e.Props["limit"])
	}
	if _, has := e.Props["period"]; has {
		t.Errorf("period should be omitted for a sub-second window; got %q", e.Props["period"])
	}
}

func TestScalaRateLimit_Http4sThrottle_HonestPartialNonLiteral(t *testing.T) {
	// amount + per come from config vals — rate_limited stamped, numeric rate omitted.
	src := `
import org.http4s.server.middleware.Throttle
val app = Throttle(cfg.maxReqs, cfg.window)(routes)
`
	ents := extract(t, rlKey, fi("App.scala", "scala", src))
	e, ok := findRLBySource(ents, "http4s_throttle")
	if !ok {
		t.Fatalf("expected http4s_throttle marker; got: %+v", ents)
	}
	if e.Props["rate_limited"] != "true" {
		t.Errorf("rate_limited = %q, want true", e.Props["rate_limited"])
	}
	if v, has := e.Props["rate_limit"]; has {
		t.Errorf("rate_limit should be omitted (non-literal); got %q", v)
	}
	if v, has := e.Props["limit"]; has {
		t.Errorf("limit should be omitted (non-literal amount); got %q", v)
	}
}

func TestScalaRateLimit_AkkaThrottle_Positional(t *testing.T) {
	src := `
import akka.http.scaladsl.server.Directives._
import akka.stream.scaladsl.Source
import scala.concurrent.duration._

val route =
  path("events") {
    get {
      complete(eventsSource.throttle(100, 1.second))
    }
  }
`
	ents := extract(t, rlKey, fi("Routes.scala", "scala", src))
	e, ok := findRLBySource(ents, "akka_throttle")
	if !ok {
		t.Fatalf("expected akka_throttle marker; got: %+v", ents)
	}
	if e.Props["rate_limited"] != "true" {
		t.Errorf("rate_limited = %q, want true", e.Props["rate_limited"])
	}
	if e.Props["rate_limit"] != "100/1s" {
		t.Errorf("rate_limit = %q, want 100/1s", e.Props["rate_limit"])
	}
	if e.Props["rate_limit_scope"] != "route" {
		t.Errorf("rate_limit_scope = %q, want route", e.Props["rate_limit_scope"])
	}
	if e.Props["framework"] != "akka-http" {
		t.Errorf("framework = %q, want akka-http", e.Props["framework"])
	}
}

func TestScalaRateLimit_PekkoThrottle_Named(t *testing.T) {
	src := `
import org.apache.pekko.http.scaladsl.server.Directives._
import org.apache.pekko.stream.scaladsl.Source
import scala.concurrent.duration._

val route =
  path("stream") {
    get {
      complete(src.throttle(elements = 5, per = 2.minutes, maximumBurst = 1, mode = ThrottleMode.Shaping))
    }
  }
`
	ents := extract(t, rlKey, fi("Routes.scala", "scala", src))
	e, ok := findRLBySource(ents, "pekko_throttle")
	if !ok {
		t.Fatalf("expected pekko_throttle marker; got: %+v", ents)
	}
	if e.Props["rate_limit"] != "5/120s" {
		t.Errorf("rate_limit = %q, want 5/120s", e.Props["rate_limit"])
	}
	if e.Props["period"] != "120" {
		t.Errorf("period = %q, want 120", e.Props["period"])
	}
	if e.Props["framework"] != "pekko-http" {
		t.Errorf("framework = %q, want pekko-http", e.Props["framework"])
	}
}

// --- NEGATIVES ------------------------------------------------------------

func TestScalaRateLimit_Negative_PlainRoute(t *testing.T) {
	src := `
import org.http4s._
val routes = HttpRoutes.of[IO] {
  case GET -> Root / "users" => Ok("ok")
}
`
	ents := extract(t, rlKey, fi("App.scala", "scala", src))
	for _, e := range ents {
		if e.Subtype == "rate_limit" {
			t.Errorf("plain route must not stamp a rate_limit marker; got %s", e.Name)
		}
	}
}

func TestScalaRateLimit_Negative_NonThrottleMiddleware(t *testing.T) {
	// CORS / GZip middleware are NOT throttles → no rate_limit stamping.
	src := `
import org.http4s.server.middleware.{CORS, GZip}
val app = CORS.policy(GZip(routes)).orNotFound
`
	ents := extract(t, rlKey, fi("App.scala", "scala", src))
	for _, e := range ents {
		if e.Subtype == "rate_limit" {
			t.Errorf("non-throttle middleware must not stamp rate_limit; got %s", e.Name)
		}
	}
}

func TestScalaRateLimit_Negative_NonHttp4sThrottle(t *testing.T) {
	// A `Throttle` symbol with no http4s signal in the file is not attributed.
	src := `
package mygame
class Throttle(amount: Int, per: Int)
val t = Throttle(10, 5)
`
	ents := extract(t, rlKey, fi("Game.scala", "scala", src))
	for _, e := range ents {
		if e.Subtype == "rate_limit" {
			t.Errorf("non-http4s Throttle must not stamp rate_limit; got %s", e.Name)
		}
	}
}

func TestScalaRateLimit_Negative_WrongLanguage(t *testing.T) {
	src := `Throttle(100, 1.minute)(routes)`
	ents := extract(t, rlKey, fi("App.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("kotlin file must yield no scala rate_limit entities; got %+v", ents)
	}
}
