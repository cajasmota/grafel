package vbnet

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The real-tree gate (AGENTS.md "Evidence", #6363).
//
// #6363 is the record of what constructed fixtures cost this epic: S3's review
// found three declaration-corrupting defects — `Option Explicit On` swallowing
// the next line, `End With` desynchronising the container stack, and the `32&`
// Long type character eating a declaration — none of which its 156-subtest
// suite could see, because hand-written fixtures are tidy and real files are
// not. This test runs the parser over whatever real VB.NET tree the
// environment points at and fails on a regression in the parse rate.
//
// It skips when no corpus is configured, so it is a gate where a corpus exists
// and never a false green where one does not. Point it at a tree with:
//
//	GRAFEL_VBNET_CORPUS=$HOME/Projects/archigraph-corpora go test ./internal/vbnet/
//
// The floor below was measured, not chosen: see TestCorpusParseRate's failure
// message, which prints the current numbers whenever it trips.
const corpusCleanFloor = 0.99

func corpusFiles(t *testing.T) []string {
	t.Helper()
	root := os.Getenv("GRAFEL_VBNET_CORPUS")
	if root == "" {
		t.Skip("GRAFEL_VBNET_CORPUS unset: no real VB.NET tree to measure against (#6363)")
	}
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".vb") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(out) == 0 {
		t.Skipf("no .vb files under %s", root)
	}
	sort.Strings(out)
	return out
}

// TestCorpusParseRate measures the fraction of real .vb files that parse with
// no diagnostic, and prints every distinct failure so a regression names
// itself rather than only moving a number.
func TestCorpusParseRate(t *testing.T) {
	files := corpusFiles(t)
	clean := 0
	byMessage := map[string]int{}
	var examples []string

	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		res := Parse(string(src))
		if len(res.Diagnostics) == 0 {
			clean++
			continue
		}
		if len(examples) < 10 {
			examples = append(examples, fmt.Sprintf("%s: %s", filepath.Base(f), res.Diagnostics[0]))
		}
		for _, d := range res.Diagnostics {
			byMessage[shapeOfDiagnostic(d.Message)]++
		}
	}

	rate := float64(clean) / float64(len(files))
	report := fmt.Sprintf("%d/%d files parsed clean (%.1f%%)", clean, len(files), rate*100)
	for msg, n := range byMessage {
		report += fmt.Sprintf("\n  %4d x %s", n, msg)
	}
	for _, e := range examples {
		report += "\n  e.g. " + e
	}
	if rate < corpusCleanFloor {
		t.Fatalf("parse rate below the recorded floor of %.0f%%:\n%s", corpusCleanFloor*100, report)
	}
	t.Log(report)
}

// TestMultiLineLiteralIsUnsupported records the one shape the parser does NOT
// handle, in the direction that makes the gap testable: it asserts the parser
// REPORTS the file rather than silently mis-parsing it, so the day the
// limitation is lifted this test fails and must be updated.
//
// VB 14 allows a string literal to span physical lines, and an interpolation
// hole to do the same. The pre-pass is line-oriented — scanLine resets literal
// state at every newline — so from the opening quote to the next quote on a
// later line, text is scanned as code. Two of the 302 corpus files do this
// (staxrip's Audio.vb embeds an AviSynth script; Thumbnailer.vb has a hole
// broken across five lines), which is the entire residual of the measured
// 300/302 parse rate.
func TestMultiLineLiteralIsUnsupported(t *testing.T) {
	// The interior of the literal is scanned as code, so a line inside it that
	// happens to read as an End closes a block that is still open. staxrip's
	// Audio.vb embeds an AviSynth script in exactly this way.
	src := strings.Join([]string{
		"Public Class C",
		"    Shared Function Script() As String",
		"        Return \"# script text",
		"End Function",
		"more text\"",
		"    End Function",
		"End Class",
	}, "\n")
	res := Parse(src)
	if len(res.Diagnostics) == 0 {
		t.Fatalf("multi-line string literals now parse; lift the limitation "+
			"recorded here and in the package doc. Tree:\n%s", res.File.Dump())
	}
}

// shapeOfDiagnostic strips the identifiers out of a diagnostic so failures
// group by cause rather than by name.
func shapeOfDiagnostic(msg string) string {
	if i := strings.IndexByte(msg, '"'); i >= 0 {
		if j := strings.IndexByte(msg[i+1:], '"'); j >= 0 {
			msg = msg[:i] + "<name>" + msg[i+j+2:]
		}
	}
	return msg
}

// TestCorpusTreeAgreesWithTable pins the two walks against each other.
//
// BuildTable (S3) and Parse (S4) are separate walks over the same source with
// separate container-stack logic. If they disagree about scope, the table
// answers a use site's paren classification for a scope the tree never puts it
// in, and every CALLS edge S5 draws from it is silently misattributed. The
// corpus is the only place that divergence shows up: constructed fixtures are
// written to one walk's expectations at a time.
func TestCorpusTreeAgreesWithTable(t *testing.T) {
	files := corpusFiles(t)
	missing, total := 0, 0
	var examples []string

	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		res := Parse(string(src))
		if len(res.Diagnostics) > 0 {
			continue // a file the parser already reports on proves nothing here
		}
		inTree := map[string]bool{}
		res.File.Walk(func(n *Node) {
			if n.Kind == NodeMethod || n.Kind.IsType() || n.Kind == NodeDelegate {
				inTree[FoldName(n.Path())] = true
			}
		})
		for _, s := range res.Table.All() {
			if s.Kind != KindType && s.Kind != KindMethod {
				continue
			}
			path := s.Name
			if s.Scope != "" {
				path = s.Scope + "." + s.Name
			}
			total++
			if inTree[FoldName(path)] {
				continue
			}
			missing++
			if len(examples) < 15 {
				examples = append(examples,
					fmt.Sprintf("%s:%d %s %s", filepath.Base(f), s.Line, s.Kind, path))
			}
		}
	}

	if missing != 0 {
		t.Fatalf("%d of %d table types/methods have no tree node at the same path:\n  %s",
			missing, total, strings.Join(examples, "\n  "))
	}
	t.Logf("%d table types/methods, all present in the tree at the same path", total)
}
