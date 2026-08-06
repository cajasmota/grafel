package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestRunWatcherRefreshCmd_BoundedEvenWithSurvivingGrandchild pins #6184 F1,
// found on review: exec.CommandContext SIGKILLs only the DIRECT child.
// CombinedOutput backs stdout/stderr with an os.Pipe, and Wait() blocks on
// that pipe until every WRITER closes it — including a grandchild that
// inherited the fd. install --refresh-state's own child is launchctl, which
// is exactly the process #6184 says can wedge: killing the direct child
// alone leaves a surviving launchctl grandchild holding the pipe open, so a
// context deadline with no WaitDelay does not bound wall-clock time at all.
//
// This exercises the REAL production function — runWatcherRefreshCmd — with
// a real child process, not a context-obeying stub, so it can actually fail
// against the pre-WaitDelay implementation (a prior version of this test
// substituted the whole function with a `select { case <-ctx.Done() }` stub,
// which honours the context by construction and could never observe this
// bug).
//
// The spawned shell backgrounds a grandchild that inherits stdout/stderr and
// outlives the direct child, then the direct child itself prints "partial"
// and sleeps 20s. Measured against the unfixed code: 1s deadline, 20.55s
// elapsed. WaitDelay must bound that to a few seconds.
func TestRunWatcherRefreshCmd_BoundedEvenWithSurvivingGrandchild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell only")
	}
	script := filepath.Join(t.TempDir(), "wedged.sh")
	body := "#!/bin/sh\n(sleep 20) &\necho partial\nsleep 20\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	out, err := runWatcherRefreshCmd(ctx, script)
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Fatalf("runWatcherRefreshCmd did not return promptly with a surviving grandchild "+
			"holding the output pipe open (#6184 F1): took %s, want well under the 20s the "+
			"grandchild sleeps\nerr=%v out=%q", elapsed, err, out)
	}
	if err == nil {
		t.Errorf("expected an error from the killed child, got nil (out=%q)", out)
	}
}
