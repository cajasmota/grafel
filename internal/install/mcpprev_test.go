package install

// mcpprev_test.go — the durable previously-registered MCP signal (#6170).
//
// Every test isolates HOME, XDG_CONFIG_HOME and GRAFEL_HOME into a t.TempDir
// and drives the registry through injected accessors, so nothing here can
// reach the developer's real ~/.claude.json, ~/.grafel or launchd.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/install/mcpreg"
	"github.com/cajasmota/grafel/internal/registry"
)

// isolatePrevHome points HOME/XDG_CONFIG_HOME/GRAFEL_HOME at a temp dir and
// returns it. Nothing under test writes; this guards the READS too so a
// missing fixture can never be answered by a real file.
func isolatePrevHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("GRAFEL_HOME", filepath.Join(home, ".grafel"))
	return home
}

// writePrevState writes an install.json under the isolated home recording the
// given RegisteredPaths, and returns a statePathFn pointing at it.
func writePrevState(t *testing.T, home string, paths []string) func() (string, error) {
	t.Helper()
	statePath := filepath.Join(home, ".grafel", "install.json")
	st := NewState(ModeCopy)
	st.MCP = MCPRecord{Name: mcpreg.ServerName, RegisteredPaths: paths}
	if err := WriteState(statePath, st); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	return func() (string, error) { return statePath, nil }
}

// noGroups is the "nothing registered yet" registry: no explicit tool
// selection exists anywhere, so nothing can be deliberately disabled.
func noGroups() (func() ([]registry.GroupRef, error), func(string) (*registry.GroupConfig, error)) {
	return func() ([]registry.GroupRef, error) { return nil, nil },
		func(string) (*registry.GroupConfig, error) { return nil, nil }
}

// TestPreviouslyRegisteredMCPPaths_ReadsRegisteredPaths verifies the recorded
// paths come back as a cleaned set — the signal that survives the grafel entry
// being deleted from the config file (#6170).
func TestPreviouslyRegisteredMCPPaths_ReadsRegisteredPaths(t *testing.T) {
	home := isolatePrevHome(t)
	claudeJSON := filepath.Join(home, ".claude.json")
	statePathFn := writePrevState(t, home, []string{claudeJSON + "/", ""})
	groupsFn, loadFn := noGroups()

	got := previouslyRegisteredMCPPaths(statePathFn, groupsFn, loadFn)
	if !got[claudeJSON] {
		t.Errorf("want %s in the recorded set; got %v", claudeJSON, got)
	}
	if len(got) != 1 {
		t.Errorf("empty entries must be dropped; got %v", got)
	}
}

// TestPreviouslyRegisteredMCPPaths_NoStateFile verifies a machine with no
// install.json yields nil — the pre-#6170 default, not a crash.
func TestPreviouslyRegisteredMCPPaths_NoStateFile(t *testing.T) {
	home := isolatePrevHome(t)
	missing := func() (string, error) { return filepath.Join(home, ".grafel", "install.json"), nil }
	groupsFn, loadFn := noGroups()

	if got := previouslyRegisteredMCPPaths(missing, groupsFn, loadFn); got != nil {
		t.Errorf("no install.json should yield nil; got %v", got)
	}
}

// TestPreviouslyRegisteredMCPPaths_CorruptStateFile verifies an unreadable
// install.json degrades to nil rather than propagating an error into the
// wizard.
func TestPreviouslyRegisteredMCPPaths_CorruptStateFile(t *testing.T) {
	home := isolatePrevHome(t)
	statePath := filepath.Join(home, ".grafel", "install.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	groupsFn, loadFn := noGroups()

	if got := previouslyRegisteredMCPPaths(func() (string, error) { return statePath, nil }, groupsFn, loadFn); got != nil {
		t.Errorf("corrupt install.json should yield nil; got %v", got)
	}
}

// TestPreviouslyRegisteredMCPPaths_DeliberateOptOutSubtracted is the safety
// property at the install layer: a group config that explicitly enables only
// cursor is a deliberate "claude off", so claude's recorded path must NOT be
// handed to the wizard as a reason to re-check it.
func TestPreviouslyRegisteredMCPPaths_DeliberateOptOutSubtracted(t *testing.T) {
	home := isolatePrevHome(t)
	claudeJSON := filepath.Join(home, ".claude.json")
	cursorJSON := filepath.Join(home, ".cursor", "mcp.json")
	statePathFn := writePrevState(t, home, []string{claudeJSON, cursorJSON})

	groupsFn, loadFn := fakeGroups(&registry.GroupConfig{Name: "g", Tools: []string{"cursor"}})

	got := previouslyRegisteredMCPPaths(statePathFn, groupsFn, loadFn)
	if got[claudeJSON] {
		t.Errorf("claude is explicitly disabled in the group config — its recorded path must be subtracted; got %v", got)
	}
	if !got[cursorJSON] {
		t.Errorf("cursor is still enabled — its recorded path must survive; got %v", got)
	}
}

// TestPreviouslyRegisteredMCPPaths_ClaudeSidecarSubtracted verifies the
// subtraction covers every path family step 3 registers a tool at, not just
// the canonical one: a ~/.claude-personal profile must be dropped too when
// claude is disabled.
func TestPreviouslyRegisteredMCPPaths_ClaudeSidecarSubtracted(t *testing.T) {
	home := isolatePrevHome(t)
	sidecarDir := filepath.Join(home, ".claude-personal")
	if err := os.MkdirAll(sidecarDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(sidecarDir, ".claude.json")
	if err := os.WriteFile(sidecar, []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	statePathFn := writePrevState(t, home, []string{sidecar})

	groupsFn, loadFn := fakeGroups(&registry.GroupConfig{Name: "g", Tools: []string{"cursor"}})

	if got := previouslyRegisteredMCPPaths(statePathFn, groupsFn, loadFn); got[sidecar] {
		t.Errorf("the sidecar Claude profile must be subtracted with claude disabled; got %v", got)
	}
}

// TestPreviouslyRegisteredMCPPaths_EmptyToolsIsNotAnOptOut verifies the
// empty-means-all contract: a group with no explicit Tools selection disables
// nothing, so the recorded paths pass through.
func TestPreviouslyRegisteredMCPPaths_EmptyToolsIsNotAnOptOut(t *testing.T) {
	home := isolatePrevHome(t)
	claudeJSON := filepath.Join(home, ".claude.json")
	statePathFn := writePrevState(t, home, []string{claudeJSON})

	groupsFn, loadFn := fakeGroups(&registry.GroupConfig{Name: "g"}) // Tools nil = all

	if got := previouslyRegisteredMCPPaths(statePathFn, groupsFn, loadFn); !got[claudeJSON] {
		t.Errorf("an empty Tools list means ALL tools, not an opt-out; got %v", got)
	}
}

// TestPreviouslyRegisteredMCPPaths_UnreadableGroupFailsClosed verifies the
// safety-critical direction of an error: if a group config cannot be loaded we
// cannot know whether a tool was deliberately disabled, so NO paths are
// offered. Fail-open here would silently re-check a tool the user turned off
// on nothing more than a transient permission error.
func TestPreviouslyRegisteredMCPPaths_UnreadableGroupFailsClosed(t *testing.T) {
	home := isolatePrevHome(t)
	claudeJSON := filepath.Join(home, ".claude.json")
	statePathFn := writePrevState(t, home, []string{claudeJSON})

	groupsFn := func() ([]registry.GroupRef, error) {
		return []registry.GroupRef{{Name: "g", ConfigPath: "/fake/g.json"}}, nil
	}
	loadFn := func(string) (*registry.GroupConfig, error) {
		return nil, errors.New("permission denied")
	}

	if got := previouslyRegisteredMCPPaths(statePathFn, groupsFn, loadFn); got != nil {
		t.Errorf("an unreadable group config must yield NO paths (fail closed); got %v", got)
	}
}

// TestPreviouslyRegisteredMCPPaths_RegistryErrorFailsClosed is the same
// property one level up: an unreadable registry is not evidence that nothing
// is disabled.
func TestPreviouslyRegisteredMCPPaths_RegistryErrorFailsClosed(t *testing.T) {
	home := isolatePrevHome(t)
	statePathFn := writePrevState(t, home, []string{filepath.Join(home, ".claude.json")})

	groupsFn := func() ([]registry.GroupRef, error) { return nil, errors.New("registry unreadable") }
	loadFn := func(string) (*registry.GroupConfig, error) { return nil, nil }

	if got := previouslyRegisteredMCPPaths(statePathFn, groupsFn, loadFn); got != nil {
		t.Errorf("an unreadable registry must yield NO paths (fail closed); got %v", got)
	}
}

// TestDeliberatelyDisabledMCPTools_NoGroupsIsNoSignal verifies absence of
// evidence is not an opt-out: with no groups registered nothing is reported
// disabled, so a fresh machine's recorded paths are never subtracted away.
func TestDeliberatelyDisabledMCPTools_NoGroupsIsNoSignal(t *testing.T) {
	groupsFn, loadFn := noGroups()
	if got, ok := deliberatelyDisabledMCPTools(groupsFn, loadFn); !ok || len(got) != 0 {
		t.Errorf("no groups must yield no disabled tools and a READABLE verdict; got %v ok=%v", got, ok)
	}
}
