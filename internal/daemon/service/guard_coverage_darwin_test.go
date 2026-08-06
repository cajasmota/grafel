//go:build darwin

package service

// guard_coverage_darwin_test.go proves the service-manager guard actually
// covers THIS package's mutating call sites.
//
// These four vars manage `com.grafel.daemon` — the label serving the user's
// live MCP session. A test that reaches launchctlBootout here does not disable
// a watcher, it kills the running daemon. They were seamed (package vars) but
// unguarded, which is the "declared a seam, then did not use it" shape that
// already let install.go drive real launchctl calls while a test believed
// itself isolated.
//
// Why a PATH-shim measurement does not substitute for this: a shim that makes
// `launchctl list` exit non-zero makes every Unload short-circuit before its
// bootout, so "zero mutating verbs observed" proves only that the path was not
// reached under that launchd state — not that it cannot be. Guarding the call
// site makes the property hold regardless of what is loaded.

import (
	"strings"
	"testing"
)

// TestMutatingLaunchctlHelpersAreGuarded calls each REAL helper (not a stub)
// and requires it to panic rather than shell out.
func TestMutatingLaunchctlHelpersAreGuarded(t *testing.T) {
	cases := []struct {
		name string
		call func()
	}{
		{"bootout", func() { _ = launchctlBootout("0") }},
		{"bootstrap", func() { _, _ = launchctlBootstrap("0", "/nonexistent.plist") }},
		{"disable", func() { _ = launchctlDisable("0") }},
		{"enable", func() { _ = launchctlEnable("0") }},
	}
	for _, c := range cases {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("launchctl %s is NOT guarded — a test can boot out the user's live daemon", c.name)
				}
				msg, ok := r.(string)
				if !ok || !strings.Contains(msg, c.name) {
					t.Fatalf("panic does not name the verb %q: %v", c.name, r)
				}
			}()
			c.call()
		}()
	}
}
