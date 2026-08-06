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
func TestSystemdUnit_SystemdAnalyzeVerify(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd-analyze is Linux-only")
	}
	bin, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze not on PATH")
	}
	for name, u := range map[string]Unit{"plain": sample, "hostile": percentHostile} {
		path := filepath.Join(t.TempDir(), name+".service")
		if err := os.WriteFile(path, []byte(SystemdUnit(u)), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(bin, "verify", "--no-pager", path).CombinedOutput(); err != nil {
			t.Errorf("systemd-analyze verify rejected the %s unit: %v\n%s\n%s", name, err, out, SystemdUnit(u))
		}
	}
}
