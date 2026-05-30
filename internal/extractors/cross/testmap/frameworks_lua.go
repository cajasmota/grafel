// Package testmap — Lua test-framework detection and call resolution.
//
// Deep linkage (#3485) for the two dominant Lua test runners:
//
//	busted (BDD): describe("X", function() it("does y", function() … end) end).
//	  Each it()/pending() leaf is a test case; its body (the function() … end
//	  block) is scanned for direct production calls. The enclosing describe()
//	  subject is carried as a naming-convention fallback when the leaf body has
//	  no resolvable production call.
//
//	luaunit (xUnit): TestFoo = {}; function TestFoo:testBar() … end. Each
//	  TestClass:testXxx method is a test case; the receiver class name (minus a
//	  leading "Test") is the subject-under-test fallback, mirroring the
//	  C#/Java/Kotlin describeSubject path.
//
// Lua blocks are not brace-delimited, so this file carries a Lua-specific
// keyword-balanced body extractor (extractLuaBlockBody) that pairs
// function/if/for/while/do block openers with their matching `end` (and
// repeat … until), quote- and comment-aware.
package testmap

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Lua block-body extraction — keyword balanced (function/if/for/while/do … end)
// ---------------------------------------------------------------------------

// luaBlockOpeners is the set of Lua keywords that open a block closed by `end`.
//
// Crucially, `for` and `while` are NOT counted as openers: in Lua they are
// always followed by a `do` that introduces the loop body (`for … do … end`,
// `while … do … end`), and we count that `do` instead. Counting both `for` and
// its `do` would double-increment the depth. Likewise `if … then` is balanced by
// counting `if` (and `then` is not an opener). `repeat` opens a block closed by
// `until`, handled separately in extractLuaBlockBody.
//
// Net effect — each `end` is matched by exactly one of: function | if | do.
var luaBlockOpeners = []string{"function", "if", "do"}

// luaIdentByteRE reports whether a byte can be part of a Lua identifier — used to
// avoid treating `endpoint` / `function_name` substrings as block keywords.
func luaIsIdentByte(c byte) bool {
	return c == '_' ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}

// extractLuaBlockBody returns the source slice from the first block-opening
// keyword at or after startAt up to (and including) its matching `end` /
// `until`. It is keyword-balanced and skips string literals (single, double and
// long-bracket [[ … ]]) and comments (-- line, --[[ … ]] block) so that the word
// `end` inside a string or comment never closes a block.
//
// When balancing fails (truncated source) it returns the slice from the opener
// to end-of-source, so the resolver still gets a body to scan rather than "".
func extractLuaBlockBody(source string, startAt int) string {
	n := len(source)
	// Locate the first opener keyword at/after startAt.
	openStart := -1
	for i := startAt; i < n; {
		c := source[i]
		// Skip strings/comments while searching for the opener so a keyword inside
		// them is not mistaken for the block start.
		if skipped, next := luaSkipNonCode(source, i); skipped {
			i = next
			continue
		}
		if isLuaBlockOpenerAt(source, i) {
			openStart = i
			break
		}
		_ = c
		i++
	}
	if openStart < 0 {
		return ""
	}

	depth := 0
	i := openStart
	for i < n {
		if skipped, next := luaSkipNonCode(source, i); skipped {
			i = next
			continue
		}
		// `repeat` blocks close with `until`, not `end`.
		if matchLuaWord(source, i, "repeat") {
			depth++
			i += len("repeat")
			continue
		}
		if matchLuaWord(source, i, "until") {
			depth--
			if depth == 0 {
				end := i + len("until")
				return source[openStart:end]
			}
			i += len("until")
			continue
		}
		if isLuaBlockOpenerAt(source, i) {
			// Determine which opener so we can advance past it.
			for _, kw := range luaBlockOpeners {
				if matchLuaWord(source, i, kw) {
					depth++
					i += len(kw)
					break
				}
			}
			continue
		}
		if matchLuaWord(source, i, "end") {
			depth--
			if depth == 0 {
				return source[openStart : i+len("end")]
			}
			i += len("end")
			continue
		}
		i++
	}
	// Unbalanced — return what we have.
	return source[openStart:]
}

// luaSkipNonCode reports whether position i begins a Lua string literal or
// comment and, if so, returns the index just past it. Handles: -- line comments,
// --[[ … ]] block comments, [[ … ]] long-bracket strings, and '…' / "…" short
// strings (with backslash escapes).
func luaSkipNonCode(source string, i int) (bool, int) {
	n := len(source)
	c := source[i]
	// Comments: -- … (line) or --[[ … ]] (block).
	if c == '-' && i+1 < n && source[i+1] == '-' {
		j := i + 2
		// Block comment --[[ … ]] (also --[==[ … ]==]).
		if j < n && source[j] == '[' {
			if end, ok := luaLongBracketEnd(source, j); ok {
				return true, end
			}
		}
		// Line comment: to end of line.
		for j < n && source[j] != '\n' {
			j++
		}
		return true, j
	}
	// Long-bracket string [[ … ]] / [==[ … ]==].
	if c == '[' && i+1 < n && (source[i+1] == '[' || source[i+1] == '=') {
		if end, ok := luaLongBracketEnd(source, i); ok {
			return true, end
		}
	}
	// Short strings.
	if c == '"' || c == '\'' {
		j := i + 1
		for j < n {
			if source[j] == '\\' {
				j += 2
				continue
			}
			if source[j] == c {
				j++
				break
			}
			j++
		}
		return true, j
	}
	return false, i
}

// luaLongBracketEnd parses a Lua long bracket starting at index i (which must
// point at '['). It returns the index just past the matching close bracket and
// true on success, or (i, false) when i is not a valid long-bracket opener.
func luaLongBracketEnd(source string, i int) (int, bool) {
	n := len(source)
	if i >= n || source[i] != '[' {
		return i, false
	}
	j := i + 1
	level := 0
	for j < n && source[j] == '=' {
		level++
		j++
	}
	if j >= n || source[j] != '[' {
		return i, false
	}
	j++ // past the second '['
	closeSeq := "]" + strings.Repeat("=", level) + "]"
	idx := strings.Index(source[j:], closeSeq)
	if idx < 0 {
		return n, true // unterminated — consume to EOF
	}
	return j + idx + len(closeSeq), true
}

// matchLuaWord reports whether the whole word `word` begins at index i in source
// (i.e. preceded and followed by a non-identifier byte).
func matchLuaWord(source string, i int, word string) bool {
	if i+len(word) > len(source) {
		return false
	}
	if source[i:i+len(word)] != word {
		return false
	}
	if i > 0 && luaIsIdentByte(source[i-1]) {
		return false
	}
	after := i + len(word)
	if after < len(source) && luaIsIdentByte(source[after]) {
		return false
	}
	return true
}

// isLuaBlockOpenerAt reports whether a whole-word `end`-closed block opener
// (function | if | do) starts at index i. `repeat` is intentionally excluded —
// it is closed by `until`, not `end`, and is handled directly in
// extractLuaBlockBody.
func isLuaBlockOpenerAt(source string, i int) bool {
	for _, kw := range luaBlockOpeners {
		if matchLuaWord(source, i, kw) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// busted — describe / it / pending  (BDD)
// ---------------------------------------------------------------------------

// bustedItRE matches a busted leaf case: it("desc", …) / pending("desc", …).
// Group 1 is the verb, group 2 the description. `spec` is an alias some suites
// use; `it` and `pending` are the canonical leaves.
var bustedItRE = regexp.MustCompile(
	`(?m)\b(it|pending|spec)\s*\(\s*["']([^"']{1,200})["']\s*,`,
)

// bustedDescribeRE matches a busted container: describe("Subject", …) /
// context("Subject", …). The first describe whose subject looks like a code
// identifier (CamelCase / dotted / module-ish) is used as the subject-under-test
// fallback. Group 1 = verb, group 2 = subject text.
var bustedDescribeRE = regexp.MustCompile(
	`(?m)\b(describe|context)\s*\(\s*["']([^"']{1,200})["']\s*,`,
)

// bustedSubjectIdentRE recognises a describe subject that names a code symbol
// (e.g. "UserService", "users.handler", "Account.create") so it can seed a
// naming-convention TESTS edge. Plain prose subjects ("returns 200 on GET") are
// rejected — they have spaces and are not identifier-shaped.
var bustedSubjectIdentRE = regexp.MustCompile(`^[A-Za-z_][\w]*(?:[.:][A-Za-z_][\w]*)*$`)

// bustedDescribeSubject returns the first describe/context subject that is
// identifier-shaped (no spaces), trimming a trailing "()" some authors append.
// Returns "" when no describe block names a code symbol.
func bustedDescribeSubject(source string) string {
	for _, m := range bustedDescribeRE.FindAllStringSubmatch(source, -1) {
		subj := strings.TrimSpace(m[2])
		subj = strings.TrimSuffix(subj, "()")
		if bustedSubjectIdentRE.MatchString(subj) {
			// Use the tail of a dotted/colon subject ("users.handler" → "handler").
			if idx := strings.IndexAny(subj, ".:"); idx >= 0 {
				tail := subj[strings.LastIndexAny(subj, ".:")+1:]
				if tail != "" {
					return tail
				}
			}
			return subj
		}
	}
	return ""
}

func detectBusted(source string) []testFunction {
	subject := bustedDescribeSubject(source)

	var out []testFunction
	seen := map[string]bool{}
	for _, m := range bustedItRE.FindAllStringSubmatchIndex(source, -1) {
		desc := source[m[4]:m[5]]
		name := jestCaseQName(desc) // reuse JS normaliser: spaces → underscores
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		// Body is the function() … end block that follows the description arg.
		body := extractLuaBlockBody(source, m[1])
		out = append(out, testFunction{qname: name, body: body, describeSubject: subject})
	}
	return out
}

// ---------------------------------------------------------------------------
// luaunit — TestClass:testXxx  (xUnit)
// ---------------------------------------------------------------------------

// luaunitMethodRE matches a luaunit test method definition:
//
//	function TestFoo:testBar() … end
//	function TestFoo.testBar() … end
//
// Group 1 = test class name, group 2 = test method name (test-prefixed).
var luaunitMethodRE = regexp.MustCompile(
	`(?m)\bfunction\s+(\w+)\s*[.:]\s*(test\w*)\s*\(`,
)

// luaunitSubjectFromClass derives the subject-under-test from a luaunit test
// class name by stripping a leading "Test" (the luaunit convention is
// `Test<Subject>`):
//
//	TestUserService → UserService
//	TestAccount     → Account
//	(no Test prefix)→ ""
func luaunitSubjectFromClass(className string) string {
	if strings.HasPrefix(className, "Test") && len(className) > len("Test") {
		return className[len("Test"):]
	}
	return ""
}

func detectLuaunit(source string) []testFunction {
	var out []testFunction
	seen := map[string]bool{}
	for _, m := range luaunitMethodRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		method := source[m[4]:m[5]]
		if seen[method] {
			continue
		}
		seen[method] = true
		// Scan from the `function` keyword (match start) so the method's own
		// function … end block is the body — the body does not re-open a block
		// after the parameter list.
		body := extractLuaBlockBody(source, m[0])
		out = append(out, testFunction{
			qname:           method,
			body:            body,
			describeSubject: luaunitSubjectFromClass(className),
		})
	}
	return out
}
