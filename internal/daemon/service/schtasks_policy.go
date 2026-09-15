package service

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"
)

// This file holds the Windows scheduled-task POLICY: the task XML renderer,
// the restart/readiness constants that have to agree with one another, and the
// bounded retry around `schtasks /run`.
//
// It carries no build tag on purpose, for the same reason schtasks_wrapper.go
// does not (#6320): none of it touches a Windows syscall, and keeping it behind
// //go:build windows is exactly how #7051 stayed invisible for as long as it
// did. RestartOnFailure's PT1M lived in one file, waitReady's 60 s budget in
// another, one was meant to protect the other, and no test binary on any
// contributor's machine ever compiled the file that would have let them be
// compared. The genuinely Windows-specific code — the schtasks invocations,
// the %LOCALAPPDATA% path derivation, the user SID lookup — stays in
// schtasks_windows.go.

// ---------------------------------------------------------------------------
// Restart / readiness: ONE constant, everything else derived from it.
// ---------------------------------------------------------------------------

// restartOnFailureInterval is Task Scheduler's own crash-restart interval —
// the <Interval> inside <RestartOnFailure> in the task XML below, and the
// lower bound the readiness budget has to clear.
//
// It is the fixed end of the relationship, not the free one: Task Scheduler
// documents one minute as the minimum for this setting, so it cannot be shrunk
// to fit a shorter budget. The budget is the side that moves. See #7051.
const restartOnFailureInterval = time.Minute

// restartOnFailureCount is the <Count> inside <RestartOnFailure>.
const restartOnFailureCount = 3

// intervalXML renders a duration as the ISO-8601 duration the task XML schema
// requires. Whole minutes only — the schema's minimum is a minute, so
// sub-minute precision would be unrepresentable anyway, and rounding down could
// silently produce PT0S.
//
// It takes the duration as a PARAMETER rather than reading
// restartOnFailureInterval directly: a zero-argument renderer can only be
// asserted against the one string a correct implementation happens to produce
// today ("PT1M"), which a body of `return "PT1M"` satisfies just as well. With
// an input it can be driven with a sentinel no literal coincides with.
func intervalXML(d time.Duration) string {
	minutes := int(d / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	return fmt.Sprintf("PT%dM", minutes)
}

// schtasksReadiness is the readiness configuration install/restart use on
// Windows, and it is DERIVED rather than chosen (#7051).
//
// The whole purpose of RestartOnFailure is to cover a launch that failed. It
// cannot do that if the caller has already declared failure by the time the
// retry lands — which is exactly what shipped: PT1M against a flat 60 s
// budget, two literals picked independently in two files, where one was meant
// to protect the other. Expressing the budget as a function of the interval is
// what stops a future edit to either one from desynchronising them again; a
// comment saying "keep these in sync" demonstrably does not.
//
// The budget is one full safety-net interval (so the first automatic retry can
// land inside the window we are waiting on) PLUS the platform-neutral
// cold-start allowance from #4458 (because once that retry finally launches
// the daemon, the daemon still needs its usual time to open a large store).
var schtasksReadiness = derivedReadiness(restartOnFailureInterval, defaultReadiness)

// derivedReadiness computes that budget from its two inputs.
//
// It exists to be gradeable. As a bare expression over two package constants
// the derivation could not be distinguished from a literal, because
// restartOnFailureInterval and defaultReadiness.budget are both 60s today and
// every plausible hardcoding (120 * time.Second, 2 * defaultReadiness.budget)
// produces the identical value. As a function it can be driven with an
// interval and a base that are equal neither to each other nor to 60s, which no
// literal can coincide with. See TestDerivedReadinessIsAFunctionOfItsInputs.
func derivedReadiness(interval time.Duration, base readinessConfig) readinessConfig {
	return readinessConfig{
		budget:   interval + base.budget,
		interval: base.interval,
	}
}

// ---------------------------------------------------------------------------
// `schtasks /run`: bounded, observable retry.
// ---------------------------------------------------------------------------

// runAttemptPolicy bounds the in-process retry of `schtasks /run`.
//
// Every field is a bound, and every bound is enforced by normalized() rather
// than by callers behaving — an uncapped retry, or one that can outlast the
// budget it lives inside, is the same class of defect as the one being fixed.
type runAttemptPolicy struct {
	// attempts is the TOTAL number of /run invocations, including the first.
	attempts int
	// backoff is the delay between attempts. There is no exponential growth:
	// the reporter's case is a task-cache that has not settled yet, which a
	// second attempt a moment later resolves or does not.
	backoff time.Duration
	// attemptTimeout bounds a SINGLE invocation, so a wedged schtasks cannot
	// turn a transient failure into a hang.
	attemptTimeout time.Duration
}

// Clamps. maxRunAttempts*maxRunAttemptTimeout + (maxRunAttempts-1)*maxRunBackoff
// must stay strictly under restartOnFailureInterval, so that whatever policy is
// configured, our own retry is finished before Task Scheduler's would land.
// TestRetryRunNeverOutlastsTheSafetyNetItPrecedes asserts precisely that.
const (
	maxRunAttempts       = 5
	maxRunBackoff        = 5 * time.Second
	maxRunAttemptTimeout = 5 * time.Second
)

// defaultRunAttempts is the production policy. Three fast attempts: the
// reported failure is a first-run-after-/create flake where "a plain retry of
// the exact same, unmodified task succeeds immediately" (#7051).
var defaultRunAttempts = runAttemptPolicy{
	attempts:       3,
	backoff:        750 * time.Millisecond,
	attemptTimeout: 5 * time.Second,
}

// normalized returns the policy with every field clamped into range.
func (p runAttemptPolicy) normalized() runAttemptPolicy {
	if p.attempts < 1 {
		p.attempts = 1
	}
	if p.attempts > maxRunAttempts {
		p.attempts = maxRunAttempts
	}
	if p.backoff < 0 {
		p.backoff = 0
	}
	if p.backoff > maxRunBackoff {
		p.backoff = maxRunBackoff
	}
	if p.attemptTimeout <= 0 || p.attemptTimeout > maxRunAttemptTimeout {
		p.attemptTimeout = maxRunAttemptTimeout
	}
	return p
}

// maxElapsed is the worst-case wall time retryRun can consume under this
// policy: every attempt burning its full timeout, with a backoff between each
// pair. This retry happens BEFORE waitReady starts counting, so it is time the
// user waits on top of the readiness budget.
func (p runAttemptPolicy) maxElapsed() time.Duration {
	n := p.normalized()
	return time.Duration(n.attempts)*n.attemptTimeout + time.Duration(n.attempts-1)*n.backoff
}

// retryRun invokes attempt up to policy.attempts times, stopping at the first
// success, and returns the last error if none succeeded.
//
// sleep is a seam so the backoff can be observed from a test on any platform
// without spending real time; production passes time.Sleep. attempt receives a
// per-attempt context already bounded by policy.attemptTimeout, plus the
// 1-based attempt number for its error message.
func retryRun(ctx context.Context, policy runAttemptPolicy, sleep func(time.Duration), attempt func(context.Context, int) error) error {
	p := policy.normalized()
	if sleep == nil {
		sleep = time.Sleep
	}
	var lastErr error
	for i := 1; i <= p.attempts; i++ {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return fmt.Errorf("%w (giving up after %d attempt(s): %w)", err, i-1, lastErr)
			}
			return err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, p.attemptTimeout)
		err := attempt(attemptCtx, i)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if i < p.attempts {
			sleep(p.backoff)
		}
	}
	return fmt.Errorf("after %d attempt(s): %w", p.attempts, lastErr)
}

// ---------------------------------------------------------------------------
// Recording the failures Load() swallows.
// ---------------------------------------------------------------------------

// maxLoadWarningLen caps a single recorded warning. A /run warning carries the
// full retryRun error, which wraps up to maxRunAttempts untruncated schtasks
// CombinedOutput blobs, and that string ends up inside an error a caller may
// print. The text was never unbounded, but it was unbudgeted (#7058 review).
const maxLoadWarningLen = 512

// loadWarningLog records the non-fatal failures a backend's Load() swallowed.
//
// It is a separate, untagged type rather than three methods on the Windows
// manager for one reason: a source-level pin proves only that Load CONTAINS a
// call to a recorder, not that the recorder records anything — gutting the
// method body left the whole suite green (#7058 review, R5). Here the
// recording itself is executed by a test on every platform.
type loadWarningLog struct {
	warnings []string
}

// truncationMarker is appended to a warning that was cut short, so a reader
// knows the text is incomplete rather than that schtasks stopped there.
const truncationMarker = "… (truncated)"

// note records one warning, truncating an over-long one.
//
// The cut lands on a UTF-8 rune boundary. maxLoadWarningLen counts BYTES, and
// the string being cut is schtasks' own CombinedOutput: this repo already
// documents that schtasks speaks the user's locale (the //nolint:localematch
// English text-match in Unload) and that a non-ASCII %USERPROFILE% is routine
// enough to have caused #6325. A byte slice through a multi-byte sequence would
// append the marker to a broken rune. Whether that arises in practice is
// UNRESOLVED — it depends on whether Windows hands Go valid UTF-8 for schtasks
// output on a non-English locale, which cannot be determined from macOS — but
// backing off is three lines and cannot make any input worse.
//
// The backoff is bounded by utf8.UTFMax-1 so that input which is not valid
// UTF-8 at all (a raw OEM-codepage blob, where continuation-shaped bytes are
// common) loses at most three bytes rather than unwinding arbitrarily far.
func (l *loadWarningLog) note(msg string) {
	if len(msg) > maxLoadWarningLen {
		cut := maxLoadWarningLen
		for i := 0; i < utf8.UTFMax-1 && cut > 0 && !utf8.RuneStart(msg[cut]); i++ {
			cut--
		}
		msg = msg[:cut] + truncationMarker
	}
	l.warnings = append(l.warnings, msg)
}

// reset drops warnings from a previous Load, so each load reports on its own
// attempt rather than on an earlier one's.
func (l *loadWarningLog) reset() { l.warnings = nil }

// LoadWarnings implements the loadDiagnostics optional interface (manager.go).
func (l *loadWarningLog) LoadWarnings() []string { return l.warnings }

// ---------------------------------------------------------------------------
// Task XML.
// ---------------------------------------------------------------------------

// daemonTaskXMLTemplate is the Windows Task Scheduler XML definition for
// the grafel daemon. The task runs at logon for the registering user,
// restarts on failure, and is hidden from the Task Scheduler UI so it doesn't
// clutter the user's view.
//
// Key semantics that mirror the macOS LaunchAgent and Linux systemd unit:
//   - LogonTrigger — starts at user login (equivalent to RunAtLoad + KeepAlive)
//   - RestartOnFailure — crash-restart (equivalent to KeepAlive). Its interval
//     and count are injected from restartOnFailureInterval /
//     restartOnFailureCount rather than written here as literals, because the
//     readiness budget is derived from that same interval (#7051).
//   - Hidden — keeps the Task Scheduler UI tidy
//   - wscript wrapper — launches grafel without a console, waits for it, and
//     propagates its exit code so Task Scheduler remains the process supervisor
//   - MultipleInstancesPolicy IgnoreNew — a task-launched instance can never
//     coexist with a live incumbent from the same lineage
//   - RunLevel LeastPrivilege — no UAC elevation required (user-level service)
const daemonTaskXMLTemplate = `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>grafel knowledge-graph daemon — managed by grafel install/uninstall</Description>
    <URI>\{{.TaskName}}</URI>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      {{if .UserSID}}<UserId>{{.UserSID}}</UserId>{{end}}
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      {{if .UserSID}}<UserId>{{.UserSID}}</UserId>{{end}}
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Hidden>true</Hidden>
    <RestartOnFailure>
      <Interval>{{xml .RestartInterval}}</Interval>
      <Count>{{.RestartCount}}</Count>
    </RestartOnFailure>
    <Enabled>true</Enabled>
  </Settings>
  <Actions>
    <Exec>
      <Command>{{xml .WrapperHost}}</Command>
      <Arguments>//B //NoLogo &quot;{{xml .WrapperPath}}&quot;</Arguments>
    </Exec>
  </Actions>
</Task>
`

type daemonTaskVars struct {
	TaskName        string
	UserSID         string
	WrapperHost     string
	WrapperPath     string
	RestartInterval string
	RestartCount    int
}

func xmlText(value string) string {
	var buf strings.Builder
	_ = xml.EscapeText(&buf, []byte(value))
	return buf.String()
}

// renderTaskXML is the pure renderer: task XML as a function of its vars, with
// no environment lookup in it. Windows callers go through generateTaskXML,
// which fills in the SID and the wscript path first.
func renderTaskXML(vars daemonTaskVars) ([]byte, error) {
	tmpl, err := template.New("task").Funcs(template.FuncMap{"xml": xmlText}).Parse(daemonTaskXMLTemplate)
	if err != nil {
		return nil, err
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, vars); err != nil {
		return nil, err
	}
	// Task Scheduler requires UTF-16 LE for XML files referenced by /xml.
	// We write the XML via a temp file; schtasks on modern Windows (>=10)
	// also accepts UTF-8 when the BOM is absent, but the spec calls for
	// UTF-16. We store the rendered UTF-8 bytes.
	return []byte(buf.String()), nil
}
