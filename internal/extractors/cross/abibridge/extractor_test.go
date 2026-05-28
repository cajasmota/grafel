package abibridge_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsc "github.com/smacker/go-tree-sitter/c"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/assembly" // register "assembly"
	_ "github.com/cajasmota/archigraph/internal/extractors/cpp"      // register "c"/"cpp"
	_ "github.com/cajasmota/archigraph/internal/extractors/cross/abibridge"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// extractAll runs the per-file C, assembly, and abi-bridge extractors over a
// (path, content, language) input exactly as the pipeline does (Pass 1 +
// Pass 3), returning the merged entity slice.
func extractAll(t *testing.T, path string, content []byte, lang string) []types.EntityRecord {
	t.Helper()
	var tree *sitter.Tree
	if lang == "c" {
		p := sitter.NewParser()
		p.SetLanguage(tsc.GetLanguage())
		tr, err := p.ParseCtx(context.Background(), nil, content)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		tree = tr
	}
	in := extractor.FileInput{Path: path, Content: content, Language: lang, Tree: tree}

	var out []types.EntityRecord
	// Pass 1: the language extractor.
	if ext, ok := extractor.Get(lang); ok {
		recs, err := ext.Extract(context.Background(), in)
		if err != nil {
			t.Fatalf("%s extract %s: %v", lang, path, err)
		}
		out = append(out, recs...)
	}
	// Pass 3: the abi-bridge cross extractor.
	if ext, ok := extractor.Get("_cross_abibridge"); ok {
		recs, err := ext.Extract(context.Background(), in)
		if err != nil {
			t.Fatalf("abibridge extract %s: %v", path, err)
		}
		out = append(out, recs...)
	}
	return out
}

// assignIDs stamps deterministic graph IDs (as the pipeline does before
// resolution) so BuildIndex can index the entities.
func assignIDs(recs []types.EntityRecord) {
	for i := range recs {
		recs[i].ID = graph.EntityID("", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
}

func idForNameKind(recs []types.EntityRecord, name, kind, file string) string {
	for _, r := range recs {
		if r.Name == name && r.Kind == kind && (file == "" || r.SourceFile == file) {
			return r.ID
		}
	}
	return ""
}

func findEntity(recs []types.EntityRecord, name, subtype string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Name == name && recs[i].Subtype == subtype {
			return &recs[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// asm → C : extern decl + .globl procedure → IMPLEMENTS
// ---------------------------------------------------------------------------

func TestAsmToC_ExternDeclImplementedByGloblProcedure(t *testing.T) {
	cRecs := extractAll(t, "caller.c", loadFixture(t, "caller.c.fixture"), "c")
	asmRecs := extractAll(t, "crypt.s", loadFixture(t, "crypt.s.fixture"), "assembly")

	// The abi bridge emitted a per-file extern-decl marker carrying one
	// IMPLEMENTS edge per bridged symbol. The marker's Name is the synthetic
	// __abi_bridge__ (never a real symbol) so it does not pollute the by-name
	// index — that is the load-bearing design point.
	marker := findEntity(cRecs, "__abi_bridge__", "abi_extern_decl")
	if marker == nil {
		t.Fatal("abi bridge did not emit an extern-decl marker entity")
	}
	if marker.Properties["abi_bridge"] != "c_extern_decl" {
		t.Errorf("marker abi_bridge prop = %q want c_extern_decl", marker.Properties["abi_bridge"])
	}
	implTargets := map[string]types.RelationshipRecord{}
	for _, rel := range marker.Relationships {
		if rel.Kind == "IMPLEMENTS" {
			implTargets[rel.ToID] = rel
		}
	}
	if _, ok := implTargets["crypt_block"]; !ok {
		t.Fatalf("marker should carry an IMPLEMENTS edge to crypt_block; got %+v", marker.Relationships)
	}
	if _, ok := implTargets["fast_memcpy"]; !ok {
		t.Errorf("marker should carry an IMPLEMENTS edge to fast_memcpy; got %+v", marker.Relationships)
	}
	if implTargets["crypt_block"].Properties["abi_bridge"] != "asm_implements_c_decl" {
		t.Errorf("IMPLEMENTS abi_bridge prop = %q", implTargets["crypt_block"].Properties["abi_bridge"])
	}

	// The asm side exported crypt_block as a procedure.
	if findEntity(asmRecs, "crypt_block", "procedure") == nil {
		t.Fatal("assembly extractor did not emit a crypt_block procedure")
	}

	// Build the resolver index over BOTH languages' entities and confirm the
	// IMPLEMENTS edge's bare-symbol ToID binds to the asm procedure by name
	// (the c-cpp ↔ assembly boundary hop). Because the marker does NOT take
	// the name crypt_block, the symbol resolves UNAMBIGUOUSLY to the asm proc.
	all := append(append([]types.EntityRecord{}, cRecs...), asmRecs...)
	assignIDs(all)
	idx := resolve.BuildIndex(all)

	gotID, ok := idx.Lookup("crypt_block")
	if !ok {
		t.Fatal("symbol crypt_block did not resolve in the by-name index")
	}
	wantAsm := idForNameKind(all, "crypt_block", "SCOPE.Operation", "crypt.s")
	if wantAsm == "" {
		t.Fatal("crypt_block asm procedure missing after merge")
	}
	if gotID != wantAsm {
		t.Errorf("crypt_block resolved to %q want asm procedure %q", gotID, wantAsm)
	}

	// The IMPLEMENTS edge itself rewrites to the asm procedure under the
	// resolver's reference pass (the actual cross-language link).
	implRels := []types.RelationshipRecord{implTargets["crypt_block"]}
	assignIDs(all) // ensure marker has an ID for FromID rewrite
	resolve.References(implRels, idx)
	if implRels[0].ToID != wantAsm {
		t.Errorf("IMPLEMENTS edge ToID resolved to %q want asm procedure %q", implRels[0].ToID, wantAsm)
	}

	// And the second bridged symbol resolves to its asm procedure too.
	if _, hit := idx.Lookup("fast_memcpy"); !hit {
		t.Error("fast_memcpy (second extern decl + .globl) did not resolve")
	}
}

// ---------------------------------------------------------------------------
// C → asm : asm `call <cfunc>` binds to a C function_definition
// ---------------------------------------------------------------------------

func TestCToAsm_AsmCallBindsToCFunction(t *testing.T) {
	cRecs := extractAll(t, "caller.c", loadFixture(t, "caller.c.fixture"), "c")
	asmRecs := extractAll(t, "driver.s", loadFixture(t, "driver.s.fixture"), "assembly")

	// caller.c defines exported_c_routine as a real C function.
	all := append(append([]types.EntityRecord{}, cRecs...), asmRecs...)
	assignIDs(all)
	idx := resolve.BuildIndex(all)

	gotID, ok := idx.Lookup("exported_c_routine")
	if !ok {
		t.Fatal("exported_c_routine did not resolve in the by-name index")
	}
	wantC := idForNameKind(all, "exported_c_routine", "SCOPE.Operation", "caller.c")
	if wantC == "" {
		t.Fatal("exported_c_routine C function missing after merge")
	}
	if gotID != wantC {
		t.Errorf("exported_c_routine resolved to %q want C function %q", gotID, wantC)
	}

	// The asm_entry procedure carries a bare-name CALLS edge to it.
	var found bool
	for _, r := range asmRecs {
		if r.Name != "asm_entry" {
			continue
		}
		for _, rel := range r.Relationships {
			if rel.Kind == "CALLS" && rel.ToID == "exported_c_routine" {
				found = true
			}
		}
	}
	if !found {
		t.Error("asm_entry should CALL exported_c_routine (C → asm-caller direction)")
	}
}

// ---------------------------------------------------------------------------
// C inline asm referencing an external label
// ---------------------------------------------------------------------------

func TestInlineAsm_CallTargetLinked(t *testing.T) {
	cRecs := extractAll(t, "inline.c", loadFixture(t, "inline.c.fixture"), "c")

	marker := findEntity(cRecs, "__inline_asm__", "abi_inline_asm")
	if marker == nil {
		t.Fatal("abi bridge did not emit an inline-asm marker entity")
	}
	var hit bool
	for _, rel := range marker.Relationships {
		if rel.Kind == "CALLS" && rel.ToID == "rdtsc_helper" {
			hit = true
			if rel.Properties["abi_bridge"] != "c_inline_asm_call" {
				t.Errorf("inline-asm CALLS abi_bridge prop = %q", rel.Properties["abi_bridge"])
			}
		}
	}
	if !hit {
		t.Fatalf("inline-asm marker should CALL rdtsc_helper; got %+v", marker.Relationships)
	}

	// The inline-asm CALLS target is a bare symbol name, so it binds by name
	// to an asm `.globl` procedure of the same name. Verify against an asm
	// procedure alone (the inline.c extern decl + the asm proc share a name,
	// which is itself the proven asm→C bridge but makes the bare name
	// ambiguous — exercise the resolution against the asm proc in isolation).
	asmOnly := []types.EntityRecord{{
		Name: "rdtsc_helper", Kind: "SCOPE.Operation", Subtype: "procedure",
		SourceFile: "rdtsc.s", Language: "assembly",
	}}
	assignIDs(asmOnly)
	idx := resolve.BuildIndex(asmOnly)
	if _, ok := idx.Lookup("rdtsc_helper"); !ok {
		t.Error("rdtsc_helper inline-asm target did not resolve to the asm procedure")
	}
}

// ---------------------------------------------------------------------------
// Non-C languages and definitions are not mistaken for extern decls
// ---------------------------------------------------------------------------

func TestNonCFamilyIsNoop(t *testing.T) {
	ext, _ := extractor.Get("_cross_abibridge")
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: "crypt.s", Content: loadFixture(t, "crypt.s.fixture"), Language: "assembly",
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("assembly file should be a no-op for the abi bridge; got %d entities", len(recs))
	}
}
