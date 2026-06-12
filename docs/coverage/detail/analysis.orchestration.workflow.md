<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `analysis.orchestration.workflow` — Workflow orchestration (Temporal/Cadence/Step-Functions)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** Workflow / DAG & State Machines
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-06-12` | — | `internal/engine/workflow_edges.go`<br>`internal/engine/workflow_edges_test.go` | Emits STARTS_WORKFLOW (client.start_workflow/ExecuteWorkflow producer -> Workflow), EXECUTES_ACTIVITY (Workflow -> Activity within-workflow call), and STEPFUNCTION_STEP_INVOKES (StateMachine Task state -> Lambda/target). Edges deduped per (caller,workflow)/(workflow,activity). Statically-named workflows/activities only. |
| Resource extraction | ✅ `full` | `2026-06-12` | — | `internal/engine/detector.go`<br>`internal/engine/workflow_edges.go`<br>`internal/engine/workflow_edges_test.go` | #934: append-only detector pass applyWorkflowEdges (detector.go) synthesises SCOPE.Workflow (workflow:temporal:<name>), SCOPE.Activity (activity:temporal:<name>), and SCOPE.StateMachine (statemachine:aws-sfn:<name>) entities for Temporal (Python/Go/Java/Kotlin SDKs), Cadence (Java @WorkflowMethod/@ActivityMethod), and AWS Step Functions (ASL JSON + CDK/Terraform/CloudFormation). SourceFile left empty so the import-channel linker joins producer/consumer cross-repo (same strategy as Kafka #726, gRPC #725). Distinct from workflow_dag (Airflow/Argo/Celery-canvas). 25 value-asserting tests. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update analysis.orchestration.workflow ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
