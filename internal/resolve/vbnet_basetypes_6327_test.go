package resolve

// The load-bearing half of isVBExternalBaseType (#6327, AGENTS.md
// "Derive, don't list").
//
// vbExternalBaseTypes is a HAND LIST, and a hand list is only admissible when a
// test fails in BOTH directions: a listed entry that no longer matches, and an
// unmatched item that appears. This file supplies both directions by driving
// the real vbnet extractor over a real VB.NET tree and re-deriving the set of
// unresolved EXTENDS / IMPLEMENTS targets on every run.
//
// WHAT IS SEARCHED, stated separately from what is CONCLUDED: every `.vb` file
// under $GRAFEL_VBNET_CORPUS (302 files across WakeOnLAN, StaxRip and
// display-drivers-uninstaller as of 2026-08-20), extracted, id-stamped and put
// through BuildImportTable -> ResolveImports -> BuildIndex ->
// ReferencesEmbedded. The CONCLUSION is only about the EXTENDS / IMPLEMENTS
// targets that survive that chain unresolved and carry no dot.
//
// It SKIPS without a corpus, so it is a gate where one exists and never a
// false green where one does not.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/vbnet"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// vbKnownInTreeAmbiguous are unresolved bare targets that are NOT framework
// types and must therefore NOT be added to vbExternalBaseTypes. Each names an
// in-tree declaration the resolver refused to bind, and refusing is correct:
//
//	Profile              two unrelated declarations — a Structure in
//	                     WakeOnLAN/Search.vb:28 and a Class in
//	                     staxrip/Source/General/Misc.vb:972. The corpus merges
//	                     three unrelated repos into one index, so this
//	                     particular ambiguity is an artifact of the
//	                     measurement, not of any real group.
//	TaskDialogBaseForm   a VB.NET PARTIAL class split across
//	                     staxrip/Source/UI/TaskDialogBaseForm.vb:6 and
//	                     .Designer.vb:4. Two records, two ids, one logical
//	                     type. Merging partials is S7's `.Designer.vb` work
//	                     (#6327), not S5's.
var vbKnownInTreeAmbiguous = map[string]bool{
	"Profile":            true,
	"TaskDialogBaseForm": true,
}

func vbCorpusRecords(t *testing.T) []types.EntityRecord {
	t.Helper()
	root := os.Getenv("GRAFEL_VBNET_CORPUS")
	if root == "" {
		t.Skip("GRAFEL_VBNET_CORPUS unset: no real VB.NET tree to derive the set from")
	}
	ext, ok := extractor.Get("vbnet")
	if !ok {
		t.Fatal("vbnet extractor not registered")
	}
	var files []string
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".vb") {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	// A denominator floor, not a rate: growing the corpus must only ever make
	// this gate harder (the lesson #6363 recorded for the S4 gate).
	if len(files) < 300 {
		t.Fatalf("GRAFEL_VBNET_CORPUS=%s yields %d .vb files, want at least 300", root, len(files))
	}
	sort.Strings(files)

	var recs []types.EntityRecord
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		rel, err := filepath.Rel(root, f)
		if err != nil {
			rel = f
		}
		ents, err := ext.Extract(t.Context(), extractor.FileInput{
			Path: filepath.ToSlash(rel), Content: src, Language: "vbnet",
		})
		if err != nil {
			t.Fatalf("Extract(%s): %v", rel, err)
		}
		recs = append(recs, ents...)
	}
	for i := range recs {
		recs[i].ID = graph.EntityID("vbnet-basetypes", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
	ResolveImports(recs, BuildImportTable(recs))
	ReferencesEmbedded(recs, BuildIndex(recs))
	return recs
}

// TestVBExternalBaseTypesAreLoadBearing fails in both directions.
func TestVBExternalBaseTypesAreLoadBearing(t *testing.T) {
	recs := vbCorpusRecords(t)

	unresolvedBare := map[string]int{}
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "EXTENDS" && r.Kind != "IMPLEMENTS" {
				continue
			}
			if isHexID(r.ToID) || strings.ContainsRune(r.ToID, dottedNameSep) {
				continue
			}
			unresolvedBare[r.ToID]++
		}
	}
	if len(unresolvedBare) == 0 {
		t.Fatal("no unresolved bare hierarchy targets at all, so this test is vacuous: " +
			"the extractor emitted nothing, or the corpus walk found nothing")
	}

	// Direction 1 — a listed entry that no longer appears is dead weight.
	for _, name := range sortedSetKeys(vbExternalBaseTypes) {
		if unresolvedBare[name] == 0 {
			t.Errorf("vbExternalBaseTypes lists %q but no unresolved EXTENDS/IMPLEMENTS "+
				"target in the corpus is spelled that way — either it now resolves "+
				"in-tree (in which case the entry is dead and must go) or the corpus "+
				"changed under it", name)
		}
	}

	// Direction 2 — an unresolved target that is neither listed nor a known
	// in-tree ambiguity is a new framework type nobody classified.
	for _, name := range sortedCountKeys(unresolvedBare) {
		if _, ok := vbExternalBaseTypes[name]; ok {
			continue
		}
		if vbKnownInTreeAmbiguous[name] {
			continue
		}
		t.Errorf("unresolved hierarchy target %q (%d edges) is in neither "+
			"vbExternalBaseTypes nor vbKnownInTreeAmbiguous, so it classifies as "+
			"bug-extractor. Decide which it is: a .NET Framework type goes in the "+
			"allowlist, an in-tree declaration the resolver refused goes in the "+
			"ambiguity list WITH the reason", name, unresolvedBare[name])
	}

	// And the ambiguity list is load-bearing too: an entry that starts
	// resolving must be removed, or it silently excuses a future regression.
	for name := range vbKnownInTreeAmbiguous {
		if unresolvedBare[name] == 0 {
			t.Errorf("vbKnownInTreeAmbiguous lists %q but it now resolves — remove "+
				"the entry so it cannot excuse a future failure", name)
		}
	}
}

// TestVBExternalBaseTypeRootNamespaceRule pins the DERIVED half: a dotted
// target rooted at a .NET framework namespace is external by construction, and
// a dotted target rooted anywhere else is not.
func TestVBExternalBaseTypeRootNamespaceRule(t *testing.T) {
	for _, s := range []string{
		"System.Windows.Forms.Form",
		"System.Configuration.ApplicationSettingsBase",
		"System.Data.TypedTableBase",
		"Microsoft.Win32.SafeHandles.SafeHandleZeroOrMinusOneIsInvalid",
		"Windows.Forms.TextBox",
	} {
		if !isVBExternalBaseType(s) {
			t.Errorf("isVBExternalBaseType(%q) = false, want true", s)
		}
	}
	for _, s := range []string{
		"StaxRip.UI.DialogBase",
		"MyCompany.Framework.Base",
		"",
	} {
		if isVBExternalBaseType(s) {
			t.Errorf("isVBExternalBaseType(%q) = true, want false — the rule keys on the "+
				"ROOT namespace precisely so it cannot claim application code", s)
		}
	}
	// The generic tail is stripped before lookup, so a stub that still carries
	// one folds onto the same answer.
	if !isVBExternalBaseType("IComparable(Of Foo)") {
		t.Error(`isVBExternalBaseType("IComparable(Of Foo)") = false, want true`)
	}
}

// TestVBNetHierarchyDispositionArm kills the "drop the classifyDispositionLang
// arm" mutant. Without the arm every framework base type classifies as
// bug-extractor, which understates OUR OWN quality numbers rather than the
// graph's — the second silent-failure trap #6327 names.
//
// MEASURED on the 302-file corpus: removing the arm moves 969 endpoints from
// external-known to bug-extractor (1,617 -> 2,586) and the bug rate from
// 0.0823 to 0.1304.
func TestVBNetHierarchyDispositionArm(t *testing.T) {
	idx := BuildIndex(nil)
	for _, target := range []string{
		"System.Windows.Forms.Form",
		"UserControl",
		"IValueConverter",
		"IComparable",
	} {
		if got := idx.classifyDispositionLang("", target, "vbnet", nil); got != DispositionExternalKnown {
			t.Errorf("classifyDispositionLang(%q, lang=vbnet) = %s, want %s — the "+
				"vbnet arm is the difference between external-known and bug-extractor",
				target, got, DispositionExternalKnown)
		}
	}
	// The lang gate is the safer-bias rule (#94): the same bare name in another
	// language must NOT be excused by VB.NET's allowlist.
	if got := idx.classifyDispositionLang("", "UserControl", "kotlin", nil); got == DispositionExternalKnown {
		t.Errorf("classifyDispositionLang(%q, lang=kotlin) = %s; the vbnet allowlist "+
			"must not shadow a same-named type in another language", "UserControl", got)
	}
}

func sortedSetKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedCountKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
