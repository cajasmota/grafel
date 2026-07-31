package membench

import (
	"os"
	"strconv"
)

// specFromEnv returns the fixture spec, scaled down by default so a plain
// `go test ./internal/membench` runs in bounded time/heap, but overridable to
// the full #5681 monorepo scale via GRAFEL_MEMBENCH_ENTITIES=220000 (and the
// companion knobs) for the real measurement run.
func specFromEnv() FixtureSpec {
	s := FixtureSpec{
		Entities:         40_000,
		Files:            4_000,
		CallEdges:        200_000,
		ImportsPerFile:   16,
		ExternalPackages: 2_000,
		Seed:             0x5681,
	}
	if v := os.Getenv("GRAFEL_MEMBENCH_ENTITIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			s = DefaultLargeSpec()
			s.Entities = n
			// Scale companion counts proportionally to keep a realistic density.
			s.Files = n / 12
			s.CallEdges = n * 5
		}
	}
	return s
}
