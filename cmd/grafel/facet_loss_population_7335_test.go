package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Issue #7335, arm 2 — the 38-fixture sweep, as a graded instrument.
//
// Arm 1 (91c4fd663) landed the detector and measured the population ONCE, in a
// throwaway sweep over every golden fixture with a src/ tree. That sweep is how
// elixir-phoenix-mini was found — the hand-picked roster of the first attempt
// missed it, and the arithmetic of the table it replaced did not even close.
// But a one-off sweep is not a test: nothing re-runs it, so the next fixture
// that starts losing a facet is exactly as invisible as swift's `extension User`
// was for years.
//
// This file re-runs it and compares the WHOLE population against a recorded
// baseline, in both directions:
//
//   - a fixture that starts losing a facet fails (a new destroyed entity);
//   - a fixture that STOPS losing one also fails. A silent improvement is still
//     a change to a recorded measurement, and the thing #7335 is about is
//     silence that was never earned. If a producer fix or a fold removes a
//     loss, that is a result worth a line in a diff, not a number that quietly
//     drifts to zero;
//   - a fixture whose drops fall to zero fails even if it never lost anything,
//     because a fixture that no longer reaches the dedup branch grades nothing.
//     That is the arm-1 review finding (four of five roster entries were an
//     empty loop) applied to the whole population rather than to five names.
//
// THE FIXTURE LIST IS DERIVED FROM DISK, NEVER HAND-MAINTAINED. A roster with
// no referent is #6887 and has bitten this repo repeatedly — including inside
// this very issue. facetLossPopulationFixtures globs
// internal/quality/golden/*/src, so a new golden fixture enters the measurement
// by existing. What the baseline holds is not the list: it is the MEASUREMENT
// KEYED BY the list, and a name present on disk but absent from the baseline is
// a failure that names the fixture (not a silent skip, and not a count that
// still adds up).
//
// THE BASELINE PINS NAMES *AND* A FLOOR — THEY ARE NOT ALTERNATIVES. A sweep
// that discovers zero fixtures, or silently skips half of them, reports a
// perfectly clean tree: a clean tree and an empty loop are the same output.
// Names are what catch "the wrong 38", which a bare count cannot. But every
// name-based check compares against the RECORDED baseline, so all of them are
// defeated by narrowing the enumeration and regenerating once — and the
// floor is the only assertion that survives that. The earlier version of this
// paragraph argued the floor was redundant. It was a false dichotomy, and the
// hole it left was measured: see facetLossFixtureFloor.
//
// COST, MEASURED. The sweep indexes every fixture in-process, serially.
// Twelve clean-tree runs at GOMAXPROCS=3 on two machines: 11.5-14.6s of test
// time (the author measured 11.5-12.0s over five runs, an independent reviewer
// 13.6-14.6s over seven — quote the range, not the optimistic end), against
// ~178s measured ONCE on this branch for the rest of this package. Every
// fixture is indexed with default-ON extractors — no custom gate — so this
// measures what a user's index does. It is skipped under `-short`. ~8% of the
// package is small enough to belong in the normal suite; if the -race leg
// (roughly 7x) ever makes it unwelcome, the next stop is its own gate —
// quality.yml exists for exactly this class of always-on measurement — NOT a
// quiet deletion of the assertion.
//
// REGENERATING. Set GRAFEL_UPDATE_7335_POPULATION=1 to rewrite the baseline
// from the current tree. Do it deliberately: every line of the diff is a real
// change to what assembly destroys, and the diff is the review artefact.
//
// WHAT IS GRADED BY WHAT, AND WHAT IS ONLY A MESSAGE. Each assertion below was
// scored against a planted violation that makes it fire; the ones that no
// probe can isolate are labelled as such rather than left to read as grading:
//
//   - the per-fixture diff is graded by a baseline whose loss CONTENT differs
//     while every count matches — counts alone do not see it;
//   - its two directions are graded AS A PAIR, by a baseline whose loss row
//     is retyped in place (counts and row counts unchanged, so the row stays
//     self-consistent): suppressing either direction alone still fails,
//     suppressing both passes. They USED to be gradeable separately, by a
//     baseline with a row added or removed — the per-row self-consistency
//     check below now rejects such a baseline earlier, which is a better
//     failure and a weaker isolation. Recorded because it is a drift: the
//     fix for one review finding cost the isolation of another;
//   - loss rows are compared as a MULTISET. A set comparison let one row be
//     swapped for a duplicate of another with every count unchanged, and the
//     shipped baseline already contains a duplicate for that to hide behind;
//   - both source-level plants (a second `extension User` added to, and the
//     existing one removed from, the shipped swift fixture) fire end to end
//     through the real indexer, not just through the comparator;
//   - the enumeration checks and the stored totals BACK EACH OTHER UP. An
//     empty enumeration is caught three times over (the empty-Fatal, the
//     baseline-names-a-fixture-not-found check, and the totals row count), so
//     no single one of them is independently graded — deleting any one alone
//     leaves the probe failing. They are killed as a set, and that is stated
//     here rather than implied to be three independent claims;
//   - the zero-drops branch below is a MESSAGE, not a claim. Deleting it is
//     ALIVE: a fixture whose drops fall to zero is already reported by the
//     diff as `drops: N -> 0`. It exists to name the remedy, and it must not
//     be counted as the thing that grades the collision floor. So is the
//     empty-enumeration Fatal (implied by the floor) and the cap-exceeded
//     message (implied by the attribution-agreement check) — three messages,
//     labelled, so nobody counts them as assertions;
//   - EVERY CHECK ABOVE EXCEPT THE FLOOR IS DEFEATED BY ONE REGENERATION.
//     They compare the sweep against the recorded baseline, so narrowing the
//     enumeration and regenerating once makes the smaller population the new
//     truth with nothing red on the way — measured: 34/13/5 became 3/0/0 and
//     stayed green. facetLossFixtureFloor and the anti-shrink guard in
//     facetLossWriteBaseline are the answer, and they are the only two
//     assertions here that survive a regeneration.
//
// WHAT THE ENUMERATION STILL CANNOT SEE. It requires <fixture>/src to be a
// directory, so a future golden fixture laid out without one is outside "the
// whole population" and nothing says so. Not live today (38 of 38 have src/),
// and recorded here rather than implied away.

// facetLossGoldenDir is the fixture root. Every directory under it holding a
// src/ tree is in the population, by construction.
const facetLossGoldenDir = "../../internal/quality/golden"

const facetLossPopulationBaseline = "testdata/facet_loss_population_7335.json"

// facetLossFixtureFloor is the smallest population this sweep may measure.
//
// IT IS THE ONLY ASSERTION THAT SURVIVES REGENERATION, which is why it sits
// ALONGSIDE the name-set comparison rather than instead of it. The three
// enumeration backstops (the empty-Fatal, the baseline-names-a-fixture-not-
// found check, the totals row count) all compare against the RECORDED
// baseline, so they protect against a partial sweep only while the baseline is
// unchanged. Review measured the hole: narrow the enumeration, run ONCE with
// GRAFEL_UPDATE_7335_POPULATION=1, and the baseline is rewritten to the
// surviving handful — the #7335 headline collapsed from 34/13/5 to 3/0/0 and
// every later run was green. The first remedy the old failure message
// suggested was "regenerate", i.e. the one action that must not be taken for
// that failure.
//
// A floor alone is satisfied by "38 of the wrong ones", which is why it is not
// sufficient. It is also the only thing a regeneration cannot talk its way
// past, which is why it is necessary. Both, or the instrument has an off
// switch.
//
// IT MAY ONLY BE RAISED. A golden fixture added under internal/quality/golden/
// raises it. Nothing legitimate lowers it: if a fixture is genuinely deleted,
// lowering this number is a deliberate, reviewable line in a diff — exactly
// the property the regeneration path lacks.
const facetLossFixtureFloor = 38

// facetLossPopulationEntry is one fixture's measured loss.
//
// Counts AND content. The counts make a regression a one-line diff; Losses
// makes it actionable and keeps the counts honest, because a count is
// satisfied by the wrong pair (arm 1's `unrecoverable=N` was a count that read
// as a different quantity than it measured).
type facetLossPopulationEntry struct {
	Drops        int            `json:"drops"`
	LostRecords  int            `json:"lost_records"`
	LostEntities int            `json:"lost_entities"`
	Conflicts    int            `json:"conflicts"`
	ByField      map[string]int `json:"by_field,omitempty"`
	// Losses is every attributed conflict as
	// "kind|name|file|field|survivor|dropped", sorted. It is bounded by
	// facetLossSampleCap, which the test asserts is not being hit.
	Losses []string `json:"losses,omitempty"`
}

// facetLossPopulationTotals is the headline #7335 reports. It is stored, and
// it is also re-derived from the per-fixture rows: a baseline whose totals
// disagree with its own rows has been hand-edited, and this is the only check
// that can see that.
type facetLossPopulationTotals struct {
	Fixtures          int `json:"fixtures"`
	FixturesColliding int `json:"fixtures_colliding"`
	FixturesLosing    int `json:"fixtures_losing"`
	Drops             int `json:"drops"`
	LostRecords       int `json:"lost_records"`
	LostEntities      int `json:"lost_entities"`
	Conflicts         int `json:"conflicts"`
}

type facetLossPopulation struct {
	Totals   facetLossPopulationTotals           `json:"totals"`
	Fixtures map[string]facetLossPopulationEntry `json:"fixtures"`
}

// facetLossPopulationFixtures enumerates the fixtures from disk. It returns
// the sorted directory names of every golden fixture with a src/ tree.
//
// It Fatals on an empty result. An enumeration that finds nothing is the one
// failure mode that reports a clean tree while measuring nothing at all, and
// it must never be reachable through the pass path.
func facetLossPopulationFixtures(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(facetLossGoldenDir, "*", "src"))
	if err != nil {
		t.Fatalf("glob golden fixtures: %v", err)
	}
	var names []string
	for _, m := range matches {
		info, serr := os.Stat(m)
		if serr != nil || !info.IsDir() {
			continue
		}
		names = append(names, filepath.Base(filepath.Dir(m)))
	}
	sort.Strings(names)
	// A MESSAGE, NOT A SECOND CLAIM: strictly implied by the floor below
	// (which is 38, so zero fails there too). It is kept because "discovered
	// nothing" and "discovered too few" have different causes and the reader
	// should not have to infer which one happened.
	if len(names) == 0 {
		t.Fatalf("enumerated zero golden fixtures under %s — a sweep that discovers nothing "+
			"reports a perfectly clean tree; fix the enumeration rather than the baseline",
			facetLossGoldenDir)
	}
	if len(names) < facetLossFixtureFloor {
		t.Fatalf("enumerated %d golden fixtures under %s; the floor is %d. THE ENUMERATION IS "+
			"PARTIAL, and a partial sweep reports a clean tree. Do NOT regenerate: regenerating "+
			"a narrowed sweep bakes the smaller population in (measured — the headline collapses "+
			"from 34/13/5 to 3/0/0 and every later run is green). Fix the enumeration; lower "+
			"facetLossFixtureFloor only as a reviewed line in the diff when a fixture is "+
			"genuinely deleted. Found: %v",
			len(names), facetLossGoldenDir, facetLossFixtureFloor, names)
	}
	return names
}

// facetLossMeasure indexes one fixture and folds its stats into a comparable
// entry. Unlike facetLossOnFixture it does NOT skip when the tree is missing:
// the caller enumerated it from disk, so a missing tree is a broken sweep, and
// a skip here would be a partial enumeration wearing a green tick.
func facetLossMeasure(t *testing.T, fixture string) facetLossPopulationEntry {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join(facetLossGoldenDir, fixture, "src"))
	if err != nil {
		t.Fatalf("abs(%s): %v", fixture, err)
	}
	idx := newTestIndexer(t, fixture, nil, "")
	if _, rerr := idx.Run(context.Background(), abs); rerr != nil {
		t.Fatalf("Run(%s): %v", fixture, rerr)
	}
	s := idx.facetLoss
	e := facetLossPopulationEntry{
		Drops:        s.Drops,
		LostRecords:  s.LostRecords,
		LostEntities: s.LostEntities,
		Conflicts:    s.Conflicts,
	}
	if len(s.ByField) > 0 {
		e.ByField = map[string]int{}
		for k, v := range s.ByField {
			e.ByField[k] = v
		}
	}
	// The retained attribution must account for every conflict, or the
	// content half of this baseline is silently truncated while the counts
	// stay exact. facetLossSampleCap is 64 and the whole measured population
	// is 34 conflicts, so hitting it means something changed by an order of
	// magnitude and the pinned Losses stopped being the whole story.
	//
	// THE FIRST OF THE TWO IS A MESSAGE, NOT A CLAIM. Records is capped and
	// Conflicts is not, so Conflicts > cap FORCES len(Records) != Conflicts:
	// the agreement check below fires on its own (verified — with the cap at
	// 2, deleting this branch leaves the run red via that message alone, and
	// at 65 both fire). It survives because "the cap was hit" and "the
	// attribution disagrees" have different remedies. The consequence worth
	// stating: the content half is never SILENTLY partial — the agreement
	// check says so loudly.
	if s.Conflicts > facetLossSampleCap {
		t.Errorf("%s: %d conflicts exceeds the %d-record attribution cap; the pinned losses "+
			"are now a truncated sample and this baseline no longer pins content",
			fixture, s.Conflicts, facetLossSampleCap)
	}
	if len(s.Records) != s.Conflicts {
		t.Errorf("%s: %d attributed records for %d conflicts; they must agree below the cap",
			fixture, len(s.Records), s.Conflicts)
	}
	for _, r := range s.Records {
		e.Losses = append(e.Losses, strings.Join([]string{
			r.Kind, r.Name, r.SourceFile, r.Field, r.Survivor, r.Dropped,
		}, "|"))
	}
	sort.Strings(e.Losses)
	return e
}

func facetLossDeriveTotals(fixtures map[string]facetLossPopulationEntry) facetLossPopulationTotals {
	tot := facetLossPopulationTotals{Fixtures: len(fixtures)}
	for _, e := range fixtures {
		if e.Drops > 0 {
			tot.FixturesColliding++
		}
		if e.LostRecords > 0 {
			tot.FixturesLosing++
		}
		tot.Drops += e.Drops
		tot.LostRecords += e.LostRecords
		tot.LostEntities += e.LostEntities
		tot.Conflicts += e.Conflicts
	}
	return tot
}

// TestFacetLossPopulation_AllGoldenFixtures is the sweep.
func TestFacetLossPopulation_AllGoldenFixtures(t *testing.T) {
	if testing.Short() {
		t.Skip("indexes every golden fixture with a src/ tree; see this file's COST note")
	}
	fixtures := facetLossPopulationFixtures(t)

	observed := map[string]facetLossPopulationEntry{}
	for _, f := range fixtures {
		f := f
		t.Run(f, func(t *testing.T) {
			observed[f] = facetLossMeasure(t, f)
		})
	}
	// A subtest failure means a fixture was never measured; comparing a
	// hole against the baseline would report it as "stopped colliding",
	// which is a false finding on top of a real one.
	if t.Failed() {
		t.Fatal("a fixture failed to index; the population comparison below would misreport it")
	}

	if os.Getenv("GRAFEL_UPDATE_7335_POPULATION") == "1" {
		facetLossWriteBaseline(t, observed)
		t.Logf("rewrote %s from %d fixtures", facetLossPopulationBaseline, len(observed))
		return
	}

	want := facetLossReadBaseline(t)

	// The baseline's own totals must agree with its own rows. Nothing else
	// can catch a hand-edited headline, and the headline is what the issue
	// quotes.
	if derived := facetLossDeriveTotals(want.Fixtures); derived != want.Totals {
		t.Errorf("baseline totals %+v disagree with its own %d rows %+v — the file has been "+
			"hand-edited; regenerate with GRAFEL_UPDATE_7335_POPULATION=1",
			want.Totals, len(want.Fixtures), derived)
	}
	// EACH ROW MUST AGREE WITH ITSELF, TOO. len(losses) == conflicts ==
	// sum(by_field) holds for every measurement this sweep produces, but
	// nothing checked it of the STORED rows — so a hand-edit that adds,
	// removes or retypes one loss row left a baseline that is internally
	// impossible and still passed. (The identity holds only below
	// facetLossSampleCap; above it the attribution is a sample and the
	// agreement check in facetLossMeasure is what says so.)
	var rowNames []string
	for f := range want.Fixtures {
		rowNames = append(rowNames, f)
	}
	sort.Strings(rowNames)
	for _, f := range rowNames {
		e := want.Fixtures[f]
		sum := 0
		for _, n := range e.ByField {
			sum += n
		}
		if len(e.Losses) != e.Conflicts || sum != e.Conflicts {
			t.Errorf("baseline row %s is internally impossible: %d losses, %d conflicts, "+
				"by_field summing to %d — all three count the same thing; the file has been "+
				"hand-edited", f, len(e.Losses), e.Conflicts, sum)
		}
	}

	// Enumeration first, and BY NAME. A partial sweep that measured 20 of
	// 38 fixtures would otherwise report every unmeasured fixture as having
	// stopped colliding, and an empty one would report a clean tree.
	for _, f := range fixtures {
		if _, ok := want.Fixtures[f]; !ok {
			t.Errorf("golden fixture %s exists on disk but is not in %s — a new fixture joins "+
				"this measurement automatically and its population must be recorded, not skipped; "+
				"regenerate with GRAFEL_UPDATE_7335_POPULATION=1 and review the added row",
				f, facetLossPopulationBaseline)
		}
	}
	onDisk := map[string]bool{}
	for _, f := range fixtures {
		onDisk[f] = true
	}
	for f := range want.Fixtures {
		if !onDisk[f] {
			// REMEDY ORDER MATTERS. This message used to lead with
			// "regenerate", which is the one action that turns this exact
			// failure into a permanently smaller measurement. Partial
			// enumeration first; regeneration last, and only for a fixture
			// that really was deleted.
			t.Errorf("baseline names fixture %s, which the sweep did not find under %s — THE "+
				"ENUMERATION MAY BE PARTIAL, and a partial sweep reports a clean tree. Do not "+
				"regenerate to make this green: regenerating a narrowed sweep bakes the smaller "+
				"population in. Only if the fixture was genuinely deleted, regenerate and lower "+
				"facetLossFixtureFloor",
				f, facetLossGoldenDir)
		}
	}
	if t.Failed() {
		return
	}

	for _, f := range fixtures {
		w, o := want.Fixtures[f], observed[f]
		// A MESSAGE, NOT A SECOND CLAIM: the diff below already reports
		// `drops: N -> 0`. Deleting this branch is an ALIVE mutant. It is
		// here because "this fixture now grades nothing" is the actionable
		// reading of that line, and the arm-1 review finding was precisely
		// that nobody read it that way.
		if w.Drops > 0 && o.Drops == 0 {
			t.Errorf("%s no longer reaches the dedup branch (baseline %d drops). Zero drops means "+
				"this fixture now grades nothing: confirm the producer/fold change that removed "+
				"the collision and regenerate",
				f, w.Drops)
		}
		if diff := facetLossDiffEntry(w, o); diff != "" {
			t.Errorf("%s: the recorded facet-loss population changed.\n%s\n"+
				"Both directions are failures: a new loss is a destroyed entity nobody accounted "+
				"for, and a removed one is a fix whose effect must be recorded rather than "+
				"drifting silently. Confirm the cause, then regenerate with "+
				"GRAFEL_UPDATE_7335_POPULATION=1", f, diff)
		}
	}

	// The headline, asserted as a whole. Per-fixture equality implies it,
	// but an arithmetic claim that nobody re-derives is how arm 1's first
	// table came to contain "9 of 14" over a universe of 13.
	if got, w := facetLossDeriveTotals(observed), want.Totals; got != w {
		t.Errorf("population totals %+v, baseline %+v", got, w)
	}
}

// facetLossDiffEntry renders the difference between two measurements, naming
// the direction of each change.
func facetLossDiffEntry(want, got facetLossPopulationEntry) string {
	var b strings.Builder
	num := func(field string, w, g int) {
		if w != g {
			fmt.Fprintf(&b, "  %s: %d -> %d\n", field, w, g)
		}
	}
	num("drops", want.Drops, got.Drops)
	num("lost_records", want.LostRecords, got.LostRecords)
	num("lost_entities", want.LostEntities, got.LostEntities)
	num("conflicts", want.Conflicts, got.Conflicts)
	// The union of both key sets, not facetLossFields: a field this
	// detector does not know about yet must still show up in the diff.
	seen := map[string]bool{}
	var fields []string
	for f := range want.ByField {
		if !seen[f] {
			seen[f], fields = true, append(fields, f)
		}
	}
	for f := range got.ByField {
		if !seen[f] {
			seen[f], fields = true, append(fields, f)
		}
	}
	sort.Slice(fields, func(a, b int) bool {
		if ra, rb := facetLossRank(fields[a]), facetLossRank(fields[b]); ra != rb {
			return ra < rb
		}
		return fields[a] < fields[b]
	})
	for _, f := range fields {
		num("by_field."+f, want.ByField[f], got.ByField[f])
	}
	// MULTISET, NOT SET. Multiplicity is real data here: rust-tokio-mini's
	// baseline names the struct-vs-impl subtype loss TWICE, because two impl
	// blocks destroy the same facet under one id. A set comparison lets one
	// row be swapped for a duplicate of another with every count unchanged —
	// the baseline then asserts a loss that did not happen and omits one that
	// did, and nothing sees it. That was mutant M-DUP, and it was ALIVE until
	// this counted the rows.
	countOf := func(ls []string) map[string]int {
		m := map[string]int{}
		for _, l := range ls {
			m[l]++
		}
		return m
	}
	cw, cg := countOf(want.Losses), countOf(got.Losses)
	var rows []string
	for l := range cw {
		rows = append(rows, l)
	}
	for l := range cg {
		if _, dup := cw[l]; !dup {
			rows = append(rows, l)
		}
	}
	sort.Strings(rows)
	for _, l := range rows {
		if d := cg[l] - cw[l]; d > 0 {
			fmt.Fprintf(&b, "  NEW LOSS      x%d %s\n", d, l)
		} else if d < 0 {
			fmt.Fprintf(&b, "  LOSS DISAPPEARED x%d %s\n", -d, l)
		}
	}
	// (A row-count summary line lived here and was deleted after scoring: any
	// length difference implies some row's count differs, so the loop above
	// already prints it. A fourth un-labelled message is how a decorative
	// line gets read as an assertion.)
	return b.String()
}

func facetLossReadBaseline(t *testing.T) facetLossPopulation {
	t.Helper()
	raw, err := os.ReadFile(facetLossPopulationBaseline)
	if err != nil {
		t.Fatalf("read %s: %v", facetLossPopulationBaseline, err)
	}
	var p facetLossPopulation
	if uerr := json.Unmarshal(raw, &p); uerr != nil {
		t.Fatalf("parse %s: %v", facetLossPopulationBaseline, uerr)
	}
	if len(p.Fixtures) == 0 {
		t.Fatalf("%s records no fixtures; an empty baseline agrees with an empty sweep",
			facetLossPopulationBaseline)
	}
	return p
}

func facetLossWriteBaseline(t *testing.T, fixtures map[string]facetLossPopulationEntry) {
	t.Helper()
	// ANTI-SHRINK. Regeneration is the ONE path that can reduce what this
	// instrument measures without anything going red: narrow the enumeration,
	// regenerate once, and the smaller population is the new truth. So the
	// update path refuses to drop a fixture the baseline already records.
	// Removing a fixture from the measurement now requires editing this file
	// and the floor — a diff a reviewer sees — rather than running a command.
	if raw, rerr := os.ReadFile(facetLossPopulationBaseline); rerr == nil {
		var prev facetLossPopulation
		if json.Unmarshal(raw, &prev) == nil {
			var lost []string
			for f := range prev.Fixtures {
				if _, ok := fixtures[f]; !ok {
					lost = append(lost, f)
				}
			}
			sort.Strings(lost)
			if len(lost) > 0 {
				t.Fatalf("refusing to regenerate: this measurement drops %d fixture(s) the "+
					"baseline already records: %v. Regeneration must never SHRINK the "+
					"population — that is precisely how a partial sweep becomes the new truth "+
					"(the #7335 headline collapses and every later run is green). If these "+
					"fixtures were genuinely deleted, delete their rows and lower "+
					"facetLossFixtureFloor by hand.", len(lost), lost)
			}
		}
	}
	p := facetLossPopulation{Totals: facetLossDeriveTotals(fixtures), Fixtures: fixtures}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}
	if mkerr := os.MkdirAll(filepath.Dir(facetLossPopulationBaseline), 0o755); mkerr != nil {
		t.Fatalf("mkdir testdata: %v", mkerr)
	}
	if werr := os.WriteFile(facetLossPopulationBaseline, append(raw, '\n'), 0o644); werr != nil {
		t.Fatalf("write %s: %v", facetLossPopulationBaseline, werr)
	}
}
