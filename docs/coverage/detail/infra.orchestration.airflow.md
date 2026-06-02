<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.orchestration.airflow` — Apache Airflow (DAG topology)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** Workflow / DAG & State Machines
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-06-02` | — | `internal/engine/detector.go`<br>`internal/engine/workflow_dag_edges.go` | #3628 area #12: applyWorkflowDAGEdges (engine pass, registered in detector.go after applyWorkflowEdges) extracts the Airflow task-dependency DAG. Classic operators `t1 = PythonOperator(task_id="extract")` and TaskFlow `@task def extract()` become SCOPE.Activity tasks owned by the SCOPE.Workflow (DAG name from `with DAG("id")` / `@dag(dag_id=...)`) via EXECUTES_ACTIVITY. Static dependency operators emit TASK_DEPENDS_ON upstream->downstream: `extract >> transform >> load` -> extract->transform->load; `[a,b] >> c` fan-in; `t1.set_downstream(t2)` / `set_upstream` (direction-flipped); `extract() >> transform()` decorator-call chains. Value-asserting tests pin exact edge direction and assert a lone task yields no false dependency edge. Honest-partial: dynamic task mapping (.expand()), loop-built operators, and programmatically-assembled chains are NOT resolved. |
| Resource extraction | 🟢 `partial` | `2026-06-02` | — | `internal/engine/workflow_dag_edges.go` | #3628 area #12: emits SCOPE.Workflow (the DAG, keyed workflow:airflow:<dag>) and SCOPE.Activity (each task, keyed task:airflow:<dag>:<task>) entities with workflow_engine=airflow and topology=dag properties. Synthetics carry SourceFile="" so the import-channel linker joins them. Partial: only statically-named tasks. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.orchestration.airflow ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
