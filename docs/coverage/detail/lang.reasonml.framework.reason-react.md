<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.reasonml.framework.reason-react` — Reason-React ([@react.component])

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ReasonML](../by-language/reasonml.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 36

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | 🟢 `partial` | `2026-06-24` | 5379 | `internal/extractors/reasonml/extractor.go`<br>`internal/extractors/reasonml/react.go`<br>`internal/extractors/reasonml/react_test.go` | Reason-React ([@react.component]): a [@react.component]-annotated let binding (idiomatically 'make') is re-kinded SCOPE.UIComponent, subtype react_component, Properties[ui_framework]=reason-react. Reason uses the bracket-attribute syntax [@react.component] (vs ReScript's bare @react.component); the React model is shared. Reason compiles to JS and binds the same React runtime as the JS/TS ecosystem, so the JS-ecosystem React model is reused. Partial: hooks (React.useState/useReducer) and context are not separately modelled; JSX RENDERS edges are not emitted by the ReasonML base extractor (deferred); detection is heuristic (attribute + binding-line proximity). |
| Context extraction | 🔴 `missing` | — | 5379 | — | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | 🔴 `missing` | — | 5379 | — | — |
| Data fetching | 🔴 `missing` | — | 5379 | — | — |
| Prop extraction | 🟢 `partial` | `2026-06-24` | 5379 | `internal/extractors/reasonml/extractor.go`<br>`internal/extractors/reasonml/react.go`<br>`internal/extractors/reasonml/react_test.go` | The labelled-argument names of a [@react.component] binding (~name, ~onClick) are the component props; recorded as Properties[props] (comma-joined), captured across continuation lines up to the => body boundary. Partial: prop NAME set only — prop types and default/optional flags are not separately modelled. |
| State management | 🔴 `missing` | — | 5379 | — | — |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Router pattern | 🔴 `missing` | — | 5379 | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 5379 | — | — |
| Interface extraction | 🔴 `missing` | — | 5379 | — | — |
| Type alias extraction | 🔴 `missing` | — | 5379 | — | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | 🔴 `missing` | — | 5379 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | 5379 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 5379 | — | — |
| Config consumption | 🔴 `missing` | — | 5379 | — | — |
| Constant propagation | 🔴 `missing` | — | 5379 | — | — |
| DB effect | 🔴 `missing` | — | 5379 | — | — |
| Dead code detection | 🔴 `missing` | — | 5379 | — | — |
| Def use chain extraction | 🔴 `missing` | — | 5379 | — | — |
| Env fallback recognition | 🔴 `missing` | — | 5379 | — | — |
| Error flow | 🔴 `missing` | — | 5379 | — | — |
| Feature flag gating | 🔴 `missing` | — | 5379 | — | — |
| Fs effect | 🔴 `missing` | — | 5379 | — | — |
| HTTP effect | 🔴 `missing` | — | 5379 | — | — |
| Import resolution quality | 🔴 `missing` | — | 5379 | — | — |
| Module cycle detection | 🔴 `missing` | — | 5379 | — | — |
| Mutation effect | 🔴 `missing` | — | 5379 | — | — |
| Pure function tagging | 🔴 `missing` | — | 5379 | — | — |
| Reachability analysis | 🔴 `missing` | — | 5379 | — | — |
| Request shape extraction | 🔴 `missing` | — | 5379 | — | — |
| Response shape extraction | 🔴 `missing` | — | 5379 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 5379 | — | — |
| Schema drift detection | 🔴 `missing` | — | 5379 | — | — |
| Taint sink detection | 🔴 `missing` | — | 5379 | — | — |
| Taint source detection | 🔴 `missing` | — | 5379 | — | — |
| Template pattern catalog | 🔴 `missing` | — | 5379 | — | — |
| Vulnerability finding | 🔴 `missing` | — | 5379 | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.reasonml.framework.reason-react ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
