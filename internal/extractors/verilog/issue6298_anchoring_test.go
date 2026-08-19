package verilog_test

import (
	"slices"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
)

// issue6298_anchoring_test.go — a SystemVerilog class's EXTENDS edge belongs to
// the class, not to the .sv file. Same defect as Solidity's #6295/#6297.
//
// MEASURED BEFORE THE FIX, on the two classes below in one file, id-stamped and
// pushed through ResolveImports → ReferencesEmbedded:
//
//	base_test EXTENDS uvm_test:     FromID = "e0cbdeeb860dcd8e"
//	reg_test  EXTENDS uvm_reg_test: FromID = "e0cbdeeb860dcd8e"
//
// One id, twice. e0cbdeeb860dcd8e is the FILE entity this package emits
// (extractVerilog's first append: extractor.FileEntity, Name = "src/tb/tests.sv",
// Kind SCOPE.Component) — measured by stamping graph.EntityID over the same
// record set. The path was non-empty and non-hex, so
// ReferencesEmbeddedWithAllowlist rewrote it, and it matched that file entity:
// both edges left the FILE and the two classes' base lists merged onto that one
// node. After the fix both carry FromID "" and assembly stamps each class's own
// id (base_test's is 8f9227f6484340e6, distinct).
//
// Two file-anchored edges in this extractor are deliberate and stay:
// the tool USES edge (its owning record IS file-scoped, exactly like
// vhdl/extractor.go's) and IMPORTS (#120).
func TestVerilog_ExtendsAnchoredOnClass(t *testing.T) {
	const path = "src/tb/tests.sv"
	src := `package tb_pkg;

class base_test extends uvm_test;
  int n;
endclass

class reg_test extends uvm_reg_test;
  int m;
endclass

endpackage
`
	recs := runVerilog(t, src, path, "systemverilog")
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6298", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
	resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
	resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))

	want := map[string]string{"base_test": "uvm_test", "reg_test": "uvm_reg_test"}
	seen := map[string]bool{}
	for i := range recs {
		rec := &recs[i]
		base, tracked := want[rec.Name]
		if !tracked || rec.Kind != "SCOPE.Component" {
			continue
		}
		seen[rec.Name] = true
		var bases []string
		for _, r := range rec.Relationships {
			if r.Kind != "EXTENDS" {
				continue
			}
			if r.FromID != "" {
				t.Errorf("%s EXTENDS %s: FromID = %q, want \"\" so assembly stamps the class's own id",
					rec.Name, r.ToID, r.FromID)
			}
			bases = append(bases, r.ToID)
		}
		if !slices.Equal(bases, []string{base}) {
			t.Errorf("%s bases = %v, want [%s]", rec.Name, bases, base)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("no SCOPE.Component entity for class %s", name)
		}
	}
}
