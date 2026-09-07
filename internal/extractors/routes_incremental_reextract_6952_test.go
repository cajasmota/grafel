// Package extractors — routes_incremental_reextract_6952_test.go
//
// Issue #6952 routed Play/Revel routes files in internal/classifier. The PR
// body originally claimed the classifier was consulted at ONE boundary. THAT
// WAS FALSE, and the review caught it: there is a fourth site, here in
// incremental.go — `classifier.New(nil)` at the top of Step 6 and
// `cls.ClassifyWithSize(ctx, rel, …)` in the re-extract loop. It is the UPDATE
// path: the walker decides what is indexed once, and this decides what is
// re-indexed every time a watched file changes.
//
// internal/daemon/watch/routes_event_parity_6952_test.go proves the change
// EVENT is not dropped. Nothing proved the RE-EXTRACT lands, and the two are
// different failures: an event that arrives and then hits a `continue` here is
// #6934 wearing a different hat — indexed once at walk time, never refreshed.
//
// The loop has exactly two ways to drop a changed file, and this test is those
// two conditions, in order:
//
//	cr := cls.ClassifyWithSize(ctx, rel, int64(len(content)))
//	if cr.Skip || cr.Language == "" { … continue }   // (1)
//	ext, ok := Get(cr.Language)
//	if !ok { … continue }                            // (2)
//
// (2) is the one the classifier change cannot satisfy on its own and is the
// reason #6952 routes to "scala" rather than to a routes-specific token: a
// token with no registry entry passes (1) and dies at (2), silently, with the
// file's entities already evicted by Step 5.
//
// WHAT THIS TEST DOES NOT CLAIM. `incremental.go` never calls
// RunCustomExtractors — no incremental re-extraction has ever dispatched ANY of
// the ~340 custom/framework extractors, so the Play routes producer itself does
// not run on this path. That is pre-existing, universal to every custom
// extractor, and the same shape as cmd/grafel's default-off
// GRAFEL_INPROC_CUSTOM_EXTRACTORS gate; it is not something #6952 introduced or
// can fix from the classifier. What is asserted here is the part #6952 owns:
// the routes file reaches the re-extract loop's extractor dispatch instead of
// being dropped by it.
package extractors

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/classifier"
)

func TestRoutesFilesSurviveTheIncrementalReExtractGates6952(t *testing.T) {
	cls := classifier.New(nil)
	ctx := context.Background()

	for _, rel := range []string{
		"conf/routes",
		"conf/admin.routes",
		"main/resources/routes",
	} {
		// Gate (1) — incremental.go's classify step. Spelled with
		// ClassifyWithSize and a real byte count because that is the call the
		// loop makes; Classify is a different function and #6952's first cut
		// could have fixed only one of them.
		cr := cls.ClassifyWithSize(ctx, rel, 512)
		if cr.Skip || cr.Language == "" {
			t.Fatalf("incremental gate 1: ClassifyWithSize(%q) = {lang=%q skip=%v reason=%s} — "+
				"the re-extract loop `continue`s here, so an edit to this file would evict its "+
				"entities in Step 5 and re-add nothing", rel, cr.Language, cr.Skip, cr.SkipReason)
		}

		// Gate (2) — the registry lookup. This is what makes "route it to a
		// language that HAS an extractor" a requirement rather than a
		// preference.
		if _, ok := Get(cr.Language); !ok {
			t.Fatalf("incremental gate 2: Get(%q) = !ok for %q — the classifier names a language "+
				"no extractor is registered for, so the re-extract loop drops the file after "+
				"Step 5 has already evicted it", cr.Language, rel)
		}
	}

	// Negative control. Without it every assertion above is satisfiable by a
	// Get() that returns ok for everything and a classifier that never skips.
	if cr := cls.ClassifyWithSize(ctx, "conf/routes.bak", 512); !cr.Skip {
		t.Errorf("premise: ClassifyWithSize(conf/routes.bak) = {lang=%q skip=false} — a backup "+
			"must still be dropped at gate 1, or gate 1 is not gating", cr.Language)
	}
	if _, ok := Get("no_such_language_6952"); ok {
		t.Error("premise: Get(\"no_such_language_6952\") = ok — the registry answers for an " +
			"unregistered key, so the gate-2 assertions above are vacuous")
	}
}
