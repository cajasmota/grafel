package resolve

// The load-bearing half of isVBExternalBaseType (#6327, AGENTS.md
// "Derive, don't list").
//
// vbExternalBaseTypes is a HAND LIST, and a hand list is only admissible when a
// test fails in BOTH directions: a listed entry that no longer matches, and an
// unmatched instance that appears.
//
// THE FIRST VERSION OF THIS FILE DID NOT DELIVER THAT. It derived the instance
// set from $GRAFEL_VBNET_CORPUS and skipped when the variable was unset — and
// that variable appears nowhere in .github/ or the Makefile, so in CI neither
// direction ran: a bogus entry passed, and deleting a real one passed. Same
// shape as the S4 corpus gate (#6363), one story later.
//
// So the pin now runs UNCONDITIONALLY against a checked-in fixture
// (testdata/vbnet_external_basetypes_6327.vb), which is a property of the list
// and the classifier rather than of anybody's checkout. The corpus check
// survives as a SEPARATE, skippable test: it answers a different question —
// "is the list still right about a real tree" — and it may not silently stand
// in for the pin.

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

// vbResolveFiles runs the Path-B chain over the given in-memory .vb files and
// returns the records with ToIDs rewritten.
func vbResolveFiles(t *testing.T, files map[string]string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("vbnet")
	if !ok {
		t.Fatal("vbnet extractor not registered")
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var recs []types.EntityRecord
	for _, p := range paths {
		ents, err := ext.Extract(t.Context(), extractor.FileInput{
			Path: p, Content: []byte(files[p]), Language: "vbnet",
		})
		if err != nil {
			t.Fatalf("Extract(%s): %v", p, err)
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

// vbUnresolvedBareHierarchy collects the EXTENDS / IMPLEMENTS targets that
// stayed unresolved and carry no dot — i.e. the exact population the bare-leaf
// half of isVBExternalBaseType is consulted for.
func vbUnresolvedBareHierarchy(recs []types.EntityRecord) map[string]int {
	out := map[string]int{}
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "EXTENDS" && r.Kind != "IMPLEMENTS" {
				continue
			}
			if isHexID(r.ToID) || strings.ContainsRune(r.ToID, dottedNameSep) {
				continue
			}
			out[r.ToID]++
		}
	}
	return out
}

// TestVBExternalBaseTypesAreLoadBearing is the unconditional pin. No corpus, no
// environment variable, no skip.
//
// WHAT IS SEARCHED, stated separately from what is CONCLUDED: every
// Inherits/Implements clause in testdata/vbnet_external_basetypes_6327.vb,
// after extraction and resolution. The CONCLUSION is only about that file. The
// real-tree question is TestVBExternalBaseTypesMatchTheCorpus below.
func TestVBExternalBaseTypesAreLoadBearing(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "vbnet_external_basetypes_6327.vb"))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	recs := vbResolveFiles(t, map[string]string{"fixture/BaseTypes.vb": string(src)})
	got := vbUnresolvedBareHierarchy(recs)
	if len(got) == 0 {
		t.Fatal("the fixture produced no unresolved bare hierarchy target at all, " +
			"so this test is vacuous: the extractor emitted nothing, or every " +
			"clause resolved")
	}

	// Direction 1 — a listed entry with no clause in the fixture is dead
	// weight, and nothing else would ever notice.
	for _, name := range sortedSetKeys(vbExternalBaseTypes) {
		if got[name] == 0 {
			t.Errorf("vbExternalBaseTypes lists %q but the fixture has no clause "+
				"naming it. Add `Inherits %s` / `Implements %s` to "+
				"testdata/vbnet_external_basetypes_6327.vb, or drop the entry — an "+
				"entry no test exercises is a claim nobody checked", name, name, name)
		}
	}

	// Direction 2 — a clause with no entry is a name that classifies
	// bug-extractor.
	for _, name := range sortedCountKeys(got) {
		if _, ok := vbExternalBaseTypes[name]; !ok {
			t.Errorf("the fixture names %q as an unresolved hierarchy target but "+
				"vbExternalBaseTypes does not list it, so it classifies as "+
				"bug-extractor", name)
		}
	}

	// And the arm actually fires for each of them, which is the thing the list
	// exists to do. isHexID("") is false, so the empty resolvedID means
	// "unresolved" here.
	idx := BuildIndex(recs)
	for _, name := range sortedCountKeys(got) {
		if d := idx.classifyDispositionLang("", name, "vbnet", nil); d != DispositionExternalKnown {
			t.Errorf("classifyDispositionLang(%q, lang=vbnet) = %s, want %s", name, d, DispositionExternalKnown)
		}
	}
}

// TestVBExternalBaseTypeArmDoesNotShadowInTreeTypes is the guard the first
// version of the arm was missing.
//
// UNRESOLVED IS NOT ABSENT. `Panel` is in the allow-list and is also a name a
// WinForms application readily gives its own class; when two records carry it —
// a partial class split across `Foo.vb` and `Foo.Designer.vb`, or a duplicate
// declaration — the endpoint reaches classifyDispositionLang unresolved with
// the type very much present. Classifying that external-known would hide the
// partial-class defect #6327 S7 exists to fix, in the one metric that would
// otherwise show it.
func TestVBExternalBaseTypeArmDoesNotShadowInTreeTypes(t *testing.T) {
	recs := vbResolveFiles(t, map[string]string{
		// Two declarations of Panel, which is how a partial class arrives at
		// the resolver before S7 merges them.
		"app/Panel.vb": "Public Class Panel\nEnd Class\n",
		"app/Panel.Designer.vb": "Partial Class Panel\n" +
			"    Private components As Object\n" +
			"End Class\n",
		"app/Uses.vb": "Public Class MyPanel\n    Inherits Panel\nEnd Class\n",
	})

	if got := vbUnresolvedBareHierarchy(recs); got["Panel"] == 0 {
		t.Fatal("Panel resolved, so this test cannot observe the shadowing case — " +
			"the fixture must produce an AMBIGUOUS in-tree Panel")
	}

	idx := BuildIndex(recs)
	if !idx.nameExists("Panel") {
		t.Fatal("nameExists(\"Panel\") = false, so the guard under test is not " +
			"even reachable; this test would be vacuous")
	}
	if d := idx.classifyDispositionLang("", "Panel", "vbnet", nil); d == DispositionExternalKnown {
		t.Errorf("classifyDispositionLang(%q, lang=vbnet) = %s for a type that "+
			"EXISTS in-tree (two records carry the name). The vbnet arm must be "+
			"gated on !idx.nameExists — an unresolved endpoint is very often an "+
			"ambiguous in-tree one, and excusing it hides the S7 partial-class "+
			"defect", "Panel", d)
	}
}

// TestVBNetHierarchyDispositionArm kills the "drop the classifyDispositionLang
// arm" mutant, on names that do NOT exist in the graph.
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

// TestVBExternalBaseTypeRootNamespaceRule pins the DERIVED half: a dotted
// target rooted at a .NET framework namespace is external, and a dotted target
// rooted anywhere else is not.
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
		// Dropped from vbFrameworkRootNamespaces: neither appears as a dotted
		// target anywhere in the corpus, so each was an unmeasured guess whose
		// only possible effect was to excuse an in-tree namespace.
		"Accessibility.Widgets.Base",
		"Mono.Helpers.Thing",
		"",
	} {
		if isVBExternalBaseType(s) {
			t.Errorf("isVBExternalBaseType(%q) = true, want false — the rule keys on "+
				"the ROOT namespace precisely so it cannot claim application code", s)
		}
	}
	// The generic tail is stripped before lookup, so a stub that still carries
	// one folds onto the same answer.
	if !isVBExternalBaseType("IComparable(Of Foo)") {
		t.Error(`isVBExternalBaseType("IComparable(Of Foo)") = false, want true`)
	}
}

// ---------------------------------------------------------------------------
// Corpus-gated: a DIFFERENT question, deliberately kept separate.
// ---------------------------------------------------------------------------

// vbKnownInTreeAmbiguous are unresolved bare targets in the real corpus that
// are NOT framework types and must therefore NOT be added to
// vbExternalBaseTypes. Each names an in-tree declaration the resolver refused
// to bind, and refusing is correct:
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
//	                     type. Merging partials is S7's `.Designer.vb` work.
//
// Both are the shape TestVBExternalBaseTypeArmDoesNotShadowInTreeTypes pins:
// present in the graph, unresolved at the endpoint, and correctly NOT excused.
var vbKnownInTreeAmbiguous = map[string]bool{
	"Profile":            true,
	"TaskDialogBaseForm": true,
}

// TestVBExternalBaseTypesMatchTheCorpus asks whether the list is still right
// about a REAL tree. It skips without one, which is legitimate here precisely
// because it is no longer the only thing pinning the list.
func TestVBExternalBaseTypesMatchTheCorpus(t *testing.T) {
	root := os.Getenv("GRAFEL_VBNET_CORPUS")
	if root == "" {
		t.Skip("GRAFEL_VBNET_CORPUS unset — the unconditional pin is " +
			"TestVBExternalBaseTypesAreLoadBearing; this test only adds the real tree")
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

	contents := make(map[string]string, len(files))
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		rel, err := filepath.Rel(root, f)
		if err != nil {
			rel = f
		}
		contents[filepath.ToSlash(rel)] = string(src)
	}
	got := vbUnresolvedBareHierarchy(vbResolveFiles(t, contents))
	if len(got) == 0 {
		t.Fatal("no unresolved bare hierarchy target in the whole corpus, so this " +
			"test is vacuous")
	}

	for _, name := range sortedCountKeys(got) {
		if _, ok := vbExternalBaseTypes[name]; ok {
			continue
		}
		if vbKnownInTreeAmbiguous[name] {
			continue
		}
		t.Errorf("unresolved hierarchy target %q (%d edges) is in neither "+
			"vbExternalBaseTypes nor vbKnownInTreeAmbiguous, so it classifies as "+
			"bug-extractor. Decide which it is: a .NET Framework type goes in the "+
			"allowlist AND in testdata/vbnet_external_basetypes_6327.vb, an in-tree "+
			"declaration the resolver refused goes in the ambiguity list WITH the "+
			"reason", name, got[name])
	}
	// The ambiguity list is load-bearing too: an entry that starts resolving
	// must be removed, or it silently excuses a future regression.
	for name := range vbKnownInTreeAmbiguous {
		if got[name] == 0 {
			t.Errorf("vbKnownInTreeAmbiguous lists %q but it now resolves — remove "+
				"the entry so it cannot excuse a future failure", name)
		}
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
