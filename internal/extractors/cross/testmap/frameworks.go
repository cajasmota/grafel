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
	lang            string // optional source language ("fsharp", …); gates language-specific resolver passes (#5034)
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

// includeTokenRE captures C/C++ preprocessor includes in both bracket and quote
// forms: `#include <gtest/gtest.h>` and `#include "catch.hpp"`. The captured
// token (e.g. "gtest/gtest.h") is added to the import set so the C/C++ test
// frameworks can be selected by their header path the same way other languages
// match on import statements. (#3495)
var includeTokenRE = regexp.MustCompile(
	`(?m)^\s*#\s*include\s+[<"]([\w@][\w\-./:]*)[>"]`,
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
	for _, m := range includeTokenRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 2 {
			add(m[1])
		}
	}
	return out
}

// namedImportRE captures the *named symbols* brought into scope by an import
// statement (as opposed to importTokenRE, which captures the module path). The
// set it produces is used by the resolver to gate the high-confidence direct
// -call signal: a call to an imported symbol is the strongest test→SUT signal,
// whereas a call to a same-named identifier that was never imported is more
// likely a local/builtin collision and is held at medium.
//
// Covered styles (one capture group = the brace/parenthesised symbol list):
//
//	JS/TS:  import { UserService, createOrder } from '../user-service'
//	JS/TS:  import UserService from './user-service'          (default — group 2)
//	Python: from app.users import create_user, UserService
//	Python: from app.users import (create_user, UserService)
var namedImportBraceRE = regexp.MustCompile(
	`(?m)\bimport\s*\{([^}]*)\}\s*from\b`,
)

// pyFromImportRE captures the symbol list of a Python `from x import a, b` /
// `from x import (a, b)` statement. Group 1 = the comma-separated names.
var pyFromImportRE = regexp.MustCompile(
	`(?m)^\s*from\s+[\w.]+\s+import\s+\(?([^()\n#]+)\)?`,
)

// jsDefaultImportRE captures a JS/TS default import binding:
// `import UserService from './user-service'`. Group 1 = the binding name.
var jsDefaultImportRE = regexp.MustCompile(
	`(?m)\bimport\s+([A-Za-z_$][\w$]*)\s*(?:,\s*\{[^}]*\}\s*)?from\b`,
)

// extractNamedImports returns the set of symbol names brought into scope by the
// file's import statements (JS/TS named + default imports, Python from-imports).
// Names are kept in their original case. An empty set means "no named imports
// were recognised" — in that case the resolver does NOT gate on imports (so Go
// same-package calls, wildcard imports, etc. keep their existing behaviour).
//
// Aliases (`import { A as B }`, `from x import a as b`) record the LOCAL binding
// (B / b) — that is the name the test body actually calls.
func extractNamedImports(source string) map[string]bool {
	out := map[string]bool{}
	addList := func(list string) {
		for _, raw := range strings.Split(list, ",") {
			name := strings.TrimSpace(raw)
			if name == "" || name == "*" {
				continue
			}
			// `A as B` → bind B (the local name used in the body).
			if idx := strings.Index(name, " as "); idx >= 0 {
				name = strings.TrimSpace(name[idx+len(" as "):])
			}
			// Strip a `type ` prefix (TS `import { type Foo }`).
			name = strings.TrimPrefix(name, "type ")
			name = strings.TrimSpace(name)
			// Keep only plain identifiers.
			if name != "" && isPlainIdent(name) {
				out[name] = true
			}
		}
	}
	for _, m := range namedImportBraceRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 2 {
			addList(m[1])
		}
	}
	for _, m := range pyFromImportRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 2 {
			addList(m[1])
		}
	}
	for _, m := range jsDefaultImportRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 2 && isPlainIdent(m[1]) {
			out[m[1]] = true
		}
	}
	return out
}

// isPlainIdent reports whether s is a single bare identifier (no dots, spaces,
// braces, or operators) — the only form we record as an imported symbol.
func isPlainIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_', r == '$':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
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

// junitMethodRE matches a JUnit (4 or 5) annotated test method. It accepts
// @Test, @ParameterizedTest and @RepeatedTest (JUnit 5 — each optionally
// carrying an argument list), optional additional annotation lines (e.g.
// @DisplayName(…), @ValueSource(…), @CsvSource(…)) between the test annotation
// and the method signature, and an optional non-void return type (JUnit 5
// allows e.g. `Stream<DynamicTest>` factory-style or value-returning helpers,
// though `void` is by far the common case). Group 1 captures the method name.
var junitMethodRE = regexp.MustCompile(
	`(?m)@(?:Test|ParameterizedTest|RepeatedTest)(?:\s*\([^)]*\))?` +
		`(?:\s*@\w+(?:\s*\([^)]*\))?)*` +
		`\s*(?:public\s+|private\s+|protected\s+|static\s+|final\s+)*` +
		`(?:void|[\w<>\[\].,?\s]+?)\s+(\w+)\s*\([^)]*\)\s*(?:throws\s+[\w., ]+)?\s*{`,
)

// junitClassRE captures the first declared class name in a Java test source
// file. Used to derive the JUnit subject-under-test from the test-class name
// (UserServiceTest → UserService), mirroring the Kotlin/C# describeSubject path
// so a class-level naming-convention edge is emitted even when a test body
// contains no direct production call (e.g. Mockito-only bodies).
var junitClassRE = regexp.MustCompile(
	`(?m)^\s*(?:(?:public|private|protected|abstract|final|sealed|static)\s+)*class\s+(\w+)`,
)

// junitFirstClassName returns the first class name found in source, or "".
func junitFirstClassName(source string) string {
	if m := junitClassRE.FindStringSubmatch(source); m != nil {
		return m[1]
	}
	return ""
}

func detectJUnit(source string) []testFunction {
	// Derive the class-under-test from the test-class name (UserServiceTest →
	// UserService). Reuses csTestSubjectFromClassName, which strips a trailing
	// Tests/Test suffix — the dominant Java convention. (An "IT"-suffixed
	// integration-test class name yields no subject here; those resolve via the
	// body-call scan or the file-name convention fallback in extractor.go.)
	subject := csTestSubjectFromClassName(junitFirstClassName(source))

	var out []testFunction
	for _, m := range junitMethodRE.FindAllStringSubmatchIndex(source, -1) {
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
// Kotlin — JUnit5 / kotlin.test / kotest / spek / mockk  (deep linkage, #3437)
//
// Deep linkage covers four families:
//
//	junit5  — @Test / @ParameterizedTest / @RepeatedTest fun (incl. backtick
//	          names) and @Nested inner classes. Subject derived from the
//	          enclosing class name (UserServiceTest → UserService) so a class-
//	          level naming-convention edge is emitted when no direct call is
//	          found, mirroring the C#/Java describeSubject path.
//	kotest   — spec-style DSL test cases: StringSpec `"desc" { … }`,
//	          FunSpec `test("x") { … }`, DescribeSpec/BehaviorSpec
//	          (describe/context/given/when/then/it), ShouldSpec `should("x")`.
//	          The case body is scanned for the production call.
//	spek     — describe/context/group/it DSL (Spek2). Same body-scan approach.
//	mockk    — `mockk<T>()` records the mocked type T as a describeSubject hint;
//	          the mocked call (`every { svc.foo() }` / `verify { svc.foo() }`)
//	          is NOT treated as the tested subject (its receiver is a mock) —
//	          the mockk DSL verbs are stop-worded in resolver.go.
// ---------------------------------------------------------------------------

// kotlinJUnitTestRE matches a JUnit5/kotlin.test annotated function. It accepts
// @Test, @ParameterizedTest and @RepeatedTest (each optionally carrying an
// argument list), optional additional annotation lines (e.g. @DisplayName(…),
// @ValueSource(…)) between the test annotation and the `fun`, and backtick fun
// names containing spaces. Group 1 captures the function name (with backticks).
var kotlinJUnitTestRE = regexp.MustCompile(
	`(?m)@(?:Test|ParameterizedTest|RepeatedTest)(?:\s*\([^)]*\))?` +
		`(?:\s*@\w+(?:\s*\([^)]*\))?)*` +
		`\s*(?:public\s+|private\s+|internal\s+|protected\s+|override\s+|suspend\s+)*` +
		`fun\s+(` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)\s*\([^)]*\)\s*(?::\s*[\w<>.,? ]+\s*)?{`,
)

// kotlinClassRE captures the first declared class name in a Kotlin source file.
// Used to derive the JUnit subject under test from the test-class name.
var kotlinClassRE = regexp.MustCompile(
	`(?m)^\s*(?:(?:public|private|internal|abstract|open|final|sealed|data)\s+)*class\s+(\w+)`,
)

// kotlinSpecClassRE captures a kotest/spek spec class or object and its spec
// style. Group 1 is the class/object name, group 2 the spec base. Kotest specs
// declare `class FooTest : StringSpec({ … })`; Spek2 declares
// `object FooSpec : Spek({ … })`. Both `class` and `object` are accepted.
var kotlinSpecClassRE = regexp.MustCompile(
	`(?m)\b(?:class|object)\s+(\w+)\s*:\s*(StringSpec|FunSpec|DescribeSpec|BehaviorSpec|ShouldSpec|FreeSpec|WordSpec|AnnotationSpec|ExpectSpec|FeatureSpec|Spek)\b`,
)

// kotestStringCaseRE matches a StringSpec / FreeSpec leaf case: a string literal
// immediately followed by a `{` lambda — `"adds two numbers" { … }`. Group 1 is
// the description.
var kotestStringCaseRE = regexp.MustCompile(
	`(?m)"([^"]{1,200})"\s*{`,
)

// kotestFnCaseRE matches a kotest DSL case introduced by a verb taking a string
// description: test("x"){}, describe("x"){}, context("x"){}, given("x"){},
// `when`("x"){}, then("x"){}, it("x"){}, should("x"){}, feature/scenario/expect.
// Group 1 = verb, group 2 = description.
var kotestFnCaseRE = regexp.MustCompile(
	"(?m)\\b(test|describe|context|given|`when`|when|then|it|should|feature|scenario|expect|xtest|xdescribe|xcontext|xit)\\s*\\(\\s*\"([^\"]{1,200})\"\\s*\\)\\s*{",
)

// kotlinMockkTypeRE captures the mocked type T in `mockk<T>()` / `spyk<T>()` /
// `mockkClass(T::class)`. Group 1 or group 2 carries the type name. The mocked
// type is the subject the test exercises through the mock.
var kotlinMockkTypeRE = regexp.MustCompile(
	`(?m)\b(?:mockk|spyk)\s*<\s*([A-Z]\w*)\s*>|\bmockkClass\s*\(\s*([A-Z]\w*)::class`,
)

// kotlinMockkBlockRE locates the start of a MockK stubbing/verification block —
// `every {`, `coEvery {`, `verify {`, `coVerify {`, `verifyOrder {`,
// `verifySequence {`, `verifyAll {`, `excludeRecords {`. The mocked call inside
// these blocks is on a mock receiver, NOT the production subject, so the block
// body is blanked before the resolver scans for production calls. The mocked
// type is still recorded separately as the describeSubject (mockk<T>()).
var kotlinMockkBlockRE = regexp.MustCompile(
	`\b(?:every|coEvery|verify|coVerify|verifyOrder|verifySequence|verifyAll|excludeRecords)\s*(?:\([^)]*\))?\s*{`,
)

// blankKotlinMockkBlocks blanks out every MockK every/verify lambda body in body
// (replacing characters with spaces, preserving offsets and newlines) so the
// resolver never treats the mocked call (e.g. `gateway.charge(...)` inside
// `every { … }`) as a tested production subject.
func blankKotlinMockkBlocks(body string) string {
	if !kotlinMockkBlockRE.MatchString(body) {
		return body
	}
	out := body
	for {
		loc := kotlinMockkBlockRE.FindStringIndex(out)
		if loc == nil {
			break
		}
		// The lambda body starts at the trailing `{` of the match.
		block := extractBraceBody(out, loc[1]-1)
		if block == "" {
			// Unbalanced — blank from the match start to end of the keyword to
			// avoid an infinite loop, then stop.
			out = out[:loc[0]] + strings.Repeat(" ", loc[1]-loc[0]) + out[loc[1]:]
			continue
		}
		blockStart := strings.Index(out[loc[0]:], block) + loc[0]
		blanked := subtractRanges(out, [][2]int{{loc[0], blockStart + len(block)}})
		out = blanked
	}
	return out
}

// kotlinSubjectFromClassName derives the class under test from a Kotlin test
// class name by stripping trailing "Tests"/"Test"/"Spec" suffixes.
//
//	UserServiceTest  → UserService
//	UserServiceTests → UserService
//	UserServiceSpec  → UserService
//	(no suffix)      → ""
func kotlinSubjectFromClassName(className string) string {
	for _, suf := range []string{"Tests", "Test", "Spec"} {
		if strings.HasSuffix(className, suf) && len(className) > len(suf) {
			return className[:len(className)-len(suf)]
		}
	}
	return ""
}

// kotlinFirstClassName returns the first class name declared in source.
func kotlinFirstClassName(source string) string {
	if m := kotlinClassRE.FindStringSubmatch(source); m != nil {
		return m[1]
	}
	return ""
}

// kotlinMockkSubject returns the first mocked type found via mockk<T>()/spyk<T>()
// /mockkClass(T::class), or "".
func kotlinMockkSubject(source string) string {
	if m := kotlinMockkTypeRE.FindStringSubmatch(source); m != nil {
		if m[1] != "" {
			return m[1]
		}
		return m[2]
	}
	return ""
}

// detectKotlinTest detects Kotlin test cases across junit5, kotest, spek and
// mockk. kotest/spek spec files are handled first (they declare a recognised
// spec base class); plain annotated junit5 files fall through to the @Test path.
func detectKotlinTest(source string) []testFunction {
	// kotest / spek spec-class files: scan DSL cases inside the spec lambda.
	if specClass := kotlinSpecClassRE.FindStringSubmatch(source); specClass != nil {
		className := specClass[1]
		subject := kotlinSubjectFromClassName(className)
		// A mockk subject (mockk<UserService>()) is more specific than the class
		// name when the spec name does not encode the subject.
		if mockSub := kotlinMockkSubject(source); mockSub != "" {
			subject = mockSub
		}
		return detectKotlinSpecCases(source, subject)
	}

	// junit5 / kotlin.test annotated functions.
	className := kotlinFirstClassName(source)
	subject := kotlinSubjectFromClassName(className)
	if mockSub := kotlinMockkSubject(source); mockSub != "" && subject == "" {
		subject = mockSub
	}

	var out []testFunction
	seen := map[string]bool{}
	for _, m := range kotlinJUnitTestRE.FindAllStringSubmatchIndex(source, -1) {
		name := strings.Trim(source[m[2]:m[3]], "`")
		// Backtick names can contain spaces — normalise.
		name = strings.ReplaceAll(name, " ", "_")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		body := blankKotlinMockkBlocks(extractBraceBody(source, m[1]-1))
		out = append(out, testFunction{qname: name, body: body, describeSubject: subject})
	}
	return out
}

// detectKotlinSpecCases scans a kotest/spek spec file for leaf DSL cases and
// returns one testFunction per case. Each case carries its body (scanned for
// production calls) and the spec-level describeSubject (mockk type or class-name
// convention) as a medium-confidence fallback.
func detectKotlinSpecCases(source, subject string) []testFunction {
	var out []testFunction
	seen := map[string]bool{}
	add := func(name, body string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, testFunction{
			qname:           name,
			body:            blankKotlinMockkBlocks(body),
			describeSubject: subject,
		})
	}

	// Leaf verbs introduce an actual test case; container verbs only group cases.
	// We collect leaf-verb cases first; when at least one leaf exists, container
	// cases (describe/context/given/feature/scenario/when) are skipped so the
	// finest-grained leaf wins and we don't emit a redundant container case whose
	// body merely re-scans the same nested production calls.
	leafVerb := map[string]bool{
		"test": true, "it": true, "should": true, "then": true,
		"expect": true, "xtest": true, "xit": true, "scenario": true,
	}

	type caseHit struct {
		verb, name, body string
	}
	var hits []caseHit
	haveLeaf := false
	for _, m := range kotestFnCaseRE.FindAllStringSubmatchIndex(source, -1) {
		verb := strings.Trim(source[m[2]:m[3]], "`")
		desc := source[m[4]:m[5]]
		body := extractBraceBody(source, m[1]-1)
		if leafVerb[verb] {
			haveLeaf = true
		}
		hits = append(hits, caseHit{verb: verb, name: jestCaseQName(desc), body: body})
	}
	for _, h := range hits {
		if haveLeaf && !leafVerb[h.verb] {
			continue
		}
		add(h.name, h.body)
	}

	// StringSpec/FreeSpec leaf cases: "desc" { … }. Only scanned when no verb-
	// style cases were found, to avoid double-counting the string argument of a
	// verb-style case and to keep the finest-grained leaf as the case.
	if len(hits) == 0 {
		for _, m := range kotestStringCaseRE.FindAllStringSubmatchIndex(source, -1) {
			desc := source[m[2]:m[3]]
			body := extractBraceBody(source, m[1]-1)
			add(jestCaseQName(desc), body)
		}
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
// Scala — ScalaTest / specs2 / MUnit / ZIO Test  (deep linkage, #3457)
//
// Deep linkage covers four families, all subject-aware: the class/object under
// test is derived from the spec's own type name (UserServiceSpec → UserService),
// mirroring the C#/Kotlin/Java describeSubject path, so a naming-convention
// TESTS edge is emitted even when a leaf case contains no direct production
// call. Where a leaf case body DOES contain a production call, the resolver
// promotes it to a high-confidence edge (the Scala assertion helpers — assert,
// assertResult, mustBe, shouldBe, assertTrue, … — are stop-worded in
// resolver.go so they never masquerade as the tested subject).
//
//	scalatest — AnyFunSuite     test("desc") { … }
//	            AnyFlatSpec     "X" should "do y" in { … }
//	            AnyWordSpec     "X" should { "do y" in { … } }
//	            AnyFunSpec      describe("X") { it("does y") { … } }
//	specs2    — class XSpec extends Specification { "x" should { "y" in { … } } }
//	            (the `>>` and `in` leaf forms are both accepted)
//	munit     — class XSuite extends FunSuite { test("y") { … } }
//	zio-test  — object XSpec extends ZIOSpecDefault {
//	                def spec = suite("x")(test("y")(assertTrue(…)))
//	            }
// ---------------------------------------------------------------------------

// scalaSpecTypeRE captures the first class/object name in a Scala test source so
// the subject under test can be derived from it (FooSpec → Foo). Both `class`
// and `object` (zio-test specs are objects) are accepted, as are the optional
// `final`/`abstract`/`sealed`/`case` modifiers.
var scalaSpecTypeRE = regexp.MustCompile(
	`(?m)^\s*(?:(?:final|abstract|sealed|case|private|protected)\s+)*(?:class|object)\s+(\w+)`,
)

// scalaFunSuiteCaseRE matches a ScalaTest AnyFunSuite / MUnit FunSuite leaf:
//
//	test("computes the total") { … }
//
// Group 1 is the description. Shared by scalatest-funsuite and munit (both use
// the identical `test("…") { … }` surface).
var scalaFunSuiteCaseRE = regexp.MustCompile(
	`(?m)\btest\s*\(\s*"([^"]{1,200})"\s*\)\s*{`,
)

// scalaFlatSpecCaseRE matches a ScalaTest AnyFlatSpec leaf:
//
//	"A Stack" should "pop values in LIFO order" in { … }
//	"it"      must    "do something"           in { … }
//
// Group 1 is the subject phrase, group 2 the verb phrase. The leaf description
// is the concatenation; the body follows `in {`.
var scalaFlatSpecCaseRE = regexp.MustCompile(
	`(?m)"([^"]{1,200})"\s+(?:should|must|can|may)\s+"([^"]{1,200})"\s+in\s*{`,
)

// scalaWordSpecLeafRE matches a ScalaTest AnyWordSpec / specs2 leaf case:
//
//	"return the user" in { … }
//	"return the user" >> { … }     (specs2 acceptance/unit style)
//
// Group 1 is the leaf description. The enclosing `"subject" should { … }` block
// is handled by the type-name subject; we only need the leaf bodies.
var scalaWordSpecLeafRE = regexp.MustCompile(
	`(?m)"([^"]{1,200})"\s+(?:in|>>)\s*{`,
)

// scalaFunSpecCaseRE matches a ScalaTest AnyFunSpec leaf:
//
//	it("does the thing") { … }
//
// Group 1 is the description. `describe("…")` containers are intentionally not
// emitted as leaves — `it` is the finest-grained case.
var scalaFunSpecCaseRE = regexp.MustCompile(
	`(?m)\bit\s*\(\s*"([^"]{1,200})"\s*\)\s*{`,
)

// scalaZioTestCaseRE matches a ZIO Test leaf inside a suite(...) tree:
//
//	test("returns the user") { … }
//	test("returns the user")(assertTrue(…))
//
// ZIO uses both the brace-lambda and the paren-thunk forms. Group 1 is the
// description; the brace-lambda form is captured by scalaFunSuiteCaseRE, so this
// RE only needs to add the paren-thunk form. Group 1 = description.
var scalaZioTestParenCaseRE = regexp.MustCompile(
	`(?m)\btest\s*\(\s*"([^"]{1,200})"\s*\)\s*\(`,
)

// scalaSubjectFromSpecName derives the class/object under test from a Scala
// spec/suite type name by stripping the conventional trailing suffixes.
//
//	UserServiceSpec  → UserService
//	UserServiceTest  → UserService
//	UserServiceSuite → UserService
//	UserServiceSpecs → UserService   (specs2 plural convention)
//	(no suffix)      → ""
func scalaSubjectFromSpecName(typeName string) string {
	for _, suf := range []string{"Specs", "Spec", "Suite", "Tests", "Test"} {
		if strings.HasSuffix(typeName, suf) && len(typeName) > len(suf) {
			return typeName[:len(typeName)-len(suf)]
		}
	}
	return ""
}

// scalaFirstSpecType returns the first class/object name declared in source.
func scalaFirstSpecType(source string) string {
	if m := scalaSpecTypeRE.FindStringSubmatch(source); m != nil {
		return m[1]
	}
	return ""
}

// scalaFlatSpecSubjectRE captures the leading string-literal subject of a
// ScalaTest FlatSpec / specs2 block: the quoted phrase immediately before the
// `should` / `must` / `can` / `when` / `in` chaining verb that opens a spec
// behaviour group. For `"OrderService" should "place an order" in { … }` group
// 1 is `OrderService`. specs2's `"OrderService" should { … }` block form is
// caught by the same shape. #4360 — Tier-2 subject affinity.
var scalaFlatSpecSubjectRE = regexp.MustCompile(
	`(?m)"([A-Za-z_][\w]*)"\s+(?:should|must|can|when)\b`,
)

// scalaNewSubjectRE captures a locally-constructed system-under-test:
// `new OrderService(...)` / `new OrderService`. Group 1 is the type. #4360 —
// Tier-3 subject affinity (instantiated SUT).
var scalaNewSubjectRE = regexp.MustCompile(
	`\bnew\s+([A-Z][\w]*)\b`,
)

// scalaMockSubjectRE captures a mockito-scala / scalamock SUT-shaped target:
// `mock[OrderService]` / `mock[OrderService](...)`. Group 1 is the mocked type.
// #4360 — Tier-3 subject affinity (mocked SUT).
var scalaMockSubjectRE = regexp.MustCompile(
	`\bmock\s*\[\s*([A-Z][\w]*)\s*\]`,
)

// scalaSubjectStopwords are common ScalaTest / specs2 / library type names that
// the leading-literal (Tier 2) and `new`/`mock` (Tier 3) passes must never
// surface as the production subject under test — they are test-harness or
// stdlib constructs, not the SUT. Lower-cased for case-insensitive lookup is
// not needed (these are exact type names), so they are matched verbatim.
var scalaSubjectStopwords = map[string]bool{
	// ScalaTest / specs2 fixtures & matchers commonly instantiated in a body.
	"TestKit": true, "TestProbe": true, "ActorSystem": true, "Scheduler": true,
	"FakeRequest": true, "TestData": true, "FixtureParam": true,
	// stdlib / collection constructors that appear as `new X` but are never SUTs.
	"String": true, "StringBuilder": true, "Array": true, "Exception": true,
	"RuntimeException": true, "Throwable": true, "Random": true, "Date": true,
}

// scalaSubjectFromBody resolves a Tier-2/Tier-3 subject from a single leaf
// body / spec source slice: a leading string-literal subject (`"X" should`),
// then a locally-constructed `new X(...)`, then a `mock[X]`. Returns "" when no
// honest subject is found (never a spurious guess). Stopword-filtered so
// test-harness constructs are not mistaken for the SUT.
func scalaSubjectFromBody(s string) string {
	if m := scalaFlatSpecSubjectRE.FindStringSubmatch(s); m != nil {
		if cand := m[1]; !scalaSubjectStopwords[cand] {
			return cand
		}
	}
	if m := scalaNewSubjectRE.FindStringSubmatch(s); m != nil {
		if cand := m[1]; !scalaSubjectStopwords[cand] {
			return cand
		}
	}
	if m := scalaMockSubjectRE.FindStringSubmatch(s); m != nil {
		if cand := m[1]; !scalaSubjectStopwords[cand] {
			return cand
		}
	}
	return ""
}

// detectScalaTest detects ScalaTest (FunSuite/FlatSpec/WordSpec/FunSpec),
// specs2, MUnit and ZIO Test leaf cases. Every leaf is annotated with the
// subject derived from the spec type name, so a TESTS edge is emitted even for
// pure-naming-convention cases; leaves whose body contains a production call are
// promoted by the resolver to high confidence.
func detectScalaTest(source string) []testFunction {
	// Subject affinity — three honest tiers (#4360), most-trusted first:
	//   Tier 1: spec-class stem convention (OrderServiceSpec → OrderService).
	//   Tier 2: leading string-literal subject ("OrderService" should …).
	//   Tier 3: a locally-constructed SUT (new OrderService(…) / mock[OrderService]).
	// The file-level subject is the fallback (describeSubject) the resolver uses
	// only when a leaf body has no direct production call; a per-leaf body SUT
	// (Tier 2/3 inside the leaf) overrides it so each leaf links to the symbol it
	// actually exercises. Empty subject ⇒ honest no fallback edge.
	fileSubject := scalaSubjectFromSpecName(scalaFirstSpecType(source))
	if fileSubject == "" {
		fileSubject = scalaSubjectFromBody(source)
	}

	var out []testFunction
	seen := map[string]bool{}
	// flatSpecLeafEnds records the byte offset of the `{` that opens each
	// FlatSpec `"subj" should "verb" in {` leaf. The WordSpec / specs2 leaf
	// scan (`"…" in {`) overlaps the same `in {` token, so without this the
	// finer FlatSpec leaf and a redundant `"verb" in {` leaf are both emitted
	// for one test — a scaffolding duplicate (#4360). Tracking the consumed
	// brace lets the WordSpec pass skip leaves already claimed by FlatSpec.
	flatSpecLeafEnds := map[int]bool{}
	add := func(rawName, body string) {
		name := jestCaseQName(rawName)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		// Per-leaf subject: a SUT named/instantiated inside this leaf body wins
		// over the file-level fallback; otherwise inherit the file subject.
		subject := scalaSubjectFromBody(body)
		if subject == "" {
			subject = fileSubject
		}
		out = append(out, testFunction{qname: name, body: body, describeSubject: subject})
	}

	// FunSuite / MUnit / ZIO brace form: test("…") { … }
	for _, m := range scalaFunSuiteCaseRE.FindAllStringSubmatchIndex(source, -1) {
		add(source[m[2]:m[3]], extractBraceBody(source, m[1]-1))
	}
	// ZIO paren-thunk form: test("…")( … ) — body is the paren group. We reuse
	// extractBraceBody is brace-only, so capture from the opening paren to its
	// matching close via extractParenBody.
	for _, m := range scalaZioTestParenCaseRE.FindAllStringSubmatchIndex(source, -1) {
		add(source[m[2]:m[3]], scalaParenBody(source, m[1]-1))
	}
	// FlatSpec: "subject" should "verb" in { … }
	for _, m := range scalaFlatSpecCaseRE.FindAllStringSubmatchIndex(source, -1) {
		desc := source[m[2]:m[3]] + "_" + source[m[4]:m[5]]
		// m[1] is the index just past the matched `{`; record the brace offset
		// so the WordSpec leaf pass below can skip this same leaf.
		flatSpecLeafEnds[m[1]-1] = true
		add(desc, extractBraceBody(source, m[1]-1))
	}
	// FunSpec: it("…") { … }
	for _, m := range scalaFunSpecCaseRE.FindAllStringSubmatchIndex(source, -1) {
		add(source[m[2]:m[3]], extractBraceBody(source, m[1]-1))
	}
	// WordSpec / specs2 leaf: "…" in { … } / "…" >> { … }. Scanned last so that
	// the more specific FlatSpec/FunSpec forms claim their descriptions first
	// (jestCaseQName de-dupes identical leaf names via `seen`). A leaf whose
	// opening `{` was already consumed by the FlatSpec pass is skipped so the
	// same test is not emitted twice under a shorter, verb-only name (#4360).
	for _, m := range scalaWordSpecLeafRE.FindAllStringSubmatchIndex(source, -1) {
		if flatSpecLeafEnds[m[1]-1] {
			continue
		}
		add(source[m[2]:m[3]], extractBraceBody(source, m[1]-1))
	}

	return out
}

// scalaParenBody returns the substring of source from the first `(` at or after
// startAt to its balanced `)`. Used for the ZIO `test("…")( … )` paren-thunk
// form. Mirrors extractBraceBody for parentheses; quote-aware so a `)` inside a
// string literal does not close the group prematurely.
func scalaParenBody(source string, startAt int) string {
	n := len(source)
	i := startAt
	for i < n && source[i] != '(' {
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
		case '(':
			depth++
		case ')':
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
// Elixir — ExUnit / StreamData  (deep linkage, #3473)
//
// Deep linkage covers two families, both subject-aware: the module under test
// is derived from the test module name (FooTest → Foo), mirroring the
// C#/Kotlin/Scala describeSubject path, so a naming-convention TESTS edge is
// emitted even for a case whose body contains no direct production call. A
// case body that calls a production function (Foo.bar(...)) is promoted by the
// resolver to a high-confidence edge (the ExUnit assertion macros — assert,
// refute, assert_raise, assert_received, assert_in_delta, catch_throw — are
// stop-worded in resolver.go so they never masquerade as the tested subject).
//
//	exunit     — defmodule FooTest do
//	                use ExUnit.Case
//	                test "does x" do … end
//	                describe "group" do test "does y" do … end end
//	             end
//	streamdata — property "round-trips" do check all … end  (StreamData)
//
// Elixir delimits blocks with `do … end` (not braces), so a dedicated
// balanced `do`/`end` body capture is used (extractElixirDoBody). describe
// containers are NOT emitted as their own case — the inner `test`/`property`
// leaves are the finest-grained cases (the describe body is re-scanned via the
// leaf bodies), matching the kotest/scalatest container-suppression rule.
// ---------------------------------------------------------------------------

// elixirTestModuleRE captures the first test module name in an Elixir test
// source so the subject under test can be derived from it (FooTest → Foo).
// Elixir test modules conventionally end in `Test` and are namespaced with
// `.` (e.g. MyApp.UserServiceTest); group 1 captures the full dotted name.
var elixirTestModuleRE = regexp.MustCompile(
	`(?m)^\s*defmodule\s+([A-Z][A-Za-z0-9_.]*)\s+do\b`,
)

// elixirTestCaseRE matches an ExUnit `test "description" do` leaf case. The
// description is a double-quoted string; group 1 captures it. ExUnit also
// accepts a trailing context argument (`test "x", %{conn: conn} do`), so the
// match tolerates anything between the closing quote and the `do`.
var elixirTestCaseRE = regexp.MustCompile(
	`(?m)^\s*test\s+"([^"]{1,200})"[^\n]*\bdo\b`,
)

// elixirPropertyRE matches a StreamData `property "description" do` leaf case.
// group 1 captures the description.
var elixirPropertyRE = regexp.MustCompile(
	`(?m)^\s*property\s+"([^"]{1,200})"[^\n]*\bdo\b`,
)

// elixirSubjectFromModuleName derives the module under test from an Elixir test
// module name by stripping the trailing "Test" suffix and taking the final
// dotted segment:
//
//	FooTest                  → Foo
//	MyApp.UserServiceTest    → UserService
//	MyApp.Accounts.UserTest  → User
//	(no Test suffix)         → ""
func elixirSubjectFromModuleName(moduleName string) string {
	if !strings.HasSuffix(moduleName, "Test") || len(moduleName) <= len("Test") {
		return ""
	}
	stem := moduleName[:len(moduleName)-len("Test")]
	if idx := strings.LastIndexByte(stem, '.'); idx >= 0 {
		stem = stem[idx+1:]
	}
	return stem
}

// extractElixirDoBody returns the body of the `do … end` block whose opening
// `do` keyword ends at bodyStart (i.e. bodyStart is the offset immediately
// after the opening `do`, as produced by the leaf-case regexes whose match end
// index sits right after `\bdo\b`). It balances nested `do`/`fn` … `end`
// keyword pairs so a `describe`/`test` block containing nested blocks is
// captured in full. Inline `do:` one-liners are not treated as block openers.
// Returns "" when the block is unbalanced (caller falls back to naming
// convention). String literals are skipped so a `"do"`/`"end"` inside a string
// does not perturb the balance.
func extractElixirDoBody(source string, bodyStart int) string {
	n := len(source)
	if bodyStart < 0 || bodyStart > n {
		return ""
	}
	depth := 1
	i := bodyStart
	for i < n {
		c := source[i]
		// Skip string literals (double-quoted; Elixir also has charlists/sigils,
		// but double-quoted strings are the common case for keywords-in-strings).
		if c == '"' {
			i++
			for i < n && source[i] != '"' {
				if source[i] == '\\' {
					i += 2
					continue
				}
				i++
			}
			i++
			continue
		}
		// Match a keyword only on a word boundary.
		if isElixirWordStart(source, i) {
			if hasWordAt(source, i, "end") {
				depth--
				if depth == 0 {
					return source[bodyStart:i]
				}
				i += 3
				continue
			}
			if hasWordAt(source, i, "do") {
				// Inline `do:` does not open a block.
				if !(i+2 < n && source[i+2] == ':') {
					depth++
				}
				i += 2
				continue
			}
			if hasWordAt(source, i, "fn") {
				depth++
				i += 2
				continue
			}
		}
		i++
	}
	return ""
}

// isElixirWordStart reports whether position i begins a new identifier token
// (the preceding byte is not an identifier character).
func isElixirWordStart(source string, i int) bool {
	if i == 0 {
		return true
	}
	p := source[i-1]
	return !(p == '_' || (p >= 'a' && p <= 'z') || (p >= 'A' && p <= 'Z') || (p >= '0' && p <= '9'))
}

// hasWordAt reports whether the identifier `word` occurs at position i with a
// trailing word boundary (the character after the word is not an identifier
// character). The leading boundary is the caller's responsibility
// (isElixirWordStart).
func hasWordAt(source string, i int, word string) bool {
	if i+len(word) > len(source) {
		return false
	}
	if source[i:i+len(word)] != word {
		return false
	}
	j := i + len(word)
	if j >= len(source) {
		return true
	}
	c := source[j]
	return !(c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'))
}

// detectExUnit detects ExUnit `test "…" do` and StreamData `property "…" do`
// leaf cases. Each leaf is annotated with the subject derived from the test
// module name (FooTest → Foo) so a TESTS edge is emitted even for a leaf with
// no direct production call; leaves whose body calls a production symbol are
// promoted by the resolver to high confidence.
func detectExUnit(source string) []testFunction {
	subject := elixirSubjectFromModuleName(elixirFirstModuleName(source))

	var out []testFunction
	seen := map[string]bool{}
	add := func(rawName, body string) {
		name := jestCaseQName(rawName) // reuse string→ident scrubber
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, testFunction{qname: name, body: body, describeSubject: subject})
	}

	// ExUnit test cases: test "…" do … end
	for _, m := range elixirTestCaseRE.FindAllStringSubmatchIndex(source, -1) {
		add(source[m[2]:m[3]], extractElixirDoBody(source, m[1]))
	}
	// StreamData property tests: property "…" do … end
	for _, m := range elixirPropertyRE.FindAllStringSubmatchIndex(source, -1) {
		add(source[m[2]:m[3]], extractElixirDoBody(source, m[1]))
	}

	return out
}

// elixirFirstModuleName returns the first `defmodule` name declared in source.
func elixirFirstModuleName(source string) string {
	if m := elixirTestModuleRE.FindStringSubmatch(source); m != nil {
		return m[1]
	}
	return ""
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
	// Elixir — StreamData / ExUnit: listed FIRST because the C# xUnit import hint
	// "xunit" is a substring of the Elixir "exunit" token and matchesAnyImport
	// uses a tolerant substring match; placing the Elixir entries ahead of the C#
	// block ensures an ExUnit file (token "exunit") is not mis-attributed to the
	// xunit framework. The Elixir hints ("exunit"/"streamdata") do not substring-
	// match any C# token, so the C# entries remain correct for .cs files.
	//
	// StreamData property tests are listed before exunit so a file that imports
	// ExUnitProperties / StreamData (and necessarily ExUnit.Case too) is
	// attributed to the streamdata framework. Both share detectExUnit, which
	// surfaces `property "…" do` and `test "…" do` leaves alike.
	{
		name: "streamdata",
		importHints: []string{
			"exunitproperties", "streamdata",
		},
		detect: detectExUnit,
	},
	// Elixir — ExUnit: import hint `use ExUnit.Case` (token exunit.case / exunit)
	// or the `*_test.exs` filename convention. .exs is the script extension used
	// exclusively for tests, so the filename hint is a strong signal.
	{
		name: "exunit",
		importHints: []string{
			"exunit.case", "exunit", "exunit.casetemplate",
		},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`_test\.exs$`),
		},
		pathHints: []*regexp.Regexp{
			regexp.MustCompile(`/test/.*_test\.exs$`),
		},
		detect: detectExUnit,
	},
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
		name: "kotlin_test",
		importHints: []string{
			"kotlin.test", "org.junit", "junit.jupiter",
			// kotest spec DSL + assertions, Spek2 DSL, and MockK.
			"io.kotest", "kotest", "org.spekframework", "spek", "io.mockk", "mockk",
		},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`Test\.kt$`),
			regexp.MustCompile(`Tests\.kt$`),
			// kotest specs conventionally end in Spec.kt.
			regexp.MustCompile(`Spec\.kt$`),
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
	// Scala — specs2 / MUnit / ZIO Test: import-hints only, listed BEFORE the
	// scalatest filename-fallback entry so a file carrying an explicit framework
	// import is attributed to the right framework even though all four share the
	// detectScalaTest detector (the leaf-case surfaces overlap). Files with no
	// recognised import fall through to the scalatest entry's filename hints.
	{
		name:        "specs2",
		importHints: []string{"org.specs2", "specs2"},
		detect:      detectScalaTest,
	},
	{
		name:        "munit",
		importHints: []string{"munit"},
		detect:      detectScalaTest,
	},
	{
		name:        "zio_test",
		importHints: []string{"zio.test", "zio.Test", "ziospec"},
		detect:      detectScalaTest,
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
	// Lua — busted (BDD) and luaunit (xUnit). #3485. busted is listed FIRST so a
	// file that uses both `describe`/`it` and a luaunit require is attributed to
	// busted (the describe/it surface is the test cases). luaunit catches the
	// xUnit TestClass:testXxx style. Both detect by import token OR the
	// *_spec.lua / *_test.lua filename convention, plus a /spec/ or /tests?/ path
	// fallback for suites that require their helpers indirectly.
	{
		name:        "busted",
		importHints: []string{"busted", "luassert", "say"},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`_spec\.lua$`),
		},
		pathHints: []*regexp.Regexp{
			regexp.MustCompile(`/spec/.*\.lua$`),
		},
		detect: detectBusted,
	},
	{
		name:        "luaunit",
		importHints: []string{"luaunit"},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`_test\.lua$`),
			regexp.MustCompile(`[Tt]est.*\.lua$`),
		},
		pathHints: []*regexp.Regexp{
			regexp.MustCompile(`/tests?/.*\.lua$`),
		},
		detect: detectLuaunit,
	},
	// Nim — std/unittest (suite "...": test "...": …). #4749. Detected by the
	// `std/unittest` / `unittest` import token OR the nimble test conventions
	// (`tFoo.nim`, `*_test.nim`, `test_*.nim`, files under /tests/). The
	// `import std/unittest` form yields the `std/unittest` token (and the split
	// `std`); the older `import unittest` form yields `unittest`.
	{
		name:        "nim-unittest",
		importHints: []string{"std/unittest"},
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`_test\.nim$`),
			regexp.MustCompile(`^t[A-Z][\w]*\.nim$`),
			regexp.MustCompile(`^test_.*\.nim$`),
		},
		pathHints: []*regexp.Regexp{
			regexp.MustCompile(`/tests?/.*\.nim$`),
		},
		detect: detectNimUnittest,
	},
	// F# — Expecto (testList/testCase) + xUnit/NUnit ([<Fact>]/[<Theory>]/[<Test>])
	// (#4906). F# uses `open` (not `import`) so importTokenRE cannot gate it;
	// detection is filename/path-hint gated on the standard F# test conventions
	// and the detector self-confirms (a non-test .fs file yields zero cases and
	// is dropped downstream, like the rust_test entry). Off-side-rule bodies are
	// scanned via the shared indentation block extractor.
	{
		name: "fsharp-expecto",
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`Tests?\.fs$`),
			regexp.MustCompile(`^Test.*\.fs$`),
			regexp.MustCompile(`_test\.fs$`),
		},
		pathHints: []*regexp.Regexp{
			regexp.MustCompile(`/tests?/.*\.fs$`),
		},
		detect: detectFSharpExpecto,
	},
	// Elm — elm-test (#5375). describe "..." [ test "..." <| \_ -> … ] leaves +
	// fuzz/fuzzN/fuzzWith property cases. FILENAME/PATH gated (the standard
	// elm-test conventions `*Test.elm` / `*Tests.elm` / files under `tests/`) and
	// NOT import gated: Elm's `import Test`/`import Expect` would yield the bare
	// tokens `test`/`expect`, which the substring import matcher would over-match
	// against unrelated tokens (e.g. C++ `gtest/gtest.h` contains "test"). The
	// detector self-confirms — a non-test .elm yields zero cases and is dropped
	// downstream, exactly like the rust_test / fsharp-expecto entries.
	{
		name: "elm-test",
		filenameHints: []*regexp.Regexp{
			regexp.MustCompile(`Test\.elm$`),
			regexp.MustCompile(`Tests\.elm$`),
		},
		pathHints: []*regexp.Regexp{
			regexp.MustCompile(`/tests?/.*\.elm$`),
		},
		detect: detectElmTest,
	},
	// ---------------------------------------------------------------------
	// C/C++ — gtest, catch2, doctest, boost.test, cppunit, cpputest (#3495).
	//
	// Ordering matters because several frameworks share macro surfaces:
	//   - gtest TEST(a,b) and cpputest TEST(a,b) are identical → both gated on
	//     framework-specific import headers (gtest.h vs CppUTest/TestHarness.h)
	//     so the right detector is selected by #include, not by the macro.
	//   - catch2 and doctest share TEST_CASE("name") → catch2 is listed first;
	//     a doctest file with a doctest.h include is routed to doctest by its
	//     import hint, otherwise the shared detector (detectCatch2) handles both.
	//
	// All C/C++ entries are IMPORT-HINT gated (the #include directive, captured
	// by importTokenRE as a `<dir/file.h>` token) so plain .cpp/.h source files
	// are never mis-classified as tests. No filename convention is reliable
	// across C/C++ projects (tests live in foo_test.cpp, test_foo.cpp,
	// FooTest.cpp, tests/foo.cpp …), so filename/path hints are added as a
	// secondary signal only.
	{
		name: "gtest",
		importHints: []string{
			"gtest/gtest.h", "gtest/gtest", "gtest", "gmock/gmock.h", "gmock/gmock", "gmock",
		},
		// No filename hints: gtest, catch2 and cpputest all use *_test.cpp, so a
		// filename match would shadow the import-correct framework (selectFramework
		// returns the first match and does not retry). Detection is #include-gated.
		detect: detectGTest,
	},
	{
		name: "cpputest",
		importHints: []string{
			"cpputest/testharness.h", "cpputest/testharness", "cpputest",
			"cpputest/commandlinetestrunner.h",
		},
		detect: detectCppUTest,
	},
	{
		name: "doctest",
		importHints: []string{
			"doctest/doctest.h", "doctest/doctest", "doctest.h", "doctest",
		},
		detect: detectCatch2,
	},
	{
		name: "catch2",
		importHints: []string{
			"catch2/catch_test_macros.hpp", "catch2/catch_all.hpp", "catch2/catch.hpp",
			"catch2/catch", "catch2", "catch.hpp",
		},
		// Import-gated for the same reason as gtest above.
		detect: detectCatch2,
	},
	{
		name: "boost-test",
		importHints: []string{
			"boost/test/unit_test.hpp", "boost/test/included/unit_test.hpp",
			"boost/test/auto_unit_test.hpp", "boost/test",
		},
		detect: detectBoostTest,
	},
	{
		name: "cppunit",
		importHints: []string{
			"cppunit/testfixture.h", "cppunit/extensions/helpermacros.h",
			"cppunit/testcase.h", "cppunit/ui/text/testrunner.h", "cppunit",
		},
		detect: detectCppUnit,
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
		case "pytest":
			// pytest's import hints include the bare `unittest` token, which
			// also appears in Nim's `import unittest` / `import std/unittest`.
			// pytest is ordered before the nim-unittest entry, so skip it for
			// `.nim` files (the nim-unittest entry wins) — the same precedent as
			// the junit/.kt skip below. (#4749)
			if strings.HasSuffix(strings.ToLower(filePath), ".nim") {
				continue
			}
			if importMatch || fileMatch || pathMatch {
				return fe
			}
		case "junit":
			// The Java JUnit entry shares import hints (org.junit / junit.jupiter)
			// with Kotlin tests, and is listed before kotlin_test. A Kotlin test
			// using org.junit.jupiter.* would otherwise be routed to detectJUnit
			// (which scans for `void` Java methods) and yield zero results. Skip
			// the Java entry for .kt files so the kotlin_test entry wins.
			if strings.HasSuffix(strings.ToLower(filePath), ".kt") {
				continue
			}
			if importMatch || fileMatch || pathMatch {
				return fe
			}
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
