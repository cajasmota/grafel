//go:build !unix

package sched

import "os"

// maxRSSBytes has no portable equivalent off Unix: Go's os.ProcessState
// exposes no peak-working-set figure on Windows. Callers must treat ok=false
// as "no child peak available" and fall back to the in-daemon sampler rather
// than reporting a fabricated zero (#6107).
func maxRSSBytes(_ *os.ProcessState) (uint64, bool) { return 0, false }
