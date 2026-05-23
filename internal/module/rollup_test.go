package module_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/module"
)

// ─── Derive ──────────────────────────────────────────────────────────────────

func TestDerive_RootFiles(t *testing.T) {
	for _, sf := range []string{
		"main.go",
		"setup.py",
		"index.js",
		"README.md",
		"./main.go",
	} {
		got := module.Derive(sf, nil)
		if got != module.RootLabel {
			t.Errorf("Derive(%q, nil) = %q, want %q", sf, got, module.RootLabel)
		}
	}
}

func TestDerive_DefaultDepthFallback(t *testing.T) {
	// No markers: should take first 2 segments.
	cases := []struct {
		sf   string
		want string
	}{
		{"src/features/auth/login.ts", "src/features"},
		{"src/features/auth/deep/nested/file.ts", "src/features"},
		{"core/views/accounts.py", "core/views"},
		{"core/models/user.py", "core/models"},
		{"internal/graph/graph.go", "internal/graph"},
		{"cmd/archigraph/index.go", "cmd/archigraph"},
		{"a/b/c/d/e/f.go", "a/b"},
		// Single-segment directory
		{"pkg/foo.go", "pkg"},
	}
	for _, tc := range cases {
		got := module.Derive(tc.sf, nil)
		if got != tc.want {
			t.Errorf("Derive(%q, nil) = %q, want %q", tc.sf, got, tc.want)
		}
	}
}

func TestDerive_MarkerBoundaryPython(t *testing.T) {
	// core/views has __init__.py → boundary at depth 2
	ms := module.BuildMarkerSet([]string{
		"core/__init__.py",
		"core/views/__init__.py",
		"core/models/__init__.py",
	})

	cases := []struct {
		sf   string
		want string
	}{
		{"core/views/accounts.py", "core/views"},
		{"core/views/deep/nested.py", "core/views"}, // capped at marker depth 2
		{"core/models/user.py", "core/models"},
		{"core/utils.py", "core"},              // file in 'core' dir with __init__.py
		{"other/stuff/file.py", "other/stuff"}, // no marker → DefaultDepth = 2 → two segments
	}
	for _, tc := range cases {
		got := module.Derive(tc.sf, ms)
		if got != tc.want {
			t.Errorf("Derive(%q, ms) = %q, want %q", tc.sf, got, tc.want)
		}
	}
}

func TestDerive_MarkerBoundaryJS(t *testing.T) {
	ms := module.BuildMarkerSet([]string{
		"src/features/auth/package.json",
		"src/features/dashboard/index.ts",
	})

	cases := []struct {
		sf   string
		want string
	}{
		{"src/features/auth/login.ts", "src/features/auth"},
		{"src/features/auth/components/Button.tsx", "src/features/auth"}, // MaxDepth cap
		{"src/features/dashboard/App.tsx", "src/features/dashboard"},
		{"src/shared/utils.ts", "src/shared"}, // no marker → DefaultDepth
	}
	for _, tc := range cases {
		got := module.Derive(tc.sf, ms)
		if got != tc.want {
			t.Errorf("Derive(%q, ms) = %q, want %q", tc.sf, got, tc.want)
		}
	}
}

func TestDerive_MarkerBoundaryGo(t *testing.T) {
	ms := module.BuildMarkerSet([]string{
		"go.mod",
		"internal/graph/go.mod", // hypothetical nested module
	})

	cases := []struct {
		sf   string
		want string
	}{
		// internal/graph has a nested go.mod → boundary at depth 2
		{"internal/graph/graph.go", "internal/graph"},
		{"internal/graph/coverage.go", "internal/graph"},
		// no nested go.mod deeper than 2 → DefaultDepth
		{"internal/extractor/extractor.go", "internal/extractor"},
		{"cmd/archigraph/index.go", "cmd/archigraph"},
	}
	for _, tc := range cases {
		got := module.Derive(tc.sf, ms)
		if got != tc.want {
			t.Errorf("Derive(%q, ms) = %q, want %q", tc.sf, got, tc.want)
		}
	}
}

func TestDerive_MaxDepthCap(t *testing.T) {
	// Marker at depth 3 — MaxDepth=3 so boundary is used exactly.
	ms := module.BuildMarkerSet([]string{
		"a/b/c/__init__.py",
	})
	got := module.Derive("a/b/c/d/e/file.py", ms)
	// Marker is at depth 3, which equals MaxDepth → label is "a/b/c".
	want := "a/b/c"
	if got != want {
		t.Errorf("Derive cap at MaxDepth=3: got %q, want %q", got, want)
	}

	// Marker at depth 4 is beyond MaxDepth=3 → scan never sees it → DefaultDepth=2.
	ms2 := module.BuildMarkerSet([]string{
		"a/b/c/d/__init__.py",
	})
	got2 := module.Derive("a/b/c/d/e/file.py", ms2)
	want2 := "a/b" // marker beyond MaxDepth → falls back to DefaultDepth
	if got2 != want2 {
		t.Errorf("Derive beyond MaxDepth: got %q, want %q", got2, want2)
	}
}

func TestDerive_SingleSegmentDirectory(t *testing.T) {
	// File is one level deep — label is just that segment.
	got := module.Derive("pkg/foo.go", nil)
	if got != "pkg" {
		t.Errorf("got %q, want %q", got, "pkg")
	}
}

func TestDerive_Deterministic(t *testing.T) {
	// Same input must always produce the same output (no randomness).
	ms := module.BuildMarkerSet([]string{"core/__init__.py"})
	for i := 0; i < 100; i++ {
		a := module.Derive("core/views/accounts.py", ms)
		b := module.Derive("core/views/accounts.py", ms)
		if a != b {
			t.Fatalf("non-deterministic: %q vs %q", a, b)
		}
	}
}

func TestDerive_WindowsBackslash(t *testing.T) {
	got := module.Derive("src\\features\\auth\\login.ts", nil)
	want := "src/features"
	if got != want {
		t.Errorf("backslash normalisation: got %q, want %q", got, want)
	}
}

func TestDerive_LeadingDotSlash(t *testing.T) {
	got := module.Derive("./src/features/login.ts", nil)
	want := "src/features"
	if got != want {
		t.Errorf("leading ./ strip: got %q, want %q", got, want)
	}
}

func TestDerive_EmptyAndDotOnly(t *testing.T) {
	for _, sf := range []string{"", ".", "./"} {
		got := module.Derive(sf, nil)
		if got != module.RootLabel {
			t.Errorf("Derive(%q) = %q, want %q", sf, got, module.RootLabel)
		}
	}
}

// ─── BuildMarkerSet ───────────────────────────────────────────────────────────

func TestBuildMarkerSet_Empty(t *testing.T) {
	ms := module.BuildMarkerSet(nil)
	if len(ms) != 0 {
		t.Fatalf("expected empty MarkerSet, got %d entries", len(ms))
	}
}

func TestBuildMarkerSet_IgnoresNonMarkers(t *testing.T) {
	ms := module.BuildMarkerSet([]string{
		"core/views/accounts.py",
		"core/models/user.py",
		"README.md",
	})
	if len(ms) != 0 {
		t.Fatalf("expected empty MarkerSet, got %v", ms)
	}
}

func TestBuildMarkerSet_RootMarker(t *testing.T) {
	ms := module.BuildMarkerSet([]string{
		"package.json",
		"go.mod",
	})
	// Root is stored as ""
	if _, ok := ms[""]; !ok {
		t.Error("expected root marker (empty string key) to be present")
	}
}

func TestBuildMarkerSet_CorrectDirs(t *testing.T) {
	ms := module.BuildMarkerSet([]string{
		"core/__init__.py",
		"core/views/__init__.py",
		"src/features/auth/package.json",
	})
	want := []string{"core", "core/views", "src/features/auth"}
	for _, w := range want {
		if _, ok := ms[w]; !ok {
			t.Errorf("expected marker at dir %q, not found; markers: %v", w, ms)
		}
	}
	if len(ms) != len(want) {
		t.Errorf("expected %d markers, got %d: %v", len(want), len(ms), ms)
	}
}

// ─── EnsureModule ─────────────────────────────────────────────────────────────

func TestEnsureModule_NilProps(t *testing.T) {
	got := module.EnsureModule(nil, "core/views/accounts.py", nil)
	if got == nil {
		t.Fatal("expected non-nil map")
	}
	if got["module"] == "" {
		t.Error("expected module key to be set")
	}
}

func TestEnsureModule_AlreadySet(t *testing.T) {
	props := map[string]string{"module": "custom/label"}
	got := module.EnsureModule(props, "core/views/accounts.py", nil)
	if got["module"] != "custom/label" {
		t.Errorf("expected existing value preserved, got %q", got["module"])
	}
}

func TestEnsureModule_SetsWhenAbsent(t *testing.T) {
	props := map[string]string{"language": "python"}
	got := module.EnsureModule(props, "core/views/accounts.py", nil)
	if got["language"] != "python" {
		t.Error("expected language key preserved")
	}
	if got["module"] == "" {
		t.Error("expected module key to be set")
	}
	if got["module"] != "core/views" {
		t.Errorf("got %q, want %q", got["module"], "core/views")
	}
}

// ─── Coverage: every entity gets a non-empty module ───────────────────────────

func TestDerive_NeverEmpty(t *testing.T) {
	paths := []string{
		"main.go",
		"setup.py",
		"src/index.ts",
		"src/features/auth/login.ts",
		"src/features/auth/components/Button.tsx",
		"internal/graph/coverage.go",
		"cmd/archigraph/index.go",
		"a/b/c/d/e/f/g.go",
	}
	ms := module.BuildMarkerSet([]string{
		"src/features/auth/package.json",
		"internal/graph/__init__.py", // unusual but valid test
	})
	for _, p := range paths {
		got := module.Derive(p, ms)
		if got == "" {
			t.Errorf("Derive(%q) returned empty string, want non-empty label", p)
		}
	}
}
