package mcp

// warming_5690_test.go — issue #5690.
//
// Verifies that the MCP surface reports a real warming/readiness signal
// sourced from the daemon scheduler. During post-index enrichment (algo /
// links passes still pending) queries are slow; without this signal agents
// cannot distinguish "warming" from "slow query".
//
// The scheduler handle is injected as a read-only closure via
// State.SetWarmingSnapshot. When no handle is injected the surface must
// degrade gracefully to warming=false with the prior fields unchanged.

import (
	"context"
	"path/filepath"
	"testing"

	mcpapi "github.com/mark3labs/mcp-go/mcp"

	"github.com/cajasmota/grafel/internal/daemon"
)

func newWarmingTestServer(t *testing.T) *Server {
	t.Helper()
	tmp := t.TempDir()
	setTestHome(t, tmp)
	t.Setenv("GRAFEL_WHOAMI_NUDGE", "quiet")

	repoDir := filepath.Join(tmp, "repo-a")
	writeGraph(t, repoDir, fixtureDoc("repo-a"))

	regPath := makeRegistry(t, tmp, map[string]map[string]string{
		"g": {"repo-a": repoDir},
	})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

func whoamiJSON(t *testing.T, srv *Server) map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "g"}
	res, err := srv.handleWhoami(context.Background(), req)
	if err != nil {
		t.Fatalf("handleWhoami: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.Content)
	}
	return extractResultJSON(t, res)
}

// TestWhoami_warming_true asserts that an injected warming snapshot
// (IndexInFlight=true, PendingAlgo=N) surfaces warming=true, the pending
// counts, and a "warming" tier.
func TestWhoami_warming_true(t *testing.T) {
	srv := newWarmingTestServer(t)
	srv.State.SetWarmingSnapshot(func() daemon.WarmingSnapshot {
		return daemon.WarmingSnapshot{
			IndexInFlight: true,
			PendingAlgo:   3,
			PendingLinks:  1,
		}
	})

	out := whoamiJSON(t, srv)

	if got := out["warming"]; got != true {
		t.Errorf("warming: got %v (%T), want true", got, got)
	}
	if got := out["indexing"]; got != true {
		t.Errorf("indexing: got %v (%T), want true", got, got)
	}
	if got := out["pending_algo"]; got != float64(3) {
		t.Errorf("pending_algo: got %v (%T), want 3", got, got)
	}
	if got := out["pending_links"]; got != float64(1) {
		t.Errorf("pending_links: got %v (%T), want 1", got, got)
	}
	if got := out["tier"]; got != "warming" {
		t.Errorf("tier: got %v, want warming", got)
	}
}

// TestWhoami_warming_pendingAlgoOnly asserts warming is true when only a
// pending enrichment pass remains (no index in flight) — enrichment is the
// slow phase the signal exists to disambiguate.
func TestWhoami_warming_pendingAlgoOnly(t *testing.T) {
	srv := newWarmingTestServer(t)
	srv.State.SetWarmingSnapshot(func() daemon.WarmingSnapshot {
		return daemon.WarmingSnapshot{PendingAlgo: 2}
	})

	out := whoamiJSON(t, srv)
	if got := out["warming"]; got != true {
		t.Errorf("warming: got %v, want true", got)
	}
	if got := out["indexing"]; got != false {
		t.Errorf("indexing: got %v, want false", got)
	}
	if got := out["tier"]; got != "warming" {
		t.Errorf("tier: got %v, want warming", got)
	}
}

// TestWhoami_notWarming_noSnapshot asserts graceful default: with no
// injected handle warming=false, tier keeps its prior value ("hot"), and the
// pending counts are zero.
func TestWhoami_notWarming_noSnapshot(t *testing.T) {
	srv := newWarmingTestServer(t)

	out := whoamiJSON(t, srv)
	if got := out["warming"]; got != false {
		t.Errorf("warming: got %v, want false", got)
	}
	if got := out["indexing"]; got != false {
		t.Errorf("indexing: got %v, want false", got)
	}
	if got := out["pending_algo"]; got != float64(0) {
		t.Errorf("pending_algo: got %v, want 0", got)
	}
	if got := out["tier"]; got != "hot" {
		t.Errorf("tier: got %v, want hot (unchanged prior value)", got)
	}
}

// TestWhoami_notWarming_idleSnapshot asserts warming=false when a handle is
// injected but the scheduler is idle (no in-flight index, no pending passes).
func TestWhoami_notWarming_idleSnapshot(t *testing.T) {
	srv := newWarmingTestServer(t)
	srv.State.SetWarmingSnapshot(func() daemon.WarmingSnapshot {
		return daemon.WarmingSnapshot{}
	})

	out := whoamiJSON(t, srv)
	if got := out["warming"]; got != false {
		t.Errorf("warming: got %v, want false", got)
	}
	if got := out["tier"]; got != "hot" {
		t.Errorf("tier: got %v, want hot", got)
	}
}
