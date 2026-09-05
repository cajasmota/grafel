package links

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// source_read_guard_6823_test.go — #6823, the portable half.
//
// Deliberately NOT build-tagged. Nothing in this file needs a FIFO: the guard
// is static text scanning and the bound tests are ordinary file I/O, so they
// run on the Windows leg too. Only the tests that call mkfifo live behind
// //go:build !windows, in safe_source_read_6823_test.go.

// TestNoUnhardenedSourcePathReadsInPackage is the anti-divergence guard.
//
// WHAT IT CATCHES: any non-test file in this package that opens a path built
// from a source root with os.ReadFile, os.Open or os.OpenFile. Matching is
// case-INSENSITIVE and covers both the local names the eleven sites used
// (`abs`, `srcRoot`) and the struct field they all ultimately read from
// (`g.FileRoot`). The case-sensitive first cut of this guard missed
// `os.ReadFile(filepath.Join(g.FileRoot, file))` — skipping the `srcRoot :=`
// local is the more natural way to write the next copy, and it walked
// straight past. `os.OpenFile` was missed too, because "os.Open(" does not
// substring-match "os.OpenFile(".
//
// WHAT IT DOES NOT CATCH: a read whose path expression names none of those
// three tokens (a helper that takes an already-joined string, say), and
// divergence from the ENGINE twin's own posture beyond the byte bound —
// TestSourceReadBoundMatchesTheEngineTwin covers only the bound. Collapsing
// the two resolvers is the only thing that closes the rest (#6450).
//
// WHY IT COUNTS WHAT IT SCANNED. A walk that reaches no files finds no
// offenders and is indistinguishable from a clean package: point os.ReadDir
// at an empty directory, or break the .go filter, and this reports success
// having read nothing. That is the exact failure 52aaa84f1 fixed in the
// secrets scanner ("reported a repo it never read as clean"), and the one
// internal/types guards with scanGoEntityKinds' FilesParsed floor. A guard
// is the thing that is supposed to notice, so a vacuous one is worse than
// no guard at all — it occupies the slot and reports PASS. The floor is
// loose (26 non-test files today) because its job is to catch a walk that
// collapsed, not to track the file count.
func TestNoUnhardenedSourcePathReadsInPackage(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var files []guardFile
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		src, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, guardFile{name: n, body: string(src)})
	}

	// ONE walk-integrity check with three failure modes, because they are one
	// question — "is the input this guard is about to scan real?" — and three
	// separate Fatalf sites read as three unrelated rules.
	//
	//  1. Did the walk read ANYTHING? minScannedFiles is a floor, not a count:
	//     below it the walk is broken and an empty offender list means
	//     nothing. Deliberately far under the 26 non-test files present today
	//     so ordinary churn never trips it.
	//  2. Did it read the RIGHT files? The floor alone does not say so — this
	//     package has 35 test files against 26 non-test ones, so a filter
	//     inverted to read only tests clears a count floor comfortably while
	//     never opening a single production file.
	//  3. Did it read their actual CONTENT, all of it? Names and bodies are
	//     collected separately, so `body: ""` or `body: n` would pass both
	//     checks above while handing the matcher nothing to find — and a
	//     TRUNCATING walk (`body[:4000]`) would pass a token check too, since
	//     both tokens below sit near the top of their files while the real
	//     read in constant_propagation.go is at byte 12961. Presence is a
	//     weaker property than completeness, so the body's length is compared
	//     to the file's actual size on disk: exact, unsatisfiable by any
	//     prefix, and dependent on no string surviving a refactor.
	//
	//     All three modes interrogate `files` — the exact slice handed to the
	//     matcher — and NOT a parallel map built alongside it: a first cut
	//     checked a second copy of the bodies, which left both `body: ""` and
	//     `body: n` alive because the thing verified was not the thing used.
	//
	//     The tokens are kept as a cheaper, more legible second opinion.
	//     `package links` is the package clause, present in every file here
	//     and moved by no refactoring short of renaming the package;
	//     `readSourceFile` is the helper this guard exists to push callers
	//     towards, so a rename that broke it would already require rewriting
	//     the message below.
	const minScannedFiles = 15
	mustScan := []struct {
		file   string
		tokens []string
	}{
		{"constant_propagation.go", []string{"package links"}},
		{"string_pass.go", []string{"package links"}},
		{"safe_source_read.go", []string{"package links", "readSourceFile"}},
	}
	broken := ""
	if len(files) < minScannedFiles {
		broken = fmt.Sprintf("it scanned only %d non-test .go files, want at least %d", len(files), minScannedFiles)
	} else {
		for _, m := range mustScan {
			body, ok := "", false
			for _, f := range files {
				if f.name == m.file {
					body, ok = f.body, true
					break
				}
			}
			if !ok {
				broken = fmt.Sprintf("%s was never scanned — the walk read something, but not the production sources this guard grades", m.file)
				break
			}
			fi, err := os.Stat(m.file)
			if err != nil {
				t.Fatalf("stat %s: %v", m.file, err)
			}
			if int64(len(body)) != fi.Size() {
				broken = fmt.Sprintf("the walk collected %d bytes for %s but the file is %d — it read a PREFIX, so every read past the cut is invisible to the matcher", len(body), m.file, fi.Size())
				break
			}
			for _, tok := range m.tokens {
				if !strings.Contains(body, tok) {
					broken = fmt.Sprintf("%s was scanned but the body collected for it does not contain %q — the walk passed the right file NAME with the wrong content", m.file, tok)
					break
				}
			}
			if broken != "" {
				break
			}
		}
	}
	if broken != "" {
		t.Fatalf("THE GUARD'S WALK IS BROKEN, not the package: %s (walked %q, %d files). An empty offender list from a walk that did not read this package's sources is not evidence of a clean package (#6823; cf. 52aaa84f1).",
			broken, mustGetwd(t), len(files))
	}

	checkNoUnhardenedReads(t, files)
}

// guardFile is one file for scanUnhardenedReads to examine. It exists so the
// scan can be pointed at a synthetic input as well as at the real package
// directory, WITHOUT there being two copies of the scan.
type guardFile struct {
	name string
	body string
}

// scanUnhardenedReads is the guard's entire matcher AND its entire loop — the
// walk above contributes nothing but the file list, and holds no matching
// logic of its own to drift out of step. That split is the point: extracting
// only the predicate and unit-testing it would leave the loop free to ignore
// it, a defect class this repo has already shipped. Mutate anything in here —
// the call tokens, the root tokens, the comment skip, the lowering — and BOTH
// the real scan and TestScanUnhardenedReads_DetectsAndDiscriminates change
// together, because they are the same code.
//
// Each offending line is reported once, as "<file>:<line>: <trimmed line>".
func scanUnhardenedReads(files []guardFile) []string {
	calls := []string{"os.ReadFile(", "os.OpenFile(", "os.Open("}
	roots := []string{"abs", "srcroot", "fileroot"}
	var offenders []string
	for _, f := range files {
		for i, line := range strings.Split(f.body, "\n") {
			// Comment lines are not reads. safe_source_read.go and this file
			// both quote the offending pattern verbatim.
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			// Every call token is examined, not just the first that matches.
			// Stopping at the first left `os.Open(abs); os.ReadFile(k)`
			// unflagged: os.ReadFile is checked first, so the remainder began
			// after it and `abs` was never in view. Two opens on one line is
			// not idiomatic, but it is legal Go, and this guard's whole value
			// is being the thing that does not need the next copy to be
			// idiomatic. flagged keeps one offender per line.
			flagged := false
			for _, call := range calls {
				idx := strings.Index(line, call)
				if idx < 0 || flagged {
					continue
				}
				arg := strings.ToLower(line[idx+len(call):])
				for _, root := range roots {
					if strings.Contains(arg, root) && !flagged {
						offenders = append(offenders, fmt.Sprintf("%s:%d: %s", f.name, i+1, strings.TrimSpace(line)))
						flagged = true
					}
				}
			}
		}
	}
	return offenders
}

// checkNoUnhardenedReads is the guard's REACTION, split out for the same
// reason the matcher was: so something can observe it. A guard has three
// moving parts and each was ungraded in turn — the walk (does it read
// anything, and the right things), the matcher (does it detect and
// discriminate), and this: does the guard actually ACT on what the matcher
// returns. Wrapping the decision in `false &&` at the call site made the
// package green with the matcher fully intact, which is the same defect class
// as extracting a predicate and leaving the call site free to ignore it.
//
// It takes testing.TB rather than *testing.T so the control below can pass a
// recorder and observe both outcomes. Nothing follows the Fatalf, so the
// function is correct whether or not Fatalf is terminal for the TB it was
// handed.
func checkNoUnhardenedReads(tb testing.TB, files []guardFile) {
	tb.Helper()
	offenders := scanUnhardenedReads(files)
	if len(offenders) > 0 {
		tb.Fatalf("source-path reads must go through readSourceFile (safeio), not os.ReadFile/os.Open/os.OpenFile — a FIFO there parks a group-link worker forever (#6416/#6823):\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// recordingTB is a testing.TB that records a failure instead of aborting.
//
// testing.TB embeds a private method, so it is embedded here to satisfy the
// interface; only Helper and Fatalf are ever called, and any other method
// would panic loudly rather than silently pass. Fatalf deliberately does NOT
// call runtime.Goexit — recording and returning is what lets one test observe
// both the failing and the passing outcome on its own goroutine.
type recordingTB struct {
	testing.TB
	failed bool
	msg    string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.failed = true
	r.msg = fmt.Sprintf(format, args...)
}

// TestCheckNoUnhardenedReads_ActsOnWhatTheMatcherReturns is the positive
// control for the guard's REACTION — layer four.
//
// Layers one and two grade the walk, layer three grades the matcher. None of
// them observes that the guard does anything with the offenders it is handed:
// `if offenders := scanUnhardenedReads(files); false && len(offenders) > 0`
// left the whole package green. This asserts both directions — a dirty file
// set must fail, a clean one must not — so neither discarding the matcher's
// output nor failing unconditionally survives.
func TestCheckNoUnhardenedReads_ActsOnWhatTheMatcherReturns(t *testing.T) {
	dirty := []guardFile{{
		name: "dirty.go",
		body: "package p\n\nfunc f() {\n\tb, err := os.ReadFile(abs)\n}\n",
	}}
	var onDirty recordingTB
	checkNoUnhardenedReads(&onDirty, dirty)
	if !onDirty.failed {
		t.Fatal("the guard did not fail on a file set containing an unhardened source-path read: it is not acting on what the matcher returns, so the whole guard is inert regardless of how well the matcher works")
	}
	if !strings.Contains(onDirty.msg, "dirty.go:4") {
		t.Fatalf("failure message does not name the offending line; got %q", onDirty.msg)
	}

	clean := []guardFile{{
		name: "clean.go",
		body: "package p\n\nfunc f() {\n\tb, err := readSourceFile(abs, maxSourceFileBytes)\n}\n",
	}}
	var onClean recordingTB
	checkNoUnhardenedReads(&onClean, clean)
	if onClean.failed {
		t.Fatalf("the guard failed on a clean file set: %s", onClean.msg)
	}
}

// TestScanUnhardenedReads_DetectsAndDiscriminates is the positive control for
// the guard's MATCHER — the third of three layers:
//
//  1. did the walk read anything?      -> the floor above
//  2. did it read the RIGHT things?    -> the must-scan anchors above
//  3. can the matcher DETECT anything? -> this test
//
// Layers 1 and 2 both grade the walk. Without this one, neutering `roots` or
// `calls` turns the guard into a very thorough no-op: it scans all 26 files,
// clears every anchor, finds nothing, and reports success — the exact failure
// mode the guard exists to prevent, reachable through the one part of it that
// nothing checked.
//
// It grades BOTH directions. The offending lines must be flagged (recall) and
// the safe ones must not be (over-firing). The expected set is exact, line
// numbers included, which is what makes the second half real: a test asserting
// only "some offenders" would survive a matcher that flags every line.
func TestScanUnhardenedReads_DetectsAndDiscriminates(t *testing.T) {
	const body = `package p

func f() {
	b, err := os.ReadFile(abs)
	// os.ReadFile(abs) — a quoted pattern in a comment is not a read
	c, err := os.OpenFile(g.FileRoot, os.O_RDONLY, 0)
	d, err := os.Open(filepath.Join(srcRoot, rel))
	e, err := os.ReadFile(path)
	h, err := readSourceFile(abs, maxSourceFileBytes)
	os.Open(abs); os.ReadFile(k)
}
`
	got := scanUnhardenedReads([]guardFile{{name: "synthetic.go", body: body}})
	want := []string{
		// line 4: the plain shape
		"synthetic.go:4: b, err := os.ReadFile(abs)",
		// line 6: os.OpenFile, and FileRoot in its declared casing — this
		// entry is what keeps the ToLower and the OpenFile token graded.
		"synthetic.go:6: c, err := os.OpenFile(g.FileRoot, os.O_RDONLY, 0)",
		// line 7: os.Open with the joined srcRoot
		"synthetic.go:7: d, err := os.Open(filepath.Join(srcRoot, rel))",
		// line 10: two opens on one line, the case the `break` used to miss
		"synthetic.go:10: os.Open(abs); os.ReadFile(k)",
	}
	// Not flagged, and each for a different reason: line 5 is a comment that
	// quotes an offending pattern verbatim, line 8 is a real os.ReadFile whose
	// argument names no source root, line 9 is the hardened helper this whole
	// guard exists to push callers towards.
	if len(got) != len(want) {
		t.Fatalf("scanUnhardenedReads flagged %d lines, want %d.\n got: %v\nwant: %v\nAn empty result means the matcher detects nothing and the guard is a no-op; a longer one means it fires on safe lines.", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("offender %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestSourceReadBoundMatchesTheEngineTwin pins the bound's VALUE, which is
// the whole stated rationale for choosing it: 1 MiB is not "a bound", it is
// the ENGINE's bound, adopted so the two resolvers do not acquire a second
// axis of divergence on top of the one #6823 exists to close.
//
// It reads the engine file as text rather than importing it, because
// substrateMaxFileBytes is unexported. That makes this the one check here
// that can see across the twin boundary at all: if either side changes its
// bound, this fails and the change has to be made deliberately on both.
//
// It does NOT verify that the two constants are used the same way — the
// engine's read is lazy and capped at substrateMaxFileReads per run, this
// package's is eager. Only the byte bound is compared.
func TestSourceReadBoundMatchesTheEngineTwin(t *testing.T) {
	if maxSourceFileBytes != 1<<20 {
		t.Fatalf("maxSourceFileBytes = %d, want 1<<20 — it is deliberately the engine twin's value (#6823)", maxSourceFileBytes)
	}
	const engineFile = "../engine/http_endpoint_substrate_fold.go"
	src, err := os.ReadFile(engineFile)
	if err != nil {
		t.Fatalf("read %s: %v", engineFile, err)
	}
	const want = "const substrateMaxFileBytes int64 = 1 << 20"
	if !strings.Contains(string(src), want) {
		t.Fatalf("%s no longer declares %q.\nThe engine twin's byte bound changed and this package's maxSourceFileBytes did not. #6823 was filed because these two diverged once already: change both deliberately, or record why they now differ.", engineFile, want)
	}
}

// TestScanFile_DiscardsAFileOverFourMiB pins the string pass's bound against
// its own discard threshold — the pair that actually interacts, and the pair
// the first round's masking analysis missed.
//
// stringScanMaxFileBytes is 4 MiB + 1 precisely so an over-length file still
// arrives over-length and is still discarded by `len(body) > 4*1024*1024`.
// Set the bound one byte lower and a 4 MiB + 1 file is truncated to exactly
// 4 MiB, fails the discard, and is SCANNED — a permissive change in the
// direction the code comment claims is impossible. Delete the discard and
// the same file is scanned. This test fails on either.
func TestScanFile_DiscardsAFileOverFourMiB(t *testing.T) {
	const marker = "orders.created.v1"
	dir := t.TempDir()

	// Positive control first: the literal must be extractable at all, or the
	// negative half below would pass for the wrong reason.
	small := filepath.Join(dir, "small.go")
	if err := os.WriteFile(small, []byte("package p\n\nconst Q = \""+marker+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !extracts(t, small, "small.go", marker) {
		t.Fatal("control: the marker literal is not extractable even from a small file; the fixture is wrong")
	}

	// Exactly one byte past the discard threshold, with the literal at the
	// front so a truncating read would still find it.
	head := "package p\n\nconst Q = \"" + marker + "\"\n"
	pad := strings.Repeat("// pad\n", 1024)
	var b strings.Builder
	b.WriteString(head)
	for b.Len() < 4*1024*1024+1 {
		b.WriteString(pad)
	}
	big := filepath.Join(dir, "big.go")
	if err := os.WriteFile(big, []byte(b.String()[:4*1024*1024+1]), 0o600); err != nil {
		t.Fatal(err)
	}
	if extracts(t, big, "big.go", marker) {
		t.Fatalf("a %d-byte file was scanned; files over 4 MiB must be discarded, which requires the read bound to sit ABOVE the discard threshold, not on it", 4*1024*1024+1)
	}
}

func extracts(t *testing.T, abs, rel, marker string) bool {
	t.Helper()
	exs, err := scanFile(abs, rel, "")
	if err != nil {
		t.Fatalf("scanFile(%s): %v", rel, err)
	}
	for _, e := range exs {
		if strings.Contains(e.Value, marker) {
			return true
		}
	}
	return false
}

// mustGetwd names the directory the guard actually walked, so a broken-walk
// failure says WHERE it looked rather than only that it found nothing.
func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		return "<unknown>"
	}
	return wd
}
