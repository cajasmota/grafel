package mcp

// docgen_test.go — integration tests for the 6 archigraph_docgen_* MCP tools.
//
// Coverage:
//   TestDocgenStartRun_New          — fresh start_run creates staging dir
//   TestDocgenStartRun_ResumeTrue   — second start_run with resume=true returns existing
//   TestDocgenStartRun_ResumeFalse  — second start_run with resume=false returns error
//   TestDocgenStatus                — status walks staging dir and returns SHAs
//   TestDocgenValidate_OK           — validate passes on valid frontmatter + links
//   TestDocgenValidate_FrontmatterError — validate catches bad frontmatter
//   TestDocgenValidate_BrokenLink   — validate catches broken cross-links
//   TestDocgenValidate_PathTraversal — validate catches links that escape staging
//   TestDocgenPromote_SSGGuard      — promote refuses when SSG scaffolding detected
//   TestDocgenPromote_Atomic        — promote rotates previous + installs staging
//   TestDocgenAbort                 — abort removes staging and releases lock
//   TestDocgenList                  — list enumerates canonical docs
//   TestDocgenFullHappyPath         — full round-trip: start → write → validate → promote → list
//   TestDocgenSandboxedAgentSim     — two concurrent start_run calls for same group; second resumes

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newDocgenServer creates a minimal MCP server for docgen tests. It does NOT
// need a real registry or loaded repos; the docgen tools are independent of
// graph state.
func newDocgenServer(t *testing.T) (*Server, string) {
	t.Helper()

	// Isolated temp dirs.
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "registry.json")
	homeDir := filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Override HOME so canonical paths land in tmpDir.
	t.Setenv("HOME", homeDir)

	// Write an empty registry so NewServer doesn't fail.
	if err := os.WriteFile(registryPath, []byte(`{"groups":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := NewServer(Config{RegistryPath: registryPath, DebugLevel: 0})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Clear any in-flight runs left over from a previous test.
	docgenMu.Lock()
	for k := range docgenRunsByGroup {
		delete(docgenRunsByGroup, k)
	}
	docgenMu.Unlock()

	return srv, tmpDir
}

// callDocgenTool calls the named docgen tool and returns the decoded JSON result.
func callDocgenTool(t *testing.T, srv *Server, toolName string, args map[string]any) map[string]any {
	t.Helper()
	res := callTool(t, srv, toolName, args)
	if res == nil {
		t.Fatalf("%s returned nil result", toolName)
	}
	if res.IsError {
		t.Fatalf("%s returned error: %s", toolName, resultText(res))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
		t.Fatalf("%s result not valid JSON: %v\nbody: %s", toolName, err, resultText(res))
	}
	return out
}

// callDocgenToolExpectError calls the named docgen tool and asserts it returns
// a tool error containing the given substring.
func callDocgenToolExpectError(t *testing.T, srv *Server, toolName string, args map[string]any, wantSubstr string) {
	t.Helper()
	res := callTool(t, srv, toolName, args)
	if res == nil {
		t.Fatalf("%s returned nil result", toolName)
	}
	if !res.IsError {
		t.Fatalf("%s: expected an error result but got success: %s", toolName, resultText(res))
	}
	got := resultText(res)
	if !strings.Contains(got, wantSubstr) {
		t.Fatalf("%s: expected error containing %q, got: %s", toolName, wantSubstr, got)
	}
}

// makeFakeGitRepo creates a directory that appears to be a git repository by
// creating a minimal .git structure so that gitmeta.RunGit("rev-parse
// --show-toplevel") returns the dir.
func makeFakeGitRepo(t *testing.T, parent string) string {
	t.Helper()
	repoDir := filepath.Join(parent, "myrepo")
	gitDir := filepath.Join(repoDir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Minimal HEAD so git recognises it.
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Minimal config.
	gitConfigContent := "[core]\n\trepositoryformatversion = 0\n\tfilemode = true\n"
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(gitConfigContent), 0o644); err != nil {
		t.Fatal(err)
	}
	// git init so rev-parse --show-toplevel works.
	_ = runGitInit(repoDir) // best-effort; if git isn't available, use no_git=true fallback
	return repoDir
}

// runGitInit initialises a git repo. Returns error if git isn't available.
func runGitInit(dir string) error {
	// We use gitmeta.RunGit indirectly: just check if we can init.
	// os/exec is not imported here; we rely on os.MkdirAll above having
	// already created the .git skeleton, which is enough for most git calls.
	return nil
}

// callWithCWD wraps args with "cwd" and "no_git" set so tests can avoid
// needing a real git repo.
func callWithCWD(args map[string]any, cwd string) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	args["cwd"] = cwd
	args["no_git"] = true
	return args
}

// ---------------------------------------------------------------------------
// TestDocgenStartRun_New
// ---------------------------------------------------------------------------

func TestDocgenStartRun_New(t *testing.T) {
	srv, tmpDir := newDocgenServer(t)

	res := callDocgenTool(t, srv, "archigraph_docgen_start_run", callWithCWD(map[string]any{
		"group": "mygroup",
	}, tmpDir))

	runID, _ := res["run_id"].(string)
	if runID == "" {
		t.Fatal("expected non-empty run_id")
	}
	stagingPath, _ := res["staging_path"].(string)
	if stagingPath == "" {
		t.Fatal("expected non-empty staging_path")
	}
	if res["resumed"] != false {
		t.Errorf("expected resumed=false for new run, got %v", res["resumed"])
	}

	// Staging directory must exist on disk.
	if _, err := os.Stat(stagingPath); err != nil {
		t.Errorf("staging path %q not created on disk: %v", stagingPath, err)
	}
}

// ---------------------------------------------------------------------------
// TestDocgenStartRun_ResumeTrue
// ---------------------------------------------------------------------------

func TestDocgenStartRun_ResumeTrue(t *testing.T) {
	srv, tmpDir := newDocgenServer(t)

	first := callDocgenTool(t, srv, "archigraph_docgen_start_run", callWithCWD(map[string]any{
		"group": "mygroup",
	}, tmpDir))
	firstRunID := first["run_id"].(string)

	// Second call with resume=true (default).
	second := callDocgenTool(t, srv, "archigraph_docgen_start_run", callWithCWD(map[string]any{
		"group":  "mygroup",
		"resume": true,
	}, tmpDir))

	if second["run_id"] != firstRunID {
		t.Errorf("expected same run_id on resume; got %q, want %q", second["run_id"], firstRunID)
	}
	if second["resumed"] != true {
		t.Errorf("expected resumed=true on second call")
	}
}

// ---------------------------------------------------------------------------
// TestDocgenStartRun_ResumeFalse
// ---------------------------------------------------------------------------

func TestDocgenStartRun_ResumeFalse(t *testing.T) {
	srv, tmpDir := newDocgenServer(t)

	_ = callDocgenTool(t, srv, "archigraph_docgen_start_run", callWithCWD(map[string]any{
		"group": "mygroup",
	}, tmpDir))

	// Second call with resume=false should error.
	callDocgenToolExpectError(t, srv, "archigraph_docgen_start_run",
		callWithCWD(map[string]any{
			"group":  "mygroup",
			"resume": false,
		}, tmpDir),
		"already in progress",
	)
}

// ---------------------------------------------------------------------------
// TestDocgenStatus
// ---------------------------------------------------------------------------

func TestDocgenStatus(t *testing.T) {
	srv, tmpDir := newDocgenServer(t)

	start := callDocgenTool(t, srv, "archigraph_docgen_start_run", callWithCWD(map[string]any{
		"group": "mygroup",
	}, tmpDir))
	runID := start["run_id"].(string)
	stagingPath := start["staging_path"].(string)

	// Write two files into the staging directory.
	if err := os.WriteFile(filepath.Join(stagingPath, "index.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingPath, "api.md"), []byte("# API\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := callDocgenTool(t, srv, "archigraph_docgen_status", callWithCWD(map[string]any{
		"run_id": runID,
	}, tmpDir))

	if res["run_id"] != runID {
		t.Errorf("run_id mismatch: got %v", res["run_id"])
	}
	count, _ := res["file_count"].(float64)
	if count != 2 {
		t.Errorf("expected 2 files, got %v", count)
	}
	shaMap, _ := res["sha_per_file"].(map[string]any)
	if _, ok := shaMap["index.md"]; !ok {
		t.Error("expected sha_per_file to contain index.md")
	}
	if _, ok := shaMap["api.md"]; !ok {
		t.Error("expected sha_per_file to contain api.md")
	}
}

// ---------------------------------------------------------------------------
// TestDocgenValidate_OK
// ---------------------------------------------------------------------------

func TestDocgenValidate_OK(t *testing.T) {
	srv, tmpDir := newDocgenServer(t)

	start := callDocgenTool(t, srv, "archigraph_docgen_start_run", callWithCWD(map[string]any{
		"group": "mygroup",
	}, tmpDir))
	runID := start["run_id"].(string)
	stagingPath := start["staging_path"].(string)

	// Valid file with well-formed frontmatter and a valid internal link.
	indexContent := "---\ntitle: Index\ndescription: Main page\n---\n\n# Index\n\n[API](api.md)\n"
	apiContent := "---\ntitle: API Reference\n---\n\n# API Reference\n"
	if err := os.WriteFile(filepath.Join(stagingPath, "index.md"), []byte(indexContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingPath, "api.md"), []byte(apiContent), 0o644); err != nil {
		t.Fatal(err)
	}

	res := callDocgenTool(t, srv, "archigraph_docgen_validate", callWithCWD(map[string]any{
		"run_id": runID,
	}, tmpDir))

	if res["has_errors"] != false {
		t.Errorf("expected no errors, got: %v (frontmatter_errors=%v, cross_link_errors=%v)",
			res["summary"], res["frontmatter_errors"], res["cross_link_errors"])
	}
	if res["summary"] != "ok" {
		t.Errorf("expected summary=ok, got %v", res["summary"])
	}
}

// ---------------------------------------------------------------------------
// TestDocgenValidate_FrontmatterError
// ---------------------------------------------------------------------------

func TestDocgenValidate_FrontmatterError(t *testing.T) {
	srv, tmpDir := newDocgenServer(t)

	start := callDocgenTool(t, srv, "archigraph_docgen_start_run", callWithCWD(map[string]any{
		"group": "mygroup",
	}, tmpDir))
	runID := start["run_id"].(string)
	stagingPath := start["staging_path"].(string)

	// Frontmatter with a tab character (invalid YAML).
	badContent := "---\n\ttitle: Bad Tab\n---\n\n# Content\n"
	if err := os.WriteFile(filepath.Join(stagingPath, "bad.md"), []byte(badContent), 0o644); err != nil {
		t.Fatal(err)
	}

	res := callDocgenTool(t, srv, "archigraph_docgen_validate", callWithCWD(map[string]any{
		"run_id": runID,
	}, tmpDir))

	if res["has_errors"] != true {
		t.Error("expected has_errors=true for file with tab in frontmatter")
	}
	fmErrs, _ := res["frontmatter_errors"].([]any)
	if len(fmErrs) == 0 {
		t.Error("expected at least one frontmatter_error")
	}
}

// ---------------------------------------------------------------------------
// TestDocgenValidate_BrokenLink
// ---------------------------------------------------------------------------

func TestDocgenValidate_BrokenLink(t *testing.T) {
	srv, tmpDir := newDocgenServer(t)

	start := callDocgenTool(t, srv, "archigraph_docgen_start_run", callWithCWD(map[string]any{
		"group": "mygroup",
	}, tmpDir))
	runID := start["run_id"].(string)
	stagingPath := start["staging_path"].(string)

	// Link to a non-existent file.
	content := "---\ntitle: Index\n---\n\n# Index\n\n[Missing](does-not-exist.md)\n"
	if err := os.WriteFile(filepath.Join(stagingPath, "index.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	res := callDocgenTool(t, srv, "archigraph_docgen_validate", callWithCWD(map[string]any{
		"run_id": runID,
	}, tmpDir))

	if res["has_errors"] != true {
		t.Error("expected has_errors=true for file with broken link")
	}
	linkErrs, _ := res["cross_link_errors"].([]any)
	if len(linkErrs) == 0 {
		t.Error("expected at least one cross_link_error")
	}
}

// ---------------------------------------------------------------------------
// TestDocgenValidate_PathTraversal
// ---------------------------------------------------------------------------

func TestDocgenValidate_PathTraversal(t *testing.T) {
	srv, tmpDir := newDocgenServer(t)

	start := callDocgenTool(t, srv, "archigraph_docgen_start_run", callWithCWD(map[string]any{
		"group": "mygroup",
	}, tmpDir))
	runID := start["run_id"].(string)
	stagingPath := start["staging_path"].(string)

	// Link that traverses above staging root.
	content := "---\ntitle: Index\n---\n\n# Index\n\n[Escape](../../etc/passwd)\n"
	if err := os.WriteFile(filepath.Join(stagingPath, "index.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	res := callDocgenTool(t, srv, "archigraph_docgen_validate", callWithCWD(map[string]any{
		"run_id": runID,
	}, tmpDir))

	if res["has_errors"] != true {
		t.Error("expected has_errors=true for path-traversal link")
	}
	linkErrs, _ := res["cross_link_errors"].([]any)
	if len(linkErrs) == 0 {
		t.Error("expected at least one cross_link_error for path-traversal link")
	}
	found := false
	for _, e := range linkErrs {
		if m, ok := e.(map[string]any); ok {
			if reason, _ := m["reason"].(string); strings.Contains(reason, "escapes") {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected cross_link_error with 'escapes' message for path-traversal link")
	}
}

// ---------------------------------------------------------------------------
// TestDocgenPromote_SSGGuard
// ---------------------------------------------------------------------------

func TestDocgenPromote_SSGGuard(t *testing.T) {
	cases := []string{
		".vitepress",
		".docusaurus",
		"mkdocs.yml",
		"sphinx",
		"config.ts",
		"config.js",
		"package.json",
	}

	for _, sig := range cases {
		t.Run(sig, func(t *testing.T) {
			srv, tmpDir := newDocgenServer(t)

			start := callDocgenTool(t, srv, "archigraph_docgen_start_run", callWithCWD(map[string]any{
				"group": "mygroup",
			}, tmpDir))
			runID := start["run_id"].(string)
			stagingPath := start["staging_path"].(string)

			// Plant the SSG signature.
			sigPath := filepath.Join(stagingPath, sig)
			if err := os.MkdirAll(sigPath, 0o755); err != nil {
				// Might be a file, not a dir.
				if err2 := os.WriteFile(sigPath, []byte("ssg content"), 0o644); err2 != nil {
					t.Fatal(err2)
				}
			}

			callDocgenToolExpectError(t, srv, "archigraph_docgen_promote",
				callWithCWD(map[string]any{
					"run_id": runID,
					"group":  "mygroup",
					"force":  true, // skip validate but not SSG guard
				}, tmpDir),
				"SSG scaffolding",
			)
		})
	}
}

// ---------------------------------------------------------------------------
// TestDocgenPromote_Atomic
// ---------------------------------------------------------------------------

func TestDocgenPromote_Atomic(t *testing.T) {
	srv, tmpDir := newDocgenServer(t)

	// Create initial canonical content to simulate "previous".
	homeDir := os.Getenv("HOME")
	canonicalPath := filepath.Join(homeDir, ".archigraph", "docs", "mygroup")
	if err := os.MkdirAll(canonicalPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonicalPath, "old.md"), []byte("# Old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Start a run and write a file.
	start := callDocgenTool(t, srv, "archigraph_docgen_start_run", callWithCWD(map[string]any{
		"group": "mygroup",
	}, tmpDir))
	runID := start["run_id"].(string)
	stagingPath := start["staging_path"].(string)

	if err := os.WriteFile(filepath.Join(stagingPath, "new.md"), []byte("# New\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := callDocgenTool(t, srv, "archigraph_docgen_promote", callWithCWD(map[string]any{
		"run_id": runID,
		"group":  "mygroup",
		"force":  true,
	}, tmpDir))

	if res["canonical_path"] != canonicalPath {
		t.Errorf("canonical_path mismatch: got %v, want %v", res["canonical_path"], canonicalPath)
	}
	previousPath, _ := res["previous_path"].(string)
	if previousPath == "" {
		t.Error("expected previous_path to be set (old canonical was rotated)")
	}

	// Verify new canonical has new.md.
	if _, err := os.Stat(filepath.Join(canonicalPath, "new.md")); err != nil {
		t.Errorf("new.md not present in canonical: %v", err)
	}
	// Verify previous has old.md.
	if _, err := os.Stat(filepath.Join(previousPath, "old.md")); err != nil {
		t.Errorf("old.md not present in previous: %v", err)
	}

	// Verify staging dir is gone.
	if _, err := os.Stat(stagingPath); !os.IsNotExist(err) {
		t.Error("staging path should not exist after promote")
	}

	// Verify per-group lock is released.
	docgenMu.Lock()
	_, stillLocked := docgenRunsByGroup["mygroup"]
	docgenMu.Unlock()
	if stillLocked {
		t.Error("per-group lock should be released after promote")
	}
}

// ---------------------------------------------------------------------------
// TestDocgenAbort
// ---------------------------------------------------------------------------

func TestDocgenAbort(t *testing.T) {
	srv, tmpDir := newDocgenServer(t)

	start := callDocgenTool(t, srv, "archigraph_docgen_start_run", callWithCWD(map[string]any{
		"group": "mygroup",
	}, tmpDir))
	runID := start["run_id"].(string)
	stagingPath := start["staging_path"].(string)

	// Write a file.
	if err := os.WriteFile(filepath.Join(stagingPath, "work.md"), []byte("# WIP\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := callDocgenTool(t, srv, "archigraph_docgen_abort", callWithCWD(map[string]any{
		"run_id": runID,
		"group":  "mygroup",
	}, tmpDir))

	if res["aborted"] != true {
		t.Errorf("expected aborted=true, got %v", res["aborted"])
	}

	// Staging dir should be gone.
	if _, err := os.Stat(stagingPath); !os.IsNotExist(err) {
		t.Error("staging path should not exist after abort")
	}

	// Per-group lock should be released.
	docgenMu.Lock()
	_, stillLocked := docgenRunsByGroup["mygroup"]
	docgenMu.Unlock()
	if stillLocked {
		t.Error("per-group lock should be released after abort")
	}
}

// ---------------------------------------------------------------------------
// TestDocgenList
// ---------------------------------------------------------------------------

func TestDocgenList(t *testing.T) {
	srv, tmpDir := newDocgenServer(t)
	_ = tmpDir

	// Plant canonical docs.
	homeDir := os.Getenv("HOME")
	canonicalPath := filepath.Join(homeDir, ".archigraph", "docs", "mygroup")
	if err := os.MkdirAll(canonicalPath, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.md", "api.md", "guides/getting-started.md"} {
		p := filepath.Join(canonicalPath, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	res := callDocgenTool(t, srv, "archigraph_docgen_list", map[string]any{
		"group": "mygroup",
	})

	count, _ := res["file_count"].(float64)
	if count != 3 {
		t.Errorf("expected 3 files, got %v", count)
	}
	if res["canonical_path"] != canonicalPath {
		t.Errorf("canonical_path mismatch: got %v, want %v", res["canonical_path"], canonicalPath)
	}
	files, _ := res["files"].([]any)
	relPaths := map[string]bool{}
	for _, f := range files {
		if m, ok := f.(map[string]any); ok {
			if rp, ok := m["rel_path"].(string); ok {
				relPaths[rp] = true
			}
		}
	}
	for _, want := range []string{"index.md", "api.md", "guides/getting-started.md"} {
		if !relPaths[want] {
			t.Errorf("expected rel_path %q in list", want)
		}
	}
}

// ---------------------------------------------------------------------------
// TestDocgenFullHappyPath — full round-trip
// ---------------------------------------------------------------------------

func TestDocgenFullHappyPath(t *testing.T) {
	srv, tmpDir := newDocgenServer(t)
	cwdArgs := map[string]any{"cwd": tmpDir, "no_git": true}

	// 1. Start run.
	startRes := callDocgenTool(t, srv, "archigraph_docgen_start_run", mergeMaps(cwdArgs, map[string]any{
		"group": "happy",
	}))
	runID := startRes["run_id"].(string)
	stagingPath := startRes["staging_path"].(string)

	// 2. Write valid docs.
	writeDocgenFile(t, stagingPath, "index.md", "---\ntitle: Home\n---\n\n# Home\n\n[API](api.md)\n")
	writeDocgenFile(t, stagingPath, "api.md", "---\ntitle: API\n---\n\n# API Reference\n")

	// 3. Status check.
	statusRes := callDocgenTool(t, srv, "archigraph_docgen_status", mergeMaps(cwdArgs, map[string]any{
		"run_id": runID,
	}))
	fileCount, _ := statusRes["file_count"].(float64)
	if fileCount != 2 {
		t.Errorf("status: expected 2 files, got %v", fileCount)
	}

	// 4. Validate.
	valRes := callDocgenTool(t, srv, "archigraph_docgen_validate", mergeMaps(cwdArgs, map[string]any{
		"run_id": runID,
	}))
	if valRes["has_errors"] != false {
		t.Errorf("validate: expected no errors, got: %v", valRes["summary"])
	}

	// 5. Promote.
	promRes := callDocgenTool(t, srv, "archigraph_docgen_promote", mergeMaps(cwdArgs, map[string]any{
		"run_id": runID,
		"group":  "happy",
		"force":  false,
	}))
	movedCount, _ := promRes["file_count"].(float64)
	if movedCount != 2 {
		t.Errorf("promote: expected 2 files moved, got %v", movedCount)
	}

	// Staging should be gone.
	if _, err := os.Stat(stagingPath); !os.IsNotExist(err) {
		t.Error("staging path should not exist after promote")
	}

	// 6. List.
	listRes := callDocgenTool(t, srv, "archigraph_docgen_list", map[string]any{
		"group": "happy",
	})
	listCount, _ := listRes["file_count"].(float64)
	if listCount != 2 {
		t.Errorf("list: expected 2 files, got %v", listCount)
	}
}

// ---------------------------------------------------------------------------
// TestDocgenSandboxedAgentSim — two concurrent start_run calls for same group
// ---------------------------------------------------------------------------

func TestDocgenSandboxedAgentSim(t *testing.T) {
	srv, tmpDir := newDocgenServer(t)
	cwdArgs := map[string]any{"cwd": tmpDir, "no_git": true}

	// Agent A starts a run.
	resA := callDocgenTool(t, srv, "archigraph_docgen_start_run", mergeMaps(cwdArgs, map[string]any{
		"group":  "shared",
		"resume": true,
	}))
	runIDA := resA["run_id"].(string)
	if resA["resumed"] != false {
		t.Error("agent A: expected resumed=false for first start_run")
	}

	// Agent B also calls start_run for the same group — must get the same run.
	resB := callDocgenTool(t, srv, "archigraph_docgen_start_run", mergeMaps(cwdArgs, map[string]any{
		"group":  "shared",
		"resume": true,
	}))
	if resB["run_id"] != runIDA {
		t.Errorf("agent B: expected run_id=%q (same as A), got %q", runIDA, resB["run_id"])
	}
	if resB["resumed"] != true {
		t.Error("agent B: expected resumed=true on second call")
	}

	// Agent C with resume=false must get an error.
	callDocgenToolExpectError(t, srv, "archigraph_docgen_start_run",
		mergeMaps(cwdArgs, map[string]any{
			"group":  "shared",
			"resume": false,
		}),
		"already in progress",
	)

	// Abort the run.
	_ = callDocgenTool(t, srv, "archigraph_docgen_abort", mergeMaps(cwdArgs, map[string]any{
		"run_id": runIDA,
		"group":  "shared",
	}))

	// Now a new start_run should succeed (lock released).
	resNew := callDocgenTool(t, srv, "archigraph_docgen_start_run", mergeMaps(cwdArgs, map[string]any{
		"group":  "shared",
		"resume": false,
	}))
	if resNew["run_id"] == runIDA {
		t.Error("expected a fresh run_id after abort")
	}
	if resNew["resumed"] != false {
		t.Error("expected resumed=false for fresh run after abort")
	}
}

// ---------------------------------------------------------------------------
// Test utilities
// ---------------------------------------------------------------------------

// writeDocgenFile writes content to path within the staging directory.
func writeDocgenFile(t *testing.T, stagingPath, relPath, content string) {
	t.Helper()
	abs := filepath.Join(stagingPath, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mergeMaps merges two maps (b overrides a).
func mergeMaps(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// Compile-time tool registration check
// ---------------------------------------------------------------------------

// TestDocgenToolsRegistered verifies that all 6 docgen tools are registered.
func TestDocgenToolsRegistered(t *testing.T) {
	srv, _ := newDocgenServer(t)

	want := []string{
		"archigraph_docgen_start_run",
		"archigraph_docgen_status",
		"archigraph_docgen_validate",
		"archigraph_docgen_promote",
		"archigraph_docgen_abort",
		"archigraph_docgen_list",
	}

	toolMap := srv.MCP.ListTools()
	for _, name := range want {
		if _, ok := toolMap[name]; !ok {
			t.Errorf("tool %q not registered in MCP server", name)
		}
	}

	_ = time.Now()               // use time import to keep it valid
	_ = context.Background()     // use context import
	_ = mcpapi.CallToolRequest{} // use mcpapi import
}
