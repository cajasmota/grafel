package patterns

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// propertyTestDetector detects property-based testing patterns.
// Matches Python property_test_detector.py.
type propertyTestDetector struct{}

var (
	ptdPyHypothesisRE   = regexp.MustCompile(`@(?:hypothesis\.)?(?:given|settings|example)\s*\(`)
	ptdJSFastCheckRE    = regexp.MustCompile(`(?:fc\.|fastcheck\.)(?:property|assert|check)\s*\(`)
	ptdJavaJqwikRE      = regexp.MustCompile(`@(?:Property|ForAll|net\.jqwik)\b`)
	ptdKotlinKotestRE   = regexp.MustCompile(`(?:checkAll|forAll|Arb\.)\s*\{`)
	ptdScalaCheckRE     = regexp.MustCompile(`forAll\s*\{|Gen\.\w+`)
	ptdRustProptest     = regexp.MustCompile(`proptest!\s*\{`)
	ptdGoQuickCheckRE   = regexp.MustCompile(`quick\.Check\s*\(`)
)

func (p *propertyTestDetector) Category() string { return "property_test" }

func (p *propertyTestDetector) AppliesTo(src string) bool {
	return ptdPyHypothesisRE.MatchString(src) ||
		ptdJSFastCheckRE.MatchString(src) ||
		ptdJavaJqwikRE.MatchString(src) ||
		ptdKotlinKotestRE.MatchString(src) ||
		ptdScalaCheckRE.MatchString(src) ||
		ptdRustProptest.MatchString(src) ||
		ptdGoQuickCheckRE.MatchString(src) ||
		strings.Contains(src, "hypothesis") ||
		strings.Contains(src, "fast-check")
}

func (p *propertyTestDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, library string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"property_test_"+library, "SCOPE.Pattern", "property_test", language, line,
			map[string]string{"kind": "property_test", "library": library}))
	}

	if m := ptdPyHypothesisRE.FindStringIndex(src); m != nil {
		emit("py:hypothesis", "hypothesis", lineOf(src, m[0]))
	}
	if m := ptdJSFastCheckRE.FindStringIndex(src); m != nil {
		emit("js:fastcheck", "fast-check", lineOf(src, m[0]))
	}
	if m := ptdJavaJqwikRE.FindStringIndex(src); m != nil {
		emit("java:jqwik", "jqwik", lineOf(src, m[0]))
	}
	if m := ptdKotlinKotestRE.FindStringIndex(src); m != nil {
		emit("kotlin:kotest", "kotest-property", lineOf(src, m[0]))
	}
	if m := ptdScalaCheckRE.FindStringIndex(src); m != nil {
		emit("scala:scalacheck", "scalacheck", lineOf(src, m[0]))
	}
	if m := ptdRustProptest.FindStringIndex(src); m != nil {
		emit("rust:proptest", "proptest", lineOf(src, m[0]))
	}
	if m := ptdGoQuickCheckRE.FindStringIndex(src); m != nil {
		emit("go:quickcheck", "go-check", lineOf(src, m[0]))
	}

	return results
}

func init() {
	Register(&propertyTestDetector{})
}
