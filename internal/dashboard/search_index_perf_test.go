//go:build perf

// Latency budgets for the dashboard entity search index.
//
// Gated behind the `perf` build tag (see CONTRIBUTING.md, "Performance tests").
// assertSearchFast asserts on wall clock ONLY — it logs the hit count but never
// checks it — so these three tests carry no correctness signal. The ranking and
// matching behaviour they might otherwise be thought to cover is asserted by
// TestSearchIndex_ExactMatch / _PrefixBeforeSubstring / _SubstringMatch /
// _LimitRespected / _ShortQuery in search_index_test.go, all of which stay in
// the release gate.
//
// The budgets were already bumped once (#2477, 500ms → 1000ms at 50k) to
// accommodate a macOS runner measured at 639ms p99. That is the shape of a
// measurement that does not belong in a shared-runner gate.
//
//	go test -tags perf ./internal/dashboard/ -run 'SearchIndex_Scale' -v

package dashboard

import (
	"testing"
	"time"
)

func TestSearchIndex_Scale_1k(t *testing.T) {
	grp := makeScaleGroup(1_000)
	assertSearchFast(t, grp, "alpha", 500*time.Millisecond)
	assertSearchFast(t, grp, "entity", 500*time.Millisecond)
	assertSearchFast(t, grp, "beta", 500*time.Millisecond)
}

func TestSearchIndex_Scale_10k(t *testing.T) {
	grp := makeScaleGroup(10_000)
	assertSearchFast(t, grp, "alpha", 500*time.Millisecond)
	assertSearchFast(t, grp, "entity", 500*time.Millisecond)
	assertSearchFast(t, grp, "service5", 500*time.Millisecond)
}

func TestSearchIndex_Scale_50k(t *testing.T) {
	grp := makeScaleGroup(50_000)
	assertSearchFast(t, grp, "alpha", 1000*time.Millisecond)
	assertSearchFast(t, grp, "entity", 1000*time.Millisecond)
	assertSearchFast(t, grp, "beta", 1000*time.Millisecond)
}
