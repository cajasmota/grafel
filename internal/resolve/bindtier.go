package resolve

import (
	"fmt"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// BindTier names the resolver tier that CHOSE a relationship's ToID, for
// the tiers that chose it by lexical guess rather than by evidence.
//
// Issue #7071. A "guess tier" is one where the target was selected from a
// bare name plus a locality — same file, same package directory, same
// crate, or "the only entity by that name anywhere in the graph" — with no
// type information from the extractor participating in the decision. The
// extractor emitted a bare name precisely because it could not type the
// receiver; these tiers then supply a target from lexical proximity alone.
//
// An "evidence tier" is one where the extractor minted the address the
// resolver matched on: an exact QualifiedName hit (refs.go:2227, :2292), a
// structural ref (lookupStructural, refs.go:2759), a stamped and
// unambiguous receiver type (refs.go:7329), a resolved import qualifier
// (imports.go:2280, :2344, :2551, :2648). Those are NOT marked. Marking
// them would defame them, and marking at the two funnels every tier exits
// through (refs.go's `r.ToID = newID` after rewriteOneWithCaller, and the
// same line on the caller-context-free References path) does exactly that
// while ALSO missing the four per-shape tiers below, which never pass
// through either funnel. The tier is therefore decided at the deciding
// site and carried out to the write site.
type BindTier string

const (
	// BindTierGlobalName — refs.go LookupStatusHint, `idx.byName[lookupName]`.
	// The name is unique ANYWHERE IN THE GRAPH: no file, no package, no kind
	// participated. Structurally the FIRST tier any lookup reaches and the
	// only one that needs no caller context at all, so it is reachable from
	// every call site including the References path. Measured on two real
	// corpora it is also by far the largest population (django 27,903 of
	// 32,003; grpc-go-examples 555 of 1,516).
	//
	// KNOWN COARSENESS — READ BEFORE CONSUMING THIS VALUE. byName is keyed on
	// the entity's Name, and the STUB it is matched against may be a bare
	// leaf (`Touch`) or a dotted name the extractor BUILT from a receiver type
	// it resolved (`AuditEntry.Touch`). Both land here, and this tier does not
	// tell them apart. The second population is not a lexical guess in the
	// sense #7071 describes: the type evidence is inside the string.
	//
	// Measured, not assumed: an `AuditEntry.Touch` stub resolving to the
	// entity of that Name is marked `global-name` today, and the
	// csharp-aspnet-core-mini row #7068 added for exactly that edge is
	// therefore rejected by a `to_must_be_typed` narrowing that should accept
	// it. Splitting this tier on whether the stub contains a separator is the
	// obvious repair; it is deliberately NOT done here because #7071's
	// grounding fixed the tier list at twelve and changing it is a scope
	// decision, not an implementation detail. Until it is split, treat a
	// `global-name` mark as "bound on a name alone", which is true, and NOT
	// as "bound with no type information", which is true only of its bare-leaf
	// half.
	BindTierGlobalName BindTier = "global-name"

	// BindTierGlobalKindFamily — refs.go LookupStatusHint → lookupByKindHint
	// → uniqueMatchInFamily. The bare name is ambiguous graph-wide but unique
	// within the entity-kind family the relationship kind biases toward. The
	// narrowing evidence is the EDGE's kind, never the target's type.
	BindTierGlobalKindFamily BindTier = "global-kind-family"

	// BindTierFileKind — lookupBareWithLocality tier 1,
	// byLocationKind[callerFile][name].real, kind-filtered over the relKind
	// family. Same-file real entity.
	BindTierFileKind BindTier = "file-kind"

	// BindTierPackageOperation — lookupBareWithLocality,
	// byPackageOperation[callerPkgDir][name] on a CALLS-shaped relKind. Same
	// directory, dot-free operation name.
	BindTierPackageOperation BindTier = "package-operation"

	// BindTierFileLeafName — lookupBareWithLocality →
	// lookupMemberByLeafName(callerFile, …). Suffix match: an entity in the
	// caller's file whose dotted Name ENDS in the bare stub. This is the tier
	// #7071's end-to-end reproduction runs through — the one that rewrote a
	// bare `Touch` to `AuditEntry.Touch` because it was the only same-named
	// method in the file, laundering an untyped guess into exactly the target
	// the golden fixture was asserting.
	BindTierFileLeafName BindTier = "file-leaf-name"

	// BindTierPackageLeafName — lookupBareWithLocality →
	// lookupPackageMemberByLeafName(callerPkgDir, …). Same suffix match,
	// widened from the caller's file to the caller's directory.
	BindTierPackageLeafName BindTier = "package-leaf-name"

	// BindTierPackageComponent — lookupBareWithLocality,
	// byPackageComponent[callerPkgDir][name] on EXTENDS / IMPLEMENTS.
	BindTierPackageComponent BindTier = "package-component"

	// BindTierFileScopePlaceholder — lookupBareWithLocality's last resort:
	// byLocationKind[callerFile][name].base under SCOPE.Operation or
	// SCOPE.Component, CALLS only. Worth calling out separately from the
	// others because the target is not a declaration at all — it is a
	// synthetic placeholder. That is #7056's "admissible-looking carrier"
	// shape in a second, language-agnostic place.
	//
	// The two probes (SCOPE.Operation, then SCOPE.Component) are ONE tier
	// here, not two: they are the same decision — "the only same-file thing
	// wearing this name is a placeholder, bind it anyway" — run against two
	// placeholder kinds in a fixed order. A consumer that wants to tell them
	// apart can read the bound entity's Kind, which is not something the
	// marker has to duplicate.
	BindTierFileScopePlaceholder BindTier = "file-scope-placeholder"

	// BindTierGoInterfaceDispatch — refs.go, issue #614. The edge carries an
	// `interface_dispatch_type` property, and EXACTLY ONE known implementer
	// of that interface has a member by this name. Property-informed, but
	// "exactly one implementer in the graph" is an inference about the corpus,
	// not a proof about the call: a second implementer appearing later
	// changes the answer.
	BindTierGoInterfaceDispatch BindTier = "go-interface-dispatch"

	// BindTierGoAmbiguousReceiverLeaf — refs.go, the #6125 hoist. The
	// receiver type WAS stamped but was ambiguous within the package, so the
	// type evidence is explicitly discarded and the same-file leaf-name scan
	// runs instead. A guess that had evidence available and declined it.
	BindTierGoAmbiguousReceiverLeaf BindTier = "go-ambiguous-receiver-leaf"

	// BindTierGoPackageComponent — refs.go, refs #44. Go DEPENDS_ON /
	// EXTENDS / IMPLEMENTS whose ToID is a bare type name unique within the
	// parent's package directory.
	BindTierGoPackageComponent BindTier = "go-package-component"

	// BindTierRustCrateUniqueMember — imports.go
	// ResolveRustCrossModuleCalls' crate-wide fallback (lookupUniqueMember).
	// Reached only AFTER every candidate directory the import resolver
	// offered has failed; `scope::leaf` is then matched on uniqueness across
	// the WHOLE crate, deliberately outside the offered dirs. Scope-qualified,
	// but crate-wide uniqueness is the same species of guess.
	//
	// The candidate-directory binds in that same pass are NOT marked: those
	// directories came from resolved `use` statements, which is evidence.
	BindTierRustCrateUniqueMember BindTier = "rust-crate-unique-member"
)

// AllBindTiers enumerates every guess tier in a stable order, so a report
// over BindTierCounts is deterministic regardless of map iteration order.
//
// It is also the list a reader should check a new tier against: adding a
// guess site to the resolver without adding its constant here means the
// site is unmeasured, which is the state #7071 exists to end.
var AllBindTiers = []BindTier{
	BindTierGlobalName,
	BindTierGlobalKindFamily,
	BindTierFileKind,
	BindTierPackageOperation,
	BindTierFileLeafName,
	BindTierPackageLeafName,
	BindTierPackageComponent,
	BindTierFileScopePlaceholder,
	BindTierGoInterfaceDispatch,
	BindTierGoAmbiguousReceiverLeaf,
	BindTierGoPackageComponent,
	BindTierRustCrateUniqueMember,
}

// recordBindTier tallies one edge bound by tier t.
//
// It is called from stampBindTier ONLY, so the counter and the property can
// never disagree: BindTierCounts[t] is exactly the number of edges in the
// resolved output carrying types.PropBindTier == string(t). A tier that is
// decided and then discarded (the lookup returned a tier but the caller did
// not take the result) is deliberately not counted — the question this
// counter answers is "how many shipped edges are guesses", not "how many
// times did the code consider guessing".
func (s *Stats) recordBindTier(t BindTier) {
	if t == "" {
		return
	}
	if s.BindTierCounts == nil {
		s.BindTierCounts = make(map[BindTier]int, len(AllBindTiers))
	}
	s.BindTierCounts[t]++
}

// stampBindTier writes the guess marker onto a relationship whose ToID was
// just bound by tier t, and tallies it.
//
// A blank tier is a no-op: that is what every evidence tier hands back, and
// it is what keeps an exact-qualified-name bind unmarked.
func stampBindTier(r *types.RelationshipRecord, t BindTier, stats *Stats) {
	if t == "" || r == nil {
		return
	}
	r.Properties.Set(types.PropBindTier, string(t))
	if stats != nil {
		stats.recordBindTier(t)
	}
}

// FormatBindTiers renders a per-tier tally as a stable, log-friendly line:
// `global-name=41 file-leaf-name=3 total=44`. Tiers that did not fire are
// omitted — twelve zeros is noise, and the absent ones are recoverable from
// AllBindTiers.
//
// Returns "" for an empty or all-zero tally so the caller can decide
// whether to print a line at all.
func FormatBindTiers(counts map[BindTier]int) string {
	if len(counts) == 0 {
		return ""
	}
	var parts []string
	total := 0
	for _, t := range AllBindTiers {
		n := counts[t]
		if n == 0 {
			continue
		}
		total += n
		parts = append(parts, fmt.Sprintf("%s=%d", t, n))
	}
	if len(parts) == 0 {
		return ""
	}
	parts = append(parts, fmt.Sprintf("total=%d", total))
	return strings.Join(parts, " ")
}

// MergeBindTiers folds src's per-tier counters into dst. Mirrors
// MergeDispositions, which the same callers use for the disposition maps.
func MergeBindTiers(dst, src *Stats) {
	if dst == nil || src == nil || len(src.BindTierCounts) == 0 {
		return
	}
	if dst.BindTierCounts == nil {
		dst.BindTierCounts = make(map[BindTier]int, len(AllBindTiers))
	}
	for t, n := range src.BindTierCounts {
		dst.BindTierCounts[t] += n
	}
}
