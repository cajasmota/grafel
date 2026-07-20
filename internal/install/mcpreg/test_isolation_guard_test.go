package mcpreg

// test_isolation_guard_test.go — verifies the fail-closed guard against
// leaking the test binary path into a REAL host MCP config: a write that
// targets a path inside the REAL user home panics LOUDLY, while a write into
// an isolated TempDir succeeds exactly as before.

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestGuard_RegisterPathPanicsWhenWritingRealHome proves the guard fires when
// RegisterPath's target lands inside the genuine user home — the exact
// dashboard-wizard leak. We do NOT expect any file to be created; the guard
// must panic before MkdirAll/backupOnce/writeSettings run.
func TestGuard_RegisterPathPanicsWhenWritingRealHome(t *testing.T) {
	if realUserHomeAtInit == "" {
		t.Skip("no real user home captured; cannot exercise the escape path")
	}

	escape := filepath.Join(realUserHomeAtInit, ".cursor", "mcp.json.guardtest")

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected guard panic when writing MCP config to real home %q, got none", escape)
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "TEST SANDBOX ESCAPE") || !strings.Contains(msg, "IsolateHome") {
			t.Fatalf("panic message did not mention the guard / remediation: %q", msg)
		}
	}()

	_, _ = RegisterPath(escape, "/bin/grafel")
	t.Fatalf("RegisterPath returned without panicking — guard did not fire")
}

// TestGuard_UnregisterPathPanicsWhenWritingRealHome mirrors the above for the
// uninstall path.
func TestGuard_UnregisterPathPanicsWhenWritingRealHome(t *testing.T) {
	if realUserHomeAtInit == "" {
		t.Skip("no real user home captured")
	}
	escape := filepath.Join(realUserHomeAtInit, ".claude.json.guardtest")
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected guard panic unregistering MCP config at real home %q", escape)
		}
	}()
	_ = UnregisterPath(escape)
	t.Fatalf("UnregisterPath returned without panicking — guard did not fire")
}

// TestGuard_RestorePathPanicsWhenWritingRealHome mirrors the above for the
// rollback path.
func TestGuard_RestorePathPanicsWhenWritingRealHome(t *testing.T) {
	if realUserHomeAtInit == "" {
		t.Skip("no real user home captured")
	}
	escape := filepath.Join(realUserHomeAtInit, ".kiro", "settings", "mcp.json.guardtest")
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected guard panic restoring MCP config at real home %q", escape)
		}
	}()
	_ = RestorePath(escape)
	t.Fatalf("RestorePath returned without panicking — guard did not fire")
}

// TestGuard_AllowsWriteUnderIsolatedHome proves the guard is inert once the
// target is under a TempDir (the isolated case), and the write actually lands
// — i.e. it does not regress the ~38 existing isolated mcpreg tests.
func TestGuard_AllowsWriteUnderIsolatedHome(t *testing.T) {
	withHome(t)
	path, err := Register(ClaudeCode, "/bin/grafel", "")
	if err != nil {
		t.Fatalf("Register under isolated home should succeed: %v", err)
	}
	if realUserHomeAtInit != "" {
		abs, _ := filepath.Abs(path)
		if strings.HasPrefix(filepath.Clean(abs)+string(filepath.Separator), realUserHomeAtInit+string(filepath.Separator)) {
			t.Fatalf("isolated config path %q unexpectedly under real home %q", abs, realUserHomeAtInit)
		}
	}
	if !HasGrafelEntry(path) {
		t.Fatalf("expected grafel entry to be registered at isolated path %q", path)
	}
}
