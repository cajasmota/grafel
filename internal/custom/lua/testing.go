// testing.go — Lua test linkage extractor (tests_linkage).
//
// Covers the Testing lane for Lua frameworks by detecting:
//
//	busted (dominant Lua BDD test runner):
//	  - describe("name", function() ... end) block registration
//	  - it("name", function() ... end) test cases
//	  - before_each / after_each hooks
//	  - assert.equal / assert.same / assert.truthy / assert.falsy / assert.has_error
//	  - spy.on / stub / mock
//
//	luaunit (xUnit-style):
//	  - TestCase class patterns: local MyTest = {}
//	  - function MyTest:testXxx() ... end
//	  - luaunit.run() invocation
//
//	lapis.spec (Lapis integration testing):
//	  - use_test_server() + request() patterns
//	  - lapis.spec.request module
//
//	Kong PDK test helpers:
//	  - helpers.start_kong / helpers.stop_kong
//	  - spec.helpers module
//
// All cells are partial: detect test framework usage and test function
// definitions. Full TESTS edge linkage (test → production entity) requires
// the multi-hop HTTP engine pass which operates on the full graph.
package lua

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("lua_testing", &luaTestingExtractor{})
}

// luaTestingExtractor detects test patterns in Lua source files.
type luaTestingExtractor struct{}

func (e *luaTestingExtractor) Language() string { return "lua_testing" }

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

var (
	// busted describe blocks
	reBustedDescribe = regexp.MustCompile(
		`(?m)\b(describe|context|setup|teardown)\s*\(\s*["']([^"']+)["']`)

	// busted it() test cases
	reBustedIt = regexp.MustCompile(
		`(?m)\b(it|pending|spec)\s*\(\s*["']([^"']+)["']`)

	// busted hooks
	reBustedHook = regexp.MustCompile(
		`(?m)\b(before_each|after_each|before_all|after_all|setup|teardown)\s*\(`)

	// busted assertions
	reBustedAssert = regexp.MustCompile(
		`(?m)\bassert\s*\.\s*(equal|same|truthy|falsy|has_error|not_equal|are|is_nil|is_not_nil)\s*\(`)

	// busted spies / stubs / mocks
	reBustedSpy = regexp.MustCompile(
		`(?m)\b(spy\.on|stub|mock)\s*[\({]`)

	// require("busted") or require "busted"
	reBustedRequire = regexp.MustCompile(
		`(?m)\brequire\s*[("']busted["']?\)?`)

	// luaunit: require("luaunit") / local lu = require("luaunit")
	reLuaunitRequire = regexp.MustCompile(
		`(?m)\brequire\s*[("']luaunit["']?\)?`)

	// luaunit test method: function ClassName:testXxx()
	reLuaunitTest = regexp.MustCompile(
		`(?m)\bfunction\s+\w+\s*:\s*(test\w+)\s*\(`)

	// luaunit.run() invocation
	reLuaunitRun = regexp.MustCompile(
		`(?m)\b(?:luaunit|lu)\s*\.\s*run\s*\(`)

	// Lapis spec: require("lapis.spec") / use_test_server
	reLapisSpec = regexp.MustCompile(
		`(?m)\brequire\s*[("']lapis\.spec[.a-z]*["']?\)?|\buse_test_server\s*\(\s*\)`)

	// Kong spec helpers
	reKongHelpers = regexp.MustCompile(
		`(?m)\brequire\s*[("']spec\.helpers["']?\)?|\bhelpers\.(?:start_kong|stop_kong)\s*\(`)
)

// Extract implements extractor.Extractor.
func (e *luaTestingExtractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)

	// Only process spec/test files or files with test signals.
	fp := strings.ToLower(file.Path)
	isTestFile := strings.Contains(fp, "_spec.lua") || strings.Contains(fp, "_test.lua") ||
		strings.Contains(fp, "/spec/") || strings.Contains(fp, "/test/") ||
		strings.Contains(fp, "/tests/")
	hasTestSignal := strings.Contains(src, "describe(") || strings.Contains(src, "it(") ||
		strings.Contains(src, "luaunit") || strings.Contains(src, "busted") ||
		strings.Contains(src, "lapis.spec") || strings.Contains(src, "spec.helpers")
	if !isTestFile && !hasTestSignal {
		return nil, nil
	}

	var out []types.EntityRecord

	// busted describe/context blocks
	for _, idx := range reBustedDescribe.FindAllStringSubmatchIndex(src, -1) {
		keyword := src[idx[2]:idx[3]]
		name := src[idx[4]:idx[5]]
		ln := lineOf(src, idx[0])
		entity := makeEntity("describe:"+name, string(types.EntityKindPattern), "test_suite", file.Path, "lua", ln)
		setProps(&entity, "signal", "testing", "framework", "busted", "kind", keyword, "suite_name", name)
		out = append(out, entity)
	}

	// busted it() test cases
	for _, idx := range reBustedIt.FindAllStringSubmatchIndex(src, -1) {
		name := src[idx[4]:idx[5]]
		ln := lineOf(src, idx[0])
		entity := makeEntity("test:"+name, string(types.EntityKindPattern), "test_case", file.Path, "lua", ln)
		setProps(&entity, "signal", "testing", "framework", "busted", "kind", "test_case", "test_name", name)
		out = append(out, entity)
	}

	// busted hooks
	for _, idx := range reBustedHook.FindAllStringSubmatchIndex(src, -1) {
		hook := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		entity := makeEntity("hook:"+hook, string(types.EntityKindPattern), "test_hook", file.Path, "lua", ln)
		setProps(&entity, "signal", "testing", "framework", "busted", "kind", hook)
		out = append(out, entity)
	}

	// busted assertions
	for _, idx := range reBustedAssert.FindAllStringSubmatchIndex(src, -1) {
		assertion := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		entity := makeEntity("assert."+assertion, string(types.EntityKindPattern), "test_assertion", file.Path, "lua", ln)
		setProps(&entity, "signal", "testing", "framework", "busted", "kind", "assertion", "assertion_type", assertion)
		out = append(out, entity)
	}

	// busted spy/stub/mock
	for _, idx := range reBustedSpy.FindAllStringSubmatchIndex(src, -1) {
		kind := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		entity := makeEntity("test_double:"+kind, string(types.EntityKindPattern), "test_double", file.Path, "lua", ln)
		setProps(&entity, "signal", "testing", "framework", "busted", "kind", kind)
		out = append(out, entity)
	}

	// luaunit require
	if reLuaunitRequire.MatchString(src) {
		idx := reLuaunitRequire.FindStringIndex(src)
		ln := lineOf(src, idx[0])
		entity := makeEntity("luaunit_import", string(types.EntityKindPattern), "test_framework", file.Path, "lua", ln)
		setProps(&entity, "signal", "testing", "framework", "luaunit", "kind", "import")
		out = append(out, entity)
	}

	// luaunit test methods
	for _, idx := range reLuaunitTest.FindAllStringSubmatchIndex(src, -1) {
		name := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		entity := makeEntity("test:"+name, string(types.EntityKindPattern), "test_case", file.Path, "lua", ln)
		setProps(&entity, "signal", "testing", "framework", "luaunit", "kind", "test_method", "test_name", name)
		out = append(out, entity)
	}

	// luaunit.run()
	if reLuaunitRun.MatchString(src) {
		idx := reLuaunitRun.FindStringIndex(src)
		ln := lineOf(src, idx[0])
		entity := makeEntity("luaunit_run", string(types.EntityKindPattern), "test_runner", file.Path, "lua", ln)
		setProps(&entity, "signal", "testing", "framework", "luaunit", "kind", "run_invocation")
		out = append(out, entity)
	}

	// Lapis spec
	if reLapisSpec.MatchString(src) {
		idx := reLapisSpec.FindStringIndex(src)
		ln := lineOf(src, idx[0])
		entity := makeEntity("lapis_spec_import", string(types.EntityKindPattern), "test_framework", file.Path, "lua", ln)
		setProps(&entity, "signal", "testing", "framework", "lapis", "kind", "spec_import")
		out = append(out, entity)
	}

	// Kong spec helpers
	if reKongHelpers.MatchString(src) {
		idx := reKongHelpers.FindStringIndex(src)
		ln := lineOf(src, idx[0])
		entity := makeEntity("kong_spec_helpers", string(types.EntityKindPattern), "test_framework", file.Path, "lua", ln)
		setProps(&entity, "signal", "testing", "framework", "kong", "kind", "spec_helpers")
		out = append(out, entity)
	}

	return out, nil
}
