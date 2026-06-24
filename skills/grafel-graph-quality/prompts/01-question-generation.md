# Phase 1 - Question generation

You are generating the test set for an grafel MCP quality benchmark. Your output is a JSON file of ~13 questions adapted to the registered group's actual entities. Subsequent phases will answer these questions twice (with MCP, without MCP) and compare results.

The benchmark exists to **discriminate good MCP from grep+read**. Smoke tests and pure entity-lookups do not do this — they are too easy to answer from a single file. Every question must require real investigation.

## Complexity mandate

**Average question complexity MUST be ≥ 3 MCP tool calls or 2+ reasoning hops to answer well.**

Before finalising the question set, score each question by expected tool-call depth:
- **Tier 1 (shallow)** — 1 call: name lookup, single-file inspection. At most 2 of these are allowed per run, and only as anchors alongside harder follow-ups.
- **Tier 2 (medium)** — 2–3 calls: callers + context, cross-file trace one hop. Acceptable; aim for ≤ 4.
- **Tier 3 (deep)** — 4+ calls: cross-repo trace, multi-hop flow, end-to-end feature reconstruction. **Majority of questions must be Tier 3.**

If the set average is below Tier 2.5, regenerate until it meets the threshold.

## Repo-mix mandate

**Roughly 50% of questions must be cross-repo** (mobile/frontend → backend handler → DB, or client → server contract), **and 50% single-repo deep-dives** (subsystem internals, call chains, patterns). Do not skew one way even for single-repo groups — for a single-repo group, substitute "across subsystems" (e.g., API layer → service layer → persistence layer) for the cross-repo questions.

## Positive + negative evidence mandate

At least **3 questions must require both positive and negative evidence** — i.e., they ask "is X connected to Y? if not, what IS X connected to?" or "does `<entity>` follow the `<pattern>` convention? show evidence for or against." This flushes out fabrication: a model that hallucinates a connection cannot provide the negative evidence.

A question qualifies if:
- It asks whether a relationship exists and requires naming the actual relationship found when the expected one is absent.
- It asks whether a pattern or convention applies and requires showing counter-evidence when it does not.
- It asks for the owner/caller of an entity where the intuitive owner is wrong.

Mark these with `"requires_negative_evidence": true` in the schema.

## End-to-end feature reconstruction mandate

Include **1–2 "reverse-engineer a feature end-to-end" questions** per run. These are modelled on deep field investigations: pick a visible UI surface or business outcome (a report, a notification, a sync, a payment flow) and ask the agent to trace its complete implementation — from entry point through service logic, data access, and any cross-repo calls — and explain how it works. A correct answer requires 6–10+ tool calls and must cite concrete entities at every layer.

## Question categories and weights

Generate ~13 questions total. The category weights below are **targets**, not hard caps — adjust within ±1 based on what the group actually contains. **Skip a category only if the group genuinely lacks the required entities; record the reason in `skipped_categories`.**

| # | Category | Target count | Tier floor |
|---|----------|-------------|------------|
| 1 | **cross-stack-tracing** | 2–3 | Tier 3 |
| 2 | **specific-trace** | 2 | Tier 3 |
| 3 | **subsystem-deep-dive** | 2 | Tier 2 |
| 4 | **http-cross-repo** | 1–2 | Tier 3 |
| 5 | **pattern-discovery** | 1–2 | Tier 2 |
| 6 | **end-to-end-feature** | 1–2 | Tier 3 |
| 7 | **data-access** | 1 | Tier 2 |
| 8 | **architecture-overview** | 1 | Tier 2 |
| 9 | **entity-lookup** | 1–2 | Tier 1 (anchor only — must be paired with a Tier 3 follow-up in the same question or as the next question in sequence) |

Entity-lookup questions that stand alone without a follow-up are **not permitted**.

## Setup

1. Confirm the run directory: `~/.grafel/quality-check/<YYYY-MM-DD-HHMMSS>/` (the orchestrator passes the exact path). Create it if missing.
2. Call `grafel_orient (view=me)` to resolve the group, repos, and entity stats. If this fails, **stop** and report "no daemon running" - do not start one.
3. Capture the group's entity-kind distribution and top entities by degree using `grafel_orient (view=overview)` and `grafel_orient (view=clusters)`.

## Category definitions

1. **entity-lookup** — "what is `<EntityName>`?" — pick a high-degree entity. **Must** be immediately followed by a Tier 3 question that builds on the answer (e.g., "now trace how `<EntityName>` handles `<operation>`"). Do not emit standalone entity-lookup questions.

2. **reference-finding** — subsumed into **specific-trace** and **cross-stack-tracing**. Do not emit a bare "what calls X?" question unless it is the first hop of a multi-hop trace question.

3. **cross-stack-tracing** — "how does `<frontend-feature>` reach the backend, all the way to the data layer?" — requires ≥2 repos. Trace the full path from UI event/API call through backend handler to storage. Include the layer transitions explicitly in `expected_signals`.

4. **pattern-discovery** — "what's the convention for adding a new `<thing>`?" AND "does `<specific-entity>` actually follow this convention, or does it deviate?" — requires `grafel_patterns`. Always ask for a concrete conformance check, not just a description.

5. **architecture-overview** — "what are the main subsystems and how do they depend on each other?" — always include once. Tier 2 minimum: must ask for dependency direction, not just names.

6. **subsystem-deep-dive** — "describe the internal structure and key call flows within `<cluster-label>`." — pick a Louvain cluster with ≥10 members. Requires internal call-chain enumeration, not just member listing.

7. **specific-trace** — "trace from `<entry-entity>` to the data write (or network call), naming every intermediate entity." — pick a real entry-point (HTTP route, button handler, job trigger). Tier 3: answer must name ≥3 hops.

8. **data-access** — "where does `<Entity>.<field>` get read or written, and which callers have write access?" — pick a field with ≥2 readers and ≥1 writer. Must include a negative check: "is there any component that reads this field WITHOUT going through `<expected-accessor>`?"

9. **http-cross-repo** — "what endpoints does `<client-repo>` actually call on `<server-repo>`, and which server-side handlers service them?" — only if the group has a client + server pair.

10. **end-to-end-feature** — "explain, end-to-end, how `<visible-feature-or-business-outcome>` is implemented. Start from the entry point, trace through every layer, and name the key entities at each layer." — pick a non-trivial feature (report generation, sync pipeline, notification dispatch, payment flow, etc.). The expected answer must span ≥4 entities across ≥2 layers.

## Adaptation rules

- Never invent entity names. Every entity in every question MUST exist in the group (verified via `grafel_find` or `grafel_inspect`).
- If a category cannot be filled (e.g., single-repo group has no cross-stack trace), record `"skipped": true` for that category with a reason string. The final report flags skipped categories.
- For single-repo groups: treat "cross-repo" questions as "cross-subsystem" — trace across API, service, and persistence layers instead of across repos.
- If `--focus <category>` is set, only generate questions from that category (4-8 of them) and skip the others entirely.
- If `--question-set <path>` is set, **skip auto-generation** — load that file, validate the schema below, and pass through. Each question's entities must still be verified to exist; flag any that don't. Also verify the loaded set meets the complexity mandate (average ≥ Tier 2.5) and warn if it does not.

## Output schema (`questions.json`)

```json
{
  "version": 2,
  "group": "<group-name>",
  "repos": ["<repo-1>", "<repo-2>"],
  "generated_at": "<RFC3339>",
  "focus": "<category-or-null>",
  "complexity_summary": {
    "tier1_count": 1,
    "tier2_count": 4,
    "tier3_count": 8,
    "average_tier": 2.7
  },
  "cross_repo_count": 6,
  "single_repo_count": 7,
  "questions": [
    {
      "id": "q01",
      "category": "cross-stack-tracing",
      "tier": 3,
      "requires_negative_evidence": false,
      "question": "How does the proposals report in the frontend reach the backend and ultimately the database? Trace the full path from the UI component through the API call, backend handler, service logic, and data layer, naming every intermediate entity.",
      "anchors": [
        {"kind": "entity_name", "value": "ProposalsReport"},
        {"kind": "repo", "value": "web-frontend"},
        {"kind": "entity_name", "value": "ProposalsHandler"},
        {"kind": "repo", "value": "api-backend"}
      ],
      "expected_signals": [
        "UI component or page that initiates the request",
        "HTTP client call with endpoint URL",
        "backend route registration",
        "handler function name and file",
        "service or business-logic layer entity",
        "ORM or query method used for data access",
        "layer transitions explicitly described"
      ]
    },
    {
      "id": "q02",
      "category": "entity-lookup",
      "tier": 1,
      "requires_negative_evidence": false,
      "question": "What is OrderService? What is its role, where is it defined, and what are its primary callers?",
      "anchors": [
        {"kind": "entity_name", "value": "OrderService"},
        {"kind": "repo", "value": "shop-backend"}
      ],
      "expected_signals": [
        "definition location (file + line)",
        "kind (class/struct/module)",
        "key methods",
        "callers or referencers"
      ]
    },
    {
      "id": "q03",
      "category": "data-access",
      "tier": 2,
      "requires_negative_evidence": true,
      "question": "Where is `Order.status` read and written? Which callers have write access? Is there any component that reads this field WITHOUT going through `OrderService`?",
      "anchors": [
        {"kind": "entity_name", "value": "Order"},
        {"kind": "field", "value": "status"},
        {"kind": "repo", "value": "shop-backend"}
      ],
      "expected_signals": [
        "list of readers with file locations",
        "list of writers with file locations",
        "whether all readers go through OrderService",
        "any direct-access outliers (negative evidence)"
      ]
    }
  ],
  "skipped_categories": [
    {"category": "http-cross-repo", "reason": "single-repo group"}
  ]
}
```

- `tier` records the expected tool-call depth (1/2/3) for scoring and complexity validation.
- `requires_negative_evidence` flags questions that require the agent to disprove an expected relationship.
- `anchors` are the real entities the question references. The judge in Phase 4 uses them to seed ground-truth discovery.
- `expected_signals` is a free-form list of facts the judge expects in a "full" answer. Used for scoring coverage. For Tier 3 questions, list ≥5 signals spanning multiple layers.
- `complexity_summary` is computed from the final question set and included for audit.

## Privacy and naming

- Never include source-code content in the question or anchors.
- Never use the name of any competitor tool ("predecessor MCP tool" / "Tool A" if needed).
- File paths in anchors are relative to repo root.

## Output

Write `questions.json` to the run directory and print a short summary table:

```
id  | tier | requires_neg | category              | question (truncated)
q01 |   3  |    false     | cross-stack-tracing   | How does the proposals report...
```

Also print the `complexity_summary` block. Do not call any tool again after the write — return control to the orchestrator.
