<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rescript.framework.rescript-react` — ReScript-React (@rescript/react)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ReScript](../by-language/rescript.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 36

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | 🟢 `partial` | `2026-06-24` | 5378 | `internal/extractors/rescript/extractor.go`<br>`internal/extractors/rescript/react.go`<br>`internal/extractors/rescript/react_test.go` | ReScript-React (@rescript/react): a @react.component-annotated let binding (idiomatically 'make') is re-kinded SCOPE.UIComponent, subtype react_component, Properties[ui_framework]=rescript-react. JSX component usage already flows as RENDERS edges (reusing the JS-ecosystem React render model — ReScript compiles to JS and binds the same React runtime). Partial: hooks (React.useState/useEffect) and context are not separately modelled; detection is heuristic (decorator + binding-line proximity). |
| Context extraction | 🔴 `missing` | — | 5378 | — | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | 🔴 `missing` | — | 5378 | — | — |
| Data fetching | 🔴 `missing` | — | 5378 | — | — |
| Prop extraction | 🟢 `partial` | `2026-06-24` | 5378 | `internal/extractors/rescript/extractor.go`<br>`internal/extractors/rescript/react.go`<br>`internal/extractors/rescript/react_test.go` | The labelled-argument names of a @react.component binding (~name, ~onClick) are the component props; recorded as Properties[props] (comma-joined). Partial: prop NAME set only — prop types and default/optional flags are not separately modelled. |
| State management | 🔴 `missing` | — | 5378 | — | — |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Router pattern | 🔴 `missing` | — | 5378 | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 5378 | — | — |
| Interface extraction | 🔴 `missing` | — | 5378 | — | — |
| Type alias extraction | 🔴 `missing` | — | 5378 | — | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | 🔴 `missing` | — | 5378 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | 5378 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 5378 | — | — |
| Config consumption | 🔴 `missing` | — | 5378 | — | — |
| Constant propagation | 🔴 `missing` | — | 5378 | — | — |
| DB effect | 🔴 `missing` | — | 5378 | — | — |
| Dead code detection | 🔴 `missing` | — | 5378 | — | — |
| Def use chain extraction | 🔴 `missing` | — | 5378 | — | — |
| Env fallback recognition | 🔴 `missing` | — | 5378 | — | — |
| Error flow | 🔴 `missing` | — | 5378 | — | — |
| Feature flag gating | 🔴 `missing` | — | 5378 | — | — |
| Fs effect | 🔴 `missing` | — | 5378 | — | — |
| HTTP effect | 🔴 `missing` | — | 5378 | — | — |
| Import resolution quality | 🔴 `missing` | — | 5378 | — | — |
| Module cycle detection | 🔴 `missing` | — | 5378 | — | — |
| Mutation effect | 🔴 `missing` | — | 5378 | — | — |
| Pure function tagging | 🔴 `missing` | — | 5378 | — | — |
| Reachability analysis | 🔴 `missing` | — | 5378 | — | — |
| Request shape extraction | 🔴 `missing` | — | 5378 | — | — |
| Response shape extraction | 🔴 `missing` | — | 5378 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 5378 | — | — |
| Schema drift detection | 🔴 `missing` | — | 5378 | — | — |
| Taint sink detection | 🔴 `missing` | — | 5378 | — | — |
| Taint source detection | 🔴 `missing` | — | 5378 | — | — |
| Template pattern catalog | 🔴 `missing` | — | 5378 | — | — |
| Vulnerability finding | 🔴 `missing` | — | 5378 | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rescript.framework.rescript-react ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
