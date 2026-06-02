<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `analysis.architecture.shared-db-coupling` — Shared-database cross-service coupling (SHARES_DATA)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** App Topology & Integration
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Shared data coupling | ✅ `full` | `2026-06-02` | — | `cmd/archigraph/index.go`<br>`internal/engine/shared_db_coupling.go`<br>`internal/engine/shared_db_coupling_test.go` | #3628 area #13: detects when ≥2 DISTINCT modules access the SAME logical table/collection — the cross-service data-ownership / boundary-violation signal. LIVE project-scope engine pass engine.ApplySharedDataCoupling, registered in cmd/archigraph/index.go as Pass 8.8 (PassSharedDB, --skip-pass=shared-db) AFTER module-agg (Pass 8) so synthetic Module nodes exist and every entity carries Properties[module]. Over the assembled graph it converges table/collection accesses by NAME (per repo) from three signals: ACCESSES_TABLE edges (function→SCOPE.DataAccess, table from the DataAccess `table` prop, accessor module from the source/function entity), JOINS_COLLECTION edges (Mongo $lookup, `from` collection), and SCOPE.DataAccess entities directly (module+table props). For each (repo,table) touched by ≥2 distinct REAL-module accessors it stamps shared=true/accessor_count=N/accessor_modules=<sorted csv> on the SCOPE.DataAccess entities and emits a SHARES_DATA edge between every co-accessing Module pair (smaller Module ID = FromID; props coupling=shared_data, shared_tables csv, shared_count, provenance=SHARED_DB_COUPLING). Distinct axis from structural coupling (Pass 8.6 import fan-in/out) and commit-couple (Pass 8.5 temporal). Honest: the `_external`/empty module is never a distinct accessor (unattributed access cannot fabricate coupling); a table touched by ONE module is stamped shared=false with no edge; self-pairs suppressed; skips entirely with no Module graph or no data access. Value-asserting tests: OrderSvc+BillingSvc both ACCESSES_TABLE orders → orders.shared=true/accessor_count=2/accessor_modules=BillingSvc,OrderSvc + exactly one SHARES_DATA edge (OrderSvc,BillingSvc, shared_tables=orders); a single-module table → shared=false/0 edges; JOINS_COLLECTION co-join → coupling edge; idempotent re-run adds no duplicate edge. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update analysis.architecture.shared-db-coupling ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
