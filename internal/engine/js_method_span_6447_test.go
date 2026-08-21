package engine

import (
	"strings"
	"testing"
)

// #6447 — jsFuncDeclRe declined class/object method shorthand, so an HTTP call
// made from a class method had no enclosing function to attribute to. The
// golden fixture (internal/quality/golden/angular-http-mini) grades the
// consequence — the FETCHES edges — but it cannot grade the hazard the old
// comment named: a bare `foo(` also matches an arbitrary call expression, and a
// span minted from one produces a WRONG caller rather than a missing one.
//
// These tests grade the shape directly, in BOTH directions:
//
//   - indexed: the method shapes a real TS service is written in.
//   - not indexed: control flow, call expressions, and declarations with no
//     body, which is where a more-permissive pattern does its damage.
//
// The permissive direction is the one that needs the tests. Loosening the
// pattern (dropping the `{` requirement, widening `[^()]*` to `.*`, deleting
// the jsMethodShorthandReserved reject) leaves every FETCHES row in the fixture
// green, because the fixture's TS happens to contain no `if (...) {` above a
// call site. Only these cases fail.

// spanNames returns the indexed span names in file order. Duplicates are kept:
// two spans with the same name at different offsets is a real, distinguishable
// state (see the `if`-vs-method case below).
func spanNames(t *testing.T, src string) []string {
	t.Helper()
	var out []string
	for _, s := range indexJSEnclosingFunctions(src) {
		out = append(out, s.name)
	}
	return out
}

func containsSpan(t *testing.T, src, name string) bool {
	t.Helper()
	for _, n := range spanNames(t, src) {
		if n == name {
			return true
		}
	}
	return false
}

func TestJSMethodShorthandSpansAreIndexed_6447(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "plain class method with return annotation",
			src: "class ThingService {\n" +
				"  list(): Observable<Thing[]> {\n" +
				"    return this.http.get('/api/things');\n" +
				"  }\n}\n",
			want: "list",
		},
		{
			name: "nested generic in the return annotation",
			src: "class I18nService {\n" +
				"  strings(): Observable<Record<string, string>> {\n" +
				"    return this.http.get('assets/i18n/en.json');\n" +
				"  }\n}\n",
			want: "strings",
		},
		{
			name: "TS access modifiers stack",
			src: "class A {\n" +
				"  public static async save(x: number): Promise<void> {\n" +
				"    await fetch('/api/save');\n" +
				"  }\n}\n",
			want: "save",
		},
		{
			name: "generic type parameter on the method itself",
			src: "class A {\n" +
				"  fetchOne<T>(id: string): Promise<T> {\n" +
				"    return get('/api/one');\n" +
				"  }\n}\n",
			want: "fetchOne",
		},
		{
			name: "no annotation, multiple params",
			src: "class A {\n" +
				"  rename(id, body) {\n" +
				"    return this.http.patch('/api/x', body);\n" +
				"  }\n}\n",
			want: "rename",
		},
		{
			name: "object literal method shorthand",
			src: "const api = {\n" +
				"  load() {\n" +
				"    return fetch('/api/load');\n" +
				"  },\n};\n",
			want: "load",
		},
		{
			name: "getter",
			src: "class A {\n" +
				"  get current(): Thing {\n" +
				"    return this.value;\n" +
				"  }\n}\n",
			want: "current",
		},
		{
			// The return annotation is `[^;{}]*`, not `[^;{}()]*`: a function
			// type in return position carries its own parens, and excluding
			// them made an ordinary TS shape fall through to the preceding
			// declaration's name. The `()`-free rule that closes the
			// `describe('x', () => {` collision lives in the PARAMETER group,
			// which is still `\([^()]*\)`; see the collision cases below.
			name: "function-type return annotation",
			src: "class A {\n" +
				"  makeLoader(): (id: string) => Promise<void> {\n" +
				"    return () => fetch('/api/x').then(() => undefined);\n" +
				"  }\n}\n",
			want: "makeLoader",
		},
		{
			name: "parenthesised union return annotation",
			src: "class A {\n" +
				"  pick(): (A | B) {\n" +
				"    return this.http.get('/api/pick');\n" +
				"  }\n}\n",
			want: "pick",
		},
		{
			name: "generator method",
			src: "class A {\n" +
				"  *stream() {\n" +
				"    yield fetch('/api/stream');\n" +
				"  }\n}\n",
			want: "stream",
		},
		{
			name: "async generator method",
			src: "class A {\n" +
				"  async *stream() {\n" +
				"    yield await fetch('/api/stream');\n" +
				"  }\n}\n",
			want: "stream",
		},
		{
			// The `#` is part of the identifier in the source, so it is part
			// of the span name: a class may declare BOTH `#load` and `load`,
			// and collapsing them would make the two indistinguishable to
			// enclosingJSFuncAt's consumers (sse_edges.go names a Stream
			// entity `"/" + caller`).
			name: "private method keeps its hash",
			src: "class A {\n" +
				"  #load(id) {\n" +
				"    return fetch('/api/x');\n" +
				"  }\n}\n",
			want: "#load",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !containsSpan(t, tc.src, tc.want) {
				t.Fatalf("span %q not indexed; got %v", tc.want, spanNames(t, tc.src))
			}
		})
	}
}

func TestJSMethodShorthandDoesNotSwallowNonDeclarations_6447(t *testing.T) {
	// Each source contains exactly ONE real declaration. Anything else the
	// pattern picks up is a false span, and a false span is worse than none:
	// enclosingJSFuncAt walks to the nearest PRECEDING span, so a bogus `if`
	// between a method header and its call site steals the attribution.
	cases := []struct {
		name    string
		src     string
		wantOne string
		absent  []string
	}{
		{
			name: "control flow keywords are not callers",
			src: "class A {\n" +
				"  probe(id) {\n" +
				"    if (id) {\n" +
				"      for (let i = 0; i < 3; i++) {\n" +
				"        while (i) {\n" +
				"          this.http.get('/api/x');\n" +
				"        }\n" +
				"      }\n" +
				"    }\n" +
				"  }\n}\n",
			wantOne: "probe",
			absent:  []string{"if", "for", "while"},
		},
		{
			name: "switch, catch and do are not callers",
			src: "class A {\n" +
				"  probe(id) {\n" +
				"    try {\n" +
				"      switch (id) {\n" +
				"        default:\n" +
				"          break;\n" +
				"      }\n" +
				"    } catch (e) {\n" +
				"      do {\n" +
				"        log(e);\n" +
				"      } while (false);\n" +
				"    }\n" +
				"  }\n}\n",
			wantOne: "probe",
			absent:  []string{"switch", "catch", "do", "try"},
		},
		{
			name: "call expression with a callback argument is not a declaration",
			src: "class A {\n" +
				"  probe() {\n" +
				"    describe('suite', () => {\n" +
				"      run(this.x);\n" +
				"    });\n" +
				"  }\n}\n",
			wantOne: "probe",
			absent:  []string{"describe"},
		},
		{
			name: "chained call followed by a block is not a declaration",
			src: "class A {\n" +
				"  probe() {\n" +
				"    load(this.id).then(() => {\n" +
				"      done();\n" +
				"    });\n" +
				"  }\n}\n",
			wantOne: "probe",
			absent:  []string{"load", "then"},
		},
		{
			// The parameter group is `[^()]*`, not `.*`. Widening it to `.*`
			// leaves every other case here green (a greedy `.*` still has to
			// land a `{` right after the closing paren, which `() => {` does
			// not), but it opens exactly this shape: the LAST `)` on the line
			// belongs to `function ()`, and a `{` follows it. Test files are
			// full of this, and `it` as a caller name would be attached to
			// every call site below the block.
			name: "call with a function-expression argument is not a declaration",
			src: "describe('ThingService', function () {\n" +
				"  it('loads', function () {\n" +
				"    svc.load();\n" +
				"  });\n" +
				"  probe() {\n" +
				"    this.http.get('/api/x');\n" +
				"  }\n" +
				"});\n",
			wantOne: "probe",
			absent:  []string{"it", "describe"},
		},
		{
			// Alternative 4 requires `^[ \t]+`, not `^[ \t]*`: method
			// shorthand is by construction nested inside a class or object
			// body, so it is always indented, and at column 0 the pattern
			// cannot tell `foo(a) {` from the many non-declaration shapes
			// that reach column 0 in minified or generated code.
			//
			// Be precise about what the rejection BUYS, which is less than it
			// looks: because spans are unbounded (#6500), declining to mint a
			// span does not leave the following call site with an empty
			// caller — it leaves it with the PRECEDING span's name. So this
			// case buys "no NEW wrong name", not "no wrong name"; the wrong
			// name it inherits is exactly the pre-#6447 defect this PR fixes
			// elsewhere. TestJSKnownMisattributionShapes_6447 pins that
			// consequence directly rather than leaving it to a comment.
			name: "column-0 method shorthand is not a declaration",
			src: "foo(a) {\n" +
				"  bar();\n" +
				"}\n" +
				"function probe() {\n" +
				"  this.http.get('/api/x');\n" +
				"}\n",
			wantOne: "probe",
			absent:  []string{"foo"},
		},
		{
			name: "abstract signature with no body is not a caller",
			src: "abstract class A {\n" +
				"  abstract missing(id: string): void;\n" +
				"  probe() {\n" +
				"    this.http.get('/api/x');\n" +
				"  }\n}\n",
			wantOne: "probe",
			absent:  []string{"missing"},
		},
		{
			// `with` is in jsMethodShorthandReserved and nothing else rejects
			// it: sloppy-mode `with (o) {` is indented, has a paren group with
			// no nested parens and a following `{`, so it satisfies
			// alternative 4 structurally. Legacy JS still ships it.
			name: "with statement is not a declaration",
			src: "const api = {\n" +
				"  probe(o) {\n" +
				"    with (o) {\n" +
				"      fetch('/api/x');\n" +
				"    }\n" +
				"  },\n};\n",
			wantOne: "probe",
			absent:  []string{"with"},
		},
		{
			// An ANONYMOUS function expression at line start: alternative 1
			// does not cover it (it requires a name after `function`), so
			// `function` reaches alternative 4 as the captured identifier and
			// only the reject list stops it. Without the reject, every call
			// site below would be attributed to a caller literally named
			// "function".
			name: "anonymous function expression at line start is not a declaration",
			src: "class A {\n" +
				"  probe() {\n" +
				"    this.http.get('/api/x');\n" +
				"  }\n}\n" +
				"const cb =\n" +
				"  function (x) {\n" +
				"    return x;\n" +
				"  };\n",
			wantOne: "probe",
			absent:  []string{"function"},
		},
		{
			// The return annotation is `[^;{}\n]*`, not `[^;{}]*`: in Go's
			// regexp `[^;{}]` MATCHES a newline, so without the `\n` brake the
			// annotation runs across lines until the next `{`. Combined with
			// the generator prefix `(?:\*[ \t]*)?`, a JSDoc continuation line
			// of the form ` * Word (prose):` satisfies alternative 4 in full —
			// `*` is the prefix, `Word` is the name, the parenthesised prose
			// is the parameter group, and the annotation swallows `*/`, the
			// `export function` header and its parameter list, down to the
			// body `{`. FindAll then resumes PAST that match, so alternative 1
			// never sees the real function: the whole exported function's
			// calls are attributed to a caller named after a JSDoc word, and
			// sse_edges.go names a Stream entity `"/" + caller` from it.
			//
			// This is not hypothetical — it destroyed ten real spans in this
			// repo's own webui-v2 frontend before the brake was added.
			name: "JSDoc prose above an exported function is not a declaration",
			src: "/**\n" +
				" * Resolve provenance.\n" +
				" *\n" +
				" * Precedence (best available wins, never a misleading %):\n" +
				" *   1. report\n" +
				" */\n" +
				"export function resolveCoverageProvenance(\n" +
				"  report: Report,\n" +
				"): Provenance {\n" +
				"  return fetch('/api/coverage');\n" +
				"}\n",
			wantOne: "resolveCoverageProvenance",
			absent:  []string{"Precedence"},
		},
		{
			// The same newline crossing with no comment involved: in
			// `semi: false` TypeScript an abstract member (or an overload
			// signature) ends its line with no `;` and no `{`, so an
			// unbraked annotation runs from its `:` into the NEXT method's
			// body brace. The signature then mints a span it must not have
			// — it has no body — and the real method below it mints none,
			// because FindAll has already consumed past its header.
			name: "semicolon-free abstract signature does not swallow the next method",
			src: "abstract class A {\n" +
				"  abstract load(id: string): Promise<void>\n" +
				"  save(x) {\n" +
				"    fetch('/api/save')\n" +
				"  }\n}\n",
			wantOne: "save",
			absent:  []string{"load"},
		},
		{
			name: "receiver call is not a declaration",
			src: "class A {\n" +
				"  probe() {\n" +
				"    this.http.get('/api/x');\n" +
				"  }\n}\n",
			wantOne: "probe",
			absent:  []string{"get", "http", "this"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := spanNames(t, tc.src)
			n := 0
			for _, g := range got {
				if g == tc.wantOne {
					n++
				}
			}
			if n != 1 {
				t.Fatalf("want exactly 1 %q span, got %d; all spans %v", tc.wantOne, n, got)
			}
			for _, bad := range tc.absent {
				for _, g := range got {
					if g == bad {
						t.Fatalf("false span %q indexed (spans %v) — enclosingJSFuncAt "+
							"would attribute a following call site to it", bad, got)
					}
				}
			}
		})
	}
}

// TestJSMethodSpanWinsOverPrecedingHelper_6447 is the mis-attribution half of
// the issue, at the unit level. dynamic.service.ts declares two module-level
// arrows and then a class whose methods call them; with no method spans the
// nearest-preceding walk never leaves the LAST arrow, so every call site in the
// class body below was stamped with it.
//
// Note what this does and does not prove: it holds because the method header is
// nearer to the call than the helper is, not because spans are bounded. They
// are not — see the note on jsFuncDeclRe.
func TestJSMethodSpanWinsOverPrecedingHelper_6447(t *testing.T) {
	src := "const base = (code) => '/things/' + code;\n" +
		"const legacyBase = (code) => '/legacy/' + code;\n" +
		"\n" +
		"class DynamicThingService {\n" +
		"  byCode(code: string): Observable<Thing> {\n" +
		"    return this.http.get(base(code));\n" +
		"  }\n" +
		"\n" +
		"  legacyByCode(code: string): Observable<Thing> {\n" +
		"    return this.http.get(legacyBase(code));\n" +
		"  }\n" +
		"}\n"

	funcs := indexJSEnclosingFunctions(src)

	for _, want := range []struct{ marker, caller string }{
		{"this.http.get(base(code))", "byCode"},
		{"this.http.get(legacyBase(code))", "legacyByCode"},
	} {
		pos := strings.Index(src, want.marker)
		if pos < 0 {
			t.Fatalf("test source no longer contains %q", want.marker)
		}
		if got := enclosingJSFuncAt(funcs, pos); got != want.caller {
			t.Errorf("call site %q: caller = %q, want %q (spans %v)",
				want.marker, got, want.caller, spanNames(t, src))
		}
	}
}

// TestJSKnownMisattributionShapes_6447 states, as executable fact, what
// happens to the shapes alternative 4 declines. It is the correction to a
// rationale this change originally got wrong.
//
// Spans carry (offset, name) and no end (#6500), so enclosingJSFuncAt walks to
// the nearest PRECEDING span and never stops. Declining to mint a span for a
// method therefore does NOT produce an empty caller for the calls inside it —
// it produces the previous declaration's name. Every row below is a WRONG
// caller, of exactly the kind #6447 set out to fix; they are accepted here
// because RE2 cannot separate them from non-declarations, not because they are
// harmless. This matters past attribution: sse_edges.go builds a Stream
// entity's ID as `"/" + caller`, so a wrong caller is a wrongly NAMED entity.
//
// If a later change starts matching one of these shapes, the row flips and the
// test fails — which is the point. Update it deliberately.
func TestJSKnownMisattributionShapes_6447(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		marker string
		// caller is the WRONG name the call site inherits today.
		caller string
	}{
		{
			// Column-0 shorthand: see the rejection case above. The call
			// inside `load` is stamped with `build`, the method before it.
			name: "column-0 method shorthand inherits the preceding method",
			src: "class A {\n" +
				"  build() {\n" +
				"    return 1;\n" +
				"  }\n" +
				"}\n" +
				"load(id) {\n" +
				"  fetch('/api/load');\n" +
				"}\n",
			marker: "fetch('/api/load')",
			caller: "build",
		},
		{
			// A computed method key is not an identifier, so the name capture
			// cannot start there. Rare outside generated code.
			name: "computed method key inherits the preceding method",
			src: "class A {\n" +
				"  build() {\n" +
				"    return 1;\n" +
				"  }\n" +
				"  ['load']() {\n" +
				"    fetch('/api/load');\n" +
				"  }\n}\n",
			marker: "fetch('/api/load')",
			caller: "build",
		},
		{
			// Allman brace style: alternative 4 requires the `{` on the same
			// line as the header, because a header with no brace is also an
			// abstract signature / an overload declaration / a TS interface
			// member, none of which is a caller.
			name: "brace on the next line inherits the preceding method",
			src: "class A {\n" +
				"  build() {\n" +
				"    return 1;\n" +
				"  }\n" +
				"  load(id)\n" +
				"  {\n" +
				"    fetch('/api/load');\n" +
				"  }\n}\n",
			marker: "fetch('/api/load')",
			caller: "build",
		},
		{
			// The reject list is name-based, so a method legitimately NAMED
			// after a reserved word is rejected with it. `catch` (thenable)
			// and `return` (iterator protocol) are the two that actually
			// occur. The cost is mis-attribution, not an empty caller.
			name: "method named catch inherits the preceding method",
			src: "const thenable = {\n" +
				"  then(res) {\n" +
				"    return res;\n" +
				"  },\n" +
				"  catch(err) {\n" +
				"    fetch('/api/err');\n" +
				"  },\n};\n",
			marker: "fetch('/api/err')",
			caller: "then",
		},
		{
			name: "iterator return method inherits the preceding method",
			src: "const it = {\n" +
				"  next() {\n" +
				"    return { done: false };\n" +
				"  },\n" +
				"  return(v) {\n" +
				"    fetch('/api/close');\n" +
				"  },\n};\n",
			marker: "fetch('/api/close')",
			caller: "next",
		},
		{
			// The type-parameter group is `<[^<>()]*>`: no parens (so a
			// generic cannot smuggle in the `describe('x', () => {` shape the
			// parameter group excludes) and no nested angle brackets. A
			// generic whose type parameter has a FUNCTION-TYPED default trips
			// the paren half. This is ordinary TypeScript, not exotica.
			name: "generic with a function-typed default inherits the preceding method",
			src: "class A {\n" +
				"  build() {\n" +
				"    return 1;\n" +
				"  }\n" +
				"  load<T = () => void>(x): T {\n" +
				"    fetch('/api/load');\n" +
				"  }\n}\n",
			marker: "fetch('/api/load')",
			caller: "build",
		},
		{
			// And a NESTED generic trips the angle-bracket half. Note the
			// asymmetry with the RETURN annotation, which does admit nested
			// generics (`strings(): Observable<Record<string, string>>` is an
			// indexed case above): the return annotation is a flat character
			// class, the type-PARAMETER group is bracket-delimited.
			name: "nested generic type parameter inherits the preceding method",
			src: "class A {\n" +
				"  build() {\n" +
				"    return 1;\n" +
				"  }\n" +
				"  load<Map<string, T>>(x) {\n" +
				"    fetch('/api/load');\n" +
				"  }\n}\n",
			marker: "fetch('/api/load')",
			caller: "build",
		},
		{
			// N3: alternative 4 anchors on `^[ \t]+`, so a class written on
			// ONE line mints no span at all. Intended: dropping the anchor to
			// a bare `[ \t]+` would let any mid-line `name(...) {` match, and
			// one-line class bodies are a minifier artefact, not source we
			// need callers for. Stated, not silent.
			name: "one-line class body mints no span",
			src: "class A { build() { return 1; } load() { fetch('/api/load'); } }\n" +
				"function outer() {\n" +
				"  return 2;\n" +
				"}\n",
			marker: "fetch('/api/load')",
			caller: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pos := strings.Index(tc.src, tc.marker)
			if pos < 0 {
				t.Fatalf("test source no longer contains %q", tc.marker)
			}
			got := enclosingJSFuncAt(indexJSEnclosingFunctions(tc.src), pos)
			if got != tc.caller {
				t.Fatalf("caller = %q, want %q (spans %v) — the known "+
					"mis-attribution changed; update this row deliberately",
					got, tc.caller, spanNames(t, tc.src))
			}
		})
	}
}

// TestJSMethodShorthandInDeadTextPoisons_6447 pins the hazard alternative 4
// widens rather than introduces.
//
// Alternatives 1-3 are already blind to comments and template literals — a
// commented-out `function foo(` mints a span today. What alternative 4 changes
// is the FREQUENCY: a commented-out method body inside a live class is a
// commonplace of real code, where a commented-out `function` declaration is
// not. Fixing it needs a comment/string mask over the whole pattern, which is
// a different change from this one; it is accepted here and recorded so that
// nobody rediscovers it as a surprise.
func TestJSMethodShorthandInDeadTextPoisons_6447(t *testing.T) {
	for _, tc := range []struct {
		name, src, marker, caller string
	}{
		{
			name: "block-commented method inside a live method body",
			src: "class A {\n" +
				"  load() {\n" +
				"    /*\n" +
				"    ghost(id) {\n" +
				"      return 0;\n" +
				"    }\n" +
				"    */\n" +
				"    fetch('/api/x');\n" +
				"  }\n}\n",
			marker: "fetch('/api/x')",
			caller: "ghost",
		},
		{
			name: "method shorthand inside a template literal",
			src: "class A {\n" +
				"  load() {\n" +
				"    const tpl = `\n" +
				"    ghost(id) {\n" +
				"      return 0;\n" +
				"    }\n" +
				"    `;\n" +
				"    fetch('/api/x');\n" +
				"  }\n}\n",
			marker: "fetch('/api/x')",
			caller: "ghost",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pos := strings.Index(tc.src, tc.marker)
			if pos < 0 {
				t.Fatalf("test source no longer contains %q", tc.marker)
			}
			got := enclosingJSFuncAt(indexJSEnclosingFunctions(tc.src), pos)
			if got != tc.caller {
				t.Fatalf("caller = %q, want %q (spans %v) — dead-text "+
					"poisoning changed; if it was FIXED, delete this row",
					got, tc.caller, spanNames(t, tc.src))
			}
		})
	}
}
