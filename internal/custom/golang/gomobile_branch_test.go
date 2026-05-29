package golang_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/golang"
)

// Tests for the gomobile Data Flow.branch_conditions surface (#3255): platform
// branches expressed via `if runtime.GOOS == ...` and `switch runtime.GOOS`.

// extractBranch returns the raw EntityRecords (including Relationships, which
// the lighter fullEntity view drops) for the named extractor + fixture.
func extractBranch(t *testing.T, fixture string) []types.EntityRecord {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixture, err)
	}
	e, ok := extreg.Get("custom_go_gomobile")
	if !ok {
		t.Fatal("extractor custom_go_gomobile not registered")
	}
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path: filepath.Join("testdata", fixture), Language: "go", Content: content,
	})
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return ents
}

func branchByName(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Operation" && ents[i].Subtype == "branch" && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func TestGomobileBranchIfConditions(t *testing.T) {
	ents := extractBranch(t, "gomobile_branch.go")

	cases := []struct {
		name, platform, operator string
	}{
		{`gomobile:branch:runtime.GOOS=="android"`, "android", "=="},
		{`gomobile:branch:runtime.GOOS=="ios"`, "ios", "=="},
	}
	for _, c := range cases {
		e := branchByName(ents, c.name)
		if e == nil {
			t.Fatalf("expected branch %q; got %+v", c.name, ents)
		}
		if e.Properties["branch_kind"] != "if" {
			t.Errorf("%s: branch_kind=%q want if", c.name, e.Properties["branch_kind"])
		}
		if e.Properties["platform"] != c.platform {
			t.Errorf("%s: platform=%q want %q", c.name, e.Properties["platform"], c.platform)
		}
		if e.Properties["operator"] != c.operator {
			t.Errorf("%s: operator=%q want %q", c.name, e.Properties["operator"], c.operator)
		}
		if e.Properties["framework"] != "gomobile" {
			t.Errorf("%s: framework=%q", c.name, e.Properties["framework"])
		}
		// BRANCHES_ON edge present
		var found bool
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindBranchesOn) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: missing BRANCHES_ON edge: %+v", c.name, e.Relationships)
		}
	}
}

func TestGomobileBranchSwitchConditions(t *testing.T) {
	ents := extractBranch(t, "gomobile_branch.go")

	// switch arms: "android", and the multi-platform "ios","darwin" arm split.
	for _, plat := range []string{"android", "ios", "darwin"} {
		name := `gomobile:branch:switch runtime.GOOS case "` + plat + `"`
		e := branchByName(ents, name)
		if e == nil {
			t.Fatalf("expected switch branch for %q; got %+v", plat, ents)
		}
		if e.Properties["branch_kind"] != "switch" {
			t.Errorf("%s: branch_kind=%q want switch", plat, e.Properties["branch_kind"])
		}
		if e.Properties["platform"] != plat {
			t.Errorf("%s: platform=%q", plat, e.Properties["platform"])
		}
		if e.Properties["discriminant"] != "runtime.GOOS" {
			t.Errorf("%s: discriminant=%q", plat, e.Properties["discriminant"])
		}
	}
}

func TestGomobileBranchDedup(t *testing.T) {
	ents := extractBranch(t, "gomobile_branch.go")
	// `runtime.GOOS == "android"` appears in both configPath() and guarded();
	// it must be emitted exactly once.
	n := 0
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "branch" &&
			e.Name == `gomobile:branch:runtime.GOOS=="android"` {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected 1 android if-branch (deduped), got %d", n)
	}
}

func TestGomobileBranchRequiresMarker(t *testing.T) {
	// A file with runtime.GOOS branches but NO gomobile import must emit nothing.
	src := `package p
import "runtime"
func f() { if runtime.GOOS == "android" { _ = 1 } }`
	e, _ := extreg.Get("custom_go_gomobile")
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path: "p.go", Language: "go", Content: []byte(src),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range ents {
		if ent.Subtype == "branch" {
			t.Fatalf("no gomobile marker => no branch entities, got %+v", ent)
		}
	}
}
