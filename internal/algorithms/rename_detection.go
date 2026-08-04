// Package algorithms provides graph-level analysis passes that operate on a
// fully-built graph.Document rather than on raw entity/relationship records.
//
// rename_detection.go — post-rebuild entity rename detection (#1344).
//
// After each index pass a new graph.Document is ready but the old one still
// lives on disk. This pass compares the two snapshots:
//
//   - Entities present in OLD but absent in NEW are treated as candidates for
//     deletion (they may have been renamed).
//   - Entities present in NEW but absent in OLD are treated as candidates for
//     addition (they may be the result of a rename).
//
// For every "added" entity we search the "deleted" set for a match using
// three independent signals:
//
//  1. Same kind (function / method / class / …).
//  2. Similar name: Levenshtein distance < 30 % of max(len(old), len(new)).
//  3. Preserved neighborhood: at least one caller or callee is shared between
//     old and new, OR the files are identical (intra-file rename).
//
// When all three signals agree the new entity receives a RENAMED_FROM edge
// pointing at the old entity's ID.  The edge carries a "confidence" property
// (0.0–1.0) that reflects how strongly the signals agree, a "old_name"
// property with the previous name, and a "method" property describing which
// heuristics fired.
//
// Move detection (file changed, signature identical) is handled as a
// special-case short-circuit before the fuzzy matching: if kind+name match
// exactly but the source file changed, a RENAMED_FROM edge is emitted with
// method="moved" and confidence=1.0.
//
// Split detection (one old entity → two new entities sharing callers) is
// attempted when a deleted entity has more than one new-candidate hit.  If
// two new entities each satisfy the rename heuristic against the same old
// entity, both receive RENAMED_FROM edges with method="split".
//
// The pass is append-only: it never removes or modifies existing entities or
// edges.  It is safe to skip (--skip-pass=rename-detect) without affecting any
// other pass.
//
// # Bounding (#6087)
//
// Phase 2 is pairwise: |deleted| × |added|.  With a largely dissimilar prior
// graph over a large repo (~119k entities observed) that is ~1.4e10 pairs at
// ~0.9 µs each — hours of pinned CPU on an index that otherwise takes 49
// seconds.  Three ceilings now apply, cheapest first:
//
//  1. Kind bucketing — an added entity only ever visits deleted entities of
//     the same Kind (the old code scanned every deleted entity and rejected on
//     Kind inside the loop).
//  2. Name-length banding — nameSimilarity >= 0.65 requires the edit distance
//     to be <= 35 % of the longer name, and edit distance is at least the
//     length difference.  Pairs outside that band are rejected without ever
//     calling levenshtein.
//  3. A hard WORK budget (DefaultRenameWorkBudget), denominated in
//     Levenshtein DP cells rather than in pairs.  A pair budget would be the
//     wrong unit: levenshtein is O(mn) in the name lengths, and grafel entity
//     names are not uniformly short — http_endpoint entities are named
//     "http:DELETE:/invoices/{id}" and a real nested route runs to 60+
//     characters, so 2e6 pairs costs 1 s at 13-char names and 30 s at 90-char
//     names.  Charging actual work keeps the ceiling in wall-clock terms
//     whatever the names look like.  Added entities are visited
//     cheapest-candidate-set-first, then by entity ID, so the cap falls in the
//     same place on every run.  When the budget runs out the pass stops and
//     reports RenameStats.Truncated together with how much work was dropped —
//     it never silently claims "no renames found".
//
// Ordering caveat: cheapest-first maximises the NUMBER of added entities
// examined and guarantees a rare kind is never starved by one enormous
// Function/Method bucket — but it means that when truncation does bite, the
// largest kind is the one sacrificed, and that is where renames are most
// likely to live.  The budget is sized so that truncation only occurs well
// outside the range of a realistic refactor (see DefaultRenameWorkBudget);
// inside that range every kind is examined in full and the ordering has no
// effect on the result.
package algorithms

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
)

// DefaultRenameWorkBudget caps the work Phase 2 may perform in a single
// DetectRenames call, denominated in Levenshtein DP cells.
//
// Accounting: visiting a candidate pair costs 1 unit (the length-band check is
// a pair of integer compares); a pair that survives the band and actually runs
// levenshtein costs an additional len(a)*len(b) units.  Band rejections are
// therefore ~170x cheaper than comparisons at typical name lengths, instead of
// costing the same as they would under a pair budget.
//
// Sizing: measured at 1.9–2.9 ns per DP cell, and the ceiling holds whatever
// the names look like — a 20000x20000 dissimilar delta (4e8 pairs) exhausts
// the budget in 9.1 s, and so does a 1200x1200 delta of 90-char names (7.0 s).
// Against the 3.4-hour worst case this fixes, on an index that already takes
// 49 s, that is the right trade.
//
// Coverage is what drove the number up from an earlier 2e6-PAIR cap: that cap
// returned only 666 of 3000 renames on a 3000-entity same-kind refactor that
// the old unbounded code got fully right in ~6 s — trading a hang for silent
// incompleteness, which is the worse bug.  Measured at 4e9 units: a
// 1500-entity same-kind refactor scores 1500/1500 and a 3000-entity one scores
// 3000/3000, neither truncating; the same-kind ceiling is ~4000 entities at
// typical (13-char) names, below which no realistic refactor truncates.  Above
// it recall degrades (8000 scores 2074/8000) but the run is reported truncated
// on stderr AND in graph-stats.json, so incompleteness is never silent.
const DefaultRenameWorkBudget int64 = 4_000_000_000

// pairVisitCost is what visiting a candidate pair costs before any comparison
// runs — the length-band check. One DP-cell equivalent.
const pairVisitCost = 1

// RelKindRenamedFrom is the edge kind emitted by the rename-detection pass.
// The edge runs from the NEW entity (the post-rename entity) → the OLD entity
// ID.  Consumers that care about history can follow the edge backwards to
// recover old enrichment, findings, and metrics.
const RelKindRenamedFrom = "RENAMED_FROM"

// RenameStats summarises the rename-detection pass output.
type RenameStats struct {
	// Candidates is the number of (deleted, added) entity pairs examined.
	Candidates int
	// Renames is the number of RENAMED_FROM edges emitted (rename + move).
	Renames int
	// Moves is the subset of Renames where only the source file changed.
	Moves int
	// Splits is the number of split events (one old entity → two+ new ones).
	Splits int

	// WorkBudget is the Phase-2 work ceiling this run was given, in
	// Levenshtein DP cells (#6087).
	WorkBudget int64
	// WorkUsed is the work Phase 2 actually performed, in the same units.
	// Always <= WorkBudget.
	WorkUsed int64
	// PairsExamined is the number of (deleted, added) candidate pairs Phase 2
	// visited.
	PairsExamined int
	// PairsPrefiltered is the subset of PairsExamined rejected by the cheap
	// name-length band without running levenshtein.
	PairsPrefiltered int
	// Comparisons is the number of pairs that survived the band and ran a full
	// name comparison. PairsExamined = PairsPrefiltered + Comparisons.
	Comparisons int
	// Truncated reports that the work budget ran out before every added entity
	// had been examined.  Renames may exist that this run did not look for —
	// callers must not read "0 renames" as "no renames exist".
	Truncated bool
	// AddedSkipped is the number of added entities never examined because the
	// budget ran out.  Zero unless Truncated.
	AddedSkipped int
	// PairsSkipped is the number of candidate pairs those skipped entities
	// would have contributed.  Zero unless Truncated.
	PairsSkipped int
}

// DetectRenames compares prevDoc (the last persisted graph) and newDoc (the
// freshly-built graph) and appends RENAMED_FROM edges to newDoc.Relationships.
// prevDoc may be nil (first-ever index, or the caller chose to skip loading
// it); in that case the function is a no-op.
//
// The function is idempotent: running it twice on the same pair produces the
// same set of edges (because both docs are immutable from the caller's
// perspective; only newDoc.Relationships grows).
func DetectRenames(prevDoc, newDoc *graph.Document) RenameStats {
	return DetectRenamesBounded(prevDoc, newDoc, DefaultRenameWorkBudget)
}

// DetectRenamesBounded is DetectRenames with an explicit Phase-2 work budget in
// Levenshtein DP cells (#6087).  A budget <= 0 is treated as
// DefaultRenameWorkBudget; there is no unbounded mode, because an unbounded
// quadratic on the index path is the bug.
//
// What is and is not budgeted:
//
//   - Phase 1 is a (Kind, Name) map probe and is never budgeted, so a pure MOVE
//     — same kind, same name, different file — is always detected in full,
//     however large the delta.
//   - A rename-in-place is NOT a Phase-1 match (the name changed, so the map
//     probe misses) and falls through to budgeted Phase 2.  Renames are
//     therefore subject to truncation; the budget is sized so this only bites
//     outside the range of a realistic refactor, but it is not a guarantee.
//     Check RenameStats.Truncated before reading a rename count as complete.
func DetectRenamesBounded(prevDoc, newDoc *graph.Document, workBudget int64) RenameStats {
	if workBudget <= 0 {
		workBudget = DefaultRenameWorkBudget
	}
	if prevDoc == nil || newDoc == nil {
		return RenameStats{}
	}

	// Build ID sets for fast membership tests.
	prevIDs := make(map[string]struct{}, len(prevDoc.Entities))
	for _, e := range prevDoc.Entities {
		prevIDs[e.ID] = struct{}{}
	}
	newIDs := make(map[string]struct{}, len(newDoc.Entities))
	for _, e := range newDoc.Entities {
		newIDs[e.ID] = struct{}{}
	}

	// Collect the two candidate sets.
	var deleted []graph.Entity // in prev, absent from new
	var added []graph.Entity   // in new, absent from prev

	for _, e := range prevDoc.Entities {
		if _, ok := newIDs[e.ID]; !ok {
			deleted = append(deleted, e)
		}
	}
	for _, e := range newDoc.Entities {
		if _, ok := prevIDs[e.ID]; !ok {
			added = append(added, e)
		}
	}

	if len(deleted) == 0 || len(added) == 0 {
		return RenameStats{}
	}

	// Build neighborhood indices (callers + callees) keyed by entity ID.
	prevNeighbors := buildNeighborIndex(prevDoc.Relationships)
	newNeighbors := buildNeighborIndex(newDoc.Relationships)

	// Existing RENAMED_FROM edges in newDoc — guard against duplicates.
	existingRenames := make(map[string]struct{})
	for _, r := range newDoc.Relationships {
		if r.Kind == RelKindRenamedFrom {
			existingRenames[r.FromID+"\x00"+r.ToID] = struct{}{}
		}
	}

	// For each deleted entity, track which added entities matched it so we
	// can detect splits (one old → multiple new).
	type renameEdge struct {
		fromID     string // new entity
		toID       string // old entity ID
		oldName    string
		confidence float64
		method     string
	}

	// deletedMatchCounts[deletedID] = []renameEdge — accumulate all matches
	// for a single deleted entity so splits can be flagged.
	matchesByDeleted := make(map[string][]renameEdge, len(deleted))

	var stats RenameStats
	stats.Candidates = len(deleted) * len(added)
	stats.WorkBudget = workBudget

	// Phase 1 — exact name+kind, file changed (move detection).
	// Build lookup: (kind, name) → deleted entity for O(1) move probe.
	deletedByKindName := make(map[string]graph.Entity, len(deleted))
	for _, d := range deleted {
		key := d.Kind + "\x00" + d.Name
		deletedByKindName[key] = d
	}

	remainingAdded := make([]graph.Entity, 0, len(added))
	for _, a := range added {
		key := a.Kind + "\x00" + a.Name
		d, ok := deletedByKindName[key]
		if !ok {
			remainingAdded = append(remainingAdded, a)
			continue
		}
		if d.SourceFile == a.SourceFile {
			// Same name, same file, same kind → IDs should be identical (would
			// have matched above). Skip; this is a graph schema change, not a
			// rename.
			remainingAdded = append(remainingAdded, a)
			continue
		}
		// File changed but kind+name identical → MOVE.
		dedupKey := a.ID + "\x00" + d.ID
		if _, dup := existingRenames[dedupKey]; dup {
			continue
		}
		edge := renameEdge{
			fromID:     a.ID,
			toID:       d.ID,
			oldName:    d.Name,
			confidence: 1.0,
			method:     "moved",
		}
		matchesByDeleted[d.ID] = append(matchesByDeleted[d.ID], edge)
	}

	// Phase 2 — fuzzy rename matching for remaining added entities.
	//
	// Bounded (#6087). Candidates are bucketed by Kind so an added entity never
	// scans deleted entities it could not possibly match, each bucket is sorted
	// by ID, and added entities are visited smallest-bucket-first (then by ID).
	// That ordering is deterministic AND cap-friendly: the cheap, high-yield
	// candidates are examined before the budget can be burned by one enormous
	// same-kind bucket.
	byKind := make(map[string][]candidate, 8)
	sumLowLen := make(map[string]int64, 8)
	for _, d := range deleted {
		l := len(strings.ToLower(d.Name))
		byKind[d.Kind] = append(byKind[d.Kind], candidate{ent: d, lowLen: l})
		sumLowLen[d.Kind] += int64(l)
	}
	for kind := range byKind {
		bucket := byKind[kind]
		sort.Slice(bucket, func(i, j int) bool { return bucket[i].ent.ID < bucket[j].ent.ID })
	}

	probes := make([]addedProbe, 0, len(remainingAdded))
	for _, a := range remainingAdded {
		lowLen := len(strings.ToLower(a.Name))
		probes = append(probes, addedProbe{
			ent:     a,
			lowLen:  lowLen,
			bucketN: len(byKind[a.Kind]),
			// Worst-case cost of scanning this entity's whole bucket: one
			// visit per candidate, plus a full levenshtein against every one
			// of them. Computed in O(1) from the per-kind length sum.
			maxCost: int64(len(byKind[a.Kind]))*pairVisitCost + int64(lowLen)*sumLowLen[a.Kind],
		})
	}
	sort.Slice(probes, func(i, j int) bool {
		if probes[i].maxCost != probes[j].maxCost {
			return probes[i].maxCost < probes[j].maxCost
		}
		return probes[i].ent.ID < probes[j].ent.ID
	})

	budgetLeft := workBudget
	for pi, p := range probes {
		bucket := byKind[p.ent.Kind]
		if p.maxCost > budgetLeft {
			// Not enough budget left to examine this entity's candidates in
			// full. Stop here rather than half-examining it: a partial scan
			// would pick a "best match" from an arbitrary prefix of the
			// bucket. Everything from here on is reported as dropped.
			//
			// The admission test is deliberately conservative — it uses the
			// worst case (every candidate survives the band) rather than the
			// actual cost, which is not knowable without doing the scan. A
			// bucket that would in fact have been cheap can therefore be
			// dropped; the alternative is a partial scan, which is worse.
			stats.Truncated = true
			for _, rest := range probes[pi:] {
				stats.AddedSkipped++
				stats.PairsSkipped += len(byKind[rest.ent.Kind])
			}
			break
		}

		a := p.ent
		bestEdge := renameEdge{}
		bestScore := -1.0

		for _, c := range bucket {
			d := c.ent
			budgetLeft -= pairVisitCost
			stats.WorkUsed += pairVisitCost
			stats.PairsExamined++

			// Cheap prefilter: nameSimilarity >= 0.65 requires an edit distance
			// <= 35 % of the longer name, and edit distance is never smaller
			// than the length difference. Pairs outside the band cannot pass,
			// so skip the O(mn) levenshtein entirely — and, crucially, charge
			// nothing for it beyond the visit: a band rejection is two integer
			// compares, not a comparison's worth of work.
			if !lengthBandOK(p.lowLen, c.lowLen) {
				stats.PairsPrefiltered++
				continue
			}
			cost := int64(p.lowLen) * int64(c.lowLen)
			budgetLeft -= cost
			stats.WorkUsed += cost
			stats.Comparisons++

			// Signal 1: name similarity.
			// Threshold: reject pairs where more than 35 % of the longer name
			// needs to change (sim < 0.65). This accepts common rename patterns
			// like getUserByID→getUserByName (sim≈0.69) while rejecting
			// completely unrelated names like foo→bar (sim=0.0).
			nameSim := nameSimilarity(d.Name, a.Name)
			if nameSim < nameSimilarityFloor {
				continue
			}

			// Signal 2: neighborhood preservation.
			nbSim := neighborhoodSimilarity(d.ID, a.ID, prevNeighbors, newNeighbors)

			// Signal 3: same file (intra-file rename).
			sameFile := 0.0
			if d.SourceFile == a.SourceFile {
				sameFile = 1.0
			}

			// Require at least one of (neighborhood OR same file) to match, in
			// addition to name similarity.  Without this guard, two completely
			// unrelated functions that happen to have similar names (e.g. init /
			// init2) in different files and different callers would be linked.
			if nbSim < 0.1 && sameFile == 0.0 {
				continue
			}

			// Composite confidence: weighted average of the three signals.
			// Weights: name=0.5, neighborhood=0.35, file=0.15.
			confidence := nameSim*0.50 + nbSim*0.35 + sameFile*0.15

			// Ties keep the FIRST candidate encountered (the comparison is
			// strict). Before #6087 the scan ran over `deleted` in
			// prevDoc.Entities order; it now runs over the kind bucket sorted
			// by entity ID. Both are deterministic, but they are not the same
			// order — on equal confidence a DIFFERENT deleted entity can win,
			// so this changes which edges are emitted, not merely the order
			// they are emitted in. Sorting by ID is the deliberate choice: it
			// does not depend on how the previous graph happened to be laid
			// out on disk.
			if confidence > bestScore {
				method := buildMethodTag(nameSim, nbSim, sameFile > 0)
				bestScore = confidence
				bestEdge = renameEdge{
					fromID:     a.ID,
					toID:       d.ID,
					oldName:    d.Name,
					confidence: math.Round(confidence*100) / 100,
					method:     method,
				}
			}
		}

		if bestScore < 0 {
			continue
		}
		dedupKey := bestEdge.fromID + "\x00" + bestEdge.toID
		if _, dup := existingRenames[dedupKey]; dup {
			continue
		}
		matchesByDeleted[bestEdge.toID] = append(matchesByDeleted[bestEdge.toID], bestEdge)
	}

	// Phase 3 — emit edges. Tag splits where one deleted entity maps to 2+
	// new ones. Keys are sorted so the emitted edge order (and therefore the
	// on-disk bytes before the final sort) does not depend on map iteration.
	deletedIDs := make([]string, 0, len(matchesByDeleted))
	for id := range matchesByDeleted {
		deletedIDs = append(deletedIDs, id)
	}
	sort.Strings(deletedIDs)

	for _, deletedID := range deletedIDs {
		edges := matchesByDeleted[deletedID]
		isSplit := len(edges) > 1
		if isSplit {
			stats.Splits++
		}
		for _, edge := range edges {
			method := edge.method
			if isSplit && method != "moved" {
				method = "split"
			}

			props := map[string]string{
				"confidence": fmt.Sprintf("%.2f", edge.confidence),
				"old_name":   edge.oldName,
				"method":     method,
				"old_id":     deletedID,
			}
			rel := graph.Relationship{
				ID:     graph.RelationshipID(edge.fromID, edge.toID, RelKindRenamedFrom),
				FromID: edge.fromID,
				ToID:   edge.toID,
				Kind:   RelKindRenamedFrom,
			}.WithProperties(props)
			newDoc.Relationships = append(newDoc.Relationships, rel)
			existingRenames[edge.fromID+"\x00"+edge.toID] = struct{}{}

			stats.Renames++
			if method == "moved" {
				stats.Moves++
			}
		}
	}

	return stats
}

// ─── helpers ────────────────────────────────────────────────────────────────

// candidate is a deleted entity plus its lowercased-name length, precomputed
// once so the Phase-2 inner loop never re-lowercases (#6087).
type candidate struct {
	ent    graph.Entity
	lowLen int
}

// addedProbe is an added entity plus the size of the candidate bucket it will
// scan, used to order Phase 2 cheapest-first under the pair budget (#6087).
type addedProbe struct {
	ent     graph.Entity
	lowLen  int
	bucketN int
	// maxCost is the worst-case Phase-2 work of scanning this entity's whole
	// candidate bucket, in the same DP-cell units as the work budget.
	maxCost int64
}

// nameSimilarityFloor is the minimum normalised-Levenshtein score Phase 2
// accepts. It is duplicated as a named constant so lengthBandOK and the inner
// loop cannot drift apart.
const nameSimilarityFloor = 0.65

// lengthBandOK reports whether two lowercased names are close enough in length
// that nameSimilarity COULD reach nameSimilarityFloor.
//
// sim = 1 - dist/maxLen and dist >= |lenA-lenB|, so
// sim <= 1 - |lenA-lenB|/maxLen. If that upper bound is already below the
// floor, the pair is unmatchable and levenshtein can be skipped. The predicate
// is a strict superset of the accepted set — it never rejects a pair the full
// comparison would have accepted.
func lengthBandOK(lenA, lenB int) bool {
	maxLen := lenA
	if lenB > maxLen {
		maxLen = lenB
	}
	if maxLen == 0 {
		return true
	}
	diff := lenA - lenB
	if diff < 0 {
		diff = -diff
	}
	return 1.0-float64(diff)/float64(maxLen) >= nameSimilarityFloor
}

// neighborIndex maps an entity ID to the set of IDs it is connected to (both
// callers and callees across all edge kinds).
type neighborIndex map[string]map[string]struct{}

// buildNeighborIndex scans rels and builds a bidirectional adjacency set so
// neighborhoodSimilarity can find shared neighbors in O(1) per pair.
func buildNeighborIndex(rels []graph.Relationship) neighborIndex {
	idx := make(neighborIndex, len(rels)/2+1)
	for _, r := range rels {
		if idx[r.FromID] == nil {
			idx[r.FromID] = make(map[string]struct{})
		}
		if idx[r.ToID] == nil {
			idx[r.ToID] = make(map[string]struct{})
		}
		idx[r.FromID][r.ToID] = struct{}{}
		idx[r.ToID][r.FromID] = struct{}{}
	}
	return idx
}

// neighborhoodSimilarity returns the Jaccard similarity between the neighbor
// sets of oldID (in prevNeighbors) and newID (in newNeighbors).
// Returns 0 when both sets are empty (no structural signal).
func neighborhoodSimilarity(oldID, newID string, prev, next neighborIndex) float64 {
	oldNb := prev[oldID]
	newNb := next[newID]

	if len(oldNb) == 0 && len(newNb) == 0 {
		return 0
	}

	// Count intersection (ignoring the specific IDs which differ after rename —
	// we match by name within the neighbor sets instead of by ID).
	// Since IDs are stable-hash based on (repo, kind, name, file), the callers'
	// IDs will be the same across both snapshots as long as THEY were not renamed.
	intersection := 0
	for id := range oldNb {
		if _, ok := newNb[id]; ok {
			intersection++
		}
	}

	union := len(oldNb) + len(newNb) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// nameSimilarity returns a 0–1 score: 1 = identical, 0 = completely different.
// Based on normalised Levenshtein: 1 - dist/max(len(a),len(b)).
func nameSimilarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	// Case-insensitive comparison — rename may change casing.
	al := strings.ToLower(a)
	bl := strings.ToLower(b)
	if al == bl {
		return 0.97 // slight penalty for casing change
	}
	maxLen := len(al)
	if len(bl) > maxLen {
		maxLen = len(bl)
	}
	if maxLen == 0 {
		return 1.0
	}
	dist := levenshtein(al, bl)
	sim := 1.0 - float64(dist)/float64(maxLen)
	return sim
}

// levenshtein computes the edit distance between two lowercase strings.
// Uses the standard two-row DP; O(mn) time, O(min(m,n)) space.
func levenshtein(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	// Ensure a is the shorter string to minimise allocations.
	if len(a) > len(b) {
		a, b = b, a
	}
	prev := make([]int, len(a)+1)
	curr := make([]int, len(a)+1)
	for i := range prev {
		prev[i] = i
	}
	for j := 1; j <= len(b); j++ {
		curr[0] = j
		for i := 1; i <= len(a); i++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			del := prev[i] + 1
			ins := curr[i-1] + 1
			sub := prev[i-1] + cost
			if del < ins {
				curr[i] = del
			} else {
				curr[i] = ins
			}
			if sub < curr[i] {
				curr[i] = sub
			}
		}
		prev, curr = curr, prev
	}
	return prev[len(a)]
}

// buildMethodTag returns a human-readable description of which signals fired.
func buildMethodTag(nameSim, nbSim float64, sameFile bool) string {
	var parts []string
	if nameSim >= 0.97 {
		parts = append(parts, "name_exact")
	} else {
		parts = append(parts, "name_fuzzy")
	}
	if nbSim >= 0.1 {
		parts = append(parts, "neighborhood")
	}
	if sameFile {
		parts = append(parts, "same_file")
	}
	return strings.Join(parts, "+")
}
