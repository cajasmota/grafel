package dashboard

// enrichment_ops_test.go — unit coverage for the four LLM enrichment
// operations (merge, disqualify, rank, group) across Paths, Flows, and
// Topology surfaces. Issue #1103.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/mcp"
)

// helper — write a frontmatter doc to a temp dir and return its path.
func writeFrontmatter(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// makeOps loads EnrichmentOps from a set of inline frontmatter doc files.
// The pathResolver simply joins relative names against tmpDir.
func makeOps(t *testing.T, docs map[string]string) *EnrichmentOps {
	t.Helper()
	tmp := t.TempDir()
	paths := make([]string, 0, len(docs))
	for name, body := range docs {
		writeFrontmatter(t, tmp, name, body)
		paths = append(paths, name)
	}
	now := time.Now()
	st := &mcp.DocgenState{
		LastDocgenAt:   &now,
		GeneratedPaths: paths,
	}
	resolver := func(p string) string { return filepath.Join(tmp, p) }
	return LoadEnrichmentOps(st, resolver)
}

func TestLoadEnrichmentOps_indexesAllFour(t *testing.T) {
	ops := makeOps(t, map[string]string{
		"a.md": "---\nentity_id: A\ndisqualified: true\n---\n",
		"b.md": "---\nentity_id: B\nmerged_into: C\n---\n",
		"c.md": "---\nentity_id: D\nrank: 0.9\n---\n",
		"d.md": "---\nentity_id: E\ngroup: orders\ngroup_label: Order processing\n---\n",
	})
	if !ops.IsDisqualified("A") {
		t.Errorf("A should be disqualified")
	}
	if ops.CanonicalID("B") != "C" {
		t.Errorf("B should merge into C, got %q", ops.CanonicalID("B"))
	}
	if ops.Rank("D") != 0.9 {
		t.Errorf("D rank: got %v want 0.9", ops.Rank("D"))
	}
	if ops.Group("E") != "orders" || ops.GroupLabels["orders"] != "Order processing" {
		t.Errorf("group/label mismatch: %q / %q", ops.Group("E"), ops.GroupLabels["orders"])
	}
}

func TestLoadEnrichmentOps_resolvesMergeChain(t *testing.T) {
	ops := makeOps(t, map[string]string{
		"a.md": "---\nentity_id: A\nmerged_into: B\n---\n",
		"b.md": "---\nentity_id: B\nmerged_into: C\n---\n",
		"c.md": "---\nentity_id: C\n---\n",
	})
	if got := ops.CanonicalID("A"); got != "C" {
		t.Errorf("A→B→C chain not resolved: got %q want C", got)
	}
}

func TestApplyToEntries_disqualifySplits(t *testing.T) {
	ops := makeOps(t, map[string]string{
		"a.md": "---\nentity_id: t1\ndisqualified: true\n---\n",
	})
	entries := []map[string]any{
		{"id": "t1", "label": "first"},
		{"id": "t2", "label": "second"},
	}
	kept, rejected, _, _ := ops.ApplyToEntries(entries)
	if len(kept) != 1 || EntryIDOf(kept[0]) != "t2" {
		t.Errorf("kept = %+v", kept)
	}
	if len(rejected) != 1 || EntryIDOf(rejected[0]) != "t1" {
		t.Errorf("rejected = %+v", rejected)
	}
	if !rejected[0]["disqualified"].(bool) {
		t.Errorf("rejected entry missing disqualified=true")
	}
}

func TestApplyToEntries_mergeCollapses(t *testing.T) {
	ops := makeOps(t, map[string]string{
		"a.md": "---\nentity_id: dup\nmerged_into: canon\n---\n",
	})
	entries := []map[string]any{
		{"id": "dup", "label": "duplicate"},
		{"id": "canon", "label": "canonical"},
	}
	kept, _, aliases, _ := ops.ApplyToEntries(entries)
	if len(kept) != 1 || EntryIDOf(kept[0]) != "canon" {
		t.Fatalf("kept = %+v", kept)
	}
	if aliases["dup"] != "canon" {
		t.Errorf("aliases = %+v", aliases)
	}
	alist, _ := kept[0]["aliases"].([]string)
	if len(alist) != 1 || alist[0] != "dup" {
		t.Errorf("canonical aliases field = %+v", alist)
	}
}

func TestApplyToEntries_mergeWithoutTargetMarksMergedInto(t *testing.T) {
	ops := makeOps(t, map[string]string{
		"a.md": "---\nentity_id: lonely\nmerged_into: not-in-surface\n---\n",
	})
	entries := []map[string]any{{"id": "lonely", "label": "x"}}
	kept, _, _, _ := ops.ApplyToEntries(entries)
	if len(kept) != 1 {
		t.Fatalf("kept = %+v", kept)
	}
	if kept[0]["merged_into"] != "not-in-surface" {
		t.Errorf("expected merged_into hint when target absent; got %+v", kept[0])
	}
}

func TestApplyToEntries_rankSortsDesc(t *testing.T) {
	ops := makeOps(t, map[string]string{
		"a.md": "---\nentity_id: low\nrank: 0.1\n---\n",
		"b.md": "---\nentity_id: high\nrank: 0.9\n---\n",
	})
	entries := []map[string]any{
		{"id": "low"},
		{"id": "mid"},
		{"id": "high"},
	}
	kept, _, _, _ := ops.ApplyToEntries(entries)
	if EntryIDOf(kept[0]) != "high" {
		t.Errorf("rank sort failed: kept order = %+v", kept)
	}
}

func TestApplyToEntries_groupSummary(t *testing.T) {
	ops := makeOps(t, map[string]string{
		"a.md": "---\nentity_id: a\ngroup: orders\ngroup_label: Order processing\n---\n",
		"b.md": "---\nentity_id: b\ngroup: orders\n---\n",
		"c.md": "---\nentity_id: c\ngroup: billing\n---\n",
	})
	entries := []map[string]any{
		{"id": "a"}, {"id": "b"}, {"id": "c"}, {"id": "d"},
	}
	_, _, _, groups := ops.ApplyToEntries(entries)
	if len(groups) != 2 {
		t.Fatalf("group summary len = %d want 2: %+v", len(groups), groups)
	}
	if groups[0].Group != "orders" || groups[0].Count != 2 || groups[0].Label != "Order processing" {
		t.Errorf("top group = %+v", groups[0])
	}
	if groups[1].Group != "billing" || groups[1].Count != 1 {
		t.Errorf("second group = %+v", groups[1])
	}
}

func TestApplyPathEnrichment_allFourOps(t *testing.T) {
	ops := makeOps(t, map[string]string{
		"a.md": "---\nentity_id: hash-dup\nmerged_into: hash-canon\n---\n",
		"b.md": "---\nentity_id: hash-bad\ndisqualified: true\n---\n",
		"c.md": "---\nentity_id: hash-top\nrank: 0.95\n---\n",
		"d.md": "---\nentity_id: hash-grouped\ngroup: gw\ngroup_label: Gateway\n---\n",
	})
	rows := []PathRow{
		{PathHash: "hash-canon", Path: "/c"},
		{PathHash: "hash-dup", Path: "/d"},
		{PathHash: "hash-bad", Path: "/b"},
		{PathHash: "hash-top", Path: "/t"},
		{PathHash: "hash-grouped", Path: "/g"},
		{PathHash: "hash-plain", Path: "/p"},
	}
	kept, rejected := applyPathEnrichment(rows, ops)
	if len(rejected) != 1 || rejected[0].PathHash != "hash-bad" || !rejected[0].Disqualified {
		t.Errorf("rejected = %+v", rejected)
	}
	// hash-top should be first (highest rank).
	if kept[0].PathHash != "hash-top" || kept[0].Rank != 0.95 {
		t.Errorf("rank-promoted row not first: %+v", kept)
	}
	// dup should be collapsed into canon with aliases.
	var canon *PathRow
	for i := range kept {
		if kept[i].PathHash == "hash-canon" {
			canon = &kept[i]
		}
	}
	if canon == nil || len(canon.Aliases) != 1 || canon.Aliases[0] != "hash-dup" {
		t.Errorf("merge alias missing on canon: %+v", canon)
	}
	// grouped row should carry group + label.
	for _, r := range kept {
		if r.PathHash == "hash-grouped" {
			if r.Group != "gw" || r.GroupLabel != "Gateway" {
				t.Errorf("group fields not propagated: %+v", r)
			}
		}
	}
	// dup must be gone from kept.
	for _, r := range kept {
		if r.PathHash == "hash-dup" {
			t.Errorf("dup row should have been collapsed: %+v", kept)
		}
	}
}

func TestApplyPathEnrichment_nilOpsIsNoOp(t *testing.T) {
	rows := []PathRow{{PathHash: "x"}, {PathHash: "y"}}
	kept, rejected := applyPathEnrichment(rows, nil)
	if len(kept) != 2 || len(rejected) != 0 {
		t.Errorf("nil ops should be no-op, got kept=%d rejected=%d", len(kept), len(rejected))
	}
}

func TestMatchesEntity_suffixMatch(t *testing.T) {
	if !MatchesEntity("local", "repo:local") {
		t.Errorf("repo-prefixed surface id should match bare frontmatter id")
	}
	if !MatchesEntity("repo:local", "local") {
		t.Errorf("reverse should also match")
	}
	if MatchesEntity("a", "b") {
		t.Errorf("unrelated ids should not match")
	}
}

func TestSummarizeGroups_orderedByCountThenName(t *testing.T) {
	ops := makeOps(t, map[string]string{
		"a.md": "---\nentity_id: 1\ngroup: alpha\n---\n",
		"b.md": "---\nentity_id: 2\ngroup: alpha\n---\n",
		"c.md": "---\nentity_id: 3\ngroup: beta\n---\n",
		"d.md": "---\nentity_id: 4\ngroup: gamma\n---\n",
	})
	out := ops.SummarizeGroups([]string{"1", "2", "3", "4"})
	if len(out) != 3 {
		t.Fatalf("len=%d", len(out))
	}
	if out[0].Group != "alpha" || out[0].Count != 2 {
		t.Errorf("first should be alpha:2, got %+v", out[0])
	}
	// beta < gamma alphabetically, both count=1.
	if out[1].Group != "beta" || out[2].Group != "gamma" {
		t.Errorf("tie-break failed: %+v", out)
	}
}
