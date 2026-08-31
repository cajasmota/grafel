//go:build unix

package agents

// fifo_6478_test.go — deadline pin for upsertFile's AGENTS.md / CLAUDE.md /
// GEMINI.md read (docs/blocking-open-audit.md, "rules/agent files" family).
//
// upsertFile takes the path as a bare PARAMETER, so the #6478 AST sweep cannot
// see it: the sweep resolves identifiers, not calls. This test is the only
// thing that pins the site, which is exactly why it exists — the guard covers
// the boundary, not the sites, and pretending otherwise is how the class was
// declared closed four times.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

func TestUpsertFileFIFODoesNotHang(t *testing.T) {
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			p := testsupport.MkfifoInTemp(t, dir, name)

			var err error
			testsupport.MustReturn(t, "upsertFile with a FIFO named "+name, func() {
				err = upsertFile(p, "block\n")
			})
			// upsertFile's existing contract is "any read failure that is not
			// ENOENT is fatal". A FIFO is not ENOENT, so it must surface —
			// silently rewriting a named pipe as a regular file would be worse
			// than the hang.
			if err == nil {
				t.Fatalf("upsertFile returned a nil error for a FIFO named %s", name)
			}
		})
	}
}

// TestUpsertFileStillWritesARegularFile is the positive control.
func TestUpsertFileStillWritesARegularFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(p, []byte("user prose\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := upsertFile(p, MapStartMarker+"\nx\n"+MapEndMarker); err != nil {
		t.Fatalf("upsertFile on a regular file: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(b), "user prose") {
		t.Fatalf("upsertFile dropped user content outside the markers: %q", string(b))
	}
	if !strings.Contains(string(b), MapStartMarker) {
		t.Fatalf("upsertFile did not write the block; the guard is refusing regular files: %q", string(b))
	}
}
