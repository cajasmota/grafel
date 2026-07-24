package extract

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/caps"
)

// installCapsFile writes a cpu.json into a temp dir, installs a Store pointing at
// it as the process-wide runtime caps, and registers cleanup that clears it.
func installCapsFile(t *testing.T, json string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, caps.FileName)
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatalf("write cpu.json: %v", err)
	}
	SetRuntimeCaps(caps.NewStore(path))
	t.Cleanup(func() { SetRuntimeCaps(nil) })
}

// TestRuntimeCaps_ExtractGOMAXPROCS_FileOverridesDefault: with no env set, the
// cpu.json value takes effect (the #5137 no-restart path).
func TestRuntimeCaps_ExtractGOMAXPROCS_FileOverridesDefault(t *testing.T) {
	installCapsFile(t, `{"extract_gomaxprocs": 5}`)
	if got := extractGOMAXPROCS(); got != 5 {
		t.Fatalf("extractGOMAXPROCS() = %d, want 5 (from cpu.json)", got)
	}
}

// TestRuntimeCaps_EnvBeatsFile: env var wins over cpu.json, which wins over the
// built-in default. This is the documented precedence.
func TestRuntimeCaps_EnvBeatsFile(t *testing.T) {
	installCapsFile(t, `{"extract_gomaxprocs": 5, "rebuild_gomaxprocs": 5, "extract_concurrency": 5}`)

	t.Setenv("GRAFEL_EXTRACT_GOMAXPROCS", "9")
	if got := extractGOMAXPROCS(); got != 9 {
		t.Fatalf("env should beat file: extractGOMAXPROCS() = %d, want 9", got)
	}

	t.Setenv("GRAFEL_REBUILD_GOMAXPROCS", "11")
	if got := rebuildGOMAXPROCS(); got != 11 {
		t.Fatalf("env should beat file: rebuildGOMAXPROCS() = %d, want 11", got)
	}

	t.Setenv("GRAFEL_EXTRACT_CONCURRENCY", "7")
	if got := (CoordinatorConfig{}).concurrency(); got != 7 {
		t.Fatalf("env should beat file: concurrency() = %d, want 7", got)
	}
}

// TestRuntimeCaps_FileConcurrency: cpu.json drives the subprocess fan-out when
// neither an explicit field nor the env var is set.
func TestRuntimeCaps_FileConcurrency(t *testing.T) {
	installCapsFile(t, `{"extract_concurrency": 3}`)
	if got := (CoordinatorConfig{}).concurrency(); got != 3 {
		t.Fatalf("concurrency() = %d, want 3 (from cpu.json)", got)
	}
	// Explicit field still wins over the file.
	if got := (CoordinatorConfig{Concurrency: 6}).concurrency(); got != 6 {
		t.Fatalf("explicit field should win over file: concurrency() = %d, want 6", got)
	}
}

// TestRuntimeCaps_RebuildFileOverride: cpu.json rebuild cap applies on the
// interactive path.
func TestRuntimeCaps_RebuildFileOverride(t *testing.T) {
	installCapsFile(t, `{"rebuild_gomaxprocs": 8}`)
	if got := rebuildGOMAXPROCS(); got != 8 {
		t.Fatalf("rebuildGOMAXPROCS() = %d, want 8 (from cpu.json)", got)
	}
	if got := (CoordinatorConfig{Interactive: true}).childGOMAXPROCS(); got != 8 {
		t.Fatalf("interactive childGOMAXPROCS() = %d, want 8 (from cpu.json)", got)
	}
}

// TestRuntimeCaps_NoStore_FallsThrough: with no store installed, resolution is
// exactly the pre-#5137 env→default behavior.
func TestRuntimeCaps_NoStore_FallsThrough(t *testing.T) {
	SetRuntimeCaps(nil)
	if got := extractGOMAXPROCS(); got != 1 {
		t.Fatalf("no store: extractGOMAXPROCS() = %d, want default 1 (#5960)", got)
	}
}
