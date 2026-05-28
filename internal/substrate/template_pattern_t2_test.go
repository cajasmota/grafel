// Phase 3D T2 template-pattern registration + coverage tests (#2775).
package substrate

import "testing"

// TestTemplatePatternRegistry_T2Languages asserts every T2 slug ships a
// template-pattern sniffer. New languages added to the T2 set must
// extend this list.
func TestTemplatePatternRegistry_T2Languages(t *testing.T) {
	for _, lang := range []string{"ruby", "php", "rust", "csharp", "kotlin", "elixir", "scala", "c-cpp"} {
		if TemplatePatternSnifferFor(lang) == nil {
			t.Errorf("expected template-pattern sniffer registered for %q", lang)
		}
	}
}

// templateFixture is one per-language canonical example with three
// expected matches: an i18n key, a log-format literal, and an SQL
// literal. The sniffer must recognise at least one of each kind.
type templateFixture struct {
	lang   string
	source string
}

func TestTemplatePatternT2_PrimitiveCoverage(t *testing.T) {
	cases := []templateFixture{
		{
			lang: "ruby",
			source: "def show\n" +
				"  msg = I18n.t(\"welcome.greeting\")\n" +
				"  Rails.logger.info(\"showing %s\")\n" +
				"  User.connection.execute(\"SELECT * FROM users\")\n" +
				"end\n",
		},
		{
			lang: "php",
			source: "<?php\n" +
				"function show() {\n" +
				"  $m = __(\"welcome.greeting\");\n" +
				"  Log::info(\"showing %s\");\n" +
				"  $db->query(\"SELECT * FROM users\");\n" +
				"}\n",
		},
		{
			lang: "rust",
			source: "fn show() {\n" +
				"  let m = t!(\"welcome.greeting\");\n" +
				"  log::info!(\"showing {}\");\n" +
				"  sqlx::query(\"SELECT * FROM users\");\n" +
				"}\n",
		},
		{
			lang: "csharp",
			source: "public class C {\n" +
				"  public void Show() {\n" +
				"    var m = _localizer[\"welcome.greeting\"];\n" +
				"    _logger.LogInformation(\"showing {0}\");\n" +
				"    db.Execute(@\"SELECT * FROM users\");\n" +
				"  }\n" +
				"}\n",
		},
		{
			lang: "kotlin",
			source: "fun show() {\n" +
				"  val m = getString(\"welcome.greeting\")\n" +
				"  Log.i(TAG, \"showing %s\")\n" +
				"  db.execSQL(\"SELECT * FROM users\")\n" +
				"}\n",
		},
		{
			lang: "elixir",
			source: "defmodule C do\n" +
				"  def show do\n" +
				"    m = gettext(\"welcome.greeting\")\n" +
				"    Logger.info(\"showing ~p\")\n" +
				"    Ecto.Adapters.SQL.query!(repo, \"SELECT * FROM users\")\n" +
				"  end\n" +
				"end\n",
		},
		{
			lang: "scala",
			source: "object C {\n" +
				"  def show(): Unit = {\n" +
				"    val m = Messages(\"welcome.greeting\")\n" +
				"    logger.info(\"showing %s\")\n" +
				"    db.run(\"SELECT * FROM users\")\n" +
				"  }\n" +
				"}\n",
		},
		{
			lang: "c-cpp",
			source: "#include <cstdio>\n" +
				"void show() {\n" +
				"  const char *m = gettext(\"welcome.greeting\");\n" +
				"  printf(\"showing %s\");\n" +
				"  exec_sql(\"SELECT * FROM users\");\n" +
				"}\n",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.lang, func(t *testing.T) {
			sniff := TemplatePatternSnifferFor(c.lang)
			if sniff == nil {
				t.Fatalf("no sniffer registered for %q", c.lang)
			}
			matches := sniff(c.source)
			gotKinds := map[TemplateKind]bool{}
			for _, m := range matches {
				gotKinds[m.Kind] = true
			}
			if !gotKinds[TemplateKindI18n] {
				t.Errorf("%s: expected at least one i18n match in %v", c.lang, matches)
			}
			if !gotKinds[TemplateKindLog] {
				t.Errorf("%s: expected at least one log_format match in %v", c.lang, matches)
			}
			if !gotKinds[TemplateKindSQL] {
				t.Errorf("%s: expected at least one sql match in %v", c.lang, matches)
			}
		})
	}
}
