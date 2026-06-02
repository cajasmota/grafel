<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.state-machine.python-transitions` — Python transitions (FSM topology)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** Workflow / DAG & State Machines
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-06-02` | 3704 | `internal/engine/state_machine_edges.go` | #3704 (epic #3628 area #20): each dict-form transition `{'trigger':'go','source':'A','dest':'B'}` emits TRANSITIONS_TO A->B with event=go. `source` may be a list (`'source':['solid','liquid']`) which fans out to one edge per source. Value-asserting tests pin A --go--> B, B --back--> A and the list fan-out solid/liquid --heat--> gas. Honest-partial: list-form transitions (['trigger','src','dst']) and wildcard sources ('*') are not resolved. |
| Resource extraction | 🟢 `partial` | `2026-06-02` | 3704 | `internal/engine/state_machine_edges.go` | #3704 (epic #3628 area #20): applyStateMachineEdges parses `Machine(states=[...], transitions=[...], initial=...)` (gated on Machine( / from transitions). Each string in `states=['A','B']` becomes a SCOPE.State entity keyed state:python-transitions:transitions:<state>; `initial='A'` stamps initial=true. Honest-partial: dict-form state objects ({'name':'A','on_enter':...}) capture the name only; class-based / dynamically-generated states not resolved. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.state-machine.python-transitions ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
