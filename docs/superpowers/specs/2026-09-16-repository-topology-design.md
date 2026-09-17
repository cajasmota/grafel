# Repository Topology and Large-Graph Performance Design

## Goal

Make very large multi-repository groups understandable and usable without downloading or rendering the full entity graph. Add a repository-level topology that preserves cross-repository relationships, supports server-side analysis filters and progressive drill-down, and keeps the existing entity Graph, Dubbo, Links, Topology, and MCP surfaces compatible.

The complete feature ships in one feature branch and one pull request. The implementation may use multiple focused commits, but the user-visible repository topology, advanced filters, drill-down, and existing Graph performance protections are delivered and validated together.

## Problem

The current Graph screen is entity-centric. Every visible class, method, field, endpoint, service, and related entity is a node, while relationships and cross-repository links become edges. Repository mode changes coloring and grouping but does not make repositories themselves the graph nodes.

This model becomes unusable for very large groups containing hundreds of thousands of entities and more than a million relationships. The Graph screen prefers the SSE endpoint, but the stream currently builds the full graph and sends it in chunks. The frontend then accumulates the complete payload, repeatedly updates React state, applies most repository and edge-kind filters locally, and finally sends a large node and edge set to the WebGL renderer. Streaming improves progress visibility but does not reduce graph construction, transfer size, browser memory, or layout cost.

The current default level of detail is `high`, and `high` has an unlimited node cap. The non-streaming endpoint understands LoD, but the streaming URL and handler do not apply the selected LoD. Module Overview is visually smaller, but the route still starts the entity-graph stream before deciding which surface to render.

Even if the complete graph loaded quickly, hundreds of thousands of entities do not form an understandable architecture view. The default experience must operate at repository level and load entity details only after an explicit drill-down.

## User Experience

### Navigation and compatibility

- Add a `Repository Topology` group navigation item at `/g/:groupId/repository-topology`.
- Keep the existing `/g/:groupId/graph` entity Graph unchanged as a separate advanced surface, apart from bounded performance and LoD fixes described below.
- Keep existing Dubbo, Links, message Topology, Paths, and MCP behavior unchanged.
- Do not redirect the existing Graph route or silently change its data semantics.

### Default repository topology

- Render one node per Git repository.
- Show only repositories that have at least one edge after all active filters are applied.
- Default relationship channels are Dubbo, HTTP, Kafka, and RabbitMQ.
- Aggregate relationships by source repository, target repository, and channel.
- Preserve direction. A relationship from repository A to repository B is distinct from B to A.
- Show edge thickness from the number of underlying relationships, with a bounded visual scale so one high-volume pair does not dominate the canvas.
- Show channel, relationship count, evidence status, and issue counts on hover or selection.
- Use a deterministic, static layout suitable for tens or hundreds of repository nodes. Do not run the full entity force simulation.

### Repository nodes

Each repository node shows or exposes:

- repository slug and primary language;
- indexed entity and module counts;
- filtered inbound and outbound relationship counts;
- distinct connected repository count;
- graph/index state when available;
- confirmed, inferred, dangling, and ambiguous relationship counts.

Repository statistics describe the filtered repository topology, while entity and module totals describe the underlying indexed repository. Labels must distinguish these two scopes.

### Aggregated edges

Each repository edge represents one channel between one ordered repository pair. The initial supported channels are:

- `dubbo`;
- `http`;
- `kafka`;
- `rabbitmq`;
- `other` for cross-repository links that cannot be assigned to a first-class channel.

The API must not classify ordinary internal `CALLS`, `REFERENCES`, `CONTAINS`, `IMPORTS`, or similar structural edges as cross-repository communication unless the persisted cross-repository link explicitly represents such a relationship.

An aggregated edge includes:

- source and target repository;
- normalized channel;
- total underlying relationship count;
- distinct contract or identifier count;
- counts by evidence status;
- representative labels for compact display;
- whether more detail rows exist than the inline sample contains.

When two repositories communicate through multiple channels, render parallel or separately selectable edges rather than merging all protocols into an untyped edge.

### Analysis filters

All filters execute on the server before the response is returned:

- channels: Dubbo, HTTP, Kafka, RabbitMQ, other;
- repository selection;
- focus repository;
- direction: inbound, outbound, or both;
- depth: one to three repository hops;
- evidence: confirmed, inferred, dangling, ambiguous, external;
- minimum underlying relationship count;
- source repository and target repository path query;
- free-text matching on repository, contract, identifier, endpoint, topic, queue, routing key, or representative label.

After edge filtering, remove repository nodes with zero remaining degree. When a focus repository is supplied, return only the requested directed neighborhood. When source and target repositories are supplied, return the bounded shortest path result rather than the entire connected component.

### Analysis presets

Provide presets that configure filters without introducing separate backend modes:

- Cross-repository overview: Dubbo, HTTP, Kafka, and RabbitMQ; connected only.
- Dubbo calls: Dubbo only.
- Message flow: Kafka and RabbitMQ.
- HTTP dependencies: HTTP only.
- Requirement impact: selected focus repository, inbound and outbound, depth two.
- Incident investigation: selected focus repository, inbound and outbound, depth three, including unresolved evidence.
- Graph health: inferred, dangling, ambiguous, and external relationships.
- Repository path: selected source and target repositories.

Presets remain editable after selection. The resulting explicit filter state is reflected in the URL so the view can be bookmarked and shared.

### Selection and drill-down

- Selecting a repository opens a repository detail panel with summary metrics, inbound and outbound grouped relationships, and modules.
- Selecting an aggregated edge opens a paginated relationship-detail panel.
- Relationship details show channel-specific contract metadata, evidence status, source and target repository, source references, identifiers, and properties available on the persisted cross-repository link.
- Dubbo detail rows link to the existing Dubbo screen when an exact service contract is available.
- Kafka and RabbitMQ detail rows link to the existing message Topology detail surface when a corresponding topology entity is available.
- HTTP detail rows link to relevant entity Graph nodes or source references.
- A user may explicitly open a bounded entity view for a selected repository, repository pair, or relationship. The drill-down must pass repository and relationship filters to the backend rather than opening the unfiltered full graph.

## Backend Architecture

### Dedicated endpoint

Add a dedicated v2 repository-topology endpoint instead of reusing the entity Graph response:

```text
GET /api/v2/repository-topology/{group}
```

Supported query parameters:

```text
channels=dubbo,http,kafka,rabbitmq,other
repos=repo-a,repo-b
focus=repo-a
direction=inbound|outbound|both
depth=1|2|3
evidence=confirmed,inferred,dangling,ambiguous,external
min_count=1
source=repo-a
target=repo-b
q=search-text
```

Invalid repository slugs, channels, evidence values, directions, depths, or count values return a structured v2 bad-request response. `source` and `target` must be supplied together. Path mode and focus mode are mutually exclusive.

### Data source and aggregation

- Build repository topology directly from repository metadata and persisted cross-repository links in the loaded group.
- Do not call `buildV2Graph`, allocate entity-level wire nodes, or traverse all internal repository relationships.
- Normalize the channel from structured link metadata first, then stable method/kind fields, then contract identifiers. Use a deterministic `other` fallback rather than dropping unknown cross-repository relationships.
- Resolve source and target repository from persisted link endpoints. Never infer a repository solely from a display label.
- Aggregate in one pass over the relevant link set using a key of `(sourceRepo, targetRepo, channel)`.
- Sort repositories, edges, facets, samples, and detail rows deterministically.

The initial topology response includes bounded representative relationship samples only. Full relationship rows are served by a detail endpoint:

```text
GET /api/v2/repository-topology/{group}/edge
    ?source=repo-a
    &target=repo-b
    &channel=dubbo
    &page=1
    &page_size=25
```

The detail endpoint applies the same evidence and text filters where relevant, defaults to 25 rows, and caps page size at 100.

### Evidence model

Normalize underlying relationships into these evidence states:

- `confirmed`: both endpoints and the relationship contract resolve unambiguously;
- `inferred`: the relationship is produced by an inference pass or marked inferred by persisted metadata;
- `dangling`: only one relationship endpoint resolves inside the indexed group;
- `ambiguous`: multiple candidates remain and no unique target is selected;
- `external`: the resolved target is intentionally outside the indexed group.

If current persisted records cannot distinguish one of these states, report it as unresolved metadata in the detail record rather than inventing certainty. The implementation plan must identify the existing fields used for every classification and add focused decoding tests before UI work depends on them.

An aggregated edge exposes counts for every state. Its display status is not a lossy single enum: the UI derives styling from the counts and lets the user filter to exact evidence subsets.

### Repository graph filtering

Apply filters in this order:

1. normalize and validate query parameters;
2. select persisted cross-repository links by channel, evidence, repository set, and text query;
3. aggregate matching links into repository edges;
4. remove edges below `min_count`;
5. build repository adjacency from the remaining directed edges;
6. apply focus/depth traversal or source/target shortest-path traversal;
7. discard edges outside the selected repository set;
8. remove zero-degree repository nodes;
9. compute response facets and filtered summary counts.

Depth traversals are bounded to three hops. Path mode returns one deterministic shortest repository path, with stable tie-breaking by repository slug and channel ordering. The response must indicate when safety limits truncate the result.

### Caching and limits

- Cache unfiltered repository aggregation per loaded graph generation, not per arbitrary query string.
- Apply lightweight filtering, traversal, and pagination to the cached aggregation and link indexes.
- Invalidate topology caches whenever the loaded group graph generation changes.
- Cap repository nodes, aggregated edges, inline samples, detail page size, and search result work.
- Return `truncated`, applied limits, and pre-truncation counts whenever a cap changes the response.
- Record handler timing and result-size diagnostics using existing dashboard conventions.

The repository topology must remain small enough to return as ordinary JSON. It does not require SSE in the initial implementation.

## Frontend Architecture

### Data layer

- Add repository-topology wire types in `webui-v2/src/data/types.ts`.
- Add API methods for summary and paginated edge details.
- Add TanStack Query hooks keyed by group and every explicit filter.
- Keep pure helpers for URL filter parsing, preset expansion, and view-model conversion separately testable.

### Route and components

Add a dedicated route and focused components rather than growing `graph.tsx` or `graph-canvas.tsx` further:

- repository-topology route and toolbar;
- repository topology canvas;
- filter drawer;
- repository detail panel;
- edge detail panel;
- preset selector;
- empty, truncated, loading, and error states.

Reuse existing UI primitives, repository colors, source-reference components, and a deterministic graph-layout approach from the compound/module topology surfaces. Do not reuse the entity Graph force simulation or its very large canvas component as the primary renderer.

### URL state

Encode explicit filter state in search parameters. Opening a preset writes the expanded filter values, not only the preset name, so bookmarked links remain stable if preset defaults evolve. Invalid URL values fall back to documented defaults and display no destructive side effects.

### Empty and large-result states

- If no relationships match, show a clear filtered-empty state and retain all controls.
- If the selected focus repository has no matching cross-repository relationships, keep the selected repository visible in the detail context but do not render it as an isolated canvas node.
- If a result is truncated, show a visible warning with the active limits and suggestions to add a focus repository, channel, or smaller depth.
- Do not silently omit links or repositories.

## Existing Graph Performance Protections

The same pull request fixes the current entity Graph without changing its response semantics for bounded requests:

- Add `lod` to `graphStreamUrl` and pass the selected LoD from the Graph route.
- Apply `lodNodeCap` in the streaming handler, using the same graph builder and cap behavior as the non-streaming endpoint.
- Include LoD and other server-side filters in stream and payload cache keys.
- Do not start entity-graph streaming while Module Overview is the active surface.
- Pass active repository and supported relationship filters to the server before entity payload construction where the existing API contract supports them.
- Replace unlimited `high/full` behavior with a documented finite safety cap appropriate for browser rendering.
- Add a server-side edge safety cap paired with explicit truncation metadata; never emit edges whose endpoints are absent.
- Keep an intentional full-graph option for bounded groups, but require an explicit user action when an estimate exceeds safe thresholds.
- Preserve progressive loading, fallback behavior, deterministic stream ordering, and existing graph deep links.

The exact entity node and edge caps must be selected from benchmarks rather than guessed. Tests pin cap consistency and truncation metadata; browser verification on a large multi-repository fixture confirms the page remains responsive.

## Performance Requirements

- Repository Topology initial loading must scale with cross-repository links and repository count, not total entity relationships.
- The initial response must not contain entity-level node or edge arrays.
- Server-side filtering must prevent excluded relationships from reaching the browser.
- Repository Topology must remain interactive for large multi-repository groups.
- Opening Repository Topology must not trigger `/api/v2/graph/{group}` or `/stream`.
- Opening Module Overview must not trigger the entity graph stream.
- Edge details must remain paginated and bounded.
- Entity Graph safety limits must prevent browser hangs and allocation failures while reporting all omissions.

Performance verification records:

- endpoint duration;
- response bytes;
- repository node and aggregated edge counts;
- entity Graph streamed node and edge counts at every LoD;
- browser time to first useful render;
- browser responsiveness and memory behavior during interaction.

## Error Handling

- Use existing v2 response envelopes and error codes.
- Distinguish invalid filters, unknown repositories, graph warming, graph load failure, truncated results, and empty results.
- A failed edge-detail request must not remove or reset the loaded repository topology.
- A stale or reloaded group invalidates cached results and lets TanStack Query refetch normally.
- UI errors must include a retry action and enough detail to distinguish a filter error from a daemon/index problem.

## Compatibility and Non-Goals

### Compatibility guarantees

- Existing entity Graph endpoints remain available.
- Existing Graph, Dubbo, Links, Topology, Paths, Settings, and MCP routes remain available.
- Existing persisted graph and cross-repository link formats continue to decode.
- No extractor behavior changes are required for the repository topology itself.
- The local unpublished MQ tracing worktree is not copied into this pull request.
- Existing Dubbo and message relationships are consumed through official persisted graph/link data.

### Non-goals

- Replacing Grafel's graph store.
- Rendering every entity in Repository Topology.
- Runtime traffic, latency, health, or production observability metrics.
- Editing graph relationships from the topology canvas.
- Unlimited path enumeration.
- Guaranteeing a repository topology relationship when the underlying persisted graph lacks sufficient metadata.

## Testing Strategy

### Go tests

- channel and evidence normalization;
- repository endpoint resolution;
- deterministic aggregation and ordering;
- connected-only cleanup;
- channel, repository, direction, evidence, count, and text filters;
- one-to-three-hop inbound/outbound traversal;
- deterministic shortest repository path;
- truncation and safety limits;
- cache invalidation on graph generation changes;
- paginated edge details and maximum page size;
- v2 validation and error envelopes;
- streaming and non-streaming LoD parity;
- no dangling entity edges after node/edge capping;
- compatibility tests for existing Graph routes.

### Frontend tests

- URL filter parsing and serialization;
- preset expansion;
- repository and edge view-model conversion;
- connected-only behavior as represented by the API;
- edge thickness and evidence styling;
- repository and edge selection;
- detail pagination;
- empty, truncated, loading, and error states;
- no entity Graph request from Repository Topology or Module Overview;
- Graph stream URL includes LoD and supported server-side filters.

### Build and regression validation

- focused Go dashboard tests;
- complete affected Go package tests;
- frontend unit tests, type checking, lint, and production build;
- `go vet` for affected packages or the full repository as required;
- dashboard bundle verification;
- existing Dubbo, Topology, Links, and Graph focused regression suites;
- coverage matrix validation only if implementation changes a tracked extraction capability;
- quality ratchet only if extraction behavior changes;
- browser verification against a large multi-repository fixture;
- network inspection proving the initial repository page does not download the entity graph.

## Delivery

- Develop in `feat/repository-topology` under an isolated worktree.
- Keep implementation commits focused even though delivery uses one pull request.
- Include before/after performance evidence and screenshots in the pull request.
- Document endpoint contracts and the distinction between entity Graph, message Topology, and Repository Topology.
- Run graph/change analysis and inspect the staged file list before every commit.
- Push only the feature branch to the contributor fork and open one pull request against upstream `main` after all validation passes.
