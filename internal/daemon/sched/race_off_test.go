//go:build !race

package sched

// raceInstrumented reports whether this test binary was built with -race.
//
// It exists for TestChildMaxRSSMatchesKnownAllocation, which re-execs
// os.Args[0] — i.e. THIS binary, instrumentation and all — and then asserts a
// tight band on the child's ru_maxrss. ThreadSanitizer's shadow memory makes
// that child resident well beyond what the same code costs in production, so
// the band has to know which binary it is measuring.
const raceInstrumented = false
