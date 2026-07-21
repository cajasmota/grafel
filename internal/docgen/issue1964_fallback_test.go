package docgen

// Issue #1964 — when extractors emit start_line=0 or end_line=0 the bundle
// helper must fall back to a by-name lookup in the source file rather than
// returning an empty source_window. These tests cover the standalone
// findEntityLinesByName helper that powers that fallback.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func writeTempSource(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestFindEntityLinesByName_PythonMethod(t *testing.T) {
	src := "" +
		"class FixtureViewSet:\n" + // 1
		"    queryset = []\n" + // 2
		"\n" + // 3
		"    def assign_contacts(self, request):\n" + // 4
		"        # block 1\n" + // 5
		"        if request is None:\n" + // 6
		"            return None\n" + // 7
		"        return {'ok': True}\n" + // 8
		"\n" + // 9
		"    def other_action(self):\n" + // 10
		"        return 1\n" // 11
	path := writeTempSource(t, "views.py", src)

	e := &graph.Entity{
		Name:    "FixtureViewSet.assign_contacts",
		Kind:    "SCOPE.Operation",
		Subtype: "method",
	}
	start, end, ok := findEntityLinesByName(path, graph.EntityViewOf(e))
	if !ok {
		t.Fatalf("expected fallback to find the method")
	}
	if start != 4 {
		t.Errorf("start: got %d, want 4", start)
	}
	if end != 8 {
		t.Errorf("end: got %d, want 8 (last body line before next def)", end)
	}
}

func TestFindEntityLinesByName_PythonClass(t *testing.T) {
	src := "" +
		"# header\n" + // 1
		"\n" + // 2
		"class FixtureModel:\n" + // 3
		"    a = 1\n" + // 4
		"    b = 2\n" + // 5
		"    c = 3\n" + // 6
		"\n" + // 7
		"\n" + // 8
		"def trailing():\n" + // 9
		"    return 0\n" // 10
	path := writeTempSource(t, "models.py", src)

	e := &graph.Entity{
		Name:    "FixtureModel",
		Kind:    "SCOPE.Component",
		Subtype: "class",
	}
	start, end, ok := findEntityLinesByName(path, graph.EntityViewOf(e))
	if !ok {
		t.Fatalf("expected fallback to find the class")
	}
	if start != 3 {
		t.Errorf("start: got %d, want 3", start)
	}
	// Body ends on line 6 (the last non-blank line whose indent > class indent).
	if end != 6 {
		t.Errorf("end: got %d, want 6", end)
	}
}

func TestFindEntityLinesByName_JSXFunctionComponent(t *testing.T) {
	src := "" +
		"import React from 'react';\n" + // 1
		"\n" + // 2
		"export function FixtureComponentA(props) {\n" + // 3
		"  if (!props.label) {\n" + // 4
		"    return null;\n" + // 5
		"  }\n" + // 6
		"  return <div>{props.label}</div>;\n" + // 7
		"}\n" + // 8
		"\n" + // 9
		"export default FixtureComponentA;\n" // 10
	path := writeTempSource(t, "ComponentA.jsx", src)

	e := &graph.Entity{
		Name:    "FixtureComponentA",
		Kind:    "SCOPE.Operation",
		Subtype: "react_component",
	}
	start, end, ok := findEntityLinesByName(path, graph.EntityViewOf(e))
	if !ok {
		t.Fatalf("expected fallback to find the component")
	}
	if start != 3 {
		t.Errorf("start: got %d, want 3", start)
	}
	if end != 8 {
		t.Errorf("end: got %d, want 8 (closing brace)", end)
	}
}

func TestFindEntityLinesByName_MissingFile(t *testing.T) {
	e := &graph.Entity{Name: "Whatever", Kind: "SCOPE.Operation"}
	_, _, ok := findEntityLinesByName(filepath.Join(t.TempDir(), "does-not-exist.py"), graph.EntityViewOf(e))
	if ok {
		t.Fatalf("expected ok=false for missing file")
	}
}

func TestFindEntityLinesByName_EmptyName(t *testing.T) {
	path := writeTempSource(t, "blank.py", "# nothing\n")
	_, _, ok := findEntityLinesByName(path, graph.EntityViewOf(&graph.Entity{Name: "", Kind: "SCOPE.Operation"}))
	if ok {
		t.Fatalf("expected ok=false for empty name")
	}
}
