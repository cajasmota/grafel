<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.asynq` — asynq (Go task queue)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Task Queues
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🟢 `partial` | `2026-06-12` | 4923 | `internal/engine/scheduled_jobs_edges.go`<br>`internal/engine/scheduled_jobs_edges_test.go` | #4923: an asynq.ServeMux registration mux.HandleFunc("task:type", handler) / mux.Handle("task:type", asynq.HandlerFunc(...)) becomes a SCOPE.ScheduledJob (asynq:<task_type>, framework=asynq) with a TRIGGERS edge to the handler func (synthesizeGoAsynq). Honest-partial: file-level guard requires the literal "asynq" token in the source; asynq.HandlerFunc/inline-wrapped handlers emit the job node without a TRIGGERS target (handler ident unresolved); dynamic/concatenated task-type strings and non-literal mux receivers are not modelled. Value-asserted in scheduled_jobs_edges_test.go (TestScheduledJobs_GoAsynq_HandlerAndEnqueueConverge). |
| Producer extraction | 🟢 `partial` | `2026-06-12` | 4923 | `internal/engine/scheduled_jobs_edges.go`<br>`internal/engine/scheduled_jobs_edges_test.go` | #4923: an asynq.NewTask("task:type", payload) producer site (typically client.Enqueue(task)) emits an ENQUEUES edge (synthesizeGoAsynqEnqueueEdges) from the enclosing Go function to the task-type SCOPE.ScheduledJob, so producer and consumer converge on the asynq:<task_type> node by task-type name. Honest-partial: the edge is only emitted when the matching HandleFunc registration was seen in the indexed group (no phantom nodes — TestScheduledJobs_GoAsynq_NoPhantomWhenHandlerUnknown); dynamic task-type strings not resolved. |
| Topic attribution | 🟢 `partial` | `2026-06-12` | 4923 | `internal/engine/scheduled_jobs_edges.go`<br>`internal/engine/scheduled_jobs_edges_test.go` | #4923: the literal task-type string is the rendezvous topic — carried as task_type on the SCOPE.ScheduledJob node and used as the stable cross-file job ID (asynq:<task_type>) so producer ENQUEUES and consumer TRIGGERS attribute to the same topic node. Honest-partial: only string-literal task types are attributed. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.asynq ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
