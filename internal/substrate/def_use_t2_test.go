// Phase 3C T2 def-use registration + primitive-coverage tests (#2775).
package substrate

import "testing"

// TestDefUseRegistry_T2Languages asserts every T2 slug ships a def-use
// sniffer. New languages added to the T2 set must extend this list.
func TestDefUseRegistry_T2Languages(t *testing.T) {
	for _, lang := range []string{"ruby", "php", "rust", "csharp", "kotlin", "elixir", "scala", "c-cpp"} {
		if DefUseSnifferFor(lang) == nil {
			t.Errorf("expected def-use sniffer registered for %q", lang)
		}
	}
}

// defUseFixture is one per-language canonical example. Each fixture is a
// minimal function that defines a local and reads it back exactly once
// — the sniffer must lift at least one def and one matching use.
type defUseFixture struct {
	lang    string
	source  string
	wantDef string // expected var name in at least one def
	wantUse string // expected var name in at least one use
}

func TestDefUseT2_PrimitiveCoverage(t *testing.T) {
	cases := []defUseFixture{
		{
			lang: "ruby",
			source: "def greet(name)\n" +
				"  message = \"hello \" + name\n" +
				"  puts message\n" +
				"end\n",
			wantDef: "message",
			wantUse: "message",
		},
		{
			lang: "php",
			source: "<?php\n" +
				"function greet($name) {\n" +
				"  $message = 'hello ' . $name;\n" +
				"  echo $message;\n" +
				"}\n",
			wantDef: "message",
			wantUse: "message",
		},
		{
			lang: "rust",
			source: "fn greet(name: &str) {\n" +
				"  let message = format!(\"hello {}\", name);\n" +
				"  println!(\"{}\", message);\n" +
				"}\n",
			wantDef: "message",
			wantUse: "message",
		},
		{
			lang: "csharp",
			source: "public class G {\n" +
				"  public string Greet(string name) {\n" +
				"    var message = \"hello \" + name;\n" +
				"    return message;\n" +
				"  }\n" +
				"}\n",
			wantDef: "message",
			wantUse: "message",
		},
		{
			lang: "kotlin",
			source: "fun greet(name: String): String {\n" +
				"  val message = \"hello \" + name\n" +
				"  return message\n" +
				"}\n",
			wantDef: "message",
			wantUse: "message",
		},
		{
			lang: "elixir",
			source: "defmodule Greeter do\n" +
				"  def greet(name) do\n" +
				"    message = \"hello \" <> name\n" +
				"    IO.puts(message)\n" +
				"  end\n" +
				"end\n",
			wantDef: "message",
			wantUse: "message",
		},
		{
			lang: "scala",
			source: "object Greeter {\n" +
				"  def greet(name: String): String = {\n" +
				"    val message = \"hello \" + name\n" +
				"    message\n" +
				"  }\n" +
				"}\n",
			wantDef: "message",
			wantUse: "message",
		},
		{
			lang: "c-cpp",
			source: "#include <string>\n" +
				"std::string greet(const std::string &name) {\n" +
				"  std::string message = \"hello \" + name;\n" +
				"  return message;\n" +
				"}\n",
			wantDef: "message",
			wantUse: "message",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.lang, func(t *testing.T) {
			sniff := DefUseSnifferFor(c.lang)
			if sniff == nil {
				t.Fatalf("no sniffer registered for %q", c.lang)
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

func containsDefVar(defs []VarDef, name string) bool {
	for _, d := range defs {
		if d.Var == name {
			return true
		}
	}
	return false
}

func containsUseVar(uses []VarUse, name string) bool {
	for _, u := range uses {
		if u.Var == name {
			return true
		}
	}
	return false
}
