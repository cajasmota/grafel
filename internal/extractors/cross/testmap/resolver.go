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

// fsharpSpaceAppRE captures an F# SPACE-APPLIED call head (`createUser "ada"`),
// the dominant curried-application idiom that produces no paren/pipe match and
// is therefore invisible to directCallRE. This is a faithful port of the base
// F# extractor's spaceAppRE (internal/extractors/fsharp/extractor.go #4939) so
// the cross-language test→SUT resolver picks up the same call sites the
// extractor records on the production side (#5034).
//
// To stay conservative (F# is whitespace-sensitive; a bare identifier followed
// by another identifier is ambiguous with type annotations / record fields) the
// head must sit at a CLAUSE-STARTER position and be followed by at least one
// whitespace-separated ARGUMENT-STARTER. Recognised clause starters: line start
// (after indentation), `=`, `(`, `[`, `;`, `,`, `|>`, `<|`, `->`, and the block
// keywords return/yield/do/then/else. The argument starter is a string/char/
// number literal, an opening paren/bracket, or a lower-case identifier (an
// upper-case follower is more likely a type/DU-case, so it is excluded).
//
// This pass is gated to F# test functions only (tf.lang == "fsharp"); it never
// runs on other languages' bodies, so it cannot regress their false-positive
// rates. Captured heads are still filtered through the shared isStopword
// denylist (Expecto/Unquote/FsUnit/xUnit assertion combinators are stop-worded
// in #4906), so test-harness combinators never surface as the SUT.
var fsharpSpaceAppRE = regexp.MustCompile(
	`(?m)(?:^[ \t]*|[=([;,]\s*|\|>\s*|<\|\s*|->\s*|\breturn!?\s+|\byield!?\s+|\bdo!?\s+|\bthen\s+|\belse\s+)` +
		`([a-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)` +
		`[ \t]+(?:"|'|@"|\$"|[0-9]|\(|\[|[a-z_])`,
)

// fsharpKeywordHeads are F# keywords/combinators that fsharpSpaceAppRE can match
// in a head position but are never production-call targets. Mirrors the base
// extractor's fsharpKeywords gate so the resolver does not emit a TESTS edge to
// `if`, `let`, `match`, etc. when they are followed by an argument starter.
var fsharpKeywordHeads = map[string]bool{
	"if": true, "elif": true, "else": true, "then": true,
	"while": true, "for": true, "do": true, "done": true,
	"match": true, "with": true, "when": true,
	"try": true, "finally": true,
	"raise": true, "failwith": true, "failwithf": true,
	"return": true, "yield": true, "and": true, "or": true, "not": true,
	"let": true, "in": true, "fun": true, "function": true,
	"type": true, "open": true, "module": true, "namespace": true,
	"new": true, "use": true, "using": true,
	"async": true, "seq": true, "query": true,
	"upcast": true, "downcast": true, "typeof": true, "typedefof": true,
	"sizeof": true, "nameof": true, "mutable": true, "rec": true,
}

// elmSpaceAppRE captures an Elm SPACE-APPLIED call head (`add 2 2`), the
// dominant curried-application idiom that produces no paren match and is
// therefore invisible to directCallRE. Mirrors the F# space-app port (#5375):
// Elm, like F#, is whitespace-sensitive and curried, so the head must sit at a
// CLAUSE-STARTER position and be followed by at least one whitespace-separated
// ARGUMENT-STARTER. Recognised clause starters: line start (after indentation),
// `=`, `(`, `[`, `,`, the pipe operators `|>` / `<|`, the lambda/case arrow
// `->`, and the keywords `of`/`then`/`else`/`in`. The argument starter is a
// string/char/number literal, an opening paren/bracket, or a lower-case
// identifier (an upper-case follower is a type/constructor, excluded).
//
// Gated to tf.lang == "elm" so other languages' false-positive rates are
// untouched. Heads pass the Elm keyword gate AND the shared isStopword denylist
// (Expect/Fuzz/describe/test combinators are stop-worded in #5375).
var elmSpaceAppRE = regexp.MustCompile(
	`(?m)(?:^[ \t]*|[=([,]\s*|\|>\s*|<\|\s*|->\s*|\bof\s+|\bthen\s+|\belse\s+|\bin\s+)` +
		`([a-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)` +
		`[ \t]+(?:"|'|[0-9]|\(|\[|[a-z_])`,
)

// elmKeywordHeads are Elm keywords that elmSpaceAppRE can match in a head
// position but are never production-call targets.
var elmKeywordHeads = map[string]bool{
	"if": true, "then": true, "else": true,
	"case": true, "of": true, "let": true, "in": true,
	"type": true, "alias": true, "module": true, "import": true,
	"exposing": true, "as": true, "port": true, "where": true,
}

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
	// JUnit 5 Assertions / Hamcrest / AssertJ matcher entry points. These are
	// assertion-harness identifiers and must never surface as the tested
	// production subject. directCallRE captures the bare/dotted call form
	// (assertThat(…), assertTrue(…), org.junit.jupiter.api.Assertions.* tail).
	"assertthat": true, "asserttrue_junit": true, "assertfalse_junit": true,
	"assertiterableequals": true, "assertarrayequals": true,
	"assertlinesmatch": true, "asserttimeout": true, "asserttimeoutpreemptively": true,
	// (assertinstanceof / assertsame / assertnotsame already stop-worded above.)
	// Hamcrest matchers (assertThat(x, is(y)) / matchers used standalone).
	"is": true, "isin": true, "hasitem": true, "hasitems": true, "hassize": true,
	"hasentry": true, "hasvalue": true, "containsstring": true,
	"greaterthan": true, "lessthan": true, "notnullvalue": true,
	"nullvalue": true, "instanceof": true, "samepropertyvaluesas": true,
	// AssertJ fluent assertions: assertThat(x).isEqualTo(y) — the chain heads are
	// matched here; the dotted `.isEqualTo`/`.contains` tails are caught by the
	// suffix filter additions below.
	"assertthatthrownby": true, "assertthatexceptionoftype": true,
	"assertthatcode": true, "assertthatnullpointerexception": true,
	// AssertJ fluent assertion verbs captured in BARE form — `assertThat(x)
	// .isEqualTo(0)` yields a bare `isEqualTo(` token because the `)` after the
	// subject breaks the dotted chain (directCallRE then sees `isEqualTo` alone).
	// These are assertion verbs, never the production subject. (#3855)
	"isequalto": true, "isnotequalto": true,
	"isgreaterthanorequalto": true, "islessthanorequalto": true,
	"isbetween": true, "startswith": true, "endswith": true,
	"containsexactly": true, "containsonly": true, "containsexactlyinanyorder": true,
	"hasfieldorpropertywithvalue": true, "extracting": true, "usingrecursivecomparison": true,
	"hasmessage": true, "hasmessagecontaining": true, "isinstanceof": true,
	// Mockito stubbing/verification verbs captured in bare form — `when(...)`
	// is already stop-worded (kotlin/catch2 `when`); add the chained verbs that
	// surface as bare idents after the `)` chain break.
	"thenreturn": true, "thenthrow": true, "thenanswer": true, "thencallrealmethod": true,
	"doreturn": true, "dothrow": true, "donothing": true, "doanswer": true,
	"verifyno more interactions": true, "verifynomoreinteractions": true,
	"verifyzerointeractions": true, "given_mockito": true, "willreturn": true,
	"willthrow": true,
	// MockMvc / Spring test web — `mockMvc.perform(...)`, the static request
	// builders (get/post already stop-worded as HTTP verbs), and the result
	// matchers `andExpect`/`andDo`/`andReturn`/`status()`/`content()`/`jsonPath()`
	// are HTTP-test plumbing, not production calls. Without these the resolver
	// emits high-confidence TESTS edges to `andExpect`/`isOk`/`status` instead of
	// the controller handler. (#3855)
	"mockmvc.perform": true, "perform": true,
	"andexpect": true, "anddo": true, "andreturn": true,
	"status": true, "content": true, "jsonpath": true, "header": true,
	"isok": true, "iscreated": true, "isnotfound": true, "isbadrequest": true,
	"isunauthorized": true, "isforbidden": true, "isnocontent": true,
	"isconflict": true, "isinternalservererror": true, "isaccepted": true,
	"redirectedurl": true, "forwardedurl": true, "model": true, "view": true,
	"mvcresult": true, "asyncdispatch": true,
	// REST-assured fluent DSL — `given().when().get("/x").then().statusCode(200)`.
	// `when`/`then`/`given` are already stop-worded (catch2/BDD block); add the
	// REST-assured-specific verbs so the chain never resolves to a fake subject.
	"statuscode": true, "extract": true, "body_restassured": true,
	"contenttype": true, "pathparam": true, "queryparam": true, "formparam": true,
	"andstuff": true, "rootpath": true, "spec": true, "log": true,
	// WebTestClient (Spring WebFlux) — `webTestClient.get().uri(...).exchange()
	// .expectStatus().isOk()`. Test-client plumbing, not production calls.
	"webtestclient.get": true, "webtestclient.post": true, "uri": true,
	"exchange": true, "expectstatus": true, "expectbody": true, "expectheader": true,
	"returnresult": true,
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
	// JS/TS — Jest / Vitest / Jasmine matcher verbs captured in BARE form
	// (#4466). `expect(x).toBe(y)` yields a bare `toBe(` token because the
	// `)` after the subject breaks the dotted chain (directCallRE then sees
	// `toBe` alone — the `.tobe` SUFFIX filter never fires). Mirrors the
	// AssertJ/Mockito bare-form handling added in #3855. Without these the
	// resolver emits a medium-confidence TESTS edge to `toBe`/`toEqual`/…
	// for nearly every assertion, driving TESTS edges toward one-per-entity.
	"tobe": true, "tobedefined": true, "tobeundefined": true,
	"tobecloseto": true, "tobegreaterthan": true,
	"tobegreaterthanorequal": true, "tobelessthan": true,
	"tobelessthanorequal": true, "tobeinstanceof": true,
	"tomatch": true, "tomatchobject": true, "tomatchsnapshot": true,
	"tomatchinlinesnapshot": true, "tocontainequal": true,
	"tohavelength": true, "tohaveproperty": true,
	"tohavebeencalled": true, "tohavebeencalledwith": true,
	"tohavebeencalledtimes": true, "tohavebeencalledonce": true,
	"tohavebeenlastcalledwith": true, "tohavebeennthcalledwith": true,
	"tohavereturned": true, "tohavereturnedwith": true,
	"toreturn": true, "tostrictequal": true, "tothrowerror": true,
	"toresolve": true, "toreject": true, "tobetruthy": true,
	"tobefalsy": true, "tohavebeencalledbefore": true,
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
	// C/C++ — gtest / catch2 / doctest / boost.test / cppunit / cpputest
	// assertion and harness macros (#3495). These are test-harness identifiers
	// and must never surface as the tested production subject. directCallRE
	// captures the macro name before its `(`. Lower-cased (resolver lowercases
	// before lookup).
	//
	// gtest / gmock:
	"expect_eq": true, "expect_ne": true, "expect_lt": true, "expect_le": true,
	"expect_gt": true, "expect_ge": true, "expect_true": true, "expect_false": true,
	"expect_streq": true, "expect_strne": true, "expect_strcaseeq": true,
	"expect_float_eq": true, "expect_double_eq": true, "expect_near": true,
	"expect_throw": true, "expect_no_throw": true, "expect_any_throw": true,
	"expect_death": true, "expect_nonfatal_failure": true, "expect_call": true,
	"assert_lt": true, "assert_le": true,
	"assert_gt": true, "assert_ge": true, "assert_true": true, "assert_false": true,
	"assert_streq": true, "assert_strne": true, "assert_strcaseeq": true,
	"assert_float_eq": true, "assert_double_eq": true, "assert_near": true,
	"assert_throw": true, "assert_no_throw": true, "assert_any_throw": true,
	"assert_death": true, "succeed": true, "on_call": true,
	// catch2 / doctest (`require`/`check` are already stop-worded as keywords
	// above; `capture` and `then` are covered by the MockK / Lua blocks):
	"require_false": true, "require_throws": true,
	"require_throws_as": true, "require_throws_with": true, "require_nothrow": true,
	"check_false": true, "check_throws": true, "check_throws_as": true,
	"check_throws_with": true, "check_nothrow": true,
	"require_that": true, "check_that": true,
	"section": true, "scenario": true, "given": true, "when": true,
	"and_given": true, "and_when": true, "and_then": true, "info": true, "warn": true,
	"succeed_catch": true, "fail_catch": true,
	"doctest_check": true, "check_eq": true, "check_ne": true, "check_lt": true,
	"check_le": true, "check_gt": true, "check_ge": true,
	"require_eq": true, "require_ne": true, "subcase": true,
	"message": true, "warn_eq": true, "warn_ne": true,
	// boost.test:
	"boost_check": true, "boost_require": true, "boost_check_equal": true,
	"boost_require_equal": true, "boost_check_ne": true, "boost_require_ne": true,
	"boost_check_lt": true, "boost_check_le": true, "boost_check_gt": true,
	"boost_check_ge": true, "boost_check_close": true, "boost_check_close_fraction": true,
	"boost_check_small": true, "boost_check_throw": true, "boost_require_throw": true,
	"boost_check_no_throw": true, "boost_require_no_throw": true,
	"boost_check_message": true, "boost_check_equal_collections": true,
	"boost_error": true, "boost_fail": true, "boost_warn": true, "boost_test": true,
	"boost_check_predicate": true, "boost_test_message": true,
	// cppunit:
	"cppunit_assert": true, "cppunit_assert_equal": true,
	"cppunit_assert_equal_message": true, "cppunit_assert_message": true,
	"cppunit_assert_doubles_equal": true, "cppunit_assert_throw": true,
	"cppunit_assert_no_throw": true, "cppunit_fail": true, "cppunit_test": true,
	"cppunit_test_suite": true, "cppunit_test_suite_end": true,
	"cppunit_test_suite_registration": true,
	// cpputest (`check_false` covered by the catch2 block above):
	"check_equal": true, "check_true": true,
	"check_text": true, "longs_equal": true, "unsigned_longs_equal": true,
	"bytes_equal": true, "pointers_equal": true, "doubles_equal": true,
	"strcmp_equal": true, "strcmp_nocase_equal": true, "strncmp_equal": true,
	"strcmp_contains": true, "test_exit": true, "fail_test": true,
	"mock": true, "mock_c": true, "checkequal": true,
	// F# — Expecto / Unquote / FsUnit / xUnit / NUnit assertion & DSL surface
	// (#4906). These are test-harness identifiers (the case combinators, the
	// Expecto `Expect.*` assertion family, FsUnit `should`, xUnit `Assert.*`)
	// and must never surface as the F# production subject under test. The
	// directCall scanner captures both the bare combinator (`testCase`) and the
	// dotted assertion (`expect.equal`). Lower-cased (resolver lowercases first).
	"testcase": true, "testcaseasync": true, "testlist": true, "testfixture": true,
	"testtask": true, "ptestcase": true, "ftestcase": true, "testproperty": true,
	"testpropertywithconfig": true, "testtheory": true, "tests": true,
	"expect.equal": true, "expect.notequal": true, "expect.istrue": true,
	"expect.isfalse": true, "expect.issome": true, "expect.isnone": true,
	"expect.isok": true, "expect.iserror": true, "expect.isnull": true,
	"expect.isnotnull": true, "expect.isempty": true, "expect.isnonempty": true,
	"expect.throws": true, "expect.throwst": true, "expect.containsall": true,
	"expect.sequenceequal": true, "expect.isgreaterthan": true, "expect.islessthan": true,
	"expect.equals": true, "expect.all": true, "expect.exists": true,
	// xUnit / NUnit (F# usage) — FsUnit `should` combinator (the Assert.* family
	// is already covered by the C# xUnit denylist block above).
	"should": true,
	// Elm — elm-test (#5375). The describe/test/fuzz case combinators and the
	// Expect.* / Fuzz.* assertion+fuzzer families are test-harness identifiers and
	// must never surface as the Elm production subject under test. (`test` and
	// `expect.equal` are already denied above; these add the Elm-specific
	// combinators and the Expect/Fuzz surfaces not shared with the F# block.)
	"fuzz": true, "fuzz2": true, "fuzz3": true, "fuzzwith": true,
	"skip": true, "only": true, "concat": true,
	"expect.atleast": true, "expect.atmost": true, "expect.greaterthan": true,
	"expect.lessthan": true, "expect.within": true, "expect.notwithin": true,
	"expect.pass": true, "expect.fail": true, "expect.ontag": true,
	"expect.true": true, "expect.false": true, "expect.err": true,
	"expect.ok": true, "expect.equallists": true, "expect.equaldicts": true,
	"expect.equalsets": true,
	"fuzz.int": true, "fuzz.string": true, "fuzz.bool": true, "fuzz.float": true,
	"fuzz.list": true, "fuzz.constant": true, "fuzz.map": true, "fuzz.andmap": true,
	"fuzz.intrange": true, "fuzz.oneof": true, "fuzz.frequency": true,
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
		// Spring MockMvc / WebTestClient test infrastructure (#3855).
		// mockMvc.perform(...).andExpect(...), webTestClient.get()...exchange(),
		// resultActions.andExpect(...) — all HTTP-test plumbing, never a prod call.
		"mockmvc.", "webtestclient.", "resultactions.", "mvc.",
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
		// Java/Spring assertion + HTTP-test fluent chains (#3855). AssertJ
		// assertThat(x).isEqualTo(y), MockMvc .andExpect/.andDo/.andReturn, and
		// REST-assured/WebTestClient .statusCode/.expectStatus/.exchange tails are
		// test-harness chains, never the production subject.
		".isequalto", ".isnotequalto", ".isnull", ".isnotnull", ".istrue",
		".isfalse", ".isempty", ".isnotempty", ".contains", ".containsexactly",
		".hassize", ".isinstanceof", ".isgreaterthan", ".islessthan",
		".andexpect", ".anddo", ".andreturn", ".statuscode", ".expectstatus",
		".expectbody", ".exchange", ".isok", ".iscreated",
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

// headIdent returns the first `.` segment of a dotted name — the receiver of a
// method call (`UserService` in `UserService.create`). For a bare identifier it
// returns the identifier unchanged. Used by the import-aware gate to test
// whether the call's receiver symbol was imported into the test file.
func headIdent(qname string) string {
	idx := strings.IndexByte(qname, '.')
	if idx < 0 {
		return qname
	}
	return qname[:idx]
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
//
// importedSyms is the set of named symbols the test file imports (JS/TS
// `import { X }`, Python `from m import X`). When this set is NON-EMPTY it gates
// the direct-call signal for QUALITY (#3628): a call to an *imported* symbol is
// the strongest test→SUT signal and stays high; a direct call to a head symbol
// that was never imported (a probable local/builtin name collision) is held at
// medium so it is never a high-confidence false link. An empty set disables the
// gate entirely (Go same-package, wildcard imports, conventions with no named
// imports) — behaviour is then identical to the pre-#3628 resolver.
func resolveCalls(tf testFunction, prodFile, convSymbol string, importedSyms map[string]bool) []testedCall {
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

	gateImports := len(importedSyms) > 0

	// Pass 1: direct calls → high (held at medium when import-aware and the
	// receiver symbol was not imported).
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
		conf := "high"
		if gateImports {
			// The receiver of the call expression is the symbol that must have
			// been imported: for `UserService.create()` it is `UserService`;
			// for a bare `createOrder()` it is `createOrder`. If neither the
			// receiver nor the whole dotted name was imported, hold the edge at
			// medium rather than emitting a high-confidence link to a
			// possibly-unrelated local. (#3628)
			head := headIdent(qname)
			if !importedSyms[head] && !importedSyms[qname] {
				conf = "medium"
			}
		}
		upgrade(qname, conf)
	}

	// Pass 1b (F# only): space-applied calls (`createUser "ada"`). F#'s dominant
	// curried-application idiom is not paren-captured by directCallRE, so an F#
	// test that exercises the SUT via space application yields no direct-call
	// signal without this port of the extractor's gated head-symbol scan (#5034).
	// Gated to tf.lang == "fsharp" so other languages' false-positive rates are
	// untouched. Heads are filtered through the F# keyword gate AND the shared
	// isStopword denylist (Expecto/FsUnit/xUnit combinators), then subjected to
	// the same import-aware high/medium gate as Pass 1.
	if tf.lang == "fsharp" {
		for _, m := range fsharpSpaceAppRE.FindAllStringSubmatch(tf.body, -1) {
			if len(m) < 2 {
				continue
			}
			qname := m[1]
			if fsharpKeywordHeads[headIdent(qname)] {
				continue
			}
			if isStopword(qname) || isStopword(tailIdent(qname)) {
				continue
			}
			tail := tailIdent(qname)
			if len(tail) < 3 {
				continue
			}
			conf := "high"
			if gateImports {
				head := headIdent(qname)
				if !importedSyms[head] && !importedSyms[qname] {
					conf = "medium"
				}
			}
			upgrade(qname, conf)
		}
	}

	// Pass 1c (Elm only): space-applied calls (`add 2 2`). Elm's curried-
	// application idiom is not paren-captured by directCallRE, so an elm-test case
	// that exercises the SUT via space application yields no direct-call signal
	// without this gated head-symbol scan (#5375). Gated to tf.lang == "elm" so
	// other languages are untouched; heads pass the Elm keyword gate AND the
	// shared isStopword denylist (Expect/Fuzz/describe/test combinators).
	if tf.lang == "elm" {
		for _, m := range elmSpaceAppRE.FindAllStringSubmatch(tf.body, -1) {
			if len(m) < 2 {
				continue
			}
			qname := m[1]
			if elmKeywordHeads[headIdent(qname)] {
				continue
			}
			if isStopword(qname) || isStopword(tailIdent(qname)) {
				continue
			}
			tail := tailIdent(qname)
			if len(tail) < 3 {
				continue
			}
			conf := "high"
			if gateImports {
				head := headIdent(qname)
				if !importedSyms[head] && !importedSyms[qname] {
					conf = "medium"
				}
			}
			upgrade(qname, conf)
		}
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
	//
	// #4466: this fallback is a last resort with NO per-function signal —
	// the file-anchored convention symbol (convSymbol, from the test FILE
	// name) or, when that is empty, the stripped test-function name. The
	// previous behaviour emitted one such edge for EVERY test function with
	// no resolvable call, so a spec with 30 it() blocks produced 30
	// identical low-confidence edges, driving TESTS edges toward one per
	// entity. The edge is still emitted (it preserves directory-convention
	// coverage, e.g. e2e/ and tests/ files with no stem convention), but
	// the Extract caller now caps the pure-fallback edge to ONCE per file
	// via isPureLowConventionFallback.
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
		// byName lookup, especially for grafel + upvate where common
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
