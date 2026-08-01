package install

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/version"
)

// ── #6070: the step-4 version guard compared a DECORATED string to a BARE tag ─
//
// `grafel install` aborted at step 4 on every platform from 2026-07-17 onward
// (v0.2.0 shipped with it) because verifyDaemonVersion compared:
//
//	running   = version.String() = "v0.2.0 (commit f2fb8c3, built 2026-07-25T…)"
//	installed = version.Version  = "v0.2.0"
//
// with an exact equality after stripping a single leading 'v'. Those can never
// be equal, so the guard fired on the SUCCESS path and rolled the install back
// — leaving the MCP server unregistered.
//
// Every fixture in this file derives its "running" value from the REAL
// version.String() / version.Version pair (or from the exact shapes the
// release, acceptance and Makefile builds produce) rather than hand-typed bare
// versions. A fixture that feeds two bare strings to the comparison passes
// while the product stays broken — the failure mode this repo has hit
// repeatedly. See TestReleaseIdentity_RejectsNaiveTrimPrefix for the guard
// that the fixture itself can actually fail.

// realProbeShapes returns the (display, bare) pairs a real daemon reports,
// for every build configuration grafel actually ships or builds.
func realProbeShapes() []struct {
	name    string
	display string
	bare    string
} {
	return []struct {
		name    string
		display string
		bare    string
	}{
		{
			// THE fixture: the live version package as compiled into this test
			// binary. It cannot drift from the product's real format because it
			// IS the product's format function.
			name:    "live version package",
			display: version.String(),
			bare:    version.Version,
		},
		{
			// release.yml: -X version.Version=${GITHUB_REF_NAME} (v-prefixed
			// tag) + Commit=${SHORT_SHA} + Date=${BUILD_DATE}. Measured, not
			// guessed: this is the exact string quoted in issue #6070.
			name:    "release build (v0.2.0)",
			display: "v0.2.0 (commit f2fb8c3, built 2026-07-25T12:00:00Z)",
			bare:    "v0.2.0",
		},
		{
			// acceptance.yml builds with -ldflags "-s -w" only, so no version
			// is injected and String() falls back to debug.ReadBuildInfo.
			name:    "acceptance build (no ldflags injection)",
			display: "0.0.0-dev (commit f2fb8c315403, built 2026-08-01T11:08:38Z)",
			bare:    "0.0.0-dev",
		},
		{
			// Makefile: version.Version = `git describe --tags --always --dirty`.
			// 67 characters — longer than the old 64-char looksLikeVersion cap.
			name:    "make build (git describe)",
			display: "v0.1.9-82-gf2fb8c315 (commit f2fb8c315, built 2026-08-01T11:08:38Z)",
			bare:    "v0.1.9-82-gf2fb8c315",
		},
		{
			// A dirty working tree pushes it to 73 characters.
			name:    "make build (dirty tree)",
			display: "v0.1.9-82-gf2fb8c315-dirty (commit f2fb8c315, built 2026-08-01T11:08:38Z)",
			bare:    "v0.1.9-82-gf2fb8c315-dirty",
		},
	}
}

// TestVersionString_IsActuallyDecorated is the premise check. If version.String()
// ever stopped decorating the bare version, the whole class of tests below would
// pass vacuously — so assert the premise explicitly.
func TestVersionString_IsActuallyDecorated(t *testing.T) {
	display := version.String()
	bare := version.Version
	if display == bare {
		t.Fatalf("premise broken: version.String() == version.Version == %q; "+
			"the #6070 fixtures below would no longer exercise the decorated shape", display)
	}
	if !strings.HasPrefix(display, bare) {
		t.Fatalf("version.String() = %q does not start with version.Version = %q; "+
			"releaseIdentity's leading-token parse assumes it does", display, bare)
	}
	t.Logf("real shapes in this build: display=%q bare=%q", display, bare)
}

// TestReleaseIdentity_RejectsNaiveTrimPrefix is the fixture's own proof of
// failure: it asserts that the PRE-FIX comparison (a bare TrimPrefix equality,
// reproduced verbatim here) is WRONG for every real shape. If someone reverts
// releaseIdentity to the old one-line TrimPrefix, this test's premise — that
// the naive comparison disagrees — still holds, and the tests below go red.
func TestReleaseIdentity_RejectsNaiveTrimPrefix(t *testing.T) {
	// The exact pre-fix implementation from copy.go:247.
	preFix := func(a, b string) bool {
		return strings.TrimPrefix(a, "v") == strings.TrimPrefix(b, "v")
	}
	for _, c := range realProbeShapes() {
		if preFix(c.display, c.bare) {
			t.Errorf("%s: the pre-fix comparison unexpectedly matched %q vs %q — "+
				"this fixture is not exercising the decorated shape and would pass "+
				"against the broken code", c.name, c.display, c.bare)
		}
	}
}

// TestVersionsEquivalent_DecoratedVsBare is the direct unit repro of #6070.
func TestVersionsEquivalent_DecoratedVsBare(t *testing.T) {
	for _, c := range realProbeShapes() {
		if !versionsEquivalent(c.display, c.bare) {
			t.Errorf("%s: versionsEquivalent(%q, %q) = false, want true — "+
				"the decorated daemon string names the same release as the bare tag",
				c.name, c.display, c.bare)
		}
		// Symmetric, and reflexive on the bare form.
		if !versionsEquivalent(c.bare, c.display) {
			t.Errorf("%s: versionsEquivalent is not symmetric for %q / %q", c.name, c.bare, c.display)
		}
	}
}

// TestVersionsEquivalent_GenuineMismatchStillDetected is the counterweight: a
// fix that makes the guard always pass is WORSE than the bug, because it lets a
// genuinely stale daemon through silently. An actually-different release must
// still compare unequal, including when the stale daemon reports a decorated
// string (which is what a real stale daemon does).
func TestVersionsEquivalent_GenuineMismatchStillDetected(t *testing.T) {
	cases := []struct{ running, installed, why string }{
		{"v0.1.9 (commit aaaaaaa, built 2026-07-01T00:00:00Z)", "v0.2.0",
			"stale daemon one minor behind, decorated"},
		{"v0.2.0 (commit aaaaaaa, built 2026-07-01T00:00:00Z)", "v0.2.1",
			"stale daemon one patch behind, decorated"},
		{"0.0.0-dev (commit aaaaaaa, built 2026-07-01T00:00:00Z)", "v0.2.0",
			"a dev daemon must NOT be accepted as a release install"},
		{"v0.1.9-82-gf2fb8c315 (commit f2fb8c315, built 2026-08-01T11:08:38Z)", "v0.1.9",
			"a make build 82 commits past the tag is not the tag"},
		{"v0.2.0 (commit aaaaaaa, built 2026-07-01T00:00:00Z)", "v0.20.0",
			"prefix-only similarity must not match"},
		{"v0.2.0 (commit aaaaaaa, built 2026-07-01T00:00:00Z)",
			"v0.2.0-rc1", "a release candidate is a different release"},
	}
	for _, c := range cases {
		if versionsEquivalent(c.running, c.installed) {
			t.Errorf("versionsEquivalent(%q, %q) = true, want false (%s)", c.running, c.installed, c.why)
		}
	}
}

// TestVerifyDaemonVersion_RealDecoratedString_Succeeds exercises the guard the
// way step 4 calls it, with the probe reporting exactly what a real daemon
// reports: the decorated display string plus the structured bare field.
func TestVerifyDaemonVersion_RealDecoratedString_Succeeds(t *testing.T) {
	for _, c := range realProbeShapes() {
		got, err := verifyDaemonVersion(c.bare, func() (ProbedVersion, error) {
			return ProbedVersion{Display: c.display, Bare: c.bare}, nil
		})
		if err != nil {
			t.Errorf("%s: verifyDaemonVersion(installed=%q) failed against a real daemon "+
				"reporting %q: %v", c.name, c.bare, c.display, err)
			continue
		}
		// The recorded/printed version must stay the verbatim display string —
		// the comparison is normalised, the RECORD is not.
		if got != strings.TrimSpace(c.display) {
			t.Errorf("%s: verifyDaemonVersion returned %q, want the verbatim display string %q",
				c.name, got, c.display)
		}
	}
}

// TestVerifyDaemonVersion_NoStructuredField_FallsBackToParse covers the daemon
// that predates PingReply.VersionBare (anything <= v0.2.0). That is not an edge
// case: it is EXACTLY the population the guard exists to catch, because a
// pre-v0.2.1 binary left bound to the socket is the stale daemon of #5850. The
// structured field cannot be relied on alone, so the parse fallback must work.
func TestVerifyDaemonVersion_NoStructuredField_FallsBackToParse(t *testing.T) {
	const display = "v0.2.0 (commit f2fb8c3, built 2026-07-25T12:00:00Z)"

	// Same release, no structured field → must succeed via the parse fallback.
	if _, err := verifyDaemonVersion("v0.2.0", func() (ProbedVersion, error) {
		return ProbedVersion{Display: display}, nil
	}); err != nil {
		t.Errorf("old daemon (no version_bare) reporting %q vs installed v0.2.0: %v", display, err)
	}

	// Genuinely stale, no structured field → must still be caught.
	_, err := verifyDaemonVersion("v0.2.1", func() (ProbedVersion, error) {
		return ProbedVersion{Display: display}, nil
	})
	if err == nil {
		t.Fatal("a genuinely stale old daemon (no version_bare) must still be detected")
	}
	if !strings.Contains(err.Error(), "0.2.0") || !strings.Contains(err.Error(), "0.2.1") {
		t.Errorf("error must name both releases, got: %v", err)
	}
}

// TestVerifyDaemonVersion_StructuredFieldWins asserts the structured field is
// the PRIMARY comparison basis, not a tiebreaker. If the daemon says its bare
// release is X, that is authoritative even when the display string is
// unparseable — parsing a display string is what broke this in the first place.
func TestVerifyDaemonVersion_StructuredFieldWins(t *testing.T) {
	// Display deliberately does NOT lead with the release token.
	const weird = "grafel daemon build v0.2.0 (commit f2fb8c3)"

	if _, err := verifyDaemonVersion("v0.2.0", func() (ProbedVersion, error) {
		return ProbedVersion{Display: weird, Bare: "v0.2.0"}, nil
	}); err != nil {
		t.Errorf("structured bare field should be authoritative over the display string: %v", err)
	}
	// And it must be authoritative in the REJECTING direction too — a daemon
	// whose display string happens to lead with the right token but whose
	// structured field says otherwise is stale.
	if _, err := verifyDaemonVersion("v0.2.0", func() (ProbedVersion, error) {
		return ProbedVersion{Display: "v0.2.0 (commit f2fb8c3)", Bare: "v0.1.9"}, nil
	}); err == nil {
		t.Error("structured bare field disagreeing with installed must be reported as stale")
	}
}

// TestVerifyDaemonVersion_ErrorNamesBothReleases is the acceptance-legibility
// requirement. acceptance.yml was red for five consecutive runs across three OS
// legs and was read as "acceptance is just broken", partly because the error
//
//	daemon is running stale version "v0.2.0 (commit …)", installed version is "v0.2.0"
//
// looks like a bug in the daemon rather than a bug in the comparison. The
// message must state the compared RELEASE IDENTITIES and where they came from,
// so a reader can see at a glance whether the releases actually differ.
func TestVerifyDaemonVersion_ErrorNamesBothReleases(t *testing.T) {
	_, err := verifyDaemonVersion("v0.2.1", func() (ProbedVersion, error) {
		return ProbedVersion{
			Display: "v0.2.0 (commit f2fb8c3, built 2026-07-25T12:00:00Z)",
			Bare:    "v0.2.0",
		}, nil
	})
	if err == nil {
		t.Fatal("expected a stale-version error")
	}
	msg := err.Error()
	for _, want := range []string{
		"v0.2.0 (commit f2fb8c3", // the verbatim running display string
		"v0.2.1",                 // the installed version
		"release",                // labels the normalised identities
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("stale-version error must contain %q for the failure to be legible; got: %s", want, msg)
		}
	}
}

// TestLooksLikeVersion_AcceptsRealDecoratedStrings guards the OTHER half of the
// defect class. looksLikeVersion capped plausible versions at 64 characters,
// but a Makefile-built daemon reports 67 (73 on a dirty tree). That build's
// probe result was rejected as "implausible" BEFORE any comparison happened —
// so fixing versionsEquivalent alone would leave `grafel install` broken for
// anyone running a locally-built daemon.
func TestLooksLikeVersion_AcceptsRealDecoratedStrings(t *testing.T) {
	for _, c := range realProbeShapes() {
		if !looksLikeVersion(c.display) {
			t.Errorf("%s: looksLikeVersion(%q) = false (len %d); a real daemon's version "+
				"string must never be rejected as implausible",
				c.name, c.display, len(c.display))
		}
	}
}
