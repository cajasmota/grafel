package registry

// test_isolation_guard.go — fail-closed guard against the exact incident in
// #5443: a test that forgot to isolate $HOME/$XDG_CONFIG_HOME/$GRAFEL_HOME
// resolved the real ~/.config/grafel/<group>.fleet.json (or ~/.grafel) and
// CLOBBERED the developer's live fleet config, repointing the group's repos at
// a deleted t.TempDir so the group went to 0 entities.
//
// The opt-in TestMain guard (testsupport.GuardRealHomeMain) only fires when a
// package wires it up AND GRAFEL_TEST_REQUIRE_ISOLATED_HOME=1 is exported — it
// is trivially skipped, which is how #5443 slipped through. This guard lives in
// the WRITE path of the registry/fleet-config writers themselves, so it cannot
// be skipped: any test that is about to PERSIST the registry.json or a
// <group>.fleet.json into the REAL user home panics LOUDLY before a single byte
// is written.
//
// Why guard the writers and not the path resolvers: a read-only resolve (e.g.
// the dashboard reporting the perf-history path) is harmless and very common in
// tests; only a WRITE clobbers the developer's live config. Guarding
// registry.saveTo / registry.SaveGroupConfig catches the exact #5443 incident
// (a test persisting a fleet config to ~/.config/grafel) without breaking the
// many tests that merely resolve a path string.
//
// It is inert outside tests (`testing.Testing()` is false in the shipping
// binary) and inert inside tests that DID isolate (the write target no longer
// lands under the real home, so the panic condition is never met).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realUserHomeAtInit is captured at package-init time, BEFORE any test has had a
// chance to call t.Setenv("HOME", ...). It is the home we must never write to
// from a test. Captured the same way internal/testsupport captures it (kept
// independent to avoid an import cycle: testsupport is a test helper that may
// itself depend on registry).
var realUserHomeAtInit = func() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return filepath.Clean(h)
	}
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Clean(h)
	}
	return ""
}()

// guardResolvedConfigPath panics if, while running under `go test`, the path we
// are about to WRITE a config/registry/fleet file to lands inside the REAL user
// home directory. That can only happen when the test failed to redirect
// HOME / XDG_CONFIG_HOME / GRAFEL_HOME into a TempDir — i.e. the #5443 bug.
//
// It is a no-op in the shipping binary (testing.Testing() == false) and a no-op
// for any test that correctly isolated (the target path is then under a
// TempDir, not the real home).
func guardResolvedConfigPath(resolved, what string) {
	if !testing.Testing() {
		return
	}
	if realUserHomeAtInit == "" || resolved == "" {
		return
	}
	if isUnsafeTestWritePath(resolved, realUserHomeAtInit, os.TempDir()) {
		panic(fmt.Sprintf(
			"registry: TEST SANDBOX ESCAPE — about to WRITE %s to %q, which is inside the "+
				"REAL user home %q. This test would clobber the developer's live "+
				"~/.config/grafel/<group>.fleet.json / ~/.grafel/registry.json (see #5443). "+
				"Call testsupport.IsolateHome(t) at the top of the test before writing any "+
				"config/registry/fleet file.",
			what, resolved, realUserHomeAtInit,
		))
	}
}

// isUnsafeTestWritePath reports whether path targets the real user home while
// exempting the operating system's temp tree. On Windows, t.TempDir lives
// below %USERPROFILE%\AppData\Local\Temp, so a raw "under real home" prefix
// check falsely rejects correctly isolated tests.
func isUnsafeTestWritePath(path, realHome, tempRoot string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	abs = filepath.Clean(abs)
	if tempRoot != "" && pathWithin(abs, tempRoot) {
		return false
	}
	return pathWithin(abs, realHome)
}

func pathWithin(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
