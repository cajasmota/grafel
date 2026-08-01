package dashboard

import (
	"testing"

	"github.com/cajasmota/grafel/internal/version"
)

// ── #6070, one function away ────────────────────────────────────────────────
//
// The install bug was a comparison fed two DIFFERENTLY-SHAPED version strings
// while its tests only ever fed it matching shapes. handleUpdatesCheck has the
// same defect:
//
//	reply.LatestVersion = strings.TrimPrefix(rel.TagName, "v")   // "0.2.1"
//	reply.UpdateAvailable = isNewerVersion(reply.LatestVersion, version.Version)
//	                                                             // "v0.2.0"
//
// One side is v-stripped, the other is not. `"0.2.1" > "v0.2.0"` is FALSE in a
// byte-wise comparison ('0' = 0x30 < 'v' = 0x76), so the dashboard's update
// banner never appears on any release build — the only builds that can be
// updated. Every case in TestIsNewerVersion passes bare-vs-bare, which is why
// it stayed green.

// TestIsNewerVersion_RealCallSiteShapes drives isNewerVersion with exactly what
// handleUpdatesCheck passes it: a v-STRIPPED GitHub tag against the raw,
// possibly v-PREFIXED version.Version.
func TestIsNewerVersion_RealCallSiteShapes(t *testing.T) {
	// Mirrors handlers_updates.go: TrimPrefix on the tag, version.Version raw.
	callSite := func(githubTag, currentVersion string) bool {
		latest := trimV(githubTag)
		return isNewerVersion(latest, currentVersion)
	}

	tests := []struct {
		githubTag, current string
		want               bool
		why                string
	}{
		{"v0.2.1", "v0.2.0", true, "release build one patch behind must see the update"},
		{"v0.3.0", "v0.2.0", true, "release build one minor behind must see the update"},
		{"v1.0.0", "v0.2.0", true, "release build one major behind must see the update"},
		{"v0.2.0", "v0.2.0", false, "same release must not offer an update"},
		{"v0.2.0", "v0.2.1", false, "a newer local build must not be told to downgrade"},
		// The Makefile build: version.Version = `git describe --tags --dirty`.
		{"v0.2.1", "v0.1.9-82-gf2fb8c315", true, "a make build past v0.1.9 is behind v0.2.1"},
		// Double-digit minors are coming and byte-wise comparison gets them
		// backwards: "0.10.0" > "0.9.0" is false lexicographically.
		{"v0.10.0", "v0.9.0", true, "0.10.0 is newer than 0.9.0 — numeric, not lexicographic"},
		{"v0.9.0", "v0.10.0", false, "0.9.0 is older than 0.10.0"},
		{"v1.2.10", "v1.2.9", true, "double-digit patch"},
	}
	for _, tc := range tests {
		if got := callSite(tc.githubTag, tc.current); got != tc.want {
			t.Errorf("update check (github tag %q, current %q) = %v, want %v — %s",
				tc.githubTag, tc.current, got, tc.want, tc.why)
		}
	}
}

// TestIsNewerVersion_AgainstLiveVersionPackage uses the real version.Version of
// this build rather than a literal, so the test cannot drift from whatever
// shape the build system actually bakes in.
func TestIsNewerVersion_AgainstLiveVersionPackage(t *testing.T) {
	// This binary is built without ldflags injection, so version.Version is the
	// "0.0.0-dev" sentinel and every release counts as newer.
	if !isNewerVersion("99.0.0", version.Version) {
		t.Errorf("a far-future release must be newer than this build's version.Version = %q",
			version.Version)
	}
}

// trimV mirrors the v-stripping handleUpdatesCheck applies to the GitHub tag.
func trimV(tag string) string {
	if len(tag) > 0 && (tag[0] == 'v' || tag[0] == 'V') {
		return tag[1:]
	}
	return tag
}
