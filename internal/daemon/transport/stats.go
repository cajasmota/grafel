package transport

import (
	"net"
	"sync/atomic"
)

// statsConn wraps a net.Conn and tracks bytes read and written using
// lock-free atomic counters. Both counters are safe to read from any
// goroutine at any time.
type statsConn struct {
	net.Conn
	bytesRead    atomic.Int64
	bytesWritten atomic.Int64
}

func newStatsConn(c net.Conn) *statsConn {
	return &statsConn{Conn: c}
}

// Read delegates to the underlying connection and adds the byte count.
func (s *statsConn) Read(p []byte) (int, error) {
	n, err := s.Conn.Read(p)
	if n > 0 {
		s.bytesRead.Add(int64(n))
	}
	return n, err
}

// Write delegates to the underlying connection and adds the byte count.
func (s *statsConn) Write(p []byte) (int, error) {
	n, err := s.Conn.Write(p)
	if n > 0 {
		s.bytesWritten.Add(int64(n))
	}
	return n, err
}
