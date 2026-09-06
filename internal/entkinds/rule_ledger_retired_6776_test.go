package entkinds_test

// rule_ledger_retired_6776_test.go — #6776 arm B9 retires #6744's ledger.
//
// # Why a RETIREMENT needs its own guard, and is not "the ratchet reached zero"
//
// Every anti-vacuity guard in this area is built on the same premise: an empty
// ledger is indistinguishable from a scan that stopped looking. That premise is
// correct while the ledger is the only evidence, and it is the reason
// TestEnumEntityKinds6818_NoDeferredLedgerOverlapsTheEnum used to t.Fatal on an
// empty map.
//
// The population, not the ledger, is what actually reached zero: the rule-YAML
// half of the scan declares 532 sites and NOT ONE of them names a kind
// types.AllEntityKinds() rejects. That is a positive, measured statement, and
// it is strictly stronger than any ledger row — it is what the ledger was a
// stand-in for. So this file asserts the population directly, and the empty
// ledger becomes a consequence rather than an assumption.
//
// # WHAT THIS IS STRONGER AND WEAKER THAN, stated in both directions
//
// STRONGER than any ledger row in the direction the ledger was built for: a
// kind outside the enum, anywhere in the rule tree, fails here with a
// line-exact diagnosis and no row available to absorb it.
//
// WEAKER, on its own, in the direction the ledger held INCIDENTALLY: a row
// named a kind, so the sweep noticed when that kind's sites disappeared. An
// "is the set empty" question cannot notice that a MEMBER's producers vanished.
// That is why this file also pins EntityKindEndpointBare's three sites
// line-exactly below — the retirement would otherwise have dropped the only
// coupling between the member and the three producers that justify it.
//
// Varies: nothing — this is a population floor, an emptiness pin, and a
// site-level anchor for the member arm B9 admitted.
// Holds constant: the live scan of the shipped rule tree.

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/entkinds"
	"github.com/cajasmota/grafel/internal/types"
)

// minRuleYAMLSites is a slack FLOOR under the rule-YAML half, not a count.
// Measured on this branch with entkinds.ScanRuleYAML: 732 YAML files, 532
// sites, 0 unresolved. The floor catches a scan that COLLAPSES — which is the
// only way "zero invalid kinds" could become a lie — and deliberately does not
// track the churn of the rule tree.
const minRuleYAMLSites = 300

// TestRuleDeclaredLedger6776_IsRetiredBecauseThePopulationIsEmpty
//
// This is the assertion that replaces the ledger. Read it as: the thing the
// ledger recorded no longer exists, and here is the measurement, not the
// absence of a measurement.
func TestRuleDeclaredLedger6776_IsRetiredBecauseThePopulationIsEmpty(t *testing.T) {
	res, err := entkinds.ScanRuleYAML(repoRoot(t))
	if err != nil {
		t.Fatalf("ScanRuleYAML: %v", err)
	}

	// FLOOR FIRST. "No invalid kind was found" is the same sentence for a live
	// scan and for a scan that read nothing, and #6534 is this repo's standing
	// example of the second one shipping as the first.
	if len(res.Sites) < minRuleYAMLSites {
		t.Fatalf("the rule-YAML scan produced only %d site(s) from %d file(s) (floor: %d). "+
			"An emptied scan reports every rule file clean; the zero below would then mean "+
			"'nothing was read', not 'nothing is invalid'.",
			len(res.Sites), res.YAMLFilesParsed, minRuleYAMLSites)
	}
	if res.Unresolved() != 0 {
		t.Errorf("%d rule-YAML site(s) are unresolved; a site whose kind could not be read is "+
			"outside the sweep below and must not be counted as clean", res.Unresolved())
	}

	// THE PREDICATE'S OWN POSITIVE CONTROL. Of the five ways a
	// scan-and-assert-absence guard goes no-op, the floor above covers "failed
	// to read" and the sweep below covers "read the wrong files / the wrong
	// content / failed to detect / failed to act". The fifth is the DETECTOR
	// itself going permissive, and it was ungraded here: making
	// IsValidEntityKind return true for any non-empty string left this package
	// GREEN while internal/types and internal/graph/fbwriter went red — a hole
	// in this fixture, not in the suite, and the same discipline this arm
	// already applies to IsHTTPEndpointKind in
	// TestEndpointBare6776_MembershipIsNotHTTPMembership.
	const notAKind = "SCOPE.ZZNotAKindAnyProducerWrites"
	if types.IsValidEntityKind(notAKind) {
		t.Fatalf("fixture is inert: IsValidEntityKind(%q) is true, so it accepts everything and "+
			"the empty set below is a statement about a constant, not about the rule tree", notAKind)
	}

	// THE SITES THAT JUSTIFY THE MEMBER. This is the direction the retirement
	// dropped and the ledger used to hold incidentally: with the row gone,
	// nothing observed that EntityKindEndpointBare still HAS producers.
	// Measured — re-spelling electron.yaml's three `entity_type: Endpoint` rows
	// to `SCOPE.Endpoint` (the silent rename #6820 rejected) left all six
	// packages green on this branch, and failed three tests on the parent.
	//
	// It is asserted LINE-EXACT rather than as a count, because the count alone
	// would accept the three sites moving to a file that has nothing to do with
	// Electron IPC, which is the concept the member is named for.
	wantEndpointSites := []string{
		"internal/engine/rules/javascript_typescript/frameworks/electron.yaml:41",
		"internal/engine/rules/javascript_typescript/frameworks/electron.yaml:46",
		"internal/engine/rules/javascript_typescript/frameworks/electron.yaml:52",
	}
	var gotEndpointSites []string
	for _, s := range res.Sites {
		if s.Kind == string(types.EntityKindEndpointBare) {
			gotEndpointSites = append(gotEndpointSites, fmt.Sprintf("%s:%d", s.File, s.Line))
		}
	}
	sort.Strings(gotEndpointSites)
	if strings.Join(gotEndpointSites, ",") != strings.Join(wantEndpointSites, ",") {
		t.Errorf("rule-YAML sites for %q:\n  got  %v\n  want %v\n\n"+
			"#6776 arm B9 admitted this kind to types.AllEntityKinds() on the strength of exactly "+
			"those three producers (ipcMain / ipcRenderer / contextBridge). If they are gone or "+
			"re-spelled, the member is enum-only and that is a KindVocabularyVersion decision plus "+
			"an enumOnlyEntityKinds row — not something to pass in silence. A re-spelling to "+
			"SCOPE.Endpoint in particular is the rename #6820 considered and rejected.",
			types.EntityKindEndpointBare, gotEndpointSites, wantEndpointSites)
	}

	invalid := map[string][]string{}
	for _, s := range res.Sites {
		if types.IsValidEntityKind(s.Kind) {
			continue
		}
		invalid[s.Kind] = append(invalid[s.Kind], fmt.Sprintf("%s:%d", s.File, s.Line))
	}
	if len(invalid) > 0 {
		var lines []string
		for k, where := range invalid {
			lines = append(lines, fmt.Sprintf("%s (%d site(s)): %s", k, len(where), strings.Join(where, ", ")))
		}
		sort.Strings(lines)
		t.Errorf("the rule-YAML half declares %d entity kind(s) types.AllEntityKinds() rejects:\n  %s\n\n"+
			"#6744's ledger was RETIRED by #6776 arm B9 on the strength of this set being empty. "+
			"Do not reinstate it to hold a new entry: declare the kind in internal/types/kinds.go, "+
			"which is what every arm B5-B9 did.",
			len(invalid), strings.Join(lines, "\n  "))
	}

	// And the ledger itself must be gone, in BOTH of its halves — a retired
	// ledger that keeps a family tag is a ledger with room for a row.
	if len(ruleDeclaredKindsDeferred) != 0 || ruleDeclaredKindsDeferredMax != 0 {
		t.Errorf("ruleDeclaredKindsDeferred has %d entry/entries and the ratchet reads %d, but the "+
			"population above is empty. The ledger is retired: a row here now names a kind the "+
			"live scan does not produce, which TestNoUndeclaredRuleEntityKinds reports as stale.",
			len(ruleDeclaredKindsDeferred), ruleDeclaredKindsDeferredMax)
	}
	if len(ruleDeclaredFamily) != 0 {
		t.Errorf("ruleDeclaredFamily still explains %d family/families for an empty ledger. "+
			"TestYAMLHalfObservesDeclaredKinds requires live YAML traffic for every family it "+
			"holds, so a family with no rows is either a broken guard or an invitation to add one.",
			len(ruleDeclaredFamily))
	}
}

// TestRuleDeclaredLedger6776_EveryFamilyIsUsedByARow is the reverse coupling
// the perFamily traffic check in TestYAMLHalfObservesDeclaredKinds used to
// provide and cannot any more.
//
// With the ledger empty, that check's loop body is unreachable, so a future
// author could reintroduce ruleDeclaredFamily["something"] with no ledger row
// and nothing would object. This says the two maps move together in the
// direction the ledger's own family check does not cover: an entry needs a
// family (checked there), and a family needs an entry (checked here).
func TestRuleDeclaredLedger6776_EveryFamilyIsUsedByARow(t *testing.T) {
	used := map[string]bool{}
	for _, fam := range ruleDeclaredKindsDeferred {
		used[fam] = true
	}
	for fam := range ruleDeclaredFamily {
		if !used[fam] {
			t.Errorf("ruleDeclaredFamily explains family %q, which no ledger row uses. Delete it, "+
				"or add the row it exists for — an unused family is a documented slot with no "+
				"decision behind it.", fam)
		}
	}
}
