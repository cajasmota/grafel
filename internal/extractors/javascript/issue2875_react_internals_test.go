// Package javascript_test — issue #2875 React Internals proving tests.
//
// Proves the three genuine React framework_specific idioms (lazy code-split,
// suspense/error-boundary, portal) against the hand-written fixture
// testdata/react_internals/AppShell.tsx. Each assertion is the proving artifact
// for a coverage cell flipped in docs/coverage/registry.json["React Internals"]:
//   - lazy_code_splitting     → react_lazy + lazy_module on the lazy wrapper.
//   - suspense_error_boundary → react_suspense on the boundary component +
//                               react_error_boundary on the ErrorBoundary class.
//   - portal_recognition      → react_portal on the createPortal component.
//
// hooks / context_hoc are deliberately NOT asserted here — they are covered by
// the generic Structure group (#2854 hook_recognition, #611 context_extraction,
// HOC wrapper recognition) and marked not_applicable in the registry.
package javascript_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// extractTSXFixture parses and extracts a testdata .tsx file with the TSX grammar.
func extractTSXFixture(t *testing.T, relPath string) []types.EntityRecord {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", relPath))
	if err != nil {
		t.Fatalf("read fixture %s: %v", relPath, err)
	}
	tree := parseTSX(t, content)
	ext, _ := extreg.Get("typescript")
	ents, err := ext.Extract(context.Background(), extreg.FileInput{
		Path:     relPath,
		Content:  content,
		Language: "typescript",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("extract %s: %v", relPath, err)
	}
	return ents
}

func TestIssue2875_ReactInternals(t *testing.T) {
	ents := extractTSXFixture(t, "react_internals/AppShell.tsx")

	// lazy_code_splitting — the lazy() wrapper carries react_lazy + the
	// dynamic-import code-split target module.
	settings := findByName(ents, "SettingsPanel")
	if settings == nil {
		t.Fatalf("SettingsPanel (lazy wrapper) not extracted; names: %v", entityNames(ents))
	}
	if settings.Properties["react_lazy"] != "true" {
		t.Errorf("SettingsPanel: react_lazy=%q, want \"true\"; props=%v", settings.Properties["react_lazy"], settings.Properties)
	}
	if got := settings.Properties["lazy_module"]; got != "./SettingsPanel" {
		t.Errorf("SettingsPanel: lazy_module=%q, want \"./SettingsPanel\"", got)
	}

	// suspense_error_boundary — AppShell renders <Suspense>.
	appShell := findByName(ents, "AppShell")
	if appShell == nil {
		t.Fatalf("AppShell not extracted")
	}
	if appShell.Properties["react_suspense"] != "true" {
		t.Errorf("AppShell: react_suspense=%q, want \"true\"; props=%v", appShell.Properties["react_suspense"], appShell.Properties)
	}

	// suspense_error_boundary — ErrorBoundary class declares the contract.
	eb := findByName(ents, "ErrorBoundary")
	if eb == nil {
		t.Fatalf("ErrorBoundary class not extracted")
	}
	if eb.Properties["react_error_boundary"] != "true" {
		t.Errorf("ErrorBoundary: react_error_boundary=%q, want \"true\"; props=%v", eb.Properties["react_error_boundary"], eb.Properties)
	}

	// portal_recognition — Modal renders via createPortal.
	modal := findByName(ents, "Modal")
	if modal == nil {
		t.Fatalf("Modal not extracted")
	}
	if modal.Properties["react_portal"] != "true" {
		t.Errorf("Modal: react_portal=%q, want \"true\"; props=%v", modal.Properties["react_portal"], modal.Properties)
	}

	// Negative case — PlainCard must NOT pick up any React Internals markers.
	plain := findByName(ents, "PlainCard")
	if plain == nil {
		t.Fatalf("PlainCard not extracted")
	}
	for _, k := range []string{"react_lazy", "react_suspense", "react_portal", "react_error_boundary"} {
		if v, ok := plain.Properties[k]; ok && v != "" {
			t.Errorf("PlainCard: unexpected %s=%q (false positive)", k, v)
		}
	}
}
