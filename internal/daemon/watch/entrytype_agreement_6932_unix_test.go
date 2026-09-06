//go:build unix

package watch

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/walk"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// TestExistsOnDisk_AgreesWithWalker is the #6932 MA-1 pin.
//
// The poller admits a git-reported path to its candidate set through
// existsOnDisk; the indexer decides what to index through walk.WalkRepo. Two
// predicates answering "would grafel index this?" is one too many, and a
// divergence is a defect in EITHER direction:
//
//   - poller says yes, walker says no  -> a path that can never be stamped,
//     dirty on every cycle forever (blocker 1's shape).
//   - poller says no, walker says yes  -> a file the index CONTAINS whose
//     changes the poller can never see (MA-1's shape: existsOnDisk used
//     os.Lstat, which answers about the LINK, while irregularSkipRule resolves
//     a symlink with os.Stat and indexes a link to a regular file).
//
// So the assertion is AGREEMENT, over an ENUMERATED entry-type space rather
// than a hand-picked list, and the walker's verdict is DERIVED from a real
// WalkRepo run rather than restated — the test cannot encode a belief about
// what the walker does.
func TestExistsOnDisk_AgreesWithWalker(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "realdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(repo, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, rel string) {
		if err := os.Symlink(target, filepath.Join(repo, rel)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}

	// The entry-type space, enumerated. Every distinct shape the type bits can
	// take for a non-directory entry, plus a directory and both symlink
	// resolution failures.
	write("regular.go", "package p\n")
	write("realdir/target.go", "package p\n")
	link(filepath.Join(repo, "regular.go"), "link-to-regular.go")
	link(filepath.Join(repo, "link-to-regular.go"), "link-to-link-to-regular.go")
	link(filepath.Join(repo, "no-such-file.go"), "dangling.go")
	link(filepath.Join(repo, "realdir"), "link-to-dir.go")
	link(filepath.Join(repo, "loop-b.go"), "loop-a.go")
	link(filepath.Join(repo, "loop-a.go"), "loop-b.go")
	testsupport.MkfifoInTemp(t, root, "repo", "fifo.go")
	link(filepath.Join(repo, "fifo.go"), "link-to-fifo.go")

	candidates := []string{
		"regular.go",
		"realdir",
		"realdir/target.go",
		"link-to-regular.go",
		"link-to-link-to-regular.go",
		"dangling.go",
		"link-to-dir.go",
		"loop-a.go",
		"loop-b.go",
		"fifo.go",
		"link-to-fifo.go",
		"absent.go", // never created: both halves must say no
	}

	// The walker's verdict, derived from a real walk. testsupport.MustReturn
	// bounds it: under a regression that opens the FIFO this would park in
	// open(2) forever and wedge the binary with no attribution.
	var walked []string
	var skips []walk.SkipEntry
	testsupport.MustReturn(t, "WalkRepo over the entry-type fixture", func() {
		var err error
		walked, skips, err = walk.WalkRepo(repo, nil)
		if err != nil {
			t.Errorf("WalkRepo: %v", err)
		}
	})
	indexed := make(map[string]bool, len(walked))
	for _, f := range walked {
		indexed[f] = true
	}

	// Premises: the fixture must actually EXERCISE the space, or agreement is
	// vacuous. At least one entry on each side of the walker's verdict, and the
	// symlink-to-regular case must be one the walker INDEXES — that is the
	// asymmetry MA-1 lived in.
	if !indexed["link-to-regular.go"] {
		t.Fatalf("fixture premise broken: the walker does not index a symlink to a regular file; walked=%v skips=%v", walked, skips)
	}
	if indexed["fifo.go"] {
		t.Fatal("fixture premise broken: the walker indexed a FIFO")
	}

	var disagreements []string
	for _, rel := range candidates {
		wantIndexed := indexed[rel]
		gotAdmitted := existsOnDisk(repo, rel)
		if wantIndexed != gotAdmitted {
			disagreements = append(disagreements,
				rel+": walker indexes="+boolStr(wantIndexed)+" poller admits="+boolStr(gotAdmitted))
		}
	}
	if len(disagreements) > 0 {
		sort.Strings(disagreements)
		t.Fatalf("existsOnDisk and walk.WalkRepo disagree on %d entr(ies):\n  %s\nwalked=%v",
			len(disagreements), strings.Join(disagreements, "\n  "), walked)
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// The consequence, driven end to end rather than argued: a NEW symlink to a
// source file inside a tracked directory is reported by git as untracked, is
// indexed by a full walk, and therefore must be detected by the poller — and
// must then converge once the index pass has stamped it.
//
// The reviewer proved the two predicates disagreed on the same path but did not
// drive the cycle through to the missed reindex. This is that step.
func TestChangePoller_NewSymlinkToSourceIsDetected(t *testing.T) {
	repo, state := cpNewRepo(t)
	cpIndexPass(t, repo, state)
	p, _ := cpNewTestPoller(t, repo, state)

	if err := os.Symlink(filepath.Join(repo, "alpha.go"), filepath.Join(repo, "linked.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	// Premise 1: git reports it, so it reaches the git half of discovery.
	out := strings.TrimSpace(cpGitRun(t, repo, "status", "--porcelain", "-unormal"))
	if !strings.Contains(out, "linked.go") {
		t.Fatalf("fixture premise broken: git does not report the symlink: %q", out)
	}
	// Premise 2: a full walk INDEXES it, so refusing it is a missed change and
	// not a conservative skip.
	files, _, err := walk.WalkRepo(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cpContains(files, "linked.go") {
		t.Fatalf("fixture premise broken: the walker does not index the symlink; walked=%v", files)
	}

	if got := cpCycle(t, p, repo); !cpContains(got, "linked.go") {
		t.Fatalf("a new symlink to a source file was NOT detected — the index contains a file the poller cannot see change: %v", got)
	}
	cpIndexPass(t, repo, state)
	if n := cpConverges(t, p, repo, 4); n != 0 {
		t.Fatalf("the symlink stayed dirty for %d cycles after being indexed", n)
	}
}
