package entkinds_test

import (
	"fmt"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// Issue #6776, arm B1 — the eight kinds arm A (c428cde7a) measured at ZERO.
//
// Arm A's runtime entity-kind counter found eight of the then-25 ledgered kinds
// producing no entities on either corpus. The cheap reading is "dead rules,
// delete them, and ratchet ruleDeclaredKindsDeferredMax down by eight".
//
// Arm B1 checked instead of assuming, and the answer is that NONE of them is
// dead: internal/engine's TestZeroProducingKinds6776_EverySiteFires drives the
// shipped rule tree over the construct each of the 17 declaration sites names,
// and every one produces its entity. The zeros are absence of the construct
// from two corpora, not absence of a rule — so arm B1 deletes nothing and
// ruleDeclaredKindsDeferredMax does not move. Deleting these would have removed
// eight kinds' worth of real extraction capability to make a number go down.
//
// This file is the ledger-side record of that: the eight kinds stay on the
// ledger, and their declaration-site counts — the "17 sites between them"
// arm A reported — are pinned against the live scan rather than restated in a
// comment.
// ---------------------------------------------------------------------------

// zeroProducingKinds6776 is arm A's zero list, with the declaration-site count
// the live YAML scan must still report for each. The counts are what make the
// engine-side table complete: if a new site appears for one of these kinds,
// this goes red and the site must be added to zeroKindSites() there too, or it
// ships unexercised.
var zeroProducingKinds6776 = map[string]int{
	"Decorator":      1, // graphql/frameworks/graphql_schema.yaml:75
	"Fixture":        1, // python/frameworks/pytest.yaml:65
	"Implementation": 6, // kotlin/frameworks/kmp.yaml:43,46,49,52,72,78
	"Interface":      4, // kotlin/frameworks/kmp.yaml:40,59,65 + graphql_schema.yaml:65
	"Relationship":   1, // python/frameworks/sqlalchemy.yaml:74
	"Template":       2, // python/frameworks/flask.yaml:52 + ansible_core.yaml:37
	"TestClass":      1, // python/frameworks/pytest.yaml:60
	"TestConfig":     1, // python/frameworks/pytest.yaml:48
}

// TestZeroProducingKinds6776_SiteCountsHold pins arm A's site inventory against
// the live scan. "17 sites between them" is the number that decides how much
// capability a deletion would have thrown away, and it was prose until here.
func TestZeroProducingKinds6776_SiteCountsHold(t *testing.T) {
	sites := yamlSites(scanRepo(t))

	got := map[string]int{}
	where := map[string][]string{}
	for _, s := range sites {
		if _, tracked := zeroProducingKinds6776[s.Kind]; !tracked {
			continue
		}
		got[s.Kind]++
		where[s.Kind] = append(where[s.Kind], fmt.Sprintf("%s:%d", s.File, s.Line))
	}

	total := 0
	kinds := make([]string, 0, len(zeroProducingKinds6776))
	for k := range zeroProducingKinds6776 {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		want := zeroProducingKinds6776[k]
		total += want
		if got[k] != want {
			sort.Strings(where[k])
			t.Errorf("%s: scan finds %d rule-YAML declaration sites, arm A measured %d.\n"+
				"  sites: %v\n"+
				"  If a site was ADDED, add it to zeroKindSites() in "+
				"internal/engine/zero_producing_rule_kinds_6776_test.go so it is exercised in "+
				"both directions. If a site was REMOVED, say why in this table.",
				k, got[k], want, where[k])
		}
	}
	if total != 17 {
		t.Errorf("the eight zero-producing kinds now account for %d sites, not the 17 arm A "+
			"measured; the engine-side table's length check must move with it", total)
	}
}

// TestZeroProducingKinds6776_StayOnTheLedgerOrInTheEnum records the arm-B1
// decision as an assertion: none of the eight was deleted, so each is STILL
// ACCOUNTED FOR — either deferred on ruleDeclaredKindsDeferred, or declared in
// types.AllEntityKinds() by a later arm. What it forbids is the third state:
// gone from both, which is the ledger shrinking because capability was deleted.
//
// It is deliberately obstructive, and the obstruction survived arm B5. B5
// migrated seven of the eight (Decorator, Fixture, Implementation, Interface,
// Relationship, TestClass, TestConfig) into the enum, which is the LEGITIMATE
// way off the ledger; the earlier form of this test would have forced B5 to
// delete those rows from zeroProducingKinds6776, taking their 17-site pin and
// the engine-side firing table's non-vacuity with them. The pin is the arm-B1
// evidence, so the test moved and the table did not.
//
// Varies: which of the two accounted-for states each kind is in — after B5 the
// eight are split seven-in-the-enum / one-on-the-ledger (Template, a deferred
// twin), so a mutant that checks only one of the two disjuncts fails.
// Holds constant: the eight kinds and their site counts, pinned above.
func TestZeroProducingKinds6776_StayOnTheLedgerOrInTheEnum(t *testing.T) {
	// Both disjuncts must be exercised, or this degenerates into the one-sided
	// check it replaced and stops grading the branch it was widened for.
	var ledgered, declared int
	for k := range zeroProducingKinds6776 {
		onLedger := false
		if _, ok := ruleDeclaredKindsDeferred[k]; ok {
			onLedger = true
			ledgered++
		}
		inEnum := types.IsValidEntityKind(k)
		if inEnum {
			declared++
		}
		// NOT asserted here: the BOTH state (ledgered AND declared). It is
		// already impossible to reach past TestNoUndeclaredRuleEntityKinds'
		// stale half, which fires for any ledger entry the live scan stops
		// producing — and every one of these eight has live YAML sites, pinned
		// above. A check here would be a second guard that can only ever fire
		// when that one does, which grades nothing and hides which of the two
		// is load-bearing.
		if !onLedger && !inEnum {
			t.Errorf("%s is neither on ruleDeclaredKindsDeferred nor in types.AllEntityKinds(). Arm "+
				"B1 measured its rule(s) as LIVE "+
				"(internal/engine/zero_producing_rule_kinds_6776_test.go), so this is capability "+
				"quietly deleted to lower a count. If the rule really went, say which and why here "+
				"and drop the entry from zeroProducingKinds6776 — deliberately, not silently.", k)
		}
	}
	if ledgered == 0 {
		t.Errorf("no zero-producing kind is on the ledger any more; delete this test's ledger " +
			"disjunct rather than leaving an arm of it ungraded")
	}
	if declared == 0 {
		t.Errorf("no zero-producing kind is in the enum; the enum disjunct is ungraded, so this " +
			"test is still the one-sided check it was widened away from")
	}
}
