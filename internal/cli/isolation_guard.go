package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/envguard"
)

// isolationGuardExempt names the commands that run even under a partially
// isolated environment.
//
// The list is deliberately tiny, and everything on it is here because failing
// it is worse than running it:
//
//   - help / completion / __complete / __completeNoDesc: cobra's own
//     introspection. They touch no state, and a shell completion handler that
//     errors out corrupts the user's prompt.
//   - statusline: internal/cli/statusline.go emits a shell snippet that a
//     prompt runs on every keystroke-ish interval. It resolves
//     "${GRAFEL_HOME:-$HOME/.grafel}/status/..." itself and reads one JSON
//     file; a hard refusal there would break the user's shell prompt over an
//     environment problem that prompt cannot fix.
//   - doctor: the command a user runs to diagnose exactly the state this guard
//     complains about (internal/install/copy.go:240 says so in as many words).
//     Refusing it hands the user an error and no diagnostic. It instead reports
//     the partial isolation as a [warn] finding — see reportIsolationFinding —
//     which is strictly more information than the refusal carried.
//
// `version` is deliberately NOT here: there is no `version` subcommand in this
// tree, and `grafel --version` is a cobra flag handled before any hook runs, so
// an entry would only misdescribe the command surface.
//
// Everything else — including the read-only commands — is guarded. A partially
// isolated `grafel status` or `grafel list` reports on the WRONG store while
// looking authoritative, which is the same class of silent wrongness #6331 is
// about; there is no reason to exempt reads.
var isolationGuardExempt = map[string]bool{
	"help":             true,
	"completion":       true,
	"__complete":       true,
	"__completeNoDesc": true,
	"statusline":       true,
	"doctor":           true,
}

// isHelpInvocation reports whether this invocation is asking for usage.
//
// Cobra's own help short-circuit (execute() returns flag.ErrHelp before
// preRun) only fires for commands whose flags it parses. Seven commands in this
// tree set DisableFlagParsing — index and its three subcommands, dashboard,
// extract, quality — so cobra never sees their help flag and `grafel index
// --help` reached the guard and was refused. Printing a refusal where usage
// belongs contradicts the exemption list's own principle that introspection
// must never fail, so the guard detects the help request itself.
//
// For flag-parsing commands args carries no flags, so the scan is a no-op there
// and the cobra flag is consulted instead. Scanning stops at "--": everything
// after it is a positional payload, not a request for usage.
func isHelpInvocation(cmd *cobra.Command, args []string) bool {
	if f := cmd.Flags().Lookup("help"); f != nil && f.Value.String() == "true" {
		return true
	}
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

// isExemptFromIsolationGuard reports whether cmd (or any ancestor) is exempt.
// Ancestors count so `grafel help advanced` and `grafel completion zsh` are
// covered by their parent's entry.
func isExemptFromIsolationGuard(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Parent() == nil {
			break // the root command itself is never the exemption
		}
		if isolationGuardExempt[c.Name()] {
			return true
		}
	}
	return false
}

// installIsolationGuard wires envguard onto the root command's
// PersistentPreRunE, so every subcommand is guarded by default rather than by
// remembering to opt in (#6331).
//
// PersistentPreRunE is the right hook and it is currently unclaimed: cobra runs
// only the CLOSEST PersistentPreRunE up the chain, and no subcommand in this
// tree defines one (verified: `grep -rn PersistentPreRun internal/ cmd/` over
// non-test files returns nothing at a1e9deb3e). Any subcommand that grows one
// later must call this guard itself; isolation_guard_6331_test.go fails if one
// appears without doing so.
//
// Deliberately NOT covering the pre-cobra entrypoints in cmd/grafel/main.go
// (index-internal, group-algo, links-internal, xrepo-verify, selftest). Those
// are not user-typed: the first three are fork-exec'd by the daemon's scheduler
// with cmd.Env = os.Environ(), so they inherit whatever environment the daemon
// was started with. Guarding them would refuse a child whose PARENT was already
// vetted at its own startup — and would hard-break any daemon an operator
// deliberately runs partially isolated. selftest sets HOME, GRAFEL_HOME,
// GRAFEL_DAEMON_ROOT and XDG_CONFIG_HOME itself (cmd/grafel/selftest.go:206-235)
// before doing anything, so it is isolated by construction.
func installIsolationGuard(root *cobra.Command) {
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if isExemptFromIsolationGuard(cmd) || isHelpInvocation(cmd, args) {
			return nil
		}
		return envguard.Assert(os.Stderr)
	}
}
