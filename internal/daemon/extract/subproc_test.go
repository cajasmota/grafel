package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
