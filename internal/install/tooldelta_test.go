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

// Shared rules file: claude+codex both... actually claude→CLAUDE.md,
// codex→AGENTS.md are distinct. Use a case where disabling does NOT strip a
// file still owned by a surviving tool. claude (CLAUDE.md) + codex (AGENTS.md):
// disabling codex strips AGENTS.md only, leaving CLAUDE.md.
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
