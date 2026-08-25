# Language-release calendar (A3, epic #5359 — milestone 0.1.4)

_Calendar maintained as of 2026-08-26. Companion to
[`grammars.lock`](../grammars.lock) and
[`docs/grammar-freshness-audit.md`](./grammar-freshness-audit.md)._

## Why this exists

grafel rides tree-sitter grammars for ~28 languages. When a language ships a new
version, **two** gaps can open (see epic #5359):

1. **Syntax gap** — the bundled grammar doesn't recognise the new syntax. A
   grammar bump fixes it. tree-sitter is error-tolerant, so indexing never
   *breaks*; it silently emits `ERROR` nodes instead.
2. **Modeling gap** — even once the syntax *parses*, grafel's **extractors don't
   model the new construct** in the graph (a new DI mechanism, routing syntax,
   async idiom, data construct…). This needs new detection logic + a
   coverage-registry update. This is the hard, per-feature work.

The automated alarms ([A2 cron](#how-this-fits-the-automated-alarms),
[A4 canary](#how-this-fits-the-automated-alarms)) tell us *after the fact* that a
grammar has moved or that parse errors have spiked. This **calendar is the
proactive trigger**: it fires *ahead* of a known release window so we verify
grammar + extractors against version N around the day it lands, rather than
waiting for an alarm to catch up.

> **Read [What A2 and A4 cannot detect today](#what-a2-and-a4-cannot-detect-today)
> before relying on either alarm as release coverage.** Neither one can currently
> answer *"did the grammar for the language that just shipped fall behind?"* — for
> structural reasons, not because of a bug. The per-release checklist below is
> still the right procedure; what it cannot do is delegate step 1 to automation.

## How to use this on a release

When a language's version N lands (or is about to), run the
[per-release checklist](#per-release-checklist) below and feed the result into
the **C1 new-feature triage process (#5415)**, which classifies each new feature
as parse-only / needs-new-extraction / changes-existing-extraction and produces a
per-version "feature impact" report. The reminder issue the
[cron](#the-cron) opens links straight back here.

## Release cadence table

Dates are typical windows, not guarantees — always confirm against the upstream
release notes. "Grammar repo" is the upstream tree-sitter grammar tracked in
`grammars.lock`; the extractors live under `internal/` per language.

| Language | Cadence | Typical window(s) | Grammar repo (`tree-sitter/…`) | Notes / recent high-value features |
|---|---|---|---|---|
| **Java** | annual + LTS | **March** & **September** | `tree-sitter-java` | Sep is the .0; new LTS every ~2yr. Watch: sealed types, record patterns in `switch`, virtual threads, unnamed patterns. |
| **C# / .NET** | annual | **November** | `tree-sitter-c-sharp` | Ships with the .NET GA. Watch: primary constructors, collection expressions, keyed DI, ref/readonly evolutions. |
| **Python** | annual | **October** | `tree-sitter-python` | PEP-driven. Watch: PEP 695 `type` params (3.12), per-version `match`/typing additions. |
| **Go** | biannual | **February** & **August** | `tree-sitter-go` | Two minor releases/yr. Watch: generics evolutions, range-over-func, loop-var semantics. |
| **TypeScript** | ~quarterly | **roughly Q1/Q2/Q3/Q4** | `tree-sitter-typescript` | No fixed calendar; ~4 releases/yr. Watch: const type params, `using`/explicit resource mgmt, decorator changes. |
| **Rust** | ~6-week train | **every ~6 weeks** | `tree-sitter-rust` | Fast train; most releases are parse-only, but edition bumps (e.g. 2024) are big. Treat editions as the real trigger. |
| **Kotlin** | ~biannual | **spring & autumn** (irregular) | _(heuristic/grammar varies)_ | Cadence loose; verify on each minor. Watch: context receivers, data objects. |
| **Swift** | annual | **~September** (WWDC-aligned GA) | `tree-sitter-swift` | Macro system, typed throws, ownership additions. |
| **Ruby** | annual | **December (25th)** | `tree-sitter-ruby` | Holiday release. Watch: pattern matching, `it` block param, namespace changes. |
| **PHP** | annual | **November** | `tree-sitter-php` | Watch: enums, readonly/asymmetric visibility, property hooks. |
| **C / C++** | multi-year std | **irregular** (C23, C++23/26) | `tree-sitter-c`, `tree-sitter-cpp` | Standards-driven; compilers adopt incrementally. Verify when a new `-std` becomes common. |
| **Scala** | irregular | **irregular** | `tree-sitter-scala` | Scala 3 line; verify on minors. |
| **Elixir / others** | irregular | **irregular** | per `grammars.lock` | Treat as on-demand; the A2 cron is the catch-all for these. |

> Languages not listed with a fixed window are **irregular** — there is no
> predictable date to pre-schedule. The calendar cron focuses its reminders on the
> predictable-cadence languages above, which leaves the irregular ones to the
> **A2 monthly cron (#5411)** and the **A4 parse-error canary (#5414)** — but note
> that neither currently provides a per-release signal
> ([why](#what-a2-and-a4-cannot-detect-today)). For these languages the practical
> safety net is running the checklist on demand when a release is noticed.

## Per-release checklist

For each language version N that lands, verify (and record the outcome in the
C1 impact report, #5415):

1. **Does the new syntax parse?** — **this step is manual today.** Neither alarm
   will raise it for you; see
   [What A2 and A4 cannot detect today](#what-a2-and-a4-cannot-detect-today).
   - Index a small sample using N-only syntax and **read the per-language rate
     yourself** from `parse_error_canary.languages[]` in `graph-stats.json`
     (`internal/graph/graph.go:586-588`). Compare it against the same language on
     a sample of pre-N code. A materially higher rate on the N-only sample ⇒ the
     bundled grammar can't parse N ⇒ a **grammar bump is needed** (syntax gap).
     See `docs/grammar-freshness-audit.md` §4b.
     Do **not** wait for the canary's own `spiked` verdict to tell you this: it
     compares against an empty baseline and nothing outside tests reads it.
   - Cross-check the **A2 cron** tracking issue for the **upstream** side: it
     lists which upstream grammar repos have moved ahead of the bundled snapshot,
     and by how much (`tools/grammar-freshness/check.go:73`, rendered at `:137`
     and `:169`). It reports the same large, growing set of grammars as behind
     every month (23 of 27 by the dates recorded in `grammars.lock` — the four
     exceptions are grammars whose own upstream has not committed since 2021-2022),
     so treat it as a lookup table for your language's magnitude, not as an alarm
     that fired.
     If upstream has shipped support for N, the bundled smacker snapshot is the
     blocker → schedule the B1-style catch-up bump (behind the benchmark gate).

2. **Do the extractors model the new constructs?** (the modeling gap)
   - List N's notable new features (data constructs, DI/IoC, routing/endpoints,
     async/reactive, module/visibility — see epic #5359 Part C).
   - For each, run **C1 triage (#5415)** to classify:
     **(a) parse-only** — grammar handles it, no extractor change;
     **(b) needs-new-extraction** — a new construct grafel should model;
     **(c) changes-existing-extraction** — an existing extractor must adapt.
   - For (b)/(c), open a follow-up using the **C2 extractor recipe (#5416)** and
     remember the coverage-registry standing rule: every new/changed capability
     updates `registry.json` + coverage docs in the **same PR**
     (`coverage fmt --check` gate).

3. **Record the verdict.** Update `grammars.lock` `last_verified` for the
   language if you confirmed it current, and file/refresh the C1 per-version
   impact report. Backfill candidates feed **C3 (#5417)**.

## How this fits the automated alarms

This calendar is the **proactive** leg of the four-part freshness story (epic
#5359 Part A). It does not replace the automated alarms — it complements them:

| Mechanism | Trigger | Tells you |
|---|---|---|
| **A1 — Renovate** (`renovate.json`) | upstream **dependency** moves | a newer grammar binding / per-language grammar module exists (blind today: the smacker binding is unmaintained, so grammar freshness comes from A2). |
| **A2 — `grammar-freshness.yml` cron** (#5411) | monthly, per-**grammar** | which upstream `tree-sitter-<lang>` repos have moved ahead of the bundled snapshot, and by how much. **Cannot** single out a language that just fell behind — see [below](#what-a2-and-a4-cannot-detect-today). |
| **A3 — this calendar + cron** | ahead of a known **release date** | "verify grammar + extractors handle version N" — the proactive nudge before the syntax/modeling gap opens. |
| **A4 — parse-error canary** (#5414) | every **index run** | per-language `ERROR`-node-rate figures in `graph-stats.json`. Its **spike verdict has no consumer** and its test corpus deliberately excludes new syntax — see [below](#what-a2-and-a4-cannot-detect-today) (#6635). |

The catch-all source of truth tying them together is
[`grammars.lock`](../grammars.lock).

## What A2 and A4 cannot detect today

Earlier revisions of this calendar cited A2 and A4 as the two mechanisms that
would catch a language-release regression. **Neither can do that job right now.**
Both are working as designed; the gap is that the design does not cover this
question. Recorded here so the checklist above is not mistaken for automation.

### A2 (grammar freshness) — accurate, but the baseline never moves

**Can tell you:** for each of the 27 grammars in `grammars.lock`, whether the
*upstream* repo has commits newer than the vendored snapshot, and the size of that
gap
(`tools/grammar-freshness/check.go:73` computes `Behind`, `:98-99` sorts by it,
`:137` and `:169` render it). The monthly cron genuinely fires
(`.github/workflows/grammar-freshness.yml:10-13`) and is advisory by design — it
opens or updates a tracking issue and never fails a build
(`.github/workflows/grammar-freshness.yml:63-72`;
`tools/grammar-freshness/main.go:37-42` exits non-zero only on a hard error).

**Cannot tell you:** *"did the grammar for the language that just shipped fall
behind?"* The comparison baseline is a **single global date shared by all 27
grammars** — `bundled := parseDate(lock.Binding.PinnedDate)`
(`tools/grammar-freshness/check.go:50`), stamped onto every result at `:56` and
compared at `:70-75`.

The reason is structural, not a bug in the checker:

- The vendored bundle carries **no per-grammar version provenance**. From
  `grammars.lock` (`binding.notes`, line 15): *"The smacker bundle vendors grammar
  C sources with NO per-grammar version provenance (only ABI LANGUAGE_VERSION
  numbers in parser.h), so bundled_version below is recorded as the binding
  snapshot date, not an upstream grammar semver."* Every one of the 27 entries
  accordingly carries the identical `"bundled_via": "smacker@2024-08-27"`
  (`grammars.lock:26-52`).
- That snapshot **cannot advance**. The same note records the pinned commit *is*
  upstream HEAD (`ahead_by 0`) and that the binding "appears unmaintained";
  `grammars.lock:12-14` states it as `binding_is_at_upstream_head: true` with
  `binding_upstream_head_date: 2024-08-27`.

So the "stale" set is permanently behind a fixed point, by an ever-growing
margin, and the only grammars that escape it are the ones whose *own* upstream has
also stopped moving (by the dates recorded in `grammars.lock`, 23 of 27 are behind
the pin; `lua`, `proto`, `toml` and `yaml` are not). That is an accurate
measurement of a real condition — it simply carries no *per-release* signal,
because the answer for any actively-maintained grammar is "yes, along with all the
others" regardless of which language shipped.

**Blocked on:** the **B2 decouple** recorded in the lock file as
`decouple_target_b2` (`grammars.lock:17-23`) — migrating to the official
`tree-sitter/go-tree-sitter` binding with per-language grammar modules, which is
what would give each language a provenance of its own to be measured against. See
also [ADR-0023](./adrs/0023-migrate-to-official-tree-sitter-binding-per-language-modules.md)
and [`docs/treesitter-cutover-plan.md`](./treesitter-cutover-plan.md).

### A4 (parse-error canary) — measured, but nothing reads the verdict

**Can tell you:** the per-language `ERROR`-node counts and rates it writes into
`graph-stats.json`, if you go and read them. Those numbers are real and are
produced on every index run.

**Cannot tell you** — two separate reasons, both tracked in **#6635**:

1. **The spike verdict has no consumer.** On a spike the indexer writes a stderr
   `WARN` line (`cmd/grafel/index.go:3466-3475`) and sets one JSON field
   (`cmd/grafel/sidecar_build.go:47` → `internal/graph/graph.go:586-588`). Outside
   tests nothing reads it: the only reader of `ParseErrorSpike` in the tree is
   `cmd/grafel/sidecar_build_test.go:125`, which constructs the sidecar directly
   rather than running an index. No exit code, no CI step, no assertion — so a
   spike during a release window surfaces to nobody.
2. **It has no calibrated zero.** `docs/grammar-canary-baseline.json` is
   `{"version": 1, "by_lang": {}}` and nothing in the tree writes it. The relative
   test is gated on `base.TotalNodes >= minBaselineNodes`
   (`internal/treesitter/canary.go:341`, `minBaselineNodes = 200` at `:172`), which
   an empty baseline can never satisfy, so the only live rule is the **2 %
   absolute** threshold (`defaultAbsThreshold = 0.02`, `canary.go:161`) — a
   property of the indexed repo, not of grafel's grammars.

**And the test-suite arm cannot substitute for it either.** The canary's parse
assertions trip only above `maxDominatedRatio = 0.50`
(`internal/treesitter/parse_canary_test.go:76`, asserted at `:833` and `:894`) —
more than half the tree being `ERROR` nodes, i.e. a total recovery storm. A
grammar that fails on one new statement produces a few percent. And the corpus is
deliberately built to exclude the very thing a release check would need
(`parse_canary_test.go:82-84`):

> "Keep these idiomatic and within the currently-pinned grammars' syntax (no
> bleeding-edge constructs) so the canary stays a regression net, not a feature
> gate."

That design is defensible on its own terms — it is a regression net and a good
one. The error was citing it here for a job its own source declines to do. **#6635
Arm B** tracks the separate, red-capable new-syntax corpus that would do that job.

## The cron

`.github/workflows/language-release-calendar.yml` runs on a **monthly cron**
(plus manual `workflow_dispatch`) — it is *not* wired to push/PR, to stay inside
free-tier CI minutes (CI policy). With minimal permissions (`issues: write`,
`contents: read`) it opens or updates a **single idempotent reminder issue**
(stable label **`grammar-release-watch`**) pointing back at this calendar and
flagging the release windows coming up in the next ~8 weeks. Re-runs edit the
same issue rather than spamming new ones.
