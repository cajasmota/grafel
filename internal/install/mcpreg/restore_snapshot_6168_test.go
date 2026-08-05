package mcpreg

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// #6168: RestorePath's no-snapshot fallback is a trap for any caller whose
// intent is "undo a registration that DID run". Once the pristine sidecar has
// been discarded, the "restore" silently becomes a "delete" — which is exactly
// how an install that got as far as registering MCP, then tripped over an
// unrelated later step, ended up removing the user's MCP server.
//
// RestoreSnapshot exists so that intent is expressible: no snapshot means
// nothing to restore, reported as ErrNoSnapshot, never acted on.

// TestRestorePath_DegradesToDeleteWithoutSnapshot pins the documented (and
// dangerous) behaviour of RestorePath so the difference below is not
// theoretical.
func TestRestorePath_DegradesToDeleteWithoutSnapshot(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".claude.json")
	if _, err := RegisterPath(path, "/bin/grafel"); err != nil {
		t.Fatalf("RegisterPath: %v", err)
	}
	ClearBackup(path) // the discarded-too-early snapshot

	if err := RestorePath(path); err != nil {
		t.Fatalf("RestorePath: %v", err)
	}
	if hasGrafel(t, path) {
		t.Fatal("RestorePath kept the entry — this test pins the DELETE behaviour it is documented to have")
	}
}

// TestRestoreSnapshot_ReportsInsteadOfDeleting is the guard: the same setup
// must NOT remove the entry.
func TestRestoreSnapshot_ReportsInsteadOfDeleting(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".claude.json")
	if _, err := RegisterPath(path, "/bin/grafel"); err != nil {
		t.Fatalf("RegisterPath: %v", err)
	}
	ClearBackup(path)

	err := RestoreSnapshot(path)
	if !errors.Is(err, ErrNoSnapshot) {
		t.Fatalf("RestoreSnapshot error = %v, want ErrNoSnapshot", err)
	}
	if !hasGrafel(t, path) {
		t.Fatal("RestoreSnapshot deleted the grafel entry it had no snapshot for (#6168)")
	}
}

// TestRestoreSnapshot_RestoresWhenSnapshotExists confirms the happy path is
// unchanged: a real snapshot is restored byte-for-byte.
func TestRestoreSnapshot_RestoresWhenSnapshotExists(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".claude.json")
	pre := `{"mcpServers":{"playwright":{"command":"/bin/playwright"}},"other":"keep"}`
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterPath(path, "/bin/grafel"); err != nil {
		t.Fatalf("RegisterPath: %v", err)
	}
	if err := RestoreSnapshot(path); err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(b) != pre {
		t.Fatalf("not restored byte-for-byte:\n got: %s\nwant: %s", b, pre)
	}
}

func hasGrafel(t *testing.T, path string) bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		return false
	}
	_, ok := servers[ServerName]
	return ok
}
