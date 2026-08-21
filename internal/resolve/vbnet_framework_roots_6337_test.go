package resolve

// The load-bearing half of the DOTTED rule (#6337 round 3).
//
// vbExternalBaseTypes — the bare-leaf table — has had a both-directions pin
// since #6327 (TestVBExternalBaseTypesAreLoadBearing). Its dotted sibling
// vbFrameworkRootNamespaces had NONE, and it is the higher-volume half: every
// `System.*` / `Microsoft.*` / `Windows.*` hierarchy target in the corpus is
// classified by that three-entry root set, not by any curated type name.
//
// An independent review of #6473 ran a WIDENING mutant — `vbFrameworkRootNamespaces`
// += `"My"`, `"Forms"` — and the entire suite stayed green (mutant W4). Every
// one of the author's own ten mutants pushed the narrowing direction, so the
// widening direction was unguarded on the map that matters most.
//
// The widening direction is the dangerous one HERE specifically, because `My`
// is VB.NET's compiler-generated IN-TREE namespace (`My.MyProject.MySettings`).
// Admitting it converts generated in-tree declarations into external
// placeholders — and because resolve.IsResolvedToID is a pure shape check, that
// RAISES the resolution metric while resolving nothing. A widening that scores
// better is the failure mode this file exists to make impossible.
//
// The pin is modelled on TestVBExternalBaseTypesAreLoadBearing and follows its
// shape deliberately: an unconditional checked-in fixture, real extractor, real
// resolution, set equality in both directions, no corpus and no environment
// variable. The corpus is a separate question and stays in the separate,
// skippable corpus test.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// vbUnresolvedDottedHierarchy collects the EXTENDS / IMPLEMENTS targets that
// stayed unresolved and DO carry a dot — the exact population
// vbFrameworkRootNamespaces is consulted for. It is the dotted mirror of
// vbUnresolvedBareHierarchy in vbnet_basetypes_6327_test.go.
func vbUnresolvedDottedHierarchy(recs []types.EntityRecord) map[string]int {
	out := map[string]int{}
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "EXTENDS" && r.Kind != "IMPLEMENTS" {
				continue
			}
			if isHexID(r.ToID) || !strings.ContainsRune(r.ToID, dottedNameSep) {
				continue
			}
			out[r.ToID]++
		}
	}
	return out
}

// vbRootSegment returns the segment before the first dot — the only part of a
// dotted spelling vbExternalBaseName actually keys on.
func vbRootSegment(s string) string {
	if dot := strings.IndexByte(s, dottedNameSep); dot > 0 {
		return s[:dot]
	}
	return s
}

// vbLoadFixture extracts and resolves one checked-in .vb fixture and returns
// its unresolved dotted hierarchy targets, failing rather than skipping if the
// fixture produced none — a fixture that yields nothing makes every assertion
// below vacuously true, which is the exact way the first version of the
// bare-leaf pin failed (#6327).
func vbLoadDottedFixture(t *testing.T, name string) map[string]int {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	got := vbUnresolvedDottedHierarchy(vbResolveFiles(t, map[string]string{"fixture/" + name: string(src)}))
	if len(got) == 0 {
		t.Fatalf("%s produced no unresolved DOTTED hierarchy target at all, so every "+
			"assertion against it is vacuous: the extractor emitted nothing, every "+
			"clause resolved, or the extractor stopped keeping the namespace in the "+
			"ToID", name)
	}
	return got
}

// TestVBFrameworkRootNamespacesAreLoadBearing is the unconditional pin. No
// corpus, no environment variable, no skip.
//
// WHAT IS SEARCHED, stated separately from what is CONCLUDED: every
// Inherits/Implements clause in the two checked-in fixtures, after extraction
// and resolution. The CONCLUSION is only about those files. The real-tree
// question stays with TestVBExternalBaseTypesMatchTheCorpus.
func TestVBFrameworkRootNamespacesAreLoadBearing(t *testing.T) {
	positive := vbLoadDottedFixture(t, "vbnet_framework_roots_6337.vb")

	// The root segments the positive fixture actually exercises.
	roots := map[string]int{}
	for target, n := range positive {
		roots[vbRootSegment(target)] += n
	}

	// Direction 1 — NARROWING. A root in the map with no clause in the
	// positive fixture is dead weight, and nothing else would ever notice.
	// Deleting a real root fails here too, via the classification loop below.
	for _, root := range sortedSetKeys(vbFrameworkRootNamespaces) {
		if roots[root] == 0 {
			t.Errorf("vbFrameworkRootNamespaces lists %q but no clause in "+
				"testdata/vbnet_framework_roots_6337.vb is rooted there. Add an "+
				"`Inherits %s.Some.Type` clause, or drop the entry — a root no test "+
				"exercises is an unmeasured guess, which is what `Accessibility` and "+
				"`Mono` turned out to be", root, root)
		}
	}

	// Direction 2 — WIDENING, the direction mutant W4 walked straight through.
	// Set equality: a root observed in the fixture must be in the map, AND
	// (with direction 1) the map may hold nothing else. Adding `My` or `Forms`
	// to the map fails direction 1; adding a clause here without the map entry
	// fails this loop.
	for _, target := range sortedCountKeys(positive) {
		root := vbRootSegment(target)
		if _, ok := vbFrameworkRootNamespaces[root]; !ok {
			t.Errorf("testdata/vbnet_framework_roots_6337.vb names %q as an unresolved "+
				"dotted hierarchy target but vbFrameworkRootNamespaces does not list "+
				"root %q, so it classifies as bug-extractor", target, root)
		}
		if !isVBExternalBaseType(target) {
			t.Errorf("isVBExternalBaseType(%q) = false; the dotted rule must classify "+
				"a framework-rooted target, or the arm resolves nothing", target)
		}
	}

	// Direction 3 — the named negatives, asserted by NAME rather than only by
	// set arithmetic so a failure says which root was wrongly admitted.
	// `My` is VB.NET's compiler-generated in-tree namespace; admitting it
	// converts generated in-tree code into external placeholders and RAISES
	// the resolution metric for doing so.
	negative := vbLoadDottedFixture(t, "vbnet_nonframework_roots_6337.vb")
	// Non-vacuity for the direction that matters: it is not enough for the
	// negative fixture to produce SOME dotted target. It must still produce a
	// `My`-rooted one, or the mutant this whole file exists to kill walks
	// through a fixture that quietly stopped naming it.
	var sawMy bool
	for target := range negative {
		if vbRootSegment(target) == "My" {
			sawMy = true
		}
	}
	if !sawMy {
		t.Error("testdata/vbnet_nonframework_roots_6337.vb no longer yields any " +
			"`My.`-rooted unresolved hierarchy target, so the W4 mutant " +
			"(vbFrameworkRootNamespaces += \"My\") is no longer pinned by name")
	}
	for _, target := range sortedCountKeys(negative) {
		root := vbRootSegment(target)
		if _, ok := vbFrameworkRootNamespaces[root]; ok {
			t.Errorf("vbFrameworkRootNamespaces admits root %q (from %q), which "+
				"testdata/vbnet_nonframework_roots_6337.vb declares application-owned. "+
				"`My` in particular is generated INTO the project by the VB compiler, "+
				"so classifying it external hides in-tree code behind a placeholder",
				root, target)
		}
		if isVBExternalBaseType(target) {
			t.Errorf("isVBExternalBaseType(%q) = true, want false — the rule keys on "+
				"the ROOT namespace precisely so it cannot claim application code", target)
		}
	}

	// The two fixtures must not overlap, or direction 3 could be satisfied by a
	// fixture that simply stopped naming the risky roots.
	for target := range negative {
		if positive[target] != 0 {
			t.Errorf("%q appears in BOTH fixtures; the negative half only pins "+
				"anything while it is disjoint from the positive half", target)
		}
	}
}

// TestVBFrameworkRootRuleIsRootOnly pins the shape of the rule itself, so the
// fixture-driven test above cannot be satisfied by a classifier that has
// stopped keying on the root segment.
//
// Both directions on the same spelling: `Windows.Forms.TextBox` classifies
// because `Windows` is a root, and `Forms.DialogBase` does not, even though
// `Forms` is a segment of a spelling that does classify. That near-miss is the
// second half of mutant W4 and is the tempting generalisation to make when
// reading `Windows.Forms.TextBox` in the corpus.
func TestVBFrameworkRootRuleIsRootOnly(t *testing.T) {
	for _, s := range []string{
		"Windows.Forms.TextBox",
		"System.Windows.Forms.Form",
		"Microsoft.Win32.SafeHandles.SafeHandleZeroOrMinusOneIsInvalid",
	} {
		if !isVBExternalBaseType(s) {
			t.Errorf("isVBExternalBaseType(%q) = false, want true", s)
		}
	}
	for _, s := range []string{
		// The W4 mutant's two additions, stated as first-class negatives.
		"My.MyProject.MySettings",
		"My.Application.BaseForm",
		"Forms.DialogBase",
		"Forms.Windows.TextBox",
		// A non-root occurrence of a real root must not classify either: the
		// rule is the FIRST segment, not any segment.
		"App.System.Base",
		"App.Microsoft.Base",
		"App.Windows.Base",
	} {
		if isVBExternalBaseType(s) {
			t.Errorf("isVBExternalBaseType(%q) = true, want false — the dotted rule "+
				"keys on the FIRST segment only", s)
		}
	}
}
