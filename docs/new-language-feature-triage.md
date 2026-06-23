# New-language-feature triage (C1, epic #5359 — milestone 0.1.4)

_Process maintained as of 2026-06-23. Companion to
[`docs/language-release-calendar.md`](./language-release-calendar.md) (A3),
[`docs/grammar-freshness-audit.md`](./grammar-freshness-audit.md) (B3),
[`grammars.lock`](../grammars.lock), and the
[extractor recipe](./extractor-recipe.md) (C2)._

## Why this exists

A grammar bump makes new syntax **parse**; grafel must also **model** it. Those
are two different kinds of work (epic #5359 Part C):

1. **Syntax gap** — the bundled grammar doesn't recognise the new syntax. A
   grammar bump fixes it (mechanical). tree-sitter is error-tolerant, so indexing
   never *breaks* — it silently emits `ERROR` nodes instead.
2. **Modeling gap** — even once the syntax parses, grafel's extractors don't put
   the new construct in the graph. A new DI mechanism, routing syntax, async
   idiom, or data construct needs **new detection logic + a coverage-registry
   update**. This is the hard, per-feature work.

C1 is the **decision procedure**: given a language version N's notable features,
classify each one so the work is scoped before anyone opens an extractor PR. It
turns "Java 25 shipped" into a concrete list of (b)/(c) action items that become
[C2 recipe](./extractor-recipe.md) PRs (or [C3 backfill](https://github.com/cajasmota/grafel/issues/5417)).

## The classification

For each notable feature of version N, assign exactly one bucket:

| Bucket | Meaning | grafel work | Routes to |
|---|---|---|---|
| **(a) parse-only** | The grammar's CST already represents it adequately and grafel's existing extractors either don't need to model it or already cover it. No new node kind to walk, no entity/edge to emit. | **None.** Note it and move on. | — |
| **(b) needs-new-extraction** | A genuinely new construct grafel *should* model — a new node kind in the grammar that maps to an entity/edge grafel doesn't currently emit (a record type, a DI attribute, a routing form, an async/concurrency idiom, a new visibility/module mechanism). | **New extractor logic + new coverage cell.** | [C2 recipe](./extractor-recipe.md) → new PR |
| **(c) changes-existing-extraction** | An existing construct grafel already extracts, but the new version **changed its syntax** (a new node-type name, a new field, an alternate declaration form). An existing extractor's `switch node.Type()` / `ChildByFieldName(...)` must adapt or it silently stops emitting. | **Patch the existing extractor + refresh its coverage cell.** | [C2 recipe](./extractor-recipe.md) → fix PR |

### Deciding between the buckets

- **(a) vs (b):** ask *"would grafel's graph be richer if it modeled this?"* If
  the construct is a new kind of component/operation/endpoint/edge that a graph
  consumer (MCP query, dashboard, impact radius) would want to see — it's (b). If
  it's pure syntactic sugar that produces nodes grafel already walks (or that no
  consumer cares about) — it's (a).
- **(b) vs (c):** ask *"did grafel already emit something for the OLD form of
  this?"* If yes and the syntax merely changed shape — it's (c) (an adaptation,
  usually smaller). If the construct is new to the language — it's (b).
- **Triage is spec-driven, not grammar-driven.** Because the bundled grammar is
  ~22 months stale (see B3 audit §1), it may not even parse N yet. **Do not block
  triage on the grammar bump.** Classify from the language *release notes* /
  spec; record "grammar bump (B1) is a prerequisite" against any (b)/(c) item
  whose node kind the bundled grammar can't yet produce. The grammar bump is the
  prerequisite for *implementing* (b)/(c), not for *triaging* it.

## The per-version impact-report template

When version N lands (the A3 calendar cron, #5413, fires the reminder), copy the
block below into `docs/feature-impact/<lang>-<version>.md` and fill it in. It is
the standing artifact that turns a release into action items.

```markdown
# Feature-impact report — <Language> <Version>

- **Released:** <YYYY-MM-DD>
- **Triaged:** <YYYY-MM-DD> by <who>
- **Grammar status:** <bundled snapshot parses N? — from A4 canary / A2 cron>
  - A4 canary (`parse_error_canary` in `graph-stats.json`): <spike? per-lang rate>
  - A2 cron tracking issue: <has upstream tree-sitter-<lang> shipped N support?>
- **Grammar-bump prerequisite:** <yes/no — does any (b)/(c) item below need B1 first?>

## Feature triage

| # | Feature (from release notes) | Bucket | Rationale | Extractor / registry cell touched |
|---|---|---|---|---|
| 1 | <feature> | a / b / c | <why this bucket> | <internal/extractors/<lang>/… · registry id, or “none”> |
| … | | | | |

## Action items

- [ ] (b) <feature> → C2 recipe PR: add <node_kind> extraction in
      `internal/extractors/<lang>/…`, register Kind <…> if new, update
      `registry.json` cell <id> + coverage docs (same PR), `coverage fmt --check`.
- [ ] (c) <feature> → patch `internal/extractors/<lang>/…` `switch node.Type()`
      for renamed/added node kind <…>; refresh registry cell <id>.
- [ ] (a) items: no work — recorded above for the audit trail.
- [ ] Update `grammars.lock` `last_verified` for <lang> once confirmed current.
- [ ] Backfill candidates (released in the catch-up window) → C3 (#5417).
```

The report's deliverable is the **Action items** list: every (b) and (c) becomes
a tracked follow-up built via the [C2 recipe](./extractor-recipe.md); (a) items
are recorded for the audit trail and need no work.

## Worked example — C# 13 (.NET 9, Nov 2024)

Triaged spec-first from the C# 13 release notes. At triage time the bundled
`tree-sitter-c-sharp` snapshot (2024-08-27) predates full C# 13 support, so
several node kinds below **may emit `ERROR` nodes until the B1 catch-up bump** —
that's flagged as the grammar-bump prerequisite, not a triage blocker. The
relevant grafel extractor is `internal/extractors/csharp/` (entry
`csharp.go`, the `walk()` `switch node.Type()` traversal).

| # | C# 13 feature | Bucket | Rationale | Extractor / registry cell |
|---|---|---|---|---|
| 1 | **`params` collections** (`params ReadOnlySpan<T>`, any collection type) | **a** | Just a wider parameter modifier on `method_declaration`/`constructor_declaration`, which `csharp.go` already walks and emits as `SCOPE.Operation`. No new node kind, no new edge a consumer needs. | none |
| 2 | **`ref struct` interface implementation / `allows ref struct`** anti-constraint | **c** | grafel already emits `struct_declaration → SCOPE.Component` and IMPLEMENTS edges. The new `allows ref struct` clause changes the `type_parameter_constraints` shape; the constraint-walking path may need to tolerate the new node so it doesn't drop the type. Adaptation of existing extraction. | `csharp.go` `buildComponent` / constraint walk; registry `csharp` type-system cell |
| 3 | **`field` keyword (preview)** — semi-auto property backing field | **b** | A property accessor body referencing `field` is a new *member-level* construct. If grafel models property backing storage (field-membership, #4854 area), the `field` keyword introduces a new binding target the field-members pass should recognise. New modeling. | `internal/extractors/csharp/field_members.go`; registry `csharp` field-membership cell |
| 4 | **New lock object** (`System.Threading.Lock`) | **a** | Purely a type swap at the call site (`lock` statement over a `Lock` instance). The `lock_statement` node is unchanged; grafel doesn't model lock objects as graph entities today and a consumer wouldn't query them. | none |
| 5 | **Partial properties & indexers** | **c** | grafel walks `property_declaration` for field-membership; `partial` properties split a declaration across two `property_declaration` nodes with `partial` modifiers. The existing property/field path must de-duplicate / merge the split halves rather than emit two members. Adaptation. | `csharp.go` / `field_members.go`; registry `csharp` field-membership cell |
| 6 | **Implicit index access in object initializers** (`^1` in initializers) | **a** | Sugar inside `initializer_expression`; produces expression nodes grafel doesn't model as entities. No graph-relevant construct. | none |
| 7 | **Overload resolution priority** (`[OverloadResolutionPriority]` attribute) | **a** | A new attribute on `method_declaration`. grafel already emits the method as `SCOPE.Operation`; the attribute carries no graph edge a consumer queries (it's a compiler hint). Could become (b) later if attribute-driven analysis is added. | none |
| 8 | **`Span<T>`/`ReadOnlySpan<T>` extension-method lookups** (collection-expr conversions) | **a** | Changes which extension methods bind, but the CALLS-edge target shape (`csharp.go` `extractCallRelationships`) is unchanged. Already-extracted call edges; no new node kind. | none |

**Verdict for C# 13:** 5×(a) parse-only · 1×(b) `field` keyword field-membership
modeling · 2×(c) `allows ref struct` constraint tolerance + partial-property
merge. The two (c)s and the one (b) become C2-recipe follow-ups; all three carry
the **grammar-bump prerequisite** (the bundled snapshot must parse C# 13 first —
B1 catch-up bump, behind the fidelity/coverage benchmark gate). The five (a)s are
recorded and need no work.

## How this fits the cadence

C1 is the **decision step** between the freshness alarms and the build work:

| Signal | Mechanism | Feeds C1 by telling you… |
|---|---|---|
| **Proactive nudge** | [A3 calendar + cron](./language-release-calendar.md) (#5413) | a known release window is coming — *run the triage now*. The reminder issue links straight to this doc. |
| **"Upstream moved"** | A2 `grammar-freshness.yml` cron (#5411) | the upstream `tree-sitter-<lang>` grammar shipped support for N — the bundled snapshot is the blocker (grammar-bump prerequisite for (b)/(c)). |
| **"Parsing is actually failing"** | A4 parse-error canary (#5414) | the bundled grammar is emitting `ERROR` nodes on real indexed code — a hard syntax gap, version-agnostic. Fill the **Grammar status** row of the report from this. |

The output of C1 — the per-version impact report's **Action items** — is the
input to:

- **[C2 — extractor recipe](./extractor-recipe.md) (#5416):** the repeatable build
  path for each (b)/(c) item (locate node kind → add traversal/emit → register
  Kind → update `registry.json` + coverage docs in the same PR →
  `coverage fmt --check` → fixture/test).
- **C3 — backfill (#5417):** the (b)/(c) features already released during the
  ~22-month catch-up window, modeled (not just parsed) once B1 lands.

All four reconcile against [`grammars.lock`](../grammars.lock), the source of
truth for which grammar version each language rides.
