package main

import (
	"errors"
	"io/fs"
	"testing"
)

// TestSelftestDaemonConfigWiresShutdownCleanup is the #5264 regression at the
// wiring level: the in-process selftest daemon config MUST install the
// graceful-stop cleanup (daemonShutdownCleanup), otherwise the MCP activity disk
// log handle that the grafel_stats call lazily opens is never closed and the
// Windows teardown layer fails to remove the isolated root. The previous fix
// (#5271) only wired this into the production runDaemon config.
func TestSelftestDaemonConfigWiresShutdownCleanup(t *testing.T) {
	env := &selftestEnv{}
	cfg := selftestDaemonConfig(env)
	if cfg.ShutdownCleanup == nil {
		t.Fatal("selftest daemon config must set ShutdownCleanup so the MCP " +
			"activity log handle is closed on shutdown (#5264)")
	}
	// It must be safe to invoke even when no MCP server / broker was ever
	// constructed (nothing to stop or close): best-effort + idempotent.
	cfg.ShutdownCleanup()
	cfg.ShutdownCleanup()
}

// TestIsWindowsFileInUse verifies the cross-platform teardown backstop predicate:
// it matches the typed permission error (fs.ErrPermission) and rejects everything
// else — including LOCALIZED OS message strings, which are intentionally NOT
// matched (#5321 locale-invariance). Real Windows file-in-use errors carry a
// numeric syscall.Errno (32/5/33), matched by the Windows-only errno path
// (exercised in selftest_errno_windows_test.go). The predicate is only consulted
// on Windows but the cross-platform surface is unit-tested everywhere.
func TestIsWindowsFileInUse(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"permission", fs.ErrPermission, true},
		// Localized message strings must NOT match (locale-invariance, #5321):
		// a Spanish/Japanese Windows emits different text but the same errno.
		{"localized in-use message (not matched)", errors.New("remove C:\\x: The process cannot access the file because it is being used by another process."), false},
		{"localized sharing violation (not matched)", errors.New("CreateFile foo: sharing violation"), false},
		{"localized access denied (not matched)", errors.New("open bar: Access is denied."), false},
		{"unrelated", errors.New("no such file or directory"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isWindowsFileInUse(tc.err); got != tc.want {
				t.Fatalf("isWindowsFileInUse(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
