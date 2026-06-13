<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.csharp.framework.polly` — Polly (.NET resilience / fault-handling)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Resilience
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| HTTP client binding | ✅ `full` | `2026-06-13` | 5075 | `internal/custom/csharp/polly_resilience.go`<br>`internal/custom/csharp/polly_resilience_test.go` | #5075: the .NET HttpClientFactory integration is captured -- services.AddHttpClient("name").AddPolicyHandler(policy) (v7) and .AddResilienceHandler("pipeline", b => ...) (v8) emit a resilience_policy marker (resilience_type=http_client_policy) carrying binding=add_policy_handler|add_resilience_handler and the http_client name resolved from the AddHttpClient("name") on the same statement chain, answering 'which HTTP clients are protected by a Polly policy?'. Honest-partial: a policy passed as a cross-file variable resolves the binding + client but not the strategy params (those are recorded on the policy-definition marker when in-file). |
| Resilience policy extraction | ✅ `full` | `2026-06-13` | 5075 | `internal/custom/csharp/polly_resilience.go`<br>`internal/custom/csharp/polly_resilience_test.go` | #5075 (spun out of #5016/#4969; sibling of the Java MicroProfile @Retry/@CircuitBreaker pass): custom_csharp_polly stamps SCOPE.Pattern(resilience_policy) markers carrying resilience_type + literal params for both Polly surfaces. v7 fluent -- Policy.Handle<T>().Retry(n)/WaitAndRetry(n,...) -> retry (retry_count + handled_exception); .CircuitBreaker(threshold, TimeSpan) -> circuit_breaker (break_threshold + break_seconds); Policy.Timeout(TimeSpan) -> timeout (timeout_seconds); Policy.Bulkhead(maxParallelization:n) -> bulkhead (max_parallel); .Fallback(...) -> fallback. v8 -- new ResiliencePipelineBuilder().AddRetry/AddCircuitBreaker/AddTimeout/AddHedging/AddConcurrencyLimiter/AddRateLimiter(...) -> the same resilience_type taxonomy with MaxRetryAttempts/FailureRatio/BreakDuration/Timeout/MaxParallelization options resolved (api_version=v7|v8). Honest-partial: config-/variable-driven params (Retry(maxRetries)) are omitted rather than guessed; sub-second TimeSpans not rounded. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.csharp.framework.polly ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
