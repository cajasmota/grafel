// rate_limit.go — endpoint rate-limit / throttle stamping for Elixir web
// frameworks (#4099, child of #3628 Middleware/rate_limit_stamping). Elixir
// greenfield: prior to this pass every elixir framework cell for
// rate_limit_stamping was `missing` — a Hammer-protected Phoenix controller
// action carried no rate-limit signal at all.
//
// Elixir greenfield sibling of the Ruby pass
// (internal/custom/ruby/rate_limit_endpoint.go, #4074) and the C# pass (#4093);
// JS/TS + Java + Python shipped under #3778. This file stamps the SAME flat
// property contract the other languages use so the graph answers "which
// surfaces are throttled and at what rate?":
//
//	rate_limited      — "true" when a throttle applies.
//	rate_limit        — human rate "5/60s" when statically resolvable from
//	                    literal scale + limit; OMITTED (honest-partial) when
//	                    config-/expression-driven.
//	rate_limit_scope  — "route" (Hammer/ExRated call inside a controller action)
//	                    | "request" (PlugAttack throttle — binds to a conn
//	                    discriminator, not a named route).
//	rate_limit_source — the recognised idiom (`hammer`, `exrated`, `plug_attack`).
//	rate_limit_name   — the limiter bucket / throttle-rule name (evidence).
//	limit / period    — the resolved numeric cap + window-seconds (when literal).
//
// Three recognised Elixir surfaces:
//
//	Hammer  — `Hammer.check_rate("login:#{ip}", 60_000, 5)` (also
//	          `check_rate_inc`) called INSIDE a `def <action>(conn, _) do ... end`
//	          controller action. Hammer's signature is
//	          check_rate(id, scale_ms, limit): the SECOND arg is the window in
//	          MILLISECONDS, the THIRD is the request cap. We attribute the throttle
//	          to the enclosing action op (`action:<name>`, the same key
//	          phoenix.go emits for controller actions) and resolve
//	          limit + window/1000 → human rate. The id is commonly interpolated
//	          (`"login:#{ip}"`), so the *static* bucket prefix is recorded as
//	          rate_limit_name when present (honest evidence, not a route binding).
//
//	ExRated — `ExRated.check_rate("bucket", 60_000, 100)` — identical numeric
//	          contract (id, scale_ms, limit), source=exrated.
//
//	PlugAttack — `rule "throttle login", conn do throttle("...", limit: 100,
//	             period: 60_000) end` → a `SCOPE.Pattern/rate_limit` MARKER naming
//	             the throttle + limit/period. A PlugAttack throttle discriminates
//	             by request (the conn matcher), NOT a named route, so a route join
//	             is heuristic — the marker records the throttle posture honestly
//	             without fabricating a route binding (honest-partial scope=request).
//	             PlugAttack's `allow(...)` / `block(...)` rules are NOT throttles →
//	             not stamped as a rate limit.
//
// Like the sibling passes, the Hammer/ExRated surface adds NO new node — it
// stamps the flat contract on the controller-action op phoenix.go already emits
// (MergeWithCustom dedups by Name so the custom version wins). The PlugAttack
// surface adds a marker entity (there is no per-route endpoint to attach to).
//
// Honest-partial cases (rate_limited stamped, numeric rate OMITTED):
//   - the scale or limit arg is not an integer literal (a config var / expr);
//   - PlugAttack period/limit driven by config.
//
// Refs #4099.
package elixir

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
	extractor.Register("custom_elixir_rate_limit", &rateLimitExtractor{})
}

type rateLimitExtractor struct{}

func (e *rateLimitExtractor) Language() string { return "custom_elixir_rate_limit" }

var (
	// Hammer / ExRated check_rate call:
	//   Hammer.check_rate("login:#{ip}", 60_000, 5)        → limit = arg3
	//   ExRated.check_rate("bucket", 60_000, 100)           → limit = arg3
	//   Hammer.check_rate_inc("id", 60_000, 1, 5)           → limit = arg4
	// The `_inc` form takes an extra `increment` arg BEFORE the limit
	// (check_rate_inc(id, scale_ms, limit, increment) is the Hammer 6 order, while
	// older Hammer used check_rate_inc(id, scale_ms, increment, limit) — both place
	// the limit as the LAST integer arg, so we capture the trailing integer arg as
	// the limit and ignore an interposed increment). Group 1 = module, group 2 =
	// the bucket-id expression (between the first pair of quotes), group 3 =
	// scale_ms, group 4 = limit (3rd literal arg), group 5 = optional 4th literal
	// arg (present only in the inc form) — when set it is the real limit.
	// The id may be interpolated; we capture only its leading static literal
	// portion (up to the first `#{`).
	reHammerCheckRate = regexp.MustCompile(
		`\b(Hammer|ExRated)\.check_rate(?:_inc)?\s*\(\s*"([^"]*)"\s*,\s*([0-9_]+)\s*,\s*([0-9_]+)(?:\s*,\s*([0-9_]+))?`)

	// def <action>(conn, ...) do — a Phoenix controller action header. Group 1 =
	// the action name. We match the def whose offset most-closely precedes a
	// check_rate call to attribute the throttle to that action.
	reActionDef = regexp.MustCompile(
		`(?m)^\s*def\s+([a-z_][\w]*[?!]?)\s*\(`)

	// PlugAttack `throttle("name", limit: 100, period: 60_000)`. Group 1 = the
	// throttle name, group 2 = the option string (limit/period).
	rePlugAttackThrottle = regexp.MustCompile(
		`\bthrottle\s*\(\s*"([^"]+)"\s*,\s*([^)]*)\)`)

	// `limit: 100` inside a PlugAttack throttle option string.
	rePlugAttackLimit = regexp.MustCompile(`\blimit:\s*([0-9_]+)`)
	// `period: 60_000` (milliseconds) inside a PlugAttack throttle option string.
	rePlugAttackPeriod = regexp.MustCompile(`\bperiod:\s*([0-9_]+)`)
	// `use PlugAttack` — the marker `use` that identifies a PlugAttack module, so
	// a stray local `throttle(...)` helper in a non-PlugAttack module is not
	// mis-attributed.
	reUsePlugAttack = regexp.MustCompile(`(?m)^\s*use\s+PlugAttack\b`)
)

// parseElixirInt parses an Elixir integer literal allowing `_` digit grouping
// ("60_000" → 60000). Returns (n, true) only for a clean integer literal.
func parseElixirInt(lit string) (int, bool) {
	lit = strings.ReplaceAll(strings.TrimSpace(lit), "_", "")
	if lit == "" {
		return 0, false
	}
	n, err := strconv.Atoi(lit)
	if err != nil {
		return 0, false
	}
	return n, true
}

// humanRateFromMs builds the shared "<limit>/<window>s" rate from a request cap
// and a window expressed in MILLISECONDS, or "" (honest-partial) when either is
// not a static integer literal. The window is rounded to whole seconds; a
// sub-second window (< 1000ms) keeps at least 1s precision is not attempted —
// such windows are rare for endpoint throttles and would mislead, so a window
// that does not divide into whole seconds is reported honestly with the
// fractional seconds dropped only when >= 1s.
func humanRateFromMs(limit int, scaleMs int) string {
	if scaleMs <= 0 {
		return ""
	}
	secs := scaleMs / 1000
	if secs < 1 {
		// Sub-second window: express in milliseconds to stay honest.
		return strconv.Itoa(limit) + "/" + strconv.Itoa(scaleMs) + "ms"
	}
	return strconv.Itoa(limit) + "/" + strconv.Itoa(secs) + "s"
}

// staticBucketPrefix returns the static leading portion of a Hammer bucket id
// up to the first interpolation `#{`, trimmed of a trailing separator. For a
// fully-static id it returns the whole id; for `"login:#{ip}"` it returns
// "login". Returns "" when the id begins with interpolation (no static prefix).
func staticBucketPrefix(id string) string {
	if i := strings.Index(id, "#{"); i >= 0 {
		id = id[:i]
	}
	return strings.TrimRight(id, ":/-_ ")
}

func (e *rateLimitExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/elixir")
	_, span := tracer.Start(ctx, "indexer.elixir_rate_limit.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		))
	defer span.End()

	if len(file.Content) == 0 || file.Language != "elixir" {
		return nil, nil
	}
	src := string(file.Content)
	// Fast guard: must mention one of the recognised idioms.
	if !strings.Contains(src, "check_rate") && !strings.Contains(src, "PlugAttack") {
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

	for _, ent := range e.extractCheckRate(src, file) {
		add(ent)
	}
	for _, ent := range e.extractPlugAttack(src, file) {
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// extractCheckRate stamps the flat contract on the controller action enclosing
// each Hammer/ExRated `check_rate` call. The action op shares the `action:<name>`
// Name phoenix.go emits, so MergeWithCustom replaces phoenix's bare action op
// with this rate-limited one (scope=route). When no enclosing `def` precedes the
// call (a module-level limiter helper), the call is skipped — there is no action
// to attribute it to and inventing a route would be dishonest.
func (e *rateLimitExtractor) extractCheckRate(src string, file extractor.FileInput) []types.EntityRecord {
	if !strings.Contains(src, "check_rate") {
		return nil
	}
	// Pre-compute action-def offsets in source order for enclosing lookup.
	defs := reActionDef.FindAllStringSubmatchIndex(src, -1)

	var out []types.EntityRecord
	seen := make(map[string]bool)
	for _, m := range reHammerCheckRate.FindAllStringSubmatchIndex(src, -1) {
		module := src[m[2]:m[3]]
		bucketID := src[m[4]:m[5]]
		scaleLit := src[m[6]:m[7]]
		limitLit := src[m[8]:m[9]]
		// `_inc` form: a 4th literal arg is present (group 5) — the trailing
		// integer is the real limit; group 4 is the increment.
		if m[10] >= 0 {
			limitLit = src[m[10]:m[11]]
		}
		callOff := m[0]

		// Find the nearest preceding `def <action>(` — the enclosing action.
		action := ""
		var defOff int
		for _, d := range defs {
			if d[0] < callOff {
				action = src[d[2]:d[3]]
				defOff = d[0]
			} else {
				break
			}
		}
		if action == "" {
			// No enclosing action (module-level helper) — honest skip.
			continue
		}
		key := "action:" + action
		if seen[key] {
			continue
		}
		seen[key] = true

		source := "hammer"
		if module == "ExRated" {
			source = "exrated"
		}
		ent := makeEntity(key, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, defOff))
		setProps(&ent,
			"framework", "phoenix",
			"provenance", "INFERRED_FROM_ELIXIR_CHECK_RATE",
			"action_name", action,
			"rate_limited", "true",
			"rate_limit_scope", "route",
			"rate_limit_source", source)
		if name := staticBucketPrefix(bucketID); name != "" {
			ent.Properties["rate_limit_name"] = name
		}
		limit, limOK := parseElixirInt(limitLit)
		scaleMs, scaleOK := parseElixirInt(scaleLit)
		if limOK {
			ent.Properties["limit"] = strconv.Itoa(limit)
		}
		if scaleOK && scaleMs >= 1000 {
			ent.Properties["period"] = strconv.Itoa(scaleMs / 1000)
		}
		if limOK && scaleOK {
			if rate := humanRateFromMs(limit, scaleMs); rate != "" {
				ent.Properties["rate_limit"] = rate
			}
		}
		out = append(out, ent)
	}
	return out
}

// extractPlugAttack emits a marker entity per PlugAttack `throttle("name",
// limit: N, period: ms)`. The throttle binds to a request discriminator (the
// conn matcher), not a named route, so the marker records the throttle posture
// without a route join (honest-partial scope=request). `allow`/`block` rules are
// intentionally NOT matched (they are not rate limits). Only fires in a module
// that `use PlugAttack` so a stray local `throttle(...)` helper is not matched.
func (e *rateLimitExtractor) extractPlugAttack(src string, file extractor.FileInput) []types.EntityRecord {
	if !reUsePlugAttack.MatchString(src) {
		return nil
	}
	var out []types.EntityRecord
	for _, m := range rePlugAttackThrottle.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		opts := src[m[4]:m[5]]
		ent := makeEntity("plug_attack_throttle:"+name, "SCOPE.Pattern", "rate_limit", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "plug_attack",
			"kind", "rate_limit",
			"rate_limited", "true",
			"rate_limit_source", "plug_attack",
			"rate_limit_name", name,
			// PlugAttack discriminates by request (a conn matcher); the binding is
			// a request matcher, not a named route.
			"rate_limit_scope", "request")
		limit, limOK := 0, false
		if lm := rePlugAttackLimit.FindStringSubmatch(opts); lm != nil {
			limit, limOK = parseElixirInt(lm[1])
			if limOK {
				ent.Properties["limit"] = strconv.Itoa(limit)
			}
		}
		scaleMs, scaleOK := 0, false
		if pm := rePlugAttackPeriod.FindStringSubmatch(opts); pm != nil {
			scaleMs, scaleOK = parseElixirInt(pm[1])
			if scaleOK && scaleMs >= 1000 {
				ent.Properties["period"] = strconv.Itoa(scaleMs / 1000)
			}
		}
		if limOK && scaleOK {
			if rate := humanRateFromMs(limit, scaleMs); rate != "" {
				ent.Properties["rate_limit"] = rate
			}
		}
		out = append(out, ent)
	}
	return out
}
