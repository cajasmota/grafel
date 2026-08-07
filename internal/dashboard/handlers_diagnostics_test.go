// handlers_diagnostics_test.go — unit coverage for convertGroupHealth's
// field mapping (#5949).
//
// convertGroupHealth copies cli.DoctorGroupHealth.RepairCandidates /
// EnrichmentCandidates into GroupDiagnostics.PendingRepairs /
// PendingEnrichments (the wire field names — and their JSON tags — stay
// "pending_repairs"/"pending_enrichments" on purpose; only the *source*
// struct's Go field names were renamed). Nothing previously asserted that
// the two assignments land in the matching field rather than each other's:
// a transposition compiles cleanly and still produces two plausible
// integers, so it would have shipped silently via GET /api/diagnostics.
package dashboard

import (
	"testing"

	"github.com/cajasmota/grafel/internal/cli"
)

// TestConvertGroupHealth_RepairAndEnrichmentCountsNotSwapped uses distinct,
// non-zero values for RepairCandidates and EnrichmentCandidates so that a
// mapping swap between the two assignments in convertGroupHealth is
// detectable — equal values would make this assertion vacuous against
// exactly the mutation this test exists to catch.
func TestConvertGroupHealth_RepairAndEnrichmentCountsNotSwapped(t *testing.T) {
	gh := &cli.DoctorGroupHealth{
		GroupName:            "test-group",
		RepairCandidates:     12,
		EnrichmentCandidates: 89,
	}

	gd := convertGroupHealth(gh)

	if gd.PendingRepairs != 12 {
		t.Errorf("GroupDiagnostics.PendingRepairs = %d, want 12 (from DoctorGroupHealth.RepairCandidates)", gd.PendingRepairs)
	}
	if gd.PendingEnrichments != 89 {
		t.Errorf("GroupDiagnostics.PendingEnrichments = %d, want 89 (from DoctorGroupHealth.EnrichmentCandidates)", gd.PendingEnrichments)
	}
}
