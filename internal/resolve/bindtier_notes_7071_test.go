package resolve

import (
	"os"
	"strings"
	"testing"
)

// #7071 — internal/types/bindtier.go replaced a false universal ("every
// pass that can bind a name lexically stamps this key") with two lists: the
// tiers that stamp it (AllBindTiers) and the sites CHECKED AND FOUND TO BE
// EVIDENCE, recorded as an `EVIDENCE (#7071)` note at each such site.
//
// The second list was itself partly fictional the moment it was written.
// Review of round 3 grepped for the token the doc names and found three
// refs.go sites using a DIFFERENT spelling (`EVIDENCE tier (#7071)`), and
// the Rust candidate-directory rung — named in the doc — carrying no note
// at all. Nothing graded the notes, so the doc could name sites that did
// not exist. That is the same class of claim as the universal it replaced:
// an assertion over a set nobody checked.
//
// This file is the check. It is deliberately a source-text test rather than
// a behavioural one, because what it grades IS a source-text convention.

const (
	// evidenceNote is the canonical form. The em dash is part of it: a bare
	// "EVIDENCE (#7071)" also appears in PROSE (the importTier declaration
	// explains what the absence of a note means), and counting prose as
	// notes is how this check would go vacuous.
	evidenceNote = "// EVIDENCE (#7071) —"

	// staleNote is the spelling three refs.go sites used before round 4.
	// Pinned at zero so a reintroduction is caught rather than silently
	// splitting the convention in two again.
	staleNote = "EVIDENCE tier (#7071)"
)

// evidenceNoteCounts is the audited inventory: file → exact number of
// canonical notes. EXACT, not a floor, and that is the point — adding an
// evidence classification means updating this number, which is the audit
// happening. A floor would let a site be silently reclassified.
//
// Changing a number here without changing the corresponding note is the
// only way to make this test lie, and that edit is visible in review.
var evidenceNoteCounts = map[string]int{
	"refs.go":    3,
	"imports.go": 10,
}

// readResolveSource reads one file of this package from disk and fails if
// it is empty. The read proof matters: a scan-and-assert test has several
// ways to be a no-op, and "the file was never opened" is the first of them.
func readResolveSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if len(b) < 10000 {
		t.Fatalf("%s is %d bytes — far too small to be the real file; this test would "+
			"pass vacuously against a stub", name, len(b))
	}
	return string(b)
}

// TestEvidenceNotesExistAndAreUniform_7071 grades the mechanism that
// replaced the universal.
//
// VARIED across the two rows: which source file is audited.
// HELD CONSTANT: the token spelling, the requirement that the file was
// actually read and is of plausible size, and the assertion shape (exact
// count, plus zero of the stale spelling).
func TestEvidenceNotesExistAndAreUniform_7071(t *testing.T) {
	for name, want := range evidenceNoteCounts {
		t.Run(name, func(t *testing.T) {
			src := readResolveSource(t, name)

			if got := strings.Count(src, evidenceNote); got != want {
				t.Fatalf("%s carries %d %q notes, want %d.\n"+
					"internal/types/bindtier.go tells readers that a site checked and found "+
					"to be evidence carries this note AT THE SITE. If you added an evidence "+
					"classification, update the count here — that edit IS the audit. If you "+
					"removed one, the site it described is now unclassified, which is a "+
					"different claim and must be reflected wherever it was named.",
					name, got, evidenceNote, want)
			}

			if got := strings.Count(src, staleNote); got != 0 {
				t.Fatalf("%s carries %d %q — the convention is a single spelling, %q. "+
					"Two spellings means a reader grepping the one the doc names finds only "+
					"some of the sites, which is how this list became fictional in the first "+
					"place.", name, got, staleNote, evidenceNote)
			}
		})
	}
}

// TestEveryGuessTierConstantIsUsed_7071 is the other half: a tier constant
// that no production site stamps is a tier that cannot fire, and a reader
// auditing AllBindTiers has no way to tell one from a live tier.
//
// This catches the inverse of the notes problem — the enumeration naming a
// tier nothing produces, rather than the doc naming a note nothing carries.
// Test files are excluded on purpose: a constant used only by its own test
// is exactly the dead tier this grades for.
func TestEveryGuessTierConstantIsUsed_7071(t *testing.T) {
	// The Go identifier for each tier value, derived from the constants
	// themselves so a renamed constant cannot silently drop out.
	byValue := map[BindTier]string{
		BindTierGlobalName:                    "BindTierGlobalName",
		BindTierGlobalKindFamily:              "BindTierGlobalKindFamily",
		BindTierFileKind:                      "BindTierFileKind",
		BindTierPackageOperation:              "BindTierPackageOperation",
		BindTierFileLeafName:                  "BindTierFileLeafName",
		BindTierPackageLeafName:               "BindTierPackageLeafName",
		BindTierPackageComponent:              "BindTierPackageComponent",
		BindTierFileScopePlaceholder:          "BindTierFileScopePlaceholder",
		BindTierGoInterfaceDispatch:           "BindTierGoInterfaceDispatch",
		BindTierGoAmbiguousReceiverLeaf:       "BindTierGoAmbiguousReceiverLeaf",
		BindTierGoPackageComponent:            "BindTierGoPackageComponent",
		BindTierImportPlainModuleAttr:         "BindTierImportPlainModuleAttr",
		BindTierImportWildcard:                "BindTierImportWildcard",
		BindTierImportClassModuleAttr:         "BindTierImportClassModuleAttr",
		BindTierJavaCanonicalFileTiebreak:     "BindTierJavaCanonicalFileTiebreak",
		BindTierImportNamespaceRepresentative: "BindTierImportNamespaceRepresentative",
		BindTierImportJSDefaultBasename:       "BindTierImportJSDefaultBasename",
		BindTierImportPythonReexportParent:    "BindTierImportPythonReexportParent",
		BindTierRustCrateUniqueMember:         "BindTierRustCrateUniqueMember",
	}
	if len(byValue) != len(AllBindTiers) {
		t.Fatalf("this test knows %d tiers, AllBindTiers has %d — a tier was added without "+
			"adding it here, so nothing checks that it is reachable", len(byValue), len(AllBindTiers))
	}

	// Production source only. bindtier.go holds the declarations and
	// AllBindTiers, so a mention there is not a use.
	src := readResolveSource(t, "refs.go") + readResolveSource(t, "imports.go")

	for _, tier := range AllBindTiers {
		ident, ok := byValue[tier]
		if !ok {
			t.Fatalf("AllBindTiers holds %q, which this test has no identifier for", tier)
		}
		if !strings.Contains(src, ident) {
			t.Errorf("tier %q (%s) is never stamped from refs.go or imports.go — it is in "+
				"the enumeration, so every report and every reader treats it as live, but "+
				"nothing can produce it", tier, ident)
		}
	}
}
