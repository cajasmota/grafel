package daemon

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/process"
)

// TestResolveMemLimitMB_EnvOverride asserts the env override wins and is
// parsed as megabytes.
func TestResolveMemLimitMB_EnvOverride(t *testing.T) {
	t.Setenv(memLimitEnv, "4096")
	mb, src := resolveMemLimitMB()
	if mb != 4096 {
		t.Errorf("env override: want 4096 got %d", mb)
	}
	if src != memLimitEnv {
		t.Errorf("source: want %q got %q", memLimitEnv, src)
	}
}

// TestResolveMemLimitMB_Disabled asserts "off"/"0" disables the limit.
func TestResolveMemLimitMB_Disabled(t *testing.T) {
	for _, v := range []string{"off", "0"} {
		t.Setenv(memLimitEnv, v)
		if mb, _ := resolveMemLimitMB(); mb > 0 {
			t.Errorf("%q should disable (mb<=0); got %d", v, mb)
		}
	}
}

// TestResolveMemLimitMB_DefaultClamped asserts the default is a fraction of
// system RAM, clamped into [floor, ceiling]. The ceiling is what bounds
// big-RAM hosts (#5237); the floor protects tiny hosts.
func TestResolveMemLimitMB_DefaultClamped(t *testing.T) {
	t.Setenv(memLimitEnv, "") // ensure no override
	t.Setenv("GRAFEL_HOME", t.TempDir())
	mb, src := resolveMemLimitMB()
	if mb < memLimitFloorMB {
		t.Errorf("default limit %d below floor %d (src=%s)", mb, memLimitFloorMB, src)
	}
	if mb > memLimitCeilingMB {
		t.Errorf("default limit %d above ceiling %d (src=%s)", mb, memLimitCeilingMB, src)
	}
	// The default must equal the fraction-of-RAM value clamped into the band.
	if total := process.TotalMemoryMB(); total > 0 {
		want := int64(float64(total) * memLimitFraction)
		if want > memLimitCeilingMB {
			want = memLimitCeilingMB
		}
		if want < memLimitFloorMB {
			want = memLimitFloorMB
		}
		if mb != want {
			t.Errorf("default limit: want %d (%.0f%% of %dMB, clamped to [%d,%d]) got %d",
				want, memLimitFraction*100, total, memLimitFloorMB, memLimitCeilingMB, mb)
		}
	}
}

func TestResolveMemLimitMB_SettingsOverride(t *testing.T) {
	t.Setenv(memLimitEnv, "")
	t.Setenv("GOMEMLIMIT", "")
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{"daemon_go_memory_limit_mb":8192}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mb, src := resolveMemLimitMB()
	if mb != 8192 || src != "settings.json:daemon_go_memory_limit_mb" {
		t.Fatalf("settings override: got mb=%d src=%q", mb, src)
	}

	t.Setenv(memLimitEnv, "4096")
	if mb, src := resolveMemLimitMB(); mb != 4096 || src != memLimitEnv {
		t.Fatalf("env must win over settings: got mb=%d src=%q", mb, src)
	}
}

// TestMemLimitClampBoundaries verifies the cap + floor formula directly for
// representative host sizes (8GB, 64GB, and a tiny host) independent of the
// machine the test runs on. Mirrors resolveMemLimitMB's clamp logic.
func TestMemLimitClampBoundaries(t *testing.T) {
	clamp := func(totalMB int64) int64 {
		limit := int64(float64(totalMB) * memLimitFraction)
		if limit > memLimitCeilingMB {
			return memLimitCeilingMB
		}
		if limit < memLimitFloorMB {
			return memLimitFloorMB
		}
		return limit
	}
	cases := []struct {
		name    string
		totalMB int64
		want    int64
	}{
		// 0.40*8192 = 3276 → capped to ceiling 2560.
		{"8GB host capped", 8192, memLimitCeilingMB},
		// 0.40*65536 = 26214 → capped to ceiling 2560.
		{"64GB host capped", 65536, memLimitCeilingMB},
		// 0.40*4096 = 1638 → below floor → floor 2048.
		{"4GB host floored", 4096, memLimitFloorMB},
		// 0.40*6144 = 2457 → inside [2048,2560] band, untouched.
		{"6GB host in band", 6144, 2457},
	}
	for _, tc := range cases {
		if got := clamp(tc.totalMB); got != tc.want {
			t.Errorf("%s: clamp(%dMB)=%d want %d", tc.name, tc.totalMB, got, tc.want)
		}
	}
}

// TestMemLimitSummary_GoMemLimitWins asserts an explicit GOMEMLIMIT is
// reported with the runtime-env source and takes precedence over the default.
func TestMemLimitSummary_GoMemLimitWins(t *testing.T) {
	t.Setenv(memLimitEnv, "")
	t.Setenv("GOMEMLIMIT", "4GiB")
	mb, src := MemLimitSummary()
	if mb != 0 {
		t.Errorf("GOMEMLIMIT set: want mb=0 (unparsed) got %d", mb)
	}
	if !strings.Contains(src, "GOMEMLIMIT") || !strings.Contains(src, "4GiB") {
		t.Errorf("source %q should mention GOMEMLIMIT=4GiB", src)
	}
}

// TestMemLimitSummary_EnvOverrideThenDefault asserts the override and default
// flow through MemLimitSummary when no GOMEMLIMIT is set.
func TestMemLimitSummary_EnvOverrideThenDefault(t *testing.T) {
	t.Setenv("GOMEMLIMIT", "")
	t.Setenv(memLimitEnv, "1536")
	if mb, src := MemLimitSummary(); mb != 1536 || src != memLimitEnv {
		t.Errorf("override via summary: got mb=%d src=%q want 1536 / %q", mb, src, memLimitEnv)
	}
}

// TestApplyMemoryLimit_DoesNotPanic is a smoke test: applyMemoryLimit must
// run cleanly with a nil logger and an explicit override.
func TestApplyMemoryLimit_DoesNotPanic(t *testing.T) {
	// Save + restore the process-global soft limit so this test does not
	// leak a tight limit into sibling tests.
	prev := debug.SetMemoryLimit(-1) // -1 reads without changing
	debug.SetMemoryLimit(prev)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })

	t.Setenv("GOMEMLIMIT", "")    // don't defer to a real env limit
	t.Setenv(memLimitEnv, "8192") // 8GB soft limit
	applyMemoryLimit(nil)         // nil logger → slog.Default()
	if got := debug.SetMemoryLimit(-1); got != 8192*1024*1024 {
		t.Errorf("applyMemoryLimit(8192MB): runtime limit = %d, want %d", got, int64(8192)*1024*1024)
	}

	t.Setenv(memLimitEnv, "off") // disabled path must not change the limit
	applyMemoryLimit(nil)
	if got := debug.SetMemoryLimit(-1); got != 8192*1024*1024 {
		t.Errorf("disabled path changed the limit to %d", got)
	}
}
