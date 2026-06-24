// Package testmap — Elm test-framework (elm-test) detection and call resolution.
//
// #5375 (epic #5360 Group A — bootstrap). Linkage for elm-test
// (https://github.com/elm-explorations/test), the standard Elm test runner:
//
//	import Test exposing (..)
//	import Expect
//
//	suite : Test
//	suite =
//	    describe "Math"
//	        [ test "adds two numbers" <|
//	            \_ -> Expect.equal 4 (add 2 2)
//	        , fuzz int "is commutative" <|
//	            \n -> Expect.equal (add n 1) (add 1 n)
//	        ]
//
// Each `test "..."` / `fuzz* "..."` leaf is a test case. Its body — the
// `<| \_ -> …` thunk that follows — is scanned by the shared resolver for direct
// production calls (`add 2 2` → the `add` operation). The enclosing
// `describe "Subject"` is carried as a naming-convention subject-under-test
// fallback (the Elm analog of the Nim `suite` / RSpec `describe` subject path),
// used only when the subject is identifier-shaped.
//
// Elm test bodies are delimited by the off-side rule (top-level declarations
// start at column 0) AND by the surrounding `[ … ]` list / `( … )` group. A
// `test`/`fuzz` leaf header (`test "desc" <|`) introduces a body that runs until
// the next `, test`/`, fuzz` list element, the closing `]`, or a dedent to
// column 0 — whichever comes first. extractElmTestBody captures that run,
// quote-aware so a `test` keyword inside a string never opens a block. The
// elm-test / Expect / Fuzz DSL (Expect.*, Fuzz.*, describe/test/fuzz combinators)
// is denylisted in resolver.go so it never surfaces as the production subject.
//
// elm-test files use `import Test`/`import Expect`; the framework entry is
// IMPORT-token gated on those plus the `tests/` path / `*Test.elm` filename
// conventions, and the detector self-confirms (a non-test .elm yields zero cases
// and is dropped downstream, like the rust_test / fsharp-expecto entries).
package testmap

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// elm-test — describe "..." [ test "..." <| \_ -> … ]
// ---------------------------------------------------------------------------

// elmTestCaseRE matches an elm-test leaf case header:
//
//	test "adds two numbers" <|
//	fuzz int "is commutative" <|
//	fuzz2 int int "is associative" <|
//	test "trims" <|              (the `<|` may sit on the next line, see below)
//
// The description string is the LAST string literal on the header (fuzz* leaves
// carry a fuzzer argument BEFORE the description). Group 1 captures the leading
// keyword (test / fuzz / fuzz2 / fuzz3 / fuzzWith); the description is captured by
// elmTestDescRE applied to the matched header line.
var elmTestCaseRE = regexp.MustCompile(
	`(?m)^[ \t]*[\[,]?[ \t]*(test|fuzz[0-9]?|fuzzWith)\s+[^\n\r]*?"([^"\n\r]{1,200})"\s*(?:<\|)?[ \t]*$`,
)

// elmDescribeRE matches an elm-test container: `describe "Subject"`. The first
// describe whose subject is identifier-shaped seeds the subject-under-test
// fallback (mirrors nimUnittestSuiteRE / fsharpExpectoListRE).
var elmDescribeRE = regexp.MustCompile(
	`(?m)^[ \t]*describe\s+"([^"\n\r]{1,200})"`,
)

// elmSubjectIdentRE recognises a describe subject that names a code symbol
// (CamelCase / dotted / lower-ident), so a prose subject ("returns 200 on GET")
// is rejected.
var elmSubjectIdentRE = regexp.MustCompile(`^[A-Za-z_][\w]*(?:[.][A-Za-z_][\w]*)*$`)

// elmDescribeSubject returns the first identifier-shaped describe subject, or "".
func elmDescribeSubject(source string) string {
	for _, m := range elmDescribeRE.FindAllStringSubmatch(source, -1) {
		subj := strings.TrimSpace(m[1])
		if elmSubjectIdentRE.MatchString(subj) {
			if idx := strings.LastIndexByte(subj, '.'); idx >= 0 {
				if tail := subj[idx+1:]; tail != "" {
					return tail
				}
			}
			return subj
		}
	}
	return ""
}

// extractElmTestBody returns the source slice of the test-leaf body whose header
// line begins at byte offset headerStart. The body is the run of subsequent lines
// up to (but excluding) the next list element — a line whose first non-space
// character is a `,` introducing the next `test`/`fuzz`, the closing `]` of the
// describe list, or a dedent to column 0 (the next top-level declaration). The
// header line itself is included so an inline `\_ -> Expect.equal 4 (add 2 2)`
// body is captured. Quote-/comment-awareness is handled by the shared resolver's
// scrub; here we only need line-structural bounds.
func extractElmTestBody(source string, headerStart int) string {
	nl := strings.IndexByte(source[headerStart:], '\n')
	bodyStart := headerStart
	if nl < 0 {
		return source[headerStart:]
	}
	headerLine := source[headerStart : headerStart+nl]
	headerIndent := indentWidth(headerLine)

	i := headerStart + nl + 1
	n := len(source)
	for i < n {
		lineEnd := strings.IndexByte(source[i:], '\n')
		var line string
		if lineEnd < 0 {
			line = source[i:]
		} else {
			line = source[i : i+lineEnd]
		}
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			ind := indentWidth(line)
			// Next list element (`, test …` / `, fuzz …`) at or below the header
			// indent ends this body. A leading `]` (list close) or a dedent to
			// column 0 (next top-level decl) also ends it.
			if ind <= headerIndent && (strings.HasPrefix(trimmed, ",") || strings.HasPrefix(trimmed, "]")) {
				return source[bodyStart:i]
			}
			if ind == 0 {
				return source[bodyStart:i]
			}
		}
		if lineEnd < 0 {
			return source[bodyStart:]
		}
		i += lineEnd + 1
	}
	return source[bodyStart:]
}

// elmTestCaseName normalises a `test "does a thing"` description into a snake
// case-ish identifier. Reuses the Nim normaliser (a pure identifier helper).
func elmTestCaseName(desc string) string { return nimTestCaseName(desc) }

func detectElmTest(source string) []testFunction {
	subject := elmDescribeSubject(source)

	var out []testFunction
	seen := map[string]bool{}
	for _, m := range elmTestCaseRE.FindAllStringSubmatchIndex(source, -1) {
		// Group 2 (m[4]:m[5]) is the description string.
		if m[4] < 0 || m[5] < 0 {
			continue
		}
		desc := source[m[4]:m[5]]
		name := elmTestCaseName(desc)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		body := extractElmTestBody(source, m[0])
		out = append(out, testFunction{qname: name, body: body, describeSubject: subject, lang: "elm"})
	}
	return out
}
