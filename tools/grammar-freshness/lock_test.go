package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadLock_RealManifest parses the committed grammars.lock to guard against
// the manifest and the parser drifting apart.
func TestLoadLock_RealManifest(t *testing.T) {
	l, err := loadLock("../../grammars.lock")
	if err != nil {
		t.Fatalf("loadLock: %v", err)
	}
	if len(l.Grammars) != 27 {
		t.Errorf("grammar count = %d, want 27", len(l.Grammars))
	}
	for _, g := range l.Grammars {
		if g.Language == "" || g.Source == "" {
			t.Errorf("grammar entry missing language/source: %+v", g)
		}
	}
}

// TestValidateBinding_RejectsAModuleAbsentFromGoMod is the guard against
// reintroducing #6749: a lock that declares a bundling module the repo does not
// actually depend on is a lie, and the tool must refuse to run on it rather than
// quietly using its date for every row.
func TestValidateBinding_RejectsAModuleAbsentFromGoMod(t *testing.T) {
	dir := t.TempDir()
	gomod := filepath.Join(dir, "go.mod")
	writeFile(t, gomod, fixtureGoMod)

	err := validateBinding(Binding{
		Module:     "github.com/smacker/go-tree-sitter",
		Version:    "v0.0.0-20240827094217-dd81d9e9be82",
		PinnedDate: "2024-08-27",
	}, gomod)
	if err == nil {
		t.Fatal("a binding module absent from go.mod must be a hard error")
	}
	if !strings.Contains(err.Error(), "github.com/smacker/go-tree-sitter") {
		t.Errorf("error must name the offending module, got %v", err)
	}

	// A binding that IS a real dependency is fine.
	if err := validateBinding(Binding{Module: "github.com/tree-sitter/go-tree-sitter"}, gomod); err != nil {
		t.Errorf("a module present in go.mod must validate, got %v", err)
	}
	// No binding declared at all is the expected steady state.
	if err := validateBinding(Binding{}, gomod); err != nil {
		t.Errorf("an absent binding block must validate, got %v", err)
	}
}

// TestRealLock_DeclaresNoStaleBinding checks the committed manifest itself: the
// smacker binding block was the source of #6749 and must stay gone.
func TestRealLock_DeclaresNoStaleBinding(t *testing.T) {
	l, err := loadLock("../../grammars.lock")
	if err != nil {
		t.Fatalf("loadLock: %v", err)
	}
	if err := validateBinding(l.Binding, "../../go.mod"); err != nil {
		t.Errorf("the committed grammars.lock does not validate against go.mod: %v", err)
	}
}
