package mcpreg

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// #6168: a "restore" that silently becomes a "delete" once its snapshot has
// been discarded is how an install that got as far as registering MCP, then
// tripped over an unrelated later step, ended up removing the user's MCP
// server. The old RestorePath had exactly that fallback; it has been deleted
// rather than documented around, because an exported name whose contract is
// "may delete instead" is what the next author reaches for by mistake.
//
// RestoreSnapshot makes the intent explicit: no snapshot means nothing to
// restore, reported as ErrNoSnapshot, never acted on. Callers that want removal
// call UnregisterPath, which says so.

// TestRestoreSnapshot_ReportsInsteadOfDeleting: a snapshot discarded mid-flight
// must NOT cause the entry to be removed.
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
