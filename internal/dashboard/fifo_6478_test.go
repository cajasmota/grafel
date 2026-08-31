//go:build unix

package dashboard

// fifo_6478_test.go — deadline pin for the two dashboard reads
// docs/blocking-open-audit.md lists as name-chosen.
//
// readManifestGroup is the worst of the 26: #6478's own grounding comment says
// "internal/dashboard/handlers_onboard.go:299 takes a user-supplied path, which
// is the worst of the 26 and the one to fix first". A caller hands the onboard
// handler a directory and grafel reads .grafel/group.json under it, so a FIFO
// there parked an HTTP handler goroutine for the life of the daemon.
//
// parseSkillMeta's sibling in handlers_repo_manifest.go reads AGENTS.md /
// CLAUDE.md / GEMINI.md behind an os.Stat that never checked regularity — the
// TOCTOU shape the issue names verbatim.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

func TestReadManifestGroupFIFODoesNotHang(t *testing.T) {
	dir := t.TempDir()
	p := testsupport.MkfifoInTemp(t, dir, ".grafel", "group.json")

	var got string
	testsupport.MustReturn(t, "readManifestGroup with a FIFO at .grafel/group.json", func() {
		got = readManifestGroup(p)
	})
	// This reader's contract is "" on any failure, and that is preserved
	// deliberately — safeio returns the error UNCHANGED and the skip is
	// reported to stderr by safeio.ReportSkip rather than by changing a
	// signature the HTTP layer depends on.
	if got != "" {
		t.Fatalf("readManifestGroup returned %q for a FIFO, want \"\"", got)
	}
}

// TestReadManifestGroupStillReadsARegularFile is the positive control: a reader
// that refused everything would pass the test above.
func TestReadManifestGroupStillReadsARegularFile(t *testing.T) {
	dir := t.TempDir()
	p := writeGroupManifest(t, dir, `{"group":"acme"}`)
	if got := readManifestGroup(p); got != "acme" {
		t.Fatalf("readManifestGroup = %q, want \"acme\"; the guard is refusing regular files", got)
	}
}

func writeGroupManifest(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, ".grafel", "group.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}
