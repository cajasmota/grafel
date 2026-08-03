package sched

// group_algo_log_honesty_6108_test.go — the group-algo scheduler log must not
// assert a bound it cannot account for, and must not go silent (#6108).
//
// Observed: `group-algo: starting group=… cap=2` followed by ~4 hours of
// nothing, while the process sustained 571.9% CPU. Two separate log defects:
// the line named a cap whose enforcement depended on a branch the line did not
// mention, and there was no way to tell a queued pass from a running one from a
// wedged one.

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func captureLogger() (*slog.Logger, func() string) {
	var mu sync.Mutex
	buf := &bytes.Buffer{}
	h := slog.NewTextHandler(&lockedWriter{mu: &mu, w: buf}, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), func() string {
		mu.Lock()
		defer mu.Unlock()
		return buf.String()
	}
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// TestGroupAlgoStartLog_NamesTheEnforcementMode: `cap=` is only meaningful
// alongside WHICH path is enforcing it. Before #6108 the in-process path
// enforced nothing at all, and the line was indistinguishable from the child's.
func TestGroupAlgoStartLog_NamesTheEnforcementMode(t *testing.T) {
	for _, tc := range []struct {
		name       string
		subprocess bool
		wantMode   string
	}{
		{"child", true, "mode=child"},
		{"in-process", false, "mode=in-process"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prev := SetSubprocessIndexEnabled(tc.subprocess)
			t.Cleanup(func() { SetSubprocessIndexEnabled(prev) })

			logger, dump := captureLogger()
			s := New(Config{
				Workers:            1,
				LinkDebounce:       time.Hour,
				GroupAlgoDebounce:  time.Hour,
				GroupAlgoMaxWait:   time.Hour,
				Logger:             logger,
				Links:              func(_ context.Context, _ string) error { return nil },
				GroupAlgo:          func(_ context.Context, _ string) error { return nil },
				GroupsForRepo:      func(_ string) []string { return nil },
				MemReleaseDisabled: true,
			})
			s.Start()
			defer s.Stop()

			s.runGroupAlgo(context.Background(), "gLog")

			out := dump()
			if !strings.Contains(out, "group-algo: starting") {
				t.Fatalf("no start line emitted at all:\n%s", out)
			}
			if !strings.Contains(out, tc.wantMode) {
				t.Errorf("start line does not carry %q — `cap=` names a bound whose enforcement depends on this branch, so the branch must be on the line (#6108):\n%s", tc.wantMode, out)
			}
			if !strings.Contains(out, "cap=") {
				t.Errorf("start line lost its cap= field:\n%s", out)
			}
		})
	}
}

// TestGroupAlgoQueuedPass_SaysItIsQueued: a pass that cannot get an algo slot
// must say so. Otherwise "starting" + silence covers both "running slowly" and
// "not running at all", which is exactly the ambiguity #6108 was diagnosed
// through.
func TestGroupAlgoQueuedPass_SaysItIsQueued(t *testing.T) {
	logger, dump := captureLogger()
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	s := New(Config{
		Workers:           1,
		AlgoCap:           1,
		LinkDebounce:      time.Hour,
		GroupAlgoDebounce: time.Hour,
		GroupAlgoMaxWait:  time.Hour,
		Logger:            logger,
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(ctx context.Context, _ string) error {
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
			return nil
		},
		GroupsForRepo:      func(_ string) []string { return nil },
		MemReleaseDisabled: true,
	})
	s.Start()
	defer s.Stop()

	go s.runGroupAlgo(context.Background(), "gHold")
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first pass never entered GroupAlgo")
	}

	done := make(chan struct{})
	go func() { defer close(done); s.runGroupAlgo(context.Background(), "gQueued") }()

	deadline := time.After(5 * time.Second)
	for {
		if strings.Contains(dump(), "group-algo: waiting for an algo slot") {
			break
		}
		select {
		case <-deadline:
			close(release)
			<-done
			t.Fatalf("a group-algo pass blocked on the algo semaphore logged nothing — an operator cannot tell it apart from a pass that is running (#6108):\n%s", dump())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	close(release)
	<-done
}
