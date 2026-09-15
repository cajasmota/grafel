package service

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// This file is deliberately free of a //go:build tag, for the same reason
// schtasks_wrapper.go is: the policy it covers — how many times we fire
// `schtasks /run`, how long we are willing to wait for the task to come up,
// and what the task XML says about restarting itself — is pure Go with no
// Windows syscall in it. Keeping it behind //go:build windows is exactly how
// #7051 stayed invisible: the two constants that were supposed to protect one
// another lived in a file nobody's test binary ever compiled.

// ---------------------------------------------------------------------------
// The #7051 invariant: the OS safety net must land INSIDE the budget that is
// waiting for it.
// ---------------------------------------------------------------------------

func TestSchtasksReadinessOutlastsRestartOnFailure(t *testing.T) {
	// The defect: RestartOnFailure fires at PT1M, waitReady gave up at 60s, so
	// the first automatic retry could not possibly land inside the window
	// install/start actually waits on.
	if schtasksReadiness.budget <= restartOnFailureInterval {
		t.Fatalf("readiness budget %s does not outlast RestartOnFailure interval %s: "+
			"the retry that is meant to cover a failed /run lands after we have already given up (#7051)",
			schtasksReadiness.budget, restartOnFailureInterval)
	}
	// Outlasting it by a hair is not enough: once the retry launches the
	// daemon, the daemon still needs its whole cold-start allowance (#4458).
	if slack := schtasksReadiness.budget - restartOnFailureInterval; slack < defaultReadiness.budget {
		t.Fatalf("budget leaves only %s after the RestartOnFailure retry lands; "+
			"a cold start needs the full %s allowance", slack, defaultReadiness.budget)
	}
}

func TestSchtasksReadinessIsDerivedNotChosen(t *testing.T) {
	// A comment is not a mechanism. If the budget is a literal, a future edit
	// to restartOnFailureInterval silently desynchronises them again — which is
	// precisely how this bug was born.
	want := restartOnFailureInterval + defaultReadiness.budget
	if schtasksReadiness.budget != want {
		t.Fatalf("schtasksReadiness.budget = %s, want %s (restartOnFailureInterval + cold-start allowance)",
			schtasksReadiness.budget, want)
	}
	if schtasksReadiness.interval != defaultReadiness.interval {
		t.Fatalf("poll interval = %s, want the shared default %s",
			schtasksReadiness.interval, defaultReadiness.interval)
	}
}

// ---------------------------------------------------------------------------
// The task XML must consume the same constant the budget is derived from.
// ---------------------------------------------------------------------------

type parsedTaskSettings struct {
	RestartOnFailure struct {
		Interval string `xml:"Interval"`
		Count    int    `xml:"Count"`
	} `xml:"Settings>RestartOnFailure"`
	MultipleInstancesPolicy string `xml:"Settings>MultipleInstancesPolicy"`
}

func renderTestTaskXML(t *testing.T) parsedTaskSettings {
	t.Helper()
	out, err := renderTaskXML(daemonTaskVars{
		TaskName:        "com.grafel.daemon",
		WrapperHost:     `C:\Windows\System32\wscript.exe`,
		WrapperPath:     `C:\Users\u\AppData\Local\grafel\tasks\com.grafel.daemon.vbs`,
		RestartInterval: restartOnFailureIntervalXML(),
		RestartCount:    restartOnFailureCount,
	})
	if err != nil {
		t.Fatalf("renderTaskXML: %v", err)
	}
	var got parsedTaskSettings
	dec := xml.NewDecoder(bytes.NewReader(out))
	// The declaration says UTF-16 (what Task Scheduler's schema calls for)
	// while renderTaskXML emits UTF-8 bytes; see its comment. Pass the bytes
	// through rather than transcoding — this test is about the settings, not
	// the encoding.
	dec.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("rendered task XML does not parse: %v\n%s", err, out)
	}
	return got
}

func TestTaskXMLRestartIntervalComesFromTheConstant(t *testing.T) {
	got := renderTestTaskXML(t)
	if want := restartOnFailureIntervalXML(); got.RestartOnFailure.Interval != want {
		t.Fatalf("<Interval> = %q, want %q — the XML must not carry its own literal, "+
			"or it can drift away from the budget derived from it (#7051)",
			got.RestartOnFailure.Interval, want)
	}
	if got.RestartOnFailure.Count != restartOnFailureCount {
		t.Fatalf("<Count> = %d, want %d", got.RestartOnFailure.Count, restartOnFailureCount)
	}
}

func TestRestartOnFailureIntervalXMLIsValidISO8601(t *testing.T) {
	// Task Scheduler's documented minimum for RestartOnFailure/Interval is one
	// minute, so this constant cannot be shrunk to fit a budget — the budget is
	// the side that has to move. Pin both the encoding and the floor.
	if restartOnFailureInterval < time.Minute {
		t.Fatalf("restartOnFailureInterval = %s, below Task Scheduler's documented 1-minute minimum",
			restartOnFailureInterval)
	}
	got := restartOnFailureIntervalXML()
	if !strings.HasPrefix(got, "PT") {
		t.Fatalf("restartOnFailureIntervalXML() = %q, want an ISO-8601 PTnM duration", got)
	}
	if got != "PT1M" {
		t.Fatalf("restartOnFailureIntervalXML() = %q, want %q for a %s interval", got, "PT1M", restartOnFailureInterval)
	}
}

func TestTaskXMLKeepsIgnoreNewInstancePolicy(t *testing.T) {
	// Load-bearing for the retraction on #7051: IgnoreNew is why a
	// task-launched retry cannot coexist with a live incumbent from the same
	// lineage. If it ever changes, the reasoning behind widening the budget
	// stops holding.
	got := renderTestTaskXML(t)
	if got.MultipleInstancesPolicy != "IgnoreNew" {
		t.Fatalf("<MultipleInstancesPolicy> = %q, want %q", got.MultipleInstancesPolicy, "IgnoreNew")
	}
}

// ---------------------------------------------------------------------------
// The /run retry itself.
// ---------------------------------------------------------------------------

type runRecorder struct {
	calls  int
	sleeps []time.Duration
	errs   []error // errs[i] is returned by attempt i; past the end -> nil
}

func (r *runRecorder) sleep(d time.Duration) { r.sleeps = append(r.sleeps, d) }

func (r *runRecorder) attempt(_ context.Context, _ int) error {
	i := r.calls
	r.calls++
	if i < len(r.errs) {
		return r.errs[i]
	}
	return nil
}

func TestRetryRunSucceedsOnFirstAttempt(t *testing.T) {
	r := &runRecorder{}
	if err := retryRun(context.Background(), defaultRunAttempts, r.sleep, r.attempt); err != nil {
		t.Fatalf("retryRun: %v", err)
	}
	if r.calls != 1 {
		t.Fatalf("attempts = %d, want 1 — a succeeding /run must not be re-fired", r.calls)
	}
	if len(r.sleeps) != 0 {
		t.Fatalf("slept %v after a successful attempt; install must not pay a backoff it does not need", r.sleeps)
	}
}

func TestRetryRunRecoversAfterATransientFailure(t *testing.T) {
	// The reporter's case: the first /run fails, an identical unmodified retry
	// succeeds immediately.
	r := &runRecorder{errs: []error{errors.New("the system cannot find the file specified")}}
	if err := retryRun(context.Background(), defaultRunAttempts, r.sleep, r.attempt); err != nil {
		t.Fatalf("retryRun should have recovered on the second attempt, got %v", err)
	}
	if r.calls != 2 {
		t.Fatalf("attempts = %d, want 2", r.calls)
	}
	if len(r.sleeps) != 1 {
		t.Fatalf("sleeps = %v, want exactly one backoff between the two attempts", r.sleeps)
	}
	if r.sleeps[0] != defaultRunAttempts.backoff {
		t.Fatalf("backoff = %s, want %s", r.sleeps[0], defaultRunAttempts.backoff)
	}
}

func TestRetryRunGivesUpAtTheCapAndReportsWhy(t *testing.T) {
	boom := errors.New("access is denied")
	r := &runRecorder{errs: []error{boom, boom, boom, boom, boom, boom, boom, boom}}
	err := retryRun(context.Background(), defaultRunAttempts, r.sleep, r.attempt)
	if err == nil {
		t.Fatal("retryRun returned nil after every attempt failed — the exit code would be discarded again (#7051)")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("error %v does not wrap the underlying failure", err)
	}
	if r.calls != defaultRunAttempts.attempts {
		t.Fatalf("attempts = %d, want exactly %d — an uncapped retry is the failure mode here",
			r.calls, defaultRunAttempts.attempts)
	}
	if len(r.sleeps) != defaultRunAttempts.attempts-1 {
		t.Fatalf("sleeps = %d, want %d (no backoff after the final attempt)",
			len(r.sleeps), defaultRunAttempts.attempts-1)
	}
}

func TestRunAttemptPolicyIsClampedNoMatterWhatItIsGiven(t *testing.T) {
	// The bound must be structural, not a matter of the caller behaving. A
	// policy asking for a million attempts an hour apart must not be able to
	// park install() for an hour.
	insane := runAttemptPolicy{attempts: 1_000_000, backoff: time.Hour, attemptTimeout: time.Hour}
	r := &runRecorder{errs: make([]error, 1_000_000)}
	for i := range r.errs {
		r.errs[i] = errors.New("nope")
	}
	if err := retryRun(context.Background(), insane, r.sleep, r.attempt); err == nil {
		t.Fatal("expected failure")
	}
	if r.calls > maxRunAttempts {
		t.Fatalf("made %d attempts, clamp is %d", r.calls, maxRunAttempts)
	}
	for _, d := range r.sleeps {
		if d > maxRunBackoff {
			t.Fatalf("slept %s, clamp is %s", d, maxRunBackoff)
		}
	}
}

func TestRetryRunNeverOutlastsTheSafetyNetItPrecedes(t *testing.T) {
	// Our in-process retry runs BEFORE waitReady starts counting, so its whole
	// worst case is added to what the user waits. It must finish before Task
	// Scheduler's own retry would land, or the two race.
	if got := defaultRunAttempts.maxElapsed(); got >= restartOnFailureInterval {
		t.Fatalf("default /run retry worst case %s >= RestartOnFailure interval %s", got, restartOnFailureInterval)
	}
	// And that must hold for the worst policy the clamp permits, not just for
	// the default anyone can edit.
	worst := runAttemptPolicy{attempts: 1_000_000, backoff: time.Hour, attemptTimeout: time.Hour}.normalized()
	if got := worst.maxElapsed(); got >= restartOnFailureInterval {
		t.Fatalf("clamped worst case %s >= RestartOnFailure interval %s: the clamp does not actually bound the wait",
			got, restartOnFailureInterval)
	}
}

func TestRetryRunCannotHangOnAWedgedAttempt(t *testing.T) {
	// A transient /run failure must not become a hang: each attempt gets its
	// own deadline, and an attempt that ignores nothing but its context still
	// has to end.
	p := runAttemptPolicy{attempts: 2, backoff: time.Millisecond, attemptTimeout: 40 * time.Millisecond}
	calls := 0
	blocked := func(ctx context.Context, _ int) error {
		calls++
		<-ctx.Done()
		return ctx.Err()
	}
	done := make(chan error, 1)
	go func() { done <- retryRun(context.Background(), p, func(time.Duration) {}, blocked) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a wedged attempt must not report success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("retryRun hung on an attempt that never returns on its own")
	}
	if calls != 2 {
		t.Fatalf("attempts = %d, want 2", calls)
	}
}

func TestRetryRunStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &runRecorder{errs: []error{errors.New("a"), errors.New("b"), errors.New("c")}}
	err := retryRun(ctx, defaultRunAttempts, r.sleep, r.attempt)
	if err == nil {
		t.Fatal("expected an error from an already-cancelled context")
	}
	if r.calls > 1 {
		t.Fatalf("attempts = %d after cancellation, want at most 1", r.calls)
	}
}

// ---------------------------------------------------------------------------
// Observability: a swallowed /run failure must stop being invisible.
// ---------------------------------------------------------------------------

// warnManager is a fakeManager whose Load() "succeeds" while reporting a
// non-fatal sub-step failure, exactly as the Windows backend does when every
// `schtasks /run` attempt failed but the logon trigger is still armed.
type warnManager struct {
	fakeManager
	warnings []string
}

func (w *warnManager) LoadWarnings() []string { return w.warnings }

func TestEnsureLoadedSurfacesSwallowedLoadWarnings(t *testing.T) {
	w := &warnManager{
		fakeManager: fakeManager{neverReady: true},
		warnings:    []string{"schtasks /run failed after 3 attempts: access is denied"},
	}
	var progress []string
	_, err := ensureLoaded(context.Background(), w,
		readinessConfig{budget: 20 * time.Millisecond, interval: 5 * time.Millisecond},
		func(s string) { progress = append(progress, s) })
	if err == nil {
		t.Fatal("expected a not-ready failure")
	}
	if !strings.Contains(err.Error(), "access is denied") {
		t.Fatalf("error %q does not mention the swallowed /run failure — "+
			"the reporter's complaint is that this failure is recorded nowhere (#7051)", err)
	}
	if !strings.Contains(strings.Join(progress, "\n"), "access is denied") {
		t.Fatalf("progress %q never mentioned the swallowed /run failure", progress)
	}
}

func TestEnsureLoadedIsSilentWhenLoadHadNothingToReport(t *testing.T) {
	w := &warnManager{fakeManager: fakeManager{probeReadyAfter: 0}}
	var progress []string
	if _, err := ensureLoaded(context.Background(), w, defaultReadiness,
		func(s string) { progress = append(progress, s) }); err != nil {
		t.Fatalf("ensureLoaded: %v", err)
	}
	for _, line := range progress {
		if strings.Contains(line, "warning") {
			t.Fatalf("emitted a warning line %q for a clean load", line)
		}
	}
}
