package daemon_test

// Tests for #802 — rebuild progress tracking via IndexProgress RPC.

import (
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/daemon/client"
	"github.com/cajasmota/archigraph/internal/daemon/proto"
)

// slowRebuildFunc returns a RebuildFunc that sleeps for delay before
// returning the given repos list. This lets the test poll progress while
// the rebuild is "in flight".
func slowRebuildFunc(repos []string, delay time.Duration) daemon.RebuildFunc {
	return func(args proto.RebuildArgs) ([]string, string, error) {
		time.Sleep(delay)
		return repos, "", nil
	}
}

// TestIndexProgress_TokenNotFound verifies that polling an unknown token
// returns Done=true rather than an error (so the CLI doesn't loop forever
// if it polls after the session expired).
func TestIndexProgress_TokenNotFound(t *testing.T) {
	layout := runDaemonForTest(t, nil, slowRebuildFunc([]string{"/repo/a"}, 0))
	c, err := client.DialPath(layout.SocketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	reply, err := c.IndexProgress("nonexistent-token")
	if err != nil {
		t.Fatalf("IndexProgress error: %v", err)
	}
	if !reply.Done {
		t.Errorf("expected Done=true for unknown token, got false")
	}
}

// TestIndexProgress_EmptyToken verifies that an empty token returns an
// RPC error (not a panic).
func TestIndexProgress_EmptyToken(t *testing.T) {
	layout := runDaemonForTest(t, nil, slowRebuildFunc([]string{"/repo/a"}, 0))
	c, err := client.DialPath(layout.SocketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	_, err = c.IndexProgress("")
	if err == nil {
		t.Error("expected error for empty token, got nil")
	}
}

// TestIndexProgress_LivePolling verifies that a rebuild with a ProgressToken
// can be polled mid-flight and returns Done=true after the rebuild completes.
func TestIndexProgress_LivePolling(t *testing.T) {
	repos := []string{"/repo/alpha", "/repo/beta"}
	// 200ms delay gives the test enough time to poll once mid-flight.
	layout := runDaemonForTest(t, nil, slowRebuildFunc(repos, 200*time.Millisecond))

	// Primary connection: fires off the blocking Rebuild.
	primary, err := client.DialPath(layout.SocketPath)
	if err != nil {
		t.Fatalf("primary dial: %v", err)
	}
	defer primary.Close()

	// Poll connection: polls progress while primary is blocked.
	poll, err := client.DialPath(layout.SocketPath)
	if err != nil {
		t.Fatalf("poll dial: %v", err)
	}
	defer poll.Close()

	token := "test-token-live-" + t.Name()

	// Fire rebuild in background.
	type rebuildResult struct {
		reply proto.RebuildReply
		err   error
	}
	resultCh := make(chan rebuildResult, 1)
	go func() {
		reply, err := primary.Rebuild(proto.RebuildArgs{
			Group:         "test-group",
			ProgressToken: token,
		})
		resultCh <- rebuildResult{reply, err}
	}()

	// Allow the rebuild goroutine to start and register the session.
	time.Sleep(20 * time.Millisecond)

	// Poll for progress — at this point the rebuild is sleeping.
	prog, err := poll.IndexProgress(token)
	if err != nil {
		t.Fatalf("IndexProgress mid-flight: %v", err)
	}
	// The session is registered and active — Done must be false.
	if prog.Done {
		t.Error("expected Done=false mid-flight, got true")
	}
	// We should have at least one repo state visible (the started stub).
	if len(prog.Repos) == 0 {
		t.Error("expected at least one repo state mid-flight")
	}

	// Wait for rebuild to complete.
	res := <-resultCh
	if res.err != nil {
		t.Fatalf("rebuild error: %v", res.err)
	}

	// Final poll — Done must be true now.
	final, err := poll.IndexProgress(token)
	if err != nil {
		t.Fatalf("IndexProgress after completion: %v", err)
	}
	if !final.Done {
		t.Error("expected Done=true after rebuild, got false")
	}
	if len(final.Repos) == 0 {
		t.Error("expected at least one completed repo state after rebuild")
	}
}

// TestRebuildWithoutToken verifies that Rebuild without a ProgressToken
// still works correctly (backward-compatible path).
func TestRebuildWithoutToken(t *testing.T) {
	repos := []string{"/repo/x"}
	layout := runDaemonForTest(t, nil, slowRebuildFunc(repos, 0))
	c, err := client.DialPath(layout.SocketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	reply, err := c.Rebuild(proto.RebuildArgs{Group: "g"})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if len(reply.Repos) != 1 || reply.Repos[0] != repos[0] {
		t.Errorf("unexpected repos: %v", reply.Repos)
	}
}
