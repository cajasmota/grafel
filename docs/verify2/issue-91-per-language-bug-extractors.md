# Issue #91 — Per-language bug-extractor categorization

**Date:** 2026-05-09
**Corpus:** post-#96 sample-app refresh (apps that USE frameworks, not framework internals)
**Method:** per-repo `archigraph index --json-stats` + `ARCHIGRAPH_BUG_EXTRACTOR_SAMPLES=80` dump, bucketed by category histogram + manual stub-pattern analysis.
**Time on harness:** ~5 min (per-repo scoped — full corpus run on full matrix was killed at >30 min on `requests` alone).

forbidden-term grep: clean

## Per-language baseline (pre-fix)

| language | repos sampled | files | considered | resolved | extK | extU | dyn | bug-ext | bug-res | bug-rate% |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| csharp | 1 (aspnetcore-realworld) | 97 | 1374 | 16 | 0 | 203 | 203 | 864 | 88 | 69.29 |
| go | 2 (gin, chi) | 214 | 29144 | 11293 | 752 | 411 | 828 | 15113 | 747 | 54.40 |
| java | 1 (spring-petclinic) | 120 | 4382 | 1320 | 100 | 67 | 79 | 2732 | 84 | 64.26 |
| javascript | 1 (express-realworld) | 66 | 724 | 203 | 19 | 22 | 120 | 340 | 20 | 49.72 |
| kotlin | 1 (ktor-samples) | 509 | 8006 | 4619 | 23 | 37 | 0 | 2690 | 637 | 41.56 |
| php | 1 (symfony-demo) | 241 | 716 | 98 | 1 | 12 | 29 | 529 | 47 | 80.45 |
| ruby | 2 (rails-realworld, sidekiq) | 190 | 9974 | 6439 | 106 | 255 | 151 | 1933 | 1090 | 30.31 |
| rust | 2 (mini-redis, actix-examples) | 493 | 14348 | 6112 | 144 | 814 | 698 | 5748 | 832 | 45.92 |
| swift | 1 (vapor-api-template) | 21 | 32 | 4 | 0 | 0 | 0 | 18 | 10 | 87.50 |
| typescript | 1 (nestjs-starter) | 16 | 112 | 22 | 7 | 3 | 26 | 47 | 7 | 48.21 |

Every language is above the 8% target. Note: corpus is per-repo sampled — full-matrix follow-up needed (the existing `scripts/verify2/run.sh` stalls on `requests` and exceeds the per-task budget).

## Cross-language pattern: markdown CONTAINS noise

Across **every** language repo with a sample-app README/CONTRIBUTING/CHANGELOG, the markdown extractor emits `CONTAINS` edges from `Document` to a synthetic ToID `<path>::<heading-slug>`. Those slugs are NOT registered as resolvable entity targets, so every heading anchor becomes a `bug-extractor` row tagged with empty `lang` and category `structural-ref` / `bare-kind-prefixed` / `dotted-other`.

Histogram evidence:

- `go-gin`: 3,395 of 7,555 bug-extractor edges (45%) are markdown structural-ref CONTAINS. `bare-other` (the actual Go bugs) is only 47%.
- `go-chi`: 987 of 2,396 (41%).
- `php-symfony-demo`: 202 of 265 (76%).
- `csharp-aspnetcore-realworld`: 89 of 394 (22.6%).

This is the single highest-leverage fix in the corpus and dwarfs every per-language item below — but it is a **markdown extractor / resolver wiring** problem, not a per-language extractor problem. Filed separately (see follow-up issues).

## Per-language top-3 patterns

### Go (gin, chi) — bug-rate 54.4%

1. **Bare-Pascal stdlib + framework method names** (e.g. `Write`, `Get`, `Set`, `Header`, `ListenAndServe`, `HandleFunc`, `MethodFunc`, `AbortWithStatus`, `Quote`, `EncodeToString`). Tree-sitter strips the receiver (`w.Write` → `Write`, `r.Header` → `Header`); name reaches resolver bare. Stdlib `fmt.*` already covered by existing list — `net/http`, `encoding/base64`, `crypto/subtle` etc. are not. 8 of top-15 sample patterns.
2. **Bare-snake builtins** (`append`, `make`, `len`, `panic`) — Go language builtins, missing from the resolver allowlist.
3. **Cross-package method calls on user receivers** (`searchCredential`, `requestHeader`) — receiver-typed methods where the receiver type lives in another file in the same module. Indexer's cross-file Go resolution does not yet bind receiver→type→method.

### JavaScript / TypeScript (express-realworld, nestjs-starter) — 49.7% / 48.2%

1. **Prisma ORM method names** (`findUnique`, `findMany`, `findFirst`, `create`, `update`, `delete`, `count`) — 18 of 56 code-row samples in express-realworld. The Prisma client is generated code; method names are well-known. Either: stop-list these in the JS dynamic catalog (already used for some patterns), or recognise `prisma.<model>.<method>` as a Prisma-shaped call.
2. **Array/Promise stdlib methods** (`some`, `push`, `trim`, `compare`, `sign`, `isArray`) — JS stdlib bare-name calls after receiver strip. Same shape as the Python `append`/`split` pattern fixed in #89.
3. **Custom user functions invoked on objects with no static type info** (`randParagraph`, `articleMapper`) — pure receiver-binding cross-file resolution gap, like Go (3) above.

### Java (spring-petclinic) — 64.3%

1. **`org.springframework.*` and `jakarta.*` IMPORTS not in the allowlist** — 34 of top-15 sample IMPORT rows (`org.springframework.boot.SpringApplication`, `jakarta.persistence.Column`, `org.springframework.aot.hint.RuntimeHints`, …). **Quick-win 1 (this PR)** added `jakarta` to `knownExternalPackages`. `org.springframework.*` was already there.
2. **Spring Bean / JPA Entity method calls on auto-wired or annotation-injected receivers** (`getId`, `findById`, `save`, `orElseThrow`, `getTotalElements`, `rejectValue`, `addFlashAttribute`) — receiver is a `@Repository` / `@Autowired` field; cross-class binding fails.
3. **Bare-Pascal exception types** (`IllegalArgumentException`) — `throw new IllegalArgumentException(...)` reaches the resolver as bare `IllegalArgumentException`. Java stdlib exception list is missing.

### Kotlin (ktor-samples) — 41.6%

1. **Kotlin / Ktor stdlib types** (`Frame`, `CloseReason`, `CopyOnWriteArrayList`) — Pascal-case types from `kotlinx.coroutines`, `io.ktor.*`, `java.util.concurrent`. Either: extend the dotted-import allowlist with `io.ktor`, `kotlinx`, or add a Kotlin-specific bare-Pascal stdlib stop-list.
2. **`synchronized` / `it` / `lateinit`** — Kotlin language builtins arriving as bare-snake calls. The Kotlin extractor doesn't suppress language keywords as call targets.
3. **Member-property accesses indistinguishable from calls** (`members`, `memberNames`, `lastMessages`, `usersCounter`, `connections`) — extracted as CALLS but they're actually PROPERTY references on a receiver inside the same class. Receiver-binding gap.

### Ruby (rails-realworld, sidekiq) — 30.3%

1. **Rails ActionController DSL methods** (`render`, `permit`, `require`, `find_by_slug!`, `find_by_username!`, `find_by_*`) — these are method_missing-driven and inherited from `ActionController::Base`. 10 of 15 top patterns are this shape. Solution: add Rails controller method names to the Ruby dynamic-pattern catalog (already used for ActiveRecord scopes elsewhere).
2. **ActiveRecord query methods** (`order`, `save`, `find`, `comments`, `user_id`) — auto-generated via `has_many :comments` etc. These are the `method_missing`/`belongs_to`-driven ones. ALREADY partially handled in #95 dynamic catalog; needs the controller-DSL group added.
3. **Generic Object instance methods** (`new`, `nil?`, `present?`, `respond_to?`, `class`, `serialize`) — Ruby Object/Kernel methods. Add to bare-name stdlib list. Several already collide with user code (`new`, `class`) so must be Ruby-language-tagged.

### Rust (mini-redis, actix-examples) — 45.9%

1. **`use foo::bar::Baz` IMPORT statements with `::` separator** — entire shape arrives as `bare-other` because `synth.go` only splits on `.`. `tokio::net::TcpListener`, `actix_web::{App, HttpResponse, ...}`, `mini_redis::{clients::Client, Result}` all become bug-extractor edges. **Highest-impact Rust fix.**
2. **Pascal-case prelude items** (`Ok`, `Err`, `Some`, `None`, `Box`, `Vec`, `Result`, `Option`) — bare calls because the prelude is implicit. Cannot add globally (collides with Go/JS user identifiers); needs a per-language allowlist (gated on `Language: rust`).
3. **Method-call receiver strip** (`.to`, `.into`, `.clone`, `.bind`, `.wrap`, `.service`, `.resource`, `.app_data`) — receiver type info lost. Same cross-file binding gap as Go/JS.

### PHP (symfony-demo) — 80.5%

1. **`Symfony\*`, `App\*`, `Doctrine\*` IMPORTS** — PHP namespace separator is `\`, which `synth.go` treats as a path-separator and rejects (line 304 `strings.ContainsAny(name, "/\\")`). Every PHP `use` statement therefore becomes a bug-extractor. Catastrophic for PHP — must add `\` as a recognised namespace separator OR pre-strip `\`-segments in the PHP extractor before emitting the stub.
2. **Markdown CONTAINS** — `assets/styles/bootswatch/README.md::*` headings (76% of categorised bug-extractor rows in this repo).
3. **JS embedded in PHP repo** (Stimulus controllers under `assets/`) — `getAttribute`, `querySelector`, `dispatchEvent`, etc. These are browser DOM stdlib; need a `browser-dom` allowlist similar to `console`/`fetch` already on the list.

### Swift (vapor-api-template) — 87.5%

Sample size very small (32 endpoints, 4 bug-extractor in samples; histogram TOTAL=4). **The Vapor api-template doesn't exercise enough Swift code to draw firm conclusions.** Need a larger Swift sample-app — `vapor/penny-bot` or similar. Filed as follow-up.

### C# (aspnetcore-realworld) — 69.3% → 52.9% post-quick-win

1. **`System.*` / `Microsoft.*` / `Conduit.*` IMPORTS** — top 3 import roots account for 65 of 65 IMPORT samples. **Quick-win 1 (this PR)** added `system` and `microsoft` to `knownExternalPackages`. Re-run shows: ext-known 0 → 327, ext-unknown 203 → 101, bug-extractor 864 → 661, bug-rate 69.3% → 52.9% (-16.4pp).
2. **Conduit.* (project-internal namespaces)** — 18 import-root rows. Cross-file binding failure: imports of in-project namespaces don't resolve to the local entity. Same shape as Java point 2.
3. **EF Core extension methods on `DbSet<T>`** (`Where`, `Include`, `ThenInclude`, `FirstOrDefaultAsync`, `ToListAsync`) — extension-method receiver strip. Cross-language pattern.

## Quick wins applied in this PR

Both edits in `internal/external/synth.go`:

1. **Allowlist additions to `knownExternalPackages`**: `jakarta`, `system`, `microsoft`, `tokio`, `actix_web`, `actix`, `serde`, `serde_json`, `anyhow`, `thiserror`, `tracing`, `tracing_subscriber`, `clap`, `reqwest`, `futures`, `async_trait`, `opentelemetry`. Lookup is case-folded so `System` and `Microsoft` C# imports route correctly. **Measured impact: aspnetcore-realworld bug-rate 69.29% → 52.91% (-16.4pp).** Java/Rust gains smaller because the dominant rust-import shape uses `::` (separately filed). PHP entries were drafted but **removed before commit** because synth rejects `\` paths — adding lowercase keys without resolver support would do nothing.

2. **`stdlibBareNames` additions**: `assert_eq`, `assert_ne` (Rust test macros). Conservative — `Ok`/`Err`/`Some`/`None` deliberately NOT added because the bare-name lookup is global and those identifiers commonly appear in non-Rust user code (per the #94 lesson: bias to misses).

Existing tests in `internal/external/` and `internal/resolve/` continue to pass.

## Follow-up issues filed

All on milestone `1.0 — initial port`, project Status=Todo, label `bug,scope:indexer`. Each scoped to <1 day. All cross-reference #91.

| # | language(s) | title |
| --- | --- | --- |
| #100 | cross-language | markdown CONTAINS edges become bug-extractor for every README heading |
| #101 | rust | Rust `use foo::bar` imports become bug-extractor — synth.go splits only on '.' not '::' |
| #102 | php | PHP `use Foo\Bar\Baz` imports become bug-extractor — synth rejects '\' as path separator |
| #103 | go | Go bare-Pascal stdlib + framework method names reach resolver after receiver strip |
| #104 | js / ts | JavaScript/TypeScript Prisma ORM method names become bug-extractor |
| #105 | java | Java Spring/JPA bean method calls fail cross-class binding |
| #106 | kotlin | Kotlin stdlib types and language keywords reach resolver as bare-name bug-extractor |
| #107 | ruby | Ruby Rails ActionController DSL methods become bug-extractor |
| #108 | rust | Per-language Rust prelude allowlist (Ok, Err, Some, None, Box, Vec, Result, Option) |
