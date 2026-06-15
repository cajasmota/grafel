# Cross-link format

How to format links that cross repo boundaries inside generated docs.

## In-repo links

Plain relative markdown links. Standard practice:

```markdown
See [`OrderViewSet`](../modules/orders/README.md#orderviewset) for handler details.
```

## Cross-repo links

A cross-repo link points from one repo's docs to another repo's docs. Two conventions, in priority order:

### 1. Relative path link

Link by relative path through the group state directory:

```markdown
See [`BillingService`](../../../<other-repo>/docs/modules/billing/README.md#billingservice) in the billing repo.
```

The `../../../` count depends on where the source file lives; producers must compute it correctly.

### 2. Bridge by heading slug only

When a real link path is impractical (e.g., the target repo has not been documented yet), reference the target by name in backticks and let the slug-collision rule (ADR-0007) bridge it in the graph:

```markdown
The order flow ultimately calls `BillingService` in the billing repo.
```

This is the **lowest-fidelity option** — the reader cannot click through. Use it only when option 1 is unavailable.

## When `grafel_cross_links` returns a pending candidate

A pending candidate from `grafel_cross_links(action=list)` is **not** a confirmed link. While it is `pending`, do not write it as a fact. Write it as:

```markdown
> Pending cross-repo link: `<this entity>` may invoke `<other entity>` in `<other repo>`. Pass 8 will confirm.
```

Once Pass 8 calls `grafel_cross_links(action=accept, candidate_id=...)`, the candidate is a fact and can be cited normally.

## When the target is in another grafel group

Multi-group references use the explicit `group=<name>` route:

```markdown
See `<entity>` in [`<other-group>`](/groups/<other-group>/docs/) (other grafel group).
```

The agent cannot follow this link via MCP unless the other group is loaded; documentation only.
