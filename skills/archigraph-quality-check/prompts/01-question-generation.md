# Phase 1 - Question generation

You are generating the test set for an archigraph MCP quality benchmark. Your output is a JSON file of ~10-15 questions adapted to the registered group's actual entities. Subsequent phases will answer these questions twice (with MCP, without MCP) and compare results.

## Setup

1. Confirm the run directory: `~/.archigraph/quality-check/<YYYY-MM-DD-HHMMSS>/` (the orchestrator passes the exact path). Create it if missing.
2. Call `archigraph_whoami` to resolve the group, repos, and entity stats. If this fails, **stop** and report "no daemon running" - do not start one.
3. Capture the group's entity-kind distribution and top entities by degree using `archigraph_graph_stats` and `archigraph_list_clusters`.

## Nine question categories

Generate at least one question per category, totaling 10-15 questions:

1. **Entity lookup** - "what is `<EntityName>`?" - pick a real high-degree entity from the group.
2. **Reference finding** - "what calls `<Class>.<method>` (or `<function>`)?" - pick a method with ≥3 callers per graph stats.
3. **Cross-stack tracing** - "how does `<frontend-feature>` reach the backend?" - only include if the group has more than one repo AND `archigraph_list_link_candidates` shows cross-repo edges.
4. **Pattern discovery** - "what's the convention for adding a new `<thing>` (endpoint / handler / migration / etc.)?" - pick a kind with ≥3 promoted patterns from `archigraph_patterns(action=list)`. If none exist, replace with a structural-recurrence question.
5. **Architecture overview** - "what are the main subsystems in this group?" - always include.
6. **Subsystem deep-dive** - "describe `<cluster-label>`." - pick a Louvain cluster from `archigraph_list_clusters` with ≥10 members.
7. **Specific trace** - "trace from `<entry-entity>` to the data write." - pick a real entry-point entity (HTTP route, button handler, etc.).
8. **Data access** - "where does `<Entity>.<field>` get read or written?" - pick a domain field with ≥2 readers and ≥1 writer.
9. **HTTP cross-repo** - "what endpoints does `<client-repo>` actually call?" - only if the group has a client + server pair.

## Adaptation rules

- Never invent entity names. Every entity in every question MUST exist in the group (verified via `archigraph_search` or `archigraph_describe`).
- If a category cannot be filled (e.g., single-repo group has no cross-stack trace), record `"skipped": true` for that category with a reason string. The final report flags skipped categories.
- If `--focus <category>` is set, only generate questions from that category (4-8 of them) and skip the others entirely.
- If `--question-set <path>` is set, **skip auto-generation** - load that file, validate the schema below, and pass through. Each question's entities must still be verified to exist; flag any that don't.

## Output schema (`questions.json`)

```json
{
  "version": 1,
  "group": "<group-name>",
  "repos": ["<repo-1>", "<repo-2>"],
  "generated_at": "<RFC3339>",
  "focus": "<category-or-null>",
  "questions": [
    {
      "id": "q01",
      "category": "entity-lookup",
      "question": "What is OrderService?",
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
    }
  ],
  "skipped_categories": [
    {"category": "cross-stack-tracing", "reason": "single-repo group"}
  ]
}
```

- `anchors` are the real entities the question references. The judge in Phase 4 uses them to seed ground-truth discovery.
- `expected_signals` is a free-form list of facts the judge expects in a "full" answer. Used for scoring coverage.

## Privacy and naming

- Never include source-code content in the question or anchors.
- Never use the name of any competitor tool ("predecessor MCP tool" / "Tool A" if needed).
- File paths in anchors are relative to repo root.

## Output

Write `questions.json` to the run directory and print a short summary table: `id | category | question`. Do not call any tool again after the write - return control to the orchestrator.
