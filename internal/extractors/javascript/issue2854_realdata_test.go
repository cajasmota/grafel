// Package javascript — issue #2854 real-data verification: run the registered
// TypeScript/TSX extractor over the in-repo *real-world* fixture corpus
// (testdata/fixtures/real-world) and assert the new Structure-group signals
// (Angular component subtype, React hook subtype, USES_HOOK edges) fire on
// real-shaped source rather than only the hand-written unit fixtures.
package javascript_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tstsx "github.com/smacker/go-tree-sitter/typescript/tsx"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func TestIssue2854_RealData_AngularComponent(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "fixtures", "real-world", "typescript", "angular_component.ts")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("real-world angular fixture not present: %v", err)
	}
	p := sitter.NewParser()
	p.SetLanguage(tstsx.GetLanguage())
	tree, _ := p.ParseCtx(context.Background(), nil, content)
	ext, _ := extreg.Get("typescript")
	ents, err := ext.Extract(context.Background(), extreg.FileInput{
		Path: path, Content: content, Language: "typescript", Tree: tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var angularComp *types.EntityRecord
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && ents[i].Subtype == "angular_component" {
			angularComp = &ents[i]
			break
		}
	}
	if angularComp == nil {
		t.Fatalf("expected an angular_component entity in real-world fixture; got %s", dumpKinds(ents))
	}
	if angularComp.Properties["selector"] != "app-post-list" {
		t.Errorf("selector = %q, want app-post-list", angularComp.Properties["selector"])
	}
	t.Logf("real-data: angular component %q selector=%q rels=%d",
		angularComp.Name, angularComp.Properties["selector"], len(angularComp.Relationships))
}
