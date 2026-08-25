package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #6652 — StartLine for a synthesised FastAPI mount must agree with Python's
// universal-newline line numbering. CPython terminates a line on `\n`, on
// `\r\n`, AND on a LONE `\r`; counting only `\n` reports a line number that is
// low by one for every lone CR that precedes the entity.
//
// These tests assert the OBSERVABLE — the emitted entity's StartLine — never an
// internal counter (#6613). The `wantLine` values below are what CPython
// reports, obtained independently of this package's arithmetic: each fixture is
// built so that the mount call sits on a line whose number can be read off the
// terminator sequence directly.

// pyLineOfMountForTest is the oracle: it re-derives the 1-based line of the
// substring `needle` in `src` under Python's universal-newline rule, by
// scanning terminators one at a time. It is deliberately written differently
// from the production helper so it cannot inherit the same mistake.
func pyLineOfMountForTest(t *testing.T, src, needle string) int {
	t.Helper()
	off := strings.Index(src, needle)
	if off < 0 {
		t.Fatalf("#6652: fixture bug — needle %q not present in source", needle)
	}
	line := 1
	for i := 0; i < off; i++ {
		switch src[i] {
		case '\n':
			line++
		case '\r':
			line++
			if i+1 < len(src) && src[i+1] == '\n' {
				i++ // \r\n is ONE terminator
			}
		}
	}
	return line
}

func mountStartLine(t *testing.T, src, rel string) int {
	t.Helper()
	got := fastapiMountPointSynthetics(src, rel)
	if len(got) != 1 {
		t.Fatalf("#6652: expected exactly 1 mount synthetic, got %d", len(got))
	}
	return got[0].StartLine
}

// TestIssue6652_LoneCR_MountStartLineMatchesPython is the reproduction. Three
// lone `\r` terminators precede the mount call, so Python counts them as three
// line breaks; counting only `\n` loses all three.
func TestIssue6652_LoneCR_MountStartLineMatchesPython(t *testing.T) {
	// NB: no `#` comment before a lone CR. The comment scanner's terminator is
	// a SEPARATE defect (#6648) — a `#` line ended by a bare \r currently masks
	// out the rest of the file, which would suppress the mount entirely and
	// stop this fixture from observing the line map at all.
	src := "from fastapi import FastAPI\r" +
		"app = FastAPI()\r" +
		"router = users.router\r" +
		"app.include_router(users.router, prefix=\"/api\")\n"

	want := pyLineOfMountForTest(t, src, "app.include_router")
	if want != 4 {
		t.Fatalf("#6652: oracle disagrees with the hand-computed fixture: got %d, want 4", want)
	}
	if got := mountStartLine(t, src, "app/main.py"); got != want {
		t.Errorf("#6652: lone-CR file: mount StartLine = %d, want %d "+
			"(Python's universal newlines terminate a line on a bare \\r; "+
			"counting only \\n under-reports by one per CR)", got, want)
	}
}

// TestIssue6652_MixedCRandLF_MountStartLineMatchesPython varies the shape: the
// CRs are interleaved with LFs rather than uniform, so a fix that special-cases
// "a file with no \n at all" does not pass.
func TestIssue6652_MixedCRandLF_MountStartLineMatchesPython(t *testing.T) {
	src := "from fastapi import FastAPI\n" +
		"app = FastAPI()\r" +
		"x = 1\n" +
		"y = 2\r" +
		"z = 3\n" +
		"app.include_router(users.router, prefix=\"/api\")\n"

	want := pyLineOfMountForTest(t, src, "app.include_router")
	if want != 6 {
		t.Fatalf("#6652: oracle disagrees with the hand-computed fixture: got %d, want 6", want)
	}
	if got := mountStartLine(t, src, "app/main.py"); got != want {
		t.Errorf("#6652: mixed CR/LF file: mount StartLine = %d, want %d", got, want)
	}
}

// TestIssue6652_CRLF_ControlUnchanged is the CONTROL. Well-formed CRLF is
// already correct under the `\n`-only count, so this observes nothing about the
// bug — its job is to fail if the fix double-counts `\r\n` as two lines.
func TestIssue6652_CRLF_ControlUnchanged(t *testing.T) {
	src := "from fastapi import FastAPI\r\n" +
		"app = FastAPI()\r\n" +
		"# a windows file\r\n" +
		"app.include_router(users.router, prefix=\"/api\")\r\n"

	want := pyLineOfMountForTest(t, src, "app.include_router")
	if want != 4 {
		t.Fatalf("#6652: oracle disagrees with the hand-computed fixture: got %d, want 4", want)
	}
	if got := mountStartLine(t, src, "app/main.py"); got != want {
		t.Errorf("#6652: CRLF control: mount StartLine = %d, want %d "+
			"(a \\r\\n pair is ONE terminator, not two)", got, want)
	}
}

// TestIssue6652_LFOnly_ControlUnchanged is the second control: the ordinary
// Unix file must keep the line it has always reported. A fix that counts `\r`
// INSTEAD of `\n` passes the CR tests and fails here.
func TestIssue6652_LFOnly_ControlUnchanged(t *testing.T) {
	src := "from fastapi import FastAPI\n" +
		"app = FastAPI()\n" +
		"# a unix file\n" +
		"app.include_router(users.router, prefix=\"/api\")\n"

	want := pyLineOfMountForTest(t, src, "app.include_router")
	if want != 4 {
		t.Fatalf("#6652: oracle disagrees with the hand-computed fixture: got %d, want 4", want)
	}
	if got := mountStartLine(t, src, "app/main.py"); got != want {
		t.Errorf("#6652: LF-only control: mount StartLine = %d, want %d", got, want)
	}
}

// TestIssue6652_CRAtOffsetBoundary pins the boundary case the naive
// prefix-arithmetic gets wrong: a `\r\n` whose `\r` is the last byte of the
// counted prefix. Counting `\r` and `\n` and subtracting `\r\n` occurrences
// found only WITHIN the prefix would count that split pair as a terminator that
// has not been crossed yet.
func TestIssue6652_CRAtOffsetBoundary(t *testing.T) {
	// The mount is preceded by CRLF pairs; the byte just before each candidate
	// offset is the '\n' of a pair, so no pair is split at the mount offset —
	// but the file also carries a lone CR, exercising both rules at once.
	src := "from fastapi import FastAPI\r\n" +
		"app = FastAPI()\r" +
		"# lone CR above, CRLF below\r\n" +
		"app.include_router(users.router, prefix=\"/api\")\r\n"

	want := pyLineOfMountForTest(t, src, "app.include_router")
	if want != 4 {
		t.Fatalf("#6652: oracle disagrees with the hand-computed fixture: got %d, want 4", want)
	}
	if got := mountStartLine(t, src, "app/main.py"); got != want {
		t.Errorf("#6652: mixed CRLF + lone CR: mount StartLine = %d, want %d", got, want)
	}
}

// TestIssue6652_PythonLineOfOffset_SplitCRLFPair pins the helper's split-pair
// guard directly. The entity-level tests above CANNOT reach it:
// fastapiIncludeRouterCallRe starts its match on `[A-Za-z_]`, so a mount offset
// is never the "\n" of a "\r\n" pair. The guard is nonetheless part of the
// helper's contract — an offset sitting ON a line terminator has not yet
// crossed it — and without this test the guard is an unpinned survivor.
func TestIssue6652_PythonLineOfOffset_SplitCRLFPair(t *testing.T) {
	const src = "a\r\nb\r\nc"
	cases := []struct {
		name string
		off  int
		want int
	}{
		{"start of file", 0, 1},
		{"on the first \\r (terminator not yet crossed)", 1, 1},
		{"on the first \\n (pair is ONE terminator, still line 1)", 2, 1},
		{"first byte of line 2", 3, 2},
		{"on the second \\r", 4, 2},
		{"on the second \\n", 5, 2},
		{"first byte of line 3", 6, 3},
		{"end of file", len(src), 3},
	}
	for _, tc := range cases {
		if got := pythonLineOfOffset(src, tc.off); got != tc.want {
			t.Errorf("#6652: pythonLineOfOffset(%q, %d) [%s] = %d, want %d",
				src, tc.off, tc.name, got, tc.want)
		}
	}
	// Out-of-range offsets clamp rather than panic.
	if got := pythonLineOfOffset(src, -5); got != 1 {
		t.Errorf("#6652: negative offset clamps to line 1, got %d", got)
	}
	if got := pythonLineOfOffset(src, len(src)+99); got != 3 {
		t.Errorf("#6652: past-EOF offset clamps to the last line, got %d", got)
	}
}

// TestIssue6652_LFThenCR_MountStartLineMatchesPython pins the ADJACENCY the
// original fixture set missed: a "\n" immediately followed by a "\r". Only
// "\r\n" is a single terminator — "\n\r" is TWO, the "\n" ending one line and
// the bare "\r" ending the (empty) next one. Arithmetic that subtracts "\n\r"
// occurrences alongside "\r\n" survives every other fixture here and reports
// two lines too few in this file.
//
// The shape is real: a CRLF file edited by a tool that writes bare LF, or line
// endings reversed by a bad transform.
func TestIssue6652_LFThenCR_MountStartLineMatchesPython(t *testing.T) {
	src := "from fastapi import FastAPI\n\r" +
		"app = FastAPI()\n\r" +
		"app.include_router(users.router, prefix=\"/api\")\n"

	want := pyLineOfMountForTest(t, src, "app.include_router")
	if want != 5 {
		t.Fatalf("#6652: oracle disagrees with the hand-computed fixture: got %d, want 5", want)
	}
	if got := mountStartLine(t, src, "app/main.py"); got != want {
		t.Errorf("#6652: \\n\\r-adjacency file: mount StartLine = %d, want %d "+
			"(\\n\\r is TWO terminators — the \\n ends a line and the bare \\r ends "+
			"the empty line after it; only \\r\\n collapses to one)", got, want)
	}
}

// TestIssue6652_PythonLineOfOffset_TerminatorAdjacency pins the adjacency cases
// at the helper level, where the arithmetic actually lives. The three shapes
// below are each distinct from the pairs already covered by
// TestIssue6652_PythonLineOfOffset_SplitCRLFPair, whose source ("a\r\nb\r\nc")
// never places two terminators next to each other.
func TestIssue6652_PythonLineOfOffset_TerminatorAdjacency(t *testing.T) {
	cases := []struct {
		name string
		src  string
		off  int
		want int
	}{
		// "\n\r" — TWO terminators. This is the survivor the rest of the suite
		// missed: a `- strings.Count(prefix, "\n\r")` term returns 3 here.
		{`"\n\r" adjacency, end of "a\n\rb\n\rc"`, "a\n\rb\n\rc", 7, 5},
		// Offset lands ON a LONE "\r" (content[3] is 'b', so this \r is not
		// part of a pair). This is the symmetric neighbour of the lone-"\n"
		// offsets pinned by TestIssue6652_PythonLineOfOffset_OffsetOnLoneLF.
		{`"\n\r" adjacency, ON a lone \r`, "a\n\rb\n\rc", 2, 2},
		{`"\n\r" adjacency, first byte of line 3`, "a\n\rb\n\rc", 3, 3},
		// "\r\n\r\n" — two well-formed pairs, so two lines, not four.
		{`two CRLF pairs "a\r\n\r\nb"`, "a\r\n\r\nb", 5, 3},
		// "\r\r\n" — a lone CR followed by a pair: two terminators, not three.
		{`lone CR then CRLF "a\r\r\nb"`, "a\r\r\nb", 4, 3},
		// "\r\r" — two lone CRs, two lines.
		{`two lone CRs "a\r\rb"`, "a\r\rb", 3, 3},
		// "\n\n" — two LFs, the ordinary blank-line case, two lines.
		{`two LFs "a\n\nb"`, "a\n\nb", 3, 3},
	}
	for _, tc := range cases {
		if got := pythonLineOfOffset(tc.src, tc.off); got != tc.want {
			t.Errorf("#6652: pythonLineOfOffset(%q, %d) [%s] = %d, want %d",
				tc.src, tc.off, tc.name, got, tc.want)
		}
	}
}

// TestIssue6652_PythonLineOfOffset_OffsetOnLoneLF pins the `content[off-1] ==
// '\r'` half of the split-pair guard — the half no other case reaches.
//
// SplitCRLFPair already exercises the `content[off] == '\n'` half (its offsets
// land on the "\n" OF A PAIR). Nothing, until now, passed an offset that lands
// on a LONE "\n", where the guard must NOT fire. Dropping the "\r" check makes
// the guard fire on every plain Unix newline and decrement a line number that
// was never incremented, returning **line 0** — not a wrong line, an impossible
// one.
//
// This matters precisely because of why the guard was kept (#6652 review):
// pythonLineOfOffset is a GENERAL offset->line helper, and unreachable-today is
// a property of one caller's regex, not of the helper. A general helper that
// returns 0 for an ordinary offset is broken for exactly the callers that
// generality was preserved for.
func TestIssue6652_PythonLineOfOffset_OffsetOnLoneLF(t *testing.T) {
	const src = "a\nb\nc" // plain Unix: every "\n" is lone, none is part of a pair
	cases := []struct {
		name string
		off  int
		want int
	}{
		{"ON the first lone \\n — terminator not yet crossed", 1, 1},
		{"first byte of line 2", 2, 2},
		{"ON the second lone \\n", 3, 2},
		{"first byte of line 3", 4, 3},
	}
	for _, tc := range cases {
		got := pythonLineOfOffset(src, tc.off)
		if got != tc.want {
			t.Errorf("#6652: pythonLineOfOffset(%q, %d) [%s] = %d, want %d",
				src, tc.off, tc.name, got, tc.want)
		}
		if got < 1 {
			t.Errorf("#6652: pythonLineOfOffset(%q, %d) returned %d — line numbers "+
				"are 1-based and no offset may ever produce a line below 1",
				src, tc.off, got)
		}
	}
}

// --- #6652 round 4: the DECORATOR ROUTE line, in the same pass ---------------

// TestIssue6652_LoneCR_DecoratorRouteStartLineMatchesPython is the second
// reproduction. Review found the first round fixed the MOUNT's line map and
// left the DECORATOR ROUTE's alone — two sites inside synthesizeFastAPI itself
// still went through the "\n"-only lineOfOffset. One lone-CR file therefore
// produced a correct mount line and a wrong route line from the SAME pass, the
// SAME language and the SAME observable.
//
// This asserts the route's defLine, the value that reaches the emitted
// http_endpoint_definition's StartLine.
func TestIssue6652_LoneCR_DecoratorRouteStartLineMatchesPython(t *testing.T) {
	// No `#` comment before a lone \r — that is #6648 and would mask the file.
	src := "from fastapi import FastAPI\r" +
		"app = FastAPI()\r" +
		"router = app\r" +
		"app.include_router(users.router, prefix=\"/api\")\r" +
		"@app.get(\"/ping\")\r" +
		"def ping():\r" +
		"    return {}\n"

	wantRoute := pyLineOfMountForTest(t, src, "def ping")
	if wantRoute != 6 {
		t.Fatalf("#6652: oracle disagrees with the hand-computed fixture: got %d, want 6", wantRoute)
	}

	type got struct {
		name string
		line int
	}
	var routes []got
	var mounts []types.EntityRecord
	synthesizeFastAPI(src, "app/main.py",
		func(method, canonicalPath, framework, handlerKind, handlerName string, defLine int) {
			routes = append(routes, got{name: handlerName, line: defLine})
		},
		func(e types.EntityRecord) { mounts = append(mounts, e) },
	)

	if len(routes) != 1 {
		t.Fatalf("#6652: expected exactly 1 decorator route, got %d (%v)", len(routes), routes)
	}
	if routes[0].line != wantRoute {
		t.Errorf("#6652: decorator route %q StartLine = %d, want %d "+
			"(lineOfOffset counts \\n only; Python terminates a line on a bare \\r too)",
			routes[0].name, routes[0].line, wantRoute)
	}

	// The two sites must agree WITH EACH OTHER as well as with Python. A file
	// that reports a correct mount line and a wrong route line is the exact
	// defect review found, so pin the consistency directly.
	if len(mounts) != 1 {
		t.Fatalf("#6652: expected exactly 1 mount, got %d", len(mounts))
	}
	wantMount := pyLineOfMountForTest(t, src, "app.include_router")
	if mounts[0].StartLine != wantMount {
		t.Errorf("#6652: mount StartLine = %d, want %d", mounts[0].StartLine, wantMount)
	}
	if mounts[0].StartLine == wantMount && routes[0].line != wantRoute {
		t.Errorf("#6652: SAME FILE, SAME PASS: mount line is correct (%d) but the "+
			"route line is not (%d, want %d) — the line map was fixed at one site "+
			"and not the other", mounts[0].StartLine, routes[0].line, wantRoute)
	}
}

// TestIssue6652_PythonLineOfOffset_OffsetAtEOFAfterLoneCR pins the `off <
// len(content)` term of the guard's conjunction — the bounds check.
//
// Relaxing it to `off <= len(content)` is not a wrong-line bug: it indexes
// content[off] one past the end and PANICS. Every other case in this file that
// reaches off == len(content) has a non-"\r" byte at content[off-1], so the &&
// short-circuits before the out-of-range index and the mutant survives. A
// source ending in a LONE "\r", read at offset len(src), is the one shape that
// reaches the index.
//
// Third survivor found in this guard, one per term of the conjunction, so the
// remaining terms are enumerated in
// TestIssue6652_PythonLineOfOffset_GuardConjunction rather than left to the
// next review.
func TestIssue6652_PythonLineOfOffset_OffsetAtEOFAfterLoneCR(t *testing.T) {
	const src = "x = 1\r" // ends on a LONE \r; len(src) == 6
	// Python: the trailing \r terminates line 1, so offset 6 is on line 2.
	if got := pythonLineOfOffset(src, len(src)); got != 2 {
		t.Errorf("#6652: pythonLineOfOffset(%q, %d) = %d, want 2", src, len(src), got)
	}
	// Past-EOF offsets clamp to the same place and must not index out of range.
	if got := pythonLineOfOffset(src, len(src)+1); got != 2 {
		t.Errorf("#6652: pythonLineOfOffset(%q, %d) = %d, want 2", src, len(src)+1, got)
	}
	// A lone \r followed by exactly one byte: off == len-1 lands ON the \r.
	if got := pythonLineOfOffset("x\ry", 1); got != 1 {
		t.Errorf(`#6652: pythonLineOfOffset("x\ry", 1) = %d, want 1`, got)
	}
}

// TestIssue6652_PythonLineOfOffset_GuardConjunction walks the split-pair guard's
// four terms deliberately, so each has a case that fails when that term alone is
// broken. Three separate reviews each found one survivor here; enumerating the
// conjunction is cheaper than a fourth round.
//
//	if off > 0 && off < len(content) && content[off-1] == '\r' && content[off] == '\n'
//
//	term 1  off > 0                  — offset 0 must not index content[-1]
//	term 2  off < len(content)       — OffsetAtEOFAfterLoneCR (panic)
//	term 3  content[off-1] == '\r'   — OffsetOnLoneLF (returned line 0)
//	term 4  content[off] == '\n'     — SplitCRLFPair
func TestIssue6652_PythonLineOfOffset_GuardConjunction(t *testing.T) {
	// term 1: offset 0 on a source whose FIRST byte is "\n". Dropping `off > 0`
	// indexes content[-1] and panics; weakening it to `off >= 0` does the same.
	if got := pythonLineOfOffset("\nabc", 0); got != 1 {
		t.Errorf(`#6652 term1: pythonLineOfOffset("\nabc", 0) = %d, want 1`, got)
	}
	// term 1, the CRLF-at-offset-0 shape: still line 1, still no negative index.
	if got := pythonLineOfOffset("\r\nabc", 0); got != 1 {
		t.Errorf(`#6652 term1: pythonLineOfOffset("\r\nabc", 0) = %d, want 1`, got)
	}
	// term 3: content[off-1] == '\r' mutated to == '\n'. Witness "a\n\nb" at 2 —
	// prefix "a\n" is line 2; firing the guard there yields 1.
	if got := pythonLineOfOffset("a\n\nb", 2); got != 2 {
		t.Errorf(`#6652 term3: pythonLineOfOffset("a\n\nb", 2) = %d, want 2`, got)
	}
	// term 4: content[off] == '\n' mutated away — the guard would fire after any
	// "\r", including a lone one, where no pair exists to un-count.
	if got := pythonLineOfOffset("a\rb", 2); got != 2 {
		t.Errorf(`#6652 term4: pythonLineOfOffset("a\rb", 2) = %d, want 2`, got)
	}
}

// collectFastAPIRoutes runs the pass and returns handlerName -> defLine.
func collectFastAPIRoutes(src string) map[string]int {
	out := map[string]int{}
	synthesizeFastAPI(src, "app/main.py",
		func(method, canonicalPath, framework, handlerKind, handlerName string, defLine int) {
			out[handlerName] = defLine
		}, nil)
	return out
}

// TestIssue6652_LoneCR_ApiRouteDecoratorStartLine covers the SECOND site inside
// synthesizeFastAPI (the generic @<recv>.api_route(...) form). Reverting this
// site alone survives every other fixture in this file, so without this case the
// fix would be pinned at one call site and merely assumed at the other.
func TestIssue6652_LoneCR_ApiRouteDecoratorStartLine(t *testing.T) {
	src := "from fastapi import FastAPI\r" +
		"app = FastAPI()\r" +
		"x = 1\r" +
		"@app.api_route(\"/ping\", methods=[\"GET\"])\r" +
		"def ping():\r" +
		"    return {}\n"

	want := pyLineOfMountForTest(t, src, "def ping")
	if want != 5 {
		t.Fatalf("#6652: oracle disagrees with the hand-computed fixture: got %d, want 5", want)
	}
	routes := collectFastAPIRoutes(src)
	got, ok := routes["ping"]
	if !ok {
		t.Fatalf("#6652: api_route decorator produced no route (got %v)", routes)
	}
	if got != want {
		t.Errorf("#6652: api_route handler StartLine = %d, want %d", got, want)
	}
}

// TestIssue6652_LoneCR_AddApiRouteFallbackStartLine covers the THIRD site,
// synthesizeFastAPIAddRoute's lineOfOffset FALLBACK. The handler named here is
// not defined in the file, so findPyDefLine returns 0 and the fallback is the
// branch actually taken.
//
// NOTE the limit of this fix: when the handler IS defined in the file,
// findPyDefLine supplies the line and that helper is still "\n"-only. That path
// remains wrong and is covered by the follow-up issue, not by this PR.
func TestIssue6652_LoneCR_AddApiRouteFallbackStartLine(t *testing.T) {
	src := "from fastapi import FastAPI\r" +
		"app = FastAPI()\r" +
		"x = 1\r" +
		"app.add_api_route(\"/ping\", endpoint=external.handler, methods=[\"GET\"])\n"

	want := pyLineOfMountForTest(t, src, "app.add_api_route")
	if want != 4 {
		t.Fatalf("#6652: oracle disagrees with the hand-computed fixture: got %d, want 4", want)
	}
	var lines []int
	synthesizeFastAPIAddRoute(src,
		func(method, canonicalPath, framework, handlerKind, handlerName string, defLine int) {
			lines = append(lines, defLine)
		})
	if len(lines) != 1 {
		t.Fatalf("#6652: expected exactly 1 add_api_route route, got %d (%v)", len(lines), lines)
	}
	if lines[0] != want {
		t.Errorf("#6652: add_api_route fallback StartLine = %d, want %d", lines[0], want)
	}
}
