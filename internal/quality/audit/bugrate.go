package audit

// bugrate.go — the unresolved-import ("bug rate") metric, as a pair of counts
// rather than a bare percentage (#7271).
//
// Every surface that reports a bug rate — `grafel doctor`, the dashboard
// quality/composite endpoints, the rebuild webhook payload and the health
// history — derives it from the same two numbers: how many IMPORTS edges were
// seen, and how many of those resolved to something addressable. Carrying the
// counts instead of the percentage is what makes the metric honest:
//
//   - A float64 alone cannot distinguish "every import resolved" from "no
//     import edge was ever scanned". Both are 0.0, and 0.0 renders as a
//     perfect score. That collapse IS the defect of #7271: doctor printed
//     "0.0% ✓" unconditionally, and a naive wiring (guard the division on a
//     nonzero denominator, leave the zero value) reproduces it exactly.
//   - A companion "known" bool can desynchronise from the value it qualifies.
//     The denominator cannot: it is the evidence, and Known() is a statement
//     about the evidence rather than a second record of it.
//
// Percentages are therefore never passed between packages; BugRate is.

// BugRateHealthyMaxPct is the largest unresolved-import percentage still
// considered healthy. It mirrors the dashboard's fidelity band (fidelity =
// 100 - bug_rate, healthy at fidelity >= 0.97), so the terminal and the
// dashboard cannot disagree about whether the same figure is acceptable.
const BugRateHealthyMaxPct = 3.0

// BugRate is the unresolved-import tally for a repo or a group.
//
// The zero value is the "nothing measured" state, NOT a perfect score — see
// Known.
type BugRate struct {
	// TotalImports is the number of IMPORTS edges seen. It is the
	// denominator, and zero means the rate is undefined rather than zero.
	TotalImports int
	// ResolvedImports is how many of those pointed at an addressable target
	// (an entity id, or a fully qualified external placeholder).
	ResolvedImports int
}

// AddEdge folds one relationship into the tally. Edges that are not IMPORTS
// are ignored, so a caller can hand it every edge of a graph in the pass it is
// already making. kind is matched case-insensitively, exactly as the audit
// pass matches it.
func (b *BugRate) AddEdge(kind, toID string) {
	if !isImportsKind(kind) {
		return
	}
	b.TotalImports++
	switch ClassifyImportToID(toID) {
	case ImportFormatHex, ImportFormatExtQualified:
		b.ResolvedImports++
	}
}

// Add merges another tally into this one — group aggregation over repos.
func (b *BugRate) Add(other BugRate) {
	b.TotalImports += other.TotalImports
	b.ResolvedImports += other.ResolvedImports
}

// Known reports whether anything was measured, i.e. whether Pct means
// anything. Callers MUST check it before rendering, persisting or comparing a
// percentage: an unknown rate is not a zero rate.
func (b BugRate) Known() bool { return b.TotalImports > 0 }

// Pct is the percentage of IMPORTS edges that did not resolve, in [0, 100].
// It returns 0 when !Known(), which is why Known() is not optional.
func (b BugRate) Pct() float64 {
	if !b.Known() {
		return 0
	}
	return 100.0 * float64(b.TotalImports-b.ResolvedImports) / float64(b.TotalImports)
}

// PctPtr returns the percentage, or nil when nothing was measured. It is the
// form wire structs and persisted records want: a null bug_rate says "not
// measured" to a consumer that cannot read this source to find out.
func (b BugRate) PctPtr() *float64 {
	if !b.Known() {
		return nil
	}
	p := b.Pct()
	return &p
}

// BugRateFromReport builds the tally from a finished RepoReport, whose
// per-format histogram was populated by the same ClassifyImportToID that
// AddEdge consults. This is the entry point for callers that audited a repo
// rather than walking its edges themselves.
func BugRateFromReport(rr *RepoReport) BugRate {
	if rr == nil {
		return BugRate{}
	}
	return BugRate{
		TotalImports: rr.ImportsTotal,
		ResolvedImports: rr.ImportsToIDFormat[ImportFormatHex] +
			rr.ImportsToIDFormat[ImportFormatExtQualified],
	}
}

// isImportsKind is the single place the IMPORTS edge kind is recognised for
// this metric.
func isImportsKind(kind string) bool {
	const want = "IMPORTS"
	if len(kind) != len(want) {
		return false
	}
	for i := 0; i < len(kind); i++ {
		c := kind[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		if c != want[i] {
			return false
		}
	}
	return true
}
