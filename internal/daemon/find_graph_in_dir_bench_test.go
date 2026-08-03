package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// #6080 asked for the freshness gate's cost to be MEASURED rather than assumed,
// because #6060 exists to keep the serving reload path free of git subprocesses.
// These two benchmarks are the before/after of the gate's per-repo-per-reload
// work: BenchmarkGate_StatPinnedFile is what it used to do (one os.Stat of the
// pinned path, which is why it could not see a generation flip);
// BenchmarkGate_FindGraphFileInDir is what it does now.
//
// Both are pure filesystem calls on a tiny directory. Neither forks.
func benchStateDir(b *testing.B) (dir, genPath string) {
	b.Helper()
	dir = b.TempDir()
	p, err := graph.WriteGenGraph(dir, []byte("not-a-real-graph-but-a-real-file"))
	if err != nil {
		b.Fatalf("WriteGenGraph: %v", err)
	}
	return dir, p
}

func BenchmarkGate_StatPinnedFile(b *testing.B) {
	_, genPath := benchStateDir(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := os.Stat(genPath); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGate_FindGraphFileInDir(b *testing.B) {
	dir, genPath := benchStateDir(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if p, _ := FindGraphFileInDir(dir); p != genPath {
			b.Fatalf("resolved %s, want %s", p, genPath)
		}
	}
}

// BenchmarkGate_StateDirForRepo is the cost the gate must NOT pay per reload —
// the git-capture path #2550/#6060 removed. Present for scale: the two
// benchmarks above must stay orders of magnitude below it.
func BenchmarkGate_StateDirForRepo(b *testing.B) {
	dir := b.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = StateDirForRepo(dir)
	}
}
