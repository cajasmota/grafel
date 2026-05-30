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
	qname           string // fully qualified test function name
	body            string // textual body used for call/mock scanning
	describeSubject string // RSpec/Minitest: the described class/module name (e.g. "User", "UsersController")
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

// goHTTPHandlerCallRE detects direct HTTP handler invocations in Go test bodies.
// These are the primary shape of handler→test linkage in Go HTTP testing:
//
//	handler.ServeHTTP(w, r)         — net/http Handler interface
//	h.ServeHTTP(w, r)               — any variable holding a Handler
//	router.ServeHTTP(w, r)          — gin.Engine / chi.Mux / echo.Echo / mux.Router
//	handlerFunc(w, r)               — net/http.HandlerFunc call
//
// Group 1 captures the handler receiver / func name so the resolver can
// surface it as a medium-confidence production call alongside direct calls.
// ServeHTTP is treated as medium (not high) because `w` and `r` make the call
// unambiguous as a handler dispatch, but the receiver name may not match the
// entity name exactly (e.g. `router` vs `Router.ServeHTTP`).
var goHTTPHandlerCallRE = regexp.MustCompile(
	`(?m)\b(\w+)\.ServeHTTP\s*\(`,
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
		// Augment the body with HTTP-handler dispatch calls (ServeHTTP) so the
		// resolver can surface handler→test linkage as medium-confidence edges.
		// Each `recv.ServeHTTP(w, r)` call in the body becomes a synthetic
		// `recv.ServeHTTP(` token — kept as-is so directCallRE picks it up at
		// high confidence (the handler IS being directly called). This approach
		// requires no changes to the resolver; it relies on the existing direct-
		// call scanner to find `recv.ServeHTTP(` as a production call, and
		// `ServeHTTP` is NOT in the stopword list so it survives the filter.
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

// goHTTPHandlerNames returns the set of handler-receiver names that appear in
// a Go test file via `recv.ServeHTTP(w, r)` calls. Used by the resolver to
// verify that handler dispatch calls in test bodies are surfaced correctly.
// Exported for use in tests.
func goHTTPHandlerNames(source string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range goHTTPHandlerCallRE.FindAllStringSubmatchIndex(source, -1) {
		recv := source[m[2]:m[3]]
		if !seen[recv] {
			seen[recv] = true
			out = append(out, recv)
		}
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

// rspecDescribeConstRE matches `describe SomeConst do` / `RSpec.describe SomeConst do`
// / `context SomeConst do` at the TOP level (or any nesting). It captures the
// constant name so we can derive the test subject.
var rspecDescribeConstRE = regexp.MustCompile(
	`(?m)^\s*(?:RSpec\.)?(?:describe|context)\s+([A-Z][A-Za-z0-9_:]*)\b`,
)

// rspecDescribeStringRE matches `describe "some thing" do` (string form).
var rspecDescribeStringRE = regexp.MustCompile(
	`(?m)^\s*(?:RSpec\.)?(?:describe|context)\s+['"]([^'"]+)['"]`,
)

// rspecItRE matches `it 'name' do` or `it "name" do`.
var rspecItRE = regexp.MustCompile(
	`(?m)\bit\s+['"]([^'"]{1,200})['"]\s+do\b`,
)

// rspecItBlockRE also matches `specify 'name' do`.
var rspecSpecifyRE = regexp.MustCompile(
	`(?m)\bspecify\s+['"]([^'"]{1,200})['"]\s+do\b`,
)

// railsSpecSubjectFromPath derives the expected class/module name from a Rails
// spec file path using the Rails spec/ directory convention:
//
//	spec/models/user_spec.rb              → User
//	spec/controllers/users_controller_spec.rb → UsersController
//	spec/requests/users_spec.rb           → (blank — too ambiguous)
//	spec/jobs/import_job_spec.rb          → ImportJob
//	spec/mailers/notification_mailer_spec.rb → NotificationMailer
//	spec/helpers/application_helper_spec.rb  → ApplicationHelper
//	spec/services/billing_service_spec.rb → BillingService
//	spec/serializers/user_serializer_spec.rb → UserSerializer
//
// When the path does not follow a recognisable Rails spec convention, an empty
// string is returned and the caller falls back to the generic _spec suffix rule.
func railsSpecSubjectFromPath(filePath string) string {
	norm := filepath.ToSlash(filePath)
	// Strip spec/ prefix segments — handle paths like app/spec/... or spec/...
	idx := strings.Index(norm, "/spec/")
	if idx < 0 {
		return ""
	}
	rel := norm[idx+len("/spec/"):]
	parts := strings.SplitN(rel, "/", 2)
	if len(parts) < 2 {
		return ""
	}
	dir := parts[0]
	base := parts[1]
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(filepath.Base(base), ext)
	// strip trailing _spec
	if strings.HasSuffix(stem, "_spec") {
		stem = stem[:len(stem)-len("_spec")]
	}

	switch dir {
	case "models", "controllers", "jobs", "mailers", "helpers",
		"services", "serializers", "presenters", "decorators",
		"validators", "policies", "uploaders", "workers", "forms":
		return railsTestCamelCase(stem)
	case "requests", "features", "system", "integration":
		// These specs test HTTP endpoints / browser flows, not a single class.
		return ""
	}
	return ""
}

// railsTestCamelCase converts a snake_case stem (e.g. "users_controller") to
// CamelCase (e.g. "UsersController"). Already-capitalised words are preserved.
func railsTestCamelCase(snake string) string {
	parts := strings.Split(snake, "_")
	var sb strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(p)
		if r[0] >= 'a' && r[0] <= 'z' {
			r[0] -= 'a' - 'A'
		}
		sb.WriteString(string(r))
	}
	return sb.String()
}

// rspecDescribeSubject returns the primary describe/RSpec.describe subject for
// the file. It prefers a constant-form subject (e.g. `describe User do`) over a
// string label, and returns the first top-level match found.
func rspecDescribeSubject(source string) string {
	// Prefer constant-form (e.g. `describe User do`)
	if m := rspecDescribeConstRE.FindStringSubmatch(source); m != nil {
		return m[1]
	}
	return ""
}

// detectRSpec detects RSpec it/specify examples and annotates each with the
// described subject so that the resolver can emit a TESTS edge even when the
// example body contains no explicit production call.
func detectRSpec(source string) []testFunction {
	subject := rspecDescribeSubject(source)

	var out []testFunction
	for _, re := range []*regexp.Regexp{rspecItRE, rspecSpecifyRE} {
		for _, m := range re.FindAllStringSubmatchIndex(source, -1) {
			name := source[m[2]:m[3]]
			body := rspecBlockBody(source, m[1])
			out = append(out, testFunction{
				qname:           rspecQName(name),
				body:            body,
				describeSubject: subject,
			})
		}
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
// Ruby — Minitest / ActiveSupport::TestCase
// ---------------------------------------------------------------------------

// railsMinitestClassRE matches `class FooTest < Minitest::Test` or
// `class FooTest < ActiveSupport::TestCase` (and the generic `< Minitest::Spec`).
var railsMinitestClassRE = regexp.MustCompile(
	`(?m)^\s*class\s+(\w+Test\w*)\s*<\s*(?:Minitest::(?:Test|Spec|Unit)|ActiveSupport::TestCase|ActionController::TestCase|ActionDispatch::IntegrationTest|ActionMailer::TestCase|ActionView::TestCase)\b`,
)

// railsMinitestTestBlockRE matches the DSL-style `test "description" do` form.
var railsMinitestTestBlockRE = regexp.MustCompile(
	`(?m)^\s*test\s+['"]([^'"]{1,200})['"]\s+do\b`,
)

// railsMinitestDefRE matches the method-style `def test_something` form.
var railsMinitestDefRE = regexp.MustCompile(
	`(?m)^\s*def\s+(test_\w+)\b`,
)

// railsMinitestSubjectFromClass derives the tested subject name from the test
// class name by stripping trailing "Test(s)". Examples:
//
//	UserTest        → User
//	UsersControllerTest → UsersController
//	ImportJobTest   → ImportJob
func railsMinitestSubjectFromClass(className string) string {
	for _, suf := range []string{"Tests", "Test"} {
		if strings.HasSuffix(className, suf) && len(className) > len(suf) {
			return className[:len(className)-len(suf)]
		}
	}
	return ""
}

// detectMinitest detects Minitest / ActiveSupport::TestCase test functions.
// It handles:
//   - DSL form:    test "description" do ... end
//   - Method form: def test_foo ... end
//
// Each detected test function carries the class name's derived subject
// (e.g. UserTest → User) as describeSubject.
func detectMinitest(source string) []testFunction {
	// Derive the described subject from the class name.
	subject := ""
	if m := railsMinitestClassRE.FindStringSubmatch(source); m != nil {
		subject = railsMinitestSubjectFromClass(m[1])
	}

	var out []testFunction

	// DSL-style: test "description" do ... end
	for _, m := range railsMinitestTestBlockRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		body := rspecBlockBody(source, m[1])
		out = append(out, testFunction{
			qname:           rspecQName(name),
			body:            body,
			describeSubject: subject,
		})
	}

	// Method-style: def test_foo ... end
	for _, m := range railsMinitestDefRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		body := rspecBlockBody(source, m[1])
		out = append(out, testFunction{
			qname:           name,
			body:            body,
			describeSubject: subject,
		})
	}

	return out
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
// C# — xUnit / NUnit / MSTest  (deep linkage, #3383)
// ---------------------------------------------------------------------------

// csharpXUnitMethodRE matches xUnit [Fact] and [Theory] test methods.
//
// Design notes:
//   - Allows zero or more additional attribute lines between [Fact/Theory] and
//     the method signature (e.g. [InlineData(...)], [MemberData(...)]).
//   - Accepts any return type identifier (void, Task, int, Task<T>, etc.) so
//     [Theory] methods returning a typed value are covered.
//   - Captures: (1) method name.
var csharpXUnitMethodRE = regexp.MustCompile(
	`(?m)\[(?:Fact|Theory)(?:\([^)]*\))?\](?:\s*\[[^\]]*\])*\s*(?:(?:public|private|protected|internal|static|override|virtual|async|sealed)\s+)*[\w<>\[\]?]+\s+(\w+)\s*\([^)]*\)\s*\{`,
)

// csharpNUnitMethodRE matches NUnit [Test] and [TestCase] test methods.
//
// Allows additional attribute lines between the test attribute and the method
// signature (e.g. [TestCase(...)]) and accepts any return type.
// Captures: (1) method name.
var csharpNUnitMethodRE = regexp.MustCompile(
	`(?m)\[(?:Test|TestCase)(?:\([^)]*\))?\](?:\s*\[[^\]]*\])*\s*(?:(?:public|private|protected|internal|static|override|virtual|async|sealed)\s+)*[\w<>\[\]?]+\s+(\w+)\s*\([^)]*\)\s*\{`,
)

// csharpMSTestMethodRE matches MSTest [TestMethod] test methods.
//
// Accepts any return type. Captures: (1) method name.
var csharpMSTestMethodRE = regexp.MustCompile(
	`(?m)\[TestMethod(?:\([^)]*\))?\](?:\s*\[[^\]]*\])*\s*(?:(?:public|private|protected|internal|static|override|virtual|async|sealed)\s+)*[\w<>\[\]?]+\s+(\w+)\s*\([^)]*\)\s*\{`,
)

// csharpXUnitClassRE detects the containing test class — xUnit does NOT require
// a class-level attribute; the class is discovered by containing [Fact]/[Theory].
// We capture the class name from `public class XTests` / `public class XTests :`.
//
// Captures: (1) class name.
var csharpTestClassRE = regexp.MustCompile(
	`(?m)^\s*(?:(?:public|internal|private|protected|abstract|sealed|partial)\s+)*class\s+(\w+)`,
)

// csharpNUnitFixtureRE detects `[TestFixture]` classes for NUnit.
var csharpNUnitFixtureRE = regexp.MustCompile(`(?m)\[TestFixture(?:\([^)]*\))?\]`)

// csharpMSTestClassRE detects `[TestClass]` classes for MSTest.
var csharpMSTestClassAttrRE = regexp.MustCompile(`(?m)\[TestClass(?:\([^)]*\))?\]`)

// csharpWebAppFactoryRE detects WebApplicationFactory<T> integration tests.
// Group 1 captures the entry-point type T.
var csharpWebAppFactoryRE = regexp.MustCompile(
	`\bWebApplicationFactory\s*<\s*(\w+)\s*>`,
)

// csTestSubjectFromClassName derives the class under test from a C# test class
// name by stripping trailing "Tests"/"Test" suffixes, mirroring the Java/Kotlin
// convention already handled by productionFileFromTestPath.
//
//	OrderServiceTests → OrderService
//	OrderServiceTest  → OrderService
//	UserControllerTests → UserController
//	(no suffix)       → ""
func csTestSubjectFromClassName(className string) string {
	for _, suf := range []string{"Tests", "Test"} {
		if strings.HasSuffix(className, suf) && len(className) > len(suf) {
			return className[:len(className)-len(suf)]
		}
	}
	return ""
}

// csFirstClassName returns the first class name found in source.
func csFirstClassName(source string) string {
	if m := csharpTestClassRE.FindStringSubmatch(source); m != nil {
		return m[1]
	}
	return ""
}

// csWebAppFactoryType returns the T from WebApplicationFactory<T>, or "".
func csWebAppFactoryType(source string) string {
	if m := csharpWebAppFactoryRE.FindStringSubmatch(source); m != nil {
		return m[1]
	}
	return ""
}

// detectXUnitTest detects xUnit [Fact]/[Theory] test methods.
// Annotates each test with the class-under-test derived from the class name
// so the resolver can emit a TESTS edge even without explicit instantiation.
func detectXUnitTest(source string) []testFunction {
	className := csFirstClassName(source)
	subject := csTestSubjectFromClassName(className)
	// Integration tests: WebApplicationFactory<T> → subject = T (higher specificity)
	if waf := csWebAppFactoryType(source); waf != "" {
		subject = waf
	}
	var out []testFunction
	for _, m := range csharpXUnitMethodRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		body := extractBraceBody(source, m[1]-1)
		out = append(out, testFunction{
			qname:           name,
			body:            body,
			describeSubject: subject,
		})
	}
	return out
}

// detectNUnitTest detects NUnit [Test]/[TestCase] methods inside [TestFixture] classes.
func detectNUnitTest(source string) []testFunction {
	className := csFirstClassName(source)
	subject := csTestSubjectFromClassName(className)
	if waf := csWebAppFactoryType(source); waf != "" {
		subject = waf
	}
	var out []testFunction
	for _, m := range csharpNUnitMethodRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		body := extractBraceBody(source, m[1]-1)
		out = append(out, testFunction{
			qname:           name,
			body:            body,
			describeSubject: subject,
		})
	}
	return out
}

// detectMSTest detects MSTest [TestMethod] methods inside [TestClass] classes.
func detectMSTest(source string) []testFunction {
	className := csFirstClassName(source)
	subject := csTestSubjectFromClassName(className)
	if waf := csWebAppFactoryType(source); waf != "" {
		subject = waf
	}
	var out []testFunction
	for _, m := range csharpMSTestMethodRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		body := extractBraceBody(source, m[1]-1)
		out = append(out, testFunction{
			qname:           name,
			body:            body,
			describeSubject: subject,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Rust — cargo test (#[test] / #[tokio::test] / #[async_std::test]),
//        criterion benchmarks, proptest property tests, mockall mocks.
//
// Deep linkage (#3415): each detected test/bench/property function is annotated
// with both its body (so the resolver can find direct production calls) and a
// naming-convention describeSubject derived from the function name
// (`test_register` → `register`, `bench_parse` → `parse`). The resolver treats
// the body call as high confidence and the naming-convention subject as a
// medium-confidence fallback when no direct call is found.
// ---------------------------------------------------------------------------

// rustTestRE matches a Rust test function preceded by any of the recognised
// test attributes:
//
//	#[test]
//	#[tokio::test]            (and #[tokio::test(flavor = "multi_thread")])
//	#[async_std::test]
//	#[actix_web::test] / #[actix_rt::test]
//	#[rstest]                 (rstest fixture-driven cases)
//
// Group 1 captures the function name. The attribute line is allowed to carry
// arguments (e.g. `#[tokio::test(flavor = "multi_thread")]`) and may be
// followed by additional attribute lines (e.g. `#[should_panic]`) before the
// `fn` declaration.
var rustTestRE = regexp.MustCompile(
	`(?m)#\[(?:test|tokio::test|async_std::test|actix_web::test|actix_rt::test|rstest)\b[^\]]*\]` +
		`(?:\s*#\[[^\]]*\])*` + // optional extra attributes (#[should_panic], #[case(..)], …)
		`\s*(?:pub\s+)?(?:async\s+)?fn\s+(\w+)\s*\([^)]*\)(?:\s*->\s*[^\{]+)?\s*{`,
)

// rustCriterionBenchRE matches a criterion benchmark function, recognised by
// the conventional `&mut Criterion` parameter:
//
//	fn bench_parse(c: &mut Criterion) { … }
//	pub fn bench_parse(c: &mut Criterion) { … }
//
// Group 1 captures the bench function name. The production target is recovered
// from the `c.bench_function("name", |b| b.iter(|| target()))` call in the
// body (a direct call the resolver already picks up) and, failing that, from
// the bench-name naming convention.
var rustCriterionBenchRE = regexp.MustCompile(
	`(?m)(?:pub\s+)?fn\s+(\w+)\s*\(\s*\w+\s*:\s*&mut\s+Criterion\s*\)\s*{`,
)

// rustProptestRE matches a property test declared inside a `proptest! { … }`
// macro block:
//
//	proptest! {
//	    #[test]
//	    fn p_roundtrip(s in ".*") { parse(&s); }
//	}
//
// Inside the macro the `in <strategy>` argument syntax replaces normal typed
// params, so the parameter list is matched loosely. Group 1 captures the
// property function name.
var rustProptestFnRE = regexp.MustCompile(
	`(?m)#\[test\]\s*(?:#\[[^\]]*\]\s*)*fn\s+(\w+)\s*\([^)]*\)\s*{`,
)

// rustProptestBlockRE locates `proptest! { … }` macro blocks so property
// functions are only scanned within them (avoiding double-counting the inner
// `#[test]` via rustTestRE — the block bodies are subtracted first).
var rustProptestBlockRE = regexp.MustCompile(`proptest!\s*{`)

// rustAutomockRE matches a trait annotated with `#[automock]` (mockall). The
// generated mock exercises the trait, so the trait name is the subject under
// test. Group 1 captures the trait name.
var rustAutomockRE = regexp.MustCompile(
	`(?m)#\[(?:automock|cfg_attr\([^)]*\bautomock\b[^)]*\))\]\s*(?:pub\s+)?(?:unsafe\s+)?trait\s+(\w+)`,
)

// rustMockBangRE matches a `mock! { TraitName { … } }` mockall declaration.
// Group 1 captures the mock struct name; group 2 (optional) the mocked trait.
var rustMockBangRE = regexp.MustCompile(
	`(?m)mock!\s*{\s*(?:pub\s+)?(\w+)\b`,
)

// rustTestSubject derives the production symbol a test/bench function exercises
// from its name by stripping the conventional test/bench prefixes:
//
//	test_register      → register
//	test_register_user → register_user
//	bench_parse        → parse
//	it_parses_input    → parses_input
//	prop_roundtrip     → roundtrip
//	p_roundtrip        → roundtrip      (single-letter proptest convention)
//
// Returns "" when no prefix applies (so we don't invent a subject from an
// already-bare name like `roundtrip`).
func rustTestSubject(name string) string {
	for _, pfx := range []string{"test_", "bench_", "it_", "prop_", "p_"} {
		if strings.HasPrefix(name, pfx) && len(name) > len(pfx) {
			return name[len(pfx):]
		}
	}
	return ""
}

// subtractRanges blanks out (replaces with spaces, preserving offsets) every
// [start,end) range in source so a subsequent regex pass does not re-match
// content already consumed by an earlier pass. Newlines are preserved so the
// `(?m)` anchors of later passes stay aligned.
func subtractRanges(source string, ranges [][2]int) string {
	if len(ranges) == 0 {
		return source
	}
	b := []byte(source)
	for _, r := range ranges {
		for i := r[0]; i < r[1] && i < len(b); i++ {
			if b[i] != '\n' {
				b[i] = ' '
			}
		}
	}
	return string(b)
}

func detectRustTest(source string) []testFunction {
	var out []testFunction
	seen := map[string]bool{}
	add := func(name, body, subject string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, testFunction{qname: name, body: body, describeSubject: subject})
	}

	// Pass A: proptest! { … } blocks. Scan inner property functions first, then
	// subtract the block bodies so the inner #[test] attrs aren't re-matched by
	// the cargo-test pass.
	var consumed [][2]int
	for _, loc := range rustProptestBlockRE.FindAllStringIndex(source, -1) {
		block := extractBraceBody(source, loc[1]-1)
		if block == "" {
			continue
		}
		blockStart := strings.Index(source[loc[0]:], block) + loc[0]
		consumed = append(consumed, [2]int{loc[0], blockStart + len(block)})
		for _, m := range rustProptestFnRE.FindAllStringSubmatchIndex(block, -1) {
			name := block[m[2]:m[3]]
			body := extractBraceBody(block, m[1]-1)
			add(name, body, rustTestSubject(name))
		}
	}
	scanSrc := subtractRanges(source, consumed)

	// Pass B: cargo test — #[test] / #[tokio::test] / #[async_std::test] / …
	for _, m := range rustTestRE.FindAllStringSubmatchIndex(scanSrc, -1) {
		name := scanSrc[m[2]:m[3]]
		body := extractBraceBody(scanSrc, m[1]-1)
		add(name, body, rustTestSubject(name))
	}

	// Pass C: criterion benchmarks — fn bench_x(c: &mut Criterion) { … }.
	for _, m := range rustCriterionBenchRE.FindAllStringSubmatchIndex(scanSrc, -1) {
		name := scanSrc[m[2]:m[3]]
		body := extractBraceBody(scanSrc, m[1]-1)
		add(name, body, rustTestSubject(name))
	}

	// Pass D: mockall — associate each mock with the trait it mocks. We emit a
	// synthetic "test function" named after the mock whose describeSubject is
	// the mocked trait, producing a medium-confidence TESTS edge mock → trait.
	for _, m := range rustAutomockRE.FindAllStringSubmatch(source, -1) {
		trait := m[1]
		add("automock_"+trait, "", trait)
	}
	for _, m := range rustMockBangRE.FindAllStringSubmatch(source, -1) {
		// mock! { MockFoo: FooTrait { … } } — prefer the explicit trait when the
		// `: Trait` form is present; otherwise the mock name itself is the subject
		// hint (mockall convention: mock name == Mock + Trait).
		mockName := m[1]
		subject := strings.TrimPrefix(mockName, "Mock")
		if subject == mockName || subject == "" {
			subject = mockName
		}
		add("mock_"+mockName, "", subject)
	}

	return out
}

// ---------------------------------------------------------------------------
// PHP — PHPUnit (deep linkage, #3399)
// ---------------------------------------------------------------------------

// phpUnitTestNameRE matches PHPUnit test methods by the three recognised forms:
//
//  1. test* name prefix:            public function testGetUser() { … }
//  2. #[Test] PHP8 attribute:       #[Test] public function getUserById() { … }
//  3. /** @test */ docblock:        /** @test */ public function it_gets_user() { … }
//
// Group 1 captures the method name for all three forms.
var phpUnitTestNameRE = regexp.MustCompile(
	`(?m)(?:public\s+|private\s+|protected\s+)?function\s+(test\w+)\s*\([^)]*\)\s*(?::\s*\w+\s*)?{`,
)

// phpUnitAttrTestRE matches PHP8 #[Test] attribute before a public method.
// Captures group 1 = method name.
var phpUnitAttrTestRE = regexp.MustCompile(
	`(?m)#\[Test\](?:\s*\n)+\s*(?:public\s+|protected\s+|private\s+)?(?:static\s+)?function\s+(\w+)\s*\(`,
)

// phpUnitDocTestRE matches /** @test */ docblock before a public method.
// Captures group 1 = method name.
var phpUnitDocTestRE = regexp.MustCompile(
	`(?m)/\*\*[^*]*@test[^*]*\*/\s*(?:(?:public|protected|private|static|abstract|final)\s+)*function\s+(\w+)\s*\(`,
)

// phpUnitClassRE detects a PHPUnit test class extending TestCase (or its
// Laravel/Symfony subclasses). Captures group 1 = class name.
var phpUnitClassRE = regexp.MustCompile(
	`(?m)class\s+(\w+)\s+extends\s+(?:\w+\\)*TestCase\b`,
)

// phpUnitInstantiationRE detects `new UserService()` / `new SomeClass(` patterns
// in PHP test bodies that identify the class-under-test.
// Captures group 1 = class name being instantiated.
var phpUnitInstantiationRE = regexp.MustCompile(
	`\bnew\s+([A-Z][A-Za-z0-9_]*)\s*\(`,
)

// phpTestSubjectFromClassName derives the class under test from the test class
// name by stripping trailing "Test(s)". Examples:
//
//	UserTest          → User
//	UserServiceTest   → UserService
//	UserControllerTest → UserController
func phpTestSubjectFromClassName(className string) string {
	for _, suf := range []string{"Tests", "Test"} {
		if strings.HasSuffix(className, suf) && len(className) > len(suf) {
			return className[:len(className)-len(suf)]
		}
	}
	return ""
}

func detectPHPUnit(source string) []testFunction {
	// Derive the described subject from the class name.
	subject := ""
	if m := phpUnitClassRE.FindStringSubmatch(source); m != nil {
		subject = phpTestSubjectFromClassName(m[1])
	}

	seen := map[string]bool{}
	var out []testFunction
	add := func(name, body string) {
		if seen[name] {
			return
		}
		seen[name] = true
		// Augment body with instantiation targets: `new UserService()` → subject hint
		// carried in describeSubject so resolver can pick up class-under-test even
		// when the body has no direct method call matching a production function.
		ds := subject
		if ds == "" {
			// Try to infer from body instantiation (e.g. new UserService())
			if im := phpUnitInstantiationRE.FindStringSubmatch(body); im != nil {
				ds = im[1]
			}
		}
		out = append(out, testFunction{qname: name, body: body, describeSubject: ds})
	}

	// Form 1: test* prefix methods.
	for _, m := range phpUnitTestNameRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		body := extractBraceBody(source, m[1]-1)
		add(name, body)
	}

	// Form 2: #[Test] attribute methods.
	for _, m := range phpUnitAttrTestRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		// Find the function body by scanning forward from the match.
		body := extractBraceBody(source, m[1])
		add(name, body)
	}

	// Form 3: /** @test */ docblock methods.
	for _, m := range phpUnitDocTestRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		body := extractBraceBody(source, m[1])
		add(name, body)
	}

	return out
}

// ---------------------------------------------------------------------------
// PHP — Pest (functional test DSL, #3399)
// ---------------------------------------------------------------------------

// pestItRE matches Pest it('description', function () { … }) and
// test('description', function () { … }) or arrow-function variants.
// Allows optional leading whitespace so that nested it() inside describe()
// blocks is also detected.
// Captures group 1 = single-quoted description, group 2 = double-quoted.
var pestItRE = regexp.MustCompile(
	`(?m)^\s*(?:it|test)\s*\(\s*(?:'([^']{1,300})'|"([^"]{1,300})")\s*,`,
)

// pestUsesSubjectRE matches uses(ClassName::class) to extract the test subject
// for use in describeSubject. Captures group 1 = class name (without ::class).
var pestUsesSubjectRE = regexp.MustCompile(
	`(?m)uses\s*\(\s*([A-Za-z_][A-Za-z0-9_\\]*?)::class`,
)

// pestDescribeSubjectRE matches describe('ClassName', ...) where the description
// is a PascalCase identifier — used as a fallback subject.
var pestDescribeSubjectRE = regexp.MustCompile(
	`(?m)^\s*describe\s*\(\s*(?:'([A-Z][A-Za-z0-9_]*)'|"([A-Z][A-Za-z0-9_]*)")`,
)

func detectPhpPest(source string) []testFunction {
	// Derive subject from uses(ClassName::class).
	subject := ""
	if m := pestUsesSubjectRE.FindStringSubmatch(source); m != nil {
		// Take the last segment of a namespaced class: App\Services\UserService → UserService
		parts := strings.Split(m[1], `\`)
		subject = parts[len(parts)-1]
	}
	// Fallback: describe('ClassName', ...) where first arg looks like a class.
	if subject == "" {
		if m := pestDescribeSubjectRE.FindStringSubmatch(source); m != nil {
			if m[1] != "" {
				subject = m[1]
			} else if m[2] != "" {
				subject = m[2]
			}
		}
	}

	var out []testFunction
	for _, m := range pestItRE.FindAllStringSubmatchIndex(source, -1) {
		// Pick whichever quote group matched.
		rawName := ""
		if m[2] >= 0 && m[3] >= 0 {
			rawName = source[m[2]:m[3]]
		} else if m[4] >= 0 && m[5] >= 0 {
			rawName = source[m[4]:m[5]]
		}
		if rawName == "" {
			continue
		}
		name := jestCaseQName(rawName) // reuse JS normaliser: spaces → underscores
		body := extractBraceBody(source, m[1])
		out = append(out, testFunction{qname: name, body: body, describeSubject: subject})
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
//
// C# frameworks are listed FIRST so that .cs test files with known framework
// imports (nunit.framework, microsoft.visualstudio.testtools) are selected
// before go_testing, whose import hint "testing" is a suffix of the MSTest
// namespace "microsoft.visualstudio.testtools.unittesting" and would otherwise
// cause a false-positive match on C# files.
var frameworkOrder = []frameworkEntry{
	// C# — NUnit: import-hints only (no filenameHints so the xUnit fallback
	// below is not shadowed when only the filename matches).
	{
		name:        "nunit",
		importHints: []string{"nunit.framework", "nunit"},
		detect:      detectNUnitTest,
	},
	// C# — MSTest: import-hints only.
	{
		name:        "mstest",
		importHints: []string{"microsoft.visualstudio.testtools", "microsoft.visualstudio.testtools.unittesting"},
		detect:      detectMSTest,
	},
	// C# — xUnit: listed AFTER nunit/mstest so those two win when a recognised
	// import is present. Falls back to filename detection for files without a
	// using directive (common in small xUnit projects).
	{
		name:        "xunit",
		importHints: []string{"xunit", "xunit.abstractions", "xunit.core"},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`Test\.cs$`),
			regexp.MustCompile(`Tests\.cs$`),
		},
		detect: detectXUnitTest,
	},
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
		},
		pathHints: []*regexp.Regexp{
			regexp.MustCompile(`/cypress/e2e/`),
			regexp.MustCompile(`/cypress/integration/`),
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
		// Rails projects keep all specs under spec/ regardless of import markers.
		pathHints: []*regexp.Regexp{
			regexp.MustCompile(`/spec/.*_spec\.rb$`),
		},
		detect: detectRSpec,
	},
	{
		// Minitest / ActiveSupport::TestCase — Rails default test framework.
		// Detected by import tokens (require 'minitest', 'minitest/autorun') OR
		// by the file name convention (*_test.rb inside a test/ directory).
		// NOTE: listed AFTER rspec so that spec/_spec.rb files always win.
		name:        "minitest",
		importHints: []string{"minitest", "minitest/autorun", "minitest/spec", "active_support/test_case"},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`_test\.rb$`),
		},
		pathHints: []*regexp.Regexp{
			regexp.MustCompile(`/test/.*_test\.rb$`),
			// Rails also accepts plain files under test/ subdirectories.
			regexp.MustCompile(`/test/(?:models|controllers|helpers|jobs|mailers|integration|system)/.*\.rb$`),
		},
		detect: detectMinitest,
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
		name:        "rust_test",
		importHints: []string{}, // Rust uses #[test] attribute — detection is body-based
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`\.rs$`),
		},
		detect: detectRustTest,
	},
	// PHP — Pest: listed BEFORE phpunit so that files with an explicit Pest
	// import are routed to the Pest detector first. Import-hints-only — no
	// path hints — to avoid false-positive matches on PHPUnit files that also
	// live under tests/. Files that use Pest DSL without a recognised import
	// fall through to phpunit detection (which will yield 0 results and skip
	// the file), which is an acceptable partial miss rather than a wrong match.
	{
		name:        "phptest_pest",
		importHints: []string{"pest", "pestphp", "pest\\expect", "pest\\test"},
		detect:      detectPhpPest,
	},
	{
		name:        "phpunit",
		importHints: []string{"phpunit", "phpunit\\framework", "phpunit\\framework\\testcase"},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`Test\.php$`),
			regexp.MustCompile(`Tests\.php$`),
		},
		// Laravel/Symfony PHPUnit tests commonly live under tests/ — detect by
		// path even when the use PHPUnit statement is absent (e.g. when the test
		// class extends a framework-provided TestCase base that hides the import).
		pathHints: []*regexp.Regexp{
			regexp.MustCompile(`/tests?/.*Test\.php$`),
			regexp.MustCompile(`/tests?/.*Tests\.php$`),
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
