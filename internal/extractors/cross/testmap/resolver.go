// Package testmap — call / mock resolution inside a test function body.
//
// Given a test function's raw body text, resolveCalls returns the best-guess
// list of production functions the test exercises. The resolver is
// intentionally lightweight — it does not consult a symbol table; that is a
// post-processing concern of the Transform stage.
//
// Confidence ladder:
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
	"testing.short":   true,
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
	// Cypress
	"cy": true,
	// JUnit
	"assertions.assertequals": true, "assertequals": true,
	"assertnull": true, "assertnotnull": true,
	// Kotlin — kotest matchers (infix + paren forms), kotlin.test / JUnit5
	// assertions, and MockK DSL verbs. These are test-harness identifiers and
	// must never surface as the tested production subject. Lower-cased.
	"shouldbe": true, "shouldnotbe": true, "shouldthrow": true,
	"shouldthrowany": true, "shouldthrowexactly": true, "shouldnotthrow": true,
	"shouldcontain": true, "shouldnotcontain": true, "shouldcontainall": true,
	"shouldcontainexactly": true, "shouldstartwith": true, "shouldendwith": true,
	"shouldbeempty": true, "shouldnotbeempty": true, "shouldbenull": true,
	"shouldnotbenull": true, "shouldbetrue": true, "shouldbefalse": true,
	"shouldhavesize": true, "shouldbegreaterthan": true, "shouldbelessthan": true,
	"shouldhavekey": true, "shouldbeinstanceof": true, "shouldmatch": true,
	"shouldbeequal": true, "shouldbesameinstanceas": true, "shouldcontainkey": true,
	"assertthrows": true, "assertdoesnotthrow": true, "assertall": true,
	"assertnotsame": true, "assertcontentequals": true,
	"assertfailswith": true, "assertfails": true, "fail": true,
	// MockK DSL — mock construction, stubbing and verification verbs. The mocked
	// CALL inside every/verify is on a mock receiver, not the production subject,
	// so the whole MockK surface is stop-worded (the real subject is recorded as
	// the describeSubject from mockk<T>()).
	"every": true, "coevery": true, "verify": true, "coverify": true,
	"verifyall": true, "verifyorder": true, "verifysequence": true,
	"confirmverified": true, "clearmocks": true, "clearallmocks": true,
	"unmockkall": true, "unmockkstatic": true, "mockkstatic": true,
	"mockkobject": true, "justrun": true, "answers": true, "returnsmany": true,
	"mockk": true, "spyk": true, "mockkclass": true, "slot": true, "capture": true,
	// kotest lifecycle / property-test DSL
	"beforetest": true, "aftertest": true, "beforespec": true, "afterspec": true,
	"forall": true, "checkall": true,
	// MockK argument matchers (any()/eq()/match()/capture(slot)) — these are
	// stubbing helpers, never the production subject under test.
	"any": true, "anynullable": true, "neq": true, "less": true,
	"more": true, "oftype": true, "allany": true,
	// RSpec matchers and DSL helpers
	"allow": true, "receive": true, "expect.to": true,
	"be_valid": true, "be_nil": true, "be_present": true, "be_empty": true,
	"be_persisted": true, "be_new_record": true, "be_truthy": true, "be_falsy": true,
	"have_http_status": true, "render_template": true, "redirect_to": true,
	"be_successful": true, "be_redirect": true, "be_created": true,
	"have_received": true, "change": true, "include": true, "match": true,
	"eq": true, "eql": true, "equal": true, "respond_to": true,
	"raise_error": true, "raise_exception": true, "output": true,
	"have_attributes": true, "satisfy": true, "be_a": true, "be_an": true,
	"be_kind_of": true, "be_instance_of": true, "be_between": true,
	// RSpec Capybara / request helpers
	"have_content": true, "have_text": true, "have_selector": true,
	"have_css": true, "have_link": true, "have_button": true,
	"visit": true, "click_on": true, "click_link": true, "click_button": true,
	"fill_in": true, "choose": true, "check": true, "uncheck": true,
	// Minitest assertions
	"assert_equal": true, "assert_nil": true, "assert_not_nil": true,
	"assert_includes": true, "assert_response": true, "assert_redirected_to": true,
	"assert_template": true, "assert_difference": true, "assert_no_difference": true,
	"assert_raises": true, "assert_enqueued_jobs": true, "assert_performed_jobs": true,
	"assert_enqueued_with": true, "assert_emails": true, "refute_nil": true,
	"refute_equal": true, "refute_includes": true,
	// Rust — std assertion macros (directCallRE captures the ident WITHOUT the
	// trailing `!`, so the bare forms are the ones actually looked up; the `!`
	// variants are kept for defensiveness).
	"assert_eq": true, "assert_ne": true,
	"assert_eq!": true, "assert_ne!": true, "assert!": true,
	"assert_matches": true, "assert_matches!": true,
	"debug_assert": true, "debug_assert_eq": true, "debug_assert_ne": true,
	"panic": true, "panic!": true, "unreachable": true, "unimplemented": true,
	"todo": true, "dbg": true, "matches": true,
	// proptest assertion macros.
	"prop_assert": true, "prop_assert_eq": true, "prop_assert_ne": true,
	"prop_assume": true,
	// criterion benchmark harness helpers — test infrastructure, not prod calls.
	"black_box": true, "c.bench_function": true, "c.bench_with_input": true,
	"b.iter": true, "b.iter_batched": true, "criterion_group": true,
	"criterion_main": true, "bencher.iter": true,
	// mockall expectation builders — `.expect_*`, `.returning(`, `.times(` etc.
	// (`.returning`/`.times` are handled by the suffix filter below.)
	// C# — xUnit (non-duplicate entries only; assert.equal/true/false/empty/contains/notequal covered above)
	"assert.null": true, "assert.notnull": true, "assert.same": true, "assert.notsame": true,
	"assert.doesnotcontain": true, "assert.notempty": true,
	"assert.throwsany": true, "assert.throwsasync": true,
	"assert.istype": true, "assert.isnottype": true, "assert.isassignablefrom": true,
	"assert.inrange": true, "assert.notinrange": true,
	"assert.startswith": true, "assert.endswith": true, "assert.matches": true,
	"assert.collection": true, "assert.single": true, "assert.multiple": true,
	"assert.fail": true, "assert.skip": true,
	// C# — NUnit (non-duplicate entries)
	"assert.areequal": true, "assert.arenotequal": true, "assert.isnull": true, "assert.isnotnull": true,
	"assert.istrue": true, "assert.isfalse": true, "assert.isempty": true, "assert.isnotempty": true,
	"assert.isnan": true, "assert.ispositive": true, "assert.isnegative": true,
	"assert.greater": true, "assert.greaterorequal": true, "assert.less": true, "assert.lessorequal": true,
	"assert.catch": true, "assert.doesnotthrow": true,
	"assert.pass": true, "assert.ignore": true, "assert.inconclusive": true,
	"classicassert.areequal": true, "classicassert.istrue": true, "classicassert.isfalse": true,
	// C# — MSTest
	"assert.aresame": true, "assert.arenotsame": true, "assert.isinstanceoftype": true,
	"assert.isnotinstanceoftype": true, "assert.throwsexception": true, "assert.throwsexceptionasync": true,
	"collectionassert.areequal": true, "collectionassert.areequivalent": true,
	"collectionassert.contains": true, "collectionassert.doesnotcontain": true,
	"collectionassert.allitemsareinrangeof": true, "collectionassert.allitemsarenotnull": true,
	"collectionassert.allitemsareunique": true, "collectionassert.issubsetof": true,
	"stringassert.contains": true, "stringassert.startswith": true, "stringassert.endswith": true,
	"stringassert.matches": true, "stringassert.doesnotmatch": true,
	// C# common test framework helpers (all frameworks)
	"testcontext.writeline": true, "testcontext.write": true,
	"output.writeline": true, "output.write": true,
	// PHP — PHPUnit assertion helpers (non-duplicate; assertequal/asserttrue/assertfalse
	// /assertnull/assertnotnull/assertempty/assertnotempty/assertcontains/assertnotcontains
	// are already covered by the generic assert.* entries above).
	"assertsame": true, "assertinstanceof": true,
	"assertcount": true, "assertarrayhaskey": true,
	"assertstringcontainstring": true, "assertstringnotcontainstring": true,
	"assertmatchesregularexpression": true, "assertdatabasehas": true,
	"assertdatabasemissing": true, "assertdatabasecount": true, "assertsoftdeleted": true,
	"assertnotdeleted": true,
	// PHP — Pest DSL helpers (should not be production call targets).
	// "expect" is already in the generic stopwords block above.
	"tobetrue": true, "tobefalse": true, "tobenull": true,
	"toequal": true, "tobesame": true, "tocontain": true, "tohavecount": true,
	"tohavekey": true, "tobeempty": true, "tobeaninstanceof": true,
	"tobestring": true, "tobefloat": true, "tobeinf": true, "tobenan": true,
	"tobebetween": true, "tothrow": true, "tobegreaterhan": true,
	// PHP — Laravel feature test HTTP helpers (test infrastructure, not prod calls)
	// $this->get/post/put/patch/delete/json/getJson/postJson/putJson/patchJson/deleteJson
	"$this->get": true, "$this->post": true, "$this->put": true,
	"$this->patch": true, "$this->delete": true, "$this->json": true,
	"$this->getjson": true, "$this->postjson": true, "$this->putjson": true,
	"$this->patchjson": true, "$this->deletejson": true,
	// normalised lowercase (the resolver lowercases before lookup)
	"this.get": true, "this.post": true, "this.put": true,
	"this.patch": true, "this.delete": true, "this.json": true,
	"this.getjson": true, "this.postjson": true, "this.putjson": true,
	"this.patchjson": true, "this.deletejson": true,
	// Laravel test assertion helpers on TestResponse
	"assertstatus": true, "assertok": true, "assertcreated": true,
	"assertnotfound": true, "assertforbidden": true, "assertunauthorized": true,
	"assertredirect": true, "assertjson": true, "assertjsonpath": true,
	"assertjsonfragment": true, "assertjsoncount": true, "assertjsonstructure": true,
	"assertsee": true, "assertdontesee": true, "assertseeinorder": true,
	"assertheader": true, "assertcookie": true, "assertsession": true,
	"assertviewhas": true, "assertviewmissing": true,
	// Laravel RefreshDatabase / HTTP test lifecycle helpers
	"refreshdatabase": true, "seeddatabase": true, "withoutexceptionhandling": true,
	"withheaders": true, "actingas": true, "withtoken": true, "be": true,
	// Scala — ScalaTest / specs2 / MUnit / ZIO Test assertion & matcher helpers
	// (#3457). These are test-harness identifiers and must never surface as the
	// tested production subject. directCallRE captures the paren-call forms
	// (assert(…), assertResult(…), assertTrue(…)); the infix matcher forms
	// (`x shouldBe y`, `x must_== y`) are included defensively for the dotted
	// `x.shouldBe(y)` style. Lower-cased (resolver lowercases before lookup).
	"assertresult": true, "assertthrows_scala": true,
	"assertcompiles": true, "assertdoesnotcompile": true, "asserttypeerror": true,
	// ScalaTest matchers (Matchers / MustMatchers): infix + dotted forms.
	// (shouldbe/shouldmatch/shouldcontain/shouldnotbe already covered by the
	// Kotlin kotest matcher block above.)
	"shouldequal": true, "should_be": true,
	"mustbe": true, "mustequal": true, "must_==": true, "must_be": true,
	"mustmatch": true, "mustcontain": true,
	"mustnotbe": true, "shouldreturn": true,
	"shouldhave": true, "musthave": true,
	// specs2 matchers (`x must beEqualTo(y)`, `x mustEqual y`, `x === y`).
	"mustequalto": true, "beequalto": true, "beequalto_": true,
	"mustbeequaltoignoringcase": true, "bnull": true, "benull": true,
	"besome": true, "benone": true, "beleft": true, "beright": true,
	"betrue": true, "befalse": true, "throwa": true, "throwan": true,
	"contain_": true, "haveclass": true, "havesize": true,
	// MUnit assertion helpers (assertEquals/assertNotEquals already covered by
	// the generic assert* / assertequals entries; add the MUnit-specific ones).
	"assertnoexception": true, "interceptmessage": true, "intercept": true,
	"assertequalsdouble": true, "assertequalsfloat": true,
	// ZIO Test assertions (assertTrue covered above; add the operator forms).
	"assertz": true, "assertcompletes": true, "assertcompleteszio": true,
	"equalto": true, "isleft": true, "isright": true, "issome": true, "isnone": true,
	"issubtype": true, "haskey": true, "hasfield": true, "isgreaterthan": true,
	"islessthan": true,
	// Elixir — ExUnit assertion/lifecycle macros and StreamData property-test
	// DSL (#3473). These are test-harness identifiers and must never surface as
	// the tested production subject. `assert` is already covered by the generic
	// pytest block above. directCallRE captures the paren-call and bare-macro
	// forms (assert_raise(...), refute foo(), assert_received :msg). Lower-cased
	// (the resolver lowercases before lookup).
	"refute": true, "assert_raise": true, "refute_raise": true,
	"assert_received": true, "refute_received": true,
	"assert_receive": true, "refute_receive": true,
	"assert_in_delta": true, "refute_in_delta": true,
	"assert_in_epsilon": true, "refute_in_epsilon": true,
	"catch_throw": true, "catch_exit": true, "catch_error": true,
	"flunk": true,
	// ExUnit lifecycle / case DSL — setup, callbacks, tags. (`describe` is
	// already covered by the generic Jest/Mocha block above.)
	"setup": true, "setup_all": true, "on_exit": true, "start_supervised": true,
	"start_supervised!": true,
	// StreamData property-test DSL — `check all x <- gen`, generators, and the
	// `member_of`/`one_of`/`constant`/`map`/`bind`/`filter` combinators are
	// generator builders, not the production subject under test. (`check` is a
	// directCall-captured bare ident; `assert_raises` covered above.)
	"check_all": true, "gen": true, "member_of": true,
	"one_of": true, "constant": true, "integer": true, "binary": true,
	"list_of": true, "map_of": true, "fixed_list": true,
	"bind": true, "filter": true, "frequency": true, "tuple": true,
	// Lua — busted (luassert) and luaunit assertion / lifecycle / spy DSL (#3485).
	// These are test-harness identifiers and must never surface as the tested
	// production subject. directCallRE captures the dotted forms (assert.equal,
	// assert.are.equal, luaunit.assertEquals); the bare `assert`/`spy`/`stub`/
	// `mock` are added too. Lower-cased (resolver lowercases before lookup).
	// (`assert`, `assert.equal`, `assert.nil`, `assert.true`, `assert.false`,
	// `assert.same`, `assert.contains`, `mock`, `setup`, `describe`, `it` are
	// already covered by the generic blocks above.)
	"assert.are": true, "assert.are.equal": true, "assert.are.same": true,
	"assert.are_not": true, "assert.are_not.equal": true,
	"assert.is": true, "assert.is.truthy": true, "assert.is.falsy": true,
	"assert.is_true": true, "assert.is_false": true, "assert.is_nil": true,
	"assert.is_not_nil": true, "assert.is_not": true,
	"assert.truthy": true, "assert.falsy": true, "assert.has_error": true,
	"assert.has.errors": true, "assert.has_no.errors": true,
	"assert.no_error": true, "assert.near": true,
	"assert.not_equal": true, "assert.not_same": true, "assert.spy": true,
	"assert.stub": true, "assert.unique": true, "assert.message": true,
	// busted spy / stub / mock DSL constructors and lifecycle (setup/teardown
	// covered above; before_each/after_each are busted-specific).
	"spy": true, "spy.on": true, "spy.new": true, "stub": true, "stub.new": true,
	"before_each": true, "after_each": true, "before_all": true, "after_all": true,
	"insulate": true, "expose": true, "randomize": true, "finally": true,
	// luaunit — luaunit.assertXxx assertion family and runner entry points.
	"luaunit.assertequals": true, "luaunit.assertnotequals": true,
	"luaunit.asserttrue": true, "luaunit.assertfalse": true,
	"luaunit.assertnil": true, "luaunit.assertnotnil": true,
	"luaunit.assertis": true, "luaunit.assertnotis": true,
	"luaunit.asserterror": true, "luaunit.asserterrormsgcontains": true,
	"luaunit.assertstrcontains": true, "luaunit.assertitemsequals": true,
	"luaunit.run": true, "lu.assertequals": true, "lu.asserttrue": true,
	"lu.assertfalse": true, "lu.assertnil": true, "lu.assertnotnil": true,
	"lu.run": true, "luaunit.assertalmostequals": true,
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
	// Lua keywords and ubiquitous standard-library globals that appear in
	// call-like positions inside test bodies but are never the production subject.
	"function": true, "local": true, "end": true, "then": true, "elseif": true,
	"else": true, "until": true, "repeat": true, "nil": true, "and": true,
	"or": true, "not": true, "require": true, "pcall": true, "xpcall": true,
	"pairs": true, "ipairs": true, "tostring": true, "tonumber": true,
	"type": true, "select": true, "rawget": true, "rawset": true, "rawequal": true,
	"setmetatable": true, "getmetatable": true, "next": true, "unpack": true,
	"error": true, "assert_": true, "table.insert": true, "table.remove": true,
	"table.concat": true, "table.sort": true, "string.format": true,
	"string.match": true, "string.gmatch": true, "string.gsub": true,
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
	// RSpec matcher helpers that start with "be_", "have_", "match_" are always
	// assertion helpers, never production calls.
	if strings.HasPrefix(low, "be_") || strings.HasPrefix(low, "have_") || strings.HasPrefix(low, "match_") {
		return true
	}
	// Rust assertion macro families and mockall expectation builders. The
	// tail identifier (post `.`) is what is checked here:
	//   assert_*       — std/proptest assertion macros (assert_eq!, …)
	//   prop_assert_*  — proptest assertion macros
	//   expect_*       — mockall expectation setter `mock.expect_register()`
	//                    (the real subject is the mocked trait, recorded by the
	//                    detector — the expectation setter is not a prod call)
	tail := tailIdent(low)
	if strings.HasPrefix(tail, "assert_") || strings.HasPrefix(tail, "prop_assert") ||
		strings.HasPrefix(tail, "debug_assert") || strings.HasPrefix(tail, "expect_") {
		return true
	}
	// Rails integration/controller test HTTP dispatch methods: `get :index`,
	// `post :create`, etc. — these are test-framework helpers, not production
	// calls, even though `get`/`post` also appear in production route helpers.
	// We only drop single-word bare names (length <= 6) that match HTTP verbs.
	if low == "get" || low == "post" || low == "put" || low == "patch" || low == "delete" || low == "head" {
		return true
	}
	// Cypress global object — cy.visit(), cy.get(), etc.
	if strings.HasPrefix(low, "cy.") {
		return true
	}
	// Django / FastAPI / Starlette / Flask / aiohttp / httpx test clients.
	// self.client.post('/api/...') is an HTTP test call, not a production
	// call — the test client is a test infrastructure object. Without this
	// filter the resolver emits a high-confidence TESTS edge targeting the
	// HTTP verb ("post", "get", …) rather than the actual ViewSet handler.
	// We cover: Django (self.client.*), generic test clients (client.*),
	// async clients (async_client.*, ac.*), and aiohttp sessions (session.*).
	// (#3173)
	//
	// ASP.NET Core integration test infrastructure: _factory.CreateClient(),
	// _client.GetAsync(), response.EnsureSuccessStatusCode() etc. are all test
	// plumbing — not production calls. (#3383)
	for _, prefix := range []string{
		"self.client.", "client.", "self.async_client.", "async_client.", "ac.", "session.",
		"self.app.", "app.test_client.", "requests.",
		// ASP.NET Core / HttpClient test infrastructure
		"_factory.", "factory.", "_client.", "httpclient.",
		"response.", "_response.",
		// PHP / Laravel feature-test infrastructure (#3399).
		// $this->get('/url'), $this->post(...), $this->assertStatus(200) etc. are
		// TestResponse helpers or HTTP dispatch on the test kernel — not production calls.
		// directCallRE captures `this.get(` / `this.post(` after PHP $ is stripped by
		// the regex ($ is not a word character so `\b$this\b` never matches; the RE
		// sees `this` as the receiver). We drop any call whose receiver is `this` and
		// the method is an HTTP verb or an assertion helper — covered by stopwords above
		// for the common cases, and here for the compound `this.<verb>` form.
		"this.assertstatus", "this.assertok", "this.assertcreated", "this.assertnotfound",
		"this.assertforbidden", "this.assertunauthorized", "this.assertredirect",
		"this.assertjson", "this.assertjsonpath", "this.assertjsonfragment",
		"this.assertjsoncount", "this.assertjsonstructure",
		"this.assertsee", "this.assertdontsee", "this.assertheader",
		"this.assertcookie", "this.assertsession",
		"this.assertviewhas", "this.assertviewmissing",
		"this.assertdatabasehas", "this.assertdatabasemissing",
		"this.assertdatabasecount", "this.assertsoftdeleted",
		"this.refreshdatabase", "this.seed", "this.actingas", "this.withtoken",
		"this.withheaders", "this.withoutexceptionhandling",
	} {
		if strings.HasPrefix(low, prefix) {
			return true
		}
	}
	// Anything that looks like an assertion on a method (`.should`, `.toBe`,
	// `.toEqual`, `.toHaveBeenCalledWith`, etc.).
	for _, suf := range []string{
		".tobe", ".toequal", ".tohavebeencalled", ".tohavebeencalledwith",
		".tothrow", ".toreturn", ".should", ".expect", ".called", ".mockreturnvalue",
		".not.tobe",
		// Rust noise: .unwrap()/.expect()/.iter() on values, and mockall
		// expectation builders (.expect_x(), .returning(), .times(), .with()).
		".unwrap", ".unwrap_err", ".iter", ".returning", ".return_const",
		".times", ".never", ".with", ".withf",
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

	// Pass 3a: RSpec/Minitest describe-subject linkage.
	// When the test function was extracted from a describe/context block whose
	// subject is a named constant (e.g. `describe User do` / `class UserTest`),
	// use it as a medium-confidence target — the it-block exercises the described
	// class even when there is no explicit call site in the body.
	if len(seen) == 0 && tf.describeSubject != "" {
		seen[tf.describeSubject] = "medium"
	}

	// Pass 3b: naming convention fallback when no call/mock was found.
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
		// Issue #2060 — populate prodFile for ALL confidences, not just
		// "low". The resolver's scope:operation:<file>#<name> short-form
		// path (internal/resolve/refs.go lookupStructural) tries
		// byLocation[file][name] and a byMember[file] walk; either is
		// dramatically more likely to hit than the "?" form's global
		// byName lookup, especially for archigraph + upvate where common
		// production names (e.g. "GetUser", "create_order") are not
		// globally unique. When the convention couldn't infer a file
		// (no fallback applies to the test path) prodFile stays empty
		// and the productionFunctionRef emits the "?" form, preserving
		// the existing high-confidence-uses-global-byName behaviour.
		out = append(out, testedCall{
			qname:      qname,
			confidence: conf,
			prodFile:   prodFile,
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
