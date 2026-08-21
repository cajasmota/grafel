//go:build unix

package licenses

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// licensesFIFODeadline bounds each subtest. A correctly-gated read returns in
// microseconds — safeio's stat gate never opens a FIFO — so anything near this
// value is the hang, not a slow machine. It is deliberately well under the
// package test timeout so a regression FAILS with attribution rather than
// wedging the suite until the watchdog kills the binary.
const licensesFIFODeadline = 10 * time.Second

// mkfifoInTemp creates a named pipe at dir/rel, where dir must be a
// t.TempDir(). A FIFO outside a temp dir would outlive the test and hang any
// other process that walks over it, so this helper takes the ROOT temp
// DIRECTORY separately from the relative path and cannot be pointed elsewhere
// by accident.
func mkfifoInTemp(t *testing.T, dir string, rel ...string) string {
	t.Helper()
	p := filepath.Join(append([]string{dir}, rel...)...)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
	}
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatalf("mkfifo %s: %v", p, err)
	}
	// t.TempDir's RemoveAll unlinks a FIFO without opening it, so cleanup is
	// already correct; this is belt-and-braces against a future refactor that
	// stops using t.TempDir.
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

// mustReturn runs fn and fails the test if it has not returned within
// licensesFIFODeadline. Every call into the readers below needs this, not just
// the liveness tests: under the pre-fix code a bare call parks in open(2)
// forever, which would wedge the whole test binary until the -timeout watchdog
// killed it with no attribution AND leave the t.TempDir cleanup unrun — which
// leaks a FIFO onto a shared machine.
func mustReturn(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(licensesFIFODeadline):
		t.Fatalf("HANG: %s did not return within %s", what, licensesFIFODeadline)
	}
}

// TestDetectProjectLicenseFIFODoesNotHang is the #6416 regression for the five
// name-chosen reads at the REPO ROOT.
//
// DetectProjectLicense is the first line of ScanRepoLicenses, which is what
// internal/mcp/license_tools.go calls. Every path it reads is joined from a
// literal filename onto the repo root — no walker sits in between, so the
// walker's entry-type gate cannot protect any of them. An MCP client pointing
// the license tool at a repo containing `mkfifo LICENSE` parked a daemon
// goroutine in open(2) permanently: no timeout, no error, no log line.
//
// This is a HANG, so the honest way to pin it is a deadline, not an assertion
// about a return value: the pre-fix code never produced a value to assert on.
func TestDetectProjectLicenseFIFODoesNotHang(t *testing.T) {
	// Every name DetectProjectLicense chooses for itself, in the order it
	// tries them. The LICENSE loop short-circuits on the first name that
	// reads, so each name needs its own tree to be reached at all.
	for _, name := range []string{
		"LICENSE", "LICENSE.txt", "LICENSE.md", "LICENCE", "COPYING",
		"package.json", "pyproject.toml", "Cargo.toml",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			mkfifoInTemp(t, dir, name)

			var lic, src string
			mustReturn(t, "DetectProjectLicense with a FIFO named "+name, func() {
				lic, src = DetectProjectLicense(dir)
			})
			// A refused file must degrade to the same "nothing found"
			// behaviour an absent one already produces, not to a partial or
			// fabricated identifier.
			if lic != "Unknown" || src != "" {
				t.Errorf("DetectProjectLicense = (%q, %q), want (\"Unknown\", \"\") for a refused %s", lic, src, name)
			}
		})
	}
}

// TestNPMLicenseFIFODoesNotHang covers the two npm readers. Both build their
// path from a literal filename — "package.json" under node_modules/<pkg>, and
// "package-lock.json" at the root — so both are name-chosen and neither is
// walker-gated.
func TestNPMLicenseFIFODoesNotHang(t *testing.T) {
	t.Run("node_modules/package.json", func(t *testing.T) {
		dir := t.TempDir()
		mkfifoInTemp(t, dir, "node_modules", "left-pad", "package.json")

		var got map[string]string
		mustReturn(t, "DetectNPMLicenses with a FIFO package.json", func() {
			got = DetectNPMLicenses(dir, []string{"left-pad"})
		})
		if got["left-pad"] != "Unknown" {
			t.Errorf("DetectNPMLicenses[left-pad] = %q, want \"Unknown\"", got["left-pad"])
		}
	})

	t.Run("package-lock.json", func(t *testing.T) {
		dir := t.TempDir()
		mkfifoInTemp(t, dir, "package-lock.json")

		var got map[string]string
		mustReturn(t, "ResolveNPMTransitiveDeps with a FIFO package-lock.json", func() {
			got = ResolveNPMTransitiveDeps(dir)
		})
		if len(got) != 0 {
			t.Errorf("ResolveNPMTransitiveDeps = %v, want none for a refused lockfile", got)
		}
	})
}

// TestPyPIMetadataFIFODoesNotHang covers tryDistInfo's METADATA read. The
// dist-info DIRECTORY name comes from a ReadDir, but "METADATA" inside it is a
// literal, so a FIFO by that name reproduces the hang.
func TestPyPIMetadataFIFODoesNotHang(t *testing.T) {
	dir := t.TempDir()
	mkfifoInTemp(t, dir, ".venv", "lib", "requests-2.31.0.dist-info", "METADATA")

	var got map[string]string
	mustReturn(t, "DetectPyPILicensesLocal with a FIFO METADATA", func() {
		got = DetectPyPILicensesLocal(dir, []string{"requests"})
	})
	if got["requests"] != "Unknown" {
		t.Errorf("DetectPyPILicensesLocal[requests] = %q, want \"Unknown\"", got["requests"])
	}
}

// TestGoModCacheLicenseFIFODoesNotHang covers the module-cache LICENSE read.
// The versioned directory is discovered by ReadDir, but the four filenames
// inside it are a fixed list — the same shape as DetectProjectLicense's loop.
func TestGoModCacheLicenseFIFODoesNotHang(t *testing.T) {
	for _, name := range []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "COPYING"} {
		t.Run(name, func(t *testing.T) {
			modCache := t.TempDir()
			// The module cache escapes "/" in the rest of the module path as
			// "+", so github.com/foo/bar lives at github.com/foo+bar@v1.2.3.
			// Getting this wrong makes the fixture miss the read entirely and
			// the test pass vacuously — it did, on the first RED run.
			mkfifoInTemp(t, modCache, "github.com", "foo+bar@v1.2.3", name)

			var got string
			mustReturn(t, "detectGoModLicense with a FIFO named "+name, func() {
				got = detectGoModLicense(modCache, "github.com/foo/bar")
			})
			if got != "" {
				t.Errorf("detectGoModLicense = %q, want \"\" for a refused %s", got, name)
			}
		})
	}
}

// TestGemLicenseFIFODoesNotHang covers the two Ruby readers: "Gemfile.lock" at
// the repo root, and the bundler-cache gemspec whose path is built from a name
// and version parsed out of that lockfile. Neither comes from the walker.
func TestGemLicenseFIFODoesNotHang(t *testing.T) {
	t.Run("Gemfile.lock", func(t *testing.T) {
		dir := t.TempDir()
		mkfifoInTemp(t, dir, "Gemfile.lock")

		var got map[string]string
		mustReturn(t, "DetectGemLicenses with a FIFO Gemfile.lock", func() {
			got = DetectGemLicenses(dir)
		})
		if len(got) != 0 {
			t.Errorf("DetectGemLicenses = %v, want none for a refused lockfile", got)
		}
	})

	t.Run("gemspec", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		mkfifoInTemp(t, home, ".bundle", "cache", "rails-7.1.0")

		var got string
		mustReturn(t, "readGemspecLicense with a FIFO gemspec", func() {
			got = readGemspecLicense("rails", "7.1.0")
		})
		if got != "" {
			t.Errorf("readGemspecLicense = %q, want \"\" for a refused gemspec", got)
		}
	})
}

// TestLicenseSkipIsReported pins the half of the fix that is not the hang.
//
// The #6416 re-review's standing finding is that safeio.ErrNotRegular carries a
// precise reason — path plus entry kind — and that consumers kept throwing it
// away with a bare `return ""`. Here that silence is worse than a gap: a
// refused LICENSE does not merely omit a fact, it makes the project license
// "Unknown", which flips CheckCompatibility's verdict for every dependency in
// the repo. The user gets a different answer with no stated cause, and no way
// to distinguish it from a genuinely unlicensed repo.
func TestLicenseSkipIsReported(t *testing.T) {
	dir := t.TempDir()
	p := mkfifoInTemp(t, dir, "LICENSE")

	var buf strings.Builder
	restore := setLicenseSkipOutput(&buf)
	defer restore()

	mustReturn(t, "DetectProjectLicense", func() { _, _ = DetectProjectLicense(dir) })

	got := buf.String()
	if !strings.Contains(got, p) {
		t.Fatalf("skip report does not name the path %q; got %q", p, got)
	}
	if !strings.Contains(got, "named-pipe") {
		t.Fatalf("skip report does not say WHY (named-pipe); got %q", got)
	}
	if !strings.Contains(got, "#6416") {
		t.Fatalf("skip report does not cite the issue; got %q", got)
	}
}

// TestLicenseMissingIsNotReported guards the other half of the convention, and
// it matters more here than in the go.mod reader: DetectProjectLicense probes
// EIGHT filenames at every repo root and expects most of them to be absent, so
// reporting ENOENT would emit several lines for every healthy repo scanned and
// bury the one line that means something.
func TestLicenseMissingIsNotReported(t *testing.T) {
	dir := t.TempDir()

	var buf strings.Builder
	restore := setLicenseSkipOutput(&buf)
	defer restore()

	mustReturn(t, "DetectProjectLicense on an empty tree", func() {
		_, _ = DetectProjectLicense(dir)
		_ = ResolveNPMTransitiveDeps(dir)
		_ = DetectGemLicenses(dir)
	})

	if got := buf.String(); got != "" {
		t.Fatalf("absent license files were reported as skips: %q", got)
	}
}

// TestLicenseSkipReportIsCapped pins maxLicenseSkipReports. The cap is
// load-bearing rather than a backstop in this package: unlike go.mod (one path
// per repo root) or tsconfig.json, the readers here run once per DEPENDENCY,
// so an attacker-shaped node_modules could otherwise turn the report into the
// denial-of-service the report exists to prevent.
func TestLicenseSkipReportIsCapped(t *testing.T) {
	dir := t.TempDir()

	var buf strings.Builder
	restore := setLicenseSkipOutput(&buf)
	defer restore()

	pkgs := make([]string, 0, maxLicenseSkipReports+8)
	for i := 0; i < maxLicenseSkipReports+8; i++ {
		name := fmt.Sprintf("dep%02d", i)
		mkfifoInTemp(t, dir, "node_modules", name, "package.json")
		pkgs = append(pkgs, name)
	}

	mustReturn(t, "DetectNPMLicenses over many FIFOs", func() {
		_ = DetectNPMLicenses(dir, pkgs)
	})

	got := buf.String()
	skips := strings.Count(got, "not read because reading one can block forever")
	if skips != maxLicenseSkipReports {
		t.Errorf("reported %d skips, want the cap of %d", skips, maxLicenseSkipReports)
	}
	if !strings.Contains(got, "further license-file skips suppressed") {
		t.Errorf("hitting the cap was not announced; got %q", got)
	}
}
