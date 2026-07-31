//go:build windows

package transport

import (
	"context"
	"net"
	"os/user"
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

// WindowsPipeName returns the named-pipe path for the daemon rooted at
// root.
//
// Form: \\.\pipe\grafel-daemon-<username>-<rootHash>
//
// The pipe is scoped by BOTH the current username AND a hash of the daemon
// root directory. The username keeps two different users on the same box from
// colliding (each user can only open their own pipe via the SDDL ACL anyway).
// The root hash is the critical part: it makes the pipe name unique per daemon
// root, exactly mirroring the Unix transport, where the RPC socket lives at
// <root>/sockets/daemon.sock and is therefore already root-scoped.
//
// Without the root hash the Windows pipe was a single process-global object
// per user (`grafel-daemon-<user>`). An isolated daemon — the selftest, a
// parallel agent, or simply a second `grafel` instance with a different
// GRAFEL_DAEMON_ROOT — would collide on that one shared pipe: the isolated
// listener wedged at socket-listen while a DIFFERENT daemon answered probes,
// cascading into "MCP: no groups registered" / "cold index: graph.fb not
// found" / "persistence: daemon not running" (issue #5264). Scoping the pipe
// by root makes distinct roots truly independent, which both fixes the
// selftest wedge and lets two real daemons coexist on one Windows host.
//
// The same root MUST be passed on both the listen side (paths_windows.go's
// DefaultLayout) and the dial side (client, which also reads
// DefaultLayout().SocketPath), so the derived name is identical and the two
// sides connect. Production passes its default root (%APPDATA%\grafel or
// $GRAFEL_DAEMON_ROOT); its pipe name changes from the old global one, which
// is fine — it is still deterministic per root.
//
// An empty root degrades to the legacy user-only name so a degenerate caller
// still produces a valid, stable pipe path.
func WindowsPipeName(root string) string {
	u, err := user.Current()
	username := "daemon"
	if err == nil {
		username = u.Username
	}
	return buildWindowsPipeName(username, root)
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

// Close closes the named-pipe listener under a bounded budget.
//
// go-winio's win32PipeListener.Close can block forever when an Accept is
// outstanding and the aborted connect completes with an errno its
// classification misses — see closeBounded for the full mechanism and the CI
// evidence. Unix has no equivalent hazard (net.UnixListener.Close is already
// bounded), which is why this override lives only on the Windows side.
func (sl *statsListener) Close() error {
	return closeBounded(sl.Listener.Close)
}
