package install

import (
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// shortSocketPath returns a bindable Unix-socket path.
//
// t.TempDir() is unusable here: on macOS $TMPDIR is
// /var/folders/<hash>/<hash>/T/ and the test name is appended, which puts the
// path well past the 104-byte sun_path limit — bind fails with EINVAL. When
// that failure was a t.Skip, every socket test in this package silently
// skipped and the package still reported ok.
func shortSocketPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "gf")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

// pingStubService answers Daemon.Ping with a canned reply — a real net/rpc
// service over a real Unix socket, so the probe exercises the same
// DialPath+Ping path production uses rather than a hand-rolled fake.
type pingStubService struct{ reply proto.PingReply }

func (s *pingStubService) Ping(_ proto.PingArgs, out *proto.PingReply) error {
	*out = s.reply
	return nil
}

// pingStubSocket serves Daemon.Ping on a temporary Unix socket and returns its
// path plus the ProbedVersion a correct probe should extract from it.
func pingStubSocket(t *testing.T, display, bare string) (string, ProbedVersion) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix-socket ping stub; the Windows named-pipe equivalent is not modelled here")
	}
	sock := shortSocketPath(t, "p")

	srv := rpc.NewServer()
	if err := srv.RegisterName(proto.ServiceName, &pingStubService{
		reply: proto.PingReply{Version: display, VersionBare: bare},
	}); err != nil {
		t.Fatalf("register ping stub: %v", err)
	}

	ln, err := net.Listen("unix", sock)
	if err != nil {
		// NOT a skip. A skip here is how this whole file silently passed while
		// testing nothing: t.TempDir() under $TMPDIR blew past the 104-byte
		// sun_path limit, every bind failed, and the package reported ok.
		t.Fatalf("bind unix socket %s: %v", sock, err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			// ServeCodec on OUR server, not jsonrpc.ServeConn (which would use
			// the process-global default rpc.Server and leak registrations
			// between tests).
			go srv.ServeCodec(jsonrpc.NewServerCodec(conn))
		}
	}()

	return sock, ProbedVersion{Display: display, Bare: bare}
}
