# Ship-gate baseline refresh v2 — 2026-05-10 (Refs #44)

Re-runs the same 32-repo VERIFY-2 corpus measured in
`docs/verify2/ship-gate-baseline-refresh-2026-05-10.md` after the DSL-classification
batch:

- #435 Kotlin Ktor DSL methods classified
- #436 Swift Vapor DSL methods classified
- #439 PHP Laravel + Symfony DSL classified
- #440 Rust Actix-web DSL classified
- #441 C# ASP.NET Core MVC + EF Core DSL classified

## Headline

| metric | refresh-1 (post-#420..#424 + #433) | refresh-2 (post-#435..#441) | delta |
| --- | ---: | ---: | ---: |
| repos measured | 32 | 32 | +0 |
| total files | 6,275 | 6,275 | +0 |
| total relationships | 246,010 | 246,010 | +0 |
| total endpoints | 492,020 | 492,020 | +0 |
| **aggregate bug_rate** | **16.82 %** | **16.50 %** | **-0.32 pp** |
| #44 ship-gate target (≤ 1 %) | NOT MET | NOT MET | — |

Total endpoint counts are unchanged because none of #435/#436/#439/#440/#441 alter
extraction or relationship emission — they re-classify already-emitted endpoints
out of `bug-extractor` / `bug-resolver` into `resolved`. Net effect is a 0.32 pp
improvement and three repos crossing their per-repo bars.

## Per-repo comparison

| repo | lang | files | endpoints | OLD bug % | NEW bug % | delta pp | bar % | hit |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | :---: |
| actix-examples | rust | 460 | 12,200 | 22.39 | 19.41 | -2.98 | 25 | YES |
| aspnetcore-realworld | csharp | 97 | 2,576 | 21.74 | 17.93 | -3.81 | 35 | YES |
| chi | go | 93 | 7,498 | 12.74 | 12.74 | -0.00 | 35 | YES |
| click | python | 138 | 14,948 | 10.58 | 10.58 | +0.00 | 10 | NO |
| django-realworld | python | 48 | 1,060 | 17.83 | 17.83 | +0.00 | 15 | NO |
| etcd | go | 424 | 59,010 | 19.42 | 19.42 | -0.00 | 35 | YES |
| exposed | kotlin | 117 | 8,472 | 13.09 | 13.09 | +0.00 | 25 | YES |
| express | javascript | 208 | 2,136 | 19.62 | 19.62 | -0.00 | 25 | YES |
| express-realworld | javascript | 66 | 692 | 19.65 | 19.65 | +0.00 | 25 | YES |
| flask | python | 225 | 11,564 | 18.89 | 18.89 | +0.00 | 10 | NO |
| flask-realworld | python | 43 | 1,868 | 20.18 | 20.18 | +0.00 | 15 | NO |
| gin | go | 121 | 22,654 | 12.52 | 12.52 | +0.00 | 35 | YES |
| kafka | java | 489 | 56,394 | 21.09 | 21.09 | +0.00 | 35 | YES |
| ktor | kotlin | 245 | 9,462 | 23.07 | 21.33 | -1.74 | 25 | YES |
| ktor-samples | kotlin | 509 | 9,230 | 34.68 | 31.66 | -3.02 | 25 | NO |
| laravel-quickstart | php | 83 | 382 | 25.65 | 24.35 | -1.30 | 25 | YES |
| laravel-routing | php | 90 | 4,950 | 21.13 | 20.10 | -1.03 | 25 | YES |
| mini-redis | rust | 33 | 2,094 | 16.67 | 16.67 | -0.00 | 25 | YES |
| nestjs | typescript | 289 | 15,514 | 24.91 | 24.91 | +0.00 | 25 | YES |
| nestjs-starter | typescript | 16 | 114 | 16.67 | 16.67 | -0.00 | 25 | YES |
| pandas | python | 197 | 60,682 | 14.65 | 14.65 | -0.00 | — | — |
| phoenix-todo-list | elixir | 69 | 1,428 | 12.75 | 12.75 | -0.00 | 25 | YES |
| rails-actionpack | ruby | 541 | 63,422 | 20.02 | 20.02 | -0.00 | 15 | NO |
| rails-realworld | ruby | 105 | 526 | 6.84 | 6.84 | +0.00 | 15 | YES |
| requests | python | 111 | 46,080 | 1.97 | 1.97 | +0.00 | 10 | YES |
| sidekiq | ruby | 85 | 9,466 | 15.24 | 15.24 | +0.00 | 15 | NO |
| spring-petclinic | java | 120 | 4,580 | 8.73 | 8.73 | +0.00 | 35 | YES |
| symfony-demo | php | 241 | 2,998 | 24.32 | 23.38 | -0.94 | 25 | YES |
| symfony-routing | php | 375 | 18,174 | 15.74 | 15.50 | -0.24 | 25 | YES |
| tokio | rust | 389 | 36,740 | 16.32 | 16.18 | -0.14 | 25 | YES |
| vapor | swift | 227 | 5,012 | 27.41 | 18.75 | -8.66 | 25 | YES |
| vapor-api-template | swift | 21 | 94 | 30.85 | 21.28 | -9.57 | 25 | YES |

## Aggregate disposition breakdown (new run, 32 repos incl. pandas)

| disposition | count | pct |
| --- | ---: | ---: |
| resolved | 288,650 | 58.67 % |
| external-known | 22,546 | 4.58 % |
| external-unknown | 24,625 | 5.00 % |
| dynamic | 74,992 | 15.24 % |
| bug-extractor | 62,715 | 12.75 % |
| bug-resolver | 18,492 | 3.76 % |
| unclassified | 0 | 0.00 % |
| **total** | **492,020** | **100.00 %** |

(Disposition totals derived by re-summing per-repo `disposition_counts` from the
fresh JSON stats; per-repo files/rels/endpoints are byte-identical to refresh-1
because none of the merged PRs touch extraction or relationship emission.)

## Repos crossing their per-repo bar (3 wins)

| repo | lang | OLD % | NEW % | bar % | source PR |
| --- | --- | ---: | ---: | ---: | --- |
| laravel-quickstart | php | 25.65 | 24.35 | 25 | #439 |
| vapor | swift | 27.41 | 18.75 | 25 | #436 |
| vapor-api-template | swift | 30.85 | 21.28 | 25 | #436 |

Repos at their per-repo bar increased from **21/31** (refresh-1) to **24/31** (refresh-2).

## Repos now at ≤ 8 %

| repo | lang | NEW % | bar % |
| --- | --- | ---: | ---: |
| requests | python | 1.97 | 10 |
| rails-realworld | ruby | 6.84 | 15 |

No new repos crossed the 8 % line in this refresh; only the same two from
refresh-1 sit below it.

## Repos now at ≤ 1 % (#44 ship-gate target)

None. The #44 ship-gate target remains **NOT MET**. The two below 8 % are
`requests` (1.97 %, ↑0.97 pp above target) and `rails-realworld` (6.84 %).

## Repos still above their bar (7)

| repo | lang | NEW % | bar % | gap pp |
| --- | --- | ---: | ---: | ---: |
| ktor-samples | kotlin | 31.66 | 25 | +6.66 |
| flask | python | 18.89 | 10 | +8.89 |
| flask-realworld | python | 20.18 | 15 | +5.18 |
| rails-actionpack | ruby | 20.02 | 15 | +5.02 |
| django-realworld | python | 17.83 | 15 | +2.83 |
| click | python | 10.58 | 10 | +0.58 |
| sidekiq | ruby | 15.24 | 15 | +0.24 |

Down from 10 (refresh-1) → 7 (refresh-2). The three swift/php fixes (vapor,
vapor-api-template, laravel-quickstart) all dropped under their bars; the
remaining residuals are concentrated in Python (4) and Ruby (2) plus
ktor-samples — none of which were targeted by the merged PRs.

## Top movers (by improvement)

| repo | lang | OLD % | NEW % | delta pp | source PR |
| --- | --- | ---: | ---: | ---: | --- |
| vapor-api-template | swift | 30.85 | 21.28 | -9.57 | #436 |
| vapor | swift | 27.41 | 18.75 | -8.66 | #436 |
| aspnetcore-realworld | csharp | 21.74 | 17.93 | -3.81 | #441 |
| ktor-samples | kotlin | 34.68 | 31.66 | -3.02 | #435 |
| actix-examples | rust | 22.39 | 19.41 | -2.98 | #440 |
| ktor | kotlin | 23.07 | 21.33 | -1.74 | #435 |
| laravel-quickstart | php | 25.65 | 24.35 | -1.30 | #439 |
| laravel-routing | php | 21.13 | 20.10 | -1.03 | #439 |
| symfony-demo | php | 24.32 | 23.38 | -0.94 | #439 |
| symfony-routing | php | 15.74 | 15.50 | -0.24 | #439 |
| tokio | rust | 16.32 | 16.18 | -0.14 | #440 |

All 11 movers are exactly the repos targeted by the DSL-classification batch
(#435 Ktor, #436 Vapor, #439 PHP Laravel/Symfony, #440 Rust Actix, #441 C#
ASP.NET Core MVC + EF Core). Repos in untouched languages (Python, Ruby,
Java, Go, JavaScript/TypeScript) registered 0.00 pp delta — confirms the
batch is non-regressive elsewhere.

## Methodology / reproducibility

- Worktree: `/Users/jorgecajas/Documents/Projects/archigraph-worktrees/baseline-3`
- Branch: `investigate/baseline-3` off `origin/main` @ `b556b46` (post-#444 merge,
  i.e. all five DSL fixes landed).
- Binary: `go build -o /tmp/archigraph-baseline-3 ./cmd/archigraph` from worktree HEAD.
- Indexer: `archigraph index --json-stats <repo>` per repo, 600 s `gtimeout` cap,
  6-way parallel via `xargs -P6`.
- Same 32 repos as `docs/verify2/ship-gate-baseline-2026-05-10.md` and
  `docs/verify2/ship-gate-baseline-refresh-2026-05-10.md`. Three framework-source
  repos (`django`, `nextjs`, `aspnetcore-mvc`) remain excluded under #96 policy.
- All 32 repos succeeded; no timeouts, no failures.
