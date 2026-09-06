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
// Varies: nothing — this is a population floor plus an emptiness pin.
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
