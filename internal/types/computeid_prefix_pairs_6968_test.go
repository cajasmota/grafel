package types

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// EntityRecord.ComputeID hashes OrgID+ProjectID+SourceFile+Kind+Name as a BARE
// concatenation, with no separators, while graph.EntityID NUL-separates every
// field. Two records that split the same characters differently across the
// Kind/Name boundary therefore share one ComputeID and hold two distinct
// EntityIDs — the #6968 disagreement.
//
// The Kind/Name boundary can only be crossed when one kind is a PROPER PREFIX
// of another: kind A named B+X collides with kind A+B named X. That makes the
// reachable surface a pure property of the kind vocabulary, and this test pins
// it: the set of ordered prefix pairs over AllEntityKinds() must equal the
// roster below, exactly.
//
// A NEW pair means a newly-added kind widened the surface — the guard fails and
// whoever added the kind decides whether the pairing is safe. A REMOVED pair
// means the roster has gone stale and must shrink. Neither direction is a
// silent pass, and the test is deliberately over AllEntityKinds() (the emit
// vocabulary) rather than over a source scan of kinds.go, so a constant that is
// declared but never emittable cannot inflate it and a kind moved behind a
// conversion cannot vanish from it.
//
// Scanning the corpus for a live instance (#6968) found none: 0 colliding
// groups over 464,651 entities / 464,280 (SourceFile, ComputeID) groups with
// the custom-extractor gate OFF, and 0 over 539,291 / 538,921 with it ON,
// across 58 archigraph-corpora repos plus grafel itself (macOS/APFS). That
// zero is corpus-relative and this test does NOT assert that no collision can
// occur: a pair on the roster is reachable the moment two entities of the
// paired kinds land in one file with completing names, and three of the twelve
// pairs (SCOPE.External/SCOPE.ExternalEndpoint, Test/TestClass, Test/TestConfig)
// already co-occur inside a single corpus repo. What this test does is convert
// "we checked once" into "we keep checking".
//
// KNOWN BLIND SPOT, deliberately not papered over: the emit surface is WIDER
// than AllEntityKinds(). The corpus scan observed ten kinds that no
// AllEntityKinds() entry declares — ChannelEvent, File, SCOPE.DI,
// SCOPE.Interface, SCOPE.Middleware, SCOPE.Observability, SCOPE.Router,
// SCOPE.Type, Stream, Subscription — all from internal/custom/** extractors.
// One of them, SCOPE.Router, forms a THIRTEENTH prefix pair with the declared
// SCOPE.Route, and both members are emitted on the corpus today. This test
// cannot see that pair, because a free-form kind string is not enumerable from
// the vocabulary; closing the gap means making those producers declare their
// kinds, which is a separate change. Refs #6968.
func TestComputeIDKindPrefixPairs6968(t *testing.T) {
	want := map[string]bool{
		// The eight SCOPE.* pairs enumerated on the issue.
		"SCOPE.Channel|SCOPE.ChannelBinding":    true,
		"SCOPE.Event|SCOPE.EventBusEvent":       true,
		"SCOPE.Event|SCOPE.EventFlow":           true,
		"SCOPE.Event|SCOPE.EventType":           true,
		"SCOPE.External|SCOPE.ExternalEndpoint": true,
		"SCOPE.External|SCOPE.ExternalService":  true,
		"SCOPE.Model|SCOPE.ModelEvent":          true,
		"SCOPE.State|SCOPE.StateMachine":        true,
		// Four more that the issue's SCOPE.*-only enumeration missed: the emit
		// vocabulary also holds unprefixed and snake_case kinds, and they pair
		// among themselves. "Test" + name "ClassFoo" against "TestClass" + name
		// "Foo" is the most ordinary-looking of the twelve.
		"Test|TestClass":                         true,
		"Test|TestConfig":                        true,
		"http_endpoint|http_endpoint_call":       true,
		"http_endpoint|http_endpoint_definition": true,
	}

	kinds := AllEntityKinds()
	if len(kinds) < 2 {
		t.Fatalf("AllEntityKinds() returned %d kinds — the enumeration below would be vacuous", len(kinds))
	}

	got := map[string]bool{}
	for _, a := range kinds {
		for _, b := range kinds {
			if a == b {
				continue
			}
			as, bs := string(a), string(b)
			if as == "" || bs == "" {
				t.Errorf("empty entity kind in AllEntityKinds(): %q / %q", as, bs)
				continue
			}
			// a is a PROPER prefix of b: an entity of kind a named
			// strings.TrimPrefix(bs, as)+X has the same Kind+Name
			// concatenation as one of kind b named X.
			if len(as) < len(bs) && strings.HasPrefix(bs, as) {
				got[as+"|"+bs] = true
			}
		}
	}

	var added, removed []string
	for p := range got {
		if !want[p] {
			added = append(added, p)
		}
	}
	for p := range want {
		if !got[p] {
			removed = append(removed, p)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)

	for _, p := range added {
		parts := strings.SplitN(p, "|", 2)
		t.Errorf("NEW ComputeID prefix pair %s: an entity of kind %s named %q+X shares a ComputeID with one of kind %s named X in the same file, while their graph.EntityIDs differ (#6968). "+
			"Either rename the kind so neither is a prefix of the other, or add the pair to the roster in this test with a note on why the collision is acceptable.",
			p, parts[0], strings.TrimPrefix(parts[1], parts[0]), parts[1])
	}
	for _, p := range removed {
		t.Errorf("stale ComputeID prefix pair %s in this test's roster: it is no longer produced by AllEntityKinds(). Remove it from `want`.", p)
	}
	if len(added) == 0 && len(removed) == 0 && len(got) != len(want) {
		t.Errorf("roster size %d != enumerated size %d", len(want), len(got))
	}
	if t.Failed() {
		var all []string
		for p := range got {
			all = append(all, p)
		}
		sort.Strings(all)
		t.Log("full enumerated prefix-pair set:\n" + fmt.Sprint(strings.Join(all, "\n")))
	}
}
