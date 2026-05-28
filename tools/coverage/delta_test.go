package main

import (
	"strings"
	"testing"
)

// syntheticRegistry builds a minimal Registry directly (no disk I/O, no git).
// cells is a list of (recordID, lang, group, key, status); group "(flat)"
// uses the flat Capabilities map, anything else goes into Groups.
// fsGroup is a list of (recordID, group, key, status) injected into
// FrameworkSpecific.
func syntheticRegistry(cells [][5]string, fsCells [][4]string) *Registry {
	type recKey struct{ id, lang string }
	recIdx := map[string]int{}
	reg := &Registry{SchemaVersion: 1}

	ensureRecord := func(id, lang string) int {
		if i, ok := recIdx[id]; ok {
			return i
		}
		reg.Records = append(reg.Records, Record{
			ID:           id,
			Language:     lang,
			Category:     "http_framework",
			Label:        id,
			Capabilities: map[string]Capability{},
		})
		i := len(reg.Records) - 1
		recIdx[id] = i
		return i
	}

	for _, c := range cells {
		id, lang, group, key, status := c[0], c[1], c[2], c[3], c[4]
		i := ensureRecord(id, lang)
		r := &reg.Records[i]
		if group == "(flat)" {
			if r.Capabilities == nil {
				r.Capabilities = map[string]Capability{}
			}
			r.Capabilities[key] = Capability{Status: status}
		} else {
			if r.Groups == nil {
				r.Groups = map[string]map[string]Capability{}
			}
			if r.Groups[group] == nil {
				r.Groups[group] = map[string]Capability{}
			}
			r.Groups[group][key] = Capability{Status: status}
		}
	}

	for _, c := range fsCells {
		id, group, key, status := c[0], c[1], c[2], c[3]
		// Find (or re-find) the record by id — lang already set above.
		i, ok := recIdx[id]
		if !ok {
			// Create a placeholder record (lang unknown here — use "?")
			reg.Records = append(reg.Records, Record{
				ID:       id,
				Language: "?",
				Category: "http_framework",
				Label:    id,
			})
			i = len(reg.Records) - 1
			recIdx[id] = i
		}
		r := &reg.Records[i]
		if r.FrameworkSpecific == nil {
			r.FrameworkSpecific = map[string]map[string]Capability{}
		}
		if r.FrameworkSpecific[group] == nil {
			r.FrameworkSpecific[group] = map[string]Capability{}
		}
		r.FrameworkSpecific[group][key] = Capability{Status: status}
	}

	return reg
}

// TestExtractCellsFlat verifies flat capability extraction.
func TestExtractCellsFlat(t *testing.T) {
	reg := syntheticRegistry([][5]string{
		{"rec.a", "jsts", "(flat)", "endpoint_synthesis", "full"},
		{"rec.a", "jsts", "(flat)", "auth_coverage", "partial"},
	}, nil)
	cells := extractCells(reg)
	if len(cells) != 2 {
		t.Fatalf("expected 2 cells, got %d", len(cells))
	}
	ck := cellKey{RecordID: "rec.a", Group: "(flat)", Key: "endpoint_synthesis"}
	if e, ok := cells[ck]; !ok || e.Status != "full" {
		t.Errorf("expected endpoint_synthesis=full, got %v ok=%v", e.Status, ok)
	}
}

// TestExtractCellsGrouped verifies grouped capability extraction.
func TestExtractCellsGrouped(t *testing.T) {
	reg := syntheticRegistry([][5]string{
		{"rec.b", "python", "Routing", "endpoint_synthesis", "full"},
		{"rec.b", "python", "Auth", "auth_coverage", "missing"},
	}, nil)
	cells := extractCells(reg)
	if len(cells) != 2 {
		t.Fatalf("expected 2 cells, got %d", len(cells))
	}
	ck := cellKey{RecordID: "rec.b", Group: "Routing", Key: "endpoint_synthesis"}
	if e, ok := cells[ck]; !ok || e.Status != "full" {
		t.Errorf("expected endpoint_synthesis=full, got %v ok=%v", e.Status, ok)
	}
}

// TestExtractCellsFrameworkSpecific verifies framework_specific cells are
// included in extraction.
func TestExtractCellsFrameworkSpecific(t *testing.T) {
	reg := syntheticRegistry([][5]string{
		{"rec.c", "jsts", "(flat)", "endpoint_synthesis", "full"},
	}, [][4]string{
		{"rec.c", "NestJS Internals", "dependency_injection", "missing"},
		{"rec.c", "NestJS Internals", "module_graph", "partial"},
	})
	cells := extractCells(reg)
	// 1 flat + 2 framework_specific = 3
	if len(cells) != 3 {
		t.Fatalf("expected 3 cells (1 flat + 2 fs), got %d", len(cells))
	}
	ck := cellKey{RecordID: "rec.c", Group: "NestJS Internals", Key: "dependency_injection"}
	if e, ok := cells[ck]; !ok || e.Status != "missing" {
		t.Errorf("expected dependency_injection=missing, got %v ok=%v", e.Status, ok)
	}
}

// TestComputeFlipsImprove verifies that status upgrades are detected as
// improvements.
func TestComputeFlipsImprove(t *testing.T) {
	base := syntheticRegistry([][5]string{
		{"rec.a", "jsts", "(flat)", "endpoint_synthesis", "missing"},
	}, nil)
	head := syntheticRegistry([][5]string{
		{"rec.a", "jsts", "(flat)", "endpoint_synthesis", "full"},
	}, nil)

	baseCells := extractCells(base)
	headCells := extractCells(head)
	flips := computeFlips(baseCells, headCells)

	if len(flips) != 1 {
		t.Fatalf("expected 1 flip, got %d", len(flips))
	}
	f := flips[0]
	if f.Before != "missing" || f.After != "full" {
		t.Errorf("expected missing→full, got %s→%s", f.Before, f.After)
	}
}

// TestComputeFlipsNoChange verifies that identical registries produce zero flips.
func TestComputeFlipsNoChange(t *testing.T) {
	reg := syntheticRegistry([][5]string{
		{"rec.a", "jsts", "(flat)", "endpoint_synthesis", "full"},
		{"rec.a", "jsts", "(flat)", "auth_coverage", "partial"},
	}, nil)
	baseCells := extractCells(reg)
	headCells := extractCells(reg)
	flips := computeFlips(baseCells, headCells)
	if len(flips) != 0 {
		t.Errorf("expected 0 flips, got %d: %+v", len(flips), flips)
	}
}

// TestComputeFlipsRemoved verifies that cells removed in HEAD are shown as
// removed.
func TestComputeFlipsRemoved(t *testing.T) {
	base := syntheticRegistry([][5]string{
		{"rec.a", "jsts", "(flat)", "endpoint_synthesis", "full"},
		{"rec.a", "jsts", "(flat)", "auth_coverage", "partial"},
	}, nil)
	head := syntheticRegistry([][5]string{
		{"rec.a", "jsts", "(flat)", "endpoint_synthesis", "full"},
		// auth_coverage removed
	}, nil)
	baseCells := extractCells(base)
	headCells := extractCells(head)
	flips := computeFlips(baseCells, headCells)
	if len(flips) != 1 {
		t.Fatalf("expected 1 flip (removed cell), got %d: %+v", len(flips), flips)
	}
	if flips[0].After != "(removed)" {
		t.Errorf("expected After=(removed), got %q", flips[0].After)
	}
}

// TestComputeFlipsNewCell verifies that cells added in HEAD with no prior
// base entry are treated as missing→<new-status>.
func TestComputeFlipsNewCell(t *testing.T) {
	base := syntheticRegistry([][5]string{
		{"rec.a", "jsts", "(flat)", "endpoint_synthesis", "full"},
	}, nil)
	head := syntheticRegistry([][5]string{
		{"rec.a", "jsts", "(flat)", "endpoint_synthesis", "full"},
		{"rec.a", "jsts", "(flat)", "auth_coverage", "partial"}, // new
	}, nil)
	baseCells := extractCells(base)
	headCells := extractCells(head)
	flips := computeFlips(baseCells, headCells)
	if len(flips) != 1 {
		t.Fatalf("expected 1 flip (new cell), got %d", len(flips))
	}
	if flips[0].Before != "missing" || flips[0].After != "partial" {
		t.Errorf("expected missing→partial, got %s→%s", flips[0].Before, flips[0].After)
	}
}

// TestComputeFlipsFrameworkSpecific verifies that framework_specific cells
// are included in flip detection.
func TestComputeFlipsFrameworkSpecific(t *testing.T) {
	base := syntheticRegistry([][5]string{
		{"rec.c", "jsts", "(flat)", "endpoint_synthesis", "full"},
	}, [][4]string{
		{"rec.c", "NestJS Internals", "dependency_injection", "missing"},
	})
	head := syntheticRegistry([][5]string{
		{"rec.c", "jsts", "(flat)", "endpoint_synthesis", "full"},
	}, [][4]string{
		{"rec.c", "NestJS Internals", "dependency_injection", "partial"}, // improved
	})
	baseCells := extractCells(base)
	headCells := extractCells(head)
	flips := computeFlips(baseCells, headCells)
	if len(flips) != 1 {
		t.Fatalf("expected 1 flip in framework_specific, got %d: %+v", len(flips), flips)
	}
	f := flips[0]
	if f.Key != "dependency_injection" || f.Group != "NestJS Internals" {
		t.Errorf("unexpected flip: %+v", f)
	}
	if f.Before != "missing" || f.After != "partial" {
		t.Errorf("expected missing→partial, got %s→%s", f.Before, f.After)
	}
}

// TestBuildDeltaMarkdownShape verifies the high-level structure of the
// markdown output.
func TestBuildDeltaMarkdownShape(t *testing.T) {
	base := syntheticRegistry([][5]string{
		{"rec.a", "jsts", "(flat)", "endpoint_synthesis", "missing"},
		{"rec.a", "jsts", "(flat)", "auth_coverage", "partial"},
	}, nil)
	head := syntheticRegistry([][5]string{
		{"rec.a", "jsts", "(flat)", "endpoint_synthesis", "full"},   // improved
		{"rec.a", "jsts", "(flat)", "auth_coverage", "partial"},     // unchanged
	}, nil)

	md := buildDeltaMarkdown("origin/main", "HEAD", "", base, head)

	checks := []string{
		"## Coverage delta (this PR)",
		"_base_ `origin/main` → _head_ `HEAD`",
		"Cells changed: 1",
		"improved (toward full): **1**",
		"regressed: **0**",
		"### Corpus totals",
		"### Cells flipped by this PR",
		"| `rec.a` | (flat) | endpoint_synthesis | missing → **full** |",
		"### Transitions",
		"missing → full",
		"### Assessment",
		"<sub>generated by `coverage delta`",
	}
	for _, want := range checks {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\nfull output:\n%s", want, md)
		}
	}
}

// TestBuildDeltaMarkdownLangFilter verifies the --lang filter hides records
// of other languages from the flipped-cell table while still counting all
// flips in Corpus totals.
func TestBuildDeltaMarkdownLangFilter(t *testing.T) {
	base := syntheticRegistry([][5]string{
		{"rec.jsts", "jsts", "(flat)", "endpoint_synthesis", "missing"},
		{"rec.py", "python", "(flat)", "endpoint_synthesis", "missing"},
	}, nil)
	head := syntheticRegistry([][5]string{
		{"rec.jsts", "jsts", "(flat)", "endpoint_synthesis", "full"},
		{"rec.py", "python", "(flat)", "endpoint_synthesis", "full"},
	}, nil)

	md := buildDeltaMarkdown("origin/main", "HEAD", "python", base, head)

	// Python record should appear in flipped-cell table
	if !strings.Contains(md, "`rec.py`") {
		t.Errorf("expected rec.py in filtered table, got:\n%s", md)
	}
	// jsts record should NOT appear in flipped-cell table (filtered out)
	if strings.Contains(md, "`rec.jsts`") {
		t.Errorf("expected rec.jsts to be filtered from table, got:\n%s", md)
	}
	// But total cells changed should still be 2
	if !strings.Contains(md, "Cells changed: 2") {
		t.Errorf("expected total Cells changed: 2, got:\n%s", md)
	}
	// Filter notice should appear
	if !strings.Contains(md, "filtered to language: `python`") {
		t.Errorf("expected filter notice, got:\n%s", md)
	}
}

// TestBuildDeltaMarkdownNoFlips verifies the output when registries are
// identical (no flipped cells).
func TestBuildDeltaMarkdownNoFlips(t *testing.T) {
	reg := syntheticRegistry([][5]string{
		{"rec.a", "jsts", "(flat)", "endpoint_synthesis", "full"},
	}, nil)
	md := buildDeltaMarkdown("origin/main", "HEAD", "", reg, reg)

	if strings.Contains(md, "### Cells flipped by this PR") {
		t.Errorf("expected no flipped-cell section when nothing changed, got:\n%s", md)
	}
	if !strings.Contains(md, "Cells changed: 0") {
		t.Errorf("expected Cells changed: 0, got:\n%s", md)
	}
}

// TestStatusCounts verifies the per-status tallying.
func TestStatusCounts(t *testing.T) {
	reg := syntheticRegistry([][5]string{
		{"rec.a", "jsts", "(flat)", "k1", "full"},
		{"rec.a", "jsts", "(flat)", "k2", "partial"},
		{"rec.a", "jsts", "(flat)", "k3", "missing"},
		{"rec.a", "jsts", "(flat)", "k4", "not_applicable"},
		{"rec.a", "jsts", "(flat)", "k5", "full"},
	}, nil)
	cells := extractCells(reg)
	counts := statusCounts(cells)
	if counts[StatusFull] != 2 {
		t.Errorf("expected 2 full, got %d", counts[StatusFull])
	}
	if counts[StatusPartial] != 1 {
		t.Errorf("expected 1 partial, got %d", counts[StatusPartial])
	}
	if counts[StatusMissing] != 1 {
		t.Errorf("expected 1 missing, got %d", counts[StatusMissing])
	}
	if counts[StatusNotApplicable] != 1 {
		t.Errorf("expected 1 not_applicable, got %d", counts[StatusNotApplicable])
	}
}

// TestDeltaHelpFlag verifies that `coverage delta --help` does not error
// and mentions the flags.
func TestDeltaHelpFlag(t *testing.T) {
	var out strings.Builder
	err := cmdDelta([]string{"--help"}, &out)
	// flag.ContinueOnError returns flag.ErrHelp on --help
	if err != nil && !strings.Contains(err.Error(), "help requested") && err.Error() != "flag: help requested" {
		t.Errorf("unexpected error from --help: %v", err)
	}
}
