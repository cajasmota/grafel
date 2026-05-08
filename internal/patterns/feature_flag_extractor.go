package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// featureFlagExtractor detects feature flag evaluations.
// Matches Python feature_flag_extractor.py.
type featureFlagExtractor struct{}

var ffImportTokens = []string{
	"launchdarkly", "unleash", "flagsmith", "flipt", "split", "statsig",
	"featureflags", "feature-flags", "PostHog", "GrowthBook",
}

var (
	ffSourcePatternRE   = regexp.MustCompile(`(?:featureFlags\.isEnabled|isFeatureEnabled|getFlag)\s*\(`)
	ffLDVariationRE     = regexp.MustCompile(`\bldClient\s*\.\s*\w*[Vv]ariation\s*\(\s*["']([^"']+)["']`)
	ffLDUseFlagsRE      = regexp.MustCompile(`\buseFlags\s*\(\s*\)\s*\[\s*["']([^"']+)["']\s*\]`)
	ffUnleashEnabledRE  = regexp.MustCompile(`isEnabled\s*\(\s*["']([^"']+)["']`)
	ffCustomIsEnabledRE = regexp.MustCompile(`isFeatureEnabled\s*\(\s*["']([^"']+)["']`)
	ffCustomGetFlagRE   = regexp.MustCompile(`getFlag\s*\(\s*["']([^"']+)["']`)
)

func (f *featureFlagExtractor) Category() string { return "feature_flag" }

func (f *featureFlagExtractor) AppliesTo(src string) bool {
	srcLower := strings.ToLower(src)
	for _, tok := range ffImportTokens {
		if strings.Contains(srcLower, strings.ToLower(tok)) {
			return true
		}
	}
	return ffSourcePatternRE.MatchString(src) ||
		ffLDVariationRE.MatchString(src) ||
		ffUnleashEnabledRE.MatchString(src) ||
		ffCustomIsEnabledRE.MatchString(src) ||
		ffCustomGetFlagRE.MatchString(src)
}

func (f *featureFlagExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(flagName, sdk string, line int) {
		key := fmt.Sprintf("%s:%s", sdk, flagName)
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"feature_flag_"+flagName, "SCOPE.Pattern", "feature_flag", language, line,
			map[string]string{"kind": "feature_flag", "flag_name": flagName, "sdk": sdk}))
	}

	for _, m := range ffLDVariationRE.FindAllStringSubmatchIndex(src, -1) {
		emit(src[m[2]:m[3]], "launchdarkly", lineOf(src, m[0]))
	}
	for _, m := range ffLDUseFlagsRE.FindAllStringSubmatchIndex(src, -1) {
		emit(src[m[2]:m[3]], "launchdarkly-react", lineOf(src, m[0]))
	}
	for _, m := range ffUnleashEnabledRE.FindAllStringSubmatchIndex(src, -1) {
		emit(src[m[2]:m[3]], "unleash", lineOf(src, m[0]))
	}
	for _, m := range ffCustomIsEnabledRE.FindAllStringSubmatchIndex(src, -1) {
		emit(src[m[2]:m[3]], "custom", lineOf(src, m[0]))
	}
	for _, m := range ffCustomGetFlagRE.FindAllStringSubmatchIndex(src, -1) {
		emit(src[m[2]:m[3]], "custom", lineOf(src, m[0]))
	}

	return results
}

func init() {
	Register(&featureFlagExtractor{})
}
