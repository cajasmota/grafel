package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/envguard"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// TestRootWiresIsolationGuard: the guard is only worth anything if it is
// actually on the root's PersistentPreRunE. newRoot() is the single
// construction point, so this pins the wiring rather than the function.
func TestRootWiresIsolationGuard(t *testing.T) {
	if newRoot().PersistentPreRunE == nil {
		t.Fatal("root command has no PersistentPreRunE — the #6331 isolation guard is not wired")
	}
}

// TestNoSubcommandShadowsTheGuard is the reason this file exists as a gate and
// not just a unit test.
//
// cobra walks UP from the executed command and runs only the CLOSEST
// PersistentPreRunE it finds. A subcommand that grows its own therefore
// silently disarms the root guard for its entire subtree, with no compile error
// and no test failure anywhere else. If this fails, the offending command must
// call envguard.Assert itself (or be added to isolationGuardExempt on purpose).
func TestNoSubcommandShadowsTheGuard(t *testing.T) {
	var offenders []string
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.PersistentPreRunE != nil || sub.PersistentPreRun != nil {
				offenders = append(offenders, sub.CommandPath())
			}
			walk(sub)
		}
	}
	walk(newRoot())
	if len(offenders) > 0 {
		t.Fatalf("these subcommands define their own PersistentPreRun(E) and thereby shadow the "+
			"#6331 isolation guard on the root; each must call envguard.Assert itself: %s",
			strings.Join(offenders, ", "))
	}
}

// TestGuardRefusesPartialIsolation drives the real cobra tree, which is the
// only way to prove the guard runs BEFORE a command's RunE.
//
// `list` is the vehicle: it is a state-reading command that resolves the store,
// so under GRAFEL_HOME-only it would read the sandbox store while any daemon
// call went to the live socket.
func TestGuardRefusesPartialIsolation(t *testing.T) {
	if envguard.RealUserHome() == "" {
		t.Skip("user.Current() unavailable on this runner")
	}
	// Isolate first so a bug in the guard cannot let the command touch real
	// state, then deliberately re-create the partial shape on top.
	testsupport.IsolateHome(t)
	t.Setenv("HOME", envguard.RealUserHome()) // put HOME back to the real one
	t.Setenv("USERPROFILE", envguard.RealUserHome())
	t.Setenv(envguard.EnvDaemonRoot, "")
	t.Setenv(envguard.EnvGrafelHome, t.TempDir())

	root := newRoot()
	root.SetArgs([]string{"list"})
	root.SetOut(&strings.Builder{})
	root.SetErr(&strings.Builder{})

	err := root.Execute()
	if err == nil {
		t.Fatal("`grafel list` ran under GRAFEL_HOME-only isolation; the guard did not refuse")
	}
	if !strings.Contains(err.Error(), envguard.EnvDaemonRoot) {
		t.Fatalf("refusal does not name %s:\n%v", envguard.EnvDaemonRoot, err)
	}
}

// TestGuardAllowsFullIsolation is the control: with all three set the tree must
// dispatch normally. Without this, a guard that refused everything would pass
// the test above.
func TestGuardAllowsFullIsolation(t *testing.T) {
	if envguard.RealUserHome() == "" {
		t.Skip("user.Current() unavailable on this runner")
	}
	testsupport.IsolateHome(t)

	root := newRoot()
	root.PersistentPreRunE = nil
	installIsolationGuard(root)

	ran := false
	probe := &cobra.Command{Use: "probe-6331", RunE: func(*cobra.Command, []string) error {
		ran = true
		return nil
	}}
	root.AddCommand(probe)
	root.SetArgs([]string{"probe-6331"})
	root.SetOut(&strings.Builder{})
	root.SetErr(&strings.Builder{})

	if err := root.Execute(); err != nil {
		t.Fatalf("fully isolated run was refused: %v", err)
	}
	if !ran {
		t.Fatal("probe command never ran")
	}
}

// TestExemptCommandsRunUnderPartialIsolation pins the exemption list's
// behaviour: help and statusline must survive a broken environment, because
// refusing them breaks the user's shell rather than protecting anything.
func TestExemptCommandsRunUnderPartialIsolation(t *testing.T) {
	if envguard.RealUserHome() == "" {
		t.Skip("user.Current() unavailable on this runner")
	}
	testsupport.IsolateHome(t)
	t.Setenv("HOME", envguard.RealUserHome())
	t.Setenv("USERPROFILE", envguard.RealUserHome())
	t.Setenv(envguard.EnvDaemonRoot, "")
	t.Setenv(envguard.EnvGrafelHome, t.TempDir())

	root := newRoot()
	root.SetArgs([]string{"help"})
	root.SetOut(&strings.Builder{})
	root.SetErr(&strings.Builder{})
	if err := root.Execute(); err != nil {
		t.Fatalf("`grafel help` was refused under partial isolation: %v", err)
	}
}

// isolateDaemonRootShort pins GRAFEL_DAEMON_ROOT at a fresh temp directory
// with a SHORT path, for tests that already isolate GRAFEL_HOME and drive the
// cobra root (which the #6331 guard now refuses under GRAFEL_HOME-only).
//
// The path has to be short because daemon.DefaultLayout builds the unix socket
// as <root>/sockets/daemon.sock and the kernel caps sun_path at 104 bytes; a
// t.TempDir() root on macOS (/var/folders/<32 chars>/T/<TestName><digits>/001)
// blows that limit, which is a different failure wearing the same clothes.
func isolateDaemonRootShort(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "g6331")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv(envguard.EnvDaemonRoot, dir)
	return dir
}
