// platform_variants_test.go — tests for #713 React Native platform-specific
// file variant detection and test-file TESTS edge emission.
package javascript_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Helper: find relationship edges by kind on a named entity.
// ---------------------------------------------------------------------------

func relEdgesOfKind(ents []types.EntityRecord, fromName, kind string) []string {
	src := findByNameRel(ents, fromName)
	if src == nil {
		return nil
	}
	var out []string
	for _, r := range src.Relationships {
		if r.Kind == kind {
			out = append(out, r.ToID)
		}
	}
	return out
}

// fileEntWithPath returns the file-level SCOPE.Component entity (subtype
// "file") for the given source path, or nil if not found.
func fileEntWithPath(ents []types.EntityRecord, filePath string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Subtype == "file" && ents[i].SourceFile == filePath {
			return &ents[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// isPlatformVariantFile unit tests
// ---------------------------------------------------------------------------

// TestIsPlatformVariantFile_IOS verifies .ios.tsx is a platform variant.
func TestIsPlatformVariantFile_IOS(t *testing.T) {
	// We test via the extractor output (no exported function) by running a
	// minimal file through the extractor and checking for PLATFORM_VARIANT_OF.
	// The extractor emits unconditionally when repoRoot is empty (unit-test mode).
	src := `export default function IconSymbol() { return null; }`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "components/ui/icon-symbol.ios.tsx")

	// File entity should have PLATFORM_VARIANT_OF pointing to the canonical.
	fileEnt := fileEntWithPath(ents, "components/ui/icon-symbol.ios.tsx")
	if fileEnt == nil {
		t.Fatal("file entity not found")
	}
	edges := relEdgesOfKind(ents, fileEnt.Name, "PLATFORM_VARIANT_OF")
	if len(edges) == 0 {
		t.Errorf("expected PLATFORM_VARIANT_OF edge; got none (relationships: %v)", fileEnt.Relationships)
	}
	// The canonical should be the .tsx without the .ios suffix.
	wantCanonical := "components/ui/icon-symbol.tsx"
	found := false
	for _, e := range edges {
		if e == wantCanonical {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("PLATFORM_VARIANT_OF should point to %q; got %v", wantCanonical, edges)
	}
}

// TestIsPlatformVariantFile_Android verifies .android.tsx is a platform variant.
func TestIsPlatformVariantFile_Android(t *testing.T) {
	src := `export default function Button() { return null; }`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "components/Button.android.tsx")

	fileEnt := fileEntWithPath(ents, "components/Button.android.tsx")
	if fileEnt == nil {
		t.Fatal("file entity not found")
	}
	edges := relEdgesOfKind(ents, fileEnt.Name, "PLATFORM_VARIANT_OF")
	if len(edges) == 0 {
		t.Errorf("expected PLATFORM_VARIANT_OF edge for .android.tsx; got none")
	}
	wantCanonical := "components/Button.tsx"
	found := false
	for _, e := range edges {
		if e == wantCanonical {
			found = true
		}
	}
	if !found {
		t.Errorf("PLATFORM_VARIANT_OF should point to %q; got %v", wantCanonical, edges)
	}
}

// TestIsPlatformVariantFile_Tablet verifies .tablet.tsx is a platform variant.
func TestIsPlatformVariantFile_Tablet(t *testing.T) {
	src := `export default function DeficiencyEditTablet() { return null; }`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree,
		"src/features/deficiencyEdit/DeficiencyEdit.tablet.tsx")

	fileEnt := fileEntWithPath(ents, "src/features/deficiencyEdit/DeficiencyEdit.tablet.tsx")
	if fileEnt == nil {
		t.Fatal("file entity not found")
	}
	edges := relEdgesOfKind(ents, fileEnt.Name, "PLATFORM_VARIANT_OF")
	if len(edges) == 0 {
		t.Errorf("expected PLATFORM_VARIANT_OF edge for .tablet.tsx; got none")
	}
	wantCanonical := "src/features/deficiencyEdit/DeficiencyEdit.tsx"
	found := false
	for _, e := range edges {
		if e == wantCanonical {
			found = true
		}
	}
	if !found {
		t.Errorf("expected canonical %q; got %v", wantCanonical, edges)
	}
}

// TestIsPlatformVariantFile_Landscape verifies .landscape.tsx is a variant.
func TestIsPlatformVariantFile_Landscape(t *testing.T) {
	src := `export default function DeficiencyEditLandscape() { return null; }`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree,
		"src/features/deficiencyEdit/DeficiencyEdit.landscape.tsx")

	fileEnt := fileEntWithPath(ents, "src/features/deficiencyEdit/DeficiencyEdit.landscape.tsx")
	if fileEnt == nil {
		t.Fatal("file entity not found")
	}
	edges := relEdgesOfKind(ents, fileEnt.Name, "PLATFORM_VARIANT_OF")
	if len(edges) == 0 {
		t.Errorf("expected PLATFORM_VARIANT_OF for .landscape.tsx; got none")
	}
}

// TestIsPlatformVariantFile_CompoundSuffix verifies `.mobile.landscape.tsx`
// (two platform suffixes) resolves to the canonical `.tsx`.
func TestIsPlatformVariantFile_CompoundSuffix(t *testing.T) {
	src := `export default function InspectionMobileLandscape() { return null; }`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree,
		"src/features/InspectionDeficiencies.mobile.landscape.tsx")

	fileEnt := fileEntWithPath(ents, "src/features/InspectionDeficiencies.mobile.landscape.tsx")
	if fileEnt == nil {
		t.Fatal("file entity not found")
	}
	edges := relEdgesOfKind(ents, fileEnt.Name, "PLATFORM_VARIANT_OF")
	if len(edges) == 0 {
		t.Errorf("expected PLATFORM_VARIANT_OF for .mobile.landscape.tsx; got none")
	}
	// Canonical should be the double-stripped name.
	for _, e := range edges {
		if strings.Contains(e, "mobile") || strings.Contains(e, "landscape") {
			t.Errorf("canonical path should not contain platform suffix: %q", e)
		}
	}
}

// TestIsPlatformVariantFile_NoEdgeForNonVariant verifies that a plain .tsx
// file (no platform suffix) does NOT receive a PLATFORM_VARIANT_OF edge.
func TestIsPlatformVariantFile_NoEdgeForNonVariant(t *testing.T) {
	src := `export default function Button() { return null; }`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "components/Button.tsx")

	fileEnt := fileEntWithPath(ents, "components/Button.tsx")
	if fileEnt == nil {
		t.Fatal("file entity not found")
	}
	edges := relEdgesOfKind(ents, fileEnt.Name, "PLATFORM_VARIANT_OF")
	if len(edges) != 0 {
		t.Errorf("non-variant file should have no PLATFORM_VARIANT_OF edge; got %v", edges)
	}
}

// TestIsPlatformVariantFile_PlatformSpecificTSFile verifies .ios.ts (not .tsx)
// also receives the PLATFORM_VARIANT_OF edge.
func TestIsPlatformVariantFile_PlatformSpecificTSFile(t *testing.T) {
	src := `export function useIOSHook() { return {}; }`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "hooks/usePlatform.ios.ts")

	fileEnt := fileEntWithPath(ents, "hooks/usePlatform.ios.ts")
	if fileEnt == nil {
		t.Fatal("file entity not found")
	}
	edges := relEdgesOfKind(ents, fileEnt.Name, "PLATFORM_VARIANT_OF")
	if len(edges) == 0 {
		t.Errorf("expected PLATFORM_VARIANT_OF for .ios.ts; got none")
	}
	wantCanonical := "hooks/usePlatform.ts"
	found := false
	for _, e := range edges {
		if e == wantCanonical {
			found = true
		}
	}
	if !found {
		t.Errorf("canonical should be %q; got %v", wantCanonical, edges)
	}
}

// ---------------------------------------------------------------------------
// Test-file TESTS edge (#713 beyond-minimum)
// ---------------------------------------------------------------------------

// TestTestFile_TestsEdge verifies that a *.test.tsx file emits a TESTS
// edge to its source .tsx peer.
func TestTestFile_TestsEdge(t *testing.T) {
	src := `import { render } from "@testing-library/react";
import Button from "./Button";
test("renders", () => { render(<Button />); });
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "components/Button.test.tsx")

	fileEnt := fileEntWithPath(ents, "components/Button.test.tsx")
	if fileEnt == nil {
		t.Fatal("file entity not found")
	}
	edges := relEdgesOfKind(ents, fileEnt.Name, "TESTS")
	if len(edges) == 0 {
		t.Errorf("expected TESTS edge for .test.tsx; got none")
	}
	wantSource := "components/Button.tsx"
	found := false
	for _, e := range edges {
		if e == wantSource {
			found = true
		}
	}
	if !found {
		t.Errorf("TESTS should point to %q; got %v", wantSource, edges)
	}
}

// TestTestFile_SpecEdge verifies *.spec.ts also emits a TESTS edge.
func TestTestFile_SpecEdge(t *testing.T) {
	src := `import { myFn } from "./utils";
describe("myFn", () => { it("works", () => {}); });
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/utils.spec.ts")

	fileEnt := fileEntWithPath(ents, "src/utils.spec.ts")
	if fileEnt == nil {
		t.Fatal("file entity not found")
	}
	edges := relEdgesOfKind(ents, fileEnt.Name, "TESTS")
	if len(edges) == 0 {
		t.Errorf("expected TESTS edge for .spec.ts; got none")
	}
	wantSource := "src/utils.ts"
	found := false
	for _, e := range edges {
		if e == wantSource {
			found = true
		}
	}
	if !found {
		t.Errorf("TESTS should point to %q; got %v", wantSource, edges)
	}
}

// TestTestFile_NoEdgeForNonTest verifies that a plain .tsx file does NOT
// get a TESTS edge (no false positives).
func TestTestFile_NoEdgeForNonTest(t *testing.T) {
	src := `export function add(a, b) { return a + b; }`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/math.ts")

	fileEnt := fileEntWithPath(ents, "src/math.ts")
	if fileEnt == nil {
		t.Fatal("file entity not found")
	}
	edges := relEdgesOfKind(ents, fileEnt.Name, "TESTS")
	if len(edges) != 0 {
		t.Errorf("non-test file should have no TESTS edge; got %v", edges)
	}
}

// TestPlatformVariant_NoPlatformVariantEdgeOnCanonical checks that a file
// like `Button.tsx` (the canonical itself) does NOT receive PLATFORM_VARIANT_OF
// even if there might be .ios.tsx / .android.tsx variants (those emit the edges,
// not the canonical).
func TestPlatformVariant_NoPlatformVariantEdgeOnCanonical(t *testing.T) {
	src := `export default function Button() { return null; }`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "components/Button.tsx")

	fileEnt := fileEntWithPath(ents, "components/Button.tsx")
	if fileEnt == nil {
		t.Fatal("file entity not found")
	}
	for _, r := range fileEnt.Relationships {
		if r.Kind == "PLATFORM_VARIANT_OF" {
			t.Errorf("canonical file should not have PLATFORM_VARIANT_OF; got %+v", r)
		}
	}
}
