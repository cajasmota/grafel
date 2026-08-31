//go:build unix

package fitness

// fifo_6478_test.go — deadline pin for the .grafel/fitness.yaml read
// (docs/blocking-open-audit.md, "group/config json" family).
//
// LoadConfig joins DefaultConfigName onto a stateDir that is the USER repo's
// .grafel directory, so `mkfifo .grafel/fitness.yaml` inside any indexed repo
// parked the caller in open(2) permanently: no timeout, no error, no log line.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

func TestLoadConfigFIFODoesNotHang(t *testing.T) {
	dir := t.TempDir()
	testsupport.MkfifoInTemp(t, dir, DefaultConfigName)

	var cfg *Config
	var err error
	testsupport.MustReturn(t, "fitness.LoadConfig with a FIFO named "+DefaultConfigName, func() {
		cfg, err = LoadConfig(dir)
	})

	// The refusal must be LOUD. Mapping ErrNotRegular to an empty config would
	// close the hang and keep the silence, which is the exact half-fix rounds 2
	// and 3 of #6416 shipped (docs/blocking-open-audit.md, "Routing is not
	// enough on its own").
	if err == nil {
		t.Fatalf("LoadConfig returned cfg=%+v and a nil error for a FIFO; a refused config file must "+
			"be reported, not silently treated as absent", cfg)
	}
}

// TestLoadConfigStillReadsARegularFile is the positive control. Without it a
// LoadConfig that refused EVERYTHING would pass the test above.
func TestLoadConfigStillReadsARegularFile(t *testing.T) {
	dir := t.TempDir()
	writeFitnessFixture(t, dir)

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig on a regular file: %v", err)
	}
	if cfg == nil || len(cfg.Rules) == 0 {
		t.Fatalf("LoadConfig parsed no rules from a valid config; the guard is refusing regular files")
	}
}

func writeFitnessFixture(t *testing.T, dir string) {
	t.Helper()
	const src = "rules:\n  - name: no cycles\n    threshold: 'import_cycles.count == 0'\n"
	if err := os.WriteFile(filepath.Join(dir, DefaultConfigName), []byte(src), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
