package extract

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// #6338 — on the subprocess-extract path, a file with no extractor is dropped
// by bucketByLanguage and is never written into a batch, so no subprocess ever
// sees it and none could report it. The coordinator is the only place on this
// path where it can be counted.
func TestBucketByLanguage_TalliesUnsupportedExtensions(t *testing.T) {
	repo := t.TempDir()
	files := []string{
		"cmd/main.go",
		"internal/a.go",
		// Two DISTINCT unsupported extensions, one at a count > 1 — that pair
		// is what proves the tally aggregates per-extension.
		//
		// NOTE: both MUST stay absent from classifier.extensionLanguageMap
		// (internal/classifier/classifier.go:313). These were ".vb" until
		// #6327 S5 registered a VB.NET extractor and .vb correctly stopped
		// being tallied. If Fortran or Pascal ever ships an extractor, swap in
		// another still-unsupported extension — do not just shrink the
		// expectation, or this test goes green while testing nothing.
		"legacy/solver1.f90",
		"legacy/solver2.f90",
		"legacy/sub/kernel.f90",
		"legacy/unit1.pas",
		// Not extractor coverage — must not be folded in.
		"vendor/github.com/x/y/z.f90",
		"assets/logo.png",
	}
	for _, rel := range files {
		p := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	buckets, unsupported := bucketByLanguage(context.Background(), repo, files)

	want := map[string]int{".f90": 3, ".pas": 1}
	if !reflect.DeepEqual(unsupported, want) {
		t.Fatalf("unsupported tally:\n got  %v\n want %v", unsupported, want)
	}
	// The buckets themselves must be unchanged: the tally is pure observation.
	if got := len(buckets["go"]); got != 2 {
		t.Fatalf("bucketing regressed: go bucket has %d files, want 2", got)
	}
	// Named-key checks are vacuous here — "fortran" is not a language tag the
	// classifier can ever emit. Assert on the bucket set instead: go is the
	// ONLY bucket, so every unsupported file was dropped.
	if len(buckets) != 1 {
		t.Fatalf("unsupported files must still be dropped from the buckets, got %v", buckets)
	}
}

// A repo with full extractor coverage tallies nothing, so the Result carries an
// empty map and every downstream consumer prints nothing.
func TestBucketByLanguage_NoTallyWhenFullySupported(t *testing.T) {
	repo := t.TempDir()
	for _, rel := range []string{"a.go", "b.go", "c.py"} {
		if err := os.WriteFile(filepath.Join(repo, rel), []byte("x\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	_, unsupported := bucketByLanguage(context.Background(), repo, []string{"a.go", "b.go", "c.py"})
	if len(unsupported) != 0 {
		t.Fatalf("fully-supported repo must tally nothing, got %v", unsupported)
	}
}
