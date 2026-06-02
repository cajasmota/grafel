<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.state-machine.aasm` — Ruby AASM (FSM topology)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** Workflow / DAG & State Machines
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-06-02` | 3704 | `internal/engine/state_machine_edges.go` | #3704 (epic #3628 area #20): each `event :activate do transitions from: :pending, to: :active end` emits TRANSITIONS_TO pending->active with event=activate. `from:` may be a single symbol or an array (`from: [:active, :idle]`) which fans out to one edge per source. Value-asserting tests pin pending --activate--> active and the array fan-out. Honest-partial: guard/:if-gated transitions are still emitted (the guard is not modeled); dynamic from/to expressions not resolved. |
| Resource extraction | 🟢 `partial` | `2026-06-02` | 3704 | `internal/engine/state_machine_edges.go` | #3704 (epic #3628 area #20): applyStateMachineEdges parses the `aasm do ... end` block. Each `state :name` declares a SCOPE.State entity keyed state:aasm:aasm:<state>; `state :pending, initial: true` stamps initial=true. Honest-partial: dynamically/metaprogrammed state declarations not resolved. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.state-machine.aasm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
