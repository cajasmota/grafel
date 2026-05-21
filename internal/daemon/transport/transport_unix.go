//go:build !windows

package transport

import (
	"net"
	"time"
)

// listen creates a Unix-domain socket listener at path.
// The socket file is created by the OS; the daemon is responsible for
// removing any stale file before calling Listen (see server.go).
func listen(addr string) (net.Listener, error) {
	l, err := net.Listen("unix", addr)
	if err != nil {
		return nil, err
	}
	return &statsListener{Listener: l}, nil
}

// dialTimeout dials the Unix-domain socket at path with the given timeout.
// Returns a stats-tracked connection.
func dialTimeout(addr string, timeout time.Duration) (net.Conn, error) {
	c, err := net.DialTimeout("unix", addr, timeout)
	if err != nil {
		return nil, err
	}
	return newStatsConn(c), nil
}

// statsListener wraps net.Listener so that every accepted connection is
// wrapped in a statsConn.
type statsListener struct {
	net.Listener
}

func (sl *statsListener) Accept() (net.Conn, error) {
	c, err := sl.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return newStatsConn(c), nil
}
