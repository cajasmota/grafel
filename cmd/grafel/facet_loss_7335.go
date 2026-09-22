package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #7335 — make the #4406 dedup's LOSSES observable.
//
// buildDocument keys assembly on graph.EntityID(repoTag, Kind, Name,
// SourceFile), which is blind to BOTH Subtype and StartLine, and takes the
// first writer. The loser is not a duplicate anybody can find in the output —
// it is a MISSING entity, and the absence of duplicate IDs in graph.json is
// currently read as evidence of health when it is produced by the very
// mechanism that hides the problem. #6440 measured one such collision
// destroying a node AND minting a phantom self-CALLS edge; it took a
// hand-written quality fixture to see it, years after the mechanism shipped.
//
// This file is the instrument that would have surfaced it: a collision whose
// dropped record carried a facet the survivor cannot absorb is counted and
// attributed (repo, id, kind, name, source file, field, both values).
//
// WHY A DIAGNOSTIC AND NOT AN IDENTITY CHANGE. Putting Subtype/StartLine into
// graph.EntityID or EntityRecord.ComputeID moves every id in every graph,
// forcing an fbversion bump plus a global cross-language reindex. The project
// has twice chosen producer-side disambiguation over exactly that (#6440's
// VB.NET discriminator — explicitly "option B" for this reason — and #7327's
// scala `dto:` prefix). This arm deliberately changes NO identity and NO
// output: it only reports.
//
// # THE CLASSIFICATION, AND WHY IT IS DERIVED RATHER THAN ROSTERED
//
// Most drops at this site are BY DESIGN and already repaired. A diagnostic
// that fires on all of them is dominated by noise, gets ignored, and is worse
// than nothing because it reads as "we looked". So the question is which drops
// lose something nobody accounted for.
//
// Production already answers that question, in code that executes — the
// gap-fill chain in buildDocument's dedup branch (and its port,
// internal/extractors/incremental.go's foldDuplicateEntity). Its SCALAR
// clauses have the form
//
//	if surv.F == "" && r.F != "" { surv.F = r.F }
//
// which is a standing declaration: "a dropped record's F is RECOVERABLE onto
// the survivor exactly when the survivor left F empty." The complement of that
// declaration — survivor non-empty, dropped non-empty, values differ — is the
// set of facets the repair machinery provably cannot carry across. That
// complement, OVER THOSE SIX SCALARS, is what facetLossConflicts computes.
// There is no roster: add a scalar gap-fill clause and this detector narrows
// with it; delete one and it widens. (A hand-maintained allowlist of
// "known-fine pairs" is the #6887 defect and has bitten this repo repeatedly —
// hence, explicitly, none here.)
//
// SCOPE, STATED ONCE AND MEANT EVERYWHERE. This detector covers the SIX SCALAR
// FIELDS ONLY: qualified_name, subtype, signature, language, start_line,
// end_line. The gap-fill chain does not stop there — Properties and Metadata
// are gap-filled too, by KEY PRESENCE:
//
//	if _, exists := surv.PropLookup(pk); !exists { surv.PropSet(pk, pv) }
//	if _, exists := surv.Metadata[mk]; !exists  { surv.Metadata[mk] = mv }
//
// That is a different shape, and its complement is a different computation: a
// dropped record whose Properties["route"] DIFFERS from the survivor's is a
// facet this repair cannot carry either, and facetLossConflicts does not look
// at it. So a pair differing ONLY in a property or metadata key is INVISIBLE
// to this instrument. The `consumes_api`-vs-file pair below is caught only
// because those two records also differ in the scalar `subtype`. Extending the
// detector to key-presence gap-fill is a separate arm with its own grading; it
// is deliberately not attempted here, and it is recorded as a known blind spot
// rather than implied away.
//
// This classification is evaluated AFTER the #6275 ormlink-sentinel repair has
// blanked the sentinel's synthetic Subtype/QualifiedName, so a
// sentinel-vs-real-class pair presents as survivor-empty — recoverable — and
// does NOT fire. That is not a special case written for ormlink: the repair
// path runs, the survivor's field is empty when this looks, and the general
// rule reports nothing. Drops repaired BEFORE the dedup are invisible here for
// an even simpler reason — foldClassHierarchyShadows drops the #1613 shadow and
// re-homes its edges before the assembly loop, so the pair never collides at
// this site at all.
//
// THE INCREMENTAL PORT IS NOT INSTRUMENTED, AND THE REASON COVERS ONLY HALF
// THE FIELDS. internal/extractors/incremental.go's foldDuplicateEntity is a
// documented port of this dedup. Nothing here breaks the "must stay in step"
// contract — this adds no field to identity and changes no fold semantics, so
// both paths still produce byte-identical entities. But the deferral reason is
// narrower than it looks: on that path a same-id pair is usually
// existing-vs-re-extracted (ONE entity across two indexes), so a start_line /
// end_line difference there is a FILE EDIT, not a collision, and this rule
// would report every touched line as a destroyed facet. That argument does NOT
// cover subtype/signature/qualified_name/language: two producers colliding
// inside a single re-extracted file — the rust struct-vs-impl shape measured
// firing here — destroys a facet on the incremental path identically, and
// foldDuplicateEntity is blind to it while this path reports it. Those four
// scalar directions are DEFERRED, not inapplicable.
//
// KNOWN CONTAMINANT OF THE CONFLICT COUNT. Some signature conflicts are
// producer punctuation, not a lost facet: on scala-play-mini 3 of 19 conflicts
// are a pair differing only by a trailing " =". That inflates Conflicts (not
// LostRecords or LostEntities, which count things rather than fields). It is
// recorded, not normalised, here — normalising is a producer-side decision and
// silently suppressing a difference is how the original defect was built.
//
// WHAT THIS DOES *NOT* EXCUSE. Shadow-vs-SHADOW (cross/hierarchy emitting an
// `extends` parent as subtype "class" and an `implements` interface as
// "interface", same name, same path, both line 0) is NOT prevented by the
// #1613 fold, and it fires here. That is correct: nothing accounts for that
// loss today.

// facetLossRecord attributes one unrecoverable facet loss to the pair that
// caused it. Every field here is needed to act on the report: the repo and
// source file locate it, kind+name identify the colliding producers' output,
// and Survivor/Dropped name the two facets so the reader can tell which
// producer to disambiguate.
type facetLossRecord struct {
	Repo       string
	EntityID   string
	Kind       string
	Name       string
	SourceFile string
	Field      string
	Survivor   string
	Dropped    string
}

// facetLossStats accumulates one buildDocument run's dedup losses.
//
// FOUR NUMBERS, BECAUSE ONE NUMBER LIES. An instrument whose purpose is making
// a loss *counted* must be able to say how many things were lost, and a
// field-conflict count is not that number: one destroyed swift `extension`
// raises three conflicts (subtype, start_line, end_line), and one Rust struct
// colliding with two separate `impl` blocks destroys TWO records under ONE
// entity id. So:
//
//	Drops        — every record the dedup dropped, by-design ones included.
//	               This is the denominator, and it is what tells a silent
//	               fixture apart from a fixture that never collided at all.
//	LostRecords  — dropped records carrying at least one unrecoverable facet.
//	               THIS IS THE COUNT OF DESTROYED THINGS (rust store.rs: 2).
//	LostEntities — distinct entity ids affected (rust store.rs: 1).
//	Conflicts    — field-level conflicts, the finest grain (rust store.rs: 8).
//
// Records holds the first facetLossSampleCap attributions. A count on its own
// is not the instrument — a count floor is satisfied by the wrong answers, and
// the thing to act on is WHICH pair collided.
type facetLossStats struct {
	Drops        int
	LostRecords  int
	LostEntities int
	Conflicts    int
	ByField      map[string]int
	Records      []facetLossRecord

	// lostIDs backs LostEntities. It holds only ids that actually lost a
	// facet — the population this instrument exists to report — so it is
	// bounded by the thing being reported rather than by the entity set.
	lostIDs map[string]struct{}
}

// facetLossSampleCap bounds the retained attribution. A corpus-wide index can
// hold hundreds of thousands of entities; the counters stay exact while the
// per-pair detail is capped so the report cannot become the memory story.
const facetLossSampleCap = 64

// facetLossFields is the ORDER conflicts are reported in, not their
// membership: membership is fixed by the SCALAR half of the gap-fill chain
// this mirrors, and any change to that chain must be mirrored here or the
// detector stops being its complement.
var facetLossFields = []string{"qualified_name", "subtype", "signature", "language", "start_line", "end_line"}

// facetLossConflicts returns the SCALAR fields where the survivor and the
// record about to be dropped BOTH carry a value and the values differ — i.e.
// the facets the scalar gap-fill provably cannot transfer, because it only
// ever writes into an empty survivor field.
//
// It does NOT inspect Properties or Metadata; see the SCOPE paragraph in this
// file's header for why, and for what that leaves invisible.
//
// It must be called with the survivor in the state the gap-fill will see it:
// after the #6275 sentinel blanking, before the fills themselves. (Calling it
// after the fills would give the same verdict — a fill only ever touches a
// field this function already judged non-conflicting — but "before" is the
// statement of intent.)
func facetLossConflicts(surv *graph.Entity, r *types.EntityRecord) []facetLossRecord {
	var out []facetLossRecord
	add := func(field, a, b string) {
		out = append(out, facetLossRecord{Field: field, Survivor: a, Dropped: b})
	}
	conflictStr := func(field, a, b string) {
		if a != "" && b != "" && a != b {
			add(field, a, b)
		}
	}
	conflictInt := func(field string, a, b int) {
		if a != 0 && b != 0 && a != b {
			add(field, strconv.Itoa(a), strconv.Itoa(b))
		}
	}
	conflictStr("qualified_name", surv.QualifiedName, r.QualifiedName)
	conflictStr("subtype", surv.Subtype, r.Subtype)
	conflictStr("signature", surv.Signature, r.Signature)
	conflictStr("language", surv.Language, r.Language)
	conflictInt("start_line", surv.StartLine, r.StartLine)
	conflictInt("end_line", surv.EndLine, r.EndLine)
	return out
}

// observe records ONE dedup drop: its conflicts if it lost anything, and the
// drop itself either way. Counting every drop — not only the lossy ones — is
// what lets a caller tell "this index had nothing to lose here" apart from
// "this detector found nothing", which are the two readings a bare zero cannot
// separate.
func (s *facetLossStats) observe(repo, id string, r *types.EntityRecord, conflicts []facetLossRecord) {
	s.Drops++
	if len(conflicts) == 0 {
		return
	}
	s.LostRecords++
	if s.lostIDs == nil {
		s.lostIDs = map[string]struct{}{}
	}
	if _, seen := s.lostIDs[id]; !seen {
		s.lostIDs[id] = struct{}{}
		s.LostEntities++
	}
	if s.ByField == nil {
		s.ByField = map[string]int{}
	}
	for _, c := range conflicts {
		s.Conflicts++
		s.ByField[c.Field]++
		if len(s.Records) >= facetLossSampleCap {
			continue
		}
		c.Repo = repo
		c.EntityID = id
		c.Kind = r.Kind
		c.Name = r.Name
		c.SourceFile = r.SourceFile
		s.Records = append(s.Records, c)
	}
}

// report writes the diagnostic. It writes NOTHING when no unrecoverable loss
// was seen: an unconditional status line reporting zero is indistinguishable
// from a diagnostic that never ran, and this one is only worth reading when it
// has something to say.
func (s *facetLossStats) report(w io.Writer) {
	if s.Conflicts == 0 {
		return
	}
	fields := make([]string, 0, len(s.ByField))
	for f := range s.ByField {
		fields = append(fields, f)
	}
	sort.Slice(fields, func(a, b int) bool {
		return facetLossRank(fields[a]) < facetLossRank(fields[b])
	})
	line := ""
	for _, f := range fields {
		line += fmt.Sprintf(" %s=%d", f, s.ByField[f])
	}
	fmt.Fprintf(w,
		"assembly-facet-loss: drops=%d lost_records=%d lost_entities=%d conflicts=%d attributed=%d by_field:%s\n",
		s.Drops, s.LostRecords, s.LostEntities, s.Conflicts, len(s.Records), line)
	for _, r := range s.Records {
		fmt.Fprintf(w,
			"assembly-facet-loss:   repo=%s id=%s kind=%s name=%s file=%s field=%s survivor=%q dropped=%q\n",
			r.Repo, r.EntityID, r.Kind, r.Name, r.SourceFile, r.Field, r.Survivor, r.Dropped)
	}
}

func facetLossRank(field string) int {
	for i, f := range facetLossFields {
		if f == field {
			return i
		}
	}
	return len(facetLossFields)
}
