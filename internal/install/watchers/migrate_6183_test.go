package watchers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordingLoader records Unload/Load calls by LABEL instead of touching the
// real OS scheduler. Nothing in this file ever runs launchctl / systemctl /
// schtasks.
type recordingLoader struct {
	unloaded []string
	loaded   []string
	made     int
}

func (r *recordingLoader) Load(u Unit) error { r.loaded = append(r.loaded, u.Label()); return nil }
func (r *recordingLoader) Unload(u Unit) error {
	r.unloaded = append(r.unloaded, u.Label())
	return nil
}
func (r *recordingLoader) Status(u Unit) (WatcherStatus, error) {
	return WatcherStatus{TaskName: u.Label()}, nil
}

func (r *recordingLoader) ctor() Loader { r.made++; return r }

// migrateSandbox points every unit-directory derivation at a temp dir.
func migrateSandbox(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
}

// TestMigrateLegacyUnit_BootsOutThenRemoves is the migration half of #6183.
//
// Changing Label orphans every installed unit: the old file stays on disk AND
// stays loaded under the old identity. If migration only deleted the file, the
// job would remain registered with launchd with nothing left to name it, and
// the new label would register alongside it — two watchers per repo, strictly
// worse than the collision. The deregistration must therefore happen, and it
// must happen while the file still exists.
func TestMigrateLegacyUnit_BootsOutThenRemoves(t *testing.T) {
	migrateSandbox(t)
	u := Unit{Group: "g", Repo: filepath.Join(t.TempDir(), "api"), BinPath: "/bin/grafel"}

	legacyPath, err := LegacyUnitPath(u)
	if err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	if _, err := Write(LegacyOf(u)); err != nil {
		t.Fatal(err)
	}

	rec := &recordingLoader{}
	removed, err := MigrateLegacyUnit(u, rec.ctor)
	if err != nil {
		t.Fatal(err)
	}
	if removed != legacyPath {
		t.Fatalf("removed = %q, want %q", removed, legacyPath)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy unit file survived migration: %v", err)
	}
	if len(rec.unloaded) != 1 || rec.unloaded[0] != LegacyOf(u).Label() {
		t.Fatalf("migration did not deregister the OLD label; unloaded=%v want [%s]",
			rec.unloaded, LegacyOf(u).Label())
	}
	if rec.unloaded[0] == u.Label() {
		t.Fatalf("migration deregistered the NEW label")
	}
}

// TestMigrateLegacyUnit_IsIdempotentAndLoaderFree pins the two properties that
// make it safe to call on every install and every reconcile: running it twice
// changes nothing, and on an already-migrated machine it constructs no Loader
// at all — which is what preserves #6179's "zero launchctl on an up-to-date
// machine".
func TestMigrateLegacyUnit_IsIdempotentAndLoaderFree(t *testing.T) {
	migrateSandbox(t)
	u := Unit{Group: "g", Repo: filepath.Join(t.TempDir(), "api"), BinPath: "/bin/grafel"}
	if _, err := UnitPath(u); err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	if _, err := Write(LegacyOf(u)); err != nil {
		t.Fatal(err)
	}

	rec := &recordingLoader{}
	if _, err := MigrateLegacyUnit(u, rec.ctor); err != nil {
		t.Fatal(err)
	}
	afterFirst := rec.made

	for i := 2; i <= 3; i++ {
		removed, err := MigrateLegacyUnit(u, rec.ctor)
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if removed != "" {
			t.Fatalf("run %d removed %q; migration is not idempotent", i, removed)
		}
	}
	if rec.made != afterFirst {
		t.Fatalf("Loader constructed %d times, %d after the first run — an already-migrated "+
			"machine must construct none", rec.made, afterFirst)
	}
	if len(rec.unloaded) != 1 {
		t.Fatalf("Unload called %d times, want 1", len(rec.unloaded))
	}
}

// TestMigrateLegacyUnit_NoLegacyFileTouchesNothing covers the fresh install and
// the already-migrated fleet: no legacy unit, so no loader and no filesystem
// mutation. The live unit in particular must survive.
func TestMigrateLegacyUnit_NoLegacyFileTouchesNothing(t *testing.T) {
	migrateSandbox(t)
	u := Unit{Group: "g", Repo: filepath.Join(t.TempDir(), "api"), BinPath: "/bin/grafel"}
	current, err := Write(u)
	if err != nil {
		t.Skipf("unsupported OS: %v", err)
	}

	rec := &recordingLoader{}
	removed, err := MigrateLegacyUnit(u, rec.ctor)
	if err != nil {
		t.Fatal(err)
	}
	if removed != "" {
		t.Fatalf("removed %q with no legacy unit present", removed)
	}
	if rec.made != 0 {
		t.Fatalf("constructed a Loader with nothing to migrate")
	}
	if _, err := os.Stat(current); err != nil {
		t.Fatalf("migration deleted the LIVE unit: %v", err)
	}
}

// TestMigrateLegacyUnit_LeavesOtherReposAlone: the migration names the unit it
// retires by deriving the old label from the same (group, repo) as the new one.
// It can therefore never reach a unit belonging to a different repo or group —
// including groups this binary has no config for.
func TestMigrateLegacyUnit_LeavesOtherReposAlone(t *testing.T) {
	migrateSandbox(t)
	base := t.TempDir()
	mine := Unit{Group: "g", Repo: filepath.Join(base, "api"), BinPath: "/bin/grafel"}
	otherRepo := Unit{Group: "g", Repo: filepath.Join(base, "web"), BinPath: "/bin/grafel"}
	otherGroup := Unit{Group: "unregistered", Repo: filepath.Join(base, "api"), BinPath: "/bin/grafel"}

	if _, err := Write(LegacyOf(mine)); err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	otherRepoPath, _ := Write(LegacyOf(otherRepo))
	otherGroupPath, _ := Write(LegacyOf(otherGroup))

	rec := &recordingLoader{}
	if _, err := MigrateLegacyUnit(mine, rec.ctor); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{otherRepoPath, otherGroupPath} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("migration removed an unrelated unit %s: %v", p, err)
		}
	}
	if len(rec.unloaded) != 1 {
		t.Fatalf("unloaded %v; migration must deregister exactly its own old label", rec.unloaded)
	}
}

// TestCleanup_RemovesLegacyUnitToo: Cleanup runs when a repo stops being
// registered, and it is the last moment at which the pre-#6183 label can be
// derived at all — once the group leaves the registry nothing knows the repo
// path any more. A legacy plist left behind here would stay loaded forever with
// no way to name it.
func TestCleanup_RemovesLegacyUnitToo(t *testing.T) {
	migrateSandbox(t)
	stubLaunchctl(t)
	rec := &recordingLoader{}
	prev := newLoader
	newLoader = rec.ctor
	t.Cleanup(func() { newLoader = prev })

	u := Unit{Group: "demo", Repo: filepath.Join(t.TempDir(), "core"), BinPath: "/bin/grafel"}

	legacyPath, err := Write(LegacyOf(u))
	if err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	currentPath, _ := Write(u)

	Cleanup(u.Group, u.Repo, u.BinPath)

	for _, p := range []string{legacyPath, currentPath} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("Cleanup left %s behind: %v", p, err)
		}
	}
	// Deleting the file does not deregister the job. Both labels must have been
	// booted out — and after this point neither can be derived again.
	want := map[string]bool{u.Label(): false, LegacyOf(u).Label(): false}
	for _, l := range rec.unloaded {
		want[l] = true
	}
	for label, seen := range want {
		if !seen {
			t.Fatalf("Cleanup never deregistered %s; unloaded=%v", label, rec.unloaded)
		}
	}
}

// statLoader records, at Unload time, whether the unit file still exists.
type statLoader struct {
	recordingLoader
	path             string
	fileAtUnloadTime bool
}

func (s *statLoader) Unload(u Unit) error {
	if _, err := os.Stat(s.path); err == nil {
		s.fileAtUnloadTime = true
	}
	return s.recordingLoader.Unload(u)
}

func (s *statLoader) ctor() Loader { s.made++; return s }

// TestMigrateLegacyUnit_DeregistersBeforeDeleting pins the ordering.
//
// Deregistration has to happen while the unit file is still there. systemctl
// --user disable resolves the unit by reading the file, so disabling after
// deletion leaves the enablement state dangling and the job still wired up; on
// launchd the plist is what a path-form bootout names. Deleting first is the
// natural-looking order and it is wrong on both.
func TestMigrateLegacyUnit_DeregistersBeforeDeleting(t *testing.T) {
	migrateSandbox(t)
	u := Unit{Group: "g", Repo: filepath.Join(t.TempDir(), "api"), BinPath: "/bin/grafel"}
	legacyPath, err := Write(LegacyOf(u))
	if err != nil {
		t.Skipf("unsupported OS: %v", err)
	}

	sl := &statLoader{path: legacyPath}
	if _, err := MigrateLegacyUnit(u, sl.ctor); err != nil {
		t.Fatal(err)
	}
	if len(sl.unloaded) != 1 {
		t.Fatalf("Unload called %d times, want 1", len(sl.unloaded))
	}
	if !sl.fileAtUnloadTime {
		t.Fatalf("the legacy unit file was already deleted when Unload ran; " +
			"deregistration must happen first")
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy unit file survived: %v", err)
	}
}

// ── The pre-slash-normalisation label ────────────────────────────────────────

// windowsClean is filepath.Clean's Windows behaviour, spelled for a host that
// is not Windows: it is what makes the superseded derivation reachable — and
// therefore testable — from darwin and linux.
func windowsClean(s string) string {
	return strings.ReplaceAll(filepath.Clean(s), "/", `\`)
}

// TestMigrateNativeDigestUnit_BootsOutThenRemoves is the migration half of the
// slash normalisation, and it is the same defect as #6183 one derivation later.
//
// pathDigest now hashes the SLASH-NORMALISED path, so a Windows machine's units
// — hashed over filepath.Clean's backslash form — are all renamed by an
// upgrade. Nothing else in the tree can derive the old name: reconcile stats
// the current and pre-#6183 labels only, finds neither, counts the repo Absent
// and does nothing, while the old `.xml` and (worse) its REGISTERED SCHEDULED
// TASK stay put and keep spawning `grafel watch`. Then install writes the new
// label beside it. Two watchers per repo.
func TestMigrateNativeDigestUnit_BootsOutThenRemoves(t *testing.T) {
	migrateSandbox(t)
	restore := SetNativeCleanForTest(windowsClean)
	t.Cleanup(restore)

	u := Unit{Group: "g", Repo: filepath.Join(t.TempDir(), "api"), BinPath: "/bin/grafel"}
	old := NativeDigestOf(u)
	if old.Label() == u.Label() {
		t.Fatalf("precondition: the superseded label must differ from the current one (%q)", u.Label())
	}
	oldPath, err := NativeDigestUnitPath(u)
	if err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	writeRawUnit(t, oldPath)

	rec := &recordingLoader{}
	removed, err := MigrateNativeDigestUnit(u, rec.ctor)
	if err != nil {
		t.Fatal(err)
	}
	if removed != oldPath {
		t.Fatalf("removed = %q, want %q", removed, oldPath)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("the superseded unit file survived: %v", err)
	}
	// Deleting the file is NOT enough: on Windows the scheduled task is
	// registered under the label, independently of the .xml, and only Unload
	// (schtasks /delete /tn <label>) removes it.
	if len(rec.unloaded) != 1 || rec.unloaded[0] != old.Label() {
		t.Fatalf("unloaded = %v, want exactly [%s] — the scheduled task outlives the file",
			rec.unloaded, old.Label())
	}
}

// TestMigrateNativeDigestUnit_IsANoOpWhereTheDerivationsCoincide: off Windows
// the superseded and current derivations are the same string, and a migration
// that did not notice would boot out and delete the LIVE unit on every install.
func TestMigrateNativeDigestUnit_IsANoOpWhereTheDerivationsCoincide(t *testing.T) {
	migrateSandbox(t)
	u := Unit{Group: "g", Repo: filepath.Join(t.TempDir(), "api"), BinPath: "/bin/grafel"}
	path, err := Write(u)
	if err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	if NativeDigestOf(u).Label() != u.Label() {
		t.Skip("this host's native clean differs from the slash form; covered by the migration test")
	}

	rec := &recordingLoader{}
	removed, err := MigrateNativeDigestUnit(u, rec.ctor)
	if err != nil {
		t.Fatal(err)
	}
	if removed != "" {
		t.Fatalf("removed %q — that is the repo's LIVE unit", removed)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the live unit was deleted: %v", err)
	}
	if rec.made != 0 || len(rec.unloaded) != 0 {
		t.Fatalf("constructed %d loaders and unloaded %v; a coincident derivation must cost nothing",
			rec.made, rec.unloaded)
	}
}

// writeRawUnit writes a placeholder unit file at path. The superseded unit is
// addressed by RawLabel, which Unit documents as unusable for Write/Render, so
// the file is created directly rather than through Write.
func writeRawUnit(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("superseded unit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
