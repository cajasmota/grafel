# Cross-repo HTTP route↔fetch — framework coverage report (#1409)

This report records the framework endpoint/call **extraction** coverage measured
while fixing the cross-repo HTTP path-normalization bug (#1409). The cross-repo
matcher in `internal/links/http_pass.go` can only link what the per-language
extractors emit as `http_endpoint` synthetics; this report identifies which
frameworks lack endpoint and/or call extraction so they can be filed as
follow-up issues.

## Method

Each repo was indexed in isolation with a from-source binary into a temp graph
location (no live daemon), then `RunAllPasses` was run and the resulting
`http_endpoint` synthetics counted by `pattern_type`:

- `http_endpoint_synthesis` / `*_router_expanded` / `urlconf_nested_include` /
  `django_admin_synthetic` / `webhook_synthesis` → **producer** (endpoint)
- `http_endpoint_client_synthesis` → **consumer** (call)

Harness: `archigraph xrepo-verify <group> <slug>=<path>...` (hidden verification
subcommand, `cmd/archigraph/xrepo_verify.go`).

## ShipFast (polyglot-platform) — per-service extraction

| Service | Language | Framework | endpoints | calls | Endpoint extraction | Call extraction |
|---|---|---|---:|---:|---|---|
| gateway | TS | **NestJS** | 0 | 0 | ❌ none | ❌ none |
| orders | Python | FastAPI | 4 | 2 | ✅ | ✅ |
| users-auth | Python | Django | 1 | 0 | ✅ | n/a |
| catalog | TS | **Express** | 0 | 0 | ⚠️ synthesized then **dropped at resolve** | ❌ |
| payments | TS | Express | 3 | 0 | ✅ | n/a |
| shipping | Go | gin | 3 | 0 | ✅ | n/a |
| pricing | Rust | **axum** | 0 | 0 | ❌ none | n/a |
| billing | PHP | **Laravel** | 0 | 0 | ❌ none | n/a |
| semantic-search | Python | FastAPI | 2 | 1 | ✅ | ✅ |
| order-saga | Python | FastAPI | 1 | 0 | ✅ | n/a |
| search-graphql | TS | **Apollo** | 0 | 0 | ❌ none (GraphQL, not REST) | ❌ |
| notifications | Kotlin | **Spring Boot** | 0 | 0 | ❌ none | n/a |
| web | TS | React (axios) | 0 | 3 | n/a | ✅ |
| mobile | TS | React Native | 0 | 1 | n/a | ✅ |
| admin | TS | React + Apollo | 0 | 0 | n/a | ⚠️ GraphQL client not extracted |
| workers | Python | httpx client | 0 | 2 | n/a | ✅ |

## Coverage diagnosis (the 6 frameworks in scope)

| Framework | Endpoint extraction | Call extraction | Verdict |
|---|---|---|---|
| **FastAPI** | ✅ extracted | ✅ extracted (httpx/http_client) | covered |
| **gin** | ✅ extracted | n/a in fixture | endpoint covered |
| **Express** | ⚠️ inconsistent — works for some handler shapes (payments=3), **drops inline arrow-function handlers** (catalog: `synthetics=3 handler_resolved=0 handler_dropped=3`) | ✅ extracted (axios) | **bug: inline-handler resolve drop** |
| **NestJS** | ❌ not extracted — `@Controller`/`@Get`/`@Post` decorators are not recognized | ❌ HTTP-service calls (`orders.post(...)`, axios instances) not emitted as client synthetics | **no extractor** |
| **Laravel** | ❌ not extracted — `Route::get/post(...)` + `Route::prefix()->group()` in `routes/web.php` not recognized | n/a in fixture | **no extractor** |
| **Apollo** | ❌ not extracted (GraphQL resolvers ≠ REST endpoints; out of HTTP route↔fetch scope) | ❌ GraphQL client queries not extracted | GraphQL is a separate model; track separately |

## Diff vs MANIFEST §2 (~21 expected REST cross-service edges)

MANIFEST §2 documents 21 REST HTTP cross-service edges (excluding the GraphQL
`admin → search-graphql` row and the gRPC/WebSocket rows). The matcher currently
links **3** of them:

- `web → orders` (`POST /orders`)
- `mobile → orders` (`GET /orders/{id}`)
- `workers → shipping` (`POST /shipments`)

The other ~18 are blocked **upstream of the matcher** by missing/buggy
extraction — every edge whose producer is NestJS gateway, Laravel billing,
axum pricing, Spring notifications, Apollo search-graphql, or whose Express
producer dropped its inline handler (catalog) has nothing for the matcher to
link to. The path-normalization fix in this PR does not change ShipFast's count
because none of its gaps are normalization-driven.

## Follow-up issues to file

1. **NestJS endpoint + HTTP-client extraction** — `@Controller`/`@Get`/`@Post`
   decorator routes and `HttpService`/axios-instance proxy calls.
2. **Laravel route extraction** — `Route::verb()` and `Route::prefix()->group()`
   in `routes/*.php`, mapped to controller methods.
3. **Express inline-handler endpoint drop** — `app.get("/x", async (req,res)=>…)`
   synthetics are dropped at the resolve stage when the handler is an inline
   arrow function (`handler_dropped`, `no_handler_prop`); they should survive
   with the synthetic as fallback handler (as the matcher already tolerates).
4. **axum (Rust) endpoint extraction** — `Router::new().route("/x", get(handler))`.
5. **Spring Boot (Kotlin) endpoint extraction** — `@RequestMapping`/`@GetMapping`.
6. **GraphQL cross-repo linking** (Apollo) — separate model from HTTP
   route↔fetch; track as its own epic.
7. **upvate producer-side route gap** — the residual upvate orphans (~157) are
   dominated by DRF custom `@action` routes / nested sub-routers (e.g.
   `/groups/companies`, `/sync-logs`, `/note-types`) that the DRF router
   expansion does not emit as endpoints. Not a normalization problem.

## Path-normalization fix (this PR) — measured impact

| Group | links before | links after | orphan calls before | orphan calls after |
|---|---:|---:|---:|---:|
| upvate | 215 | 221 | 163 | 157 |
| shipfast | 3 | 3 | 6 | 6 |

The fix hardens the matcher's `byPath` index normalization (case-fold, trailing
slash, `<int:id>` angle-bracket params) and adds a **property-free** generic
`/api`, `/api/vN`, `/vN` prefix strip on both producer and consumer sides,
complementing the `url_prefix`-driven strip from #819 (which only fires when the
producer carries a `url_prefix` property). The bulk of upvate's prefix mismatch
(`/inspections/{id}` ↔ `/api/v1/inspections/{pk}`) was already reclaimed by
#819; the generic strip closes the remaining cases where `url_prefix` is absent
(hand-written routers, `urlconf_nested_include`).
