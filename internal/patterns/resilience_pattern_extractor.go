package patterns

import (
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// resiliencePatternExtractor detects circuit breakers, retry, bulkhead patterns.
// Matches Python resilience_pattern_extractor.py.
type resiliencePatternExtractor struct{}

var (
	rpR4JTriggerRE      = regexp.MustCompile(`(?:io\.github\.resilience4j|@CircuitBreaker|@Retry|@Bulkhead|@RateLimiter)`)
	rpPollyTriggerRE    = regexp.MustCompile(`(?:Polly\.|Policy\.Handle|WaitAndRetry|CircuitBreaker)`)
	rpGoGobreakerTrigger = regexp.MustCompile(`gobreaker\.`)
	rpGoHystrixTrigger  = regexp.MustCompile(`hystrix-go`)
	rpR4JAnnotRE        = regexp.MustCompile(`@(CircuitBreaker|Retry|Bulkhead|RateLimiter|TimeLimiter)\s*\(\s*name\s*=\s*["']([^"']+)["']`)
	rpHystrixCmdRE      = regexp.MustCompile(`hystrix\.HystrixCommand\b`)
	rpPollyCallRE       = regexp.MustCompile(`Policy\.(Handle|WrapAsync|Wrap)\s*\(`)
	rpGobreakerRE       = regexp.MustCompile(`gobreaker\.NewCircuitBreaker\s*\(`)
	rpHystrixGoRE       = regexp.MustCompile(`hystrix\.Command\s*\{`)
)

func (r *resiliencePatternExtractor) Category() string { return "resilience_pattern" }

func (r *resiliencePatternExtractor) AppliesTo(src string) bool {
	return rpR4JTriggerRE.MatchString(src) ||
		rpPollyTriggerRE.MatchString(src) ||
		rpGoGobreakerTrigger.MatchString(src) ||
		rpGoHystrixTrigger.MatchString(src)
}

func (r *resiliencePatternExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, name, pattern, framework string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			name, "SCOPE.Pattern", "resilience_pattern", language, line,
			map[string]string{"kind": "resilience_pattern", "pattern": pattern, "framework": framework}))
	}

	// Resilience4j annotations
	for _, m := range rpR4JAnnotRE.FindAllStringSubmatchIndex(src, -1) {
		annotName := src[m[2]:m[3]]
		instName := src[m[4]:m[5]]
		emit("r4j:"+annotName+":"+instName,
			"resilience_r4j_"+annotName+"_"+instName,
			annotName, "resilience4j",
			lineOf(src, m[0]))
	}

	// Polly
	if m := rpPollyCallRE.FindStringIndex(src); m != nil {
		emit("polly:policy", "resilience_polly", "polly_policy", "polly", lineOf(src, m[0]))
	}

	// Go gobreaker
	if m := rpGobreakerRE.FindStringIndex(src); m != nil {
		emit("go:gobreaker", "resilience_gobreaker", "circuit_breaker", "gobreaker", lineOf(src, m[0]))
	}

	// Go hystrix-go
	if m := rpHystrixGoRE.FindStringIndex(src); m != nil {
		emit("go:hystrix", "resilience_hystrix_go", "circuit_breaker", "hystrix-go", lineOf(src, m[0]))
	}

	// Java Hystrix
	if m := rpHystrixCmdRE.FindStringIndex(src); m != nil {
		emit("java:hystrix", "resilience_hystrix_java", "circuit_breaker", "hystrix", lineOf(src, m[0]))
	}

	return results
}

func init() {
	Register(&resiliencePatternExtractor{})
}
