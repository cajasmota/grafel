package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// TestCoordinate_CrossBatchORMFieldLookup is the multi-batch integration test
// for issue #2505.
//
// Setup:
//   - Batch A: models.py — defines the Django User model with field cognito_id.
//   - Batch B: views.py  — queries User.objects.filter(cognito_id=...).
//
// The coordinator must scan ALL Python files before spawning subprocesses and
// write a shared ORM-fields file. Each subprocess loads this file so models
// from other batches are visible.
//
// Expected outcome:
//   - At least one READS_FIELD relationship emitted for cognito_id (the cross-
//     batch field reference in views.py).
//
// Skipped under `go test -short`.
func TestCoordinate_CrossBatchORMFieldLookup(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test — skipped in -short mode")
	}
	bin := buildArchigraph(t)

	repo := t.TempDir()
	must := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Batch A source: Django model definition in models.py.
	must("models.py", `from django.db import models

class User(models.Model):
    cognito_id = models.CharField(max_length=200)
    email = models.EmailField()
`)

	// Batch B source: ORM query in views.py — references User.cognito_id
	// which is defined in a DIFFERENT file (models.py).
	must("views.py", `from django.http import JsonResponse
from .models import User

def get_user_by_cognito(request, uid):
    user = User.objects.filter(cognito_id=uid).first()
    return JsonResponse({"found": user is not None})
`)

	files := []string{"models.py", "views.py"}

	// Run via Coordinate — the coordinator must write an ORM-fields file
	// and pass --orm-fields to each subprocess so that views.py (which
	// may land in a separate batch from models.py) can resolve cognito_id.
	var stderrBuf bytes.Buffer
	res, err := Coordinate(context.Background(), repo, files, CoordinatorConfig{
		BinaryPath: bin,
		BatchSize:  1, // force 1 file per subprocess → guaranteed cross-batch
		Stderr:     &stderrBuf,
	})
	if err != nil {
		t.Fatalf("Coordinate: %v\n---stderr---\n%s", err, stderrBuf.String())
	}

	// Count READS_FIELD relationships in the result.
	var readsField int
	for _, r := range res.Relationships {
		if r.Kind == string(types.RelationshipKindReadsField) {
			readsField++
		}
	}

	t.Logf("cross-batch ORM test: READS_FIELD=%d subprocesses=%d stderr=%s",
		readsField, res.Subprocesses, stderrBuf.String())

	// With the fix: at least one READS_FIELD edge must exist for
	// User.cognito_id resolved from views.py across the batch boundary.
	if readsField == 0 {
		t.Errorf("expected at least one READS_FIELD edge for cross-batch ORM reference "+
			"(views.py → User.cognito_id defined in models.py); got 0\n"+
			"subprocesses=%d — confirm BatchSize=1 forced cross-batch split\n"+
			"stderr:\n%s", res.Subprocesses, stderrBuf.String())
	}

	// Confirm the coordinator actually spawned multiple subprocesses (one
	// per file with BatchSize=1) — otherwise we're testing intra-batch only.
	if res.Subprocesses < 2 {
		t.Errorf("expected >=2 subprocesses with BatchSize=1 for 2 Python files; got %d", res.Subprocesses)
	}
}

func buildArchigraph(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	name := "archigraph"
	if runtime.GOOS == "windows" {
		name = "archigraph.exe"
	}
	out := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", out, "github.com/cajasmota/archigraph/cmd/archigraph")
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("build archigraph: %v", err)
	}
	return out
}

// TestCoordinate_Pass1EntitiesEquivalence is the equivalence test for
// issue #2429: the subprocess-extract path (Coordinate) and the in-process
// path (Run directly) must produce the same number of READS_FIELD /
// WRITES_FIELD relationship edges when given the same Python ORM fixture.
//
// Before this fix, the subprocess path didn't stamp FileInput.Pass1Entities,
// so applyORMFieldEdges fell back to the regex field index. The fallback
// produces the same edges when the model is in the same file — but the
// plumbing fix is what ensures the side-channel is live for future Phase B
// cross-file extension. This test confirms both paths produce identical edge
// counts.
func TestCoordinate_Pass1EntitiesEquivalence(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test — skipped in -short mode")
	}
	bin := buildArchigraph(t)

	// Fixture: single Python file with Django model definition + ORM query
	// accessing the model's own fields (intra-file resolution, Phase A scope).
	repo := t.TempDir()
	src := `from django.db import models

class Product(models.Model):
    name = models.CharField(max_length=200)
    price = models.DecimalField(max_digits=10, decimal_places=2)
    stock = models.IntegerField(default=0)

def find_cheap():
    return Product.objects.filter(price=0, stock=0)

def restock():
    return Product.objects.filter(name="").update(stock=100)
`
	if err := os.WriteFile(filepath.Join(repo, "products.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	files := []string{"products.py"}

	// Run via Coordinate (subprocess path — the path fixed by issue #2429).
	var stderrCoord bytes.Buffer
	coordRes, err := Coordinate(context.Background(), repo, files, CoordinatorConfig{
		BinaryPath: bin,
		Stderr:     &stderrCoord,
	})
	if err != nil {
		t.Fatalf("Coordinate: %v\n---stderr---\n%s", err, stderrCoord.String())
	}

	// Count READS_FIELD / WRITES_FIELD in the Coordinate result.
	var coordReads, coordWrites int
	for _, r := range coordRes.Relationships {
		switch r.Kind {
		case string(types.RelationshipKindReadsField):
			coordReads++
		case string(types.RelationshipKindWritesField):
			coordWrites++
		}
	}

	// Run directly via Run() (the subprocess's own entrypoint — in-process
	// equivalent for comparison).
	batch := filepath.Join(t.TempDir(), "batch.txt")
	if err := os.WriteFile(batch, []byte("products.py\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var runBuf bytes.Buffer
	if err := Run(context.Background(), SubprocessOptions{
		RepoRoot:  repo,
		BatchPath: batch,
		BatchID:   "equiv-test",
		Output:    &runBuf,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var runReads, runWrites int
	dec := json.NewDecoder(&runBuf)
	for dec.More() {
		var env Envelope
		if derr := dec.Decode(&env); derr != nil {
			t.Fatalf("decode: %v", derr)
		}
		if env.Type == KindRelationship && env.Rel != nil {
			switch env.Rel.Kind {
			case string(types.RelationshipKindReadsField):
				runReads++
			case string(types.RelationshipKindWritesField):
				runWrites++
			}
		}
	}

	t.Logf("Coordinate path:  READS_FIELD=%d WRITES_FIELD=%d", coordReads, coordWrites)
	t.Logf("Run() path:       READS_FIELD=%d WRITES_FIELD=%d", runReads, runWrites)

	if coordReads != runReads {
		t.Errorf("READS_FIELD mismatch: Coordinate=%d Run=%d (subprocess path is missing Pass1Entities plumbing?)", coordReads, runReads)
	}
	if coordWrites != runWrites {
		t.Errorf("WRITES_FIELD mismatch: Coordinate=%d Run=%d (subprocess path is missing Pass1Entities plumbing?)", coordWrites, runWrites)
	}
	if coordReads == 0 && runReads == 0 {
		t.Error("both paths produced 0 READS_FIELD edges — ORM field-edge synthesis may not be running at all")
	}
}
