package cli

// #6338 review follow-up — the capped table tells the reader to run
// `grafel doctor --json` for the full list. That payload emitted only
// install.DoctorReport and returned early (doctor.go), so it never carried a
// single language row: the tool printing a confidently-wrong instruction,
// which is the exact failure mode this whole change exists to remove.
//
// These tests assert on the SERIALISED JSON, not on the struct, because the
// defect was that the data never reached the payload.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/registry"
)

// seedGroup6338 registers a group whose single repo's sidecar carries counts.
func seedGroup6338(t *testing.T, group string, unsupported map[string]int) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv(daemon.EnvRoot, t.TempDir())

	repoPath := filepath.Join(home, "repos", "legacy")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	side := &graph.GraphStatsSidecar{
		Version:               1,
		ComputedAt:            time.Now(),
		TotalEntities:         5,
		UnsupportedExtensions: unsupported,
	}
	if err := graph.WriteSidecar(graph.SidecarPath(stateDir), side, false); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(home, "groups", group+".json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`{"name":%q,"repos":[{"slug":"legacy","path":%q}]}`, group, repoPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup(group, cfgPath); err != nil {
		t.Fatal(err)
	}
}

func emitJSON6338(t *testing.T) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	_ = emitDoctorJSON(&buf, &install.DoctorReport{SchemaVersion: 1, OK: true})
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("doctor --json emitted invalid JSON: %v\n%s", err, buf.String())
	}
	return doc
}

// The payload carries the ROWS, with their counts and language names — not
// merely a field that exists.
func TestDoctorJSONCarriesUnsupportedRows(t *testing.T) {
	seedGroup6338(t, "acme", map[string]int{".vb": 672, ".pas": 14, ".json": 1938, ".go": 9000})

	doc := emitJSON6338(t)
	groups, ok := doc["groups"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatalf(`doctor --json has no "groups" — the capped table points here for the full list; got: %v`, doc)
	}
	g := groups[0].(map[string]any)
	if g["name"] != "acme" {
		t.Fatalf("group name = %v, want acme", g["name"])
	}
	rows, ok := g["unsupported_languages"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("want 2 rows (.vb, .pas), got %v", g["unsupported_languages"])
	}
	first := rows[0].(map[string]any)
	if first["extension"] != ".vb" {
		t.Fatalf("first row extension = %v, want .vb", first["extension"])
	}
	if n, _ := first["files"].(float64); int(n) != 672 {
		t.Fatalf("first row files = %v, want 672", first["files"])
	}
	if first["language"] != "VB.NET" {
		t.Fatalf("first row language = %v, want VB.NET", first["language"])
	}
	if first["issue"] != "#6327" {
		t.Fatalf("first row issue = %v, want #6327", first["issue"])
	}
	// The same filters the table applies, applied here.
	for _, r := range rows {
		ext := r.(map[string]any)["extension"]
		if ext == ".json" || ext == ".go" {
			t.Fatalf("%v leaked into the JSON payload", ext)
		}
	}
}

// The promise the remainder line makes: the JSON is UNCAPPED. A payload that
// reused the rendered (capped) list would satisfy the test above and still
// leave the instruction wrong.
func TestDoctorJSONIsUncapped(t *testing.T) {
	counts := map[string]int{}
	for _, ext := range []string{
		".vb", ".pas", ".f90", ".ada", ".jl", ".tcl", ".hx", ".coffee",
		".abap", ".rpg", ".ps1", ".cmd",
	} {
		counts[ext] = 50
	}
	if len(counts) <= UnsupportedMaxRows {
		t.Fatalf("fixture must exceed the render cap of %d", UnsupportedMaxRows)
	}
	seedGroup6338(t, "big", counts)

	doc := emitJSON6338(t)
	groups := doc["groups"].([]any)
	rows := groups[0].(map[string]any)["unsupported_languages"].([]any)
	if len(rows) != len(counts) {
		t.Fatalf("JSON must carry all %d rows uncapped, got %d", len(counts), len(rows))
	}

	// And the human table really does cap and point here, so the two agree.
	var buf bytes.Buffer
	PrintUnsupportedLanguages(&buf, "  ", UnsupportedRows(counts, DoctorUnsupportedMinFiles))
	if !strings.Contains(buf.String(), "doctor --json") {
		t.Fatalf("the capped table no longer points at the JSON:\n%s", buf.String())
	}
}

// Backward compatibility: every key the payload carried before is still there,
// unchanged and in the same order. Only "groups" is added.
func TestDoctorJSONRemainsBackwardCompatible(t *testing.T) {
	seedGroup6338(t, "acme", map[string]int{".vb": 672})

	report := &install.DoctorReport{SchemaVersion: 1, OK: false, Remediation: "run grafel install"}
	var withGroups bytes.Buffer
	_ = emitDoctorJSON(&withGroups, report)

	// The pre-change payload, byte for byte.
	before, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	trimmed := strings.TrimSuffix(strings.TrimSpace(string(before)), "}")
	if !strings.HasPrefix(strings.TrimSpace(withGroups.String()), strings.TrimSpace(trimmed)) {
		t.Fatalf("existing keys changed name, value or order\n--- now ---\n%s\n--- before ---\n%s",
			withGroups.String(), before)
	}
	if !strings.Contains(withGroups.String(), `"groups"`) {
		t.Fatalf(`"groups" missing: %s`, withGroups.String())
	}
}

// Clean group: no "groups" key at all, matching the table's "print nothing when
// there is nothing to say".
func TestDoctorJSONOmitsGroupsWhenClean(t *testing.T) {
	seedGroup6338(t, "clean", nil)

	doc := emitJSON6338(t)
	if _, present := doc["groups"]; present {
		t.Fatalf(`a clean group must emit no "groups" key, got %v`, doc["groups"])
	}
}

// .sqlrpgle is the modern IBM i form and was a near-miss against the .rpgle
// entry that was already there.
func TestSqlRpgleIsNamed(t *testing.T) {
	rows := UnsupportedRows(map[string]int{".sqlrpgle": 40}, DoctorUnsupportedMinFiles)
	if len(rows) != 1 || rows[0].Language != "RPG" {
		t.Fatalf(".sqlrpgle must be named RPG, got %+v", rows)
	}
}
