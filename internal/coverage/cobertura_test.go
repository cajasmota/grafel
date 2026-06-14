package coverage

import (
	"os"
	"strings"
	"testing"
)

func loadCobertura(t *testing.T) *Report {
	t.Helper()
	f, err := os.Open("testdata/sample.cobertura.xml")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	rep, err := ParseCobertura(f)
	if err != nil {
		t.Fatalf("ParseCobertura: %v", err)
	}
	return rep
}

func TestParseCobertura_FileSummaries(t *testing.T) {
	rep := loadCobertura(t)
	if rep.Source != SourceCobertura {
		t.Errorf("source: want %q, got %q", SourceCobertura, rep.Source)
	}
	if len(rep.Files) != 2 {
		t.Fatalf("want 2 files, got %d", len(rep.Files))
	}

	calc := rep.ByPath("src/calc.py")
	if calc == nil {
		t.Fatal("calc.py not parsed")
	}
	// Two <class> blocks for src/calc.py merge: lines 1-5,10,11,14 covered (8),
	// 7-9 uncovered → 11 total, 8 covered.
	if calc.TotalLines != 11 || calc.CoveredLines != 8 {
		t.Errorf("calc totals: want 8/11, got %d/%d", calc.CoveredLines, calc.TotalLines)
	}
	if calc.LineHits[1] != 5 || calc.LineHits[7] != 0 || calc.LineHits[14] != 2 {
		t.Errorf("line hits wrong: l1=%d l7=%d l14=%d", calc.LineHits[1], calc.LineHits[7], calc.LineHits[14])
	}

	util := rep.ByPath("src/util.py")
	if util == nil || util.TotalLines != 2 || util.CoveredLines != 2 {
		t.Errorf("util totals: want 2/2, got %+v", util)
	}
}

func TestParseCobertura_Malformed(t *testing.T) {
	// Not XML at all → decode error.
	if _, err := ParseCobertura(strings.NewReader("not xml at all")); err == nil {
		t.Error("want error on non-XML input, got nil")
	}
}

func TestParseCobertura_NoMatchNoOp(t *testing.T) {
	// Well-formed but with no <packages>/<class> → empty report, no error.
	in := strings.NewReader(`<?xml version="1.0"?><coverage></coverage>`)
	rep, err := ParseCobertura(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rep.Files) != 0 {
		t.Errorf("want 0 files for empty coverage, got %d", len(rep.Files))
	}
}
