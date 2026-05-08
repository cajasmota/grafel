// Package testmap — call / mock resolution inside a test function body.
//
// Given a test function's raw body text, resolveCalls returns the best-guess
// list of production functions the test exercises. The resolver is
// intentionally lightweight — it does not consult a symbol table; that is a
// post-processing concern of the Transform stage.
//
// Confidence ladder (from MX-1051):
//
//	high   — direct call to an identifier that looks like a production
//	         function. The identifier must not itself be a test/mock/assert
//	         helper.
//	medium — a mock set-up line (`mock.On("Name"…)`, `when(svc.name(…))`,
//	         `stub(obj).method`) names a production function.
//	low    — nothing of the above was found; we emit a single low-confidence
//	         mapping to the production symbol guessed from the test file name.
package testmap

import (
	"regexp"
	"sort"
	"strings"
)

// directCallRE finds all `Identifier(` and `pkg.Identifier(` / `obj.Method(`
// invocation sites in a text body. Each match yields the trailing identifier
// (the thing being called).
var directCallRE = regexp.MustCompile(
	`\b([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\s*\(`,
)

// mockSetupRE captures common mock-library setup lines. The first capture
// group is the qualified production identifier being stubbed.
//
// Covered styles:
//
//	mock.On("MethodName", …)                       (testify — Go)
//	mocker.patch("module.func", …)                 (pytest mock — Python)
//	when(svc.method(…)).thenReturn(…)              (Mockito — Java/Kotlin)
//	stub(obj).method { … }                         (Mockito-Kotlin)
//	jest.mock('module', …) / jest.spyOn(obj, 'name')  (Jest — JS/TS)
//	sinon.stub(obj, 'name')                        (Sinon — JS/TS)
//	allow(obj).to receive(:name)                   (RSpec)
var mockSetupREs = []*regexp.Regexp{
	regexp.MustCompile(`\bmock\.On\s*\(\s*["']([\w.]+)["']`),
	regexp.MustCompile(`\bmocker\.patch\s*\(\s*["']([\w.]+)["']`),
	regexp.MustCompile(`\bpatch\s*\(\s*["']([\w.]+)["']`),
	regexp.MustCompile(`\bwhen\s*\(\s*[\w.]+\.(\w+)\s*\(`),
	regexp.MustCompile(`\bstub\s*\(\s*[\w.]+\s*\)\s*\.(\w+)`),
	regexp.MustCompile(`\bjest\.spyOn\s*\(\s*[\w.]+\s*,\s*['"](\w+)['"]`),
	regexp.MustCompile(`\bsinon\.stub\s*\(\s*[\w.]+\s*,\s*['"](\w+)['"]`),
	regexp.MustCompile(`\ballow\s*\(\s*[\w.]+\s*\)\s*\.\s*to\s+receive\s*\(\s*:(\w+)`),
}

// stopwords is the set of identifiers that are NEVER considered production
// calls — test framework entry points, assertion helpers, mock libraries, and
// common standard-library utilities that would otherwise dominate the output.
//
// Kept lowercase; resolver compares with strings.EqualFold.
var stopwords = map[string]bool{
	// Go testing
	"t.run": true, "t.errorf": true, "t.fatalf": true, "t.error": true,
	"t.fatal": true, "t.log": true, "t.logf": true, "t.helper": true,
	"t.parallel": true, "t.skip": true, "t.skipf": true, "t.cleanup": true,
	"t.name": true, "t.tempdir": true, "t.failed": true,
	"testing.short": true,
	"require.noerror": true, "require.equal": true, "require.nil": true,
	"require.notnil": true, "require.true": true, "require.false": true,
	"assert.equal": true, "assert.nil": true, "assert.notnil": true,
	"assert.noerror": true, "assert.error": true, "assert.true": true,
	"assert.false": true, "assert.len": true, "assert.empty": true,
	"assert.contains": true, "assert.notequal": true, "assert.notcontains": true,
	// Python / pytest
	"assert": true, "assertequal": true, "assertnotequal": true,
	"asserttrue": true, "assertfalse": true, "assertraises": true,
	"assertin": true, "assertisnotnone": true, "assertisnone": true,
	"self.assertequal": true, "self.asserttrue": true, "self.assertfalse": true,
	"self.assertraises": true, "pytest.raises": true, "pytest.fixture": true,
	"pytest.mark.parametrize": true, "pytest.skip": true,
	// Jest / Mocha / Chai
	"expect": true, "expect.tobe": true, "expect.toequal": true,
	"jest.fn": true, "jest.mock": true, "jest.spyon": true,
	"beforeeach": true, "aftereach": true, "beforeall": true, "afterall": true,
	"it": true, "test": true, "describe": true,
	// JUnit
	"assertions.assertequals": true, "assertequals": true,
	"assertnull": true, "assertnotnull": true,
	// RSpec
	"allow": true, "receive": true, "expect.to": true,
	// Rust
	"assert_eq": true, "assert_ne": true, "assert_ne!": true, "assert_eq!": true,
	// Common language keywords that end up in call-like positions
	"if": true, "for": true, "while": true, "switch": true, "return": true,
	"func": true, "def": true, "class": true, "struct": true, "new": true,
	"fn": true, "let": true, "var": true, "const": true, "throw": true,
	"catch": true, "try": true, "async": true, "await": true, "with": true,
	"print": true, "println": true, "println!": true, "println!(": true,
	"fmt.println": true, "fmt.printf": true, "fmt.sprintf": true, "fmt.errorf": true,
	"errors.new": true, "errors.is": true, "errors.as": true,
	"make": true, "len": true, "cap": true, "append": true, "copy": true,
	"string": true, "int": true, "bool": true, "map": true, "list": true,
	"range": true,
}

// isStopword reports whether id is a test-helper, assertion, mock library,
// language keyword, or other non-production identifier.
func isStopword(id string) bool {
	low := strings.ToLower(id)
	if stopwords[low] {
		return true
	}
	// Any identifier that starts with "test" or "mock" is not a production
	// call for mapping purposes.
	if strings.HasPrefix(low, "test") || strings.HasPrefix(low, "mock") {
		return true
	}
	// Anything that looks like an assertion on a method (`.should`, `.toBe`,
	// `.toEqual`, `.toHaveBeenCalledWith`, etc.).
	for _, suf := range []string{
		".tobe", ".toequal", ".tohavebeencalled", ".tohavebeencalledwith",
		".tothrow", ".toreturn", ".should", ".expect", ".called", ".mockreturnvalue",
		".not.tobe",
	} {
		if strings.HasSuffix(low, suf) {
			return true
		}
	}
	return false
}

// tailIdent returns the last `.` segment of a dotted name. For a bare
// identifier (no dot) it returns the identifier unchanged.
func tailIdent(qname string) string {
	idx := strings.LastIndexByte(qname, '.')
	if idx < 0 {
		return qname
	}
	return qname[idx+1:]
}

// resolveCalls returns the list of (production function, confidence) pairs
// derived from a single test function body.
//
// Duplicates are collapsed — if a test calls `GetUser` three times only one
// high-confidence mapping is emitted. When a direct call and a mock setup
// both target the same symbol, high wins over medium.
//
// When no direct call and no mock line is found, the function emits exactly
// one low-confidence mapping to `convSymbol` (the naming-convention guess).
// If `convSymbol` is empty a low-confidence mapping is still emitted
// targeting the test function's stripped name (e.g. `TestGetUser` → `GetUser`).
func resolveCalls(tf testFunction, prodFile, convSymbol string) []testedCall {
	seen := map[string]string{} // qname → confidence

	upgrade := func(qname, conf string) {
		if qname == "" {
			return
		}
		if prior, ok := seen[qname]; ok {
			if rank(conf) > rank(prior) {
				seen[qname] = conf
			}
			return
		}
		seen[qname] = conf
	}

	// Pass 1: direct calls → high.
	for _, m := range directCallRE.FindAllStringSubmatch(tf.body, -1) {
		if len(m) < 2 {
			continue
		}
		qname := m[1]
		if isStopword(qname) || isStopword(tailIdent(qname)) {
			continue
		}
		// Skip obviously-local identifiers: single-letter, or all lowercase
		// single-word names shorter than 3 chars (these are usually loop
		// variables or params).
		tail := tailIdent(qname)
		if len(tail) < 3 {
			continue
		}
		upgrade(qname, "high")
	}

	// Pass 2: mock targets → medium (may upgrade to high if already present).
	for _, re := range mockSetupREs {
		for _, m := range re.FindAllStringSubmatch(tf.body, -1) {
			if len(m) < 2 {
				continue
			}
			qname := m[1]
			if isStopword(qname) || isStopword(tailIdent(qname)) {
				continue
			}
			upgrade(qname, "medium")
		}
	}

	// Pass 3: naming convention fallback when no call/mock was found.
	if len(seen) == 0 {
		sym := convSymbol
		if sym == "" {
			sym = stripTestPrefix(tf.qname)
		}
		if sym != "" {
			seen[sym] = "low"
		}
	}

	out := make([]testedCall, 0, len(seen))
	for qname, conf := range seen {
		file := ""
		if conf == "low" {
			file = prodFile
		}
		out = append(out, testedCall{
			qname:      qname,
			confidence: conf,
			prodFile:   file,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].confidence != out[j].confidence {
			return rank(out[i].confidence) > rank(out[j].confidence)
		}
		return out[i].qname < out[j].qname
	})
	return out
}

// rank gives a numeric score to a confidence level so the resolver can
// pick the highest when the same symbol appears in multiple passes.
func rank(c string) int {
	switch c {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	}
	return 0
}

// stripTestPrefix takes a test function name like "TestGetUser" or
// "test_get_user" and returns the production symbol guess ("GetUser" /
// "get_user"). Returns an empty string when no transformation applies.
func stripTestPrefix(name string) string {
	switch {
	case strings.HasPrefix(name, "Test") && len(name) > 4:
		return name[4:]
	case strings.HasPrefix(name, "test_") && len(name) > 5:
		return name[5:]
	case strings.HasPrefix(name, "it_") && len(name) > 3:
		return name[3:]
	}
	return ""
}
