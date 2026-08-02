package install

import (
	"errors"
	"strings"
	"testing"
)

// spaIndexHTML is the dashboard's index.html, which is what an unmatched
// GET /healthz actually receives: the daemon registers no /healthz route, so
// the request falls through to the SPA catch-all and comes back HTTP 200 with
// ~900 bytes of HTML instead of a version.
const spaIndexHTML = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <title>grafel</title>
    <script type="module" crossorigin src="/assets/index-a1b2c3d4.js"></script>
    <link rel="stylesheet" href="/assets/index-e5f6a7b8.css" />
  </head>
  <body>
    <div id="root"></div>
  </body>
</html>
`

func stubProbe(p ProbedVersion, err error) DaemonVersionProbeFunc {
	return func() (ProbedVersion, error) { return p, err }
}

// Issue #6072: `grafel doctor` printed the dashboard's index.html as the
// daemon's running version. checkDaemon GET /healthz — a route that does not
// exist — json.Unmarshal failed on the HTML that came back, the plain-text
// fallback stuffed the ENTIRE document into the version field, and line 397
// compared that blob against install.json's DaemonVersion and printed it.
//
// checkDaemon now reads the RPC socket instead, but the invariant that actually
// protects the user is independent of the channel: whatever the version source
// hands back, an HTML document must never surface as a version.
//
// MUTATION ORACLE: delete the looksLikeVersion guard in checkDaemon → this test
// fails with the HTML document quoted in the drift message.
func TestCheckDaemon_NeverReportsHTMLAsTheRunningVersion(t *testing.T) {
	state := &State{DaemonVersion: "v0.2.1"}

	cr := checkDaemon(state, stubProbe(ProbedVersion{Display: spaIndexHTML}, nil))

	if cr.OK {
		t.Fatal("checkDaemon reported OK for a daemon that returned no usable version")
	}
	for _, d := range cr.Drift {
		if strings.ContainsAny(d, "<>") || strings.Contains(d, "doctype") || strings.Contains(d, "\n") {
			t.Fatalf("doctor reported HTML as a version string; drift = %q", d)
		}
	}
}

// A healthy daemon whose decorated display version names the SAME release as
// install.json must pass. This is the case the old raw-string comparison could
// never satisfy (#6070): the daemon reports "v0.2.1 (commit …, built …)" while
// install.json records the bare tag "v0.2.1".
//
// MUTATION ORACLE: compare `running != state.DaemonVersion` instead of the
// release identities → this test fails with a spurious version mismatch.
func TestCheckDaemon_DecoratedDisplayMatchingInstalledReleaseIsOK(t *testing.T) {
	state := &State{DaemonVersion: "v0.2.1"}
	probed := ProbedVersion{
		Display: "v0.2.1 (commit f2fb8c315, built 2026-08-01T11:08:38Z)",
		Bare:    "v0.2.1",
	}
	cr := checkDaemon(state, stubProbe(probed, nil))
	if !cr.OK {
		t.Fatalf("healthy matching daemon flagged as drift: %v", cr.Drift)
	}
}

// The structured VersionBare is the comparison basis when present, and the
// display string is the fallback for pre-v0.2.1 daemons that omit it.
func TestCheckDaemon_ComparisonBasis(t *testing.T) {
	t.Run("bare field used when present", func(t *testing.T) {
		state := &State{DaemonVersion: "v0.2.1"}
		cr := checkDaemon(state, stubProbe(ProbedVersion{
			Display: "v0.2.1 (commit abc, built 2026-08-01T00:00:00Z)",
			Bare:    "v0.2.1",
		}, nil))
		if !cr.OK {
			t.Fatalf("want OK, got drift %v", cr.Drift)
		}
	})
	t.Run("display parsed when bare is absent", func(t *testing.T) {
		state := &State{DaemonVersion: "v0.2.1"}
		cr := checkDaemon(state, stubProbe(ProbedVersion{
			Display: "v0.2.1 (commit abc, built 2026-08-01T00:00:00Z)",
		}, nil))
		if !cr.OK {
			t.Fatalf("want OK for a pre-VersionBare daemon on the same release, got drift %v", cr.Drift)
		}
	})
}

// A genuinely stale daemon must still be caught — the fix must not make the
// check unconditionally green.
//
// MUTATION ORACLE: drop the release comparison → this test fails (no drift
// reported for a daemon two releases behind).
func TestCheckDaemon_GenuineVersionSkewIsReported(t *testing.T) {
	state := &State{DaemonVersion: "v0.2.1"}
	cr := checkDaemon(state, stubProbe(ProbedVersion{
		Display: "v0.1.9 (commit deadbee, built 2026-07-01T00:00:00Z)",
		Bare:    "v0.1.9",
	}, nil))
	if cr.OK {
		t.Fatal("a daemon running v0.1.9 against an installed v0.2.1 was reported OK")
	}
	if cr.Severity != SeverityWarning {
		t.Fatalf("severity = %v, want warning", cr.Severity)
	}
	joined := strings.Join(cr.Drift, " ")
	if !strings.Contains(joined, "v0.1.9") || !strings.Contains(joined, "v0.2.1") {
		t.Fatalf("drift does not name both versions: %q", joined)
	}
}

// An unreachable daemon is a critical failure, not a version warning.
func TestCheckDaemon_UnreachableIsCritical(t *testing.T) {
	state := &State{DaemonVersion: "v0.2.1"}
	cr := checkDaemon(state, stubProbe(ProbedVersion{}, errors.New("dial unix: no such file or directory")))
	if cr.OK {
		t.Fatal("unreachable daemon reported OK")
	}
	if cr.Severity != SeverityCritical {
		t.Fatalf("severity = %v, want critical", cr.Severity)
	}
}

// With no recorded DaemonVersion there is nothing to compare against; a live
// daemon reporting a plausible version must not be flagged.
func TestCheckDaemon_NoInstalledVersionSkipsComparison(t *testing.T) {
	cr := checkDaemon(&State{}, stubProbe(ProbedVersion{Display: "v0.2.1", Bare: "v0.2.1"}, nil))
	if !cr.OK {
		t.Fatalf("want OK when install.json records no daemon version, got drift %v", cr.Drift)
	}
}
