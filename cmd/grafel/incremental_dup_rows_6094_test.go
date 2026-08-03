// Package main — incremental_dup_rows_6094_test.go
//
// #6094 — the incremental path persists DUPLICATE relationship rows into
// graph.fb, and they COMPOUND: one additional copy of the affected rows per
// incremental pass. Incremental is the default path for a watched repo, so a
// long-lived watched graph accumulates duplicates without bound.
//
// ROOT CAUSE OF #6094.
// ────────────────────
// The full-index assembly loop (cmd/grafel/index.go, buildDocument) converts a
// record-embedded types.RelationshipRecord into a graph.Relationship with two
// steps the incremental re-extraction loop omitted:
//
//  1. `if fromID == "" { fromID = id }` — a record-embedded edge with no
//     explicit FromID is owned by the entity record carrying it, so the
//     owner's ID is substituted.
//  2. a `seenRel` guard keyed on (FromID, ToID, Kind) — an extractor may emit
//     the same owned edge twice within one file.
//
// extractors.relRecordToGraphRel did neither, so the incremental path emitted
// those edges with an EMPTY FromID — and the stale-edge eviction in
// TryIncremental drops an old edge only when removedEntityIDs[r.FromID]. An
// empty FromID matches no removed entity, so each pass's copies SURVIVE while
// re-extraction appends fresh ones. That is the compounding: +1 copy per pass,
// unbounded. The fix restores both steps; there is no downstream row dedupe,
// which would have hidden the malformed edges instead of removing them.
//
// #6098 IS A DIFFERENT DEFECT and is NOT fixed here. The fix above moves its
// numbers by exactly zero — see TestIncremental_DependsOnWeightParity_6098
// below for the measurement and the actual chain.
//
// This file is the regression gate. It asserts on the graph READ BACK OFF DISK
// (never the run log, which over-reports — that over-reporting is #6094), over
// THREE sequential incremental passes, so a fix that merely removes the first
// pass's surplus without stopping the accumulation still fails here.
package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// dupRowKey is the identity of a relationship ROW for duplicate detection.
//
// (FromID, ToID, Kind) alone is NOT a unique relationship key: several
// producers deliberately salt the relationship ID so that edges sharing a
// triple stay distinct (internal/engine/migration_schema_ops.go,
// phantom_edges.go, process_flow.go, event_flow.go, tests_walkup.go). Keying a
// duplicate check on the bare triple would report those legitimate edges as
// duplicates — and any dedupe built on it would destroy them. The key here is
// (FromID, ToID, Kind, ID, properties): two rows collide only when they are
// indistinguishable in every persisted dimension, which is the only case where
// a second copy carries no information.
func dupRowKey(r graph.Relationship) string {
	props := r.PropsSnapshot()
	ks := make([]string, 0, len(props))
	for k := range props {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	var b strings.Builder
	b.WriteString(r.FromID)
	b.WriteString("\x00")
	b.WriteString(r.ToID)
	b.WriteString("\x00")
	b.WriteString(r.Kind)
	b.WriteString("\x00")
	b.WriteString(r.ID)
	for _, k := range ks {
		b.WriteString("\x00")
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(props[k])
	}
	return b.String()
}

// dupSurplusRows counts rows beyond the first for each dupRowKey, and returns a
// sorted sample of the offending keys.
func dupSurplusRows(doc *graph.Document) (surplus int, sample []string) {
	c := map[string]int{}
	for _, r := range doc.Relationships {
		c[dupRowKey(r)]++
	}
	for k, n := range c {
		if n > 1 {
			surplus += n - 1
			sample = append(sample, fmt.Sprintf("%s ×%d", strings.ReplaceAll(k, "\x00", "|"), n))
		}
	}
	sort.Strings(sample)
	return surplus, sample
}

// dupEmptyFrom counts relationships with an empty FromID and samples them.
func dupEmptyFrom(doc *graph.Document) (n int, sample []string) {
	seen := map[string]bool{}
	for _, r := range doc.Relationships {
		if r.FromID != "" {
			continue
		}
		n++
		k := fmt.Sprintf("(empty)→%s:%s", r.ToID, r.Kind)
		if !seen[k] {
			seen[k] = true
			sample = append(sample, k)
		}
	}
	sort.Strings(sample)
	return n, sample
}

// TestIncremental_NoDuplicateRowsAcrossPasses_6094 is the compounding gate.
//
// A 40-file Go corpus (structs + interfaces + methods — a corpus of plain
// functions does NOT reproduce this; the owned CONTAINS/REFERENCES edges the
// defect rides on only exist for type members) is full-rebuilt, then three
// SEQUENTIAL single-file deltas are applied, each through a real
// TryIncremental. After EVERY pass the persisted graph.fb is read back and
// asserted to carry zero surplus rows and zero empty-FromID edges.
//
// Asserting after every pass — not only at the end — is what makes this a
// compounding gate: the pre-fix numbers were 1 → 5 → 9 surplus rows, so a
// per-pass trace also documents the accumulation rate if it ever returns.
//
// Every corpus file gets a UNIQUE basename on purpose: diff.Filter
// cross-invalidates same-basename files, so a corpus of 40 files all named f.go
// turns a 1-file delta into a 40-file change and trips the too-many-changed
// full-reindex fallback — silently making the case vacuous. dvIncremental
// additionally fails the test if TryIncremental falls back.
func TestIncremental_NoDuplicateRowsAcrossPasses_6094(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	const nFiles = 40
	for i := 0; i < nFiles; i++ {
		dvWriteFile(t, repo, fmt.Sprintf("r%02d.go", i), dvMultisetCorpusFile(i))
	}

	dvFullRebuild(t, repo, stateDir)

	// Control: the full rebuild must be clean on both dimensions, so anything
	// observed after an incremental pass is attributable to the incremental path.
	base, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load baseline: %v", err)
	}
	if n, sample := dupSurplusRows(base); n != 0 {
		t.Fatalf("control violated: the FULL rebuild already carries %d surplus relationship row(s): %v", n, sample)
	}
	if n, sample := dupEmptyFrom(base); n != 0 {
		t.Fatalf("control violated: the FULL rebuild already carries %d empty-FromID edge(s): %v", n, sample)
	}
	// Pass-over-pass stability, not equality with the full rebuild. The
	// incremental graph is legitimately a few rows SHORT of a full rebuild on
	// this corpus for an unrelated, still-open reason (#6098: the scoped
	// resolver has no receiver-type / package-member tier, so an intra-file
	// method CALLS stays a bare-name stub and the process-flow pass never builds
	// the SCOPE.Process it would have anchored — see the comment on
	// TestIncremental_DependsOnWeightParity_6098). Asserting equality here would
	// couple this gate to that defect. What #6094 is about is GROWTH: the row
	// count must reach a fixed point after the first pass and stay there.
	var prevRels int

	var trace []string
	for pass := 1; pass <= 3; pass++ {
		dvSeedManifest(t, repo, stateDir)
		dvWriteFile(t, repo, "r07.go", fmt.Sprintf(`package svc

type T7 struct{ N int }

type I7 interface{ Do() int }

func (t *T7) Do() int { return t.N + h7() + %d }

func h7() int { return 7 }

func New7() *T7 { return &T7{N: 7} }

func Use7(x I7) int { return x.Do() }
`, pass))
		dvIncremental(t, repo, stateDir)

		// Read the PERSISTED graph — never the run log, which over-reports.
		b, err := graph.LoadGraphFromDir(stateDir)
		if err != nil {
			t.Fatalf("pass %d: load persisted graph: %v", pass, err)
		}
		surplus, dupSample := dupSurplusRows(b)
		empty, emptySample := dupEmptyFrom(b)
		trace = append(trace, fmt.Sprintf("pass %d: rels=%d surplus=%d empty-from=%d", pass, len(b.Relationships), surplus, empty))

		if empty != 0 {
			t.Errorf("pass %d: persisted graph carries %d relationship(s) with an EMPTY FromID (full rebuild: 0): %v\n"+
				"the incremental re-extraction must substitute the owning entity record's ID for an empty "+
				"record-embedded FromID, exactly as the full-index assembly loop does", pass, empty, emptySample)
		}
		if surplus != 0 {
			t.Errorf("pass %d: persisted graph carries %d SURPLUS relationship row(s) keyed on "+
				"(FromID, ToID, Kind, ID, properties): %v", pass, surplus, dupSample)
		}
		// The corpus end-state is the same shape on every pass (only a literal
		// inside one method body changes), so the total row count must be a fixed
		// point from pass 2 onwards. This catches accumulation even in a form the
		// surplus key above would miss — e.g. rows that differ in a property and
		// so are not byte-identical duplicates, but still pile up.
		if pass > 1 && len(b.Relationships) != prevRels {
			t.Errorf("pass %d: persisted relationship count %d ≠ previous pass's %d — the incremental path "+
				"is not at a fixed point; it is accumulating (or shedding) rows across passes",
				pass, len(b.Relationships), prevRels)
		}
		prevRels = len(b.Relationships)
	}
	t.Logf("per-pass trace:\n  %s", strings.Join(trace, "\n  "))
}

// TestIncremental_DependsOnWeightParity_6098 is a live, self-contained
// reproduction of #6098 — SKIPPED, because #6098 is a DIFFERENT defect from
// #6094 and its fix does not belong in this change. Un-skip it when the
// resolver work below is picked up; it is already red for the right reason.
//
// #6098 IS NOT THE SAME ROOT CAUSE AS #6094. Evidence.
// ─────────────────────────────────────────────────────
// The empty-FromID fix in extractors.relRecordToGraphRel removes 100% of the
// #6094 surplus (1→5→9 becomes 0→0→0) and moves the #6098 weights by exactly
// ZERO: [39, 117] incremental vs [40, 120] full, both before and after. An
// initial hypothesis that module aggregation was losing the empty-FromID edges
// through idMod[r.FromID] is therefore refuted by measurement.
//
// The actual chain, from a full row-level diff of the two persisted graphs:
//
//  1. The scoped resolver (internal/extractors/sresolver) resolves a stub
//     endpoint through two tiers only: whole-string name/qualified-name, and
//     Format A (file, tail). It has NO receiver-type tier. So the Go
//     extractor's `Use7 -CALLS-> "Do"` and `T7.Do -CALLS-> "N"` — bare-name
//     stubs carrying Properties["receiver_type"]={I7,T7} — stay unresolved,
//     where the full resolver binds them via its package-scoped member index
//     (internal/resolve/refs.go:5588, issues #148/#364) to T7.Do and T7.N.
//  2. With those CALLS dangling, the process-flow pass cannot chain
//     Use7 → T7.Do → T7.N, so the SCOPE.Process entity `Use7 → T7.N` is never
//     built on the incremental path (it IS built by the full rebuild).
//  3. That missing process costs 5 relationship rows: 3 STEP_IN_PROCESS, 1
//     ENTRY_POINT_OF, 1 CONTAINS — which is exactly the 882 vs 877 row gap.
//  4. Module aggregation weighs cross-module edges. The 3 STEP_IN_PROCESS run
//     _external→test-repo (120−117=3) and the ENTRY_POINT_OF runs
//     test-repo→_external (40−39=1). The weight shortfall is fully accounted
//     for by the missing rows, with no residual.
//
// So #6098 is a RESOLUTION-PARITY gap in sresolver, surfacing as a weight
// discrepancy three passes downstream. Closing it means porting the full
// resolver's receiver-type / package-member tier — which is built on a
// package-scoped member index over the entire entity set — onto the scoped
// path. That is a separate change against a separate component, and shipping a
// partial version of it here would be worse than leaving the defect visible.
func TestIncremental_DependsOnWeightParity_6098(t *testing.T) {
	t.Skip("#6098 is open and is NOT the #6094 root cause — see the comment above for the " +
		"measured evidence and the exact chain; un-skip when sresolver gains a receiver-type tier")

	repo := t.TempDir()
	stateDir := t.TempDir()
	const nFiles = 20
	for i := 0; i < nFiles; i++ {
		dvWriteFile(t, repo, fmt.Sprintf("a/r%02d.go", i), dvMultisetCorpusFile(i))
	}
	for i := nFiles; i < 2*nFiles; i++ {
		dvWriteFile(t, repo, fmt.Sprintf("b/r%02d.go", i), dvMultisetCorpusFile(i))
	}

	dvFullRebuild(t, repo, stateDir)

	for pass := 1; pass <= 3; pass++ {
		dvSeedManifest(t, repo, stateDir)
		dvWriteFile(t, repo, "a/r07.go", fmt.Sprintf(`package svc

type T7 struct{ N int }

type I7 interface{ Do() int }

func (t *T7) Do() int { return t.N + h7() + %d }

func h7() int { return 7 }

func New7() *T7 { return &T7{N: 7} }

func Use7(x I7) int { return x.Do() }
`, pass))
		dvIncremental(t, repo, stateDir)
	}

	b, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load incremental graph: %v", err)
	}
	// Full-rebuild the SAME end-state into a fresh state dir — the reference.
	endDir := t.TempDir()
	c := dvFullRebuild(t, repo, endDir)

	got := dvModuleLayer(b)
	want := dvModuleLayer(c)
	if len(want) == 0 {
		t.Fatal("fixture is vacuous: the full rebuild produced no Module→Module DEPENDS_ON edge to compare weights on")
	}
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			t.Errorf("module edge %s missing from the incremental graph (full rebuild weight=%s)", k, wv)
			continue
		}
		if gv != wv {
			t.Errorf("module edge %s: incremental weight=%s, full-rebuild weight=%s (#6098)", k, gv, wv)
		}
	}
	for k, gv := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("module edge %s present ONLY in the incremental graph (weight=%s)", k, gv)
		}
	}
	t.Logf("incremental module layer: %v\nfull rebuild:            %v", got, want)
}
