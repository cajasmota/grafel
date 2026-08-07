package watchers

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// percentHostile is a Unit whose group and repo path contain '%', the
// systemd specifier-escape character. systemd requires '%%' for a literal
// percent; a raw '%' yields a broken ExecStart that systemd silently
// rejects, so the watcher never runs and nothing explains why (#6185).
//
// This is a different escape rule from the XML fix in #6179: xmlEsc is wrong
// for INI and '%%' is wrong for XML, so this is deliberately its own check,
// not a shared "escape for output format" helper.
var percentHostile = Unit{
	Group:   "R%D",
	Repo:    "/tmp/100%done",
	BinPath: "/usr/local/bin/graf%el",
}

// TestSystemdUnit_EscapesPercent pins #6185. Before the fix every
// interpolated field (Group, Repo, BinPath) went into the INI body raw.
func TestSystemdUnit_EscapesPercent(t *testing.T) {
	body := SystemdUnit(percentHostile)

	if strings.Contains(body, "R%D") {
		t.Errorf("group's raw '%%' survived unescaped:\n%s", body)
	}
	if strings.Contains(body, "100%done") {
		t.Errorf("repo path's raw '%%' survived unescaped:\n%s", body)
	}
	if strings.Contains(body, "graf%el") {
		t.Errorf("bin path's raw '%%' survived unescaped:\n%s", body)
	}
	if !strings.Contains(body, "R%%D") {
		t.Errorf("expected the group's '%%' escaped to '%%%%':\n%s", body)
	}
	if !strings.Contains(body, "100%%done") {
		t.Errorf("expected the repo path's '%%' escaped to '%%%%':\n%s", body)
	}
	if !strings.Contains(body, "graf%%el") {
		t.Errorf("expected the bin path's '%%' escaped to '%%%%':\n%s", body)
	}
}

// TestSystemdUnit_SystemdAnalyzeVerify runs systemd-analyze verify over a
// generated unit where available — the systemd-side equivalent of the
// plutil -lint test that pins the macOS half of #6179.
//
// The units are built over paths this test CREATES rather than over the
// package's `sample` / `percentHostile` fixtures. systemd-analyze does not stop
// at syntax: it stats ExecStart and fails the whole run with "Command
// /usr/local/bin/grafel is not executable: No such file or directory" on any
// machine where that fixture path does not happen to exist — which is every CI
// runner, and is why this test had never passed on Linux. Verifying against a
// real binary keeps the assertion (verify must ACCEPT the unit) intact and adds
// one: because systemd resolves `%%` back to a literal `%`, verify finding the
// file proves the #6185 escaping round-trips through systemd itself, not just
// through strings.Contains.
func TestSystemdUnit_SystemdAnalyzeVerify(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd-analyze is Linux-only")
	}
	bin, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze not on PATH")
	}

	// realUnit returns a Unit whose BinPath is an executable that exists and
	// whose Repo is a directory that exists, both named as caller asks so the
	// hostile case still carries '%' through the generated INI.
	realUnit := func(t *testing.T, group, repoName, binName string) Unit {
		t.Helper()
		dir := t.TempDir()
		repo := filepath.Join(dir, repoName)
		if err := os.MkdirAll(repo, 0o755); err != nil {
			t.Fatal(err)
		}
		binPath := filepath.Join(dir, binName)
		if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return Unit{Group: group, Repo: repo, BinPath: binPath}
	}

	units := map[string]Unit{
		"plain":   realUnit(t, "demo", "core", "grafel"),
		"hostile": realUnit(t, "R%D", "100%done", "graf%el"),
	}
	for name, u := range units {
		body := SystemdUnit(u)
		path := filepath.Join(t.TempDir(), name+".service")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(bin, "verify", "--no-pager", path).CombinedOutput(); err != nil {
			t.Errorf("systemd-analyze verify rejected the %s unit: %v\n%s\n%s", name, err, out, body)
		}
	}
}
