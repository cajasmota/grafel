<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.orchestration.argo` — Argo Workflows (DAG topology)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** Workflow / DAG & State Machines
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-06-02` | — | `internal/engine/workflow_dag_edges.go` | #3628 area #12: applyWorkflowDAGEdges parses Argo Workflow/WorkflowTemplate/CronWorkflow YAML (gated on argoproj.io / kind:) via gopkg.in/yaml.v3. spec.templates[].dag.tasks[] with `dependencies: [t1]` emit TASK_DEPENDS_ON dependency->task (build->test->deploy); two deps fan in. `steps:` stages emit sequential TASK_DEPENDS_ON prev-stage->next-stage. Each task is a SCOPE.Activity owned by the SCOPE.Workflow (metadata.name) via EXECUTES_ACTIVITY. Value-asserting tests pin `dependencies:[build]` -> build->deploy direction and assert a lone task / non-Argo manifest (Deployment) yield no edges. Honest-partial: withItems/withParam dynamic task expansion not resolved. |
| Resource extraction | 🟢 `partial` | `2026-06-02` | — | `internal/engine/workflow_dag_edges.go` | #3628 area #12: emits SCOPE.Workflow (workflow:argo:<name>) and SCOPE.Activity (task:argo:<name>:<task>) entities with workflow_engine=argo. Partial: statically-declared dag.tasks / steps only. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.orchestration.argo ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
