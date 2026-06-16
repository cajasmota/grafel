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

// TestIsWindowsFileInUse verifies the teardown backstop predicate matches the
// Windows sharing-violation surfaces (typed permission error + OS message text)
// and rejects unrelated errors. The predicate is only consulted on Windows, but
// is unit-tested on every platform.
func TestIsWindowsFileInUse(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"permission", fs.ErrPermission, true},
		{"in-use message", errors.New("remove C:\\x: The process cannot access the file because it is being used by another process."), true},
		{"sharing violation", errors.New("CreateFile foo: sharing violation"), true},
		{"access denied", errors.New("open bar: Access is denied."), true},
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
