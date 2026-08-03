// Package main — diff_reindex_multiset_test.go
//
// #6037: the parity comparator compared SETS, not multisets. This file is the
// demonstration that the multiset conversion is a real instrument and not a
// notional one: it runs the actual incremental path over a 40-file Go corpus,
// reads the persisted graph back off disk, and shows that
//
//   - the graph the incremental path PERSISTS carries duplicate relationship
//     rows (#6094), and
//   - the new multiset comparator reports them, while the pre-#6037 set-based
//     comparator — reproduced verbatim below — reports parity on the very same
//     pair of documents.
//
// Both directions of that demonstration matter. A new comparator that fires on a
// fixture where the old one also fired proves nothing.
package main

import (
	"fmt"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/parity"
)

// ───────────────────── the pre-#6037 (set-based) comparator ─────────────────
//
// Reproduced from internal/graph/parity/parity.go@65c50e0a9: entities and
// relationships were keyed into map[string]T, so N rows sharing an identity
// collapsed into ONE slot and the last writer won. dvLegacySetDiffs is that
// algorithm, run over the same raw documents the new comparator sees — not a
// pre-deduplicated stand-in, which would make its verdict tautological.

func dvLegacyEntityKey(e graph.Entity) string {
	id := e.ID
	if id == "" {
		id = graph.EntityID("", e.Kind, e.Name, e.SourceFile)
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s", id, e.Kind, e.Name, e.QualifiedName, e.SourceFile)
}

func dvLegacyRelKey(r graph.Relationship) string {
	return r.FromID + "→" + r.ToID + ":" + r.Kind
}

// dvLegacySetDiffs returns the number of divergences the OLD comparator would
// report: entities/relationships present on exactly one side, plus per-key
// property drift on the surviving (last-write-wins) representative.
func dvLegacySetDiffs(a, b *graph.Document) int {
	entA := map[string]graph.Entity{}
	for _, e := range a.Entities {
		entA[dvLegacyEntityKey(e)] = e
	}
	entB := map[string]graph.Entity{}
	for _, e := range b.Entities {
		entB[dvLegacyEntityKey(e)] = e
	}
	relA := map[string]graph.Relationship{}
	for _, r := range a.Relationships {
		relA[dvLegacyRelKey(r)] = r
	}
	relB := map[string]graph.Relationship{}
	for _, r := range b.Relationships {
		relB[dvLegacyRelKey(r)] = r
	}

	n := 0
	for k, ea := range entA {
		eb, ok := entB[k]
		if !ok {
			n++
			continue
		}
		if ea.Signature != eb.Signature || ea.StartLine != eb.StartLine || ea.EndLine != eb.EndLine {
			n++
		}
	}
	for k := range entB {
		if _, ok := entA[k]; !ok {
			n++
		}
	}
	for k, ra := range relA {
		rb, ok := relB[k]
		if !ok {
			n++
			continue
		}
		if dvRenderProps(ra.PropsSnapshot()) != dvRenderProps(rb.PropsSnapshot()) {
			n++
		}
	}
	for k := range relB {
		if _, ok := relA[k]; !ok {
			n++
		}
	}
	return n
}

func dvRenderProps(m map[string]string) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	out := ""
	for _, k := range ks {
		out += k + "=" + m[k] + ";"
	}
	return out
}

// ─────────────────────────────── the fixture ────────────────────────────────

// dvMultisetCorpusFile is one file of the 40-file corpus: a struct, an
// interface, a method, a constructor and two plain functions. The richer shape
// matters — a corpus of bare functions does not exercise the pass that appends
// the duplicate rows.
//
// Every file gets a UNIQUE basename on purpose: diff.Filter cross-invalidates
// same-basename files, so a corpus of 40 files all called f.go turns a 1-file
// delta into a 40-file change and trips the too-many-changed full-reindex
// fallback, silently making the case vacuous.
func dvMultisetCorpusFile(i int) string {
	return fmt.Sprintf(`package svc

type T%d struct{ N int }

type I%d interface{ Do() int }

func (t *T%d) Do() int { return t.N + h%d() }

func h%d() int { return %d }

func New%d() *T%d { return &T%d{N: %d} }

func Use%d(x I%d) int { return x.Do() }
`, i, i, i, i, i, i, i, i, i, i, i, i)
}

// dvDuplicateRows returns the number of SURPLUS rows (rows beyond the first for
// each identity key) in doc, plus a sorted sample of the duplicated keys.
func dvDuplicateRows(doc *graph.Document) (dupEnts, dupRels int, sample []string) {
	ce := map[string]int{}
	for _, e := range doc.Entities {
		ce[dvLegacyEntityKey(e)]++
	}
	for _, n := range ce {
		if n > 1 {
			dupEnts += n - 1
		}
	}
	cr := map[string]int{}
	for _, r := range doc.Relationships {
		cr[dvLegacyRelKey(r)]++
	}
	for k, n := range cr {
		if n > 1 {
			dupRels += n - 1
			sample = append(sample, fmt.Sprintf("%s ×%d", k, n))
		}
	}
	sort.Strings(sample)
	return dupEnts, dupRels, sample
}

// dvCollapseToSet returns a copy of doc with duplicate rows collapsed — one row
// per identity key, keeping the LAST occurrence exactly as the pre-#6037 map
// build did. This is the reference document: it differs from doc in MULTIPLICITY
// AND NOTHING ELSE, which is what isolates the dimension under test.
func dvCollapseToSet(doc *graph.Document) *graph.Document {
	out := &graph.Document{}
	entIdx := map[string]int{}
	for _, e := range doc.Entities {
		k := dvLegacyEntityKey(e)
		if i, ok := entIdx[k]; ok {
			out.Entities[i] = e
			continue
		}
		entIdx[k] = len(out.Entities)
		out.Entities = append(out.Entities, e)
	}
	relIdx := map[string]int{}
	for _, r := range doc.Relationships {
		k := dvLegacyRelKey(r)
		if i, ok := relIdx[k]; ok {
			out.Relationships[i] = r
			continue
		}
		relIdx[k] = len(out.Relationships)
		out.Relationships = append(out.Relationships, r)
	}
	return out
}

// dvInjectDuplicateRows returns a copy of doc in which ONE relationship of each
// requested kind has been replicated to the requested multiplicity, replaying
// the shape #6094 used to produce here before it was fixed. The chosen row is
// the first of its kind in canonical order, so the injection is deterministic.
//
// Injecting rather than mutating in place keeps the caller's document intact,
// and injecting only EXACT copies is what makes dvCollapseToSet's output differ
// from the result in multiplicity and in nothing else — the property the
// comparator assertions below depend on.
func dvInjectDuplicateRows(t *testing.T, doc *graph.Document, kindMultiplicity map[string]int) *graph.Document {
	t.Helper()
	out := &graph.Document{
		Entities:      append([]graph.Entity(nil), doc.Entities...),
		Relationships: append([]graph.Relationship(nil), doc.Relationships...),
	}
	for kind, mult := range kindMultiplicity {
		var seed *graph.Relationship
		for i := range doc.Relationships {
			if doc.Relationships[i].Kind == kind {
				seed = &doc.Relationships[i]
				break
			}
		}
		if seed == nil {
			t.Fatalf("cannot inject %s duplicates: the persisted graph carries no %s edge at all — "+
				"the corpus stopped exercising the shape this case is about", kind, kind)
		}
		for i := 1; i < mult; i++ {
			out.Relationships = append(out.Relationships, *seed)
		}
	}
	return out
}

// ───────────────────────────── the demonstration ────────────────────────────

// TestDiffReindex_MultisetComparatorCatchesDuplicateRows is the #6037 acceptance
// case, run against the real #6094 defect.
//
// It builds a 40-file corpus, full-rebuilds it, then applies three SEQUENTIAL
// single-file deltas, each followed by a real TryIncremental pass, and reads the
// persisted graph.fb back with LoadGraphFromDir. The duplicates asserted here
// are rows ON DISK, not entries in a run log.
//
// (Issue #6094 states the duplicates are "silently absorbed by the writer's
// dedupe" and that the persisted graph is correct. That is not what this fixture
// observes: fbwriter dedupes shared STRINGS, not rows, and the surplus rows
// survive into graph.fb — and COMPOUND, one more copy per incremental pass.)
func TestDiffReindex_MultisetComparatorCatchesDuplicateRows(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	const nFiles = 40
	for i := 0; i < nFiles; i++ {
		dvWriteFile(t, repo, fmt.Sprintf("r%02d.go", i), dvMultisetCorpusFile(i))
	}

	dvFullRebuild(t, repo, stateDir)

	// The full rebuild is the control: it must carry NO duplicate rows, so the
	// duplication below is attributable to the incremental path.
	base, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load baseline: %v", err)
	}
	if de, dr, sample := dvDuplicateRows(base); de != 0 || dr != 0 {
		t.Fatalf("control violated: the FULL rebuild already carries duplicate rows (%d entity, %d relationship): %v",
			de, dr, sample)
	}

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
	}

	// B is the graph the incremental path PERSISTED — read back off disk.
	b, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load incremental graph: %v", err)
	}
	// #6094 IS FIXED, so this graph no longer duplicates rows on its own — the
	// incremental path now substitutes the owning record's ID for an empty
	// record-embedded FromID and dedupes the re-extraction batch, exactly as the
	// full-index assembly loop does. The regression gate for that lives in
	// TestIncremental_NoDuplicateRowsAcrossPasses_6094.
	//
	// What THIS case exists for is #6037: proving the multiset comparator is a
	// real instrument. That demonstration needs a document carrying duplicate
	// rows, and it no longer gets one for free. It now INJECTS the duplication
	// into the persisted graph — re-appending rows that are already present,
	// with the exact multiplicities the pre-fix defect produced on this very
	// fixture (CONTAINS ×3, REFERENCES ×6). The comparator input is
	// therefore the same shape it always was; only its provenance changed, from
	// "a live defect" to "a defect replayed on purpose".
	if de, dr, sample := dvDuplicateRows(b); de != 0 || dr != 0 {
		t.Fatalf("#6094 has REGRESSED: the persisted incremental graph carries duplicate rows again "+
			"(%d entity, %d relationship): %v", de, dr, sample)
	}
	b = dvInjectDuplicateRows(t, b, map[string]int{
		"CONTAINS":   3,
		"REFERENCES": 6,
	})

	dupEnts, dupRels, sample := dvDuplicateRows(b)
	t.Logf("injected duplication: %d entities, %d relationships; surplus rows: %d entity, %d relationship\nduplicated keys: %v",
		len(b.Entities), len(b.Relationships), dupEnts, dupRels, sample)
	if dupEnts+dupRels == 0 {
		t.Fatal("fixture is inert: the injection produced no duplicate rows, so neither comparator is under test")
	}

	// A is B with duplicate rows collapsed. A and B therefore differ in
	// MULTIPLICITY AND NOTHING ELSE — no entity is added or dropped, no property
	// drifts, no community is relabelled. Anything a comparator reports here is
	// the duplication and only the duplication.
	a := dvCollapseToSet(b)

	// ── Direction 1: the NEW comparator sees it ──────────────────────────────
	rep := parity.CompareWithOptions(a, b, dvBaselineProfile())
	if rep.Equivalent {
		t.Fatalf("the multiset comparator reported parity between a graph with %d surplus rows and the same graph without them:\n%s",
			dupEnts+dupRels, rep.String())
	}
	if len(rep.RelMultiplicityDiffs) == 0 && len(rep.EntityMultiplicityDiffs) == 0 {
		t.Fatalf("duplication must be reported in the row-count buckets, got:\n%s", rep.String())
	}
	// Every reported row-count diff must be a real duplicate key, and every real
	// duplicate key must be reported — no over- or under-reporting.
	if got, want := len(rep.RelMultiplicityDiffs), len(sample); got != want {
		t.Errorf("RelMultiplicityDiffs=%d, want %d (one per duplicated relationship key)\nreport:\n%s",
			got, want, rep.String())
	}
	// Nothing OTHER than multiplicity may be reported — the two documents are
	// identical in every other dimension by construction, so a diff elsewhere
	// means the comparator gained a false positive.
	if len(rep.EntitiesOnlyInA)+len(rep.EntitiesOnlyInB)+len(rep.EntityFieldDiffs)+
		len(rep.RelsOnlyInA)+len(rep.RelsOnlyInB)+len(rep.RelPropDiffs)+
		len(rep.CommunityAssignmentDiffs) != 0 {
		t.Errorf("comparator reported a non-multiplicity divergence between a graph and its own de-duplicated copy:\n%s", rep.String())
	}
	t.Logf("NEW multiset comparator correctly reports the duplication:\n%s", rep.String())

	// ── Direction 2: the OLD set comparator is blind to it ───────────────────
	// Sanity first: the legacy comparator is not blind to EVERYTHING — it does
	// catch a dropped row, so a verdict of "parity" below is about multiplicity
	// specifically and not about a broken stand-in.
	maimed := &graph.Document{
		Entities:      a.Entities,
		Relationships: a.Relationships[:len(a.Relationships)-1],
	}
	if n := dvLegacySetDiffs(a, maimed); n == 0 {
		t.Fatal("the legacy set comparator stand-in is inert — it fails to report even a dropped edge")
	}
	if n := dvLegacySetDiffs(a, b); n != 0 {
		t.Fatalf("premise of #6037 does not hold on this fixture: the legacy set comparator reported %d divergence(s) "+
			"between a graph and its de-duplicated copy; the multiset conversion would not be what caught this", n)
	}
	t.Logf("OLD set-based comparator reports PARITY on the same pair (%d surplus rows invisible to it)", dupEnts+dupRels)
}
