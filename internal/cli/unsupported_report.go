package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/cajasmota/grafel/internal/classifier"
)

// Thresholds for the "unsupported languages" report (#6338).
//
// `doctor` is the diagnostic command — it shows the full table, so a single
// stray file is still discoverable when someone is actually investigating.
// `status` is the everyday command, where one `.pas` file is noise and 672
// `.vb` files are the headline; it only shows extensions at or above the floor.
const (
	// DoctorUnsupportedMinFiles is doctor's floor. It is above 1: a single
	// stray file of some language is never the "grafel silently indexed
	// nothing for a whole language" signal this report exists to carry, and a
	// floor of 1 maximised the row count on exactly the repos where the table
	// was already hardest to read.
	DoctorUnsupportedMinFiles = 5
	// StatusUnsupportedMinFiles is the everyday command's floor, higher again.
	StatusUnsupportedMinFiles = 25
	// UnsupportedMaxRows caps the rendered table; the remainder is summarised
	// in a single trailing line. A table nobody reads to the end reports
	// nothing, however clean each row is.
	UnsupportedMaxRows = 8
)

// UnsupportedRow is one aggregated line of the report: an extension, how many
// files carried it, and — where we have one — the language's name and the
// issue tracking support for it.
type UnsupportedRow struct {
	Ext      string
	Count    int
	Language string // "" when we have no confident name
	Issue    string // "" when nothing tracks it
}

// UnsupportedRows turns a per-extension count map (as persisted in
// graph-stats.json) into the rows to display, dropping:
//
//   - extensions with a non-positive count — "report zero as absent, not as a
//     zero row";
//   - extensions some extractor now claims — the counts come off a sidecar that
//     may predate the extractor, so this is re-checked at RENDER time, not only
//     when the counts were collected. This is what makes `.vb` disappear from
//     the report the moment VB.NET extraction lands (#6327), whether or not the
//     repo has been reindexed since;
//   - extensions that name no language. The collection side stopped counting
//     these when the classifier grew its two-disposition split, but a sidecar
//     written by a build from BEFORE that split still carries `.json 1938`,
//     `.log 67`, `.ds_store 11`. Without this filter every upgrading user would
//     read the junk table once, from disk, until their next full reindex;
//   - extensions below minFiles.
//
// Rows are sorted by count descending, then extension ascending, so the biggest
// silent gap is the first thing read and the output is stable across runs.
func UnsupportedRows(counts map[string]int, minFiles int) []UnsupportedRow {
	if minFiles < 1 {
		minFiles = 1
	}
	rows := make([]UnsupportedRow, 0, len(counts))
	for ext, n := range counts {
		if n < minFiles {
			continue
		}
		// LanguageDisplayName is "" both for a supported extension and for one
		// that names no language, so this single check covers both filters.
		name := classifier.LanguageDisplayName(ext)
		if name == "" {
			continue
		}
		rows = append(rows, UnsupportedRow{
			Ext:      ext,
			Count:    n,
			Language: name,
			Issue:    classifier.TrackingIssue(ext),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Ext < rows[j].Ext
	})
	return rows
}

// PrintUnsupportedLanguages writes the report block, indented by indent.
//
// It writes NOTHING at all when there are no rows — no header, no blank line.
// A healthy repo's `doctor`/`status` output is byte-identical to what it was
// before this feature existed; the whole point is that the section appears only
// when grafel really did skip something.
func PrintUnsupportedLanguages(w io.Writer, indent string, rows []UnsupportedRow) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(w, "%sUnsupported languages (no extractor):\n", indent)

	shown, hiddenRows, hiddenFiles := rows, 0, 0
	if len(rows) > UnsupportedMaxRows {
		shown = rows[:UnsupportedMaxRows]
		for _, r := range rows[UnsupportedMaxRows:] {
			hiddenRows++
			hiddenFiles += r.Count
		}
	}

	extWidth, countWidth := 0, 0
	for _, r := range shown {
		if len(r.Ext) > extWidth {
			extWidth = len(r.Ext)
		}
		if n := len(fmtInt(r.Count)); n > countWidth {
			countWidth = n
		}
	}
	for _, r := range shown {
		noun := "files"
		if r.Count == 1 {
			noun = "file"
		}
		// UnsupportedRows drops every extension that names no language, so
		// r.Language is always set here. (Measured: a mutant emitting unnamed
		// rows is killed by TestUnsupportedRows_UnnamedExtensionsNeverRendered.)
		suffix := fmt.Sprintf("  (%s — not supported", r.Language)
		if r.Issue != "" {
			suffix += ", see " + r.Issue
		}
		suffix += ")"
		fmt.Fprintf(w, "%s  %-*s  %*s %s%s\n",
			indent, extWidth, r.Ext, countWidth, fmtInt(r.Count), noun, suffix)
	}
	if hiddenRows > 0 {
		fmt.Fprintf(w, "%s  … and %d more (%s files) — `grafel doctor --json` for the full list\n",
			indent, hiddenRows, fmtInt(hiddenFiles))
	}
}
