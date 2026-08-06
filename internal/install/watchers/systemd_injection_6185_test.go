package watchers

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// #6185 F3 (found on review): iniEsc only escapes '%'. A newline in a
// repo/group/bin-path field is not something any systemd assignment line can
// escape — WorkingDirectory=, Description=, etc. take the rest of the
// physical line literally, so an embedded '\n' always starts a NEW line that
// systemd's unit parser reads as a new directive. Example: a repo path of
// "/tmp/r\nExecStartPost=/bin/sh" makes ExecStartPost run at login, injected
// into the [Service] section beneath WorkingDirectory. The same vector
// reaches Description= via the group name, and — because #6186's
// validateGroupName only blocked path separators and "."/".." — a group
// named "g\n[Service]\nExecStart=evil" passed registry validation entirely.
//
// There is no per-value escape for this (unlike '%'), so the fix is to
// refuse to render/persist a unit whose fields contain control characters,
// at the one place that actually writes a file: Write().

// hostileControl unions the two concrete injection vectors above: an
// ExecStartPost injected via the repo path (through WorkingDirectory), and a
// bogus directive injected via the group name (through Description).
var hostileControl = Unit{
	Group:   "g\n[Service]\nExecStart=evil",
	Repo:    "/tmp/r\nExecStartPost=/bin/sh",
	BinPath: "/usr/local/bin/grafel",
}

// TestWrite_RejectsControlCharacters pins that Write — the sole place a unit
// is ever persisted (internal/install/install.go, internal/install/
// watchersync.go both go through it) — refuses to write a file for any of
// the three fields when it contains a control character, instead of
// silently persisting an injected unit.
func TestWrite_RejectsControlCharacters(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("GRAFEL_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))

	cases := []Unit{
		{Group: "g\nbad", Repo: "/tmp/ok", BinPath: "/usr/local/bin/grafel"},
		{Group: "ok", Repo: "/tmp/r\nExecStartPost=/bin/sh", BinPath: "/usr/local/bin/grafel"},
		{Group: "ok", Repo: "/tmp/ok", BinPath: "/usr/local/bin/graf\rel"},
		{Group: "ok", Repo: "/tmp/ok\x00", BinPath: "/usr/local/bin/grafel"},
		hostileControl,
	}
	for i, u := range cases {
		if path, err := Write(u); err == nil {
			os.Remove(path)
			t.Errorf("case %d: Write(%+v) succeeded, want rejection of the control character "+
				"(#6185 F3); wrote %s", i, u, path)
		}
	}
}

// TestWrite_StillAcceptsOrdinaryUnits pins that the fix does not overreach:
// ordinary group/repo/bin-path values (including ones with spaces, which are
// common in real directory names) must still write successfully.
func TestWrite_StillAcceptsOrdinaryUnits(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("GRAFEL_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))

	u := Unit{Group: "demo", Repo: filepath.Join(dir, "My Repo"), BinPath: "/usr/local/bin/grafel"}
	path, err := Write(u)
	if err != nil {
		t.Fatalf("Write(%+v) failed, want success: %v", u, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("unit file not written: %v", err)
	}
}

// ── minimal systemd unit parser, used as a real assertion oracle ───────────
//
// #6185 F4 (found on review): the original test suite was six
// strings.Contains checks pinning only the one character ('%') that was
// fixed; the ExecStartPost-injection unit above contains no raw '%' and
// passed all six unchanged. This parser is a real (if minimal) INI/unit-file
// reader: it walks the actual line structure systemd would see, so an
// injected line becomes an extra (section,key) pair or a malformed line
// rather than a substring that a Contains check can miss. It also makes
// unbalanced ExecStart quoting a hard parse error, mirroring systemd's own
// word extractor.
//
// It is intentionally strict: any line that is not a "[Section]" header, a
// "key=value" assignment, or blank is rejected, and a repeated key within a
// section is rejected as a directive collision. This is stricter than real
// systemd where useful, but a false rejection of a well-formed unit fails
// TestSystemdUnit_ParsesCleanly loudly rather than silently under-detecting.
type parsedUnit struct {
	order    []string // section names, in file order
	sections map[string]map[string]string
}

func parseSystemdUnit(body string) (*parsedUnit, error) {
	p := &parsedUnit{sections: map[string]map[string]string{}}
	cur := ""
	for i, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return nil, fmt.Errorf("line %d: malformed section header %q", i+1, line)
			}
			name := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			if _, ok := p.sections[name]; ok {
				return nil, fmt.Errorf("line %d: section %q appears more than once (possible "+
					"injected duplicate section)", i+1, name)
			}
			cur = name
			p.sections[cur] = map[string]string{}
			p.order = append(p.order, cur)
			continue
		}
		if cur == "" {
			return nil, fmt.Errorf("line %d: assignment %q outside any section", i+1, line)
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: not a \"key=value\" assignment: %q "+
				"(possible injected directive or unescaped newline)", i+1, line)
		}
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", i+1)
		}
		if _, dup := p.sections[cur][key]; dup {
			return nil, fmt.Errorf("line %d: key %q set more than once in [%s] "+
				"(possible injected duplicate directive)", i+1, key, cur)
		}
		if err := checkBalancedQuotes(val); err != nil {
			return nil, fmt.Errorf("line %d: %s=%s: %w", i+1, key, val, err)
		}
		p.sections[cur][key] = val
	}
	return p, nil
}

// checkBalancedQuotes rejects a value with an odd number of unescaped double
// quotes — systemd's ExecStart= word extractor treats '"' as a shell-style
// quote delimiter, and an odd count means a quote that never closes.
func checkBalancedQuotes(val string) error {
	depth := 0
	esc := false
	for _, r := range val {
		if esc {
			esc = false
			continue
		}
		switch r {
		case '\\':
			esc = true
		case '"':
			depth ^= 1
		}
	}
	if depth != 0 {
		return fmt.Errorf("unbalanced double quote")
	}
	return nil
}

// systemdExpectedKeys is the allow-list of keys this package ever emits, per
// section. Anything else showing up is an injected directive.
var systemdExpectedKeys = map[string][]string{
	"Unit":    {"Description", "After", "StartLimitIntervalSec", "StartLimitBurst"},
	"Service": {"Type", "ExecStart", "WorkingDirectory", "Restart", "RestartSec"},
	"Install": {"WantedBy"},
}

func (p *parsedUnit) assertNoInjection(t *testing.T) {
	t.Helper()
	for section, keys := range p.sections {
		allowed, ok := systemdExpectedKeys[section]
		if !ok {
			t.Errorf("unexpected section [%s] (possible injected section)", section)
			continue
		}
		for k := range keys {
			if !contains(allowed, k) {
				t.Errorf("unexpected key %q in [%s] (possible injected directive): %v", k, section, keys)
			}
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// TestSystemdUnit_ParsesCleanly is the sanity check for the oracle itself:
// legitimate output from a legitimate Unit must parse with no errors and no
// unexpected keys.
func TestSystemdUnit_ParsesCleanly(t *testing.T) {
	body := SystemdUnit(sample)
	p, err := parseSystemdUnit(body)
	if err != nil {
		t.Fatalf("parser rejected a legitimate unit: %v\n%s", err, body)
	}
	p.assertNoInjection(t)
	got := append([]string{}, p.order...)
	sort.Strings(got)
	want := []string{"Install", "Service", "Unit"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("sections = %v, want %v", got, want)
	}
}

// TestSystemdUnit_OracleDetectsTheInjection proves the oracle is a real
// detector, not a rubber stamp: fed the RAW render of the hostile unit
// (bypassing Write's guard, exactly as the unescaped code before this fix
// would have persisted it), it must either fail to parse or surface an
// injected key/section — not silently accept it the way six
// strings.Contains checks did.
func TestSystemdUnit_OracleDetectsTheInjection(t *testing.T) {
	body := SystemdUnit(hostileControl)
	p, err := parseSystemdUnit(body)
	if err != nil {
		// A parse failure IS detection: systemd's own line-oriented parser
		// would choke on this too.
		return
	}
	if svc, ok := p.sections["Service"]; ok {
		if _, injected := svc["ExecStartPost"]; injected {
			return // detected: the injected directive parsed as its own key
		}
	}
	t.Errorf("oracle failed to detect the ExecStartPost injection in a raw (unescaped) render:\n%s", body)
}
