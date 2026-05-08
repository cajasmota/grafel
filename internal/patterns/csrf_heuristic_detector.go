package patterns

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// csrfHeuristicDetector detects CSRF protection or absence patterns.
// Matches Python csrf_heuristic_detector.py.
type csrfHeuristicDetector struct{}

var (
	csrfSpringDisabledRE    = regexp.MustCompile(`\.csrf\s*\(\s*\)\s*\.\s*disable\s*\(\s*\)`)
	csrfNestJSRE            = regexp.MustCompile(`csurf\s*\(`)
	csrfDjangoExemptRE      = regexp.MustCompile(`@csrf_exempt`)
csrfDjangoCookieSecRE   = regexp.MustCompile(`CSRF_COOKIE_SECURE\s*=\s*True`)
	csrfRailsProtectRE      = regexp.MustCompile(`protect_from_forgery`)
	csrfRailsSkipRE         = regexp.MustCompile(`skip_before_action\s*:verify_authenticity_token`)
	csrfLaravelVerifyRE     = regexp.MustCompile(`VerifyCsrfToken`)
)


func (c *csrfHeuristicDetector) Category() string { return "csrf_policy" }

func (c *csrfHeuristicDetector) AppliesTo(src string) bool {
	srcLower := strings.ToLower(src)
	return strings.Contains(srcLower, "csrf") ||
		csrfRailsProtectRE.MatchString(src) ||
		csrfLaravelVerifyRE.MatchString(src)
}

func (c *csrfHeuristicDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, name, csrfKind string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			name, "SCOPE.Pattern", "csrf_policy", language, line,
			map[string]string{"kind": "csrf_policy", "csrf_kind": csrfKind}))
	}

	// Spring: .csrf().disable()
	if m := csrfSpringDisabledRE.FindStringIndex(src); m != nil {
		emit("spring:disabled", "csrf_disabled_spring", "disabled", lineOf(src, m[0]))
	}

	// NestJS / Express: csurf()
	if m := csrfNestJSRE.FindStringIndex(src); m != nil {
		emit("nestjs:csurf", "csrf_enabled_csurf", "enabled", lineOf(src, m[0]))
	}

	// Django: @csrf_exempt
	for _, m := range csrfDjangoExemptRE.FindAllStringIndex(src, -1) {
		emit("django:exempt", "csrf_exempt_django", "exempt", lineOf(src, m[0]))
	}

	// Django: CSRF_COOKIE_SECURE = True
	if csrfDjangoCookieSecRE.MatchString(src) {
		emit("django:cookie_secure", "csrf_cookie_secure_django", "cookie_secure", 1)
	}

	// Rails: protect_from_forgery
	if m := csrfRailsProtectRE.FindStringIndex(src); m != nil {
		emit("rails:protect", "csrf_enabled_rails", "enabled", lineOf(src, m[0]))
	}

	// Rails: skip_before_action :verify_authenticity_token
	if m := csrfRailsSkipRE.FindStringIndex(src); m != nil {
		emit("rails:skip", "csrf_skipped_rails", "skipped", lineOf(src, m[0]))
	}

	// Laravel: VerifyCsrfToken
	if m := csrfLaravelVerifyRE.FindStringIndex(src); m != nil {
		emit("laravel:verify", "csrf_enabled_laravel", "enabled", lineOf(src, m[0]))
	}

	return results
}

func init() {
	Register(&csrfHeuristicDetector{})
}
