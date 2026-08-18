package cli

import (
	"bytes"
	"strings"
	"testing"
)

// Requirement 1: nothing to report → print NOTHING. Not a header, not a zero
// row. `grafel doctor` on a healthy Go repo must look exactly as it did.
func TestPrintUnsupportedLanguages_CleanRepoPrintsNothing(t *testing.T) {
	for name, counts := range map[string]map[string]int{
		"nil":            nil,
		"empty":          {},
		"all-zero":       {".vb": 0},
		"all-supported":  {".go": 400, ".py": 90},
		"below-min":      {".vb": 3},
		"zero-and-supp":  {".go": 12, ".ts": 0},
		"negative-count": {".vb": -5},
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			PrintUnsupportedLanguages(&buf, "  ", UnsupportedRows(counts, 10))
			if buf.Len() != 0 {
				t.Fatalf("expected no output, got:\n%s", buf.String())
			}
		})
	}
}

// Requirement 2: a mixed repo reports ONLY the unsupported extensions, with
// correct counts, ordered so the headline is first.
func TestUnsupportedRows_MixedRepoOnlyUnsupported(t *testing.T) {
	rows := UnsupportedRows(map[string]int{
		".go":  12045,
		".ts":  3300,
		".vb":  672,
		".pas": 14,
		".zzq": 3,
	}, DoctorUnsupportedMinFiles)

	if len(rows) != 3 {
		t.Fatalf("want 3 unsupported rows, got %d: %+v", len(rows), rows)
	}
	// Ordered by count descending — 672 .vb files is the headline, not the
	// 3 .zzq strays.
	wantExt := []string{".vb", ".pas", ".zzq"}
	wantCount := []int{672, 14, 3}
	for i, r := range rows {
		if r.Ext != wantExt[i] || r.Count != wantCount[i] {
			t.Fatalf("row %d = %+v, want ext=%s count=%d", i, r, wantExt[i], wantCount[i])
		}
	}
	for _, r := range rows {
		if r.Ext == ".go" || r.Ext == ".ts" {
			t.Fatalf("supported extension %s leaked into the report", r.Ext)
		}
	}
}

// Requirement 3 — the regression that matters. An extension that BECOMES
// supported must disappear from the report even when a stale on-disk sidecar
// still carries a count for it. `.go` stands in for `.vb` after #6327 lands.
//
// This is also the vacuity guard: a report that listed every extension would
// satisfy "contains .vb" and fail here.
func TestUnsupportedRows_NowSupportedExtensionDisappears(t *testing.T) {
	// A sidecar written before the extractor existed still says 672.
	stale := map[string]int{".go": 672}
	rows := UnsupportedRows(stale, DoctorUnsupportedMinFiles)
	if len(rows) != 0 {
		t.Fatalf("a now-supported extension must vanish from the report, got %+v", rows)
	}
	var buf bytes.Buffer
	PrintUnsupportedLanguages(&buf, "  ", rows)
	if buf.Len() != 0 {
		t.Fatalf("and the section header must vanish with it, got:\n%s", buf.String())
	}
}

// The rendered block: aggregated one row per extension, named where we can.
func TestPrintUnsupportedLanguages_Rendering(t *testing.T) {
	var buf bytes.Buffer
	rows := UnsupportedRows(map[string]int{".vb": 672, ".pas": 14, ".zzq": 3}, DoctorUnsupportedMinFiles)
	PrintUnsupportedLanguages(&buf, "  ", rows)
	out := buf.String()

	if !strings.Contains(out, "Unsupported languages (no extractor):") {
		t.Fatalf("missing section header:\n%s", out)
	}
	// Named where we can — ".vb" alone is not actionable.
	if !strings.Contains(out, "VB.NET") {
		t.Fatalf("row for .vb must name the language:\n%s", out)
	}
	if !strings.Contains(out, "#6327") {
		t.Fatalf("row for .vb must point at the tracking issue:\n%s", out)
	}
	if !strings.Contains(out, "Pascal") {
		t.Fatalf("row for .pas must name the language:\n%s", out)
	}

	// Aggregated: ONE line per extension, carrying the count. A per-file
	// report would emit 672 lines.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	vbLines := 0
	for _, l := range lines {
		if strings.Contains(l, ".vb") {
			vbLines++
		}
	}
	if vbLines != 1 {
		t.Fatalf("want exactly 1 aggregated line for .vb, got %d:\n%s", vbLines, out)
	}
	if len(lines) != 4 { // header + 3 rows
		t.Fatalf("want header + 3 rows = 4 lines, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(out, "672 files") {
		t.Fatalf("the .vb row must carry the aggregate count 672:\n%s", out)
	}

	// An extension we have no confident name for renders bare rather than
	// guessing.
	for _, l := range lines {
		if strings.Contains(l, ".zzq") && strings.Contains(l, "not supported") {
			t.Fatalf(".zzq has no known language and must render bare, got %q", l)
		}
	}
}

// The `status` floor: one stray .pas is noise on the everyday command, 672 .vb
// files are the headline. `doctor` shows the full table.
func TestUnsupportedRows_StatusFloorVsDoctorFullTable(t *testing.T) {
	counts := map[string]int{".vb": 672, ".pas": 3}

	statusRows := UnsupportedRows(counts, StatusUnsupportedMinFiles)
	if len(statusRows) != 1 || statusRows[0].Ext != ".vb" {
		t.Fatalf("status must show only the extensions above the floor, got %+v", statusRows)
	}

	doctorRows := UnsupportedRows(counts, DoctorUnsupportedMinFiles)
	if len(doctorRows) != 2 {
		t.Fatalf("doctor must show the full table, got %+v", doctorRows)
	}

	if StatusUnsupportedMinFiles <= DoctorUnsupportedMinFiles {
		t.Fatalf("the status floor (%d) must be above doctor's (%d), or there is no floor at all",
			StatusUnsupportedMinFiles, DoctorUnsupportedMinFiles)
	}
}

// Ties break on extension name so the output is stable across runs (map
// iteration order is randomised).
func TestUnsupportedRows_DeterministicTieBreak(t *testing.T) {
	counts := map[string]int{".vb": 5, ".pas": 5, ".zzq": 5}
	want := []string{".pas", ".vb", ".zzq"}
	for i := 0; i < 20; i++ {
		rows := UnsupportedRows(counts, DoctorUnsupportedMinFiles)
		for j, r := range rows {
			if r.Ext != want[j] {
				t.Fatalf("iteration %d: row %d = %s, want %s", i, j, r.Ext, want[j])
			}
		}
	}
}
