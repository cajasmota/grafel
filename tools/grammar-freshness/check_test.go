package main

import (
	"context"
	"strings"
	"testing"
)

// fakeSource returns canned upstream state keyed by repo slug, so the compare
// logic is exercised without any network access.
type fakeSource struct {
	data map[string]Upstream
	errs map[string]error
}

func (f fakeSource) Latest(_ context.Context, repo string) (Upstream, error) {
	if err := f.errs[repo]; err != nil {
		return Upstream{}, err
	}
	return f.data[repo], nil
}

func newLock() *Lock {
	return &Lock{
		Binding: Binding{
			Module:     "github.com/smacker/go-tree-sitter",
			Version:    "v0.0.0-20240827094217-dd81d9e9be82",
			PinnedDate: "2024-08-27",
		},
		LastVerified: "2026-06-23",
		Grammars: []GrammarSpec{
			{Language: "java", Source: "tree-sitter/tree-sitter-java"},
			{Language: "frozen", Source: "example/tree-sitter-frozen"},
			{Language: "norelease", Source: "example/tree-sitter-norelease"},
			{Language: "broken", Source: "example/tree-sitter-broken"},
		},
	}
}

func TestCheck_ClassifiesStaleCurrentNoReleaseAndError(t *testing.T) {
	src := fakeSource{
		data: map[string]Upstream{
			// Stale: upstream commit well after the 2024-08-27 snapshot.
			"tree-sitter/tree-sitter-java": {Release: "v0.23.5", CommitDate: "2025-09-15", Kind: "release"},
			// Current: upstream commit predates the snapshot.
			"example/tree-sitter-frozen": {Release: "v1.0.0", CommitDate: "2022-01-01", Kind: "release"},
			// Stale but no release — fallback to commit date.
			"example/tree-sitter-norelease": {Release: "", CommitDate: "2026-01-01", Kind: "commit"},
		},
		errs: map[string]error{
			"example/tree-sitter-broken": context.DeadlineExceeded,
		},
	}

	rep := check(context.Background(), newLock(), src)

	if rep.StaleCount != 2 {
		t.Fatalf("StaleCount = %d, want 2 (java + norelease)", rep.StaleCount)
	}
	if rep.Errored != 1 {
		t.Fatalf("Errored = %d, want 1 (broken)", rep.Errored)
	}

	by := map[string]Result{}
	for _, g := range rep.Grammars {
		by[g.Language] = g
	}

	if !by["java"].Stale {
		t.Error("java should be stale")
	}
	if by["java"].Behind <= 0 {
		t.Error("java Behind should be positive")
	}
	if by["frozen"].Stale {
		t.Error("frozen should be current (upstream older than snapshot)")
	}
	if !by["norelease"].Stale {
		t.Error("norelease should be stale via commit-date fallback")
	}
	if by["norelease"].UpstreamRelease != "" {
		t.Error("norelease should carry an empty release")
	}
	if by["broken"].Err == nil {
		t.Error("broken should carry the lookup error")
	}
	if by["broken"].Stale {
		t.Error("an errored grammar must not be counted stale")
	}

	// Sort invariant: stale grammars sort ahead of errored ones.
	if rep.Grammars[len(rep.Grammars)-1].Language != "broken" {
		t.Errorf("errored grammar should sort last, got order %v", names(rep.Grammars))
	}
}

func TestMarkdown_StableAndContainsStale(t *testing.T) {
	src := fakeSource{data: map[string]Upstream{
		"tree-sitter/tree-sitter-java": {Release: "v0.23.5", CommitDate: "2025-09-15", Kind: "release"},
	}}
	lock := &Lock{
		Binding:  Binding{Module: "m", Version: "v", PinnedDate: "2024-08-27"},
		Grammars: []GrammarSpec{{Language: "java", Source: "tree-sitter/tree-sitter-java"}},
	}
	rep := check(context.Background(), lock, src)

	var sb strings.Builder
	writeMarkdown(&sb, rep)
	out := sb.String()
	for _, want := range []string{"1 of 1 grammars stale", "tree-sitter-java", "v0.23.5", "#5359"} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q\n%s", want, out)
		}
	}
}

func TestMonths(t *testing.T) {
	// 2024-08-27 -> 2025-09-15 is ~12.5 months.
	d := parseDate("2025-09-15").Sub(parseDate("2024-08-27"))
	if m := months(d); m < 12 || m > 13 {
		t.Errorf("months = %d, want ~12", m)
	}
}

func names(rs []Result) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Language
	}
	return out
}
