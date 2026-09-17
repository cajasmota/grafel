package resolve

import (
	"go/ast"
	"go/parser"
	"go/token"
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
	//
	// #7082: this used to be strings.Contains over the raw file text, which a
	// MENTION satisfies — and seven of the nineteen tiers name their own
	// identifier in a "// See BindTierX" comment on the line above the stamp,
	// so deleting the stamp left the check green for those seven. That is
	// vacuous in exactly the case the check exists for: a tier with no
	// behavioural rows is overwhelmingly likely to be a newly added one with
	// an explanatory comment beside it. The scan is now structural — an
	// identifier in an expression position in the parsed AST — so a comment,
	// a commented-out stamp, and a string literal all fail to satisfy it.
	used := identsUsedInResolveSource(t, "refs.go", "imports.go")

	for _, tier := range AllBindTiers {
		ident, ok := byValue[tier]
		if !ok {
			t.Fatalf("AllBindTiers holds %q, which this test has no identifier for", tier)
		}
		if !used[ident] {
			t.Errorf("tier %q (%s) is never stamped from refs.go or imports.go — it is in "+
				"the enumeration, so every report and every reader treats it as live, but "+
				"nothing can produce it.\n"+
				"A mention in a comment or a string literal does NOT count (#7082): the scan "+
				"walks the parsed AST and only counts the identifier in a real expression "+
				"position. If the tier is produced indirectly — bound to another constant or "+
				"stamped from a third file — this scan cannot see it, and the indirection "+
				"needs to be named here.", tier, ident)
		}
	}
}

// identsUsedInResolveSource parses the named production files of this package
// and returns the set of identifier names appearing in the AST.
//
// Comments are not AST nodes and string-literal contents are *ast.BasicLit,
// not *ast.Ident, so neither can put a name in this set — which is the whole
// point (#7082). The files are read through readResolveSource so the
// was-it-actually-opened and plausible-size proofs still apply before the
// parse; a file that does not parse is a hard failure, never a silent empty
// set, because an empty set would make every caller's assertion vacuous in
// the permissive direction.
//
// Known limit, stated rather than papered over: this sees only what is
// written literally in these files. A tier reached indirectly (assigned to
// another constant, or stamped from a file not listed) is invisible to it,
// exactly as it was to the strings.Contains form this replaced.
func identsUsedInResolveSource(t *testing.T, names ...string) map[string]bool {
	t.Helper()
	if len(names) == 0 {
		t.Fatal("identsUsedInResolveSource called with no files — the scan would be vacuous")
	}
	used := map[string]bool{}
	for _, name := range names {
		src := readResolveSource(t, name)
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		before := len(used)
		ast.Inspect(file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				used[id.Name] = true
			}
			return true
		})
		if len(used) == before {
			t.Fatalf("%s contributed no new identifiers to the scan — the walk found nothing, "+
				"so any assertion over this set would pass vacuously", name)
		}
	}
	return used
}
