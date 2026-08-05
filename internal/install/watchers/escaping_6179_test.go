package watchers

import (
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// hostile is a Unit whose group and repo path contain the three characters that
// are illegal as raw XML character data. Group names and repo paths are both
// user-supplied, so this is reachable — a group named `R&D` is enough.
var hostile = Unit{
	Group:   "R&D",
	Repo:    "/tmp/re<po>&more",
	BinPath: "/usr/local/bin/graf&el",
}

// TestLaunchdPlist_EscapesXML pins #6179 F5. Before the fix the body contained
// a raw `&`, which makes the whole plist unparseable: launchd rejects the job
// silently, so the watcher never runs and nothing explains why.
func TestLaunchdPlist_EscapesXML(t *testing.T) {
	body := LaunchdPlist(hostile)
	assertWellFormedXML(t, body)
	if strings.Contains(body, "re<po>") {
		t.Errorf("repo path was not escaped:\n%s", body)
	}
	if !strings.Contains(body, "&amp;") {
		t.Errorf("expected an escaped ampersand in the body:\n%s", body)
	}
	// And the escaping must be escaping, not deletion — the real path has to
	// survive a round-trip so launchd actually watches the right directory.
	if got := roundTripStrings(t, body); !containsString(got, hostile.Repo) {
		t.Errorf("repo path did not survive escaping; decoded <string> values: %q", got)
	}
	if !containsString(roundTripStrings(t, body), hostile.BinPath) {
		t.Errorf("bin path did not survive escaping")
	}
}

// TestSchtasksXML_EscapesXML pins the identical defect in the Windows task
// definition, which is built by the same string-concatenation shape.
func TestSchtasksXML_EscapesXML(t *testing.T) {
	body := SchtasksXML(hostile)
	// The task definition declares encoding="UTF-16", which Go's decoder
	// refuses without a CharsetReader. Drop the declaration — we are checking
	// escaping and nesting, not the encoding label schtasks wants.
	if i := strings.Index(body, "?>"); i >= 0 {
		assertWellFormedXML(t, body[i+2:])
	} else {
		t.Fatalf("no XML declaration in schtasks body:\n%s", body)
	}
	if strings.Contains(body, "R&D") {
		t.Errorf("group was not escaped:\n%s", body)
	}
}

// TestLaunchdPlist_PlutilLint runs Apple's own parser over a generated plist.
// This is the only check in the suite that uses a real system tool; it writes
// to a temp dir and never touches ~/Library/LaunchAgents or launchctl.
func TestLaunchdPlist_PlutilLint(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("plutil is macOS-only")
	}
	plutil, err := exec.LookPath("plutil")
	if err != nil {
		t.Skip("plutil not on PATH")
	}
	for name, u := range map[string]Unit{"plain": sample, "hostile": hostile} {
		path := filepath.Join(t.TempDir(), name+".plist")
		if err := os.WriteFile(path, []byte(LaunchdPlist(u)), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(plutil, "-lint", path).CombinedOutput(); err != nil {
			t.Errorf("plutil -lint rejected the %s plist: %v\n%s\n%s",
				name, err, out, LaunchdPlist(u))
		}
	}
}

// TestSystemdUnit_RateLimitedAndGivesUp pins #6179 F4.
//
// Restart=on-failure already carried the exit-status half of the fix, but
// RestartSec=5 across 140 units is ~28 restarts/second — worse than macOS was.
// RestartSec must move to the same floor as ThrottleIntervalSeconds, and
// because five 60s-spaced restarts can never trip systemd's default 10s
// StartLimit window, the window has to be widened explicitly or Linux loses its
// only give-up.
func TestSystemdUnit_RateLimitedAndGivesUp(t *testing.T) {
	body := SystemdUnit(sample)
	if strings.Contains(body, "RestartSec=5\n") {
		t.Fatalf("RestartSec=5 is the unfixed value\n%s", body)
	}
	if RestartSecSeconds != ThrottleIntervalSeconds {
		t.Errorf("RestartSecSeconds = %d, want it to track ThrottleIntervalSeconds = %d",
			RestartSecSeconds, ThrottleIntervalSeconds)
	}
	for _, want := range []string{
		"Restart=on-failure",
		"RestartSec=60",
		"StartLimitIntervalSec=3600",
		"StartLimitBurst=5",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("systemd unit missing %q\n%s", want, body)
		}
	}
	// The give-up must actually be reachable: burst restarts spaced RestartSec
	// apart have to fit inside the limit window, or the unit restarts forever.
	if RestartSecSeconds*StartLimitBurst >= StartLimitIntervalSeconds {
		t.Errorf("StartLimit window (%ds) is too short for %d restarts %ds apart — "+
			"systemd would never fail the unit, so there is no give-up",
			StartLimitIntervalSeconds, StartLimitBurst, RestartSecSeconds)
	}
}

func assertWellFormedXML(t *testing.T, body string) {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(body))
	dec.Strict = true
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("not well-formed XML: %v\n%s", err, body)
		}
	}
}

// roundTripStrings decodes every <string> element's character data.
func roundTripStrings(t *testing.T, body string) []string {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(body))
	var out []string
	in := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			in = el.Name.Local == "string"
		case xml.CharData:
			if in {
				out = append(out, string(el))
			}
		case xml.EndElement:
			in = false
		}
	}
}

func containsString(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
