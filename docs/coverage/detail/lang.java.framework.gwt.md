<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.gwt` — Google Web Toolkit (GWT)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 33

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | ❌ `missing` | — | — | — | — |
| Context extraction | ❌ `missing` | — | — | — | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | ❌ `missing` | — | — | — | — |
| Data fetching | ❌ `missing` | — | — | — | — |
| Prop extraction | ❌ `missing` | — | — | — | — |
| State management | ❌ `missing` | — | — | — | — |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Router pattern | ❌ `missing` | — | — | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ❌ `missing` | — | — | — | — |
| Interface extraction | ❌ `missing` | — | — | — | — |
| Type alias extraction | ❌ `missing` | — | — | — | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | ❌ `missing` | — | — | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ❌ `missing` | — | — | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | ⚠️ `partial` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Response shape extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Sanitizer recognition | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.framework.gwt ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
