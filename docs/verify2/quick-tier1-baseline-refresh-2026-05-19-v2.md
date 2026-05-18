# VERIFY-2 Quick Tier-1 Refresh v2 (2026-05-19)

_Re-measurement after wave-1 + wave-2 PRs (#466 HCL, #467 YAML, #468 cpp, #469 scala, #470 GraphQL, #471 Kotlin, #473 Razor, #472 Proto) all merged to `origin/main`._

_Binary built from commit `cbf2efc` (post-#473 merge). Later fixes #474-#478 are NOT included in this measurement; they will be captured in the next refresh._

**Headline:** aggregate bug-rate **11.34%** across **40** successfully-indexed tier-1 repos.

**Delta vs prior quick-tier1 baseline:** **17.55% to 11.34% (-6.21 pp).**

40/40 repos completed, 0 timeouts, 0 crashes. Serial run, per-repo timeout 300s. Same 40-repo intersection as the prior baseline (tier-1 entries from `scripts/verify2/run.sh` that are already cloned in `~/Documents/Projects/archigraph-corpora/`).

## Caveat

- Same **quick partial** scope as the prior baseline: **40 of 116** tier-1 repos.
- NOT the full ship-gate. Full ship-gate requires the remaining 76 tier-1 repos plus the tier-2/3 set.
- Coverage gaps list from the prior baseline is unchanged.

## Comparison vs prior baselines

| Run | Repos measured | Aggregate bug-rate | At/below 8% bar |
|---|---:|---:|---:|
| v4 baseline | 32 | 15.63% | n/a |
| quick tier-1 (prior, 2026-05-19) | 40 | 17.55% | 6 / 40 |
| quick tier-1 refresh v2 (this run) | 40 | **11.34%** | **10 / 40** |

**Delta vs prior quick-tier1:** **-6.21 pp** aggregate; **+4** repos newly at/below the 8% bar.

## Aggregate disposition counts

| Disposition | Count | vs prior |
|---|---:|---:|
| resolved | 249,510 | +4,798 |
| external-known | 30,495 | +8,000 |
| external-unknown | 34,339 | +9,296 |
| dynamic | 83,971 | +6,556 |
| bug-extractor | 35,467 | -26,267 |
| bug-resolver | 15,488 | -1,467 |
| unclassified | 0 | 0 |
| **total endpoints** | **449,270** | +916 |
| **bugs (extractor+resolver+unclassified)** | **50,955** | **-27,734** |

The bug drop is concentrated in `bug-extractor` (-26k of -28k total). Bugs that disappeared from the extractor pool migrated chiefly to `external-known` and `external-unknown` (correctly classified as out-of-repo references) and `dynamic` (correctly classified as unresolvable at static-analysis time).

## Per-repo before/after (sorted by new bug-rate, worst first; bar = 8.0%)

| Repo | Lang | Files | Rels | Endpoints | Bugs | Bug-rate (new) | Bug-rate (prior) | Delta | vs Bar |
|---|---|---:|---:|---:|---:|---:|---:|---:|:---:|
| laravel-quickstart | php | 83 | 191 | 382 | 92 | 24.08% | 24.35% | -0.27 | FAIL |
| symfony-demo | php | 241 | 1,499 | 2,998 | 690 | 23.02% | 23.38% | -0.36 | FAIL |
| kafka-streams-examples | java | 172 | 8,156 | 16,312 | 3,639 | 22.31% | 22.89% | -0.58 | FAIL |
| vapor-api-template | swift | 21 | 47 | 94 | 20 | 21.28% | 21.28% | -0.00 | FAIL |
| http.zig | zig | 36 | 1,874 | 3,748 | 763 | 20.36% | 20.60% | -0.24 | FAIL |
| usermanager-example | clojure | 17 | 76 | 152 | 30 | 19.74% | 19.74% | -0.00 | FAIL |
| actix-examples | rust | 460 | 6,100 | 12,200 | 2,288 | 18.75% | 19.39% | -0.64 | FAIL |
| just | just | 290 | 19,731 | 39,462 | 6,842 | 17.34% | 17.42% | -0.08 | FAIL |
| nextjs-commerce | typescript | 76 | 668 | 1,336 | 230 | 17.22% | 23.35% | -6.13 | FAIL |
| nestjs-starter | typescript | 16 | 57 | 114 | 19 | 16.67% | 16.67% | -0.00 | FAIL |
| tokio | rust | 389 | 18,370 | 36,740 | 5,893 | 16.04% | 16.18% | -0.14 | FAIL |
| argocd-example-apps | yaml | 91 | 178 | 356 | 57 | 16.01% | 47.58% | **-31.57** | FAIL |
| mini-redis | rust | 33 | 1,047 | 2,094 | 311 | 14.85% | 16.67% | -1.82 | FAIL |
| flask-realworld | python | 43 | 934 | 1,868 | 276 | 14.78% | 15.10% | -0.32 | FAIL |
| django-realworld | python | 48 | 530 | 1,060 | 148 | 13.96% | 13.96% | +0.00 | FAIL |
| pandas | python | 197 | 30,385 | 60,770 | 8,424 | 13.86% | 13.87% | -0.01 | FAIL |
| sidekiq | ruby | 85 | 4,733 | 9,466 | 1,275 | 13.47% | 13.85% | -0.38 | FAIL |
| etcd | go | 424 | 29,020 | 58,040 | 7,198 | 12.40% | 19.66% | **-7.26** | FAIL |
| starter-workflows | yaml | 514 | 2,287 | 4,574 | 544 | 11.89% | 48.31% | **-36.42** | FAIL |
| grpc-go-examples | proto | 203 | 7,206 | 14,412 | 1,548 | 10.74% | 24.26% | **-13.52** | FAIL |
| ktor-samples | kotlin | 509 | 4,615 | 9,230 | 960 | 10.40% | 29.35% | **-18.95** | FAIL |
| kickstart.nvim | lua | 15 | 69 | 138 | 14 | 10.14% | 3.45% | **+6.69** | FAIL |
| express-realworld | javascript | 66 | 346 | 692 | 68 | 9.83% | 19.65% | -9.82 | FAIL |
| aspnetcore-realworld | csharp | 97 | 1,288 | 2,576 | 253 | 9.82% | 17.93% | -8.11 | FAIL |
| phoenix-todo-list | elixir | 69 | 714 | 1,428 | 134 | 9.38% | 12.75% | -3.37 | FAIL |
| tide | fish | 130 | 754 | 1,508 | 136 | 9.02% | 9.15% | -0.13 | FAIL |
| gin | go | 121 | 11,327 | 22,654 | 1,955 | 8.63% | 12.52% | -3.89 | FAIL |
| exposed | kotlin | 115 | 4,274 | 8,548 | 732 | 8.56% | 12.53% | -3.97 | FAIL |
| chi | go | 93 | 3,771 | 7,542 | 641 | 8.50% | 12.74% | -4.24 | FAIL |
| spring-petclinic | java | 120 | 2,291 | 4,582 | 387 | 8.45% | 8.73% | -0.28 | FAIL |
| play-scala-starter | scala | 37 | 71 | 142 | 11 | 7.75% | 31.69% | **-23.94** | ok |
| spdlog | cpp | 175 | 3,326 | 6,652 | 462 | 6.95% | 32.47% | **-25.52** | ok |
| click | python | 138 | 7,841 | 15,682 | 1,076 | 6.86% | 7.11% | -0.25 | ok |
| rails-realworld | ruby | 105 | 263 | 526 | 35 | 6.65% | 6.65% | +0.00 | ok |
| terraform-aws-vpc | hcl | 105 | 3,650 | 7,300 | 463 | 6.34% | 73.95% | **-67.61** | ok |
| aspnetcore-docs-samples | razor | 2,674 | 14,459 | 28,918 | 1,787 | 6.18% | 25.89% | **-19.71** | ok |
| apollo-server | graphql | 293 | 8,645 | 17,290 | 829 | 4.79% | 26.21% | **-21.42** | ok |
| requests | python | 111 | 23,584 | 47,168 | 725 | 1.54% | 1.69% | -0.15 | ok |
| openapi-stripe | yaml | 5 | 19 | 38 | 0 | 0.00% | 0.00% | +0.00 | ok |
| prometheus-helm | yaml | 52 | 239 | 478 | 0 | 0.00% | 1.05% | -1.05 | ok |

## Repos newly at/below the 8% bar (4)

| Repo | Lang | Before | After | Driving PR |
|---|---|---:|---:|---|
| terraform-aws-vpc | hcl | 73.95% | 6.34% | #466 (HCL) |
| spdlog | cpp | 32.47% | 6.95% | #468 (cpp) |
| play-scala-starter | scala | 31.69% | 7.75% | #469 (scala) |
| aspnetcore-docs-samples | razor | 25.89% | 6.18% | #473 (Razor/csharp) |
| apollo-server | graphql | 26.21% | 4.79% | #470 (GraphQL) |

(5 repos crossed the bar; total at-bar count went from 6 to 10 because one repo previously at-bar — kickstart.nvim — regressed above it.)

## Repos at/below bar after this run (10 of 40)

prometheus-helm (0.00%), openapi-stripe (0.00%), requests (1.54%), apollo-server (4.79%), aspnetcore-docs-samples (6.18%), terraform-aws-vpc (6.34%), rails-realworld (6.65%), click (6.86%), spdlog (6.95%), play-scala-starter (7.75%).

## Top remaining offenders (still above the 8% bar)

| Repo | Lang | Bug-rate | Notes |
|---|---|---:|---|
| laravel-quickstart | php | 24.08% | PHP extractor untouched in wave-1+2 |
| symfony-demo | php | 23.02% | PHP |
| kafka-streams-examples | java | 22.31% | Java |
| vapor-api-template | swift | 21.28% | Swift untouched |
| http.zig | zig | 20.36% | Zig untouched |
| usermanager-example | clojure | 19.74% | Clojure untouched |
| actix-examples | rust | 18.75% | Rust |
| just | just | 17.34% | just-lang untouched |
| nextjs-commerce | typescript | 17.22% | TS still high despite -6.13 pp |
| nestjs-starter | typescript | 16.67% | TS |

## Notable regression

- **kickstart.nvim (lua):** 3.45% to 10.14% (+6.69 pp). Endpoints went 116 to 138 (more relationships extracted), but absolute bugs went 4 to 14. Lua was not targeted in wave-1+2 — likely fallout from a transitive change (cross-lang or hierarchy pass touched something). Worth a follow-up issue.

## Wave-1+2 PR effectiveness summary

| PR | Lang | Repo | Before | After | Delta |
|---|---|---|---:|---:|---:|
| #466 | hcl | terraform-aws-vpc | 73.95% | 6.34% | -67.61 |
| #467 | yaml | starter-workflows | 48.31% | 11.89% | -36.42 |
| #467 | yaml | argocd-example-apps | 47.58% | 16.01% | -31.57 |
| #468 | cpp | spdlog | 32.47% | 6.95% | -25.52 |
| #469 | scala | play-scala-starter | 31.69% | 7.75% | -23.94 |
| #470 | graphql | apollo-server | 26.21% | 4.79% | -21.42 |
| #471 | kotlin | ktor-samples | 29.35% | 10.40% | -18.95 |
| #471 | kotlin | exposed | 12.53% | 8.56% | -3.97 |
| #472 | proto | grpc-go-examples | 24.26% | 10.74% | -13.52 |
| #473 | razor/csharp | aspnetcore-docs-samples | 25.89% | 6.18% | -19.71 |
| #473 | razor/csharp | aspnetcore-realworld | 17.93% | 9.82% | -8.11 |

All 8 wave-1+2 PRs delivered measurable per-repo improvements on the targeted languages. starter-workflows, argocd-example-apps, ktor-samples, grpc-go-examples and aspnetcore-realworld remain above the 8% bar despite large drops — wave-3 targeting for these languages still warranted.

## Reproduction

```
git worktree add ../archigraph-worktrees/remeasure -b investigate-or-fix/remeasure origin/main
cd archigraph-worktrees/remeasure
go build -o /tmp/ag-rem ./cmd/archigraph
# intersection of run.sh tier-1 and ~/Documents/Projects/archigraph-corpora/
awk -F'|' '{ if (system("test -d ~/Documents/Projects/archigraph-corpora/" $1) == 0) print $1 }' \
  <(grep -E '^\s*"[^|]+\|' scripts/verify2/run.sh \
     | sed -E 's/^[[:space:]]*"([^|]+)\|.*/\1/') > /tmp/qm2_names.txt
while read -r name; do
  gtimeout 300 /tmp/ag-rem index -json-stats \
    "$HOME/Documents/Projects/archigraph-corpora/$name" > /tmp/qm2/$name.json
done < /tmp/qm2_names.txt
```

forbidden-term grep: clean
