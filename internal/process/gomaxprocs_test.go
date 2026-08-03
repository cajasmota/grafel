package process

import (
	"errors"
	"runtime"
	"sync"
	"testing"
)

// gomaxprocs_test.go — behavioural coverage for WithGOMAXPROCSCap (#6108).
//
// These assert the EFFECTIVE runtime.GOMAXPROCS observed from inside the
// callback, not the presence of any particular source construct. Deleting the
// clamp, inverting the min, or forgetting the restore each fails one of them.

func TestWithGOMAXPROCSCap_ClampsInsideCallback(t *testing.T) {
	before := runtime.GOMAXPROCS(0)
	if before < 2 {
		t.Skipf("host GOMAXPROCS=%d — nothing to clamp below", before)
	}
	want := before - 1

	var inside int
	err := WithGOMAXPROCSCap(want, func() error {
		inside = runtime.GOMAXPROCS(0)
		return nil
	})
	if err != nil {
		t.Fatalf("WithGOMAXPROCSCap: %v", err)
	}
	if inside != want {
		t.Errorf("GOMAXPROCS inside the capped region = %d, want %d — the cap is not in force on the work it is supposed to bound (#6108)", inside, want)
	}
	if after := runtime.GOMAXPROCS(0); after != before {
		t.Errorf("GOMAXPROCS after the capped region = %d, want the prior %d — the cap leaked past the pass and throttles the whole daemon", after, before)
	}
}

func TestWithGOMAXPROCSCap_NeverRaises(t *testing.T) {
	before := runtime.GOMAXPROCS(0)
	var inside int
	err := WithGOMAXPROCSCap(before+8, func() error {
		inside = runtime.GOMAXPROCS(0)
		return nil
	})
	if err != nil {
		t.Fatalf("WithGOMAXPROCSCap: %v", err)
	}
	if inside != before {
		t.Errorf("GOMAXPROCS inside = %d, want the unchanged %d — a cap ABOVE the host value must never widen the runtime (foreground work is uncapped, not over-capped)", inside, before)
	}
	if after := runtime.GOMAXPROCS(0); after != before {
		t.Errorf("GOMAXPROCS after = %d, want %d", after, before)
	}
}

func TestWithGOMAXPROCSCap_NonPositiveIsNoOp(t *testing.T) {
	before := runtime.GOMAXPROCS(0)
	for _, n := range []int{0, -1} {
		var inside int
		if err := WithGOMAXPROCSCap(n, func() error { inside = runtime.GOMAXPROCS(0); return nil }); err != nil {
			t.Fatalf("WithGOMAXPROCSCap(%d): %v", n, err)
		}
		if inside != before {
			t.Errorf("WithGOMAXPROCSCap(%d): GOMAXPROCS inside = %d, want %d — a bogus core count must not pin the process to 1", n, inside, before)
		}
	}
	if after := runtime.GOMAXPROCS(0); after != before {
		t.Errorf("GOMAXPROCS after = %d, want %d", after, before)
	}
}

func TestWithGOMAXPROCSCap_RestoresOnErrorAndPanic(t *testing.T) {
	before := runtime.GOMAXPROCS(0)
	if before < 2 {
		t.Skipf("host GOMAXPROCS=%d — nothing to clamp below", before)
	}

	sentinel := errors.New("boom")
	if err := WithGOMAXPROCSCap(1, func() error { return sentinel }); !errors.Is(err, sentinel) {
		t.Errorf("error not propagated: got %v, want %v", err, sentinel)
	}
	if after := runtime.GOMAXPROCS(0); after != before {
		t.Errorf("GOMAXPROCS after an erroring pass = %d, want %d", after, before)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Errorf("panic was swallowed — a wedged pass must still crash loudly")
			}
		}()
		_ = WithGOMAXPROCSCap(1, func() error { panic("wedged") })
	}()
	if after := runtime.GOMAXPROCS(0); after != before {
		t.Errorf("GOMAXPROCS after a panicking pass = %d, want %d — the daemon is left throttled for the rest of its life", after, before)
	}
}

// TestWithGOMAXPROCSCap_SerialisesConcurrentCallers: two overlapping capped
// regions must not have the inner one's restore clobber the outer's baseline.
// Serialising is the simplest correct answer (background heavy passes are
// already mutually exclusive under the daemon's stage gate).
func TestWithGOMAXPROCSCap_SerialisesConcurrentCallers(t *testing.T) {
	before := runtime.GOMAXPROCS(0)
	if before < 2 {
		t.Skipf("host GOMAXPROCS=%d — nothing to clamp below", before)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = WithGOMAXPROCSCap(1, func() error {
				if got := runtime.GOMAXPROCS(0); got != 1 {
					t.Errorf("GOMAXPROCS inside a concurrent capped region = %d, want 1", got)
				}
				return nil
			})
		}()
	}
	wg.Wait()
	if after := runtime.GOMAXPROCS(0); after != before {
		t.Errorf("GOMAXPROCS after concurrent capped regions = %d, want %d — a restore raced and the baseline was lost", after, before)
	}
}
