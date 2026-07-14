//go:build darwin

package process

import (
	"math"
	"os"
	"testing"
)

// TestParseCPUTime covers every `ps -o cputime` shape the darwin
// CPUTimeSeconds reader must handle: MM:SS[.ss], HH:MM:SS, and DD-HH:MM:SS.
func TestParseCPUTime(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"0:00.00", 0},
		{"0:00.42", 0.42},
		{"12:34", 12*60 + 34},
		{"1:02:03", 1*3600 + 2*60 + 3},
		{"1-02:03:04", 1*86400 + 2*3600 + 3*60 + 4},
	}
	for _, c := range cases {
		got, err := parseCPUTime(c.in)
		if err != nil {
			t.Errorf("parseCPUTime(%q): %v", c.in, err)
			continue
		}
		if math.Abs(got-c.want) > 1e-6 {
			t.Errorf("parseCPUTime(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestCPUTimeSeconds_CurrentProcess smoke-tests that the darwin reader returns
// a non-negative cumulative time for the live process.
func TestCPUTimeSeconds_CurrentProcess(t *testing.T) {
	secs, err := CPUTimeSeconds(os.Getpid())
	if err != nil {
		t.Fatalf("CPUTimeSeconds: %v", err)
	}
	if secs < 0 {
		t.Errorf("CPUTimeSeconds = %v, want >= 0", secs)
	}
}
