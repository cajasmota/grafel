# Quality baseline — Phase 2 (2026-05-20)

Phase 2 adds four new fixtures (typescript-react-mini, java-spring-mini,
go-chi-mini, rust-tokio-mini) and wires the quality runner into the verify2
CI channel via `.github/workflows/quality.yml`.

Numbers below are reproducible via:

```bash
scripts/quality/run.sh
# or via the verify2 channel:
scripts/verify2/run-quality.sh
```

## Summary table (Phase 2 baseline, 2026-05-20)

| Fixture | Language | Entities (must-have) | Relationships (must-have) | Forbidden hits |
|---|---|---|---|---|
| python-django-mini | Python/Django | **28 / 28 = 100%** | **12 / 12 = 100%** | 0 |
| typescript-react-mini | TypeScript/React | **20 / 20 = 100%** | **16 / 16 = 100%** | 0 |
| java-spring-mini | Java/Spring | **25 / 25 = 100%** | **21 / 21 = 100%** | 0 |
| go-chi-mini | Go/chi | **20 / 20 = 100%** | **19 / 19 = 100%** | 0 |
| rust-tokio-mini | Rust/tokio | **16 / 16 = 100%** | **13 / 13 = 100%** | 0 |

All five fixtures achieve 100% must-have recall and zero forbidden-relationship
hits on `main` as of the Phase 2 merge.

---

## python-django-mini

| Metric | Value |
|---|---|
| Entity recall (must-have) | **28 / 28 = 100.0%** |
| Relationship recall (must-have) | **12 / 12 = 100.0%** |
| Forbidden-relationship hits | **0** |
| Nice-to-have entities | 0 / 2 |
| Nice-to-have relationships | 1 / 5 |
| Extracted entity total | 83 |
| Extracted relationship total | 109 |

Fixture: `internal/quality/golden/python-django-mini/` —
11 source files exercising Django Model + custom Manager + class-based +
function-based Views + URLconf + post_save signal receiver + admin
registration + `AppConfig.ready` import side-effect.

### Outstanding nice-to-haves

1. `@admin.register(User)` -> `DEPENDS_ON` edge from `UserAdmin` to `User`.
2. `@receiver(post_save, sender=User)` -> synthesized `post_save_receiver` SCOPE.Pattern entity.
3. `user_post_save` -> bare-name `CALLS full_label` (import-placeholder-prune currently
   removes this stub; candidate for a future resolver chain-fix).
4. `UserDetailView.get` -> `User.full_label` cross-file method binding.
5. `include('users.urls')` -> direct SERVES fan-out instead of per-Route.

---

## typescript-react-mini

| Metric | Value |
|---|---|
| Entity recall (must-have) | **20 / 20 = 100.0%** |
| Relationship recall (must-have) | **16 / 16 = 100.0%** |
| Forbidden-relationship hits | **0** |
| Nice-to-have entities | 1 / 1 |
| Nice-to-have relationships | 0 / 1 |
| Extracted entity total | 75 |
| Extracted relationship total | 149 |

Fixture: `internal/quality/golden/typescript-react-mini/` —
9 source files exercising React functional components, hooks (useState,
useEffect, useContext, useQuery), custom hook extraction, React Router
routes, and cross-file component imports.

---

## java-spring-mini

| Metric | Value |
|---|---|
| Entity recall (must-have) | **25 / 25 = 100.0%** |
| Relationship recall (must-have) | **21 / 21 = 100.0%** |
| Forbidden-relationship hits | **0** |
| Nice-to-have entities | — |
| Nice-to-have relationships | — |
| Extracted entity total | 55 |
| Extracted relationship total | 120 |

Fixture: `internal/quality/golden/java-spring-mini/` —
6 source files exercising Spring Boot `@SpringBootApplication`,
`@RestController` with `@GetMapping` / `@PostMapping`, `@Service` business
logic, `@Repository` extending `JpaRepository`, and a JPA `@Entity` model.

---

## go-chi-mini

| Metric | Value |
|---|---|
| Entity recall (must-have) | **20 / 20 = 100.0%** |
| Relationship recall (must-have) | **19 / 19 = 100.0%** |
| Forbidden-relationship hits | **0** |
| Nice-to-have entities | — |
| Nice-to-have relationships | — |
| Extracted entity total | 64 |
| Extracted relationship total | 123 |

Fixture: `internal/quality/golden/go-chi-mini/` —
5 source files (+ `go.mod`) exercising chi router registration, struct
receiver handler methods (`UsersHandler.List`, `UsersHandler.Get`),
middleware function, `Store` interface declaration + `MemoryStore` struct
implementation, and a goroutine-launching worker function.

---

## rust-tokio-mini

| Metric | Value |
|---|---|
| Entity recall (must-have) | **16 / 16 = 100.0%** |
| Relationship recall (must-have) | **13 / 13 = 100.0%** |
| Forbidden-relationship hits | **0** |
| Nice-to-have entities | 1 / 2 |
| Nice-to-have relationships | 0 / 1 |
| Extracted entity total | 28 |
| Extracted relationship total | 39 |

Fixture: `internal/quality/golden/rust-tokio-mini/` —
5 source files exercising `#[tokio::main]` async entry point, async fn
handlers, a trait (`UserStore`) + implementing struct (`MemoryStore`),
structs with derive macros, `mod` hierarchy, and a tokio channel worker.

### Outstanding nice-to-haves

1. `UserStore` trait bounds on a function parameter — not yet surfaced as an
   entity distinct from the struct impl. Candidate for a future chain-fix.
2. `run_worker` -> `handle_message` intra-module CALLS — bare-name stub
   currently unresolved.

---

## Notes on methodology

- The fixture's source tree is run through the SAME indexer pipeline used
  in production (`cmd/archigraph.Index`), so a regression in any pass
  (extract / framework rules / cross-language / resolver / synthesis)
  surfaces here.
- Fixtures are tiny on purpose (<10 files) — a fixture failure points at a
  specific extractor or rule, not at a corpus-wide trend. Corpus-level
  trends are still measured by verify2 / bug-rate.
- The reporter annotates each miss with WHY (neither endpoint extracted /
  from missing / to missing / both present but edge absent). The last
  category is the most actionable.
- Phase 2 also wires the runner into CI: `.github/workflows/quality.yml`
  runs `scripts/verify2/run-quality.sh` on every PR and uploads per-fixture
  JSON reports as artifacts.

## Phase 1 baseline (archived)

Phase 1 baseline (2026-05-19, after PR #600):

| Fixture | Entities | Relationships | Forbidden |
|---|---|---|---|
| python-django-mini | 28 / 28 = 100% | 13 / 13 = 100% | 0 |

Note: Phase 1 counted 13 must-have relationships; one (`user_post_save
-[CALLS]-> full_label` bare-name) was downgraded to `nice_to_have` in Phase 2
when the import-placeholder-prune pass started stripping the bare-name stub.
The actual extractor behaviour for this edge is tracked as a future chain-fix.
