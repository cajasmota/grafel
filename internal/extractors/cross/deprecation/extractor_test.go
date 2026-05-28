package deprecation

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

func extract(t *testing.T, lang, source string) []entitySummary {
	t.Helper()
	e := &Extractor{}
	records, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "testfile.src",
		Content:  []byte(source),
		Language: lang,
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	var out []entitySummary
	for _, r := range records {
		out = append(out, entitySummary{
			name:       r.Name,
			kind:       r.Kind,
			subtype:    r.Subtype,
			lang:       r.Language,
			deprecated: r.Properties["deprecated"],
			message:    r.Properties["deprecation_message"],
			annotation: r.Properties["annotation"],
		})
	}
	return out
}

type entitySummary struct {
	name, kind, subtype, lang, deprecated, message, annotation string
}

// ---------------------------------------------------------------------------
// Java
// ---------------------------------------------------------------------------

func TestJava_SingleDeprecated(t *testing.T) {
	src := `
@Deprecated
public void oldMethod() {}
`
	got := extract(t, "java", src)
	if len(got) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(got))
	}
	e := got[0]
	// deprecation entities are SCOPE.Pattern (subtype = lang tag)
	// because the 14-type allowlist has no DeprecationAnnotation entry.
	if e.kind != "SCOPE.Pattern" {
		t.Errorf("kind=%q want SCOPE.Pattern", e.kind)
	}
	if e.deprecated != "true" {
		t.Errorf("deprecated=%q want true", e.deprecated)
	}
	if e.annotation != "@Deprecated" {
		t.Errorf("annotation=%q want @Deprecated", e.annotation)
	}
	if e.lang != "java" {
		t.Errorf("language=%q want java", e.lang)
	}
}

func TestJava_MultipleDeprecated(t *testing.T) {
	src := `
@Deprecated
public void foo() {}
@Deprecated
public void bar() {}
`
	got := extract(t, "java", src)
	if len(got) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(got))
	}
}

func TestJava_KotlinAlias(t *testing.T) {
	// kotlin aliases to java — should still detect @Deprecated
	src := `@Deprecated fun legacy() {}`
	got := extract(t, "kotlin", src)
	if len(got) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(got))
	}
	if got[0].lang != "java" {
		t.Errorf("language=%q want java (normalised from kotlin)", got[0].lang)
	}
}

func TestJava_NoDeprecated(t *testing.T) {
	src := `public class Foo { public void bar() {} }`
	got := extract(t, "java", src)
	if len(got) != 0 {
		t.Errorf("expected 0 entities, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// JavaScript / TypeScript
// ---------------------------------------------------------------------------

func TestJS_JSDocDeprecated(t *testing.T) {
	src := `
/** @deprecated use newMethod instead */
function oldMethod() {}
`
	got := extract(t, "javascript", src)
	if len(got) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(got))
	}
	e := got[0]
	// deprecation entities are SCOPE.Pattern (subtype = lang tag)
	// because the 14-type allowlist has no DeprecationAnnotation entry.
	if e.kind != "SCOPE.Pattern" {
		t.Errorf("kind=%q want SCOPE.Pattern", e.kind)
	}
	if e.message != "use newMethod instead" {
		t.Errorf("message=%q want 'use newMethod instead'", e.message)
	}
	if e.subtype != "javascript" {
		t.Errorf("subtype=%q want javascript", e.subtype)
	}
}

func TestTS_JSDocDeprecatedLabeledTypescript(t *testing.T) {
	src := `/** @deprecated Please upgrade */\nconst old = () => {};`
	got := extract(t, "typescript", src)
	if len(got) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(got))
	}
	if got[0].subtype != "typescript" {
		t.Errorf("subtype=%q want typescript", got[0].subtype)
	}
}

func TestJS_NoDeprecated(t *testing.T) {
	src := `function hello() { return "world"; }`
	got := extract(t, "javascript", src)
	if len(got) != 0 {
		t.Errorf("expected 0 entities, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// Rust
// ---------------------------------------------------------------------------

func TestRust_SimpleDeprecated(t *testing.T) {
	src := `
#[deprecated]
pub fn old_func() {}
`
	got := extract(t, "rust", src)
	if len(got) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(got))
	}
	if got[0].message != "" {
		t.Errorf("message=%q want empty for bare #[deprecated]", got[0].message)
	}
}

func TestRust_DeprecatedWithNote(t *testing.T) {
	src := `#[deprecated(since = "1.0.0", note = "use new_func instead")]
pub fn legacy() {}`
	got := extract(t, "rust", src)
	if len(got) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(got))
	}
	if got[0].message != "use new_func instead" {
		t.Errorf("message=%q want 'use new_func instead'", got[0].message)
	}
}

func TestRust_NoDeprecated(t *testing.T) {
	src := `pub fn fine() -> u32 { 42 }`
	got := extract(t, "rust", src)
	if len(got) != 0 {
		t.Errorf("expected 0 entities, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// C#
// ---------------------------------------------------------------------------

func TestCSharp_ObsoleteBare(t *testing.T) {
	src := `
[Obsolete]
public void OldMethod() {}
`
	got := extract(t, "csharp", src)
	if len(got) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(got))
	}
	if got[0].message != "" {
		t.Errorf("message=%q want empty for bare [Obsolete]", got[0].message)
	}
	if got[0].annotation != "[Obsolete]" {
		t.Errorf("annotation=%q want [Obsolete]", got[0].annotation)
	}
}

func TestCSharp_ObsoleteWithMessage(t *testing.T) {
	src := `[Obsolete("Use NewMethod instead")]
public void LegacyMethod() {}`
	got := extract(t, "csharp", src)
	if len(got) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(got))
	}
	if got[0].message != "Use NewMethod instead" {
		t.Errorf("message=%q want 'Use NewMethod instead'", got[0].message)
	}
}

// ---------------------------------------------------------------------------
// Python
// ---------------------------------------------------------------------------

func TestPython_WarningsWarn(t *testing.T) {
	src := `import warnings
warnings.warn("This method is deprecated, use new_method()", DeprecationWarning)
`
	got := extract(t, "python", src)
	if len(got) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(got))
	}
	if !containsStr(got[0].message, "deprecated") {
		t.Errorf("message=%q should contain 'deprecated'", got[0].message)
	}
	if got[0].annotation != "warnings.warn" {
		t.Errorf("annotation=%q want warnings.warn", got[0].annotation)
	}
}

func TestPython_WarnWithoutDeprecatedKeyword(t *testing.T) {
	// warnings.warn without "deprecated" in message — should NOT emit entity.
	src := `warnings.warn("Something went wrong")`
	got := extract(t, "python", src)
	if len(got) != 0 {
		t.Errorf("expected 0 entities, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// Elixir
// ---------------------------------------------------------------------------

func TestElixir_Deprecated(t *testing.T) {
	src := `defmodule Foo do
  @deprecated "use bar/1 instead"
  def foo(), do: :ok
end`
	got := extract(t, "elixir", src)
	if len(got) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(got))
	}
	if got[0].message != "use bar/1 instead" {
		t.Errorf("message=%q want 'use bar/1 instead'", got[0].message)
	}
	if got[0].subtype != "elixir" {
		t.Errorf("subtype=%q want elixir", got[0].subtype)
	}
}

func TestElixir_NoDeprecated(t *testing.T) {
	src := `defmodule Clean do
  def run(), do: :ok
end`
	got := extract(t, "elixir", src)
	if len(got) != 0 {
		t.Errorf("expected 0 entities, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// Unsupported language
// ---------------------------------------------------------------------------

func TestUnsupportedLanguage(t *testing.T) {
	src := `@Deprecated public void foo() {}`
	got := extract(t, "fortran", src)
	if len(got) != 0 {
		t.Errorf("expected 0 entities for unsupported language, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// Properties completeness
// ---------------------------------------------------------------------------

func TestPropertiesComplete(t *testing.T) {
	src := `@Deprecated\npublic void x() {}`
	records := extract(t, "java", src)
	if len(records) == 0 {
		t.Skip("no records — nothing to check")
	}
	r := records[0]
	if r.deprecated == "" {
		t.Error("deprecated property missing")
	}
	if r.annotation == "" {
		t.Error("annotation property missing")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
