// installwindows_6163_test.go covers the two Windows installers (#6163).
//
// ── HONESTY NOTE — read this before trusting anything below ──────────────────
//
// These are STATIC assertions over the text of install.bat and install.ps1.
// Neither script has been executed, here or anywhere: this repository has no
// Windows host and no cmd.exe/pwsh in CI, and the same is true of the machine
// the fix was written on. What follows can prove that the dangerous command is
// gone and the narrow ones are present and correctly ordered. It CANNOT prove
// that either script runs — batch quoting, PowerShell parameter binding and the
// behaviour of `& $exe` with arguments are unverified by this file.
//
// That is the same limitation installscript_version_test.go already operates
// under (it models install.bat's string transforms rather than running them),
// and it is stated here rather than left implicit because an unstated "known
// limit" is what let a darwin-only build failure sit on main for two weeks.
//
// ── Why these tests parse rather than grep ──────────────────────────────────
//
// The first version of this file scanned raw script text with strings.Contains
// and keyed the dangerous-command guard on the literal `grafel.exe" install` —
// closing quote included. Review found it worthless in both directions:
//
//	B1: every install.bat assertion passed on a script whose real command lines
//	    had been replaced by `REM` prose containing the same words. The tests
//	    could not tell a comment from code, and since the script is never
//	    executed anywhere they were the ONLY evidence the remedy was present.
//	B2: the guard was defeated by re-quoting. `%BINDIR%\grafel.exe install`
//	    (unquoted — the most natural alternative spelling of the exact bug) and
//	    `set "G=…\grafel.exe"` + `"%G%" install` both passed.
//
// So the scripts are now reduced to a line model that strips comments and
// blanks quoted spans, and `install` is matched as a TOKEN on those lines
// rather than as a quoting-specific prefix. The model itself is tested against
// synthetic fixtures below (TestScriptLineModel_*), because a guard whose
// parser is wrong is indistinguishable from no guard.
//
// ── The two defects ─────────────────────────────────────────────────────────
//
// install.bat ran bare `grafel.exe install` — the full seven-step RunCopy
// transaction — from whatever directory the console happened to be in. That
// appends /.grafel/ to that directory's .gitignore and writes four git hooks
// there (a repository the user never named, #6162), rewrites every detected
// .claude.json with a step-4 daemon restart on a 60s budget behind it, and,
// because a .bat is run interactively from a console, hits stdinIsTTY() in
// resolveToolSelection and BLOCKS on the tool-selection wizard mid-install.
//
// install.ps1 had the opposite defect: it wrote no install.json at all. It
// copies the new .exe over the old one and runs `doctor`; RunQuickDoctor
// compares state.CLI.SHA256 against the binary at state.CLI.Path, so after
// every upgrade the recorded SHA is the previous binary's and every grafel
// command permanently prints "binary updated since last install". Its restart
// hint compounded that by prescribing `grafel install` — the command the other
// half of this issue is about.
package install

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Script line model
// ─────────────────────────────────────────────────────────────────────────────

// scriptLang selects the comment syntax.
type scriptLang int

const (
	langBat scriptLang = iota
	langPs1
)

// scriptLine is one source line reduced to its executable content.
type scriptLine struct {
	// N is the 1-based line number, for error messages that a human can act on.
	N int
	// Raw is the original text.
	Raw string
	// Code is Raw with comments removed and every quoted span blanked to
	// spaces. Blanking rather than deleting keeps token boundaries intact, so
	// `"%BINDIR%\grafel.exe" install` still yields the token `install` while
	// `"…grafel install…"` inside a string yields nothing.
	Code string
}

// Fields returns the whitespace-separated tokens of the executable content.
func (l scriptLine) Fields() []string { return strings.Fields(l.Code) }

// blankQuoted replaces the contents of every quoted span — and the quote
// characters themselves — with spaces.
//
// Both cmd.exe and PowerShell treat "…" as a quoted span; PowerShell also has
// '…' and a backtick escape. Over-blanking is the safe direction for these
// tests: it can only hide a token from the guard if that token is inside
// quotes, which for `install` means it is prose, not a command.
func blankQuoted(s string, lang scriptLang) string {
	out := []rune(s)
	var quote rune
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		if lang == langPs1 && c == '`' && i+1 < len(runes) {
			// Backtick escapes the next character; neither can open or close a
			// span. Blank both so an escaped quote cannot flip the state.
			out[i] = ' '
			out[i+1] = ' '
			i++
			continue
		}
		switch {
		case quote != 0:
			closing := c == quote
			out[i] = ' '
			if closing {
				quote = 0
			}
		case c == '"' || (lang == langPs1 && c == '\''):
			quote = c
			out[i] = ' '
		}
	}
	return string(out)
}

// outputVerbs are commands whose remaining arguments are text printed to the
// user, not a command to run. cmd.exe's `echo` does not quote its argument, so
// without this `echo install failed.` and `if exist … echo   upgrading existing
// install at …` both read as invocations of `install`.
var outputVerbs = map[string]bool{
	"echo": true, "@echo": true,
	"write-info": true, "write-host": true, "write-output": true,
	"write-error": true, "write-warning": true,
}

// segmentSeparators split one source line into independently-executed commands.
// Truncating a whole line at `echo` would otherwise hide a real invocation
// chained after it (`echo hi & grafel.exe install`), so the truncation is
// applied per segment rather than per line.
func segmentSeparators(lang scriptLang) []string {
	if lang == langPs1 {
		return []string{";", "|"}
	}
	return []string{"&&", "||", "&", "|"}
}

// truncateAtOutputVerb drops everything from the first output verb onwards.
func truncateAtOutputVerb(code string) string {
	offset := 0
	rest := code
	for {
		trimmedLen := len(rest) - len(strings.TrimLeft(rest, " \t"))
		rest = rest[trimmedLen:]
		offset += trimmedLen
		if rest == "" {
			return code
		}
		tokLen := len(rest)
		if i := strings.IndexAny(rest, " \t"); i >= 0 {
			tokLen = i
		}
		if outputVerbs[strings.ToLower(rest[:tokLen])] {
			return code[:offset]
		}
		rest = rest[tokLen:]
		offset += tokLen
	}
}

// scriptLines reduces a script to its executable command segments. One source
// line can yield several segments; each keeps the source line number so a
// failure message points at something a human can open.
func scriptLines(src string, lang scriptLang) []scriptLine {
	var out []scriptLine
	for i, raw := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(raw)
		code := trimmed

		if lang == langBat {
			upper := strings.ToUpper(trimmed)
			// `REM comment`, a bare `REM`, and the `::` label-comment form.
			if upper == "REM" || strings.HasPrefix(upper, "REM ") || strings.HasPrefix(trimmed, "::") {
				code = ""
			}
		}

		code = blankQuoted(code, lang)

		if lang == langPs1 {
			// A '#' that survived quote-blanking starts a real comment.
			if idx := strings.Index(code, "#"); idx >= 0 {
				code = code[:idx]
			}
		}

		// Split into independently-executed segments, then drop each segment's
		// printed text.
		seps := segmentSeparators(lang)
		parts := []string{code}
		for _, sep := range seps {
			var next []string
			for _, p := range parts {
				next = append(next, strings.Split(p, sep)...)
			}
			parts = next
		}
		for _, p := range parts {
			out = append(out, scriptLine{N: i + 1, Raw: trimmed, Code: truncateAtOutputVerb(p)})
		}
	}
	return out
}

// installInvocations returns every executable line carrying `install` as a
// standalone token, paired with the flag that follows it ("" when there is
// none). This is the set of lines that actually run `grafel install`, however
// the binary happens to be spelled or quoted.
func installInvocations(lines []scriptLine) []scriptLine {
	var out []scriptLine
	for _, l := range lines {
		for _, f := range l.Fields() {
			if f == "install" {
				out = append(out, l)
				break
			}
		}
	}
	return out
}

// installFlag returns the token immediately after `install` on a line, or "".
func installFlag(l scriptLine) string {
	fields := l.Fields()
	for i, f := range fields {
		if f != "install" {
			continue
		}
		if i+1 < len(fields) {
			return fields[i+1]
		}
		return ""
	}
	return ""
}

// bareInstallInvocations returns the lines that run `grafel install` with no
// flag after it — i.e. the full RunCopy transaction.
func bareInstallInvocations(lines []scriptLine) []scriptLine {
	var out []scriptLine
	for _, l := range installInvocations(lines) {
		if flag := installFlag(l); flag == "" || !strings.HasPrefix(flag, "--") {
			out = append(out, l)
		}
	}
	return out
}

// installFlagsUsed returns the set of flags passed to `install` across a script.
func installFlagsUsed(lines []scriptLine) map[string]scriptLine {
	out := map[string]scriptLine{}
	for _, l := range installInvocations(lines) {
		if flag := installFlag(l); strings.HasPrefix(flag, "--") {
			out[flag] = l
		}
	}
	return out
}

// repoRootFile reads a file at the repository root.
func repoRootFile(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve test file path")
	}
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(raw)
}

func batLines(t *testing.T) []scriptLine {
	t.Helper()
	return scriptLines(repoRootFile(t, "install.bat"), langBat)
}

func ps1Lines(t *testing.T) []scriptLine {
	t.Helper()
	return scriptLines(repoRootFile(t, "install.ps1"), langPs1)
}

// firstLineWith returns the line number of the first executable line whose Code
// contains sub, or -1.
func firstLineWith(lines []scriptLine, sub string) int {
	for _, l := range lines {
		if strings.Contains(l.Code, sub) {
			return l.N
		}
	}
	return -1
}

// ─────────────────────────────────────────────────────────────────────────────
// Guard the guard: the line model must actually distinguish code from prose.
// ─────────────────────────────────────────────────────────────────────────────

// TestScriptLineModel_IgnoresComments is the direct answer to review finding
// B1. Replacing install.bat's two real command lines with `REM` prose
// containing the same words left every assertion in the previous version of
// this file green.
func TestScriptLineModel_IgnoresComments(t *testing.T) {
	const bat = `@echo off
REM "%BINDIR%\grafel.exe" install --refresh-state
:: "%BINDIR%\grafel.exe" install --register-mcp
rem %BINDIR%\grafel.exe install
echo Done.
`
	lines := scriptLines(bat, langBat)
	if got := installInvocations(lines); len(got) != 0 {
		t.Errorf("commented-out commands were collected as invocations: %+v", got)
	}
	if got := bareInstallInvocations(lines); len(got) != 0 {
		t.Errorf("a commented-out bare install was flagged: %+v", got)
	}

	const ps1 = `# & $existing install --refresh-state
Write-Info "run 'grafel install' if this fails"   # & $existing install
$msg = "grafel install"
Write-Host grafel.exe install
`
	pl := scriptLines(ps1, langPs1)
	if got := installInvocations(pl); len(got) != 0 {
		t.Errorf("ps1 comments/strings were collected as invocations: %+v", got)
	}
}

// TestScriptLineModel_CatchesEveryBareSpelling is review finding B2. Each of
// these is a real, natural way to reintroduce the bug that the old
// literal-prefix guard let through.
func TestScriptLineModel_CatchesEveryBareSpelling(t *testing.T) {
	batCases := map[string]string{
		"quoted":            `"%BINDIR%\grafel.exe" install`,
		"unquoted":          `%BINDIR%\grafel.exe install`,
		"via variable":      `"%G%" install`,
		"call prefix":       `call "%BINDIR%\grafel.exe" install`,
		"trailing redirect": `"%BINDIR%\grafel.exe" install >nul`,
		// Chained after printed text: truncating the whole line at `echo`
		// would hide this, which is why the truncation is per segment.
		"chained after echo": `echo installing… & "%BINDIR%\grafel.exe" install`,
	}
	for name, line := range batCases {
		t.Run("bat/"+name, func(t *testing.T) {
			lines := scriptLines(line+"\n", langBat)
			if got := bareInstallInvocations(lines); len(got) != 1 {
				t.Errorf("bare install not detected in %q (got %d)", line, len(got))
			}
		})
	}

	ps1Cases := map[string]string{
		"variable":  `& $existing install`,
		"quoted":    `& "$BinDir\grafel.exe" install`,
		"bare path": `& $BinDir\grafel.exe install`,
		"amp-less":  `$existing install`,
	}
	for name, line := range ps1Cases {
		t.Run("ps1/"+name, func(t *testing.T) {
			lines := scriptLines(line+"\n", langPs1)
			if got := bareInstallInvocations(lines); len(got) != 1 {
				t.Errorf("bare install not detected in %q (got %d)", line, len(got))
			}
		})
	}
}

// TestScriptLineModel_AcceptsFlaggedInvocations: the guard must not fire on the
// narrow commands, or it would be unsatisfiable and get deleted.
func TestScriptLineModel_AcceptsFlaggedInvocations(t *testing.T) {
	for _, line := range []string{
		`"%BINDIR%\grafel.exe" install --refresh-state`,
		`"%BINDIR%\grafel.exe" install --register-mcp`,
	} {
		lines := scriptLines(line+"\n", langBat)
		if got := bareInstallInvocations(lines); len(got) != 0 {
			t.Errorf("flagged invocation %q was wrongly reported as bare", line)
		}
		if len(installInvocations(lines)) != 1 {
			t.Errorf("flagged invocation %q was not collected at all", line)
		}
	}
	for _, line := range []string{
		`try { & $existing install --refresh-state } catch { }`,
		`try { & $existing install --register-mcp } catch { }`,
	} {
		lines := scriptLines(line+"\n", langPs1)
		if got := bareInstallInvocations(lines); len(got) != 0 {
			t.Errorf("flagged invocation %q was wrongly reported as bare", line)
		}
		if len(installInvocations(lines)) != 1 {
			t.Errorf("flagged invocation %q was not collected at all", line)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// install.bat — the dangerous half. It writes to a repository the user never
// named and can hang, so it goes first.
// ─────────────────────────────────────────────────────────────────────────────

// TestInstallBat_NeverRunsTheFullInstallTransaction is the #6163 headline.
//
// A bare `grafel install` from a console is the case RunCopy's IntentInstall is
// built FOR — the user standing in the repo they mean. install.bat is the case
// it is not: the console's cwd is wherever the user launched from, and #6162's
// intent gate does not rescue that, because install.bat invokes IntentInstall's
// own entrypoint.
func TestInstallBat_NeverRunsTheFullInstallTransaction(t *testing.T) {
	for _, l := range bareInstallInvocations(batLines(t)) {
		t.Errorf("install.bat:%d runs the full RunCopy transaction (gitignore + git hooks "+
			"in the console's cwd, daemon restart, and a TTY tool-selection wizard that "+
			"blocks mid-install): %q", l.N, l.Raw)
	}
}

// TestInstallBat_RunsTheNarrowSteps: removing the dangerous command is only
// half the fix — install.bat still has to leave a machine on which grafel
// works, i.e. install.json recorded and the MCP server registered.
//
// This reads the INVOCATION set, not the raw text: review finding B1 was that
// the previous version passed on a script whose commands were all comments.
func TestInstallBat_RunsTheNarrowSteps(t *testing.T) {
	flags := installFlagsUsed(batLines(t))
	for _, want := range []string{"--refresh-state", "--register-mcp"} {
		if _, ok := flags[want]; !ok {
			t.Errorf("install.bat never RUNS `grafel install %s` (found: %v)", want, flagNames(flags))
		}
	}
}

// TestInstallBat_RefreshStatePrecedesTheVersionReport: --refresh-state exists to
// stop the stale-checksum warning being printed by the very installer that
// caused it, which only works if it runs first.
func TestInstallBat_RefreshStatePrecedesTheVersionReport(t *testing.T) {
	lines := batLines(t)
	flags := installFlagsUsed(lines)
	refresh, ok := flags["--refresh-state"]
	if !ok {
		t.Fatal("install.bat never runs `install --refresh-state`")
	}
	report := firstLineWith(lines, "--version")
	if report < 0 {
		t.Fatal("could not locate the version report in install.bat")
	}
	if refresh.N > report {
		t.Errorf("install.bat reports the version at line %d before recording the binary "+
			"it placed at line %d", report, refresh.N)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// install.ps1 — the stale-state half. Permanent but cosmetic; no data touched.
// ─────────────────────────────────────────────────────────────────────────────

// TestInstallPs1_RecordsTheBinaryItPlaced is the ps1 defect: Copy-Item then
// doctor, with nothing in between that writes install.json.
func TestInstallPs1_RecordsTheBinaryItPlaced(t *testing.T) {
	lines := ps1Lines(t)
	flags := installFlagsUsed(lines)
	refresh, ok := flags["--refresh-state"]
	if !ok {
		t.Fatal("install.ps1 never records the binary it just placed; every subsequent " +
			"grafel command will print `binary updated since last install` forever")
	}
	copyN := firstLineWith(lines, "Copy-Item -Path $binSrc.FullName")
	doctorN := firstLineWith(lines, "$existing doctor")
	if copyN < 0 || doctorN < 0 {
		t.Fatalf("could not locate the copy/doctor sequence (copy=%d doctor=%d)", copyN, doctorN)
	}
	if refresh.N < copyN {
		t.Errorf("install.ps1 records the binary at line %d before placing it at line %d", refresh.N, copyN)
	}
	if refresh.N > doctorN {
		t.Errorf("install.ps1 reports doctor output at line %d before recording the binary at "+
			"line %d, so it prints the stale-checksum warning it caused itself", doctorN, refresh.N)
	}
}

// TestInstallPs1_RegistersMCP: the ps1 installer has the #6169 gap too — it
// places a binary and registers nothing.
func TestInstallPs1_RegistersMCP(t *testing.T) {
	if _, ok := installFlagsUsed(ps1Lines(t))["--register-mcp"]; !ok {
		t.Errorf("install.ps1 never registers the MCP server, so a first-ever Windows " +
			"install leaves a binary no AI coding tool can see")
	}
}

// TestInstallPs1_NeverRunsTheFullInstallTransaction: the fix for the missing
// state must not be the dangerous command install.bat is losing.
func TestInstallPs1_NeverRunsTheFullInstallTransaction(t *testing.T) {
	for _, l := range bareInstallInvocations(ps1Lines(t)) {
		t.Errorf("install.ps1:%d runs the full RunCopy transaction: %q", l.N, l.Raw)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Shared: no installer may prescribe bare `grafel install` as a next step.
// ─────────────────────────────────────────────────────────────────────────────

// TestInstallers_NoHintPrescribesBareInstall.
//
// All three installers printed "re-run 'grafel install'" as their remedy for a
// daemon that did not restart. That advice is unsafe precisely where it is
// printed: the daemon is known to be unhealthy, so RunCopy's step-4 restart is
// the step most likely to fail, and its failure path rolls step 3 back —
// restoring MCP host configs from snapshots, which on a first-ever install
// means deleting the file and every foreign server in it (#6168). `grafel
// restart` is the safe equivalent and touches no config.
//
// This one DOES scan raw text on purpose: it is about prose shown to the user,
// so a match inside a string literal or an echo is exactly what it must catch.
func TestInstallers_NoHintPrescribesBareInstall(t *testing.T) {
	for _, name := range []string{"install.sh", "install.ps1", "install.bat"} {
		t.Run(name, func(t *testing.T) {
			src := repoRootFile(t, name)
			for _, form := range []string{"'grafel install'", `"grafel install"`} {
				if strings.Contains(src, form) {
					t.Errorf("%s prescribes bare %s; use 'grafel restart' for a daemon that "+
						"did not pick up the new binary", name, form)
				}
			}
		})
	}
}

func flagNames(m map[string]scriptLine) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
