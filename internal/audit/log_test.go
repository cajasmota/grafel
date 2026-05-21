package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppend_writesToDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	l := New(path)

	l.AppendOK("rebuild", "fixture-a", map[string]any{"wipe": false})
	l.AppendErr("settings_update", "", nil, "invalid theme")

	// Give the background worker time to flush.
	l.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	lines := splitLines(data)
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %s", len(lines), data)
	}

	var e1 Entry
	if err := json.Unmarshal([]byte(lines[0]), &e1); err != nil {
		t.Fatalf("decode line 1: %v", err)
	}
	if e1.Operation != "rebuild" {
		t.Errorf("want operation=rebuild, got %q", e1.Operation)
	}
	if e1.Target != "fixture-a" {
		t.Errorf("want target=fixture-a, got %q", e1.Target)
	}
	if e1.Result != "ok" {
		t.Errorf("want result=ok, got %q", e1.Result)
	}
	if e1.Timestamp == "" {
		t.Error("want non-empty timestamp")
	}

	var e2 Entry
	if err := json.Unmarshal([]byte(lines[1]), &e2); err != nil {
		t.Fatalf("decode line 2: %v", err)
	}
	if e2.Result != "error" {
		t.Errorf("want result=error, got %q", e2.Result)
	}
	if e2.Error == "" {
		t.Error("want non-empty error field")
	}
}

func TestReadHistory_basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	l := New(path)

	for i := 0; i < 10; i++ {
		l.AppendOK("rebuild", "g", nil)
	}
	l.AppendOK("settings_update", "theme", nil)
	l.Close()

	// Read last 5
	entries, err := ReadHistory(path, 5, "")
	if err != nil {
		t.Fatalf("ReadHistory: %v", err)
	}
	if len(entries) != 5 {
		t.Errorf("want 5 entries, got %d", len(entries))
	}

	// Filter by operation
	entries, err = ReadHistory(path, 100, "settings_update")
	if err != nil {
		t.Fatalf("ReadHistory filtered: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("want 1 settings_update entry, got %d", len(entries))
	}
}

func TestReadHistory_missingFile(t *testing.T) {
	entries, err := ReadHistory("/nonexistent/audit.jsonl", 50, "")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil entries for missing file")
	}
}

func TestAppend_timestampAutoSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	l := New(path)

	before := time.Now().UTC()
	l.Append(Entry{Operation: "test", Result: "ok"})
	l.Close()

	entries, err := ReadHistory(path, 1, "")
	if err != nil || len(entries) != 1 {
		t.Fatalf("read: %v, len=%d", err, len(entries))
	}
	if entries[0].Timestamp == "" {
		t.Error("timestamp should be auto-set")
	}
	_ = before
}

func splitLines(data []byte) []string {
	var lines []string
	start := 0
	for i := 0; i <= len(data); i++ {
		if i == len(data) || data[i] == '\n' {
			if i > start {
				lines = append(lines, string(data[start:i]))
			}
			start = i + 1
		}
	}
	return lines
}
