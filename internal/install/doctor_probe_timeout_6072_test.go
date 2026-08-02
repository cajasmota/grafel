package install

import (
	"net"
	"runtime"
	"testing"
	"time"
)

// wedgedSocket starts a Unix listener that ACCEPTS connections and then never
// writes a byte — the wedged-daemon state. This is not the same as a daemon
// that is down: a missing socket fails instantly at os.Stat, and a refused
// connection fails instantly at dial. A wedged daemon completes the dial and
// then blocks the caller forever on the read, and it is precisely the state
// someone runs `grafel doctor` to diagnose.
func wedgedSocket(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix-socket wedged-daemon stub; the Windows named-pipe equivalent is not modelled here")
	}
	sock := shortSocketPath(t, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		// Fatal, never Skip — see shortSocketPath.
		t.Fatalf("bind unix socket %s: %v", sock, err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			// Hold the connection open and answer nothing.
			go func(c net.Conn) {
				<-done
				_ = c.Close()
			}(conn)
		}
	}()
	return sock
}

// H-1 (issue #6072 review): moving the version check from HTTP to the RPC
// socket dropped the only timeout on it. dclient.DialPath bounds the DIAL at
// 2s, but Ping() sets no deadline and net/rpc/jsonrpc sets none either — there
// is no SetDeadline anywhere on that path (internal/daemon/pidfile.go:51 is the
// codebase's own precedent for adding one). The old HTTP probe was hard-capped
// by DoctorOptions.DaemonTimeout; the RPC probe was capped by nothing, so
// `grafel doctor` hung forever against a wedged daemon.
//
// MUTATION ORACLE: call the underlying dial+Ping directly instead of routing
// through probeDaemonVersionWithin → this test fails on its 8s watchdog.
func TestDaemonVersionProbe_TimesOutAgainstAWedgedDaemon(t *testing.T) {
	sock := wedgedSocket(t)

	const budget = 300 * time.Millisecond
	start := time.Now()

	returned := make(chan error, 1)
	go func() {
		_, err := probeDaemonVersionWithin(budget, sock)
		returned <- err
	}()

	select {
	case err := <-returned:
		if err == nil {
			t.Fatal("probe against a wedged daemon returned success")
		}
		if elapsed := time.Since(start); elapsed > 4*time.Second {
			t.Fatalf("probe took %s to give up on a %s budget", elapsed, budget)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("probe did not return after 8s against a wedged daemon — grafel doctor hangs forever")
	}
}

// checkDaemon must surface the wedged daemon as an unreachable-class failure
// rather than hanging or reporting OK.
func TestCheckDaemon_WedgedDaemonIsCriticalNotAHang(t *testing.T) {
	sock := wedgedSocket(t)
	probe := func() (ProbedVersion, error) { return probeDaemonVersionWithin(300*time.Millisecond, sock) }

	done := make(chan CheckResult, 1)
	go func() { done <- checkDaemon(&State{DaemonVersion: "v0.2.1"}, probe) }()

	select {
	case cr := <-done:
		if cr.OK {
			t.Fatal("wedged daemon reported OK")
		}
		if cr.Severity != SeverityCritical {
			t.Fatalf("severity = %v, want critical", cr.Severity)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("checkDaemon did not return after 8s against a wedged daemon")
	}
}

// The bounded probe must not turn a HEALTHY daemon's normal latency into a
// spurious timeout, and must still reject an implausible (HTML) version.
func TestDaemonVersionProbe_BoundedProbeStillReadsAHealthyDaemon(t *testing.T) {
	sock, want := pingStubSocket(t, "v0.2.1 (commit abc, built 2026-08-01T00:00:00Z)", "v0.2.1")

	got, err := probeDaemonVersionWithin(5*time.Second, sock)
	if err != nil {
		t.Fatalf("probe against a healthy daemon: %v", err)
	}
	if got.Display != want.Display {
		t.Fatalf("Display = %q, want %q", got.Display, want.Display)
	}
	if got.Bare != want.Bare {
		t.Fatalf("Bare = %q, want %q", got.Bare, want.Bare)
	}
}

// A daemon answering with the SPA's HTML must be rejected by the probe itself,
// not just by checkDaemon's backstop.
func TestDaemonVersionProbe_RejectsImplausibleVersion(t *testing.T) {
	sock, _ := pingStubSocket(t, spaIndexHTML, "")

	if _, err := probeDaemonVersionWithin(5*time.Second, sock); err == nil {
		t.Fatal("probe accepted an HTML document as a version")
	}
}
