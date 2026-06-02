<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.orchestration.celery-canvas` — Celery canvas (chain/group/chord topology)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** Workflow / DAG & State Machines
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-06-02` | — | `internal/engine/workflow_dag_edges.go` | #3628 area #12: applyWorkflowDAGEdges extracts Celery canvas topology (distinct from celery_pubsub_edges.go which models broker dispatch). `chain(a.s(), b.s(), c.s())` emits sequential TASK_DEPENDS_ON a->b->c; `chord([a.s(),b.s()])(callback.s())` emits fan-in a->callback, b->callback; `group(a.s(),b.s())` registers parallel members with NO ordering edge (honest). Each .s()/.si() signature becomes a SCOPE.Activity (task:celery:canvas:<task>) owned by the SCOPE.Workflow via EXECUTES_ACTIVITY. Value-asserting tests pin chain direction, chord fan-in, and assert group members produce zero TASK_DEPENDS_ON edges. Honest-partial: nested canvas topology is flattened in source order; dynamically-built signatures not resolved. |
| Resource extraction | 🟢 `partial` | `2026-06-02` | — | `internal/engine/workflow_dag_edges.go` | #3628 area #12: emits SCOPE.Workflow (workflow:celery:canvas) and SCOPE.Activity (task:celery:canvas:<task>) entities with workflow_engine=celery. Partial: statically-named signatures only. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.orchestration.celery-canvas ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
