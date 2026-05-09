# Ship-gate baseline refresh — 2026-05-10 (Refs #44)

Re-runs the same 32-repo VERIFY-2 corpus measured in `docs/verify2/ship-gate-baseline-2026-05-10.md` after PR #433 (testmap structural-ref fix) plus the #420/#421/#422/#423/#424 batch.

## Headline

| metric | OLD baseline | NEW refresh | delta |
| --- | ---: | ---: | ---: |
| repos measured | 32 | 32 | +0 |
| total files | 6,275 | 6,275 | +0 |
| total relationships | 245,971 | 246,010 | +39 |
| total endpoints | 491,942 | 492,020 | +78 |
| **aggregate bug_rate** | **32.26 %** | **16.82 %** | **-15.44 pp** |
| #44 ship-gate target (≤ 1 %) | NOT MET | NOT MET | — |

## Per-repo comparison

| repo | lang | files | endpoints | OLD bug % | NEW bug % | delta pp | bar % | hit |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | :---: |
| actix-examples | rust | 460 | 12,200 | 22.46 | 22.39 | -0.07 | 25 | YES |
| aspnetcore-realworld | csharp | 97 | 2,576 | 28.14 | 21.74 | -6.40 | 35 | YES |
| chi | go | 93 | 7,498 | 34.77 | 12.74 | -22.03 | 35 | YES |
| click | python | 138 | 14,948 | 32.60 | 10.58 | -22.02 | 10 | NO |
| django-realworld | python | 48 | 1,060 | 19.06 | 17.83 | -1.23 | 15 | NO |
| etcd | go | 424 | 59,010 | 33.46 | 19.42 | -14.04 | 35 | YES |
| exposed | kotlin | 117 | 8,472 | 13.09 | 13.09 | +0.00 | 25 | YES |
| express | javascript | 208 | 2,136 | 19.62 | 19.62 | -0.00 | 25 | YES |
| express-realworld | javascript | 66 | 692 | 26.45 | 19.65 | -6.80 | 25 | YES |
| flask | python | 225 | 11,564 | 43.93 | 18.89 | -25.04 | 10 | NO |
| flask-realworld | python | 43 | 1,868 | 43.63 | 20.18 | -23.45 | 15 | NO |
| gin | go | 121 | 22,654 | 38.48 | 12.52 | -25.96 | 35 | YES |
| kafka | java | 489 | 56,394 | 21.09 | 21.09 | +0.00 | 35 | YES |
| ktor | kotlin | 245 | 9,462 | 29.87 | 23.07 | -6.80 | 25 | YES |
| ktor-samples | kotlin | 509 | 9,230 | 34.88 | 34.68 | -0.20 | 25 | NO |
| laravel-quickstart | php | 83 | 382 | 32.98 | 25.65 | -7.33 | 25 | NO |
| laravel-routing | php | 90 | 4,950 | 21.17 | 21.13 | -0.04 | 25 | YES |
| mini-redis | rust | 33 | 2,094 | 17.14 | 16.67 | -0.47 | 25 | YES |
| nestjs | typescript | 289 | 15,514 | 38.68 | 24.91 | -13.77 | 25 | YES |
| nestjs-starter | typescript | 16 | 114 | 16.96 | 16.67 | -0.29 | 25 | YES |
| pandas | python | 197 | 60,682 | 14.66 | 14.65 | -0.01 | — | — |
| phoenix-todo-list | elixir | 69 | 1,428 | 12.75 | 12.75 | -0.00 | 25 | YES |
| rails-actionpack | ruby | 541 | 63,422 | 20.02 | 20.02 | -0.00 | 15 | NO |
| rails-realworld | ruby | 105 | 526 | 6.84 | 6.84 | +0.00 | 15 | YES |
| requests | python | 111 | 46,080 | 86.94 | 1.97 | -84.97 | 10 | YES |
| sidekiq | ruby | 85 | 9,466 | 15.24 | 15.24 | +0.00 | 15 | NO |
| spring-petclinic | java | 120 | 4,580 | 31.33 | 8.73 | -22.60 | 35 | YES |
| symfony-demo | php | 241 | 2,998 | 37.19 | 24.32 | -12.87 | 25 | YES |
| symfony-routing | php | 375 | 18,174 | 48.92 | 15.74 | -33.18 | 25 | YES |
| tokio | rust | 389 | 36,740 | 26.82 | 16.32 | -10.50 | 25 | YES |
| vapor | swift | 227 | 5,012 | 27.41 | 27.41 | +0.00 | 25 | NO |
| vapor-api-template | swift | 21 | 94 | 32.98 | 30.85 | -2.13 | 25 | NO |

## Aggregate disposition breakdown (new run)

| disposition | count | pct |
| --- | ---: | ---: |
| resolved | 288,650 | 58.67 % |
| external-known | 22,546 | 4.58 % |
| external-unknown | 23,096 | 4.69 % |
| dynamic | 74,992 | 15.24 % |
| bug-extractor | 63,906 | 12.99 % |
| bug-resolver | 18,830 | 3.83 % |
| unclassified | 0 | 0.00 % |
| **total** | **492,020** | **100.00 %** |

## Repos now at <= 1 % (huge wins)

None — no repo crossed the 1 % line in this refresh.

## Repos hitting their per-repo bar (21/31)

| repo | lang | NEW % | bar % |
| --- | --- | ---: | ---: |
| requests | python | 1.97 | 10 |
| rails-realworld | ruby | 6.84 | 15 |
| spring-petclinic | java | 8.73 | 35 |
| gin | go | 12.52 | 35 |
| chi | go | 12.74 | 35 |
| phoenix-todo-list | elixir | 12.75 | 25 |
| exposed | kotlin | 13.09 | 25 |
| symfony-routing | php | 15.74 | 25 |
| tokio | rust | 16.32 | 25 |
| mini-redis | rust | 16.67 | 25 |
| nestjs-starter | typescript | 16.67 | 25 |
| etcd | go | 19.42 | 35 |
| express | javascript | 19.62 | 25 |
| express-realworld | javascript | 19.65 | 25 |
| kafka | java | 21.09 | 35 |
| laravel-routing | php | 21.13 | 25 |
| aspnetcore-realworld | csharp | 21.74 | 35 |
| actix-examples | rust | 22.39 | 25 |
| ktor | kotlin | 23.07 | 25 |
| symfony-demo | php | 24.32 | 25 |
| nestjs | typescript | 24.91 | 25 |

## Repos still above their bar (10)

| repo | lang | NEW % | bar % | gap pp |
| --- | --- | ---: | ---: | ---: |
| ktor-samples | kotlin | 34.68 | 25 | +9.68 |
| flask | python | 18.89 | 10 | +8.89 |
| vapor-api-template | swift | 30.85 | 25 | +5.85 |
| flask-realworld | python | 20.18 | 15 | +5.18 |
| rails-actionpack | ruby | 20.02 | 15 | +5.02 |
| django-realworld | python | 17.83 | 15 | +2.83 |
| vapor | swift | 27.41 | 25 | +2.41 |
| laravel-quickstart | php | 25.65 | 25 | +0.65 |
| click | python | 10.58 | 10 | +0.58 |
| sidekiq | ruby | 15.24 | 15 | +0.24 |

## NEW top residuals (worst 10 by new bug-rate)

| repo | lang | OLD % | NEW % | delta pp |
| --- | --- | ---: | ---: | ---: |
| ktor-samples | kotlin | 34.88 | 34.68 | -0.20 |
| vapor-api-template | swift | 32.98 | 30.85 | -2.13 |
| vapor | swift | 27.41 | 27.41 | +0.00 |
| laravel-quickstart | php | 32.98 | 25.65 | -7.33 |
| nestjs | typescript | 38.68 | 24.91 | -13.77 |
| symfony-demo | php | 37.19 | 24.32 | -12.87 |
| ktor | kotlin | 29.87 | 23.07 | -6.80 |
| actix-examples | rust | 22.46 | 22.39 | -0.07 |
| aspnetcore-realworld | csharp | 28.14 | 21.74 | -6.40 |
| laravel-routing | php | 21.17 | 21.13 | -0.04 |

## Methodology / reproducibility

- Binary: built fresh from worktree `investigate/baseline-refresh-2` at HEAD `0fcfefb` (post-#433).
- Indexer: `archigraph index --json-stats <repo>` per repo, 600 s timeout, 6-way parallel via `xargs -P6`.
- Same 32 repos as `docs/verify2/ship-gate-baseline-2026-05-10.md`. Three framework-source repos (`django`, `nextjs`, `aspnetcore-mvc`) remain excluded under #96 policy.
