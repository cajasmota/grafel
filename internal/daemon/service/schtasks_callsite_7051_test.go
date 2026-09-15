package service

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Grading the mutants a value-only assertion cannot see.
// ---------------------------------------------------------------------------

func TestTaskXMLCarriesWhateverIntervalItIsGiven(t *testing.T) {
	// TestTaskXMLRestartIntervalComesFromTheConstant compares the rendered
	// value against intervalXML(restartOnFailureInterval), which today is "PT1M" —
	// exactly what the template used to hardcode. So on its own it survives a
	// revert to the literal. Varying the interval while holding every other
	// field constant is what actually distinguishes "injected" from "hardcoded".
	out, err := renderTaskXML(daemonTaskVars{
		TaskName:        "com.grafel.daemon",
		WrapperHost:     `C:\Windows\System32\wscript.exe`,
		WrapperPath:     `C:\Users\u\t.vbs`,
		RestartInterval: "PT7M",
		RestartCount:    9,
	})
	if err != nil {
		t.Fatalf("renderTaskXML: %v", err)
	}
	if !strings.Contains(string(out), "<Interval>PT7M</Interval>") {
		t.Fatalf("the template ignored the injected interval — it is still carrying its own literal (#7051)\n%s", out)
	}
	if !strings.Contains(string(out), "<Count>9</Count>") {
		t.Fatalf("the template ignored the injected count\n%s", out)
	}
}

func TestMaxElapsedCountsTheAttemptTimeouts(t *testing.T) {
	// A maxElapsed that only summed the backoffs would UNDER-report the worst
	// case, and every "fits inside the budget" assertion built on it would then
	// pass for the wrong reason.
	p := runAttemptPolicy{attempts: 2, backoff: time.Second, attemptTimeout: 2 * time.Second}
	if got, want := p.maxElapsed(), 5*time.Second; got != want {
		t.Fatalf("maxElapsed() = %s, want %s (2 attempts x 2s timeout + 1 x 1s backoff)", got, want)
	}
}

// ---------------------------------------------------------------------------
// Source-level pins for the Windows-only call sites.
//
// Load(), runTaskNow(), install() and restartService() sit behind
// //go:build windows, so no test binary on a macOS or Linux machine executes
// them. These assert the call SITE rather than the behaviour: a weaker
// instrument than running the code, stated plainly as such — but strictly
// stronger than the nothing that guarded these lines before, and the same
// technique TestWriteUnit_UsesAtomicArtifactWrites already uses in this
// package for exactly the same reason.
// ---------------------------------------------------------------------------

func TestLoadNoLongerDiscardsTheRunExitCode(t *testing.T) {
	body := funcBodySource6325svc(t, "schtasks_windows.go", "Load")
	if strings.Contains(body, `_ = schtasksCmd("/run"`) {
		t.Fatalf("Load still fires /run and throws the exit code away — the whole of #7051\n%s", body)
	}
	if !strings.Contains(body, "m.runTaskNow(") {
		t.Fatalf("Load does not go through runTaskNow, so the /run retry is not on this path\n%s", body)
	}
	if !strings.Contains(body, "m.note(") {
		t.Fatalf("Load swallows the /run failure without recording it, and the reporter's "+
			"complaint is precisely that this failure is written down nowhere (#7051)\n%s", body)
	}
	if !strings.Contains(body, "m.reset()") {
		t.Fatalf("Load does not clear the previous load's warnings first\n%s", body)
	}
}

func TestGenerateTaskXMLInjectsTheConstantsRatherThanLiterals(t *testing.T) {
	// The INJECTION SITE, which is a different seam from the template.
	// TestTaskXMLCarriesWhateverIntervalItIsGiven proves renderTaskXML honours
	// the parameter it is handed; it says nothing about the caller that fills
	// that parameter, and putting `RestartInterval: "PT1M", RestartCount: 3`
	// back at this call site left the entire suite green (#7058 review, R3).
	// Grading a template is not grading its caller.
	body := funcBodySource6325svc(t, "schtasks_windows.go", "generateTaskXML")
	if !strings.Contains(body, "RestartInterval: intervalXML(restartOnFailureInterval)") {
		t.Errorf("generateTaskXML does not inject the interval from restartOnFailureInterval, "+
			"so the XML can drift away from the budget derived from that same constant (#7051)\n%s", body)
	}
	if !strings.Contains(body, "RestartCount:    restartOnFailureCount") {
		t.Errorf("generateTaskXML does not inject restartOnFailureCount\n%s", body)
	}
	if strings.Contains(body, `"PT1M"`) {
		t.Errorf("generateTaskXML has a hardcoded PT1M back in it\n%s", body)
	}
}

func TestRunTaskNowGoesThroughTheBoundedRetry(t *testing.T) {
	body := funcBodySource6325svc(t, "schtasks_windows.go", "runTaskNow")
	if !strings.Contains(body, "retryRun(") {
		t.Fatalf("runTaskNow does not use retryRun, so nothing bounds it\n%s", body)
	}
	if !strings.Contains(body, "defaultRunAttempts") {
		t.Fatalf("runTaskNow does not use the clamped default policy\n%s", body)
	}
	if !strings.Contains(body, "schtasksCmdContext(") {
		t.Fatalf("runTaskNow uses an unbounded schtasksCmd; a wedged /run would hang install\n%s", body)
	}
}

func TestWindowsEntryPointsUseTheDerivedReadinessBudget(t *testing.T) {
	for _, fn := range []string{"install", "restartService"} {
		body := funcBodySource6325svc(t, "schtasks_windows.go", fn)
		if !strings.Contains(body, "schtasksReadiness") {
			t.Errorf("%s does not use schtasksReadiness, so its budget is not derived from "+
				"RestartOnFailure's interval and can silently fall back under it (#7051)\n%s", fn, body)
		}
		if strings.Contains(body, "defaultReadiness") {
			t.Errorf("%s still passes the platform-neutral defaultReadiness, which is shorter than "+
				"the RestartOnFailure interval meant to cover a failed launch (#7051)\n%s", fn, body)
		}
	}
}

func TestSchtasksReadinessIsAssignedFromTheDerivation(t *testing.T) {
	// The last seam, and it cannot be closed by any value assertion.
	//
	// TestDerivedReadinessIsAFunctionOfItsInputs proves derivedReadiness
	// computes its answer from its arguments, and
	// TestSchtasksReadinessGoesThroughTheDerivation proves the production value
	// EQUALS the derivation applied to the production inputs. Neither notices a
	// literal `readinessConfig{budget: 120 * time.Second, …}` at the assignment,
	// because that literal IS the correct value — re-scored ALIVE after the
	// first two were added (#7058 review, R1). Comparing a value against the
	// function that should have produced it cannot distinguish "was produced by
	// it" from "happens to match it"; only the assignment itself can say that.
	//
	// So this pins the assignment, the same instrument that closes R3 at the
	// XML injection site, and with the same honest limit: it proves the call is
	// written, and the two tests above prove the call does the right thing.
	const want = "var schtasksReadiness = derivedReadiness(restartOnFailureInterval, defaultReadiness)"
	src := readSourceFile6325(t, "schtasks_policy.go")
	if !strings.Contains(src, want) {
		t.Fatalf("schtasksReadiness is not assigned as %q.\n"+
			"A literal there is indistinguishable by value (restartOnFailureInterval and "+
			"defaultReadiness.budget are both 60s, so 120*time.Second matches exactly), which "+
			"is how the two constants desynchronised in the first place (#7051)", want)
	}
}
