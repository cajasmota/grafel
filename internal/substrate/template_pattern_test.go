package substrate

import (
	"strings"
	"testing"
)

func TestTemplatePatternRegistry_T1(t *testing.T) {
	for _, lang := range []string{"jsts", "python", "java", "go"} {
		if TemplatePatternSnifferFor(lang) == nil {
			t.Errorf("template-pattern sniffer not registered for %q", lang)
		}
	}
}

func TestTemplatePattern_JSTS(t *testing.T) {
	src := "function f(){ t('home.title'); console.log('hello {}', name); db.query(\"SELECT * FROM users WHERE id = ?\"); }"
	out := sniffTemplatePatternsJSTS(src)
	if !hasTemplateKind(out, TemplateKindI18n) {
		t.Errorf("missing i18n match: %+v", out)
	}
	if !hasTemplateKind(out, TemplateKindLog) {
		t.Errorf("missing log match: %+v", out)
	}
	if !hasTemplateKind(out, TemplateKindSQL) {
		t.Errorf("missing sql match: %+v", out)
	}
}

func TestTemplatePattern_Python(t *testing.T) {
	src := "def f():\n    _('hello')\n    logger.info('done')\n    cursor.execute('SELECT * FROM t')\n"
	out := sniffTemplatePatternsPython(src)
	if !hasTemplateKind(out, TemplateKindI18n) {
		t.Errorf("missing i18n match")
	}
	if !hasTemplateKind(out, TemplateKindLog) {
		t.Errorf("missing log match")
	}
	if !hasTemplateKind(out, TemplateKindSQL) {
		t.Errorf("missing sql match")
	}
}

func TestTemplatePattern_Java(t *testing.T) {
	src := `class C {
  void f() {
    bundle.getString("welcome");
    logger.info("done {}");
    db.execute("SELECT * FROM t");
  }
}`
	out := sniffTemplatePatternsJava(src)
	if !hasTemplateKind(out, TemplateKindI18n) {
		t.Errorf("missing i18n match")
	}
	if !hasTemplateKind(out, TemplateKindLog) {
		t.Errorf("missing log match")
	}
	if !hasTemplateKind(out, TemplateKindSQL) {
		t.Errorf("missing sql match")
	}
}

func TestTemplatePattern_Go(t *testing.T) {
	src := "package p\nfunc f(){ p.T(\"home.title\"); fmt.Printf(\"hello %s\\n\", x); db.Query(\"SELECT * FROM t\") }\n"
	out := sniffTemplatePatternsGo(src)
	if !hasTemplateKind(out, TemplateKindI18n) {
		t.Errorf("missing i18n match: %+v", out)
	}
	if !hasTemplateKind(out, TemplateKindLog) {
		t.Errorf("missing log match: %+v", out)
	}
	if !hasTemplateKind(out, TemplateKindSQL) {
		t.Errorf("missing sql match: %+v", out)
	}
}

func TestTruncateLiteral(t *testing.T) {
	short := "hi"
	if TruncateLiteral(short) != short {
		t.Errorf("short literal mutated")
	}
	long := strings.Repeat("x", maxLiteralLength+50)
	got := TruncateLiteral(long)
	if !strings.HasSuffix(got, "...") {
		t.Errorf("long literal not truncated: %q", got)
	}
}

func hasTemplateKind(out []TemplatePattern, k TemplateKind) bool {
	for _, m := range out {
		if m.Kind == k {
			return true
		}
	}
	return false
}
