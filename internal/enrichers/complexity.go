// Package enrichers implements post-extraction entity enrichers ported from
// the Python memx_indexer languages/_engine enrichers.
//
// Enrichers operate on []types.EntityRecord, enriching Metadata and Properties
// fields. They are pure computation — no external API calls.
package enrichers

import (
	"regexp"
	"strings"
)

// complexityKeywords are control-flow constructs counted for cyclomatic complexity.
var complexityKeywords = []*regexp.Regexp{
	regexp.MustCompile(`\bif\b`),
	regexp.MustCompile(`\belse\b`),
	regexp.MustCompile(`\bfor\b`),
	regexp.MustCompile(`\bwhile\b`),
	regexp.MustCompile(`\bswitch\b`),
	regexp.MustCompile(`\bcase\b`),
	regexp.MustCompile(`\bcatch\b`),
	regexp.MustCompile(`\belif\b`),
	regexp.MustCompile(`\bexcept\b`),
	regexp.MustCompile(`\brescue\b`),
	regexp.MustCompile(`\bunless\b`),
	regexp.MustCompile(`\bselect\b`),
	regexp.MustCompile(`\?\s*\w`),
	regexp.MustCompile(`&&`),
	regexp.MustCompile(`\|\|`),
}

var externalCallPatterns = []*regexp.Regexp{
	regexp.MustCompile(`http\.(Get|Post|Put|Delete|Do)\s*\(`),
	regexp.MustCompile(`fetch\s*\(`),
	regexp.MustCompile(`axios\.(get|post|put|delete|patch)\s*\(`),
	regexp.MustCompile(`requests\.(get|post|put|delete|patch)\s*\(`),
	regexp.MustCompile(`grpc\.Dial\s*\(`),
	regexp.MustCompile(`redis\.(Get|Set|Del|Incr)`),
	regexp.MustCompile(`db\.(Query|Exec|QueryRow)\s*\(`),
	regexp.MustCompile(`\.aggregate\s*\(`),
}

var conditionalPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bif\b`),
	regexp.MustCompile(`\bswitch\b`),
	regexp.MustCompile(`\bwhile\b`),
	regexp.MustCompile(`\bfor\b`),
	regexp.MustCompile(`\?\s*\w`),
}

// ComputeCyclomaticComplexity returns the cyclomatic complexity of source.
// Algorithm: 1 + count(control-flow keywords). Matches Python complexity.py.
func ComputeCyclomaticComplexity(source string) int {
	count := 1
	for _, re := range complexityKeywords {
		count += len(re.FindAllString(source, -1))
	}
	return count
}

// HasConditionals returns true when source contains any branching construct.
func HasConditionals(source string) bool {
	for _, re := range conditionalPatterns {
		if re.MatchString(source) {
			return true
		}
	}
	return false
}

// HasExternalCalls returns true when source contains external system call patterns.
func HasExternalCalls(source string) bool {
	for _, re := range externalCallPatterns {
		if re.MatchString(source) {
			return true
		}
	}
	return false
}

// ComputeMaxCallDepth estimates maximum call nesting depth from indentation.
func ComputeMaxCallDepth(source string) int {
	maxDepth := 0
	for _, line := range strings.Split(source, "\n") {
		indent := countLeadingSpaces(line)
		depth := indent / 4
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	if maxDepth > 10 {
		maxDepth = 10
	}
	return maxDepth
}

func countLeadingSpaces(s string) int {
	n := 0
	for _, ch := range s {
		switch ch {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}
