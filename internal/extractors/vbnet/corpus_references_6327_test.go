package vbnet_test

// The real-tree gate for S7b of #6327: `Handles` and `AddressOf` -> REFERENCES.
//
// Same contract as corpus_6327_test.go — point it at a tree and it measures,
// skip it and it never greens falsely:
//
//	GRAFEL_VBNET_CORPUS=$HOME/Projects/archigraph-corpora \
//	  go test ./internal/extractors/vbnet/ -run CorpusReferences -v
//
// It measures three numbers that a fixture cannot: how many clauses and
// operands EXIST in real source, how many became an edge, and what the
// residual is. The claim S7b makes is a totality claim over the first number,
// so the first number is counted from the raw text and NOT from the parse
// tree — counting from the tree would make the parser its own judge and hide
// exactly the files it failed to parse.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/vbnet"
)

// rawHandles / rawAddressOf find the two keywords as standalone tokens in
// source that has had comments and string literals removed, which is the only
// honest denominator: `' AddressOf Foo` in a comment is not an operand, and
// counting it would understate coverage against a target that was never real.
var (
	rawHandles   = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_.\[])handles($|[^A-Za-z0-9_\]])`)
	rawAddressOf = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_.\[])addressof($|[^A-Za-z0-9_\]])`)
)

// stripCommentsAndLiterals blanks ' comments and "..." literals, preserving
// length so nothing else shifts. It is deliberately independent of
// internal/vbnet's own stripper: a denominator computed with the code under
// test is not a denominator.
func stripCommentsAndLiterals(src string) string {
	out := []byte(src)
	inStr := false
	for i := 0; i < len(out); i++ {
		switch {
		case out[i] == '\n':
			inStr = false
		case inStr:
			if out[i] == '"' {
				inStr = false
			} else {
				out[i] = ' '
			}
		case out[i] == '"':
			inStr = true
		case out[i] == '\'':
			for ; i < len(out) && out[i] != '\n'; i++ {
				out[i] = ' '
			}
		}
	}
	return string(out)
}

func TestCorpusReferencesCoverage(t *testing.T) {
	files := corpusFiles(t)
	root := os.Getenv("GRAFEL_VBNET_CORPUS")

	var rawH, rawA, parsedH, parsedA int
	perFileMiss := map[string]int{}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		clean := stripCommentsAndLiterals(string(src))
		h := len(rawHandles.FindAllStringIndex(clean, -1))
		a := len(rawAddressOf.FindAllStringIndex(clean, -1))
		rawH += h
		rawA += a

		res := vbnet.Parse(string(src))
		var ph, pa int
		res.File.Walk(func(n *vbnet.Node) {
			ph += len(n.Handles)
			pa += len(n.AddressOfs)
		})
		parsedH += ph
		parsedA += pa

		rel, err := filepath.Rel(root, f)
		if err != nil {
			rel = f
		}
		// A Handles CLAUSE may name several targets, so parsed >= raw is
		// normal and only a SHORTFALL is a miss.
		if miss := (h - ph) + (a - pa); miss > 0 {
			perFileMiss[filepath.ToSlash(rel)] = miss
		}
	}

	t.Logf("RAW keyword tokens over %d .vb files (comments and literals stripped):", len(files))
	t.Logf("  Handles   %d", rawH)
	t.Logf("  AddressOf %d", rawA)
	t.Logf("PARSED:")
	t.Logf("  Handles targets    %d (a clause may name several)", parsedH)
	t.Logf("  AddressOf operands %d", parsedA)

	if len(perFileMiss) > 0 {
		t.Logf("FILES WITH A SHORTFALL (%d):", len(perFileMiss))
		for _, f := range sortedByCount(perFileMiss) {
			t.Logf("    %4d  %s", perFileMiss[f], f)
		}
	}

	// Now the edge half, through the same Path-B chain the CLI runs.
	g := buildCorpus(t)
	var total, resolved int
	unresolved := map[string]int{}
	dotted, bare := 0, 0
	var samples []string
	byVia := map[string]int{}
	resolvedByVia := map[string]int{}
	for i := range g.recs {
		for _, r := range g.recs[i].Relationships {
			if r.Kind != "REFERENCES" {
				continue
			}
			total++
			via := r.Properties.Get("via")
			byVia[via]++
			if strings.Contains(r.ToID, ".") && !isHex16(r.ToID) {
				dotted++
			} else if !isHex16(r.ToID) {
				bare++
			}
			if isHex16(r.ToID) {
				resolved++
				resolvedByVia[via]++
				if len(samples) < 14 {
					ev := r.Properties.Get("event")
					if ev != "" {
						ev = " event=" + ev
					}
					samples = append(samples, fmt.Sprintf("  %-9s%s\n     FROM %s\n     TO   %s",
						via, ev, g.describe(r.FromID), g.describe(r.ToID)))
				}
			} else {
				unresolved[r.ToID]++
			}
			if r.FromID == "" {
				t.Errorf("REFERENCES->%s has no FROM endpoint", r.ToID)
			}
			if from, ok := g.byID[r.FromID]; !ok || from.Subtype == "file" {
				t.Errorf("REFERENCES->%s anchors on a file carrier, not the owning "+
					"member (#6295 defect class)", r.ToID)
			}
		}
	}
	t.Logf("REFERENCES edges: total=%d resolved=%d unresolved=%d "+
		"(unresolved shape: %d dotted, %d bare)", total, resolved, total-resolved, dotted, bare)
	t.Log("RESOLVED REFERENCES edges, FROM and TO endpoints (a COUNT proves " +
		"nothing about whether the bind is right — #6369 is the standing example):")
	for _, sm := range samples {
		t.Log(sm)
	}
	for _, v := range sortedKeys(byVia) {
		t.Logf("  via=%-9s total=%-5d resolved=%d", v, byVia[v], resolvedByVia[v])
	}
	// The residual is not one thing, and a single number hides that. Split it
	// by whether the graph HOLDS a node under the target's name: if it does,
	// the extractor named the target correctly and the lookup declined (the
	// name is declared in several files, so binding either one would be a
	// guess — StaxRip declares `Public Class EncoderParams` three times). If
	// it does not, the target is qualified by something outside the enclosing
	// type — a module, a local, `My.Forms.Explorer` — which is the same
	// unnameable receiver callTarget refuses for a qualified call site.
	byName := map[string]bool{}
	for i := range g.recs {
		byName[g.recs[i].Name] = true
	}
	ambiguous, foreign := 0, 0
	for name, n := range unresolved {
		if byName[name] {
			ambiguous += n
		} else {
			foreign += n
		}
	}
	t.Logf("RESIDUAL %d: %d name a node the graph HOLDS (declined as ambiguous), "+
		"%d name nothing in-tree (foreign qualifier or external member)",
		total-resolved, ambiguous, foreign)
	t.Logf("UNRESOLVED REFERENCES targets (%d distinct, top 40):", len(unresolved))
	for i, name := range sortedByCount(unresolved) {
		if i >= 40 {
			break
		}
		t.Logf("    %4d  %s", unresolved[name], name)
	}

	// Invariants, not numbers. Both constructs must be represented — a zero on
	// either half makes the whole measurement vacuous — and the edge count must
	// account for every operand, since AddressOf targets are deduped per owner
	// but never dropped.
	if parsedH == 0 || parsedA == 0 {
		t.Fatalf("vacuous measurement: parsed %d Handles targets and %d AddressOf operands",
			parsedH, parsedA)
	}
	if total == 0 {
		t.Fatal("no REFERENCES edges at all: S7b emitted nothing on real source")
	}
	// Deduplication is per owning record, so total <= parsed. A total ABOVE
	// the parsed count would mean an edge with no construct behind it.
	if total > parsedH+parsedA {
		t.Errorf("%d REFERENCES edges from only %d parsed constructs: an edge has no source",
			total, parsedH+parsedA)
	}
}
