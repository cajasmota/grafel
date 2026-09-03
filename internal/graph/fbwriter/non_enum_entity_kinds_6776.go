package fbwriter

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6776 arm A — count and report ENTITY kinds that reach the write path
// without appearing in types.AllEntityKinds().
//
// #6744 froze a static ledger: internal/engine/rules/**/*.yaml declares 532
// entity-kind sites over 27 distinct values, of which 25 are not valid entity
// kinds (every one of them un-prefixed, where the Go vocabulary is spelled
// SCOPE.*). #6776 proposes migrating those 25 to SCOPE.*, and its own closing
// line is the reason this file exists:
//
//	"532 sites is an inventory, not a measurement. Which of these 26 actually
//	reach a written graph, and in what volume, is unknown."
//
// (26 is the issue body's figure; the live scan on this commit says 25 — see
// internal/entkinds/rule_declared_kinds_sweep_guard_6744_test.go, whose ledger
// and ratchet both say 25.)
//
// The warning is #6757's lesson restated: there, a static ledger ranked 22
// relationship kinds as equals and the runtime count showed ONE of them was
// 99.1% of the population. Ranking a migration off declaration-site counts
// works the wrong end of the list, and a kind declared at 91 sites that
// produces no entities is 91 edits that buy nothing.
//
// # Why a runtime counter and not a validation hook
//
// internal/engine/detector.go copies SourcePattern.EntityType straight into
// types.EntityRecord.Kind with no validation, so types.IsValidEntityKind is
// never consulted on that path at all. There is no hook to hang a count on;
// the only place the population is visible is where the value is serialized.
//
// buildEntity is that leaf, and it is the sole one: StreamingWriter.
// WriteEntity, the segmented entity-segment loop and the single-file flat path
// all funnel through it, and nothing builds an entity FlatBuffer outside it.
//
// It is per-entity, hot, and returns no error, so like buildRelationship's
// tally it CANNOT reject. Nothing is dropped and no index fails because of it.
//
// # What this measures, precisely
//
// Membership of types.AllEntityKinds(), and nothing else. It is NOT
// "undeclared": a rule YAML declares `Route`, and `Route` is counted here
// because the ENUM is what was checked. There is no entity analogue of #6773's
// derived vocabulary, so unlike the relationship report there is one
// population here, not two.
//
// The tally is SHARED with the relationship half — one *nonEnumKindTally per
// write, observing both vectors. What that buys is PLUMBING, and nothing more:
// every path that already threaded a tally and a NonEnumKindReport
// (streamingMarshal, writeGraphGenFlat, writeSegments, ApplyToSidecar) carries
// the entity population without a second parameter, a second return value or a
// second sidecar conversion to drift out of sync.
//
// It is NOT what makes Scanned cover both populations — NonEnumKindReport has
// a single Scanned bool, so that would hold with two separate tallies too. The
// two counts stay SEPARATE fields for the reason #6773 kept the derived
// relationship count separate: a population you cannot see individually is a
// population you cannot rank a migration by.

// NonEnumEntityKind is one distinct entity kind that was written to the graph
// without appearing in types.AllEntityKinds().
type NonEnumEntityKind struct {
	// Kind is the kind string exactly as it was serialized.
	Kind string
	// Entities is how many entities carried it.
	Entities int
}

// EntityKindsClean reports that a write path ran this tally AND every entity
// it wrote carried an enum kind. An unscanned report is not clean — it is
// unknown, exactly as Clean() treats the relationship half.
//
// It is deliberately SEPARATE from Clean(): the two populations have separate
// totals, and folding them into one predicate would make a caller unable to
// say which vector is dirty.
func (r NonEnumKindReport) EntityKindsClean() bool { return r.Scanned && r.Entities == 0 }

// EntityKindNames returns the reported entity-kind names in report order
// (truncated to NonEnumKindListCap, like EntityKinds).
func (r NonEnumKindReport) EntityKindNames() []string {
	out := make([]string, 0, len(r.EntityKinds))
	for _, k := range r.EntityKinds {
		out = append(out, k.Kind)
	}
	return out
}

// EntitySummary renders a one-line human/agent-readable report of the ENTITY
// half, or "" when there is nothing to say (clean, or never scanned).
//
// Separate from Summary() rather than a third clause inside it: Summary's two
// clauses are both about relationships and its callers grep it as such, and a
// check scoped to "relationship edge(s)" must not start matching entity text.
func (r NonEnumKindReport) EntitySummary() string {
	if r.Entities == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d entity(s) across %d kind(s) absent from the entity-kind vocabulary: %s",
		r.Entities, r.EntityDistinctKinds, strings.Join(r.EntityKindNames(), ", "))
	if r.EntityDistinctKinds > len(r.EntityKinds) {
		fmt.Fprintf(&b, " (+%d more)", r.EntityDistinctKinds-len(r.EntityKinds))
	}
	return b.String()
}

// applyEntityKindsToSidecar copies the ENTITY half of this report into the
// graph-stats.json payload. Called by ApplyToSidecar, which stays the single
// conversion from report to sidecar for both vectors.
func (r NonEnumKindReport) applyEntityKindsToSidecar(side *graph.GraphStatsSidecar) {
	if side == nil {
		return
	}
	side.EntityKindsScanned = r.Scanned
	side.EntitiesKindNotInEnum = r.Entities
	// EntityDistinctKinds, NOT len(r.EntityKinds): the list is truncated at
	// NonEnumKindListCap and the count is not. Reading the count off the
	// truncated list would cap the report in exactly the dimension it exists
	// to measure — the same asymmetry the relationship half carries.
	side.EntityDistinctKindsNotInEnum = r.EntityDistinctKinds
	if len(r.EntityKinds) > 0 {
		kinds := make(map[string]int, len(r.EntityKinds))
		for _, k := range r.EntityKinds {
			kinds[k.Kind] = k.Entities
		}
		side.EntityKindsNotInEnum = kinds
	}
}

// entityEnumKindSet is types.AllEntityKinds() as a lookup set.
// IsValidEntityKind is a linear scan that rebuilds the whole slice per call —
// unusable per entity on the write path. Built exactly once.
var entityEnumKindSet = sync.OnceValue(func() map[string]struct{} {
	all := types.AllEntityKinds()
	set := make(map[string]struct{}, len(all))
	for _, k := range all {
		set[string(k)] = struct{}{}
	}
	return set
})

// observeEntity records kind if — and only if — it is absent from the entity
// enum. Counting every entity instead would make the report useless: the
// number would just track graph size and could never say anything is wrong.
func (t *nonEnumKindTally) observeEntity(kind string) {
	if t == nil {
		return
	}
	if _, inEnum := entityEnumKindSet()[kind]; inEnum {
		return
	}
	t.entities++
	if t.entityCounts == nil {
		t.entityCounts = make(map[string]int)
	}
	t.entityCounts[kind]++
}

// rankEntityKinds orders an entity tally busiest-first then by name and
// truncates the NAME list at NonEnumKindListCap. The caller keeps the uncapped
// totals.
func rankEntityKinds(counts map[string]int) []NonEnumEntityKind {
	if len(counts) == 0 {
		return nil
	}
	out := make([]NonEnumEntityKind, 0, len(counts))
	for k, n := range counts {
		out = append(out, NonEnumEntityKind{Kind: k, Entities: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Entities != out[j].Entities {
			return out[i].Entities > out[j].Entities
		}
		return out[i].Kind < out[j].Kind
	})
	if len(out) > NonEnumKindListCap {
		out = out[:NonEnumKindListCap]
	}
	return out
}
