package treesitter_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// newTestFactory returns a ParserFactory wired to an in-memory span exporter so
// tests can assert that OTel spans are emitted with the correct attributes.
func newTestFactory(t *testing.T, exporter *tracetest.InMemoryExporter) *treesitter.ParserFactory {
	t.Helper()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	tracer := tp.Tracer("treesitter-test")
	return treesitter.NewParserFactory(tracer)
}

// ---------- happy paths ----------

func TestParse_GoFile_ZeroErrorRatio(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	f := newTestFactory(t, exp)

	src := []byte(`package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`)
	res, err := f.Parse(context.Background(), src, "go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TSTree == nil {
		t.Fatal("expected non-nil tree")
	}
	if res.ErrorRatio != 0.0 {
		t.Errorf("expected error_ratio=0.0, got %.4f", res.ErrorRatio)
	}
	if res.NodeCount == 0 {
		t.Error("expected node_count > 0")
	}
	if res.Language != "go" {
		t.Errorf("expected language=go, got %s", res.Language)
	}
}

func TestParse_PythonFile_Correct(t *testing.T) {
	f := treesitter.NewParserFactory(nil)

	src := []byte(`def greet(name: str) -> str:
    return f"hello {name}"

class Greeter:
    def __init__(self, name: str):
        self.name = name
`)
	res, err := f.Parse(context.Background(), src, "python")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ErrorRatio != 0.0 {
		t.Errorf("expected error_ratio=0.0, got %.4f", res.ErrorRatio)
	}
	if res.NodeCount == 0 {
		t.Error("expected node_count > 0")
	}
}

// ---------- error paths ----------

func TestParse_MalformedFile_TriggerHighSyntaxErrorRate(t *testing.T) {
	f := treesitter.NewParserFactory(nil)

	// Severely malformed Go: mix of symbols, unclosed brackets, random tokens.
	src := []byte(`!@#$%^&*() }{][|\ func ??? {{{{{{{{
broken broken broken broken ??? !!! @@@ ###
func )()()(  }{}{}{} :::
@@@!!!### broken ???
`)
	res, err := f.Parse(context.Background(), src, "go")
	if err == nil {
		t.Fatalf("expected ErrHighSyntaxErrorRate, got nil (error_ratio=%.4f)", res.ErrorRatio)
	}
	if !errors.Is(err, treesitter.ErrHighSyntaxErrorRate) {
		t.Errorf("expected ErrHighSyntaxErrorRate, got: %v", err)
	}
	// Result is returned alongside the error so callers can inspect the tree.
	if res == nil {
		t.Error("expected non-nil ParseResult even on ErrHighSyntaxErrorRate")
	}
	if res != nil && res.ErrorRatio <= 0.10 {
		t.Errorf("expected error_ratio > 0.10, got %.4f", res.ErrorRatio)
	}
}

func TestParse_UnknownLanguage_ReturnsErrUnsupportedLanguage(t *testing.T) {
	f := treesitter.NewParserFactory(nil)

	_, err := f.Parse(context.Background(), []byte("some code"), "brainfuck")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, treesitter.ErrUnsupportedLanguage) {
		t.Errorf("expected ErrUnsupportedLanguage, got: %v", err)
	}
}

func TestParse_EmptyFile_ZeroNodesNoError(t *testing.T) {
	f := treesitter.NewParserFactory(nil)

	res, err := f.Parse(context.Background(), []byte{}, "go")
	if err != nil {
		t.Fatalf("unexpected error on empty file: %v", err)
	}
	if res.NodeCount != 0 {
		t.Errorf("expected node_count=0 for empty file, got %d", res.NodeCount)
	}
	if res.ErrorRatio != 0.0 {
		t.Errorf("expected error_ratio=0.0 for empty file, got %.4f", res.ErrorRatio)
	}
	if res.TSTree != nil {
		t.Error("expected nil tree for empty file")
	}
}

func TestParse_VeryLargeFile_NoPanic(t *testing.T) {
	f := treesitter.NewParserFactory(nil)

	// Generate a >100KB Go file with many functions.
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	for i := 0; i < 3000; i++ {
		sb.WriteString(`func doWork() int {
	x := 1 + 2
	return x
}
`)
	}
	src := []byte(sb.String())
	if len(src) < 100*1024 {
		t.Fatalf("generated source too small: %d bytes", len(src))
	}

	res, err := f.Parse(context.Background(), src, "go")
	if err != nil {
		t.Fatalf("unexpected error on large file: %v", err)
	}
	if res.NodeCount == 0 {
		t.Error("expected node_count > 0 on large file")
	}
}

// ---------- OTel span ----------

func TestParse_OTelSpan_EmittedWithCorrectAttributes(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	f := newTestFactory(t, exp)

	src := []byte(`x = 1`)
	res, err := f.Parse(context.Background(), src, "python")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span, got none")
	}

	var parseSpan *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == "treesitter.parse" {
			parseSpan = &spans[i]
			break
		}
	}
	if parseSpan == nil {
		t.Fatal("span named 'treesitter.parse' not found")
	}

	attrs := make(map[string]interface{})
	for _, a := range parseSpan.Attributes {
		attrs[string(a.Key)] = a.Value.AsInterface()
	}

	if attrs["language"] != "python" {
		t.Errorf("expected language=python, got %v", attrs["language"])
	}
	if attrs["file_size_bytes"] != int64(len(src)) {
		t.Errorf("expected file_size_bytes=%d, got %v", len(src), attrs["file_size_bytes"])
	}
	if _, ok := attrs["error_ratio"]; !ok {
		t.Error("expected error_ratio attribute")
	}
	if _, ok := attrs["node_count"]; !ok {
		t.Error("expected node_count attribute")
	}
	_ = res
}

func TestParse_OTelSpan_EmittedOnUnsupportedLanguage(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	f := newTestFactory(t, exp)

	_, err := f.Parse(context.Background(), []byte("code"), "fortran")
	if !errors.Is(err, treesitter.ErrUnsupportedLanguage) {
		t.Fatalf("expected ErrUnsupportedLanguage, got: %v", err)
	}

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected span even on error")
	}
	found := false
	for _, s := range spans {
		if s.Name == "treesitter.parse" {
			found = true
		}
	}
	if !found {
		t.Error("treesitter.parse span not emitted for unsupported language")
	}
}

// ---------- SupportedLanguages ----------

func TestSupportedLanguages_ReturnsExpectedCount(t *testing.T) {
	langs := treesitter.SupportedLanguages()
	// 28 entries: 27 unique grammars + "terraform" alias for "hcl".
	// Added: groovy, proto. Dropped: markdown (official-grammar flip, #5418).
	const expected = 28
	if len(langs) != expected {
		t.Errorf("expected %d languages, got %d: %v", expected, len(langs), langs)
	}
}

func TestSupportedLanguages_ContainsRequiredEntries(t *testing.T) {
	langs := treesitter.SupportedLanguages()
	set := make(map[string]bool, len(langs))
	for _, l := range langs {
		set[l] = true
	}

	required := []string{
		"go", "python", "javascript", "typescript", "java", "kotlin",
		"ruby", "php", "rust", "c", "cpp", "csharp", "swift", "scala",
		"elixir", "bash", "lua", "ocaml", "toml", "yaml", "sql",
		"html", "css", "dockerfile", "hcl", "terraform",
		"groovy", "proto",
	}
	for _, lang := range required {
		if !set[lang] {
			t.Errorf("missing required language: %s", lang)
		}
	}
}

func TestSupportedLanguages_EachLanguageParsesSnippet(t *testing.T) {
	f := treesitter.NewParserFactory(nil)

	// Minimal valid snippets for each language to confirm grammar loads.
	snippets := map[string]string{
		"bash":       `echo "hello"`,
		"c":          `int main() { return 0; }`,
		"cpp":        `int main() { return 0; }`,
		"css":        `body { color: red; }`,
		"csharp":     `class A {}`,
		"dockerfile": `FROM ubuntu:22.04`,
		"elixir":     `def hello, do: :world`,
		"go":         `package main`,
		"groovy":     `class Foo { def bar() { return 1 } }`,
		"hcl":        `variable "x" {}`,
		"html":       `<html></html>`,
		"java":       `class A {}`,
		"javascript": `const x = 1;`,
		"kotlin":     `fun main() {}`,
		"lua":        `print("hi")`,
		"ocaml":      `let x = 1`,
		"php":        `<?php echo "hi"; ?>`,
		"proto":      `syntax = "proto3"; message Foo { string id = 1; }`,
		"python":     `x = 1`,
		"ruby":       `puts "hi"`,
		"rust":       `fn main() {}`,
		"scala":      `object A {}`,
		"sql":        `SELECT 1;`,
		"swift":      `var x = 1`,
		"terraform":  `variable "x" {}`,
		"toml":       `[section]`,
		"typescript": `const x: number = 1;`,
		"yaml":       `key: value`,
	}

	langs := treesitter.SupportedLanguages()
	for _, lang := range langs {
		lang := lang
		t.Run(lang, func(t *testing.T) {
			snippet, ok := snippets[lang]
			if !ok {
				t.Fatalf("no test snippet for language %s — add one", lang)
			}
			res, err := f.Parse(context.Background(), []byte(snippet), lang)
			if err != nil && !errors.Is(err, treesitter.ErrHighSyntaxErrorRate) {
				t.Fatalf("Parse(%s) returned unexpected error: %v", lang, err)
			}
			if err == nil && res.TSTree == nil {
				t.Errorf("Parse(%s) returned nil tree", lang)
			}
		})
	}
}
