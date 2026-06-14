package coverage

import (
	"os"
	"strings"
	"testing"
)

func loadJaCoCo(t *testing.T) *Report {
	t.Helper()
	f, err := os.Open("testdata/sample.jacoco.xml")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	rep, err := ParseJaCoCo(f)
	if err != nil {
		t.Fatalf("ParseJaCoCo: %v", err)
	}
	return rep
}

func TestParseJaCoCo_FileSummaries(t *testing.T) {
	rep := loadJaCoCo(t)
	if rep.Source != SourceJaCoCo {
		t.Errorf("source: want %q, got %q", SourceJaCoCo, rep.Source)
	}
	if len(rep.Files) != 2 {
		t.Fatalf("want 2 files, got %d", len(rep.Files))
	}

	// package name is joined with the sourcefile name.
	calc := rep.ByPath("com/example/Calc.java")
	if calc == nil {
		t.Fatal("Calc.java not parsed")
	}
	// ci>0 covered: lines 1-5,10,11 covered (7); 7-9 missed → 10 total, 7 covered.
	if calc.TotalLines != 10 || calc.CoveredLines != 7 {
		t.Errorf("calc totals: want 7/10, got %d/%d", calc.CoveredLines, calc.TotalLines)
	}
	if calc.LineHits[1] != 3 || calc.LineHits[7] != 0 {
		t.Errorf("line hits wrong: l1=%d l7=%d", calc.LineHits[1], calc.LineHits[7])
	}

	util := rep.ByPath("com/example/Util.java")
	if util == nil || util.TotalLines != 2 || util.CoveredLines != 2 {
		t.Errorf("util totals: want 2/2, got %+v", util)
	}
}

func TestParseJaCoCo_Malformed(t *testing.T) {
	if _, err := ParseJaCoCo(strings.NewReader("not xml at all")); err == nil {
		t.Error("want error on non-XML input, got nil")
	}
}

func TestParseJaCoCo_NoMatchNoOp(t *testing.T) {
	in := strings.NewReader(`<?xml version="1.0"?><report name="empty"></report>`)
	rep, err := ParseJaCoCo(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rep.Files) != 0 {
		t.Errorf("want 0 files for empty report, got %d", len(rep.Files))
	}
}
