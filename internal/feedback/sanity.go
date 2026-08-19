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
//     not gating. A kind where more than 9 in 10 entities carry no semantic
//     edge in either direction is not partially degraded; it is functionally
//     unwired.
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

// SanityResult is the outcome of a single sanity check.
type SanityResult struct {
	Name   string
	Passed bool
	Note   string
}

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
	// "Orphan" is direction-aware since #6313: no semantic edge in EITHER
	// direction. Kinds with zero observed semantic participation anywhere in
	// the group are not in OrphanByKind at all — they are in
	// OrphanTerminalByKind, and check 2b below is what gates THOSE. The two
	// checks partition the range between them: 2b covers exactly 100%
	// unwired, 2 covers everything above orphanRateFailThreshold and below it.
	// Neither end may be left unwatched (see 2b).
	for kind, ks := range r.OrphanByKind {
		if ks.Total < 10 {
			continue
		}
		passed := ks.OrphanPct <= orphanRateFailThreshold
		note := ""
		if !passed {
			note = fmt.Sprintf(
				"kind %q: %d of %d entities (%.1f%%) have no semantic edge in either direction — above the %.0f%% threshold, this kind is functionally unwired",
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
	// so rather than asserting a defect. The asymmetry justifies it: a false
	// failure costs one confidence point and one line of text that a human
	// dismisses in seconds, while a false pass ships a broken extractor with a
	// clean bill of health. Measured cost on 12 corpus repos: 14 firings, of
	// which at least one (a group with 172 `Route` entities and not one
	// semantic edge among them) is unambiguously the defect this exists to
	// catch.
	//
	// The size floor is not an independent policy knob — it is the report's
	// own visibility floor. Generate only publishes kinds with Total >= 10, so
	// gating below that would fail on data the reader cannot see.
	for kind, tks := range r.OrphanTerminalByKind {
		if tks.Total < 10 {
			continue
		}
		passed := tks.OrphanCount < tks.Total
		note := ""
		if !passed {
			note = fmt.Sprintf(
				"kind %q: none of its %d entities carries a semantic edge in either direction anywhere in the group — either the kind is terminal by design or its resolver never ran; the graph alone cannot tell these apart, so triage it (if this kind used to participate, it is a regression)",
				kind, tks.Total)
		}
		results = append(results, SanityResult{
			Name:   participationCheckName(kind),
			Passed: passed,
			Note:   note,
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
