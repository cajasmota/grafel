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
			// body, so it is always indented. A column-0 `foo(a) {` is not
			// valid method shorthand in any JS/TS dialect -- the declaration
			// form at column 0 is `function foo(` , which alternative 1
			// already owns and which this case leaves intact.
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
