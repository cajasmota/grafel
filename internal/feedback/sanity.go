package feedback

import (
	"fmt"
	"math"
)

// orphanRateFailThreshold is the per-kind orphan rate (percent, exclusive)
// above which the sanity gate fails.
//
// It used to be 100.0, which made the gate unfailable in practice: a kind only
// failed when EVERY entity was orphaned, so one accidental edge anywhere in
// the group pinned it to PASS forever. #6313 is the demonstration — an
// `http_endpoint_definition` orphan rate of 99.6% on a real group sailed
// straight through, and the reporter had to find it by reading the table by
// hand.
//
// 90% is chosen deliberately, not as a round number:
//
//   - The gate answers "is this kind wired up AT ALL?", not "is every instance
//     wired?". The latter is what the per-kind orphan table is for — readable,
//     not gating. A kind where 9 in 10 or more entities carry no semantic edge
//     in either direction is not partially degraded; it is functionally
//     unwired. The comparison is strict (`< 90.0` passes), so 90.0 itself
//     fails — both because the check's published name says "below 90pct" and
//     because 90.0 is the arithmetic ceiling for a 10-entity kind, the
//     smallest size the report publishes.
//
//   - It leaves headroom for genuine per-instance gaps so they stay VISIBLE in
//     the table instead of being converted into a gate failure that swamps the
//     report. #6374 is the concrete case: 23% of http_endpoint_definition
//     entities across 12 corpus repos have no IMPLEMENTS edge and therefore no
//     semantic edge at all. That is a real defect, it is reported as a
//     non-zero orphan count, and it must not fail this gate — a gate that
//     fires on every kind teaches the reader to skip the section, which is
//     exactly how #6313 went unnoticed.
//
//   - Measured on 12 corpus repos (nextjs-commerce, openapi-stripe, nestjs,
//     nestjs-starter, express, express-realworld, django, django-realworld,
//     flask, flask-realworld, spring-petclinic, laravel-routing), counting
//     THIS check only: the old `< 100.0` rule produced 47 failures, of which
//     the majority were sinks miscounted by the old outgoing-only metric;
//     direction-awareness plus this threshold produces 15, none of them a
//     direction artifact. At 75% it would produce 22 and at 50% it would
//     produce 32, with the added entries dominated by kinds in the 50-90% band
//     where "unwired kind" and "kind with real per-instance gaps" are no
//     longer distinguishable.
//
//     Counting ALL sanity checks over the same corpus, the totals are 50
//     before and 32 after (15 here plus 14 from check 2b plus 3 unrelated).
//     Stated because the two figures are easy to confuse and only one of them
//     is a like-for-like comparison of this check.
//
//     Triage of the 9 kinds that newly fail here: 7 (django Config/Module/
//     Route/Test, flask Module, flask-realworld Module, spring-petclinic
//     Config) contain no field-subtype entities at all, so they cannot be an
//     artifact of the per-entity exemptions report.go deleted — they are kinds
//     where >90% of instances carry no semantic edge while a handful do. The
//     other 2 (express-realworld and laravel-routing SCOPE.Schema) ARE that
//     artifact: 43 of 60 and 109 of 112 of their unwired entities are field
//     leaves, which the deleted per-subtype rule exempted individually. See
//     the granularity note on report.go's participation derivation.
const orphanRateFailThreshold = 90.0

// orphanCheckName is the sanity-result name for the per-kind orphan check.
func orphanCheckName(kind string) string {
	return fmt.Sprintf("orphan-rate-below-%.0fpct[%s]", orphanRateFailThreshold, kind)
}

// participationCheckName is the sanity-result name for the per-kind semantic
// participation check.
func participationCheckName(kind string) string {
	return fmt.Sprintf("kind-carries-semantic-edges[%s]", kind)
}

// participationRegressionCheckName is the sanity-result name for the
// longitudinal check: a kind that participated in a PRIOR report for this
// group and carries no semantic edge now (#6377).
func participationRegressionCheckName(kind string) string {
	return fmt.Sprintf("kind-participation-not-regressed[%s]", kind)
}

// SanityResult is the outcome of a single sanity check.
type SanityResult struct {
	Name   string
	Passed bool
	Note   string
}

// What #6346 asked for, and what is actually done here — stated because the
// two are not the same and the gap should not be rediscovered later:
//
//	Direction 1 ("the threshold is exactly 100%, so it can never fire on a
//	real regression") is FIXED. The gate fires at and above
//	orphanRateFailThreshold, it fires on the smallest published bucket
//	(check 2), and the total-breakage end is covered by check 2b.
//
//	Direction 2 ("terminal-by-design kinds fail it, correctly, forever") was
//	relocated by #6346 and is addressed by #6377, though only from the SECOND
//	report onward. As shipped by #6346, all 14 check-2b failures measured per
//	group across the corpus were kinds that also failed at base under
//	`orphan-rate-not-100pct[...]` — same repos, same kinds, new check name.
//	~8 were markdown code fences, CSS selectors, HTML input fields and
//	dependency manifests, which fired every run forever with no action
//	available to the reader: the "trains the reader to ignore the whole
//	section" failure #6346 direction 2 names.
//
//	Distinguishing a terminal-by-design kind from a dead resolver needs
//	evidence the graph does not have — both are zero participation — and a
//	hand-maintained name list is what #6346 ruled out. The evidence that
//	works is longitudinal, and #6377 established it was already on disk:
//	`grafel feedback` writes every report to
//	~/.grafel/feedback/<group>-<timestamp>.md, so "did this kind LOSE edges
//	it used to have?" is answerable by reading history (history.go). Check 2b
//	below now defers to that answer whenever history has one, and falls back
//	to the unconditional gate only for kinds with no history at all.
//
//	What that does NOT fix: a kind broken on the group's FIRST report still
//	fires 2b, because there is nothing to compare against. Those 14 corpus
//	firings were all first reports and are unchanged; the cost they impose is
//	now paid once per kind rather than on every run forever. Keeping that
//	first-run gate is deliberate — it is the new-extractor case, which the
//	longitudinal check is structurally blind to.
//
// runSanityChecks evaluates the loaded metrics against the defined sanity checks
// and returns a slice of results plus a confidence score (passed/total as 0–100).
func runSanityChecks(r *Report) ([]SanityResult, int) {
	var results []SanityResult

	// 1. Entity count > 0 for each indexed language.
	for lang, count := range r.EntitiesByLanguage {
		passed := count > 0
		note := ""
		if !passed {
			note = fmt.Sprintf("language %q has 0 entities", lang)
		}
		results = append(results, SanityResult{
			Name:   fmt.Sprintf("entity-count-nonzero[%s]", lang),
			Passed: passed,
			Note:   note,
		})
	}

	// 2. Orphan rate at or below orphanRateFailThreshold for every kind.
	//
	// N >= 10 is not an extra rule here, it is the shape of the data: Generate
	// only writes kinds with Total >= 10 into OrphanByKind, because rarer
	// kinds are suppressed from the report for anonymity. The guard below is
	// defensive so a caller-constructed Report cannot smuggle a 1-entity kind
	// into the gate. (#6346 asked whether N >= 10 hides small kinds — it does,
	// and the fix for that would be in the report's suppression policy, not
	// here.)
	//
	// "Orphan" is direction-aware since c6e6e148c (#6346/#6375, the fix for
	// the #6313 report): no semantic edge in EITHER
	// direction. A kind with zero observed semantic participation anywhere in
	// the group contributes NO orphans to OrphanByKind — every one of them is
	// counted in OrphanTerminalByKind instead, so the kind still has a row
	// here, reading 0 orphans, and check 2b below is what gates it. (That
	// double listing is why history.go reads participation from table
	// MEMBERSHIP rather than from the 0.0% figure in this table.) The two
	// checks partition the range between them: 2b covers exactly 100%
	// unwired, 2 covers everything above orphanRateFailThreshold and below it.
	// Neither end may be left unwatched (see 2b).
	for kind, ks := range r.OrphanByKind {
		if ks.Total < 10 {
			continue
		}
		// Strictly below, so the predicate matches the check's own published
		// name. It is also the only way the smallest bucket can fire at all:
		// kindOrphans accrues only for kinds with >= 1 participating entity,
		// so a kind of size N cannot exceed 100*(N-1)/N, which at N == 10 —
		// the smallest size Generate publishes — is exactly 90.0. With `<=`
		// every 10-entity kind was unfirable no matter how broken.
		passed := ks.OrphanPct < orphanRateFailThreshold
		note := ""
		if !passed {
			note = fmt.Sprintf(
				"kind %q: %d of %d entities (%.1f%%) have no semantic edge in either direction — at or above the %.0f%% threshold, this kind is functionally unwired",
				kind, ks.OrphanCount, ks.Total, ks.OrphanPct, orphanRateFailThreshold)
		}
		results = append(results, SanityResult{
			Name:   orphanCheckName(kind),
			Passed: passed,
			Note:   note,
		})
	}

	// 2b. Every reported kind must carry at least one semantic edge somewhere
	// in the group.
	//
	// Without this, deriving terminality from observed participation would
	// hand back the one case the old `OrphanPct < 100.0` rule DID catch:
	// report.go routes a zero-participation kind entirely into
	// OrphanTerminalByKind, so it never appears in the loop above and a kind
	// that lost EVERY semantic edge would score 0% defect orphan and 100%
	// confidence. Sensitivity would then be non-monotonic — blind at total
	// breakage, loud just below it — and a group whose entities carry only
	// CONTAINS (a language extractor shipped before its resolver lands, which
	// is a live shape on this milestone) would be reported as perfectly
	// healthy. Telling a maintainer exactly that is the report's job.
	//
	// This deliberately fires on kinds that are genuinely terminal by design
	// too, because nothing in the graph distinguishes the two — the note says
	// so rather than asserting a defect.
	//
	// Measured cost, per report (i.e. per group, which is how this actually
	// runs), across 12 corpus repos: 14 firings over 10 distinct kinds —
	// Config, Fixture, Route, SCOPE.CodeBlock, SCOPE.Config, SCOPE.Constraint,
	// SCOPE.Stylesheet, SCOPE.UIComponent, Service, Test. Sampling the sources
	// behind them, roughly 8 of the 14 are unarguably terminal by design
	// (markdown code fences, CSS selectors, HTML input fields, dependency
	// manifests) and will fail every run forever; 2 are unambiguous true
	// positives (`Route` in two Flask groups — `@app.route` paths never bound
	// to the function beneath them); the remaining ~4 are arguable. A ~14%
	// true-positive rate is a real cost and is NOT the whole of #6346
	// direction 2 solved — see the residual note on runSanityChecks.
	//
	// The asymmetry is what justifies failing rather than warning anyway: a
	// false failure costs one confidence point and one line of text a human
	// dismisses in seconds, while a false pass ships a broken extractor with a
	// clean bill of health.
	//
	// Do not restate this set from an aggregate measurement. An earlier
	// revision claimed "4 kinds corpus-wide" because it read a table that had
	// merged all 12 repos into ONE synthetic group; merging lets a kind borrow
	// participation from another repo, which hid Route, Test, Config,
	// SCOPE.Config, SCOPE.UIComponent and Service. The number that matters is
	// per-group, because that is the unit a user runs the report on.
	//
	// The size floor is not an independent policy knob — it is the report's
	// own visibility floor. Generate only publishes kinds with Total >= 10, so
	// gating below that would fail on data the reader cannot see.
	for kind, tks := range r.OrphanTerminalByKind {
		if tks.Total < 10 {
			continue
		}

		// #6377: prefer the longitudinal answer whenever history has one.
		//
		// `everParticipated, observed` is a three-state read of this group's
		// stored reports (history.go):
		//
		//   observed && everParticipated — the kind HAD semantic edges in a
		//     comparable report and has none now. That is the shape of a
		//     resolver regression, and it is the case the unconditional check
		//     below could only ever GUESS at. Fail, and cite the evidence.
		//     The note stops short of ASSERTING a regression: the stop can
		//     also be legitimate and permanent (repo removed from the group,
		//     extractor retired, language dropped), in which case no code
		//     change clears it and the note names the only remedy — deleting
		//     the group's stored reports. History older than #6375 is not
		//     comparable at all and never reaches here; history.go drops it.
		//
		//   observed && !everParticipated — zero participation in every report
		//     this group has ever produced. CSS selectors, markdown code
		//     fences, HTML input fields and dependency manifests live here.
		//     Nothing changed, no action is available to the reader, and
		//     firing again is what trains them to skip the section. Stay
		//     silent — this is the ~86% of firings #6377 measured as false.
		//
		//   !observed — no history for this kind: a first report, or a kind a
		//     new extractor only just started emitting. The longitudinal check
		//     has nothing to say, so the original 100%-end gate below still
		//     runs. That is deliberate and must not be removed: the
		//     new-extractor case is exactly what check 2b was added to catch,
		//     and deleting it here would trade one blind spot for another. The
		//     two checks are complementary, not substitutes.
		if everParticipated, observed := r.priorParticipation[kind]; observed {
			if everParticipated {
				results = append(results, SanityResult{
					Name:   participationRegressionCheckName(kind),
					Passed: false,
					Note: fmt.Sprintf(
						"kind %q: none of its %d entities carries a semantic edge in either direction, but a prior report for this group recorded this kind participating — most often a resolver regression rather than a terminal-by-design kind, since the terminal case does not usually acquire and then lose edges. If the stop is legitimate and permanent (the repo left the group, the extractor was retired, the language was dropped), nothing in the code can clear this: delete this group's stored reports (~/.grafel/feedback/%s-*.md) to reset it to first-run behaviour",
						kind, tks.Total, groupNameOrGlob(r.GroupName)),
				})
			}
			continue
		}

		// Unconditional failure, deliberately: membership in
		// OrphanTerminalByKind IS the finding. report.go populates that map
		// only for kinds where no entity participates, so there is no state in
		// which this check could pass, and writing a comparison here would
		// invite a reader to believe otherwise.
		//
		// The comparison it replaces (`tks.OrphanCount < tks.Total`) was worse
		// than merely unreachable: Total counts entity OCCURRENCES across docs
		// while OrphanCount counts UNIQUE ids, so a duplicate entity ID across
		// two docs — the #6368 shape, which landed two commits before this
		// branch — made it TRUE and silently converted the failure into a pass.
		// TestRunSanityChecks_ParticipationCheckCannotFalsePass reproduces
		// exactly that (OrphanCount 10, Total 20) and pins the behaviour.
		results = append(results, SanityResult{
			Name:   participationCheckName(kind),
			Passed: false,
			Note: fmt.Sprintf(
				"kind %q: none of its %d entities carries a semantic edge in either direction anywhere in the group, and no prior report for this group has ever recorded this kind — either it is terminal by design or its resolver never ran; with no history the graph alone cannot tell these apart, so triage it once (a later report answers it automatically)",
				kind, tks.Total),
		})
	}

	// 3. Resolution vector sums to 100% ± 0.1%.
	if r.ResolutionTotal > 0 {
		sum := r.Resolution.ResolvedPct +
			r.Resolution.ExternalKnownPct +
			r.Resolution.ExternalUnknownPct +
			r.Resolution.BugExtractorPct +
			r.Resolution.BugResolverPct +
			r.Resolution.DynamicPct
		passed := math.Abs(sum-100.0) <= 0.1
		note := ""
		if !passed {
			note = fmt.Sprintf("resolution vector sums to %.3f%% (expected 100.0%% ± 0.1%%)", sum)
		}
		results = append(results, SanityResult{
			Name:   "resolution-vector-sums-to-100pct",
			Passed: passed,
			Note:   note,
		})
	}

	// 4. Framework hits >= 1 if known-framework files were detected.
	if r.FrameworkFilesDetected > 0 {
		total := 0
		for _, count := range r.FrameworkHits {
			total += count
		}
		passed := total >= 1
		note := ""
		if !passed {
			note = fmt.Sprintf("%d known-framework files detected but framework_detector_hits = 0", r.FrameworkFilesDetected)
		}
		results = append(results, SanityResult{
			Name:   "framework-hits-if-detected",
			Passed: passed,
			Note:   note,
		})
	}

	// 5. Total entities >= 50.
	passed := r.TotalEntities >= minEntitiesForReport
	note := ""
	if !passed {
		note = fmt.Sprintf("total entities = %d (minimum %d required for reliable report)", r.TotalEntities, minEntitiesForReport)
	}
	results = append(results, SanityResult{
		Name:   "minimum-entity-count",
		Passed: passed,
		Note:   note,
	})

	// Compute confidence.
	passing := 0
	for _, res := range results {
		if res.Passed {
			passing++
		}
	}
	confidence := 0
	if len(results) > 0 {
		confidence = int(math.Round(100.0 * float64(passing) / float64(len(results))))
	}
	return results, confidence
}

// groupNameOrGlob renders the group component of the ~/.grafel/feedback path
// in the participation-regression note. Report.GroupName is empty for reports
// generated without one (tests, ad-hoc calls), and "-*.md" alone would read as
// a path that matches every group.
func groupNameOrGlob(group string) string {
	if group == "" {
		return "<group>"
	}
	return group
}
