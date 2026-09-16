package main

import (
	"sort"
	"testing"
)

// TestProbeCsharpSurface is a temporary diagnostic, not an assertion.
func TestProbeCsharpSurface(t *testing.T) {
	root := modRoot(t)
	res := evalPkg(t, root, "./internal/extractors/csharp", nil, emptyBaseline(t))
	byForm := map[string]int{}
	files := map[string]bool{}
	for _, s := range res.Sites {
		byForm[s.Form]++
		files[s.File] = true
	}
	var fs []string
	for f := range files {
		fs = append(fs, f)
	}
	sort.Strings(fs)
	t.Logf("sites=%d byForm=%v resolved=%d sinks=%d", len(res.Sites), byForm, res.Resolved, len(res.Sinks))
	t.Logf("sinks=%v", res.Sinks)
	t.Logf("files=%d %v", len(fs), fs)
	t.Logf("for_each_statement sites=%v", sitesFor(res, "for_each_statement"))
}
