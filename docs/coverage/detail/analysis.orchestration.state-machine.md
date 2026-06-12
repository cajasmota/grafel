<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `analysis.orchestration.state-machine` — Finite-state-machine topology (SCOPE.State + TRANSITIONS_TO)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** App Topology & Integration
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-06-12` | — | `internal/engine/state_machine_edges.go`<br>`internal/engine/state_machine_edges_test.go` | Emits TRANSITIONS_TO edges (RelationshipKindTransitionsTo) source-state -> target-state carrying the triggering event as the 'event' property (e.g. idle --FETCH--> loading). Deduped by transitionsToEdgeKind|from|to|event. A state with no outgoing transition yields a SCOPE.State node but no edge; statically-named targets only. |
| Resource extraction | ✅ `full` | `2026-06-12` | — | `internal/engine/detector.go`<br>`internal/engine/state_machine_edges.go`<br>`internal/engine/state_machine_edges_test.go` | #3704 (epic #3628 area #20): append-only detector pass applyStateMachineEdges (detector.go) emits SCOPE.State entities (kind EntityKindState, ID state:<lib>:<machine>:<stateName>, states scoped by machine so two machines' 'idle' do not collide) for the 4 dominant FSM libs: XState (createMachine states/on), Ruby AASM (state:/event do transitions from:to:), Spring StateMachine Java (.withStates().initial/.state + .withTransitions source/target/event), Python transitions (Machine states/transitions trigger/source/dest). Honest-partial: only statically-named states are emitted; dynamically computed state/target names are not resolved. 11 value-asserting tests. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update analysis.orchestration.state-machine ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
