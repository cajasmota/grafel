// rate_limit.go — endpoint rate-limit / throttle stamping for Rust web
// frameworks (#4124, child of #3628 Middleware/rate_limit_stamping). Rust
// greenfield: prior to this pass EVERY Rust HTTP-framework cell for
// rate_limit_stamping was `missing` — an axum router guarded by a tower-governor
// `GovernorLayer`, an actix app guarded by `Governor::new(&conf)`, or a tower
// `RateLimitLayer`-wrapped service carried no rate-limit signal.
//
// Rust greenfield sibling of the Scala pass
// (internal/custom/scala/rate_limit.go, #4105) and the C++ pass
// (internal/custom/cpp/rate_limit.go, #4115). It stamps the SAME flat property
// contract the other languages use so the graph answers "which surfaces are
// throttled and at what rate?":
//
//	rate_limited      — "true" when a throttle applies.
//	rate_limit        — human rate "5/s" / "100/60s" when statically resolvable
//	                    from a literal rate; OMITTED (honest-partial) when the
//	                    builder is chained cross-statement or non-literal.
//	rate_limit_scope  — "router" (a tower-governor GovernorLayer is applied to a
//	                    whole axum Router via .layer(...), not a named route)
//	                    | "app"   (an actix Governor is .wrap(...)-ed onto the
//	                      App/Scope, app-wide)
//	                    | "engine" (a bare tower RateLimitLayer / a governor
//	                      config builder not bound to a layer in-file).
//	rate_limit_source — the recognised idiom: "tower_governor" (axum
//	                    GovernorLayer) | "actix_governor" (actix Governor wrap)
//	                    | "tower_ratelimit" (tower::limit RateLimitLayer).
//	limit / period    — the resolved numeric cap + window-seconds (when literal).
//	rate_limit_burst  — the resolved governor burst_size (when literal).
//
// Three recognised Rust surfaces:
//
//  1. tower-governor on axum —
//     let conf = GovernorConfigBuilder::default()
//     .per_second(5).burst_size(10).finish().unwrap();
//     Router::new().route(...).layer(GovernorLayer { config: conf });
//     The GovernorLayer is the tower middleware; per_second is the steady-state
//     replenish rate (→ "<n>/s") and burst_size the bucket capacity. We resolve
//     per_second/burst_size from the GovernorConfigBuilder chain anywhere in the
//     file (the builder is conventionally assigned to a config value and then
//     referenced by the layer). The layer guards the whole Router, so
//     scope=router. A GovernorConfigBuilder with NO bound GovernorLayer in-file
//     is engine-scope (config defined, binding not seen here).
//
//  2. actix-governor —
//     let governor_conf = GovernorConfigBuilder::default()
//     .per_second(2).burst_size(5).finish().unwrap();
//     App::new().wrap(Governor::new(&governor_conf));
//     Governor is actix-web middleware applied via .wrap(...) — app-wide
//     (scope=app). Shares the GovernorConfigBuilder so per_second/burst_size are
//     resolved the same way.
//
//  3. tower RateLimitLayer (tower::limit) —
//     ServiceBuilder::new().layer(RateLimitLayer::new(100, Duration::from_secs(60)));
//     RateLimitLayer::new(num, per) — num requests per the Duration window. The
//     FIRST arg is the cap, the SECOND a std::time::Duration (from_secs(n) /
//     from_millis(n)). Resolves limit/period + human rate "100/60s". It wraps a
//     tower Service, not a named route → scope=engine.
//
// Like the sibling passes these surfaces add NO new node KIND beyond a marker
// (`SCOPE.Pattern/rate_limit`) — a governor layer guards a whole Router/App and
// a tower RateLimitLayer wraps a Service rather than a named route. The marker
// carries the full throttle posture as evidence; MergeWithCustom folds it onto
// the base op when (SourceFile, Kind, Name) all match (#6104).
//
// Honest-partial cases (rate_limited stamped, numeric rate OMITTED):
//   - per_second / burst_size / RateLimitLayer args are non-literal (a config
//     val / expression) or the builder is chained across statements we cannot
//     statically join to the binding;
//   - the Duration unit is unrecognised.
//
// Negatives proven: a plain axum/actix route with no governor/rate layer, and a
// non-rate tower layer (CorsLayer / TraceLayer), do NOT stamp.
//
// Honesty: partial — heuristic regex on source text gated on a tower-governor /
// actix-governor / tower-RateLimitLayer signal so a same-named symbol from
// another crate is not mis-attributed.
//
// Refs #4124.
package rust

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_rust_rate_limit", &rustRateLimitExtractor{})
}

type rustRateLimitExtractor struct{}

func (e *rustRateLimitExtractor) Language() string { return "custom_rust_rate_limit" }

var (
	// reGovernorLayer matches the tower-governor axum middleware binding:
	//   .layer(GovernorLayer { config: conf })
	//   .layer(GovernorLayer { config })          (field-init shorthand)
	//   .layer(GovernorLayer::new(...))            (newer builder form)
	// We only need the GovernorLayer token presence + its offset; the rate is
	// resolved from the GovernorConfigBuilder chain (reGovernorPerSecond /
	// reGovernorBurst) elsewhere in the file.
	reGovernorLayer = regexp.MustCompile(`\bGovernorLayer\b`)

	// reActixGovernorWrap matches the actix-governor middleware binding:
	//   .wrap(Governor::new(&governor_conf))
	//   .wrap(Governor::new(&conf))
	reActixGovernorWrap = regexp.MustCompile(`\.wrap\s*\(\s*Governor::new\s*\(`)

	// reGovernorConfigBuilder matches the shared governor config builder entry
	// point so a builder with no bound layer in-file can still be stamped
	// engine-scope.
	reGovernorConfigBuilder = regexp.MustCompile(`\bGovernorConfigBuilder::default\s*\(\s*\)`)

	// reGovernorPerSecond captures a literal .per_second(N) on the governor
	// builder chain. Group 1 = the replenish rate (requests/second).
	reGovernorPerSecond = regexp.MustCompile(`\.per_second\s*\(\s*(\d+)\s*\)`)

	// reGovernorPerMillisecond captures a literal .per_millisecond(N) on the
	// governor builder chain (sub-second replenish). Group 1 = ms-per-cell.
	reGovernorPerMillisecond = regexp.MustCompile(`\.per_millisecond\s*\(\s*(\d+)\s*\)`)

	// reGovernorBurst captures a literal .burst_size(M). Group 1 = bucket capacity.
	reGovernorBurst = regexp.MustCompile(`\.burst_size\s*\(\s*(\d+)\s*\)`)

	// reTowerRateLimitLayer matches the tower::limit RateLimitLayer:
	//   RateLimitLayer::new(100, Duration::from_secs(60))
	//   RateLimitLayer::new(50, Duration::from_millis(500))
	// Group 1 = num (cap), group 2 = the trailing Duration args text (parsed by
	// reDurationSecs / reDurationMillis).
	reTowerRateLimitLayer = regexp.MustCompile(
		`\bRateLimitLayer::new\s*\(\s*(\d+)\s*,\s*([^)]*\)?[^,)]*)\)`)

	// reDurationSecs / reDurationMillis parse a std::time::Duration constructor
	// from the trailing args of RateLimitLayer::new. Group 1 = count.
	reDurationSecs   = regexp.MustCompile(`Duration::from_secs\s*\(\s*(\d+)\s*\)`)
	reDurationMillis = regexp.MustCompile(`Duration::from_millis\s*\(\s*(\d+)\s*\)`)
)

// rustGovernorRate resolves the governor steady-state rate from the file's
// GovernorConfigBuilder chain. Prefers .per_second(N) (→ "<n>/s", periodSecs=1);
// falls back to .per_millisecond(N) (→ "1/<n>ms", sub-second, periodSecs unset).
// Returns ("", 0, 0, false) when no literal replenish rate is present (the
// builder is chained cross-statement or non-literal → honest-partial).
func rustGovernorRate(src string) (rate string, limit int, periodSecs int, ok bool) {
	if m := reGovernorPerSecond.FindStringSubmatch(src); m != nil {
		n, _ := strconv.Atoi(m[1])
		return strconv.Itoa(n) + "/s", n, 1, true
	}
	if m := reGovernorPerMillisecond.FindStringSubmatch(src); m != nil {
		n, _ := strconv.Atoi(m[1])
		// per_millisecond(N) = one cell replenished every N ms.
		return "1/" + strconv.Itoa(n) + "ms", 1, 0, true
	}
	return "", 0, 0, false
}

// rustGovernorBurst resolves a literal .burst_size(M) from the builder chain.
func rustGovernorBurst(src string) (burst int, ok bool) {
	if m := reGovernorBurst.FindStringSubmatch(src); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	return 0, false
}

// rustTowerWindowSecs parses the std::time::Duration window from the trailing
// args of a RateLimitLayer::new(num, Duration) call. Returns (secs, ms, ok):
// from_secs → (n, 0, true); from_millis → (0, n, true).
func rustTowerWindowSecs(argsText string) (secs int, ms int, ok bool) {
	if m := reDurationSecs.FindStringSubmatch(argsText); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, 0, true
	}
	if m := reDurationMillis.FindStringSubmatch(argsText); m != nil {
		n, _ := strconv.Atoi(m[1])
		return 0, n, true
	}
	return 0, 0, false
}

// rustTowerHumanRate builds "<num>/<secs>s" (or "<num>/<ms>ms" for a sub-second
// window) from the tower RateLimitLayer cap + window; "" when window unresolved.
func rustTowerHumanRate(num, secs, ms int) string {
	switch {
	case secs > 0:
		return strconv.Itoa(num) + "/" + strconv.Itoa(secs) + "s"
	case ms > 0:
		return strconv.Itoa(num) + "/" + strconv.Itoa(ms) + "ms"
	}
	return ""
}

func (e *rustRateLimitExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rust_rate_limit.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		))
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
		return nil, nil
	}
	src := string(file.Content)
	// Fast guard: must mention one of the recognised idioms.
	if !strings.Contains(src, "GovernorLayer") &&
		!strings.Contains(src, "Governor::new") &&
		!strings.Contains(src, "GovernorConfigBuilder") &&
		!strings.Contains(src, "RateLimitLayer") {
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

	// Resolve the shared governor config (builder is conventionally assigned to a
	// config value and referenced by the layer/wrap; we resolve per_second /
	// burst_size from the chain anywhere in the file). Applied to both the
	// tower-governor (axum) and actix-governor surfaces.
	govRate, govLimit, govPeriod, govRateOK := rustGovernorRate(src)
	govBurst, govBurstOK := rustGovernorBurst(src)

	applyGovernorProps := func(ent *types.EntityRecord) {
		if govRateOK {
			ent.Properties["rate_limit"] = govRate
			ent.Properties["limit"] = strconv.Itoa(govLimit)
			if govPeriod > 0 {
				ent.Properties["period"] = strconv.Itoa(govPeriod)
			}
		}
		if govBurstOK {
			ent.Properties["rate_limit_burst"] = strconv.Itoa(govBurst)
		}
	}

	// ---------------------------------------------------------------------
	// 1. tower-governor GovernorLayer on an axum Router (scope=router).
	// ---------------------------------------------------------------------
	layerBound := false
	for _, m := range reGovernorLayer.FindAllStringIndex(src, -1) {
		layerBound = true
		line := lineOf(src, m[0])
		name := "tower_governor:" + file.Path + ":" + strconv.Itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "rate_limit", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "axum",
			"provenance", "INFERRED_FROM_TOWER_GOVERNOR",
			"kind", "rate_limit",
			"rate_limited", "true",
			"rate_limit_scope", "router",
			"rate_limit_source", "tower_governor")
		applyGovernorProps(&ent)
		add(ent)
	}

	// ---------------------------------------------------------------------
	// 2. actix-governor Governor::new wrap (scope=app).
	// ---------------------------------------------------------------------
	wrapBound := false
	for _, m := range reActixGovernorWrap.FindAllStringIndex(src, -1) {
		wrapBound = true
		line := lineOf(src, m[0])
		name := "actix_governor:" + file.Path + ":" + strconv.Itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "rate_limit", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "actix-web",
			"provenance", "INFERRED_FROM_ACTIX_GOVERNOR",
			"kind", "rate_limit",
			"rate_limited", "true",
			"rate_limit_scope", "app",
			"rate_limit_source", "actix_governor")
		applyGovernorProps(&ent)
		add(ent)
	}

	// ---------------------------------------------------------------------
	// 3. A GovernorConfigBuilder with NO bound GovernorLayer / Governor::new
	//    wrap in-file: the config is defined but its binding is not seen here.
	//    Stamp engine-scope (still carries the resolved rate as evidence).
	// ---------------------------------------------------------------------
	if !layerBound && !wrapBound {
		for _, m := range reGovernorConfigBuilder.FindAllStringIndex(src, -1) {
			line := lineOf(src, m[0])
			name := "governor_config:" + file.Path + ":" + strconv.Itoa(line)
			ent := makeEntity(name, "SCOPE.Pattern", "rate_limit", file.Path, file.Language, line)
			setProps(&ent,
				"framework", "tower-governor",
				"provenance", "INFERRED_FROM_GOVERNOR_CONFIG",
				"kind", "rate_limit",
				"rate_limited", "true",
				"rate_limit_scope", "engine",
				"rate_limit_source", "tower_governor")
			applyGovernorProps(&ent)
			add(ent)
		}
	}

	// ---------------------------------------------------------------------
	// 4. tower::limit RateLimitLayer::new(num, Duration) (scope=engine).
	// ---------------------------------------------------------------------
	for _, m := range reTowerRateLimitLayer.FindAllStringSubmatchIndex(src, -1) {
		num, _ := strconv.Atoi(src[m[2]:m[3]])
		argsText := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		name := "tower_ratelimit:" + file.Path + ":" + strconv.Itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "rate_limit", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "tower",
			"provenance", "INFERRED_FROM_TOWER_RATELIMIT_LAYER",
			"kind", "rate_limit",
			"rate_limited", "true",
			"rate_limit_scope", "engine",
			"rate_limit_source", "tower_ratelimit")
		if num > 0 {
			ent.Properties["limit"] = strconv.Itoa(num)
		}
		if secs, ms, ok := rustTowerWindowSecs(argsText); ok {
			if secs > 0 {
				ent.Properties["period"] = strconv.Itoa(secs)
			}
			if rate := rustTowerHumanRate(num, secs, ms); rate != "" {
				ent.Properties["rate_limit"] = rate
			}
		}
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
