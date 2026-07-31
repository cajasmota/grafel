//go:build !windows

package daemon

// reaper_eperm_unix_test.go — the ONE unix-visible behaviour change in the
// #6053 reaper fix, named rather than left as a footnote.
//
// The claim "swapping pidAliveProbe for pidAlive is invisible on unix" is
// false in exactly one case, and the difference is worth stating because it
// changes what the reaper does to a real class of process:
//
//	old pidAliveProbe: return p.Signal(syscall.Signal(0)) == nil  → EPERM means DEAD
//	new process.IsAlive (isalive_unix.go:31): ... || err == EPERM → EPERM means ALIVE
//
// EPERM is returned for a pid that EXISTS but belongs to another uid — the
// kernel had to look it up in order to deny us. Zombies, pid <= 0 and
// pid-reuse behave identically under both; only foreign-uid pids differ.
//
// Consequence for a foreign-uid `grafel watch` entry on darwin/linux:
//
//   - before: classified Dead, dropped from the registry silently.
//   - after:  classified Alive, so it reaches the ownership check. If its
//     owner PID does not match the live daemon it is treated as an orphan and
//     SIGTERMed (which fails with EPERM and is recorded in KillErrors, so the
//     failure is visible); if the owner does match, it is kept.
//
// The new behaviour is the deliberate one: "exists but not ours" is a fact we
// should surface, not silently forget, and it matches every other IsAlive call
// site in the tree. Do not "fix" this back.

import (
	"os"
	"syscall"
	"testing"

	"github.com/cajasmota/grafel/internal/process"
)

// TestPidAlive_ForeignUidPidCountsAsAlive pins the EPERM semantics against a
// real foreign-uid process. pid 1 (init/launchd) is root-owned and always
// running, so as a non-root user the signal-0 probe returns EPERM: the exact
// input on which the old and new probes disagree.
func TestPidAlive_ForeignUidPidCountsAsAlive(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: signalling pid 1 is permitted, so EPERM cannot be produced")
	}

	// Establish that this fixture really does exercise EPERM rather than
	// quietly testing an ordinary live pid — otherwise it proves nothing.
	p, err := os.FindProcess(1)
	if err != nil {
		t.Fatalf("FindProcess(1): %v", err)
	}
	if err := p.Signal(syscall.Signal(0)); err != syscall.EPERM {
		t.Skipf("signal-0 on pid 1 returned %v, not EPERM; no foreign-uid pid available here", err)
	}

	// The old probe would have said false here. That is the regression.
	if !pidAlive(1) {
		t.Fatal("pidAlive(1) = false for a live root-owned process — the reaper is back on the " +
			"signal-0 probe, and a foreign-uid watcher would be silently dropped from the registry")
	}
}

// TestPidAlive_AgreesOnTheUnambiguousCases pins that the swap changed ONLY the
// EPERM case: a pid that is ours and running is alive, and the invalid pids
// are dead under both probes.
func TestPidAlive_AgreesOnTheUnambiguousCases(t *testing.T) {
	if !pidAlive(os.Getpid()) {
		t.Error("pidAlive(self) = false, want true")
	}
	for _, pid := range []int{0, -1, -12345} {
		if pidAlive(pid) {
			t.Errorf("pidAlive(%d) = true, want false", pid)
		}
	}
}

// TestReaperUsesTheSharedProbe binds the reaper's Alive dependency to
// internal/process rather than to any local copy: same answers on every input
// the two could disagree about.
func TestReaperUsesTheSharedProbe(t *testing.T) {
	for _, pid := range []int{os.Getpid(), 1, 0, -1} {
		if got, want := pidAlive(pid), process.IsAlive(pid); got != want {
			t.Errorf("pidAlive(%d) = %v, process.IsAlive(%d) = %v — the daemon has diverged "+
				"from the shared probe", pid, got, pid, want)
		}
	}
}
