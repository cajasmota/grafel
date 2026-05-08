# ADR-007: Doc-as-bridge for cross-repo and dynamic connections

- **Status**: Accepted
- **Date**: 2026-05-08
- **Deciders**: Jorge Cajas

## Context

Static analysis can see what is written in source: imports, calls, class hierarchies, IaC declarations. It cannot see connections that materialize only at runtime or only through deployment configuration:

- HTTP routes whose paths are constructed dynamically from environment variables or feature flags.
- Queues, topics, and tables wired by IaC (Terraform, CDK, Pulumi) where the consumer is identified by string ARN at deploy time.
- Lambda triggers configured outside the lambda's own code.
- Cross-repo couplings where service A calls service B through a generated client whose target host is environment-dependent.

These dynamic connections are precisely the ones developers and AI agents most need help understanding, because they are invisible at code-read time.

archigraph already indexes markdown documentation alongside code (heading entities, code-block entities, link edges). Generated documentation read by AI writer agents (or by humans following a documentation discipline) routinely names code identifiers in headings and prose. If we treat those mentions as graph signals, documentation becomes a way to encode dynamic connections that static analysis cannot see.

## Decision

archigraph indexes markdown documentation as first-class graph content. Markdown headings and code blocks become nodes in the same graph as code symbols. The key collision rule: when a markdown heading uses a backticked code identifier (e.g., `` `OrderViewSet` ``), the **slug derived from the heading collides with the code symbol's slug**, so the two nodes merge in the graph.

A documentation page that says:

> ## How `OrderViewSet` calls `BillingService`

becomes a node that the graph treats as the same entity as the code symbol `OrderViewSet`, with edges to the doc page that mentions `BillingService`. The doc page therefore becomes a queryable bridge between two code symbols that have **no static edge** between them. Writer agents (via archigraph's doc-generation skill) effectively contribute graph edges by writing prose, with the slug-collision rule turning prose into structured data.

Two conventions support this in practice:

1. The rendering convention `_graph-searchability.md` documents that **headings naming code symbols must wrap them in backticks**. Without backticks, the slug collision does not happen and the bridge does not form.
2. The doc-generation skill enforces the convention when writing new pages and flags violations during reviews.

## Consequences

### Positive
- Dynamic connections become queryable without runtime instrumentation or deploy-time tracing.
- Writer agents add architectural value, not just prose; their output is structurally meaningful to the graph.
- A documentation page is a natural place for humans to record connections they know about that the parser cannot see.
- The same mechanism solves cross-repo coupling: a doc page describing service-to-service calls bridges symbols that live in different repos.

### Negative
- Stale documentation degrades the bridge until regenerated; the graph trusts the docs, and out-of-date docs can mislead.
- Heading-text changes mutate the slug ID, so renaming a heading silently breaks bridge edges. Mitigation: writer agents are instructed not to rename headings without intent.
- Authors who do not follow the backtick convention create headings that look right to humans but do not bridge in the graph. The convention must be taught, tooled, and lint-enforced.
- Increases the importance of doc quality; archigraph effectively makes documentation part of the architecture.

### Neutral
- This decision treats documentation as a peer of code in the data model, which is consistent with the SCOPE taxonomy in ADR-003 (markdown content can map to `SCOPE.Component` / `SCOPE.Reference` and similar kinds).
- The slug-collision behavior is documented in `INDEXING.md` and tested with fixtures.

## Alternatives considered

- **Runtime tracing to discover dynamic connections** — rejected for v1.0: requires deploying instrumentation, has privacy/cost implications, and is out of scope for a static-analysis-first tool.
- **Manual edge-injection files** (a YAML where users declare extra edges) — rejected: writing YAML is a worse author experience than writing prose, and we already need docs for humans regardless.
- **Treat docs as opaque text and keyword-search at query time** — rejected: loses graph structure, no community/centrality benefits from ADR-005, no slug-level joins.
- **Require a structured frontmatter block declaring connections** — partial overlap with the chosen approach; we permit frontmatter for explicit declarations but the slug-collision rule does most of the work invisibly.
