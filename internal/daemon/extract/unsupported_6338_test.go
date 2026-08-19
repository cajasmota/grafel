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
		"legacy/Form1.vb",
		"legacy/Form2.vb",
		"legacy/sub/Mod1.vb",
		"legacy/unit1.pas",
		// Not extractor coverage — must not be folded in.
		"vendor/github.com/x/y/z.vb",
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

	want := map[string]int{".vb": 3, ".pas": 1}
	if !reflect.DeepEqual(unsupported, want) {
		t.Fatalf("unsupported tally:\n got  %v\n want %v", unsupported, want)
	}
	// The buckets themselves must be unchanged: the tally is pure observation.
	if got := len(buckets["go"]); got != 2 {
		t.Fatalf("bucketing regressed: go bucket has %d files, want 2", got)
	}
	if _, ok := buckets["vb"]; ok {
		t.Fatal("unsupported files must still be dropped from the buckets")
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
