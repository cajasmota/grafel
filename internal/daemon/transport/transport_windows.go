//go:build windows

package transport

import (
	"net"
	"os/user"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
)

// WindowsPipeName returns the canonical named-pipe path for the current user.
// Form: \\.\pipe\archigraph-daemon-<username>
// The username is lower-cased and stripped of any domain prefix so that
// "DOMAIN\User" → "user".
func WindowsPipeName() string {
	u, err := user.Current()
	username := "daemon"
	if err == nil {
		username = u.Username
		// Strip domain prefix (e.g. "DESKTOP-123\alice" → "alice").
		if idx := strings.LastIndex(username, "\\"); idx >= 0 {
			username = username[idx+1:]
		}
		username = strings.ToLower(username)
	}
	return `\\.\pipe\archigraph-daemon-` + username
}

// listen creates a Windows named-pipe listener at addr.
// addr should be a named-pipe path of the form \\.\pipe\<name>.
// The pipe is created with a security descriptor that restricts access to
// the current user only (SDDL "D:P(A;;GA;;;OW)"), matching the Unix 0600
// behaviour.
func listen(addr string) (net.Listener, error) {
	cfg := &winio.PipeConfig{
		// SDDL: discretionary ACL — allow Generic All for the object owner only.
		// This is the Windows equivalent of mode 0600 on a Unix socket.
		SecurityDescriptor: "D:P(A;;GA;;;OW)",
		MessageMode:        false,
		InputBufferSize:    65536,
		OutputBufferSize:   65536,
	}
	l, err := winio.ListenPipe(addr, cfg)
	if err != nil {
		return nil, err
	}
	return &statsListener{Listener: l}, nil
}

// dialTimeout dials a Windows named pipe at addr with the given timeout.
// Returns a stats-tracked connection.
func dialTimeout(addr string, timeout time.Duration) (net.Conn, error) {
	c, err := winio.DialPipe(addr, &timeout)
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
