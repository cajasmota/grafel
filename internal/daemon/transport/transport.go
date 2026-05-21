// Package transport provides platform-specific IPC transport for the
// archigraph daemon. On Unix systems (Linux, macOS) it uses Unix-domain
// sockets; on Windows it uses named pipes via github.com/Microsoft/go-winio.
//
// The package exposes a single abstraction — Transport — that maps cleanly
// onto net.Listener (server side) and net.Conn (client side) so the rest
// of the daemon can stay platform-agnostic.
//
// # Stats
//
// Every connection returned by Listen or Dial is wrapped in a statsConn
// that counts bytes read and written. Cumulative per-connection totals are
// accessible via ConnStats.
//
// # Reconnect
//
// Dial supports automatic reconnection via DialWithRetry. The caller
// supplies a RetryPolicy that controls the number of attempts, the base
// backoff, and the maximum backoff. Retries are bounded so the function
// always returns in finite time.
//
// # Connection pool
//
// Pool manages a fixed-size pool of idle connections to the daemon.
// Callers call Pool.Get to borrow a connection and Pool.Put to return it.
// Connections are closed when the pool is full or when a returned
// connection has already been closed.
package transport

import (
	"net"
	"time"
)

// DefaultDialTimeout is used by Dial and DialWithRetry when no explicit
// deadline is provided.
const DefaultDialTimeout = 2 * time.Second

// ConnStats holds byte-transfer counters for a single connection. The
// counters are monotonically increasing and safe to read from any goroutine
// after the connection is closed (or at any time — reads are atomic via
// the statsConn implementation).
type ConnStats struct {
	// BytesRead is the total number of bytes consumed from the remote peer.
	BytesRead int64
	// BytesWritten is the total number of bytes sent to the remote peer.
	BytesWritten int64
}

// RetryPolicy governs automatic reconnect behaviour in DialWithRetry.
type RetryPolicy struct {
	// MaxAttempts is the maximum number of dial attempts including the
	// first one. Zero or negative means try once with no retries.
	MaxAttempts int

	// BaseDelay is the initial wait between attempts. Subsequent delays
	// are doubled (exponential back-off) up to MaxDelay.
	BaseDelay time.Duration

	// MaxDelay caps the per-attempt sleep. When zero, no cap is applied.
	MaxDelay time.Duration
}

// DefaultRetryPolicy is a sensible reconnect policy for interactive CLI
// usage: 5 attempts, 50 ms base back-off, 1 s ceiling.
var DefaultRetryPolicy = RetryPolicy{
	MaxAttempts: 5,
	BaseDelay:   50 * time.Millisecond,
	MaxDelay:    time.Second,
}

// Listen creates a platform-appropriate listener on addr.
//
//   - Unix/macOS:  addr is a filesystem path (e.g. ~/.archigraph/sockets/daemon.sock)
//   - Windows:     addr is a named-pipe path (e.g. \\.\pipe\archigraph-daemon-<user>)
//
// The returned net.Listener yields net.Conn values whose byte-transfer
// stats are tracked internally. Use ConnStatsFrom to retrieve them.
func Listen(addr string) (net.Listener, error) {
	return listen(addr)
}

// Dial opens a single connection to the daemon at addr with DefaultDialTimeout.
// Returns a net.Conn whose byte-transfer stats are tracked via ConnStatsFrom.
func Dial(addr string) (net.Conn, error) {
	return DialTimeout(addr, DefaultDialTimeout)
}

// DialTimeout opens a single connection to the daemon at addr with the
// given timeout.
func DialTimeout(addr string, timeout time.Duration) (net.Conn, error) {
	return dialTimeout(addr, timeout)
}

// DialWithRetry opens a connection, retrying on failure according to
// policy. The last error is returned when all attempts are exhausted.
func DialWithRetry(addr string, timeout time.Duration, policy RetryPolicy) (net.Conn, error) {
	attempts := policy.MaxAttempts
	if attempts <= 0 {
		attempts = 1
	}
	delay := policy.BaseDelay
	var lastErr error
	for i := 0; i < attempts; i++ {
		conn, err := dialTimeout(addr, timeout)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if i < attempts-1 {
			time.Sleep(delay)
			delay *= 2
			if policy.MaxDelay > 0 && delay > policy.MaxDelay {
				delay = policy.MaxDelay
			}
		}
	}
	return nil, lastErr
}

// ConnStatsFrom returns the byte-transfer stats for a connection returned
// by Listen or Dial. Returns zero-value ConnStats if c is not a statsConn
// (e.g. a plain net.Conn from tests).
func ConnStatsFrom(c net.Conn) ConnStats {
	if sc, ok := c.(*statsConn); ok {
		return ConnStats{
			BytesRead:    sc.bytesRead.Load(),
			BytesWritten: sc.bytesWritten.Load(),
		}
	}
	return ConnStats{}
}
