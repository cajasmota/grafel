<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.state-machine.spring-statemachine` — Spring StateMachine (FSM topology)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** Workflow / DAG & State Machines
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-06-02` | 3704 | `internal/engine/state_machine_edges.go` | #3704 (epic #3628 area #20): each `.withExternal()/.withInternal()/.withLocal()` transition chain emits TRANSITIONS_TO from `.source(X)` to `.target(Y)` with `.event(E)` as the event property (final dotted segment). Value-asserting test pins IDLE --START--> RUNNING and RUNNING --FINISH--> DONE across an `.and()`-chained config. Honest-partial: anonymous/timer transitions (no .event) emit an event-less edge; guard/action wiring is not modeled. |
| Resource extraction | 🟢 `partial` | `2026-06-02` | 3704 | `internal/engine/state_machine_edges.go` | #3704 (epic #3628 area #20): applyStateMachineEdges parses the Spring StateMachine fluent config (gated on StateMachineConfigurerAdapter / .withStates()). `.initial(States.IDLE)`, `.state(States.RUNNING)`, `.end(States.DONE)` each declare a SCOPE.State entity keyed state:spring-statemachine:spring:<state> (final dotted segment, so States.IDLE -> IDLE); initial(...) stamps initial=true. Honest-partial: states introduced only as a transition source/target without a withStates() declaration still materialize via the edge auto-declare. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.state-machine.spring-statemachine ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
