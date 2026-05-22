//go:build fixture_verify

package walk

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestFixtureVerify_PolyglotPlatform runs WalkRepo against the real
// polyglot-platform fixture and confirms that:
//   - _generated/ dirs (D24) produce zero file results
//   - vendor/ dirs (D24/D25) produce zero file results
//   - _generated appears in the skipped list
//
// Run with: go test -tags fixture_verify -v ./internal/daemon/walk/
func TestFixtureVerify_PolyglotPlatform(t *testing.T) {
	root := "/Users/jorgecajas/Documents/Projects/polyglot-platform"
	files, skipped, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	fmt.Printf("Total files walked: %d\n", len(files))
	fmt.Printf("Total dirs skipped: %d\n", len(skipped))

	fmt.Println("\nSkipped dirs:")
	for _, s := range skipped {
		rel := strings.TrimPrefix(s.AbsPath, root+"/")
		fmt.Printf("  [%s] %s\n", s.Rule, rel)
	}

	// Check _generated is excluded.
	generatedLeaks := []string{}
	for _, f := range files {
		if strings.Contains(f, "_generated/") {
			generatedLeaks = append(generatedLeaks, f)
		}
	}
	if len(generatedLeaks) > 0 {
		t.Errorf("_generated files leaked (%d):\n  %s", len(generatedLeaks), strings.Join(generatedLeaks, "\n  "))
	} else {
		fmt.Println("\n[PASS] No _generated/ files in walk results")
	}

	// Check vendor is excluded.
	vendorLeaks := []string{}
	for _, f := range files {
		if strings.HasPrefix(f, "vendor/") || strings.Contains(f, "/vendor/") {
			vendorLeaks = append(vendorLeaks, f)
		}
	}
	if len(vendorLeaks) > 0 {
		t.Errorf("vendor/ files leaked (%d):\n  %s", len(vendorLeaks), strings.Join(vendorLeaks, "\n  "))
	} else {
		fmt.Println("[PASS] No vendor/ files in walk results")
	}

	// Confirm _generated appears in skipped list.
	generatedSkipped := false
	for _, s := range skipped {
		if filepath.Base(s.AbsPath) == "_generated" {
			generatedSkipped = true
			break
		}
	}
	if !generatedSkipped {
		t.Error("_generated dir was not in skipped list")
	} else {
		fmt.Println("[PASS] _generated dir appears in skipped list")
	}
}
