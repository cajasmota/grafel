// Package client dials the archigraph daemon over a Unix-domain socket
// and exposes typed wrappers around each RPC method declared in the
// proto package.
//
// All public functions return ErrDaemonNotRunning when the socket is
// missing or unconnectable. Callers print the canonical message from
// ADR-0017:
//
//	daemon not running; run 'archigraph start' or reinstall via 'archigraph install'
//
// We do not embed that text inside this package because cmd output is
// the CLI's responsibility — the client just reports the condition.
package client

import (
	"errors"
	"fmt"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/daemon/proto"
)

// ErrDaemonNotRunning indicates the daemon socket could not be reached.
// Callers use errors.Is to recognise this condition (it wraps the
// underlying dial error so verbose diagnostics are still available).
var ErrDaemonNotRunning = errors.New("daemon not running")

// Client is a connected RPC client. Construct it with Dial; release it
// with Close.
type Client struct {
	rpc        *rpc.Client
	socketPath string
}

// Dial connects to the daemon at the default socket path. The default
// dial deadline is 2 seconds — generous for a local UDS but tight
// enough to fail fast when the daemon is down.
func Dial() (*Client, error) {
	layout, err := daemon.DefaultLayout()
	if err != nil {
		return nil, err
	}
	return DialPath(layout.SocketPath)
}

// DialPath is the testing entrypoint — connects to an arbitrary socket.
func DialPath(socketPath string) (*Client, error) {
	if _, err := os.Stat(socketPath); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s missing", ErrDaemonNotRunning, socketPath)
		}
		return nil, fmt.Errorf("stat %s: %w", socketPath, err)
	}
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDaemonNotRunning, err)
	}
	return &Client{
		rpc:        jsonrpc.NewClient(conn),
		socketPath: socketPath,
	}, nil
}

// Close releases the underlying RPC connection.
func (c *Client) Close() error {
	if c == nil || c.rpc == nil {
		return nil
	}
	return c.rpc.Close()
}

// Ping returns the daemon's reported version. Used by `archigraph status`
// as a liveness probe before calling Status (which is allowed to fail
// in informative ways).
func (c *Client) Ping() (proto.PingReply, error) {
	var reply proto.PingReply
	if err := c.rpc.Call(proto.ServiceName+".Ping", proto.PingArgs{}, &reply); err != nil {
		return proto.PingReply{}, err
	}
	return reply, nil
}

// Status reads daemon runtime state. Never returns ErrDaemonNotRunning —
// if the daemon was reachable for the Dial, the Status call should also
// succeed; transport errors here are real and bubble up.
func (c *Client) Status() (proto.StatusReply, error) {
	var reply proto.StatusReply
	if err := c.rpc.Call(proto.ServiceName+".Status", proto.StatusArgs{}, &reply); err != nil {
		return proto.StatusReply{}, err
	}
	return reply, nil
}

// Index forwards an index request to the daemon. Synchronous — blocks
// until the daemon completes the index. Phase B will introduce an
// async variant for fs-driven jobs.
func (c *Client) Index(args proto.IndexArgs) (proto.IndexReply, error) {
	var reply proto.IndexReply
	if err := c.rpc.Call(proto.ServiceName+".Index", args, &reply); err != nil {
		return proto.IndexReply{}, err
	}
	return reply, nil
}

// Rebuild forwards a force-rebuild request to the daemon.
func (c *Client) Rebuild(args proto.RebuildArgs) (proto.RebuildReply, error) {
	var reply proto.RebuildReply
	if err := c.rpc.Call(proto.ServiceName+".Rebuild", args, &reply); err != nil {
		return proto.RebuildReply{}, err
	}
	return reply, nil
}

// Stop asks the daemon to shut down. The call returns as soon as the
// daemon accepts the request; the daemon then drains its in-flight
// work before exiting.
func (c *Client) Stop() error {
	var reply proto.StopReply
	return c.rpc.Call(proto.ServiceName+".Stop", proto.StopArgs{}, &reply)
}

// QualityAudit forwards an audit-orphans request to the daemon. The
// daemon runs internal/quality/audit and returns a pre-formatted report
// alongside the scalar summary fields so the CLI can write output
// without importing the audit package.
func (c *Client) QualityAudit(args proto.QualityAuditRequest) (proto.QualityAuditReply, error) {
	var reply proto.QualityAuditReply
	if err := c.rpc.Call(proto.ServiceName+".QualityAudit", args, &reply); err != nil {
		return proto.QualityAuditReply{}, err
	}
	return reply, nil
}
