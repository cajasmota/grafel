package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeMapStatusFixture creates a temp repo root with a mapping file
// and one source file containing the cited function. Returns the root
// so each test can invoke the subcommand against it.
func writeMapStatusFixture(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "tools", "coverage"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "internal", "engine"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const mapping = `
records:
  lang.python.framework.flask:
    capabilities:
      endpoint_synthesis:
        status: full
        symbols:
          - file: internal/engine/flask.go
            functions: [synthesizeFlask, missingFn]
        tests:
          - file: internal/engine/flask_test.go
        verified_at: "2026-05-28"
        issues_implemented: ["2681"]
  lang.jsts.framework.react:
    capabilities:
      "Data Flow":
        state_management:
          status: partial
          symbols:
            - file: internal/engine/flask.go
              functions: [synthesizeFlask]
          verified_at: "2026-05-28"
`
	if err := os.WriteFile(filepath.Join(tmp, "tools", "coverage", "capability-map.yaml"), []byte(mapping), 0o644); err != nil {
		t.Fatalf("write map: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "internal", "engine", "flask.go"), []byte("package engine\n\nfunc synthesizeFlask() {}\n"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	return tmp
}

// TestMapStatus_FlatRecord exercises the human-readable output path
// against a flat record. We check the salient lines rather than the
// whole transcript to keep the assertion stable.
func TestMapStatus_FlatRecord(t *testing.T) {
	tmp := writeMapStatusFixture(t)
	var out bytes.Buffer
	err := cmdMapStatus([]string{"--repo-root", tmp, "lang.python.framework.flask/endpoint_synthesis"}, &out)
	if err != nil {
		t.Fatalf("map-status: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "lang.python.framework.flask/endpoint_synthesis") {
		t.Fatalf("missing header: %s", text)
	}
	if !strings.Contains(text, "[ok]       synthesizeFlask") {
		t.Fatalf("missing ok function row: %s", text)
	}
	if !strings.Contains(text, "[MISSING]  missingFn") {
		t.Fatalf("missing MISSING row for missingFn: %s", text)
	}
	if !strings.Contains(text, "functions 1/2") {
		t.Fatalf("bad checks summary: %s", text)
	}
}

// TestMapStatus_GroupedRecord exercises the three-part key form.
func TestMapStatus_GroupedRecord(t *testing.T) {
	tmp := writeMapStatusFixture(t)
	var out bytes.Buffer
	err := cmdMapStatus([]string{"--repo-root", tmp, "lang.jsts.framework.react/Data Flow/state_management"}, &out)
	if err != nil {
		t.Fatalf("map-status: %v", err)
	}
	if !strings.Contains(out.String(), "Data Flow/state_management") {
		t.Fatalf("missing grouped key in output: %s", out.String())
	}
}

// TestMapStatus_JSON sanity-checks the JSON emitter wires through.
func TestMapStatus_JSON(t *testing.T) {
	tmp := writeMapStatusFixture(t)
	var out bytes.Buffer
	err := cmdMapStatus([]string{"--repo-root", tmp, "--json", "lang.python.framework.flask/endpoint_synthesis"}, &out)
	if err != nil {
		t.Fatalf("map-status: %v", err)
	}
	body := out.String()
	for _, want := range []string{`"record_id": "lang.python.framework.flask"`, `"functions_missing": 1`, `"functions_ok": 1`} {
		if !strings.Contains(body, want) {
			t.Fatalf("json missing %q in %s", want, body)
		}
	}
}

// TestMapStatus_UnknownRecord returns an error rather than silently
// printing an empty report.
func TestMapStatus_UnknownRecord(t *testing.T) {
	tmp := writeMapStatusFixture(t)
	var out bytes.Buffer
	err := cmdMapStatus([]string{"--repo-root", tmp, "no.such.record/key"}, &out)
	if err == nil {
		t.Fatal("expected error for unknown record")
	}
}

// TestParseMapStatusKey verifies the key-parser handles flat, grouped,
// and malformed inputs.
func TestParseMapStatusKey(t *testing.T) {
	rec, group, cap, err := parseMapStatusKey("a.b/c")
	if err != nil || rec != "a.b" || group != "" || cap != "c" {
		t.Fatalf("flat: rec=%q group=%q cap=%q err=%v", rec, group, cap, err)
	}
	rec, group, cap, err = parseMapStatusKey("a.b/G/c")
	if err != nil || rec != "a.b" || group != "G" || cap != "c" {
		t.Fatalf("grouped: rec=%q group=%q cap=%q err=%v", rec, group, cap, err)
	}
	if _, _, _, err := parseMapStatusKey("toofew"); err == nil {
		t.Fatal("expected error for missing slash")
	}
}
