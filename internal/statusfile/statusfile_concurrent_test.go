package statusfile_test

import (
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/statusfile"
)

// TestWrite_ConcurrentSameRepo_NoTornRead reproduces the review #5734 BLOCKING
// finding: concurrent Writes to the SAME repo must never publish a torn/garbled
// file that a concurrent Read decodes as corrupt.
//
// With the original fixed tmp name (`tmp := path + ".tmp"`), N writers all
// O_TRUNC the same tmp inode and interleave, then rename a garbled file into
// place — a looping reader intermittently gets a JSON unmarshal error and (in
// the CLI) falls back to Status:"unknown" mid-index, exactly what the status
// plane must not do. The reviewer measured 2 corrupt reads / 1884.
//
// This test drives 8 concurrent writers to one repo plus a looping reader and
// asserts ZERO corrupt reads. It fails against the fixed-tmp implementation and
// passes with the unique-tmp (os.CreateTemp) fix.
func TestWrite_ConcurrentSameRepo_NoTornRead(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repo := "/some/repo/under/concurrent/write"

	// Seed once so the reader always finds the file (a legitimate NotExist
	// before the first write is not a torn read).
	if err := statusfile.Write(repo, &statusfile.File{EnginePID: 1, RepoPath: repo}); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	const (
		writers         = 8
		writesPerWorker = 400
	)

	var (
		writersWG sync.WaitGroup
		readerWG  sync.WaitGroup
		stop      atomic.Bool
		corrupt   atomic.Int64
		reads     atomic.Int64
		firstErr  atomic.Value // string: first corrupt-read description
	)

	// Reader: spins reading the file, counting any decode failure. os.IsNotExist
	// is impossible after the seed write (Write renames atomically), so any error
	// here is a genuine torn/corrupt read.
	readerWG.Add(1)
	go func() {
		defer readerWG.Done()
		for !stop.Load() {
			f, err := statusfile.Read(repo)
			reads.Add(1)
			if err != nil {
				if os.IsNotExist(err) {
					continue // impossible post-seed; never a torn read
				}
				corrupt.Add(1)
				firstErr.CompareAndSwap(nil, err.Error())
				continue
			}
			// A well-formed-but-truncated JSON could still decode to a
			// zero-value struct, so assert an invariant every writer upholds.
			if f.RepoPath != repo {
				corrupt.Add(1)
				firstErr.CompareAndSwap(nil, "unexpected RepoPath: "+f.RepoPath)
			}
		}
	}()

	for w := 0; w < writers; w++ {
		writersWG.Add(1)
		go func(id int) {
			defer writersWG.Done()
			for i := 0; i < writesPerWorker; i++ {
				f := &statusfile.File{
					EnginePID:     id*writesPerWorker + i,
					HeartbeatAt:   time.Now().UTC(),
					Version:       "concurrent-writer",
					RepoPath:      repo,
					IndexedCommit: "deadbeefcafe",
					Entities:      int64(i),
					Relationships: int64(id),
				}
				if err := statusfile.Write(repo, f); err != nil {
					corrupt.Add(1)
					firstErr.CompareAndSwap(nil, "write error: "+err.Error())
					return
				}
			}
		}(w)
	}

	writersWG.Wait()
	stop.Store(true)
	readerWG.Wait()

	if c := corrupt.Load(); c != 0 {
		t.Fatalf("torn/corrupt reads = %d over %d reads (first: %v) — concurrent same-repo Write must be atomic",
			c, reads.Load(), firstErr.Load())
	}
	if reads.Load() == 0 {
		t.Fatal("reader never observed a read — test did not exercise the race")
	}
}
