package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install"
)

// TestPrintReconcileSummary_AnnouncesTheRename covers the user-visible half of
// #6183. The label now carries a path digest, so the one-time repair RENAMES
// every already-installed unit. A user who greps ~/Library/LaunchAgents for the
// old filename after upgrading would otherwise find nothing and conclude their
// watchers had been deleted.
func TestPrintReconcileSummary_AnnouncesTheRename(t *testing.T) {
	var buf bytes.Buffer
	printReconcileSummary(&buf, &install.ReconcileWatcherResult{
		Current:   10,
		Rewritten: []string{"/a.plist", "/b.plist"},
		Reloaded:  []string{"/a.plist", "/b.plist"},
		Migrated:  []string{"/old-a.plist", "/old-b.plist"},
	})
	got := buf.String()
	if !strings.Contains(got, "renamed") {
		t.Fatalf("summary does not mention the rename:\n%s", got)
	}
	if !strings.Contains(got, "#6183") {
		t.Fatalf("summary does not point at the issue:\n%s", got)
	}
	if !strings.Contains(got, "2 watcher unit") {
		t.Fatalf("summary does not report how many were renamed:\n%s", got)
	}
}

// A migration with nothing else to report must still be announced: the units
// moved, which is exactly the surprising part.
func TestPrintReconcileSummary_MigrationOnlyIsNotSilent(t *testing.T) {
	var buf bytes.Buffer
	printReconcileSummary(&buf, &install.ReconcileWatcherResult{
		Current:  1,
		Migrated: []string{"/old.plist"},
	})
	if !strings.Contains(buf.String(), "renamed") {
		t.Fatalf("a migration-only reconcile printed nothing:\n%q", buf.String())
	}
}

// And the steady state stays silent — #6179's property.
func TestPrintReconcileSummary_SilentWhenNothingHappened(t *testing.T) {
	var buf bytes.Buffer
	printReconcileSummary(&buf, &install.ReconcileWatcherResult{Current: 140})
	if buf.Len() != 0 {
		t.Fatalf("expected silence, got:\n%s", buf.String())
	}
}

// TestPrintReconcileSummary_SurfacesAbsentRepos covers #6183 F2's minimum.
//
// A repo that is registered with watchers enabled but has no unit under either
// label is left alone by reconcile — creating it is Apply's job. That is a
// defensible policy only if somebody is told. It was counted into Absent and
// then never printed, so an interrupted migration, or any repo whose unit was
// removed by hand, was unwatched with no signal anywhere.
func TestPrintReconcileSummary_SurfacesAbsentRepos(t *testing.T) {
	var buf bytes.Buffer
	printReconcileSummary(&buf, &install.ReconcileWatcherResult{Current: 3, Absent: 2})
	got := buf.String()
	if !strings.Contains(got, "2 registered repo") {
		t.Fatalf("Absent repos were not surfaced:\n%q", got)
	}
	if !strings.Contains(got, "grafel install") {
		t.Fatalf("no remedy offered:\n%q", got)
	}
	// Nothing was rewritten, so the refresh headline must not claim otherwise.
	if strings.Contains(got, "watcher units refreshed") {
		t.Fatalf("claimed a refresh that did not happen:\n%q", got)
	}
}
