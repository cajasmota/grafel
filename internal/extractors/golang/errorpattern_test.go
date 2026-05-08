package golang_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/golang"
	"github.com/cajasmota/archigraph/internal/types"
)

// errorPatternEntities filters the extracted records down to only the
// SCOPE.Pattern records emitted by the error-handling pass. Returns
// them in source order so we can make deterministic line assertions.
func errorPatternEntities(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	recs, err := extractRecordsTyped(t, src, "test.go")
	if err != nil {
		t.Fatalf("extractRecordsTyped: %v", err)
	}
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Kind == "SCOPE.Pattern" {
			out = append(out, r)
		}
	}
	return out
}

// extractRecordsTyped is a typed wrapper around the test helper that returns
// []types.EntityRecord instead of []interface{}.
func extractRecordsTyped(t *testing.T, src, path string) ([]types.EntityRecord, error) {
	t.Helper()
	content := []byte(src)
	tree := parseGo(content)
	ext, ok := extractor.Get("go")
	if !ok {
		t.Fatal("go extractor not registered")
	}
	return ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  content,
		Language: "go",
		Tree:     tree,
	})
}

// TestErrorPattern_SingleIfErrNil verifies one basic `if err != nil`
// emits one SCOPE.Pattern entity with the correct Name key and line.
func TestErrorPattern_SingleIfErrNil(t *testing.T) {
	src := `package main

import "errors"

func load() error {
	err := errors.New("boom")
	if err != nil {
		return err
	}
	return nil
}
`
	patterns := errorPatternEntities(t, src)
	if len(patterns) != 1 {
		t.Fatalf("expected 1 error pattern, got %d", len(patterns))
	}
	p := patterns[0]
	if p.Kind != "SCOPE.Pattern" {
		t.Errorf("Kind = %q, want %q", p.Kind, "SCOPE.Pattern")
	}
	wantName := "error_handling:go_error_return:7"
	if p.Name != wantName {
		t.Errorf("Name = %q, want %q", p.Name, wantName)
	}
	if p.StartLine != 7 {
		t.Errorf("StartLine = %d, want 7", p.StartLine)
	}
	if p.EndLine != 7 {
		t.Errorf("EndLine = %d, want 7", p.EndLine)
	}
	if p.Language != "go" {
		t.Errorf("Language = %q, want %q", p.Language, "go")
	}
	if p.SourceFile != "test.go" {
		t.Errorf("SourceFile = %q, want %q", p.SourceFile, "test.go")
	}
	pt, _ := p.Metadata["pattern_type"].(string)
	if pt != "error_handling" {
		t.Errorf("metadata.pattern_type = %q, want %q", pt, "error_handling")
	}
}

// TestErrorPattern_MultipleOccurrences verifies every separate
// `if err != nil` block produces its own entity, keyed by its own
// line number. Tests behaviour rule #1: one record per occurrence,
// not one per file.
func TestErrorPattern_MultipleOccurrences(t *testing.T) {
	src := `package main

func a() error {
	err := run()
	if err != nil {
		return err
	}
	err = run2()
	if err != nil {
		return err
	}
	return nil
}

func run() error  { return nil }
func run2() error { return nil }
`
	patterns := errorPatternEntities(t, src)
	if len(patterns) != 2 {
		t.Fatalf("expected 2 error patterns, got %d", len(patterns))
	}
	// findAll is DFS stack-based so emission order is not guaranteed to
	// match source order. Assert by presence on a set.
	got := make(map[string]bool, len(patterns))
	for _, p := range patterns {
		got[p.Name] = true
	}
	for _, line := range []int{5, 9} {
		want := fmt.Sprintf("error_handling:go_error_return:%d", line)
		if !got[want] {
			t.Errorf("missing expected pattern %q; got %v", want, got)
		}
	}
}

// TestErrorPattern_InitStatementForm verifies the grammar where the if
// has an init short-var decl — `if x, err := f(); err != nil { ... }`.
// The condition field on tree-sitter-go points at the boolean side only.
func TestErrorPattern_InitStatementForm(t *testing.T) {
	src := `package main

func read() error {
	if x, err := doWork(); err != nil {
		_ = x
		return err
	}
	return nil
}

func doWork() (int, error) { return 0, nil }
`
	patterns := errorPatternEntities(t, src)
	if len(patterns) != 1 {
		t.Fatalf("expected 1 error pattern, got %d", len(patterns))
	}
	if patterns[0].StartLine != 4 {
		t.Errorf("StartLine = %d, want 4", patterns[0].StartLine)
	}
}

// TestErrorPattern_CamelCaseErrIdent verifies error-typed identifiers
// whose name ends in "Err" (e.g. parseErr, requestErr) are matched.
func TestErrorPattern_CamelCaseErrIdent(t *testing.T) {
	src := `package main

func parse() error {
	parseErr := run()
	if parseErr != nil {
		return parseErr
	}
	return nil
}

func run() error { return nil }
`
	patterns := errorPatternEntities(t, src)
	if len(patterns) != 1 {
		t.Fatalf("expected 1 error pattern, got %d", len(patterns))
	}
}

// TestErrorPattern_ExcludesIfErrEqNil verifies the `if err == nil`
// form is NOT emitted — only `!=` is a return/abort pattern.
func TestErrorPattern_ExcludesIfErrEqNil(t *testing.T) {
	src := `package main

func a() error {
	err := run()
	if err == nil {
		return nil
	}
	return err
}

func run() error { return nil }
`
	patterns := errorPatternEntities(t, src)
	if len(patterns) != 0 {
		t.Fatalf("expected 0 error patterns for == form, got %d", len(patterns))
	}
}

// TestErrorPattern_ExcludesNonErrorCondition verifies that non-error
// conditions (e.g. `if x != nil`, `if cfg != nil`) are NOT emitted —
// only identifiers whose name looks like an error value match.
func TestErrorPattern_ExcludesNonErrorCondition(t *testing.T) {
	src := `package main

func a(cfg *string) {
	if cfg != nil {
		return
	}
}
`
	patterns := errorPatternEntities(t, src)
	if len(patterns) != 0 {
		t.Fatalf("expected 0 patterns, got %d", len(patterns))
	}
}

// TestErrorPattern_ExcludesSentinelUpperCase verifies a sentinel error
// reference like `ErrNotFound != nil` does NOT match the naming rule
// (uppercase-leading name → not a local error var).
func TestErrorPattern_ExcludesSentinelUpperCase(t *testing.T) {
	src := `package main

var ErrNoRows = (func() error { return nil })()

func a() {
	if ErrNoRows != nil {
		return
	}
}
`
	patterns := errorPatternEntities(t, src)
	if len(patterns) != 0 {
		t.Fatalf("expected 0 patterns for uppercase sentinel, got %d", len(patterns))
	}
}

// TestErrorPattern_NestedFunctionBody verifies error patterns in nested
// function bodies are captured (the walker visits the whole tree).
func TestErrorPattern_NestedFunctionBody(t *testing.T) {
	src := `package main

func outer() error {
	inner := func() error {
		err := run()
		if err != nil {
			return err
		}
		return nil
	}
	return inner()
}

func run() error { return nil }
`
	patterns := errorPatternEntities(t, src)
	if len(patterns) != 1 {
		t.Fatalf("expected 1 error pattern in nested func, got %d", len(patterns))
	}
}

// TestErrorPattern_EmptyFile verifies an empty Go file produces zero
// pattern records (no spurious warn logs).
func TestErrorPattern_EmptyFile(t *testing.T) {
	src := `package main
`
	patterns := errorPatternEntities(t, src)
	if len(patterns) != 0 {
		t.Fatalf("expected 0 patterns for empty file, got %d", len(patterns))
	}
}

// TestErrorPattern_PreservesBaseExtraction verifies the secondary pass
// does NOT shadow or replace entities from the base pass. The test
// file has 1 function and 1 error pattern, so we should see both.
func TestErrorPattern_PreservesBaseExtraction(t *testing.T) {
	src := `package main

func load() error {
	err := run()
	if err != nil {
		return err
	}
	return nil
}

func run() error { return nil }
`
	recs, err := extractRecordsTyped(t, src, "test.go")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	var hasFunc, hasPattern bool
	for _, r := range recs {
		if r.Kind == "SCOPE.Operation" && r.Name == "load" {
			hasFunc = true
		}
		if r.Kind == "SCOPE.Pattern" && strings.HasPrefix(r.Name, "error_handling:go_error_return:") {
			hasPattern = true
		}
	}
	if !hasFunc {
		t.Error("base extraction for function 'load' missing — secondary pass broke primary output")
	}
	if !hasPattern {
		t.Error("expected error handling pattern entity to be present")
	}
}

// TestErrorPattern_SampleFixture verifies the real sample.go fixture
// emits the parity-expected number of error_handling:go_error_return
// entities. MX-1047 AC#1 target is 19.
//
// The fixture is loaded from testdata/sample.go.fixture so it does not
// get compiled as part of the package. Skips gracefully if the fixture
// is missing (keeps hermetic-CI runs green when the checkout is narrow).
func TestErrorPattern_SampleFixture(t *testing.T) {
	const expected = 19
	path := filepath.Join("testdata", "sample.go.fixture")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("sample.go.fixture not available: %v", err)
	}
	recs, err := extractRecordsTyped(t, string(src), "sample.go")
	if err != nil {
		t.Fatalf("extractRecordsTyped: %v", err)
	}
	var errPatterns []types.EntityRecord
	for _, r := range recs {
		if r.Kind == "SCOPE.Pattern" && strings.HasPrefix(r.Name, "error_handling:go_error_return:") {
			errPatterns = append(errPatterns, r)
		}
	}
	if len(errPatterns) < expected {
		t.Fatalf("sample fixture: expected at least %d error_handling patterns, got %d", expected, len(errPatterns))
	}
	t.Logf("sample fixture: emitted %d error_handling patterns (AC target: %d)", len(errPatterns), expected)
}

// TestErrorPattern_UniqueLineNumbers verifies every emitted pattern
// has a unique Name — duplicates would indicate an off-by-one walker
// bug. Exercises multi-pattern output.
func TestErrorPattern_UniqueLineNumbers(t *testing.T) {
	src := `package main

func a() error {
	err := run()
	if err != nil { return err }
	err = run()
	if err != nil { return err }
	err = run()
	if err != nil { return err }
	return nil
}

func run() error { return nil }
`
	patterns := errorPatternEntities(t, src)
	if len(patterns) != 3 {
		t.Fatalf("expected 3 patterns, got %d", len(patterns))
	}
	seen := make(map[string]bool)
	for _, p := range patterns {
		if seen[p.Name] {
			t.Errorf("duplicate pattern Name %q — off-by-one walker bug", p.Name)
		}
		seen[p.Name] = true
	}
}
