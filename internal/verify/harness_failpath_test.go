package verify

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// chattyChildEnv gates the helper test below. It is set only by
// startChattyChild, so a normal `go test ./internal/verify` run skips it.
const chattyChildEnv = "GRAFEL_TEST_CHATTY_CHILD"

// chattyChildMarker is what the child writes. The parent asserts on it, so a
// silent child (which would make the race unreachable and the grading
// vacuous) fails the test rather than passing it.
const chattyChildMarker = "grafel-7211-child-noise"

// TestChattyChildHelper is not a test: it is the child process spawned by
// startChattyChild, re-executing this same test binary so the fixture needs no
// shell and works on Windows. It streams to stdout until killed.
func TestChattyChildHelper(t *testing.T) {
	if os.Getenv(chattyChildEnv) != "1" {
		t.Skip("helper process; only run when re-executed by startChattyChild")
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		// Written straight to fd 1 so os/exec's copier goroutine — the
		// writer half of the race — is the thing filling the buffer.
		_, _ = os.Stdout.WriteString(chattyChildMarker + "\n")
		time.Sleep(time.Millisecond)
	}
}

// startChattyChild launches a child process whose stdout/stderr are wired to
// out, exactly as the harness wires the daemon's, and returns once the child
// has actually produced output. The child is killed on cleanup; only this PID
// is ever signalled.
func startChattyChild(t *testing.T, out *outputBuffer) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestChattyChildHelper$", "-test.v")
	cmd.Env = append(os.Environ(), chattyChildEnv+"=1")
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		t.Fatalf("start chatty child: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// Positive control: the fixture is worthless unless the child is really
	// writing concurrently. Wait for first output before proceeding.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), chattyChildMarker) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("chatty child produced no output within 20s; fixture cannot exercise the failure path")
}

// TestWaitForDaemon_FailurePathReadsOutputConcurrently drives the harness's
// daemon-startup failure path — the ONLY path that reads the child's output
// buffer while the child is alive — with a socket that can never become
// connectable.
//
// #7211: this is the grading fixture for the buffer synchronisation. Under
// -race with an unsynchronised outputBuffer the detector fires here (read by
// waitForDaemon vs. write by os/exec's copier goroutine) and the test aborts
// with "race detected during execution of test". The success path of
// TestHarness_FixturesCorpus never touches this, so it grades nothing.
func TestWaitForDaemon_FailurePathReadsOutputConcurrently(t *testing.T) {
	out := newOutputBuffer()
	startChattyChild(t, out)

	// A socket path that never exists and can never be dialled.
	socket := filepath.Join(t.TempDir(), "never-there.sock")

	c, err := waitForDaemon(socket, out, 400*time.Millisecond)
	if c != nil {
		_ = c.Close()
		t.Fatalf("waitForDaemon connected to a socket that does not exist: %s", socket)
	}
	if err == nil {
		t.Fatalf("waitForDaemon returned no error for an unusable socket %s", socket)
	}
	// The diagnostic must carry the child's output — that embedding is what
	// forces the concurrent read in the first place.
	if !strings.Contains(err.Error(), chattyChildMarker) {
		t.Fatalf("failure diagnostic dropped the child output; got: %v", err)
	}
	if !strings.Contains(err.Error(), socket) {
		t.Fatalf("failure diagnostic dropped the socket path; got: %v", err)
	}
}

// TestOutputBuffer_ConcurrentWriteAndRead grades the buffer type directly,
// independent of waitForDaemon: a reader and a writer hitting the same
// outputBuffer with no external synchronisation. Removing the lock from
// EITHER half (write side or read side) must trip the detector here.
func TestOutputBuffer_ConcurrentWriteAndRead(t *testing.T) {
	out := newOutputBuffer()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2000; i++ {
			_, _ = out.Write([]byte("xxxxxxxxxxxxxxxx"))
		}
	}()
	for i := 0; i < 2000; i++ {
		_ = out.String()
	}
	<-done
	if got := out.String(); len(got) != 2000*16 {
		t.Fatalf("outputBuffer lost writes: len=%d, want %d", len(got), 2000*16)
	}
}
