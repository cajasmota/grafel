package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #6858. internal/types/kinds.go's arm B6 and B7 blocks justify their
// `Bare`-suffixed constants by asserting facts about THIS package's tables.
// Until now those assertions were prose: they were checked once, by hand,
// against a blanket "every kind appears in both spellings" rule that
// classfold.go carried and #6841 retracted as false. Nothing observed them, so
// the citation stayed on the page after the premise under it was withdrawn —
// the third and fourth instances of the inference #6841 found wrong twice.
//
// These tests are that block's readings, re-derived per row against the live
// tables. They fail if a pairing state, priority, or canon rank the comment
// states ever stops being true, so the correction cannot silently drift back.
//
// The kinds are named through their types.EntityKind constants rather than as
// string literals, so removing or renaming the constant the comment justifies
// breaks the build here too.

// cit6858Pair is one reading kinds.go states: a bare kind and its prefixed
// spelling, both declared TwinInMap, at the priority and canon rank the
// comment quotes. A canonRank of 0 means the comment makes no rank claim, and
// none is checked (0 is also "absent from the rank map", which is why the
// distinction has to be explicit rather than implied by a zero value).
type cit6858Pair struct {
	bare       string
	priority   int
	canonRank  int
	ranked     bool
	kindsGoRef string
}

func cit6858ClassfoldJustifiedPairs() []cit6858Pair {
	return []cit6858Pair{
		{bare: string(types.EntityKindModelBare), priority: 100, canonRank: 5, ranked: true, kindsGoRef: "arm B6"},
		{bare: string(types.EntityKindViewBare), priority: 100, kindsGoRef: "arm B6"},
		{bare: string(types.EntityKindSchemaBare), priority: 80, kindsGoRef: "arm B6"},
		{bare: string(types.EntityKindServiceBare), priority: 100, canonRank: 4, ranked: true, kindsGoRef: "arm B7"},
	}
}

// The load-bearing half: every pair kinds.go cites classfold FOR must actually
// be a pair, per row, in the table #6841 made the authority.
func TestKindsCitation6858_ClassfoldJustifiedPairsAreTwinInMap(t *testing.T) {
	pairs := cit6858ClassfoldJustifiedPairs()
	if len(pairs) != 4 {
		t.Fatalf("expected the 4 classfold-justified pairs kinds.go cites, got %d", len(pairs))
	}
	for _, p := range pairs {
		for _, spelling := range []string{p.bare, cfOpposite(p.bare)} {
			twin, declared := FrameworkClassKindTwins[spelling]
			if !declared {
				t.Errorf("%s: kinds.go (%s) cites %q as a shipped classfold pair, but "+
					"FrameworkClassKindTwins has no declaration for it (#6858)", p.bare, p.kindsGoRef, spelling)
				continue
			}
			if twin.State != TwinInMap {
				t.Errorf("%s: kinds.go (%s) states %q is TwinInMap; the table now says %v. "+
					"The comment's justification is false — fix the comment, not this test (#6858)",
					p.bare, p.kindsGoRef, spelling, twin.State)
			}
			if got := FrameworkClassKindPriority[spelling]; got != p.priority {
				t.Errorf("%s: kinds.go (%s) quotes priority %d for %q; FrameworkClassKindPriority has %d (#6858)",
					p.bare, p.kindsGoRef, p.priority, spelling, got)
			}
			if p.ranked {
				rank, ok := FrameworkClassKindCanonRank[spelling]
				if !ok || rank != p.canonRank {
					t.Errorf("%s: kinds.go (%s) quotes canon-rank %d for %q; FrameworkClassKindCanonRank has %d (present=%v) (#6858)",
						p.bare, p.kindsGoRef, p.canonRank, spelling, rank, ok)
				}
			}
		}
	}
}

// The other direction, and the part the retracted blanket rule made
// unthinkable: kinds.go names four `Bare` kinds that classfold says nothing
// about, and says so explicitly. If any of them BECOMES a classfold row, the
// comment's "not a classfold row in either spelling" turns false and the
// pairing question for it becomes live — which is exactly when a reader needs
// to be sent back to the block.
func TestKindsCitation6858_KindsGoDisclaimsAreNotClassfoldRows(t *testing.T) {
	notRows := []struct{ bare, kindsGoRef string }{
		{string(types.EntityKindComponentBare), "arm B6 NOTE"},
		{string(types.EntityKindOperationBare), "arm B7"},
		{string(types.EntityKindRouteBare), "arm B7"},
		{string(types.EntityKindConfigBare), "arm B7"},
	}
	// Grade the enumeration itself: emptying the list would leave every
	// assertion below unexecuted and the test green, which is this PR's own
	// subject — a claim that looks checked and observes nothing (#6858).
	if len(notRows) != 4 {
		t.Fatalf("expected the 4 kinds kinds.go disclaims, got %d", len(notRows))
	}
	for _, k := range notRows {
		for _, spelling := range []string{k.bare, cfOpposite(k.bare)} {
			if _, ok := FrameworkClassKindPriority[spelling]; ok {
				t.Errorf("%s: kinds.go (%s) states %q is not a FrameworkClassKindPriority row in either "+
					"spelling, and rests its justification elsewhere; it is a row now (#6858)",
					k.bare, k.kindsGoRef, spelling)
			}
		}
	}
}

// kinds.go's rewritten arm B6 block restates classfold's own count — "eleven
// bare rows and two prefixed rows have no twin" — as the measure of how far the
// retracted blanket rule was from true. That number was ungraded in
// classfold.go's prose (:41, :162) and this PR would otherwise have made a
// second ungraded copy of it in a second package, inside the very prose it is
// correcting. So it is derived from the map here instead: change the pairing of
// any row and the sentence in kinds.go fails rather than drifts.
func TestKindsCitation6858_UnpairedRowCountsAreWhatKindsGoStates(t *testing.T) {
	bareUnpaired, prefixedUnpaired := 0, 0
	for kind := range FrameworkClassKindPriority {
		if _, paired := FrameworkClassKindPriority[cfOpposite(kind)]; paired {
			continue
		}
		if strings.HasPrefix(kind, "SCOPE.") {
			prefixedUnpaired++
		} else {
			bareUnpaired++
		}
	}
	if bareUnpaired != 11 || prefixedUnpaired != 2 {
		t.Errorf("internal/types/kinds.go (arm B6) states eleven bare rows and two prefixed rows have "+
			"no twin; FrameworkClassKindPriority now has %d bare and %d prefixed. Update that sentence "+
			"and classfold.go's own copies of the count (#6858)", bareUnpaired, prefixedUnpaired)
	}
}
