package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/enrichment"
)

// TestEnrichmentCandidates_DjangoFixture confirms a full index run emits
// an enrichment-candidates.json file with the expected envelope.
func TestEnrichmentCandidates_DjangoFixture(t *testing.T) {
	// Copy the django_app fixture into a tmp dir so we can inspect the
	// state output without polluting source-tree fixtures.
	// #1626: per-repo state lives in the external store; use explicit stateDir.
	stateDir := t.TempDir()
	tmp := t.TempDir()
	src, err := filepath.Abs("testdata/django_app")
	if err != nil {
		t.Fatal(err)
	}
	if err := copyTree(src, tmp); err != nil {
		t.Fatalf("copyTree: %v", err)
	}

	idx := newTestIndexer(t, "django_app", nil, stateDir)
	doc, err := idx.Run(context.Background(), tmp)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// #5720 — Pass 6 candidate emission is no longer part of Run(); Index()
	// schedules it AFTER graph.fb is written (inline for small graphs, on
	// the background worker for large ones). Tests exercising Run() directly
	// invoke the same inline entry point Index() uses for small graphs.
	idx.runPass6EmitEnrichmentCandidates(doc, tmp)
	// runPass6 writes into the per-repo store state dir.
	candPath := filepath.Join(daemon.StateDirForRepo(tmp), "enrichment-candidates.json")
	data, err := os.ReadFile(candPath)
	if err != nil {
		t.Fatalf("read candidates: %v (entities=%d)", err, len(doc.Entities))
	}
	var env struct {
		Version    int                    `json:"version"`
		Candidates []enrichment.Candidate `json:"candidates"`
		Bare       []enrichment.Candidate `json:"-"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Version != enrichment.CandidatesSchemaVersion {
		t.Fatalf("version = %d, want %d", env.Version, enrichment.CandidatesSchemaVersion)
	}
	if len(env.Candidates) == 0 {
		t.Fatalf("expected at least 1 enrichment candidate, got 0")
	}
	// Every candidate must have a non-empty id, kind, subject_id.
	for i, c := range env.Candidates {
		if c.ID == "" || c.Kind == "" || c.SubjectID == "" {
			t.Fatalf("candidate %d malformed: %+v", i, c)
		}
	}
}

// TestEnrichmentResolutions_MergeBack confirms a pre-seeded resolutions
// file causes ApplyResolutions to populate entity.Properties before the
// next emit, and that the previously-resolved entity no longer produces
// a describe_entity candidate.
func TestEnrichmentResolutions_MergeBack(t *testing.T) {
	// #1626: per-repo state lives in the external store; use explicit stateDir
	// shared across both runs so the second run can load the pre-seeded resolutions.
	stateDir := t.TempDir()
	tmp := t.TempDir()
	src, err := filepath.Abs("testdata/django_app")
	if err != nil {
		t.Fatal(err)
	}
	if err := copyTree(src, tmp); err != nil {
		t.Fatalf("copyTree: %v", err)
	}

	// First run — discover an entity ID we can pre-resolve.
	idx := newTestIndexer(t, "django_app", nil, stateDir)
	doc, err := idx.Run(context.Background(), tmp)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	idx.runPass6EmitEnrichmentCandidates(doc, tmp)
	if len(doc.Entities) == 0 {
		t.Fatalf("no entities extracted")
	}
	subject := doc.Entities[0].ID

	// Seed a resolution file mapping that subject to a "description".
	archDir := daemon.StateDirForRepo(tmp)
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		t.Fatal(err)
	}
	resolutions := []enrichment.Resolution{{
		ID:        "ec:test",
		SubjectID: subject,
		Kind:      "description",
		Value:     "Pre-resolved by agent.",
	}}
	rd, _ := json.MarshalIndent(resolutions, "", "  ")
	if err := os.WriteFile(filepath.Join(archDir, "enrichment-resolutions.json"), rd, 0o644); err != nil {
		t.Fatal(err)
	}

	// Second run — Pass 6 should merge the resolution back AND skip
	// describe_entity for that subject.
	idx2 := newTestIndexer(t, "django_app", nil, stateDir)
	doc2, err := idx2.Run(context.Background(), tmp)
	if err != nil {
		t.Fatalf("Run 2: %v", err)
	}
	idx2.runPass6EmitEnrichmentCandidates(doc2, tmp)
	var got *string
	for i := range doc2.Entities {
		if doc2.Entities[i].ID == subject {
			if v, ok := doc2.Entities[i].Properties["description"]; ok {
				got = &v
			}
			break
		}
	}
	if got == nil || *got != "Pre-resolved by agent." {
		t.Fatalf("description not merged onto entity: %v", got)
	}

	// And the candidates file must NOT contain a describe_entity row for
	// that subject any more.
	data, err := os.ReadFile(filepath.Join(archDir, "enrichment-candidates.json"))
	if err != nil {
		t.Fatalf("read candidates: %v", err)
	}
	var env struct {
		Candidates []enrichment.Candidate `json:"candidates"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	for _, c := range env.Candidates {
		if c.SubjectID == subject && c.Kind == enrichment.KindDescribeEntity {
			t.Fatalf("describe_entity candidate not skipped for already-resolved subject %s", subject)
		}
	}
}

// copyTree recursively copies src into dst (which already exists).
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}
