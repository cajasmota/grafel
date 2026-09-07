// Package watch — routes_event_parity_6952_test.go
//
// Issue #6952 routed Play/Revel routes files (`conf/routes`, `conf/*.routes`)
// in internal/classifier. That is a PRODUCER-side change, and #6934 is the
// standing lesson about what a producer-only change costs: the indexer starts
// reading a file the watcher still drops, so the graph is correct exactly once
// — at the initial index — and never updates again.
//
// The classifier has FOUR consumers, enumerated after review corrected an
// earlier claim of one: cmd/grafel/index.go:3973,
// internal/daemon/extract/coordinator.go:662, internal/daemon/extract/
// subproc.go:105, and internal/extractors/incremental.go:885/1002. All four
// call the same classifier, so none of them can disagree about a routes file —
// but "there is one consumer" was wrong as written, and the fourth is the
// UPDATE path, which is precisely where the #6934 class lives.
//
// The watcher consults none of them: ShouldSkipPath is a DENYLIST over SkipDirs
// / SkipExts / SkipBaseGlobs. So #6952 needed no watcher change, and these rows
// are what makes that a checked fact rather than a claim — they fail the moment
// anyone adds `conf` to SkipDirs or `.routes` to SkipExts.
//
// THIS FILE GRADES THE EVENT, NOT THE RE-EXTRACT. A change event that survives
// here and then hits a `continue` in incremental.go's re-extract loop is the
// same user-visible failure. That second half is graded separately, in
// internal/extractors/routes_incremental_reextract_6952_test.go.
//
// The `.bak` rows are the deliberate negative control, and they are the reason
// this test is not vacuous: `.bak` IS on SkipExts, so the watcher already drops
// exactly the file the classifier also declines to route (conf/routes.bak). A
// test that only asserted "nothing is skipped" would pass against a SkipExts of
// any size.
package watch

import "testing"

func TestRoutesFileEventsSurviveTheWatcherBoundary6952(t *testing.T) {
	mustWatch := []struct{ path, why string }{
		{"conf/routes", "the canonical Play/Revel router"},
		{"modules/admin/conf/routes", "a sub-project's own conf/"},
		{"src/main/resources/routes", "Play's maven-layout location"},
		{"conf/admin.routes", "a `-> /admin admin.Routes` sub-router include"},
		{"documentation/code/scalaguide.http.routing.routes", "a multi-dot include name — SkipBaseGlobs matches ANYWHERE in the basename, so a multi-dot name is exactly the shape those globs can catch by accident"},
	}
	for _, tc := range mustWatch {
		if ShouldSkipPath(tc.path) {
			t.Errorf("ShouldSkipPath(%q) = true — %s. The classifier routes this file (#6952); "+
				"dropping it here reproduces #6934: indexed once at walk time, never re-indexed "+
				"on change", tc.path, tc.why)
		}
	}

	// Negative control: the watcher must still drop what it always dropped.
	for _, p := range []string{"conf/routes.bak", "conf/routes.tmp", "conf/routes.swp"} {
		if !ShouldSkipPath(p) {
			t.Errorf("premise: ShouldSkipPath(%q) = false — SkipExts no longer holds this "+
				"suffix, so the rows above assert nothing", p)
		}
	}
	// And `conf` must not have become a skipped directory basename.
	if ShouldSkipDir("conf") {
		t.Error("ShouldSkipDir(\"conf\") = true — every Play routes file lives there")
	}
}
