// enumerate_by_kind_pr4b_test.go — parity test for PR4b (ADR-0027 Path P,
// memory epic #5850, residual read-migration): enumerateByKind's foldCap
// early-return loop (tools.go) was converted from
//
//	for i := range r.Doc.Entities { if len(out) >= foldCap { return out }; ... }
//
// to an lr.forEachEntity yield with a captured `capReached` flag checked after
// the callback returns, re-raising the function-level `return out` outside the
// forEach call. This is exactly the class of conversion the PR4b brief flags as
// needing a parity test (early-return whose control flow crosses a repo
// boundary): the cap must stop enumeration of the CURRENT repo immediately
// (mid-scan) AND must never spill into a later repo in the `repos` slice, in
// both flag-OFF (Doc-sourced) and flag-ON (mmap Reader-sourced) forEachEntity
// paths.
package mcp

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// manyFunctionsDoc builds n "function"-kind entities (real bodies, non-noise,
// default confidence) all named fN, qualified r.fN — every one matches
// kindFilter="function" and clears the noise/confidence gates in
// enumerateByKind.
func manyFunctionsDoc(repo string, n int) *graph.Document {
	doc := &graph.Document{Version: 1, Repo: repo}
	for i := 0; i < n; i++ {
		id := repo + "::f" + itoa(i)
		doc.Entities = append(doc.Entities, graph.Entity{
			ID: id, Name: "f" + itoa(i), QualifiedName: repo + ".f" + itoa(i),
			Kind: "function", SourceFile: "f" + itoa(i) + ".go", Language: "go",
			StartLine: 1 + i, EndLine: 2 + i,
		})
	}
	return doc
}

// runEnumerateByKindCap exercises enumerateByKind over two repos: r1 carries
// more matching entities than foldCap (200, the floor for a small maxResults),
// r2 carries a handful more. It returns the repo names present in the result,
// in order, plus the total count — the assertions the caller makes are
// (a) the result is capped at exactly foldCap and (b) it contains ONLY r1
// entities (the cap must fire before r2 is ever scanned).
func runEnumerateByKindCap(t *testing.T, mmapOn bool) []scored {
	t.Helper()
	forceServeFromMMap(t, mmapOn)

	doc1 := manyFunctionsDoc("r1", 250)
	doc2 := manyFunctionsDoc("r2", 5)

	r1 := &LoadedRepo{Repo: "r1", Doc: doc1}
	r2 := &LoadedRepo{Repo: "r2", Doc: doc2}

	if mmapOn {
		wireLoadedRepoReader(t, r1, doc1)
		wireLoadedRepoReader(t, r2, doc2)
	}

	// maxResults=1 -> foldCap = max(1*4, 200) = 200.
	out := enumerateByKind(nil, []*LoadedRepo{r1, r2}, "function", false, 0, 1)
	return out
}

// wireLoadedRepoReader writes doc to a temp graph.fb, opens a real mmap Reader
// over it, and publishes the handle on lr so lr.forEachEntity's flag-ON branch
// sources rows from the mmap Reader instead of lr.Doc (mirrors the wiring in
// mmap_readermu_sigbus_test.go / view_iter_test.go).
func wireLoadedRepoReader(t *testing.T, lr *LoadedRepo, doc *graph.Document) {
	t.Helper()
	fbPath := t.TempDir() + "/graph.fb"
	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	rdr, err := fbreader.Open(fbPath)
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	h := newMapHandle(rdr)
	lr.publishHandle(h)
}

func TestEnumerateByKind_FoldCapStopsAtRepoBoundary_FlagOff(t *testing.T) {
	out := runEnumerateByKindCap(t, false)
	assertFoldCapCappedToR1(t, out)
}

func TestEnumerateByKind_FoldCapStopsAtRepoBoundary_FlagOn(t *testing.T) {
	out := runEnumerateByKindCap(t, true)
	assertFoldCapCappedToR1(t, out)
}

func assertFoldCapCappedToR1(t *testing.T, out []scored) {
	t.Helper()
	const foldCap = 200
	if len(out) != foldCap {
		t.Fatalf("enumerateByKind: got %d results, want exactly foldCap=%d", len(out), foldCap)
	}
	for i, sc := range out {
		if sc.repo.Repo != "r1" {
			t.Fatalf("result[%d]: repo=%q, want r1 only — cap must stop before r2 is scanned", i, sc.repo.Repo)
		}
	}
}

// TestEnumerateByKind_FlagOnOffParity asserts the exact same entity IDs, in the
// same order, are returned whether forEachEntity sources from lr.Doc (flag-OFF)
// or the mmap Reader (flag-ON) — the behavior-neutral guarantee PR4b requires
// for every converted site.
func TestEnumerateByKind_FlagOnOffParity(t *testing.T) {
	off := runEnumerateByKindCap(t, false)
	on := runEnumerateByKindCap(t, true)
	if len(off) != len(on) {
		t.Fatalf("flag-off yielded %d, flag-on yielded %d", len(off), len(on))
	}
	for i := range off {
		if off[i].hit.Entity.ID != on[i].hit.Entity.ID {
			t.Fatalf("result[%d]: flag-off ID=%q, flag-on ID=%q", i, off[i].hit.Entity.ID, on[i].hit.Entity.ID)
		}
	}
}
