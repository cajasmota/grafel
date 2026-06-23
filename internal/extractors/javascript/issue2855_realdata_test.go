// Package javascript — issue #2855 real-data verification (Data Flow group).
// Runs the registered TypeScript/TSX extractor over the in-repo real-world
// fixtures and asserts the new Data-Flow signals (component_prop, data_fetch,
// branch_condition, state_setter, ngrx Store CALLS) fire on real-shaped source.
package javascript_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tstsx "github.com/smacker/go-tree-sitter/typescript/tsx"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func extractRealWorld(t *testing.T, rel string) []types.EntityRecord {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "fixtures", "real-world", "typescript", rel)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("real-world fixture %s not present: %v", rel, err)
	}
	parser, _ := tssmacker.New().NewParser(tssmacker.WrapLanguage(tstsx.GetLanguage()))
	defer parser.Close()
	tree, _ := parser.Parse(content)
	ext, _ := extreg.Get("typescript")
	ents, err := ext.Extract(context.Background(), extreg.FileInput{
		Path: path, Content: content, Language: "typescript", TSTree: tree,
	})
	if err != nil {
		t.Fatalf("Extract %s: %v", rel, err)
	}
	return ents
}

func countSubtype(ents []types.EntityRecord, subtype string) int {
	n := 0
	for i := range ents {
		if ents[i].Subtype == subtype {
			n++
		}
	}
	return n
}

func TestIssue2855_RealData_AngularDataFlow(t *testing.T) {
	ents := extractRealWorld(t, "angular_dataflow_component.ts")

	if got := countSubtype(ents, "component_prop"); got < 4 {
		t.Errorf("angular component_prop count = %d, want >= 4 (@Input title/pageSize + @Output selected/removed); %s", got, dumpKinds(ents))
	}
	if got := countSubtype(ents, "data_fetch"); got < 2 {
		t.Errorf("angular data_fetch count = %d, want >= 2 (http get/delete)", got)
	}
	if got := countSubtype(ents, "branch_condition"); got < 1 {
		t.Errorf("angular branch_condition count = %d, want >= 1 (*ngIf/@if/*ngFor)", got)
	}

	comp := findByName(ents, "UserListComponent")
	if comp == nil {
		t.Fatalf("UserListComponent not extracted")
	}
	if !hasRel(comp.Relationships, "CALLS", "Store.select") {
		t.Errorf("missing ngrx CALLS Store.select; rels=%v", comp.Relationships)
	}
	if !hasRel(comp.Relationships, "CALLS", "Store.dispatch") {
		t.Errorf("missing ngrx CALLS Store.dispatch")
	}
	t.Logf("real-data angular: props=%d fetch=%d branch=%d",
		countSubtype(ents, "component_prop"), countSubtype(ents, "data_fetch"),
		countSubtype(ents, "branch_condition"))
}

func TestIssue2855_RealData_ReactDataFlow(t *testing.T) {
	ents := extractRealWorld(t, "react_dataflow_component.tsx")

	if got := countSubtype(ents, "component_prop"); got < 3 {
		t.Errorf("react component_prop count = %d, want >= 3 (userId/title/onSelect); %s", got, dumpKinds(ents))
	}
	if got := countSubtype(ents, "state_setter"); got < 2 {
		t.Errorf("react state_setter count = %d, want >= 2 (setName/setOpen/dispatch)", got)
	}
	t.Logf("real-data react: props=%d state_setter=%d",
		countSubtype(ents, "component_prop"), countSubtype(ents, "state_setter"))
}
