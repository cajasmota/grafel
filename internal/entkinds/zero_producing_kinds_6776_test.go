package entkinds_test

import (
	"fmt"
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// Issue #6776, arm B1 — the eight kinds arm A (c428cde7a) measured at ZERO.
//
// Arm A's runtime entity-kind counter found eight of the 25 ledgered kinds
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

// TestZeroProducingKinds6776_StayOnTheLedger records the arm-B1 decision as an
// assertion: none of the eight was deleted, so all eight remain deferred and
// ruleDeclaredKindsDeferredMax is unchanged at 25.
//
// It is deliberately obstructive. Dropping one of these from the ledger means
// its rule was deleted, and this test forces that change to come here and state
// which rule went and why — rather than the eight quietly evaporating into a
// lower ratchet, which is the outcome arm B1 exists to prevent.
func TestZeroProducingKinds6776_StayOnTheLedger(t *testing.T) {
	for k := range zeroProducingKinds6776 {
		if _, ok := ruleDeclaredKindsDeferred[k]; !ok {
			t.Errorf("%s left ruleDeclaredKindsDeferred. Arm B1 measured its rule(s) as LIVE "+
				"(internal/engine/zero_producing_rule_kinds_6776_test.go), so a removal here is "+
				"either a real enum declaration — in which case delete the entry from "+
				"zeroProducingKinds6776 too — or capability quietly deleted to lower a count.", k)
		}
	}
}
