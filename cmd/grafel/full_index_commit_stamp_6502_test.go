package main

// #6502 — a FULL index must label the graph it just built with the commit that
// was checked out when the pass STARTED, in both places that label is written:
// the diff manifest's GitCommit/GitCommitFull, and the graph header's
// IndexedSHA.
//
// Before this change neither did. `gitmeta.Capture` ran AFTER `idx.Run`, so
// IndexedSHA was a post-extraction read, and `commitManifest` reached
// `diff.SaveManifest`, which reads live HEAD at save time — later still. A
// commit landing during the pass (minutes, on a real repo) was therefore
// recorded as the commit the graph was built from, for work it never saw. The
// #5710 head-advance detector then sees HEAD == manifest on the next pass,
// finds zero changes and returns Done over a graph built from an earlier
// commit — self-concealing, because nothing re-requests the work.
//
// The sibling incremental sites were fixed in #6474
// (internal/extractors/incremental.go passes its pass-start commit to
// diff.SaveManifestAtCommit); this is the last unfixed one, so until now the
// two halves of one invariant disagreed.
//
// VACUITY TRAP. Against a plain t.TempDir() every commit stamp is
// unconditionally "" and this test would pass against the defective code. So
// it uses a REAL git repo, asserts the mid-pass commit actually differs from
// the pass-start one, and asserts the seam fired exactly once — no assertion
// below is reachable without the intended path having run.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// gitOut runs git in dir and returns trimmed stdout, failing the test on error.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v in %s: %v", args, dir, err)
	}
	return strings.TrimSpace(string(out))
}

func TestFullIndex_StampsPassStartCommit_NotMidPassHEAD(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	root := t.TempDir()
	// Never touch the real ~/.grafel or the running daemon's state.
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "grafelhome"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemonroot"))

	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "svc"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, repo, "go.mod", "module stampfixture\n\ngo 1.21\n")
	writeFixtureFile(t, repo, "svc/widget.go", "package svc\n\ntype Widget struct{ Name string }\n\nfunc (w *Widget) Render() string { return w.Name }\n")
	seedGitRun(t, repo, "init", "-q", "-b", "main")
	seedGitRun(t, repo, "add", "-A")
	seedGitRun(t, repo, "commit", "-q", "-m", "fixture")

	// c1 — the commit the graph is actually built from, in all three
	// abbreviations the two stamps use.
	c1Short, c1Full := diff.HeadCommitPair(repo)
	c1Short12 := gitOut(t, repo, "rev-parse", "--short=12", "HEAD")
	if c1Full == "" || c1Short == "" || c1Short12 == "" {
		t.Fatalf("fixture is not a git repo (short=%q short12=%q full=%q) — every stamp "+
			"below would be \"\" and this test would pass against the defect", c1Short, c1Short12, c1Full)
	}

	// c2 — an empty commit landing mid-pass. Empty so it changes HEAD and
	// nothing else: the walk's file set is identical either way, which is what
	// makes the ONLY observable difference the commit label.
	fired := 0
	var c2Short, c2Full string
	prev := gitCaptureHook
	gitCaptureHook = func() {
		fired++
		seedGitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "mid-pass")
		c2Short, c2Full = diff.HeadCommitPair(repo)
	}
	defer func() { gitCaptureHook = prev }()

	outPath := filepath.Join(root, "state", "graph.json")
	if err := Index(repo, outPath, "stamp-repo", nil, false, false, WithManifestPersist()); err != nil {
		t.Fatalf("Index: %v", err)
	}
	stateDir := filepath.Dir(outPath)

	if fired != 1 {
		t.Fatalf("gitCaptureHook fired %d times, want exactly 1 — the mid-pass commit was "+
			"never made, so nothing below distinguishes the fix from the defect", fired)
	}
	if c2Full == "" || c2Full == c1Full || c2Short == c1Short {
		t.Fatalf("HEAD did not advance mid-pass (c1=%s/%s c2=%s/%s) — the window this test "+
			"exists to close was never opened", c1Short, c1Full, c2Short, c2Full)
	}

	m := diff.LoadManifest(stateDir)
	if m == nil || len(m.Files) == 0 {
		t.Fatalf("full index wrote no manifest in %s — the assertions below would be vacuous", stateDir)
	}
	if m.GitCommit != c1Short {
		t.Errorf("manifest GitCommit = %q, want the pass-start commit %q (mid-pass HEAD was %q).\n"+
			"A manifest labelled with a commit the graph was never built from makes the next "+
			"incremental pass diff against work it never saw (#6502).", m.GitCommit, c1Short, c2Short)
	}
	if m.GitCommitFull != c1Full {
		t.Errorf("manifest GitCommitFull = %q, want the pass-start commit %q (mid-pass HEAD was %q).\n"+
			"SaveManifestAtCommit documents short and full as describing the SAME commit; an empty "+
			"or mid-pass full SHA breaks that pairing (#6502).", m.GitCommitFull, c1Full, c2Full)
	}

	s, err := graph.OpenGraphStream(stateDir)
	if err != nil {
		t.Fatalf("OpenGraphStream(%s): %v", stateDir, err)
	}
	defer s.Close()
	h := s.Header()
	if h.IndexedSHA != c1Short12 {
		t.Errorf("graph header IndexedSHA = %q, want the pass-start commit %q (mid-pass HEAD was %q).\n"+
			"Fixing only the manifest would leave the manifest and the fb header disagreeing about "+
			"the SAME graph, which is worse than both being wrong the same way (#6502).",
			h.IndexedSHA, c1Short12, gitOut(t, repo, "rev-parse", "--short=12", "HEAD"))
	}
}
