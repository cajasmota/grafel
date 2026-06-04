package daemon

import (
	"runtime/debug"
	"testing"

	"github.com/cajasmota/archigraph/internal/process"
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

// TestResolveMemLimitMB_DefaultFractionOrFloor asserts the default is a
// fraction of system RAM, never below the floor.
func TestResolveMemLimitMB_DefaultFractionOrFloor(t *testing.T) {
	t.Setenv(memLimitEnv, "") // ensure no override
	mb, src := resolveMemLimitMB()
	if mb < memLimitFloorMB {
		t.Errorf("default limit %d below floor %d (src=%s)", mb, memLimitFloorMB, src)
	}
	// If the host reports total RAM, the default must be ~fraction of it
	// (and at least the floor).
	if total := process.TotalMemoryMB(); total > 0 {
		want := int64(float64(total) * memLimitFraction)
		if want < memLimitFloorMB {
			want = memLimitFloorMB
		}
		if mb != want {
			t.Errorf("default limit: want %d (%.0f%% of %dMB, floored) got %d",
				want, memLimitFraction*100, total, mb)
		}
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
