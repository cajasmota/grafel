package main

// #6338 — a real end-to-end index must record, in graph-stats.json, the files
// it walked past because no extractor claims their extension.
//
// This runs the actual Index() pipeline over a real temp repo rather than
// unit-testing the tally in isolation: the reported defect is that the number
// never reaches disk, and only the whole walk can show that it now does.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func write6338(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

// index6338 indexes repoRoot and returns the sidecar it wrote.
func index6338(t *testing.T, repoRoot string) *graph.GraphStatsSidecar {
	t.Helper()
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())
	out := filepath.Join(t.TempDir(), "graph.json")
	if err := Index(repoRoot, out, "test-6338", nil, false, false); err != nil {
		t.Fatalf("Index: %v", err)
	}
	side, err := graph.LoadSidecar(filepath.Dir(out))
	if err != nil {
		t.Fatalf("load sidecar: %v", err)
	}
	return side
}

// Requirements 2 + 3 + the skip-reason distinction, on the real walk.
func TestIndexRecordsUnsupportedExtensions(t *testing.T) {
	repo := t.TempDir()
	// Supported — must NOT be counted, however many there are. This is the
	// vacuity guard: a tally that counted every extension would report ".go 3".
	write6338(t, repo, "cmd/main.go", "package main\n\nfunc main() {}\n")
	write6338(t, repo, "internal/a.go", "package internal\n\nfunc A() {}\n")
	write6338(t, repo, "internal/b.go", "package internal\n\nfunc B() {}\n")
	// Unsupported — the whole point.
	write6338(t, repo, "legacy/Form1.vb", "Public Class Form1\nEnd Class\n")
	write6338(t, repo, "legacy/Form2.vb", "Public Class Form2\nEnd Class\n")
	write6338(t, repo, "legacy/sub/Mod1.vb", "Module Mod1\nEnd Module\n")
	write6338(t, repo, "legacy/unit1.pas", "unit Unit1;\nend.\n")
	// Skipped for reasons that are NOT extractor coverage. NOTE: at THIS layer
	// these two are weak evidence — measured with a mutant that made the tally
	// count every skip reason, the assertion below still passed, because the
	// repo walker drops vendor/ and binary files before classification is even
	// reached. The skip-reason filter is bound where it actually lives:
	// classifier.TestUnsupportedTally_OtherSkipReasonsNotCounted and
	// extract.TestBucketByLanguage_TalliesUnsupportedExtensions, both of which
	// that mutant does kill. They stay here as a end-to-end sanity check only.
	write6338(t, repo, "vendor/github.com/x/y/z.vb", "Public Class Z\nEnd Class\n")
	write6338(t, repo, "assets/logo.png", "\x89PNG\r\n\x1a\n")

	side := index6338(t, repo)

	want := map[string]int{".vb": 3, ".pas": 1}
	if !reflect.DeepEqual(side.UnsupportedExtensions, want) {
		t.Fatalf("sidecar unsupported_extensions:\n got  %v\n want %v",
			side.UnsupportedExtensions, want)
	}
}

// Requirement 1 on the real walk: a repo with full extractor coverage records
// NOTHING — the key is absent from the JSON entirely, not present-and-empty,
// so every downstream consumer prints nothing without needing to know about
// this feature.
func TestIndexRecordsNothingForFullySupportedRepo(t *testing.T) {
	repo := t.TempDir()
	write6338(t, repo, "cmd/main.go", "package main\n\nfunc main() {}\n")
	write6338(t, repo, "internal/a.go", "package internal\n\nfunc A() {}\n")

	side := index6338(t, repo)
	if len(side.UnsupportedExtensions) != 0 {
		t.Fatalf("a fully-supported repo must record nothing, got %v", side.UnsupportedExtensions)
	}

	// And the key must not be in the serialised JSON at all.
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())
	out := filepath.Join(t.TempDir(), "graph.json")
	if err := Index(repo, out, "test-6338", nil, false, false); err != nil {
		t.Fatalf("Index: %v", err)
	}
	raw, err := os.ReadFile(graph.SidecarPath(filepath.Dir(out)))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	if got := string(raw); strings.Contains(got, "unsupported_extensions") {
		t.Fatalf("clean repo's sidecar must omit the key entirely, got:\n%s", got)
	}
}
