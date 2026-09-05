package types_test

// prefixed_producer_enum_membership_6776_test.go — #6776 arm B4.
//
// Arm B3 closed the identifier-blindness hole in the Go producer guard and
// left what it found on a ledger: six SCOPE.*-prefixed entity kinds that a Go
// producer writes and types.AllEntityKinds() did not carry. This arm puts them
// in the enum and empties that ledger.
//
// "What it found" is the honest scope: the ledger holds what ScanGo RESOLVES,
// which is the `Kind:` field of an Entity / EntityRecord composite literal. A
// producer passing the kind as a function argument was never on it. That does
// not change which six kinds this arm promotes — it is why b4PromotedKinds
// below lists a producer file the sweep cannot see.
//
// # What arm B4 changes, and what it deliberately does not
//
// ENUM MEMBERSHIP ONLY. The six keep their CURRENT spellings, so nothing
// already on disk changes and KindVocabularyVersion does not move — the rule on
// that constant is explicit that adding a kind strands nothing. No reindex, no
// stored-graph migration, no golden re-baseline. The producers keep writing
// their own package-level constants; this arm does not rewire them, because
// rewiring is a separate change with its own failure modes and the ledger is
// emptied either way (ScanGo resolves the identifier and IsValidEntityKind now
// accepts the value).
//
// # Why this file has a negative half at all
//
// RECALL CANNOT DETECT OVER-FIRING. Six assertions that six kinds are valid are
// all satisfied by `func IsValidEntityKind(string) bool { return true }`, and
// equally by an implementation that accepted every "SCOPE."-prefixed string.
// TestEntityKindEnum6776_B4_KindsOutsideTheEnumStayOutside is the other
// direction, and it is not decorative: internal/graph/fbwriter's arm-A fixture
// (non_enum_entity_kinds_6776_test.go) hard-fails with "fixture is inert" the
// moment `Route` or `Config` become valid, so the two rows naming them pin a
// live cross-package dependency rather than a hypothetical.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// b4PromotedKinds is the exact set arm B3's ledger held: every SCOPE.*-prefixed
// kind a Go producer declares that the enum did not carry. Every one is written
// by its producer as a package-level CONSTANT rather than a string literal,
// which is precisely why the pre-B3 literal-only guard could not see any of
// them.
//
// The list is NOT the authority on what the producers declare: that is
// TestProducerEntityKinds6776_PrefixedLedgerIsExact, which rescans the tree and
// fails on any prefixed kind it RESOLVES outside the (now empty) ledger. This
// list is the authority on what arm B4 promoted, so that a later change
// removing one of them from the enum fails here rather than silently re-opening
// the drift.
//
// The file lists are documentation and are not asserted, so they are written to
// be complete rather than to match the sweep: SCOPE.ScheduledJob's third
// producer, iac_cloudformation_edges.go, passes the kind as a FUNCTION ARGUMENT
// (emitEntity(id, cfnScheduledJobKind, ...)) and is therefore outside what
// ScanGo reads at all — see the sweep's limits in
// producer_entity_kinds_6776_test.go. Listing only what the scanner happens to
// see would let this comment inherit the scanner's blind spot.
var b4PromotedKinds = map[string]string{
	"SCOPE.Activity":     "engine/workflow_dag_edges.go, engine/workflow_edges.go",
	"SCOPE.EventFlow":    "engine/event_flow.go",
	"SCOPE.Process":      "engine/process_flow.go",
	"SCOPE.ScheduledJob": "engine/scheduled_jobs_edges.go, engine/serverless_framework_edges.go, engine/iac_cloudformation_edges.go (call site, unscanned)",
	"SCOPE.StateMachine": "engine/workflow_edges.go",
	"SCOPE.Workflow":     "engine/workflow_dag_edges.go, engine/workflow_edges.go",
}

// TestEntityKindEnum6776_B4_PromotedKindsAreValid is the POSITIVE half: each of
// the six is accepted by types.IsValidEntityKind and appears in
// AllEntityKinds() EXACTLY ONCE.
//
// The once-ness matters on its own axis: AllEntityKinds() is a hand-written
// slice, IsValidEntityKind is a linear scan over it, and a kind listed twice
// would validate identically while silently double-counting anywhere the slice
// is used as a roster (internal/graph/fbwriter builds its tally set from it).
//
// That check is scoped to THESE SIX and nothing else. Duplicating a non-B4
// entry — EntityKindClass, say — is ALIVE under the whole suite; scored, and
// left alive deliberately, because roster-wide duplicate protection is a
// different guard than arm B4's worklist and is nobody's worklist yet. Stated
// so the next reader does not mistake this for it.
//
// Varies: the kind. Holds constant: the validator and the roster.
func TestEntityKindEnum6776_B4_PromotedKindsAreValid(t *testing.T) {
	if len(b4PromotedKinds) != 6 {
		t.Fatalf("b4PromotedKinds has %d entries, want 6 — arm B4's worklist was exactly the six on B3's ledger", len(b4PromotedKinds))
	}
	counts := map[string]int{}
	for _, k := range types.AllEntityKinds() {
		counts[string(k)]++
	}
	for kind, producers := range b4PromotedKinds {
		if !types.IsValidEntityKind(kind) {
			t.Errorf("%q (%s) is written by a Go producer but IsValidEntityKind rejects it; "+
				"declare it in kinds.go AND list it in AllEntityKinds()", kind, producers)
		}
		switch counts[kind] {
		case 1:
			// expected
		case 0:
			t.Errorf("%q is not in AllEntityKinds() at all", kind)
		default:
			t.Errorf("AllEntityKinds() lists %q %d times; a duplicated roster entry "+
				"validates the same but double-counts every consumer that builds a set from it",
				kind, counts[kind])
		}
	}
}

// TestEntityKindEnum6776_B4_KindsOutsideTheEnumStayOutside is the NEGATIVE
// half: rows a too-permissive validator would admit while still passing the
// positive half above. Every row uses t.Errorf, so no row's failure masks
// another's and each is reported independently.
//
// The rows are NOT all distinct axes, and saying so would be this issue's own
// defect: Endpoint and Plugin are the SAME axis — un-prefixed rule-declared
// kinds — and no validator permissiveness admits one without the other. They
// are both present because both are named by internal/graph/fbwriter's arm-A
// fixture, which hard-fails "fixture is inert" if either becomes valid, so
// pinning them from this side documents that live cross-package dependency
// rather than widening coverage. The genuinely separate mechanisms are:
// un-prefixed rule-declared (Endpoint/Plugin), the not-promoted synthetic
// (File), the other B3 ledger (ChannelEvent), prefix-stripping (Workflow),
// near-miss spelling (SCOPE.Workflows), and re-admitting a retired kind
// (SCOPE.ExternalAPI).
//
// These two rows held Route and Config until #6776 arm B7 declared both, and
// were re-picked from the four kinds still on internal/entkinds' ledger
// (Constraint, Endpoint, Plugin, Template).
//
// THE MIRROR IS NOT COUPLED, BUT IT IS NOT UNGUARDED EITHER, and the
// difference is worth stating precisely.
//
// The drift that matters — a kind on either side quietly becoming VALID — is
// caught on the far side, loudly, by internal/graph/fbwriter's inert-fixture
// guards: :69, :233, :337 and :551 each t.Fatalf naming the kind that went
// stale AND pointing at internal/entkinds' ledger to re-pick from. Graded, not
// assumed: declaring `Endpoint` — a B8 kind, not one of arm B7's four — is
// DEAD across three packages and eleven tests, with five of those guards
// firing. A textual cross-package guard would add detection the suite already
// has, and would cost #6829's shape (one package reading another's unexported
// fixture literals) to do it, with a worse message than the guards produce.
//
// What the inert guards do NOT cover is a swap between two kinds that are BOTH
// still outside the enum: replacing either row below with another live ledger
// kind leaves this package and internal/graph/fbwriter green — measured. That
// is the honest limit of the arrangement, and it is the cheap direction to get
// wrong, not the dangerous one. What the rows below enforce directly is that
// each kind they name stays outside the enum.
//
// Varies: the shape of the non-member. Holds constant: the validator.
//
//	Endpoint, Plugin       un-prefixed rule-declared kinds — what remains of
//	                       #6776 arm B8's worklist, and the values
//	                       internal/graph/fbwriter's arm-A fixture asserts are
//	                       still invalid. ONE axis, two rows, for the reason
//	                       above.
//	File                   the commit-coupling synthetic, deliberately NOT
//	                       promoted (894 entities of an internal artefact)
//	ChannelEvent           a Go producer's UN-prefixed kind: B4 promoted the
//	                       prefixed ledger only, not goUnprefixedKindsDeferred
//	Workflow               the prefix-stripped spelling of a promoted kind — a
//	                       validator that ignored the "SCOPE." prefix passes the
//	                       positive half and fails here
//	SCOPE.Workflows        a near-miss spelling — catches prefix-only acceptance
//	SCOPE.ExternalAPI      retired by #6451; re-admitting it would undo that
func TestEntityKindEnum6776_B4_KindsOutsideTheEnumStayOutside(t *testing.T) {
	for axis, kind := range map[string]string{
		"rule-declared, arm B8's worklist (fbwriter's arm-A fixture depends on it)": "Endpoint",
		"rule-declared, the second row of that same axis":                           "Plugin",
		"the commit-coupling synthetic, deliberately not promoted":                  "File",
		"a Go producer's un-prefixed kind, on the other B3 ledger":                  "ChannelEvent",
		"the prefix-stripped spelling of a kind B4 DID promote":                     "Workflow",
		"a near-miss spelling that no producer writes":                              "SCOPE.Workflows",
		"retired by #6451": "SCOPE.ExternalAPI",
	} {
		if types.IsValidEntityKind(kind) {
			t.Errorf("%s: IsValidEntityKind(%q) is true, but nothing in this arm makes it a member; "+
				"arm B4 promotes exactly the six SCOPE.*-prefixed Go producers and nothing else", axis, kind)
		}
	}
}
