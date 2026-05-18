package links

import (
	"path/filepath"
	"testing"
)

// TestImportPass_SkipsBareNameExt verifies the issue #509 fix:
// two repos that each independently reference `ext:filter` (a bare-name
// external placeholder for a built-in like `[].filter()`) must NOT
// produce a cross-repo link. Bare-name ext:* placeholders are each
// repo's own unresolved use of a built-in, not a shared symbol.
func TestImportPass_SkipsBareNameExt(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "alpha",
		Entities: []map[string]any{
			{"id": "a_local", "name": "doStuff", "kind": "function", "source_file": "src/a.js"},
			{"id": "ext:filter", "name": "filter", "kind": "External", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "a_local", "to_id": "ext:filter", "kind": "calls"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "beta",
		Entities: []map[string]any{
			{"id": "b_local", "name": "doMore", "kind": "function", "source_file": "src/b.js"},
			{"id": "ext:filter", "name": "filter", "kind": "External", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "b_local", "to_id": "ext:filter", "kind": "calls"},
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g509", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g509-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodImport {
			t.Errorf("expected zero import-method links for bare-name ext:filter, got %+v", l)
		}
	}
}

// TestImportPass_EmitsQualifiedExt verifies the converse: qualified
// "ext:<module>:<name>" placeholders (e.g. `ext:react:useState`) DO
// carry real module identity and remain eligible for cross-repo
// linking when two repos point at the same qualified ID.
func TestImportPass_EmitsQualifiedExt(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "alpha",
		Entities: []map[string]any{
			{"id": "a_comp", "name": "Component", "kind": "function", "source_file": "src/a.tsx"},
			{"id": "ext:react:useState", "name": "useState", "kind": "External", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "a_comp", "to_id": "ext:react:useState", "kind": "calls"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "beta",
		Entities: []map[string]any{
			{"id": "b_comp", "name": "Other", "kind": "function", "source_file": "src/b.tsx"},
			{"id": "ext:react:useState", "name": "useState", "kind": "External", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "b_comp", "to_id": "ext:react:useState", "kind": "calls"},
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g509q", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g509q-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var count int
	for _, l := range doc.Links {
		if l.Method == MethodImport {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 import-method link for ext:react:useState, got %d (%+v)", count, doc.Links)
	}
}

func TestIsBareNameExt(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"ext:filter", true},
		{"ext:split", true},
		{"ext:", true}, // pathological — treat as bare
		{"ext:react:useState", false},
		{"ext:django:models.Model", false},
		{"ext:click.command", true}, // dot-qualified but no second colon — still bare per spec
		{"a_local", false},
		{"", false},
		{"react:useState", false}, // no ext: prefix
	}
	for _, c := range cases {
		if got := isBareNameExt(c.in); got != c.want {
			t.Errorf("isBareNameExt(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
