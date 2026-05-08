package patterns

import (
	"fmt"
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// patternRecommendationEnricher detects anti-patterns and recommends improvements.
// Matches Python pattern_recommendation_enricher.py.
type patternRecommendationEnricher struct{}

var (
	// High severity: hard-coded credentials
	prHighCredentialRE = regexp.MustCompile(`(?i)(?:password|passwd|secret|api_key|apikey|token)\s*=\s*["'][^"']{4,}["']`)
	// High: eval/exec usage
	prHighEvalRE = regexp.MustCompile(`\b(?:eval|exec)\s*\(`)
	// Medium: print debugging left in
	prMediumPrintDebugRE = regexp.MustCompile(`(?m)^\s*(?:print|console\.log)\s*\(`)
	// Medium: TODO/FIXME in production code
	prMediumTodoRE = regexp.MustCompile(`(?i)\b(?:TODO|FIXME|HACK)\b`)
)

func (p *patternRecommendationEnricher) Category() string { return "pattern_recommendation" }

func (p *patternRecommendationEnricher) AppliesTo(src string) bool {
	return prHighCredentialRE.MatchString(src) ||
		prHighEvalRE.MatchString(src) ||
		prMediumPrintDebugRE.MatchString(src) ||
		prMediumTodoRE.MatchString(src)
}

func (p *patternRecommendationEnricher) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, name, severity, recommendation string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			name, "SCOPE.Pattern", "pattern_recommendation", language, line,
			map[string]string{
				"kind":           "pattern_recommendation",
				"severity":       severity,
				"recommendation": recommendation,
			}))
	}

	// Hard-coded credentials (high)
	if m := prHighCredentialRE.FindStringIndex(src); m != nil {
		emit("high:credentials", "hardcoded_credential", "high", "use_env_vars", lineOf(src, m[0]))
	}

	// eval/exec (high)
	for _, m := range prHighEvalRE.FindAllStringIndex(src, -1) {
		key := fmt.Sprintf("high:eval:%d", lineOf(src, m[0]))
		emit(key, "eval_usage", "high", "avoid_eval", lineOf(src, m[0]))
		break // one per file
	}

	// Print debugging (medium)
	count := len(prMediumPrintDebugRE.FindAllString(src, -1))
	if count > 0 {
		emit("medium:print_debug", "print_debug_statements", "medium", "remove_debug_prints", 1)
	}

	// TODO/FIXME (medium)
	if m := prMediumTodoRE.FindStringIndex(src); m != nil {
		emit("medium:todo", "todo_fixme_markers", "medium", "resolve_todos", lineOf(src, m[0]))
	}

	return results
}

func init() {
	Register(&patternRecommendationEnricher{})
}
