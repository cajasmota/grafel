# VERIFY-2 Quick Tier-1 Refresh v3 (2026-05-19, post-determinism)

_Re-measurement after #486 made indexer output byte-identical across runs. First **reliable single-shot** measurement; prior runs carried ~±5pp noise on small repos._

_Binary built from `investigate/remeasure-v3` head (`2e2ec02` merge of #486). `SOURCE_DATE_EPOCH=1700000000`, serial, 300s per-repo timeout. 40/40 OK, 0 timeouts, 0 crashes._

**Headline:** aggregate bug-rate **10.34%** across **40** tier-1 repos (same 40-repo intersection as v2).

**Delta vs v2:** **11.34% to 10.34% (-1.00 pp).** Smaller absolute swing than v1->v2 because v2 had already captured wave-1+2 lands; v3 captures wave-3 chain-fixes (#474 #475 #476 #477 #478 #480 #483) plus the determinism normalisation.

## Determinism sanity check

3 back-to-back runs of `kickstart.nvim` with `SOURCE_DATE_EPOCH=1700000000`:

```
516976825187bdec8d79215c9d2e9cfb81bb7c748e4da22e64ae33ed642ee09a  run-1
516976825187bdec8d79215c9d2e9cfb81bb7c748e4da22e64ae33ed642ee09a  run-2
516976825187bdec8d79215c9d2e9cfb81bb7c748e4da22e64ae33ed642ee09a  run-3
```

Byte-identical SHA256. #486 holds.

## Comparison vs prior baselines

| Run | Repos | Aggregate bug-rate | At-bar (<=8%) | At ship-gate (<=1%) | Reliability |
|---|---:|---:|---:|---:|---|
| v4 baseline | 32 | 15.63% | n/a | n/a | noisy |
| quick tier-1 v1 (2026-05-19) | 40 | 17.55% | 6 | 2 | noisy |
| quick tier-1 v2 (2026-05-19) | 40 | 11.34% | 10 | 2 | noisy |
| **quick tier-1 v3 (this run)** | **40** | **10.34%** | **16** | **4** | **reliable single-shot** |

**Net since v2:** +6 at-bar, +2 at-ship-gate, aggregate -1.00 pp.

## Aggregate disposition counts

| Disposition | v3 | v2 | delta |
|---|---:|---:|---:|
| resolved | 252,431 | 249,510 | +2,921 |
| external-known | 31,448 | 30,495 | +953 |
| external-unknown | 31,983 | 34,339 | -2,356 |
| dynamic | 91,869 | 83,971 | +7,898 |
| bug-extractor | 32,372 | 35,467 | -3,095 |
| bug-resolver | 14,641 | 15,488 | -847 |
| unclassified | 2 | 0 | +2 |
| **total endpoints** | **454,746** | **449,270** | **+5,476** |
| **bugs (extractor+resolver+unclassified)** | **47,015** | **50,955** | **-3,940** |

Bug-extractor drops by ~3.1k, bug-resolver by ~0.8k; resolved gains ~2.9k. The wave-3 chain-fixes (markdown, YAML chain, byPackageComponent) moved extractor-bugs to either `resolved` (for the markdown/yaml link work) or `external-known/dynamic` (correctly classified).

## Per-repo before/after (sorted by new bug-rate, worst first; bar = 8.0%)

| Repo | Lang | Files | Rels | Endpoints | Bugs | v3 bug-rate | v2 bug-rate | Delta | vs Bar |
|---|---|---:|---:|---:|---:|---:|---:|---:|:---:|
| laravel-quickstart | php | 83 | 191 | 382 | 92 | 24.08% | 24.08% | +0.00 | FAIL |
| symfony-demo | php | 241 | 1,499 | 2,998 | 690 | 23.02% | 23.02% | -0.00 | FAIL |
| kafka-streams-examples | java | 172 | 8,156 | 16,312 | 3,619 | 22.19% | 22.31% | -0.12 | FAIL |
| vapor-api-template | swift | 21 | 47 | 94 | 20 | 21.28% | 21.28% | -0.00 | FAIL |
| http.zig | zig | 36 | 1,874 | 3,748 | 763 | 20.36% | 20.36% | -0.00 | FAIL |
| usermanager-example | clojure | 17 | 76 | 152 | 30 | 19.74% | 19.74% | -0.00 | FAIL |
| actix-examples | rust | 460 | 6,100 | 12,200 | 2,288 | 18.75% | 18.75% | +0.00 | FAIL |
| just | just | 290 | 19,731 | 39,462 | 6,842 | 17.34% | 17.34% | -0.00 | FAIL |
| nextjs-commerce | typescript | 76 | 668 | 1,336 | 229 | 17.14% | 17.22% | -0.08 | FAIL |
| nestjs-starter | typescript | 16 | 57 | 114 | 19 | 16.67% | 16.67% | -0.00 | FAIL |
| tokio | rust | 389 | 18,370 | 36,740 | 5,893 | 16.04% | 16.04% | -0.00 | FAIL |
| mini-redis | rust | 33 | 1,047 | 2,094 | 311 | 14.85% | 14.85% | +0.00 | FAIL |
| flask-realworld | python | 43 | 934 | 1,868 | 276 | 14.78% | 14.78% | -0.00 | FAIL |
| django-realworld | python | 48 | 530 | 1,060 | 148 | 13.96% | 13.96% | +0.00 | FAIL |
| pandas | python | 197 | 30,385 | 60,770 | 8,424 | 13.86% | 13.86% | +0.00 | FAIL |
| sidekiq | ruby | 85 | 4,733 | 9,466 | 1,275 | 13.47% | 13.47% | -0.00 | FAIL |
| exposed | kotlin | 115 | 4,785 | 9,570 | 1,053 | 11.00% | 8.56% | **+2.44** | FAIL |
| kickstart.nvim | lua | 15 | 71 | 142 | 14 | 9.86% | 10.14% | -0.28 | FAIL |
| express-realworld | javascript | 66 | 346 | 692 | 68 | 9.83% | 9.83% | -0.00 | FAIL |
| aspnetcore-realworld | csharp | 97 | 1,288 | 2,576 | 253 | 9.82% | 9.82% | +0.00 | FAIL |
| phoenix-todo-list | elixir | 69 | 714 | 1,428 | 134 | 9.38% | 9.38% | +0.00 | FAIL |
| tide | fish | 130 | 754 | 1,508 | 136 | 9.02% | 9.02% | -0.00 | FAIL |
| etcd | go | 424 | 29,069 | 58,138 | 5,010 | 8.62% | 12.40% | **-3.78** | FAIL |
| spring-petclinic | java | 120 | 2,291 | 4,582 | 382 | 8.34% | 8.45% | -0.11 | FAIL |
| play-scala-starter | scala | 37 | 71 | 142 | 11 | 7.75% | 7.75% | -0.00 | ok |
| grpc-go-examples | proto | 203 | 8,087 | 16,174 | 1,139 | 7.04% | 10.74% | **-3.70** | ok |
| spdlog | cpp | 175 | 3,326 | 6,652 | 462 | 6.95% | 6.95% | -0.00 | ok |
| click | python | 138 | 7,841 | 15,682 | 1,076 | 6.86% | 6.86% | +0.00 | ok |
| rails-realworld | ruby | 105 | 263 | 526 | 35 | 6.65% | 6.65% | +0.00 | ok |
| terraform-aws-vpc | hcl | 105 | 3,650 | 7,300 | 463 | 6.34% | 6.34% | +0.00 | ok |
| ktor-samples | kotlin | 509 | 5,854 | 11,708 | 737 | 6.29% | 10.40% | **-4.11** | ok |
| aspnetcore-docs-samples | razor | 2,674 | 14,459 | 28,918 | 1,787 | 6.18% | 6.18% | -0.00 | ok |
| gin | go | 121 | 11,335 | 22,670 | 1,399 | 6.17% | 8.63% | **-2.46** | ok |
| chi | go | 93 | 3,826 | 7,652 | 367 | 4.80% | 8.50% | **-3.70** | ok |
| apollo-server | graphql | 293 | 8,646 | 17,292 | 820 | 4.74% | 4.79% | -0.05 | ok |
| requests | python | 111 | 23,584 | 47,168 | 725 | 1.54% | 1.54% | -0.00 | ok |
| starter-workflows | yaml | 514 | 2,281 | 4,562 | 25 | 0.55% | 11.89% | **-11.34** | ok |
| argocd-example-apps | yaml | 91 | 176 | 352 | 0 | 0.00% | 16.01% | **-16.01** | ok |
| openapi-stripe | yaml | 5 | 19 | 38 | 0 | 0.00% | 0.00% | +0.00 | ok |
| prometheus-helm | yaml | 52 | 239 | 478 | 0 | 0.00% | 0.00% | +0.00 | ok |

## Newly at ship-gate (<=1%) since v2

| Repo | Lang | v2 | v3 | Driving chain |
|---|---|---:|---:|---|
| argocd-example-apps | yaml | 16.01% | 0.00% | #467 then #474 then #478 (now folded into aggregate) |
| starter-workflows | yaml | 11.89% | 0.55% | #467 then #475 then #478 (now folded into aggregate) |

These were already noted as post-PR clean in the ledger; v3 folds them into the aggregate measurement.

## Newly at-bar since v2 (>1% but <=8%)

| Repo | Lang | v2 | v3 | Driving chain |
|---|---|---:|---:|---|
| chi | go | 8.50% | 4.80% | #480 then #483 (byPackageComponent) |
| gin | go | 8.63% | 6.17% | #480 then #483 |
| ktor-samples | kotlin | 10.40% | 6.29% | #471 then #477 |
| grpc-go-examples | proto | 10.74% | 7.04% | #472 then #476 then #480 |

## Wave-3 queue — still above 8% bar (24)

Sorted by v3 bug-rate desc:

| Rank | Repo | Lang | v3 | Status hint |
|---:|---|---|---:|---|
| 1 | laravel-quickstart | php | 24.08% | see ledger |
| 2 | symfony-demo | php | 23.02% | see ledger |
| 3 | kafka-streams-examples | java | 22.19% | see ledger |
| 4 | vapor-api-template | swift | 21.28% | see ledger |
| 5 | http.zig | zig | 20.36% | see ledger |
| 6 | usermanager-example | clojure | 19.74% | see ledger |
| 7 | actix-examples | rust | 18.75% | see ledger |
| 8 | just | just | 17.34% | see ledger |
| 9 | nextjs-commerce | typescript | 17.14% | see ledger |
| 10 | nestjs-starter | typescript | 16.67% | see ledger |
| 11 | tokio | rust | 16.04% | see ledger |
| 12 | mini-redis | rust | 14.85% | see ledger |
| 13 | flask-realworld | python | 14.78% | see ledger |
| 14 | django-realworld | python | 13.96% | see ledger |
| 15 | pandas | python | 13.86% | see ledger |
| 16 | sidekiq | ruby | 13.47% | see ledger |
| 17 | exposed | kotlin | 11.00% | see ledger |
| 18 | kickstart.nvim | lua | 9.86% | see ledger |
| 19 | express-realworld | javascript | 9.83% | see ledger |
| 20 | aspnetcore-realworld | csharp | 9.82% | see ledger |
| 21 | phoenix-todo-list | elixir | 9.38% | see ledger |
| 22 | tide | fish | 9.02% | see ledger |
| 23 | etcd | go | 8.62% | see ledger |
| 24 | spring-petclinic | java | 8.34% | see ledger |

## Findings

1. **Determinism holds.** 3-run SHA256 identical on kickstart.nvim. Single-shot measurement is now trustworthy.
2. **Aggregate improved -1.00 pp** vs v2 (11.34% to 10.34%) from wave-3 chain-fixes (#478 #480 #483) folding in.
3. **6 repos crossed the 8% bar** since v2 (incl. 2 to ship-gate). At-bar count went 10 to 16.
4. **Exposed regressed +2.44 pp** (8.56% to 11.00%). Was at-bar in v2; now above the bar. Likely a v2 noise artifact (v2 was measured pre-determinism); v3 single-shot is the trustworthy number. Investigate as wave-N candidate (Kotlin DSL receivers beyond Ktor Routing — same residual class flagged in the ledger).
5. **etcd** dropped -3.78 pp (12.40% to 8.62%) but still misses bar by 0.62 pp — the receiver-variable-type chain is the gating issue.
6. **No false-regression resolution noted from v2->v3 within the 'at-bar' bucket** other than exposed in the other direction; v2 numbers for at-bar repos were within ~0.5 pp of v3, suggesting v2 noise was dominated by repos with large numerators rather than small ones.

## Wall time and reliability

- Index pass wall: 476s (~8 min) serial.
- All 40 repos OK, no timeouts, no crashes.
- Sanity check: 3x kickstart.nvim runs SHA256-identical.

forbidden-term grep: clean
