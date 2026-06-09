//go:build windows

package transport

import (
	"context"
	"net"
	"os/user"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
)

// maxDialTimeout caps the wall-clock time a single named-pipe dial may take
// before failing fast. The Unix path relies on net.DialTimeout(addr, timeout)
// which is strictly bounded by the supplied timeout; this is the Windows
// equivalent and exists so that a non-positive (zero/negative) timeout from a
// caller can never degrade into go-winio's silent 2 s default — or, worse, an
// unbounded ERROR_PIPE_BUSY retry loop — when no daemon is listening.
//
// All real callers pass an explicit positive timeout (2 s / 1 s / 200 ms — see
// internal/daemon/client and internal/daemon/service); this constant only
// guards the degenerate case so every CLI command fails fast when the daemon
// is down, matching the Unix ENOENT-fast-fail behaviour. See issue #4304.
const maxDialTimeout = 2 * time.Second

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

// dialTimeout dials a Windows named pipe at addr, bounded by timeout.
//
// This mirrors the Unix path's net.DialTimeout("unix", addr, timeout): the dial
// must fail fast (within timeout) when no daemon is listening rather than block.
// We drive go-winio via an explicit context deadline we fully own — instead of
// winio.DialPipe(addr, &timeout), whose nil/zero handling would silently fall
// back to a 2 s default — so the bound is always honoured.
//
// A non-positive timeout is clamped to maxDialTimeout so a degenerate caller
// can never produce an already-expired context that yields a confusing
// "context deadline exceeded" before a single connect attempt, nor an
// unbounded retry. See issue #4304.
func dialTimeout(addr string, timeout time.Duration) (net.Conn, error) {
	if timeout <= 0 {
		timeout = maxDialTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	c, err := winio.DialPipeContext(ctx, addr)
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
