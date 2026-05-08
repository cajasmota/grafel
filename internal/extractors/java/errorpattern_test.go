package java_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/java"
	"github.com/cajasmota/archigraph/internal/types"
)

// extractJava runs the java extractor and returns the records.
func extractJava(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Test.java",
		Content:  []byte(src),
		Language: "java",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return recs
}

// errorPatternsJava filters records to SCOPE.Pattern entities.
func errorPatternsJava(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	var out []types.EntityRecord
	for _, r := range extractJava(t, src) {
		if r.Kind == "SCOPE.Pattern" {
			out = append(out, r)
		}
	}
	return out
}

// TestErrorPatternJava_SingleTryCatch verifies one try/catch emits one
// SCOPE.Pattern entity with the correct key and line number.
func TestErrorPatternJava_SingleTryCatch(t *testing.T) {
	src := `
package com.example;

public class Foo {
    public void load() {
        try {
            doWork();
        } catch (Exception e) {
            System.out.println(e);
        }
    }

    private void doWork() {}
}
`
	patterns := errorPatternsJava(t, src)
	if len(patterns) != 1 {
		t.Fatalf("expected 1 error pattern, got %d", len(patterns))
	}
	p := patterns[0]
	if p.Kind != "SCOPE.Pattern" {
		t.Errorf("Kind = %q, want %q", p.Kind, "SCOPE.Pattern")
	}
	if !strings.HasPrefix(p.Name, "error_handling:try_catch:") {
		t.Errorf("Name = %q, missing error_handling:try_catch: prefix", p.Name)
	}
	if p.Language != "java" {
		t.Errorf("Language = %q, want %q", p.Language, "java")
	}
	pt, _ := p.Metadata["pattern_type"].(string)
	if pt != "error_handling" {
		t.Errorf("metadata.pattern_type = %q, want %q", pt, "error_handling")
	}
	if p.StartLine == 0 || p.EndLine == 0 {
		t.Errorf("StartLine/EndLine must be non-zero, got %d/%d", p.StartLine, p.EndLine)
	}
	if p.StartLine != p.EndLine {
		t.Errorf("StartLine (%d) must equal EndLine (%d)", p.StartLine, p.EndLine)
	}
}

// TestErrorPatternJava_MultipleTryCatch verifies each try/catch block
// produces its own entity.
func TestErrorPatternJava_MultipleTryCatch(t *testing.T) {
	src := `
package com.example;

public class Foo {
    public void a() {
        try { x(); } catch (Exception e) {}
        try { y(); } catch (Exception e) {}
        try { z(); } catch (Exception e) {}
    }
    private void x() {}
    private void y() {}
    private void z() {}
}
`
	patterns := errorPatternsJava(t, src)
	if len(patterns) != 3 {
		t.Fatalf("expected 3 patterns, got %d", len(patterns))
	}
	seen := make(map[string]bool, len(patterns))
	for _, p := range patterns {
		if seen[p.Name] {
			t.Errorf("duplicate Name %q", p.Name)
		}
		seen[p.Name] = true
	}
}

// TestErrorPatternJava_TryFinallyNoCatch verifies a try-finally block
// without a catch clause is still captured.
func TestErrorPatternJava_TryFinallyNoCatch(t *testing.T) {
	src := `
package com.example;

public class Foo {
    public void a() {
        try {
            doWork();
        } finally {
            cleanup();
        }
    }
    private void doWork() {}
    private void cleanup() {}
}
`
	patterns := errorPatternsJava(t, src)
	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern for try/finally, got %d", len(patterns))
	}
}

// TestErrorPatternJava_TryWithResources verifies a try-with-resources
// block produces a pattern entity.
func TestErrorPatternJava_TryWithResources(t *testing.T) {
	src := `
package com.example;

import java.io.*;

public class Foo {
    public void a() throws IOException {
        try (BufferedReader br = new BufferedReader(new FileReader("x"))) {
            br.readLine();
        }
    }
}
`
	patterns := errorPatternsJava(t, src)
	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern for try-with-resources, got %d", len(patterns))
	}
}

// TestErrorPatternJava_NestedTry verifies nested try/catch blocks are
// each captured separately.
func TestErrorPatternJava_NestedTry(t *testing.T) {
	src := `
package com.example;

public class Foo {
    public void a() {
        try {
            try {
                x();
            } catch (RuntimeException e) {}
        } catch (Exception e) {}
    }
    private void x() {}
}
`
	patterns := errorPatternsJava(t, src)
	if len(patterns) != 2 {
		t.Fatalf("expected 2 patterns for nested try, got %d", len(patterns))
	}
}

// TestErrorPatternJava_NoTry verifies files without try blocks produce
// no pattern records.
func TestErrorPatternJava_NoTry(t *testing.T) {
	src := `
package com.example;

public class Foo {
    public void a() {
        System.out.println("hi");
    }
}
`
	patterns := errorPatternsJava(t, src)
	if len(patterns) != 0 {
		t.Fatalf("expected 0 patterns, got %d", len(patterns))
	}
}

// TestErrorPatternJava_EmptyFile verifies empty content produces no
// patterns (no crash on nil tree).
func TestErrorPatternJava_EmptyFile(t *testing.T) {
	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.java",
		Content:  []byte(""),
		Language: "java",
		Tree:     nil,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var count int
	for _, r := range recs {
		if r.Kind == "SCOPE.Pattern" {
			count++
		}
	}
	if count != 0 {
		t.Fatalf("expected 0 patterns for empty file, got %d", count)
	}
}

// TestErrorPatternJava_PreservesBaseExtraction verifies class + method
// records still come through after the secondary pass is added.
func TestErrorPatternJava_PreservesBaseExtraction(t *testing.T) {
	src := `
package com.example;

public class Worker {
    public void run() {
        try {
            doWork();
        } catch (Exception e) {}
    }
    private void doWork() {}
}
`
	recs := extractJava(t, src)
	var hasClass, hasMethod, hasPattern bool
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Name == "Worker" {
			hasClass = true
		}
		if r.Kind == "SCOPE.Operation" && r.Name == "run" {
			hasMethod = true
		}
		if r.Kind == "SCOPE.Pattern" {
			hasPattern = true
		}
	}
	if !hasClass {
		t.Error("base class extraction missing")
	}
	if !hasMethod {
		t.Error("base method extraction missing")
	}
	if !hasPattern {
		t.Error("error handling pattern missing")
	}
}

// TestErrorPatternJava_UniqueNames verifies multi-pattern output has
// no duplicate Name values.
func TestErrorPatternJava_UniqueNames(t *testing.T) {
	src := `
package com.example;

public class Foo {
    public void a() {
        try { x(); } catch (Exception e) {}
        try { x(); } catch (Exception e) {}
    }
    private void x() {}
}
`
	patterns := errorPatternsJava(t, src)
	seen := make(map[string]bool, len(patterns))
	for _, p := range patterns {
		if seen[p.Name] {
			t.Errorf("duplicate Name %q", p.Name)
		}
		seen[p.Name] = true
	}
}

// TestErrorPatternJava_NamePreservesLine verifies the Name line suffix
// matches the StartLine value exactly (catches stringification bugs).
func TestErrorPatternJava_NamePreservesLine(t *testing.T) {
	src := `
package com.example;

public class Foo {
    public void a() {
        int x = 1;
        try {
            doWork();
        } catch (Exception e) {}
    }
    private void doWork() {}
}
`
	patterns := errorPatternsJava(t, src)
	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(patterns))
	}
	p := patterns[0]
	wantName := fmt.Sprintf("error_handling:try_catch:%d", p.StartLine)
	if p.Name != wantName {
		t.Errorf("Name = %q, want %q", p.Name, wantName)
	}
}
