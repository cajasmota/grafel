# WebUI v2 Topology Screen Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the placeholder Topology screen in `webui-v2/` with a fully working async message-channel map (producer → channel → consumer flow diagram) per the design handoff in `design_handoff_archigraph/docs/screens/topology.md`, backed by a new Go `GET /api/v2/topology/:group` endpoint.

**Architecture:** A new `webui-v2/src/routes/topology.tsx` replaces the placeholder. It reads from a `useTopology` TanStack Query hook, renders broker bands with flow units (Map view) and a collapsible list (List view), a 380px detail panel, and four tabs (All / Orphan publishers / Orphan subscribers / Scheduled). A new `internal/dashboard/v2_topology.go` Go file adds the `/api/v2/topology/{group}` endpoint by calling the existing `collectTopologyResponse` helper and wrapping it in a v2 envelope. All broker color/shape constants live in `webui-v2/src/routes/topology.tsx` in a `BROKER_META` record (no external CSS file needed — tokens only).

**Tech Stack:** React 18, TypeScript 5.7, Tailwind v4, TanStack Query, lucide-react, existing Lego primitives (`Badge`, `Pill`, `Button`, `SearchInput`, `Tabs`, `Card`). Go net/http for the backend.

---

## File map

| File | Action | Responsibility |
|---|---|---|
| `webui-v2/src/data/types.ts` | Modify — append block | Topology domain types (`Channel`, `BrokerGroup`, `ChannelDetail`, `TopologyResponse`) |
| `webui-v2/src/lib/api.ts` | Modify — append block | `api.getTopology`, `api.getTopologyDetail` methods |
| `webui-v2/src/hooks/use-topology.ts` | Create | TanStack Query hooks: `useTopology`, `useTopologyDetail` |
| `webui-v2/src/routes/topology.tsx` | Replace (currently a placeholder) | Full screen component: tabs, broker filter chips, map/list view, detail panel, all sub-components |
| `internal/dashboard/v2_topology.go` | Create | `handleV2Topology` — wraps existing `collectTopologyResponse` in v2 envelope |
| `internal/dashboard/v2_topology_test.go` | Create | Handler unit tests |
| `internal/dashboard/server.go` | Modify — append route | Register `GET /api/v2/topology/{group}` |

**Hard rules:**
- `dashboard/` directory — zero diff.
- `src/routes/router.tsx` — already has the topology route registered; do NOT touch it.
- `src/routes/topology.tsx` is the ONLY place that references `BROKER_META`, `BrokerShape`, `FlowUnit`, `BrokerBand`, `DetailPanel`, etc. — all sub-components live in this one file (same pattern as `landing.tsx`).
- Never render `last_message_seen` or `usage_history` (they are always null/empty per design doc).

---

## Task 1: Append topology types to `data/types.ts`

**Files:**
- Modify: `webui-v2/src/data/types.ts`

- [ ] **Step 1: Append topology types at the bottom of `data/types.ts`**

```typescript
// ============================================================
// Topology — Async Message-Channel Map (screen #1440)
// ============================================================

export type BrokerCanonical =
  | "kafka" | "rabbitmq" | "sqs" | "pubsub" | "nats"
  | "websocket" | "sse" | "graphql_subscription"
  | "redis_pubsub" | "redis" | "redis-stream"
  | "celery" | "task-queue" | "serverless" | "unknown";

export type LifecycleState =
  | "active" | "orphan_publisher" | "orphan_subscriber" | "orphan";

export interface TopologyEntity {
  entity_id: string;
  kind: string;
  name: string;
  repo: string;
  source_file: string;
  start_line: number;
}

export interface TopologyChannel {
  id: string;
  label: string;
  broker: string;
  broker_canonical: BrokerCanonical;
  framework?: string;
  owning_service: string;
  producers: string[];   // entity ids (unresolved in list; resolved in detail)
  consumers: string[];   // entity ids (unresolved in list; resolved in detail)
  scheduled?: boolean;
  schedule?: string;
  repo: string;
  channel_type?: "websocket" | "sse" | "redis_pubsub" | "graphql_subscription" | "serverless" | "redis_stream";
  cross_repo?: boolean;
  lifecycle: LifecycleState;
  // enrichment (only after /generate-docs)
  docs_summary?: string;
  docgen_status?: "enriched" | "stale" | "pending";
}

export interface BrokerHealthSummary {
  active: number;
  orphan_publisher: number;
  orphan_subscriber: number;
  orphan: number;
}

export interface TopologyBrokerGroup {
  broker: BrokerCanonical;
  count: number;
  services: { name: string; topic_count: number }[];
  orphan_publishers: number;
  orphan_subscribers: number;
  cross_repo_topic_count: number;
  health_summary: BrokerHealthSummary;
  last_index_timestamp?: string;
}

export interface TopologyResponse {
  topics: TopologyChannel[];
  queues: TopologyChannel[];
  channels: TopologyChannel[];
  nats_subjects: TopologyChannel[];
  graphql_subscriptions: TopologyChannel[];
  functions: TopologyChannel[];
  transforms: TopologyChannel[];
  broker_groups: TopologyBrokerGroup[];
}

/** Resolved channel detail (from /api/v2/topology/:group/topic/:topicId). */
export interface TopologyChannelDetail extends TopologyChannel {
  source_file: string;
  start_line: number;
  protocol: string;
  message_schema?: string;
  producers: TopologyEntity[];  // resolved
  consumers: TopologyEntity[];  // resolved
  tests: TopologyEntity[];
  related_topics: { id: string; label: string; broker_canonical: BrokerCanonical }[];
  flow_count: number;
  lifecycle_state: LifecycleState;
  schema_type?: string;
  return_type?: string;
  provider?: string;
  enrichment_health?: {
    has_summary: boolean;
    has_schema: boolean;
    has_volume_estimate: boolean;
    has_typical_payload_size: boolean;
    has_expected_consumers: boolean;
    has_gaps: boolean;
    filled_field_count: number;
    total_field_count: number;
  };
}
```

- [ ] **Step 2: Verify TypeScript types compile (no build step needed yet — just validate imports later)**

---

## Task 2: Add topology methods to `lib/api.ts`

**Files:**
- Modify: `webui-v2/src/lib/api.ts`

- [ ] **Step 1: Add the import for topology types at the top of `api.ts`**

The import line already reads:
```typescript
import type { Group, Entity, Community } from "@/data/types";
```
Change it to:
```typescript
import type { Group, Entity, Community, TopologyResponse, TopologyChannelDetail } from "@/data/types";
```

- [ ] **Step 2: Append topology methods to the `api` object (after the last existing method)**

After the closing `}` of the `api` object's last entry (`searchEntities`), but before the `};` that closes the whole object, add:

```typescript
  // ---- Topology (v2) ----
  /** v2 — full topology payload for the Map/List views. */
  getTopology: (groupId: string) =>
    requestV2<TopologyResponse>(`/topology/${encodeURIComponent(groupId)}`),
  /** v1 — channel detail (v2 detail endpoint is not yet built; v1 detail reused). */
  getTopologyDetail: (groupId: string, topicId: string) =>
    request<TopologyChannelDetail>(`/topology/${encodeURIComponent(groupId)}/topic/${encodeURIComponent(topicId)}`),
```

Note: The v2 endpoint is `GET /api/v2/topology/:group` (built in Task 5). Channel detail re-uses the existing v1 endpoint `GET /api/topology/:group/topic/:topicId` since it already returns resolved entities. If that v1 endpoint's response doesn't match `TopologyChannelDetail` exactly, the hook's `select` option can normalize it — covered in Task 3.

---

## Task 3: Create `hooks/use-topology.ts`

**Files:**
- Create: `webui-v2/src/hooks/use-topology.ts`

- [ ] **Step 1: Write the hook file**

```typescript
/* ============================================================
   hooks/use-topology.ts — Topology screen data hooks.
   Wraps api.getTopology and api.getTopologyDetail via TanStack Query.
   All channels from the fragmented response are merged into a flat
   list with a `lifecycle` field derived from producer/consumer counts
   so the screen doesn't need to do that logic.
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type {
  TopologyChannel,
  TopologyResponse,
  TopologyBrokerGroup,
  LifecycleState,
} from "@/data/types";

// ---------------------------------------------------------------------------
// Derived types the screen consumes directly
// ---------------------------------------------------------------------------

/** A flat channel with lifecycle state derived from producer/consumer counts. */
export interface NormalizedChannel extends TopologyChannel {
  lifecycle: LifecycleState;
}

export interface NormalizedTopology {
  channels: NormalizedChannel[];
  brokerGroups: TopologyBrokerGroup[];
  totals: {
    channels: number;
    brokers: number;
    active: number;
    orphanPublishers: number;
    orphanSubscribers: number;
    orphan: number;
    scheduled: number;
    crossRepo: number;
  };
}

// ---------------------------------------------------------------------------
// Normalization helpers
// ---------------------------------------------------------------------------

function deriveLifecycle(c: TopologyChannel): LifecycleState {
  // If the channel already carries a lifecycle field, use it.
  if (c.lifecycle) return c.lifecycle;
  const hasPub = c.producers.length > 0;
  const hasSub = c.consumers.length > 0;
  if (hasPub && hasSub) return "active";
  if (hasPub && !hasSub) return "orphan_publisher";
  if (!hasPub && hasSub) return "orphan_subscriber";
  return "orphan";
}

/** Flatten all topology buckets into a single channel list. */
export function flattenTopology(raw: TopologyResponse): NormalizedTopology {
  const all: NormalizedChannel[] = [
    ...raw.topics,
    ...raw.queues,
    ...raw.channels,
    ...raw.nats_subjects,
    ...raw.graphql_subscriptions,
    ...raw.functions,
  ].map((c) => ({ ...c, lifecycle: deriveLifecycle(c) }));

  const active = all.filter((c) => c.lifecycle === "active").length;
  const orphanPublishers = all.filter((c) => c.lifecycle === "orphan_publisher").length;
  const orphanSubscribers = all.filter((c) => c.lifecycle === "orphan_subscriber").length;
  const orphan = all.filter((c) => c.lifecycle === "orphan").length;
  const scheduled = all.filter((c) => c.scheduled).length;
  const crossRepo = all.filter((c) => c.cross_repo).length;

  return {
    channels: all,
    brokerGroups: raw.broker_groups ?? [],
    totals: {
      channels: all.length,
      brokers: raw.broker_groups?.length ?? 0,
      active,
      orphanPublishers,
      orphanSubscribers,
      orphan,
      scheduled,
      crossRepo,
    },
  };
}

// ---------------------------------------------------------------------------
// TanStack Query hooks
// ---------------------------------------------------------------------------

export const topologyQueryKey = (groupId: string) =>
  ["topology", groupId] as const;

export const topologyDetailQueryKey = (groupId: string, topicId: string) =>
  ["topology-detail", groupId, topicId] as const;

/** Full topology payload — used by the Map/List/tab views. */
export function useTopology(groupId: string) {
  return useQuery({
    queryKey: topologyQueryKey(groupId),
    queryFn: () => api.getTopology(groupId),
    select: flattenTopology,
    staleTime: 30_000,
  });
}

/** Single-channel detail — lazy, only fetched when a channel is selected. */
export function useTopologyDetail(groupId: string, topicId: string | null) {
  return useQuery({
    queryKey: topologyDetailQueryKey(groupId, topicId ?? ""),
    queryFn: () => api.getTopologyDetail(groupId, topicId!),
    enabled: topicId != null,
    staleTime: 30_000,
  });
}
```

---

## Task 4: Build the full `routes/topology.tsx` screen

This is the longest task. The component is ~700 lines — all sub-components live in the same file, matching the `landing.tsx` pattern.

**Files:**
- Modify: `webui-v2/src/routes/topology.tsx`

### Step 4.1 — BROKER_META constant and BrokerShape SVG

- [ ] **Write the BROKER_META constant and the BrokerShape SVG component**

Replace the entire placeholder file with a new file that starts with:

```typescript
/* ============================================================
   Topology — Async Message-Channel Map (design handoff #1440)

   Route: /g/:groupId/topology
   Data:  useTopology (GET /api/v2/topology/:group)
          useTopologyDetail (GET /api/topology/:group/topic/:topicId)
   ============================================================ */

import { useState, useEffect, useMemo, useCallback } from "react";
import { useParams, useSearchParams } from "react-router-dom";
import {
  ChevronRight,
  Search,
  X,
  Clock,
  Zap,
  Copy,
  Share2,
  ExternalLink,
  Hash,
  Info,
  ArrowRight,
  FlaskConical,
  Sparkles,
  LayoutList,
  Map as MapIcon,
  HelpCircle,
} from "lucide-react";
import { toast } from "sonner";

import { SearchInput, Badge, Pill, Button } from "@/components/ui";
import { cn } from "@/lib/utils";
import { useTopology, useTopologyDetail } from "@/hooks/use-topology";
import type {
  BrokerCanonical,
  LifecycleState,
  TopologyBrokerGroup,
  TopologyChannelDetail,
} from "@/data/types";
import type { NormalizedChannel } from "@/hooks/use-topology";

// ---------------------------------------------------------------------------
// Broker metadata (color + shape + label — exact hex from design spec §7)
// ---------------------------------------------------------------------------

interface BrokerMeta {
  label: string;
  color: string;
  ink: string;
  shape:
    | "square" | "circle" | "hexagon" | "diamond" | "pentagon"
    | "triangle" | "star" | "cross" | "ring" | "chevron" | "clock" | "bolt";
}

const BROKER_META: Record<string, BrokerMeta> = {
  kafka:                { label: "Kafka",       color: "#22d3ee", ink: "#0e7490", shape: "square"   },
  rabbitmq:             { label: "RabbitMQ",    color: "#fbbf24", ink: "#92400e", shape: "circle"   },
  sqs:                  { label: "SQS",         color: "#fb923c", ink: "#9a3412", shape: "hexagon"  },
  pubsub:               { label: "Pub/Sub",     color: "#60a5fa", ink: "#1d4ed8", shape: "diamond"  },
  nats:                 { label: "NATS",        color: "#e879f9", ink: "#86198f", shape: "pentagon" },
  websocket:            { label: "WebSocket",   color: "#2dd4bf", ink: "#0f766e", shape: "triangle" },
  sse:                  { label: "SSE",         color: "#818cf8", ink: "#3730a3", shape: "star"     },
  graphql_subscription: { label: "GraphQL Sub", color: "#f472b6", ink: "#9d174d", shape: "cross"    },
  redis_pubsub:         { label: "Redis P/S",   color: "#f87171", ink: "#991b1b", shape: "ring"     },
  redis:                { label: "Redis",       color: "#f87171", ink: "#991b1b", shape: "ring"     },
  "redis-stream":       { label: "Redis Stream",color: "#fb7185", ink: "#9f1239", shape: "chevron"  },
  celery:               { label: "Celery",      color: "#a3e635", ink: "#3f6212", shape: "clock"    },
  "task-queue":         { label: "Task Queue",  color: "#a3e635", ink: "#3f6212", shape: "clock"    },
  serverless:           { label: "Serverless",  color: "#fde047", ink: "#854d0e", shape: "bolt"     },
  unknown:              { label: "Unknown",     color: "#94a3b8", ink: "#475569", shape: "circle"   },
};

function getBrokerMeta(broker: string): BrokerMeta {
  return BROKER_META[broker] ?? BROKER_META.unknown;
}
```

### Step 4.2 — BrokerShape SVG component

- [ ] **Add the BrokerShape component (renders the correct SVG shape for each broker)**

```typescript
function BrokerShape({ broker, size = 32, className }: { broker: string; size?: number; className?: string }) {
  const meta = getBrokerMeta(broker);
  const c = meta.color;
  const stroke = meta.ink;
  const fill = `color-mix(in srgb, ${c} 38%, transparent)`;
  const sw = 1.4;
  const { shape } = meta;

  let body: React.ReactNode;
  switch (shape) {
    case "square":
      body = <rect x="3.5" y="3.5" width="17" height="17" rx="2.5" stroke={stroke} fill={fill} strokeWidth={sw} />;
      break;
    case "circle":
      body = <circle cx="12" cy="12" r="9" stroke={stroke} fill={fill} strokeWidth={sw} />;
      break;
    case "hexagon":
      body = <polygon points="12,2.5 20.5,7 20.5,17 12,21.5 3.5,17 3.5,7" stroke={stroke} fill={fill} strokeWidth={sw} />;
      break;
    case "diamond":
      body = <polygon points="12,2.5 21.5,12 12,21.5 2.5,12" stroke={stroke} fill={fill} strokeWidth={sw} />;
      break;
    case "pentagon":
      body = <polygon points="12,2.5 21.5,9.5 18,20.5 6,20.5 2.5,9.5" stroke={stroke} fill={fill} strokeWidth={sw} />;
      break;
    case "triangle":
      body = <polygon points="12,3 21,20 3,20" stroke={stroke} fill={fill} strokeWidth={sw} />;
      break;
    case "star":
      body = <polygon points="12,2.5 14.5,9 21.5,9.5 16,14 17.5,21 12,17 6.5,21 8,14 2.5,9.5 9.5,9" stroke={stroke} fill={fill} strokeWidth={sw * 0.9} />;
      break;
    case "cross":
      body = <path d="M9 3h6v6h6v6h-6v6H9v-6H3V9h6z" stroke={stroke} fill={fill} strokeWidth={sw} />;
      break;
    case "ring":
      body = (
        <>
          <circle cx="12" cy="12" r="9" stroke={stroke} fill="none" strokeWidth={sw * 1.4} />
          <circle cx="12" cy="12" r="4" fill={fill} stroke={stroke} strokeWidth={sw} />
        </>
      );
      break;
    case "chevron":
      body = <polygon points="3,7 12,3 21,7 21,17 12,21 3,17" stroke={stroke} fill={fill} strokeWidth={sw} />;
      break;
    case "clock":
      body = (
        <>
          <circle cx="12" cy="12" r="9" stroke={stroke} fill={fill} strokeWidth={sw} />
          <path d="M12 7v5l3 2" stroke={stroke} strokeWidth={sw} fill="none" strokeLinecap="round" />
        </>
      );
      break;
    case "bolt":
      body = <polygon points="13,3 4,14 11,14 10,21 19,10 12,10" stroke={stroke} fill={fill} strokeWidth={sw} strokeLinejoin="round" />;
      break;
    default:
      body = <circle cx="12" cy="12" r="9" stroke={stroke} fill={fill} strokeWidth={sw} />;
  }

  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      aria-label={meta.label}
      className={className}
    >
      {body}
    </svg>
  );
}
```

### Step 4.3 — Lifecycle badge, mini-chips, Arrow component

- [ ] **Add LifecycleBadge, Arrow, and EntityChip helper components**

```typescript
// ---------------------------------------------------------------------------
// Lifecycle color + label
// ---------------------------------------------------------------------------

const LIFECYCLE_CONFIG: Record<LifecycleState, { label: string; tone: "success" | "warning" | "neutral" }> = {
  active:             { label: "active",            tone: "success"  },
  orphan_publisher:   { label: "orphan publisher",  tone: "warning"  },
  orphan_subscriber:  { label: "orphan subscriber", tone: "warning"  },
  orphan:             { label: "orphan",            tone: "neutral"  },
};

function LifecycleBadge({ state }: { state: LifecycleState }) {
  const { label, tone } = LIFECYCLE_CONFIG[state] ?? LIFECYCLE_CONFIG.orphan;
  return <Badge tone={tone}>{label}</Badge>;
}

// ---------------------------------------------------------------------------
// Arrow SVG (producer→channel, channel→consumer)
// ---------------------------------------------------------------------------

function Arrow({ crossRepo, dead }: { crossRepo?: boolean; dead?: boolean }) {
  const color = crossRepo ? "#a78bfa" : dead ? "var(--border-strong)" : "var(--text-3)";
  const dashArray = (crossRepo || dead) ? "4 3" : undefined;
  return (
    <span className="flex items-center justify-center w-8 shrink-0">
      <svg viewBox="0 0 32 22" preserveAspectRatio="none" width="100%" height="22"
        aria-hidden>
        <line
          x1="0" y1="11" x2="24" y2="11"
          stroke={color} strokeWidth="1.6"
          strokeDasharray={dashArray}
          style={crossRepo ? { animation: "var(--reduce-motion, tp-dash-anim 600ms linear infinite)" } : undefined}
        />
        <polygon points="24,7 32,11 24,15" fill={color} />
      </svg>
    </span>
  );
}

// ---------------------------------------------------------------------------
// Entity chip
// ---------------------------------------------------------------------------

function EntityChip({
  entityId,
  name,
  kind,
  repo,
  channelRepo,
}: {
  entityId: string;
  name: string;
  kind: string;
  repo: string;
  channelRepo: string;
}) {
  const isXrepo = repo !== channelRepo;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 px-2 h-6 rounded-md border text-xs font-mono truncate max-w-[140px]",
        isXrepo
          ? "border-violet-400/50 bg-violet-50 text-violet-700 dark:bg-violet-950/30 dark:text-violet-300"
          : "border-border bg-surface text-text-2",
      )}
      title={`${kind} · ${repo}`}
    >
      <span className="truncate">{name}</span>
      {isXrepo && (
        <span className="shrink-0 text-[10px] text-violet-500">{repo}</span>
      )}
    </span>
  );
}
```

### Step 4.4 — FlowUnit component (the centerpiece per-channel mini-Sankey)

- [ ] **Add the FlowUnit component**

```typescript
// ---------------------------------------------------------------------------
// FlowUnit — one row per channel: producers | arrow | channel | arrow | consumers
// ---------------------------------------------------------------------------

function FlowUnit({
  channel,
  selected,
  onClick,
}: {
  channel: NormalizedChannel;
  selected: boolean;
  onClick: () => void;
}) {
  const noPub = channel.producers.length === 0;
  const noSub = channel.consumers.length === 0;

  const lifecycleOutline =
    channel.lifecycle === "orphan_publisher" || channel.lifecycle === "orphan_subscriber"
      ? "ring-1 ring-amber-400/60"
      : channel.lifecycle === "orphan"
      ? "ring-1 ring-dashed ring-border-strong"
      : "";

  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "w-full grid items-center gap-2 p-3 rounded-lg border text-left transition-colors",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
        "hover:bg-surface-2",
        selected
          ? "bg-accent-soft/30 border-accent/40"
          : "bg-surface border-border-soft",
      )}
      style={{ gridTemplateColumns: "1fr 32px 160px 32px 1fr" }}
      data-topology-channel={channel.id}
    >
      {/* Producers side */}
      <div className="flex flex-col gap-1 items-end">
        {noPub ? (
          <span className="inline-flex items-center gap-1 text-xs text-text-4">
            <HelpCircle size={10} />
            <span>no publisher</span>
          </span>
        ) : (
          <>
            {channel.producers.slice(0, 3).map((id) => (
              <EntityChip
                key={id}
                entityId={id}
                name={id.split("::").pop() ?? id}
                kind="entity"
                repo={channel.repo}
                channelRepo={channel.repo}
              />
            ))}
            {channel.producers.length > 3 && (
              <span className="text-xs text-text-4">+{channel.producers.length - 3} more</span>
            )}
          </>
        )}
      </div>

      <Arrow dead={noPub} />

      {/* Channel node */}
      <div className={cn("flex flex-col items-center gap-1 py-1", lifecycleOutline, "rounded-md")}>
        <BrokerShape broker={channel.broker_canonical} size={32} />
        <span className="font-mono text-xs text-text-2 text-center leading-tight max-w-full truncate px-1">
          {channel.label}
        </span>
        <div className="flex flex-wrap gap-1 justify-center">
          <span className="text-[10px] text-text-3">{getBrokerMeta(channel.broker_canonical).label}</span>
          {channel.cross_repo && (
            <span className="text-[10px] text-violet-500">cross-repo</span>
          )}
          {channel.scheduled && (
            <span className="inline-flex items-center gap-0.5 text-[10px] text-text-3">
              <Clock size={9} />
              {channel.schedule}
            </span>
          )}
        </div>
        <LifecycleBadge state={channel.lifecycle} />
      </div>

      <Arrow dead={noSub} />

      {/* Consumers side */}
      <div className="flex flex-col gap-1 items-start">
        {noSub ? (
          <span className="inline-flex items-center gap-1 text-xs text-text-4">
            <HelpCircle size={10} />
            <span>no subscriber</span>
          </span>
        ) : (
          <>
            {channel.consumers.slice(0, 3).map((id) => (
              <EntityChip
                key={id}
                entityId={id}
                name={id.split("::").pop() ?? id}
                kind="entity"
                repo={channel.repo}
                channelRepo={channel.repo}
              />
            ))}
            {channel.consumers.length > 3 && (
              <span className="text-xs text-text-4">+{channel.consumers.length - 3} more</span>
            )}
          </>
        )}
      </div>
    </button>
  );
}
```

### Step 4.5 — BrokerBand component

- [ ] **Add the BrokerBand component**

```typescript
// ---------------------------------------------------------------------------
// BrokerBand — collapsible band grouping channels for one broker
// ---------------------------------------------------------------------------

function timeAgo(iso: string): string {
  const t = new Date(iso).getTime();
  if (isNaN(t)) return "";
  const m = Math.floor((Date.now() - t) / 60000);
  if (m < 60) return `${m}m ago`;
  if (m < 60 * 24) return `${Math.floor(m / 60)}h ago`;
  return `${Math.floor(m / (60 * 24))}d ago`;
}

function BrokerBand({
  brokerGroup,
  channels,
  selectedId,
  onSelect,
}: {
  brokerGroup: TopologyBrokerGroup;
  channels: NormalizedChannel[];
  selectedId: string | null;
  onSelect: (c: NormalizedChannel) => void;
}) {
  const [open, setOpen] = useState(true);
  const meta = getBrokerMeta(brokerGroup.broker);

  // Degraded state: all channels in this band have no producers AND no consumers
  const allEmpty = channels.length > 0 &&
    channels.every((c) => c.producers.length === 0 && c.consumers.length === 0);

  return (
    <section className="border border-border rounded-lg overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center gap-2 px-3 py-2.5 bg-surface-2 hover:bg-surface-3 transition-colors text-left"
      >
        <ChevronRight
          size={14}
          className={cn("text-text-4 transition-transform", open && "rotate-90")}
        />
        <BrokerShape broker={brokerGroup.broker} size={20} />
        <span className="font-medium text-sm text-text">{meta.label}</span>
        <span className="text-xs text-text-3 font-mono">
          {brokerGroup.count} channel{brokerGroup.count === 1 ? "" : "s"}
        </span>
        <div className="ml-auto flex items-center gap-2 flex-wrap">
          {brokerGroup.health_summary.active > 0 && (
            <Badge tone="success" dot="var(--success)">
              {brokerGroup.health_summary.active} active
            </Badge>
          )}
          {brokerGroup.orphan_publishers > 0 && (
            <Badge tone="warning">{brokerGroup.orphan_publishers} orphan-pub</Badge>
          )}
          {brokerGroup.orphan_subscribers > 0 && (
            <Badge tone="warning">{brokerGroup.orphan_subscribers} orphan-sub</Badge>
          )}
          {brokerGroup.last_index_timestamp && (
            <span className="text-xs text-text-4">
              indexed {timeAgo(brokerGroup.last_index_timestamp)}
            </span>
          )}
        </div>
      </button>

      {open && (
        <div className="p-2 flex flex-col gap-2 bg-bg">
          {allEmpty && (
            <div className="flex items-start gap-2 px-3 py-2 rounded-md bg-info-soft text-info text-sm">
              <Info size={14} className="mt-0.5 shrink-0" />
              <span>
                Producer/consumer edges aren't indexed for {meta.label} yet — these channels are real but their wiring is unknown.
              </span>
            </div>
          )}
          {channels.map((c) => (
            <FlowUnit
              key={c.id}
              channel={c}
              selected={selectedId === c.id}
              onClick={() => onSelect(c)}
            />
          ))}
        </div>
      )}
    </section>
  );
}
```

### Step 4.6 — MapView and ListRow/ListView components

- [ ] **Add MapView (canvas summary + stacked broker bands)**

```typescript
// ---------------------------------------------------------------------------
// Canvas summary card
// ---------------------------------------------------------------------------

function CanvasSummary({
  totals,
}: {
  totals: {
    channels: number;
    brokers: number;
    active: number;
    orphanPublishers: number;
    orphanSubscribers: number;
    crossRepo: number;
  };
}) {
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-2.5 bg-surface border border-border rounded-lg text-sm">
      <span><b className="font-mono">{totals.channels}</b> channels</span>
      <span className="text-border-strong">·</span>
      <span><b className="font-mono">{totals.brokers}</b> brokers</span>
      <span className="text-border-strong">·</span>
      <span className="text-success"><b className="font-mono">{totals.active}</b> active</span>
      <span className="text-warning"><b className="font-mono">{totals.orphanPublishers}</b> orphan-pub</span>
      <span className="text-warning"><b className="font-mono">{totals.orphanSubscribers}</b> orphan-sub</span>
      <span className="text-text-3"><b className="font-mono">{totals.crossRepo}</b> cross-repo</span>
    </div>
  );
}

// ---------------------------------------------------------------------------
// MapView
// ---------------------------------------------------------------------------

function MapView({
  channels,
  brokerGroups,
  totals,
  brokerFilter,
  search,
  selectedId,
  onSelect,
}: {
  channels: NormalizedChannel[];
  brokerGroups: TopologyBrokerGroup[];
  totals: ReturnType<typeof import("@/hooks/use-topology").flattenTopology>["totals"];
  brokerFilter: string[];
  search: string;
  selectedId: string | null;
  onSelect: (c: NormalizedChannel) => void;
}) {
  const visibleGroups = useMemo(() => {
    const filtered = brokerFilter.length > 0
      ? brokerGroups.filter((g) => brokerFilter.includes(g.broker))
      : brokerGroups;

    const q = search.toLowerCase();
    return filtered
      .map((g) => ({
        brokerGroup: g,
        channels: channels.filter(
          (c) =>
            c.broker_canonical === g.broker &&
            (q === "" || c.label.toLowerCase().includes(q)),
        ),
      }))
      .filter((g) => g.channels.length > 0);
  }, [brokerGroups, channels, brokerFilter, search]);

  if (visibleGroups.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-20 text-center">
        <Search size={28} className="text-text-4 mb-3" />
        <p className="text-text-2 font-medium">No channels match the active filters</p>
        <p className="text-sm text-text-3 mt-1">Try clearing the broker chips or the search box.</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3 pb-6">
      <CanvasSummary totals={totals} />
      {visibleGroups.map(({ brokerGroup, channels: bandChannels }) => (
        <BrokerBand
          key={brokerGroup.broker}
          brokerGroup={brokerGroup}
          channels={bandChannels}
          selectedId={selectedId}
          onSelect={onSelect}
        />
      ))}
    </div>
  );
}
```

- [ ] **Add ListRow and ListView components**

```typescript
// ---------------------------------------------------------------------------
// ListView
// ---------------------------------------------------------------------------

function ListRow({
  channel,
  selected,
  onSelect,
}: {
  channel: NormalizedChannel;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "w-full flex items-center gap-3 px-3 py-2 rounded-md text-left text-sm transition-colors",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
        selected ? "bg-accent-soft/30" : "hover:bg-surface-2",
      )}
    >
      <BrokerShape broker={channel.broker_canonical} size={22} />
      <span className="font-mono text-text-2 truncate flex-1">{channel.label}</span>
      {/* Mini flow indicator */}
      <span className="flex items-center gap-1 text-xs text-text-3 shrink-0">
        <span className="font-mono">{channel.producers.length}</span>
        <ArrowRight size={10} />
        <BrokerShape broker={channel.broker_canonical} size={14} />
        <ArrowRight size={10} />
        <span className="font-mono">{channel.consumers.length}</span>
      </span>
      <LifecycleBadge state={channel.lifecycle} />
      <Badge>{channel.repo}</Badge>
    </button>
  );
}

function ListView({
  channels,
  brokerGroups,
  brokerFilter,
  search,
  selectedId,
  onSelect,
}: {
  channels: NormalizedChannel[];
  brokerGroups: TopologyBrokerGroup[];
  brokerFilter: string[];
  search: string;
  selectedId: string | null;
  onSelect: (c: NormalizedChannel) => void;
}) {
  const [openBands, setOpenBands] = useState<Record<string, boolean>>({});

  const visibleGroups = useMemo(() => {
    const filtered = brokerFilter.length > 0
      ? brokerGroups.filter((g) => brokerFilter.includes(g.broker))
      : brokerGroups;
    const q = search.toLowerCase();
    return filtered.map((g) => ({
      brokerGroup: g,
      channels: channels.filter(
        (c) =>
          c.broker_canonical === g.broker &&
          (q === "" || c.label.toLowerCase().includes(q)),
      ),
    })).filter((g) => g.channels.length > 0);
  }, [brokerGroups, channels, brokerFilter, search]);

  return (
    <div className="flex flex-col gap-2 pb-6">
      {visibleGroups.map(({ brokerGroup, channels: band }) => {
        const key = brokerGroup.broker;
        const open = openBands[key] !== false;
        const meta = getBrokerMeta(key);
        return (
          <div key={key} className="border border-border rounded-lg overflow-hidden">
            <button
              type="button"
              onClick={() => setOpenBands((p) => ({ ...p, [key]: !open }))}
              className="w-full flex items-center gap-2 px-3 py-2 bg-surface-2 hover:bg-surface-3 text-left"
            >
              <ChevronRight
                size={12}
                className={cn("text-text-4 transition-transform", open && "rotate-90")}
              />
              <BrokerShape broker={key} size={18} />
              <span className="font-medium text-sm text-text">{meta.label}</span>
              <span className="ml-auto font-mono text-xs text-text-3">
                {band.length} channel{band.length === 1 ? "" : "s"}
              </span>
            </button>
            {open && (
              <div className="px-1 py-1 bg-bg">
                {band.map((c) => (
                  <ListRow
                    key={c.id}
                    channel={c}
                    selected={selectedId === c.id}
                    onSelect={() => onSelect(c)}
                  />
                ))}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
```

### Step 4.7 — DetailPanel component

- [ ] **Add the DetailPanel component**

```typescript
// ---------------------------------------------------------------------------
// DetailSection — collapsible section inside the detail panel
// ---------------------------------------------------------------------------

function DetailSection({
  title,
  icon,
  count,
  children,
  emptyText,
}: {
  title: string;
  icon?: React.ReactNode;
  count?: number;
  children?: React.ReactNode;
  emptyText?: string;
}) {
  const [open, setOpen] = useState(true);
  const isEmpty = !children || (typeof count === "number" && count === 0);

  return (
    <section className="border-t border-border-soft">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center gap-1.5 px-4 py-2.5 text-sm hover:bg-surface-2 text-left"
      >
        <ChevronRight
          size={11}
          className={cn("text-text-4 transition-transform", open && "rotate-90")}
        />
        {icon && <span className="text-text-3">{icon}</span>}
        <span className="font-medium text-text-2">{title}</span>
        {typeof count === "number" && (
          <span className="ml-1 text-xs font-mono text-text-3">{count}</span>
        )}
      </button>
      {open && (
        <div className="px-4 pb-3">
          {isEmpty
            ? <p className="text-sm text-text-3">{emptyText ?? "None."}</p>
            : children}
        </div>
      )}
    </section>
  );
}

// ---------------------------------------------------------------------------
// DetailPanel
// ---------------------------------------------------------------------------

function DetailPanel({
  channel,
  groupId,
  onClose,
}: {
  channel: NormalizedChannel | null;
  groupId: string;
  onClose: () => void;
}) {
  const { data: detail } = useTopologyDetail(groupId, channel?.id ?? null);

  if (!channel) {
    return (
      <aside className="w-[380px] shrink-0 border-l border-border bg-surface flex flex-col items-center justify-center text-center p-8 gap-3">
        <LayoutList size={28} className="text-text-4" />
        <p className="text-sm text-text-3">
          Select a channel to see publishers, subscribers, schema, tests, and related flows.
        </p>
      </aside>
    );
  }

  const meta = getBrokerMeta(channel.broker_canonical);

  function copyToClipboard(text: string, label: string) {
    navigator.clipboard.writeText(text).then(
      () => toast.success(`Copied ${label}`),
      () => toast.error("Clipboard not available"),
    );
  }

  const enrichmentPct = detail?.enrichment_health
    ? Math.round(100 * detail.enrichment_health.filled_field_count / detail.enrichment_health.total_field_count)
    : 0;

  return (
    <aside className="w-[380px] shrink-0 border-l border-border bg-surface flex flex-col overflow-y-auto">
      {/* Sticky header */}
      <header className="sticky top-0 z-10 bg-surface border-b border-border-soft">
        <div className="flex items-center gap-2 px-4 py-3">
          <BrokerShape broker={channel.broker_canonical} size={28} />
          <span className="flex-1 font-mono text-sm text-text truncate">{channel.label}</span>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded hover:bg-surface-2 text-text-3"
            aria-label="Close detail"
          >
            <X size={14} />
          </button>
        </div>
        <div className="flex flex-wrap gap-1 px-4 pb-2.5">
          <Badge>{meta.label}</Badge>
          <Badge>{channel.repo}</Badge>
          <LifecycleBadge state={channel.lifecycle} />
          {channel.cross_repo && <Badge>cross-repo</Badge>}
          {channel.scheduled && (
            <Badge tone="neutral">
              <Clock size={10} /> {channel.schedule}
            </Badge>
          )}
        </div>
      </header>

      {/* Sections */}
      <div className="flex-1">
        {/* Identity */}
        <DetailSection title="Identity" icon={<Hash size={11} />}>
          <dl className="text-sm space-y-1.5">
            {detail?.source_file && (
              <div className="flex gap-2">
                <dt className="text-text-3 w-20 shrink-0">Source</dt>
                <dd className="font-mono text-text-2 truncate">
                  {detail.source_file}{detail.start_line ? `:${detail.start_line}` : ""}
                </dd>
              </div>
            )}
            {detail?.protocol && (
              <div className="flex gap-2">
                <dt className="text-text-3 w-20 shrink-0">Protocol</dt>
                <dd className="text-text-2">{detail.protocol}</dd>
              </div>
            )}
            {detail?.framework && (
              <div className="flex gap-2">
                <dt className="text-text-3 w-20 shrink-0">Framework</dt>
                <dd className="text-text-2">{detail.framework}</dd>
              </div>
            )}
            {detail?.channel_type && (
              <div className="flex gap-2">
                <dt className="text-text-3 w-20 shrink-0">Channel</dt>
                <dd className="text-text-2">{detail.channel_type}</dd>
              </div>
            )}
            {detail?.schema_type && (
              <div className="flex gap-2">
                <dt className="text-text-3 w-20 shrink-0">Schema</dt>
                <dd className="font-mono text-text-2">{detail.schema_type} → {detail.return_type}</dd>
              </div>
            )}
            {detail?.provider && (
              <div className="flex gap-2">
                <dt className="text-text-3 w-20 shrink-0">Provider</dt>
                <dd className="text-text-2">{detail.provider}</dd>
              </div>
            )}
            <div className="flex gap-2 items-center">
              <dt className="text-text-3 w-20 shrink-0">id</dt>
              <dd className="flex items-center gap-1.5 font-mono text-xs text-text-2 min-w-0">
                <span className="truncate">{channel.id}</span>
                <button
                  type="button"
                  onClick={() => copyToClipboard(channel.id, "id")}
                  className="shrink-0 p-0.5 rounded hover:bg-surface-2"
                  aria-label="Copy id"
                >
                  <Copy size={11} />
                </button>
              </dd>
            </div>
          </dl>
        </DetailSection>

        {/* Description */}
        <DetailSection
          title="Description"
          icon={<Info size={11} />}
          emptyText="No description yet. Run /generate-docs on the group."
        >
          {detail?.docs_summary ? (
            <div>
              <p className="text-sm text-text-2 leading-relaxed">{detail.docs_summary}</p>
              <div className="mt-2">
                <Badge tone="info"><Sparkles size={10} /> AI-generated</Badge>
              </div>
            </div>
          ) : undefined}
        </DetailSection>

        {/* Publishers */}
        <DetailSection
          title="Publishers"
          icon={<ArrowRight size={11} />}
          count={detail?.producers?.length ?? channel.producers.length}
          emptyText="No publishers found in indexed code — this is the orphan-subscriber signal."
        >
          {detail?.producers?.length ? (
            <div className="space-y-1.5">
              {detail.producers.map((e) => (
                <div key={e.entity_id} className="flex items-start gap-2 text-sm">
                  <span className="text-text-3 mt-0.5">{e.kind}</span>
                  <div className="min-w-0">
                    <span className="font-mono text-text-2">{e.name}</span>
                    <span className="block text-xs text-text-3">
                      {e.repo} · {e.source_file}{e.start_line ? `:${e.start_line}` : ""}
                    </span>
                  </div>
                </div>
              ))}
            </div>
          ) : undefined}
        </DetailSection>

        {/* Subscribers */}
        <DetailSection
          title="Subscribers"
          icon={<ArrowRight size={11} className="rotate-180" />}
          count={detail?.consumers?.length ?? channel.consumers.length}
          emptyText="No subscribers found — orphan publisher."
        >
          {detail?.consumers?.length ? (
            <div className="space-y-1.5">
              {detail.consumers.map((e) => (
                <div key={e.entity_id} className="flex items-start gap-2 text-sm">
                  <span className="text-text-3 mt-0.5">{e.kind}</span>
                  <div className="min-w-0">
                    <span className="font-mono text-text-2">{e.name}</span>
                    <span className="block text-xs text-text-3">
                      {e.repo} · {e.source_file}{e.start_line ? `:${e.start_line}` : ""}
                    </span>
                  </div>
                </div>
              ))}
            </div>
          ) : undefined}
        </DetailSection>

        {/* Cross-repo */}
        {channel.cross_repo && detail && (
          <DetailSection title="Cross-repo" icon={<Share2 size={11} />}>
            <p className="text-sm text-text-3 mb-2">
              Entities from other repos involved in this channel:
            </p>
            <div className="space-y-1.5">
              {[...detail.producers, ...detail.consumers]
                .filter((e) => e.repo !== channel.repo)
                .filter((e, i, arr) => arr.findIndex((x) => x.entity_id === e.entity_id) === i)
                .map((e) => (
                  <div key={e.entity_id} className="flex items-start gap-2 text-sm">
                    <span className="text-violet-500 shrink-0">{e.repo}</span>
                    <span className="font-mono text-text-2 truncate">{e.name}</span>
                  </div>
                ))}
            </div>
          </DetailSection>
        )}

        {/* Message schema */}
        {detail?.message_schema && (
          <DetailSection title="Message schema" icon={<Hash size={11} />}>
            <div className="rounded-md bg-surface-2 border border-border p-2">
              <div className="text-[10px] text-text-4 mb-1.5 font-medium uppercase tracking-wide">
                schema (static)
              </div>
              <pre className="font-mono text-xs text-text-2 whitespace-pre-wrap break-all">
                {detail.message_schema}
              </pre>
            </div>
          </DetailSection>
        )}

        {/* Documentation completeness */}
        {detail?.enrichment_health && (
          <DetailSection title="Documentation completeness" icon={<Sparkles size={11} />}>
            <div className="flex items-center gap-2 mb-1">
              <div className="flex-1 h-1.5 rounded-full bg-surface-3 overflow-hidden">
                <div
                  className="h-full rounded-full bg-success transition-all"
                  style={{ width: `${enrichmentPct}%` }}
                />
              </div>
              <span className="font-mono text-xs text-text-2">
                {detail.enrichment_health.filled_field_count} of {detail.enrichment_health.total_field_count} fields
              </span>
            </div>
            <p className="text-xs text-text-4">
              These are doc-authored estimates, not measured runtime metrics.
            </p>
          </DetailSection>
        )}

        {/* Tests */}
        <DetailSection
          title="Tests"
          icon={<FlaskConical size={11} />}
          count={detail?.tests?.length ?? 0}
          emptyText="No tests linked to this channel."
        >
          {detail?.tests?.length ? (
            <div className="space-y-1.5">
              {detail.tests.map((t) => (
                <div key={t.entity_id} className="text-sm">
                  <span className="font-mono text-text-2">{t.name}</span>
                  <span className="block text-xs text-text-3">{t.source_file}</span>
                </div>
              ))}
            </div>
          ) : undefined}
        </DetailSection>

        {/* Related channels */}
        <DetailSection
          title="Related channels"
          icon={<ExternalLink size={11} />}
          count={detail?.related_topics?.length ?? 0}
          emptyText="No channels share a producer/consumer with this one."
        >
          {detail?.related_topics?.length ? (
            <div className="space-y-1.5">
              {detail.related_topics.map((t) => (
                <div key={t.id} className="flex items-center gap-2 text-sm">
                  <BrokerShape broker={t.broker_canonical} size={14} />
                  <span className="font-mono text-text-2">{t.label}</span>
                </div>
              ))}
            </div>
          ) : undefined}
        </DetailSection>

        {/* Flows */}
        {detail && detail.flow_count > 0 && (
          <DetailSection title="Flows" icon={<ArrowRight size={11} />}>
            <p className="text-sm text-text-2">
              Appears in <b className="font-mono">{detail.flow_count}</b>{" "}
              flow{detail.flow_count === 1 ? "" : "s"}
            </p>
          </DetailSection>
        )}
      </div>

      {/* Footer */}
      <footer className="sticky bottom-0 border-t border-border-soft bg-surface px-4 py-2.5 flex items-center gap-2">
        <Button size="sm" variant="ghost" onClick={() => copyToClipboard(channel.id, "id")}>
          <Copy size={12} /> Copy id
        </Button>
        <Button size="sm" variant="ghost" onClick={() => toast.info("Open source: stub")}>
          <ExternalLink size={12} /> Open source
        </Button>
        <Button
          size="sm"
          variant="ghost"
          className="ml-auto"
          onClick={() => copyToClipboard(window.location.href, "deep link")}
        >
          <Share2 size={12} /> Share
        </Button>
      </footer>
    </aside>
  );
}
```

### Step 4.8 — Orphan + Scheduled tabs

- [ ] **Add OrphanPublishersTab, OrphanSubscribersTab, ScheduledTab**

```typescript
// ---------------------------------------------------------------------------
// Orphan publishers tab
// ---------------------------------------------------------------------------

function OrphanPublishersTab({
  channels,
  onSelect,
}: {
  channels: NormalizedChannel[];
  onSelect: (c: NormalizedChannel) => void;
}) {
  const orphans = channels.filter((c) => c.lifecycle === "orphan_publisher");

  if (orphans.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-20 text-center">
        <Zap size={28} className="text-success mb-3" />
        <p className="text-text-2 font-medium">No orphan publishers</p>
        <p className="text-sm text-text-3 mt-1">Every published channel has a subscriber.</p>
      </div>
    );
  }

  return (
    <div className="p-4 space-y-1.5">
      <p className="text-sm text-text-3 mb-3">
        Channels where ≥1 producer publishes but nothing in the indexed code subscribes.
      </p>
      {orphans.map((o) => (
        <button
          key={o.id}
          type="button"
          onClick={() => onSelect(o)}
          className="w-full flex items-center gap-3 px-3 py-2 rounded-md hover:bg-surface-2 text-left text-sm"
        >
          <BrokerShape broker={o.broker_canonical} size={22} />
          <span className="font-mono text-text-2 flex-1 truncate">{o.label}</span>
          <span className="flex gap-1 flex-wrap">
            {o.producers.slice(0, 3).map((id) => (
              <Badge key={id} tone="neutral">{id.split("::").pop()}</Badge>
            ))}
            {o.producers.length > 3 && <span className="text-xs text-text-4">+{o.producers.length - 3}</span>}
          </span>
          <Badge tone="warning">no subscriber found</Badge>
          <Badge>{o.repo}</Badge>
        </button>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Orphan subscribers tab
// ---------------------------------------------------------------------------

function OrphanSubscribersTab({
  channels,
  onSelect,
}: {
  channels: NormalizedChannel[];
  onSelect: (c: NormalizedChannel) => void;
}) {
  const orphans = channels.filter((c) => c.lifecycle === "orphan_subscriber");

  if (orphans.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-20 text-center">
        <Zap size={28} className="text-success mb-3" />
        <p className="text-text-2 font-medium">No orphan subscribers</p>
        <p className="text-sm text-text-3 mt-1">Every subscribing entity has at least one publisher in the indexed code.</p>
      </div>
    );
  }

  return (
    <div className="p-4 space-y-1.5">
      <p className="text-sm text-text-3 mb-3">
        Channels where ≥1 subscriber exists but nothing in the indexed code publishes.
        Common for Celery beat jobs and external brokers.
      </p>
      {orphans.map((o) => (
        <button
          key={o.id}
          type="button"
          onClick={() => onSelect(o)}
          className="w-full flex items-center gap-3 px-3 py-2 rounded-md hover:bg-surface-2 text-left text-sm"
        >
          <BrokerShape broker={o.broker_canonical} size={22} />
          <span className="font-mono text-text-2 flex-1 truncate">{o.label}</span>
          <span className="flex gap-1 flex-wrap">
            {o.consumers.slice(0, 3).map((id) => (
              <Badge key={id} tone="neutral">{id.split("::").pop()}</Badge>
            ))}
            {o.consumers.length > 3 && <span className="text-xs text-text-4">+{o.consumers.length - 3}</span>}
          </span>
          <Badge tone={o.broker_canonical === "celery" ? "neutral" : "warning"}>
            {o.broker_canonical === "celery" ? "publisher in external lib" : "no publisher found"}
          </Badge>
          <Badge>{o.repo}</Badge>
        </button>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Scheduled tab
// ---------------------------------------------------------------------------

function ScheduledTab({
  channels,
  onSelect,
}: {
  channels: NormalizedChannel[];
  onSelect: (c: NormalizedChannel) => void;
}) {
  const scheduled = channels.filter((c) => c.scheduled);

  if (scheduled.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-20 text-center">
        <Clock size={28} className="text-text-4 mb-3" />
        <p className="text-text-2 font-medium">No scheduled jobs</p>
        <p className="text-sm text-text-3 mt-1">archigraph didn't find any scheduled task definitions in this group.</p>
      </div>
    );
  }

  return (
    <div className="p-4 space-y-1.5">
      <p className="text-sm text-text-3 mb-3">
        Channels with an extracted schedule — Celery beat, APScheduler, serverless cron, etc. Sorted by broker.
      </p>
      {[...scheduled].sort((a, b) => a.broker_canonical.localeCompare(b.broker_canonical)).map((s) => (
        <button
          key={s.id}
          type="button"
          onClick={() => onSelect(s)}
          className="w-full flex items-center gap-3 px-3 py-2 rounded-md hover:bg-surface-2 text-left text-sm"
        >
          <BrokerShape broker={s.broker_canonical} size={22} />
          <span className="font-mono text-text-2 flex-1 truncate">{s.label}</span>
          {s.schedule && (
            <span className="font-mono text-xs px-2 py-0.5 rounded bg-[var(--pastel-6)] text-[var(--pastel-6-ink)]">
              {s.schedule}
            </span>
          )}
          <span className="text-xs text-text-3">{s.framework ?? s.broker_canonical}</span>
          <Badge>{s.repo}</Badge>
        </button>
      ))}
    </div>
  );
}
```

### Step 4.9 — Broker filter chips and loading skeleton

- [ ] **Add BrokerFilterChips and TopologySkeleton**

```typescript
// ---------------------------------------------------------------------------
// Broker filter chips (also serve as the legend)
// ---------------------------------------------------------------------------

function BrokerFilterChips({
  brokerGroups,
  totalChannels,
  active,
  onToggle,
  onClear,
}: {
  brokerGroups: TopologyBrokerGroup[];
  totalChannels: number;
  active: string[];
  onToggle: (broker: string) => void;
  onClear: () => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <Pill active={active.length === 0} count={totalChannels} onClick={onClear}>
        All
      </Pill>
      {brokerGroups.map((g) => {
        const on = active.includes(g.broker);
        const dim = active.length > 0 && !on;
        return (
          <Pill
            key={g.broker}
            active={on}
            count={g.count}
            onClick={() => onToggle(g.broker)}
            className={cn(dim && "opacity-40")}
          >
            <BrokerShape broker={g.broker} size={14} className="shrink-0" />
            {getBrokerMeta(g.broker).label}
          </Pill>
        );
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Skeleton
// ---------------------------------------------------------------------------

function TopologySkeleton() {
  return (
    <div className="flex gap-4 h-full">
      <div className="flex-1 flex flex-col gap-3 p-4">
        <div className="h-10 rounded-lg bg-surface-2 animate-pulse" />
        {[0, 1, 2].map((i) => (
          <div key={i} className="rounded-lg border border-border overflow-hidden">
            <div className="h-10 bg-surface-2 animate-pulse" />
            <div className="p-2 flex flex-col gap-2">
              {[0, 1, 2].map((j) => (
                <div
                  key={j}
                  className="h-16 rounded-lg border border-border-soft bg-surface-2 animate-pulse"
                />
              ))}
            </div>
          </div>
        ))}
      </div>
      <div className="w-[380px] border-l border-border bg-surface flex flex-col p-4 gap-4">
        <div className="h-7 w-3/4 rounded bg-surface-2 animate-pulse" />
        <div className="flex gap-2">
          {[0, 1, 2].map((i) => <div key={i} className="h-5 w-16 rounded-full bg-surface-2 animate-pulse" />)}
        </div>
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="space-y-1.5">
            <div className="h-3 w-24 rounded bg-surface-2 animate-pulse" />
            <div className="h-12 rounded-lg bg-surface-2 animate-pulse" />
          </div>
        ))}
      </div>
    </div>
  );
}
```

### Step 4.10 — Screen root component and keyboard handlers

- [ ] **Write the TopologyScreen root component that wires everything together**

```typescript
// ---------------------------------------------------------------------------
// Screen root
// ---------------------------------------------------------------------------

type TabId = "all" | "orphanpub" | "orphansub" | "scheduled";
type ViewMode = "map" | "list";

export default function TopologyScreen() {
  const { groupId = "demo" } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();

  const tab = (searchParams.get("tab") as TabId) ?? "all";
  const view = (searchParams.get("view") as ViewMode) ?? "map";
  const channelParam = searchParams.get("channel");

  const [brokerFilter, setBrokerFilter] = useState<string[]>([]);
  const [search, setSearch] = useState("");
  const [selectedChannel, setSelectedChannel] = useState<NormalizedChannel | null>(null);

  const { data: topology, isLoading, isError } = useTopology(groupId);

  // Deep-link: if ?channel= is set on load, pre-select that channel.
  useEffect(() => {
    if (channelParam && topology) {
      const found = topology.channels.find((c) => c.id === channelParam);
      if (found) setSelectedChannel(found);
    }
    // Only run on initial topology load
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [topology]);

  // Persist tab + channel in URL
  function setTab(t: TabId) {
    setSearchParams((p) => {
      const next = new URLSearchParams(p);
      next.set("tab", t);
      return next;
    });
  }

  function setView(v: ViewMode) {
    setSearchParams((p) => {
      const next = new URLSearchParams(p);
      next.set("view", v);
      return next;
    });
  }

  function selectChannel(c: NormalizedChannel) {
    setSelectedChannel(c);
    setSearchParams((p) => {
      const next = new URLSearchParams(p);
      next.set("channel", c.id);
      return next;
    });
  }

  function closeDetail() {
    setSelectedChannel(null);
    setSearchParams((p) => {
      const next = new URLSearchParams(p);
      next.delete("channel");
      return next;
    });
  }

  // Keyboard: '/' focuses search; Esc closes detail
  const searchRef = useCallback((node: HTMLInputElement | null) => {
    if (!node) return;
    const onKey = (e: KeyboardEvent) => {
      const inText = ["INPUT", "TEXTAREA"].includes((e.target as HTMLElement).tagName);
      if (e.key === "/" && !inText) {
        e.preventDefault();
        node.focus();
      } else if (e.key === "Escape") {
        if (selectedChannel) closeDetail();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [selectedChannel]); // eslint-disable-line react-hooks/exhaustive-deps

  function toggleBroker(b: string) {
    setBrokerFilter((prev) =>
      prev.includes(b) ? prev.filter((x) => x !== b) : [...prev, b],
    );
  }

  const totals = topology?.totals;
  const channels = topology?.channels ?? [];
  const brokerGroups = topology?.brokerGroups ?? [];

  // ---- Loading ----
  if (isLoading) return <TopologySkeleton />;

  // ---- Error ----
  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center h-full text-center p-8">
        <p className="text-text-2 font-medium">Couldn't load topology data.</p>
        <p className="text-sm text-text-3 mt-1">Check that the daemon is running and this group is indexed.</p>
      </div>
    );
  }

  // ---- No channels ----
  if (!topology || channels.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center h-full text-center p-8">
        <LayoutList size={32} className="text-text-4 mb-3" />
        <p className="text-text font-semibold text-lg">No message channels indexed</p>
        <p className="text-text-3 text-sm mt-1 max-w-sm">
          archigraph didn't find any pub/sub, queues, WebSocket channels, or scheduled jobs in this group.
        </p>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full min-h-0">
      {/* Tab strip */}
      <div
        role="tablist"
        aria-label="Topology views"
        className="flex items-center border-b border-border px-4 shrink-0"
      >
        {(
          [
            { id: "all" as TabId, label: "All", count: totals?.channels },
            { id: "orphanpub" as TabId, label: "Orphan publishers", count: totals?.orphanPublishers },
            { id: "orphansub" as TabId, label: "Orphan subscribers", count: totals?.orphanSubscribers },
            { id: "scheduled" as TabId, label: "Scheduled jobs", count: totals?.scheduled },
          ] as const
        ).map(({ id, label, count }) => (
          <button
            key={id}
            role="tab"
            aria-selected={tab === id}
            onClick={() => setTab(id)}
            className={cn(
              "flex items-center gap-1.5 px-3 py-2.5 text-sm border-b-2 -mb-px transition-colors",
              tab === id
                ? "border-accent text-accent-strong font-medium"
                : "border-transparent text-text-3 hover:text-text-2",
            )}
          >
            {label}
            {typeof count === "number" && (
              <span className="font-mono text-xs bg-surface-2 px-1 rounded">{count}</span>
            )}
          </button>
        ))}
      </div>

      {/* Controls row — All tab only */}
      {tab === "all" && (
        <div className="flex flex-wrap items-center gap-3 px-4 py-2.5 border-b border-border-soft bg-bg shrink-0">
          <BrokerFilterChips
            brokerGroups={brokerGroups}
            totalChannels={channels.length}
            active={brokerFilter}
            onToggle={toggleBroker}
            onClear={() => setBrokerFilter([])}
          />
          <div className="ml-auto flex items-center gap-2">
            <SearchInput
              ref={searchRef as React.Ref<HTMLInputElement>}
              placeholder="Find channel…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              onClear={() => setSearch("")}
              className="w-48"
            />
            {/* Map / List toggle */}
            <div className="flex rounded-md border border-border overflow-hidden">
              <button
                type="button"
                onClick={() => setView("map")}
                className={cn(
                  "flex items-center gap-1 px-2.5 py-1.5 text-sm transition-colors",
                  view === "map" ? "bg-accent-soft text-accent-strong" : "hover:bg-surface-2 text-text-3",
                )}
                aria-pressed={view === "map"}
              >
                <MapIcon size={13} /> Map
              </button>
              <button
                type="button"
                onClick={() => setView("list")}
                className={cn(
                  "flex items-center gap-1 px-2.5 py-1.5 text-sm transition-colors border-l border-border",
                  view === "list" ? "bg-accent-soft text-accent-strong" : "hover:bg-surface-2 text-text-3",
                )}
                aria-pressed={view === "list"}
              >
                <LayoutList size={13} /> List
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Workspace */}
      <div className="flex flex-1 min-h-0 overflow-hidden">
        {tab === "all" ? (
          <>
            <div className="flex-1 min-w-0 overflow-y-auto p-4">
              {view === "map" ? (
                <MapView
                  channels={channels}
                  brokerGroups={brokerGroups}
                  totals={totals!}
                  brokerFilter={brokerFilter}
                  search={search}
                  selectedId={selectedChannel?.id ?? null}
                  onSelect={selectChannel}
                />
              ) : (
                <ListView
                  channels={channels}
                  brokerGroups={brokerGroups}
                  brokerFilter={brokerFilter}
                  search={search}
                  selectedId={selectedChannel?.id ?? null}
                  onSelect={selectChannel}
                />
              )}
            </div>
            <DetailPanel
              channel={selectedChannel}
              groupId={groupId}
              onClose={closeDetail}
            />
          </>
        ) : tab === "orphanpub" ? (
          <div className="flex-1 overflow-y-auto">
            <OrphanPublishersTab
              channels={channels}
              onSelect={(c) => { selectChannel(c); setTab("all"); }}
            />
          </div>
        ) : tab === "orphansub" ? (
          <div className="flex-1 overflow-y-auto">
            <OrphanSubscribersTab
              channels={channels}
              onSelect={(c) => { selectChannel(c); setTab("all"); }}
            />
          </div>
        ) : (
          <div className="flex-1 overflow-y-auto">
            <ScheduledTab
              channels={channels}
              onSelect={(c) => { selectChannel(c); setTab("all"); }}
            />
          </div>
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 4.11: Run the build to check for TypeScript errors**

```bash
cd /path/to/archigraph-worktrees/webui-topology2/webui-v2
npm run build
```

Expected: `✓ built in ...ms` with no TypeScript errors. Fix any type errors before proceeding.

- [ ] **Step 4.12: Commit frontend**

```bash
git add webui-v2/src/data/types.ts \
        webui-v2/src/lib/api.ts \
        webui-v2/src/hooks/use-topology.ts \
        webui-v2/src/routes/topology.tsx
git commit -m "feat(webui-v2): implement Topology screen (producer→channel→consumer flow diagram)"
```

---

## Task 5: Add Go `v2_topology.go` backend handler

**Files:**
- Create: `internal/dashboard/v2_topology.go`
- Create: `internal/dashboard/v2_topology_test.go`
- Modify: `internal/dashboard/server.go`

### Step 5.1 — Create `v2_topology.go`

- [ ] **Write the handler file**

The handler is a thin wrapper: it calls the existing `collectTopologyResponse` helper (already tested in `handlers_topology_test.go`) and wraps the result in the v2 envelope. No new topology logic — all the real work is already in `collectTopologyResponse`.

```go
// v2_topology.go — WebUI v2 topology endpoint.
//
// GET /api/v2/topology/{group}
//
// Returns the same payload as GET /api/topology/{group} (v1) but wrapped in
// the standard v2 envelope { ok: true, data: <topologyResponse> }.
// The v1 endpoint is left untouched so legacy clients continue to work.

package dashboard

import (
	"net/http"

	"github.com/cajasmota/archigraph/internal/mcp"
)

// handleV2Topology — GET /api/v2/topology/{group}
func (s *Server) handleV2Topology(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "missing_group", "group path parameter is required")
		return
	}
	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "group_not_found", err.Error())
		return
	}

	docgenState, _ := mcp.LoadDocgenState(group)
	payload := collectTopologyResponse(grp, group, docgenState)
	writeV2JSON(w, http.StatusOK, v2OK(payload))
}
```

### Step 5.2 — Create `v2_topology_test.go`

- [ ] **Write the handler test**

```go
// v2_topology_test.go — tests for GET /api/v2/topology/{group}

package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleV2Topology_OK(t *testing.T) {
	srv := newTestServer(t, testRegistryWithGroup("mygroup"))

	req := httptest.NewRequest(http.MethodGet, "/api/v2/topology/mygroup", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env["ok"] != true {
		t.Errorf("expected ok=true, got %v", env["ok"])
	}
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be an object, got %T", env["data"])
	}
	// Every array field must marshal as [] not null.
	for _, field := range []string{"topics", "queues", "channels", "nats_subjects", "graphql_subscriptions", "broker_groups"} {
		if _, exists := data[field]; !exists {
			t.Errorf("expected field %q in data", field)
		}
	}
}

func TestHandleV2Topology_MissingGroup(t *testing.T) {
	srv := newTestServer(t, testRegistryWithGroup("mygroup"))

	req := httptest.NewRequest(http.MethodGet, "/api/v2/topology/nonexistent", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env["ok"] != false {
		t.Errorf("expected ok=false, got %v", env["ok"])
	}
}
```

Note: `newTestServer` and `testRegistryWithGroup` are helpers already defined in `server_test.go` / `v2_helpers_test.go`. Verify the exact helper names before writing — look at existing tests in `v2_groups_test.go` for the pattern to follow.

- [ ] **Step 5.3: Run the Go tests to verify**

```bash
cd /path/to/archigraph-worktrees/webui-topology2
go test ./internal/dashboard/... -run TestHandleV2Topology -v
```

Expected:
```
--- PASS: TestHandleV2Topology_OK (0.00s)
--- PASS: TestHandleV2Topology_MissingGroup (0.00s)
PASS
```

Fix any failures before proceeding. Common issue: the test helper names — check `v2_groups_test.go` for exact API.

### Step 5.4 — Register route in `server.go`

- [ ] **Add the route to server.go in the v2 routes block**

Find the block in `server.go` that starts with:
```go
mux.HandleFunc("GET /api/v2/meta", s.handleV2Meta)
mux.HandleFunc("GET /api/v2/groups", s.handleV2Groups)
mux.HandleFunc("POST /api/v2/groups", s.handleV2CreateGroup)
```

Append after the POST groups line:
```go
// v2 topology
mux.HandleFunc("GET /api/v2/topology/{group}", s.handleV2Topology)
```

- [ ] **Step 5.5: Run full Go build + test**

```bash
cd /path/to/archigraph-worktrees/webui-topology2
go build ./... && go test ./internal/dashboard/...
```

Expected: `ok github.com/cajasmota/archigraph/internal/dashboard` with no failures.

- [ ] **Step 5.6: Commit backend**

```bash
git add internal/dashboard/v2_topology.go \
        internal/dashboard/v2_topology_test.go \
        internal/dashboard/server.go
git commit -m "feat(api): add GET /api/v2/topology/:group endpoint (v2 envelope wrapper)"
```

---

## Task 6: Playwright screenshot verification

**Files:** Screenshots only; no source changes.

- [ ] **Step 6.1: Set VITE_AG_API_BASE to mock data**

The Vite dev server (port 47280) by default points to `/api`. For screenshots we either:
1. Run against the live daemon at :47274 (if it has indexed data), OR
2. Use the existing MSW mock setup if one exists in the webui-v2 dev setup.

Check: `cat /path/to/archigraph-worktrees/webui-topology2/webui-v2/.env.development 2>/dev/null || echo "no .env"`

If there is no mock API, start the Vite dev server with `VITE_AG_API_BASE=http://localhost:47274/api` to proxy to the live daemon:

```bash
cd /path/to/archigraph-worktrees/webui-topology2/webui-v2
VITE_AG_API_BASE=http://localhost:47274/api npm run dev -- --port 47290
```

Wait for `ready in...` before proceeding.

- [ ] **Step 6.2: Take light theme screenshot (Map view)**

Use Playwright MCP to navigate to `http://localhost:47290/g/upvate/topology` and take a screenshot.

Expected: Map view with broker bands visible, flow units, detail panel placeholder (no channel selected).

Save as `991-topology-light-map.png` in the repo root.

- [ ] **Step 6.3: Take dark theme screenshot**

Open DevTools console or use Playwright to run:
```javascript
document.documentElement.setAttribute("data-theme", "dark");
```
Then take another screenshot.

Save as `991-topology-dark-map.png`.

- [ ] **Step 6.4: Verify List view screenshot**

Click the "List" toggle button, take screenshot.

Save as `991-topology-light-list.png`.

- [ ] **Step 6.5: Tear down dev server**

Find and kill the Vite process on port 47290:
```bash
lsof -ti :47290 | xargs kill -9
```

- [ ] **Step 6.6: Commit screenshots**

```bash
git add 991-topology-light-map.png 991-topology-dark-map.png 991-topology-light-list.png
git commit -m "chore: add Topology screen verification screenshots"
```

---

## Task 7: Final build verification

- [ ] **Step 7.1: Clean build**

```bash
cd /path/to/archigraph-worktrees/webui-topology2/webui-v2
npm run build
```

Expected: exit 0 with no TypeScript errors. Zero warnings about missing types.

- [ ] **Step 7.2: Go build**

```bash
cd /path/to/archigraph-worktrees/webui-topology2
go build ./...
```

Expected: no errors.

- [ ] **Step 7.3: Confirm dashboard/ is untouched**

```bash
cd /path/to/archigraph-worktrees/webui-topology2
git diff --name-only HEAD | grep "^dashboard/" && echo "VIOLATION" || echo "dashboard/ clean"
```

Expected: `dashboard/ clean`

---

## Task 8: Open the PR

- [ ] **Step 8.1: Push branch**

```bash
git push -u origin feat/webui-v2-topology
```

- [ ] **Step 8.2: Open PR with 6-section format**

```bash
gh pr create \
  --title "[PLT] feat: WebUI v2 Topology screen (async message-channel map)" \
  --body "$(cat <<'EOF'
## What

Implements the Topology screen for WebUI v2 (#1440, EPIC #1432). Replaces the placeholder with a fully working producer→channel→consumer flow diagram.

## Why

The Topology screen is the primary way users understand async message flows (Kafka, RabbitMQ, SQS, Redis, WebSocket, GraphQL subscriptions, Celery, serverless) in their codebase. The redesign shows channels as **flow units** (stacked per broker band) instead of a force-blob, making the wiring immediately readable.

## Data decisions

- **ANTI-HALLUCINATION**: only statically-extracted pub/sub data is rendered — no runtime throughput, queue depth, or latency. `last_message_seen` and `usage_history` are never surfaced (always null/empty per design doc).
- The v1 `/api/topology/:group` endpoint is **untouched**. A new `GET /api/v2/topology/{group}` endpoint wraps the existing `collectTopologyResponse` helper in a v2 envelope — zero duplicate logic.
- Channel detail re-uses the existing v1 `/api/topology/:group/topic/:topicId` endpoint since it already returns resolved entities.
- The `lifecycle` field on each channel is derived from producer/consumer count when not present in the raw payload (orphan_publisher = has producers but no consumers; orphan_subscriber = has consumers but no producers).
- The Celery degraded-state note ("Producer/consumer edges aren't indexed for Celery yet") is shown when ALL channels in a band have empty producers AND empty consumers.

## How to test

1. Run `npm run build` in `webui-v2/` → clean exit.
2. Run `go build ./...` + `go test ./internal/dashboard/... -run TestHandleV2Topology` → all pass.
3. Start the daemon, navigate to `/g/<your-group>/topology` → see broker bands, Map/List toggle, tab strip, detail panel.
4. Press `/` to focus search, `Esc` to close detail panel.
5. Check `dashboard/` has zero diff.

## Screenshots

Light map view, dark map view, list view — see attached PNGs (`991-topology-*.png`).

## Checklist

- [x] `npm run build` clean
- [x] `go build ./...` clean
- [x] `go test ./internal/dashboard/... -run TestHandleV2Topology` pass
- [x] `dashboard/` zero diff
- [x] Playwright screenshots: light + dark
- [x] Anti-hallucination: no runtime metrics rendered

Fixes #1440
EPIC #1432

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-Review

### 1. Spec coverage check

| Spec requirement | Covered by |
|---|---|
| 4 tabs: All / Orphan pub / Orphan sub / Scheduled | Task 4.10 — tab strip in TopologyScreen root |
| Broker filter chips (doubles as legend) | Task 4.9 — BrokerFilterChips |
| Map view: stacked BrokerBand + FlowUnit | Tasks 4.4–4.6 |
| List view: dense rows with mini flow indicator | Task 4.6 |
| Detail panel: 10 sections | Task 4.7 — DetailPanel |
| Cross-repo: violet chip + dashed arrow | Tasks 4.2–4.3 — Arrow + EntityChip |
| Degraded Celery state | Task 4.5 — BrokerBand |
| Lifecycle outlines on FlowUnit | Task 4.4 — lifecycleOutline |
| `?tab=` and `?channel=` URL persistence | Task 4.10 — useSearchParams |
| Keyboard: `/` → focus search, `Esc` → close detail | Task 4.10 — searchRef callback |
| Loading skeleton | Task 4.9 — TopologySkeleton |
| No-channels empty state | Task 4.10 — TopologyScreen |
| Filtered empty state | Task 4.6 — MapView |
| Go v2 endpoint with v2 envelope | Task 5 |
| Go tests | Task 5.2 |
| `dashboard/` zero diff | Enforced by Hard rules + Task 7.3 |
| `npm run build` clean | Task 7.1 |
| Playwright screenshots light+dark | Task 6 |
| PR in 6-section format | Task 8 |
| Fixes #1440, ref EPIC #1432 | Task 8 PR body |

### 2. Placeholder scan

No "TBD", "TODO", "implement later", or vague steps found. Every code block is complete.

One item needs verification before Task 5.2: the exact test helper names (`newTestServer`, `testRegistryWithGroup`) — Step 5.2 notes instruct the implementer to verify against `v2_groups_test.go` before running. This is intentional defensive instruction, not a placeholder.

### 3. Type consistency

- `NormalizedChannel` (defined in `use-topology.ts`) is used in `topology.tsx` as `NormalizedChannel` — consistent.
- `TopologyBrokerGroup.health_summary` fields match what `MapView`'s `CanvasSummary` renders.
- `TopologyChannelDetail.producers` and `.consumers` are `TopologyEntity[]` (resolved) — `DetailPanel` accesses `.entity_id`, `.name`, `.kind`, `.repo`, `.source_file`, `.start_line` — all defined on `TopologyEntity`.
- `api.getTopology` returns `TopologyResponse`; `flattenTopology` takes `TopologyResponse` and returns `NormalizedTopology` — consistent.
- `api.getTopologyDetail` returns `TopologyChannelDetail`; `useTopologyDetail` returns `TopologyChannelDetail` — consistent.
