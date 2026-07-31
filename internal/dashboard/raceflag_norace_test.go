//go:build !race

package dashboard

// raceEnabled reports whether this test binary was built with -race.
// See raceflag_race_test.go for why the distinction is needed.
const raceEnabled = false
