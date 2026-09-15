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
	"unicode/utf8"
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

func TestDerivedReadinessIsAFunctionOfItsInputs(t *testing.T) {
	// THE SENTINEL TEST. A comment is not a mechanism, and neither is a
	// value-equality assertion over two constants that happen to be equal.
	//
	// restartOnFailureInterval and defaultReadiness.budget are BOTH 60s today,
	// so comparing schtasksReadiness.budget against their sum cannot tell a
	// derivation from a literal: `120 * time.Second`,
	// `2 * defaultReadiness.budget` and `defaultReadiness.budget * 2` all
	// satisfy it. That is the same tautology
	// TestTaskXMLCarriesWhateverIntervalItIsGiven exists to escape, and it was
	// sitting on this PR's headline claim (#7058 review, R1).
	//
	// The cure is to drive the derivation with inputs that are equal neither to
	// each other nor to any constant in the package, so no literal can
	// coincide with the answer. 7m and 13s are not 60s, not each other, and
	// 7m13s is not a value anything else here produces.
	base := readinessConfig{budget: 13 * time.Second, interval: 41 * time.Millisecond}
	got := derivedReadiness(7*time.Minute, base)
	if want := 7*time.Minute + 13*time.Second; got.budget != want {
		t.Fatalf("derivedReadiness(7m, 13s).budget = %s, want %s — the budget is not "+
			"actually computed from the interval it is meant to outlast (#7051)", got.budget, want)
	}
	if got.interval != base.interval {
		t.Fatalf("poll interval = %s, want the base's %s", got.interval, base.interval)
	}

	// A second, unrelated pair: one point can be hit by a coincidence, two
	// cannot be hit by any constant.
	base2 := readinessConfig{budget: 2 * time.Second, interval: time.Millisecond}
	if want := 3*time.Minute + 2*time.Second; derivedReadiness(3*time.Minute, base2).budget != want {
		t.Fatalf("derivedReadiness(3m, 2s).budget = %s, want %s",
			derivedReadiness(3*time.Minute, base2).budget, want)
	}
}

func TestSchtasksReadinessGoesThroughTheDerivation(t *testing.T) {
	// And the production value is the derivation applied to the production
	// inputs. Together with the sentinel test above, a literal cannot survive:
	// this pins WHICH inputs, that pins WHAT the function does with them.
	want := derivedReadiness(restartOnFailureInterval, defaultReadiness)
	if schtasksReadiness != want {
		t.Fatalf("schtasksReadiness = %+v, want derivedReadiness(restartOnFailureInterval, defaultReadiness) = %+v",
			schtasksReadiness, want)
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
		RestartInterval: intervalXML(restartOnFailureInterval),
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
	if want := intervalXML(restartOnFailureInterval); got.RestartOnFailure.Interval != want {
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
	// Graded as a FUNCTION OF ITS INPUT, not against "PT1M". Asserting only
	// that the production call returns "PT1M" pins the output to the literal a
	// correct implementation happens to produce today, so a body of
	// `return "PT1M"` — one that ignores restartOnFailureInterval entirely —
	// passes it (#7058 review, R2).
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{7 * time.Minute, "PT7M"},   // sentinel: no literal in the package
		{23 * time.Minute, "PT23M"}, // two digits
		{time.Minute, "PT1M"},       // the production value
		{30 * time.Second, "PT1M"},  // floor: never round down to PT0S
		{0, "PT1M"},                 // floor
	} {
		if got := intervalXML(tc.in); got != tc.want {
			t.Errorf("intervalXML(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := intervalXML(restartOnFailureInterval); !strings.HasPrefix(got, "PT") {
		t.Fatalf("intervalXML(restartOnFailureInterval) = %q, want an ISO-8601 PTnM duration", got)
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

// ---------------------------------------------------------------------------
// The recorder itself, executed rather than text-matched (#7058 review, R5).
// ---------------------------------------------------------------------------

func TestLoadWarningLogActuallyRecords(t *testing.T) {
	// M11 asserts that Load's SOURCE contains a call to the recorder. That
	// survives emptying the recorder's body — the call stays written, records
	// nothing, and the whole observability deliverable is defeated silently.
	// This runs the recorder.
	var l loadWarningLog
	if got := l.LoadWarnings(); len(got) != 0 {
		t.Fatalf("a fresh log reports %v, want nothing", got)
	}
	l.note("schtasks /run failed: access is denied")
	l.note("second")
	got := l.LoadWarnings()
	if len(got) != 2 {
		t.Fatalf("recorded %d warnings, want 2 — note() is not recording anything", len(got))
	}
	if got[0] != "schtasks /run failed: access is denied" || got[1] != "second" {
		t.Fatalf("recorded %q, want the messages as given, in order", got)
	}
}

func TestLoadWarningLogResetDropsThePreviousLoad(t *testing.T) {
	var l loadWarningLog
	l.note("from an earlier load")
	l.reset()
	if got := l.LoadWarnings(); len(got) != 0 {
		t.Fatalf("after reset the log still reports %v — a later load would inherit "+
			"an earlier one's failures", got)
	}
	l.note("from this load")
	if got := l.LoadWarnings(); len(got) != 1 || got[0] != "from this load" {
		t.Fatalf("after reset the log recorded %q, want exactly the new warning", got)
	}
}

func TestLoadWarningLogCapIsActuallyABudget(t *testing.T) {
	// CN-2. The previous version of this test scaled BOTH its input
	// (maxLoadWarningLen*3) and its ceiling (maxLoadWarningLen+marker) with the
	// constant, so no value of the cap could fail it — raising it to 1<<30 left
	// the suite green. That is the R1 tautology again: a constant compared
	// against itself proves nothing about its value.
	//
	// So the cap is pinned against LITERALS. 4096 is not a second opinion about
	// the right value; it is the outer bound past which "capped" stops meaning
	// anything for a string appended to a user-facing error.
	if maxLoadWarningLen < 64 {
		t.Fatalf("maxLoadWarningLen = %d, too small to carry a usable schtasks message", maxLoadWarningLen)
	}
	if maxLoadWarningLen > 4096 {
		t.Fatalf("maxLoadWarningLen = %d: a warning that large is not budgeted, and up to "+
			"maxRunAttempts of them are appended to the \"socket not ready\" error a stuck "+
			"user is trying to read (#7058 review, CN-2)", maxLoadWarningLen)
	}
}

func TestLoadWarningLogTruncatesAnOverlongWarning(t *testing.T) {
	// The direction the cap exists for, asserted against fixed literals so it
	// stays meaningful however the constant moves.
	const oversized = 100_000
	const ceiling = 4096 + len(truncationMarker)
	// The head and the tail are DISTINGUISHABLE. A homogeneous filler cannot
	// tell "kept the front" from "kept the back" — every assertion over it is
	// satisfied by either, and a tail-keeping mutant survived exactly that
	// (#7058 round 4, CN-2f). The front is the half that names the failure.
	const headMark = "SCHTASKS-RUN-FAILED-HEAD:"
	const tailMark = ":TAIL-OF-A-VERY-LONG-DUMP"

	var l loadWarningLog
	l.note(headMark + strings.Repeat("x", oversized) + tailMark)
	got := l.LoadWarnings()[0]

	if len(got) >= oversized {
		t.Fatalf("a %d-byte warning was recorded whole (%d bytes) — nothing truncated it",
			oversized, len(got))
	}
	if len(got) > ceiling {
		t.Fatalf("recorded %d bytes, which is past the %d-byte ceiling this cap exists to hold",
			len(got), ceiling)
	}
	// Exact, for an all-ASCII input: cut at the cap, plus the marker.
	if want := maxLoadWarningLen + len(truncationMarker); len(got) != want {
		t.Fatalf("recorded %d bytes, want exactly %d (cap %d + marker)", len(got), want, maxLoadWarningLen)
	}
	if !strings.HasSuffix(got, truncationMarker) {
		t.Fatalf("a truncated warning does not say so: %q", got)
	}
	if !strings.HasPrefix(got, headMark) {
		t.Fatalf("truncation did not preserve the start of the message, which is the part that "+
			"names the failure: %q…", got[:64])
	}
	if strings.Contains(got, tailMark) {
		t.Fatalf("truncation kept the TAIL of the message instead of the head: %q", got)
	}
}

func TestLoadWarningLogBoundsTheBackoffOnInvalidUTF8(t *testing.T) {
	// schtasks writes to a console in the OEM codepage, so what reaches Go is
	// not guaranteed to be valid UTF-8 at all. In a blob of raw high bytes,
	// EVERY byte can look like a UTF-8 continuation byte, and an unbounded
	// "walk back to a rune start" would unwind arbitrarily far — discarding
	// good text to fix an encoding that was never UTF-8.
	//
	// This is the case the utf8.UTFMax-1 bound exists for, and it is the only
	// fixture that grades it: on VALID UTF-8 the bounded and unbounded loops
	// stop in the same place, so the rune-boundary test above cannot see the
	// difference (#7058 round 4, RUNE-2).
	const runOfContinuationBytes = 20
	var l loadWarningLog
	msg := strings.Repeat("x", maxLoadWarningLen-runOfContinuationBytes/2) +
		strings.Repeat("\x80", runOfContinuationBytes)
	if utf8.RuneStart(msg[maxLoadWarningLen]) {
		t.Fatalf("fixture byte at the cap is a rune start, so it grades nothing")
	}
	l.note(msg)

	body := strings.TrimSuffix(l.LoadWarnings()[0], truncationMarker)
	if short := maxLoadWarningLen - len(body); short > utf8.UTFMax-1 {
		t.Fatalf("backoff discarded %d bytes to reach a rune start; the bound is %d. "+
			"On input that is not UTF-8 at all an unbounded walk keeps going",
			short, utf8.UTFMax-1)
	}
	if len(body) > maxLoadWarningLen {
		t.Fatalf("kept %d bytes, past the %d-byte cap", len(body), maxLoadWarningLen)
	}
}

func TestLoadWarningLogLeavesAShortWarningAlone(t *testing.T) {
	// The over-aggressive direction: without this, "truncates" is satisfied by
	// a recorder that mangles everything.
	var l loadWarningLog
	short := "schtasks /run attempt 1: exit status 1"
	l.note(short)
	if got := l.LoadWarnings()[0]; got != short {
		t.Fatalf("a short warning was altered: %q, want %q", got, short)
	}
	// Exactly at the cap is still short enough to pass through untouched.
	atCap := strings.Repeat("y", maxLoadWarningLen)
	l.note(atCap)
	if got := l.LoadWarnings()[1]; got != atCap {
		t.Fatalf("a warning exactly at the cap was truncated (%d bytes in, %d out)", len(atCap), len(got))
	}
}

func TestLoadWarningLogTruncatesOnARuneBoundary(t *testing.T) {
	// maxLoadWarningLen counts BYTES. This input is built so the byte at the
	// cap is a CONTINUATION byte whatever the cap is: (cap-1) ASCII bytes, then
	// three-byte runes. A plain msg[:cap] therefore ends mid-sequence.
	//
	// The input is synthetic and is NOT a claim about what schtasks emits. What
	// real schtasks output looks like on a non-English Windows locale is
	// UNRESOLVED from here; this only asserts the recorder is correct if it is
	// ever handed multi-byte text.
	var l loadWarningLog
	msg := strings.Repeat("x", maxLoadWarningLen-1) + strings.Repeat("€", 100)
	if utf8.RuneStart(msg[maxLoadWarningLen]) {
		t.Fatalf("fixture does not straddle a rune boundary at byte %d, so it grades nothing",
			maxLoadWarningLen)
	}
	l.note(msg)

	got := l.LoadWarnings()[0]
	body := strings.TrimSuffix(got, truncationMarker)
	if body == got {
		t.Fatalf("no truncation marker on a %d-byte input: %q", len(msg), got[len(got)-32:])
	}
	if !utf8.ValidString(body) {
		t.Fatalf("truncation split a UTF-8 sequence: the kept text is not valid UTF-8 (%d bytes, "+
			"last bytes %x)", len(body), body[len(body)-4:])
	}
	if strings.ContainsRune(body, utf8.RuneError) {
		t.Fatalf("truncation left a replacement character in the kept text")
	}
	// It backed off, and it backed off by the minimum: never past the cap, and
	// never more than one rune's worth short of it.
	if len(body) > maxLoadWarningLen {
		t.Fatalf("kept %d bytes, past the %d-byte cap", len(body), maxLoadWarningLen)
	}
	if len(body) < maxLoadWarningLen-(utf8.UTFMax-1) {
		t.Fatalf("kept only %d bytes, more than %d short of the cap — the backoff is unwinding "+
			"further than one rune", len(body), utf8.UTFMax-1)
	}
}

func TestEnsureLoadedDoesNotDecorateACleanNotReadyError(t *testing.T) {
	// R7: `if len(warnings) > 0` made permissive appends an empty parenthesis
	// to every "socket not ready" error on every platform.
	f := &fakeManager{neverReady: true}
	_, err := ensureLoaded(context.Background(), f,
		readinessConfig{budget: 20 * time.Millisecond, interval: 5 * time.Millisecond}, nil)
	if err == nil {
		t.Fatal("expected a not-ready failure")
	}
	if strings.Contains(err.Error(), "()") {
		t.Fatalf("a load with no warnings still decorated its error: %q", err)
	}
}
