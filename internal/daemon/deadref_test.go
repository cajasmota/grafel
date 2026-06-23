package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeRefForgetter records ForgetRef calls and reports which (repo,ref) pairs
// it "holds" as in-memory slots.
type fakeRefForgetter struct {
	held      map[[2]string]bool
	forgotten [][2]string
}

func (f *fakeRefForgetter) ForgetRef(repoPath, ref string) bool {
	key := [2]string{repoPath, ref}
	f.forgotten = append(f.forgotten, key)
	if f.held[key] {
		delete(f.held, key)
		return true
	}
	return false
}

// writeRefStore creates <root>/refs/<refSafe>/graph.fb with `bytes` of content
// and the given mtime, returning the ref dir.
func writeRefStore(t *testing.T, refsRoot, refSafe string, bytes int, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(refsRoot, refSafe)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fb := filepath.Join(dir, "graph.fb")
	if err := os.WriteFile(fb, make([]byte, bytes), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fb, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return dir
}

// newSweeperFixture builds a sweeper over a single still-present repo whose
// refs/ store dir lives under a temp dir. liveRefs/ok drive the injected
// enumerator; primary is the protected default ref.
func newSweeperFixture(t *testing.T, repo, refsRoot string, live map[string]struct{}, ok bool, primary string, ff *fakeRefForgetter, dropped *[][2]string) *DeadRefSweeper {
	t.Helper()
	return NewDeadRefSweeper(DeadRefConfig{
		TrackedRepos:   func() []string { return []string{repo} },
		LiveRefs:       func(string) (map[string]struct{}, bool) { return live, ok },
		PrimaryRef:     func(string) string { return primary },
		RefsDirForRepo: func(string) string { return refsRoot },
		Tier:           ff,
		DropReader: func(rp, ref string) {
			*dropped = append(*dropped, [2]string{rp, ref})
		},
		// Disable the grace window in tests that don't exercise it.
		GraceWindow: -1,
		Now:         func() time.Time { return time.Unix(1_700_000_000, 0) },
	})
}

// repo must exist on disk for the sweeper to inspect it.
func mkLiveRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestDeadRef_reapsRefAbsentFromGit: a stored ref that git no longer lists is
// reaped — its store dir removed, DropReader + ForgetRef called.
func TestDeadRef_reapsRefAbsentFromGit(t *testing.T) {
	repo := mkLiveRepo(t)
	refsRoot := filepath.Join(t.TempDir(), "refs")
	old := time.Unix(1_600_000_000, 0)

	writeRefStore(t, refsRoot, "main", 1000, old)
	deadDir := writeRefStore(t, refsRoot, "feature%2Fgone", 4096, old)

	ff := &fakeRefForgetter{held: map[[2]string]bool{
		{repo, "feature/gone"}: true,
	}}
	var dropped [][2]string
	// git reports only main as live.
	live := map[string]struct{}{"main": {}}
	s := newSweeperFixture(t, repo, refsRoot, live, true, "main", ff, &dropped)

	res := s.Sweep()

	if res.RefsReaped != 1 {
		t.Fatalf("RefsReaped=%d want 1", res.RefsReaped)
	}
	if res.FreedBytes != 4096 {
		t.Errorf("FreedBytes=%d want 4096", res.FreedBytes)
	}
	if _, err := os.Stat(deadDir); !os.IsNotExist(err) {
		t.Errorf("dead ref store dir still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(refsRoot, "main")); err != nil {
		t.Errorf("live ref store dir was removed: %v", err)
	}
	if len(dropped) != 1 || dropped[0] != [2]string{repo, "feature/gone"} {
		t.Errorf("DropReader calls=%v want [{%s feature/gone}]", dropped, repo)
	}
	if res.SlotsForgotten != 1 {
		t.Errorf("SlotsForgotten=%d want 1", res.SlotsForgotten)
	}
}

// TestDeadRef_keepsPrimaryAndRecentlyIndexed: the primary ref is never reaped
// even if git omits it, and a recently-indexed ref (within grace) is kept.
func TestDeadRef_keepsPrimaryAndRecentlyIndexed(t *testing.T) {
	repo := mkLiveRepo(t)
	refsRoot := filepath.Join(t.TempDir(), "refs")
	now := time.Unix(1_700_000_000, 0)

	// Primary ref dir, indexed long ago — must survive purely on the primary guard.
	writeRefStore(t, refsRoot, "main", 1000, now.Add(-100*time.Hour))
	// A dead-in-git ref whose graph.fb is fresh (within 24h grace) — kept.
	recentDir := writeRefStore(t, refsRoot, "wip", 2000, now.Add(-1*time.Hour))

	ff := &fakeRefForgetter{held: map[[2]string]bool{}}
	var dropped [][2]string
	// git lists NOTHING (both refs absent) to prove the guards stand alone.
	live := map[string]struct{}{}
	s := NewDeadRefSweeper(DeadRefConfig{
		TrackedRepos:   func() []string { return []string{repo} },
		LiveRefs:       func(string) (map[string]struct{}, bool) { return live, true },
		PrimaryRef:     func(string) string { return "main" },
		RefsDirForRepo: func(string) string { return refsRoot },
		Tier:           ff,
		DropReader:     func(rp, ref string) { dropped = append(dropped, [2]string{rp, ref}) },
		GraceWindow:    24 * time.Hour,
		Now:            func() time.Time { return now },
	})

	res := s.Sweep()

	if res.RefsReaped != 0 {
		t.Fatalf("RefsReaped=%d want 0 (primary + grace guards)", res.RefsReaped)
	}
	if _, err := os.Stat(filepath.Join(refsRoot, "main")); err != nil {
		t.Errorf("primary ref dir was reaped: %v", err)
	}
	if _, err := os.Stat(recentDir); err != nil {
		t.Errorf("recently-indexed ref dir was reaped: %v", err)
	}
	if len(dropped) != 0 {
		t.Errorf("DropReader called unexpectedly: %v", dropped)
	}
}

// TestDeadRef_failClosedOnGitError: when git enumeration fails (ok=false) the
// repo is skipped entirely and nothing is reaped.
func TestDeadRef_failClosedOnGitError(t *testing.T) {
	repo := mkLiveRepo(t)
	refsRoot := filepath.Join(t.TempDir(), "refs")
	old := time.Unix(1_600_000_000, 0)
	deadDir := writeRefStore(t, refsRoot, "anything", 4096, old)

	ff := &fakeRefForgetter{held: map[[2]string]bool{{repo, "anything"}: true}}
	var dropped [][2]string
	// ok=false → fail-closed.
	s := newSweeperFixture(t, repo, refsRoot, nil, false, "main", ff, &dropped)

	res := s.Sweep()

	if res.RefsReaped != 0 {
		t.Fatalf("RefsReaped=%d want 0 (fail-closed)", res.RefsReaped)
	}
	if res.ReposSkipped != 1 {
		t.Errorf("ReposSkipped=%d want 1", res.ReposSkipped)
	}
	if _, err := os.Stat(deadDir); err != nil {
		t.Errorf("fail-closed but ref dir was removed: %v", err)
	}
	if len(ff.forgotten) != 0 {
		t.Errorf("ForgetRef called under fail-closed: %v", ff.forgotten)
	}
}

// TestDeadRef_reapsRemovedWorktreeRef: a worktree's checked-out ref that is no
// longer in the live set (worktree removed) gets reaped.
func TestDeadRef_reapsRemovedWorktreeRef(t *testing.T) {
	repo := mkLiveRepo(t)
	refsRoot := filepath.Join(t.TempDir(), "refs")
	old := time.Unix(1_600_000_000, 0)

	writeRefStore(t, refsRoot, "main", 1000, old)
	wtDir := writeRefStore(t, refsRoot, "agent-branch", 8192, old)

	ff := &fakeRefForgetter{held: map[[2]string]bool{{repo, "agent-branch"}: true}}
	var dropped [][2]string
	// Worktree removed → its branch no longer enumerated; only main remains.
	live := map[string]struct{}{"main": {}}
	s := newSweeperFixture(t, repo, refsRoot, live, true, "main", ff, &dropped)

	res := s.Sweep()

	if res.RefsReaped != 1 {
		t.Fatalf("RefsReaped=%d want 1", res.RefsReaped)
	}
	if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
		t.Errorf("removed-worktree ref dir still present: %v", err)
	}
	if res.FreedBytes != 8192 {
		t.Errorf("FreedBytes=%d want 8192", res.FreedBytes)
	}
}

// TestDeadRef_skipsUnknownSentinel: the _unknown sentinel dir is never reaped.
func TestDeadRef_skipsUnknownSentinel(t *testing.T) {
	repo := mkLiveRepo(t)
	refsRoot := filepath.Join(t.TempDir(), "refs")
	old := time.Unix(1_600_000_000, 0)
	unknownDir := writeRefStore(t, refsRoot, "_unknown", 512, old)

	ff := &fakeRefForgetter{held: map[[2]string]bool{}}
	var dropped [][2]string
	s := newSweeperFixture(t, repo, refsRoot, map[string]struct{}{}, true, "main", ff, &dropped)

	res := s.Sweep()
	if res.RefsReaped != 0 {
		t.Fatalf("RefsReaped=%d want 0 (sentinel skipped)", res.RefsReaped)
	}
	if _, err := os.Stat(unknownDir); err != nil {
		t.Errorf("_unknown sentinel dir was reaped: %v", err)
	}
}

// TestDeadRef_retentionCapEvictsOldestGraceHeld: when more dead-in-git refs are
// grace-protected than the retention cap allows, the oldest beyond the cap are
// reaped while the N most-recent survive — and primary is never counted/reaped.
func TestDeadRef_retentionCapEvictsOldestGraceHeld(t *testing.T) {
	repo := mkLiveRepo(t)
	refsRoot := filepath.Join(t.TempDir(), "refs")
	now := time.Unix(1_700_000_000, 0)

	// Primary, indexed long ago — survives on the primary guard, never counts.
	writeRefStore(t, refsRoot, "main", 1000, now.Add(-100*time.Hour))

	// 5 dead-in-git transient refs, ALL fresh (within 24h grace), distinct mtimes.
	// merge-1 is oldest … merge-5 is newest.
	for i := 1; i <= 5; i++ {
		writeRefStore(t, refsRoot, "merge-"+itoa(i), 80, now.Add(-time.Duration(6-i)*time.Hour))
	}

	ff := &fakeRefForgetter{held: map[[2]string]bool{
		{repo, "merge-1"}: true, {repo, "merge-2"}: true,
	}}
	var dropped [][2]string
	// git lists only main as live; the merge-* refs are all dead-in-git.
	s := NewDeadRefSweeper(DeadRefConfig{
		TrackedRepos:   func() []string { return []string{repo} },
		LiveRefs:       func(string) (map[string]struct{}, bool) { return map[string]struct{}{"main": {}}, true },
		PrimaryRef:     func(string) string { return "main" },
		RefsDirForRepo: func(string) string { return refsRoot },
		Tier:           ff,
		DropReader:     func(rp, ref string) { dropped = append(dropped, [2]string{rp, ref}) },
		GraceWindow:    24 * time.Hour,
		RetentionCap:   3, // keep 3 newest grace-held; evict the 2 oldest.
		Now:            func() time.Time { return now },
	})

	res := s.Sweep()

	if res.RefsReaped != 2 || res.CapEvicted != 2 {
		t.Fatalf("RefsReaped=%d CapEvicted=%d want 2/2", res.RefsReaped, res.CapEvicted)
	}
	// merge-1 and merge-2 (oldest) gone; merge-3..5 + main kept.
	for _, gone := range []string{"merge-1", "merge-2"} {
		if _, err := os.Stat(filepath.Join(refsRoot, gone)); !os.IsNotExist(err) {
			t.Errorf("%s should have been cap-evicted: %v", gone, err)
		}
	}
	for _, kept := range []string{"merge-3", "merge-4", "merge-5", "main"} {
		if _, err := os.Stat(filepath.Join(refsRoot, kept)); err != nil {
			t.Errorf("%s should have survived: %v", kept, err)
		}
	}
	// Reader/slot drops only for the held evicted refs.
	if res.SlotsForgotten != 2 {
		t.Errorf("SlotsForgotten=%d want 2", res.SlotsForgotten)
	}
	if len(dropped) != 2 {
		t.Errorf("DropReader calls=%v want 2", dropped)
	}
}

// TestDeadRef_retentionCapNeverEvictsPrimary: a fresh primary ref is never
// counted toward or evicted by the retention cap, even when many transient
// dead refs exceed the cap. With a cap of 1, exactly the newest transient ref
// is kept and the primary always survives.
func TestDeadRef_retentionCapNeverEvictsPrimary(t *testing.T) {
	repo := mkLiveRepo(t)
	refsRoot := filepath.Join(t.TempDir(), "refs")
	now := time.Unix(1_700_000_000, 0)

	// Primary, fresh — must survive regardless of the cap and never be counted.
	writeRefStore(t, refsRoot, "main", 1000, now.Add(-1*time.Hour))
	// 3 fresh dead transient refs; merge-3 is newest.
	for i := 1; i <= 3; i++ {
		writeRefStore(t, refsRoot, "merge-"+itoa(i), 80, now.Add(-time.Duration(4-i)*time.Hour))
	}

	s := NewDeadRefSweeper(DeadRefConfig{
		TrackedRepos:   func() []string { return []string{repo} },
		LiveRefs:       func(string) (map[string]struct{}, bool) { return map[string]struct{}{"main": {}}, true },
		PrimaryRef:     func(string) string { return "main" },
		RefsDirForRepo: func(string) string { return refsRoot },
		GraceWindow:    24 * time.Hour,
		RetentionCap:   1, // keep 1 newest transient; primary excluded from count.
		Now:            func() time.Time { return now },
	})

	res := s.Sweep()

	if res.CapEvicted != 2 { // merge-1, merge-2 evicted; merge-3 kept.
		t.Fatalf("CapEvicted=%d want 2", res.CapEvicted)
	}
	if _, err := os.Stat(filepath.Join(refsRoot, "main")); err != nil {
		t.Errorf("primary reaped by cap: %v", err)
	}
	if _, err := os.Stat(filepath.Join(refsRoot, "merge-3")); err != nil {
		t.Errorf("newest transient (within cap) reaped: %v", err)
	}
}

// TestDeadRef_retentionCapDisabled: a negative cap disables the backstop — all
// grace-held dead refs survive regardless of count.
func TestDeadRef_retentionCapDisabled(t *testing.T) {
	repo := mkLiveRepo(t)
	refsRoot := filepath.Join(t.TempDir(), "refs")
	now := time.Unix(1_700_000_000, 0)
	for i := 1; i <= 20; i++ {
		writeRefStore(t, refsRoot, "merge-"+itoa(i), 80, now.Add(-1*time.Hour))
	}
	s := NewDeadRefSweeper(DeadRefConfig{
		TrackedRepos:   func() []string { return []string{repo} },
		LiveRefs:       func(string) (map[string]struct{}, bool) { return map[string]struct{}{}, true },
		PrimaryRef:     func(string) string { return "main" },
		RefsDirForRepo: func(string) string { return refsRoot },
		GraceWindow:    24 * time.Hour,
		RetentionCap:   -1, // disabled
		Now:            func() time.Time { return now },
	})
	res := s.Sweep()
	if res.RefsReaped != 0 || res.CapEvicted != 0 {
		t.Fatalf("cap disabled: RefsReaped=%d CapEvicted=%d want 0/0", res.RefsReaped, res.CapEvicted)
	}
}

// itoa is a tiny strconv.Itoa to avoid an extra import in this table.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// TestDeadRef_reaperDrivesSweep: the Reaper invokes the dead-ref sweep on its
// Sweep() and surfaces the result.
func TestDeadRef_reaperDrivesSweep(t *testing.T) {
	repo := mkLiveRepo(t)
	refsRoot := filepath.Join(t.TempDir(), "refs")
	old := time.Unix(1_600_000_000, 0)
	writeRefStore(t, refsRoot, "main", 1000, old)
	writeRefStore(t, refsRoot, "dead", 2048, old)

	ff := &fakeRefForgetter{held: map[[2]string]bool{{repo, "dead"}: true}}
	var dropped [][2]string
	sweeper := newSweeperFixture(t, repo, refsRoot, map[string]struct{}{"main": {}}, true, "main", ff, &dropped)

	r := NewReaper(ReaperConfig{DeadRefs: sweeper})
	res := r.Sweep()

	if res.DeadRefs.RefsReaped != 1 {
		t.Fatalf("reaper-driven DeadRefs.RefsReaped=%d want 1", res.DeadRefs.RefsReaped)
	}
}
