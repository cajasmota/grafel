package mcp

import (
	"fmt"
	"path/filepath"
	"sync/atomic"

	"github.com/cajasmota/grafel/internal/registry"
)

// docsRootOverride holds the TEST-ONLY docs-root seam. nil (the zero value)
// means "use the real ~/.grafel/docs"; production never stores anything here.
//
// #6075: handleDocgenPromote's rotate branch os.Rename()s
// canonicalDocsPath(group), which resolved to $HOME/.grafel/docs/<group> — the
// user's live docs. Nothing in the test suite stopped that; the suite was safe
// only because TestWorkflowDocgenDispatch's hardcoded run ID never matched a
// generated one, so resolveStagingPath's os.Stat failed and promote returned
// before reaching rotate. A contributor improving that fixture to get real
// coverage would have been doing the right thing and would have had their own
// ~/.grafel/docs renamed, with no failing assertion and damage outside any
// temp dir.
//
// A refusal-to-rotate-outside-a-designated-root would have been the cheaper
// interim, but it leaves promote's most destructive branch permanently
// untestable — the defect is that the branch is only reachable by accident.
// Injecting the root instead makes it reachable ON PURPOSE, inside a
// t.TempDir, which is what the coverage in
// docgen_promote_containment_6075_test.go depends on.
//
// #6056/#6059: this MUST stay an atomic, for the same reason as clock.go's
// nowOverride — MCP tool handlers run on live, concurrent request goroutines,
// so a test's t.Cleanup restore would race any in-flight handler reading a
// plain package var.
var docsRootOverride atomic.Pointer[string]

// setDocsRootOverride installs (dir != "") or clears (dir == "") the docs-root
// seam. TEST-ONLY; never called from production. Tests should prefer the
// t.Cleanup-scoped wrapper setDocsRootForTest (docsroot_test.go).
func setDocsRootOverride(dir string) {
	if dir == "" {
		docsRootOverride.Store(nil)
		return
	}
	d := dir
	docsRootOverride.Store(&d)
}

// docsRoot returns the directory holding every group's promoted docs: the
// injected root when the seam is installed, else <grafel home>/docs.
//
// The real derivation goes through registry.HomeDir(), not os.Getenv("HOME").
// canonicalDocsPath used to read HOME directly and never consult GRAFEL_HOME,
// while every other docs path in the tree IS GRAFEL_HOME-aware
// (internal/daemon/docs_path.go, internal/docgen/tier0.go..tier4.go,
// internal/cli/docgen.go's promote path) — so an isolated GRAFEL_HOME run
// promoted docs to one directory and read them from another. That split-brain
// was recorded in #6178's knownDeferred ledger and deferred to this issue;
// this is where it gets paid off, and the ledger entry is removed with it.
func docsRoot() (string, error) {
	if p := docsRootOverride.Load(); p != nil {
		return *p, nil
	}
	home, err := registry.HomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine grafel home directory: %w", err)
	}
	return filepath.Join(home, "docs"), nil
}
