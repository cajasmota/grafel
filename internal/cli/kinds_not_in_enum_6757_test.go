package cli

import (
	"bytes"
	"strings"
	"testing"
)

// #6757 arm C — the review's question: the sidecar's stated rationale is "the
// stderr line is invisible to MCP, the dashboard and `doctor`, which read the
// graph", and the cited precedent (RenameDetectTruncated) IS rendered here.
// A field nothing reads does not deliver that rationale, so `doctor` consumes
// it too.
//
// The line is INFORMATIONAL and never changes a status: every one of these
// edges is in the graph — arm C counts, it never drops.

func TestKindsNotInEnumLineNamesTheKindsAndKeepsTheTotalsExact(t *testing.T) {
	line := KindsNotInEnumLine(27645, 6, map[string]int{
		"COMMIT_COUPLED": 27407, "STEP_IN_PROCESS": 175, "ENTRY_POINT_OF": 55,
		"STARTS_WORKFLOW": 4, "EXECUTES_ACTIVITY": 3, "DECORATES": 1,
	})
	if line == "" {
		t.Fatal("27,645 edges across 6 kinds rendered nothing")
	}
	// The totals, exactly — not the count of names printed.
	for _, want := range []string{"27,645", "6 relationship kind(s)"} {
		if !strings.Contains(line, want) {
			t.Errorf("line = %q, missing %q", line, want)
		}
	}
	// Busiest first, and named: a bare count says something is wrong, the
	// name says what.
	if !strings.HasPrefix(line[strings.Index(line, ": ")+2:], "COMMIT_COUPLED (27,407)") {
		t.Errorf("line = %q, want the busiest kind named first with its count", line)
	}
	// Only DoctorKindsNotInEnumNames names are spelled out; the rest are
	// summarised, never dropped silently.
	if !strings.Contains(line, "+1 more") {
		t.Errorf("line = %q, want the 6th kind summarised as +1 more (cap is %d)",
			line, DoctorKindsNotInEnumNames)
	}
	if strings.Contains(line, "DECORATES") {
		t.Errorf("line = %q names a kind past the display cap", line)
	}
}

func TestKindsNotInEnumLineIsSilentWhenThereIsNothingToReport(t *testing.T) {
	if got := KindsNotInEnumLine(0, 0, nil); got != "" {
		t.Errorf("KindsNotInEnumLine(0,0,nil) = %q, want \"\" — a repo whose every kind is in the "+
			"enum must leave doctor's output byte-identical to what it was before this line existed", got)
	}
}

// TestKindsNotInEnumLineReportsUncappedTotalsWithATruncatedMap pins the same
// asymmetry the writer and the sidecar hold: the map handed to doctor is
// already truncated at the writer's cap, so the line must take its totals from
// the arguments, never from len(kinds).
func TestKindsNotInEnumLineReportsUncappedTotalsWithATruncatedMap(t *testing.T) {
	kinds := map[string]int{"A": 5, "B": 4} // a truncated view
	line := KindsNotInEnumLine(900, 40, kinds)
	if !strings.Contains(line, "900") || !strings.Contains(line, "40 relationship kind(s)") {
		t.Errorf("line = %q, want the UNCAPPED totals 900 / 40 even though only %d names are known",
			line, len(kinds))
	}
	if !strings.Contains(line, "+38 more") {
		t.Errorf("line = %q, want the 38 unnamed kinds accounted for", line)
	}
}

// TestPrintDoctorHealthRendersTheKindsNotInEnumLine is the wiring: the value
// must reach the rendered report, not just the formatter.
func TestPrintDoctorHealthRendersTheKindsNotInEnumLine(t *testing.T) {
	g := &DoctorGroupHealth{
		GroupName: "grafel",
		Repos: []*DoctorRepoHealth{{
			Slug:                   "grafel",
			Status:                 "OK",
			Entities:               10,
			Relationships:          20,
			EdgesKindNotInEnum:     27645,
			DistinctKindsNotInEnum: 6,
			KindsNotInEnum:         map[string]int{"COMMIT_COUPLED": 27407},
		}},
	}
	var buf bytes.Buffer
	PrintDoctorHealth(&buf, []*DoctorGroupHealth{g})
	out := buf.String()
	if !strings.Contains(out, "COMMIT_COUPLED") || !strings.Contains(out, "27,645") {
		t.Fatalf("doctor did not render the non-enum kind report — the sidecar fields are read by "+
			"nothing, which is the reach the prose claims. Output:\n%s", out)
	}

	// A repo with nothing to report must not gain a line.
	clean := &DoctorGroupHealth{
		GroupName: "grafel",
		Repos:     []*DoctorRepoHealth{{Slug: "grafel", Status: "OK", Entities: 10, Relationships: 20}},
	}
	var cleanBuf bytes.Buffer
	PrintDoctorHealth(&cleanBuf, []*DoctorGroupHealth{clean})
	if strings.Contains(cleanBuf.String(), "absent from the enum") {
		t.Errorf("a clean repo gained a non-enum line:\n%s", cleanBuf.String())
	}
}
