# Ship-gate baseline refresh v4 — 2026-05-18 (Refs #44)

Re-runs the same 32-repo VERIFY-2 corpus measured in
`docs/verify2/ship-gate-baseline-refresh-v3.md` after the
Python (#455) and Kotlin (#456) bare-name residual fixes landed on
`main`:

- #455 Python stdlib + typing bare-name allowlist extension (merged via #458)
- #456 Kotlin serialization + coroutines + collections bare-name extension (merged via #457)

## Headline

| metric | v3 (post-#446..#449) | v4 (post-#455/#456) | delta |
| --- | ---: | ---: | ---: |
| repos measured | 32 | 32 | +0 |
| total files | 6,275 | 6,275 | +0 |
| total relationships | 246,010 | 246,010 | +0 |
| total endpoints | 492,020 | 492,020 | +0 |
| **aggregate bug_rate** | **15.95 %** | **15.63 %** | **-0.32 pp** |
| #44 ship-gate target (≤ 1 %) | NOT MET | NOT MET | — |

Total file/relationship/endpoint counts are unchanged because #455/#456
extend the bare-name allowlist used during resolver classification —
they re-classify already-emitted endpoints out of `bug-extractor` into
`external-unknown` (stdlib/known-library symbol references) rather than
altering extraction or relationship emission. Net effect is a 0.32 pp
improvement, one repo crossing its per-repo bar, and no regressions.

## Per-repo comparison

| repo | lang | files | endpoints | OLD bug % | NEW bug % | delta pp | bar % | hit |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | :---: |
| actix-examples | rust | 460 | 12,200 | 19.40 | 19.39 | -0.01 | 25 | YES |
| aspnetcore-realworld | csharp | 97 | 2,576 | 17.93 | 17.93 | +0.00 | 35 | YES |
| chi | go | 93 | 7,498 | 12.74 | 12.74 | +0.00 | 35 | YES |
| click | python | 138 | 14,948 | 10.16 | 6.95 | -3.21 | 10 | YES |
| django-realworld | python | 48 | 1,060 | 13.96 | 13.96 | +0.00 | 15 | YES |
| etcd | go | 424 | 59,010 | 19.42 | 19.42 | +0.00 | 35 | YES |
| exposed | kotlin | 117 | 8,472 | 13.09 | 12.51 | -0.58 | 25 | YES |
| express | javascript | 208 | 2,136 | 19.62 | 19.62 | +0.00 | 25 | YES |
| express-realworld | javascript | 66 | 692 | 19.65 | 19.65 | +0.00 | 25 | YES |
| flask | python | 225 | 11,564 | 16.88 | 14.19 | -2.69 | 10 | NO |
| flask-realworld | python | 43 | 1,868 | 16.70 | 15.10 | -1.61 | 15 | NO |
| gin | go | 121 | 22,654 | 12.52 | 12.52 | +0.00 | 35 | YES |
| kafka | java | 489 | 56,394 | 21.09 | 21.09 | +0.00 | 35 | YES |
| ktor | kotlin | 245 | 9,462 | 21.33 | 20.49 | -0.83 | 25 | YES |
| ktor-samples | kotlin | 509 | 9,230 | 31.66 | 29.35 | -2.31 | 25 | NO |
| laravel-quickstart | php | 83 | 382 | 24.35 | 24.35 | +0.00 | 25 | YES |
| laravel-routing | php | 90 | 4,950 | 20.10 | 20.10 | +0.00 | 25 | YES |
| mini-redis | rust | 33 | 2,094 | 16.67 | 16.67 | +0.00 | 25 | YES |
| nestjs | typescript | 289 | 15,514 | 24.91 | 24.91 | +0.00 | 25 | YES |
| nestjs-starter | typescript | 16 | 114 | 16.67 | 16.67 | +0.00 | 25 | YES |
| pandas | python | 197 | 60,682 | 14.41 | 13.88 | -0.53 | — | — |
| phoenix-todo-list | elixir | 69 | 1,428 | 12.75 | 12.75 | +0.00 | 25 | YES |
| rails-actionpack | ruby | 541 | 63,422 | 16.84 | 16.84 | +0.00 | 15 | NO |
| rails-realworld | ruby | 105 | 526 | 6.65 | 6.65 | +0.00 | 15 | YES |
| requests | python | 111 | 46,080 | 1.88 | 1.72 | -0.15 | 10 | YES |
| sidekiq | ruby | 85 | 9,466 | 13.85 | 13.85 | +0.00 | 15 | YES |
| spring-petclinic | java | 120 | 4,580 | 8.73 | 8.73 | +0.00 | 35 | YES |
| symfony-demo | php | 241 | 2,998 | 23.38 | 23.38 | +0.00 | 25 | YES |
| symfony-routing | php | 375 | 18,174 | 15.50 | 15.50 | +0.00 | 25 | YES |
| tokio | rust | 389 | 36,740 | 16.18 | 16.18 | +0.00 | 25 | YES |
| vapor | swift | 227 | 5,012 | 18.75 | 18.75 | +0.00 | 25 | YES |
| vapor-api-template | swift | 21 | 94 | 21.28 | 21.28 | +0.00 | 25 | YES |

## Aggregate disposition breakdown (new run, 32 repos incl. pandas)

| disposition | count | pct |
| --- | ---: | ---: |
| resolved | 288,650 | 58.67 % |
| external-known | 22,546 | 4.58 % |
| external-unknown | 27,162 | 5.52 % |
| dynamic | 76,747 | 15.60 % |
| bug-extractor | 60,487 | 12.29 % |
| bug-resolver | 16,428 | 3.34 % |
| unclassified | 0 | 0.00 % |
| **total** | **492,020** | **100.00 %** |

Resolved count is byte-identical to v3 (288,650 / 58.67 %); the
bare-name allowlist extensions in #455/#456 moved endpoints from the
`bug-extractor` bucket into `external-unknown` (stdlib/known-library
references resolved to external symbols rather than being flagged as
unresolved bare names):

| disposition | v3 | v4 | delta |
| --- | ---: | ---: | ---: |
| resolved | 288,650 | 288,650 | +0 |
| external-known | 22,546 | 22,546 | +0 |
| external-unknown | 25,311 | 27,162 | +1,851 |
| dynamic | 77,045 | 76,747 | -298 |
| bug-extractor | 62,040 | 60,487 | -1,553 |
| bug-resolver | 16,428 | 16,428 | +0 |
| unclassified | 0 | 0 | +0 |

`bug-extractor + bug-resolver` drops by 1,553 endpoints
(78,468 → 76,915), driving the headline `bug_rate` from 15.95 % to
15.63 %.

## Repos crossing their per-repo bar (1 win)

| repo | lang | OLD % | NEW % | bar % | source PR |
| --- | --- | ---: | ---: | ---: | --- |
| click | python | 10.16 | 6.95 | 10 | #455 (#458) |

Repos at their per-repo bar increased from **26/31** (v3) to **27/31**
(v4).

## Repos still above their bar (4)

| repo | lang | NEW % | bar % | gap pp |
| --- | --- | ---: | ---: | ---: |
| ktor-samples | kotlin | 29.35 | 25 | +4.35 |
| flask | python | 14.19 | 10 | +4.19 |
| rails-actionpack | ruby | 16.84 | 15 | +1.84 |
| flask-realworld | python | 15.10 | 15 | +0.10 |

Down from 5 (v3) → 4 (v4). `click` cleared its bar; `flask`,
`flask-realworld`, and `ktor-samples` all improved meaningfully but did
not fully cross. `rails-actionpack` was untouched by this batch (Ruby
residuals not in scope).

## Repos now at ≤ 8 %

| repo | lang | NEW % | bar % |
| --- | --- | ---: | ---: |
| requests | python | 1.72 | 10 |
| rails-realworld | ruby | 6.65 | 15 |
| click | python | 6.95 | 10 |

One new repo crossed the 8 % line in this refresh: `click` joined
`requests` and `rails-realworld` below the line, driven by #455.

## Repos now at ≤ 1 % (#44 ship-gate target)

None. The #44 ship-gate target remains **NOT MET**. The closest is
`requests` (1.72 %, ↑0.72 pp above target), down from 1.88 % in v3.

## Top movers (by improvement)

| repo | lang | OLD % | NEW % | delta pp | source PR |
| --- | --- | ---: | ---: | ---: | --- |
| click | python | 10.16 | 6.95 | -3.21 | #455 (#458) |
| flask | python | 16.88 | 14.19 | -2.69 | #455 (#458) |
| ktor-samples | kotlin | 31.66 | 29.35 | -2.31 | #456 (#457) |
| flask-realworld | python | 16.70 | 15.10 | -1.61 | #455 (#458) |
| ktor | kotlin | 21.33 | 20.49 | -0.83 | #456 (#457) |
| exposed | kotlin | 13.09 | 12.51 | -0.58 | #456 (#457) |
| pandas | python | 14.41 | 13.88 | -0.53 | #455 (transitive) |
| requests | python | 1.88 | 1.72 | -0.15 | #455 (transitive) |
| actix-examples | rust | 19.40 | 19.39 | -0.01 | noise |

## Regressions

**None.** All 32 repos either improved or stayed flat. Untouched
languages (Go, Java, JavaScript/TypeScript, PHP, Swift, C#, Elixir,
Ruby) all registered 0.00 pp delta — confirms the residual batch is
non-regressive elsewhere.

## Methodology / reproducibility

- Worktree: `/Users/jorgecajas/Documents/Projects/archigraph-worktrees/baseline-5`
- Branch: `investigate/baseline-5` off `origin/main` @ `b17644a` (post-#458
  merge, i.e. both #455 Python and #456 Kotlin residual fixes landed).
- Binary: `go build -o /tmp/archigraph-baseline-5 ./cmd/archigraph` from
  worktree HEAD.
- Indexer: `archigraph index -json-stats <repo>` per repo, 600 s `gtimeout`
  cap, 6-way parallel via `xargs -P6`.
- Same 32 repos as v3 and the prior baselines. Three framework-source
  repos (`django`, `nextjs`, `aspnetcore-mvc`) remain excluded under #96
  policy.
- All 32 repos succeeded; no timeouts, no failures.
- Per-repo JSON stats: `/tmp/baseline-5-stats/<repo>.json`.
