<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.state-machine.xstate` — XState (FSM topology)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [javascript](../by-language/javascript.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** Workflow / DAG & State Machines
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-06-02` | 3704 | `internal/engine/state_machine_edges.go` | #3704 (epic #3628 area #20): the `on: { EVENT: target }` map inside each state emits TRANSITIONS_TO source-state->target-state with the event name as the `event` property. Both string targets (`idle: { on: { FETCH: 'loading' } }` -> idle --FETCH--> loading) and object targets (`{ FAILURE: { target: 'idle' } }` -> loading --FAILURE--> idle) are resolved. Value-asserting tests pin exact (source,event,target) triples and assert a `type:'final'` state has a SCOPE.State entity but no outgoing transition. Honest-partial: guarded transition arrays (target as an array of cond/target objects) and computed/template targets are NOT resolved. |
| Resource extraction | 🟢 `partial` | `2026-06-02` | 3704 | `internal/engine/detector.go`<br>`internal/engine/state_machine_edges.go` | #3704 (epic #3628 area #20): applyStateMachineEdges (engine pass, registered in detector.go after applyWorkflowDAGEdges) extracts the XState finite-state-machine topology. Each key in the `states: { ... }` map of `createMachine({ id:'fetch', states:{ idle:{...}, loading:{...} } })` becomes a SCOPE.State entity keyed state:xstate:<machine>:<state> (machine from the `id:` field). The `initial:` state is stamped initial=true. Honest-partial: dynamically-named states / nested compound+parallel state hierarchies are flattened to their top-level names only. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.state-machine.xstate ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
