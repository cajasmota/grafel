# Ship-gate baseline — 2026-05-10 (Refs #44)

Refreshed VERIFY-2 measurement following the major work merged this session:

- Corpus expanded 23 → 289 repos (chunks A–EE merged).
- 24 PORT-RELS-* extractors now emit IMPORTS / CALLS / CONTAINS edges (#366–#389 + #60 audit).
- 7 structural-resolver fixes: #120 Java, #122 Kotlin, #124 Ruby, #141 SQL, #142 Python, #145 Go+PHP, #148 chi `*Mux`, #364 Go stdlib interfaces, #113 PHP project-local.
- Cross-language structural-refs CONTAINS pattern: #143 Ruby, #144 multi-lang, #146 helper.

This run is **partial** by time-budget design (60-min cap on a single workstation): a
representative subset of 32 repos covering all 12 supported languages from the
already-cached corpus (`$ARCHIGRAPH_CORPORA_DIR`). Three framework-source repos
(`django`, `nextjs`, `aspnetcore-mvc`) timed out at the 600 s per-repo cap and were
excluded — these are explicitly out-of-policy under #96 (sample apps, not framework
internals). A full 289-repo run is queued as follow-up.

## Headline

| metric | value |
| --- | ---: |
| repos measured | 32 |
| total files | 6,275 |
| total relationships | 245,971 |
| total endpoints classified | 491,942 |
| **aggregate bug_rate** | **32.26 %** |
| #44 ship-gate target (≤ 1 %) | **NOT MET** |

For comparison, the post-#96 baseline measured in #109 reported aggregate bug-rates
of 49–69 % per language across 7 languages. The new aggregate (32.26 %) reflects the
landed structural-fix work but is still ~32× the v1.0 ship-gate.

## Per-repo

| repo | lang | files | rels | endpoints | bug_rate % | bar % | hit_bar | ref |
| --- | --- | ---: | ---: | ---: | ---: | ---: | :---: | --- |
| actix-examples | rust | 460 | 6,100 | 12,200 | 22.46 | 25 | YES | #108/#111 |
| aspnetcore-realworld | csharp | 97 | 1,288 | 2,576 | 28.14 | 35 | YES | #109-cs |
| chi | go | 93 | 3,749 | 7,498 | 34.77 | 35 | YES | #103/#148/#364 |
| click | python | 138 | 7,474 | 14,948 | 32.60 | 10 | NO | #42 |
| django-realworld | python | 48 | 530 | 1,060 | 19.06 | 15 | NO | #142 |
| etcd | go | 424 | 29,505 | 59,010 | 33.46 | 35 | YES | #103 |
| exposed | kotlin | 117 | 4,236 | 8,472 | 13.09 | 25 | YES | #106 |
| express | javascript | 208 | 1,068 | 2,136 | 19.62 | 25 | YES | #104 |
| express-realworld | javascript | 66 | 346 | 692 | 26.45 | 25 | NO | #104 |
| flask | python | 225 | 5,782 | 11,564 | 43.93 | 10 | NO | #42 |
| flask-realworld | python | 43 | 934 | 1,868 | 43.63 | 15 | NO | #142 |
| gin | go | 121 | 11,327 | 22,654 | 38.48 | 35 | NO | #103 |
| kafka | java | 489 | 28,197 | 56,394 | 21.09 | 35 | YES | #105 |
| ktor | kotlin | 245 | 4,731 | 9,462 | 29.87 | 25 | NO | #106 |
| ktor-samples | kotlin | 509 | 4,612 | 9,224 | 34.88 | 25 | NO | #106 |
| laravel-quickstart | php | 83 | 191 | 382 | 32.98 | 25 | NO | #113 |
| laravel-routing | php | 90 | 2,471 | 4,942 | 21.17 | 25 | YES | #113 |
| mini-redis | rust | 33 | 1,047 | 2,094 | 17.14 | 25 | YES | #108/#111 |
| nestjs | typescript | 289 | 7,731 | 15,462 | 38.68 | 25 | NO | #104 |
| nestjs-starter | typescript | 16 | 56 | 112 | 16.96 | 25 | YES | #104 |
| pandas | python | 197 | 30,341 | 60,682 | 14.66 | — | — | #96-ignored |
| phoenix-todo-list | elixir | 69 | 714 | 1,428 | 12.75 | 25 | YES | #109 |
| rails-actionpack | ruby | 541 | 31,711 | 63,422 | 20.02 | 15 | NO | #107 |
| rails-realworld | ruby | 105 | 263 | 526 | 6.84 | 15 | YES | #107 |
| requests | python | 111 | 23,040 | 46,080 | 86.94 | 10 | NO | #42 |
| sidekiq | ruby | 85 | 4,733 | 9,466 | 15.24 | 15 | NO | #107 |
| spring-petclinic | java | 120 | 2,285 | 4,570 | 31.33 | 35 | YES | #105 |
| symfony-demo | php | 241 | 1,499 | 2,998 | 37.19 | 25 | NO | #113 |
| symfony-routing | php | 375 | 9,087 | 18,174 | 48.92 | 25 | NO | #113 |
| tokio | rust | 389 | 18,370 | 36,740 | 26.82 | 25 | NO | #108/#111 |
| vapor | swift | 227 | 2,506 | 5,012 | 27.41 | 25 | NO | #109-swift |
| vapor-api-template | swift | 21 | 47 | 94 | 32.98 | 25 | NO | #109-swift |

## Per-language aggregate

| lang | repos | files | rels | endpoints | bug_rate % |
| --- | ---: | ---: | ---: | ---: | ---: |
| csharp | 1 | 97 | 1,288 | 2,576 | 28.14 |
| elixir | 1 | 69 | 714 | 1,428 | 12.75 |
| go | 3 | 638 | 44,581 | 89,162 | 34.85 |
| java | 2 | 609 | 30,482 | 60,964 | 21.86 |
| javascript | 2 | 274 | 1,414 | 2,828 | 21.29 |
| kotlin | 3 | 871 | 13,579 | 27,158 | 26.33 |
| php | 4 | 789 | 13,248 | 26,496 | 42.18 |
| python | 6 | 762 | 68,101 | 136,202 | 44.00 |
| ruby | 3 | 731 | 36,707 | 73,414 | 19.31 |
| rust | 3 | 882 | 25,517 | 51,034 | 25.38 |
| swift | 2 | 248 | 2,553 | 5,106 | 27.52 |
| typescript | 2 | 305 | 7,787 | 15,574 | 38.52 |

## Top patterns in residual bug-extractor edges

Sampled across all 32 repos. Top recurring stub names:

1. `append` — generic JS/Python builtin still reaching resolver in 3+ repos.
2. `add` — same shape as `append`; 3 repos.
3. `register_blueprint` — Flask app-factory DSL; same-class receiver binding gap.
4. `bind`, `default`, `get` — collision-prone bare names; need per-language gating.
5. Manifest-shape stubs (`docker-compose.yml`, `eslint.config.mjs`, container image
   refs like `mcr.microsoft.com/dotnet/sdk:10.0`) — config/manifest extractors are
   emitting external references that aren't being routed to `external-known`.

The residual swift+typescript languages (vapor, nestjs) are the largest remaining
single-language gaps relative to bar.

## Top patterns in residual bug-resolver edges

1. `send`, `init`, `get`, `config` (3 repos each) — common method names that
   `nameExists()` matches but the resolver can't bind to a callable target. Same
   shape that motivated the SQL kind-aware fix in #141; needs to be extended to
   other "name collides cross-kind" cases.
2. `new` (2 repos) — Ruby/JS constructor sugar; partially handled by #124, more
   surface area remains.
3. `App`, `Route` (2 repos each) — framework class name collisions in TS / Vapor.
4. `MediatR`, `FluentValidation` — C# library names dropping into bug-resolver via
   the same `nameExists()` ambiguity.

## Per-repo issues that should now be CLOSED (acceptance bar HIT)

| repo | lang | bug_rate | bar | umbrella ref |
| --- | --- | ---: | ---: | --- |
| actix-examples | rust | 22.46 % | 25 % | #108/#111 |
| aspnetcore-realworld | csharp | 28.14 % | 35 % | #109 |
| chi | go | 34.77 % | 35 % | #103/#148/#364 |
| etcd | go | 33.46 % | 35 % | #103 |
| exposed | kotlin | 13.09 % | 25 % | #106 |
| express | javascript | 19.62 % | 25 % | #104 |
| kafka | java | 21.09 % | 35 % | #105 |
| laravel-routing | php | 21.17 % | 25 % | #113 |
| mini-redis | rust | 17.14 % | 25 % | #108/#111 |
| nestjs-starter | typescript | 16.96 % | 25 % | #104 |
| phoenix-todo-list | elixir | 12.75 % | 25 % | #109 |
| rails-realworld | ruby | 6.84 % | 15 % | #107 |
| spring-petclinic | java | 31.33 % | 35 % | #105 |

13 / 32 repos are at or below their per-repo bar. Recommend closing the
follow-up structural-fix issues (#103, #105, #106, #107, #108, #111, #113, #148,
#364) once spot-checked, while leaving #44 itself open until aggregate ≤ 1 %.

## Per-repo gaps still open

| repo | lang | bug_rate | bar | gap pp | proposed action |
| --- | --- | ---: | ---: | ---: | --- |
| requests | python | 86.94 % | 10 | +76.94 | dotted-import resolution gap; library-source artefact (revisit #96 policy or special-case stdlib) |
| flask | python | 43.93 % | 10 | +33.93 | same-class receiver binding for Flask app-factory DSL — file follow-up to #142 |
| flask-realworld | python | 43.63 % | 15 | +28.63 | same gap as `flask`; close once flask drops |
| symfony-routing | php | 48.92 % | 25 | +23.92 | extend #113 with dotted FQN segment matching for Symfony component refs |
| nestjs | typescript | 38.68 % | 25 | +13.68 | TS decorator metadata + injected-class receiver binding; analogue to #120 for TS |
| gin | go | 38.48 % | 35 | +3.48  | residual after #103/#148/#364; small follow-up — bare-name `panic` and HTTP helper allowlist |
| symfony-demo | php | 37.19 % | 25 | +12.19 | as `symfony-routing` |
| ktor-samples | kotlin | 34.88 % | 25 | +9.88  | residual after #122; need same-class field-access handling completed |
| click | python | 32.60 % | 10 | +22.60 | bare-name `paramtype/ctx/default/flag_value` decorator-DSL — follow-up to #142 |
| laravel-quickstart | php | 32.98 % | 25 | +7.98  | small fixture; sampling noise dominates — recheck after #113 follow-ups |
| vapor-api-template | swift | 32.98 % | 25 | +7.98  | small fixture (94 endpoints); Vapor `@Service` receiver binding |
| ktor | kotlin | 29.87 % | 25 | +4.87  | as ktor-samples |
| vapor | swift | 27.41 % | 25 | +2.41  | minor; routing DSL bare names |
| tokio | rust | 26.82 % | 25 | +1.82  | very small gap; trait-method bare names |
| express-realworld | javascript | 26.45 % | 25 | +1.45  | minor; Mongoose model-method bare names |
| sidekiq | ruby | 15.24 % | 15 | +0.24  | within noise; one more pass on #124 should close it |
| rails-actionpack | ruby | 20.02 % | 15 | +5.02  | as sidekiq + #107 follow-up for ActionController callback DSL |
| django-realworld | python | 19.06 % | 15 | +4.06  | follow-up to #142 for Django manager method bare names |

### Proposed follow-up issues to file

- **PYTHON-FLASK-DSL-RECEIVER**: same-class receiver binding for Flask
  `app.register_blueprint`, `app.route`, etc. — covers `flask` + `flask-realworld`.
- **TS-NESTJS-INJECTED-RECEIVER**: TypeScript analogue of #120 — bind decorator-injected
  service receivers across files.
- **PHP-SYMFONY-DOTTED-FQN**: extend #113 to handle dotted Symfony component FQNs
  unresolved by the project-local fix.
- **PYTHON-CLICK-DSL**: extend dynamic catalog with click-specific decorator DSL.
- **MANIFEST-EXTERNAL-ROUTING**: route container image refs / docker-compose service
  names / config file paths to `external-known` instead of `bug-extractor`.

## Methodology / reproducibility

- Binary: built fresh from `investigate/issue-44-fresh-baseline` at HEAD `d8932a1`.
- Indexer: `archigraph index --json-stats <repo>` per repo, 600 s timeout, 6-way
  parallel via `xargs -P6`.
- Aggregation: `/tmp/verify2-results/aggregate.py` (preserved alongside the JSON
  outputs in `/tmp/verify2-results/json/`).
- Per-repo bars sourced from issues #44, #91/#92 (per-language buckets), #103
  (Go), #104 (JS/TS), #105 (Java), #106 (Kotlin), #107 (Ruby), #108/#111 (Rust),
  #109 (C#/Swift/Elixir), #113 (PHP), #142 (Python sample apps).

## Next-run notes

- Full 289-repo corpus run (chunks A–EE) needs a longer walltime budget (likely
  4–6 h on this workstation) and a coordinator that retries timed-out repos at a
  larger per-repo cap.
- `requests` library-source bug-rate (86.94 %) is the single largest per-repo
  outlier. Reclassify it under the #96 policy as library-source (skip from the
  ship-gate aggregate, like `pandas`/`django` already are) or land a stdlib
  dotted-import fix targeted at it.
