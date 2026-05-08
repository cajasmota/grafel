package patterns

import (
	"fmt"
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// errorHandlingDetector detects error handling patterns across languages.
// Matches Python error_handling_detector.py.
type errorHandlingDetector struct{}

var (
	ehTryStatementRE    = regexp.MustCompile(`\btry\s*\{`)
	ehPyTryStatementRE  = regexp.MustCompile(`(?m)^\s*try\s*:`)
	ehGoErrNilRE        = regexp.MustCompile(`if\s+err\s*!=\s*nil`)
	ehRustMatchRE       = regexp.MustCompile(`\bmatch\b`)
	ehRustOkArmRE       = regexp.MustCompile(`\bOk\s*\(`)
	ehRustErrArmRE      = regexp.MustCompile(`\bErr\s*\(`)
	ehRustQuestionMarkRE = regexp.MustCompile(`\?\s*;?\s*\n`)
	ehElixirCaseRE      = regexp.MustCompile(`\bcase\b`)
	ehElixirOkArmRE     = regexp.MustCompile(`\{:ok\s*,`)
	ehElixirErrorArmRE  = regexp.MustCompile(`\{:error\s*,`)
	ehPanicRecoverRE    = regexp.MustCompile(`\brecover\b|\bpanic\s*\(`)
)

func (e *errorHandlingDetector) Category() string { return "error_handling" }

func (e *errorHandlingDetector) AppliesTo(src string) bool {
	return ehTryStatementRE.MatchString(src) ||
		ehPyTryStatementRE.MatchString(src) ||
		ehGoErrNilRE.MatchString(src) ||
		ehRustMatchRE.MatchString(src) ||
		ehElixirOkArmRE.MatchString(src)
}

func (e *errorHandlingDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord

	switch language {
	case "go":
		// Per-line entities for each `if err != nil` occurrence.
		for _, m := range ehGoErrNilRE.FindAllStringIndex(src, -1) {
			line := lineOf(src, m[0])
			name := fmt.Sprintf("error_handling:go_error_return:%d", line)
			results = append(results, makeEntity(filePath,
				name, "SCOPE.Pattern", "error_handling", language, line,
				map[string]string{"kind": "error_handling", "pattern": "go_error_return"}))
		}
		if ehPanicRecoverRE.MatchString(src) {
			for _, m := range ehPanicRecoverRE.FindAllStringIndex(src, -1) {
				line := lineOf(src, m[0])
				name := fmt.Sprintf("error_handling:panic_recover:%d", line)
				results = append(results, makeEntity(filePath,
					name, "SCOPE.Pattern", "error_handling", language, line,
					map[string]string{"kind": "error_handling", "pattern": "panic_recover"}))
			}
		}

	case "python":
		// Per-line entities for each `try:` occurrence — matches Python's format.
		for _, m := range ehPyTryStatementRE.FindAllStringIndex(src, -1) {
			line := lineOf(src, m[0])
			name := fmt.Sprintf("error_handling:try_catch:%d", line)
			results = append(results, makeEntity(filePath,
				name, "SCOPE.Pattern", "error_handling", language, line,
				map[string]string{"kind": "error_handling", "pattern": "try_catch"}))
		}

	case "rust":
		if ehRustMatchRE.MatchString(src) && ehRustOkArmRE.MatchString(src) && ehRustErrArmRE.MatchString(src) {
			for _, m := range ehRustMatchRE.FindAllStringIndex(src, -1) {
				line := lineOf(src, m[0])
				name := fmt.Sprintf("error_handling:rust_match_result:%d", line)
				results = append(results, makeEntity(filePath,
					name, "SCOPE.Pattern", "error_handling", language, line,
					map[string]string{"kind": "error_handling", "pattern": "match_result"}))
			}
		}
		if ehRustQuestionMarkRE.MatchString(src) {
			for _, m := range ehRustQuestionMarkRE.FindAllStringIndex(src, -1) {
				line := lineOf(src, m[0])
				name := fmt.Sprintf("error_handling:rust_question_mark:%d", line)
				results = append(results, makeEntity(filePath,
					name, "SCOPE.Pattern", "error_handling", language, line,
					map[string]string{"kind": "error_handling", "pattern": "question_mark_operator"}))
			}
		}

	case "elixir":
		if ehElixirCaseRE.MatchString(src) && ehElixirOkArmRE.MatchString(src) && ehElixirErrorArmRE.MatchString(src) {
			for _, m := range ehElixirCaseRE.FindAllStringIndex(src, -1) {
				line := lineOf(src, m[0])
				name := fmt.Sprintf("error_handling:elixir_ok_error:%d", line)
				results = append(results, makeEntity(filePath,
					name, "SCOPE.Pattern", "error_handling", language, line,
					map[string]string{"kind": "error_handling", "pattern": "case_ok_error"}))
			}
		}

	case "dart", "swift", "shell", "proto", "protobuf":
		// These languages use try/catch but the Python indexer does not emit
		// error_handling pattern entities for them. Suppress to maintain parity.

	default:
		// Curly-brace try { } — per-line for JS/Java/etc.
		for _, m := range ehTryStatementRE.FindAllStringIndex(src, -1) {
			line := lineOf(src, m[0])
			name := fmt.Sprintf("error_handling:try_catch:%d", line)
			results = append(results, makeEntity(filePath,
				name, "SCOPE.Pattern", "error_handling", language, line,
				map[string]string{"kind": "error_handling", "pattern": "try_catch"}))
		}
	}

	return results
}

func init() {
	Register(&errorHandlingDetector{})
}
