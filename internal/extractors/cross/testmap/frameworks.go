// Package testmap — per-framework test-function detection and call resolution.
//
// Each framework entry declares:
//   - Name (stored on SCOPE.Pattern.properties["test_framework"])
//   - Import markers / file-name sentinels that qualify the file as a test file
//   - A detector that returns the list of test functions in the source along
//     with each function's body so the caller can scan for production calls.
package testmap

import (
	"path/filepath"
	"regexp"
	"strings"
)

// testFunction is an internal record describing a single test case found
// inside a test file.
type testFunction struct {
	qname string // fully qualified test function name
	body  string // textual body used for call/mock scanning
}

// testedCall is an internal record describing a single (test function,
// production function) mapping the resolver produced.
type testedCall struct {
	qname      string // qualified name of the production function under test
	confidence string // high | medium | low
	prodFile   string // best-guess production file path (may be empty)
}

// frameworkDetector scans source code for test functions.
type frameworkDetector func(source string) []testFunction

// frameworkEntry describes a single test framework / convention.
type frameworkEntry struct {
	name          string
	importHints   []string          // substring match against import token set
	filenameHints []*regexp.Regexp  // alternative: filename-only detection (matched against basename)
	pathHints     []*regexp.Regexp  // alternative: full-path detection (matched against slash-normalised full path)
	detect        frameworkDetector // returns all test functions found in the file
}

// ---------------------------------------------------------------------------
// Import list extraction — shared with the endpoint extractor's pattern.
// ---------------------------------------------------------------------------

// importTokenRE captures common import/require tokens across languages.
var importTokenRE = regexp.MustCompile(
	`(?mi)(?:import|from|require|use|using|package)\s+["']?([\w@][\w\-./:]*)["']?`,
)

// importCallRE captures function-style imports: `require('x')` / `import('x')`.
var importCallRE = regexp.MustCompile(
	`(?mi)\b(?:require|import)\s*\(\s*["']([\w@][\w\-./:]*)["']\s*\)`,
)

// extractImportTokens returns the lower-cased set of import tokens in source.
func extractImportTokens(source string) map[string]bool {
	out := map[string]bool{}
	add := func(raw string) {
		if raw == "" {
			return
		}
		tok := strings.ToLower(raw)
		out[tok] = true
		if idx := strings.IndexAny(tok, "/."); idx > 0 {
			out[tok[:idx]] = true
		}
	}
	for _, m := range importTokenRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 2 {
			add(m[1])
		}
	}
	for _, m := range importCallRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 2 {
			add(m[1])
		}
	}
	return out
}

// matchesAnyImport reports whether any hint appears in the token set
// (substring match tolerant to nested import paths).
func matchesAnyImport(tokens map[string]bool, hints []string) bool {
	for _, h := range hints {
		hLower := strings.ToLower(h)
		if tokens[hLower] {
			return true
		}
		for t := range tokens {
			if strings.Contains(t, hLower) {
				return true
			}
		}
	}
	return false
}

// matchesAnyFilename reports whether the base name of path matches any of the
// provided filename regexes.
func matchesAnyFilename(path string, patterns []*regexp.Regexp) bool {
	base := filepath.Base(path)
	for _, re := range patterns {
		if re.MatchString(base) {
			return true
		}
	}
	return false
}

// matchesAnyPath reports whether the slash-normalised full path matches any of
// the provided path regexes. Unlike matchesAnyFilename, patterns here run
// against the entire repo-relative path, so directory-segment matches like
// `/tests/` work regardless of the file's basename.
func matchesAnyPath(path string, patterns []*regexp.Regexp) bool {
	// Normalise to forward slashes and ensure a leading "/" so that a pattern
	// like "/tests/" matches at the start of the path too.
	norm := "/" + filepath.ToSlash(path)
	for _, re := range patterns {
		if re.MatchString(norm) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Body-capture helper — used by every brace-delimited language.
// ---------------------------------------------------------------------------

// extractBraceBody returns the substring of source starting at the first `{`
// at or after startAt and ending at the matching `}` (balanced). When parsing
// fails it returns an empty string — callers fall back to naming convention.
func extractBraceBody(source string, startAt int) string {
	n := len(source)
	i := startAt
	for i < n && source[i] != '{' {
		i++
	}
	if i >= n {
		return ""
	}
	depth := 0
	start := i
	for i < n {
		c := source[i]
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return source[start : i+1]
			}
		case '"', '\'', '`':
			quote := c
			i++
			for i < n && source[i] != quote {
				if source[i] == '\\' {
					i += 2
					continue
				}
				i++
			}
		}
		i++
	}
	return ""
}

// ---------------------------------------------------------------------------
// Python body capture — indentation based.
// ---------------------------------------------------------------------------

// extractIndentedBody returns all lines following startAt that are indented
// more than the header line's column. Used for Python test function bodies.
func extractIndentedBody(source string, headerStart int) string {
	// Find the newline that ends the header line.
	nlIdx := strings.IndexByte(source[headerStart:], '\n')
	if nlIdx < 0 {
		return ""
	}
	bodyStart := headerStart + nlIdx + 1

	// Compute the header line's leading indent.
	headerLineStart := strings.LastIndexByte(source[:headerStart], '\n') + 1
	headerLine := source[headerLineStart:headerStart]
	headerIndent := leadingWhitespaceWidth(headerLine)

	// Accumulate lines whose leading whitespace exceeds headerIndent or whose
	// contents are blank (blank lines do not terminate a Python block).
	lines := strings.Split(source[bodyStart:], "\n")
	var out []string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			out = append(out, line)
			continue
		}
		if leadingWhitespaceWidth(line) > headerIndent {
			out = append(out, line)
			continue
		}
		break
	}
	return strings.Join(out, "\n")
}

func leadingWhitespaceWidth(s string) int {
	w := 0
	for _, r := range s {
		switch r {
		case ' ':
			w++
		case '\t':
			w += 8
		default:
			return w
		}
	}
	return w
}

// ---------------------------------------------------------------------------
// Go — go testing
// ---------------------------------------------------------------------------

var goTestFuncRE = regexp.MustCompile(
	`(?m)^\s*func\s+(Test\w+)\s*\(\s*\w+\s+\*testing\.T\s*\)\s*{`,
)

// goTestSuiteMethodRE matches testify suite receiver-method test functions of
// the form: func (s *MySuite) TestFoo() {
// The receiver type name is captured in group 1, the test name in group 2.
var goTestSuiteMethodRE = regexp.MustCompile(
	`(?m)^\s*func\s+\(\s*\w+\s+\*(\w+)\s*\)\s+(Test\w+)\s*\([^)]*\)\s*{`,
)

// goSuiteEmbedRE detects whether a named struct embeds suite.Suite from the
// testify package. It matches: suite.Suite as a field in the struct body.
// We use a simple source-level search rather than full AST parsing.
var goSuiteEmbedRE = regexp.MustCompile(
	`(?m)\bsuite\.Suite\b`,
)

// isSuiteStruct reports whether structName appears to be a testify suite struct
// by checking whether the source contains a struct definition for structName
// that embeds suite.Suite.
func isSuiteStruct(source, structName string) bool {
	// Fast path: source must reference suite.Suite at all.
	if !goSuiteEmbedRE.MatchString(source) {
		return false
	}
	// Build a regex: type <structName> struct { ... suite.Suite ... }
	// We accept any ordering / whitespace between the struct open brace and the
	// embed, covering single-field and multi-field structs.
	structRE := regexp.MustCompile(
		`(?ms)\btype\s+` + regexp.QuoteMeta(structName) + `\s+struct\s*\{[^}]*\bsuite\.Suite\b`,
	)
	return structRE.MatchString(source)
}

func detectGoTest(source string) []testFunction {
	var out []testFunction
	// Standard top-level test functions: func TestFoo(t *testing.T) { … }
	for _, m := range goTestFuncRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		body := extractBraceBody(source, m[1]-1)
		out = append(out, testFunction{qname: name, body: body})
	}
	// Testify suite receiver-method tests: func (s *MySuite) TestFoo() { … }
	// Only emit when the receiver type looks like a testify suite struct (embeds
	// suite.Suite), to avoid false-positive matches on unrelated receiver methods.
	for _, m := range goTestSuiteMethodRE.FindAllStringSubmatchIndex(source, -1) {
		receiverType := source[m[2]:m[3]]
		testName := source[m[4]:m[5]]
		if !isSuiteStruct(source, receiverType) {
			continue
		}
		body := extractBraceBody(source, m[1]-1)
		out = append(out, testFunction{qname: testName, body: body})
	}
	return out
}

// ---------------------------------------------------------------------------
// Python — pytest
// ---------------------------------------------------------------------------

var pytestFuncRE = regexp.MustCompile(
	`(?m)^([ \t]*)(?:async\s+)?def\s+(test_\w+)\s*\([^)]*\)\s*(?:->\s*[\w\[\], ]+)?\s*:`,
)

func detectPytest(source string) []testFunction {
	var out []testFunction
	for _, m := range pytestFuncRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[4]:m[5]]
		body := extractIndentedBody(source, m[0])
		out = append(out, testFunction{qname: name, body: body})
	}
	return out
}

// ---------------------------------------------------------------------------
// JavaScript / TypeScript — Jest / Mocha / Jasmine
// ---------------------------------------------------------------------------

// it('name', () => { ... }) or test('name', function () { ... })
var jestCaseRE = regexp.MustCompile(
	`(?m)\b(?:it|test)\s*\(\s*['"` + "`" + `]([^'"` + "`" + `]{1,200})['"` + "`" + `]\s*,`,
)

// describe('name', () => { ... })
var jestDescribeRE = regexp.MustCompile(
	`(?m)\bdescribe\s*\(\s*['"` + "`" + `]([^'"` + "`" + `]{1,200})['"` + "`" + `]\s*,`,
)

func detectJest(source string) []testFunction {
	var out []testFunction
	for _, m := range jestCaseRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		body := extractBraceBody(source, m[1])
		out = append(out, testFunction{qname: jestCaseQName(name), body: body})
	}
	// describe blocks are added only when they contain no inner it/test hits —
	// callers want the finest-grained case to win, and the body scan already
	// traverses nested blocks.
	if len(out) == 0 {
		for _, m := range jestDescribeRE.FindAllStringSubmatchIndex(source, -1) {
			name := source[m[2]:m[3]]
			body := extractBraceBody(source, m[1])
			out = append(out, testFunction{qname: jestCaseQName(name), body: body})
		}
	}
	return out
}

// jestCaseQName converts the arbitrary string used as a Jest test case name
// into a valid qualified-name-ish token. Spaces → underscores; non-word chars
// dropped. Falls back to "anonymous_test" when empty after scrubbing.
func jestCaseQName(raw string) string {
	var sb strings.Builder
	for _, r := range raw {
		switch {
		case r == ' ', r == '-':
			sb.WriteByte('_')
		case (r >= 'A' && r <= 'Z'),
			(r >= 'a' && r <= 'z'),
			(r >= '0' && r <= '9'),
			r == '_':
			sb.WriteRune(r)
		}
	}
	out := sb.String()
	if out == "" {
		return "anonymous_test"
	}
	return "it_" + out
}

// ---------------------------------------------------------------------------
// Ruby — RSpec
// ---------------------------------------------------------------------------

// it 'name' do ... end  OR  it "name" do ... end
var rspecItRE = regexp.MustCompile(
	`(?m)\bit\s+['"]([^'"]{1,200})['"]\s+do\b`,
)

// rspecEndRE is greedy — we capture from `do` to the next matching `end`.
// Ruby nesting is tricky with regex; we approximate by scanning ahead until
// a line starting with `end` at the same or lower indentation.
func detectRSpec(source string) []testFunction {
	var out []testFunction
	for _, m := range rspecItRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		body := rspecBlockBody(source, m[1])
		out = append(out, testFunction{qname: rspecQName(name), body: body})
	}
	return out
}

func rspecQName(raw string) string {
	return jestCaseQName(raw) // same scrubbing rules
}

// rspecBlockBody scans forward for a matching `end` at lower indentation.
func rspecBlockBody(source string, start int) string {
	// Find the `do` line we begin on to capture its indent.
	lineStart := strings.LastIndexByte(source[:start], '\n') + 1
	doLine := source[lineStart:start]
	indent := leadingWhitespaceWidth(doLine)

	lines := strings.Split(source[start:], "\n")
	var body []string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "end" && leadingWhitespaceWidth(line) == indent {
			break
		}
		body = append(body, line)
	}
	return strings.Join(body, "\n")
}

// ---------------------------------------------------------------------------
// Java — JUnit
// ---------------------------------------------------------------------------

var junitMethodRE = regexp.MustCompile(
	`(?m)@Test(?:\s*\([^)]*\))?\s*(?:public\s+|private\s+|protected\s+)?void\s+(\w+)\s*\([^)]*\)\s*(?:throws\s+[\w., ]+)?\s*{`,
)

func detectJUnit(source string) []testFunction {
	var out []testFunction
	for _, m := range junitMethodRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		body := extractBraceBody(source, m[1]-1)
		out = append(out, testFunction{qname: name, body: body})
	}
	return out
}

// ---------------------------------------------------------------------------
// C# — NUnit / xUnit / MSTest
// ---------------------------------------------------------------------------

var csharpTestRE = regexp.MustCompile(
	`(?m)\[(?:Test|Fact|Theory|TestMethod)(?:\([^)]*\))?\]\s*(?:public\s+|private\s+|protected\s+)?(?:async\s+)?(?:Task|void)\s+(\w+)\s*\([^)]*\)\s*{`,
)

func detectCSharpTest(source string) []testFunction {
	var out []testFunction
	for _, m := range csharpTestRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		body := extractBraceBody(source, m[1]-1)
		out = append(out, testFunction{qname: name, body: body})
	}
	return out
}

// ---------------------------------------------------------------------------
// Rust — #[test]
// ---------------------------------------------------------------------------

var rustTestRE = regexp.MustCompile(
	`(?m)#\[test\][^\n]*\n\s*(?:async\s+)?fn\s+(\w+)\s*\([^)]*\)(?:\s*->\s*[^\{]+)?\s*{`,
)

func detectRustTest(source string) []testFunction {
	var out []testFunction
	for _, m := range rustTestRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		body := extractBraceBody(source, m[1]-1)
		out = append(out, testFunction{qname: name, body: body})
	}
	return out
}

// ---------------------------------------------------------------------------
// PHP — PHPUnit
// ---------------------------------------------------------------------------

var phpUnitRE = regexp.MustCompile(
	`(?m)(?:public\s+|private\s+|protected\s+)?function\s+(test\w+)\s*\([^)]*\)\s*(?::\s*\w+\s*)?{`,
)

func detectPHPUnit(source string) []testFunction {
	var out []testFunction
	for _, m := range phpUnitRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		body := extractBraceBody(source, m[1]-1)
		out = append(out, testFunction{qname: name, body: body})
	}
	return out
}

// ---------------------------------------------------------------------------
// Kotlin — JUnit on Kotlin / kotlin.test
// ---------------------------------------------------------------------------

var kotlinTestRE = regexp.MustCompile(
	`(?m)@Test(?:\s*\([^)]*\))?\s*(?:public\s+|private\s+|internal\s+)?fun\s+(` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)\s*\([^)]*\)\s*(?::\s*\w+\s*)?{`,
)

func detectKotlinTest(source string) []testFunction {
	var out []testFunction
	for _, m := range kotlinTestRE.FindAllStringSubmatchIndex(source, -1) {
		name := strings.Trim(source[m[2]:m[3]], "`")
		// Backtick names can contain spaces — normalise.
		name = strings.ReplaceAll(name, " ", "_")
		body := extractBraceBody(source, m[1]-1)
		out = append(out, testFunction{qname: name, body: body})
	}
	return out
}

// ---------------------------------------------------------------------------
// Swift — XCTest
// ---------------------------------------------------------------------------

var xcTestRE = regexp.MustCompile(
	`(?m)func\s+(test\w+)\s*\([^)]*\)\s*(?:throws\s*)?{`,
)

func detectXCTest(source string) []testFunction {
	var out []testFunction
	for _, m := range xcTestRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		body := extractBraceBody(source, m[1]-1)
		out = append(out, testFunction{qname: name, body: body})
	}
	return out
}

// ---------------------------------------------------------------------------
// Scala — Spock / ScalaTest
// ---------------------------------------------------------------------------

// ScalaTest FunSuite: test("name") { ... }
var scalaTestRE = regexp.MustCompile(
	`(?m)\btest\s*\(\s*"([^"]{1,200})"\s*\)\s*{`,
)

func detectScalaTest(source string) []testFunction {
	var out []testFunction
	for _, m := range scalaTestRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		body := extractBraceBody(source, m[1]-1)
		out = append(out, testFunction{qname: jestCaseQName(name), body: body})
	}
	return out
}

// ---------------------------------------------------------------------------
// Framework registry
// ---------------------------------------------------------------------------

// frameworkOrder is deterministic. Ambiguous files (e.g. a Kotlin file that
// imports both kotlin.test and org.junit) resolve to the first entry in this
// list that matches.
var frameworkOrder = []frameworkEntry{
	{
		name:        "go_testing",
		importHints: []string{"testing"},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`_test\.go$`),
		},
		detect: detectGoTest,
	},
	{
		name:        "pytest",
		importHints: []string{"pytest", "unittest"},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`^test_[\w]+\.py$`),
			regexp.MustCompile(`^[\w]+_test\.py$`),
		},
		// #2604: Django/pytest projects place test files under a tests/ or
		// test/ directory without requiring a test_ prefix on every file
		// (e.g. core/tests/schedule.py, api/tests/views.py). Match the full
		// repo-relative path so files like tests/foo.py or app/tests/bar.py
		// are recognised as Python test files even when their basename has no
		// test_ prefix. The \.py$ guard prevents matching non-Python files.
		pathHints: []*regexp.Regexp{
			regexp.MustCompile(`/tests?/.*\.py$`),
		},
		detect: detectPytest,
	},
	{
		name:        "cypress",
		importHints: []string{"cypress", "cy.", "@cypress/"},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`\.cy\.(?:ts|tsx|js|jsx)$`),
			regexp.MustCompile(`cypress/e2e/`),
			regexp.MustCompile(`cypress/integration/`),
		},
		detect: detectJest,
	},
	{
		name:        "playwright",
		importHints: []string{"@playwright/test", "playwright"},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`\.pw\.(?:ts|tsx|js|jsx)$`),
		},
		detect: detectJest,
	},
	{
		name:        "jest",
		importHints: []string{"jest", "@jest", "vitest", "mocha", "chai", "jasmine"},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`\.test\.(?:ts|tsx|js|jsx|mjs|cjs)$`),
			regexp.MustCompile(`\.spec\.(?:ts|tsx|js|jsx|mjs|cjs)$`),
		},
		detect: detectJest,
	},
	{
		name:        "rspec",
		importHints: []string{"rspec", "rspec/core"},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`_spec\.rb$`),
		},
		detect: detectRSpec,
	},
	{
		name:        "junit",
		importHints: []string{"org.junit", "junit.jupiter", "junit.framework", "junit"},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`Test\.java$`),
			regexp.MustCompile(`Tests\.java$`),
			regexp.MustCompile(`IT\.java$`),
		},
		detect: detectJUnit,
	},
	{
		name:        "kotlin_test",
		importHints: []string{"kotlin.test", "org.junit", "junit.jupiter"},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`Test\.kt$`),
			regexp.MustCompile(`Tests\.kt$`),
		},
		detect: detectKotlinTest,
	},
	{
		name:        "nunit",
		importHints: []string{"nunit.framework", "xunit", "microsoft.visualstudio.testtools"},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`Test\.cs$`),
			regexp.MustCompile(`Tests\.cs$`),
		},
		detect: detectCSharpTest,
	},
	{
		name:        "rust_test",
		importHints: []string{}, // Rust uses #[test] attribute — detection is body-based
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`\.rs$`),
		},
		detect: detectRustTest,
	},
	{
		name:        "phpunit",
		importHints: []string{"phpunit", "phpunit\\framework"},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`Test\.php$`),
		},
		detect: detectPHPUnit,
	},
	{
		name:        "xctest",
		importHints: []string{"xctest"},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`Tests?\.swift$`),
		},
		detect: detectXCTest,
	},
	{
		name:        "scalatest",
		importHints: []string{"org.scalatest", "scalatest"},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`Spec\.scala$`),
			regexp.MustCompile(`Test\.scala$`),
		},
		detect: detectScalaTest,
	},
}

// selectFramework picks the first framework whose import hints OR filename
// hints match the file. Returns nil when the file is not a test file.
//
// The Rust entry only matches when the source contains a `#[test]`
// attribute — the filename match alone is insufficient since every Rust file
// ends in .rs.
func selectFramework(tokens map[string]bool, filePath string) *frameworkEntry {
	for i := range frameworkOrder {
		fe := &frameworkOrder[i]
		importMatch := len(fe.importHints) > 0 && matchesAnyImport(tokens, fe.importHints)
		fileMatch := matchesAnyFilename(filePath, fe.filenameHints)
		pathMatch := len(fe.pathHints) > 0 && matchesAnyPath(filePath, fe.pathHints)

		switch fe.name {
		case "rust_test":
			// Filename alone is not a signal — require the detector to
			// actually yield at least one match, which is checked at the
			// Extract level. Here, only match when the file has .rs ext
			// AND contains "#[test]" (caller will drop empty results).
			if !fileMatch {
				continue
			}
			// cheap sentinel: look for "#[test]" in tokens-less source.
			// We don't have source at this layer, so we accept the match
			// optimistically; Extract() filters zero-result files downstream.
			return fe
		default:
			if importMatch || fileMatch || pathMatch {
				return fe
			}
		}
	}
	return nil
}
