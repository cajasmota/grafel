package coverage

import (
	"os"
	"strings"
	"testing"
)

func TestParseReport_Dispatch(t *testing.T) {
	cases := []struct {
		name       string
		format     string // "" → auto-detect
		fixture    string
		wantSource string
		wantFiles  int
	}{
		{"explicit-lcov", FormatLCOV, "testdata/sample.info", SourceLCOV, 2},
		{"explicit-cobertura", FormatCobertura, "testdata/sample.cobertura.xml", SourceCobertura, 2},
		{"explicit-jacoco", FormatJaCoCo, "testdata/sample.jacoco.xml", SourceJaCoCo, 2},
		{"detect-lcov", "", "testdata/sample.info", SourceLCOV, 2},
		{"detect-cobertura", "", "testdata/sample.cobertura.xml", SourceCobertura, 2},
		{"detect-jacoco", "", "testdata/sample.jacoco.xml", SourceJaCoCo, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := os.Open(tc.fixture)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer f.Close()
			rep, err := ParseReport(tc.format, f)
			if err != nil {
				t.Fatalf("ParseReport: %v", err)
			}
			if rep.Source != tc.wantSource {
				t.Errorf("source: want %q, got %q", tc.wantSource, rep.Source)
			}
			if len(rep.Files) != tc.wantFiles {
				t.Errorf("files: want %d, got %d", tc.wantFiles, len(rep.Files))
			}
		})
	}
}

func TestParseReport_UnknownFormat(t *testing.T) {
	if _, err := ParseReport("bogus", strings.NewReader("anything")); err == nil {
		t.Error("want error for unknown format, got nil")
	}
}

func TestParseReport_DetectUnknownXMLNoOp(t *testing.T) {
	// XML root that is neither <coverage> nor <report> → unrecognized.
	if _, err := ParseReport("", strings.NewReader(`<?xml version="1.0"?><other/>`)); err == nil {
		t.Error("want error for unrecognized XML root, got nil")
	}
}

func TestDetectFormat(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"lcov", "SF:a.go\nDA:1,1\nend_of_record\n", FormatLCOV},
		{"cobertura", `<?xml version="1.0"?><coverage></coverage>`, FormatCobertura},
		{"jacoco", `<?xml version="1.0"?><report></report>`, FormatJaCoCo},
		{"jacoco-with-doctype", `<?xml version="1.0"?><!DOCTYPE report SYSTEM "report.dtd"><report></report>`, FormatJaCoCo},
		{"empty", "", ""},
		{"unknown-xml", `<other/>`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectFormat([]byte(tc.in)); got != tc.want {
				t.Errorf("detectFormat: want %q, got %q", tc.want, got)
			}
		})
	}
}
