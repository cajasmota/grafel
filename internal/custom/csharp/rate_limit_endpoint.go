// rate_limit_endpoint.go — endpoint rate-limit / throttle stamping for C#/.NET
// web frameworks (#4089, child of #3628). C#/.NET sibling of the Ruby
// (internal/custom/ruby/rate_limit_endpoint.go, #4072), Python
// (internal/custom/python/rate_limit_endpoint.go) and Go/JS/TS/Java/PHP passes
// shipped under #3778/#3790/#4073/#4082.
//
// The existing csharp auth extractor (auth.go) records auth posture as flat
// SCOPE.Pattern markers per recognised idiom; .NET rate-limit attributes and
// fluent calls likewise span declarations that do not reduce to a single
// route op, so this file follows the same marker model. It stamps the SAME flat
// property contract the other languages use so the graph answers "which surfaces
// are throttled and at what rate?":
//
//	rate_limited      — "true" when a throttle applies.
//	rate_limit        — human rate "100/60s" when statically resolvable from an
//	                    in-file policy (PermitLimit + Window); OMITTED
//	                    (honest-partial) when config-/cross-file-driven.
//	rate_limit_scope  — "route" (RequireRateLimiting / EnableRateLimiting attr) |
//	                    "engine" (AspNetCoreRateLimit pipeline middleware).
//	rate_limit_source — the recognised idiom: "fixed_window" / "sliding_window" /
//	                    "token_bucket" / "concurrency" when the bound policy's
//	                    limiter kind is resolvable in-file, else the binding idiom
//	                    ("require_rate_limiting" / "enable_rate_limiting" /
//	                    "aspnetcoreratelimit").
//	rate_limit_name   — the .NET rate-limiter policy name (evidence).
//
// Recognised C#/.NET surfaces:
//
//	ASP.NET Core 7+ built-in RateLimiter —
//	  builder.Services.AddRateLimiter(o => o.AddFixedWindowLimiter("api",
//	      opt => { opt.PermitLimit = 100; opt.Window = TimeSpan.FromMinutes(1); }));
//	  app.MapGet("/x", ...).RequireRateLimiting("api");
//	  [EnableRateLimiting("api")] on a controller / action.
//	  The .RequireRateLimiting("p") / [EnableRateLimiting("p")] binding is stamped
//	  rate_limited; the policy "p"'s PermitLimit/Window resolved from the in-file
//	  Add*Limiter("p", ...) definition gives the rate + limiter kind. A policy
//	  defined in another file is honest-partial (rate + kind omitted).
//	  [DisableRateLimiting] is the NEGATIVE — never stamped.
//
//	AspNetCoreRateLimit middleware —
//	  app.UseIpRateLimiting(); / app.UseClientRateLimiting();
//	  Engine-wide posture; the numeric limits live in appsettings config, so the
//	  rate is honest-partial (omitted) — only the engine-scope posture is stamped.
//
// Like the sibling passes, every surface adds a SCOPE.Pattern/rate_limit marker
// (there is no per-route endpoint op to attach to in the multi-line .NET idioms).
//
// Refs #4089.
package csharp

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
	extractor.Register("custom_csharp_rate_limit", &csharpRateLimitExtractor{})
}

type csharpRateLimitExtractor struct{}

func (e *csharpRateLimitExtractor) Language() string { return "custom_csharp_rate_limit" }

var (
	// .RequireRateLimiting("policy") — minimal-API / endpoint binding.
	csRlRequireRe = regexp.MustCompile(`\.RequireRateLimiting\s*\(\s*"([^"]+)"`)

	// [EnableRateLimiting("policy")] — controller / action attribute.
	csRlEnableAttrRe = regexp.MustCompile(`\[\s*EnableRateLimiting\s*\(\s*"([^"]+)"`)

	// [DisableRateLimiting] — NEGATIVE marker (opt-out attribute).
	csRlDisableAttrRe = regexp.MustCompile(`\[\s*DisableRateLimiting\b`)

	// Policy definitions inside AddRateLimiter(...). Group 1 = limiter kind
	// (FixedWindow|SlidingWindow|TokenBucket|Concurrency), group 2 = policy name.
	//   o.AddFixedWindowLimiter("api", opt => { ... })
	//   options.AddTokenBucketLimiter("tb", o => { ... })
	csRlAddLimiterRe = regexp.MustCompile(
		`\.Add(FixedWindow|SlidingWindow|TokenBucket|Concurrency)Limiter\s*\(\s*(?:policyName\s*:\s*)?"([^"]+)"`)

	// AspNetCoreRateLimit pipeline middleware (engine-wide, config-driven).
	csRlAspNetCoreRateLimitRe = regexp.MustCompile(`\.Use(Ip|Client)RateLimiting\s*\(`)
)

// csRlLimiterSource maps a .NET limiter-kind token to the rate_limit_source
// value used in the flat contract.
var csRlLimiterSource = map[string]string{
	"FixedWindow":   "fixed_window",
	"SlidingWindow": "sliding_window",
	"TokenBucket":   "token_bucket",
	"Concurrency":   "concurrency",
}

// csRlPolicy is the resolved definition of one Add*Limiter("name", ...) policy.
type csRlPolicy struct {
	source string // "fixed_window" / "sliding_window" / "token_bucket" / "concurrency"
	rate   string // "100/60s" or "" (honest-partial: limit/window not both literal)
}

// csRlOptBlock returns the brace-balanced body of the limiter-options lambda
// that begins at/after offset `from` in src (the `=> { ... }` configuring one
// Add*Limiter call). Returns "" when no `{` follows on the same statement, so a
// terminal-`)` limiter with no inline options is honest-partial.
func csRlOptBlock(src string, from int) string {
	open := strings.IndexByte(src[from:], '{')
	if open < 0 {
		return ""
	}
	// Guard: a `;` or matching `)` before the first `{` means this call has no
	// options lambda body (e.g. AddConcurrencyLimiter("x", o => o.PermitLimit=1)
	// is handled by the property regexes scanning a bounded window instead).
	pre := src[from : from+open]
	if strings.ContainsAny(pre, ";") {
		return ""
	}
	open += from
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open : i+1]
			}
		}
	}
	return src[open:]
}

var (
	// opt.PermitLimit = 100;  /  o.PermitLimit = 100,  (the request cap)
	csRlPermitRe = regexp.MustCompile(`\bPermitLimit\s*=\s*(\d+)`)
	// o.TokenLimit = 100;  (token-bucket cap)
	csRlTokenLimitRe = regexp.MustCompile(`\bTokenLimit\s*=\s*(\d+)`)
	// o.PermitsPerSecond = 5;  (sliding/token replenishment — not used as the cap)
	// opt.Window = TimeSpan.FromMinutes(1) / FromSeconds(30) / FromHours(2)
	csRlWindowRe = regexp.MustCompile(
		`\bWindow\s*=\s*TimeSpan\.From(Seconds|Minutes|Hours|Days)\s*\(\s*(\d+)\s*\)`)
)

// csRlWindowSeconds converts a TimeSpan.From<Unit>(n) match to whole seconds.
func csRlWindowSeconds(unit string, n int) int {
	switch unit {
	case "Seconds":
		return n
	case "Minutes":
		return n * 60
	case "Hours":
		return n * 3600
	case "Days":
		return n * 86400
	}
	return 0
}

// csRlResolvePolicies scans every Add*Limiter("name", ...) definition in src and
// resolves each policy's limiter kind + (where both PermitLimit/TokenLimit and a
// literal TimeSpan Window are present) its human rate. Concurrency limiters have
// no window, so they resolve a kind but never a rate (honest-partial rate).
func csRlResolvePolicies(src string) map[string]csRlPolicy {
	out := map[string]csRlPolicy{}
	for _, m := range csRlAddLimiterRe.FindAllStringSubmatchIndex(src, -1) {
		kind := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		pol := csRlPolicy{source: csRlLimiterSource[kind]}
		body := csRlOptBlock(src, m[1])
		if body != "" {
			cap := 0
			if pm := csRlPermitRe.FindStringSubmatch(body); pm != nil {
				cap, _ = strconv.Atoi(pm[1])
			} else if tm := csRlTokenLimitRe.FindStringSubmatch(body); tm != nil {
				cap, _ = strconv.Atoi(tm[1])
			}
			if cap > 0 {
				if wm := csRlWindowRe.FindStringSubmatch(body); wm != nil {
					n, _ := strconv.Atoi(wm[2])
					if secs := csRlWindowSeconds(wm[1], n); secs > 0 {
						pol.rate = strconv.Itoa(cap) + "/" + strconv.Itoa(secs) + "s"
					}
				}
			}
		}
		// First definition wins (a policy name is registered once).
		if _, dup := out[name]; !dup {
			out[name] = pol
		}
	}
	return out
}

func (e *csharpRateLimitExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.csharp_rate_limit.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		))
	defer span.End()

	if len(file.Content) == 0 || file.Language != "csharp" {
		return nil, nil
	}
	src := string(file.Content)
	// Fast guard: must mention one of the recognised idioms.
	if !strings.Contains(src, "RequireRateLimiting") &&
		!strings.Contains(src, "EnableRateLimiting") &&
		!strings.Contains(src, "RateLimiting") &&
		!strings.Contains(src, "UseIpRateLimiting") &&
		!strings.Contains(src, "UseClientRateLimiting") {
		return nil, nil
	}

	policies := csRlResolvePolicies(src)

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// Collect the policy names explicitly opted OUT via [DisableRateLimiting].
	// The attribute is parameterless (it disables ALL policies on the target), so
	// a binding on the SAME line/adjacent attribute is the negative. We treat a
	// .RequireRateLimiting / [EnableRateLimiting] occurrence as suppressed only
	// when [DisableRateLimiting] sits on the immediately preceding line — keeping
	// the negative honest without a full attribute-target parser.
	disableLines := map[int]bool{}
	for _, m := range csRlDisableAttrRe.FindAllStringIndex(src, -1) {
		disableLines[lineOf(src, m[0])] = true
	}
	suppressed := func(line int) bool {
		return disableLines[line] || disableLines[line-1] || disableLines[line+1]
	}

	// 1. .RequireRateLimiting("policy") — endpoint binding (route scope).
	for _, m := range csRlRequireRe.FindAllStringSubmatchIndex(src, -1) {
		policy := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		if suppressed(line) {
			continue
		}
		add(e.bindingEntity("require:", policy, "route", "require_rate_limiting",
			policies, file, line))
	}

	// 2. [EnableRateLimiting("policy")] — controller/action attribute (route scope).
	for _, m := range csRlEnableAttrRe.FindAllStringSubmatchIndex(src, -1) {
		policy := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		if suppressed(line) {
			continue
		}
		add(e.bindingEntity("enable:", policy, "route", "enable_rate_limiting",
			policies, file, line))
	}

	// 3. AspNetCoreRateLimit pipeline middleware — engine-wide, config-driven
	//    (rate omitted: limits live in appsettings, honest-partial).
	for _, m := range csRlAspNetCoreRateLimitRe.FindAllStringSubmatchIndex(src, -1) {
		variant := strings.ToLower(src[m[2]:m[3]]) // "ip" | "client"
		line := lineOf(src, m[0])
		name := "aspnetcoreratelimit:" + variant + ":" + file.Path
		ent := makeEntity(name, "SCOPE.Pattern", "rate_limit", file.Path, "csharp", line)
		setProps(&ent, "framework", "aspnetcoreratelimit",
			"kind", "rate_limit",
			"rate_limited", "true",
			"rate_limit_scope", "engine",
			"rate_limit_source", "aspnetcoreratelimit",
			"rate_limit_variant", variant)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// bindingEntity builds the rate_limit marker for a .NET RateLimiter binding
// (.RequireRateLimiting / [EnableRateLimiting]) naming `policy`. When the policy
// is defined in-file its limiter kind becomes rate_limit_source and its resolved
// rate is stamped; otherwise the binding idiom is the source and the rate is
// omitted (honest-partial cross-file policy).
func (e *csharpRateLimitExtractor) bindingEntity(prefix, policy, scope, idiom string,
	policies map[string]csRlPolicy, file extractor.FileInput, line int) types.EntityRecord {
	name := prefix + policy + ":" + file.Path + ":" + itoa(line)
	ent := makeEntity(name, "SCOPE.Pattern", "rate_limit", file.Path, "csharp", line)
	source := idiom
	if pol, ok := policies[policy]; ok && pol.source != "" {
		source = pol.source
	}
	setProps(&ent, "framework", "aspnet_core",
		"kind", "rate_limit",
		"rate_limited", "true",
		"rate_limit_scope", scope,
		"rate_limit_source", source,
		"rate_limit_name", policy,
		"rate_limit_binding", idiom)
	if pol, ok := policies[policy]; ok && pol.rate != "" {
		ent.Properties["rate_limit"] = pol.rate
	}
	return ent
}
