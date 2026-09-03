package types

// derived_kinds.go — the DERIVED (statistical) relationship-kind vocabulary.
//
// # Why a second vocabulary exists (#6773, decided)
//
// #6757 arm C counted relationship kinds at the FlatBuffers write path and
// found that 27,407 of 27,645 edges whose kind the Go enum does not list —
// 99.1% — were COMMIT_COUPLED, all from one emit site
// (internal/engine/commit_coupling_edges.go). Arm B's static ledger had ranked
// that site as one row among 22: a count of source sites was never a count of
// the population.
//
// Simply adding it to AllRelationshipKinds() was rejected. Every kind in that
// vocabulary is a STRUCTURAL FACT an extractor observed in the source — a call,
// an import, a route, a schema reference. COMMIT_COUPLED is not observed in the
// source at all: it is a STATISTICAL SIGNAL computed from git history, an
// inference that two files tend to change together, with a support and a
// confidence attached. Both belong in the graph; they do not answer the same
// kind of question, and a consumer must be able to take one without the other.
//
// So the vocabulary is split rather than widened. The two sets are DISJOINT,
// and that is enforced in both directions
// (derived_kinds_6773_test.go: TestDerivedAndStructuralVocabulariesAreDisjoint)
// so neither list can quietly absorb the other:
//
//   - IsValidRelationshipKind — structural only. Unchanged by #6773, so
//     nothing that already asks "is this one of the kinds the extractors
//     produce?" changed its answer.
//   - IsDerivedRelationshipKind — derived only.
//   - IsDeclaredRelationshipKind — the union. #6757 arm B's static ledger
//     (internal/relkinds) CALLS it. Arm C's write-path counter
//     (internal/graph/fbwriter) does NOT: it is per-edge and hot, and this
//     predicate is a linear scan that rebuilds both slices per call, so the
//     counter builds lookup sets from the same two accessors instead. That
//     the two classifications agree is not left to construction — fbwriter's
//     TestWritePathClassificationMatchesTheTypesPredicates asserts the write
//     path's verdict against these predicates for every kind in both
//     vocabularies.
//
// A kind being declared here is NOT a licence for the counter to stop
// reporting it: fbwriter reports the derived population under its own separate
// count, because the number dropping was never the point of the measurement.

// RelationshipKindCommitCoupled is the co-change edge emitted by the
// commit-coupling pass (internal/engine/commit_coupling_edges.go, which
// defines its KindCommitCoupled as this constant so the two cannot drift —
// pinned by internal/engine's commit_coupling_derived_kind_6773_test.go).
//
// It is DERIVED: it records that two files appeared in the same commit at
// least `support` times, not that either file references the other.
const RelationshipKindCommitCoupled RelationshipKind = "COMMIT_COUPLED"

// AllDerivedRelationshipKinds returns every derived (statistical)
// relationship kind producers may emit.
//
// These are deliberately NOT in AllRelationshipKinds(): see the file comment
// above. A consumer that wants the structural graph alone uses that accessor
// and is unaffected by anything added here; one that wants the whole graph
// uses IsDeclaredRelationshipKind or ranges over both.
//
// A fresh slice per call — the caller may sort or truncate it without
// rewriting the vocabulary for everyone else.
func AllDerivedRelationshipKinds() []RelationshipKind {
	return []RelationshipKind{
		// #6773 — 99.1% of the edges #6757 arm C found outside the enum.
		RelationshipKindCommitCoupled,
	}
}

// IsDerivedRelationshipKind reports whether s is one of the derived
// (statistical) relationship kinds. It is false for every structural kind.
func IsDerivedRelationshipKind(s string) bool {
	for _, k := range AllDerivedRelationshipKinds() {
		if string(k) == s {
			return true
		}
	}
	return false
}

// IsDeclaredRelationshipKind reports whether s belongs to EITHER relationship
// vocabulary — structural or derived.
//
// This is the predicate for "does the graph's vocabulary know this kind at
// all", as opposed to IsValidRelationshipKind's narrower "is this a structural
// kind". Callers that classify unknown kinds (#6757 arm B's ledger, arm C's
// write-path counter) use this one, so that a kind declared in either place is
// declared for both of them.
func IsDeclaredRelationshipKind(s string) bool {
	return IsValidRelationshipKind(s) || IsDerivedRelationshipKind(s)
}
