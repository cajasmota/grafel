package main

// doctor.go wires the quick-doctor hook called from every CLI command's
// entry point (#2211).
//
// Per the spec the quick mode must be < 50ms total:
//   - Single SHA-256 of the running binary against install.json.
//   - One HTTP GET to :47274/healthz with a 500ms timeout.
//
// On success it exits silently.  On drift it prints a one-line warning
// to stderr and continues — it NEVER blocks the calling command.
//
// The hook is called from main() before cobra dispatch, so every
// `archigraph` invocation gets the cheap safety net.
//
// If you need to skip the quick check (e.g. in CI jobs that explicitly
// call `archigraph doctor` and don't want double output), set the env var:
//
//	ARCHIGRAPH_SKIP_QUICK_DOCTOR=1
//
// The full drift detection (`archigraph doctor`) lives in:
//   - internal/install/doctor.go  — logic
//   - internal/cli/doctor.go      — cobra command (--json / --quick flags)

import (
	"os"

	"github.com/cajasmota/archigraph/internal/install"
)

// runQuickDoctorHook is called from main() before cobra dispatch.
// It is intentionally silent on success so it adds zero noise to normal
// CLI usage.
func runQuickDoctorHook() {
	if os.Getenv("ARCHIGRAPH_SKIP_QUICK_DOCTOR") != "" {
		return
	}
	// Errors from applyDefaults are programming mistakes — surface them.
	// Drift is printed inline by RunQuickDoctor; we discard the nil return.
	_ = install.RunQuickDoctor(install.QuickOptions{
		// Out defaults to os.Stderr inside RunQuickDoctor when nil.
	})
}
