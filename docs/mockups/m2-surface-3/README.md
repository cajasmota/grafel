# Surface 3 — Broker Topology: Mockup Package

**Sprint:** Milestone 2
**Date:** 2026-05-20
**Status:** Design complete, pending frontend implementation

---

## Files in this directory

| File | Description |
|---|---|
| `topology-full.png` | View 1: Full topology map (Kafka + RabbitMQ + SQS + NATS, producer/consumer spokes) |
| `topology-inspector.png` | View 2: Topic inspector panel (user.events selected) + View 3: WS/SSE channel track + View 4: GraphQL subscriptions |
| `mockup-topology-full.svg` | Source SVG — editable |
| `mockup-topology-inspector.svg` | Source SVG — editable |

---

## React component → plan section mapping

| Visual element | React component (§4) | File location |
|---|---|---|
| Force-directed topology canvas (2D) | `<TopologyCanvas>` | `components/topology/TopologyCanvas.tsx` |
| Kafka/RabbitMQ/SQS/NATS hub node | `<TopicNode>` | `components/topology/TopicNode.tsx` |
| Arrow arc from topic to producer | `<ProducerSpoke>` | `components/topology/ProducerSpoke.tsx` |
| Arrow arc from topic to consumer | `<ConsumerSpoke>` | `components/topology/ConsumerSpoke.tsx` |
| Dashed topic→topic edge | `<TransformEdge>` | `components/topology/TransformEdge.tsx` |
| WS/SSE/GraphQL sub tier | `<ChannelTrack>` | `components/topology/ChannelTrack.tsx` |
| Protocol chip row (Kafka, RabbitMQ, ...) | `<BrokerFilter>` | `components/topology/BrokerFilter.tsx` |
| Slide-in topic detail panel | `<TopicInspector>` | `components/topology/TopicInspector.tsx` |
| Scheduled job list in sidebar | `<ScheduledJobList>` | `components/topology/ScheduledJobList.tsx` |

**Total atomic components: 9**

---

## Hook contracts

| Hook | Responsibility |
|---|---|
| `useTopologyData(groupId, brokerFilter)` | `GET /api/topology/{group}` — topics, queues, channels, producers, consumers |
| `useBrokerFilter()` | URL param `brokers`; multi-select toggle |
| `useTopicInspector(topicId)` | Entity neighbors for selected topic node |
| `useScheduledJobs(groupId)` | `GET /api/groups/{group}/scheduled-jobs` |
| `useChannelEvents(groupId)` | Derived from `useTopologyData` — filters to `ChannelEvent` objects |
| `useTopologyLayout(nodes, edges)` | d3-force simulation; returns stable `Map<id, {x,y}>` |

---

## Visual language

### Broker node color coding

| Broker | Background | Border/text | Inner mark |
|---|---|---|---|
| Kafka | `#1B1000` | `#F59E0B` (amber-500) | "K" label |
| RabbitMQ | `#1E0808` | `#F43F5E` (rose-500) | "R" label |
| SQS | `#0D1A2E` | `#3B82F6` (blue-500) | "S" label |
| Pub-Sub | `#1A1030` | `#8B5CF6` (violet-500) | "P" label |
| NATS | `#1A0D30` | `#A855F7` (purple-500) | "N" label |

### Spoke color coding (by repo)
Spoke color = repo color derived from `useCommunityColors` repo palette (seeded, deterministic):
- `acme-api` = `#22C55E` (green)
- `acme-frontend` = `#3B82F6` (blue)
- `acme-workers` = `#F43F5E` (rose)
- `acme-mobile` = `#A855F7` (purple)

### Edge types
- **ProducerSpoke:** solid line with arrowhead pointing away from topic
- **ConsumerSpoke:** solid line with arrowhead pointing toward consumer
- **TransformEdge:** dashed `#64748B` line with `TRANSFORMS` label chip between topics

### Channel track visual tier
WS, SSE, and GraphQL subscriptions are rendered in a **separate panel below the broker graph**, not intermixed with topic nodes. Reason: they lack the producer/consumer fan-out shape — they're request/response push channels, not queues. The `<ChannelTrack>` component is visually distinct with a purple/violet color scheme and appears below the main canvas.

---

## Interaction notes

### Hovering a topic node
- **[ANIM]** Non-connected nodes + spokes: dim to 0.15 opacity, 150ms ease
- **[ANIM]** Hovered topic: scale 1.05, 120ms ease; glow radius expands
- Tooltip shows: topic name + broker type + producer count + consumer count

### Selecting a topic
- Click topic node → `useTopicInspector(topicId)` fetches neighbors
- **[ANIM]** `<TopicInspector>` slides in from right: `translateX(440px)→translateX(0)`, 280ms ease-out
- Selection ring appears (dashed amber border, 2.5px)

### Protocol filter chip
- Click chip → toggle broker visibility in canvas
- **[ANIM]** Hidden topics: fade out 200ms; remaining topics re-layout via d3-force tick

### BrokerFilter "Repo:" pills
- Multi-select — clicking a repo pill filters spokes to show only that repo's producers/consumers
- Useful for isolating a single service's pub/sub surface

### Producer/consumer spoke hover
- **[ANIM]** Spoke thickens from 1.5px → 3px, 80ms ease
- Callout appears: entity name + `file:line` + edge kind chip

### Topic inspector "Message Shape"
- Inferred from PUBLISHES_TO edge properties
- Amber caveat bar if shape is partially inferred
- Table rows: field name (mono), type (mono colored by kind), required check, source

---

## Empty / Loading / Error states

| State | Display |
|---|---|
| No topology data | EmptyState: "No message brokers detected. Ensure the indexer ran over broker config files." + `[Re-index]` |
| Loading | Skeleton: 4 circular placeholder nodes (shimmer) with 3 spoke stubs each |
| Error | Error card with amber border: "Failed to load topology." + `[Retry]` + server status hint |

---

## Spec ambiguities

1. **`/api/topology/{group}` endpoint does not exist in Section 3 catalog.** The plan lists it in Phase 5 as "ensure it returns shapes" but the endpoint is not formally specified in Section 3. Add to REST catalog with shape: `{ topics: MessageTopic[], queues: QueueNode[], channels: ChannelEvent[], producers: Entity[], consumers: Entity[], transforms: Relationship[] }`.

2. **Transform detection is implicit.** The design models `TRANSFORMS` edges as topic→topic relationships where both source and target are `MessageTopic` or `QueueNode`. The current `internal/types/kinds.go` does not have a `TRANSFORMS` relationship kind — it has `PUBLISHES_TO` and `SUBSCRIBES_TO`. Either add `TRANSFORMS` as a new kind or derive it from two-hop PUBLISHES_TO/SUBSCRIBES_TO chains where the intermediary is an entity that reads one topic and writes another. Backend to clarify.

3. **Scheduled jobs in topology sidebar vs standalone page.** The plan lists `<ScheduledJobList>` as a sidebar component on Surface 3. However, scheduled jobs have no direct relationship to message topics unless they publish/subscribe. The design shows them in a sidebar panel separate from the topology canvas. If jobs do not have PUBLISHES_TO edges, they appear as an orphaned list. Recommendation: only show a job in the topology sidebar if it has at least one PUBLISHES_TO or SUBSCRIBES_TO edge to a displayed topic.

4. **GraphQL subscription detection.** The current extractor detects REST endpoints and WebSocket channels. Whether it detects GraphQL subscriptions is not confirmed in the plan's extraction coverage notes. If not extracted, the `<ChannelTrack>` GraphQL panel will always be empty for current fixture groups.

5. **WS channel multiplicity.** A single WebSocket path (e.g., `/ws/events`) may have many distinct event types published by different handlers. The inspector shows a flat list of event types. If event count exceeds N, "Show all →" pagination is needed but not specified.

---

*Designed by BB-8 (Design Lead) · 2026-05-20*
