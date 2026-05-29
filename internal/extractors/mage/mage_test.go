package mage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func TestIsMagefile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"magefile.go", true},
		{"Magefile.go", true},
		{"sub/magefile.go", true},
		{"magefiles/build.go", true},
		{"magefiles/ci/lint.go", true},
		{"build/magefiles/x.go", true},
		{"main.go", false},
		{"magefile.py", false},
		{"magefile_test.go", false},
		{"magefiles/build_test.go", false},
		{"internal/foo.go", false},
	}
	for _, c := range cases {
		if got := IsMagefile(c.path); got != c.want {
			t.Errorf("IsMagefile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestParseMagefile_TargetsAndDeps(t *testing.T) {
	src := []byte(`//go:build mage
// +build mage

package main

import (
	"context"

	"github.com/magefile/mage/mg"
)

// Build compiles the binary.
func Build() error {
	mg.Deps(Lint, Generate)
	return nil
}

// Test runs the test suite, after Build.
func Test(ctx context.Context) error {
	mg.CtxDeps(ctx, Build)
	mg.SerialDeps(Clean)
	return nil
}

func Lint()      {}
func Generate()  {}
func Clean()     {}

// helper is unexported — not a target.
func helper() {}

// BadSig has a non-target signature (returns string) — not a target.
func BadSig() string { return "" }

// TooManyParams — not a target.
func TooManyParams(a, b int) {}
`)
	targets, ok := ParseMagefile(src)
	if !ok {
		t.Fatal("ParseMagefile returned ok=false for a mage-tagged file")
	}
	got := map[string][]string{}
	for _, tg := range targets {
		got[tg.Name] = tg.Deps
	}
	wantTargets := []string{"Build", "Test", "Lint", "Generate", "Clean"}
	for _, w := range wantTargets {
		if _, ok := got[w]; !ok {
			t.Errorf("missing target %q; got %v", w, got)
		}
	}
	if _, bad := got["helper"]; bad {
		t.Error("unexported helper should not be a target")
	}
	if _, bad := got["BadSig"]; bad {
		t.Error("BadSig (string result) should not be a target")
	}
	if _, bad := got["TooManyParams"]; bad {
		t.Error("TooManyParams should not be a target")
	}
	// Build deps.
	if deps := got["Build"]; len(deps) != 2 || deps[0] != "Lint" || deps[1] != "Generate" {
		t.Errorf("Build deps = %v, want [Lint Generate]", deps)
	}
	// Test deps: CtxDeps(ctx, Build) drops ctx → Build; SerialDeps(Clean).
	depSet := map[string]bool{}
	for _, d := range got["Test"] {
		depSet[d] = true
	}
	if !depSet["Build"] || !depSet["Clean"] {
		t.Errorf("Test deps = %v, want to include Build and Clean (not ctx)", got["Test"])
	}
	if depSet["ctx"] {
		t.Error("ctx should not be recorded as a Test dependency")
	}
}

func TestParseMagefile_NoMageTag(t *testing.T) {
	src := []byte(`package main

func Build() {}
`)
	if _, ok := ParseMagefile(src); ok {
		t.Error("ParseMagefile should return ok=false for a file without the mage build tag")
	}
}

func TestParseMagefile_MgFWrapper(t *testing.T) {
	src := []byte(`//go:build mage

package main

import "github.com/magefile/mage/mg"

func Release() {
	mg.Deps(mg.F(Build, "linux"))
}

func Build() {}
`)
	targets, ok := ParseMagefile(src)
	if !ok {
		t.Fatal("expected ok")
	}
	for _, tg := range targets {
		if tg.Name == "Release" {
			if len(tg.Deps) != 1 || tg.Deps[0] != "Build" {
				t.Errorf("Release deps = %v, want [Build] (unwrapped from mg.F)", tg.Deps)
			}
			return
		}
	}
	t.Fatal("Release target not found")
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "magefile.go", `//go:build mage

package main

import "github.com/magefile/mage/mg"

func Build() error {
	mg.Deps(Lint)
	return nil
}

func Lint() {}
`)
	// A magefiles/ directory layout file.
	writeFile(t, root, "magefiles/ci.go", `//go:build mage

package main

import "github.com/magefile/mage/mg"

func CI() {
	mg.SerialDeps(Build)
}
`)
	// A non-mage Go file with a matching basename-ish path — must be ignored.
	writeFile(t, root, "main.go", `package main

func main() {}
`)

	ents, rels, err := Discover(context.Background(), root, []string{"magefile.go", "magefiles/ci.go", "main.go"})
	if err != nil {
		t.Fatal(err)
	}

	var magefiles, targets int
	idByName := map[string]string{}
	for _, e := range ents {
		switch e.Subtype {
		case "mage_magefile":
			magefiles++
			if e.Kind != string(types.EntityKindComponent) {
				t.Errorf("magefile kind = %q, want SCOPE.Component", e.Kind)
			}
		case "mage_target":
			targets++
			idByName[e.Name] = e.ID
			if e.Kind != string(types.EntityKindOperation) {
				t.Errorf("target kind = %q, want SCOPE.Operation", e.Kind)
			}
			if e.Language != "go" {
				t.Errorf("target language = %q, want go", e.Language)
			}
		}
	}
	if magefiles != 2 {
		t.Errorf("magefiles = %d, want 2", magefiles)
	}
	// Build, Lint, CI = 3 targets.
	if targets != 3 {
		t.Errorf("targets = %d, want 3", targets)
	}

	// Edges: Build→Lint and CI→Build.
	var buildLint, ciBuild bool
	for _, r := range rels {
		if r.Kind != RelationshipKindMageDependsOn {
			t.Errorf("unexpected rel kind %q", r.Kind)
		}
		switch {
		case r.FromID == idByName["Build"] && r.ToID == idByName["Lint"]:
			buildLint = true
		case r.FromID == idByName["CI"] && r.ToID == idByName["Build"]:
			ciBuild = true
		}
	}
	if !buildLint {
		t.Error("missing Build → Lint MAGE_DEPENDS_ON edge")
	}
	if !ciBuild {
		t.Error("missing CI → Build MAGE_DEPENDS_ON edge")
	}

	// Determinism: a second run yields identical IDs.
	ents2, _, _ := Discover(context.Background(), root, []string{"magefile.go", "magefiles/ci.go", "main.go"})
	if len(ents2) != len(ents) {
		t.Fatalf("non-deterministic entity count: %d vs %d", len(ents2), len(ents))
	}
	for i := range ents {
		if ents[i].ID != ents2[i].ID {
			t.Errorf("non-deterministic ID at %d: %q vs %q", i, ents[i].ID, ents2[i].ID)
		}
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
