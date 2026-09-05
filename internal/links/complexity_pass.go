// Universal cyclomatic-complexity enrichment pass (#4831, epic #4820).
//
// Part (a) (#4821, PR #4833) computed cyclomatic_complexity / branch_count and
// stamped them — but ONLY on handler entities the data-flow pass happened to
// bind. That left the vast majority of functions/methods/operations without a
// persisted complexity number. This pass generalises the stamp: it walks EVERY
// function-like entity that carries readable source-line info (StartLine/
// EndLine + an on-disk source file) and stamps the same two properties, reusing
// the validated substrate.ComputeFunctionComplexity (correct across Python /
// JS-TS / Go / Rust / Kotlin / Scala / C# / PHP / Swift / Ruby; degenerate but
// harmless elsewhere).
//
// Single source of truth: this pass is the canonical complexity stamper. It is
// idempotent — it never overwrites a value already present (e.g. one the
// data-flow pass stamped earlier in the run for a bound handler), so the two
// paths can never diverge or double-count. New runs converge on identical
// numbers because both call the same ComputeFunctionComplexity.
//
// Why a dedicated pass (mirrors pure_function_pass.go) instead of inlining into
// data-flow: complexity is a per-function fact independent of whether the
// data-flow pass could bind the entity, so it belongs on a pass that visits
// every function-like entity, positive and negative.
package links

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/cajasmota/grafel/internal/substrate"

	"github.com/cajasmota/grafel/internal/types"
)

// MethodComplexity identifies this pass in telemetry.
const MethodComplexity = "complexity"

// runComplexityPass walks every function-like entity with readable source-line
// info and stamps cyclomatic_complexity / branch_count (idempotently). Stamping
// happens in-memory on the graphs slice; there is no sidecar — the numbers live
// on the entity Properties and are queryable directly off the graph (the
// on-demand effects MCP facet remains the richer per-effect surface).
func runComplexityPass(graphs []repoGraph, _ Paths) (PassResult, error) {
	res := PassResult{Pass: MethodComplexity}
	stamped := 0

	for ri := range graphs {
		g := &graphs[ri]

		// Resolve the on-disk source root once per repo, mirroring the data-flow
		// pass: the indexed source path if registered, else the graph's FileRoot.
		srcRoot := repoSourcePathFor(g.Repo)
		if srcRoot == "" {
			srcRoot = g.FileRoot
		}

		// File-content cache so a file with many function entities is read once.
		cache := map[string]string{}
		readFile := func(rel string) string {
			if c, ok := cache[rel]; ok {
				return c
			}
			if srcRoot == "" || rel == "" {
				cache[rel] = ""
				return ""
			}
			buf, err := readSourceFile(filepath.Join(srcRoot, rel), maxSourceFileBytes)
			if err != nil {
				cache[rel] = ""
				return ""
			}
			cache[rel] = string(buf)
			return string(buf)
		}

		// Deterministic order so re-runs are stable.
		idx := make([]int, 0, len(g.Entities))
		for ei := range g.Entities {
			idx = append(idx, ei)
		}
		sort.Slice(idx, func(a, b int) bool { return g.Entities[idx[a]].ID < g.Entities[idx[b]].ID })

		for _, ei := range idx {
			e := &g.Entities[ei]
			if !isFunctionLikeKind(e.Kind) {
				continue
			}
			if e.StartLine <= 0 || e.SourceFile == "" {
				continue // no readable source window — honest skip
			}
			res.Candidates++
			if e.Properties == nil {
				e.Properties = types.Props{}
			}
			// Idempotent: if a prior path (e.g. the data-flow pass for a bound
			// handler) already stamped complexity, leave it — both paths call the
			// same ComputeFunctionComplexity, so the value is identical anyway.
			if _, ok := e.Properties.Lookup(ComplexityPropertyKeyCyclomatic); ok {
				continue
			}
			content := readFile(e.SourceFile)
			if content == "" {
				continue
			}
			win := functionSourceWindow(content, e.StartLine, e.EndLine)
			if win == "" {
				continue
			}
			cx := substrate.ComputeFunctionComplexity(win)
			e.Properties.Set(ComplexityPropertyKeyCyclomatic, fmt.Sprintf("%d", cx.Cyclomatic))
			e.Properties.Set(ComplexityPropertyKeyBranchCount, fmt.Sprintf("%d", cx.BranchCount))
			stamped++
		}
	}

	res.LinksAdded = stamped
	return res, nil
}
