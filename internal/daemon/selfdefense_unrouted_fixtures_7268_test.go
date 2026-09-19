package daemon

import (
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

// TestNoUnroutedFixtures_7268 is the structural guard for this package's
// windows-sensitive fixture tables.
//
// WHY A GUARD AND NOT ANOTHER ROUND OF REVIEW. Four tables in this repo fed
// bare unix-absolute literals to daemon.IsCanonicalBinaryPath, whose first act
// is filepath.IsAbs — false on windows for "/usr/local/bin/grafel", so required
// rows invert and forbidden rows pass vacuously. Three independent adversarial
// reviews and seven rounds enumerated those tables BY HAND, fixed three, and
// missed the fourth; the windows CI leg found it in twenty minutes. The lesson
// recorded on #7268 is that a hand-derived list of sites is unreliable here, so
// the list is derived from code shape and checked on every run.
//
// See testsupport.ScanUnroutedFixtures for what it checks, why the unit is a
// FUNCTION rather than a file, why /tmp-prefixed fixtures are exempt, and what
// it is blind to.
func TestNoUnroutedFixtures_7268(t *testing.T) {
	found, err := testsupport.ScanUnroutedFixtures(".")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, f := range found {
		t.Errorf("%s\n\tThese reach the identity gate unrouted, so on windows they are not "+
			"absolute: the gate rejects them, required rows invert and forbidden rows pass "+
			"while grading nothing. Route them through testsupport.AbsFixture, and mark any "+
			"/tmp-prefix row tmpPrefix so it is SKIPPED on windows instead. If one of these "+
			"is prose in a positional struct field rather than a fixture, key the field "+
			"(name:/why:) so it is excluded.", f)
	}
}
