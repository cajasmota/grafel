package daemon

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// newDrainTestService builds a bare Service wired with an injected MCP tool
// dispatcher, with no socket/scheduler — enough to exercise the graceful-drain
// gating on the MCP RPC methods (#5633).
func newDrainTestService(call MCPCallToolFunc) *Service {
	s := newService(nil, nil, nil, "", make(chan struct{}), nil, 1)
	s.mcpCallTool = call
	s.mcpListTools = func(string) ([]MCPToolEntry, error) { return nil, nil }
	return s
}

// TestMCPDrain_InFlightCompletes asserts that a tool call already executing
// when graceful shutdown begins is allowed to finish (drain) rather than being
// rejected, and that waitDrain blocks until it returns (#5633 part 1).
func TestMCPDrain_InFlightCompletes(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var finished bool

	svc := newDrainTestService(func(name string, _ map[string]any, _ string) (MCPCallResult, error) {
		close(started)
		<-release // block until the test lets the in-flight call complete
		finished = true
		return MCPCallResult{Content: []map[string]any{{"type": "text", "text": "ok"}}}, nil
	})

	callErrCh := make(chan error, 1)
	go func() {
		var reply MCPToolCallReply
		callErrCh <- svc.MCPToolCall(&MCPToolCallArgs{Name: "grafel_find"}, &reply)
	}()

	<-started // the call is now in-flight inside the dispatcher

	// Begin shutdown and start draining. waitDrain must NOT return while the
	// in-flight call is still blocked.
	svc.beginDrain()
	drainResult := make(chan bool, 1)
	go func() { drainResult <- svc.waitDrain(2 * time.Second) }()

	select {
	case <-drainResult:
		t.Fatal("waitDrain returned before the in-flight call finished — drain did not wait")
	case <-time.After(100 * time.Millisecond):
		// Good: drain is still waiting.
	}

	// Let the in-flight call finish; drain must now complete cleanly.
	close(release)
	if drained := <-drainResult; !drained {
		t.Fatal("waitDrain timed out even though the in-flight call finished")
	}
	if err := <-callErrCh; err != nil {
		t.Fatalf("in-flight call returned an error during drain: %v", err)
	}
	if !finished {
		t.Fatal("in-flight call did not run to completion")
	}
}

// TestMCPDrain_NewCallRejectedRetryable asserts that a tool call arriving AFTER
// draining begins is rejected with the retryable sentinel (the bridge
// reconnects to the replacement daemon) rather than being served (#5633).
func TestMCPDrain_NewCallRejectedRetryable(t *testing.T) {
	var served bool
	svc := newDrainTestService(func(string, map[string]any, string) (MCPCallResult, error) {
		served = true
		return MCPCallResult{}, nil
	})

	svc.beginDrain()

	var reply MCPToolCallReply
	err := svc.MCPToolCall(&MCPToolCallArgs{Name: "grafel_find"}, &reply)
	if err == nil {
		t.Fatal("expected a retryable error when draining, got nil")
	}
	if !strings.Contains(err.Error(), ErrDaemonDrainingMsg) {
		t.Fatalf("error %q does not carry the retryable drain sentinel %q", err, ErrDaemonDrainingMsg)
	}
	if served {
		t.Fatal("dispatcher was invoked for a call that arrived after draining began")
	}
}

// TestMCPDrain_ToolListRejectedWhenDraining asserts MCPToolList is gated too.
func TestMCPDrain_ToolListRejectedWhenDraining(t *testing.T) {
	svc := newDrainTestService(nil)
	svc.beginDrain()

	var reply MCPToolListReply
	err := svc.MCPToolList(&MCPToolListArgs{}, &reply)
	if err == nil || !strings.Contains(err.Error(), ErrDaemonDrainingMsg) {
		t.Fatalf("MCPToolList while draining: got err=%v, want retryable sentinel", err)
	}
}

// TestMCPDrain_WaitDrainTimesOut asserts the bound: if an in-flight call never
// returns, waitDrain reports a timeout rather than blocking forever.
func TestMCPDrain_WaitDrainTimesOut(t *testing.T) {
	hang := make(chan struct{})
	defer close(hang)
	started := make(chan struct{})

	svc := newDrainTestService(func(string, map[string]any, string) (MCPCallResult, error) {
		close(started)
		<-hang
		return MCPCallResult{}, nil
	})

	go func() {
		var reply MCPToolCallReply
		_ = svc.MCPToolCall(&MCPToolCallArgs{Name: "grafel_find"}, &reply)
	}()
	<-started

	svc.beginDrain()
	if svc.waitDrain(150 * time.Millisecond) {
		t.Fatal("waitDrain reported a clean drain even though a call is still hung")
	}
}

// TestMCPDrain_NoDrainAllowsCalls is the control: without beginDrain, calls run
// normally and the drain WaitGroup balances (enter/leave) so a later drain is
// immediate.
func TestMCPDrain_NoDrainAllowsCalls(t *testing.T) {
	var wg sync.WaitGroup
	svc := newDrainTestService(func(string, map[string]any, string) (MCPCallResult, error) {
		return MCPCallResult{Content: []map[string]any{{"type": "text", "text": "ok"}}}, nil
	})

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var reply MCPToolCallReply
			if err := svc.MCPToolCall(&MCPToolCallArgs{Name: "grafel_find"}, &reply); err != nil {
				t.Errorf("normal call errored: %v", err)
			}
		}()
	}
	wg.Wait()

	svc.beginDrain()
	if !svc.waitDrain(time.Second) {
		t.Fatal("drain did not complete immediately after all calls returned — WaitGroup imbalance")
	}
}
