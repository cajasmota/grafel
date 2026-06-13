// polly_resilience.go — Polly resilience-policy extraction for C#/.NET (#5075,
// spun out of #5016 / #4969). Sibling of the Java MicroProfile fault-tolerance
// pass (internal/custom/java/microprofile.go: @Retry / @CircuitBreaker /
// @Fallback -> SCOPE.Pattern markers carrying fault_tolerance_type) and the
// C#/.NET rate-limit pass (internal/custom/csharp/rate_limit_endpoint.go).
//
// Polly is the de-facto .NET resilience library. It exposes two surfaces:
//
//   - v7 fluent policies —
//       Policy.Handle<HttpRequestException>().Retry(3)
//       Policy.Handle<T>().WaitAndRetryAsync(3, attempt => ...)
//       Policy.Handle<T>().CircuitBreaker(2, TimeSpan.FromMinutes(1))
//       Policy.Timeout(TimeSpan.FromSeconds(10))
//       Policy.Bulkhead(maxParallelization: 12)
//       Policy.Handle<T>().Fallback(fallbackValue)
//
//   - v8 ResiliencePipelineBuilder —
//       new ResiliencePipelineBuilder()
//           .AddRetry(new RetryStrategyOptions { MaxRetryAttempts = 3 })
//           .AddCircuitBreaker(new CircuitBreakerStrategyOptions { ... })
//           .AddTimeout(TimeSpan.FromSeconds(10))
//           .Build();
//
// Either surface may be attached to an HttpClient via the .NET HttpClientFactory
// integration: services.AddHttpClient("x").AddPolicyHandler(policy) (v7) or
// .AddResilienceHandler("pipeline", b => b.AddRetry(...)) (v8). Those bindings
// say "this HTTP client is protected by a resilience policy".
//
// Like the rate-limit and MicroProfile passes, each recognised surface adds a
// flat SCOPE.Pattern/resilience_policy marker (Polly policies span fluent chains
// and option lambdas that do not reduce to a single call op). The flat property
// contract answers "which calls / HTTP clients are protected, by what kind of
// policy, and with what parameters?":
//
//	framework        — "polly".
//	kind             — "resilience_policy".
//	resilience_type  — retry | circuit_breaker | timeout | bulkhead | fallback |
//	                   hedging | rate_limiter (the policy strategy).
//	api_version      — "v7" (fluent Policy.*) | "v8" (ResiliencePipelineBuilder).
//	retry_count      — the retry attempt count when a literal is resolvable
//	                   (Retry(3) / MaxRetryAttempts = 3 / WaitAndRetry(5, ...)).
//	timeout_seconds  — the timeout window in seconds when a literal TimeSpan is
//	                   resolvable (Timeout(TimeSpan.FromSeconds(10)) / Timeout =
//	                   TimeSpan.FromSeconds(10)).
//	break_seconds    — the circuit-breaker open duration in seconds when literal
//	                   (CircuitBreaker(2, TimeSpan.FromMinutes(1))).
//	break_threshold  — the circuit-breaker failure threshold when literal
//	                   (CircuitBreaker(2, ...) / FailureThreshold = ...).
//	max_parallel     — the bulkhead max-parallelization when literal.
//	handled_exception— the first Handle<T>() exception type (evidence).
//	http_client      — the HttpClient name when bound via AddPolicyHandler /
//	                   AddResilienceHandler on AddHttpClient("name").
//	binding          — "add_policy_handler" | "add_resilience_handler" when the
//	                   policy is wired to an HttpClient (else omitted).
//
// Honest-partial: parameters that are config-/variable-driven (not in-file
// literals) are omitted rather than guessed. Cross-file policy variables passed
// to AddPolicyHandler(myPolicy) record the binding + http_client but resolve no
// strategy params.
//
// Closes #5075 (Polly half).
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
	extractor.Register("custom_csharp_polly", &pollyExtractor{})
}

type pollyExtractor struct{}

func (e *pollyExtractor) Language() string { return "custom_csharp_polly" }

var (
	// Policy.Handle<HttpRequestException>() — the v7 exception predicate that
	// opens a fluent policy chain. Captures the first handled exception type.
	pollyHandleRe = regexp.MustCompile(`Policy(?:<[^>]*>)?\s*\.\s*Handle(?:Inner)?\s*<\s*([\w.]+)\s*>`)

	// v7 fluent strategy calls. Each opens a resilience policy of the named kind.
	//   .Retry(3) / .RetryAsync() / .RetryForever()
	//   .WaitAndRetry(5, ...) / .WaitAndRetryAsync(...)
	//   .CircuitBreaker(2, TimeSpan.FromMinutes(1)) / .AdvancedCircuitBreaker(...)
	//   Policy.Timeout(...) / Policy.TimeoutAsync(...)
	//   Policy.Bulkhead(12) / Policy.BulkheadAsync(...)
	//   .Fallback(...) / .FallbackAsync(...)
	pollyRetryRe   = regexp.MustCompile(`\.\s*(?:WaitAndRetry|Retry)(?:Forever)?(?:Async)?\s*\(`)
	pollyCBRe      = regexp.MustCompile(`\.\s*(?:Advanced)?CircuitBreaker(?:Async)?\s*\(`)
	pollyTimeoutRe = regexp.MustCompile(`\bPolicy\s*\.\s*Timeout(?:Async)?\s*\(`)
	pollyBulkRe    = regexp.MustCompile(`\bPolicy\s*\.\s*Bulkhead(?:Async)?\s*\(`)
	pollyFallRe    = regexp.MustCompile(`\.\s*Fallback(?:Async)?\s*\(`)

	// v8 ResiliencePipelineBuilder strategy calls.
	//   new ResiliencePipelineBuilder<T>() ... .AddRetry(...) .AddCircuitBreaker(...)
	//   .AddTimeout(...) .AddHedging(...) .AddRateLimiter(...) .AddConcurrencyLimiter(...)
	pollyPipelineRe   = regexp.MustCompile(`\bResiliencePipelineBuilder\b`)
	pollyAddStrategyRe = regexp.MustCompile(
		`\.\s*Add(Retry|CircuitBreaker|Timeout|Hedging|ConcurrencyLimiter|RateLimiter)\s*\(`)

	// HttpClientFactory bindings — these attach a policy/pipeline to a named client.
	//   services.AddHttpClient("github").AddPolicyHandler(retryPolicy);     (v7)
	//   services.AddHttpClient("github").AddResilienceHandler("p", b => ...); (v8)
	pollyAddPolicyHandlerRe     = regexp.MustCompile(`\.\s*AddPolicyHandler\s*\(`)
	pollyAddResilienceHandlerRe = regexp.MustCompile(`\.\s*AddResilienceHandler\s*\(`)
	// AddHttpClient("name") — captures the named client a handler binds onto.
	pollyAddHttpClientRe = regexp.MustCompile(`\.\s*AddHttpClient\s*(?:<[^>]*>)?\s*\(\s*"([^"]+)"`)

	// Literal parameter resolvers (in-file only; config/variable args are partial).
	pollyRetryArgRe       = regexp.MustCompile(`\b(?:Retry|WaitAndRetry)(?:Async)?\s*\(\s*(\d+)`)
	pollyMaxRetryOptRe    = regexp.MustCompile(`\bMaxRetryAttempts\s*=\s*(\d+)`)
	pollyCBThreshArgRe    = regexp.MustCompile(`\bCircuitBreaker(?:Async)?\s*\(\s*(\d+)`)
	pollyCBThreshOptRe    = regexp.MustCompile(`\b(?:FailureThreshold|FailureRatio)\s*=\s*([\d.]+)`)
	pollyTimeSpanRe       = regexp.MustCompile(`TimeSpan\.From(Seconds|Minutes|Hours|Milliseconds)\s*\(\s*(\d+)`)
	pollyBulkArgRe        = regexp.MustCompile(`\bBulkhead(?:Async)?\s*\(\s*(?:maxParallelization\s*:\s*)?(\d+)`)
	pollyMaxParallelOptRe = regexp.MustCompile(`\bMaxParallelization\s*=\s*(\d+)`)
)

// pollyTimeSpanSeconds converts a TimeSpan.From<Unit>(n) (unit, magnitude) to
// whole seconds, or "" when the unit is sub-second/unknown.
func pollyTimeSpanSeconds(unit, mag string) string {
	n, err := strconv.Atoi(mag)
	if err != nil {
		return ""
	}
	switch unit {
	case "Seconds":
	case "Minutes":
		n *= 60
	case "Hours":
		n *= 3600
	case "Milliseconds":
		if n%1000 != 0 {
			return "" // sub-second; honest-partial rather than round
		}
		n /= 1000
	default:
		return ""
	}
	return strconv.Itoa(n)
}

// pollyChain returns the source window from offset `from` bounded to the next
// ';' (statement terminator), so params from a later policy in the same file
// do not bleed into this one. Mirrors the bounded-window scan used by the
// Quartz.NET and rate-limit passes.
func pollyChain(src string, from int) string {
	rest := src[from:]
	if semi := strings.IndexByte(rest, ';'); semi >= 0 {
		return rest[:semi]
	}
	return rest
}

func (e *pollyExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.csharp_polly.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "polly"),
			attribute.String("file_path", file.Path),
		))
	defer span.End()

	if len(file.Content) == 0 || file.Language != "csharp" {
		return nil, nil
	}
	src := string(file.Content)
	// Fast guard.
	if !strings.Contains(src, "Policy") &&
		!strings.Contains(src, "ResiliencePipelineBuilder") &&
		!strings.Contains(src, "AddPolicyHandler") &&
		!strings.Contains(src, "AddResilienceHandler") {
		return nil, nil
	}

	var out []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, ent)
	}

	// marker builds a resilience_policy SCOPE.Pattern marker.
	marker := func(resType, apiVersion string, line int) types.EntityRecord {
		name := "polly:" + resType + ":" + file.Path + ":" + itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "resilience_policy", file.Path, "csharp", line)
		setProps(&ent,
			"framework", "polly",
			"kind", "resilience_policy",
			"resilience_type", resType,
			"api_version", apiVersion,
			"provenance", "INFERRED_FROM_POLLY",
		)
		return ent
	}

	// firstHandled returns the nearest preceding Handle<T>() exception type for a
	// v7 fluent strategy, scanning back from the strategy call to the start of the
	// statement (the previous ';' or '='). Honest-partial: "" when none in-file.
	firstHandled := func(callOff int) string {
		start := 0
		for _, sep := range []byte{';', '='} {
			if i := strings.LastIndexByte(src[:callOff], sep); i > start {
				start = i
			}
		}
		seg := src[start:callOff]
		if m := pollyHandleRe.FindStringSubmatch(seg); m != nil {
			return m[1]
		}
		return ""
	}

	// 1. v7 Retry / WaitAndRetry.
	for _, m := range pollyRetryRe.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		ent := marker("retry", "v7", line)
		chain := pollyChain(src, m[0])
		if rm := pollyRetryArgRe.FindStringSubmatch(chain); rm != nil {
			setProps(&ent, "retry_count", rm[1])
		}
		if h := firstHandled(m[0]); h != "" {
			setProps(&ent, "handled_exception", h)
		}
		add(ent)
	}

	// 2. v7 CircuitBreaker / AdvancedCircuitBreaker.
	for _, m := range pollyCBRe.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		ent := marker("circuit_breaker", "v7", line)
		chain := pollyChain(src, m[0])
		if cm := pollyCBThreshArgRe.FindStringSubmatch(chain); cm != nil {
			setProps(&ent, "break_threshold", cm[1])
		}
		if ts := pollyTimeSpanRe.FindStringSubmatch(chain); ts != nil {
			if secs := pollyTimeSpanSeconds(ts[1], ts[2]); secs != "" {
				setProps(&ent, "break_seconds", secs)
			}
		}
		if h := firstHandled(m[0]); h != "" {
			setProps(&ent, "handled_exception", h)
		}
		add(ent)
	}

	// 3. v7 Policy.Timeout.
	for _, m := range pollyTimeoutRe.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		ent := marker("timeout", "v7", line)
		chain := pollyChain(src, m[0])
		if ts := pollyTimeSpanRe.FindStringSubmatch(chain); ts != nil {
			if secs := pollyTimeSpanSeconds(ts[1], ts[2]); secs != "" {
				setProps(&ent, "timeout_seconds", secs)
			}
		}
		add(ent)
	}

	// 4. v7 Policy.Bulkhead.
	for _, m := range pollyBulkRe.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		ent := marker("bulkhead", "v7", line)
		chain := pollyChain(src, m[0])
		if bm := pollyBulkArgRe.FindStringSubmatch(chain); bm != nil {
			setProps(&ent, "max_parallel", bm[1])
		}
		add(ent)
	}

	// 5. v7 Fallback.
	for _, m := range pollyFallRe.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		ent := marker("fallback", "v7", line)
		if h := firstHandled(m[0]); h != "" {
			setProps(&ent, "handled_exception", h)
		}
		add(ent)
	}

	// 6. v8 ResiliencePipelineBuilder strategies. Only emit when a pipeline
	//    builder is present in-file (the .Add* names are otherwise ambiguous).
	if pollyPipelineRe.MatchString(src) {
		for _, m := range pollyAddStrategyRe.FindAllStringSubmatchIndex(src, -1) {
			strategy := src[m[2]:m[3]]
			line := lineOf(src, m[0])
			resType := pollyV8Strategy[strategy]
			ent := marker(resType, "v8", line)
			chain := pollyChain(src, m[0])
			switch resType {
			case "retry":
				if rm := pollyMaxRetryOptRe.FindStringSubmatch(chain); rm != nil {
					setProps(&ent, "retry_count", rm[1])
				}
			case "circuit_breaker":
				if cm := pollyCBThreshOptRe.FindStringSubmatch(chain); cm != nil {
					setProps(&ent, "break_threshold", cm[1])
				}
				if ts := pollyTimeSpanRe.FindStringSubmatch(chain); ts != nil {
					if secs := pollyTimeSpanSeconds(ts[1], ts[2]); secs != "" {
						setProps(&ent, "break_seconds", secs)
					}
				}
			case "timeout":
				if ts := pollyTimeSpanRe.FindStringSubmatch(chain); ts != nil {
					if secs := pollyTimeSpanSeconds(ts[1], ts[2]); secs != "" {
						setProps(&ent, "timeout_seconds", secs)
					}
				}
			case "bulkhead":
				if bm := pollyMaxParallelOptRe.FindStringSubmatch(chain); bm != nil {
					setProps(&ent, "max_parallel", bm[1])
				}
			}
			add(ent)
		}
	}

	// 7. HttpClientFactory bindings — AddPolicyHandler / AddResilienceHandler on
	//    an AddHttpClient("name") chain attach a resilience policy to the client.
	clientName := func(callOff int) string {
		// Scan back to the statement start and find the AddHttpClient("name").
		start := 0
		if i := strings.LastIndexByte(src[:callOff], ';'); i >= 0 {
			start = i
		}
		seg := src[start:callOff]
		if hm := pollyAddHttpClientRe.FindStringSubmatch(seg); hm != nil {
			return hm[1]
		}
		return ""
	}
	for _, m := range pollyAddPolicyHandlerRe.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		ent := marker("http_client_policy", "v7", line)
		setProps(&ent, "binding", "add_policy_handler")
		if c := clientName(m[0]); c != "" {
			setProps(&ent, "http_client", c)
		}
		add(ent)
	}
	for _, m := range pollyAddResilienceHandlerRe.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		ent := marker("http_client_policy", "v8", line)
		setProps(&ent, "binding", "add_resilience_handler")
		if c := clientName(m[0]); c != "" {
			setProps(&ent, "http_client", c)
		}
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// pollyV8Strategy maps a v8 .Add*(...) strategy token to the resilience_type.
var pollyV8Strategy = map[string]string{
	"Retry":              "retry",
	"CircuitBreaker":     "circuit_breaker",
	"Timeout":            "timeout",
	"Hedging":            "hedging",
	"ConcurrencyLimiter": "bulkhead",
	"RateLimiter":        "rate_limiter",
}
