package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install"
)

// #6179 F1-b: on the reporter's machine all 140 units are stale, so the
// reconcile rewrites and re-registers all 140 — and loader_darwin.go retries
// bootstrap up to 3x serially, so worst case is ~560 launchctl invocations.
// That is a Background Items notification burst in the same class as the one
// the issue reports, fired immediately after an upgrade. If it happens with no
// output explaining it, the natural reading is "the fix made it worse". Both
// call sites previously discarded the message.

// TestReportWatcherRefresh_ForwardsChildOutputOnSuccess pins the `grafel
// update` side. The child's summary was printed only when the child FAILED, so
// on the success path — the normal path, the one that produces the burst — it
// was swallowed.
func TestReportWatcherRefresh_ForwardsChildOutputOnSuccess(t *testing.T) {
	summary := "✓ watcher units refreshed: 140 rewritten, 140 re-registered, 0 already current"

	var buf bytes.Buffer
	reportWatcherRefresh(&buf, []byte(summary+"\n"), nil)
	if !strings.Contains(buf.String(), "140 rewritten") {
		t.Fatalf("the child's watcher summary was not surfaced on the success path; the user "+
			"sees a launchctl burst with no explanation (#6179 F1-b)\ngot: %q", buf.String())
	}

	// Silence stays silence: nothing stale means nothing to say.
	buf.Reset()
	reportWatcherRefresh(&buf, nil, nil)
	if strings.TrimSpace(buf.String()) != "" {
		t.Errorf("empty child output must produce no noise, got %q", buf.String())
	}

	// Failures still surface.
	buf.Reset()
	reportWatcherRefresh(&buf, []byte("boom"), io.ErrUnexpectedEOF)
	if !strings.Contains(buf.String(), "boom") {
		t.Errorf("a failing refresh must still report, got %q", buf.String())
	}
}

// TestReconcileSummary_WarnsAboutTheNotificationBurst: the summary has to say
// why macOS is about to light up, or a one-time repair reads as a relapse of
// the very bug being fixed.
func TestReconcileSummary_WarnsAboutTheNotificationBurst(t *testing.T) {
	var buf bytes.Buffer
	printReconcileSummary(&buf, &install.ReconcileWatcherResult{
		Rewritten: []string{"a", "b"},
		Reloaded:  []string{"a", "b"},
		Current:   3,
	})
	got := buf.String()
	if !strings.Contains(got, "2") {
		t.Errorf("summary should report how many units were rewritten, got %q", got)
	}
	low := strings.ToLower(got)
	if !strings.Contains(low, "notification") && !strings.Contains(low, "background item") {
		t.Errorf("the summary must warn that re-registering units produces macOS Background "+
			"Items notifications — otherwise the one-time burst reads as the bug recurring "+
			"(#6179 F1-b)\ngot: %q", got)
	}

	// Nothing rewritten: stay silent.
	buf.Reset()
	printReconcileSummary(&buf, &install.ReconcileWatcherResult{Current: 140})
	if strings.TrimSpace(buf.String()) != "" {
		t.Errorf("an up-to-date machine must print nothing, got %q", buf.String())
	}
}

// TestRunRefreshState_QuietKeepsWatcherSummary: `grafel update` re-invokes this
// command as a child and forwards its output. The install-state bookkeeping
// lines are noise in that context, but the watcher summary is the whole point,
// so quiet mode must drop the former and keep the latter.
func TestRunRefreshState_QuietKeepsWatcherSummary(t *testing.T) {
	withSandboxHome(t)
	t.Setenv(refreshStateQuietEnv, "1")

	prev := reconcileWatcherUnits
	reconcileWatcherUnits = func(w io.Writer, _ string) {
		_, _ = io.WriteString(w, "✓ watcher units refreshed: 7 rewritten\n")
	}
	t.Cleanup(func() { reconcileWatcherUnits = prev })

	var buf bytes.Buffer
	if err := runRefreshState(&buf); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "watcher units refreshed") {
		t.Errorf("quiet mode dropped the watcher summary, which is the one thing the parent "+
			"needs to forward (#6179 F1-b)\ngot: %q", got)
	}
	if strings.Contains(got, "install.json") || strings.Contains(got, "install state") {
		t.Errorf("quiet mode must suppress the install-state bookkeeping lines, got %q", got)
	}

	// Without the env var the bookkeeping line comes back.
	_ = os.Unsetenv(refreshStateQuietEnv)
	buf.Reset()
	if err := runRefreshState(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "install") {
		t.Errorf("non-quiet mode must keep reporting install state, got %q", buf.String())
	}
}
