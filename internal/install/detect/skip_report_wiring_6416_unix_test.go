//go:build unix

package detect

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestDetectMonorepoReportsFifoManifest is the wiring half: a REAL refused
// manifest, through the real DetectMonorepo, reaching the real reporter — once
// per parser, since dropping readManifest from any ONE of the three would leave
// the other two green.
//
// The unit tests next door call reportManifestSkip directly and so cannot tell
// whether anything ever calls it. This test is what kills that mutant, and it
// reproduces the user-visible bug: before the fix DetectMonorepo returned
// KindNone for these trees and said nothing about the manifest it could not
// read.
func TestDetectMonorepoReportsFifoManifest(t *testing.T) {
	for _, manifest := range []string{"package.json", "lerna.json", "pnpm-workspace.yaml"} {
		t.Run(manifest, func(t *testing.T) {
			repo := t.TempDir()
			fifo := filepath.Join(repo, manifest)
			if err := syscall.Mkfifo(fifo, 0o644); err != nil {
				t.Skipf("cannot create a fifo here: %v", err)
			}
			// lerna/pnpm are only consulted when their marker exists;
			// package.json is read unconditionally. Give the others a reason
			// to be read. The extra package.json is a real file, so it must
			// NOT itself be reported.
			if manifest != "package.json" {
				if err := os.WriteFile(filepath.Join(repo, "package.json"), []byte(`{"workspaces":["pkg/*"]}`), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			var buf strings.Builder
			restore := setManifestSkipOutput(&buf)
			defer restore()

			if _, err := DetectMonorepo(repo); err != nil {
				t.Fatalf("DetectMonorepo: %v", err)
			}

			got := buf.String()
			if got == "" {
				t.Fatalf("a FIFO named %s was skipped and reported NOWHERE; "+
					"the repo was silently classified without it", manifest)
			}
			if !strings.Contains(got, fifo) {
				t.Errorf("skip report does not name the skipped manifest %q; got %q", fifo, got)
			}
			if !strings.Contains(got, "#6416") {
				t.Errorf("skip report does not cite the issue; got %q", got)
			}
		})
	}
}

// TestDetectMonorepoStaysSilentOnOrdinaryRepos is the noise guard. A repo with
// no package.json at all is the common case, and ENOENT there must produce
// nothing — otherwise every `grafel index` of a Go or Python repo prints a
// FIFO warning about a file that simply does not exist.
func TestDetectMonorepoStaysSilentOnOrdinaryRepos(t *testing.T) {
	for _, name := range []string{"no-manifest-at-all", "real-manifest"} {
		t.Run(name, func(t *testing.T) {
			repo := t.TempDir()
			if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if name == "real-manifest" {
				if err := os.WriteFile(filepath.Join(repo, "package.json"), []byte(`{"workspaces":["pkg/*"]}`), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			var buf strings.Builder
			restore := setManifestSkipOutput(&buf)
			defer restore()

			if _, err := DetectMonorepo(repo); err != nil {
				t.Fatalf("DetectMonorepo: %v", err)
			}
			if got := buf.String(); got != "" {
				t.Errorf("an ordinary repo produced skip output: %q", got)
			}
		})
	}
}
