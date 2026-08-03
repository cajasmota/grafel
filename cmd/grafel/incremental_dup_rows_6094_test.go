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
// BLAST RADIUS — corrected. An earlier reading of #6094, including the version
// first published on the issue, held that type members were REQUIRED and that
// plain-function corpora were immune. That is false. The defect rides on ANY
// omitted-FromID record-embedded edge; a plain-function corpus carrying a single
// import accumulates too (measured pre-fix: surplus 0/1/2, empty-FromID 1/2/3).
// Only plain functions with ZERO imports are clean. Type members simply produce
// more owned edges per file, so they accumulate faster. Both shapes are in the
// table below for that reason.
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

// dupImportCorpusFile is the second corpus shape: PLAIN FUNCTIONS ONLY, no
// types at all, but carrying imports — including the same package imported
// twice under different aliases.
//
// Two things ride on this shape.
//
// FIRST, it corrects the blast radius. An earlier reading of #6094 held that
// type members were REQUIRED to trigger the defect and that plain-function
// corpora were clean. That is false, and the false version was published: a
// plain-function corpus with a single import accumulates too (measured on the
// pre-fix tree: surplus 0/1/2, empty-FromID 1/2/3, growing per pass). Only
// plain functions with ZERO imports are clean. The defect rides on ANY omitted-
// FromID record-embedded edge — here the function→import REFERENCES edge.
// Type members merely produce more of them per file.
//
// SECOND, the duplicate-alias import is the only shape in this suite that fires
// the seenRel dedupe guard through the real pipeline. Go emits one
// SCOPE.Component record PER import statement, so `strings` + `s2 "strings"`
// yields two records that both emit `<file> -IMPORTS-> ext:strings`. Without the
// guard the incremental graph carries that row twice where a full rebuild
// carries it once. Before this corpus existed the guard never fired once across
// either suite, and reverting it left every test green.
func dupImportCorpusFile(i int) string {
	return fmt.Sprintf(`package svc

import (
	"strings"
	s2 "strings"
	"strconv"
)

func Up%d(x string) string { return strings.ToUpper(x) }

func Down%d(x string) string { return s2.ToLower(x) }

func Num%d(x int) string { return strconv.Itoa(x + %d) }
`, i, i, i, i)
}

// TestIncremental_NoDuplicateRowsAcrossPasses_6094 is the compounding gate.
//
// A 40-file Go corpus is full-rebuilt, then three SEQUENTIAL single-file deltas
// are applied, each through a real TryIncremental. After EVERY pass the
// persisted graph.fb is read back and asserted to carry zero surplus rows and
// zero empty-FromID edges.
//
// Asserting after every pass — not only at the end — is what makes this a
// compounding gate: the pre-fix numbers were 1 → 5 → 9 surplus rows on the
// type-member corpus, so a per-pass trace also documents the accumulation rate
// if it ever returns.
//
// TWO corpus shapes, because they exercise different halves of the fix:
// "type-members" carries owned CONTAINS/REFERENCES edges from structs,
// interfaces and methods; "plain-functions-with-imports" has no types at all and
// pins both the corrected blast radius and the dedupe guard (see
// dupImportCorpusFile).
//
// Every corpus file gets a UNIQUE basename on purpose: diff.Filter
// cross-invalidates same-basename files, so a corpus of 40 files all named f.go
// turns a 1-file delta into a 40-file change and trips the too-many-changed
// full-reindex fallback — silently making the case vacuous. dvIncremental
// additionally fails the test if TryIncremental falls back.
func TestIncremental_NoDuplicateRowsAcrossPasses_6094(t *testing.T) {
	cases := []struct {
		name string
		file func(int) string
		// delta returns the pass-N content of the file that is edited.
		delta func(pass int) string
	}{
		{
			name: "type-members",
			file: dvMultisetCorpusFile,
			delta: func(pass int) string {
				return fmt.Sprintf(`package svc

type T7 struct{ N int }

type I7 interface{ Do() int }

func (t *T7) Do() int { return t.N + h7() + %d }

func h7() int { return 7 }

func New7() *T7 { return &T7{N: 7} }

func Use7(x I7) int { return x.Do() }
`, pass)
			},
		},
		{
			name: "plain-functions-with-imports",
			file: dupImportCorpusFile,
			delta: func(pass int) string {
				return fmt.Sprintf(`package svc

import (
	"strings"
	s2 "strings"
	"strconv"
)

func Up7(x string) string { return strings.ToUpper(x) + "%d" }

func Down7(x string) string { return s2.ToLower(x) }

func Num7(x int) string { return strconv.Itoa(x + 7) }
`, pass)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			stateDir := t.TempDir()
			const nFiles = 40
			for i := 0; i < nFiles; i++ {
				dvWriteFile(t, repo, fmt.Sprintf("r%02d.go", i), tc.file(i))
			}

			dvFullRebuild(t, repo, stateDir)

			// Control: the full rebuild must be clean on both dimensions, so
			// anything observed after an incremental pass is attributable to the
			// incremental path.
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
			fullRels := len(base.Relationships)

			// Pass-over-pass stability, NOT equality with the full rebuild.
			//
			// Read this before interpreting the row counts in the trace: the
			// incremental graph settles on a fixed point that is a few rows SHORT
			// of a full rebuild (877 vs 882 on the type-member corpus). That
			// residual gap is #6098 — still open, tracked, and verified to be
			// entirely attributable to it, not to the #6094 fix. A stable count is
			// therefore NOT parity with a full rebuild, and nothing here should be
			// read as claiming it is. #6094 is about GROWTH: the count must reach a
			// fixed point after the first pass and stay there.
			var prevRels int

			var trace []string
			for pass := 1; pass <= 3; pass++ {
				dvSeedManifest(t, repo, stateDir)
				dvWriteFile(t, repo, "r07.go", tc.delta(pass))
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
				// The corpus end-state is the same shape on every pass (only a
				// literal inside one body changes), so the total row count must be a
				// fixed point from pass 2 onwards. This catches accumulation even in
				// a form the surplus key above would miss — e.g. rows that differ in
				// a property and so are not byte-identical duplicates, but still
				// pile up.
				if pass > 1 && len(b.Relationships) != prevRels {
					t.Errorf("pass %d: persisted relationship count %d ≠ previous pass's %d — the incremental path "+
						"is not at a fixed point; it is accumulating (or shedding) rows across passes",
						pass, len(b.Relationships), prevRels)
				}
				prevRels = len(b.Relationships)
			}
			t.Logf("full rebuild: %d rels (residual gap to the incremental fixed point is #6098, still open)\nper-pass trace:\n  %s",
				fullRels, strings.Join(trace, "\n  "))
		})
	}
}

// TestIncremental_GuardSuppressesDuplicateImportRow_6094 pins, through the REAL
// pipeline, that the dedupe guard both FIRES and lands on the full rebuild's
// row count for a duplicated-alias import.
//
// The gate above asserts "no surplus rows", which a guard-less tree can satisfy
// by accident if the duplicated row is evicted before the next pass. This
// asserts the positive form directly: the incremental graph must carry the
// `<file> -IMPORTS-> strings` row EXACTLY as many times as a full rebuild of the
// identical source does. Reverting the guard makes the incremental count exceed
// the full-rebuild count, which no other assertion in the suite observes.
func TestIncremental_GuardSuppressesDuplicateImportRow_6094(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	const nFiles = 12
	for i := 0; i < nFiles; i++ {
		dvWriteFile(t, repo, fmt.Sprintf("r%02d.go", i), dupImportCorpusFile(i))
	}
	dvFullRebuild(t, repo, stateDir)

	for pass := 1; pass <= 3; pass++ {
		dvSeedManifest(t, repo, stateDir)
		dvWriteFile(t, repo, "r07.go", fmt.Sprintf(`package svc

import (
	"strings"
	s2 "strings"
	"strconv"
)

func Up7(x string) string { return strings.ToUpper(x) + "%d" }

func Down7(x string) string { return s2.ToLower(x) }

func Num7(x int) string { return strconv.Itoa(x + 7) }
`, pass))
		dvIncremental(t, repo, stateDir)
	}

	b, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load incremental graph: %v", err)
	}
	endDir := t.TempDir()
	c := dvFullRebuild(t, repo, endDir)

	// Count IMPORTS rows sourced from the edited file, on both sides. The
	// import-placeholder-prune hoists these edges onto the file's SCOPE.Component
	// and re-keys FromID to its hex ID, so the edited file has to be identified
	// by resolving FromID back to an entity and reading its SourceFile — matching
	// on the raw FromID string finds nothing.
	count := func(doc *graph.Document) map[string]int {
		src := make(map[string]string, len(doc.Entities))
		for i := range doc.Entities {
			src[doc.Entities[i].ID] = doc.Entities[i].SourceFile
		}
		out := map[string]int{}
		for _, r := range doc.Relationships {
			if r.Kind != "IMPORTS" || src[r.FromID] != "r07.go" {
				continue
			}
			out[src[r.FromID]+"→"+r.ToID]++
		}
		return out
	}
	got, want := count(b), count(c)
	if len(want) == 0 {
		t.Fatal("fixture is vacuous: the full rebuild carries no IMPORTS edge out of r07.go, so the guard " +
			"cannot be under test here — the extractor's import shape must have changed")
	}
	for k, wv := range want {
		if gv := got[k]; gv != wv {
			t.Errorf("IMPORTS row %s appears %d× in the incremental graph, %d× in a full rebuild of identical "+
				"source — the re-extraction dedupe guard is not matching the full path", k, gv, wv)
		}
	}
	for k, gv := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("IMPORTS row %s (×%d) exists ONLY in the incremental graph", k, gv)
		}
	}
	t.Logf("IMPORTS rows out of r07.go — incremental: %v ; full rebuild: %v", got, want)
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
//  3. The 882 vs 877 row gap is a NET, not a loss set. A bidirectional multiset
//     diff on this fixture reports 7 rows LOST and 2 INVENTED (7−2=5). The 2
//     invented are the same two CALLS in mis-bound stub form — `Use7→"Do"` and
//     `T7.Do→"N"` — against the resolved `→T7.Do` / `→T7.N` a full rebuild
//     produces. The net 5 is the missing process: 3 STEP_IN_PROCESS, 1
//     ENTRY_POINT_OF, 1 CONTAINS. Both absolute counts are corpus-dependent: on
//     a richer corpus (embedded structs, generics, value + pointer receivers)
//     they become 14 LOST / 9 INVENTED, net still 5. Read the NET as the
//     invariant — "net 5; N lost / N−5 invented, N growing with corpus
//     richness" — and the absolutes as fixture-specific.
//  4. Module aggregation weighs cross-module edges. The 3 STEP_IN_PROCESS run
//     _external→test-repo (120−117=3) and the ENTRY_POINT_OF runs
//     test-repo→_external (40−39=1). The 2 invented stub CALLS contribute 0 in
//     both directions — intra-module once resolved, and unresolvable while they
//     remain stubs — so the weight shortfall is fully accounted for, with no
//     residual. parity.CompareWithOptions reports exactly 2 RelPropDiffs here,
//     which is that pair and nothing else.
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
