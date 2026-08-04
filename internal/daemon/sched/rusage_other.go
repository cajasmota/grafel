//go:build !unix

package sched

import "os"

// maxRSSBytes has no portable equivalent off Unix: Go's os.ProcessState
// exposes no peak-working-set figure on Windows. ok=false means "no child peak
// available", and the run is then reported as peakSrcUnmeasured — there is no
// in-daemon sampler to fall back to, and a fabricated zero would be worse than
// saying nothing (#6107).
func maxRSSBytes(_ *os.ProcessState) (uint64, bool) { return 0, false }
