package vbnet_test

// The real-tree gate for S5 of #6327 (AGENTS.md "Evidence").
//
// S4's corpus gate measured STRUCTURE — how many files parse. This one measures
// what the edges RESOLVE TO, which is the thing that actually matters: #6369 is
// the standing example of edges that exist, resolve, and point at the wrong
// node. So every assertion below is about (FROM endpoint, TO endpoint) pairs
// after the real Path-B resolution chain, not about edge counts.
//
// Point it at a tree with:
//
//	GRAFEL_VBNET_CORPUS=$HOME/Projects/archigraph-corpora \
//	  go test ./internal/extractors/vbnet/ -run Corpus -v
//
// It SKIPS when the variable is unset, so it is a gate where a corpus exists
// and never a false green where one does not.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// corpusMinFiles mirrors internal/vbnet's gate: a rate has two failure modes
// that both make a gate WEAKER as the corpus grows, so the denominator is
// pinned independently. Measured: 302 .vb files across WakeOnLAN, StaxRip and
// display-drivers-uninstaller.
const corpusMinFiles = 300

const corpusRepoTag = "vbnet-corpus"

func corpusFiles(t *testing.T) []string {
	t.Helper()
	root := os.Getenv("GRAFEL_VBNET_CORPUS")
	if root == "" {
		t.Skip("GRAFEL_VBNET_CORPUS unset: no real VB.NET tree to measure against (#6363)")
	}
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".vb") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(out) < corpusMinFiles {
		t.Fatalf("GRAFEL_VBNET_CORPUS=%s yields %d .vb files, want at least %d",
			root, len(out), corpusMinFiles)
	}
	sort.Strings(out)
	return out
}

// corpusGraph is the resolved corpus: records after the Path-B chain, plus an
// id index so an edge endpoint can be reported as the node it landed on.
type corpusGraph struct {
	recs  []types.EntityRecord
	byID  map[string]*types.EntityRecord
	files int
	stats resolve.Stats
}

// buildCorpus drives the chain the CLI indexer runs:
//
//	extract -> graph.EntityID -> BuildImportTable -> ResolveImports
//	        -> BuildIndex -> ReferencesEmbedded -> assembly FromID stamping
//
// The FromID stamping is last on purpose: ReferencesEmbedded leaves an EMPTY
// FromID alone (refs.go, ReferencesEmbeddedWithAllowlist), and assembly then
// substitutes the owning record's id. Doing it in the other order is precisely
// the #6295 defect.
func buildCorpus(t *testing.T) *corpusGraph {
	t.Helper()
	files := corpusFiles(t)
	ext, ok := extractor.Get("vbnet")
	if !ok {
		t.Fatal("vbnet extractor not registered")
	}

	var recs []types.EntityRecord
	root := os.Getenv("GRAFEL_VBNET_CORPUS")
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		rel, err := filepath.Rel(root, f)
		if err != nil {
			rel = f
		}
		rel = filepath.ToSlash(rel)
		ents, err := ext.Extract(t.Context(), extractor.FileInput{
			Path: rel, Content: src, Language: "vbnet",
		})
		if err != nil {
			t.Fatalf("Extract(%s): %v", rel, err)
		}
		recs = append(recs, ents...)
	}
	for i := range recs {
		recs[i].ID = graph.EntityID(corpusRepoTag, recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
	resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
	stats := resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))

	byID := make(map[string]*types.EntityRecord, len(recs))
	for i := range recs {
		byID[recs[i].ID] = &recs[i]
	}
	// Graph assembly: substitute the owner's id wherever FromID is empty.
	for i := range recs {
		for j := range recs[i].Relationships {
			if recs[i].Relationships[j].FromID == "" {
				recs[i].Relationships[j].FromID = recs[i].ID
			}
		}
	}
	return &corpusGraph{recs: recs, byID: byID, files: len(files), stats: stats}
}

// describe renders an endpoint as the node it landed on, or as the raw stub.
func (g *corpusGraph) describe(id string) string {
	if r, ok := g.byID[id]; ok {
		return fmt.Sprintf("%s/%s %q @%s:%d", r.Kind, r.Subtype, r.Name, r.SourceFile, r.StartLine)
	}
	return "UNRESOLVED " + strconv.Quote(id)
}

// TestCorpusEdgeResolution is the measurement. It prints the whole picture and
// asserts only the invariants that must not regress, so a change that moves a
// number reports the number rather than only failing.
func TestCorpusEdgeResolution(t *testing.T) {
	g := buildCorpus(t)

	type bucket struct {
		total, resolved int
		unresolvedTo    map[string]int
	}
	byKind := map[string]*bucket{}
	get := func(k string) *bucket {
		b := byKind[k]
		if b == nil {
			b = &bucket{unresolvedTo: map[string]int{}}
			byKind[k] = b
		}
		return b
	}

	// FROM-endpoint audit: a hierarchy edge whose FROM is the file carrier is
	// the #6295 misanchoring, and it is invisible to an edge count.
	fileAnchoredHierarchy := 0
	var samples []string

	for i := range g.recs {
		for _, r := range g.recs[i].Relationships {
			b := get(r.Kind)
			b.total++
			if isHex16(r.ToID) {
				b.resolved++
			} else {
				b.unresolvedTo[r.ToID]++
			}
			if r.Kind == "EXTENDS" || r.Kind == "IMPLEMENTS" {
				from, ok := g.byID[r.FromID]
				if !ok || from.Subtype == "file" {
					fileAnchoredHierarchy++
				}
				if len(samples) < 12 && isHex16(r.ToID) {
					samples = append(samples, fmt.Sprintf("  %-10s FROM %s\n             TO   %s",
						r.Kind, g.describe(r.FromID), g.describe(r.ToID)))
				}
			}
		}
	}

	t.Logf("MEASURED over %d .vb files, %d entity records", g.files, len(g.recs))
	for _, k := range sortedKeys(byKind) {
		b := byKind[k]
		t.Logf("  %-9s total=%-6d resolved=%-6d unresolved=%d",
			k, b.total, b.resolved, b.total-b.resolved)
	}

	// The disposition split is the second silent-failure trap #6327 names:
	// without the lang=="vbnet" arm in classifyDispositionLang every framework
	// base type lands in bug-extractor and understates our own quality numbers.
	t.Logf("DISPOSITIONS (bug rate %.4f):", g.stats.BugRate)
	for _, d := range resolve.AllDispositions {
		if n := g.stats.DispositionCounts[d]; n > 0 {
			t.Logf("  %-18s %d", d, n)
		}
	}

	t.Log("RESOLVED hierarchy edges, FROM and TO endpoints:")
	for _, s := range samples {
		t.Log(s)
	}

	for _, k := range []string{"EXTENDS", "IMPLEMENTS"} {
		b := get(k)
		t.Logf("UNRESOLVED %s targets (%d distinct):", k, len(b.unresolvedTo))
		for _, name := range sortedByCount(b.unresolvedTo) {
			t.Logf("    %4d  %s", b.unresolvedTo[name], name)
		}
	}

	// The invariant, not the number: a hierarchy edge must never anchor on the
	// file. #6295 / #6298 / #6365 / #6367 are the same defect in four other
	// languages.
	if fileAnchoredHierarchy != 0 {
		t.Errorf("%d EXTENDS/IMPLEMENTS edges anchor on a file carrier or on no "+
			"node at all; every one must anchor on the declaring TYPE (#6295/#6298)",
			fileAnchoredHierarchy)
	}
	if get("EXTENDS").total == 0 || get("IMPLEMENTS").total == 0 || get("CALLS").total == 0 {
		t.Fatalf("a whole edge kind is missing, so this measurement is vacuous: %v", byKind)
	}
}

func isHex16(s string) bool {
	if len(s) != 16 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedByCount(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if m[out[i]] != m[out[j]] {
			return m[out[i]] > m[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}
