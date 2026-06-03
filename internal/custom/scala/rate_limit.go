// rate_limit.go — endpoint rate-limit / throttle stamping for Scala web
// frameworks (#4105, child of #3628 Middleware/rate_limit_stamping). Scala
// greenfield: prior to this pass every scala framework cell for
// rate_limit_stamping was `missing` — an http4s `Throttle(...)`-guarded app or
// an akka/pekko `.throttle(...)`-guarded route carried no rate-limit signal.
//
// Scala greenfield sibling of the Kotlin pass
// (internal/custom/kotlin/rate_limit_endpoint.go, #4095), the Ruby pass
// (internal/custom/ruby/rate_limit_endpoint.go, #4074) and the Elixir pass
// (internal/custom/elixir/rate_limit.go, #4099). It stamps the SAME flat
// property contract the other languages use so the graph answers "which
// surfaces are throttled and at what rate?":
//
//	rate_limited      — "true" when a throttle applies.
//	rate_limit        — human rate "100/60s" when statically resolvable from a
//	                    literal amount + a literal FiniteDuration window;
//	                    OMITTED (honest-partial) when amount/per is
//	                    config-/expression-driven.
//	rate_limit_scope  — "app"   (http4s Throttle wraps a whole HttpApp/HttpRoutes,
//	                    not a named route — the binding is app-wide, so claiming a
//	                    route would be dishonest)
//	                    | "route" (akka/pekko `.throttle(...)` is applied inside a
//	                    route/flow guarding the response stream for that route).
//	rate_limit_source — the recognised idiom (`http4s_throttle`, `akka_throttle`,
//	                    `pekko_throttle`).
//	limit / period    — the resolved numeric cap + window-seconds (when literal).
//
// Two recognised Scala surfaces:
//
//	http4s Throttle middleware —
//	    `Throttle(amount, per)(httpApp)`              (http4s ≤0.21 apply)
//	    `Throttle.httpApp(amount, per)(app)`          (http4s 0.23+ httpApp)
//	    `Throttle.httpRoutes(amount, per)(routes)`    (http4s 0.23+ httpRoutes)
//	  from `org.http4s.server.middleware.Throttle`. The FIRST arg is the request
//	  cap (Int), the SECOND a `scala.concurrent.duration.FiniteDuration`
//	  (`1.minute`, `100.millis`, `10.seconds`). We resolve amount + per/window
//	  when both are literal and stamp one marker entity per Throttle call. The
//	  middleware wraps the entire app/routes value, so scope=app (no per-route
//	  binding to fabricate). A `Throttle.apply` overload that takes a custom
//	  `TokenBucket` instead of `(amount, per)` is honest-partial (rate omitted).
//
//	akka-http / pekko-http `.throttle(...)` directive —
//	    `complete(source.throttle(100, 1.second))`               (positional)
//	    `.throttle(elements = 100, per = 1.second, ...)`         (named)
//	  the Akka/Pekko Streams throttle stage (also used inside akka-http route
//	  responses to rate-limit a streamed entity). The FIRST arg is `elements`
//	  (Int cap per window), a later FiniteDuration arg is the `per` window. We
//	  stamp the throttle posture on a marker entity (the throttle binds to a
//	  stream/response inside a route — a precise route join is heuristic, so we
//	  record scope=route without fabricating a specific verb+path). Resolved
//	  amount/per when literal. akka vs pekko is disambiguated by the file's
//	  framework (org.apache.pekko.* → pekko_throttle).
//
// Like the sibling passes these surfaces add NO new node KIND beyond a marker
// (`SCOPE.Pattern/rate_limit`) — there is no single per-route endpoint to attach
// an app-wide http4s Throttle to, and the akka throttle binds to a stream rather
// than a named route. MergeWithCustom dedups by Name; the marker carries the
// full throttle posture as evidence.
//
// Honest-partial cases (rate_limited stamped, numeric rate OMITTED):
//   - the amount or the duration arg is not a literal (a config val / expr);
//   - the duration unit is unrecognised;
//   - a Throttle overload taking a custom TokenBucket (no (amount, per) pair).
//
// Refs #4105.
package scala

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_scala_rate_limit", &scalaRateLimitExtractor{})
}

type scalaRateLimitExtractor struct{}

func (e *scalaRateLimitExtractor) Language() string { return "custom_scala_rate_limit" }

var (
	// reHttp4sThrottle matches the http4s Throttle middleware in its three
	// recognised call shapes, capturing the (amount, per) head:
	//   Throttle(100, 1.minute)(httpApp)
	//   Throttle.httpApp(100, 1.minute)(app)
	//   Throttle.httpRoutes(100, 1.minute)(routes)
	// Group 1 = optional `.httpApp` / `.httpRoutes` selector (or empty for the
	// bare apply); group 2 = amount expression; group 3 = per (FiniteDuration)
	// expression. An optional `[F]`/`[IO]` type-argument list (http4s 0.23+
	// `Throttle.httpApp[F]`) is allowed between the selector and the value-arg
	// paren. The args are matched non-greedily up to the first comma /
	// close-paren so a trailing `, throttleResponse` arg does not bleed in.
	reHttp4sThrottle = regexp.MustCompile(
		`\bThrottle(\.httpApp|\.httpRoutes|\.apply)?\s*(?:\[[^\]]*\]\s*)?\(\s*([^,()]+?)\s*,\s*([^,()]+?)\s*[,)]`)

	// reAkkaThrottlePositional matches a positional Streams/akka-http throttle:
	//   .throttle(100, 1.second)
	//   .throttle(100, 1.second, 10, ThrottleMode.Shaping)
	// Group 1 = elements (cap), group 2 = per (FiniteDuration). Extra args
	// (maximumBurst, mode) after the per arg are ignored.
	reAkkaThrottlePositional = regexp.MustCompile(
		`\.throttle\s*\(\s*([0-9][\w.]*?)\s*,\s*([0-9][\w.]*(?:second|minute|hour|day|milli|nano)s?)\b`)

	// reAkkaThrottleNamed matches a named-arg throttle:
	//   .throttle(elements = 100, per = 1.second, ...)
	// Group 1 = elements expr, group 2 = per expr.
	reAkkaThrottleNamed = regexp.MustCompile(
		`\.throttle\s*\(\s*elements\s*=\s*([^,()]+?)\s*,\s*per\s*=\s*([^,()]+?)\s*[,)]`)

	// reScalaIntLit matches a clean integer literal (allowing the Scala `_` digit
	// separator, e.g. 1_000).
	reScalaIntLit = regexp.MustCompile(`^[0-9][0-9_]*$`)

	// reScalaDurationLit matches a scala.concurrent.duration literal in the
	// extension form `N.unit` (1.minute / 100.millis / 10.seconds / 2.hours).
	// Group 1 = count, group 2 = unit stem.
	reScalaDurationLit = regexp.MustCompile(
		`^([0-9][0-9_]*)\.(nanos?|nanoseconds?|micros?|microseconds?|millis?|milliseconds?|seconds?|minutes?|hours?|days?)$`)
)

// parseScalaInt parses a Scala integer literal allowing `_` digit grouping.
// Returns (n, true) only for a clean integer literal.
func parseScalaInt(lit string) (int, bool) {
	lit = strings.TrimSpace(lit)
	if !reScalaIntLit.MatchString(lit) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.ReplaceAll(lit, "_", ""))
	if err != nil {
		return 0, false
	}
	return n, true
}

// scalaDurationSeconds resolves a scala.concurrent.duration FiniteDuration
// literal in the `N.unit` extension form to whole seconds, returning
// (seconds, true) only when statically literal AND the window divides into a
// whole number of seconds. Sub-second windows (millis/micros/nanos) are honest
// about not being whole seconds: they return (0, false) so the caller renders a
// sub-second human rate (see scalaHumanRate) instead of rounding to 0s. The
// bare-second/minute/hour/day units resolve directly.
func scalaDurationSeconds(lit string) (int, bool) {
	lit = strings.TrimSpace(lit)
	m := reScalaDurationLit.FindStringSubmatch(lit)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.ReplaceAll(m[1], "_", ""))
	if err != nil {
		return 0, false
	}
	switch {
	case strings.HasPrefix(m[2], "second"):
		return n, true
	case strings.HasPrefix(m[2], "minute"):
		return n * 60, true
	case strings.HasPrefix(m[2], "hour"):
		return n * 3600, true
	case strings.HasPrefix(m[2], "day"):
		return n * 86400, true
	}
	// Sub-second unit: not a whole-second window.
	return 0, false
}

// scalaDurationMillis resolves a FiniteDuration literal to whole milliseconds
// (for sub-second windows), returning (ms, true) only when literal. Used to
// render an honest "<n>/<ms>ms" rate when the window is sub-second.
func scalaDurationMillis(lit string) (int, bool) {
	lit = strings.TrimSpace(lit)
	m := reScalaDurationLit.FindStringSubmatch(lit)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.ReplaceAll(m[1], "_", ""))
	if err != nil {
		return 0, false
	}
	switch {
	case strings.HasPrefix(m[2], "milli"):
		return n, true
	case strings.HasPrefix(m[2], "second"):
		return n * 1000, true
	case strings.HasPrefix(m[2], "minute"):
		return n * 60000, true
	case strings.HasPrefix(m[2], "hour"):
		return n * 3600000, true
	case strings.HasPrefix(m[2], "day"):
		return n * 86400000, true
	}
	// nanos/micros: below ms resolution — honest-partial.
	return 0, false
}

// scalaHumanRate builds the shared "<count>/<window>s" human rate from a literal
// amount and a literal FiniteDuration window, or "" (honest-partial) when either
// is not statically resolvable. A sub-second window is rendered in milliseconds
// ("<count>/<ms>ms") to stay honest rather than rounding the window to 0s.
func scalaHumanRate(amountLit, perLit string) (rate string, limit int, periodSecs int, hasPeriod bool) {
	n, okN := parseScalaInt(amountLit)
	if !okN {
		return "", 0, 0, false
	}
	limit = n
	if secs, ok := scalaDurationSeconds(perLit); ok {
		return strconv.Itoa(n) + "/" + strconv.Itoa(secs) + "s", n, secs, true
	}
	if ms, ok := scalaDurationMillis(perLit); ok {
		return strconv.Itoa(n) + "/" + strconv.Itoa(ms) + "ms", n, 0, false
	}
	return "", n, 0, false
}

func (e *scalaRateLimitExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/scala")
	_, span := tracer.Start(ctx, "indexer.scala_rate_limit.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		))
	defer span.End()

	if len(file.Content) == 0 || file.Language != "scala" {
		return nil, nil
	}
	src := string(file.Content)
	// Fast guard: must mention one of the recognised idioms.
	if !strings.Contains(src, "Throttle") && !strings.Contains(src, ".throttle") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	for _, ent := range e.extractHttp4sThrottle(src, file) {
		add(ent)
	}
	for _, ent := range e.extractAkkaThrottle(src, file) {
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// extractHttp4sThrottle stamps the flat contract on a marker per http4s
// `Throttle(amount, per)(...)` call. http4s Throttle wraps a whole
// HttpApp/HttpRoutes value, so the binding is app-wide (scope=app) — there is no
// single per-route endpoint to attach to, so we emit a marker carrying the
// resolved amount/per. Only fires when the file is an http4s file (imports
// org.http4s) so a same-named `Throttle` from another library is not
// mis-attributed.
func (e *scalaRateLimitExtractor) extractHttp4sThrottle(src string, file extractor.FileInput) []types.EntityRecord {
	if !strings.Contains(src, "Throttle") {
		return nil
	}
	// Gate on http4s: the Throttle middleware lives in
	// org.http4s.server.middleware. Without an http4s signal a bare `Throttle`
	// identifier could be anything — honest skip.
	if !strings.Contains(src, "org.http4s") && !strings.Contains(src, "http4s") {
		return nil
	}

	var out []types.EntityRecord
	idx := 0
	for _, m := range reHttp4sThrottle.FindAllStringSubmatchIndex(src, -1) {
		amount := strings.TrimSpace(src[m[4]:m[5]])
		per := strings.TrimSpace(src[m[6]:m[7]])
		ln := lineOf(src, m[0])
		idx++
		// Unique marker name: include amount/per + line so multiple Throttles in
		// one file each get a distinct node.
		name := "http4s_throttle:" + amount + ":" + per + ":" + strconv.Itoa(ln)
		ent := makeEntity(name, "SCOPE.Pattern", "rate_limit", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "http4s",
			"kind", "rate_limit",
			"provenance", "INFERRED_FROM_HTTP4S_THROTTLE",
			"rate_limited", "true",
			"rate_limit_source", "http4s_throttle",
			// http4s Throttle wraps the whole HttpApp/HttpRoutes — app-wide, not a
			// named route.
			"rate_limit_scope", "app",
		)
		rate, limit, periodSecs, hasPeriod := scalaHumanRate(amount, per)
		if _, okN := parseScalaInt(amount); okN {
			ent.Properties["limit"] = strconv.Itoa(limit)
		}
		if hasPeriod {
			ent.Properties["period"] = strconv.Itoa(periodSecs)
		}
		if rate != "" {
			ent.Properties["rate_limit"] = rate
		}
		out = append(out, ent)
	}
	return out
}

// extractAkkaThrottle stamps the flat contract on a marker per akka/pekko
// Streams `.throttle(elements, per, ...)` stage (positional or named). The
// throttle binds to a stream/response inside a route — a precise verb+path join
// is heuristic, so we record scope=route without fabricating a route. akka vs
// pekko is disambiguated by the file's framework. Only fires in an akka/pekko
// file so a stray `.throttle` from another stream library is not mis-attributed.
func (e *scalaRateLimitExtractor) extractAkkaThrottle(src string, file extractor.FileInput) []types.EntityRecord {
	if !strings.Contains(src, ".throttle") {
		return nil
	}
	framework := detectScalaFramework(src)
	source := ""
	switch framework {
	case "akka-http":
		source = "akka_throttle"
	case "pekko-http":
		source = "pekko_throttle"
	default:
		// Also honour a raw akka/pekko Streams file (not necessarily akka-http)
		// when the akka/pekko package is imported — the throttle stage is a
		// Streams primitive used in and out of HTTP routes.
		switch {
		case strings.Contains(src, "org.apache.pekko"):
			source = "pekko_throttle"
		case strings.Contains(src, "akka."):
			source = "akka_throttle"
		default:
			return nil
		}
	}

	var out []types.EntityRecord
	emit := func(amount, per string, off int) {
		ln := lineOf(src, off)
		name := source + ":" + amount + ":" + per + ":" + strconv.Itoa(ln)
		ent := makeEntity(name, "SCOPE.Pattern", "rate_limit", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", framework,
			"kind", "rate_limit",
			"provenance", "INFERRED_FROM_AKKA_THROTTLE",
			"rate_limited", "true",
			"rate_limit_source", source,
			// The throttle guards a stream/response inside a route; a precise
			// verb+path binding is heuristic, so scope=route without a route name.
			"rate_limit_scope", "route",
		)
		rate, limit, periodSecs, hasPeriod := scalaHumanRate(amount, per)
		if _, okN := parseScalaInt(amount); okN {
			ent.Properties["limit"] = strconv.Itoa(limit)
		}
		if hasPeriod {
			ent.Properties["period"] = strconv.Itoa(periodSecs)
		}
		if rate != "" {
			ent.Properties["rate_limit"] = rate
		}
		out = append(out, ent)
	}

	// Named form first (more specific), then positional. A named throttle's
	// `.throttle(elements = ...` cannot match the positional regex (which
	// requires a leading digit after the paren), so the two never double-count
	// the same call.
	for _, m := range reAkkaThrottleNamed.FindAllStringSubmatchIndex(src, -1) {
		emit(strings.TrimSpace(src[m[2]:m[3]]), strings.TrimSpace(src[m[4]:m[5]]), m[0])
	}
	for _, m := range reAkkaThrottlePositional.FindAllStringSubmatchIndex(src, -1) {
		emit(strings.TrimSpace(src[m[2]:m[3]]), strings.TrimSpace(src[m[4]:m[5]]), m[0])
	}
	return out
}
