package mcp

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPersonaEventInputValidation verifies that archigraph_persona_event
// rejects malformed calls without panicking or writing to disk.
func TestPersonaEventInputValidation(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(regPath, []byte(`{"groups":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{
			name:    "missing persona",
			args:    map[string]any{"event_type": "invoke"},
			wantErr: "persona",
		},
		{
			name:    "empty persona",
			args:    map[string]any{"persona": "", "event_type": "invoke"},
			wantErr: "persona",
		},
		{
			name:    "missing event_type",
			args:    map[string]any{"persona": "architect"},
			wantErr: "event_type",
		},
		{
			name:    "invalid event_type",
			args:    map[string]any{"persona": "architect", "event_type": "unknown"},
			wantErr: "event_type",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := callTool(t, srv, "archigraph_persona_event", tc.args)
			if res == nil {
				t.Fatal("expected non-nil result")
			}
			if !res.IsError {
				t.Errorf("expected IsError=true for case %q, got false; content: %v", tc.name, res.Content)
			}
			text := resultText(res)
			if text == "" {
				t.Errorf("expected error text for case %q", tc.name)
			}
			// The error message should reference the bad field name.
			if tc.wantErr != "" {
				found := false
				for _, s := range []string{tc.wantErr} {
					if containsStr(text, s) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error text to contain %q; got %q", tc.wantErr, text)
				}
			}
		})
	}
}

// TestPersonaEventJSONLAppend verifies that a valid archigraph_persona_event
// call appends a well-formed JSON line to the daily JSONL file.
func TestPersonaEventJSONLAppend(t *testing.T) {
	// Override HOME so the JSONL file lands in a temp dir.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	// Also override on macOS (os.UserHomeDir can read $HOME or Getpw; set both).
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, ".config"))

	dir := t.TempDir()
	regPath := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(regPath, []byte(`{"groups":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}

	before := time.Now().UTC().Truncate(time.Second)

	// Emit an invoke event.
	res := callTool(t, srv, "archigraph_persona_event", map[string]any{
		"persona":    "architect",
		"event_type": "invoke",
	})
	if res == nil {
		t.Fatal("expected non-nil result")
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}

	// The result must contain recorded=true and a ts field.
	text := resultText(res)
	var got map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("response is not JSON: %v — text: %s", err, text)
	}
	if got["recorded"] != true {
		t.Errorf("expected recorded=true, got %v", got["recorded"])
	}
	if got["ts"] == "" || got["ts"] == nil {
		t.Errorf("expected non-empty ts, got %v", got["ts"])
	}

	// Emit a consult_out event with depth and chain.
	res2 := callTool(t, srv, "archigraph_persona_event", map[string]any{
		"persona":        "architect",
		"event_type":     "consult_out",
		"target_persona": "performance-reviewer",
		"depth":          2,
		"chain":          []string{"architect", "security-auditor"},
	})
	if res2 == nil || res2.IsError {
		t.Fatalf("consult_out event failed: %v", resultText(res2))
	}

	// Verify the JSONL file has exactly 2 lines and both are valid JSON.
	date := time.Now().UTC().Format("2006-01-02")
	evtPath := filepath.Join(tmpHome, ".archigraph", "events", "persona-events-"+date+".jsonl")
	f, err := os.Open(evtPath)
	if err != nil {
		t.Fatalf("expected JSONL file at %s: %v", evtPath, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d", len(lines))
	}

	var evt1 PersonaEvent
	if err := json.Unmarshal([]byte(lines[0]), &evt1); err != nil {
		t.Fatalf("line 1 is not valid JSON: %v — line: %s", err, lines[0])
	}
	if evt1.Persona != "architect" {
		t.Errorf("evt1.Persona: want architect, got %q", evt1.Persona)
	}
	if evt1.EventType != "invoke" {
		t.Errorf("evt1.EventType: want invoke, got %q", evt1.EventType)
	}
	ts1, err := time.Parse(time.RFC3339, evt1.Timestamp)
	if err != nil {
		t.Errorf("evt1.Timestamp is not RFC3339: %q", evt1.Timestamp)
	} else if ts1.Before(before) {
		t.Errorf("evt1.Timestamp %v is before test start %v", ts1, before)
	}

	var evt2 PersonaEvent
	if err := json.Unmarshal([]byte(lines[1]), &evt2); err != nil {
		t.Fatalf("line 2 is not valid JSON: %v — line: %s", err, lines[1])
	}
	if evt2.EventType != "consult_out" {
		t.Errorf("evt2.EventType: want consult_out, got %q", evt2.EventType)
	}
	if evt2.TargetPersona != "performance-reviewer" {
		t.Errorf("evt2.TargetPersona: want performance-reviewer, got %q", evt2.TargetPersona)
	}
	if evt2.Depth != 2 {
		t.Errorf("evt2.Depth: want 2, got %d", evt2.Depth)
	}
	if len(evt2.Chain) != 2 || evt2.Chain[0] != "architect" || evt2.Chain[1] != "security-auditor" {
		t.Errorf("evt2.Chain: want [architect security-auditor], got %v", evt2.Chain)
	}
}

// containsStr is a small helper to avoid importing strings in test scope.
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
