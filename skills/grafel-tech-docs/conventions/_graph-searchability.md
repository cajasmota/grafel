# Universal convention — graph searchability

This convention is **required reading for every other convention**. Stack-specific files include the line "See `_graph-searchability.md`" at the top and assume its rules are already in effect.

## Why this exists

grafel indexes markdown documentation as first-class graph content. A heading whose text contains a backticked code identifier produces a node whose **slug collides** with the slug of the corresponding code symbol. The two nodes merge in the graph (ADR-0007). The doc page therefore becomes a queryable bridge between code symbols, including symbols in different repos that have no static edge between them.

If a writer agent forgets the backticks, the slug collision does not happen, and the bridge does not form. A heading that looks fine to a human ("How OrderViewSet calls BillingService") produces a node slug `how-orderviewset-calls-billingservice`, which does not collide with the code symbol `OrderViewSet` (slug `orderviewset`). The bridge silently fails to materialize.

## Rules

### Rule 1 — Backticks on every code identifier in every heading

Every function name, class name, method name, type, struct, enum, constant, env-var, file path, package name, module name, route path, queue name, table name, and ARN that appears in a heading must be wrapped in backticks.

Bad:

```markdown
## How OrderViewSet calls BillingService
```

Good:

```markdown
## How `OrderViewSet` calls `BillingService`
```

### Rule 2 — Backticks on identifiers in prose too

Same rule applies in body text, lists, and table cells. The graph indexes inline code as well as headings.

### Rule 3 — Stable heading text

Renaming a heading mutates its slug, which silently breaks any bridge edge that pointed at the old slug. Treat heading text as a stable identifier:

- If you must rename, do it as a deliberate refactor; leave a redirect note in the file.
- Pass 8 (`08-cross-link.md`) re-validates link slugs after edits and surfaces breakage.

### Rule 4 — Code blocks have language tags

Every fenced code block carries a language tag. This is partly cosmetic but it also lets grafel's markdown extractor decide whether to parse the block as code (and therefore extract identifiers as nodes) or treat it as opaque.

Bad:

````markdown
```
def foo():
    pass
```
````

Good:

````markdown
```python
def foo():
    pass
```
````

### Rule 5 — File paths are backticked, not linked

A heading or sentence that names a file (e.g., `internal/cli/install.go`) puts the path in backticks. Do not turn it into a markdown link unless the link target is a real, resolvable doc page. Backticked paths participate in graph indexing; markdown links do not (the link text becomes opaque).

### Rule 6 — One identifier per backtick group

`` `OrderViewSet.create` `` is one node; `` `OrderViewSet` and `create` `` is two. Be specific: use the qualified form when you mean a method, the bare class name when you mean the class.

### Rule 7 — Avoid pluralization inside backticks

`` `OrderViewSet`s `` (the `s` is outside the backticks) is correct. `` `OrderViewSets` `` mints a different slug that does not exist in the code graph.

## How to verify

`snippets/verification-checklist.md` includes a regex that flags headings containing CamelCase or `snake_case` words outside backticks. The orchestrator runs that check before accepting any pass output.
