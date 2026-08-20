package vbnet_test

// The real-tree gate for S7c of #6327: member-level `Implements IFoo.Bar`.
//
//	GRAFEL_VBNET_CORPUS=$HOME/Projects/archigraph-corpora \
//	  go test ./internal/extractors/vbnet/ -run CorpusMemberImplements -v
//
// Same contract as corpus_references_6327_test.go. The denominator — how many
// `Implements` clauses EXIST — is counted from the raw text with comments and
// literals stripped, never from the parse tree, because a tree-derived
// denominator makes the parser its own judge and hides the files it failed on.
//
// The measurement it exists to publish is the resolution one. A member-level
// ToID names an OPERATION (`IFrameServer.GetFrame`), while
// internal/resolve/refs.go:2028 routes EXTENDS/IMPLEMENTS to
// componentKindFamily and refs.go:3526 deliberately keeps the package-scoped
// fallback Component-only. S7c does not touch internal/resolve, so whatever
// this prints is the honest disposition of the new edges, not a target.

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/cajasmota/grafel/internal/vbnet"
)

// rawImplements finds the keyword as a standalone token. It counts CLAUSES,
// not targets: one clause may name several interfaces or several members.
var rawImplements = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_.\[])implements($|[^A-Za-z0-9_\]])`)

func TestCorpusMemberImplementsCoverage_6327(t *testing.T) {
	files := corpusFiles(t)

	var raw, clausesType, clausesMember, namesType, namesMember int
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		raw += len(rawImplements.FindAllStringIndex(stripCommentsAndLiterals(string(src)), -1))

		vbnet.Parse(string(src)).File.Walk(func(n *vbnet.Node) {
			if len(n.Implements) == 0 {
				return
			}
			if n.Kind.IsType() {
				clausesType++
				namesType += len(n.Implements)
				return
			}
			clausesMember++
			namesMember += len(n.Implements)
		})
	}

	t.Logf("RAW `Implements` clauses over %d .vb files: %d", len(files), raw)
	t.Logf("PARSED clauses: %d type-level (%d names), %d member-level (%d names)",
		clausesType, namesType, clausesMember, namesMember)

	// The whole point of S7c: the form that produced no edge was the COMMON
	// one. If that ever inverts the slice's justification has changed.
	if clausesMember <= clausesType {
		t.Errorf("member-level clauses (%d) no longer outnumber type-level ones (%d) — "+
			"re-read memberImplementsEdges' justification before trusting it", clausesMember, clausesType)
	}

	g := buildCorpus(t)
	total := map[string]int{}
	resolved := map[string]int{}
	unresolved := map[string]int{}
	var samples []string
	for i := range g.recs {
		rec := &g.recs[i]
		for _, r := range rec.Relationships {
			if r.Kind != "IMPLEMENTS" {
				continue
			}
			via := r.Properties.Get("via")
			total[via]++
			if isHex16(r.ToID) {
				resolved[via]++
				if via == "implements-member" && len(samples) < 14 {
					samples = append(samples, fmt.Sprintf("     FROM %s\n     TO   %s",
						g.describe(r.FromID), g.describe(r.ToID)))
				}
			} else if via == "implements-member" {
				unresolved[r.ToID]++
			}
			if via != "implements-member" {
				continue
			}
			// The #6295/#6298/#6365/#6367 defect class, behaviourally.
			if r.FromID == "" {
				t.Errorf("member IMPLEMENTS->%s has no FROM endpoint", r.ToID)
			}
			from, ok := g.byID[r.FromID]
			if !ok || from.Subtype == "file" {
				t.Errorf("member IMPLEMENTS->%s anchors on a file carrier, not the "+
					"owning member", r.ToID)
			}
			// It must never land on the TYPE — that is the edge it would then
			// genuinely duplicate.
			if ok && from.Kind == "SCOPE.Component" {
				t.Errorf("member IMPLEMENTS->%s anchored on the TYPE %q; it must "+
					"anchor on the member", r.ToID, from.Name)
			}
		}
	}
	t.Log("RESOLVED member-level IMPLEMENTS edges, both endpoints (a COUNT " +
		"proves nothing about whether the bind is RIGHT — #6369 is the " +
		"standing example):")
	for _, sm := range samples {
		t.Log(sm)
	}
	t.Logf("IMPLEMENTS edges by via:")
	for _, v := range []string{"", "implements-member"} {
		label := v
		if label == "" {
			label = "(type-level)"
		}
		t.Logf("  %-18s total=%-5d resolved=%d", label, total[v], resolved[v])
	}
	t.Logf("UNRESOLVED member targets (%d distinct, top 30):", len(unresolved))
	for i, name := range sortedByCount(unresolved) {
		if i >= 30 {
			break
		}
		t.Logf("    %4d  %s", unresolved[name], name)
	}

	if total["implements-member"] == 0 {
		t.Fatal("vacuous: no member-level IMPLEMENTS edge in the whole corpus")
	}
	// A floor, not a rate: growing the corpus may only ever make this harder.
	if total["implements-member"] < 130 {
		t.Errorf("member-level IMPLEMENTS edges fell to %d, below the 130 floor "+
			"measured for S7c (%d clauses naming %d members were parsed)",
			total["implements-member"], clausesMember, namesMember)
	}
	if raw < clausesType+clausesMember {
		t.Errorf("parsed %d clauses from %d raw keyword tokens — the parser "+
			"invented clauses, so the denominator is wrong",
			clausesType+clausesMember, raw)
	}
}
