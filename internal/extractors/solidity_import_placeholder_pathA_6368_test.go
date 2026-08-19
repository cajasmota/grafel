// Package extractors — solidity_import_placeholder_pathA_6368_test.go
//
// #6368, Path A gate. The daemon's primary reindex is TryIncremental
// (incremental.go), NOT the CLI full rebuild, and incremental is ON BY DEFAULT
// (internal/extractor/extractor.go:39, #5231). resolve.PruneImportPlaceholders
// has exactly one non-test caller — cmd/grafel/index.go:5904, which is Path B.
// Nothing prunes import placeholders on Path A.
//
// So on Path A a solidity import placeholder does not get removed; it gets
// FOLDED. convertExtractedRecords keys on graph.EntityID, which hashes
// repo|kind|name|sourceFile and excludes Subtype and the line span, and
// foldDuplicateEntity is gap-fill-never-override:
//
//	if surv.Subtype == "" && dup.Subtype != "" { surv.Subtype = dup.Subtype }
//
// extractSolidity appends the file entity, then the import placeholders, then
// the contracts (extractor.go:124/130/139), so for a file that both imports
// `X.sol` and declares `X`, the PLACEHOLDER is always the survivor and the
// declaration is always the duplicate. That put two wrong answers on the same
// node depending on the tree:
//
//   - placeholder with an empty Subtype  -> gap-filled to "interface" from the
//     declaration, but keeping the placeholder's one-line import span;
//   - placeholder marked Subtype:"import" (the first cut of #6368) -> the
//     gap-fill is BLOCKED and a real `interface IERC20` reads as
//     subtype="import" on every daemon reindex.
//
// The fix is to not emit the placeholder at all (the #742 / #681 / #693
// pattern): the record that collides is never created, so there is nothing to
// fold and the declaration is the only row. This test pins that outcome on
// Path A specifically. It fails on both prior shapes.

package extractors

import (
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

const pathARepoTag = "probe"

// solidityRecordsForPathA extracts every file through the registered solidity
// extractor, in the order convertExtractedRecords would see them.
func solidityRecordsForPathA(t *testing.T, files map[string]string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("solidity")
	if !ok {
		t.Fatal("solidity extractor not registered")
	}
	var recs []types.EntityRecord
	for _, path := range sortedPathsForPathA(files) {
		ents, err := ext.Extract(t.Context(), extractor.FileInput{
			Path: path, Content: []byte(files[path]), Language: "solidity",
		})
		if err != nil {
			t.Fatalf("Extract(%s): %v", path, err)
		}
		recs = append(recs, ents...)
	}
	return recs
}

func sortedPathsForPathA(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// TestSolidity6368_PathA_ImportDoesNotClobberDeclaredInterface is #6368
// variant 5 driven through the INCREMENTAL record→graph seam. src/IERC20.sol
// declares `interface IERC20` on lines 5-7 and also imports another IERC20.sol
// on line 3.
func TestSolidity6368_PathA_ImportDoesNotClobberDeclaredInterface(t *testing.T) {
	recs := solidityRecordsForPathA(t, map[string]string{
		"src/IERC20.sol": `pragma solidity ^0.8.0;

import "./vendor/IERC20.sol";

interface IERC20 {
    function totalSupply() external view returns (uint256);
}
`,
		"src/vendor/IERC20.sol": `pragma solidity ^0.8.0;

interface IERC20 {
    function balanceOf(address who) external view returns (uint256);
}
`,
	})

	ents, _ := convertExtractedRecords(recs, pathARepoTag, map[string]bool{})

	var got int
	for i := range ents {
		e := &ents[i]
		if e.Kind != "SCOPE.Component" || e.Name != "IERC20" || e.SourceFile != "src/IERC20.sol" {
			continue
		}
		got++
		if e.Subtype != "interface" || e.StartLine != 5 || e.EndLine != 7 {
			t.Errorf("Path A survivor for IERC20 in src/IERC20.sol = "+
				"subtype=%q lines=%d-%d id=%s; want subtype=\"interface\" lines=5-7.\n"+
				"Nothing prunes import placeholders on the incremental path "+
				"(resolve.PruneImportPlaceholders' only non-test caller is "+
				"cmd/grafel/index.go, Path B), so an import placeholder sharing this "+
				"declaration's EntityID is FOLDED onto it by foldDuplicateEntity "+
				"instead — and because extractSolidity appends imports before "+
				"contracts, the placeholder is the survivor. The declaration must be "+
				"the only record with this id: do not emit the placeholder (#6368).",
				e.Subtype, e.StartLine, e.EndLine, e.ID[:8])
		}
	}
	if got != 1 {
		t.Errorf("expected exactly 1 SCOPE.Component named IERC20 in src/IERC20.sol after the fold, got %d", got)
	}
}

// TestSolidity6368_PathA_TwoImportsOfSameBasenameEmitNoEntity is variant 6 on
// Path A: `./a/Token.sol` and `./b/Token.sol` imported by src/Main.sol produced
// two placeholder records with one EntityID, which the fold collapsed into a
// single row whose span was whichever import statement came first. Neither
// import has any business being an entity in src/Main.sol at all.
func TestSolidity6368_PathA_TwoImportsOfSameBasenameEmitNoEntity(t *testing.T) {
	recs := solidityRecordsForPathA(t, map[string]string{
		"src/Main.sol": `pragma solidity ^0.8.0;

import "./a/Token.sol";
import "./b/Token.sol";

contract Main {
    function run() external {}
}
`,
		"src/a/Token.sol": `pragma solidity ^0.8.0;

contract Token {
    function a() external {}
}
`,
		"src/b/Token.sol": `pragma solidity ^0.8.0;

contract Token {
    function b() external {}
}
`,
	})

	ents, rels := convertExtractedRecords(recs, pathARepoTag, map[string]bool{})

	for i := range ents {
		e := &ents[i]
		if e.Name == "Token" && e.SourceFile == "src/Main.sol" {
			t.Errorf("src/Main.sol still carries a %q entity named Token "+
				"(subtype=%q lines=%d-%d id=%s) — src/Main.sol imports Token, it does "+
				"not declare one. Two distinct import paths collapsed onto one "+
				"EntityID here; the import must not be an entity (#6368).",
				e.Kind, e.Subtype, e.StartLine, e.EndLine, e.ID[:8])
		}
	}

	// The IMPORTS edges themselves must survive the removal — both of them,
	// distinctly. This is what makes "just delete the placeholder" wrong and
	// "re-host the edge on the file carrier" right. The edge is carried over
	// unchanged, ToID included.
	var imports []string
	for i := range rels {
		if rels[i].Kind == "IMPORTS" {
			imports = append(imports, rels[i].ToID)
		}
	}
	want := map[string]bool{"./a/Token.sol": true, "./b/Token.sol": true}
	if len(imports) != 2 || !want[imports[0]] || !want[imports[1]] || imports[0] == imports[1] {
		t.Errorf("IMPORTS ToIDs on Path A = %v, want both %q and %q — dropping the "+
			"placeholder must not drop the edge it carried, and two distinct import "+
			"paths must stay distinguishable (they shared one EntityID before) (#6368)",
			imports, "./a/Token.sol", "./b/Token.sol")
	}

	// #742 invariant 2: FromID stays the importing file's PATH (the indexer
	// rewrites it to the file carrier's hex id later, #577). BuildImportTable
	// keys the per-file binding on it (imports.go:216-226), so an edge carrying
	// the wrong path binds the import into the wrong file.
	for i := range rels {
		r := &rels[i]
		if r.Kind != "IMPORTS" {
			continue
		}
		if r.FromID != "src/Main.sol" {
			t.Errorf("IMPORTS edge to %s has FromID=%q, want %q (#6368)",
				r.ToID, r.FromID, "src/Main.sol")
		}
	}
}
