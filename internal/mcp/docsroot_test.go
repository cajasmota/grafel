package mcp

import "testing"

// setDocsRootForTest points every docs-root derivation at dir for the duration
// of the test and restores the real one afterwards.
//
// Mirrors setFixedNowForTest (clock_test.go). Use this rather than calling
// setDocsRootOverride directly, so a test can never leak an override into a
// later test in the same binary — which, for this particular seam, would mean
// a later test silently operating on the developer's real ~/.grafel/docs.
func setDocsRootForTest(t *testing.T, dir string) {
	t.Helper()
	setDocsRootOverride(dir)
	t.Cleanup(func() { setDocsRootOverride("") })
}
