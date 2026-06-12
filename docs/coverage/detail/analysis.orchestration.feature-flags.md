<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `analysis.orchestration.feature-flags` — Feature-flag gating topology (SCOPE.FeatureFlag + GATED_BY)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** App Topology & Integration
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-06-12` | — | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go` | Emits one GATED_BY edge (RelationshipKindGatedBy) per (enclosing function, flag) pair, pointing at the SCOPE.FeatureFlag entity — flag blast-radius ('what code is gated by flag F'). Enclosing fn resolved via shared indexEnclosingFunctions/enclosingFuncAt helpers. Deduped so multiple checks of one flag in one fn produce a single edge. Literal flag keys only. |
| Resource extraction | ✅ `full` | `2026-06-12` | — | `internal/engine/detector.go`<br>`internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go` | #3628 area #17: append-only detector pass applyFeatureFlagEdges (detector.go) scans flag-management-SDK check call sites and emits one synthetic SCOPE.FeatureFlag entity (EntityKindFeatureFlag, ID feature:<flag-key>, Subtype=detecting SDK) per distinct flag key — two repos/files checking the same key converge on one node (same cross-repo identity as MessageTopic). SDKs: LaunchDarkly (*Variation family), .NET FeatureManagement (IsEnabledAsync), Unleash, OpenFeature, plus other major SDKs. 61 value-asserting tests. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update analysis.orchestration.feature-flags ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
