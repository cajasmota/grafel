package solidity_test

// Issue #6368 — solidity emitted one `SCOPE.Component` per distinct import
// path, named after the path basename minus `.sol`, sourced to the IMPORTING
// file. `graph.EntityID` hashes repo|kind|name|sourceFile and excludes both
// Subtype and the line span, while the emit loop dedupes by PATH and names by
// BASENAME — two keys that disagree — so two records could share one id:
//
//   - variant 5: a file declaring `interface IERC20` that also imports another
//     IERC20.sol — the placeholder (the import line) and the interface;
//   - variant 6: `./a/Token.sol` and `./b/Token.sol` imported by one file —
//     two placeholders, two paths, one id.
//
// The fix is the #742 / #681 / #693 pattern: do not emit the placeholder, hang
// the IMPORTS relationship on the per-file SCOPE.Component (subtype="file")
// carrier that solidity already emits as entities[0]. The colliding record is
// never created, which is why this holds on BOTH indexing paths — the CLI full
// rebuild (Path B, exercised here) and the daemon's incremental reindex
// (Path A, exercised in
// internal/extractors/solidity_import_placeholder_pathA_6368_test.go, where
// nothing prunes and the records would instead be folded).
//
// These tests drive the real Path B chain in-process: extractor output ->
// graph.EntityID stamping -> BuildImportTable -> ResolveImports -> BuildIndex
// -> ReferencesEmbedded -> PruneImportPlaceholders.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

const probeRepoTag = "probe"

// TestSolidity_ImportsEmitNoPlaceholderEntity pins the emission shape directly:
// the IMPORTS edges live on the file carrier and no per-import entity exists.
func TestSolidity_ImportsEmitNoPlaceholderEntity(t *testing.T) {
	src := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "./IERC20.sol";
import "@openzeppelin/contracts/access/Ownable.sol";
import {SafeMath} from "./lib/SafeMath.sol";

contract TokenMint {
}
`
	ents := runSolidity(t, src, "src/TokenMint.sol")

	// Exactly two SCOPE.Components: the file carrier and the contract.
	var components []string
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			components = append(components, e.Name+"/"+e.Subtype)
		}
	}
	if len(components) != 2 {
		t.Errorf("SCOPE.Component entities = %v, want exactly the file carrier "+
			"and the contract — a per-import placeholder collides by graph.EntityID "+
			"with any same-named record in the same file (#6368)", components)
	}

	// Every IMPORTS edge is hosted on the file carrier.
	fileEnt := solFindSubtype(ents, "src/TokenMint.sol", "SCOPE.Component", "file")
	if fileEnt == nil {
		t.Fatal("no SCOPE.Component/file carrier for src/TokenMint.sol")
	}
	hosted := 0
	for _, r := range fileEnt.Relationships {
		if r.Kind == "IMPORTS" {
			hosted++
		}
	}
	if hosted != 3 {
		t.Errorf("file carrier hosts %d IMPORTS edges, want 3 (#742 pattern: the "+
			"file entity is the carrier) (#6368)", hosted)
	}
	total := 0
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				total++
			}
		}
	}
	if total != hosted {
		t.Errorf("%d IMPORTS edges exist but only %d are on the file carrier "+
			"(#6368)", total, hosted)
	}

	// FromID stays the importing file's path (#742 invariant 2) and
	// source_module keeps the specifier as written.
	wantModules := map[string]bool{
		"./IERC20.sol": true,
		"@openzeppelin/contracts/access/Ownable.sol": true,
		"./lib/SafeMath.sol":                         true,
	}
	for _, r := range fileEnt.Relationships {
		if r.Kind != "IMPORTS" {
			continue
		}
		if r.FromID != "src/TokenMint.sol" {
			t.Errorf("IMPORTS FromID = %q, want %q — BuildImportTable keys the "+
				"per-file binding on it (imports.go:216-226)", r.FromID, "src/TokenMint.sol")
		}
		m := r.Properties.Get("source_module")
		if !wantModules[m] {
			t.Errorf("unexpected source_module %q", m)
		}
		delete(wantModules, m)
	}
	for m := range wantModules {
		t.Errorf("no IMPORTS edge for %q", m)
	}
}

// TestSolidity_ImportEdgeIsCarriedOverUnchanged pins the restraint half of the
// #742 pattern: dropping the wrapper entity must not alter the edge. ToID stays
// the raw specifier.
//
// Resolving it to the target file's repo-relative path was tried and rejected:
// it let ReferencesEmbedded bind two fixtures correctly and MIS-BIND a third
// (`./B.sol` from src/A.sol landed on SCOPE.Operation/B.pong). Binding solidity
// import paths belongs to the resolver — #6369 — not to the extractor.
func TestSolidity_ImportEdgeIsCarriedOverUnchanged(t *testing.T) {
	src := `pragma solidity ^0.8.0;

import "./a/Token.sol";
import "../shared/Base.sol";
import "@openzeppelin/contracts/access/Ownable.sol";

contract Main {}
`
	ents := runSolidity(t, src, "src/deep/Main.sol")
	got := map[string]string{}
	lines := map[string]string{}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				got[r.Properties.Get("source_module")] = r.ToID
				lines[r.Properties.Get("source_module")] = r.Properties.Get("line")
			}
		}
	}
	for _, spec := range []string{
		"./a/Token.sol",
		"../shared/Base.sol",
		"@openzeppelin/contracts/access/Ownable.sol",
	} {
		if got[spec] != spec {
			t.Errorf("IMPORTS ToID for %q = %q, want the specifier unchanged (#6368)", spec, got[spec])
		}
	}
	// The import statement's line survived the entity removal — it used to be
	// the placeholder's StartLine.
	for spec, want := range map[string]string{
		"./a/Token.sol":      "3",
		"../shared/Base.sol": "4",
		"@openzeppelin/contracts/access/Ownable.sol": "5",
	} {
		if lines[spec] != want {
			t.Errorf("IMPORTS line for %q = %q, want %q — the placeholder's StartLine "+
				"carried this before (#6368)", spec, lines[spec], want)
		}
	}
}

// pipeline drives the Path B chain the CLI indexer runs.
func pipeline(t *testing.T, files map[string]string) ([]types.EntityRecord, []types.RelationshipRecord, resolve.PruneImportPlaceholderStats) {
	t.Helper()
	ext, ok := extractor.Get("solidity")
	if !ok {
		t.Fatal("solidity extractor not registered")
	}
	var recs []types.EntityRecord
	for _, path := range sortedKeys(files) {
		ents, err := ext.Extract(t.Context(), extractor.FileInput{
			Path: path, Content: []byte(files[path]), Language: "solidity",
		})
		if err != nil {
			t.Fatalf("Extract(%s): %v", path, err)
		}
		recs = append(recs, ents...)
	}
	for i := range recs {
		recs[i].ID = graph.EntityID(probeRepoTag, recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
	resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
	resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))
	return resolve.PruneImportPlaceholders(recs)
}

func sortedKeys(m map[string]string) []string {
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

// assertNoEntityIDCollision fails, naming every colliding record, when two
// surviving records hash to the same graph.EntityID.
func assertNoEntityIDCollision(t *testing.T, recs []types.EntityRecord) {
	t.Helper()
	byID := map[string][]types.EntityRecord{}
	for _, r := range recs {
		byID[r.ID] = append(byID[r.ID], r)
	}
	for id, group := range byID {
		if len(group) < 2 {
			continue
		}
		t.Errorf("EntityID collision on id=%s: %d records share it — "+
			"graph.EntityID hashes repo|kind|name|sourceFile only, so whichever "+
			"write lands last decides the span of the node (#6368)",
			id[:8], len(group))
		for _, r := range group {
			t.Errorf("    kind=%s subtype=%q name=%s file=%s lines=%d-%d",
				r.Kind, r.Subtype, r.Name, r.SourceFile, r.StartLine, r.EndLine)
		}
	}
}

// TestSolidity_Variant5_NoEntityIDCollisionWithShadowedDeclaration is #6368's
// variant 5 on Path B: src/IERC20.sol declares `interface IERC20` and imports
// another IERC20.sol.
func TestSolidity_Variant5_NoEntityIDCollisionWithShadowedDeclaration(t *testing.T) {
	out, _, _ := pipeline(t, map[string]string{
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
	assertNoEntityIDCollision(t, out)

	// The surviving IERC20 in src/IERC20.sol must be the real interface
	// declaration, not a one-line stub anchored on the import statement.
	var found int
	for _, r := range out {
		if r.Name == "IERC20" && r.SourceFile == "src/IERC20.sol" && r.Kind == "SCOPE.Component" {
			found++
			if r.Subtype != "interface" || r.StartLine != 5 || r.EndLine != 7 {
				t.Errorf("src/IERC20.sol IERC20 survivor = subtype=%q lines=%d-%d; "+
					"want subtype=\"interface\" lines=5-7 (the declaration, not the import stub)",
					r.Subtype, r.StartLine, r.EndLine)
			}
		}
	}
	if found != 1 {
		t.Errorf("expected exactly 1 surviving SCOPE.Component named IERC20 in src/IERC20.sol, got %d", found)
	}
}

// TestSolidity_Variant6_NoEntityIDCollisionBetweenTwoImportsOfSameBasename is
// #6368's variant 6 on Path B: two distinct import paths that collapsed onto
// one EntityID because dedup keyed on the path and naming keyed on the
// basename.
func TestSolidity_Variant6_NoEntityIDCollisionBetweenTwoImportsOfSameBasename(t *testing.T) {
	out, orphanRels, _ := pipeline(t, map[string]string{
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
	assertNoEntityIDCollision(t, out)

	for _, r := range out {
		if r.Name == "Token" && r.SourceFile == "src/Main.sol" {
			t.Errorf("src/Main.sol still carries an entity named Token "+
				"(kind=%s subtype=%q lines=%d-%d) — it imports Token, it does not "+
				"declare one (#6368)", r.Kind, r.Subtype, r.StartLine, r.EndLine)
		}
	}

	// Both IMPORTS edges must survive the removal, and stay distinguishable —
	// the two placeholders they replaced shared one EntityID.
	var imports []string
	for _, rel := range allImports(out, orphanRels) {
		imports = append(imports, rel.ToID)
	}
	want := map[string]bool{"./a/Token.sol": true, "./b/Token.sol": true}
	if len(imports) != 2 || !want[imports[0]] || !want[imports[1]] || imports[0] == imports[1] {
		t.Errorf("IMPORTS ToIDs = %v, want both %q and %q (#6368)",
			imports, "./a/Token.sol", "./b/Token.sol")
	}
}

// TestSolidity_ExternalImportStaysRawForExternalSynthesis pins the other half
// of the ToID rule. A package-manager specifier has no in-repo carrier, so it
// must reach external.Synthesize as the raw module string — the pre-resolution
// form that becomes `ext:<module>` — rather than being mangled into a
// repo-relative path that can never bind.
func TestSolidity_ExternalImportStaysRawForExternalSynthesis(t *testing.T) {
	const module = "@openzeppelin/contracts/access/Ownable.sol"
	out, orphanRels, _ := pipeline(t, map[string]string{
		"src/A.sol": `pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";

contract A is Ownable {
    function ping() external {}
}
`,
	})
	var imports []string
	for _, rel := range allImports(out, orphanRels) {
		imports = append(imports, rel.ToID)
	}
	if len(imports) != 1 || imports[0] != module {
		t.Errorf("IMPORTS ToIDs = %v, want [%q] (#6368)", imports, module)
	}

	// And no placeholder is left for `contract A is Ownable` to bind onto:
	// that false bind is #6296, and the entity it bound to no longer exists.
	for _, r := range out {
		if r.Name == "Ownable" {
			t.Errorf("Ownable placeholder still emitted (kind=%s subtype=%q "+
				"file=%s lines=%d-%d) (#6368)",
				r.Kind, r.Subtype, r.SourceFile, r.StartLine, r.EndLine)
		}
	}
}

func allImports(recs []types.EntityRecord, extra []types.RelationshipRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "IMPORTS" {
				out = append(out, rel)
			}
		}
	}
	for _, rel := range extra {
		if rel.Kind == "IMPORTS" {
			out = append(out, rel)
		}
	}
	return out
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
