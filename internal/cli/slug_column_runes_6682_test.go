package cli

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cajasmota/grafel/internal/statusfile"
)

// #6682 — the slug column of `grafel doctor` and `grafel status` is SIZED with
// Go's byte len() and PADDED with fmt's %-*s, which counts runes.
//
// What that actually does, measured rather than assumed: %-*s pads and never
// truncates, and a string's rune count is never greater than its byte count, so
// every slug field on every row is padded to exactly maxSlugLen RUNES. The rows
// therefore stay in agreement with each other — the table is not ragged in rune
// columns, and the #6681 column-equality guard is not defeated. The observable
// defect is that the column is sized in the wrong unit and so comes out
// OVER-WIDE by (maxBytes − maxRunes): a group holding one CJK slug pushes every
// row's payload ~5 columns right for no reason.
//
// The property pinned below is therefore the column WIDTH, not merely
// cross-line agreement: agreement already holds on unfixed code and pins
// nothing on its own. Agreement is asserted too, because it is the property
// #6681 depends on and a per-site width regression would break it.
//
// DISPLAY WIDTH IS EXPLICITLY NOT PINNED. See the comment on the fix in
// doctor_summary.go: rune count and terminal display width are different
// things, and a CJK slug still raggeds after this fix.

// runeColumn6682 returns the rune column at which token starts in line.
//
// It reports a failure rather than returning -1 when the token is absent: a
// column guard that compares two -1s passes vacuously, and a status token in
// particular is easy to lose (strings.Index(row, "OK") finds nothing on a STALE
// or MISS row). Callers get a column or a dead test, never a sentinel.
//
// The conversion from strings.Index's BYTE offset to a rune column is the whole
// point — on a non-ASCII slug the two differ, which is the unit confusion this
// file exists to pin.
func runeColumn6682(t *testing.T, what, line, token string) int {
	t.Helper()
	b := strings.Index(line, token)
	if b < 0 {
		t.Fatalf("%s: token %q not present in line %q — nothing below is measuring a column", what, token, line)
	}
	if strings.Index(line[b+len(token):], token) >= 0 {
		t.Fatalf("%s: token %q appears more than once in line %q — the column it names is ambiguous", what, token, line)
	}
	return utf8.RuneCountInString(line[:b])
}

// lineContaining6682 returns the single output line holding needle.
func lineContaining6682(t *testing.T, out, needle string) string {
	t.Helper()
	var hits []string
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, needle) {
			hits = append(hits, ln)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("want exactly 1 line containing %q, got %d:\n%s", needle, len(hits), out)
	}
	return hits[0]
}

// slugWidthPremise6682 asserts the fixture's slug set can actually tell bytes
// apart from runes, and returns the rune width the column should have.
//
// Without this a fixture of pure-ASCII slugs would satisfy every assertion
// below under BOTH the byte and the rune implementation, and the whole file
// would be vacuous while staying green. The multi-byte characters are the
// load-bearing bytes here, so their presence is asserted, not assumed.
func slugWidthPremise6682(t *testing.T, slugs []string, mustContain []string) int {
	t.Helper()
	maxBytes, maxRunes := 0, 0
	for _, s := range slugs {
		if len(s) > maxBytes {
			maxBytes = len(s)
		}
		if n := utf8.RuneCountInString(s); n > maxRunes {
			maxRunes = n
		}
	}
	if maxRunes < 4 {
		maxRunes = 4 // the minimum width for the "Slug" heading, both printers
	}
	if maxBytes < 4 {
		maxBytes = 4
	}
	if maxBytes == maxRunes {
		t.Fatalf("fixture slugs %q size to %d under BOTH byte and rune counting — "+
			"every column assertion below would pass on the unfixed code", slugs, maxBytes)
	}
	joined := strings.Join(slugs, "\x00")
	for _, c := range mustContain {
		if !strings.Contains(joined, c) {
			t.Fatalf("fixture slugs %q no longer contain %q — the multi-byte character the "+
				"byte/rune distinction rests on is gone and this fixture pins nothing", slugs, c)
		}
	}
	return maxRunes
}

// ─── doctor: the per-repo table, all three padding sites ─────────────────────

// doctorIndent6682 and columnGap6682 are the literal spacing PrintDoctorHealth
// writes around the slug field ("    %-*s  "). Pinning them absolutely, rather
// than only asserting the three lines agree with each other, is what makes a
// mutant that widens ALL THREE sites together scoreable.
const (
	doctorIndent6682 = 4
	statusIndent6682 = 2
	columnGap6682    = 2
)

func TestDoctorSlugColumnIsSizedInRunesNotBytes(t *testing.T) {
	// Three slugs on purpose, varying the axis the sizing reads:
	//   "asciixx" — 7 bytes / 7 runes, the LONGEST in runes, so it alone
	//               decides the correct width;
	//   "café"    — 5 bytes / 4 runes, a 2-byte European accent;
	//   "日本語x"  — 10 bytes / 4 runes, the longest in BYTES, so it alone
	//               decides the (wrong) unfixed width.
	// Whichever unit the code uses, exactly one of those two is the source of
	// the number, which is what separates the implementations by 3 columns.
	//
	// No slug's own byte length equals the 7-rune column, and every warning
	// below hangs off a repo whose byte length differs from it. That is
	// deliberate: it is what makes a HALF-fix scoreable — a mutant that sizes
	// in runes but leaves ONE padding site writing len(r.Slug) survives
	// otherwise, because a 5-byte slug in a 5-wide column lands on the right
	// column by luck. Measured: exactly that mutant survived until the slugs
	// were rebalanced.
	slugs := []string{"asciixx", "café", "日本語x"}
	wantWidth := slugWidthPremise6682(t, slugs, []string{"é", "日"})
	if wantWidth != 7 {
		t.Fatalf("premise: want a 7-rune column from %q, got %d", slugs, wantWidth)
	}
	for _, sl := range slugs {
		if len(sl) == wantWidth && utf8.RuneCountInString(sl) != wantWidth {
			t.Fatalf("fixture slug %q has byte length == the column width; a per-row len() "+
				"padding mutant would be invisible on its lines", sl)
		}
	}

	h := &DoctorGroupHealth{
		GroupName: "g6682", Healthy: true, Status: "HEALTHY",
		Repos: []*DoctorRepoHealth{
			// Both warnings are emitted TWICE over, once under a 5-byte/4-rune
			// slug and once under a 10-byte/4-rune one, so neither line's
			// column can be produced by that row's own byte length.
			{Slug: "café", Status: "OK", LastIndexedAge: "1h ago", Entities: 1, Relationships: 2,
				RebuildFailure:     &statusfile.RebuildFailure{Reason: "CAFE-REASON-6682"},
				RenameTruncated:    true,
				RenameAddedSkipped: 7},
			{Slug: "asciixx", Status: "OK", LastIndexedAge: "2h ago", Entities: 3, Relationships: 4},
			// STALE, not OK: a column guard keyed off the literal "OK" would
			// find nothing on this row, and the sizing must not care either.
			{Slug: "日本語x", Status: "STALE", LastIndexedAge: "9h ago", Entities: 5, Relationships: 6,
				RebuildFailure:     &statusfile.RebuildFailure{Reason: "CJK-REASON-6682"},
				RenameTruncated:    true,
				RenameAddedSkipped: 9},
		},
		IssuesFound: []string{},
	}
	var sb strings.Builder
	PrintDoctorHealth(&sb, []*DoctorGroupHealth{h})
	out := sb.String()

	wantCol := doctorIndent6682 + wantWidth + columnGap6682

	// The slug must not itself contain the token used to locate the payload,
	// or runeColumn6682 would measure the slug instead of the column.
	for _, s := range slugs {
		if strings.Contains(s, "OK") || strings.Contains(s, "STALE") || strings.Contains(s, "⚠") {
			t.Fatalf("fixture slug %q contains a column anchor token", s)
		}
	}

	type site struct{ what, needle, anchor string }
	sites := []site{
		{"status row (café, OK)", "café", "OK"},
		{"status row (asciixx, OK)", "asciixx", "OK"},
		{"status row (日本語x, STALE)", "日本語x", "STALE"},
		{"#5822 rebuild-failure warning (café)", "CAFE-REASON-6682", "⚠"},
		{"#5822 rebuild-failure warning (日本語x)", "CJK-REASON-6682", "⚠"},
		{"#6640 rename-truncation warning (café, 7 skipped)", "(7 added entities", "⚠"},
		{"#6640 rename-truncation warning (日本語x, 9 skipped)", "(9 added entities", "⚠"},
	}
	cols := make(map[string]int, len(sites))
	for _, s := range sites {
		line := lineContaining6682(t, out, s.needle)
		got := runeColumn6682(t, s.what, line, s.anchor)
		cols[s.what] = got
		if got != wantCol {
			t.Errorf("%s: payload starts at rune column %d, want %d "+
				"(%d indent + %d-rune slug column + %d gap). The slug column is sized with "+
				"byte len() but padded by rune count, so it overshoots by (bytes−runes).\nline: %q",
				s.what, got, wantCol, doctorIndent6682, wantWidth, columnGap6682, line)
		}
	}
	// The #6681 property, asserted independently of the absolute width: a
	// warning line must land on its repo row's column. This holds on unfixed
	// code too — it is here to catch a fix that moves one site and not another.
	for _, s := range sites[1:] {
		if cols[s.what] != cols[sites[0].what] {
			t.Errorf("%s sits at column %d but the status row is at %d — the warning no longer "+
				"aligns with the row it belongs to", s.what, cols[s.what], cols[sites[0].what])
		}
	}
}

// TestDoctorSlugColumnFloorIsCountedInRunes varies the slug-content axis the
// test above holds fixed: every slug here is SHORTER than the 4-column "Slug"
// minimum in runes but longer than it in bytes, so the floor is reached under
// rune counting and bypassed entirely under byte counting.
func TestDoctorSlugColumnFloorIsCountedInRunes(t *testing.T) {
	// "é" is 2 bytes / 1 rune and "日本" is 6 bytes / 2 runes: neither byte
	// length is 4, so the floor cannot be reproduced by a per-row len() either.
	slugs := []string{"é", "日本"}
	wantWidth := slugWidthPremise6682(t, slugs, []string{"é", "日"})
	if wantWidth != 4 {
		t.Fatalf("premise: want the 4-column floor from %q, got %d", slugs, wantWidth)
	}
	for _, sl := range slugs {
		if len(sl) == wantWidth {
			t.Fatalf("fixture slug %q has byte length == the floor width", sl)
		}
	}

	h := &DoctorGroupHealth{
		GroupName: "g6682b", Healthy: true, Status: "HEALTHY",
		Repos: []*DoctorRepoHealth{
			{Slug: "é", Status: "OK", LastIndexedAge: "1h ago",
				RenameTruncated: true, RenameAddedSkipped: 3},
			{Slug: "日本", Status: "OK", LastIndexedAge: "1h ago",
				RebuildFailure: &statusfile.RebuildFailure{Reason: "CJK-REASON-6682"}},
		},
		IssuesFound: []string{},
	}
	var sb strings.Builder
	PrintDoctorHealth(&sb, []*DoctorGroupHealth{h})
	out := sb.String()

	wantCol := doctorIndent6682 + wantWidth + columnGap6682
	for _, s := range []struct{ what, needle, anchor string }{
		{"status row (é)", "é ", "OK"},
		{"status row (日本)", "日本", "OK"},
		{"#6640 rename-truncation warning (é)", "rename detection TRUNCATED", "⚠"},
		{"#5822 rebuild-failure warning (日本)", "CJK-REASON-6682", "⚠"},
	} {
		line := lineContaining6682(t, out, s.needle)
		if got := runeColumn6682(t, s.what, line, s.anchor); got != wantCol {
			t.Errorf("%s: payload starts at rune column %d, want %d\nline: %q", s.what, got, wantCol, line)
		}
	}
}

// ─── status: the sibling table, all five padding sites ───────────────────────

// PrintStatusSummary carries the identical byte-sized/rune-padded column over
// five padding sites. It is fixed and pinned with doctor because the two
// tables are the same defect, and a reader comparing `grafel status` against
// `grafel doctor` on the same group would otherwise see two different widths.
func TestStatusSlugColumnIsSizedInRunesNotBytes(t *testing.T) {
	slugs := []string{"asciixx", "café", "日本語x"}
	wantWidth := slugWidthPremise6682(t, slugs, []string{"é", "日"})
	if wantWidth != 7 {
		t.Fatalf("premise: want a 7-rune column from %q, got %d", slugs, wantWidth)
	}
	for _, sl := range slugs {
		if len(sl) == wantWidth && utf8.RuneCountInString(sl) != wantWidth {
			t.Fatalf("fixture slug %q has byte length == the column width", sl)
		}
	}

	s := &StatusSummary{
		GroupName: "g6682c",
		RepoStats: map[string]*RepoStatus{
			// The payload of a normal row is the right-aligned "%5s files"
			// field, so Files is given 6 digits: fmtInt overflows the field,
			// no alignment padding is emitted, and the literal below marks the
			// payload's first column exactly.
			"café": {
				Files: 123456, Entities: 7, Relationships: 7, LastIndexedAge: "1h ago",
				RebuildFailure: &statusfile.RebuildFailure{Reason: "CAFE-REASON-6682"},
				GraphLoadError: "CAFE-GRAPHERR-6682",
			},
			"asciixx": {Files: 234567, Entities: 2, Relationships: 2, LastIndexedAge: "2h ago"},
			// RefUnknown takes the other branch of the row printer, which has
			// its own two padding sites.
			"日本語x": {
				RefUnknown:     true,
				RebuildFailure: &statusfile.RebuildFailure{Reason: "CJK-REASON-6682"},
			},
		},
	}
	var sb strings.Builder
	PrintStatusSummary(&sb, s)
	out := sb.String()

	wantCol := statusIndent6682 + wantWidth + columnGap6682
	for _, c := range []struct{ what, needle, anchor string }{
		{"status row (café)", "café", "123,456"},
		{"status row (asciixx)", "asciixx", "234,567"},
		{"#5822 D ref-UNKNOWN row (日本語x)", "state UNKNOWN", "state UNKNOWN"},
		{"#5822 rebuild-failure warning (café)", "CAFE-REASON-6682", "⚠"},
		{"#6013 graph-load-error warning (café)", "CAFE-GRAPHERR-6682", "⚠"},
		{"#5822 rebuild-failure warning under ref-UNKNOWN (日本語x)", "CJK-REASON-6682", "⚠"},
	} {
		line := lineContaining6682(t, out, c.needle)
		if got := runeColumn6682(t, c.what, line, c.anchor); got != wantCol {
			t.Errorf("%s: payload starts at rune column %d, want %d "+
				"(%d indent + %d-rune slug column + %d gap)\nline: %q",
				c.what, got, wantCol, statusIndent6682, wantWidth, columnGap6682, line)
		}
	}
}
