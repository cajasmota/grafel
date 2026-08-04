// Package main — incremental_prior_resolution_6090_test.go
//
// #6090 — incremental drops correct edges out of a changed file when the
// single-file re-extraction cannot re-resolve them.
//
// Mechanism, grounded at 2f0175dfc:
//
//   - internal/extractors/incremental.go:446 prunes every prior edge whose
//     FromID is an entity sourced from a changed file, on the principle that the
//     re-extraction is authoritative for that file's outgoing edges.
//   - The re-extraction is a SINGLE-FILE extraction followed by
//     sresolver.ResolveScoped, whose binding ladder is strictly weaker than the
//     corpus-wide resolver's (internal/extractors/sresolver/scoped.go:247 —
//     whole-string name/qualified-name, then the Format A (file, tail) tiers).
//     A Go method entity is named "T11.Do11" while the call site emits the bare
//     stub "Do11"; the full resolver binds it via its member-suffix tier, the
//     scoped one cannot.
//   - Net effect: the resolved CALLS edge the full graph held is pruned, and the
//     fresh pass re-emits the same call with an UNRESOLVED bare-name ToID.
//
// This is measurable and monotone: editing three DIFFERENT files in sequence
// loses 3 → 6 → 9 CALLS edges against a full rebuild of the same end-state, and
// nothing restores them until a full reindex.
//
// Scope, established by measurement rather than taken from the issue: the loss
// reproduced here is on the DAEMON path (internal/extractors.TryIncremental),
// not on the CLI full-pipeline incremental (cmd/grafel/index.go). The CLI path
// seeds the corpus-wide resolver with the prev graph's unchanged-file entities
// (i.incrementalCarryForwardEntities, index.go:1243), whose member tier keys on
// Name + SourceFile — both carried — so THIS stub shape resolves there and the
// CLI path measures zero loss on this fixture across all three passes.
//
// That is a statement about this stub shape, not a clean bill of health: the
// carried record (index.go:1250-1256) copies six fields and omits
// QualifiedName, which the symbol index also keys on, so stubs that resolve
// only via the QualifiedName tier against an unchanged-file entity are lost on
// the CLI path too. Same defect shape, narrower blast radius, pre-existing and
// tracked separately.
//
// The daemon path is the default for a watched repo, which is what makes this
// degrade a long-lived graph.
//
// The fix replays the previous graph's resolution decision: for a fresh edge the
// isolated pass left unresolved, if the prior graph held an edge with the same
// FromID and Kind bound to a live entity whose name matches the unresolved stub,
// the fresh edge is bound to that entity. It is a REPLAY, not a retention — the
// prior edge is only consulted to bind an edge the fresh extraction actually
// re-emitted, so a call genuinely deleted from source has nothing to bind and
// stays dropped (asserted by the companion test below).
package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/parity"
)

// pr6090File is one corpus file. Every basename is UNIQUE on purpose:
// diff.Filter cross-invalidates same-basename files, which would turn a
// one-file delta into an N-file change and trip the too-many-changed
// full-reindex fallback, making the case vacuous.
func pr6090File(i int) string {
	return fmt.Sprintf(`package svc

type T%02d struct{ N int }

func (t *T%02d) Do%02d(x int) int { return x + t.N + %d }

func Helper%02d(x int) int { return x + %d }

func Local%02d(x int) int { return Helper%02d(x) }
`, i, i, i, i, i, i, i, i)
}

// pr6090Edited is the content of the edited file at pass N — pass 0 being the
// BASELINE that the first full rebuild resolves with corpus-wide context. Its
// Local07 calls two methods defined in OTHER files, the shape the scoped
// resolver cannot bind in isolation (entity name "T11.Do11" vs call-site stub
// "Do11"). Successive passes change only a literal inside the body, so the two
// cross-file calls are unchanged in source at every pass and a full rebuild
// binds them every time.
//
// The baseline must already contain the calls: an edge the edit ITSELF
// introduces has no prior binding to replay, and closing that (a call the scoped
// resolver cannot bind on first sight) is a separate gap in sresolver's ladder,
// not the #6090 loss class.
func pr6090Edited(pass int) string {
	return fmt.Sprintf(`package svc

type T07 struct{ N int }

func (t *T07) Do07(x int) int { return x + t.N + 7 }

func Helper07(x int) int { return x + 7 }

func Local07(x int) int {
	a := &T11{N: 1}
	b := &T23{N: 2}
	return Helper07(x) + a.Do11(x) + b.Do23(x) + %d
}
`, pass)
}

// pr6090CorpusSize is small enough to run in ~1.5s and large enough that the
// scoped resolver has a real corpus to fail to bind against.
const pr6090CorpusSize = 40

func pr6090WriteCorpus(t *testing.T, repo string) {
	t.Helper()
	for i := 0; i < pr6090CorpusSize; i++ {
		dvWriteFile(t, repo, fmt.Sprintf("r%02d.go", i), pr6090File(i))
	}
	// r07.go carries the cross-file calls from the very first build.
	dvWriteFile(t, repo, "r07.go", pr6090Edited(0))
}

// pr6090IsCallsKey reports whether a parity report key names a CALLS group.
//
// The key is NOT always "<from>→<to>:CALLS": parity.keyWithCount (parity.go:554)
// renders a group of n>1 identical rows as "<from>→<to>:CALLS ×3". A plain
// HasSuffix(":CALLS") check therefore skips exactly the duplicated groups —
// so a lost or invented CALLS multiplicity, the #6094 failure shape, would be
// invisible to every assertion built on it.
func pr6090IsCallsKey(k string) bool {
	if i := strings.LastIndex(k, " ×"); i > 0 {
		k = k[:i]
	}
	return strings.HasSuffix(k, ":CALLS")
}

// pr6090CallsDiff returns the CALLS groups a full rebuild of the SAME end-state
// has that the incremental graph does not (lost), and those the incremental
// graph has that the full rebuild does not (invented). Both directions matter:
// asserting only on `lost` would accept a graph that replaced a correct edge
// with a wrong one. Read off the PERSISTED graphs (never the run log — #6094)
// and compared as multisets in both directions (#6037).
func pr6090CallsDiff(t *testing.T, incremental, full *graph.Document) (lost, invented []string) {
	t.Helper()
	rep := parity.CompareWithOptions(full, incremental, parity.Options{})
	for _, k := range rep.RelsOnlyInA {
		if pr6090IsCallsKey(k) {
			lost = append(lost, k)
		}
	}
	for _, k := range rep.RelsOnlyInB {
		if pr6090IsCallsKey(k) {
			invented = append(invented, k)
		}
	}
	// A group present on BOTH sides at different multiplicities is neither
	// "only in A" nor "only in B" — it lands here, and is a divergence too.
	for _, d := range rep.RelMultiplicityDiffs {
		if pr6090IsCallsKey(d.Key) {
			lost = append(lost, d.Key+" ("+d.Detail+")")
		}
	}
	return lost, invented
}

// pr6090AssertCallsParity fails when the incremental graph diverges from the
// full rebuild on CALLS edges in either direction.
func pr6090AssertCallsParity(t *testing.T, label string, incremental, full *graph.Document) {
	t.Helper()
	lost, invented := pr6090CallsDiff(t, incremental, full)
	if len(lost) != 0 {
		t.Errorf("%s: incremental graph is MISSING %d CALLS group(s) a full rebuild of the "+
			"identical end-state holds — the single-file re-extraction could not re-resolve them "+
			"and the prior resolved copy was pruned (#6090):\n  %s",
			label, len(lost), strings.Join(lost, "\n  "))
	}
	if len(invented) != 0 {
		t.Errorf("%s: incremental graph holds %d CALLS group(s) a full rebuild does NOT:\n  %s",
			label, len(invented), strings.Join(invented, "\n  "))
	}
}

// pr6090CountResolvedCrossFileCalls counts the CALLS edges that run from an
// entity of `file` to a live entity in a DIFFERENT file. This is the population
// #6090 destroys — the outgoing, corpus-resolved edges of a re-extracted file —
// so it is the positive form of the assertion: not "nothing is missing" (true of
// an empty graph) but "these specific edges are present and resolved".
func pr6090CountResolvedCrossFileCalls(doc *graph.Document, file string) int {
	byID := make(map[string]*graph.Entity, len(doc.Entities))
	for i := range doc.Entities {
		byID[doc.Entities[i].ID] = &doc.Entities[i]
	}
	n := 0
	for _, r := range doc.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		from, ok := byID[r.FromID]
		if !ok || from.SourceFile != file {
			continue
		}
		to, ok := byID[r.ToID] // unresolved stubs are not entity ids → excluded
		if !ok || to.SourceFile == file || to.SourceFile == "" {
			continue
		}
		n++
	}
	return n
}

// TestIncremental_PriorResolutionSurvivesRepeatedEdits_6090 is the regression
// gate. The defect is monotone, so a single-edit assertion could pass while the
// loss continues — the edge must survive THREE successive edits to the same
// file, each checked against a full rebuild of that pass's end-state.
func TestIncremental_PriorResolutionSurvivesRepeatedEdits_6090(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	pr6090WriteCorpus(t, repo)
	dvFullRebuild(t, repo, stateDir)

	// Reference corpus, rebuilt from scratch at every pass's end-state.
	fullRepo := t.TempDir()
	pr6090WriteCorpus(t, fullRepo)

	// Positive baseline — without it every assertion below is satisfied by a
	// graph with no CALLS edges at all, and the case rots into vacuity the day
	// the fixture stops producing them. The two cross-file method calls must be
	// present AND resolved in the very first full rebuild.
	base, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load baseline: %v", err)
	}
	const wantCrossFile = 2
	if n := pr6090CountResolvedCrossFileCalls(base, "r07.go"); n != wantCrossFile {
		t.Fatalf("fixture is inert: the baseline full rebuild holds %d resolved cross-file CALLS edge(s) "+
			"out of r07.go, want %d — the fixture no longer exercises #6090", n, wantCrossFile)
	}

	for pass := 1; pass <= 3; pass++ {
		dvSeedManifest(t, repo, stateDir)
		dvWriteFile(t, repo, "r07.go", pr6090Edited(pass))
		// dvIncremental fails the test if TryIncremental falls back, so a
		// silent full-reindex cannot make this vacuous.
		dvIncremental(t, repo, stateDir)
		b, err := graph.LoadGraphFromDir(stateDir)
		if err != nil {
			t.Fatalf("pass %d: load persisted incremental graph: %v", pass, err)
		}

		dvWriteFile(t, fullRepo, "r07.go", pr6090Edited(pass))
		c := dvFullRebuild(t, fullRepo, t.TempDir())

		pr6090AssertCallsParity(t, fmt.Sprintf("pass %d", pass), b, c)

		// Positive form: the edges are still THERE and still resolved, not
		// merely "not reported missing".
		if n := pr6090CountResolvedCrossFileCalls(b, "r07.go"); n != wantCrossFile {
			t.Errorf("pass %d: incremental graph holds %d resolved cross-file CALLS edge(s) out of "+
				"r07.go, want %d", pass, n, wantCrossFile)
		}
	}
}

// pr6090EditedN is the pass-N content of file r<i>.go, whose Local<i> calls a
// method on two OTHER files' types. Same shape as pr6090Edited, parameterised
// over which file is edited.
func pr6090EditedN(i, a, b, pass int) string {
	return fmt.Sprintf(`package svc

type T%02d struct{ N int }

func (t *T%02d) Do%02d(x int) int { return x + t.N + %d }

func Helper%02d(x int) int { return x + %d }

func Local%02d(x int) int {
	p := &T%02d{N: 1}
	q := &T%02d{N: 2}
	return Helper%02d(x) + p.Do%02d(x) + q.Do%02d(x) + %d
}
`, i, i, i, i, i, i, i, a, b, i, a, b, pass)
}

// TestIncremental_PriorResolutionAcrossDifferentFiles_6090 pins the property that makes
// #6090 severe: the deficit is MONOTONE across edits to DIFFERENT files. Each
// edited file permanently contributes its own unresolvable outgoing edges, so a
// long-lived watched repo degrades continuously and nothing restores the edges
// before a full reindex.
//
// Measured at 2f0175dfc on this fixture: 3 → 6 → 9 CALLS edges missing versus a
// full rebuild of the same end-state, growing by one file's worth per edit. The gate
// asserts the fixed point the fix lands on instead: ZERO, at every step.
//
// The accumulation is asserted directly rather than described: the count of
// resolved cross-file CALLS edges out of the files edited SO FAR must grow
// 2 → 4 → 6 and never shed. That number is precisely the population the defect
// destroys, so on an unfixed tree it stays flat at 0 while the fixture keeps
// adding to it — and a test that only asked "is anything missing?" would be
// satisfied by a graph that had never held the edges at all.
//
// The same-file three-edit gate above cannot see this — re-editing one file
// re-loses the SAME edges, so its deficit is constant, not growing.
func TestIncremental_PriorResolutionAcrossDifferentFiles_6090(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	edits := []int{7, 11, 23}

	// Baseline: every edited file already carries its cross-file calls, so the
	// first full rebuild resolves them with corpus-wide context.
	seed := func(dst string) {
		for i := 0; i < pr6090CorpusSize; i++ {
			dvWriteFile(t, dst, fmt.Sprintf("r%02d.go", i), pr6090File(i))
		}
		for _, idx := range edits {
			dvWriteFile(t, dst, fmt.Sprintf("r%02d.go", idx),
				pr6090EditedN(idx, (idx+3)%pr6090CorpusSize, (idx+5)%pr6090CorpusSize, 0))
		}
	}
	seed(repo)
	dvFullRebuild(t, repo, stateDir)

	fullRepo := t.TempDir()
	seed(fullRepo)

	// Positive baseline: each of the three files really does hold 2 resolved
	// cross-file CALLS edges before anything is edited.
	base, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load baseline: %v", err)
	}
	for _, idx := range edits {
		name := fmt.Sprintf("r%02d.go", idx)
		if n := pr6090CountResolvedCrossFileCalls(base, name); n != 2 {
			t.Fatalf("fixture is inert: baseline full rebuild holds %d resolved cross-file CALLS "+
				"edge(s) out of %s, want 2", n, name)
		}
	}

	var trace []string
	prevAtRisk := -1
	for pass, idx := range edits {
		content := pr6090EditedN(idx, (idx+3)%pr6090CorpusSize, (idx+5)%pr6090CorpusSize, pass+1)
		name := fmt.Sprintf("r%02d.go", idx)

		dvSeedManifest(t, repo, stateDir)
		dvWriteFile(t, repo, name, content)
		dvIncremental(t, repo, stateDir)
		b, err := graph.LoadGraphFromDir(stateDir)
		if err != nil {
			t.Fatalf("pass %d: load persisted incremental graph: %v", pass+1, err)
		}

		dvWriteFile(t, fullRepo, name, content)
		c := dvFullRebuild(t, fullRepo, t.TempDir())

		lost, invented := pr6090CallsDiff(t, b, c)
		pr6090AssertCallsParity(t, fmt.Sprintf("pass %d (edited %s)", pass+1, name), b, c)

		// The accumulating population: every file edited SO FAR must still hold
		// its resolved cross-file calls. This is the monotone axis — it grows by
		// 2 per pass here and is flat at 0 on an unfixed tree.
		atRisk := 0
		for _, done := range edits[:pass+1] {
			atRisk += pr6090CountResolvedCrossFileCalls(b, fmt.Sprintf("r%02d.go", done))
		}
		want := 2 * (pass + 1)
		trace = append(trace, fmt.Sprintf("pass %d (edited %s): lost=%d invented=%d resolved-cross-file-calls-out-of-edited-files=%d",
			pass+1, name, len(lost), len(invented), atRisk))
		if atRisk != want {
			t.Errorf("pass %d (edited %s): %d resolved cross-file CALLS edge(s) survive out of the "+
				"%d file(s) edited so far, want %d — the deficit accumulates per edited file (#6090)",
				pass+1, name, atRisk, pass+1, want)
		}
		if atRisk < prevAtRisk {
			t.Errorf("pass %d: the surviving population SHRANK %d → %d — successive edits are still "+
				"shedding edges", pass+1, prevAtRisk, atRisk)
		}
		prevAtRisk = atRisk
	}
	t.Logf("per-pass trace:\n  %s", strings.Join(trace, "\n  "))
}

// TestIncremental_DeletedCallIsStillDropped_6090 is the counter-test: the fix
// must not degrade into "retain everything". A call REMOVED from source must
// disappear from the incremental graph exactly as it does from a full rebuild.
//
// Without it, a fix that carried the prior edge forward whenever the fresh pass
// could not re-emit it would keep a call that no longer exists in source,
// invisibly and forever.
func TestIncremental_DeletedCallIsStillDropped_6090(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	pr6090WriteCorpus(t, repo)
	dvFullRebuild(t, repo, stateDir)

	// Pass 1: introduce the two cross-file method calls and let the fix bind
	// them from the prior graph's resolution.
	dvSeedManifest(t, repo, stateDir)
	dvWriteFile(t, repo, "r07.go", pr6090Edited(1))
	dvIncremental(t, repo, stateDir)

	// Pass 2: delete BOTH cross-file calls from source.
	const deleted = `package svc

type T07 struct{ N int }

func (t *T07) Do07(x int) int { return x + t.N + 7 }

func Helper07(x int) int { return x + 7 }

func Local07(x int) int { return Helper07(x) }
`
	dvSeedManifest(t, repo, stateDir)
	dvWriteFile(t, repo, "r07.go", deleted)
	dvIncremental(t, repo, stateDir)

	b, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load persisted incremental graph: %v", err)
	}

	// Reference: a full rebuild of the deleted end-state.
	fullRepo := t.TempDir()
	pr6090WriteCorpus(t, fullRepo)
	dvWriteFile(t, fullRepo, "r07.go", deleted)
	c := dvFullRebuild(t, fullRepo, t.TempDir())

	// Control: the reference must genuinely not hold the deleted calls, or the
	// assertion below is vacuous.
	if pr6090HasCallTo(c, "Do11") || pr6090HasCallTo(c, "Do23") {
		t.Fatal("premise violated: the full rebuild of the deleted end-state still holds the removed calls")
	}
	if pr6090HasCallTo(b, "Do11") || pr6090HasCallTo(b, "Do23") {
		t.Error("incremental graph retained a CALLS edge to a method whose call site was DELETED from source — " +
			"the #6090 fix must replay the prior resolution for edges the fresh pass re-emitted, " +
			"never retain edges the fresh pass did not emit at all")
	}

	// And the deleted end-state must be at strict CALLS parity with the full
	// rebuild in BOTH directions — nothing lost, nothing invented.
	pr6090AssertCallsParity(t, "after the deletion", b, c)
}

// pr6090HasCallTo reports whether doc holds a CALLS edge whose target is the
// entity named `<type>.<name>` (resolved form) or the bare stub `name`
// (unresolved form). Both forms count: the point of the assertion is that the
// deleted call is gone in EITHER shape.
func pr6090HasCallTo(doc *graph.Document, name string) bool {
	byID := make(map[string]string, len(doc.Entities))
	for _, e := range doc.Entities {
		byID[e.ID] = e.Name
	}
	for _, r := range doc.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		if r.ToID == name {
			return true
		}
		if n, ok := byID[r.ToID]; ok && (n == name || strings.HasSuffix(n, "."+name)) {
			return true
		}
	}
	return false
}
