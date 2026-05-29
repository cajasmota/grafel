package mcp

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func newFeedbackTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	regPath := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(regPath, []byte(`{"groups":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestFeedbackEventValidation(t *testing.T) {
	srv := newFeedbackTestServer(t)
	cases := []struct {
		name string
		args map[string]any
	}{
		{"missing outcome", map[string]any{"group": "g"}},
		{"invalid outcome", map[string]any{"outcome": "nope"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := callTool(t, srv, "archigraph_feedback_event", tc.args)
			if res == nil || !res.IsError {
				t.Fatalf("expected error result for %q", tc.name)
			}
			if !containsStr(resultText(res), "outcome") {
				t.Errorf("expected error to mention outcome; got %q", resultText(res))
			}
		})
	}
}

// TestFeedbackEventMilestoneAccepted verifies the neutral "milestone" outcome
// (#3206) is accepted by the tool.
func TestFeedbackEventMilestoneAccepted(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, ".config"))

	srv := newFeedbackTestServer(t)
	res := callTool(t, srv, "archigraph_feedback_event", map[string]any{
		"outcome": "milestone",
		"phase":   "port:inspections",
		"note":    "inspections module reaches parity",
	})
	if res == nil || res.IsError {
		t.Fatalf("milestone outcome should be accepted; got error: %v", resultText(res))
	}
	if !feedbackOutcomes["milestone"] {
		t.Error("milestone must be in feedbackOutcomes")
	}
}

func TestFeedbackEventJSONLAppend(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, ".config"))

	srv := newFeedbackTestServer(t)
	res := callTool(t, srv, "archigraph_feedback_event", map[string]any{
		"outcome":    "missing_capability",
		"group":      "upvate-core",
		"phase":      "port:inspections",
		"library":    "pymongo",
		"capability": "data_access",
		"note":       "no mongo collection nodes",
	})
	if res == nil || res.IsError {
		t.Fatalf("unexpected error: %v", resultText(res))
	}

	path, err := feedbackEventsFile()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		t.Fatal("expected at least one line in feedback JSONL")
	}
	var evt FeedbackEvent
	if err := json.Unmarshal(sc.Bytes(), &evt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if evt.Outcome != "missing_capability" || evt.Group != "upvate-core" ||
		evt.Library != "pymongo" || evt.Capability != "data_access" {
		t.Errorf("event roundtrip mismatch: %+v", evt)
	}
	if evt.Timestamp == "" {
		t.Error("expected timestamp to be set")
	}
}
