package main

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

func TestNameMatches(t *testing.T) {
	cases := []struct {
		entity string
		truth  string
		want   bool
	}{
		{"FuseRRF", "FuseRRF", true},
		{"httpBackend.Embed", "Embed", true},
		{"(*Store).Save", "Save", true},
		{"TestEmbed -> Embed", "Embed", true},
		{"Other", "Embed", false},
		{"EmbedSomething", "Embed", false}, // not a suffix on a "." boundary
	}
	for _, c := range cases {
		if got := nameMatches(c.entity, c.truth); got != c.want {
			t.Errorf("nameMatches(%q, %q) = %v, want %v", c.entity, c.truth, got, c.want)
		}
	}
}

func TestMatchesAny(t *testing.T) {
	e := &graph.Entity{Name: "httpBackend.Embed", SourceFile: "internal/embed/http.go"}
	truths := []queryMatcher{
		{SourceFile: "internal/embed/http.go", Name: "Embed"},
	}
	if !matchesAny(e, truths) {
		t.Fatalf("expected match for %+v", e)
	}
	// Wrong file: no match.
	e2 := &graph.Entity{Name: "httpBackend.Embed", SourceFile: "internal/other/x.go"}
	if matchesAny(e2, truths) {
		t.Fatalf("expected no match (wrong file)")
	}
}

func TestScoreHits(t *testing.T) {
	hits := []hitRow{
		{rank: 1, matched: false},
		{rank: 2, matched: true},
		{rank: 3, matched: false},
		{rank: 4, matched: true},
		{rank: 11, matched: true}, // out of @10 window
	}
	oc := scoreHits(hits, 3)
	// rank 2 and 4 are in top-5; rank 11 isn't in top-10.
	if oc.recallAt5 != 2.0/3.0 {
		t.Errorf("recall@5 = %v want 2/3", oc.recallAt5)
	}
	if oc.recallAt10 != 2.0/3.0 {
		t.Errorf("recall@10 = %v want 2/3", oc.recallAt10)
	}
	if oc.mrr != 0.5 {
		t.Errorf("mrr = %v want 0.5", oc.mrr)
	}
}

func TestScoreHitsClampedToTruthCount(t *testing.T) {
	// 4 matched entities but only 2 distinct truth slots → recall capped at 1.0.
	hits := []hitRow{
		{rank: 1, matched: true},
		{rank: 2, matched: true},
		{rank: 3, matched: true},
		{rank: 4, matched: true},
	}
	oc := scoreHits(hits, 2)
	if oc.recallAt5 != 1.0 {
		t.Errorf("recall@5 = %v want 1.0 (clamped)", oc.recallAt5)
	}
}

func TestParseBackends(t *testing.T) {
	specs, err := parseBackends("bm25,builtin,http:http://127.0.0.1:11434/v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 3 {
		t.Fatalf("want 3 specs, got %d", len(specs))
	}
	if specs[0].kind != "bm25" || specs[1].kind != "builtin" || specs[2].kind != "http" {
		t.Fatalf("bad kinds: %+v", specs)
	}
	if specs[2].url != "http://127.0.0.1:11434/v1" {
		t.Fatalf("bad url: %q", specs[2].url)
	}
}

func TestParseBackendsRejectsUnknown(t *testing.T) {
	if _, err := parseBackends("bm25,nonsense"); err == nil {
		t.Fatal("expected error for unknown backend spec")
	}
}
