# Language-release calendar (A3, epic #5359 — milestone 0.1.4)

_Calendar maintained as of 2026-06-23. Companion to
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
> predictable date to pre-schedule, so the **A2 monthly cron (#5411)** and the
> **A4 parse-error canary (#5414)** are the safety net for them. The calendar
> cron focuses its reminders on the predictable-cadence languages above.

## Per-release checklist

For each language version N that lands, verify (and record the outcome in the
C1 impact report, #5415):

1. **Does the new syntax parse?**
   - Index a small sample using N-only syntax. Check the **A4 canary**
     (`parse_error_canary` in `graph-stats.json`) for a per-language error-rate
     spike. A spike ⇒ the bundled grammar can't parse N ⇒ a **grammar bump is
     needed** (syntax gap). See `docs/grammar-freshness-audit.md` §4b.
   - Cross-check the **A2 cron** tracking issue: has the upstream grammar repo
     already shipped support for N? If yes, the bundled smacker snapshot is the
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
| **A2 — `grammar-freshness.yml` cron** (#5411) | monthly, per-**grammar** | which upstream `tree-sitter-<lang>` repos have moved ahead of the bundled snapshot. The real grammar alarm. |
| **A3 — this calendar + cron** | ahead of a known **release date** | "verify grammar + extractors handle version N" — the proactive nudge before the syntax/modeling gap opens. |
| **A4 — parse-error canary** (#5414) | every **index run** | per-language `ERROR`-node-rate spike — the direct symptom of unhandled new syntax, version-agnostic. |

The catch-all source of truth tying them together is
[`grammars.lock`](../grammars.lock).

## The cron

`.github/workflows/language-release-calendar.yml` runs on a **monthly cron**
(plus manual `workflow_dispatch`) — it is *not* wired to push/PR, to stay inside
free-tier CI minutes (CI policy). With minimal permissions (`issues: write`,
`contents: read`) it opens or updates a **single idempotent reminder issue**
(stable label **`grammar-release-watch`**) pointing back at this calendar and
flagging the release windows coming up in the next ~8 weeks. Re-runs edit the
same issue rather than spamming new ones.
