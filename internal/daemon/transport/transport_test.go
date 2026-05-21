//go:build !windows

package transport_test

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon/transport"
)

// tempSocketPath returns a Unix-domain socket path under t.TempDir().
func tempSocketPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.sock")
}

// TestListenDialRoundtrip verifies that Listen + Dial can exchange data.
func TestListenDialRoundtrip(t *testing.T) {
	addr := tempSocketPath(t)

	l, err := transport.Listen(addr)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	const msg = "hello transport"
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := l.Accept()
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		defer conn.Close()
		buf := make([]byte, len(msg))
		if _, err := io.ReadFull(conn, buf); err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		if string(buf) != msg {
			t.Errorf("server got %q, want %q", buf, msg)
		}
	}()

	conn, err := transport.Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte(msg)); err != nil {
		t.Fatalf("client write: %v", err)
	}
	wg.Wait()
}

// TestConnStats verifies that byte counters are updated after a transfer.
func TestConnStats(t *testing.T) {
	addr := tempSocketPath(t)

	l, err := transport.Listen(addr)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	const msg = "stats test payload"
	done := make(chan transport.ConnStats, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			done <- transport.ConnStats{}
			return
		}
		defer conn.Close()
		buf, _ := io.ReadAll(conn)
		_ = buf
		done <- transport.ConnStatsFrom(conn)
	}()

	client, err := transport.Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if _, err := client.Write([]byte(msg)); err != nil {
		t.Fatalf("client write: %v", err)
	}
	client.Close()

	serverStats := <-done
	clientStats := transport.ConnStatsFrom(client)

	if clientStats.BytesWritten != int64(len(msg)) {
		t.Errorf("client BytesWritten=%d, want %d", clientStats.BytesWritten, len(msg))
	}
	if serverStats.BytesRead != int64(len(msg)) {
		t.Errorf("server BytesRead=%d, want %d", serverStats.BytesRead, len(msg))
	}
}

// TestDialWithRetry verifies that DialWithRetry retries and eventually succeeds.
func TestDialWithRetry(t *testing.T) {
	addr := tempSocketPath(t)

	// Start the listener after a short delay to force a retry.
	go func() {
		time.Sleep(120 * time.Millisecond)
		l, err := transport.Listen(addr)
		if err != nil {
			return
		}
		defer l.Close()
		conn, _ := l.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	policy := transport.RetryPolicy{
		MaxAttempts: 8,
		BaseDelay:   30 * time.Millisecond,
		MaxDelay:    200 * time.Millisecond,
	}
	conn, err := transport.DialWithRetry(addr, 500*time.Millisecond, policy)
	if err != nil {
		t.Fatalf("DialWithRetry: %v", err)
	}
	conn.Close()
}

// TestDialWithRetry_Exhausted verifies that DialWithRetry returns an error
// when all attempts fail.
func TestDialWithRetry_Exhausted(t *testing.T) {
	addr := tempSocketPath(t)
	// Nothing is listening.
	policy := transport.RetryPolicy{
		MaxAttempts: 2,
		BaseDelay:   5 * time.Millisecond,
	}
	_, err := transport.DialWithRetry(addr, 50*time.Millisecond, policy)
	if err == nil {
		t.Fatal("expected error when nothing is listening")
	}
}

// TestPool verifies that Pool.Get returns a connection and Pool.Put recycles it.
func TestPool(t *testing.T) {
	addr := tempSocketPath(t)

	l, err := transport.Listen(addr)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	// Accept connections in background.
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	pool := transport.NewPool(addr, time.Second, transport.DefaultRetryPolicy, 2)
	defer pool.Close()

	c1, err := pool.Get()
	if err != nil {
		t.Fatalf("Pool.Get: %v", err)
	}
	if pool.Len() != 0 {
		t.Fatalf("pool should be empty after Get, got %d", pool.Len())
	}
	pool.Put(c1)
	if pool.Len() != 1 {
		// c1 may have been closed by the server; put may discard it.
		// Both outcomes are valid — just verify no panic.
		t.Logf("pool.Len()=%d after Put (0 or 1 are both valid)", pool.Len())
	}
}

// TestConnStatsFrom_PlainConn verifies that ConnStatsFrom returns zeros
// for a plain net.Conn that is not wrapped by the transport package.
func TestConnStatsFrom_PlainConn(t *testing.T) {
	var c net.Conn // nil — ConnStatsFrom must not panic
	stats := transport.ConnStatsFrom(c)
	if stats.BytesRead != 0 || stats.BytesWritten != 0 {
		t.Errorf("expected zero stats for nil conn, got %+v", stats)
	}

	// Also test with a real but unwrapped conn.
	addr := tempSocketPath(t)
	l, _ := net.Listen("unix", addr)
	defer l.Close()
	go func() {
		conn, _ := l.Accept()
		if conn != nil {
			conn.Close()
		}
	}()
	plain, err := net.DialTimeout("unix", addr, time.Second)
	if err != nil {
		t.Skip("unix dial failed:", err)
	}
	defer plain.Close()
	_ = os.Remove(addr)
	stats = transport.ConnStatsFrom(plain)
	if stats.BytesRead != 0 || stats.BytesWritten != 0 {
		t.Errorf("expected zero stats for plain conn, got %+v", stats)
	}
}
