package main

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// overlapSink is a sink that records whether two writers were ever inside it
// at once, and — deliberately — keeps its bytes in an UNSYNCHRONISED
// bytes.Buffer.
//
// Both properties matter. The overlap counter is a deterministic assertion in
// the only direction that can fail: while fanoutWriter holds its mutex across
// the whole Write, no two writers can ever be inside a sink together, so a
// non-zero count is proof the serialisation is gone, never scheduling luck.
// The unsynchronised buffer is what gives `go test -race` something to observe
// — a detector over single-threaded tests reports nothing by construction, and
// that was exactly the hole here.
type overlapSink struct {
	inside   atomic.Int32
	overlaps atomic.Int32
	buf      bytes.Buffer // intentionally unguarded; see above.
}

func (s *overlapSink) Write(p []byte) (int, error) {
	if s.inside.Add(1) > 1 {
		s.overlaps.Add(1)
	}
	n, err := s.buf.Write(p)
	runtime.Gosched() // widen the window a lost mutex would open.
	s.inside.Add(-1)
	return n, err
}

// countingErrWriter is a sink that always fails, counting its calls without
// racing on the counter itself (the race we are hunting is in fanoutWriter's
// own state, not in the test's bookkeeping).
type countingErrWriter struct{ n atomic.Int32 }

func (e *countingErrWriter) Write(p []byte) (int, error) {
	e.n.Add(1)
	return 0, errors.New("write /dev/stderr: The handle is invalid.")
}

// #7050 review round 4 (CM-2): the mutex added for the one-shot failure notice
// was graded by nothing. Removing it entirely left the suite green — including
// under `go test -race`, because no test ever wrote to a fanout from more than
// one goroutine, and a race detector reports only races it actually observes.
//
// The daemon is not single-threaded: runDaemon spawns several goroutines and
// the logger is process-wide, so s.reported and f.emittingNotice are
// read-modify-write state under exactly the interleaving the mutex exists for.
//
// The assertions here are interleaving-independent by construction: the SET of
// payloads each surviving sink received (never their order), the notice count,
// and the overlap counter, which can only be non-zero if writers stopped being
// serialised.
func TestFanoutWriter_ConcurrentWriters_SerialisedAndNoticedOnce(t *testing.T) {
	const (
		writers          = 8
		recordsPerWriter = 40
	)

	first := &overlapSink{}
	dead := &countingErrWriter{}
	last := &overlapSink{}
	w := newFanoutWriter(first, dead, last)

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < recordsPerWriter; j++ {
				if _, err := w.Write([]byte(fmt.Sprintf("rec-%d-%d\n", id, j))); err != nil {
					t.Errorf("writer %d record %d: %v", id, j, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	for name, sink := range map[string]*overlapSink{"first": first, "last": last} {
		if n := sink.overlaps.Load(); n != 0 {
			t.Errorf("%s sink saw %d overlapping writes — fanout writes are no longer serialised", name, n)
		}
		got := sink.buf.String()

		// Every record, exactly once. Order is not asserted: it is not an
		// invariant, and asserting it would make the test scheduling-dependent.
		for i := 0; i < writers; i++ {
			for j := 0; j < recordsPerWriter; j++ {
				rec := fmt.Sprintf("rec-%d-%d\n", i, j)
				if c := strings.Count(got, rec); c != 1 {
					t.Fatalf("%s sink holds %q %d times, want exactly 1 (of %d records)",
						name, strings.TrimSpace(rec), c, writers*recordsPerWriter)
				}
			}
		}

		// The failed sink is announced once for the whole run, no matter how
		// many goroutines raced into the failure path together.
		if c := strings.Count(got, "log fanout: a log sink failed"); c != 1 {
			t.Fatalf("%s sink holds %d failure notices, want exactly 1", name, c)
		}
	}

	if got, want := dead.n.Load(), int32(writers*recordsPerWriter); got != want {
		t.Fatalf("failing sink was attempted %d times, want %d — every record must still try it", got, want)
	}
}
