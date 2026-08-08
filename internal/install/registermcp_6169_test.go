// registermcp_6169_test.go pins the missing half of the curl install (#6169).
//
// install.sh downloads, checksum-verifies and places the binary, then runs
// `grafel install --refresh-state`, `grafel doctor` and restart_daemon. Not one
// of those registers the MCP server. The ONLY two registration sites in the
// tree are RunCopy step 3 and ApplyToolDelta, and install.sh reaches neither —
// so after a first-ever curl install the user has a binary on PATH, no grafel
// entry in ~/.claude.json, and therefore no product: grafel's entire interface
// is the MCP server.
//
// RegisterMCP is the narrow operation that closes that gap under the same
// discipline as RefreshState: it writes MCP host configs and install.json and
// NOTHING else — no daemon restart, no skills copy, no .gitignore, no git
// hooks, no TTY prompt. These tests hold it to that.
package install

import (
	"encoding/json"
	"github.com/cajasmota/grafel/internal/testsupport"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedFakeBinary writes an executable stand-in and returns its path. RegisterMCP
// hashes the binary for the install.json CLI record, so it has to be a real file.
func seedFakeBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "grafel")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	return p
}

// readMCPServers returns the mcpServers map of a JSON host config.
func readMCPServers(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		t.Fatalf("%s has no mcpServers object: %s", path, raw)
	}
	return servers
}

// grafelCommandIn returns the `command` recorded for the grafel MCP entry.
func grafelCommandIn(t *testing.T, path string) string {
	t.Helper()
	entry, ok := readMCPServers(t, path)["grafel"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no grafel MCP entry", path)
	}
	cmd, _ := entry["command"].(string)
	return cmd
}

// TestRegisterMCP_FirstEverInstallRegistersAndRecords is #6169 itself.
//
// Starting state is a machine that has never run `grafel install`: no
// install.json, no ~/.claude.json. After RegisterMCP the config must carry a
// grafel entry pointing at the binary we were given, AND install.json must
// record that path — otherwise `grafel uninstall` (which deregisters strictly
// from state.MCP.RegisteredPaths) can never remove what we just wrote.
func TestRegisterMCP_FirstEverInstallRegistersAndRecords(t *testing.T) {
	home := testsupport.IsolateHome(t)

	bin := seedFakeBinary(t)
	cfg := filepath.Join(home, ".claude.json")
	statePath := filepath.Join(home, ".grafel", "install.json")

	if _, err := os.Stat(cfg); !os.IsNotExist(err) {
		t.Fatalf("precondition: %s must not exist yet (err=%v)", cfg, err)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("precondition: %s must not exist yet (err=%v)", statePath, err)
	}

	res, err := RegisterMCP(RegisterMCPOptions{
		BinPath:          bin,
		StatePath:        statePath,
		ClaudeConfigDirs: []string{cfg},
	})
	if err != nil {
		t.Fatalf("RegisterMCP: %v", err)
	}

	if got := grafelCommandIn(t, cfg); got != bin {
		t.Errorf("grafel MCP command = %q, want %q", got, bin)
	}
	if len(res.Registered) != 1 || res.Registered[0] != cfg {
		t.Errorf("Registered = %v, want [%s]", res.Registered, cfg)
	}
	if !res.CreatedState {
		t.Errorf("CreatedState = false; a first-ever install must record itself")
	}

	st, err := ReadState(statePath)
	if err != nil || st == nil {
		t.Fatalf("ReadState after RegisterMCP: st=%v err=%v", st, err)
	}
	if !containsString(st.MCP.RegisteredPaths, cfg) {
		t.Errorf("install.json MCP.RegisteredPaths = %v, want to contain %s "+
			"(uninstall deregisters only from this list)", st.MCP.RegisteredPaths, cfg)
	}
	if st.CLI.Path != bin || st.CLI.SHA256 == "" {
		t.Errorf("install.json CLI = %+v, want path %s with a checksum", st.CLI, bin)
	}
	if !st.SkillsSkipped {
		t.Errorf("SkillsSkipped = false; RegisterMCP copies no skills and must say so " +
			"rather than let doctor read an empty manifest as a complete install")
	}
}

// TestRegisterMCP_PreservesForeignServersAndKeys: the host config belongs to
// the user, not to us. A curl installer that clobbered a hand-written MCP
// server list would be a worse bug than the one being fixed.
func TestRegisterMCP_PreservesForeignServersAndKeys(t *testing.T) {
	home := testsupport.IsolateHome(t)

	bin := seedFakeBinary(t)
	cfg := filepath.Join(home, ".claude.json")
	seed := `{
  "numStartups": 41,
  "mcpServers": {
    "someone-elses-server": {"command": "/opt/other/bin/srv", "args": ["serve"]}
  }
}`
	if err := os.WriteFile(cfg, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	if _, err := RegisterMCP(RegisterMCPOptions{
		BinPath:          bin,
		StatePath:        filepath.Join(home, ".grafel", "install.json"),
		ClaudeConfigDirs: []string{cfg},
	}); err != nil {
		t.Fatalf("RegisterMCP: %v", err)
	}

	servers := readMCPServers(t, cfg)
	other, ok := servers["someone-elses-server"].(map[string]any)
	if !ok {
		t.Fatalf("foreign MCP server was destroyed; servers=%v", servers)
	}
	if other["command"] != "/opt/other/bin/srv" {
		t.Errorf("foreign server rewritten: %v", other)
	}
	if _, ok := servers["grafel"]; !ok {
		t.Errorf("grafel entry missing; servers=%v", servers)
	}

	raw, _ := os.ReadFile(cfg)
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc["numStartups"] == nil {
		t.Errorf("unrelated top-level key numStartups was dropped: %s", raw)
	}
}

// TestRegisterMCP_PreservesAnExistingInstallRecord: on an UPGRADE install.json
// already exists and describes a real install — skills manifest, install
// timestamp, gitignore record. Re-registering MCP must not blank any of it.
func TestRegisterMCP_PreservesAnExistingInstallRecord(t *testing.T) {
	home := testsupport.IsolateHome(t)

	bin := seedFakeBinary(t)
	cfg := filepath.Join(home, ".claude.json")
	statePath := filepath.Join(home, ".grafel", "install.json")

	prior := NewState(ModeCopy)
	prior.InstalledAt = "2019-03-04T05:06:07Z"
	prior.Skills = map[string]SkillRecord{
		"grafel-help": {Files: map[string]string{"SKILL.md": strings.Repeat("a", 64)}},
	}
	prior.Gitignore = GitignoreRecord{Repos: []string{"/home/u/proj"}}
	prior.DaemonVersion = "9.9.9"
	prior.MCP = MCPRecord{Name: "grafel", RegisteredPaths: []string{"/home/u/.claude-work/.claude.json"}}
	if err := WriteState(statePath, prior); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	res, err := RegisterMCP(RegisterMCPOptions{
		BinPath:          bin,
		StatePath:        statePath,
		ClaudeConfigDirs: []string{cfg},
	})
	if err != nil {
		t.Fatalf("RegisterMCP: %v", err)
	}
	if res.CreatedState {
		t.Errorf("CreatedState = true, but install.json already existed")
	}

	st, err := ReadState(statePath)
	if err != nil || st == nil {
		t.Fatalf("ReadState: st=%v err=%v", st, err)
	}
	if st.InstalledAt != "2019-03-04T05:06:07Z" {
		t.Errorf("InstalledAt rewritten to %q; no install happened here", st.InstalledAt)
	}
	if _, ok := st.Skills["grafel-help"]; !ok {
		t.Errorf("skills manifest was blanked: %v", st.Skills)
	}
	if st.SkillsSkipped {
		t.Errorf("SkillsSkipped set on a state that HAS a skills manifest")
	}
	if len(st.Gitignore.Repos) != 1 || st.Gitignore.Repos[0] != "/home/u/proj" {
		t.Errorf("gitignore record lost: %+v", st.Gitignore)
	}
	if st.DaemonVersion != "9.9.9" {
		t.Errorf("DaemonVersion = %q; RegisterMCP restarts no daemon and must not "+
			"invalidate the record of one", st.DaemonVersion)
	}
	// Existing registrations survive; the new one is added.
	if !containsString(st.MCP.RegisteredPaths, "/home/u/.claude-work/.claude.json") {
		t.Errorf("previously registered path dropped: %v", st.MCP.RegisteredPaths)
	}
	if !containsString(st.MCP.RegisteredPaths, cfg) {
		t.Errorf("new path not recorded: %v", st.MCP.RegisteredPaths)
	}
}

// TestRegisterMCP_OneBadTargetDoesNotAbortTheRestAndRollsBackNothing.
//
// This is the #6168 lesson applied ahead of time. RunCopy step 3 is a
// transaction: a failure part-way through UNDOES the registrations it already
// made, and when the snapshot it restores is the absent-sentinel that means
// DELETING the user's config file along with every foreign server in it.
// RegisterMCP is not a transaction — the hosts are independent — so a failure
// on one must leave the others registered and must restore nothing.
func TestRegisterMCP_OneBadTargetDoesNotAbortTheRestAndRollsBackNothing(t *testing.T) {
	home := testsupport.IsolateHome(t)

	bin := seedFakeBinary(t)

	// A directory where a config file is expected: every write path fails.
	bad := filepath.Join(home, "bad", ".claude.json")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatalf("mkdir bad target: %v", err)
	}
	good := filepath.Join(home, "good", ".claude.json")

	res, err := RegisterMCP(RegisterMCPOptions{
		BinPath:          bin,
		StatePath:        filepath.Join(home, ".grafel", "install.json"),
		ClaudeConfigDirs: []string{bad, good},
	})
	if err != nil {
		t.Fatalf("RegisterMCP must not fail the install over one bad host: %v", err)
	}
	if len(res.Failed) != 1 || res.Failed[0].Path != bad {
		t.Errorf("Failed = %+v, want exactly the bad target %s", res.Failed, bad)
	}
	if !containsString(res.Registered, good) {
		t.Errorf("the good target was skipped after the bad one failed: %v", res.Registered)
	}
	if got := grafelCommandIn(t, good); got != bin {
		t.Errorf("good target command = %q, want %q", got, bin)
	}
	// Nothing was restored/deleted: the directory we used as the bad target is
	// still there, untouched.
	if info, statErr := os.Stat(bad); statErr != nil || !info.IsDir() {
		t.Errorf("bad target was mutated by a rollback: info=%v err=%v", info, statErr)
	}
}

// TestRegisterMCP_IsIdempotent: install.sh calls this on EVERY run, including
// every upgrade. A second run must not duplicate the recorded paths nor change
// the config's meaning.
func TestRegisterMCP_IsIdempotent(t *testing.T) {
	home := testsupport.IsolateHome(t)

	bin := seedFakeBinary(t)
	cfg := filepath.Join(home, ".claude.json")
	statePath := filepath.Join(home, ".grafel", "install.json")

	opts := RegisterMCPOptions{BinPath: bin, StatePath: statePath, ClaudeConfigDirs: []string{cfg}}
	if _, err := RegisterMCP(opts); err != nil {
		t.Fatalf("first RegisterMCP: %v", err)
	}
	if _, err := RegisterMCP(opts); err != nil {
		t.Fatalf("second RegisterMCP: %v", err)
	}

	st, err := ReadState(statePath)
	if err != nil || st == nil {
		t.Fatalf("ReadState: st=%v err=%v", st, err)
	}
	n := 0
	for _, p := range st.MCP.RegisteredPaths {
		if p == cfg {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%s recorded %d times in %v, want exactly 1", cfg, n, st.MCP.RegisteredPaths)
	}
	if got := grafelCommandIn(t, cfg); got != bin {
		t.Errorf("command after second run = %q, want %q", got, bin)
	}
}

// TestRegisterMCP_RepointsAMovedBinary: on an upgrade that changed prefix, the
// recorded `command` points at a binary that is gone and the MCP server simply
// fails to start. Re-registering is the repair, so it must actually overwrite.
func TestRegisterMCP_RepointsAMovedBinary(t *testing.T) {
	home := testsupport.IsolateHome(t)

	bin := seedFakeBinary(t)
	cfg := filepath.Join(home, ".claude.json")
	seed := `{"mcpServers":{"grafel":{"command":"/old/prefix/bin/grafel","args":["mcp-bridge"],"type":"stdio"}}}`
	if err := os.WriteFile(cfg, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	if _, err := RegisterMCP(RegisterMCPOptions{
		BinPath:          bin,
		StatePath:        filepath.Join(home, ".grafel", "install.json"),
		ClaudeConfigDirs: []string{cfg},
	}); err != nil {
		t.Fatalf("RegisterMCP: %v", err)
	}
	if got := grafelCommandIn(t, cfg); got != bin {
		t.Errorf("stale command left in place: %q, want %q", got, bin)
	}
}

// TestRegisterMCP_MissingBinaryIsAnError: the CLI record it writes carries a
// checksum, and there is no honest checksum for a binary that is not there.
// Failing loudly beats recording a grafel entry that points at nothing.
func TestRegisterMCP_MissingBinaryIsAnError(t *testing.T) {
	home := testsupport.IsolateHome(t)

	cfg := filepath.Join(home, ".claude.json")
	_, err := RegisterMCP(RegisterMCPOptions{
		BinPath:          filepath.Join(home, "nope", "grafel"),
		StatePath:        filepath.Join(home, ".grafel", "install.json"),
		ClaudeConfigDirs: []string{cfg},
	})
	if err == nil {
		t.Fatalf("RegisterMCP with a missing binary must fail")
	}
	if _, statErr := os.Stat(cfg); !os.IsNotExist(statErr) {
		t.Errorf("a config was written for a binary that does not exist (err=%v)", statErr)
	}
}

// TestRegisterMCP_LeavesNoRollbackSnapshotBehind.
//
// Review finding S1. "RegisterMCP has no rollback" was true of RegisterMCP and
// still left the artifact a rollback CONSUMES: RegisterPath calls backupOnce,
// which writes `<cfg>.grafel.bak` — on a first-ever install that is the
// absent-sentinel, the value whose restore DELETES the config file and every
// foreign server in it.
//
// Today both RestoreSnapshot call sites (copy.go, dev.go) run
// discardStaleMCPBackups first, so the stale sidecar is inert. That makes the
// safety COORDINATIONAL — it holds because two other files remember to sweep —
// rather than structural. A third caller, or a reordering of either sweep, and
// a snapshot this function planted becomes live again. Since RegisterMCP never
// rolls back, it needs no snapshot at all, so it clears its own.
func TestRegisterMCP_LeavesNoRollbackSnapshotBehind(t *testing.T) {
	for _, tc := range []struct {
		name string
		seed string // "" = no pre-existing config (the sentinel case)
	}{
		{"first-ever install writes the absent-sentinel", ""},
		{"upgrade snapshots real content", `{"mcpServers":{"other":{"command":"/x"}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := testsupport.IsolateHome(t)

			bin := seedFakeBinary(t)
			cfg := filepath.Join(home, ".claude.json")
			if tc.seed != "" {
				if err := os.WriteFile(cfg, []byte(tc.seed), 0o644); err != nil {
					t.Fatalf("seed config: %v", err)
				}
			}

			if _, err := RegisterMCP(RegisterMCPOptions{
				BinPath:          bin,
				StatePath:        filepath.Join(home, ".grafel", "install.json"),
				ClaudeConfigDirs: []string{cfg},
			}); err != nil {
				t.Fatalf("RegisterMCP: %v", err)
			}

			sidecar := cfg + ".grafel.bak"
			if _, err := os.Stat(sidecar); err == nil {
				raw, _ := os.ReadFile(sidecar)
				t.Errorf("RegisterMCP left a rollback snapshot at %s (content %q); "+
					"a later RestoreSnapshot would act on it", sidecar, raw)
			} else if !os.IsNotExist(err) {
				t.Fatalf("stat sidecar: %v", err)
			}

			// The registration itself must still be intact — clearing the
			// snapshot must not be confused with undoing the write.
			if got := grafelCommandIn(t, cfg); got != bin {
				t.Errorf("registration was lost: command = %q, want %q", got, bin)
			}
		})
	}
}

// TestRegisterMCP_ClearsTheSnapshotItPlantedEvenForAFailedPeer: the sweep is
// per-target, so one host failing must not leave a live sentinel on the hosts
// that succeeded.
func TestRegisterMCP_ClearsTheSnapshotItPlantedEvenForAFailedPeer(t *testing.T) {
	home := testsupport.IsolateHome(t)

	bin := seedFakeBinary(t)
	bad := filepath.Join(home, "bad", ".claude.json")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatalf("mkdir bad target: %v", err)
	}
	good := filepath.Join(home, "good", ".claude.json")

	if _, err := RegisterMCP(RegisterMCPOptions{
		BinPath:          bin,
		StatePath:        filepath.Join(home, ".grafel", "install.json"),
		ClaudeConfigDirs: []string{bad, good},
	}); err != nil {
		t.Fatalf("RegisterMCP: %v", err)
	}
	if _, err := os.Stat(good + ".grafel.bak"); err == nil {
		t.Errorf("snapshot left on the successful host after a peer failed")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat sidecar: %v", err)
	}
}

func containsString(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
