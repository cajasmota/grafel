// symlink_perm_6240_test.go pins the three #6240 defects in the way this
// package rewrites a user's MCP host config.
//
// All three come out of one three-line writer (writeSettings, and its TOML twin
// writeRaw): write a temp file, rename it over the destination.
//
//  1. The rename replaces the LINK INODE. A ~/.claude.json symlinked into a
//     dotfiles repo silently detaches from that repo on the first `grafel
//     install`, and every later edit the user makes in either place stops being
//     seen by the other.
//  2. The destination inherits the TEMP file's mode. A 0600 ~/.claude.json —
//     which holds OAuth tokens and the full per-project history — comes back
//     0644, world-readable, with no diff to notice.
//  3. `servers, _ := doc["mcpServers"].(map[string]any)` swallows a failed type
//     assertion. A config whose mcpServers is an array (or a string, or a
//     number) loses that value entirely: servers becomes a fresh empty map and
//     the write puts it back as an object.
//
// Every pre-existing fixture in this package seeds 0o644 and never reads the
// mode back, so none of them can observe (2) at all — a 0644 seed is
// indistinguishable from the widened result. The fixtures here seed 0600 and a
// read-only 0444 deliberately.
package mcpreg

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

const testBin = "/usr/local/bin/grafel"

// requireSymlinks skips when the platform (Windows without the developer-mode
// or SeCreateSymbolicLink privilege) cannot create a symlink at all. It is a
// capability PROBE rather than a runtime.GOOS check so that a Windows runner
// which CAN make symlinks still runs these tests.
func requireSymlinks(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
}

// seedConfig writes a host config carrying a foreign server at mode perm.
func seedConfig(t *testing.T, path string, perm os.FileMode) []byte {
	t.Helper()
	body := []byte(`{
  "userIDOverride": "keep-me",
  "mcpServers": {
    "other": {
      "command": "/opt/other/bin/other"
    }
  }
}`)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, body, perm); err != nil {
		t.Fatalf("seed %s: %v", path, err)
	}
	// os.WriteFile's perm is umask-masked and does not apply to an existing
	// file, so state the mode explicitly.
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("chmod seed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o666) })
	return body
}

func assertGrafelRegistered(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s is not a JSON object: %v (%s)", path, err, raw)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if _, ok := servers[ServerName]; !ok {
		t.Fatalf("no grafel entry in %s: %s", path, raw)
	}
	if _, ok := servers["other"]; !ok {
		t.Errorf("foreign server lost from %s: %s", path, raw)
	}
}

// ── Defect 2: the destination's mode must survive the write ──────────────────

// TestRegisterPath_PreservesDestinationMode is the direct #6240 case: a user
// whose ~/.claude.json is 0600 (Claude Code's own default for a file holding
// OAuth tokens) must not find it 0644 after `grafel install`.
//
// The 0444 row is what gives this test any signal on Windows. Windows has no
// Unix mode bits — os.Stat reports 0666 for a writable file and 0444 for one
// carrying FILE_ATTRIBUTE_READONLY — so the 0600 and 0644 rows collapse into
// "writable" there and a lost chmod would sail straight past them. See
// testsupport.AssertPerm.
func TestRegisterPath_PreservesDestinationMode(t *testing.T) {
	for _, perm := range []os.FileMode{0o600, 0o644, 0o444} {
		t.Run(perm.String(), func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HOME", dir)
			cfg := filepath.Join(dir, ".claude.json")
			seedConfig(t, cfg, perm)

			if _, err := RegisterPath(cfg, testBin); err != nil {
				t.Fatalf("RegisterPath: %v", err)
			}
			testsupport.AssertPerm(t, cfg, perm)
			assertGrafelRegistered(t, cfg)
		})
	}
}

// TestRegisterPath_NewConfigIsPrivate pins the deliberate behaviour change: a
// config file grafel CREATES is 0600, not the 0644 the temp file used to leak
// in. These files carry credentials (tokens in ~/.claude.json) or at minimum a
// map of every project on the machine; there is no reason for another local
// account to be able to read one grafel invented.
func TestRegisterPath_NewConfigIsPrivate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg := filepath.Join(dir, ".cursor", "mcp.json")

	if _, err := RegisterPath(cfg, testBin); err != nil {
		t.Fatalf("RegisterPath: %v", err)
	}
	testsupport.AssertPerm(t, cfg, 0o600)
}

// ── Defect 1: a symlinked config must be written THROUGH ─────────────────────

// TestRegisterPath_WritesThroughSymlink is the dotfiles-repo case. It asserts
// ONLY the write-through half — the mode half is a separate test, because the
// two fixes are independent and one test asserting both can pass for the wrong
// reason.
func TestRegisterPath_WritesThroughSymlink(t *testing.T) {
	requireSymlinks(t)
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	real := filepath.Join(dir, "dotfiles", "claude.json")
	seedConfig(t, real, 0o644)
	cfg := filepath.Join(dir, ".claude.json")
	if err := os.Symlink(real, cfg); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := RegisterPath(cfg, testBin); err != nil {
		t.Fatalf("RegisterPath: %v", err)
	}

	fi, err := os.Lstat(cfg)
	if err != nil {
		t.Fatalf("lstat %s: %v", cfg, err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is no longer a symlink — the rename replaced the link inode "+
			"and the file has silently detached from the user's dotfiles repo", cfg)
	}
	// The entry must be in the TARGET, read directly, not merely through the
	// link (reading through the link would pass even if the link were replaced).
	assertGrafelRegistered(t, real)
}

// TestRegisterPath_SymlinkPreservesTargetMode is the other half: writing
// through the link must carry the TARGET's mode, not the temp file's. A fix
// that resolves the symlink but keeps os.WriteFile(tmp, b, 0o644) passes
// TestRegisterPath_WritesThroughSymlink and fails here.
func TestRegisterPath_SymlinkPreservesTargetMode(t *testing.T) {
	requireSymlinks(t)
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	real := filepath.Join(dir, "dotfiles", "claude.json")
	seedConfig(t, real, 0o600)
	cfg := filepath.Join(dir, ".claude.json")
	if err := os.Symlink(real, cfg); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := RegisterPath(cfg, testBin); err != nil {
		t.Fatalf("RegisterPath: %v", err)
	}
	testsupport.AssertPerm(t, real, 0o600)
}

// TestUnregisterPath_WritesThroughSymlinkAndKeepsMode: uninstall shares the
// writer, so it shares both defects. A user who installs and then uninstalls
// would otherwise end up with a detached, widened config either way.
func TestUnregisterPath_WritesThroughSymlinkAndKeepsMode(t *testing.T) {
	requireSymlinks(t)
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	real := filepath.Join(dir, "dotfiles", "claude.json")
	seedConfig(t, real, 0o600)
	cfg := filepath.Join(dir, ".claude.json")
	if err := os.Symlink(real, cfg); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := RegisterPath(cfg, testBin); err != nil {
		t.Fatalf("RegisterPath: %v", err)
	}
	if err := UnregisterPath(cfg); err != nil {
		t.Fatalf("UnregisterPath: %v", err)
	}

	fi, err := os.Lstat(cfg)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("UnregisterPath replaced the symlink at %s with a regular file", cfg)
	}
	testsupport.AssertPerm(t, real, 0o600)
}

// TestRegisterPath_SymlinkTargetIsGuarded covers the sandbox-escape hole the
// symlink fix opens: guardResolvedConfigPath is handed the UNRESOLVED path, so
// a link inside a t.TempDir() pointing at the real $HOME would let a
// badly-isolated test write there — the exact incident the guard exists to
// stop, re-entering through the back door.
//
// The canary target is a directory name that is not any real dotfile and is
// removed again on cleanup, so even a REGRESSION here cannot damage the
// developer's home.
func TestRegisterPath_SymlinkTargetIsGuarded(t *testing.T) {
	requireSymlinks(t)
	if realUserHomeAtInit == "" {
		t.Skip("no real home to guard against")
	}
	canaryDir := filepath.Join(realUserHomeAtInit, ".grafel-6240-guard-canary")
	t.Cleanup(func() { _ = os.RemoveAll(canaryDir) })

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg := filepath.Join(dir, ".claude.json")
	if err := os.Symlink(filepath.Join(canaryDir, "claude.json"), cfg); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("RegisterPath followed a symlink out of the test sandbox into the "+
				"REAL user home (%s) without tripping the isolation guard", canaryDir)
		}
	}()
	_, _ = RegisterPath(cfg, testBin)
}

// ── Unresolvable link chains must fail, not flatten ──────────────────────────

// seedSnapshotThroughLink registers through a symlink so a pristine sidecar
// exists, then rewires cfg into whatever pathological link shape the caller
// wants. It returns the pristine bytes.
//
// The sidecar keys on cfg (the caller's path, see
// TestBackupSidecar_KeysOnTheUnresolvedPath), so it survives the rewiring —
// which is what lets these tests drive RestoreSnapshot down its write path.
func seedSnapshotThroughLink(t *testing.T, dir, cfg string) {
	t.Helper()
	real := filepath.Join(dir, "dotfiles", "claude.json")
	seedConfig(t, real, 0o600)
	if err := os.Symlink(real, cfg); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := RegisterPath(cfg, testBin); err != nil {
		t.Fatalf("RegisterPath: %v", err)
	}
	if err := os.Remove(cfg); err != nil {
		t.Fatalf("unlink cfg: %v", err)
	}
}

// TestRestoreSnapshot_SymlinkCycleFailsLoudly is the review finding on the
// first #6240 fix: resolveWriteTarget's comment CLAIMED a link cycle "exhausts
// the hop budget and returns the last hop; the write then fails loudly on its
// own". It did not. It returned a mid-cycle path that is ITSELF a symlink, and
// writeThrough renamed over it — flattening the user's link into a regular file
// and returning nil.
//
// RegisterPath and UnregisterPath are saved from this by accident: backupOnce's
// os.ReadFile hits ELOOP first. RestoreSnapshot reaches the writer with no
// prior read, so it was the one live path — the exact damage this issue is
// about, inside the fix for it.
func TestRestoreSnapshot_SymlinkCycleFailsLoudly(t *testing.T) {
	requireSymlinks(t)
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg := filepath.Join(dir, ".claude.json")
	seedSnapshotThroughLink(t, dir, cfg)

	// a -> b -> a
	other := filepath.Join(dir, "loop-b.json")
	if err := os.Symlink(other, cfg); err != nil {
		t.Fatalf("symlink a->b: %v", err)
	}
	if err := os.Symlink(cfg, other); err != nil {
		t.Fatalf("symlink b->a: %v", err)
	}

	if err := RestoreSnapshot(cfg); err == nil {
		t.Error("RestoreSnapshot silently resolved a symlink CYCLE and wrote through it")
	}
	fi, err := os.Lstat(cfg)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s was flattened from a symlink into a regular file by a write "+
			"that could not resolve it", cfg)
	}
}

// TestRestoreSnapshot_OverlongSymlinkChainFailsLoudly is the Linux-only sibling
// the reviewer found: a chain longer than the hop budget is not a cycle, and
// returning the last hop reached is returning a SYMLINK, which the writer then
// replaces. Linux's MAXSYMLINKS is 40, so a 33-39 link chain the kernel
// resolves happily used to exhaust the old 32-hop budget; macOS stops at 32 and
// so answered ELOOP instead, hiding it. The budget now matches the most
// permissive kernel limit AND exhausting it is an error rather than a silent
// mid-chain write.
func TestRestoreSnapshot_OverlongSymlinkChainFailsLoudly(t *testing.T) {
	requireSymlinks(t)
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg := filepath.Join(dir, ".claude.json")
	seedSnapshotThroughLink(t, dir, cfg)

	linkChain(t, dir, cfg, filepath.Join(dir, "dotfiles", "claude.json"), 44)

	if err := RestoreSnapshot(cfg); err == nil {
		t.Error("RestoreSnapshot wrote through a link chain it could not fully resolve")
	}
	fi, err := os.Lstat(cfg)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s was flattened into a regular file mid-chain", cfg)
	}
}

// linkChain builds cfg -> hop(n-1) -> ... -> hop0 -> final and returns nothing;
// cfg must not already exist.
func linkChain(t *testing.T, dir, cfg, final string, n int) {
	t.Helper()
	prev := final
	for i := 0; i < n; i++ {
		hop := filepath.Join(dir, fmt.Sprintf("hop%03d", i))
		if err := os.Symlink(prev, hop); err != nil {
			t.Fatalf("symlink hop %d: %v", i, err)
		}
		prev = hop
	}
	if err := os.Symlink(prev, cfg); err != nil {
		t.Fatalf("symlink cfg: %v", err)
	}
}

// TestRestoreSnapshot_ChainWithinTheKernelLimitStillResolves pins the hop
// BUDGET, which the failure tests above cannot: a 44-link chain exhausts 32 and
// 40 alike, so they would stay green if the budget were narrowed back.
//
// 35 links is inside Linux's MAXSYMLINKS (40) and outside macOS's (32). The
// resolution here is grafel's own walk, not the kernel's, so this must succeed
// on BOTH — and it is exactly the band where the original 32-hop budget
// returned a mid-chain symlink and the writer flattened it.
func TestRestoreSnapshot_ChainWithinTheKernelLimitStillResolves(t *testing.T) {
	requireSymlinks(t)
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg := filepath.Join(dir, ".claude.json")
	seedSnapshotThroughLink(t, dir, cfg)

	final := filepath.Join(dir, "dotfiles", "claude.json")
	linkChain(t, dir, cfg, final, 35)

	if err := RestoreSnapshot(cfg); err != nil {
		t.Fatalf("RestoreSnapshot over a 35-link chain: %v", err)
	}
	if fi, err := os.Lstat(cfg); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s was flattened rather than written through", cfg)
	}
	// The restore must have landed on the END of the chain, not on some hop.
	for i := 0; i < 35; i++ {
		hop := filepath.Join(dir, fmt.Sprintf("hop%03d", i))
		fi, err := os.Lstat(hop)
		if err != nil {
			t.Fatalf("lstat hop %d: %v", i, err)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("hop %d was replaced by a regular file: the write landed "+
				"mid-chain instead of on the real target", i)
		}
	}
	if _, err := os.Stat(final); err != nil {
		t.Errorf("chain target %s missing: %v", final, err)
	}
}

// TestRegisterPath_SymlinkCycleFailsLoudly pins the same invariant on the
// install path, which reaches it only because backupOnce's read ELOOPs first.
// That is an accident of ordering, so it is stated as a requirement here rather
// than left to whichever read happens to come first next year.
func TestRegisterPath_SymlinkCycleFailsLoudly(t *testing.T) {
	requireSymlinks(t)
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg := filepath.Join(dir, ".claude.json")
	other := filepath.Join(dir, "loop-b.json")
	if err := os.Symlink(other, cfg); err != nil {
		t.Fatalf("symlink a->b: %v", err)
	}
	if err := os.Symlink(cfg, other); err != nil {
		t.Fatalf("symlink b->a: %v", err)
	}

	if _, err := RegisterPath(cfg, testBin); err == nil {
		t.Error("RegisterPath silently wrote through a symlink cycle")
	}
	fi, err := os.Lstat(cfg)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s was flattened from a symlink into a regular file", cfg)
	}
}

// TestRestoreSnapshot_SentinelRemovalIsGuarded pins the SECOND resolved-path
// guard, the one on RestoreSnapshot's absent-sentinel branch.
//
// That branch os.Removes the resolved target without going anywhere near
// writeThrough, so writeThrough's guard does not cover it: a dangling link out
// of a t.TempDir() into the real $HOME would let a badly-isolated test DELETE a
// file there. Reviewer finding LOW-2 was that the resolved-path guard existed
// in two places and no test pinned either individually; there are now two
// guards covering two distinct operations, and one test each.
func TestRestoreSnapshot_SentinelRemovalIsGuarded(t *testing.T) {
	requireSymlinks(t)
	if realUserHomeAtInit == "" {
		t.Skip("no real home to guard against")
	}
	canaryDir := filepath.Join(realUserHomeAtInit, ".grafel-6240-sentinel-canary")
	t.Cleanup(func() { _ = os.RemoveAll(canaryDir) })

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg := filepath.Join(dir, ".claude.json")
	// A sentinel sidecar: the original file did not exist.
	if err := os.WriteFile(sidecarBackupPath(cfg), []byte(backupSentinelAbsent), 0o600); err != nil {
		t.Fatalf("seed sidecar: %v", err)
	}
	if err := os.Symlink(filepath.Join(canaryDir, "claude.json"), cfg); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("RestoreSnapshot's sentinel branch followed a symlink out of the "+
				"test sandbox and would have deleted inside the REAL user home (%s)", canaryDir)
		}
	}()
	_ = RestoreSnapshot(cfg)
}

// ── Defect 3: a wrong-typed mcpServers must not be discarded ─────────────────

// TestRegisterPath_WrongTypedMCPServersFailsCleanly mirrors, one level down,
// the contract readSettings already enforces for a non-object DOCUMENT
// (nulldoc_6163_test.go): a value grafel cannot merge into is a real config
// whose replacement destroys something, so it must be an error AND must leave
// the file byte-identical.
func TestRegisterPath_WrongTypedMCPServersFailsCleanly(t *testing.T) {
	cases := []struct {
		body     string
		wantType string
	}{
		{`{"mcpServers": []}`, "array"},
		{`{"mcpServers": [{"name":"other"}]}`, "array"},
		{`{"mcpServers": "x"}`, "string"},
		{`{"mcpServers": 42}`, "number"},
		{`{"mcpServers": true}`, "boolean"},
	}
	for _, tc := range cases {
		t.Run(tc.body, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HOME", dir)
			cfg := filepath.Join(dir, ".claude.json")
			if err := os.WriteFile(cfg, []byte(tc.body), 0o600); err != nil {
				t.Fatalf("seed: %v", err)
			}

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("RegisterPath panicked on %s: %v", tc.body, r)
				}
			}()

			_, err := RegisterPath(cfg, testBin)
			if err == nil {
				t.Fatalf("RegisterPath silently discarded a %s mcpServers value", tc.wantType)
			}
			if !errors.Is(err, ErrMalformedConfig) {
				t.Errorf("error does not classify as ErrMalformedConfig: %v", err)
			}
			if msg := err.Error(); !strings.Contains(msg, ".claude.json") || !strings.Contains(msg, tc.wantType) {
				t.Errorf("error names neither the file nor the offending type: %q", msg)
			}

			got, err := os.ReadFile(cfg)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(got) != tc.body {
				t.Errorf("file was modified despite the error:\n got: %s\nwant: %s", got, tc.body)
			}
		})
	}
}

// TestRegisterPath_UnparseableConfigAlsoClassifies: the sentinel must cover the
// WHOLE "the user's file is broken" class, not just the branch #6240 added. A
// caller that wants to tell a broken config from a failed write — none does
// today, deliberately; see the RunCopy note in the commit — would otherwise get
// a classification that is right for one shape of breakage and silent for the
// other, which is worse than none.
func TestRegisterPath_UnparseableConfigAlsoClassifies(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg := filepath.Join(dir, ".claude.json")
	if err := os.WriteFile(cfg, []byte("{ not json"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := RegisterPath(cfg, testBin)
	if err == nil {
		t.Fatal("RegisterPath accepted unparseable JSON")
	}
	if !errors.Is(err, ErrMalformedConfig) {
		t.Errorf("unparseable JSON does not classify as ErrMalformedConfig: %v", err)
	}
}

// TestRegisterPath_NullMCPServersIsTreatedAsAbsent: `null` is the one non-object
// that carries nothing, so — exactly as for a null DOCUMENT (#6163) — it is
// treated as empty and registration proceeds rather than failing.
func TestRegisterPath_NullMCPServersIsTreatedAsAbsent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg := filepath.Join(dir, ".claude.json")
	if err := os.WriteFile(cfg, []byte(`{"mcpServers": null, "keep": 1}`), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := RegisterPath(cfg, testBin); err != nil {
		t.Fatalf("RegisterPath on a null mcpServers: %v", err)
	}
	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("not an object: %v", err)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if _, ok := servers[ServerName]; !ok {
		t.Errorf("no grafel entry written: %s", raw)
	}
	if doc["keep"] == nil {
		t.Errorf("unrelated key lost: %s", raw)
	}
}

// ── RestoreSnapshot must not re-widen what the write path preserved ──────────

// TestRestoreSnapshot_DoesNotWidenMode: RestoreSnapshot hardcoded 0o644. Without
// this, a rollback silently re-introduces defect 2 the moment the write path is
// fixed — the install preserves 0600 and the undo hands it back at 0644.
//
// The two rows are NOT redundant, and finding that out cost a mutant. os.WriteFile
// applies perm only when it CREATES the file, so on the "destination still
// there" row the old hardcoded 0644 was inert: the 0600 file was truncated in
// place and kept its mode. That row is a regression pin, not a fix witness.
// The witness is the "destination gone" row, where the restore CREATES the file
// and the hardcoded 0644 hands a credential-bearing config back world-readable.
func TestRestoreSnapshot_DoesNotWidenMode(t *testing.T) {
	for _, removeFirst := range []bool{false, true} {
		name := "destination still there"
		if removeFirst {
			name = "destination gone before the rollback"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HOME", dir)
			cfg := filepath.Join(dir, ".claude.json")
			pristine := seedConfig(t, cfg, 0o600)

			if _, err := RegisterPath(cfg, testBin); err != nil {
				t.Fatalf("RegisterPath: %v", err)
			}
			if removeFirst {
				if err := os.Remove(cfg); err != nil {
					t.Fatalf("remove: %v", err)
				}
			}
			if err := RestoreSnapshot(cfg); err != nil {
				t.Fatalf("RestoreSnapshot: %v", err)
			}
			testsupport.AssertPerm(t, cfg, 0o600)

			got, err := os.ReadFile(cfg)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(got) != string(pristine) {
				t.Errorf("restore was not byte-identical:\n got: %s\nwant: %s", got, pristine)
			}
		})
	}
}

// TestRestoreSnapshot_WritesThroughSymlink: the rollback path writes with
// os.WriteFile, which already follows the link — this pins that it stays that
// way, and that a read-only destination (which os.WriteFile CANNOT open) still
// restores.
func TestRestoreSnapshot_WritesThroughSymlink(t *testing.T) {
	requireSymlinks(t)
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	real := filepath.Join(dir, "dotfiles", "claude.json")
	pristine := seedConfig(t, real, 0o444)
	cfg := filepath.Join(dir, ".claude.json")
	if err := os.Symlink(real, cfg); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := RegisterPath(cfg, testBin); err != nil {
		t.Fatalf("RegisterPath: %v", err)
	}
	if err := RestoreSnapshot(cfg); err != nil {
		t.Fatalf("RestoreSnapshot on a read-only symlinked target: %v", err)
	}
	if fi, err := os.Lstat(cfg); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("RestoreSnapshot replaced the symlink at %s", cfg)
	}
	got, err := os.ReadFile(real)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(pristine) {
		t.Errorf("restore was not byte-identical:\n got: %s\nwant: %s", got, pristine)
	}
	testsupport.AssertPerm(t, real, 0o444)
}

// ── The Codex TOML writer has the identical defects ──────────────────────────

// TestRegisterPath_TOMLPreservesMode covers ~/.codex/config.toml, whose
// writeRaw is the same three lines as writeSettings. It was not named in the
// original report; it is the same defect in the same package.
func TestRegisterPath_TOMLPreservesMode(t *testing.T) {
	for _, perm := range []os.FileMode{0o600, 0o444} {
		t.Run(perm.String(), func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HOME", dir)
			cfg := filepath.Join(dir, ".codex", "config.toml")
			if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(cfg, []byte("[mcp_servers.other]\ncommand = \"x\"\n"), perm); err != nil {
				t.Fatalf("seed: %v", err)
			}
			if err := os.Chmod(cfg, perm); err != nil {
				t.Fatalf("chmod: %v", err)
			}
			t.Cleanup(func() { _ = os.Chmod(cfg, 0o666) })

			if _, err := RegisterPath(cfg, testBin); err != nil {
				t.Fatalf("RegisterPath: %v", err)
			}
			testsupport.AssertPerm(t, cfg, perm)
			raw, err := os.ReadFile(cfg)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if !strings.Contains(string(raw), "[mcp_servers.grafel]") {
				t.Errorf("no grafel table written: %s", raw)
			}
			if !strings.Contains(string(raw), "[mcp_servers.other]") {
				t.Errorf("foreign table lost: %s", raw)
			}
		})
	}
}

// TestRegisterPath_TOMLNewConfigIsPrivate: a Codex config grafel creates is
// 0600 for the same reason the JSON ones are.
func TestRegisterPath_TOMLNewConfigIsPrivate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg := filepath.Join(dir, ".codex", "config.toml")
	if _, err := RegisterPath(cfg, testBin); err != nil {
		t.Fatalf("RegisterPath: %v", err)
	}
	testsupport.AssertPerm(t, cfg, 0o600)
}

// TestRegisterPath_TOMLWritesThroughSymlink: ~/.codex/config.toml is as likely
// to be symlinked into a dotfiles repo as ~/.claude.json.
func TestRegisterPath_TOMLWritesThroughSymlink(t *testing.T) {
	requireSymlinks(t)
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	real := filepath.Join(dir, "dotfiles", "codex.toml")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(real, []byte("[mcp_servers.other]\ncommand = \"x\"\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg := filepath.Join(dir, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(real, cfg); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := RegisterPath(cfg, testBin); err != nil {
		t.Fatalf("RegisterPath: %v", err)
	}
	fi, err := os.Lstat(cfg)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is no longer a symlink", cfg)
	}
	raw, err := os.ReadFile(real)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !strings.Contains(string(raw), "[mcp_servers.grafel]") {
		t.Errorf("grafel table did not reach the symlink target: %s", raw)
	}
}

// ── The sidecar keys on the UNRESOLVED path ──────────────────────────────────

// TestBackupSidecar_KeysOnTheUnresolvedPath pins the decision made for #6240's
// trap: the pristine `.grafel.bak` sidecar stays next to the path the CALLER
// passed, not next to the symlink target.
//
// Every other user of that string — RestoreSnapshot, ClearBackup,
// install.discardStaleMCPBackups, and the uninstall sweep — is handed the same
// unresolved path from install state. If RegisterPath alone started keying on
// the resolved path, the snapshot would be taken in one place and looked for in
// another: a restore would find nothing (ErrNoSnapshot) and a stale sidecar
// would never be swept. That divergence IS the #6168 failure mode.
func TestBackupSidecar_KeysOnTheUnresolvedPath(t *testing.T) {
	requireSymlinks(t)
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	real := filepath.Join(dir, "dotfiles", "claude.json")
	seedConfig(t, real, 0o600)
	cfg := filepath.Join(dir, ".claude.json")
	if err := os.Symlink(real, cfg); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := RegisterPath(cfg, testBin); err != nil {
		t.Fatalf("RegisterPath: %v", err)
	}

	if _, err := os.Lstat(cfg + ".grafel.bak"); err != nil {
		t.Errorf("no sidecar beside the caller's path %s: %v", cfg, err)
	}
	if _, err := os.Lstat(real + ".grafel.bak"); err == nil {
		t.Errorf("sidecar was taken beside the symlink TARGET %s; RestoreSnapshot, "+
			"ClearBackup and the uninstall sweep all key on the caller's path and "+
			"would never find it there", real)
	}
	// And the round trip works: the restore keyed on the same caller path.
	if err := RestoreSnapshot(cfg); err != nil {
		t.Fatalf("RestoreSnapshot on the caller's path: %v", err)
	}
}
