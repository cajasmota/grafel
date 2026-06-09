//go:build windows

package transport_test

import (
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon/transport"
)

// TestDialTimeout_FailsFastWhenNoPipe is the regression test for issue #4304:
// dialing a non-existent named pipe must return an error well within the
// supplied timeout instead of hanging (which previously tripped the 15-minute
// go test timeout on Windows CI via TestDelete_UnknownGroupReturnsClearError).
func TestDialTimeout_FailsFastWhenNoPipe(t *testing.T) {
	const addr = `\\.\pipe\archigraph-test-nonexistent-4304`
	const timeout = time.Second

	start := time.Now()
	conn, err := transport.DialTimeout(addr, timeout)
	elapsed := time.Since(start)

	if err == nil {
		conn.Close()
		t.Fatalf("expected error dialing non-existent pipe %s", addr)
	}
	// Allow generous slack over the 1 s bound for slow CI, but it must be far
	// below the 15-minute package timeout — a few seconds proves "fails fast".
	if elapsed > 5*time.Second {
		t.Fatalf("dial took %s; expected fast failure (~%s)", elapsed, timeout)
	}
}

// TestDialTimeout_ClampsNonPositiveTimeout verifies a zero timeout does not
// hang and does not silently inherit go-winio's 2 s default in an unbounded
// way — it must still fail fast against a missing pipe.
func TestDialTimeout_ClampsNonPositiveTimeout(t *testing.T) {
	const addr = `\\.\pipe\archigraph-test-nonexistent-4304-zero`

	start := time.Now()
	conn, err := transport.DialTimeout(addr, 0)
	elapsed := time.Since(start)

	if err == nil {
		conn.Close()
		t.Fatalf("expected error dialing non-existent pipe %s", addr)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("dial took %s; expected fast failure", elapsed)
	}
}
