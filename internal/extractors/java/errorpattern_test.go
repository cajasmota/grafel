package java_test

import (
	"context"
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

// TestErrorPatternJava_NoTryCatchEmit verifies issue #2282 — the Java
// extractor must no longer emit per-line `error_handling:try_catch:N`
// SCOPE.Pattern entities. Exercises plain try/catch and try-with-resources.
func TestErrorPatternJava_NoTryCatchEmit(t *testing.T) {
	src := `
package com.example;

import java.io.BufferedReader;
import java.io.FileReader;

public class Foo {
    public void load() {
        try {
            doWork();
        } catch (Exception e) {
            System.out.println(e);
        }
    }

    public void withResources() throws Exception {
        try (BufferedReader r = new BufferedReader(new FileReader("x"))) {
            r.readLine();
        } catch (Exception e) {
            throw e;
        }
    }

    private void doWork() {}
}
`
	for _, r := range extractJava(t, src) {
		if r.Kind == "SCOPE.Pattern" && strings.HasPrefix(r.Name, "error_handling:try_catch:") {
			t.Errorf("regression: %q emitted; #2282 dropped per-line try_catch entities", r.Name)
		}
	}
}
