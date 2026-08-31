package install

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/install/mcpreg"
	"github.com/cajasmota/grafel/internal/registry"
)

// recordingOps captures every primitive call so tests can assert the delta
// without touching the filesystem or the live machine.
type recordingOps struct {
	rulesWritten  map[string][]string // repo → targets
	rulesRemoved  map[string][]string
	mcpRegistered []mcpreg.Tool
	mcpUnregister []mcpreg.Tool
}

func newRecordingOps() (*recordingOps, ToolDeltaOps) {
	r := &recordingOps{
		rulesWritten: map[string][]string{},
		rulesRemoved: map[string][]string{},
	}
	ops := ToolDeltaOps{
		WriteRules:    func(repo string, t []string) error { r.rulesWritten[repo] = t; return nil },
		RemoveRules:   func(repo string, t []string) error { r.rulesRemoved[repo] = t; return nil },
		RegisterMCP:   func(t mcpreg.Tool) error { r.mcpRegistered = append(r.mcpRegistered, t); return nil },
		UnregisterMCP: func(t mcpreg.Tool) error { r.mcpUnregister = append(r.mcpUnregister, t); return nil },
	}
	return r, ops
}

func sortedTools(ts []mcpreg.Tool) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = string(t)
	}
	sort.Strings(out)
	return out
}

// repoPath is a platform-appropriate ABSOLUTE repo path. ApplyToolDelta keys
// its result maps by absRepo(r.Path) — on Windows a Unix-style "/tmp/repoX" is
// NOT absolute, so filepath.Abs would rewrite it to e.g. C:\tmp\repoX and the
// map key would no longer match the literal. Using an already-absolute path
// (filepath.Join of a volume-rooted temp base) makes the key stable on every
// OS while keeping the assertions exact.
var repoPath = filepath.Join(os.TempDir(), "repoX")

func cfgWithRepo() *registry.GroupConfig {
	return &registry.GroupConfig{
		Name:  "g",
		Repos: []registry.Repo{{Path: repoPath}},
	}
}

// Enabling cursor (was claude only) should write cursor's rules + register
// cursor's MCP, and touch nothing for claude.
func TestApplyToolDelta_EnableCursor(t *testing.T) {
	rec, ops := newRecordingOps()
	res, err := ApplyToolDelta(cfgWithRepo(), "g", "/bin/grafel",
		[]string{"claude"}, []string{"claude", "cursor"}, &ops)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Enabled, []string{"cursor"}) {
		t.Fatalf("enabled = %v", res.Enabled)
	}
	if len(res.Disabled) != 0 {
		t.Fatalf("disabled = %v", res.Disabled)
	}
	if got := rec.rulesWritten[repoPath]; !reflect.DeepEqual(got, []string{".cursorrules"}) {
		t.Fatalf("rules written = %v", got)
	}
	if len(rec.rulesRemoved) != 0 {
		t.Fatalf("nothing should be removed: %v", rec.rulesRemoved)
	}
	if got := sortedTools(rec.mcpRegistered); !reflect.DeepEqual(got, []string{string(mcpreg.Cursor)}) {
		t.Fatalf("mcp registered = %v", got)
	}
}

// Disabling windsurf (had claude+windsurf) removes windsurf's rules +
// unregisters windsurf's MCP; claude untouched.
func TestApplyToolDelta_DisableWindsurf(t *testing.T) {
	rec, ops := newRecordingOps()
	res, err := ApplyToolDelta(cfgWithRepo(), "g", "/bin/grafel",
		[]string{"claude", "windsurf"}, []string{"claude"}, &ops)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Disabled, []string{"windsurf"}) {
		t.Fatalf("disabled = %v", res.Disabled)
	}
	if got := rec.rulesRemoved[repoPath]; !reflect.DeepEqual(got, []string{".windsurfrules"}) {
		t.Fatalf("rules removed = %v", got)
	}
	if got := sortedTools(rec.mcpUnregister); !reflect.DeepEqual(got, []string{string(mcpreg.Windsurf)}) {
		t.Fatalf("mcp unregistered = %v", got)
	}
}

// Disabling codex strips AGENTS.md and touches nothing of claude's.
//
// NOT a shared-target case, despite the name — read it as "claude's artifacts
// are undisturbed". claude contributes NO per-repo rules target at all since
// #5702 (its guidance moved to the personal ~/.claude/CLAUDE.md), so AGENTS.md
// has exactly one owner here and the surviving-owner branch of ApplyToolDelta
// is never reached. For that branch see
// TestApplyToolDelta_OpencodeCodexShareAGENTS below — codex+opencode is the
// first adapter pair in the registry that shares a rules file at all (#6730).
func TestApplyToolDelta_DisableCodexKeepsClaudeRules(t *testing.T) {
	rec, ops := newRecordingOps()
	_, err := ApplyToolDelta(cfgWithRepo(), "g", "/bin/grafel",
		[]string{"claude", "codex"}, []string{"claude"}, &ops)
	if err != nil {
		t.Fatal(err)
	}
	if got := rec.rulesRemoved[repoPath]; !reflect.DeepEqual(got, []string{"AGENTS.md"}) {
		t.Fatalf("rules removed = %v (should strip only AGENTS.md)", got)
	}
	// codex registers an MCP entry → it should be unregistered.
	if got := sortedTools(rec.mcpUnregister); !reflect.DeepEqual(got, []string{string(mcpreg.Codex)}) {
		t.Fatalf("mcp unregistered = %v", got)
	}
}

// TestApplyToolDelta_OpencodeCodexShareAGENTS pins the SHARED-OWNERSHIP
// behaviour codex and opencode acquired in #6730 — the tradeoff the owner
// accepted when opencode was given AGENTS.md rather than a file of its own.
//
// Every existing case in this file pairs tools whose rules files are DISJOINT
// (TestApplyToolDelta_DisableCodexKeepsClaudeRules only looks disjointness in
// the eye: claude contributes no per-repo target at all since #5702). So the
// "shared target survives while any owner survives" branch of ApplyToolDelta —
// subtractTargets(targetsFor(disabled), survivingRules) — had no pair that
// actually exercised it. codex+opencode is that pair.
//
// Case 0 is the POSITIVE CONTROL: it proves opencode owns AGENTS.md on its own
// and that removal fires at all, so the three "not removed" assertions below
// mean "held back by a surviving owner" rather than "nothing was ever wired".
func TestApplyToolDelta_OpencodeCodexShareAGENTS(t *testing.T) {
	removedFor := func(t *testing.T, prev, next []string) []string {
		t.Helper()
		rec, ops := newRecordingOps()
		if _, err := ApplyToolDelta(cfgWithRepo(), "g", "/bin/grafel", prev, next, &ops); err != nil {
			t.Fatalf("ApplyToolDelta(%v→%v): %v", prev, next, err)
		}
		return rec.rulesRemoved[repoPath]
	}

	// Case 0 — positive control: opencode alone owns AGENTS.md, and disabling
	// it strips the file.
	if got := removedFor(t, []string{"opencode"}, nil); !reflect.DeepEqual(got, []string{"AGENTS.md"}) {
		t.Fatalf("control: disabling opencode alone removed %v, want [AGENTS.md] "+
			"(if this is empty, opencode does not own AGENTS.md and the cases below prove nothing)", got)
	}

	// Case 1 — disable opencode, codex survives: AGENTS.md must STAY.
	if got := removedFor(t, []string{"codex", "opencode"}, []string{"codex"}); len(got) != 0 {
		t.Fatalf("disabling opencode removed %v; AGENTS.md must survive while codex still reads it", got)
	}

	// Case 2 — the mirror: disable codex, opencode survives. AGENTS.md must
	// STAY. (Without opencode this is exactly what
	// TestApplyToolDelta_DisableCodexKeepsClaudeRules asserts DOES get removed,
	// so the two tests together show the surviving-owner check is what moved.)
	if got := removedFor(t, []string{"codex", "opencode"}, []string{"opencode"}); len(got) != 0 {
		t.Fatalf("disabling codex removed %v; AGENTS.md must survive while opencode still reads it", got)
	}

	// Case 3 — last owner out: with BOTH disabled, AGENTS.md is finally removed.
	if got := removedFor(t, []string{"codex", "opencode"}, nil); !reflect.DeepEqual(got, []string{"AGENTS.md"}) {
		t.Fatalf("disabling both removed %v, want [AGENTS.md] — no owner is left", got)
	}
}

// TestApplyToolDelta_OpencodeMCPIsNotShared is the counterpart on the MCP axis:
// unlike AGENTS.md, opencode's MCP entry is its OWN file, so disabling opencode
// while codex survives must still unregister mcpreg.Opencode. This keeps the
// shared-rules leniency above from being mistaken for a blanket "a surviving
// tool suppresses every teardown".
func TestApplyToolDelta_OpencodeMCPIsNotShared(t *testing.T) {
	rec, ops := newRecordingOps()
	if _, err := ApplyToolDelta(cfgWithRepo(), "g", "/bin/grafel",
		[]string{"codex", "opencode"}, []string{"codex"}, &ops); err != nil {
		t.Fatal(err)
	}
	if got := sortedTools(rec.mcpUnregister); !reflect.DeepEqual(got, []string{string(mcpreg.Opencode)}) {
		t.Fatalf("mcp unregistered = %v, want [%s]", got, mcpreg.Opencode)
	}
}

func TestApplyToolDelta_NoChangeNoOps(t *testing.T) {
	rec, ops := newRecordingOps()
	res, err := ApplyToolDelta(cfgWithRepo(), "g", "/bin/grafel",
		[]string{"claude"}, []string{"claude"}, &ops)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Enabled) != 0 || len(res.Disabled) != 0 {
		t.Fatalf("expected empty delta: %+v", res)
	}
	if len(rec.rulesWritten) != 0 || len(rec.rulesRemoved) != 0 ||
		len(rec.mcpRegistered) != 0 || len(rec.mcpUnregister) != 0 {
		t.Fatalf("no primitive should have been called: %+v", rec)
	}
}

func TestApplyToolDelta_NilConfigErrors(t *testing.T) {
	if _, err := ApplyToolDelta(nil, "g", "", nil, nil, &ToolDeltaOps{}); err == nil {
		t.Fatal("expected error for nil config")
	}
}
