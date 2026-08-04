//go:build race

package sched

// raceInstrumented reports whether this test binary was built with -race.
// See the !race variant for why the peak-RSS band needs to know.
const raceInstrumented = true
