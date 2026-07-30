package cli

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
)

// TestMemLimitDescription_SplitReportsBothShares is the reporting half of
// #6045: `grafel status` advertised a single mem_limit while TWO processes
// each applied it, understating real consumption by 2x. The line must now name
// the installation total AND both per-plane shares.
func TestMemLimitDescription_SplitReportsBothShares(t *testing.T) {
	t.Setenv("GOMEMLIMIT", "")
	t.Setenv("GRAFEL_DAEMON_MEMLIMIT_MB", "10000")
	t.Setenv(daemon.SplitModeEnvVar, "1")

	got, src := memLimitDescription()
	if src == "" {
		t.Error("source tag must not be empty")
	}
	serve, engine := daemon.SplitMemLimitMB(10000)
	for _, want := range []string{
		"10000MB",
		strconv.FormatInt(serve, 10) + "MB serve",
		strconv.FormatInt(engine, 10) + "MB engine",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("mem_limit description %q missing %q", got, want)
		}
	}
	// Reporting only one share is the defect: both must appear, and they must
	// be different numbers than the total.
	if strings.Count(got, "MB") < 3 {
		t.Errorf("mem_limit description %q should carry the total plus BOTH shares", got)
	}
	// The printed shares must ADD UP to the printed total — that is the whole
	// claim the line makes. A line whose shares each equal the total is the
	// 2x-understating bug wearing a nicer format.
	nums := memLimitNumbersRe.FindAllStringSubmatch(got, -1)
	if len(nums) != 3 {
		t.Fatalf("mem_limit description %q: want exactly 3 MB figures (total, serve, engine), got %d", got, len(nums))
	}
	parse := func(s string) int64 { v, _ := strconv.ParseInt(s, 10, 64); return v }
	gotTotal, gotServe, gotEngine := parse(nums[0][1]), parse(nums[1][1]), parse(nums[2][1])
	if gotServe+gotEngine != gotTotal {
		t.Errorf("mem_limit description %q: printed shares %d+%d=%d do not sum to the printed total %d",
			got, gotServe, gotEngine, gotServe+gotEngine, gotTotal)
	}
}

var memLimitNumbersRe = regexp.MustCompile(`(\d+)MB`)

// TestMemLimitDescription_MonolithReportsWholeBudget: in monolith mode there is
// one process, so the line must report the whole budget and must NOT claim a
// per-plane split.
func TestMemLimitDescription_MonolithReportsWholeBudget(t *testing.T) {
	t.Setenv("GOMEMLIMIT", "")
	t.Setenv("GRAFEL_DAEMON_MEMLIMIT_MB", "10000")
	t.Setenv(daemon.SplitModeEnvVar, "0")

	got, _ := memLimitDescription()
	if !strings.Contains(got, "10000MB") {
		t.Errorf("monolith mem_limit description %q must report the whole 10000MB budget", got)
	}
	if strings.Contains(got, "serve") || strings.Contains(got, "engine") {
		t.Errorf("monolith mem_limit description %q must not advertise a per-plane split", got)
	}
}

// TestMemLimitDescription_Unbounded keeps the disabled path readable.
func TestMemLimitDescription_Unbounded(t *testing.T) {
	t.Setenv("GOMEMLIMIT", "")
	t.Setenv("GRAFEL_DAEMON_MEMLIMIT_MB", "off")
	got, _ := memLimitDescription()
	if !strings.Contains(got, "unbounded") {
		t.Errorf("disabled limit: description %q should say unbounded", got)
	}
}
