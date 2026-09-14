package daemon

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// decodeRecords parses a JSON-handler buffer into one map per log record.
func decodeRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

// #7050: the pidfile reclaim force-kills a process that, from the victim's
// side, dies with no trace at all — TerminateProcess/SIGKILL runs no defers
// and writes no log line. Before this test the reclaim was
// `_ = forceKillFunc(existing)` with no logging anywhere, so the ONLY record
// of the kill existed in the killer's head. A user investigating a daemon
// that vanished had nothing to find on either side.
//
// The killer must say, in daemon.log, that it killed and which pid it killed.
func TestAcquirePIDFile_Reclaim_LogsTheForceKilledPID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")

	oldPID, cleanupChild := spawnLiveChild(t)
	defer cleanupChild()
	writePIDFile(t, path, oldPID)

	withFakePidIsLiveDaemon(t, oldPID)
	withFakeSocketHealth(t, false)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	release, err := AcquirePIDFile(path, "/nonexistent/socket/for/probe", logger)
	if err != nil {
		t.Fatalf("expected reclaim to succeed, got: %v", err)
	}
	defer release()

	recs := decodeRecords(t, &buf)
	if len(recs) == 0 {
		t.Fatalf("reclaim force-killed pid %d and logged NOTHING", oldPID)
	}
	var found map[string]any
	for _, rec := range recs {
		if pid, ok := rec["reclaimed_pid"]; ok {
			if int(pid.(float64)) == oldPID {
				found = rec
				break
			}
		}
	}
	if found == nil {
		t.Fatalf("no log record carries reclaimed_pid=%d; records=%v", oldPID, recs)
	}
	if lvl, _ := found["level"].(string); lvl != "WARN" {
		t.Fatalf("reclaim record level = %q, want WARN (a force-kill of another daemon is not routine)", lvl)
	}
}

// The mirror: when the existing owner is healthy, nothing is killed, so
// nothing may claim a reclaim. A log line that appears either way would be
// useless for telling "we killed it" from "we left it alone".
func TestAcquirePIDFile_HealthyOwner_LogsNoReclaim(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")

	oldPID, cleanupChild := spawnLiveChild(t)
	defer cleanupChild()
	writePIDFile(t, path, oldPID)

	withFakePidIsLiveDaemon(t, oldPID)
	withFakeSocketHealth(t, true)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	if _, err := AcquirePIDFile(path, "/nonexistent/socket/for/probe", logger); err == nil {
		t.Fatalf("expected ErrAlreadyRunning for a healthy owner")
	}
	for _, rec := range decodeRecords(t, &buf) {
		if _, ok := rec["reclaimed_pid"]; ok {
			t.Fatalf("healthy owner was not killed, but a reclaim was logged: %v", rec)
		}
	}
}

// A nil logger must not panic: AcquirePIDFile is called on a path where the
// logger is always present today, but the nil case is the one a future caller
// gets wrong, and a panic there would take the daemon down at startup.
func TestAcquirePIDFile_NilLogger_Reclaims(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")

	oldPID, cleanupChild := spawnLiveChild(t)
	defer cleanupChild()
	writePIDFile(t, path, oldPID)

	withFakePidIsLiveDaemon(t, oldPID)
	withFakeSocketHealth(t, false)

	release, err := AcquirePIDFile(path, "/nonexistent/socket/for/probe", nil)
	if err != nil {
		t.Fatalf("expected reclaim to succeed with a nil logger, got: %v", err)
	}
	defer release()
	if got := ReadPIDFile(path); got != os.Getpid() {
		t.Fatalf("pidfile = %d after reclaim, want %d", got, os.Getpid())
	}
}
