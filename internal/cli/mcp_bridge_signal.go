package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// mcp_bridge_signal.go — make a terminated bridge name its own death (#6999).
//
// #6999 took a full excavation because the death was silent on BOTH sides: the
// bridge had no SIGTERM handler (exit 143 is the default action, so nothing was
// logged and no drain ran), and the killer's own line went to the CLIENT's
// stderr, which nobody keeps. From here on a signalled bridge writes one line
// naming the signal, its pidfile, its socket and where to look — to its stderr
// AND to ~/.grafel/logs/daemon.log, which survives the session.

// bridgeSignalExitCode maps a terminating signal to the shell convention
// (128+signum) so the exit status a supervisor sees is unchanged by the fact
// that we now handle the signal rather than dying by default action.
func bridgeSignalExitCode(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return 128 + int(s)
	}
	return 143
}

// bridgeTerminationNotice is the line written when the bridge is terminated by
// a signal. It is a pure function so its content is assertable.
func bridgeTerminationNotice(sig os.Signal, pidfile, socketPath string) string {
	return fmt.Sprintf(
		"mcp-bridge (pid %d) terminating on signal %v: the client's stdin was still open, "+
			"so this was an external termination, not an end-of-session. "+
			"pidfile=%s socket=%s. grafel itself never signals another bridge (#6999); "+
			"before v0.3.3 a newly started bridge reaped this one. "+
			"If you are on an older build, that is the first thing to check.",
		os.Getpid(), sig, pidfile, socketPath)
}

// handleBridgeSignal writes the termination notice to the bridge's own logger
// and to the daemon log, then exits with the conventional status. exit is a
// parameter so the behaviour is testable without killing the test binary.
func handleBridgeSignal(sig os.Signal, pidfile, socketPath string, logf func(string, ...any), exit func(int)) {
	notice := bridgeTerminationNotice(sig, pidfile, socketPath)
	if logf != nil {
		logf("%s", notice)
	}
	appendDaemonLog(notice)
	exit(bridgeSignalExitCode(sig))
}

// installBridgeSignalLogger arms the SIGTERM/SIGINT handler for the lifetime of
// a bridge session. The returned stop function disarms it.
func installBridgeSignalLogger(pidfile, socketPath string, logf func(string, ...any)) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	done := make(chan struct{})
	go func() {
		select {
		case sig := <-ch:
			handleBridgeSignal(sig, pidfile, socketPath, logf, os.Exit)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

// appendDaemonLog appends one timestamped line to the daemon log so a bridge
// death is recorded somewhere that outlives the client's stderr. Best-effort:
// a missing directory or an unwritable log must never affect the bridge.
func appendDaemonLog(line string) {
	path := daemonLogPath()
	if path == "" || path == "." {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "%s grafel-mcp-bridge: %s\n",
		time.Now().Format("2006/01/02 15:04:05"), line)
}
