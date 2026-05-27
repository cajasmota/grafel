package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRegistry builds an in-memory Registry with one flat record and
// one grouped record so the mapping-validation tests can exercise both
// shape paths from a single fixture.
func fakeRegistry() *Registry {
	return &Registry{
		SchemaVersion: SchemaVersion,
		Records: []Record{
			{
				ID:       "lang.python.framework.flask",
				Category: "http_framework",
				Language: "python",
				Label:    "Flask",
				Capabilities: map[string]Capability{
					"endpoint_synthesis":  {Status: StatusFull},
					"handler_attribution": {Status: StatusFull},
				},
			},
			{
				ID:          "lang.jsts.framework.react",
				Category:    "http_framework",
				Subcategory: "ui_frontend",
				Language:    "jsts",
				Label:       "React",
				Groups: map[string]map[string]Capability{
					"Data Flow": {"state_management": {Status: StatusPartial}},
				},
			},
		},
	}
}

// writeFixtureFile materialises a single source file inside a temp tree
// so the symbol-existence check has something to find. Content is a
// minimal valid Go file containing the requested decl.
func writeFixtureFile(t *testing.T, root, rel, declLine string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "package fixture\n\n" + declLine + " {}\n"
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

// TestValidateCapabilityMap_HappyPath confirms an entry whose files and
// functions all exist produces zero errors and the expected counts.
func TestValidateCapabilityMap_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	writeFixtureFile(t, tmp, "internal/engine/flask.go", "func synthesizeFlask()")
	writeFixtureFile(t, tmp, "internal/extractors/javascript/zustand.go", "func emitStoreActionEntities()")

	cm := &CapabilityMap{Records: map[string]MapRecord{
		"lang.python.framework.flask": {
			Capabilities: map[string]MapEntry{
				"endpoint_synthesis": {
					Status:  StatusFull,
					Symbols: []MapSymbol{{File: "internal/engine/flask.go", Functions: []string{"synthesizeFlask"}}},
				},
				"handler_attribution": {
					Status:  StatusFull,
					Symbols: []MapSymbol{{File: "internal/engine/flask.go", Functions: []string{"synthesizeFlask"}}},
				},
			},
		},
		"lang.jsts.framework.react": {
			Groups: map[string]map[string]MapEntry{
				"Data Flow": {
					"state_management": {
						Status:  StatusPartial,
						Symbols: []MapSymbol{{File: "internal/extractors/javascript/zustand.go", Functions: []string{"emitStoreActionEntities"}}},
					},
				},
			},
		},
	}}

	res := validateCapabilityMap(cm, fakeRegistry(), tmp)
	if res.HasErrors() {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if res.RecordsChecked != 2 {
		t.Fatalf("RecordsChecked = %d", res.RecordsChecked)
	}
	if res.FunctionsChecked != 3 {
		t.Fatalf("FunctionsChecked = %d", res.FunctionsChecked)
	}
}

// TestValidateCapabilityMap_MissingFunction surfaces a rename-style
// drift: the file exists but the cited function is gone.
func TestValidateCapabilityMap_MissingFunction(t *testing.T) {
	tmp := t.TempDir()
	writeFixtureFile(t, tmp, "internal/engine/flask.go", "func somethingElse()")

	cm := &CapabilityMap{Records: map[string]MapRecord{
		"lang.python.framework.flask": {
			Capabilities: map[string]MapEntry{
				"endpoint_synthesis": {
					Status:  StatusFull,
					Symbols: []MapSymbol{{File: "internal/engine/flask.go", Functions: []string{"synthesizeFlask"}}},
				},
			},
		},
	}}

	res := validateCapabilityMap(cm, fakeRegistry(), tmp)
	if !res.HasErrors() {
		t.Fatal("expected error for missing function")
	}
	if !strings.Contains(strings.Join(res.Errors, "\n"), `function "synthesizeFlask" not found`) {
		t.Fatalf("error message did not mention missing function: %v", res.Errors)
	}
}

// TestValidateCapabilityMap_MissingFile catches the simpler drift mode
// where the source file itself was renamed or deleted.
func TestValidateCapabilityMap_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	cm := &CapabilityMap{Records: map[string]MapRecord{
		"lang.python.framework.flask": {
			Capabilities: map[string]MapEntry{
				"endpoint_synthesis": {
					Status:  StatusFull,
					Symbols: []MapSymbol{{File: "internal/engine/flask.go"}},
				},
			},
		},
	}}
	res := validateCapabilityMap(cm, fakeRegistry(), tmp)
	if !res.HasErrors() {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(strings.Join(res.Errors, "\n"), "not found on disk") {
		t.Fatalf("error: %v", res.Errors)
	}
}

// TestValidateCapabilityMap_UnknownRecord trips when the mapping
// references a record ID that has since been dropped from the registry.
func TestValidateCapabilityMap_UnknownRecord(t *testing.T) {
	tmp := t.TempDir()
	cm := &CapabilityMap{Records: map[string]MapRecord{
		"lang.does.not.exist": {
			Capabilities: map[string]MapEntry{"k": {Status: StatusFull}},
		},
	}}
	res := validateCapabilityMap(cm, fakeRegistry(), tmp)
	if !res.HasErrors() {
		t.Fatal("expected error for unknown record")
	}
	if !strings.Contains(strings.Join(res.Errors, "\n"), "no matching registry record") {
		t.Fatalf("error: %v", res.Errors)
	}
}

// TestValidateCapabilityMap_CoverageWarning ensures full/partial cells
// without a mapping entry emit a warning (not an error) so adding
// mapping coverage remains incremental.
func TestValidateCapabilityMap_CoverageWarning(t *testing.T) {
	tmp := t.TempDir()
	cm := &CapabilityMap{Records: map[string]MapRecord{}}
	res := validateCapabilityMap(cm, fakeRegistry(), tmp)
	if res.HasErrors() {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("expected coverage warnings for unmapped cells")
	}
	joined := strings.Join(res.Warnings, "\n")
	if !strings.Contains(joined, "has no mapping entry") {
		t.Fatalf("warnings: %v", res.Warnings)
	}
}

// TestValidateCapabilityMap_NilMap is a no-op — passing nil yields an
// empty result so callers can drop the map without changing the
// validate pipeline.
func TestValidateCapabilityMap_NilMap(t *testing.T) {
	res := validateCapabilityMap(nil, fakeRegistry(), t.TempDir())
	if res.HasErrors() || len(res.Warnings) != 0 {
		t.Fatalf("expected empty result, got errs=%v warns=%v", res.Errors, res.Warnings)
	}
}

// TestScanDeclarations covers the lightweight Go-source declaration
// scanner used by the symbol-existence check.
func TestScanDeclarations(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "src.go")
	body := `package x

func Top() {}
func (r *T) Method() {}
func Unexported() int { return 0 }

type Alias struct{}
type IfaceLike interface{ A() }

// func NotADecl
//  func indented
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	decls, err := scanDeclarations(path)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, want := range []string{"Top", "Method", "Unexported", "Alias", "IfaceLike"} {
		if !decls[want] {
			t.Errorf("expected decl %q in %v", want, decls)
		}
	}
	for _, denied := range []string{"NotADecl", "indented"} {
		if decls[denied] {
			t.Errorf("did not expect %q in decls", denied)
		}
	}
}
