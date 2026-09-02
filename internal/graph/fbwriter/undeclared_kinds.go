package fbwriter

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6757 arm C — count and report undeclared relationship kinds at the
// write path.
//
// types.IsValidRelationshipKind had ZERO non-test callers, so the relationship
// vocabulary was enforced by nothing: AllRelationshipKinds() described an
// intention, not a constraint. Arm B's static ledger proved 22 kinds are
// written to the graph without being declared — but 87 relationship-kind
// fields repo-wide resolve to a RUNTIME value rather than a source constant
// (custom/java/caching.go, cmd/grafel/index.go, the cobol and jcl extractors),
// and no static scan can see those. 22 is a floor, not the population.
//
// The only place the population is visible is where the value exists: the
// serialization leaf. buildRelationship is the sole one — StreamingWriter.
// WriteRelationship, WriteGraphGenSegmented and the segment path all funnel
// through it, and nothing builds a relationship FlatBuffer outside it.
//
// But it is per-edge, hot, and returns no error, so it CANNOT reject. This
// follows readDirBounded (internal/daemon/state_path.go:283) instead: work it
// cannot complete is abandoned but ADMITTED, never silently forgotten. Nothing
// is dropped and nothing fails; the tally makes the true population
// inspectable for the first time, so a later arm can decide what to declare
// and what to reject on evidence rather than on a static estimate.

// UndeclaredKindListCap bounds the number of distinct kind NAMES carried in an
// UndeclaredKindReport. The counts (Edges, DistinctKinds) are never capped, so
// a truncated list still reports the true size of the problem. Mirrors the
// internal/secrets ScanResult.Unread shape landed in #6752.
const UndeclaredKindListCap = 32

// UndeclaredKind is one distinct relationship kind that was written to the
// graph without appearing in types.AllRelationshipKinds().
type UndeclaredKind struct {
	// Kind is the kind string exactly as it was serialized.
	Kind string
	// Edges is how many relationships carried it.
	Edges int
}

// UndeclaredKindReport is what one write path observed. A bare count says
// something is wrong; the names say what, which is the whole point of the arm.
type UndeclaredKindReport struct {
	// Edges is the total number of relationships written whose kind is not
	// declared. Never capped.
	Edges int
	// DistinctKinds is the number of distinct undeclared kinds. Never capped —
	// it may exceed len(Kinds).
	DistinctKinds int
	// Kinds lists the distinct undeclared kinds, busiest first then by name,
	// truncated to UndeclaredKindListCap entries.
	Kinds []UndeclaredKind
}

// Clean reports whether every relationship written carried a declared kind.
func (r UndeclaredKindReport) Clean() bool { return r.Edges == 0 }

// KindNames returns the reported kind names in report order (truncated to the
// cap, like Kinds).
func (r UndeclaredKindReport) KindNames() []string {
	out := make([]string, 0, len(r.Kinds))
	for _, k := range r.Kinds {
		out = append(out, k.Kind)
	}
	return out
}

// Summary renders a one-line human/agent-readable report, or "" when clean. It
// names the kinds — a count alone would only say that something is wrong.
func (r UndeclaredKindReport) Summary() string {
	if r.Clean() {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d relationship edge(s) across %d undeclared kind(s): %s",
		r.Edges, r.DistinctKinds, strings.Join(r.KindNames(), ", "))
	if r.DistinctKinds > len(r.Kinds) {
		fmt.Fprintf(&b, " (+%d more)", r.DistinctKinds-len(r.Kinds))
	}
	return b.String()
}

// declaredKindSet is types.AllRelationshipKinds() as a lookup set.
// IsValidRelationshipKind is a linear scan that rebuilds the whole slice per
// call — fine for its (previously nonexistent) callers, unusable per edge on
// the write path. Built exactly once.
var declaredKindSet = sync.OnceValue(func() map[string]struct{} {
	all := types.AllRelationshipKinds()
	set := make(map[string]struct{}, len(all))
	for _, k := range all {
		set[string(k)] = struct{}{}
	}
	return set
})

// undeclaredKindTally accumulates the undeclared kinds seen by one write.
// A nil *undeclaredKindTally is a no-op observer, which is how the bounded
// probe in graphFitsSingleBuilder opts out of being counted.
//
// Not safe for concurrent use — neither is the StreamingWriter that owns it.
type undeclaredKindTally struct {
	edges  int
	counts map[string]int
}

// observe records kind if — and only if — it is not declared. Counting every
// relationship instead would make the report useless: the number would just
// track graph size and could never say anything is wrong.
func (t *undeclaredKindTally) observe(kind string) {
	if t == nil {
		return
	}
	if _, declared := declaredKindSet()[kind]; declared {
		return
	}
	t.edges++
	if t.counts == nil {
		t.counts = make(map[string]int)
	}
	t.counts[kind]++
}

// report renders the tally. A nil tally reports clean, which is honest: it
// observed nothing.
func (t *undeclaredKindTally) report() UndeclaredKindReport {
	if t == nil {
		return UndeclaredKindReport{}
	}
	rep := UndeclaredKindReport{Edges: t.edges, DistinctKinds: len(t.counts)}
	for k, n := range t.counts {
		rep.Kinds = append(rep.Kinds, UndeclaredKind{Kind: k, Edges: n})
	}
	sort.Slice(rep.Kinds, func(i, j int) bool {
		if rep.Kinds[i].Edges != rep.Kinds[j].Edges {
			return rep.Kinds[i].Edges > rep.Kinds[j].Edges
		}
		return rep.Kinds[i].Kind < rep.Kinds[j].Kind
	})
	if len(rep.Kinds) > UndeclaredKindListCap {
		rep.Kinds = rep.Kinds[:UndeclaredKindListCap]
	}
	return rep
}
