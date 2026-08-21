package walk

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeEntry is an fs.DirEntry carrying an arbitrary type bit, so the predicate
// can be driven for a FIFO, a device, a socket and a door on EVERY platform —
// including Windows, where syscall.Mkfifo does not exist. Without this the
// Windows leg of #6416 would be an empty file and prove nothing.
type fakeEntry struct {
	name string
	mode fs.FileMode
}

func (f fakeEntry) Name() string               { return f.name }
func (f fakeEntry) IsDir() bool                { return f.mode.IsDir() }
func (f fakeEntry) Type() fs.FileMode          { return f.mode.Type() }
func (f fakeEntry) Info() (fs.FileInfo, error) { return nil, os.ErrInvalid }

// TestIrregularSkipRuleClassifiesEntryTypes pins the predicate itself on all
// platforms. The walker branched only on IsDir(), so every one of these types
// was treated as a readable file.
func TestIrregularSkipRuleClassifiesEntryTypes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     fs.FileMode
		wantSkip bool
		wantRule string
	}{
		{"regular file", 0, false, ""},
		{"named pipe", fs.ModeNamedPipe, true, "irregular:named-pipe"},
		{"character device", fs.ModeDevice | fs.ModeCharDevice, true, "irregular:device"},
		{"block device", fs.ModeDevice, true, "irregular:device"},
		{"unix socket", fs.ModeSocket, true, "irregular:socket"},
		{"door / other", fs.ModeIrregular, true, "irregular:other"},
		// A symlink to a directory reaches the file branch (WalkDir does not
		// follow it) and is skipped — but under a rule OUTSIDE the irregular:
		// namespace, so the always-on "reading one can block forever" warning
		// does not fire on a symlink farm. See TestIrregularSkipReport.
		{"directory behind a symlink", fs.ModeDir, true, "symlink-to-directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule, skip := irregularSkipRule(filepath.Join(t.TempDir(), "Hang.vb"), fakeEntry{name: "Hang.vb", mode: tc.mode})
			if skip != tc.wantSkip || rule != tc.wantRule {
				t.Errorf("irregularSkipRule = (%q, %v), want (%q, %v)", rule, skip, tc.wantRule, tc.wantSkip)
			}
		})
	}
}

// TestIrregularSkipRuleFollowsSymlinks pins the Lstat-vs-Stat decision.
//
// filepath.WalkDir does not follow symlinks, so d.Type() is Lstat-shaped and
// reports ModeSymlink for BOTH a link to a regular file and a link to a FIFO.
// Deciding on that alone means either dropping legitimate files or keeping the
// hang, so the predicate resolves symlinks with os.Stat — which follows the
// link and, unlike open(2), never blocks on a FIFO.
func TestIrregularSkipRuleFollowsSymlinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.vb")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.vb")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	if rule, skip := irregularSkipRule(link, fakeEntry{name: "link.vb", mode: fs.ModeSymlink}); skip {
		t.Errorf("symlink to a regular file must be indexed, got skip with rule %q", rule)
	}

	dangling := filepath.Join(root, "dangling.vb")
	if err := os.Symlink(filepath.Join(root, "nope.vb"), dangling); err != nil {
		// Same guard as the first Symlink above: on a platform where symlinks
		// are unavailable BOTH calls fail, and only one of them skipping made
		// the failure mode depend on which ran first (#6416 review).
		t.Skipf("symlinks unavailable here: %v", err)
	}
	if rule, skip := irregularSkipRule(dangling, fakeEntry{name: "dangling.vb", mode: fs.ModeSymlink}); !skip || rule != "irregular:unresolvable" {
		t.Errorf("dangling symlink = (%q, %v), want (\"irregular:unresolvable\", true)", rule, skip)
	}
}

// TestIrregularSkipRuleReturnsPromptly is the Windows-reachable half of the
// liveness claim: whatever the predicate does, it must not itself block. It is
// os.Stat and never os.Open for exactly that reason.
func TestIrregularSkipRuleReturnsPromptly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "Hang.vb")
	done := make(chan struct{})
	go func() {
		_, _ = irregularSkipRule(p, fakeEntry{name: "Hang.vb", mode: fs.ModeNamedPipe})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("irregularSkipRule blocked; it must never open the entry")
	}
}

// TestSkipEntryRuleNamespaceIsDistinct keeps the reported rule distinguishable
// from the other skip layers, so a reader can tell a hazard skip from a
// gitignore skip.
func TestSkipEntryRuleNamespaceIsDistinct(t *testing.T) {
	rule, skip := irregularSkipRule(filepath.Join(t.TempDir(), "x.vb"), fakeEntry{name: "x.vb", mode: fs.ModeNamedPipe})
	if !skip {
		t.Fatal("want skip")
	}
	for _, other := range []string{"hardcoded", "protected:", "dir-cap", "linguist-generated", ".gitignore", ".grafelignore"} {
		if strings.HasPrefix(rule, other) {
			t.Errorf("rule %q collides with the %q namespace", rule, other)
		}
	}
}

// TestIrregularSkipReport pins the always-on report: an irregular skip must be
// visible without --print-skipped, and must stay a single bounded line.
func TestIrregularSkipReport(t *testing.T) {
	if got := IrregularSkipReport(nil); got != "" {
		t.Errorf("no irregular skips must render nothing, got %q", got)
	}
	if got := IrregularSkipReport([]SkipEntry{{AbsPath: "/r/node_modules", Rule: "hardcoded"}}); got != "" {
		t.Errorf("routine dir skips must not be reported here, got %q", got)
	}
	got := IrregularSkipReport([]SkipEntry{
		{AbsPath: "/r/vendor", Rule: "hardcoded"},
		{AbsPath: "/r/a.vb", Rule: "irregular:named-pipe"},
		{AbsPath: "/r/b.vb", Rule: "irregular:device"},
		{AbsPath: "/r/c.vb", Rule: "irregular:socket"},
		{AbsPath: "/r/d.vb", Rule: "irregular:other"},
	})
	for _, want := range []string{"skipped 4 non-regular file(s)", "/r/a.vb (named-pipe)", "and 1 more", "#6416"} {
		if !strings.Contains(got, want) {
			t.Errorf("report %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "\n") {
		t.Errorf("report must be a single line, got %q", got)
	}
	if strings.Contains(got, "/r/vendor") {
		t.Errorf("report leaked a non-irregular skip: %q", got)
	}
}

// TestSymlinkedDirectoryIsSkippedButNotWarnedAbout pins the two halves of the
// MEDIUM-6 decision together: the entry is still skipped (it was never a
// readable file), and it is deliberately absent from the always-on warning,
// because "reading one can block forever" is false of a directory and symlink
// farms are common enough that a false alarm at that scale trains people to
// ignore the line.
func TestSymlinkedDirectoryIsSkippedButNotWarnedAbout(t *testing.T) {
	rule, skip := irregularSkipRule(t.TempDir(), fakeEntry{name: "pkg", mode: fs.ModeDir})
	if !skip {
		t.Fatal("a symlinked directory must not be handed to an extractor")
	}
	if strings.HasPrefix(rule, "irregular:") {
		t.Errorf("rule %q is in the irregular: namespace, so it would trip the hang warning", rule)
	}
	if got := IrregularSkipReport([]SkipEntry{{AbsPath: "/r/pkg", Rule: rule}}); got != "" {
		t.Errorf("symlinked directory produced an always-on hang warning: %q", got)
	}
}
