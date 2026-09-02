package fbwriter

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6757 arm C — count and report relationship kinds that reach the write
// path without appearing in types.AllRelationshipKinds().
//
// types.IsValidRelationshipKind had ZERO non-test callers, so the relationship
// vocabulary was enforced by nothing: AllRelationshipKinds() described an
// intention, not a constraint. Arm B's static ledger found 22 kinds written to
// the graph that the enum does not list — but 87 relationship-kind fields
// repo-wide resolve to a RUNTIME value rather than a source constant
// (custom/java/caching.go, cmd/grafel/index.go, the cobol and jcl extractors),
// and no static scan can see those.
//
// The only place the population is visible is where the value exists: the
// serialization leaf. buildRelationship is the sole one — StreamingWriter.
// WriteRelationship, WriteGraphGenSegmented and the segment path all funnel
// through it, and nothing builds a relationship FlatBuffer outside it.
//
// But it is per-edge, hot, and returns no error, so it CANNOT reject. This
// follows readDirBounded (internal/daemon/state_path.go:283) instead: work it
// cannot complete is abandoned but ADMITTED, never silently forgotten. Nothing
// is dropped and nothing fails.
//
// # What this measures, precisely
//
// Membership of types.AllRelationshipKinds() — the GO ENUM — and nothing else.
// It is NOT "undeclared": a kind can be declared in an engine rule YAML's
// `relationship_rules:` and still be absent from the Go enum. DECORATES is
// exactly that case (internal/engine/rules/python/frameworks/fastapi.yaml:137),
// and it shows up in this report. Arm B scans BOTH mechanisms for that reason;
// this counter deliberately does not, because the union is not reachable from
// here (internal/engine imports internal/graph, so fbwriter cannot import the
// rule loader) and loading YAML per write would be the wrong trade for a hot
// path. The names below say "not in enum" so nobody reads more into the number
// than it supports.

// NonEnumKindListCap bounds the number of distinct kind NAMES carried in a
// NonEnumKindReport. The counts (Edges, DistinctKinds) are never capped, so a
// truncated list still reports the true size of the problem. Mirrors the
// internal/secrets ScanResult.Unread shape landed in #6752.
const NonEnumKindListCap = 32

// NonEnumKind is one distinct relationship kind that was written to the graph
// without appearing in types.AllRelationshipKinds().
type NonEnumKind struct {
	// Kind is the kind string exactly as it was serialized.
	Kind string
	// Edges is how many relationships carried it.
	Edges int
}

// NonEnumKindReport is what one write path observed. A bare count says
// something is wrong; the names say what, which is the whole point of the arm.
type NonEnumKindReport struct {
	// Scanned records that a write path actually ran this tally. It is the
	// difference between "counted, found nothing" and "never counted", and
	// keeping those two apart is the entire reason a counting arm exists
	// rather than a dropping one: #6534 was a scanner reporting a repo clean
	// that it had read zero bytes of. A zero-valued report is the SECOND
	// state, not the first, and Clean() will not call it clean.
	Scanned bool
	// Edges is the total number of relationships written whose kind is not in
	// the Go enum. Never capped.
	Edges int
	// DistinctKinds is the number of distinct such kinds. Never capped — it
	// may exceed len(Kinds).
	DistinctKinds int
	// Kinds lists them, busiest first then by name, truncated to
	// NonEnumKindListCap entries.
	Kinds []NonEnumKind
}

// Clean reports that a write path ran this tally AND every relationship it
// wrote carried an enum kind. An unscanned report is not clean — it is
// unknown.
func (r NonEnumKindReport) Clean() bool { return r.Scanned && r.Edges == 0 }

// KindNames returns the reported kind names in report order (truncated to the
// cap, like Kinds).
func (r NonEnumKindReport) KindNames() []string {
	out := make([]string, 0, len(r.Kinds))
	for _, k := range r.Kinds {
		out = append(out, k.Kind)
	}
	return out
}

// Summary renders a one-line human/agent-readable report, or "" when there is
// nothing to say (clean, or never scanned). It names the kinds — a count alone
// would only say that something is wrong.
func (r NonEnumKindReport) Summary() string {
	if r.Edges == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d relationship edge(s) across %d kind(s) absent from the relationship-kind enum: %s",
		r.Edges, r.DistinctKinds, strings.Join(r.KindNames(), ", "))
	if r.DistinctKinds > len(r.Kinds) {
		fmt.Fprintf(&b, " (+%d more)", r.DistinctKinds-len(r.Kinds))
	}
	return b.String()
}

// ApplyToSidecar copies this report into the graph-stats.json payload.
//
// It is the SINGLE conversion from report to sidecar: both writers of that
// file (cmd/grafel's full index and internal/extractors' incremental reindex)
// call this rather than each hand-rolling the same eight lines. Two copies
// were two places for the cap to be applied to the wrong field — the counts
// must stay uncapped while only the NAME LIST is truncated, and that property
// is now pinned once, here, instead of drifting between call sites.
//
// Whether the report is fresh is the caller's business: pass a zero-valued
// report and the sidecar records "not scanned", which is what a failed or
// skipped graph write must leave behind.
func (r NonEnumKindReport) ApplyToSidecar(side *graph.GraphStatsSidecar) {
	if side == nil {
		return
	}
	side.RelationshipKindsScanned = r.Scanned
	side.RelationshipEdgesKindNotInEnum = r.Edges
	// DistinctKinds, NOT len(r.Kinds): the list is truncated at
	// NonEnumKindListCap and the count is not. Reading the count off the
	// truncated list would silently under-report every graph with more than
	// NonEnumKindListCap distinct kinds — the report would then be capped in
	// exactly the dimension it exists to measure.
	side.RelationshipDistinctKindsNotInEnum = r.DistinctKinds
	if len(r.Kinds) > 0 {
		kinds := make(map[string]int, len(r.Kinds))
		for _, k := range r.Kinds {
			kinds[k.Kind] = k.Edges
		}
		side.RelationshipKindsNotInEnum = kinds
	}
}

// enumKindSet is types.AllRelationshipKinds() as a lookup set.
// IsValidRelationshipKind is a linear scan that rebuilds the whole slice per
// call — fine for its (previously nonexistent) callers, unusable per edge on
// the write path. Built exactly once.
var enumKindSet = sync.OnceValue(func() map[string]struct{} {
	all := types.AllRelationshipKinds()
	set := make(map[string]struct{}, len(all))
	for _, k := range all {
		set[string(k)] = struct{}{}
	}
	return set
})

// nonEnumKindTally accumulates the non-enum kinds seen by one write.
// A nil *nonEnumKindTally is a no-op observer, which is how the bounded probe
// in graphFitsSingleBuilder opts out of being counted.
//
// Not safe for concurrent use — neither is the StreamingWriter that owns it.
type nonEnumKindTally struct {
	edges  int
	counts map[string]int
}

// observe records kind if — and only if — it is absent from the enum. Counting
// every relationship instead would make the report useless: the number would
// just track graph size and could never say anything is wrong.
func (t *nonEnumKindTally) observe(kind string) {
	if t == nil {
		return
	}
	if _, inEnum := enumKindSet()[kind]; inEnum {
		return
	}
	t.edges++
	if t.counts == nil {
		t.counts = make(map[string]int)
	}
	t.counts[kind]++
}

// report renders the tally. A nil tally reports Scanned=false — no write path
// ran it, which is NOT the same as having found nothing.
func (t *nonEnumKindTally) report() NonEnumKindReport {
	if t == nil {
		return NonEnumKindReport{}
	}
	rep := NonEnumKindReport{Scanned: true, Edges: t.edges, DistinctKinds: len(t.counts)}
	for k, n := range t.counts {
		rep.Kinds = append(rep.Kinds, NonEnumKind{Kind: k, Edges: n})
	}
	sort.Slice(rep.Kinds, func(i, j int) bool {
		if rep.Kinds[i].Edges != rep.Kinds[j].Edges {
			return rep.Kinds[i].Edges > rep.Kinds[j].Edges
		}
		return rep.Kinds[i].Kind < rep.Kinds[j].Kind
	})
	if len(rep.Kinds) > NonEnumKindListCap {
		rep.Kinds = rep.Kinds[:NonEnumKindListCap]
	}
	return rep
}
