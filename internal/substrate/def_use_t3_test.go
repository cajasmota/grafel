// Phase 3C/3D T3 def-use + template-pattern registration and coverage tests (#2779).
//
// Tests assert:
//   (a) every applicable T3 language ships a def-use sniffer (3C)
//   (b) every applicable T3 language ships a template-pattern sniffer (3D)
//   (c) each sniffer correctly lifts at least one def + one use from a minimal fixture (3C)
//   (d) each template sniffer correctly lifts at least one log_format match (3D)
//       — i18n / sql are per-lang optional based on applicability.
//
// N/A languages (no Phase 1A coverage): verilog, vhdl, idris, sml, reasonml,
// rescript, elm, lisp, clojure, erlang, fsharp, haskell, ocaml, pony, groovy,
// lua, multi. These are excluded from registration assertions.
package substrate

import "testing"

// TestDefUseRegistry_T3Languages asserts every applicable T3 slug ships a
// def-use sniffer. Sniffers for vue/svelte/astro are registered under those
// slugs (delegating to JSTS internally).
func TestDefUseRegistry_T3Languages(t *testing.T) {
	for _, lang := range []string{"dart", "swift", "nim", "crystal", "zig", "solidity", "vue", "svelte", "astro"} {
		if DefUseSnifferFor(lang) == nil {
			t.Errorf("expected def-use sniffer registered for %q", lang)
		}
	}
}

// TestTemplatePatternRegistry_T3Languages asserts every applicable T3 slug
// ships a template-pattern sniffer.
func TestTemplatePatternRegistry_T3Languages(t *testing.T) {
	for _, lang := range []string{"dart", "swift", "nim", "crystal", "zig", "solidity", "vue", "svelte", "astro"} {
		if TemplatePatternSnifferFor(lang) == nil {
			t.Errorf("expected template-pattern sniffer registered for %q", lang)
		}
	}
}

// defUseT3Fixture is one per-language canonical example. Each fixture is a
// minimal function body that defines a local and reads it back exactly once.
type defUseT3Fixture struct {
	lang    string
	source  string
	wantDef string
	wantUse string
}

func TestDefUseT3_PrimitiveCoverage(t *testing.T) {
	cases := []defUseT3Fixture{
		{
			lang: "dart",
			source: "String greet(String name) {\n" +
				"  var message = 'hello ' + name;\n" +
				"  print(message);\n" +
				"  return message;\n" +
				"}\n",
			wantDef: "message",
			wantUse: "message",
		},
		{
			lang: "swift",
			source: "func greet(name: String) -> String {\n" +
				"  let message = \"hello \" + name\n" +
				"  print(message)\n" +
				"  return message\n" +
				"}\n",
			wantDef: "message",
			wantUse: "message",
		},
		{
			lang: "nim",
			source: "proc greet(name: string): string =\n" +
				"  var message = \"hello \" & name\n" +
				"  echo message\n" +
				"  result = message\n",
			wantDef: "message",
			wantUse: "message",
		},
		{
			lang: "crystal",
			source: "def greet(name : String) : String\n" +
				"  message = \"hello \" + name\n" +
				"  puts message\n" +
				"  message\n" +
				"end\n",
			wantDef: "message",
			wantUse: "message",
		},
		{
			lang: "zig",
			source: "fn greet(name: []const u8) void {\n" +
				"  const message = name;\n" +
				"  std.debug.print(\"{s}\", .{message});\n" +
				"}\n",
			wantDef: "message",
			wantUse: "message",
		},
		{
			lang: "solidity",
			source: "contract Greeter {\n" +
				"  function greet(address user) public pure returns (uint256) {\n" +
				"    uint256 result = 42;\n" +
				"    return result;\n" +
				"  }\n" +
				"}\n",
			wantDef: "result",
			wantUse: "result",
		},
		{
			lang: "vue",
			source: "<template><div>hello</div></template>\n" +
				"<script>\n" +
				"function greet(name) {\n" +
				"  const message = 'hello ' + name;\n" +
				"  return message;\n" +
				"}\n" +
				"</script>\n",
			wantDef: "message",
			wantUse: "message",
		},
		{
			lang: "svelte",
			source: "<div>hello</div>\n" +
				"<script>\n" +
				"function greet(name) {\n" +
				"  const message = 'hello ' + name;\n" +
				"  return message;\n" +
				"}\n" +
				"</script>\n",
			wantDef: "message",
			wantUse: "message",
		},
		{
			lang: "astro",
			source: "---\n" +
				"---\n" +
				"<html><body>hello</body></html>\n" +
				"<script>\n" +
				"function greet(name) {\n" +
				"  const message = 'hello ' + name;\n" +
				"  return message;\n" +
				"}\n" +
				"</script>\n",
			wantDef: "message",
			wantUse: "message",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.lang, func(t *testing.T) {
			sniff := DefUseSnifferFor(c.lang)
			if sniff == nil {
				t.Fatalf("no def-use sniffer registered for %q", c.lang)
			}
			defs, uses := sniff(c.source)
			if !containsDefVar(defs, c.wantDef) {
				t.Errorf("%s: expected def %q in %v", c.lang, c.wantDef, defs)
			}
			if !containsUseVar(uses, c.wantUse) {
				t.Errorf("%s: expected use %q in %v", c.lang, c.wantUse, uses)
			}
		})
	}
}

// templateT3Fixture is one per-language fixture for template-pattern coverage.
type templateT3Fixture struct {
	lang      string
	source    string
	wantKinds []TemplateKind
}

func TestTemplatePatternT3_PrimitiveCoverage(t *testing.T) {
	cases := []templateT3Fixture{
		{
			lang: "dart",
			source: "String show() {\n" +
				"  String m = tr(\"welcome.greeting\");\n" +
				"  print(\"showing something\");\n" +
				"  return m;\n" +
				"}\n",
			wantKinds: []TemplateKind{TemplateKindI18n, TemplateKindLog},
		},
		{
			lang: "swift",
			source: "func show() -> String {\n" +
				"  let m = NSLocalizedString(\"welcome.greeting\", comment: \"\")\n" +
				"  print(\"showing something\")\n" +
				"  return m\n" +
				"}\n",
			wantKinds: []TemplateKind{TemplateKindI18n, TemplateKindLog},
		},
		{
			lang: "nim",
			source: "proc show(): string =\n" +
				"  let m = tr(\"welcome.greeting\")\n" +
				"  echo \"showing something\"\n" +
				"  m\n",
			wantKinds: []TemplateKind{TemplateKindI18n, TemplateKindLog},
		},
		{
			lang: "crystal",
			source: "def show\n" +
				"  m = I18n.t(\"welcome.greeting\")\n" +
				"  puts \"showing something\"\n" +
				"  m\n" +
				"end\n",
			wantKinds: []TemplateKind{TemplateKindI18n, TemplateKindLog},
		},
		{
			lang: "zig",
			source: "fn show() void {\n" +
				"  std.debug.print(\"showing something\", .{});\n" +
				"  _ = db.exec(\"SELECT id FROM users WHERE id = ?\", .{1});\n" +
				"}\n",
			wantKinds: []TemplateKind{TemplateKindLog, TemplateKindSQL},
		},
		{
			lang: "solidity",
			source: "contract C {\n" +
				"  function show(uint256 id) public {\n" +
				"    require(id > 0, \"id must be positive\");\n" +
				"    emit Transfer(\"transfer completed\");\n" +
				"  }\n" +
				"}\n",
			wantKinds: []TemplateKind{TemplateKindLog},
		},
		{
			lang: "vue",
			source: "<template><div>hello</div></template>\n" +
				"<script>\n" +
				"function show() {\n" +
				"  const m = t(\"welcome.greeting\");\n" +
				"  console.log(\"showing something\");\n" +
				"}\n" +
				"</script>\n",
			wantKinds: []TemplateKind{TemplateKindI18n, TemplateKindLog},
		},
		{
			lang: "svelte",
			source: "<div>hello</div>\n" +
				"<script>\n" +
				"function show() {\n" +
				"  const m = t(\"welcome.greeting\");\n" +
				"  console.log(\"showing something\");\n" +
				"}\n" +
				"</script>\n",
			wantKinds: []TemplateKind{TemplateKindI18n, TemplateKindLog},
		},
		{
			lang: "astro",
			source: "---\n---\n<html/>\n" +
				"<script>\n" +
				"function show() {\n" +
				"  const m = t(\"welcome.greeting\");\n" +
				"  console.log(\"showing something\");\n" +
				"}\n" +
				"</script>\n",
			wantKinds: []TemplateKind{TemplateKindI18n, TemplateKindLog},
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.lang, func(t *testing.T) {
			sniff := TemplatePatternSnifferFor(c.lang)
			if sniff == nil {
				t.Fatalf("no template-pattern sniffer registered for %q", c.lang)
			}
			matches := sniff(c.source)
			gotKinds := map[TemplateKind]bool{}
			for _, m := range matches {
				gotKinds[m.Kind] = true
			}
			for _, want := range c.wantKinds {
				if !gotKinds[want] {
					t.Errorf("%s: expected at least one %q match in %v", c.lang, want, matches)
				}
			}
		})
	}
}
