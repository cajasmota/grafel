package daemon

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// #7050 review (M7): the reclaim's force-kill can FAIL. On Windows
// process.ForceKill is OpenProcess(PROCESS_TERMINATE) + TerminateProcess, and
// the open can be refused (ERROR_ACCESS_DENIED across sessions / integrity
// levels). The old code ignored that and took the pidfile anyway — leaving the
// unresponsive owner alive AND recording our pid as the owner: two live
// daemons, silently, in exactly the case the WARN exists for.
//
// Nothing stubbed forceKillFunc, so on darwin/linux killErr was always nil and
// the branch was unreachable from the suite. This pins BOTH halves of the
// answer: refuse to take the pidfile, and still log the attempt with the error.
func TestAcquirePIDFile_ReclaimKillFails_RefusesAndLogsKillErr(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")

	// A genuinely live process: the refusal is conditional on the owner still
	// being there after the failed kill (round-2 BLOCKER 1), so a fake pid
	// would exercise the "it beat us to it" branch instead of this one.
	ownerPID, cleanupChild := spawnLiveChild(t)
	defer cleanupChild()
	writePIDFile(t, path, ownerPID)
	withFakePidIsLiveDaemon(t, ownerPID)
	withFakeSocketHealth(t, false)

	killErr := errors.New("OpenProcess: Access is denied.")
	origKill := forceKillFunc
	forceKillFunc = func(int) error { return killErr }
	t.Cleanup(func() { forceKillFunc = origKill })

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	release, err := AcquirePIDFile(path, "/nonexistent/socket/for/probe", logger)
	if err == nil {
		t.Fatalf("a failed force-kill must refuse the pidfile, got success")
	}
	if release != nil {
		t.Fatalf("release must be nil when the acquire is refused")
	}
	if !errors.Is(err, killErr) {
		t.Fatalf("returned error must wrap the kill failure, got: %v", err)
	}

	// The incumbent's pid must still own the file — overwriting it is exactly
	// the two-daemons-one-pidfile state this refusal exists to prevent.
	if got := ReadPIDFile(path); got != ownerPID {
		t.Fatalf("pidfile = %d after a refused reclaim, want the incumbent %d", got, ownerPID)
	}

	var found map[string]any
	for _, rec := range decodeRecords(t, &buf) {
		if pid, ok := rec["reclaimed_pid"]; ok && int(pid.(float64)) == ownerPID {
			found = rec
			break
		}
	}
	if found == nil {
		t.Fatalf("a failed kill logged nothing — the one case the WARN exists for")
	}
	if ke, _ := found["kill_err"].(string); !strings.Contains(ke, "Access is denied") {
		t.Fatalf("kill_err = %q, want the underlying failure", ke)
	}
	if alive, _ := found["owner_still_alive"].(bool); !alive {
		t.Fatalf("owner_still_alive = false for an owner that is still running — this is the field that tells a refusal apart from a lost race")
	}
}

// #7050 review round 2 (BLOCKER 1): "the kill failed" and "the owner is still
// there" are NOT the same claim, and conflating them refused startup in
// exactly the scenario the reclaim exists for.
//
// pidIsLiveDaemonFunc sees the owner alive, socketIsHealthy then spends ~900ms
// probing and sleeping, and an incumbent finishing its graceful shutdown in
// that window makes the REAL process.ForceKill return
// "os: process already finished" (ESRCH) — on darwin and linux as much as on
// Windows. There is nothing left to protect at that point and nothing retries
// a refused acquire, so refusing leaves the daemon down until a human notices.
//
// Deliberately uses the real forceKillFunc against a reaped pid: a stub would
// only re-pin the test's own opinion of what failure means.
func TestAcquirePIDFile_ReclaimKillFailsBecauseOwnerAlreadyExited_Proceeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")

	deadPID := reapedChildPID(t)
	writePIDFile(t, path, deadPID)
	// Fake only the liveness *decision*, reproducing the race: the check that
	// gated the reclaim saw a live owner, and by the time the kill lands it is
	// gone. forceKillFunc is NOT stubbed.
	withFakePidIsLiveDaemon(t, deadPID)
	withFakeSocketHealth(t, false)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	release, err := AcquirePIDFile(path, "/nonexistent/socket/for/probe", logger)
	if err != nil {
		t.Fatalf("an owner that exited before the kill landed must not block startup, got: %v", err)
	}
	defer release()
	if got := ReadPIDFile(path); got != os.Getpid() {
		t.Fatalf("pidfile = %d, want this process %d", got, os.Getpid())
	}

	for _, rec := range decodeRecords(t, &buf) {
		if pid, ok := rec["reclaimed_pid"]; ok && int(pid.(float64)) == deadPID {
			if alive, _ := rec["owner_still_alive"].(bool); alive {
				t.Fatalf("owner_still_alive = true for a reaped pid")
			}
			return
		}
	}
	t.Fatalf("no reclaim record for pid %d — the attempt must still be logged", deadPID)
}

// #7050 review (M8/M9): of the WARN's five fields only reclaimed_pid was
// asserted, so the socket field could name the pidfile and probe_attempts
// could be a hardcoded 1 with the suite still green. socket comes from
// paths.go / paths_windows.go and is genuinely platform-dependent, and
// probe_attempts is the evidence we are asking a user to trust.
func TestAcquirePIDFile_ReclaimWARN_FieldsMatchWhatActuallyHappened(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")
	const socketPath = "/nonexistent/socket/for/probe"

	oldPID, cleanupChild := spawnLiveChild(t)
	defer cleanupChild()
	writePIDFile(t, path, oldPID)
	withFakePidIsLiveDaemon(t, oldPID)

	// Count the probes the reclaim decision actually made, so probe_attempts
	// is checked against observed behaviour rather than against the same
	// constant the production code reads.
	probes := 0
	origProbe := socketHealthProbe
	socketHealthProbe = func(string, time.Duration) bool { probes++; return false }
	t.Cleanup(func() { socketHealthProbe = origProbe })

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	release, err := AcquirePIDFile(path, socketPath, logger)
	if err != nil {
		t.Fatalf("expected reclaim to succeed: %v", err)
	}
	defer release()

	var found map[string]any
	for _, rec := range decodeRecords(t, &buf) {
		if pid, ok := rec["reclaimed_pid"]; ok && int(pid.(float64)) == oldPID {
			found = rec
			break
		}
	}
	if found == nil {
		t.Fatalf("no reclaim record for pid %d", oldPID)
	}
	if got, _ := found["socket"].(string); got != socketPath {
		t.Fatalf("socket field = %q, want the probed socket %q (not the pidfile %q)", got, socketPath, path)
	}
	if got := int(found["probe_attempts"].(float64)); got != probes {
		t.Fatalf("probe_attempts = %d but the decision made %d probes", got, probes)
	}
	if probes < 2 {
		t.Fatalf("only %d probe(s) made — see TestSocketIsHealthy_ProbeBudget", probes)
	}
}

// #7050 review (M3/M4): withFakeSocketHealth replaces socketHealthProbe
// wholesale, so socketIsHealthy's own loop — the retry budget that decides
// whether a live daemon is condemned — was executed by no test at all.
// socketHealthProbeRetries could be set to 0 (condemn on a single 300 ms miss)
// or the timeout to 1 ns, and everything stayed green.
func TestSocketIsHealthy_ProbeBudget(t *testing.T) {
	var timeouts []time.Duration
	origProbe := socketHealthProbe
	socketHealthProbe = func(_ string, to time.Duration) bool {
		timeouts = append(timeouts, to)
		return false
	}
	t.Cleanup(func() { socketHealthProbe = origProbe })

	healthy, attempts := socketIsHealthy("/nonexistent/socket/for/probe")
	if healthy {
		t.Fatalf("a socket that never answers must not be reported healthy")
	}
	// A live daemon is force-killed on the strength of this budget. One missed
	// probe must never be enough: a busy daemon (mid-GC, cold store open) can
	// miss one.
	const wantAttempts = 3 // socketHealthProbeRetries(2) + the first attempt
	if attempts != wantAttempts || len(timeouts) != wantAttempts {
		t.Fatalf("probe budget = %d attempts (%d probes), want %d — a single miss must not condemn a live daemon",
			attempts, len(timeouts), wantAttempts)
	}
	for i, to := range timeouts {
		if to != 300*time.Millisecond {
			t.Fatalf("probe %d ran with timeout %v, want 300ms — too short and a healthy-but-busy daemon is killed", i, to)
		}
	}
}

// The healthy side of the same loop: the first answering probe must stop the
// budget, so a healthy daemon is never made to wait out the full window.
func TestSocketIsHealthy_StopsAtFirstAnswer(t *testing.T) {
	probes := 0
	origProbe := socketHealthProbe
	socketHealthProbe = func(string, time.Duration) bool { probes++; return true }
	t.Cleanup(func() { socketHealthProbe = origProbe })

	healthy, attempts := socketIsHealthy("/nonexistent/socket/for/probe")
	if !healthy || attempts != 1 || probes != 1 {
		t.Fatalf("healthy=%v attempts=%d probes=%d, want healthy after exactly 1 probe", healthy, attempts, probes)
	}
}
