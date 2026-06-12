<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.broker.activejob` — Rails ActiveJob (queue abstraction)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Task Queues
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🟢 `partial` | `2026-06-12` | 4919 | `internal/custom/ruby/activejob_test.go`<br>`internal/custom/ruby/rails.go` | #4919: ActiveJob consumer side. railsExtractor (internal/custom/ruby/rails.go) emits a SCOPE.Service per job class declared as 'class XJob < ApplicationJob' or '< ActiveJob::Base' (reRailsJobClass, provenance INFERRED_FROM_ACTIVEJOB_CLASS, carrying job_base), plus the 'def perform' handler as a SCOPE.Operation (INFERRED_FROM_RAILS_JOB_PERFORM). ActiveJob is the canonical Rails job abstraction backing Sidekiq/Resque/GoodJob/DelayedJob. Honest-partial: only the regex-recognised base classes; module-namespaced/dynamic job classes and the perform argument shapes are not modelled. Value-asserted in activejob_test.go (TestActiveJobClassAndQueue, TestActiveJobBaseClass). |
| Producer extraction | 🟢 `partial` | `2026-06-12` | 4919 | `internal/custom/ruby/activejob_test.go`<br>`internal/custom/ruby/rails.go` | #4919: ActiveJob producer side (previously MISSING — only Sidekiq's lowercase-receiver perform_async was modelled). reRailsJobDispatch matches a CONSTANT-receiver enqueue 'FooJob.perform_later(...)' / 'FooJob.perform_now(...)' incl. the deferred 'FooJob.set(...).perform_later' form, emitting a SCOPE.Operation named '<JobClass>.<method>' (INFERRED_FROM_ACTIVEJOB_DISPATCH, carrying job_class + dispatch_method) so producer and consumer converge by job-class name. Honest-partial: lowercase receivers are excluded (left to the sidekiq extractor); dynamic/const-aliased class refs not resolved. Value-asserted in activejob_test.go (TestActiveJobProducerDispatch, TestActiveJobIgnoresLowercaseReceiver). |
| Topic attribution | 🟢 `partial` | `2026-06-12` | 4919 | `internal/custom/ruby/activejob_test.go`<br>`internal/custom/ruby/rails.go` | #4919: queue attribution via 'queue_as :name' / 'queue_as "name"' (reRailsQueueAs) stamps a 'queue' property on the job-class SCOPE.Service and its perform handler so the graph can answer 'which jobs run on the :mailers queue?'. Honest-partial: the default queue (no queue_as) is left unattributed; per-call '.set(queue: ...)' overrides and dynamic queue names are not captured. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.broker.activejob ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
