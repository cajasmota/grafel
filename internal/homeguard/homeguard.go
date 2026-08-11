// Package homeguard is the fail-closed guard against a test that forgot to
// isolate $HOME writing into the developer's REAL home directory.
//
// # The incident
//
// A dashboard wizard test isolated GRAFEL_HOME but not HOME, resolved the
// developer's real ~/.cursor/mcp.json, and registered the ephemeral
// `dashboard.test` binary path into it — every `go test` run silently rewrote
// the live editor MCP config. internal/install/mcpreg grew a guard in its own
// write path in response (#6240), and internal/registry grew a near-identical
// one for #5443, where a test clobbered the live fleet config and took a group
// to zero entities.
//
// # Why it lives in the write path and not in a TestMain
//
// The opt-in TestMain guard (testsupport.GuardRealHomeMain) only fires when a
// package wires it up AND GRAFEL_TEST_REQUIRE_ISOLATED_HOME=1 is exported. It is
// trivially skipped by a package that never wires it in — which is how both
// incidents happened. A check inside the writers themselves cannot be skipped.
//
// It is inert outside tests (testing.Testing() is false in the shipping binary)
// and inert inside tests that DID isolate, because the write target then lands
// under a TempDir rather than the real home.
//
// # Why this package exists (#6246)
//
// #6246 converted internal/dashboard's writeMCPConfig to FOLLOW symlinks. That
// is the correct fix for the user, and it opens a door for tests: pre-#6246 a
// link planted inside a sandbox was flattened in place, so a badly-isolated test
// stopped at the sandbox boundary; now the write goes THROUGH it. internal/
// dashboard had no analogue of mcpreg's guard, and its config paths are
// $HOME-derived. That is one careless test away from the original incident.
//
// A third hand-rolled copy was the wrong answer, so the logic is here and the
// packages that need it keep a four-line file naming their own destinations. The
// alternative — putting it in internal/testsupport — is not available: that is a
// test-helper package, and this has to be callable from production write paths.
//
// # What is deliberately NOT unified here
//
// internal/registry's copy additionally EXEMPTS os.TempDir(), because on Windows
// t.TempDir() lives below %USERPROFILE%\AppData\Local\Temp and a raw "under the
// real home" check rejects correctly-isolated tests there. mcpreg's copy has no
// such exemption, and this package reproduces mcpreg's semantics EXACTLY —
// adding the exemption would be a behaviour change to mcpreg on Windows, and
// mcpreg's untouched #6240 suite is the regression gate for the change that
// created this package. registry is therefore left on its own copy for now.
// Unifying the third caller means deciding the temp-root question on purpose,
// with mcpreg's Windows behaviour as the thing to argue about; it is a separate
// change from moving code.
//
// # What #6288 changed, and why it does not disturb the above
//
// Escapes had two fail-OPEN defects on Windows (asymmetric absolutisation, and a
// byte-exact comparison on a case-insensitive filesystem); both are fixed on the
// function itself. The obvious worry is that a guard which now matches MORE
// starts panicking the correctly-isolated Windows tests in mcpreg and dashboard,
// since this package has no temp-root exemption.
//
// On the observed CI runner it does not, and the reason is worth writing down.
// There, t.TempDir() lives under %USERPROFILE%\AppData\Local\Temp and is
// returned in 8.3 SHORT form (C:\Users\RUNNER~1\...), while RealUserHome comes
// from os.UserHomeDir() in LONG form (C:\Users\runneradmin). Those differ by
// alias, not by case, so case-folding does not bring them together — and
// resolving the alias deliberately is NOT done here, precisely because it would.
// See the recorded miss on TestContains_KnownMiss_ShortNames: closing it
// requires deciding the temp-root question above first.
//
// State that as CONTINGENT, not as a property (#6290). RealUserHome is
// %USERPROFILE% VERBATIM — an environment variable, not a canonicalised path. A
// machine whose %USERPROFILE% is itself carried in short form makes every
// t.TempDir() prefix-match it, and this guard then panics every correctly-
// isolated mcpreg and dashboard test on that box. That hazard predates #6288 and
// is not introduced here, but it is a spelling coincidence holding it off, not a
// guarantee, and the temp-root exemption is what would actually close it.
package homeguard

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// realUserHome captures the home the same way internal/testsupport and
// internal/registry capture it. Kept independent of testsupport on purpose:
// testsupport is a test-helper package and must not be imported from production
// write paths, which is exactly where this guard has to sit.
func realUserHome() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return filepath.Clean(h)
	}
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Clean(h)
	}
	return ""
}

// RealUserHome is the developer's home directory, captured at package-init time
// — BEFORE any test has had a chance to call t.Setenv("HOME", ...). It is the
// home a test must never write into.
//
// Empty when it could not be determined, in which case the guard is inert (there
// is nothing to compare against, and guessing would produce false panics).
var RealUserHome = realUserHome()

// pathsAreCaseInsensitive is the host's rule for comparing two path strings.
//
// #6288: the comparison below used to be byte-exact on every platform, which is
// wrong wherever the filesystem is not. On Windows "C:\Users\dev" and
// "c:\users\dev" name the same directory — os.UserHomeDir returns %USERPROFILE%
// verbatim while a resolved write target may have reached the caller through
// any number of other spellings — so a byte-exact test answers "does not escape"
// for a write straight into the real home. Darwin's default volume (APFS,
// case-insensitive) has the same property.
//
// This errs toward fail-CLOSED on purpose: on a case-SENSITIVE darwin volume the
// guard may now refuse a write to a path that merely looks like the home, which
// costs a spurious panic in a test. The other direction costs a developer's
// ~/.claude.json. That is not a symmetric trade.
//
// The fold itself is foldForCompare, which maps UP to match NTFS's $UpCase; see
// its doc for why folding down was measurably wrong in both directions (#6290).
var pathsAreCaseInsensitive = runtime.GOOS == "windows" || runtime.GOOS == "darwin"

// extendedLengthPrefix is Windows' \\?\ escape, which turns off path parsing and
// raises MAX_PATH. filepath.Clean deliberately preserves it (Go 1.20+), so
// "\\?\C:\Users\dev" and "C:\Users\dev" survive normalisation as different
// strings while naming the same directory.
const extendedLengthPrefix = `\\?\`

// stripExtendedLengthPrefix removes a \\?\<drive>: prefix so the drive form
// compares equal to its plain spelling.
//
// The \\?\UNC\server\share form is deliberately left alone: rewriting it to
// \\server\share is a second transformation with its own edge cases, and no
// path in this tree is produced in that form. It is a recorded miss, pinned by
// a row in TestStripExtendedLengthPrefix, not an oversight.
func stripExtendedLengthPrefix(p string) string {
	if !strings.HasPrefix(p, extendedLengthPrefix) {
		return p
	}
	rest := p[len(extendedLengthPrefix):]
	// Only the drive form: exactly "X:" followed by a separator or end.
	if len(rest) >= 2 && rest[1] == ':' &&
		((rest[0] >= 'a' && rest[0] <= 'z') || (rest[0] >= 'A' && rest[0] <= 'Z')) {
		return rest
	}
	return p
}

// foldForCompare maps a path to the form two case-insensitively-equal paths
// share.
//
// It folds UP, not down. NTFS compares names through $UpCase — a simple
// uppercase mapping — and strings.ToLower disagrees with that in BOTH
// directions. Measured (TestFoldForCompare_MatchesNTFS):
//
//   - "dırk" (U+0131 dotless i) vs "DIRK": ToLower-unequal, NTFS-EQUAL. Folding
//     down therefore reports "a different directory" for two names Windows
//     treats as one — in this guard that is FAIL-OPEN, the exact defect class
//     the folding was added to close.
//   - "İrk" (U+0130) vs "irk", and U+212A KELVIN SIGN vs "K": ToLower-equal,
//     NTFS-different, so folding down over-matches.
//
// strings.ToUpper agrees with NTFS on all three. It is still an approximation of
// $UpCase (which is a fixed table, not Unicode's current mapping) — but it is an
// approximation that errs the same way NTFS does on the cases that exist here,
// rather than one that errs open.
func foldForCompare(s string) string { return strings.ToUpper(s) }

// contains reports whether abs is home or lies below it. Both arguments must
// ALREADY be absolute and cleaned; sep and fold are the platform's separator and
// case rule.
//
// Taking the platform rules as arguments rather than reading them from the host
// is the whole point: #6288's failures were Windows-only and no reviewer here
// can run Windows, so the Windows decision is asserted from macOS and Linux by
// passing the Windows rule explicitly (windows_shapes_6288_test.go).
//
// #6290: a home that is a filesystem or drive ROOT ("/", "C:\") already ends in
// a separator. Appending another produced "//" or "C:\\", which no real path has
// as a prefix, so NOTHING under the root matched and the guard answered
// "allowed" for every path on the volume. That was a fail-open shipped by the
// commit fixing a fail-open. The separator is appended only when it is not
// already there.
func contains(abs, home string, sep byte, fold bool) bool {
	if fold {
		abs = foldForCompare(abs)
		home = foldForCompare(home)
	}
	if abs == home {
		return true
	}
	if home == "" {
		return false
	}
	if home[len(home)-1] == sep {
		// A root home. Its trailing separator is part of the path, not padding.
		return strings.HasPrefix(abs, home)
	}
	return strings.HasPrefix(abs, home+string(sep))
}

// normalizeForCompare puts a path into the coordinate system contains works in:
// absolute, cleaned, and free of Windows' \\?\ escape.
func normalizeForCompare(p string) string {
	p = stripExtendedLengthPrefix(p)
	if a, err := filepath.Abs(p); err == nil {
		p = a
	}
	return filepath.Clean(p)
}

// Escapes reports whether resolved lands inside home. Split out of Guard so both
// answers are testable without needing a process whose real home is arranged for
// the occasion.
//
// The comparison is on the path STRING after normalisation; it does not itself
// resolve symlinks. Callers must pass an ALREADY-RESOLVED path — see Guard.
//
// #6288: BOTH arguments are normalised. The old code ran filepath.Abs on
// resolved only, so on Windows a volumeless rooted home ("\home\dev") had the
// current volume stamped onto one side of the comparison and not the other, and
// Escapes("\home\dev", "\home\dev") returned FALSE — the home did not contain
// itself, and the guard allowed a write into it. An asymmetric normalisation
// compares two values that are not in the same coordinate system; no caller can
// be expected to know that only one of its arguments gets absolutised.
func Escapes(resolved, home string) bool {
	if home == "" || resolved == "" {
		return false
	}
	return contains(
		normalizeForCompare(resolved),
		normalizeForCompare(home),
		filepath.Separator,
		pathsAreCaseInsensitive,
	)
}

// Guard panics if, while running under `go test`, the path a caller is about to
// WRITE lands inside the REAL user home.
//
// component names the calling package ("mcpreg", "dashboard") and what describes
// the file ("MCP host config"); remediation is appended verbatim so each caller
// can name the files it would have clobbered and the isolation helper to call.
// The message always contains "TEST SANDBOX ESCAPE".
//
// Pass a RESOLVED path. Once a package follows symlinks, a link inside a
// properly-isolated t.TempDir() pointing at the real $HOME makes the NAMED path
// look safe while the operation is anything but — the guard decides by looking
// at a string, so it must be handed the string the write will actually use.
//
// Guard once per OPERATION that touches a resolved path, not once per exported
// entry point. #6240 found that collapsing several operations onto one shared
// check left the suite green when either of two overlapping checks was deleted:
// a guard nothing can independently kill is a guard nobody can trust.
func Guard(component, what, resolved, remediation string) {
	if !testing.Testing() {
		return
	}
	if !Escapes(resolved, RealUserHome) {
		return
	}
	abs := resolved
	if a, err := filepath.Abs(resolved); err == nil {
		abs = filepath.Clean(a)
	}
	panic(fmt.Sprintf(
		"%s: TEST SANDBOX ESCAPE — about to WRITE %s to %q, which is inside the "+
			"REAL user home %q. %s",
		component, what, abs, RealUserHome, remediation,
	))
}
