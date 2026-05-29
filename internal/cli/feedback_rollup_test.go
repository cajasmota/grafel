package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func writeEventsFile(t *testing.T, dir, name string, lines []string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFeedbackRollup_Aggregates(t *testing.T) {
	tmp := t.TempDir()
	eventsDir := filepath.Join(tmp, "events")
	outDir := filepath.Join(tmp, "out")

	writeEventsFile(t, eventsDir, "feedback-events-2026-05-30.jsonl", []string{
		`{"ts":"2026-05-30T10:00:00Z","group":"upvate-core","library":"pymongo","capability":"data_access","outcome":"missing_capability"}`,
		`{"ts":"2026-05-30T10:05:00Z","group":"upvate-core","library":"drf","capability":"routing","outcome":"helped"}`,
		`{"ts":"2026-05-30T10:06:00Z","group":"new-backend","library":"typeorm","capability":"data_access","outcome":"partial"}`,
		`{"ts":"2026-05-30T10:07:00Z","group":"new-backend","library":"class-validator","capability":"dto","outcome":"wrong"}`,
		`not-json-skip-me`,
	})
	// An older file that should be excluded by --since.
	writeEventsFile(t, eventsDir, "feedback-events-2026-05-01.jsonl", []string{
		`{"ts":"2026-05-01T10:00:00Z","group":"upvate-core","capability":"auth","outcome":"wrong"}`,
	})

	cmd := &cobra.Command{}
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	if err := runFeedbackRollup(cmd, "2026-05-15", outDir, eventsDir); err != nil {
		t.Fatalf("rollup: %v", err)
	}

	jb, err := os.ReadFile(filepath.Join(outDir, "rollup.json"))
	if err != nil {
		t.Fatalf("read rollup.json: %v", err)
	}
	var r feedbackRollup
	if err := json.Unmarshal(jb, &r); err != nil {
		t.Fatal(err)
	}

	// 4 valid events on/after 2026-05-15 (the older auth/wrong excluded, junk skipped).
	if r.TotalEvents != 4 {
		t.Fatalf("TotalEvents = %d, want 4", r.TotalEvents)
	}
	if r.ByOutcome["wrong"] != 1 || r.ByOutcome["missing_capability"] != 1 || r.ByOutcome["helped"] != 1 {
		t.Errorf("by_outcome = %v", r.ByOutcome)
	}
	if r.ByGroup["upvate-core"]["missing_capability"] != 1 {
		t.Errorf("group upvate-core missing = %v", r.ByGroup["upvate-core"])
	}
	// Fix queue: data_access (missing) + dto (wrong) + class-validator/typeorm? + pymongo.
	if len(r.FixQueue) == 0 {
		t.Fatal("expected non-empty fix queue")
	}
	// The auth/wrong from the excluded older file must NOT appear.
	for _, it := range r.FixQueue {
		if it.Key == "auth" {
			t.Error("excluded older event leaked into fix queue")
		}
	}

	if _, err := os.Stat(filepath.Join(outDir, "rollup.md")); err != nil {
		t.Errorf("rollup.md not written: %v", err)
	}
}

// TestFeedbackRollup_MilestoneNotInFixQueue verifies that the neutral
// "milestone" outcome (#3206) is counted as an outcome but never inflates the
// fix queue (which only counts wrong+missing_capability).
func TestFeedbackRollup_MilestoneNotInFixQueue(t *testing.T) {
	tmp := t.TempDir()
	eventsDir := filepath.Join(tmp, "events")
	outDir := filepath.Join(tmp, "out")

	writeEventsFile(t, eventsDir, "feedback-events-2026-05-30.jsonl", []string{
		`{"ts":"2026-05-30T10:00:00Z","group":"new-backend","capability":"data_access","outcome":"milestone","note":"inspections parity"}`,
		`{"ts":"2026-05-30T10:05:00Z","group":"new-backend","capability":"routing","outcome":"helped"}`,
	})

	cmd := &cobra.Command{}
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	if err := runFeedbackRollup(cmd, "", outDir, eventsDir); err != nil {
		t.Fatalf("rollup: %v", err)
	}

	jb, err := os.ReadFile(filepath.Join(outDir, "rollup.json"))
	if err != nil {
		t.Fatalf("read rollup.json: %v", err)
	}
	var r feedbackRollup
	if err := json.Unmarshal(jb, &r); err != nil {
		t.Fatal(err)
	}
	if r.ByOutcome["milestone"] != 1 {
		t.Errorf("milestone should be counted as an outcome; by_outcome=%v", r.ByOutcome)
	}
	if len(r.FixQueue) != 0 {
		t.Errorf("milestone (and helped) must not produce fix-queue items; got %v", r.FixQueue)
	}
}

func TestFeedbackRollup_NoEvents(t *testing.T) {
	tmp := t.TempDir()
	cmd := &cobra.Command{}
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	if err := runFeedbackRollup(cmd, "", filepath.Join(tmp, "out"), filepath.Join(tmp, "events")); err != nil {
		t.Fatalf("rollup with no events should not error: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("no feedback events")) {
		t.Errorf("expected 'no feedback events' message, got %q", out.String())
	}
}
