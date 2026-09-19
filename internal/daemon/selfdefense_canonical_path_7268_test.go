package daemon

// selfdefense_canonical_path_7268_test.go — alphabet of IsCanonicalBinaryPath,
// the identity gate the two kill-stale call sites now share (#7268).
//
// The gate answers "is this exec path our binary", and a false positive there
// means SIGTERM to a stranger's process, so the accept side is enumerated
// tightly and the reject side broadly.

import "testing"

func TestIsCanonicalBinaryPath_7268(t *testing.T) {
	accept := []string{
		"/usr/local/bin/grafel",
		"/tmp/agent-worktree/grafel",
		"/opt/grafel/daemon/bin/grafel",
		"/Users/jane smith/Library/grafel", // spaces are ordinary path characters
		"/opt/GRAFEL",                      // the basename match is case-insensitive
		"/opt/Grafel/Daemon/bin/GRAFEL",
	}
	for _, p := range accept {
		if !IsCanonicalBinaryPath(p) {
			t.Errorf("IsCanonicalBinaryPath(%q) = false, want true", p)
		}
	}

	reject := []string{
		// not our binary — the basename is what decides
		"/Users/jane smith/Library/grafel-daemon-helper/bin/helper",
		"/Users/jane/src/grafel/webui-v2/node_modules/@esbuild/darwin-arm64/bin/esbuild",
		"/usr/local/bin/grafel-daemon-old",
		"/opt/grafel/daemon/bin/daemon",
		"/opt/grafel/bin/grafeld",
		"/opt/grafel/bin/mygrafel",
		"/opt/grafel/bin/grafel-mcp",
		"/opt/grafel/bin/grafel.exe", // no extension stripping: see doc
		// not an absolute path — identity is not established
		"grafel",
		"./grafel",
		"../bin/grafel",
		"bin/grafel",
		"",
		// a directory named grafel is not a grafel binary
		"/opt/grafel/",
	}
	for _, p := range reject {
		if IsCanonicalBinaryPath(p) {
			t.Errorf("IsCanonicalBinaryPath(%q) = true, want false", p)
		}
	}
}

// TestIsCanonicalBinaryPath_AgreesWithFindCanonicalDaemon_7268 checks the gate
// against the set it was lifted from, so the two cannot drift: every name in
// canonicalBasenames must be accepted when given as an absolute path.
func TestIsCanonicalBinaryPath_AgreesWithCanonicalBasenames_7268(t *testing.T) {
	if len(canonicalBasenames) == 0 {
		t.Fatal("canonicalBasenames is empty — this test would be vacuous")
	}
	for name := range canonicalBasenames {
		if !IsCanonicalBinaryPath("/usr/local/bin/" + name) {
			t.Errorf("canonicalBasenames contains %q but IsCanonicalBinaryPath rejects it", name)
		}
		if IsCanonicalBinaryPath(name) {
			t.Errorf("IsCanonicalBinaryPath(%q) accepted a bare basename", name)
		}
	}
}
