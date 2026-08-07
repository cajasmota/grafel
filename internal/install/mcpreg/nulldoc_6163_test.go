// nulldoc_6163_test.go pins RegisterPath against a host config whose top-level
// JSON value is not an object.
//
// readSettings does `doc := map[string]any{}` and then json.Unmarshal(b, &doc).
// For every scalar and for `[]` that is a type error and the call fails cleanly.
// For the literal `null` it is NOT an error: encoding/json's documented
// behaviour for unmarshalling null into a map is to set the map to nil and
// return no error. RegisterPath then reaches `doc["mcpServers"] = servers` and
// panics with "assignment to entry in nil map".
//
// This was reachable before only through `grafel install` proper. `grafel
// install --register-mcp` (#6169) now runs on EVERY install for EVERY user
// straight out of the curl one-liner, so a single stray `null` in any detected
// host config turns a `curl … | bash` into a Go panic stack. No data is lost —
// the write never happens — but a panic is not an installer's error report.
package mcpreg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestRegisterPath_NullDocumentDoesNotPanic is the #6163 review finding.
func TestRegisterPath_NullDocumentDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg := filepath.Join(dir, ".claude.json")
	if err := os.WriteFile(cfg, []byte("null"), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	// A panic here fails the test by crashing it; the recover exists only to
	// turn that into a readable failure rather than a stack dump.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RegisterPath panicked on a top-level null config: %v", r)
		}
	}()

	if _, err := RegisterPath(cfg, "/usr/local/bin/grafel"); err != nil {
		t.Fatalf("RegisterPath on a null config: %v", err)
	}

	// A `null` document carries no user data, so treating it as empty — the
	// same as a zero-byte file — is the honest outcome: the entry is written
	// and nothing was destroyed (the pre-existing bytes are in the sidecar
	// backup either way).
	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("result is not a JSON object: %v (%s)", err, raw)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if _, ok := servers["grafel"]; !ok {
		t.Errorf("no grafel entry written: %s", raw)
	}
}

// TestRegisterPath_NonObjectDocumentsStillFailCleanly: `null` is the one
// non-object that must be tolerated, because it is indistinguishable from
// "empty" and carries nothing. A top-level array or scalar is a real config the
// caller would be destroying, so those must keep failing — and must leave the
// file byte-identical.
func TestRegisterPath_NonObjectDocumentsStillFailCleanly(t *testing.T) {
	for _, body := range []string{`[]`, `[{"mcpServers":{}}]`, `"a string"`, `42`, `true`} {
		t.Run(body, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HOME", dir)
			cfg := filepath.Join(dir, ".claude.json")
			if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
				t.Fatalf("seed config: %v", err)
			}

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("RegisterPath panicked on %s: %v", body, r)
				}
			}()

			if _, err := RegisterPath(cfg, "/usr/local/bin/grafel"); err == nil {
				t.Errorf("RegisterPath silently accepted a non-object config %s", body)
			}
			got, err := os.ReadFile(cfg)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(got) != body {
				t.Errorf("file was modified despite the error:\n got: %s\nwant: %s", got, body)
			}
		})
	}
}
