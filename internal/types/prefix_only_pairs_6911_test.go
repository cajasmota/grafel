package types_test

// prefix_only_pairs_6911_test.go — #6911.
//
// #6776's B-series, and internal/entkinds' sweep guard, record a verdict: an
// entity kind written without the `SCOPE.` prefix is an ACCIDENT of the rule
// layer, not a second namespace. Arms B5-B8 acted on it and every pair they
// touched was a true synonym.
//
// Arm B9 could not. Bare `Endpoint` is an Electron IPC channel
// (electron.yaml:41,46,52 — ipcMain.handle / ipcRenderer.invoke /
// contextBridge.exposeInMainWorld); SCOPE.Endpoint is the HTTP entrypoint. The
// rename that would make the two names differ by more than a prefix was priced
// and DECLINED by the owner in #6820, so the confusable spelling is deliberate.
// That leaves the repository with one pair whose only distinction is a prefix
// its own comments call meaningless — and a comment does not fail a build. This
// file is the build failure.
//
// Two assertions, and they grade different things:
//
//   - TestPrefixOnlyPairs6911_EndpointPairIsNotFolded — the two kinds are
//     present, distinct, and neither is spelled as the other. This is the one
//     that fires when someone "fixes the obviously missing prefix". IT IS NOT
//     THE ONLY ONE, AND IT WAS NOT FIRST: both spellings of that fold were
//     scored on #6911 and each is already caught by four pre-existing tests
//     (TestEndpointBare6776_IsADistinctValidEntityKind,
//     TestEndpointBare6776_MembershipIsNotHTTPMembership,
//     TestEntityKindVocabularyIsPinnedToItsVersion,
//     TestAllEntityKinds6830_ListsEveryDeclaredKindExactlyOnce). This one is
//     kept because its failure text names #6820, which none of theirs does —
//     not because the fold was unguarded.
//   - TestPrefixOnlyPairs6911_PopulationIsClassified — the SET of prefix-only
//     pairs is exactly the twelve classified below. #6911 asks whether any
//     OTHER pair means two things while differing only by the prefix; the
//     answer at this commit is no, and this pins it so a thirteenth pair cannot
//     appear without an author saying which of the two readings it is.
//
// Varies across the two tests: the granularity (one named pair vs the whole
// population) and the failure it can see (a fold vs an unclassified newcomer).
// Holds constant: the roster under test, AllEntityKinds(), and the definition
// of a prefix-only pair. Neither subsumes the other — a fold of the Endpoint
// pair SHRINKS the population, which the set assertion would also catch but
// would report as "a pair went missing" rather than naming the concept; and a
// new unclassified pair leaves the Endpoint pair untouched.
//
// # KNOWN LIMIT: only the STRICT direction is gated, and this was measured
//
// The table forces a thirteenth pair to be CLASSIFIED, not to be classified
// HONESTLY. Classifying a newcomer `false` (semantically distinct) is a hard
// failure until the twoConcept roster below moves with it; classifying it
// `true` (synonym) is free and green, and nothing observes whether that is
// true. The permissive half — the `Endpoint` situation recurring — is the
// unguarded one.
//
// Tying a `true` row to the `*Bare` constant family was built and scored on
// #6911 rather than argued about, and it does NOT close this. Two reasons, in
// order of weight:
//
//   - The compiler forces the name. For any prefix-only pair the identifier
//     `EntityKind<X>` is already taken by the PREFIXED kind (declaring a second
//     one does not compile — scored), so the bare constant is pushed to
//     `EntityKind<X>Bare` whatever the author means by it. A thirteenth pair
//     that means two things, declared the way anyone would declare it and
//     marked `true`, stayed GREEN under the check.
//   - `EntityKindEndpointBare` is itself in that family. The one pair on this
//     list that is NOT a synonym carries the `Bare` suffix, and kinds.go says
//     why: the suffix names the SPELLING, not the concept. So family
//     membership cannot mean "synonym" without contradicting the row this file
//     exists for.
//
// The check only ever fired on a deliberately unconventional identifier, so it
// would have graded orthography while reading as if it graded honesty. Left
// unbuilt on purpose; there is no oracle for "same concept", and the failure
// texts below state the intent instead.

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// classifiedPrefixOnlyPairs maps the un-prefixed half of every kind pair in
// AllEntityKinds() that differs only by `SCOPE.` to whether the two members
// name the SAME concept.
//
// Eleven are synonyms — the `*Bare` constants in kinds.go, admitted by #6776
// arms B6/B7/B8 as spellings rather than concepts. One is not.
var classifiedPrefixOnlyPairs = map[string]bool{
	// name: sameConcept
	"Component":  true,
	"Model":      true,
	"Schema":     true,
	"View":       true,
	"Config":     true,
	"Operation":  true,
	"Route":      true,
	"Service":    true,
	"Constraint": true,
	"Plugin":     true,
	"Template":   true,

	// The exception, and the reason this file exists. Bare `Endpoint` is an
	// Electron IPC channel; SCOPE.Endpoint is the HTTP entrypoint. See #6820
	// (rename declined) and kinds.go at EntityKindEndpointBare.
	"Endpoint": false,
}

// prefixOnlyPairsIn returns the un-prefixed names for which both `X` and
// `SCOPE.X` are members of the given roster.
func prefixOnlyPairsIn(kinds []types.EntityKind) []string {
	set := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		set[string(k)] = true
	}
	var out []string
	for s := range set {
		if bare, ok := strings.CutPrefix(s, "SCOPE."); ok && set[bare] {
			out = append(out, bare)
		}
	}
	sort.Strings(out)
	return out
}

func TestPrefixOnlyPairs6911_EndpointPairIsNotFolded(t *testing.T) {
	const bare, prefixed = "Endpoint", "SCOPE.Endpoint"

	// The constants must still spell what their names claim. Repointing
	// EntityKindEndpointBare at "SCOPE.Endpoint" is the cheapest possible
	// fold and leaves both identifiers in place, so it is checked first.
	if got := string(types.EntityKindEndpointBare); got != bare {
		t.Fatalf("EntityKindEndpointBare = %q, want %q. This constant is the Electron IPC "+
			"channel kind (electron.yaml:41,46,52); SCOPE.Endpoint is the HTTP entrypoint. "+
			"They are NOT synonyms, and #6820 priced and declined the rename that would make "+
			"them differ by more than the prefix — see #6911.", got, bare)
	}
	if got := string(types.EntityKindEndpoint); got != prefixed {
		t.Fatalf("EntityKindEndpoint = %q, want %q — the HTTP half of the pair #6911 keeps apart",
			got, prefixed)
	}

	// Both must be roster members, each exactly once. Dropping either spelling
	// orphans every stored graph that carries it.
	var sawBare, sawPrefixed int
	for _, k := range types.AllEntityKinds() {
		switch string(k) {
		case bare:
			sawBare++
		case prefixed:
			sawPrefixed++
		}
	}
	if sawBare != 1 || sawPrefixed != 1 {
		t.Errorf("AllEntityKinds() holds %q %d time(s) and %q %d time(s); want exactly one each. "+
			"These are two members naming two concepts — an Electron IPC channel and an HTTP "+
			"entrypoint. The `SCOPE.` prefix looks like the accident #6776 describes and is not "+
			"one here: the rename that would separate the names properly was declined in #6820, "+
			"so folding them re-labels every Electron IPC channel as HTTP.",
			bare, sawBare, prefixed, sawPrefixed)
	}
}

func TestPrefixOnlyPairs6911_PopulationIsClassified(t *testing.T) {
	got := prefixOnlyPairsIn(types.AllEntityKinds())

	want := make([]string, 0, len(classifiedPrefixOnlyPairs))
	for name := range classifiedPrefixOnlyPairs {
		want = append(want, name)
	}
	sort.Strings(want)

	// Positive control: the detector must actually find pairs, or every
	// assertion below is satisfied by a function that finds nothing.
	if len(got) == 0 {
		t.Fatal("fixture is inert: prefixOnlyPairsIn found no `X`/`SCOPE.X` pair at all, so it " +
			"cannot observe a fold or a newcomer")
	}

	gotSet := map[string]bool{}
	for _, name := range got {
		gotSet[name] = true
		if _, classified := classifiedPrefixOnlyPairs[name]; !classified {
			t.Errorf("%q and SCOPE.%s are both entity kinds and differ only by the prefix, but "+
				"no row here says whether that is one concept or two. Add it: `true` if the two "+
				"spellings name the SAME concept (the #6776 reading — the prefix is an accident), "+
				"`false` if the prefix carries meaning, as it does for Endpoint (#6820, #6911). "+
				"An unclassified pair is exactly the hazard this file exists to prevent.",
				name, name)
		}
	}
	for _, name := range want {
		if !gotSet[name] {
			t.Errorf("%q/SCOPE.%s was a prefix-only pair and is no longer one — one of the two "+
				"spellings left AllEntityKinds(). If that was a deliberate fold of a synonym, "+
				"delete its row here. If it was %q, it was NOT a synonym (Electron IPC vs HTTP, "+
				"rename declined in #6820) and folding it re-labels IPC channels as HTTP.",
				name, name, "Endpoint")
		}
	}

	// The table must keep saying that exactly one pair means two things, and
	// that it is the Endpoint pair. Flipping the row to `true` would turn the
	// classification above into a rubber stamp.
	var twoConcept []string
	for name, sameConcept := range classifiedPrefixOnlyPairs {
		if !sameConcept {
			twoConcept = append(twoConcept, name)
		}
	}
	sort.Strings(twoConcept)
	if len(twoConcept) != 1 || twoConcept[0] != "Endpoint" {
		t.Fatalf("pairs classified as two concepts = %v, want exactly [Endpoint]. That row is "+
			"#6820's ruling written down (Electron IPC vs HTTP, rename declined); if a second "+
			"pair genuinely means two things, this test and kinds.go's 'one exception' comment "+
			"both need updating together.", twoConcept)
	}
}
