package watch

// quarantine_q2_test.go — tests for the Q2 transparency surface (#5617):
// the Pin/Unpin tracker methods and the daemon-less file helpers
// (ReadQuarantineFile / UnquarantineFile / SetPinFile).

import (
	"path/filepath"
	"testing"
	"time"
)

// seedQuarantine writes a quarantine.json with the given reasons into repo's
// .grafel dir via the production write path.
func seedQuarantine(t *testing.T, repo string, dirs ...QuarantineReason) {
	t.Helper()
	if err := writeQuarantineFile(repo, dirs); err != nil {
		t.Fatalf("seed quarantine: %v", err)
	}
}

func TestPinUnpinTracker(t *testing.T) {
	repo := t.TempDir()
	q := NewQuarantineTracker(nil)
	q.now = func() time.Time { return time.Unix(1000, 0) }

	// Quarantine a dir by tripping churn directly.
	q.quarantineLocked(repo, "app/dist", "churn", "test", q.now())

	if !q.Pin(repo, "app/dist") {
		t.Fatal("Pin should report a change the first time")
	}
	if q.Pin(repo, "app/dist") {
		t.Fatal("Pin again should be a no-op (already pinned)")
	}
	// Pinned entries must survive a sweep even after the quiet window.
	q.now = func() time.Time { return time.Unix(1000, 0).Add(24 * time.Hour) }
	q.Sweep()
	if got := q.List(repo); len(got) != 1 || !got[0].Pinned {
		t.Fatalf("pinned dir must survive sweep; got %+v", got)
	}

	if !q.Unpin(repo, "app/dist") {
		t.Fatal("Unpin should report a change")
	}
	// Now eligible for heal → sweep removes it.
	q.Sweep()
	if got := q.List(repo); len(got) != 0 {
		t.Fatalf("unpinned + quiet dir should heal; got %+v", got)
	}

	// Pin on a non-quarantined dir is a no-op.
	if q.Pin(repo, "never/quarantined") {
		t.Fatal("Pin on unknown dir must be a no-op")
	}
}

func TestReadQuarantineFile(t *testing.T) {
	repo := t.TempDir()

	// Missing file → empty, no error.
	got, err := ReadQuarantineFile(repo)
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("missing file should be empty; got %+v", got)
	}

	now := time.Unix(2000, 0)
	seedQuarantine(t, repo,
		QuarantineReason{Rel: "z/last", Signal: "churn", At: now},
		QuarantineReason{Rel: "a/first", Signal: "churn", At: now, Pinned: true},
	)
	got, err = ReadQuarantineFile(repo)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 entries; got %d", len(got))
	}
	// Sorted by rel.
	if got[0].Rel != "a/first" || got[1].Rel != "z/last" {
		t.Fatalf("entries not sorted by rel: %+v", got)
	}
	if !got[0].Pinned {
		t.Fatal("pinned flag not round-tripped")
	}
}

func TestUnquarantineFile(t *testing.T) {
	repo := t.TempDir()
	now := time.Unix(3000, 0)
	seedQuarantine(t, repo,
		QuarantineReason{Rel: "app/dist", Signal: "churn", At: now},
		QuarantineReason{Rel: "app/cache", Signal: "churn", At: now},
	)

	// Removing an absent dir → false, no error, file unchanged.
	changed, err := UnquarantineFile(repo, "not/here")
	if err != nil || changed {
		t.Fatalf("removing absent dir: changed=%v err=%v", changed, err)
	}

	changed, err = UnquarantineFile(repo, "app/dist")
	if err != nil || !changed {
		t.Fatalf("remove present dir: changed=%v err=%v", changed, err)
	}
	got, _ := ReadQuarantineFile(repo)
	if len(got) != 1 || got[0].Rel != "app/cache" {
		t.Fatalf("after removal expected only app/cache; got %+v", got)
	}

	// ToSlash normalisation: backslash form resolves to the same key.
	seedQuarantine(t, repo, QuarantineReason{Rel: "win/dir", At: now})
	changed, err = UnquarantineFile(repo, filepath.FromSlash("win/dir"))
	if err != nil || !changed {
		t.Fatalf("slash-normalised remove failed: changed=%v err=%v", changed, err)
	}
}

func TestSetPinFile(t *testing.T) {
	repo := t.TempDir()
	now := time.Unix(4000, 0)
	seedQuarantine(t, repo, QuarantineReason{Rel: "app/dist", Signal: "churn", At: now})

	// Pin a present dir.
	changed, err := SetPinFile(repo, "app/dist", true)
	if err != nil || !changed {
		t.Fatalf("pin: changed=%v err=%v", changed, err)
	}
	got, _ := ReadQuarantineFile(repo)
	if !got[0].Pinned {
		t.Fatal("pin not persisted")
	}

	// Pinning again → no change.
	changed, _ = SetPinFile(repo, "app/dist", true)
	if changed {
		t.Fatal("re-pin should be a no-op")
	}

	// Unpin.
	changed, err = SetPinFile(repo, "app/dist", false)
	if err != nil || !changed {
		t.Fatalf("unpin: changed=%v err=%v", changed, err)
	}
	got, _ = ReadQuarantineFile(repo)
	if got[0].Pinned {
		t.Fatal("unpin not persisted")
	}

	// Pin on absent dir → no change.
	changed, _ = SetPinFile(repo, "absent", true)
	if changed {
		t.Fatal("pin on absent dir should be a no-op")
	}
}
