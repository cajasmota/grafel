package extractors

import (
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
)

// prior-resolution replay (#6090)
//
// # The problem
//
// TryIncremental re-extracts a changed file ALONE and then binds its edges with
// sresolver.ResolveScoped, whose ladder (whole-string name / qualified-name,
// then the Format A (file, tail) tiers) is strictly weaker than the corpus-wide
// resolver's. A Go method entity is named "T11.Do11" while the call site emits
// the bare stub "Do11": the full resolver binds it through its member-suffix
// tier, the scoped one leaves it verbatim.
//
// Step 5 of TryIncremental has meanwhile pruned every prior edge whose FromID is
// an entity of the changed file — correct in principle (the re-extraction is
// authoritative for that file's outgoing edges), fatal in this case: the
// resolved edge is gone and its replacement is an unresolved stub. Nothing
// restores it before a full reindex, and because each edited file contributes
// its own losses permanently, the deficit is monotone (measured on a 40-file
// fixture, editing three different files: 10 → 18 → 21 edges missing versus a
// full rebuild of the same end-state).
//
// # Why this is a replay and not a retention
//
// Three cases look alike from inside a single-file extraction, and only the
// middle one may be repaired:
//
//	the fresh pass re-emitted the edge differently  → the fresh version wins
//	the fresh pass could not RESOLVE the target     → replay the prior binding
//	the edge no longer exists in source             → drop it
//
// The separating signal is the fresh pass's own output. This function never
// looks at a prior edge on its own: it starts from an edge the CURRENT
// extraction re-emitted, observes that its ToID is an unresolved bare name, and
// consults the prior graph only to ask "what did the corpus-wide resolver bind
// THIS name to, from THIS source, under THIS kind?".
//
//   - A call deleted from source produces no fresh edge, so there is nothing to
//     bind and the prior edge stays dropped. Retention is structurally
//     impossible — the function only ever mutates the ToID of an edge the fresh
//     pass emitted; it never appends a row.
//   - A call retargeted in source produces a fresh edge naming the NEW target,
//     which does not match the prior binding's name, so nothing is replayed and
//     the stale edge stays dropped.
//   - A call the scoped resolver DID bind arrives with a hex ToID and is skipped
//     outright, so the fresh resolution always wins over the prior one.
//
// The binding replayed is the one a corpus-wide resolver computed over the same
// source — on the first incremental pass that is literally a full rebuild's
// output, and each subsequent pass replays the binding the previous pass
// preserved. It is not an inference about what the target "probably" is.
//
// # Ambiguity
//
// A name is replayed only when the prior graph bound it from this source, under
// this kind, to exactly ONE still-live entity. Two distinct prior targets for
// one name is precisely the case the full resolver itself calls ambiguous and
// leaves unresolved, so the stub is left verbatim — the same thing a full
// rebuild does. When both the prior and the fresh edge carry a `receiver_type`
// property, they must agree; a method call moved to a different receiver type is
// therefore not replayed onto the old receiver's method.
//
// # Known limits — recorded, not fixed
//
// Neither limit below can point an edge at an entity that does not exist, and
// neither can retain a deleted call, so both are precision losses rather than
// soundness holes. They are written down so the next reader does not take the
// guards for stronger than they are.
//
//  1. Dead candidates never enter the index (they have no name keys), which
//     means they are filtered BEFORE ambiguity is counted. A name the prior
//     graph bound to two targets, one of which this run destroyed, therefore
//     looks unambiguous and binds to the survivor. That target is a real, live
//     entity the prior corpus-wide resolver did associate with this name from
//     this source — so the edge is plausible rather than fabricated — but it is
//     a confident answer to a question the full resolver would have declined.
//
//  2. The receiver_type veto is stamp-dependent, and the stamp is emitted by the
//     Go extractor only — and dropped even there when the extractor cannot
//     determine the receiver. The veto is therefore inactive for every non-Go
//     language, and for exactly the Go call sites the extractor was unsure
//     about. The ambiguity guard, which needs no stamp, is the load-bearing one;
//     the veto is a refinement on top of it.
//
// Every guard named above is pinned by a mutation-gate test in
// incremental_prior_resolution_test.go: delete one and exactly one test fails.

// replayPriorResolutionReceiverProp is the extractor-supplied property that
// records the receiver type of a method call ("receiver_type:T11"). It is the
// one call-site discriminator that survives into the edge, and it is used as a
// veto — never as a binder — so extractors that do not emit it are unaffected.
const replayPriorResolutionReceiverProp = "receiver_type"

// replayPriorResolution binds unresolved ToIDs on freshly extracted edges from
// the previous graph's resolution of the same (FromID, Kind, name) triple.
//
// newRels is mutated in place; the return value is the number of endpoints
// bound. priorOutbound is the set of prior edges pruned because their FromID was
// a changed-file entity. survivors and fresh supply the entity rows the prior
// targets are named by — fresh is required as well as survivors because a prior
// target inside the changed file itself was removed from survivors and comes
// back only via re-extraction (entity IDs are deterministic over
// kind/name/source_file, so it returns under the same ID).
func replayPriorResolution(newRels []graph.Relationship, priorOutbound []graph.Relationship, survivors, fresh []graph.Entity) int {
	if len(newRels) == 0 || len(priorOutbound) == 0 {
		return 0
	}

	// Only edges with an unresolved endpoint are candidates. Bail out before
	// touching the entity vectors when there are none — the common case on a
	// corpus whose edges all bind, and survivors can be very large.
	anyUnresolved := false
	for i := range newRels {
		if !replayIsHexID(newRels[i].ToID) {
			anyUnresolved = true
			break
		}
	}
	if !anyUnresolved {
		return 0
	}

	// The prior targets we need entity names for: hex ToIDs of pruned outbound
	// edges. Typically a handful, so this bounds the entity scan's output.
	wanted := make(map[string]bool, len(priorOutbound))
	for i := range priorOutbound {
		if replayIsHexID(priorOutbound[i].ToID) {
			wanted[priorOutbound[i].ToID] = true
		}
	}
	if len(wanted) == 0 {
		return 0
	}

	// live[id] holds the name keys of a still-present prior target. An id absent
	// here either never existed or was destroyed by this run's re-extraction —
	// either way its prior binding must not be replayed onto a live edge.
	live := make(map[string][]string, len(wanted))
	collect := func(ents []graph.Entity) {
		for i := range ents {
			if !wanted[ents[i].ID] {
				continue
			}
			if _, seen := live[ents[i].ID]; seen {
				continue
			}
			live[ents[i].ID] = replayNameKeys(&ents[i])
		}
	}
	collect(survivors)
	collect(fresh)
	if len(live) == 0 {
		return 0
	}

	// Index: (FromID, Kind, name key) → the prior edges that bound that name.
	index := make(map[string][]int, len(priorOutbound))
	for i := range priorOutbound {
		keys, ok := live[priorOutbound[i].ToID]
		if !ok {
			continue
		}
		for _, nk := range keys {
			k := replayKey(priorOutbound[i].FromID, priorOutbound[i].Kind, nk)
			index[k] = append(index[k], i)
		}
	}
	if len(index) == 0 {
		return 0
	}

	bound := 0
	for i := range newRels {
		r := &newRels[i]
		if replayIsHexID(r.ToID) {
			continue
		}
		target, ok := replayLookup(index, priorOutbound, r)
		if !ok {
			continue
		}
		r.ToID = target
		// Recomputing the ID from the triple is safe ONLY because newRels is
		// structurally salt-free: it is built by convertExtractedRecords from
		// types.RelationshipRecord, which has no ID field, so every row here
		// already carries the derived RelationshipID and nothing distinguishes
		// two rows sharing a triple. That is NOT true of the graph at large —
		// several producers deliberately mint salted IDs for edges sharing one
		// triple (see graph.RelationshipIDProperty), and #6085 converged on data
		// loss by assuming the triple was a key. If a salted producer ever feeds
		// this path, this line silently collapses those siblings: carry the
		// prior row's ID forward instead of deriving one.
		r.ID = graph.RelationshipID(r.FromID, r.ToID, r.Kind)
		bound++
	}
	return bound
}

// replayLookup resolves one unresolved edge against the prior-binding index,
// returning the single live target the previous graph bound this name to from
// this source under this kind. ok is false when the name was never bound, or
// when the prior graph bound it to more than one target (the ambiguous case a
// full rebuild also leaves unresolved).
func replayLookup(index map[string][]int, priorOutbound []graph.Relationship, r *graph.Relationship) (string, bool) {
	freshRecv := r.PropGet(replayPriorResolutionReceiverProp)
	for _, name := range replayStubNames(r.ToID) {
		idxs, ok := index[replayKey(r.FromID, r.Kind, name)]
		if !ok {
			continue
		}
		target := ""
		for _, pi := range idxs {
			p := &priorOutbound[pi]
			// receiver_type veto: when both sides record one they must agree,
			// so a call moved to a different receiver is not replayed onto the
			// old receiver's method.
			if freshRecv != "" {
				if pr := p.PropGet(replayPriorResolutionReceiverProp); pr != "" && pr != freshRecv {
					continue
				}
			}
			if target == "" {
				target = p.ToID
				continue
			}
			if target != p.ToID {
				return "", false // ambiguous — leave the stub verbatim
			}
		}
		if target != "" {
			return target, true
		}
	}
	return "", false
}

// replayKey builds the index key. The separator is NUL so a name containing the
// delimiter cannot collide with a different (from, kind) pair.
func replayKey(fromID, kind, name string) string {
	var b strings.Builder
	b.Grow(len(fromID) + len(kind) + len(name) + 2)
	b.WriteString(fromID)
	b.WriteByte(0)
	b.WriteString(kind)
	b.WriteByte(0)
	b.WriteString(name)
	return b.String()
}

// replayNameKeys returns every name an unresolved stub could plausibly spell for
// this entity: its Name and QualifiedName, plus the trailing segment of each
// (the bare-member form a call site emits — entity "T11.Do11", stub "Do11").
func replayNameKeys(e *graph.Entity) []string {
	keys := make([]string, 0, 4)
	add := func(s string) {
		if s == "" {
			return
		}
		for _, k := range keys {
			if k == s {
				return
			}
		}
		keys = append(keys, s)
	}
	add(e.Name)
	add(replayTail(e.Name))
	add(e.QualifiedName)
	add(replayTail(e.QualifiedName))
	return keys
}

// replayStubNames returns the names an unresolved endpoint may be spelling: the
// stub verbatim, its trailing segment, and — for a Format A structural ref
// (`scope:<kind>:<subtype>:<lang>:<file>:<name>`) — the ref's tail. Ordered most
// specific first so a whole-string match wins over a suffix match.
func replayStubNames(stub string) []string {
	names := make([]string, 0, 3)
	add := func(s string) {
		if s == "" {
			return
		}
		for _, n := range names {
			if n == s {
				return
			}
		}
		names = append(names, s)
	}
	add(stub)
	if strings.HasPrefix(stub, "scope:") {
		if parts := strings.SplitN(stub, ":", 6); len(parts) == 6 {
			add(parts[5])
			add(replayTail(parts[5]))
		}
	}
	add(replayTail(stub))
	return names
}

// replayTail returns the segment after the last member/path separator, or "" if
// there is none (so a bare name does not index itself twice).
func replayTail(s string) string {
	if i := strings.LastIndexAny(s, ".#/"); i >= 0 && i+1 < len(s) {
		return s[i+1:]
	}
	return ""
}

// replayIsHexID reports whether s is a resolved entity id — the 16-char
// lowercase hex form graph.EntityID produces. Anything else is an unresolved
// stub. Mirrors sresolver.isHexID, which is unexported there.
func replayIsHexID(s string) bool {
	if len(s) != 16 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
