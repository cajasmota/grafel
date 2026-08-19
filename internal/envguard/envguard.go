// Package envguard refuses to run a grafel CLI command under a PARTIALLY
// isolated environment.
//
// # The incident (#6331)
//
// An agent exported GRAFEL_HOME to a temp directory, believed that sandboxed
// the run, and executed `grafel index`. GRAFEL_HOME moves the STORE
// (registry.HomeDir prefers it; the embeddings cache, group overlays and
// progress sidecars all resolve through it) but it is not read by
// daemon.DefaultLayout, so client.Dial resolved the REAL socket at
// ~/.grafel/sockets/daemon.sock and the command drove the developer's live
// daemon. A follow-up `grafel start` went through launchd and stopped it.
//
// # Neither variable alone is isolation
//
//   - GRAFEL_DAEMON_ROOT (internal/daemon.EnvRoot) moves the socket, pidfile
//     and log directory. It does NOT move the store, and the daemon's startup
//     tail runs MigrateToRefStore and PruneStaleGenerations against the store
//     — so this alone RELOCATES and DELETES parts of the live store (#6134).
//   - GRAFEL_HOME moves the store. It does NOT move the daemon plane — so this
//     alone dials, and can stop, the live daemon (#6331).
//
// Full isolation is HOME + GRAFEL_HOME + GRAFEL_DAEMON_ROOT together, which is
// exactly what testsupport.IsolateHome sets.
//
// # Why this package exists rather than more documentation
//
// The knowledge was never missing. internal/testsupport/guard_main.go:31-55
// spells out the full incantation under #6134. Every guard that enforced it,
// though, was test-only: GuardRealHome/GuardRealHomeMain take a *testing.T or
// run from TestMain, homescan.go is a static scan over test files, and
// internal/install/daemon_guard.go is scoped to uninstall (#5277). Nothing sat
// on the real binary's path. So the surface where the mistake is CHEAP (a test
// run, easily rerun) was hardened and the surface where it is EXPENSIVE (the
// real binary against real state) was not. This moves the check to where the
// damage happens.
//
// # The rule
//
//	GRAFEL_HOME   GRAFEL_DAEMON_ROOT   HOME            verdict
//	unset         unset                (any)           OK    — normal production
//	set           unset                (any)           REFUSE — partial (the #6331 shape)
//	unset         set                  (any)           REFUSE — partial (the #6134 shape)
//	set           set                  real user home  WARN   — store+daemon isolated, HOME-derived paths are not
//	set           set                  redirected      OK    — full isolation
//
// Exactly-one-set is refused because partial isolation is never intentional:
// nobody deliberately asks for a private socket onto the live store, or a
// private store behind the live socket. Both-set-under-a-real-HOME is only
// WARNED because a legitimate caller exists — scripts/phase-b-bench.sh exports
// both and leaves HOME alone — and because the residue there is real but much
// smaller: the store and the daemon plane are both redirected, and what still
// resolves from the real home are HOME-derived config paths (~/.claude.json,
// ~/.codeium, the XDG-less ConfigDir fallback), not the store.
//
// EnvAllowPartial=1 downgrades the refusal to a warning, for an operator who
// has read the above and means it.
package envguard

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

// Environment variable names. GRAFEL_DAEMON_ROOT is duplicated as a literal
// rather than imported from internal/daemon: internal/cli already depends on
// internal/daemon, but this package is deliberately dependency-free so it can
// be called from anywhere without dragging the daemon package along. The
// duplication is asserted against daemon.EnvRoot in the tests.
const (
	EnvGrafelHome = "GRAFEL_HOME"
	EnvDaemonRoot = "GRAFEL_DAEMON_ROOT"

	// EnvAllowPartial downgrades a refusal to a warning when set to "1".
	EnvAllowPartial = "GRAFEL_ALLOW_PARTIAL_ISOLATION"
)

// Verdict is the outcome of a Check.
type Verdict int

const (
	// VerdictOK — the environment is either fully un-isolated (normal
	// production) or fully isolated. Proceed silently.
	VerdictOK Verdict = iota
	// VerdictWarn — proceed, but print Message to stderr.
	VerdictWarn
	// VerdictRefuse — do not run the command; Message is the error text.
	VerdictRefuse
)

// Result carries the verdict and the operator-facing message.
type Result struct {
	Verdict Verdict
	Message string
}

// Env is the environment lookup Check reads. os.LookupEnv satisfies it; tests
// pass a map so the check is assertable without touching the process env.
type Env func(key string) (string, bool)

// Check is the pure decision function. realHome is the OS-level user home
// (NOT $HOME — see RealUserHome); pass "" when it could not be determined, in
// which case the both-set-under-a-real-HOME leg is skipped and only the
// partial-isolation leg applies.
func Check(env Env, realHome string) Result {
	// Read VERBATIM. Every consumer of these two variables tests
	// `os.Getenv(...) != ""` with no normalisation — registry.HomeDir
	// (internal/registry/registry.go), daemon paths_unix.go, paths_windows.go
	// — so GRAFEL_HOME="   " redirects the store into a literal three-space
	// directory while the daemon plane stays live. Trimming here would make
	// the guard reason about a different environment than the one the program
	// runs in, and wave through the exact #6331 shape. Only the escape hatch,
	// which no other code reads, is trimmed.
	get := func(k string) string {
		v, _ := env(k)
		return v
	}
	grafelHome := get(EnvGrafelHome)
	daemonRoot := get(EnvDaemonRoot)
	allowPartial := strings.TrimSpace(get(EnvAllowPartial)) == "1"

	hasHome := grafelHome != ""
	hasRoot := daemonRoot != ""

	switch {
	case !hasHome && !hasRoot:
		// Normal production: nothing redirected, everything consistent.
		return Result{Verdict: VerdictOK}

	case hasHome && !hasRoot:
		return refuseOrWarn(allowPartial,
			fmt.Sprintf("%s=%q is set but %s is NOT", EnvGrafelHome, grafelHome, EnvDaemonRoot),
			"the STORE is redirected but the DAEMON PLANE is not: this command will dial the "+
				"REAL daemon socket (~/.grafel/sockets/daemon.sock) and can index into, or stop, "+
				"the live daemon (#6331)",
		)

	case !hasHome && hasRoot:
		return refuseOrWarn(allowPartial,
			fmt.Sprintf("%s=%q is set but %s is NOT", EnvDaemonRoot, daemonRoot, EnvGrafelHome),
			"the DAEMON PLANE is redirected but the STORE is not: the socket/pid/log move, while "+
				"the store still resolves under the real home — and the daemon's startup tail "+
				"relocates and prunes exactly that store (#6134)",
		)

	default: // both set
		if realHome == "" {
			// Cannot tell whether HOME was redirected. Both isolation
			// variables agree, so proceed rather than guess.
			return Result{Verdict: VerdictOK}
		}
		if !sameDir(effectiveHome(env), realHome) {
			return Result{Verdict: VerdictOK} // fully isolated
		}
		return Result{
			Verdict: VerdictWarn,
			Message: fmt.Sprintf(
				"grafel: WARNING — partially isolated environment.\n"+
					"  %s=%q and %s=%q are set, but HOME is still the real user home (%q).\n"+
					"  The store and the daemon plane are redirected; HOME-derived paths are NOT "+
					"(~/.claude.json, ~/.codeium, the XDG-less config fallback), so this run can "+
					"still write to the real home.\n"+
					"  For full isolation: export HOME=$(mktemp -d); export %s=$HOME/.grafel; export %s=$HOME/.grafel\n",
				EnvGrafelHome, grafelHome, EnvDaemonRoot, daemonRoot, realHome,
				EnvGrafelHome, EnvDaemonRoot),
		}
	}
}

func refuseOrWarn(allowPartial bool, what, why string) Result {
	if allowPartial {
		// The escape hatch downgrades, it never silences. The message is
		// re-led rather than prefixed: "WARNING — refusing to run" is a
		// sentence that means nothing.
		return Result{
			Verdict: VerdictWarn,
			Message: partialMessage(
				"grafel: WARNING — PARTIALLY ISOLATED environment, proceeding because "+
					EnvAllowPartial+"=1.", what, why, ""),
		}
	}
	return Result{
		Verdict: VerdictRefuse,
		Message: partialMessage("refusing to run: PARTIALLY ISOLATED environment.", what, why,
			"  Set "+EnvAllowPartial+"=1 to proceed anyway (you are on your own).\n"),
	}
}

func partialMessage(lead, what, why, tail string) string {
	return fmt.Sprintf(
		"%s\n"+
			"  %s.\n"+
			"  Consequence: %s.\n"+
			"  Neither variable alone is isolation. Set all three, or none:\n"+
			"    export HOME=$(mktemp -d); export %s=$HOME/.grafel; export %s=$HOME/.grafel\n"+
			"%s",
		lead, what, why, EnvGrafelHome, EnvDaemonRoot, tail)
}

// effectiveHome is $HOME (or %USERPROFILE% on Windows), i.e. what
// os.UserHomeDir would return.
func effectiveHome(env Env) string {
	key := "HOME"
	if runtime.GOOS == "windows" {
		key = "USERPROFILE"
	}
	if v, _ := env(key); strings.TrimSpace(v) != "" {
		return filepath.Clean(strings.TrimSpace(v))
	}
	return ""
}

// RealUserHome returns the OS-level home directory for the current user,
// resolved WITHOUT consulting $HOME.
//
// os.UserHomeDir is unusable here: it reads $HOME on Unix and %USERPROFILE% on
// Windows, so under a redirected HOME it returns the sandbox and the
// "is HOME still real?" question answers itself yes every time. user.Current
// resolves through getpwuid (cgo) or /etc/passwd (pure Go) on Unix and through
// the process token on Windows, none of which read $HOME.
//
// Returns "" when the lookup fails; callers treat that as "unknown" and skip
// the HOME leg rather than guessing.
func RealUserHome() string {
	u, err := user.Current()
	if err != nil || u == nil || u.HomeDir == "" {
		return ""
	}
	return filepath.Clean(u.HomeDir)
}

// sameDir compares two directory paths, case-insensitively on the platforms
// whose default filesystems are case-insensitive (Windows, darwin) — the same
// reasoning homeguard.Escapes carries after #6288.
func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// Assert applies Check against the live process environment. It returns a
// non-nil error on VerdictRefuse and prints the message to w on VerdictWarn.
func Assert(w *os.File) error {
	res := Check(os.LookupEnv, RealUserHome())
	switch res.Verdict {
	case VerdictRefuse:
		// Trimmed: cli.Execute prints the error with Fprintln, which would
		// otherwise add a second newline to a message that already ends in one.
		return fmt.Errorf("%s", strings.TrimRight(res.Message, "\n"))
	case VerdictWarn:
		if w != nil {
			fmt.Fprint(w, res.Message)
		}
	}
	return nil
}
