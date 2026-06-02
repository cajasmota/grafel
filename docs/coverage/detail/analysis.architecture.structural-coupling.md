<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `analysis.architecture.structural-coupling` — Structural coupling metrics (Ca/Ce/instability)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** App Topology & Integration
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-06-02` | — | `cmd/archigraph/index.go`<br>`internal/engine/structural_coupling.go`<br>`internal/engine/structural_coupling_test.go` | #3634 (epic #3625): restored the previously-orphaned coupling_score enricher (internal/enrichers/coupling_score_enricher.go, imported by zero prod code) as a LIVE engine pass, engine.ApplyStructuralCoupling, registered in cmd/archigraph/index.go as Pass 8.6 (PassCoupling, --skip-pass=coupling) AFTER module-agg (Pass 8). Consumes the Module->Module DEPENDS_ON edges materialized by internal/module.Aggregate (no re-parse). For every Module node it stamps Properties ca (afferent coupling = incoming DEPENDS_ON), ce (efferent coupling = outgoing DEPENDS_ON), instability = Ce/(Ca+Ce) rounded to 2dp, and coupling_computed=true. Isolated module (Ca=Ce=0) -> I=0.00 by convention. Distinct axis from commit-couple (Pass 8.5, internal/engine/commit_coupling_edges.go), which is git co-change/temporal coupling, not import/structural coupling. Value-asserting tests pin exact numbers (A imports B,C -> A.Ce=2/Ca=0/I=1.00; B.Ca=1/Ce=0/I=0.00; C.Ca=1/Ce=1/I=0.50). Honest: stamps nothing when no Module graph is present (Skipped). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update analysis.architecture.structural-coupling ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
