package install_test

// update_blast_radius_6162_test.go pins the blast radius of `grafel update`.
//
// #6162: `grafel update` re-ran the FULL seven-step install transaction with
// WorkingDir defaulting to os.Getwd(), so two of those steps mutated whatever
// git repository the user's shell happened to be sitting in:
//
//	step 5 — appended /.grafel/ to that repo's (tracked) .gitignore
//	step 7 — installed four git hooks (pre-push, post-checkout, post-merge,
//	         post-rewrite) into that repo's .git/hooks
//
// Neither is mentioned in the command's description, and `git status` does not
// show the hooks at all. An upgrade replaces a binary and whatever must stay
// consistent with it; it is not a re-install, and it must not touch a repo the
// user never named.
//
// These tests assert on observable FILESYSTEM state — the bytes of .gitignore
// and the presence of hook files — not on internal flags.

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install"
)

// grafelHookNames are the four hooks step 7 installs.
var grafelHookNames = []string{"pre-push", "post-checkout", "post-merge", "post-rewrite"}

// newBinaryDownloader returns a DownloadBinary stub that writes distinct
// content, so RunUpdate never fast-paths to Skipped.
func newBinaryDownloader(content string) func(*http.Client, string, string, string, string) error {
	return func(_ *http.Client, _, _, _, destPath string) error {
		return os.WriteFile(destPath, []byte(content), 0o755)
	}
}

func hookPresent(t *testing.T, repo, name string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(repo, ".git", "hooks", name))
	return err == nil
}

// newGitRepoFixture creates a REAL git repository (own .git DIRECTORY, own
// .git/hooks) under t.TempDir() and returns its path. `git init` is required,
// not faked: a hand-rolled .git without hooks/ would make every hook assertion
// in this file vacuous, which is the exact failure mode these tests exist to
// avoid.
func newGitRepoFixture(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init unavailable (%v: %s); this test needs a real repo", err, out)
	}
	return dir
}

// standInsideRepo makes repo the PROCESS working directory, which is the only
// channel the defect actually travels down.
//
// Setting UpdateOptions.WorkingDir instead would prove nothing: RunUpdate no
// longer reads that field, so an assertion built on it holds no matter what
// the production code does. And an empty WorkingDir does NOT mean "no repo" —
// DetectGitRepo shells out to `git -C "" rev-parse --show-toplevel`, whose
// empty -C is a no-op, and InstallGitHooks defaults RepoPath to os.Getwd()
// (hooks_install.go:132). Both land on the process cwd. So the test has to
// STAND in the repo, exactly as the user does.
//
// It also verifies the fixture can express the hooks half of the bug at all:
// in a git WORKTREE, .git is a ~72-byte file rather than a directory, so
// InstallGitHooks fails its `stat .git/hooks` regardless of any gate and the
// hook assertions would pass vacuously. Fixtures must be real clones.
func standInsideRepo(t *testing.T, repo string) {
	t.Helper()

	info, err := os.Stat(filepath.Join(repo, ".git"))
	if err != nil {
		t.Fatalf("fixture %s is not a git repo: %v", repo, err)
	}
	if !info.IsDir() {
		t.Fatalf("fixture %s has a FILE .git (worktree/submodule); InstallGitHooks "+
			"cannot write there under any gate, so the hook assertions would be "+
			"vacuous. Use a real clone.", repo)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "hooks")); err != nil {
		t.Fatalf("fixture %s has no .git/hooks; the hook assertions would be "+
			"vacuous: %v", repo, err)
	}

	t.Chdir(repo)
}

// TestRunUpdate_LeavesUnregisteredRepoGitignoreByteIdentical is the primary
// #6162 regression: an update run from inside an arbitrary git checkout must
// not rewrite that checkout's .gitignore.
func TestRunUpdate_LeavesUnregisteredRepoGitignoreByteIdentical(t *testing.T) {
	env := newTestEnv(t)
	standInsideRepo(t, env.gitRepo)

	gitignorePath := filepath.Join(env.gitRepo, ".gitignore")
	before := "node_modules/\n*.log\n"
	if err := os.WriteFile(gitignorePath, []byte(before), 0o644); err != nil {
		t.Fatalf("seed .gitignore: %v", err)
	}

	opts := install.UpdateOptions{
		BinPath:           env.fakeBin,
		StatePath:         env.statePath,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		SkipDaemonRestart: true,
		Tag:               "v0.0.1-blastradius",
		DownloadBinary:    newBinaryDownloader("#!/bin/sh\necho newer-grafel"),
	}
	if _, err := install.RunUpdate(opts); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}

	after, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore after update: %v", err)
	}
	if string(after) != before {
		t.Errorf("#6162: grafel update modified the .gitignore of a repo it was\n"+
			"merely run inside.\n before = %q\n after  = %q", before, string(after))
	}
}

// TestRunUpdate_InstallsNoGitHooksInUnregisteredRepo is the invisible half of
// #6162: `git status` never shows .git/hooks, so this side effect is silent.
func TestRunUpdate_InstallsNoGitHooksInUnregisteredRepo(t *testing.T) {
	env := newTestEnv(t)
	standInsideRepo(t, env.gitRepo)

	opts := install.UpdateOptions{
		BinPath:           env.fakeBin,
		StatePath:         env.statePath,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		SkipDaemonRestart: true,
		Tag:               "v0.0.1-nohooks",
		DownloadBinary:    newBinaryDownloader("#!/bin/sh\necho newer-grafel"),
	}
	if _, err := install.RunUpdate(opts); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}

	for _, name := range grafelHookNames {
		if hookPresent(t, env.gitRepo, name) {
			t.Errorf("#6162: grafel update installed the %s hook into a repo it was "+
				"merely run inside (%s)", name, env.gitRepo)
		}
	}
}

// TestRunUpdate_DoesNotCreateGitignoreWhenAbsent covers the repo that has no
// .gitignore at all: update must not create one.
func TestRunUpdate_DoesNotCreateGitignoreWhenAbsent(t *testing.T) {
	env := newTestEnv(t)
	standInsideRepo(t, env.gitRepo)

	gitignorePath := filepath.Join(env.gitRepo, ".gitignore")
	if _, err := os.Stat(gitignorePath); err == nil {
		t.Fatalf("fixture already has a .gitignore at %s", gitignorePath)
	}

	opts := install.UpdateOptions{
		BinPath:           env.fakeBin,
		StatePath:         env.statePath,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		SkipDaemonRestart: true,
		Tag:               "v0.0.1-nocreate",
		DownloadBinary:    newBinaryDownloader("#!/bin/sh\necho newer-grafel"),
	}
	res, err := install.RunUpdate(opts)
	if err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	if _, err := os.Stat(gitignorePath); err == nil {
		t.Errorf("#6162: grafel update created %s in a repo it was merely run inside", gitignorePath)
	}
	if res.InstallResult != nil && res.InstallResult.GitignoreRepo != "" {
		t.Errorf("#6162: update reported GitignoreRepo = %q; an upgrade touches no repo",
			res.InstallResult.GitignoreRepo)
	}
}

// TestRunUpdate_StillPerformsUpgradeWork pins the OTHER side of the fix: the
// scoping change must not degenerate into "update does nothing". The steps an
// upgrade genuinely owns — new binary in place, its SHA recorded, skills
// re-copied to match the new binary, MCP entry re-registered — must all still
// happen.
func TestRunUpdate_StillPerformsUpgradeWork(t *testing.T) {
	env := newTestEnv(t)
	standInsideRepo(t, env.gitRepo)

	const newContent = "#!/bin/sh\necho newer-grafel"

	opts := install.UpdateOptions{
		BinPath:           env.fakeBin,
		StatePath:         env.statePath,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		SkipDaemonRestart: true,
		Tag:               "v0.0.1-upgradework",
		DownloadBinary:    newBinaryDownloader(newContent),
	}
	res, err := install.RunUpdate(opts)
	if err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	if res.Skipped {
		t.Fatal("update reported Skipped for a genuinely different binary")
	}

	// The binary itself was replaced.
	got, err := os.ReadFile(env.fakeBin)
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if string(got) != newContent {
		t.Errorf("binary content = %q, want %q", got, newContent)
	}

	// install.json records the NEW binary's SHA (step 1 belongs to an upgrade).
	wantSHA, err := install.SHA256FilePublic(env.fakeBin)
	if err != nil {
		t.Fatalf("sha new binary: %v", err)
	}
	state := readState(t, env.statePath)
	if state == nil {
		t.Fatal("install.json missing after update")
	}
	if state.CLI.SHA256 != wantSHA {
		t.Errorf("install.json CLI.SHA256 = %q, want the new binary's %q", state.CLI.SHA256, wantSHA)
	}
	if state.PartialInstall {
		t.Error("install.json: partial_install true after a successful update")
	}

	// Skills were re-copied (step 2 belongs to an upgrade: skills ship with
	// the binary and must stay consistent with it).
	if len(res.InstallResult.SkillsInstalled) == 0 {
		t.Error("update installed no skills; skills must stay consistent with the new binary")
	}

	// MCP registration was refreshed (step 3).
	cfg, err := os.ReadFile(env.claudeJSON)
	if err != nil {
		t.Fatalf("read .claude.json: %v", err)
	}
	if !strings.Contains(string(cfg), "grafel") {
		t.Errorf("update left no grafel MCP entry in %s: %s", env.claudeJSON, cfg)
	}
}

// TestRunUpdate_PreservesPriorInstallState pins the install.json decision: an
// upgrade MERGES into the recorded state rather than replacing it with a fresh
// NewState(ModeCopy). Concretely, the gitignore record written by the earlier
// deliberate `grafel install` must survive an update — `grafel doctor`
// iterates state.Gitignore.Repos, so blanking it silently drops that check.
func TestRunUpdate_PreservesPriorInstallState(t *testing.T) {
	env := newTestEnv(t)

	// A deliberate install, run inside the repo: this one legitimately writes
	// the .gitignore and records it.
	if _, err := install.RunCopy(install.CopyOptions{
		Intent:            install.IntentInstall,
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: true,
	}); err != nil {
		t.Fatalf("RunCopy (initial install): %v", err)
	}
	before := readState(t, env.statePath)
	if before == nil || len(before.Gitignore.Repos) == 0 {
		t.Fatalf("fixture: install did not record a gitignore repo: %+v", before)
	}

	// Now upgrade while STANDING IN A DIFFERENT REPO — the realistic case, and
	// the one that catches both failure modes at once: the record must neither
	// be blanked (no merge) nor rewritten to name this bystander (step 5 still
	// running).
	bystander := newGitRepoFixture(t, "bystander")
	standInsideRepo(t, bystander)
	if _, err := install.RunUpdate(install.UpdateOptions{
		BinPath:           env.fakeBin,
		StatePath:         env.statePath,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		SkipDaemonRestart: true,
		Tag:               "v0.0.1-mergestate",
		DownloadBinary:    newBinaryDownloader("#!/bin/sh\necho newer-grafel"),
	}); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}

	after := readState(t, env.statePath)
	if after == nil {
		t.Fatal("install.json missing after update")
	}
	if len(after.Gitignore.Repos) != len(before.Gitignore.Repos) ||
		(len(after.Gitignore.Repos) > 0 && after.Gitignore.Repos[0] != before.Gitignore.Repos[0]) {
		t.Errorf("#6162: update replaced install.json instead of merging into it;\n"+
			" gitignore repos before = %v\n after = %v",
			before.Gitignore.Repos, after.Gitignore.Repos)
	}
	// And the bystander must appear nowhere in the record, nor on disk.
	for _, r := range after.Gitignore.Repos {
		if strings.Contains(r, "bystander") {
			t.Errorf("#6162: update recorded the repo it was standing in (%s) as a "+
				"gitignore target", r)
		}
	}
	if _, err := os.Stat(filepath.Join(bystander, ".gitignore")); err == nil {
		t.Errorf("#6162: update created a .gitignore in the bystander repo %s", bystander)
	}
}

// TestRunCopy_StillDoesRepoIntegration guards the fix from over-reaching.
// `grafel install` is cwd-aware BY DESIGN — the user ran it here, on purpose —
// so it must keep writing .gitignore and installing the four hooks.
func TestRunCopy_StillDoesRepoIntegration(t *testing.T) {
	env := newTestEnv(t)

	res, err := install.RunCopy(install.CopyOptions{
		Intent:            install.IntentInstall,
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: true,
	})
	if err != nil {
		t.Fatalf("RunCopy: %v", err)
	}

	// DetectGitRepo returns git's own resolved root, so compare through
	// EvalSymlinks (/var → /private/var on macOS).
	wantRepo, err := filepath.EvalSymlinks(env.gitRepo)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	gotRepo, err := filepath.EvalSymlinks(res.GitignoreRepo)
	if err != nil {
		t.Fatalf("install recorded no usable GitignoreRepo (%q): %v", res.GitignoreRepo, err)
	}
	if gotRepo != wantRepo {
		t.Errorf("install GitignoreRepo = %q, want %q", gotRepo, wantRepo)
	}
	data, err := os.ReadFile(filepath.Join(env.gitRepo, ".gitignore"))
	if err != nil {
		t.Fatalf("install did not write .gitignore: %v", err)
	}
	if !strings.Contains(string(data), "/.grafel/") {
		t.Errorf("install .gitignore lacks /.grafel/: %q", data)
	}
	for _, name := range grafelHookNames {
		if !hookPresent(t, env.gitRepo, name) {
			t.Errorf("install did not write the %s hook", name)
		}
	}
}

// TestRunCopy_RejectsUnsetIntent pins the zero-value decision (#6162 review).
//
// Intent has no default. The two candidate defaults are not symmetric: an
// omission that means "install" silently appends to a stranger's tracked
// .gitignore and writes four hooks `git status` does not show — nothing fails,
// and the user finds out in a diff or never. Rather than pick the less-bad
// silent failure, RunCopy refuses to guess, so every caller has to state
// whether it may modify the user's repository.
func TestRunCopy_RejectsUnsetIntent(t *testing.T) {
	env := newTestEnv(t)
	standInsideRepo(t, env.gitRepo)

	_, err := install.RunCopy(install.CopyOptions{
		// Intent deliberately unset.
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: true,
	})
	if err == nil {
		t.Fatal("#6162: RunCopy accepted an unset Intent; a caller that has not " +
			"declared whether it may modify the user's repo must not be guessed for")
	}
	if !strings.Contains(err.Error(), "Intent") {
		t.Errorf("error should name the missing field, got: %v", err)
	}

	// A rejected transaction must not have run any step.
	if _, err := os.Stat(filepath.Join(env.gitRepo, ".gitignore")); err == nil {
		t.Error("#6162: rejected RunCopy still wrote .gitignore")
	}
	for _, name := range grafelHookNames {
		if hookPresent(t, env.gitRepo, name) {
			t.Errorf("#6162: rejected RunCopy still wrote the %s hook", name)
		}
	}
}

// TestRunCopy_RejectsWorkingDirUnderUpgrade pins the hard-fail (#6162 review
// F2). Accepting-and-ignoring a WorkingDir under IntentUpgrade would read as a
// second layer of defence and is not one — an empty WorkingDir still resolves
// to the process cwd inside both DetectGitRepo and InstallGitHooks. Failing
// loudly is what keeps the next reader from believing in a defence that does
// not exist.
func TestRunCopy_RejectsWorkingDirUnderUpgrade(t *testing.T) {
	env := newTestEnv(t)

	_, err := install.RunCopy(install.CopyOptions{
		Intent:            install.IntentUpgrade,
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: true,
	})
	if err == nil {
		t.Fatal("#6162: RunCopy accepted a WorkingDir under IntentUpgrade; " +
			"an upgrade has no repository in scope")
	}
	if !strings.Contains(err.Error(), "WorkingDir") {
		t.Errorf("error should name the offending field, got: %v", err)
	}
}
