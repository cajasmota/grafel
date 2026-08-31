package registry_test

// home_isolation_sweep_guard_6735_test.go — the repo-wide, BINDING guard on the
// #6171/#6288 "this test redirected HOME and left GRAFEL_HOME pointing
// somewhere else" class.
//
// # The incident (#6735)
//
// registry.HomeDir() resolves GRAFEL_HOME FIRST and only falls back to the OS
// home. A test that isolates HOME alone therefore makes the code under test
// read one directory while the fixture is written to another. Since #6210 the
// dashboard effects sidecar goes through that resolver, so
// internal/dashboard/v2_paths_effective_effects_test.go's writeEffectsSidecar —
// which redirected HOME/USERPROFILE only — wrote its fixture under $HOME and
// had the reader look under $GRAFEL_HOME. Both callers failed as
// `effective_effects: want db_write, got []`: a file-not-found wearing a
// misleading assertion message, and roughly a session of investigation.
//
// It is reachable by following our own docs. AGENTS.md tells contributors and
// agents to isolate with `export HOME=$(mktemp -d); export
// GRAFEL_HOME=$HOME/.grafel; export GRAFEL_DAEMON_ROOT=$HOME/.grafel`. The
// inner t.Setenv("HOME", …) re-points HOME and leaves GRAFEL_HOME aimed at the
// OUTER sandbox — the two diverge, and only for people who isolate properly.
// CI never sets GRAFEL_HOME, so CI cannot see this class at all, by
// construction. Detection has to be local, which is what this file is.
//
// # Why repo-wide, and why here
//
// The detector (testsupport.FindUnisolatedHomeTests) already existed — #6171
// wrote it for internal/install, #6288 moved it to internal/testsupport so
// internal/mcp could run it too. Its own doc records why that was not enough:
// "A guard whose reach is one directory is a guard that covers one directory;
// the reach was the defect, not the rule." Two packages had wired it. The
// package that took the #6735 hit had not, and no scan anywhere could see it.
//
// So this guard reuses that detector and walks the WHOLE tree, and it lives
// beside TestNoHandRolledGrafelHomePaths (home_sweep_guard_6178_test.go),
// which is the established shape for a repo-wide, allow-listed sweep in this
// codebase: scan, subtract two declared ledgers, fail on the remainder, and
// fail again on a ledger entry the live scan no longer produces.
//
// # The rule this enforces, and the one it deliberately does NOT
//
// The shared detector reports a function missing EITHER of GRAFEL_HOME or
// USERPROFILE. This guard binds only the GRAFEL_HOME half — that is the
// decision recorded on #6735, and it is also the half that is silently wrong on
// the machine of whoever is running the tests. The USERPROFILE half (#6288,
// os.UserHomeDir() reads %USERPROFILE% and ignores $HOME on Windows) stays
// reported by the two per-package wirings, which bind both.
//
// # Everything in the ledgers below was MEASURED, not assumed
//
// The sweep found 71 functions. All of internal/{cli,daemon,...,install,
// licenses,pathboundary,testsupport,dashboard} were then run with GRAFEL_HOME
// pointed at a sandbox (2026-09-01, -count=1). Exactly two tests failed, both
// in internal/dashboard, both callers of writeEffectsSidecar. Every other
// offender is latent: the divergence exists in the source but nothing it
// resolves goes through registry.HomeDir() today. The dashboard sites are fixed
// in this change; the rest are frozen in grafelHomePinDeferred so the guard is
// binding for anything NEW.
//
// That freeze is the point, and #6290 is why it is a freeze and not a sed:
// "Treating the remainder as a sed is how a guard acquires a wall of noise that
// everybody learns to ignore." A ledgered entry costs nothing to keep and
// cannot grow; converting one to testsupport.IsolateHome(t) is a mechanical
// follow-up that this guard will then FORCE the author to record, because the
// stale-entry check fails on a ledger entry the scan no longer produces.
//
// # Inherited blind spots
//
// This guard is only as sharp as testsupport.FindUnisolatedHomeTests, whose
// misses are pinned as MISS rows in internal/testsupport/homescan_test.go and
// documented in homescan.go. The big one: it is keyed on the PRESENCE of a
// Setenv("HOME", …) call, so a test that isolates NOTHING AT ALL is invisible
// to it. Deleting an IsolateHome call and leaving no Setenv behind passes this
// guard. It also reasons one function at a time, so a helper that sets HOME
// while its CALLER pins GRAFEL_HOME is a false positive.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

// deliberatelyUnpinnedHome lists functions that redirect HOME and must NOT pin
// GRAFEL_HOME, because pinning it would weaken or defeat the property under
// test. Each was read, not pattern-matched.
var deliberatelyUnpinnedHome = map[string]string{
	"internal/testsupport/isolate_test.go:TestGuardRealHomeFailsWhenHomeIsRealHome":  "points HOME back at the REAL user home on purpose, to assert GuardRealHome fires. Isolating it is the one thing this test must not do — already recorded as a false positive in homescan.go.",
	"internal/envguard/envguard_6331_test.go:TestRealUserHomeIgnoresHOME":            "asserts RealUserHome() does NOT move when HOME moves. GRAFEL_HOME is not an input to the function under test.",
	"internal/pathboundary/pathboundary_test.go:TestClimb_NoHomeWarnsOncePerProcess": "clears HOME/USERPROFILE/home to force os.UserHomeDir() to FAIL on every platform, then asserts the degraded-boundary diagnostic fires once. pathboundary resolves no grafel state; a GRAFEL_HOME pin would be inert decoration.",
	"internal/pathboundary/pathboundary_test.go:TestClimb_HomeResolvableStaysQuiet":  "the negative counterpart of the above: HOME must resolve to a known temp dir and nothing else. Same reasoning.",
}

// grafelHomePinDeferred is a debt ledger of functions that redirect HOME
// without pinning GRAFEL_HOME and are NOT known to be broken by it today —
// every one was run with GRAFEL_HOME set on 2026-09-01 and passed. The value is
// a family tag; see deferredFamilyReason.
//
// This list must only ever SHRINK. A new hit that is not the removal of an
// existing entry is exactly the defect this guard exists to catch before merge.
var grafelHomePinDeferred = map[string]string{
	"internal/cli/install_refreshstate_test.go:refreshStateEnv":                                                  "cli",
	"internal/cli/install_registermcp_6169_test.go:TestInstallRefreshState_RejectsRegisterMCP":                   "cli",
	"internal/cli/install_registermcp_6169_test.go:TestInstallRegisterMCP_AcceptsClaudeConfigDirs":               "cli",
	"internal/cli/install_registermcp_6169_test.go:TestInstallRegisterMCP_DoesNotTouchTheCallersRepo":            "cli",
	"internal/cli/install_registermcp_6169_test.go:TestInstallRegisterMCP_RegistersAndRecordsOnAVirginMachine":   "cli",
	"internal/cli/install_registermcp_6169_test.go:TestInstallRegisterMCP_RejectsCombinedFlags":                  "cli",
	"internal/cli/watcher_ctl_detect_darwin_test.go:TestDefaultServiceInstalledForThisRoot_Darwin_DifferentHome": "cli",
	"internal/cli/watcher_ctl_detect_darwin_test.go:TestDefaultServiceInstalledForThisRoot_Darwin_MatchingHome":  "cli",
	"internal/cli/watcher_ctl_detect_darwin_test.go:TestDefaultServiceInstalledForThisRoot_Darwin_NoService":     "cli",
	"internal/cli/watcher_ctl_detect_darwin_test.go:writeDarwinPlist":                                            "cli",
	"internal/cli/watcher_ctl_detect_linux_test.go:TestDefaultServiceInstalledForThisRoot_Linux_DifferentHome":   "cli",
	"internal/cli/watcher_ctl_detect_linux_test.go:TestDefaultServiceInstalledForThisRoot_Linux_MatchingHome":    "cli",
	"internal/cli/watcher_ctl_detect_linux_test.go:TestDefaultServiceInstalledForThisRoot_Linux_NoService":       "cli",
	"internal/cli/watcher_ctl_detect_linux_test.go:writeLinuxUnit":                                               "cli",
	"internal/cli/watcher_persist_6180_darwin_test.go:TestDarwinWatcherUnloadIsPersistent":                       "cli",

	"internal/daemon/paths_test.go:TestDefaultLayout_DaemonRootEnvOverridesXDG":                            "daemon",
	"internal/daemon/paths_test.go:TestDefaultLayout_XDGPathWhenAvailable":                                 "daemon",
	"internal/daemon/paths_test.go:TestSelectSocketPath_ErrorWhenBothTooLong":                              "daemon",
	"internal/daemon/paths_test.go:TestSelectSocketPath_FallbackToHomeWhenXDGTooLong":                      "daemon",
	"internal/daemon/paths_test.go:TestSelectSocketPath_PreferXDGWhenSet":                                  "daemon",
	"internal/daemon/paths_test.go:TestSelectSocketPath_UseHomeWhenXDGNotSet":                              "daemon",
	"internal/daemon/service/launchd_rewrite_darwin_test.go:TestWriteUnit_RewritesLegacyDaemonUnitToServe": "daemon",
	"internal/daemon/service/launchd_stop_persistence_darwin_test.go:darwinTestOpts":                       "daemon",
	"internal/daemon/service/registeredroot_darwin_test.go:TestExtractPlistHome":                           "daemon",
	"internal/daemon/service/registeredroot_darwin_test.go:TestRegisteredRoot_NotInstalled":                "daemon",
	"internal/daemon/service/registeredroot_darwin_test.go:TestRegisteredRoot_ReadsInstalledPlist":         "daemon",
	"internal/daemon/service/systemd_rewrite_linux_test.go:TestWriteUnit_RewritesLegacyDaemonUnitToServe":  "daemon",

	"internal/install/detect/classify_test.go:TestClassifyPath_SkipsHomeParentSiblingScan":                            "install",
	"internal/install/detect/protected_home_scan_darwin_test.go:TestClassifyHome_DoesNotDescendIntoProtectedChildren": "install",
	"internal/install/detect/protected_home_scan_darwin_test.go:TestClassifyProtectedFolderDirectly_StillDescends":    "install",
	"internal/install/mcpreg/nulldoc_6163_test.go:TestRegisterPath_NonObjectDocumentsStillFailCleanly":                "install",
	"internal/install/mcpreg/nulldoc_6163_test.go:TestRegisterPath_NullDocumentDoesNotPanic":                          "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestBackupSidecar_KeysOnTheUnresolvedPath":                     "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestRegisterPath_NewConfigIsPrivate":                           "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestRegisterPath_NullMCPServersIsTreatedAsAbsent":              "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestRegisterPath_PreservesDestinationMode":                     "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestRegisterPath_SymlinkCycleFailsLoudly":                      "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestRegisterPath_SymlinkPreservesTargetMode":                   "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestRegisterPath_SymlinkTargetIsGuarded":                       "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestRegisterPath_TOMLNewConfigIsPrivate":                       "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestRegisterPath_TOMLPreservesMode":                            "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestRegisterPath_TOMLWritesThroughSymlink":                     "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestRegisterPath_UnparseableConfigAlsoClassifies":              "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestRegisterPath_WritesThroughSymlink":                         "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestRegisterPath_WrongTypedMCPServersFailsCleanly":             "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestRestoreSnapshot_ChainWithinTheKernelLimitStillResolves":    "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestRestoreSnapshot_DoesNotWidenMode":                          "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestRestoreSnapshot_OverlongSymlinkChainFailsLoudly":           "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestRestoreSnapshot_SentinelRemovalIsGuarded":                  "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestRestoreSnapshot_SymlinkCycleFailsLoudly":                   "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestRestoreSnapshot_WritesThroughSymlink":                      "install",
	"internal/install/mcpreg/symlink_perm_6240_test.go:TestUnregisterPath_WritesThroughSymlinkAndKeepsMode":           "install",
	"internal/install/skilllink/skilllink_test.go:TestDiscoverSkillsDir":                                              "install",
	"internal/install/skilllink/skilllink_test.go:TestDiscoverSkillsDirVerbose_ReportsAllAttemptedPaths":              "install",
	"internal/install/skilllink/skilllink_test.go:TestMaterializeEmbeddedSkills":                                      "install",
	"internal/install/watchers/cleanup_test.go:TestCleanup_Idempotent":                                                "install",
	"internal/install/watchers/cleanup_test.go:TestCleanup_RemovesUnitFile":                                           "install",
	"internal/install/watchers/loader_darwin_persist_test.go:TestInstalledUnits_MissingDirIsEmpty":                    "install",
	"internal/install/watchers/loader_darwin_persist_test.go:TestInstalledUnits_ReadsFromDisk":                        "install",
	"internal/install/watchers/loader_darwin_persist_test.go:TestRawLabelRoundTrips":                                  "install",
	"internal/install/watchers/loader_darwin_persist_test.go:testUnit":                                                "install",
	"internal/install/watchers/loader_darwin_test.go:writePlist":                                                      "install",
	"internal/install/watchers/migrate_6183_test.go:migrateSandbox":                                                   "install",
	"internal/install/watchers/plist_respawn_6179_test.go:TestWrite_RewritesStalePlistInPlace":                        "install",

	"internal/licenses/licenses_fifo_6416_test.go:TestGemLicenseFIFODoesNotHang": "licenses",
}

// deferredFamilyReason explains each family tag used in grafelHomePinDeferred.
// Every claim of "measured green" refers to the 2026-09-01 -count=1 sweep
// described in this file's doc comment.
var deferredFamilyReason = map[string]string{
	"cli":      "CLI install / watcher-detection tests. They redirect HOME to keep install state and launchd/systemd units out of the real home; the paths they assert on come off HOME and XDG, not registry.HomeDir(). Measured green with GRAFEL_HOME set.",
	"daemon":   "Daemon socket / runtime-layout / launchd-plist tests. Those paths are governed by the GRAFEL_DAEMON_ROOT and XDG axes, a documented override separate from GRAFEL_HOME (see internal/testsupport/guard_main.go on the two axes). Measured green with GRAFEL_HOME set.",
	"install":  "Editor-MCP-config, watcher-unit and skills-link install tests. Same shape as the cli family: HOME/XDG-derived config paths, no registry.HomeDir() in the resolution they assert on. Measured green with GRAFEL_HOME set.",
	"licenses": "Redirects HOME only to keep a package-manager cache out of the real home; resolves no grafel state at all. Measured green with GRAFEL_HOME set.",
}

// TestNoTestRedirectsHomeWithoutPinningGrafelHome is the binding sweep.
func TestNoTestRedirectsHomeWithoutPinningGrafelHome(t *testing.T) {
	for key, fam := range grafelHomePinDeferred {
		if _, ok := deferredFamilyReason[fam]; !ok {
			t.Fatalf("grafelHomePinDeferred[%q] uses family %q, which has no entry in deferredFamilyReason — a ledger entry without a stated reason is not a decision, it is a silence", key, fam)
		}
		if _, dup := deliberatelyUnpinnedHome[key]; dup {
			t.Fatalf("%q is on BOTH ledgers — it cannot be simultaneously deliberate and deferred", key)
		}
	}

	offenders := scanUnpinnedGrafelHome(t, repoRoot(t))

	seen := map[string]bool{}
	var unexpected []string
	for _, o := range offenders {
		key := o.File + ":" + o.Fn
		seen[key] = true
		_, deliberate := deliberatelyUnpinnedHome[key]
		_, deferred := grafelHomePinDeferred[key]
		if !deliberate && !deferred {
			unexpected = append(unexpected, o.String())
		}
	}
	sort.Strings(unexpected)
	if len(unexpected) > 0 {
		t.Errorf("test function(s) redirect $HOME without pinning GRAFEL_HOME (#6735), on neither ledger:\n  %s\n\n"+
			"registry.HomeDir() prefers GRAFEL_HOME and only falls back to the OS home, so a test that "+
			"moves HOME alone makes the code under test read a DIFFERENT directory than the fixture was "+
			"written to — whenever GRAFEL_HOME is set in the ambient environment, which AGENTS.md's own "+
			"sandbox recipe tells you to do. CI never sets it, so CI will never catch this for you.\n\n"+
			"Fix: replace the t.Setenv(\"HOME\", …) block with `home := testsupport.IsolateHome(t)`, which "+
			"sets HOME, USERPROFILE, GRAFEL_HOME, GRAFEL_DAEMON_ROOT and both XDG vars together. If the "+
			"test genuinely must leave GRAFEL_HOME alone (it is asserting something ABOUT home "+
			"resolution), add it to deliberatelyUnpinnedHome with a reason. Do NOT add it to "+
			"grafelHomePinDeferred — that ledger only shrinks.",
			strings.Join(unexpected, "\n  "))
	}

	// Both ledgers are declarations about the live tree, not wishlists. An
	// entry the scan no longer produces means the site was fixed, renamed or
	// deleted; leaving it behind hides that from the next reader and lets the
	// ledger stop shrinking without anyone noticing.
	var stale []string
	for key := range deliberatelyUnpinnedHome {
		if !seen[key] {
			stale = append(stale, "deliberatelyUnpinnedHome: "+key)
		}
	}
	for key := range grafelHomePinDeferred {
		if !seen[key] {
			stale = append(stale, "grafelHomePinDeferred: "+key)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("ledger entries the live scan no longer produces (fixed, renamed or removed — delete the entry):\n  %s",
			strings.Join(stale, "\n  "))
	}
}

// TestUnpinnedGrafelHomeSweepCanFail is the guard's own guard. A gate that
// cannot go red is the failure mode this repo hits most often, so the scan is
// pointed at a synthetic tree holding one offender and one compliant function
// and required to report exactly the offender. This exercises the walk, the
// GRAFEL_HOME-only narrowing and the reporting — everything except the
// repo-root ledger subtraction.
func TestUnpinnedGrafelHomeSweepCanFail(t *testing.T) {
	root := t.TempDir()
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte(src), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	// Offender: HOME redirected, GRAFEL_HOME never pinned.
	write("offender_test.go", `package p
import "testing"
func TestOffender(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
}`)
	// Compliant: pins GRAFEL_HOME explicitly.
	write("pinned_test.go", `package p
import "testing"
func TestPinned(t *testing.T) {
	d := t.TempDir()
	t.Setenv("HOME", d)
	t.Setenv("USERPROFILE", d)
	t.Setenv("GRAFEL_HOME", d+"/.grafel")
}`)
	// Compliant: delegates to the canonical helper.
	write("isolated_test.go", `package p
import "testing"
func TestIsolated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testsupport.IsolateHome(t)
}`)
	// Not a _test.go file: outside the scan's reach entirely.
	write("prod.go", `package p
import "os"
func f() { os.Setenv("HOME", "/tmp/x") }`)

	got := scanUnpinnedGrafelHome(t, root)
	if len(got) != 1 {
		t.Fatalf("sweep reported %d offenders over the synthetic tree, want exactly 1 (TestOffender): %v", len(got), got)
	}
	if got[0].Fn != "TestOffender" || got[0].File != "offender_test.go" {
		t.Fatalf("sweep named %s:%s, want offender_test.go:TestOffender", got[0].File, got[0].Fn)
	}
	if len(got[0].Missing) != 1 || got[0].Missing[0] != "GRAFEL_HOME" {
		t.Fatalf("offender.Missing = %v, want exactly [GRAFEL_HOME] — the USERPROFILE half must be narrowed out", got[0].Missing)
	}
}

// TestUnpinnedGrafelHomeSweepIsNotVacuous pins that the repo walk actually
// reaches source: a walk that reads nothing reports nothing and looks green.
func TestUnpinnedGrafelHomeSweepIsNotVacuous(t *testing.T) {
	if n := countTestFiles(t, repoRoot(t)); n < 100 {
		t.Fatalf("repo walk parsed %d _test.go files; the sweep is not binding the repository", n)
	}
}

// scanUnpinnedGrafelHome walks root for _test.go files and returns every
// function the shared detector reports as missing a GRAFEL_HOME pin. The
// USERPROFILE half of the detector's rule is deliberately dropped here — see
// this file's doc comment.
func scanUnpinnedGrafelHome(t *testing.T, root string) []testsupport.UnisolatedHomeTest {
	t.Helper()
	var out []testsupport.UnisolatedHomeTest
	files := 0
	walkTestFiles(t, root, func(rel string, fset *token.FileSet, f *ast.File) {
		files++
		for _, o := range testsupport.FindUnisolatedHomeTests(fset, f, rel) {
			for _, missing := range o.Missing {
				if missing != "GRAFEL_HOME" {
					continue
				}
				o.Missing = []string{"GRAFEL_HOME"}
				out = append(out, o)
				break
			}
		}
	})
	if files == 0 {
		t.Fatalf("scan of %s parsed no _test.go files — it proves nothing", root)
	}
	return out
}

func countTestFiles(t *testing.T, root string) int {
	t.Helper()
	n := 0
	walkTestFiles(t, root, func(string, *token.FileSet, *ast.File) { n++ })
	return n
}

func walkTestFiles(t *testing.T, root string, visit func(rel string, fset *token.FileSet, f *ast.File)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			// .claude holds full worktree checkouts of this same repo;
			// walking it would scan (and re-report) other branches' source.
			case ".git", ".claude", "node_modules", "vendor", "testdata", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("parse %s: %v", rel, perr)
		}
		visit(filepath.ToSlash(rel), fset, f)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}
