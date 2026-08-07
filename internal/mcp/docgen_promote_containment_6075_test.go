package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docgen_promote_containment_6075_test.go — #6075.
//
// handleDocgenPromote's rotate branch os.Rename()s canonicalDocsPath(group),
// which resolved to $HOME/.grafel/docs/<group> — the user's live docs. The
// only thing keeping the suite from renaming a developer's real docs directory
// was that TestWorkflowDocgenDispatch's hardcoded run ID never matched a
// generated one, so resolveStagingPath's os.Stat failed and promote returned
// before reaching rotate. The suite's safety was a side effect of a broken
// fixture: anyone improving that fixture to get real coverage would have been
// punished with no failing assertion and damage outside any temp dir.
//
// These tests are the coverage that was previously unsafe. They only exist
// because the docs root is injectable now.

// TestCanonicalDocsPath_HonoursInjectedRoot pins the seam itself.
//
// The assertion is two-sided on purpose: it is not enough that the injected
// root is used, it must also be impossible for the resolution to land under
// the process's real home. A seam that is consulted but falls back to $HOME on
// any edge would satisfy the first half alone.
func TestCanonicalDocsPath_HonoursInjectedRoot(t *testing.T) {
	realHome := t.TempDir() // stands in for the developer's ~ during this test
	t.Setenv("HOME", realHome)
	t.Setenv("USERPROFILE", realHome)
	t.Setenv("GRAFEL_HOME", filepath.Join(realHome, ".grafel"))

	root := t.TempDir()
	setDocsRootForTest(t, root)

	got, err := canonicalDocsPath("g")
	if err != nil {
		t.Fatalf("canonicalDocsPath: %v", err)
	}
	want := filepath.Join(root, "g")
	if got != want {
		t.Fatalf("canonicalDocsPath(\"g\") = %q, want the injected root %q", got, want)
	}
	if strings.HasPrefix(got, realHome) {
		t.Fatalf("canonicalDocsPath resolved under the process home %q; with a docs root injected "+
			"it must not be able to reach $HOME at all (#6075)", realHome)
	}
}

// TestCanonicalDocsPath_HonoursGrafelHome pins the other half of the
// derivation: with NO override installed, the docs root must come from the
// shared GRAFEL_HOME-aware resolver, like every other docs writer
// (internal/daemon/docs_path.go, internal/docgen/tier*.go, internal/cli/
// docgen.go). canonicalDocsPath read os.Getenv("HOME") directly and never
// looked at GRAFEL_HOME, so an isolated run promoted docs to a different
// directory than the one every reader looked in — the live split-brain
// recorded against this function in #6178's knownDeferred ledger and deferred
// there to this issue.
func TestCanonicalDocsPath_HonoursGrafelHome(t *testing.T) {
	osHome := t.TempDir()
	grafelHome := t.TempDir() // deliberately NOT under osHome
	t.Setenv("HOME", osHome)
	t.Setenv("USERPROFILE", osHome)
	t.Setenv("GRAFEL_HOME", grafelHome)

	got, err := canonicalDocsPath("g")
	if err != nil {
		t.Fatalf("canonicalDocsPath: %v", err)
	}
	want := filepath.Join(grafelHome, "docs", "g")
	if got != want {
		t.Fatalf("canonicalDocsPath(\"g\") = %q, want %q — the docs root must come from "+
			"registry.HomeDir() so promote writes where every other docs path reads (#6178/#6075)", got, want)
	}
}

// TestCanonicalDocsPath_RefusesTraversingGroup covers the input side.
//
// In handleDocgenPromote's disk-lookup branch the group is taken straight from
// the MCP tool arguments (argString(req, "group", "")), and it is joined into
// the docs root with filepath.Join, which collapses "..". Without a
// containment assertion an agent-supplied group escapes the docs root and the
// rotate branch renames whatever is there.
func TestCanonicalDocsPath_RefusesTraversingGroup(t *testing.T) {
	// Isolate the home resolvers even though the docs root is injected below:
	// before the fix this test demonstrably resolved "../../escape" onto the
	// developer's REAL home, because the injected root was ignored. A test
	// that only becomes safe once the code is fixed is not a safe test.
	sandboxGrafelHome(t)

	root := filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir docs root: %v", err)
	}
	setDocsRootForTest(t, root)

	for _, group := range []string{"../escape", "../../escape", "..", ""} {
		got, err := canonicalDocsPath(group)
		if err == nil {
			t.Errorf("canonicalDocsPath(%q) = %q, nil; want a containment refusal — the joined "+
				"path is outside (or equal to) the docs root %q (#6075)", group, got, root)
		}
		if got != "" {
			t.Errorf("canonicalDocsPath(%q) returned path %q with its error; a refusing derivation "+
				"must return no path, or a caller ignoring err still renames it", group, got)
		}
	}
}

// TestDocgenPromote_RotatesInsideInjectedDocsRoot is the coverage #6075 asks
// for: promote actually reaching the rotate branch, which the suite could not
// safely do before.
//
// Everything it touches is inside two temp dirs. The assertions check the
// rotation really happened (old content preserved under .previous-*, new
// content in place, staging consumed) rather than just that the call returned
// without error — a promote that no-ops returns success too.
func TestDocgenPromote_RotatesInsideInjectedDocsRoot(t *testing.T) {
	// A stand-in home that nothing in this test may write to. Asserted at the
	// end, so a regression that reverts to $HOME resolution is caught here and
	// not on a developer's machine.
	realHome := t.TempDir()
	t.Setenv("HOME", realHome)
	t.Setenv("USERPROFILE", realHome)
	t.Setenv("GRAFEL_HOME", filepath.Join(realHome, ".grafel"))

	docsRootDir := t.TempDir()
	setDocsRootForTest(t, docsRootDir)

	const group = "g"
	const runID = "promote-rotate-fixture"

	// Pre-existing canonical docs — this is what rotate must preserve.
	canonical := filepath.Join(docsRootDir, group)
	if err := os.MkdirAll(canonical, 0o755); err != nil {
		t.Fatalf("mkdir canonical: %v", err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "old.md"), []byte("previous generation"), 0o644); err != nil {
		t.Fatalf("write old doc: %v", err)
	}

	// Staging directory, reachable through the disk-lookup branch of
	// resolveStagingPath: <project_root>/.grafel/staging/<run_id>.
	project := t.TempDir()
	staging := filepath.Join(project, ".grafel", "staging", runID)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatalf("mkdir staging: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staging, "new.md"), []byte("fresh generation"), 0o644); err != nil {
		t.Fatalf("write staged doc: %v", err)
	}

	srv := coreTestServer(t)
	out := callBare(t, srv.handleDocgenPromote, map[string]any{
		"run_id": runID,
		"group":  group,
		"cwd":    project,
		"no_git": true,
		"force":  true, // skip frontmatter/cross-link validation; not what this test is about
	})

	var res struct {
		CanonicalPath string   `json:"canonical_path"`
		PreviousPath  string   `json:"previous_path"`
		FilesMoved    []string `json:"files_moved"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("promote did not return a JSON result (%v): %s", err, out)
	}

	// The rotate branch must have run — this is the branch #6075 is about, and
	// an empty previous_path would mean the test never reached it.
	if res.PreviousPath == "" {
		t.Fatalf("promote returned no previous_path, so the rotate branch never ran and this "+
			"test asserts nothing about it: %s", out)
	}
	if !strings.HasPrefix(res.PreviousPath, canonical+".previous-") {
		t.Errorf("previous_path = %q, want a %q.previous-<ts> sibling", res.PreviousPath, canonical)
	}
	if got, err := os.ReadFile(filepath.Join(res.PreviousPath, "old.md")); err != nil || string(got) != "previous generation" {
		t.Errorf("rotated-away docs not preserved at %s: content %q, err %v", res.PreviousPath, got, err)
	}

	// New content is in place under the injected root.
	if res.CanonicalPath != canonical {
		t.Errorf("canonical_path = %q, want %q", res.CanonicalPath, canonical)
	}
	if got, err := os.ReadFile(filepath.Join(canonical, "new.md")); err != nil || string(got) != "fresh generation" {
		t.Errorf("staged docs not promoted into %s: content %q, err %v", canonical, got, err)
	}
	if _, err := os.Stat(filepath.Join(canonical, "old.md")); !os.IsNotExist(err) {
		t.Errorf("old.md still present in canonical after promote (stat err %v); the directory was "+
			"not replaced, it was merged", err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Errorf("staging dir %s survived promote (stat err %v)", staging, err)
	}
	if len(res.FilesMoved) != 1 || res.FilesMoved[0] != "new.md" {
		t.Errorf("files_moved = %v, want [new.md]", res.FilesMoved)
	}

	// Nothing may have been created under the stand-in home.
	if entries, err := os.ReadDir(realHome); err == nil && len(entries) > 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("promote wrote into the home directory (%v) despite an injected docs root; "+
			"this is exactly the damage #6075 describes", names)
	}
}

// TestResolveStagingPath_RefusesTraversingRunID covers the sibling traversal
// found while sweeping the delete side for #6194.
//
// resolveStagingPath's disk-lookup fallback joins the raw, unvalidated run_id
// MCP argument into <project_root>/.grafel/staging/. filepath.Join collapses
// "..", and the only other gate is an os.Stat that any existing directory
// satisfies. That path is then os.Rename()d by promote (into the docs root)
// and os.RemoveAll()d by abort — so an agent-supplied run_id reached two
// destructive operations on an arbitrary directory.
func TestResolveStagingPath_RefusesTraversingRunID(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".grafel", "staging"), 0o755); err != nil {
		t.Fatalf("mkdir staging root: %v", err)
	}
	// A real directory outside the staging root for the traversal to land on,
	// so the os.Stat gate would have been satisfied.
	victim := filepath.Join(project, "victim")
	if err := os.MkdirAll(victim, 0o755); err != nil {
		t.Fatalf("mkdir victim: %v", err)
	}
	if err := os.WriteFile(filepath.Join(victim, "precious.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("write victim file: %v", err)
	}

	// Premise: the traversal really does resolve onto the victim, so a refusal
	// below is refusing something that was genuinely reachable.
	const evil = "../../victim"
	if got := stagingDirPath(project, evil); filepath.Clean(got) != filepath.Clean(victim) {
		t.Fatalf("test premise broken: stagingDirPath(%q) = %q, want the victim %q", evil, got, victim)
	}

	srv := coreTestServer(t)
	out := callBare(t, srv.handleDocgenAbort, map[string]any{
		"run_id": evil,
		"group":  "g",
		"cwd":    project,
		"no_git": true,
	})

	if _, err := os.Stat(filepath.Join(victim, "precious.txt")); err != nil {
		t.Fatalf("abort deleted outside the staging root: %s is gone (%v); result was %s",
			filepath.Join(victim, "precious.txt"), err, out)
	}
	if !strings.Contains(out, "run_id") {
		t.Errorf("expected a run_id-related refusal, got %s", out)
	}
}
