//go:build unix

package yaml_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/extractors/yaml"
)

// helmFIFODeadline bounds each subtest. A correctly-gated read returns in
// microseconds — safeio's stat gate never opens a FIFO — so anything near this
// value is the hang, not a slow machine.
const helmFIFODeadline = 10 * time.Second

// mkfifoInTemp creates a named pipe inside dir, which must be a t.TempDir().
// A FIFO left outside a temp dir would outlive the test and hang any other
// process that walks over it, so this helper takes the DIRECTORY rather than a
// full path and cannot be pointed elsewhere by accident.
func mkfifoInTemp(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatalf("mkfifo %s: %v", p, err)
	}
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

// mustReturn runs fn and fails the test if it has not returned within
// helmFIFODeadline. Every call into the extractor needs this, not just the
// liveness test: under the pre-fix code a bare call parks in open(2) forever,
// which wedges the whole test binary until the -timeout watchdog kills it with
// no attribution AND leaves the t.TempDir cleanup unrun — which leaks a FIFO
// onto a shared machine.
func mustReturn(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(helmFIFODeadline):
		t.Fatalf("HANG: %s did not return within %s", what, helmFIFODeadline)
	}
}

// helmFIFOValuesFixture is a minimal parent values.yaml. Its content does not
// matter to this test — reaching helmSiblingSubcharts does.
var helmFIFOValuesFixture = []byte(`postgresql:
  auth:
    username: app
`)

// TestHelmSiblingChartFIFODoesNotHang is the #6416 regression for the Helm
// sibling-Chart.yaml read.
//
// helmSiblingSubcharts joins "Chart.yaml"/"Chart.yml" BY NAME onto
// RepoRoot + the values.yaml's own directory. That path never passes through
// the file walker, so the walker's entry-type gate cannot protect it: a FIFO
// named Chart.yaml beside an indexed values.yaml wedged the YAML extraction
// worker forever in open(2).
//
// It is narrower than the go.mod sites — it needs a chart directory that is
// already being indexed — but it is the same defect, and a hang is pinned with
// a deadline rather than an assertion because the pre-fix code never returned
// a value to assert on.
func TestHelmSiblingChartFIFODoesNotHang(t *testing.T) {
	for _, name := range []string{"Chart.yaml", "Chart.yml"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			chartDir := filepath.Join(dir, "charts", "umbrella")
			if err := os.MkdirAll(chartDir, 0o750); err != nil {
				t.Fatal(err)
			}
			mkfifoInTemp(t, chartDir, name)

			var err error
			mustReturn(t, "Helm values extraction with a FIFO named "+name, func() {
				_, err = extractYAMLWithRoot(helmFIFOValuesFixture, "charts/umbrella/values.yaml", dir)
			})
			if err != nil {
				t.Fatalf("extract: %v", err)
			}
		})
	}
}

// TestHelmChartSkipIsReported pins the reporting half. A FIFO named Chart.yaml
// means every subchart OVERRIDES edge for that chart silently vanishes; a
// degradation that large must not be diagnosable only by strace (#6338).
func TestHelmChartSkipIsReported(t *testing.T) {
	dir := t.TempDir()
	chartDir := filepath.Join(dir, "charts", "umbrella")
	if err := os.MkdirAll(chartDir, 0o750); err != nil {
		t.Fatal(err)
	}
	p := mkfifoInTemp(t, chartDir, "Chart.yaml")

	var buf strings.Builder
	restore := yaml.SetHelmSkipOutput(&buf)
	defer restore()

	var err error
	mustReturn(t, "Helm values extraction", func() {
		_, err = extractYAMLWithRoot(helmFIFOValuesFixture, "charts/umbrella/values.yaml", dir)
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, p) {
		t.Fatalf("skip report does not name the path %q; got %q", p, got)
	}
	if !strings.Contains(got, "named-pipe") {
		t.Fatalf("skip report does not say WHY (named-pipe); got %q", got)
	}
	if !strings.Contains(got, "#6416") {
		t.Fatalf("skip report does not cite the issue; got %q", got)
	}
}

// TestHelmMissingChartIsNotReported guards the other half of the convention:
// most values.yaml files have no sibling Chart.yaml at all, and announcing
// that ENOENT would bury the FIFO signal.
func TestHelmMissingChartIsNotReported(t *testing.T) {
	dir := t.TempDir()
	chartDir := filepath.Join(dir, "charts", "umbrella")
	if err := os.MkdirAll(chartDir, 0o750); err != nil {
		t.Fatal(err)
	}

	var buf strings.Builder
	restore := yaml.SetHelmSkipOutput(&buf)
	defer restore()

	var err error
	mustReturn(t, "Helm values extraction", func() {
		_, err = extractYAMLWithRoot(helmFIFOValuesFixture, "charts/umbrella/values.yaml", dir)
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if got := buf.String(); got != "" {
		t.Fatalf("an absent Chart.yaml was reported as a skip: %q", got)
	}
}
