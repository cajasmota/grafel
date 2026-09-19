package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/quality"
	"github.com/cajasmota/grafel/internal/quality/audit"
)

// quality_corpus_7283_test.go — `grafel quality bug-rate-corpus` appends to the
// same health-history.jsonl the daemon writes, and had the same collapse: a
// group whose audit failed was persisted with bug_rate 0 and a composite score
// of 0 as though both had been measured (#7283).

// appendAndReadRaw persists one entry and returns the JSONL line it produced,
// decoded only far enough to ask which keys are present.
func appendAndReadRaw(t *testing.T, entry quality.HealthEntry) (map[string]json.RawMessage, string) {
	t.Helper()
	root := t.TempDir()
	if err := quality.AppendEntry(root, entry); err != nil {
		t.Fatalf("AppendEntry: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, "health-history.jsonl"))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	line := strings.TrimSpace(string(b))
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		t.Fatalf("decode %q: %v", line, err)
	}
	return raw, line
}

// TestCorpusHistoryEntry_FailedGroupRecordsNoMeasurement is the forbidden row:
// the result measureCorpus builds when measureGroup errors carries no counts,
// so the persisted row must carry no rate and no score.
func TestCorpusHistoryEntry_FailedGroupRecordsNoMeasurement(t *testing.T) {
	failed := corpusGroupResult{
		Name:       "acme",
		Path:       "/nowhere/acme",
		MeasuredAt: time.Now().UTC(),
		Errors:     []string{"audit /nowhere/acme: boom"},
	}

	raw, line := appendAndReadRaw(t, corpusHistoryEntry(failed))

	if v, ok := raw["bug_rate"]; ok {
		t.Errorf("the group's audit failed, yet bug_rate=%s was persisted as a measurement\nline: %s", v, line)
	}
	if v, ok := raw["health_score"]; ok {
		t.Errorf("the group's audit failed, yet health_score=%s was persisted\nline: %s", v, line)
	}
	if _, ok := raw["group"]; !ok {
		t.Errorf("group missing — the record was not written at all\nline: %s", line)
	}
}

// TestCorpusHistoryEntry_MeasuredGroupRecordsBoth is the other half. Without
// it the assertions above would pass on a change that simply stopped writing
// bug_rate and health_score for every group.
//
// The counts differ from the rounded BugRatePct the report carries, so the
// persisted rate is provably taken from the tally rather than from the
// display field.
func TestCorpusHistoryEntry_MeasuredGroupRecordsBoth(t *testing.T) {
	measured := corpusGroupResult{
		Name:          "acme",
		MeasuredAt:    time.Now().UTC(),
		Entities:      200,
		OrphanRatePct: 4.0,
		BugRatePct:    99.9, // deliberately inconsistent: never the source
		bugRate:       audit.BugRate{TotalImports: 50, ResolvedImports: 40},
		Composite:     quality.CompositeResult{Score: 81.5},
	}

	raw, line := appendAndReadRaw(t, corpusHistoryEntry(measured))

	var bug float64
	if v, ok := raw["bug_rate"]; !ok {
		t.Fatalf("50 IMPORTS edges were counted, yet no bug_rate was persisted\nline: %s", line)
	} else if err := json.Unmarshal(v, &bug); err != nil || bug != 20.0 {
		t.Errorf("bug_rate = %s, want 20 (10 unresolved of 50)\nline: %s", v, line)
	}
	var score float64
	if v, ok := raw["health_score"]; !ok {
		t.Fatalf("bug rate measured, yet no health_score was persisted\nline: %s", line)
	} else if err := json.Unmarshal(v, &score); err != nil || score != 81.5 {
		t.Errorf("health_score = %s, want 81.5\nline: %s", v, line)
	}
}
