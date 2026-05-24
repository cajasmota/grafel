package main

import (
	"testing"
	"time"

	daemonmcp "github.com/cajasmota/archigraph/internal/daemon/mcp"
)

func TestColdLoadTiming(t *testing.T) {
	cases := []struct {
		name   string
		fbPath string
		limit  time.Duration
	}{
		{
			name:   "archigraph-main-52MB",
			fbPath: "/Users/jorgecajas/.archigraph/store/archigraph-a39d5fe7c256a9b7/refs/main/graph.fb",
			limit:  300 * time.Millisecond,
		},
		{
			name:   "archigraph-feat-ph2-108MB",
			fbPath: "/Users/jorgecajas/.archigraph/store/archigraph-a39d5fe7c256a9b7/refs/feat%2Fph2-tiered-hibernation/graph.fb",
			limit:  500 * time.Millisecond,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cache := daemonmcp.NewCache(1)
			start := time.Now()
			r, release, err := cache.Get(tc.fbPath)
			elapsed := time.Since(start)
			if err != nil {
				t.Skipf("graph.fb not found (skip on CI): %v", err)
			}
			release()
			_ = r
			t.Logf("cold-load elapsed: %s (limit %s)", elapsed, tc.limit)
			if elapsed > tc.limit {
				t.Errorf("cold-load too slow: %s > %s", elapsed, tc.limit)
			}
		})
	}
}
