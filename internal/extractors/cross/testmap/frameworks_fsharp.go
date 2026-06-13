// Package testmap — F# test-framework detection and call resolution.
//
// #4906 (the F# slice of the coverage-linkage tail epic #4749/#4615; mirrors the
// Nim slice #4749 in frameworks_nim.go). Deep linkage for the two dominant F#
// test runners:
//
//	Expecto (the de-facto F# test framework):
//	  testList "Subject" [
//	    testCase "does y" <| fun _ -> ...
//	    testCaseAsync "does z async" <| async { ... }
//	  ]
//	  Each `testCase`/`testCaseAsync`/`ptestCase`/`ftestCase` leaf is a test case;
//	  its body — the `fun _ -> ...` / `async { ... }` lambda that follows the `<|`
//	  pipe-left — is the off-side-rule (indentation-delimited) block scanned for
//	  direct production calls. The enclosing `testList "Subject"` is carried as a
//	  naming-convention subject-under-test fallback (the F# analog of the Nim
//	  `suite "..."` / RSpec `describe` subject path).
//
//	xUnit (and the structurally-identical NUnit `[<Test>]`) in F#:
//	  [<Fact>]
//	  let ``returns 200 for a known user`` () =
//	      let svc = UserService()
//	      ...
//	  The attribute-decorated `let`-binding is a named test case; its body is the
//	  indentation-delimited run that follows the `=`.
//
// F# blocks are OFF-SIDE-RULE (indentation) delimited — no braces, no `end`
// keyword — so this file reuses the Nim block-body extractor (extractNimBlockBody)
// and the Nim test-case-name normaliser (nimTestCaseName); both are pure
// indentation/identifier helpers that are not Nim-specific.
//
// F# uses `open` (not `import`) for module imports, which the shared
// importTokenRE does not capture, so the F# framework entries are FILENAME/PATH
// gated (the standard `*Test.fs` / `*Tests.fs` / `Test*.fs` / `/tests?/` /
// `/test/` conventions) and the detector self-confirms: a non-test `.fs` file
// yields zero test cases and is dropped by the Extract-level empty-result filter,
// exactly like the rust_test entry.
package testmap

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Expecto — testList "..." / testCase "..." <| fun _ -> …
// ---------------------------------------------------------------------------

// fsharpExpectoCaseRE matches an Expecto leaf case header:
//
//	testCase "description" <| fun _ ->
//	testCaseAsync "description" <| async {
//	ptestCase "pending" <| fun _ ->      (pending/skipped — still a case)
//	ftestCase "focused" <| fun _ ->      (focused)
//
// Group 1 is the description literal. The header may end with the lambda opener
// (`fun _ ->` / `async {`) on the same line; the body is the indented run that
// follows (extractNimBlockBody).
var fsharpExpectoCaseRE = regexp.MustCompile(
	`(?m)^([ \t]*)(?:p|f)?testCase(?:Async)?\s+"([^"\n\r]{1,200})"`,
)

// fsharpExpectoListRE matches an Expecto container: `testList "Subject" [`. The
// first list whose subject is identifier-shaped seeds the subject-under-test
// fallback (mirrors nimUnittestSuiteRE).
var fsharpExpectoListRE = regexp.MustCompile(
	`(?m)^[ \t]*(?:p|f)?testList\s+"([^"\n\r]{1,200})"`,
)

// fsharpSubjectIdentRE recognises a list/fixture subject that names a code symbol
// (CamelCase / dotted), so a prose subject ("returns 200 on GET") is rejected.
var fsharpSubjectIdentRE = regexp.MustCompile(`^[A-Za-z_][\w']*(?:[.][A-Za-z_][\w']*)*$`)

// ---------------------------------------------------------------------------
// xUnit / NUnit — [<Fact>] / [<Theory>] / [<Test>] let ``name`` () = …
// ---------------------------------------------------------------------------

// fsharpAttrTestRE matches an attribute-decorated F# test binding. F# allows the
// attribute and the `let` on separate lines, so this is anchored at the `let`
// and we confirm a preceding test attribute by a cheap look-back (see detector).
// It matches both the double-backtick name form (`` `name with spaces` ``) and a
// plain identifier name.
//
//	let ``returns 200`` () =
//	let getUserReturnsOk () =
//
// Group 1 = backtick name (may be empty); group 2 = plain identifier name.
var fsharpLetNameRE = regexp.MustCompile(
	"(?m)^[ \\t]*let\\s+(?:``([^`\\n\\r]{1,200})``|([A-Za-z_][A-Za-z0-9_']*))\\s*\\(",
)

// fsharpTestAttrRE matches an xUnit/NUnit test attribute on its own or inline.
var fsharpTestAttrRE = regexp.MustCompile(`\[<\s*(?:Fact|Theory|Test|TestCase|Property)\b`)

// fsharpModuleRE captures the F# `module Foo.BarTests` declaration so a pure
// xUnit/NUnit file (no testList container) can fall back to a module-derived
// subject: a `XxxTests` / `XxxTest` module names the `Xxx` subject under test
// (the standard F# test-module naming convention). Group 1 = module name tail.
var fsharpModuleRE = regexp.MustCompile(`(?m)^[ \t]*module(?:\s+rec)?\s+(?:[\w']+\.)*([\w']+)`)

// fsharpModuleSubject returns the subject implied by a `XxxTests` test-module
// name (strips a trailing `Tests`/`Test`), or "" when the module is not test-
// named or the strip leaves nothing.
func fsharpModuleSubject(source string) string {
	m := fsharpModuleRE.FindStringSubmatch(source)
	if len(m) < 2 {
		return ""
	}
	name := m[1]
	for _, suf := range []string{"Tests", "Test"} {
		if strings.HasSuffix(name, suf) && len(name) > len(suf) {
			return name[:len(name)-len(suf)]
		}
	}
	return ""
}

// fsharpListSubject returns the first identifier-shaped testList subject, or "".
func fsharpListSubject(source string) string {
	for _, m := range fsharpExpectoListRE.FindAllStringSubmatch(source, -1) {
		subj := strings.TrimSpace(m[1])
		subj = strings.TrimSuffix(subj, "()")
		if fsharpSubjectIdentRE.MatchString(subj) {
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

// detectFSharpExpecto detects Expecto + xUnit/NUnit test cases in an F# source.
func detectFSharpExpecto(source string) []testFunction {
	subject := fsharpListSubject(source)
	if subject == "" {
		// Pure xUnit/NUnit files (and Expecto files whose testList carries a
		// prose name) fall back to the test-module naming convention.
		subject = fsharpModuleSubject(source)
	}

	var out []testFunction
	seen := map[string]bool{}

	// Expecto testCase / testCaseAsync leaves.
	for _, m := range fsharpExpectoCaseRE.FindAllStringSubmatchIndex(source, -1) {
		desc := source[m[4]:m[5]]
		name := nimTestCaseName(desc)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		body := extractNimBlockBody(source, m[0])
		out = append(out, testFunction{qname: name, body: body, describeSubject: subject, lang: "fsharp"})
	}

	// xUnit / NUnit attribute-decorated `let` bindings. We require a test
	// attribute somewhere in the file to gate (else a plain `.fs` with named
	// lets would over-match), then attach the body of each attributed binding.
	if fsharpTestAttrRE.MatchString(source) {
		for _, m := range fsharpLetNameRE.FindAllStringSubmatchIndex(source, -1) {
			// Confirm THIS binding is attributed: scan the few lines above the
			// `let` for a test attribute (attributes may sit on their own line).
			if !precededByTestAttr(source, m[0]) {
				continue
			}
			var raw string
			if m[2] >= 0 { // backtick name
				raw = source[m[2]:m[3]]
			} else if m[4] >= 0 { // plain identifier
				raw = source[m[4]:m[5]]
			}
			name := nimTestCaseName(raw)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			body := extractNimBlockBody(source, m[0])
			out = append(out, testFunction{qname: name, body: body, describeSubject: subject, lang: "fsharp"})
		}
	}

	return out
}

// precededByTestAttr reports whether a test attribute ([<Fact>], [<Theory>],
// [<Test>], …) appears on the `let` header line itself or on the (whitespace-only
// gap of) up to two non-blank lines immediately above it. This binds the
// attribute to its `let` without a full parser.
func precededByTestAttr(source string, letStart int) bool {
	// Look back up to ~200 bytes / a handful of lines.
	from := letStart - 200
	if from < 0 {
		from = 0
	}
	window := source[from:letStart]
	// Only consider the tail: the lines directly above the `let`. We accept an
	// attribute that is the last non-blank content before the binding.
	lines := strings.Split(window, "\n")
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-3; i-- {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			continue
		}
		if fsharpTestAttrRE.MatchString(t) {
			return true
		}
		// A non-blank, non-attribute line breaks the binding chain unless it is
		// itself another attribute (e.g. [<Trait>] above [<Fact>]).
		if strings.HasPrefix(t, "[<") {
			continue
		}
		return false
	}
	return false
}
