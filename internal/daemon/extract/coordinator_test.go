package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func TestWriteBatches_PartitionsByLanguageAndSize(t *testing.T) {
	dir := t.TempDir()
	buckets := map[string][]string{
		"python": {"a.py", "b.py", "c.py", "d.py", "e.py"},
		"go":     {"main.go", "util.go"},
	}
	batches, err := writeBatches(dir, buckets, 2)
	if err != nil {
		t.Fatalf("writeBatches: %v", err)
	}

	// python (5 files / 2) = 3 batches; go (2 files / 2) = 1 batch -> total 4.
	if len(batches) != 4 {
		t.Fatalf("got %d batches, want 4", len(batches))
	}

	langs := map[string]int{}
	for _, b := range batches {
		langs[b.language]++
		st, statErr := os.Stat(b.path)
		if statErr != nil {
			t.Fatalf("stat batch %s: %v", b.path, statErr)
		}
		if st.Size() == 0 {
			t.Fatalf("batch %s is empty", b.path)
		}
	}
	if langs["python"] != 3 || langs["go"] != 1 {
		t.Fatalf("partition tally wrong: %v", langs)
	}
}

func TestDecodeStream_FoldsEntitiesRelsStats(t *testing.T) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	_ = enc.Encode(Envelope{Type: KindEntity, Entity: &types.EntityRecord{
		Name: "Foo", Kind: "function", SourceFile: "x.go",
	}})
	_ = enc.Encode(Envelope{Type: KindRelationship, Rel: &types.RelationshipRecord{
		FromID: "a", ToID: "b", Kind: "CALLS",
	}})
	_ = enc.Encode(Envelope{Type: KindError, Err: "non-fatal"})
	_ = enc.Encode(Envelope{Type: KindStats, Stats: &BatchStats{
		BatchID: "b0", Files: 3, Extracted: 2, Pass1Rels: 5,
	}})

	ents, rels, stats, errs := decodeStream(&buf)
	if len(ents) != 1 {
		t.Fatalf("entities=%d want 1", len(ents))
	}
	if len(rels) != 1 {
		t.Fatalf("rels=%d want 1", len(rels))
	}
	if len(errs) != 1 || errs[0] != "non-fatal" {
		t.Fatalf("errs=%v", errs)
	}
	if stats == nil || stats.BatchID != "b0" || stats.Files != 3 || stats.Pass1Rels != 5 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestSortEntityRecords_Deterministic(t *testing.T) {
	in := []types.EntityRecord{
		{SourceFile: "b.go", Name: "X", Kind: "function"},
		{SourceFile: "a.go", Name: "Z", Kind: "function"},
		{SourceFile: "a.go", Name: "Y", Kind: "function"},
	}
	sortEntityRecords(in)
	got := []string{in[0].SourceFile, in[1].SourceFile, in[2].SourceFile}
	want := []string{"a.go", "a.go", "b.go"}
	if got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("sorted order=%v want %v", got, want)
	}
}

// TestCoordinate_EndToEnd builds the archigraph binary, writes a tiny
// repo, then runs Coordinate against it. The subprocess path must
// produce at least one entity for the demo.go file. Skipped under
// `go test -short`.
func TestCoordinate_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test — skipped in -short mode")
	}
	bin := buildArchigraph(t)

	repo := t.TempDir()
	src := `package demo
func Hello() string { return "hi" }
`
	if err := os.WriteFile(filepath.Join(repo, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	res, err := Coordinate(context.Background(), repo, []string{"demo.go"},
		CoordinatorConfig{
			BinaryPath: bin,
			Stderr:     &stderr,
		})
	if err != nil {
		t.Fatalf("Coordinate: %v\n---stderr---\n%s", err, stderr.String())
	}
	if len(res.Entities) == 0 {
		t.Fatalf("expected at least one entity; stderr=%s", stderr.String())
	}
	if res.Subprocesses == 0 {
		t.Fatalf("expected >=1 subprocess; got %d", res.Subprocesses)
	}
}

// TestCoordinate_EmitsConfigEntities (#1885) — the coordinator must run
// the in-process config-discovery pass after the subprocess fan-out so
// project-level configs become first-class SCOPE.Config entities. Uses a
// minimal repo with go.mod + Makefile + .env.
func TestCoordinate_EmitsConfigEntities(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test — skipped in -short mode")
	}
	bin := buildArchigraph(t)

	repo := t.TempDir()
	must := func(name, content string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	must("demo.go", "package demo\nfunc Hello() string { return \"hi\" }\n")
	must("go.mod", "module demo/x\nrequire github.com/spf13/cobra v1.8.0\n")
	must("Makefile", ".PHONY: build\nbuild:\n\tgo build ./...\n")
	must(".env", "API_KEY=this-value-must-never-leak\nPORT=8080\n")

	files := []string{"demo.go", "go.mod", "Makefile", ".env"}
	var stderr bytes.Buffer
	res, err := Coordinate(context.Background(), repo, files,
		CoordinatorConfig{BinaryPath: bin, Stderr: &stderr})
	if err != nil {
		t.Fatalf("Coordinate: %v\n%s", err, stderr.String())
	}

	var sawGoMod, sawMake, sawEnv bool
	for _, e := range res.Entities {
		if e.Kind != "SCOPE.Config" {
			continue
		}
		switch e.SourceFile {
		case "go.mod":
			sawGoMod = true
		case "Makefile":
			sawMake = true
		case ".env":
			sawEnv = true
			// Security: env values must never appear in any property.
			for k, v := range e.Properties {
				if bytes.Contains([]byte(v), []byte("this-value-must-never-leak")) {
					t.Errorf("SECURITY: env value leaked in property %q: %q", k, v)
				}
			}
		}
	}
	if !sawGoMod || !sawMake || !sawEnv {
		t.Errorf("missing config entities: go.mod=%v Makefile=%v .env=%v", sawGoMod, sawMake, sawEnv)
	}
}

func buildArchigraph(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "archigraph")
	cmd := exec.Command("go", "build", "-o", out, "github.com/cajasmota/archigraph/cmd/archigraph")
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("build archigraph: %v", err)
	}
	return out
}
