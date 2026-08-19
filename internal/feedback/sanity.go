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
//   - It leaves headroom for genuine per-instance gaps so they stay VISIBLE in
//     the table instead of being converted into a gate failure that swamps the
//     report. #6374 is the concrete case: 23% of http_endpoint_definition
//     entities across 12 corpus repos have no IMPLEMENTS edge and therefore no
//     semantic edge at all. That is a real defect, it is reported as a
//     non-zero orphan count, and it must not fail this gate — a gate that
//     fires on every kind teaches the reader to skip the section, which is
//     exactly how #6313 went unnoticed.
//   - Measured cost/benefit on 12 corpus repos (nextjs-commerce, openapi-
//     stripe, nestjs, nestjs-starter, express, express-realworld, django,
//     django-realworld, flask, flask-realworld, spring-petclinic,
//     laravel-routing): the old `== 100%` rule produced 47 failures, of which
//     the majority were sinks miscounted by the old outgoing-only metric.
//     Direction-awareness plus this threshold produces 15, none of them a
//     direction artifact. Lowering it to 75% would produce 22 and to 50% would
//     produce 32, with the added entries dominated by kinds in the 50-90% band
//     where "unwired kind" and "kind with real per-instance gaps" are no
//     longer distinguishable.
const orphanRateFailThreshold = 90.0

// orphanCheckName is the sanity-result name for the per-kind orphan check.
func orphanCheckName(kind string) string {
	return fmt.Sprintf("orphan-rate-below-%.0fpct[%s]", orphanRateFailThreshold, kind)
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
	// OrphanTerminalByKind and deliberately not gated.
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
