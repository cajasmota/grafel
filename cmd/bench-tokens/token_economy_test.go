package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	// char/4 — matches the codebase-wide estimator.
	cases := map[string]int{"": 0, "abc": 0, "abcd": 1, "12345678": 2}
	for in, want := range cases {
		if got := estimateTokens(in); got != want {
			t.Errorf("estimateTokens(%q)=%d want %d", in, got, want)
		}
	}
}

func TestSavingsRatio(t *testing.T) {
	if got := savingsRatio(100, 400); got != 4.0 {
		t.Errorf("savingsRatio(100,400)=%v want 4.0", got)
	}
	// Graph cost of zero (error/no-match) must not divide-by-zero.
	if got := savingsRatio(0, 400); got != 0 {
		t.Errorf("savingsRatio(0,400)=%v want 0", got)
	}
}

func TestAggregateRatio(t *testing.T) {
	rows := []questionCost{
		{Question: "a", GraphTokens: 100, FileReadTokens: 400}, // 4x
		{Question: "b", GraphTokens: 200, FileReadTokens: 600}, // 3x
		{Question: "err", GraphTokens: 0, FileReadTokens: 999}, // skipped
	}
	// (400+600)/(100+200) = 1000/300 = 3.333...
	got := aggregateRatio(rows)
	if got < 3.33 || got > 3.34 {
		t.Errorf("aggregateRatio=%v want ~3.333 (zero-graph row must be excluded)", got)
	}
}

func TestAggregateRatio_AllZero(t *testing.T) {
	rows := []questionCost{{Question: "x", GraphTokens: 0, FileReadTokens: 10}}
	if got := aggregateRatio(rows); got != 0 {
		t.Errorf("aggregateRatio(all-zero)=%v want 0", got)
	}
}

func TestExtractSourceFiles(t *testing.T) {
	payload := strings.Join([]string{
		"# nodes (3 matched)",
		"TokenAuthMiddleware  internal/mcp/render.go:52",
		"OrderService.place  cmd/archigraph/index.go:140",
		"dup-hit  internal/mcp/render.go:88", // same file -> dedup
		"# edges-summary: available=2",
		"a → b  [CALLS]", // not a location, must not match
	}, "\n")
	got := extractSourceFiles(payload)
	want := []string{"cmd/archigraph/index.go", "internal/mcp/render.go"}
	if len(got) != len(want) {
		t.Fatalf("extractSourceFiles=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("extractSourceFiles=%v want %v", got, want)
		}
	}
}

func TestFileReadTokens(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.go")
	if err := os.WriteFile(f, []byte(strings.Repeat("x", 400)), 0644); err != nil {
		t.Fatal(err)
	}
	// 400 chars -> 100 tokens, plus one missing file -> fallback.
	got := fileReadTokens([]string{f, filepath.Join(dir, "missing.go")})
	want := 100 + perFileFallbackTokens
	if got != want {
		t.Errorf("fileReadTokens=%d want %d", got, want)
	}
}

func TestRenderMarkdown(t *testing.T) {
	rows := []questionCost{
		{Question: "where is auth", GraphTokens: 100, FileReadTokens: 400, FileCount: 2},
		{Question: "broken | pipe", GraphTokens: 0, FileReadTokens: 0, Note: "no matches"},
	}
	md := renderMarkdown("upvate", rows)

	for _, want := range []string{
		"# Token-economy benchmark — upvate",
		"| # | Question |",
		"4.00x",          // per-row ratio
		`broken \| pipe`, // pipe escaped
		"_(no matches)_", // note rendered
		"**Aggregate:**", // aggregate line present
	} {
		if !strings.Contains(md, want) {
			t.Errorf("renderMarkdown output missing %q\n---\n%s", want, md)
		}
	}
	// The zero-graph row must render an em-dash ratio, not a divide-by-zero.
	if !strings.Contains(md, "| — |") {
		t.Errorf("expected em-dash ratio for zero-graph row\n%s", md)
	}
}

func TestLoadQuestions(t *testing.T) {
	// Default set when no path.
	def, err := loadQuestions("")
	if err != nil || len(def) == 0 {
		t.Fatalf("loadQuestions(\"\")=%v,%v want non-empty default", def, err)
	}

	dir := t.TempDir()
	p := filepath.Join(dir, "q.txt")
	body := "# a comment\n\nfirst question\n  second question  \n# trailing comment\n"
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := loadQuestions(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"first question", "second question"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("loadQuestions=%v want %v (comments/blanks stripped, trimmed)", got, want)
	}
}

func TestLoadQuestions_EmptyFileErrors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(p, []byte("# only comments\n\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadQuestions(p); err == nil {
		t.Error("loadQuestions on comment-only file: want error, got nil")
	}
}
