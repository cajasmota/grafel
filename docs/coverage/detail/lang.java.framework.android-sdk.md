<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.android-sdk` — Android SDK (Activity/Fragment routing)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Mobile
- **Capability cells:** 16

## Capabilities


### Structure

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Context extraction | ❌ `missing` | — | — | — | — | — |

### Navigation

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Deep link extraction | ❌ `missing` | — | — | — | — | — |
| Navigation extraction | ❌ `missing` | — | — | — | — | — |
| Screen detection | ❌ `missing` | — | — | — | — | — |

### Platform

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Platform branching | ❌ `missing` | — | — | — | — | — |

### Native Bridge

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Native module imports | ❌ `missing` | — | — | — | — | — |

### Data Flow

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Branch conditions | ❌ `missing` | — | — | — | — | — |
| State management | ❌ `missing` | — | — | — | — | — |

### Type System

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Enum extraction | ❌ `missing` | — | — | — | — | — |
| Interface extraction | ❌ `missing` | — | — | — | — | — |
| Type alias extraction | ❌ `missing` | — | — | — | — | — |

### Lifecycle

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| State setter emission | ❌ `missing` | — | — | — | — | — |

### Testing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Tests linkage | ❌ `missing` | — | — | — | — | — |

### Substrate

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Constant propagation | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Import resolution quality | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.framework.android-sdk ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
