// refreshstate_test.go pins the narrow "record the binary that is actually on
// disk" operation that the curl installer needs.
//
// Background: install.sh places the new binary with `install -m 0755` and never
// runs `grafel install`, which is the only thing that writes
// ~/.grafel/install.json. The recorded CLI SHA therefore stays the PREVIOUS
// binary's forever, and RunQuickDoctor prints "binary updated since last
// install" on every single command until the user re-runs `grafel install` by
// hand.
//
// Running the full `grafel install` transaction from a curl installer is not an
// option (see the argument in refreshstate.go): steps 5 and 7 mutate whatever
// git repo the user's shell happened to be sitting in. RefreshState is the
// narrow alternative: it rewrites ONLY the CLI record of an EXISTING
// install.json and leaves every other field — skills manifest, MCP
// registration, gitignore record, install mode — exactly as it found them.
package install_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/install"
)

// writeFakeBinary writes a file with known content and returns its path.
func writeFakeBinary(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake binary %s: %v", p, err)
	}
	return p
}

// seedState writes an install.json that looks like a completed COPY install
// whose CLI record is stale (points at binPath but records an old SHA).
func seedState(t *testing.T, statePath, binPath string) *install.State {
	t.Helper()
	st := install.NewState(install.ModeCopy)
	st.CLI = install.CLIRecord{Path: binPath, SHA256: "0000000000000000000000000000000000000000000000000000000000000000"}
	st.Skills = map[string]install.SkillRecord{
		"grafel-tech-docs": {Files: map[string]string{"SKILL.md": "deadbeef"}},
	}
	st.MCP = install.MCPRecord{Name: "grafel", RegisteredPaths: []string{"/home/u/.claude.json"}}
	st.Gitignore = install.GitignoreRecord{Repos: []string{"/home/u/repo"}}
	st.DaemonVersion = "v0.1.7"
	if err := install.WriteState(statePath, st); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	return st
}

// TestRefreshState_RecordsOnDiskBinary is the core RED test: after a curl
// upgrade replaces the binary in place, RefreshState must make install.json
// agree with what is on disk.
func TestRefreshState_RecordsOnDiskBinary(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, ".grafel", "install.json")
	bin := writeFakeBinary(t, tmp, "grafel", "#!/bin/sh\necho v0.2.0\n")
	seedState(t, statePath, bin)

	res, err := install.RefreshState(install.RefreshOptions{BinPath: bin, StatePath: statePath})
	if err != nil {
		t.Fatalf("RefreshState: %v", err)
	}
	if !res.HadState {
		t.Error("HadState must be true when install.json exists")
	}
	if !res.Changed {
		t.Error("Changed must be true when the recorded SHA was stale")
	}

	got, err := install.ReadState(statePath)
	if err != nil || got == nil {
		t.Fatalf("ReadState after refresh: %v (state=%v)", err, got)
	}
	if got.CLI.Path != bin {
		t.Errorf("CLI.Path = %q, want %q", got.CLI.Path, bin)
	}
	if got.CLI.SHA256 == "" || got.CLI.SHA256 == "0000000000000000000000000000000000000000000000000000000000000000" {
		t.Errorf("CLI.SHA256 was not refreshed: %q", got.CLI.SHA256)
	}
	if got.CLI.SHA256 != res.SHA256 {
		t.Errorf("result SHA %q != persisted SHA %q", res.SHA256, got.CLI.SHA256)
	}
}

// TestRefreshState_CorrectsAWrongPath is the mutant a reviewer found surviving:
// every other test here seeds a state whose CLI.Path ALREADY equals the binary,
// so nothing proved RefreshState rewrites the PATH rather than just the SHA.
//
// That correction is load-bearing. It is the sole remedy for quick-doctor's
// "install.json records <path>, which no longer exists" warning — if it
// regressed, that warning would become permanent and unfixable, since running
// the prescribed command would leave the wrong path in place.
func TestRefreshState_CorrectsAWrongPath(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, ".grafel", "install.json")

	oldPath := filepath.Join(tmp, "old-prefix", "grafel")
	newBin := writeFakeBinary(t, tmp, "grafel", "relocated binary bytes")

	// State recorded at a prefix that no longer exists.
	st := install.NewState(install.ModeCopy)
	st.CLI = install.CLIRecord{Path: oldPath, SHA256: "1111111111111111111111111111111111111111111111111111111111111111"}
	if err := install.WriteState(statePath, st); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	res, err := install.RefreshState(install.RefreshOptions{BinPath: newBin, StatePath: statePath})
	if err != nil {
		t.Fatalf("RefreshState: %v", err)
	}
	if !res.Changed {
		t.Error("Changed must be true when the recorded PATH was wrong")
	}

	got, err := install.ReadState(statePath)
	if err != nil || got == nil {
		t.Fatalf("ReadState: %v", err)
	}
	if got.CLI.Path != newBin {
		t.Errorf("CLI.Path = %q, want %q — RefreshState must rewrite the recorded PATH, not only the SHA", got.CLI.Path, newBin)
	}
	if got.CLI.Path == oldPath {
		t.Errorf("CLI.Path is still the stale %q", oldPath)
	}
}

// TestRefreshState_PreservesInstalledAt: no install happened, so stamping the
// timestamp with "now" would make the field assert something false.
func TestRefreshState_PreservesInstalledAt(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, ".grafel", "install.json")
	bin := writeFakeBinary(t, tmp, "grafel", "new bytes")

	const originalInstall = "2020-01-02T03:04:05Z"
	st := install.NewState(install.ModeCopy)
	st.InstalledAt = originalInstall
	st.CLI = install.CLIRecord{Path: bin, SHA256: "2222222222222222222222222222222222222222222222222222222222222222"}
	if err := install.WriteState(statePath, st); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	if _, err := install.RefreshState(install.RefreshOptions{BinPath: bin, StatePath: statePath}); err != nil {
		t.Fatalf("RefreshState: %v", err)
	}

	got, err := install.ReadState(statePath)
	if err != nil || got == nil {
		t.Fatalf("ReadState: %v", err)
	}
	if got.InstalledAt != originalInstall {
		t.Errorf("InstalledAt = %q, want it preserved as %q — no install happened here", got.InstalledAt, originalInstall)
	}
}

// TestRefreshState_ClearsTheInvalidatedDaemonVersion: DaemonVersion is the one
// field an in-place binary swap definitely invalidates. install.sh replaces the
// binary and then restarts the daemon, so the recorded version describes a
// process that no longer exists and checkDaemon would report a version mismatch
// on every `grafel doctor`. checkDaemon early-returns on "", so dropping a
// known-wrong value is strictly better than preserving it.
func TestRefreshState_ClearsTheInvalidatedDaemonVersion(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, ".grafel", "install.json")
	bin := writeFakeBinary(t, tmp, "grafel", "upgraded bytes")
	seedState(t, statePath, bin) // seeds DaemonVersion = v0.1.7

	if _, err := install.RefreshState(install.RefreshOptions{BinPath: bin, StatePath: statePath}); err != nil {
		t.Fatalf("RefreshState: %v", err)
	}

	got, err := install.ReadState(statePath)
	if err != nil || got == nil {
		t.Fatalf("ReadState: %v", err)
	}
	if got.DaemonVersion != "" {
		t.Errorf("DaemonVersion = %q, want it cleared — the recorded value describes the pre-upgrade daemon", got.DaemonVersion)
	}
}

// TestRefreshState_PreservesEverythingElse is the blast-radius pin: refreshing
// the CLI record must NOT clobber the rest of the install transaction's record.
// If this ever regresses, `grafel doctor` would start reporting a fully
// installed system as having no skills and no MCP registration.
func TestRefreshState_PreservesEverythingElse(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, ".grafel", "install.json")
	bin := writeFakeBinary(t, tmp, "grafel", "new binary bytes")
	before := seedState(t, statePath, bin)

	if _, err := install.RefreshState(install.RefreshOptions{BinPath: bin, StatePath: statePath}); err != nil {
		t.Fatalf("RefreshState: %v", err)
	}

	after, err := install.ReadState(statePath)
	if err != nil || after == nil {
		t.Fatalf("ReadState: %v", err)
	}
	if after.InstallMode != before.InstallMode {
		t.Errorf("InstallMode changed: %q -> %q", before.InstallMode, after.InstallMode)
	}
	if len(after.Skills) != len(before.Skills) {
		t.Errorf("Skills manifest changed: %v -> %v", before.Skills, after.Skills)
	}
	if rec, ok := after.Skills["grafel-tech-docs"]; !ok || rec.Files["SKILL.md"] != "deadbeef" {
		t.Errorf("skill record lost or altered: %+v", after.Skills)
	}
	if after.MCP.Name != "grafel" || len(after.MCP.RegisteredPaths) != 1 {
		t.Errorf("MCP record changed: %+v", after.MCP)
	}
	if len(after.Gitignore.Repos) != 1 || after.Gitignore.Repos[0] != "/home/u/repo" {
		t.Errorf("Gitignore record changed: %+v", after.Gitignore)
	}
	if after.InstalledAt != before.InstalledAt {
		t.Errorf("InstalledAt changed: %q -> %q", before.InstalledAt, after.InstalledAt)
	}
	// DaemonVersion is the deliberate exception — see
	// TestRefreshState_ClearsTheInvalidatedDaemonVersion.
	if after.SchemaVersion != install.StateSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", after.SchemaVersion, install.StateSchemaVersion)
	}
}

// TestRefreshState_NoInstallJSONIsASilentNoOp: a machine that has never run
// `grafel install` has no install.json, and quick-doctor is silent in that
// state. RefreshState must NOT fabricate one — a synthetic state with no
// skills and no MCP record would make `grafel doctor` start reporting drift
// that does not exist.
func TestRefreshState_NoInstallJSONIsASilentNoOp(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, ".grafel", "install.json")
	bin := writeFakeBinary(t, tmp, "grafel", "fresh install")

	res, err := install.RefreshState(install.RefreshOptions{BinPath: bin, StatePath: statePath})
	if err != nil {
		t.Fatalf("RefreshState with no install.json must not error: %v", err)
	}
	if res.HadState {
		t.Error("HadState must be false when install.json is absent")
	}
	if res.Changed {
		t.Error("Changed must be false when install.json is absent")
	}
	if _, statErr := os.Stat(statePath); !os.IsNotExist(statErr) {
		t.Errorf("RefreshState must not create install.json out of nothing (stat err = %v)", statErr)
	}
}

// TestRefreshState_IdempotentWhenAlreadyCurrent: the second run of the curl
// installer over an unchanged binary must report no change (and must still not
// error), so the installer can call it unconditionally.
func TestRefreshState_IdempotentWhenAlreadyCurrent(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, ".grafel", "install.json")
	bin := writeFakeBinary(t, tmp, "grafel", "same bytes")
	seedState(t, statePath, bin)

	if _, err := install.RefreshState(install.RefreshOptions{BinPath: bin, StatePath: statePath}); err != nil {
		t.Fatalf("first RefreshState: %v", err)
	}
	res, err := install.RefreshState(install.RefreshOptions{BinPath: bin, StatePath: statePath})
	if err != nil {
		t.Fatalf("second RefreshState: %v", err)
	}
	if !res.HadState {
		t.Error("HadState must stay true")
	}
	if res.Changed {
		t.Error("Changed must be false on a no-op refresh")
	}
}

// TestRefreshState_MissingBinaryIsAnError: a caller that points at a binary
// that is not there has a real problem; do not silently write a state with an
// empty SHA (which checkCLI reports as a CRITICAL partial install).
func TestRefreshState_MissingBinaryIsAnError(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, ".grafel", "install.json")
	bin := writeFakeBinary(t, tmp, "grafel", "x")
	seedState(t, statePath, bin)

	missing := filepath.Join(tmp, "does-not-exist")
	if _, err := install.RefreshState(install.RefreshOptions{BinPath: missing, StatePath: statePath}); err == nil {
		t.Fatal("RefreshState must error when BinPath does not exist")
	}

	got, err := install.ReadState(statePath)
	if err != nil || got == nil {
		t.Fatalf("ReadState: %v", err)
	}
	if got.CLI.SHA256 == "" {
		t.Error("a failed refresh must leave the previous CLI record intact, not blank it")
	}
}
