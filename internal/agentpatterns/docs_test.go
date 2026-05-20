package agentpatterns

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func approvedPattern() Pattern {
	return Pattern{
		ID:           "abc123def4567890",
		Kind:         "AgentPattern",
		Category:     CategoryCode,
		Trigger:      Trigger{NaturalLanguage: "register a chi HTTP handler for a new endpoint"},
		Steps:        []string{"Add a route to RegisterRoutes", "Create the handler in handlers package", "Add a test under handlers_test.go"},
		Confidence:   0.72,
		Observations: 6,
		IsCandidate:  false,
		AntiPatterns: []AntiPattern{
			{DoNot: "register the route from `init()`", Reason: "breaks deterministic teardown"},
			{DoNot: "leak db handle to request", Reason: "internal team rule", Private: true},
		},
	}
}

func TestRenderMarkdown_basicShape(t *testing.T) {
	in := MarkdownInput{
		Pattern: approvedPattern(),
		ExemplarRefs: []ExemplarRef{
			{EntityName: "RegisterRoutes", FilePath: "internal/http/routes.go", StartLine: 12, EndLine: 48},
		},
		RelatedPatterns: []RelatedPattern{
			{ID: "code/other000000", Trigger: "writing a chi middleware", Edge: "CO_APPLIES_WITH"},
		},
	}
	md, err := RenderMarkdown(in)
	if err != nil {
		t.Fatal(err)
	}
	mustContain := []string{
		"# ", // title
		"**Status**: Active",
		"**Category**: code",
		"**Confidence**: 0.72",
		"## When to use",
		"## Recipe",
		"1. ",
		"## Exemplars",
		"`RegisterRoutes`",
		"internal/http/routes.go",
		"12-48",
		"## Anti-patterns",
		"register the route",
		"breaks deterministic teardown",
		"## Related patterns",
		"CO_APPLIES_WITH",
	}
	for _, w := range mustContain {
		if !strings.Contains(md, w) {
			t.Fatalf("missing %q in rendered markdown:\n%s", w, md)
		}
	}
	if strings.Contains(md, "leak db handle to request") {
		t.Fatalf("private anti-pattern leaked into generated markdown")
	}
}

func TestRenderMarkdown_skipsCandidates(t *testing.T) {
	p := approvedPattern()
	p.IsCandidate = true
	md, err := RenderMarkdown(MarkdownInput{Pattern: p})
	if err != nil {
		t.Fatal(err)
	}
	if md != "" {
		t.Fatalf("expected empty markdown for candidate, got: %s", md)
	}
}

func TestWriteMarkdown_writesCategoryDir(t *testing.T) {
	dir := t.TempDir()
	got, err := WriteMarkdown(dir, MarkdownInput{Pattern: approvedPattern()})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "code", "abc123def4567890.md")
	if got != want {
		t.Fatalf("want %s, got %s", want, got)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("file not written: %v", err)
	}
}

func TestCheckBacktickConvention_flagsBareIdentifiers(t *testing.T) {
	md := "# Configure HelloWorldHandler in MyAwesomeRouter\n\nbody text.\n"
	violations := CheckBacktickConvention(md)
	if len(violations) == 0 {
		t.Fatalf("expected violations for un-backticked CamelCase identifiers")
	}
}

func TestCheckBacktickConvention_acceptsBackticked(t *testing.T) {
	md := "# Configure `HelloWorldHandler` in `MyAwesomeRouter`\n"
	violations := CheckBacktickConvention(md)
	if len(violations) != 0 {
		t.Fatalf("expected zero violations, got: %v", violations)
	}
}

func TestCheckBacktickConvention_ignoresBody(t *testing.T) {
	md := "# Title\n\nBareIdentifierInBody should not fail.\n"
	violations := CheckBacktickConvention(md)
	if len(violations) != 0 {
		t.Fatalf("body-level identifiers must not be flagged: %v", violations)
	}
}

func TestRenderMarkdown_autoBacktick(t *testing.T) {
	p := approvedPattern()
	p.Trigger.NaturalLanguage = "register a chi_router.HandleFunc() for a new endpoint"
	md, err := RenderMarkdown(MarkdownInput{Pattern: p})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "`chi_router.HandleFunc()`") {
		t.Fatalf("auto-backtick failed for headings:\n%s", md)
	}
	violations := CheckBacktickConvention(md)
	if len(violations) != 0 {
		t.Fatalf("rendered output failed linter: %v", violations)
	}
}
