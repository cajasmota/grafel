<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.broker.laravel-queue` — Laravel Queue (queued Jobs / dispatch)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [php](../by-language/php.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Task Queues
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🟢 `partial` | `2026-06-12` | 4920 | `internal/custom/php/laravel.go`<br>`internal/custom/php/laravel_queue_test.go` | #4920: Laravel queued-Job consumer. laravelExtractor (internal/custom/php/laravel.go) emits a SCOPE.Service per 'class XJob ... implements ShouldQueue' (reLaravelJobClass, provenance INFERRED_FROM_LARAVEL_JOB, carrying job_class) plus the 'public function handle()' entrypoint as a SCOPE.Operation. Honest-partial: only the regex-recognised ShouldQueue base; namespaced/dynamic job classes and the handle() argument shapes are not modelled. Value-asserted in laravel_queue_test.go (TestLaravelQueueConsumerAttribution). |
| Producer extraction | 🟢 `partial` | `2026-06-12` | 4920 | `internal/custom/php/laravel.go`<br>`internal/custom/php/laravel_queue_test.go` | #4920: Laravel queue PRODUCER side (previously MISSING — only the consumer Job class + handle() were modelled). reLaravelJobDispatchStatic matches CONSTANT-receiver static dispatch 'FooJob::dispatch(...)' incl. dispatchSync/dispatchNow/dispatchAfterResponse/dispatchIf/dispatchUnless; reLaravelJobDispatchHelper matches 'dispatch(new FooJob(...))' and 'Bus::dispatch(new FooJob(...))'. Each emits a SCOPE.Operation '<JobClass>.dispatch' (INFERRED_FROM_LARAVEL_JOB_DISPATCH, job_class + dispatch_method) so producer and consumer converge by job-class name. Honest-partial: the Bus facade is never a target; variable-receiver dispatch and queued mailables/notifications are left to their own idioms. Value-asserted in laravel_queue_test.go (TestLaravelQueueProducerStatic, TestLaravelQueueProducerHelper, TestLaravelQueueProducerConverges). |
| Topic attribution | 🟢 `partial` | `2026-06-12` | 4920 | `internal/custom/php/laravel.go`<br>`internal/custom/php/laravel_queue_test.go` | #4920: queue attribution via a 'public/protected $queue = "name"' property (reLaravelQueueProp) stamps a 'queue' property on the job-class SCOPE.Service and its handle() so the graph can answer 'which jobs run on the :invoices queue?'. Honest-partial: file-scoped (one $queue per file attributed to every job class in that file — the dominant one-class-per-file Laravel convention); the default queue (no $queue), the connection name, and per-dispatch '->onQueue(...)' overrides are not captured. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.broker.laravel-queue ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
