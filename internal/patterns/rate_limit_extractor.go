package patterns

import (
	"fmt"
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// rateLimitExtractor detects rate-limiting middleware and decorators.
// Matches Python rate_limit_extractor.py.
type rateLimitExtractor struct{}

var (
	rateLimitExpressTriggerRE     = regexp.MustCompile(`(?:rateLimit\s*\(|['"]express-rate-limit['"])`)
	rateLimitDjangoTriggerRE      = regexp.MustCompile(`(?:throttle_classes\s*=|@throttle_classes\s*\(|from\s+rest_framework\.throttling\s+import)`)
	rateLimitSpringTriggerRE      = regexp.MustCompile(`@(?:RateLimiter|Bucket4j)\s*[(@]`)
	rateLimitASPNetTriggerRE      = regexp.MustCompile(`(?:app\.UseRateLimiting|services\.AddRateLimiter|builder\.Services\.AddRateLimiter)`)
	rateLimitGoRateTriggerRE      = regexp.MustCompile(`(?:"golang\.org/x/time/rate"|rate\.NewLimiter)`)
	rateLimitFlaskLimiterTriggerRE = regexp.MustCompile(`(?:@limiter\.limit\s*\(|Limiter\s*\()`)
	rateLimitExpressRE            = regexp.MustCompile(`rateLimit\s*\(\s*\{[^}]*\}`)
	rateLimitDjangoThrottleRE     = regexp.MustCompile(`throttle_classes\s*=\s*\[([^\]]*)\]`)
	rateLimitSpringAnnotRE        = regexp.MustCompile(`@RateLimiter\s*\(\s*name\s*=\s*["']([^"']+)["']`)
	rateLimitFlaskLimitRE         = regexp.MustCompile(`@limiter\.limit\s*\(\s*["']([^"']+)["']`)
)

func (r *rateLimitExtractor) Category() string { return "rate_limit" }

func (r *rateLimitExtractor) AppliesTo(src string) bool {
	return rateLimitExpressTriggerRE.MatchString(src) ||
		rateLimitDjangoTriggerRE.MatchString(src) ||
		rateLimitSpringTriggerRE.MatchString(src) ||
		rateLimitASPNetTriggerRE.MatchString(src) ||
		rateLimitGoRateTriggerRE.MatchString(src) ||
		rateLimitFlaskLimiterTriggerRE.MatchString(src)
}

func (r *rateLimitExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, name, framework, algorithm string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath, name, "SCOPE.Pattern", "rate_limit", language, line,
			map[string]string{"kind": "rate_limit", "framework": framework, "algorithm": algorithm}))
	}

	// Express rate limit
	for idx, m := range rateLimitExpressRE.FindAllStringIndex(src, -1) {
		emit(fmt.Sprintf("express:%d", idx),
			fmt.Sprintf("rate_limit_express_%d", idx), "express-rate-limit", "sliding_window",
			lineOf(src, m[0]))
	}
	if rateLimitExpressTriggerRE.MatchString(src) && len(rateLimitExpressRE.FindAllString(src, -1)) == 0 {
		emit("express:0", "rate_limit_express", "express-rate-limit", "sliding_window", 1)
	}

	// Django throttle
	for idx, m := range rateLimitDjangoThrottleRE.FindAllStringSubmatchIndex(src, -1) {
		emit(fmt.Sprintf("django:%d", idx),
			fmt.Sprintf("rate_limit_django_%d", idx), "django", "fixed_window",
			lineOf(src, m[0]))
	}

	// Spring @RateLimiter
	for _, m := range rateLimitSpringAnnotRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		emit(fmt.Sprintf("spring:%s", name),
			fmt.Sprintf("rate_limit_spring_%s", name), "spring", "token_bucket",
			lineOf(src, m[0]))
	}

	// ASP.NET
	if rateLimitASPNetTriggerRE.MatchString(src) {
		emit("aspnet:0", "rate_limit_aspnet", "aspnet", "fixed_window", 1)
	}

	// Go rate
	if rateLimitGoRateTriggerRE.MatchString(src) {
		emit("go-rate:0", "rate_limit_go", "go-rate", "token_bucket", 1)
	}

	// Flask-Limiter
	for idx, m := range rateLimitFlaskLimitRE.FindAllStringSubmatchIndex(src, -1) {
		limit := src[m[2]:m[3]]
		emit(fmt.Sprintf("flask:%s", limit),
			fmt.Sprintf("rate_limit_flask_%d", idx), "flask-limiter", "sliding_window",
			lineOf(src, m[0]))
	}

	return results
}

func init() {
	Register(&rateLimitExtractor{})
}
