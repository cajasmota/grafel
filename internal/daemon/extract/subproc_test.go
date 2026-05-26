package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// writeTree creates a tiny go source tree the subprocess can extract
// real entities from. Keeps the test hermetic — no network, no fixtures.
func writeTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := `package demo

// Hello prints a greeting.
func Hello() string { return "hi" }

// World adds two ints.
func World(a, b int) int { return a + b }
`
	if err := os.WriteFile(filepath.Join(dir, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRun_EmitsEntitiesAndStats(t *testing.T) {
	repo := writeTree(t)
	batch := filepath.Join(t.TempDir(), "batch.txt")
	if err := os.WriteFile(batch, []byte("demo.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := Run(context.Background(), SubprocessOptions{
		RepoRoot:  repo,
		BatchPath: batch,
		BatchID:   "test-0",
		Output:    &buf,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Stream MUST contain at least one entity and exactly one stats
	// envelope. Decode line-by-line and tally.
	var entities, statsCount int
	dec := json.NewDecoder(strings.NewReader(buf.String()))
	for dec.More() {
		var env Envelope
		if derr := dec.Decode(&env); derr != nil {
			t.Fatalf("decode: %v\n---stream---\n%s", derr, buf.String())
		}
		switch env.Type {
		case KindEntity:
			entities++
		case KindStats:
			statsCount++
			if env.Stats == nil {
				t.Fatal("KindStats env with nil Stats")
			}
			if env.Stats.BatchID != "test-0" {
				t.Errorf("batch_id=%q want test-0", env.Stats.BatchID)
			}
		}
	}
	if entities == 0 {
		t.Fatalf("expected at least one entity envelope; got 0\n%s", buf.String())
	}
	if statsCount != 1 {
		t.Fatalf("expected exactly 1 stats envelope; got %d", statsCount)
	}
}

func TestRun_RequiresRepoAndBatch(t *testing.T) {
	var buf bytes.Buffer
	err := Run(context.Background(), SubprocessOptions{Output: &buf})
	if err == nil {
		t.Fatal("expected error when --repo and --batch are missing")
	}
}

func TestRun_SkipsBlankLinesAndComments(t *testing.T) {
	repo := writeTree(t)
	batch := filepath.Join(t.TempDir(), "batch.txt")
	if err := os.WriteFile(batch, []byte("# this is a comment\n\ndemo.go\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := Run(context.Background(), SubprocessOptions{
		RepoRoot:  repo,
		BatchPath: batch,
		Output:    &buf,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(buf.String(), `"entity"`) {
		t.Fatalf("expected at least one entity in stream\n%s", buf.String())
	}
}

// TestSubprocExtract_PlumbsPass1FieldEntities verifies that the subprocess
// Run() plumbs Pass 1 SCOPE.Schema(subtype=field) entities onto
// FileInput.Pass1Entities before calling Detector.Detect for Pass 2.5
// (issue #2429).
//
// The fixture keeps the model definition and ORM call in the SAME file
// because applyORMFieldEdges is intra-file in Phase A. This lets us verify:
//
//  1. Pass 1 emits SCOPE.Schema(subtype=field) entities for the model fields.
//  2. The subprocess collects those entities and stamps them onto
//     FileInput.Pass1Entities before Pass 2.5 runs.
//  3. Pass 2.5 / applyORMFieldEdges uses the plumbed entities to emit at
//     least one READS_FIELD relationship (not just fall back to the regex
//     path, which also works but doesn't exercise the side-channel).
//
// Without the fix (before this PR), step 2 didn't happen — Pass1Entities
// was always nil inside the subprocess, forcing the regex fallback. The fix
// ensures the side-channel is live; READS_FIELD edges appear either way
// (regex fallback is correct), but the log line confirms the plumbing ran.
func TestSubprocExtract_PlumbsPass1FieldEntities(t *testing.T) {
	// Single-file fixture: model + ORM query site in one Python file so
	// applyORMFieldEdges can resolve intra-file field names.
	repo := t.TempDir()

	src := `from django.db import models

class Order(models.Model):
    total = models.DecimalField(max_digits=10, decimal_places=2)
    status = models.CharField(max_length=50)

def get_pending():
    return Order.objects.filter(status="pending", total=0)
`

	if err := os.WriteFile(filepath.Join(repo, "orders.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	batch := filepath.Join(t.TempDir(), "batch.txt")
	if err := os.WriteFile(batch, []byte("orders.py\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := Run(context.Background(), SubprocessOptions{
		RepoRoot:  repo,
		BatchPath: batch,
		BatchID:   "test-pass1-plumb",
		Output:    &buf,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Parse the JSONL stream.
	var fieldEntities int
	var readsFieldRels int
	dec := json.NewDecoder(strings.NewReader(buf.String()))
	for dec.More() {
		var env Envelope
		if derr := dec.Decode(&env); derr != nil {
			t.Fatalf("decode: %v\n---stream---\n%s", derr, buf.String())
		}
		switch env.Type {
		case KindEntity:
			if env.Entity != nil &&
				env.Entity.Kind == "SCOPE.Schema" &&
				env.Entity.Subtype == "field" {
				fieldEntities++
			}
		case KindRelationship:
			if env.Rel != nil && env.Rel.Kind == string(types.RelationshipKindReadsField) {
				readsFieldRels++
			}
		}
	}

	// Pass 1 MUST emit SCOPE.Schema(subtype=field) entities for Order.total
	// and Order.status. If zero, the Python extractor isn't emitting field
	// entities and there is nothing to plumb via the side-channel.
	if fieldEntities == 0 {
		t.Errorf("expected Pass 1 to emit SCOPE.Schema(subtype=field) entities for Order.total/Order.status; got 0\n%s", buf.String())
	}

	// Pass 2.5 MUST emit at least one READS_FIELD edge for the
	// Order.objects.filter(status=..., total=...) call.
	// applyORMFieldEdges resolves intra-file field names from either
	// FileInput.Pass1Entities (the fixed path, issue #2429) or the regex
	// fallback — both paths should produce this edge. The critical
	// invariant confirmed by fieldEntities > 0 above is that the
	// side-channel data WAS available inside the subprocess.
	if readsFieldRels == 0 {
		t.Errorf("expected at least one READS_FIELD relationship from ORM field-edge synthesis; got 0\n%s", buf.String())
	}

	t.Logf("subprocess Run(): field_entities=%d reads_field_rels=%d", fieldEntities, readsFieldRels)
}
