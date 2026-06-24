/* ============================================================
   Topology — Async Message-Channel Map.

   Per design: /Users/jorgecajas/Downloads/design_handoff_grafel/docs/screens/topology.md

   Layout: TopBar › Tab strip › Controls row › Workspace (canvas|list + detail).
   Anti-hallucination: ONLY static pub/sub data. No runtime metrics.
   Tokens are source of truth. Implements #1440 (EPIC #1432).
   ============================================================ */

import { useState, useEffect, useRef } from "react";
import { useParams, useSearchParams } from "react-router-dom";
import {
  ChevronDown,
  ChevronRight,
  X,
  Clock,
  Copy,
  ExternalLink,
  CheckCircle2,
  Map as MapIcon,
  LayoutList,
  ArrowUpRight,
  Network as NetworkIcon,
  Link2 as Link2Icon,
  Layers as LayersIcon,
} from "lucide-react";

import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { SearchInput } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { TabCount, useSetInsight } from "@/components/ui";
import type { InsightValue } from "@/components/ui";
import { cn } from "@/lib/utils";
import { RefLine } from "@/components/RefLine";
import { RepoChip } from "@/lib/repo-color";
import { CompoundTopology, CrossLinkedTopology, UnifiedTopology } from "@/components/compound-topology";
import {
  useTopology,
  useTopologyDetail,
  useOrphanPublishers,
  useOrphanSubscribers,
  flattenChannels,
  deriveCounts,
  extractOrphanPublishers,
  extractOrphanSubscribers,
} from "@/hooks/use-topology";
import type {
  TopologyChannel,
  TopologyBrokerGroup,
  ChannelLifecycle,
  BrokerCanonical,
  TopologyChannelDetail,
  TopologyEntityRef,
} from "@/data/types";

// ---------------------------------------------------------------------------
// § Color + Shape constants (per design spec §7)
// ---------------------------------------------------------------------------

type BrokerShape =
  | "square"
  | "circle"
  | "hexagon"
  | "diamond"
  | "pentagon"
  | "triangle"
  | "star"
  | "cross"
  | "ring"
  | "chevron-shape"
  | "clock-shape"
  | "bolt";

const BROKER_META: Record<
  string,
  { color: string; bgColor: string; shape: BrokerShape }
> = {
  kafka:                 { color: "#22d3ee", bgColor: "#22d3ee22", shape: "square" },
  rabbitmq:              { color: "#fbbf24", bgColor: "#fbbf2422", shape: "circle" },
  sqs:                   { color: "#fb923c", bgColor: "#fb923c22", shape: "hexagon" },
  pubsub:                { color: "#60a5fa", bgColor: "#60a5fa22", shape: "diamond" },
  nats:                  { color: "#e879f9", bgColor: "#e879f922", shape: "pentagon" },
  websocket:             { color: "#2dd4bf", bgColor: "#2dd4bf22", shape: "triangle" },
  sse:                   { color: "#818cf8", bgColor: "#818cf822", shape: "star" },
  graphql_subscription:  { color: "#f472b6", bgColor: "#f472b622", shape: "cross" },
  redis_pubsub:          { color: "#f87171", bgColor: "#f8717122", shape: "ring" },
  redis:                 { color: "#f87171", bgColor: "#f8717122", shape: "ring" },
  "redis-stream":        { color: "#fb7185", bgColor: "#fb718522", shape: "chevron-shape" },
  celery:                { color: "#a3e635", bgColor: "#a3e63522", shape: "clock-shape" },
  "task-queue":          { color: "#a3e635", bgColor: "#a3e63522", shape: "clock-shape" },
  serverless:            { color: "#fde047", bgColor: "#fde04722", shape: "bolt" },
  // Inngest event→function→event workflows (#5485). A bolt nods to the durable
  // async-function model; the indigo hue distinguishes Inngest events from the
  // yellow serverless bolt.
  inngest:               { color: "#6366f1", bgColor: "#6366f122", shape: "bolt" },
  unknown:               { color: "#94a3b8", bgColor: "#94a3b822", shape: "circle" },
};

/** Human label for a topology entity's framework badge (#5485). Inngest events
 *  and functions read as "Inngest event" / "Inngest function" so the workflow
 *  graph is legible as Inngest rather than a generic pub-sub topic. */
function inngestEntityLabel(framework?: string, kind?: string): string | null {
  if (framework !== "inngest") return null;
  const k = (kind ?? "").toLowerCase();
  if (k === "function") return "Inngest function";
  if (k === "messagetopic") return "Inngest event";
  return "Inngest";
}

function brokerMeta(canonical: BrokerCanonical) {
  return BROKER_META[canonical] ?? BROKER_META["unknown"];
}

// Lifecycle colors (always paired with label text for a11y)
const LIFECYCLE_COLOR: Record<ChannelLifecycle, string> = {
  active: "var(--success)",
  orphan_publisher: "#fbbf24",
  orphan_subscriber: "#fb923c",
  orphan: "var(--text-4)",
};
const LIFECYCLE_LABEL: Record<ChannelLifecycle, string> = {
  active: "active",
  orphan_publisher: "orphan pub",
  orphan_subscriber: "orphan sub",
  orphan: "orphan",
};

// ---------------------------------------------------------------------------
// § BrokerShapeIcon SVG component
// ---------------------------------------------------------------------------

function BrokerShapeIcon({
  canonical,
  size = 22,
  className,
}: {
  canonical: BrokerCanonical;
  size?: number;
  className?: string;
}) {
  const m = brokerMeta(canonical);
  const c = size / 2;
  const s = size;
  const strokeProps = { stroke: m.color, strokeWidth: 1.5, fill: m.bgColor };

  const shape = m.shape;

  function hexPath() {
    const pts = Array.from({ length: 6 }, (_, i) => {
      const a = (Math.PI / 3) * i - Math.PI / 6;
      return `${c + (c - 2) * Math.cos(a)},${c + (c - 2) * Math.sin(a)}`;
    });
    return `M ${pts.join(" L ")} Z`;
  }

  function pentPath() {
    const pts = Array.from({ length: 5 }, (_, i) => {
      const a = (2 * Math.PI * i) / 5 - Math.PI / 2;
      return `${c + (c - 2) * Math.cos(a)},${c + (c - 2) * Math.sin(a)}`;
    });
    return `M ${pts.join(" L ")} Z`;
  }

  function starPath() {
    const pts: string[] = [];
    for (let i = 0; i < 10; i++) {
      const a = (Math.PI * i) / 5 - Math.PI / 2;
      const r = i % 2 === 0 ? c - 2 : (c - 2) * 0.45;
      pts.push(`${c + r * Math.cos(a)},${c + r * Math.sin(a)}`);
    }
    return `M ${pts.join(" L ")} Z`;
  }

  return (
    <svg
      width={s}
      height={s}
      viewBox={`0 0 ${s} ${s}`}
      className={className}
      aria-hidden
      style={{ flexShrink: 0 }}
    >
      {shape === "square" && (
        <rect x={2} y={2} width={s - 4} height={s - 4} rx={2} {...strokeProps} />
      )}
      {(shape === "circle" || shape === "ring" || shape === "clock-shape") && (
        <>
          <circle cx={c} cy={c} r={c - 2} {...strokeProps} />
          {shape === "ring" && (
            <circle cx={c} cy={c} r={c * 0.45} stroke={m.color} strokeWidth={1.5} fill="none" />
          )}
        </>
      )}
      {shape === "hexagon" && <path d={hexPath()} {...strokeProps} />}
      {shape === "diamond" && (
        <path d={`M ${c},2 L ${s - 2},${c} L ${c},${s - 2} L 2,${c} Z`} {...strokeProps} />
      )}
      {shape === "pentagon" && <path d={pentPath()} {...strokeProps} />}
      {shape === "triangle" && (
        <path d={`M ${c},2 L ${s - 2},${s - 2} L 2,${s - 2} Z`} {...strokeProps} />
      )}
      {shape === "star" && <path d={starPath()} {...strokeProps} />}
      {shape === "cross" && (
        <path
          d={`M ${s * 0.28},2 L ${s - s * 0.28},2 L ${s - s * 0.28},${s * 0.28} L ${s - 2},${s * 0.28} L ${s - 2},${s - s * 0.28} L ${s - s * 0.28},${s - s * 0.28} L ${s - s * 0.28},${s - 2} L ${s * 0.28},${s - 2} L ${s * 0.28},${s - s * 0.28} L 2,${s - s * 0.28} L 2,${s * 0.28} L ${s * 0.28},${s * 0.28} Z`}
          {...strokeProps}
        />
      )}
      {shape === "chevron-shape" && (
        <path d={`M 2,${c} L ${c},2 L ${s - 2},${c} L ${c},${s - 2} Z`} {...strokeProps} />
      )}
      {shape === "bolt" && (
        <path
          d={`M ${c + 2},2 L ${c - 4},${c - 1} L ${c + 2},${c - 1} L ${c - 2},${s - 2} L ${c + 6},${c + 2} L ${c},${c + 2} Z`}
          {...strokeProps}
        />
      )}
    </svg>
  );
}

// ---------------------------------------------------------------------------
// § Skeleton
// ---------------------------------------------------------------------------

function SkeletonFlowUnit() {
  return (
    <div className="flex items-center gap-3 h-16 px-4 rounded-lg border border-border">
      <Skeleton w="w-6" h="h-6" className="rounded-full shrink-0" />
      <div className="flex-1 space-y-2">
        <Skeleton w="w-1/3" />
        <Skeleton w="w-1/4" h="h-2" />
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// § LifecycleChip
// ---------------------------------------------------------------------------

function LifecycleChip({
  state,
  className,
}: {
  state: ChannelLifecycle;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center h-5 px-2 rounded-full text-xs font-medium border",
        className,
      )}
      style={{
        color: LIFECYCLE_COLOR[state],
        borderColor: LIFECYCLE_COLOR[state] + "55",
        background: LIFECYCLE_COLOR[state] + "18",
      }}
    >
      {LIFECYCLE_LABEL[state]}
    </span>
  );
}

// ---------------------------------------------------------------------------
// § RepoChip (topology-local)
// ---------------------------------------------------------------------------

// Repo chip now delegates to the shared repo-color resolver (#1946).
function LocalRepoChip({
  repo,
  className,
}: {
  repo: string;
  className?: string;
}) {
  return <RepoChip slug={repo} className={className} />;
}

// ---------------------------------------------------------------------------
// § Entity-ref resolution helpers (#1583)
//
// The topology LIST endpoint now ships `producer_refs` / `consumer_refs` —
// resolved entity objects with name + source_file:line — alongside the raw
// hashed id arrays. These helpers normalise either shape to a display ref so
// the UI NEVER renders a bare hash.
// ---------------------------------------------------------------------------

type DisplayRef = {
  name: string;
  repo?: string;
  sourceFile?: string;
  startLine?: number;
  entityId?: string;
  kind?: string;
};

/** Best-effort name from a raw "repo::local:hash" id, used only as a last
 *  resort when no resolved ref is available. */
function nameFromId(id: string): { name: string; repo: string | null } {
  const parts = (id ?? "").split("::");
  const tail = parts[parts.length - 1] ?? "";
  const name = tail.split(":").pop() || id;
  const repo = parts.length > 1 ? parts[0] : null;
  return { name, repo };
}

function refToDisplay(ref: TopologyEntityRef): DisplayRef {
  // The backend emits "unresolved" kind with a derived name when an id can't be
  // matched; still prefer that name over the raw hash.
  return {
    name: ref.name || nameFromId(ref.entity_id).name,
    repo: ref.repo,
    sourceFile: ref.source_file || undefined,
    startLine: ref.start_line || undefined,
    entityId: ref.entity_id,
    kind: ref.kind,
  };
}

function idToDisplay(id: string): DisplayRef {
  const { name, repo } = nameFromId(id);
  return { name, repo: repo ?? undefined, entityId: id };
}

/** Resolve display refs for a channel side, preferring resolved refs over ids. */
function resolveSide(
  refs: TopologyEntityRef[] | undefined,
  ids: string[] | undefined,
): DisplayRef[] {
  if (refs && refs.length > 0) return refs.map(refToDisplay);
  return (ids ?? []).map(idToDisplay);
}

function shortFile(path?: string): string {
  if (!path) return "";
  return path.split("/").pop() ?? path;
}

/** Build an editor deep-link (VS Code / Cursor "vscode://file/…:line").
 *  source_file is repo-relative; we still emit a vscode://file URI which most
 *  editors resolve against the open workspace. Returns null when no file. */
function editorHref(sourceFile?: string, startLine?: number): string | null {
  if (!sourceFile) return null;
  const line = startLine && startLine > 0 ? `:${startLine}` : "";
  return `vscode://file/${sourceFile}${line}`;
}

// ---------------------------------------------------------------------------
// § EntityChip — resolved publisher/subscriber chip (name + source ref)
// ---------------------------------------------------------------------------

function EntityChip({
  ref,
  crossRepo = false,
}: {
  ref: DisplayRef;
  crossRepo?: boolean;
}) {
  const fileRef = ref.sourceFile
    ? `${shortFile(ref.sourceFile)}${ref.startLine ? `:${ref.startLine}` : ""}`
    : null;
  const title = [
    ref.name,
    ref.sourceFile ? `${ref.sourceFile}${ref.startLine ? `:${ref.startLine}` : ""}` : null,
    ref.entityId,
  ]
    .filter(Boolean)
    .join("\n");
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 h-5 px-2 rounded-full text-xs border shrink-0 max-w-full",
        crossRepo
          ? "border-[#a78bfa55] bg-[#a78bfa18] text-[#a78bfa]"
          : "border-border bg-surface-2 text-text-2",
      )}
      title={title}
    >
      {crossRepo && ref.repo && (
        <span className="text-[10px] opacity-70 shrink-0">{ref.repo}/</span>
      )}
      <span className="font-mono truncate max-w-[180px]">{ref.name}</span>
      {fileRef && (
        <span className="text-[9px] text-text-4 font-mono shrink-0">{fileRef}</span>
      )}
    </span>
  );
}

// ---------------------------------------------------------------------------
// § FlatChannel
// ---------------------------------------------------------------------------

type FlatChannel = TopologyChannel & { lifecycle_state: ChannelLifecycle };

// ---------------------------------------------------------------------------
// § BrokerDiagram — visual topology: publishers → channels → subscribers (#1583)
//
// A columnar SVG diagram for one broker group. Three columns:
//   left   = distinct publisher entities
//   middle = channel nodes (clickable, drive the detail panel)
//   right  = distinct subscriber entities
// Edges connect publisher→channel and channel→subscriber with bezier links.
// Node count is capped per column to stay memory-safe on huge graphs; the cap
// is surfaced as a "+N more" footer so nothing is silently hidden.
// ---------------------------------------------------------------------------

const DIAGRAM_MAX_CHANNELS = 40; // per broker
const DIAGRAM_MAX_SIDE = 60; // per side column

// --- Selection model (#1609) ----------------------------------------------
// A node in the Map is identified by which column it lives in plus its stable
// key. Selecting a node highlights its full related path:
//   publisher  → channels it publishes to + those channels' subscribers
//   subscriber → channels it subscribes to + those channels' publishers
//   channel    → its publishers + subscribers
// Everything else is dimmed. Selection lives in MapView so only one node is
// ever active across the whole map; clicking it again (or empty space) clears.
type MapSelection =
  | { col: "pub"; key: string }
  | { col: "chan"; key: string } // key = channel id
  | { col: "sub"; key: string }
  | null;

/** What a selection lights up: sets of pub keys, channel ids, sub keys. */
type Related = {
  pubs: Set<string>;
  chans: Set<string>;
  subs: Set<string>;
};

function BrokerDiagram({
  brokerGroup,
  channels,
  selectedId,
  onSelect,
  selection,
  onSelectNode,
}: {
  brokerGroup: TopologyBrokerGroup;
  channels: FlatChannel[];
  selectedId: string | null;
  onSelect: (ch: FlatChannel) => void;
  selection: MapSelection;
  onSelectNode: (sel: MapSelection) => void;
}) {
  const [open, setOpen] = useState(true);
  const m = brokerMeta(brokerGroup.broker);

  // Lifecycle rank: connected/active flows first, dead-ends last (#1609).
  const lifecycleRank: Record<ChannelLifecycle, number> = {
    active: 0,
    orphan_publisher: 1,
    orphan_subscriber: 2,
    orphan: 3,
  };

  // Stable side keys for a channel.
  const pubKeysOf = (ch: FlatChannel) =>
    resolveSide(ch.producer_refs, ch.producers).map((p) => p.entityId ?? p.name);
  const subKeysOf = (ch: FlatChannel) =>
    resolveSide(ch.consumer_refs, ch.consumers).map((c) => c.entityId ?? c.name);

  // Sort channels: active grouped at top (by shared publisher then subscriber to
  // minimise line crossings), orphans sink to the bottom (#1609). Stable within
  // a rank via original index.
  const sortedChannels = channels
    .map((ch, idx) => ({ ch, idx }))
    .sort((a, b) => {
      const ra = lifecycleRank[a.ch.lifecycle_state];
      const rb = lifecycleRank[b.ch.lifecycle_state];
      if (ra !== rb) return ra - rb;
      // Group rows sharing a publisher (then subscriber) adjacently.
      const pa = pubKeysOf(a.ch)[0] ?? "";
      const pb = pubKeysOf(b.ch)[0] ?? "";
      if (pa !== pb) return pa < pb ? -1 : 1;
      const sa = subKeysOf(a.ch)[0] ?? "";
      const sb = subKeysOf(b.ch)[0] ?? "";
      if (sa !== sb) return sa < sb ? -1 : 1;
      return a.idx - b.idx;
    })
    .map((x) => x.ch);

  // Cap channels rendered to keep the SVG bounded.
  const shownChannels = sortedChannels.slice(0, DIAGRAM_MAX_CHANNELS);
  const channelOverflow = sortedChannels.length - shownChannels.length;

  // Build distinct publisher / subscriber node sets keyed by entityId|name.
  const pubMap = new Map<string, DisplayRef>();
  const subMap = new Map<string, DisplayRef>();
  // Edges as [sideKey, channelIndex].
  const pubEdges: { key: string; ci: number }[] = [];
  const subEdges: { key: string; ci: number }[] = [];

  shownChannels.forEach((ch, ci) => {
    for (const p of resolveSide(ch.producer_refs, ch.producers)) {
      const key = p.entityId ?? p.name;
      if (!pubMap.has(key)) pubMap.set(key, p);
      pubEdges.push({ key, ci });
    }
    for (const c of resolveSide(ch.consumer_refs, ch.consumers)) {
      const key = c.entityId ?? c.name;
      if (!subMap.has(key)) subMap.set(key, c);
      subEdges.push({ key, ci });
    }
  });

  // Order side columns by first appearance in the (already-sorted) channel
  // list so related rows align horizontally and crossings stay minimal (#1609).
  const pubKeys = Array.from(pubMap.keys()).slice(0, DIAGRAM_MAX_SIDE);
  const subKeys = Array.from(subMap.keys()).slice(0, DIAGRAM_MAX_SIDE);
  const pubIdx = new Map(pubKeys.map((k, i) => [k, i]));
  const subIdx = new Map(subKeys.map((k, i) => [k, i]));

  // --- Resolve the active selection into highlighted sets (#1609) ----------
  const chanById = new Map(shownChannels.map((ch) => [ch.id, ch]));
  let related: Related | null = null;
  if (selection) {
    const pubs = new Set<string>();
    const chans = new Set<string>();
    const subs = new Set<string>();
    if (selection.col === "chan") {
      const ch = chanById.get(selection.key);
      if (ch) {
        chans.add(ch.id);
        for (const k of pubKeysOf(ch)) pubs.add(k);
        for (const k of subKeysOf(ch)) subs.add(k);
      }
    } else if (selection.col === "pub") {
      pubs.add(selection.key);
      for (const ch of shownChannels) {
        if (pubKeysOf(ch).includes(selection.key)) {
          chans.add(ch.id);
          for (const k of subKeysOf(ch)) subs.add(k); // full downstream path
        }
      }
    } else {
      // subscriber → its channels + those channels' publishers (upstream path)
      subs.add(selection.key);
      for (const ch of shownChannels) {
        if (subKeysOf(ch).includes(selection.key)) {
          chans.add(ch.id);
          for (const k of pubKeysOf(ch)) pubs.add(k);
        }
      }
    }
    related = { pubs, chans, subs };
  }
  // Only dim if the selection actually belongs to (lit something in) this broker.
  const dimming =
    related !== null &&
    (related.pubs.size > 0 || related.chans.size > 0 || related.subs.size > 0);

  const litPub = (k: string) => !related || related.pubs.has(k);
  const litChan = (id: string) => !related || related.chans.has(id);
  const litSub = (k: string) => !related || related.subs.has(k);
  // An edge is lit when BOTH endpoints are part of the related path.
  const litPubEdge = (k: string, id: string) =>
    !related || (related.pubs.has(k) && related.chans.has(id));
  const litSubEdge = (id: string, k: string) =>
    !related || (related.chans.has(id) && related.subs.has(k));

  function toggle(sel: NonNullable<MapSelection>) {
    if (selection && selection.col === sel.col && selection.key === sel.key) {
      onSelectNode(null);
    } else {
      onSelectNode(sel);
    }
  }

  // Layout geometry.
  const ROW_H = 30;
  const NODE_H = 24;
  const PAD_Y = 16;
  const COL_W = 220; // side-column node width (widened so longer pub/sub names fit; full name on hover via <title>)
  const CH_W = 200; // channel node width (widened so e.g. "celery:core/tasks/…" reads without hard truncation)
  const GAP = 96; // horizontal gap between columns
  const leftX = 8;
  const chX = leftX + COL_W + GAP;
  const rightX = chX + CH_W + GAP;
  const width = rightX + COL_W + 8;

  const rows = Math.max(pubKeys.length, shownChannels.length, subKeys.length, 1);
  const height = PAD_Y * 2 + rows * ROW_H;

  const yOf = (i: number) => PAD_Y + i * ROW_H + NODE_H / 2;

  function nodeColor(ref: DisplayRef) {
    return ref.kind === "unresolved" ? "var(--text-4)" : "var(--text-2)";
  }

  return (
    <div className="border border-border rounded-lg overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center gap-3 px-4 py-2.5 bg-surface-2 hover:bg-surface-3 transition-colors text-left"
        aria-expanded={open}
      >
        {open ? (
          <ChevronDown size={14} className="text-text-4 shrink-0" />
        ) : (
          <ChevronRight size={14} className="text-text-4 shrink-0" />
        )}
        <div
          className="size-7 rounded flex items-center justify-center shrink-0"
          style={{ background: m.bgColor }}
        >
          <BrokerShapeIcon canonical={brokerGroup.broker} size={18} />
        </div>
        <span className="font-medium text-text-2 capitalize">{brokerGroup.broker}</span>
        <span className="text-md text-text-3">{brokerGroup.count} channels</span>
        <div className="ml-auto flex items-center gap-3 text-xs text-text-4">
          <span>{pubMap.size} publishers</span>
          <span>{subMap.size} subscribers</span>
        </div>
      </button>

      {open && (
        <div className="bg-bg overflow-x-auto ag-scroll">
          {/* Column headers */}
          <div
            className="grid text-[10px] uppercase tracking-wide text-text-4 px-0 pt-2"
            style={{
              gridTemplateColumns: `${leftX + COL_W}px ${GAP + CH_W}px 1fr`,
              minWidth: width,
            }}
          >
            <span className="text-right pr-2">Publishers</span>
            <span className="text-center">Channel</span>
            <span className="pl-2" style={{ marginLeft: GAP }}>
              Subscribers
            </span>
          </div>

          <svg
            width={width}
            height={height}
            viewBox={`0 0 ${width} ${height}`}
            className="block"
            style={{ minWidth: width }}
            onClick={(e) => {
              // Click on empty canvas clears the selection.
              if (e.target === e.currentTarget) onSelectNode(null);
            }}
          >
            {/* Edges: publisher → channel */}
            {pubEdges.map((e, i) => {
              const pi = pubIdx.get(e.key);
              if (pi === undefined) return null;
              const chId = shownChannels[e.ci]?.id;
              const y1 = yOf(pi);
              const y2 = yOf(e.ci);
              const x1 = leftX + COL_W;
              const x2 = chX;
              const mx = (x1 + x2) / 2;
              const lit = litPubEdge(e.key, chId);
              const isSelChan = chId === selectedId;
              const active = lit && (dimming || isSelChan);
              return (
                <path
                  key={`pe-${i}`}
                  d={`M ${x1},${y1} C ${mx},${y1} ${mx},${y2} ${x2},${y2}`}
                  fill="none"
                  stroke={active ? m.color : "var(--border-strong)"}
                  strokeWidth={active ? 1.6 : 1}
                  opacity={dimming && !lit ? 0.06 : active ? 0.9 : 0.45}
                  style={{ transition: "opacity 0.18s, stroke 0.18s" }}
                />
              );
            })}
            {/* Edges: channel → subscriber */}
            {subEdges.map((e, i) => {
              const si = subIdx.get(e.key);
              if (si === undefined) return null;
              const chId = shownChannels[e.ci]?.id;
              const y1 = yOf(e.ci);
              const y2 = yOf(si);
              const x1 = chX + CH_W;
              const x2 = rightX;
              const mx = (x1 + x2) / 2;
              const lit = litSubEdge(chId, e.key);
              const isSelChan = chId === selectedId;
              const active = lit && (dimming || isSelChan);
              return (
                <path
                  key={`se-${i}`}
                  d={`M ${x1},${y1} C ${mx},${y1} ${mx},${y2} ${x2},${y2}`}
                  fill="none"
                  stroke={active ? m.color : "var(--border-strong)"}
                  strokeWidth={active ? 1.6 : 1}
                  opacity={dimming && !lit ? 0.06 : active ? 0.9 : 0.45}
                  style={{ transition: "opacity 0.18s, stroke 0.18s" }}
                />
              );
            })}

            {/* Publisher nodes (clickable — highlight full downstream path) */}
            {pubKeys.map((k, i) => {
              const ref = pubMap.get(k)!;
              const y = PAD_Y + i * ROW_H;
              const lit = litPub(k);
              const sel = selection?.col === "pub" && selection.key === k;
              return (
                <g
                  key={`p-${k}`}
                  onClick={() => toggle({ col: "pub", key: k })}
                  style={{
                    cursor: "pointer",
                    opacity: dimming && !lit ? 0.18 : 1,
                    transition: "opacity 0.18s",
                  }}
                >
                  <rect
                    x={leftX}
                    y={y}
                    width={COL_W}
                    height={NODE_H}
                    rx={5}
                    fill={sel ? m.bgColor : "var(--surface)"}
                    stroke={sel ? m.color : lit && dimming ? m.color + "99" : "var(--border)"}
                    strokeWidth={sel ? 1.6 : 1}
                    style={{ transition: "stroke 0.18s, fill 0.18s" }}
                  />
                  <text
                    x={leftX + COL_W - 8}
                    y={y + NODE_H / 2 + 4}
                    textAnchor="end"
                    fontSize={11}
                    fontFamily="ui-monospace, monospace"
                    fill={nodeColor(ref)}
                    style={{ pointerEvents: "none" }}
                  >
                    {ref.name.length > 30 ? ref.name.slice(0, 29) + "…" : ref.name}
                    <title>
                      {ref.name}
                      {ref.sourceFile ? `\n${ref.sourceFile}:${ref.startLine ?? ""}` : ""}
                    </title>
                  </text>
                </g>
              );
            })}

            {/* Channel nodes (clickable — highlight publishers + subscribers) */}
            {shownChannels.map((ch, i) => {
              const y = PAD_Y + i * ROW_H;
              const isSel = ch.id === selectedId || (selection?.col === "chan" && selection.key === ch.id);
              const lit = litChan(ch.id);
              return (
                <g
                  key={`c-${ch.id}`}
                  onClick={() => {
                    toggle({ col: "chan", key: ch.id });
                    onSelect(ch); // keep the detail panel wired to channel clicks
                  }}
                  style={{
                    cursor: "pointer",
                    opacity: dimming && !lit ? 0.18 : 1,
                    transition: "opacity 0.18s",
                  }}
                >
                  <rect
                    x={chX}
                    y={y}
                    width={CH_W}
                    height={NODE_H}
                    rx={5}
                    fill={isSel ? m.bgColor : "var(--surface-2)"}
                    stroke={isSel ? m.color : lit && dimming ? m.color : m.color + "66"}
                    strokeWidth={isSel ? 1.6 : 1}
                    style={{ transition: "stroke 0.18s, fill 0.18s" }}
                  />
                  <text
                    x={chX + CH_W / 2}
                    y={y + NODE_H / 2 + 4}
                    textAnchor="middle"
                    fontSize={11}
                    fontFamily="ui-monospace, monospace"
                    fill="var(--text)"
                    style={{ pointerEvents: "none" }}
                  >
                    {ch.label.length > 28 ? ch.label.slice(0, 27) + "…" : ch.label}
                    <title>{ch.label}</title>
                  </text>
                </g>
              );
            })}

            {/* Subscriber nodes (clickable — highlight upstream path) */}
            {subKeys.map((k, i) => {
              const ref = subMap.get(k)!;
              const y = PAD_Y + i * ROW_H;
              const lit = litSub(k);
              const sel = selection?.col === "sub" && selection.key === k;
              return (
                <g
                  key={`s-${k}`}
                  onClick={() => toggle({ col: "sub", key: k })}
                  style={{
                    cursor: "pointer",
                    opacity: dimming && !lit ? 0.18 : 1,
                    transition: "opacity 0.18s",
                  }}
                >
                  <rect
                    x={rightX}
                    y={y}
                    width={COL_W}
                    height={NODE_H}
                    rx={5}
                    fill={sel ? m.bgColor : "var(--surface)"}
                    stroke={sel ? m.color : lit && dimming ? m.color + "99" : "var(--border)"}
                    strokeWidth={sel ? 1.6 : 1}
                    style={{ transition: "stroke 0.18s, fill 0.18s" }}
                  />
                  <text
                    x={rightX + 8}
                    y={y + NODE_H / 2 + 4}
                    textAnchor="start"
                    fontSize={11}
                    fontFamily="ui-monospace, monospace"
                    fill={nodeColor(ref)}
                    style={{ pointerEvents: "none" }}
                  >
                    {ref.name.length > 30 ? ref.name.slice(0, 29) + "…" : ref.name}
                    <title>
                      {ref.name}
                      {ref.sourceFile ? `\n${ref.sourceFile}:${ref.startLine ?? ""}` : ""}
                    </title>
                  </text>
                </g>
              );
            })}
          </svg>

          {(channelOverflow > 0 ||
            pubMap.size > pubKeys.length ||
            subMap.size > subKeys.length) && (
            <p className="px-4 py-2 text-xs text-text-4 border-t border-border">
              {channelOverflow > 0 && `+${channelOverflow} more channels `}
              {pubMap.size > pubKeys.length && `· +${pubMap.size - pubKeys.length} publishers `}
              {subMap.size > subKeys.length && `· +${subMap.size - subKeys.length} subscribers `}
              — switch to List view to see all.
            </p>
          )}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § MapView
// ---------------------------------------------------------------------------

function MapView({
  channels,
  brokerGroups,
  selectedId,
  onSelect,
  selection,
  onSelectNode,
}: {
  channels: FlatChannel[];
  brokerGroups: TopologyBrokerGroup[];
  selectedId: string | null;
  onSelect: (ch: FlatChannel) => void;
  selection: MapSelection;
  onSelectNode: (sel: MapSelection) => void;
}) {
  const totalCrossRepo = brokerGroups.reduce((n, bg) => n + (bg.cross_repo_topic_count ?? 0), 0);
  const totalOrphanPub = brokerGroups.reduce((n, bg) => n + (bg.orphan_publishers ?? 0), 0);
  const totalOrphanSub = brokerGroups.reduce((n, bg) => n + (bg.orphan_subscribers ?? 0), 0);
  const totalActive = brokerGroups.reduce((n, bg) => n + (bg.health_summary?.active ?? 0), 0);

  const summaryItems = [
    { label: `${channels.length} channels` },
    { label: `${brokerGroups.length} brokers` },
    { label: `${totalActive} active`, color: "var(--success)" },
    ...(totalOrphanPub > 0 ? [{ label: `${totalOrphanPub} orphan-pub`, color: "#fbbf24" }] : []),
    ...(totalOrphanSub > 0 ? [{ label: `${totalOrphanSub} orphan-sub`, color: "#fb923c" }] : []),
    ...(totalCrossRepo > 0 ? [{ label: `${totalCrossRepo} cross-repo`, color: "#a78bfa" }] : []),
  ];

  return (
    <div className="flex flex-col gap-4">
      {/* Summary card */}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-2.5 rounded-lg bg-surface-2 border border-border text-md">
        {summaryItems.map((s, i) => (
          <span key={i} className="font-mono text-text-2 flex items-center gap-1">
            {i > 0 && <span className="text-text-4 select-none">·</span>}
            <span style={s.color ? { color: s.color } : undefined}>{s.label}</span>
          </span>
        ))}
      </div>

      {/* Broker diagrams — visual publisher → channel → subscriber flow */}
      {brokerGroups.map((bg) => {
        const bandChannels = channels.filter((ch) => ch.broker_canonical === bg.broker);
        if (bandChannels.length === 0) return null;
        return (
          <BrokerDiagram
            key={bg.broker}
            brokerGroup={bg}
            channels={bandChannels}
            selectedId={selectedId}
            onSelect={onSelect}
            selection={selection}
            onSelectNode={onSelectNode}
          />
        );
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § ListView
// ---------------------------------------------------------------------------

/** Compact "first-name (+N)" summary for one channel side, used in aligned rows. */
function SideSummary({
  refs,
  align,
  emptyLabel,
}: {
  refs: DisplayRef[];
  align: "left" | "right";
  emptyLabel: string;
}) {
  if (refs.length === 0) {
    return <span className="text-xs text-text-4 italic truncate block">{emptyLabel}</span>;
  }
  const first = refs[0];
  const overflow = refs.length - 1;
  const title = refs
    .map((r) => `${r.name}${r.sourceFile ? ` — ${shortFile(r.sourceFile)}:${r.startLine ?? ""}` : ""}`)
    .join("\n");
  return (
    <span
      className={cn("flex items-baseline gap-1 min-w-0", align === "right" && "justify-end")}
      title={title}
    >
      <span className="font-mono text-sm text-text-2 truncate">{first.name}</span>
      {overflow > 0 && <span className="text-xs text-text-4 shrink-0">+{overflow}</span>}
    </span>
  );
}

// Shared column template so the header and every row align perfectly (#1583).
const LIST_GRID =
  "grid grid-cols-[minmax(0,1fr)_16px_minmax(0,1.2fr)_16px_minmax(0,1fr)_120px_84px] items-center gap-2";

function ListHeader() {
  return (
    <div className={cn(LIST_GRID, "px-3 py-1 text-[10px] uppercase tracking-wide text-text-4")}>
      <span className="text-right">Publisher</span>
      <span />
      <span>Channel</span>
      <span />
      <span>Subscriber</span>
      <span>Status</span>
      <span className="text-right">Repo</span>
    </div>
  );
}

function ListRow({
  channel,
  selected,
  onClick,
}: {
  channel: FlatChannel;
  selected: boolean;
  onClick: () => void;
}) {
  const producers = resolveSide(channel.producer_refs, channel.producers);
  const consumers = resolveSide(channel.consumer_refs, channel.consumers);
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        LIST_GRID,
        "w-full px-3 py-1.5 rounded-md text-left transition-colors",
        selected
          ? "bg-accent-soft/20 border border-accent"
          : "border border-transparent hover:bg-surface-2",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
      )}
    >
      {/* Publisher column (right-aligned, points into the channel) */}
      <SideSummary refs={producers} align="right" emptyLabel="no publisher" />
      <span className="text-text-4 text-xs text-center">→</span>
      {/* Channel column */}
      <span className="flex items-center gap-1.5 min-w-0">
        <BrokerShapeIcon canonical={channel.broker_canonical} size={16} />
        <span className="font-mono text-md text-text truncate" title={channel.label}>
          {channel.label}
        </span>
      </span>
      <span className="text-text-4 text-xs text-center">→</span>
      {/* Subscriber column */}
      <SideSummary refs={consumers} align="left" emptyLabel="no subscriber" />
      {/* Status */}
      <LifecycleChip state={channel.lifecycle_state} className="justify-self-start" />
      {/* Repo */}
      <LocalRepoChip repo={channel.repo} className="justify-self-end max-w-full truncate" />
    </button>
  );
}

function ListView({
  channels,
  brokerGroups,
  selectedId,
  onSelect,
}: {
  channels: FlatChannel[];
  brokerGroups: TopologyBrokerGroup[];
  selectedId: string | null;
  onSelect: (ch: FlatChannel) => void;
}) {
  return (
    <div className="space-y-4">
      {brokerGroups.map((bg) => {
        const rows = channels.filter((ch) => ch.broker_canonical === bg.broker);
        if (rows.length === 0) return null;
        return (
          <div key={bg.broker}>
            <div className="flex items-center gap-2 px-2 py-1 text-sm text-text-3 border-b border-border mb-1">
              <BrokerShapeIcon canonical={bg.broker} size={14} />
              <span className="capitalize">{bg.broker}</span>
              <span className="ml-auto text-text-4">{rows.length}</span>
            </div>
            <ListHeader />
            <div className="space-y-0.5">
              {rows.map((ch) => (
                <ListRow
                  key={ch.id}
                  channel={ch}
                  selected={selectedId === ch.id}
                  onClick={() => onSelect(ch)}
                />
              ))}
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § DetailPanel
// ---------------------------------------------------------------------------

function DetailSection({
  label,
  count,
  children,
  empty,
  infoText,
}: {
  label: string;
  count?: number;
  children?: React.ReactNode;
  empty?: string;
  /** Optional (i) tooltip shown next to the label. */
  infoText?: string;
}) {
  return (
    <div className="border-t border-border pt-3 mt-3">
      <div className="flex items-center gap-1.5 mb-2 text-md font-medium text-text-2">
        <span>{label}</span>
        {infoText && (
          <span
            className="inline-flex items-center justify-center text-text-4 hover:text-accent transition-colors cursor-help"
            title={infoText}
            aria-label={`About ${label}`}
          >
            <svg width="13" height="13" viewBox="0 0 13 13" aria-hidden fill="none">
              <circle cx="6.5" cy="6.5" r="5.5" stroke="currentColor" strokeWidth="1.2" />
              <line x1="6.5" y1="5.5" x2="6.5" y2="9.5" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
              <circle cx="6.5" cy="3.8" r="0.65" fill="currentColor" />
            </svg>
          </span>
        )}
        {count !== undefined && (
          <span className="ml-auto text-xs text-text-4 tabular-nums">{count}</span>
        )}
      </div>
      {children ?? (empty ? <p className="text-sm text-text-4 italic">{empty}</p> : null)}
    </div>
  );
}

function EntityList({ ids }: { ids: string[] }) {
  if (!ids || ids.length === 0) return null;
  return (
    <div className="space-y-1">
      {ids.map((id) => {
        const parts = (id ?? "").split("::");
        const name = (parts[parts.length - 1] ?? "").split(":").pop() || id;
        const repo = parts.length > 1 ? parts[0] : null;
        return (
          <div key={id} className="flex items-center gap-2 text-sm">
            {repo && <LocalRepoChip repo={repo} className="text-[10px]" />}
            <span className="font-mono text-text truncate">{name}</span>
          </div>
        );
      })}
    </div>
  );
}

/**
 * Renders a list of producer/consumer entries from the topic-detail endpoint.
 *
 * #1943 redesign:
 *   - Primary text: entity name (large, regular) + kind chip right-aligned
 *   - Secondary line: RefLine (repo chip + file:line) — clickable to open source
 *   - Right-side ↗ flow button — navigates to cross-stack flow containing this
 *     entity + channel as a bridge step (from flow_process_ids on the record)
 *   - Unresolved entities show muted hash with tooltip
 *   - Drop the "publishes/subscribes this channel" verb subtitle
 */
function EntityRefList({
  entries,
  groupId,
}: {
  entries: (TopologyEntityRef | string)[];
  groupId: string;
}) {
  if (!entries || entries.length === 0) return null;
  return (
    <div className="space-y-1">
      {entries.map((entry, i) => {
        const ref: DisplayRef =
          typeof entry === "string" ? idToDisplay(entry) : refToDisplay(entry);
        const isUnresolved = ref.kind === "unresolved" || (!ref.name && !ref.sourceFile);
        const flowProcessIds: string[] =
          typeof entry !== "string" ? (entry.flow_process_ids ?? []) : [];
        const firstFlowId = flowProcessIds[0] ?? null;
        // #5485: Inngest function step structure + framework badge.
        const framework =
          typeof entry !== "string" ? entry.framework : undefined;
        const inngestSteps =
          typeof entry !== "string" ? (entry.inngest_steps ?? []) : [];
        const inngestLabel = inngestEntityLabel(framework, ref.kind);

        return (
          <div
            key={ref.entityId || i}
            className={cn(
              "rounded-md border bg-surface px-2.5 py-2",
              isUnresolved ? "border-border/60 opacity-70" : "border-border",
            )}
          >
            {/* Row 1: name + kind chip + ↗ flow */}
            <div className="flex items-center gap-2 min-w-0">
              {isUnresolved ? (
                <span
                  className="font-mono text-xs text-text-4 truncate flex-1"
                  title="Synthetic publisher (no resolved entity)"
                >
                  {ref.name || ref.entityId}
                </span>
              ) : (
                <span
                  className="text-sm font-medium text-text truncate flex-1"
                  title={ref.name}
                >
                  {ref.name}
                </span>
              )}
              {inngestLabel ? (
                <span
                  className="text-[10px] px-1.5 py-0.5 rounded shrink-0 border"
                  style={{
                    color: BROKER_META.inngest.color,
                    background: BROKER_META.inngest.bgColor,
                    borderColor: BROKER_META.inngest.color + "55",
                  }}
                  title={inngestLabel}
                >
                  {inngestLabel}
                </span>
              ) : (
                ref.kind &&
                ref.kind !== "unresolved" && (
                  <span className="text-[10px] px-1.5 py-0.5 rounded bg-surface-2 text-text-4 border border-border shrink-0 tabular-nums">
                    {ref.kind}
                  </span>
                )
              )}
              {/* ↗ flow action — links to first matching flow */}
              {firstFlowId && (
                <a
                  href={`/g/${groupId}/flows?flow=${encodeURIComponent(firstFlowId)}`}
                  title={
                    flowProcessIds.length > 1
                      ? `Open in flow (${flowProcessIds.length} flows available)`
                      : "Open in flow"
                  }
                  className={cn(
                    "shrink-0 inline-flex items-center gap-0.5 px-1.5 py-0.5 rounded",
                    "text-[10px] font-medium text-accent border border-accent/30",
                    "hover:bg-accent/10 transition-colors",
                  )}
                  onClick={(e) => e.stopPropagation()}
                >
                  <ArrowUpRight size={10} />
                  {flowProcessIds.length > 1 ? `${flowProcessIds.length} flows` : "flow"}
                </a>
              )}
            </div>

            {/* Row 2: RefLine (repo chip + file:line) */}
            {!isUnresolved && ref.sourceFile && (
              <div className="mt-1 -mx-1">
                <RefLine
                  repo={ref.repo ?? ""}
                  file={ref.sourceFile}
                  line={ref.startLine ?? 0}
                  name=""
                  title={`${ref.repo ?? ""} · ${ref.sourceFile}:${ref.startLine ?? ""}`}
                  className="text-[11px] py-0.5 px-1"
                />
              </div>
            )}
            {isUnresolved && (
              <p className="mt-0.5 text-[10px] text-text-4 italic">
                Synthetic publisher (no resolved entity)
              </p>
            )}

            {/* #5485: Inngest durable step structure — run / sleep / waitForEvent
                / invoke. Surfaced so clicking an Inngest event shows the steps of
                the producer/consumer function inline. */}
            {inngestSteps.length > 0 && (
              <ol className="mt-1.5 space-y-0.5 border-l-2 border-[#6366f155] pl-2">
                {inngestSteps.map((s, si) => (
                  <li
                    key={s.step_id || si}
                    className="flex items-baseline gap-1.5 text-[11px]"
                  >
                    <span className="text-text-4 tabular-nums shrink-0">
                      {si + 1}.
                    </span>
                    <span
                      className="font-mono px-1 rounded shrink-0"
                      style={{
                        color: BROKER_META.inngest.color,
                        background: BROKER_META.inngest.bgColor,
                      }}
                      title={`step.${s.step_kind}`}
                    >
                      {s.step_kind}
                    </span>
                    <span className="font-mono text-text-2 truncate">
                      {s.step_id}
                    </span>
                    {s.wait_event && (
                      <span className="text-text-4 truncate">
                        ↦ {s.wait_event}
                      </span>
                    )}
                    {s.invoke_target && (
                      <span className="text-text-4 truncate">
                        → {s.invoke_target}
                      </span>
                    )}
                  </li>
                ))}
              </ol>
            )}
          </div>
        );
      })}
    </div>
  );
}

function DetailPanel({
  channel,
  onClose,
  groupId,
}: {
  channel: FlatChannel;
  onClose: () => void;
  groupId: string;
}) {
  const { data: detail } = useTopologyDetail(groupId, channel.id);
  const m = brokerMeta(channel.broker_canonical);
  const d = detail as TopologyChannelDetail | undefined;

  function copyId() {
    void navigator.clipboard.writeText(channel.id);
  }

  // The detail endpoint returns producers/consumers as rich entity objects
  // (TopologyEntityRef), NOT plain string ids. The base TopologyChannel type
  // says string[] (list-endpoint shape); cast at the boundary. String(obj)
  // would produce "[object Object]" — use EntityRefList which handles both
  // shapes so it renders name/repo/source_file correctly (#1543).
  const producerEntries: (TopologyEntityRef | string)[] = d
    ? ((d.producers ?? []) as unknown as (TopologyEntityRef | string)[])
    : (channel.producers ?? []);
  const consumerEntries: (TopologyEntityRef | string)[] = d
    ? ((d.consumers ?? []) as unknown as (TopologyEntityRef | string)[])
    : (channel.consumers ?? []);

  return (
    <aside
      className="flex flex-col h-full bg-bg border-l border-border overflow-y-auto"
      aria-label="Channel detail"
    >
      {/* Sticky header */}
      <div className="sticky top-0 z-10 bg-bg border-b border-border px-4 py-3">
        <div className="flex items-center gap-2">
          <BrokerShapeIcon canonical={channel.broker_canonical} size={22} />
          <span className="font-mono font-semibold text-text truncate flex-1">
            {channel.label}
          </span>
          <button
            onClick={onClose}
            className="inline-flex items-center justify-center size-7 rounded-md text-text-4 hover:bg-surface-2 hover:text-text transition-colors"
            aria-label="Close detail panel"
          >
            <X size={14} />
          </button>
        </div>

        <div className="flex flex-wrap gap-1.5 mt-2">
          <span
            className="inline-flex items-center h-5 px-2 rounded text-xs"
            style={{ color: m.color, background: m.bgColor }}
          >
            {channel.broker_canonical}
          </span>
          <LocalRepoChip repo={channel.repo} />
          <LifecycleChip state={channel.lifecycle_state} />
          {channel.cross_repo && (
            <span className="inline-flex items-center h-5 px-2 rounded border-dashed border border-[#a78bfa] text-[#a78bfa] text-xs">
              cross-repo
            </span>
          )}
          {channel.scheduled && (
            <span className="inline-flex items-center gap-1 h-5 px-2 rounded bg-yellow-950/30 text-yellow-300 border border-yellow-700/40 text-xs">
              <Clock size={9} />
              {channel.schedule ?? "scheduled"}
            </span>
          )}
        </div>
      </div>

      {/* Body */}
      <div className="px-4 pb-8">
        {/* Identity */}
        <DetailSection label="Identity">
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
            {d?.source_file && (
              <>
                <dt className="text-text-4">Source</dt>
                <dd className="font-mono text-text-2 truncate min-w-0">
                  {editorHref(d.source_file, d.start_line) ? (
                    <a
                      href={editorHref(d.source_file, d.start_line)!}
                      className="text-accent hover:underline inline-flex items-center gap-1"
                      title={`Open ${d.source_file}:${d.start_line ?? ""}`}
                    >
                      <ExternalLink size={10} className="shrink-0" />
                      <span className="truncate">
                        {d.source_file}
                        {d.start_line ? `:${d.start_line}` : ""}
                      </span>
                    </a>
                  ) : (
                    <>
                      {d.source_file}
                      {d.start_line ? `:${d.start_line}` : ""}
                    </>
                  )}
                </dd>
              </>
            )}
            {d?.protocol && (
              <>
                <dt className="text-text-4">Protocol</dt>
                <dd className="text-text-2">{d.protocol}</dd>
              </>
            )}
            {channel.framework && (
              <>
                <dt className="text-text-4">Framework</dt>
                <dd className="text-text-2">{channel.framework}</dd>
              </>
            )}
            {channel.channel_type && (
              <>
                <dt className="text-text-4">Channel</dt>
                <dd className="text-text-2">{channel.channel_type}</dd>
              </>
            )}
            <dt className="text-text-4">ID</dt>
            <dd className="font-mono text-xs text-text-3 truncate">{channel.id}</dd>
          </dl>
        </DetailSection>

        {/* Description */}
        {channel.docs_summary && (
          <DetailSection label="Description">
            <p className="text-sm text-text-2 leading-relaxed">{channel.docs_summary}</p>
            {channel.docgen_status === "enriched" && (
              <span className="mt-1 inline-flex items-center gap-1 text-xs text-text-4">
                <CheckCircle2 size={10} />
                AI-generated
              </span>
            )}
          </DetailSection>
        )}

        {/* Publishers */}
        <DetailSection
          label="Publishers"
          count={producerEntries.length}
          infoText="Code that emits messages on this channel. Click ↗ flow to see the cross-stack flow it starts."
          empty={
            producerEntries.length === 0
              ? "No publishers found in indexed code — this is the orphan-subscriber signal."
              : undefined
          }
        >
          {producerEntries.length > 0 ? (
            <EntityRefList entries={producerEntries} groupId={groupId} />
          ) : null}
        </DetailSection>

        {/* Subscribers */}
        <DetailSection
          label="Subscribers"
          count={consumerEntries.length}
          infoText="Code that consumes messages from this channel. Click ↗ flow to trace the full cross-stack path."
          empty={
            consumerEntries.length === 0
              ? "No subscribers found in indexed code — this is the orphan-publisher signal."
              : undefined
          }
        >
          {consumerEntries.length > 0 ? (
            <EntityRefList entries={consumerEntries} groupId={groupId} />
          ) : null}
        </DetailSection>

        {/* Message schema */}
        {d?.message_schema && (
          <DetailSection label="Message schema">
            <pre className="text-xs font-mono bg-surface-2 border border-border rounded p-2 overflow-x-auto whitespace-pre-wrap">
              {d.message_schema}
            </pre>
            <p className="text-[10px] text-text-4 mt-1">schema (static)</p>
          </DetailSection>
        )}

        {/* Tests */}
        {d?.tests && d.tests.length > 0 && (
          <DetailSection label="Tests" count={d.tests.length}>
            <EntityList ids={d.tests} />
          </DetailSection>
        )}

        {/* Related channels */}
        {d?.related_topics && d.related_topics.length > 0 && (
          <DetailSection label="Related channels" count={d.related_topics.length}>
            <div className="space-y-1">
              {d.related_topics.map((rt) => (
                <div key={rt.id} className="flex items-center gap-2 text-sm">
                  <BrokerShapeIcon canonical={rt.broker_canonical} size={14} />
                  <span className="font-mono text-text-2 truncate">{rt.label}</span>
                </div>
              ))}
            </div>
          </DetailSection>
        )}

        {/* Flows */}
        {d?.flow_count != null && d.flow_count > 0 && (
          <DetailSection label="Flows">
            <p className="text-sm text-text-2">Appears in {d.flow_count} flows</p>
          </DetailSection>
        )}

        {/* Enrichment health */}
        {d?.enrichment_health && (
          <DetailSection label="Documentation completeness">
            <p className="text-sm text-text-2">
              {d.enrichment_health.filled_field_count} of{" "}
              {d.enrichment_health.total_field_count} fields documented
            </p>
            <p className="text-xs text-text-4 mt-1">
              doc-authored estimates, not measured runtime metrics
            </p>
          </DetailSection>
        )}
      </div>

      {/* Footer */}
      <div className="sticky bottom-0 bg-bg border-t border-border px-4 py-2 flex gap-2">
        <Button variant="ghost" size="sm" onClick={copyId} className="gap-1.5 text-xs">
          <Copy size={11} />
          Copy id
        </Button>
        {d?.source_file && editorHref(d.source_file, d.start_line) && (
          <Button
            variant="ghost"
            size="sm"
            className="gap-1.5 text-xs"
            asChild
          >
            <a
              href={editorHref(d.source_file, d.start_line)!}
              title={`Open ${d.source_file}:${d.start_line ?? ""}`}
            >
              <ExternalLink size={11} />
              Open source
            </a>
          </Button>
        )}
      </div>
    </aside>
  );
}

// ---------------------------------------------------------------------------
// § Orphan tabs
// ---------------------------------------------------------------------------

function OrphanPublisherTab({
  groupId,
  selectedId,
  onSelect,
}: {
  groupId: string;
  selectedId: string | null;
  onSelect: (id: string) => void;
}) {
  const { data, isLoading } = useOrphanPublishers(groupId);
  if (isLoading) {
    return (
      <div className="space-y-2 p-4">
        {[0, 1, 2].map((i) => (
          <SkeletonFlowUnit key={i} />
        ))}
      </div>
    );
  }
  // Real daemon returns { orphan_publishers: [...] }, not a bare array (#1535).
  // extractOrphanPublishers is the single source of truth shared with the
  // tab-strip badge so the count can never disagree with the rows (#5).
  const entries = extractOrphanPublishers(data);
  if (entries.length === 0) {
    return (
      <div className="flex flex-col items-center py-16 text-center gap-3">
        <CheckCircle2 size={32} className="text-[var(--success)]" />
        <p className="text-md font-medium text-text">No orphan publishers</p>
        <p className="text-sm text-text-3">Every published channel has a subscriber.</p>
      </div>
    );
  }
  return (
    <div className="p-4 space-y-2">
      {entries.map((e) => {
        // The daemon returns `broker` (raw name), not `broker_canonical`.
        // Resolve whichever field is present so the icon/color renders correctly (#1543).
        const brokerKey = (e.broker_canonical ?? e.broker ?? "unknown") as BrokerCanonical;
        return (
          <div
            key={e.id}
            role="button"
            tabIndex={0}
            aria-pressed={e.id === selectedId}
            onClick={() => onSelect(e.id)}
            onKeyDown={(ev) => {
              if (ev.key === "Enter" || ev.key === " ") {
                ev.preventDefault();
                onSelect(e.id);
              }
            }}
            className={cn(
              "flex items-center gap-3 px-3 py-2 rounded-lg border bg-surface cursor-pointer transition-colors hover:bg-surface-2 focus:outline-none focus-visible:ring-1 focus-visible:ring-border-strong",
              e.id === selectedId ? "border-border-strong" : "border-border",
            )}
          >
            <BrokerShapeIcon canonical={brokerKey} size={18} />
            <span className="font-mono text-md text-text truncate flex-1">{e.label}</span>
            <div className="flex gap-1 flex-wrap">
              {(e.producers ?? []).slice(0, 3).map((p) => (
                <EntityChip key={p} ref={idToDisplay(p)} />
              ))}
              {(e.producers?.length ?? 0) > 3 && (
                <span className="text-xs text-text-3">+{(e.producers?.length ?? 0) - 3}</span>
              )}
            </div>
            <span className="text-xs px-2 py-0.5 rounded-full bg-warning/10 text-warning border border-warning/40 shrink-0">
              no subscriber found
            </span>
            <LocalRepoChip repo={e.repo} />
          </div>
        );
      })}
    </div>
  );
}

function OrphanSubscriberTab({
  groupId,
  selectedId,
  onSelect,
}: {
  groupId: string;
  selectedId: string | null;
  onSelect: (id: string) => void;
}) {
  const { data, isLoading } = useOrphanSubscribers(groupId);
  if (isLoading) {
    return (
      <div className="space-y-2 p-4">
        {[0, 1, 2].map((i) => (
          <SkeletonFlowUnit key={i} />
        ))}
      </div>
    );
  }
  // Real daemon returns { orphan_subscribers: [...] }, not a bare array (#1535).
  // extractOrphanSubscribers is the single source of truth shared with the
  // tab-strip badge so the count can never disagree with the rows (#5).
  const entries = extractOrphanSubscribers(data);
  if (entries.length === 0) {
    return (
      <div className="flex flex-col items-center py-16 text-center gap-3">
        <CheckCircle2 size={32} className="text-[var(--success)]" />
        <p className="text-md font-medium text-text">No orphan subscribers</p>
        <p className="text-sm text-text-3">
          Every subscribing entity has at least one publisher in the indexed code.
        </p>
      </div>
    );
  }
  return (
    <div className="p-4 space-y-2">
      {entries.map((e) => {
        // Resolve broker: the daemon returns `broker`, not `broker_canonical` (#1543).
        const brokerKey = (e.broker_canonical ?? e.broker ?? "unknown") as BrokerCanonical;
        return (
          <div
            key={e.id}
            role="button"
            tabIndex={0}
            aria-pressed={e.id === selectedId}
            onClick={() => onSelect(e.id)}
            onKeyDown={(ev) => {
              if (ev.key === "Enter" || ev.key === " ") {
                ev.preventDefault();
                onSelect(e.id);
              }
            }}
            className={cn(
              "flex items-center gap-3 px-3 py-2 rounded-lg border bg-surface cursor-pointer transition-colors hover:bg-surface-2 focus:outline-none focus-visible:ring-1 focus-visible:ring-border-strong",
              e.id === selectedId ? "border-border-strong" : "border-border",
            )}
          >
            <BrokerShapeIcon canonical={brokerKey} size={18} />
            <span className="font-mono text-md text-text truncate flex-1">{e.label}</span>
            <div className="flex gap-1 flex-wrap">
              {(e.consumers ?? []).slice(0, 3).map((c) => (
                <EntityChip key={c} ref={idToDisplay(c)} />
              ))}
              {(e.consumers?.length ?? 0) > 3 && (
                <span className="text-xs text-text-3">+{(e.consumers?.length ?? 0) - 3}</span>
              )}
            </div>
            <span
              className={cn(
                "text-xs px-2 py-0.5 rounded-full shrink-0",
                e.reason === "publisher_only_in_external_lib"
                  ? "bg-surface-2 text-text-3 border border-dashed border-border-strong"
                  : "bg-warning/10 text-warning border border-warning/40",
              )}
            >
              {e.reason === "publisher_only_in_external_lib"
                ? "publisher in external lib"
                : "no publisher found"}
            </span>
            <LocalRepoChip repo={e.repo} />
          </div>
        );
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § Scheduled tab
// ---------------------------------------------------------------------------

function ScheduledTab({
  channels,
  selectedId,
  onSelect,
}: {
  channels: FlatChannel[];
  selectedId: string | null;
  onSelect: (ch: FlatChannel) => void;
}) {
  const scheduled = channels.filter((ch) => ch.scheduled);
  if (scheduled.length === 0) {
    return (
      <div className="flex flex-col items-center py-16 text-center gap-3">
        <Clock size={32} className="text-text-4" />
        <p className="text-md font-medium text-text">No scheduled channels</p>
        <p className="text-sm text-text-3">
          Channels with a cron or interval schedule will appear here.
        </p>
      </div>
    );
  }
  const sorted = [...scheduled].sort((a, b) =>
    (a.broker_canonical ?? "").localeCompare(b.broker_canonical ?? ""),
  );
  return (
    <div className="p-4 space-y-2">
      {sorted.map((ch) => (
        <div
          key={ch.id}
          role="button"
          tabIndex={0}
          aria-pressed={ch.id === selectedId}
          onClick={() => onSelect(ch)}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              onSelect(ch);
            }
          }}
          className={cn(
            "flex items-center gap-3 px-3 py-2.5 rounded-lg border bg-surface cursor-pointer transition-colors hover:bg-surface-2 focus:outline-none focus-visible:ring-1 focus-visible:ring-border-strong",
            ch.id === selectedId ? "border-border-strong" : "border-border",
          )}
        >
          <BrokerShapeIcon canonical={ch.broker_canonical} size={18} />
          <span className="font-mono text-md text-text truncate flex-1">{ch.label}</span>
          {ch.schedule && (
            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-mono bg-yellow-950/30 text-yellow-300 border border-yellow-700/40 shrink-0">
              <Clock size={10} />
              {ch.schedule}
            </span>
          )}
          {ch.framework && (
            <span className="text-xs px-1.5 py-0.5 rounded bg-surface-2 text-text-3 border border-border shrink-0">
              {ch.framework}
            </span>
          )}
          <LocalRepoChip repo={ch.repo} />
        </div>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § TopologyScreen — main export
// ---------------------------------------------------------------------------

// Screen insight (#4655) — registered with the breadcrumb Insights button via
// useSetInsight. Module-level constant for stable identity across renders.
const TOPOLOGY_INSIGHT: InsightValue = {
  storageKey: "topology",
  human: (
    <>
      This is your{" "}
      <strong className="text-text-2">messaging topology</strong> — a
      map of who publishes and who subscribes across your message
      queues and topics. Use it to see which services send messages to
      a channel, which ones receive them, and where a channel has a
      publisher but no subscriber (or vice-versa) so messages may be
      dropped.
    </>
  ),
  agent: {
    tool: "grafel_topology",
    example:
      "Before changing the schema of the `order.created` event, an agent calls grafel_topology to enumerate every subscriber, so it updates all consumers in lockstep — and flags the orphaned topic that has a publisher but no subscriber as a likely dropped-message bug.",
  },
};

export default function TopologyScreen() {
  useSetInsight(TOPOLOGY_INSIGHT);
  const { groupId = "" } = useParams<{ groupId: string }>();
  const [searchParams, setSearchParams] = useSearchParams();

  const tab = (searchParams.get("tab") ?? "all") as
    | "all"
    | "orphan-publishers"
    | "orphan-subscribers"
    | "scheduled";
  const channelParam = searchParams.get("channel");

  const [viewMode, setViewMode] = useState<
    "map" | "list" | "arch" | "crosslink" | "unified"
  >("map");
  const [search, setSearch] = useState("");
  const [activeBrokers, setActiveBrokers] = useState<Set<string>>(new Set());
  const [selectedId, setSelectedId] = useState<string | null>(channelParam);
  // Map-view node selection (publisher / channel / subscriber path highlight, #1609).
  const [mapSelection, setMapSelection] = useState<MapSelection>(null);
  const searchRef = useRef<HTMLInputElement>(null);

  const { data, isLoading, isError } = useTopology(groupId);
  const channels = flattenChannels(data);
  const brokerGroups = data?.broker_groups ?? [];

  // Orphan badge counts MUST come from the same endpoint arrays the orphan tabs
  // render, otherwise the pill and the list disagree (#5). The previous
  // deriveCounts-based pill counted a client-derived lifecycle bucket whose
  // orphan-pub/orphan-sub labels were inverted relative to the daemon's
  // canonical definition (handlers_topology_orphans.go), so the badge could
  // read 16 while the tab showed "No orphan publishers". We now take the badge
  // count straight from extractOrphanPublishers/Subscribers — the single source
  // of truth the OrphanPublisherTab / OrphanSubscriberTab bodies use.
  const { data: orphanPubData } = useOrphanPublishers(groupId);
  const { data: orphanSubData } = useOrphanSubscribers(groupId);
  const orphanPublishers = extractOrphanPublishers(orphanPubData);
  const orphanSubscribers = extractOrphanSubscribers(orphanSubData);
  const baseCounts = deriveCounts(channels, brokerGroups);
  const counts = {
    ...baseCounts,
    orphanPub: orphanPublishers.length,
    orphanSub: orphanSubscribers.length,
  };

  // Sync channel deep-link param
  useEffect(() => {
    if (channelParam) setSelectedId(channelParam);
  }, [channelParam]);

  // Keyboard: "/" focuses search, Escape closes detail
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "/" && document.activeElement?.tagName !== "INPUT") {
        e.preventDefault();
        searchRef.current?.focus();
      }
      if (e.key === "Escape") {
        setSelectedId(null);
        setMapSelection(null);
        setSearchParams((prev) => {
          const n = new URLSearchParams(prev);
          n.delete("channel");
          return n;
        });
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [setSearchParams]);

  const filteredChannels = channels.filter((ch) => {
    if (search && !(ch.label ?? "").toLowerCase().includes(search.toLowerCase())) return false;
    if (activeBrokers.size > 0 && !activeBrokers.has(ch.broker_canonical)) return false;
    return true;
  });

  // Orphan rows come from the orphan endpoints and may reference channels that
  // aren't in the main topology payload, so when a selected id isn't a known
  // channel we synthesize a minimal FlatChannel from the orphan entry. The
  // detail panel then hydrates the rest from the detail endpoint via the id,
  // exactly like clicking a channel (#6).
  const orphanChannelFallback = (id: string): FlatChannel | null => {
    const pub = orphanPublishers.find((e) => e.id === id);
    if (pub) {
      return {
        id: pub.id,
        label: pub.label,
        broker: pub.broker,
        broker_canonical: (pub.broker_canonical ?? pub.broker) as FlatChannel["broker_canonical"],
        owning_service: "",
        producers: pub.producers ?? [],
        consumers: [],
        repo: pub.repo,
        lifecycle_state: "orphan_publisher",
      };
    }
    const sub = orphanSubscribers.find((e) => e.id === id);
    if (sub) {
      return {
        id: sub.id,
        label: sub.label,
        broker: sub.broker,
        broker_canonical: (sub.broker_canonical ?? sub.broker) as FlatChannel["broker_canonical"],
        owning_service: "",
        producers: [],
        consumers: sub.consumers ?? [],
        repo: sub.repo,
        lifecycle_state: "orphan_subscriber",
      };
    }
    return null;
  };

  const selectedChannel = selectedId
    ? channels.find((ch) => ch.id === selectedId) ?? orphanChannelFallback(selectedId)
    : null;

  function selectChannel(ch: FlatChannel) {
    setSelectedId(ch.id);
    setSearchParams((prev) => {
      const n = new URLSearchParams(prev);
      n.set("channel", ch.id);
      return n;
    });
  }

  // Select by id (used by orphan rows whose entries aren't full FlatChannels).
  function selectChannelId(id: string) {
    setSelectedId(id);
    setSearchParams((prev) => {
      const n = new URLSearchParams(prev);
      n.set("channel", id);
      return n;
    });
  }

  function closeDetail() {
    setSelectedId(null);
    setSearchParams((prev) => {
      const n = new URLSearchParams(prev);
      n.delete("channel");
      return n;
    });
  }

  function toggleBroker(broker: string) {
    setActiveBrokers((prev) => {
      const next = new Set(prev);
      if (next.has(broker)) next.delete(broker);
      else next.add(broker);
      return next;
    });
  }

  function setTab(t: string) {
    setSearchParams((prev) => {
      const n = new URLSearchParams(prev);
      n.set("tab", t);
      return n;
    });
  }

  // Empty state
  if (!isLoading && !isError && channels.length === 0) {
    return (
      <div className="flex flex-col h-full bg-bg">
        <div className="flex flex-col items-center justify-center flex-1 text-center gap-4 px-6">
          <p className="text-xl font-semibold text-text">No async channels indexed</p>
          <p className="text-md text-text-3 max-w-sm">
            No message topics, queues, or async channels were found. Add messaging libraries to
            your group or check Settings.
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full bg-bg">
      <Tabs value={tab} onValueChange={setTab} className="flex flex-col flex-1 min-h-0">
        {/* Tab strip */}
        <div className="border-b border-border shrink-0 px-4">
          <TabsList className="border-0">
            <TabsTrigger value="all">
              All channels
              {isLoading ? (
                <span className="ml-1.5 text-text-4">…</span>
              ) : (
                <TabCount
                  value={counts.total}
                  tone="neutral"
                  active={tab === "all"}
                  label="async channels (queues/topics) indexed"
                />
              )}
            </TabsTrigger>
            <TabsTrigger value="orphan-publishers">
              Orphan publishers
              {isLoading ? (
                <span className="ml-1.5 text-text-4">…</span>
              ) : (
                <TabCount
                  value={counts.orphanPub}
                  tone={counts.orphanPub > 0 ? "warning" : "neutral"}
                  active={tab === "orphan-publishers"}
                  label="channels published to but never subscribed"
                />
              )}
            </TabsTrigger>
            <TabsTrigger value="orphan-subscribers">
              Orphan subscribers
              {isLoading ? (
                <span className="ml-1.5 text-text-4">…</span>
              ) : (
                <TabCount
                  value={counts.orphanSub}
                  tone={counts.orphanSub > 0 ? "warning" : "neutral"}
                  active={tab === "orphan-subscribers"}
                  label="channels subscribed to but never published"
                />
              )}
            </TabsTrigger>
            <TabsTrigger value="scheduled">
              Scheduled jobs
              {isLoading ? (
                <span className="ml-1.5 text-text-4">…</span>
              ) : (
                <TabCount
                  value={counts.scheduled}
                  tone="neutral"
                  active={tab === "scheduled"}
                  label="scheduled / cron-triggered producers"
                />
              )}
            </TabsTrigger>
          </TabsList>
        </div>

        {/* Plain-language intro + agent-usage banner */}
        <div className="px-4 pt-3 pb-1 border-b border-border shrink-0 space-y-2">
          
        </div>

        {/* All tab */}
        <TabsContent value="all" className="flex-1 min-h-0 flex flex-col mt-0">
          {/* Controls row */}
          <div className="flex items-center gap-3 px-4 py-2.5 border-b border-border shrink-0 flex-wrap">
            {/* Broker filter chips (= legend) */}
            <div className="flex items-center gap-1.5 flex-wrap">
              {activeBrokers.size > 0 && (
                <button
                  type="button"
                  onClick={() => setActiveBrokers(new Set())}
                  className="h-7 px-2.5 rounded-full text-xs text-text-3 border border-border hover:bg-surface-2 transition-colors"
                >
                  All
                </button>
              )}
              {brokerGroups.map((bg) => {
                const m = brokerMeta(bg.broker);
                const active = activeBrokers.size === 0 || activeBrokers.has(bg.broker);
                return (
                  <button
                    key={bg.broker}
                    type="button"
                    onClick={() => toggleBroker(bg.broker)}
                    className={cn(
                      "inline-flex items-center gap-1.5 h-7 px-2.5 rounded-full text-xs border transition-colors",
                      active
                        ? "border-border-strong text-text-2 bg-surface"
                        : "border-border text-text-4 bg-transparent opacity-50",
                    )}
                    style={active ? { borderColor: m.color + "55", color: m.color } : {}}
                    aria-pressed={active && activeBrokers.size > 0}
                    title={`${bg.broker} (${bg.count})`}
                  >
                    <BrokerShapeIcon canonical={bg.broker} size={14} />
                    <span className="capitalize">{bg.broker}</span>
                    <span className="tabular-nums opacity-70">{bg.count}</span>
                  </button>
                );
              })}
            </div>

            {/* Search */}
            <div className="flex-1 min-w-[160px] max-w-[280px]">
              <SearchInput
                ref={searchRef}
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Find channel… /"
                className="h-7 text-sm"
              />
            </div>

            {/* Map / List toggle */}
            <div className="ml-auto flex items-center border border-border rounded-md overflow-hidden">
              <button
                type="button"
                onClick={() => setViewMode("map")}
                className={cn(
                  "inline-flex items-center gap-1 px-2.5 h-7 text-xs transition-colors",
                  viewMode === "map"
                    ? "bg-surface-2 text-text"
                    : "text-text-3 hover:bg-surface",
                )}
                aria-pressed={viewMode === "map"}
                title="Map view"
              >
                <MapIcon size={13} />
                Map
              </button>
              <button
                type="button"
                onClick={() => setViewMode("list")}
                className={cn(
                  "inline-flex items-center gap-1 px-2.5 h-7 text-xs border-l border-border transition-colors",
                  viewMode === "list"
                    ? "bg-surface-2 text-text"
                    : "text-text-3 hover:bg-surface",
                )}
                aria-pressed={viewMode === "list"}
                title="List view"
              >
                <LayoutList size={13} />
                List
              </button>
              <button
                type="button"
                onClick={() => setViewMode("arch")}
                className={cn(
                  "inline-flex items-center gap-1 px-2.5 h-7 text-xs border-l border-border transition-colors",
                  viewMode === "arch"
                    ? "bg-surface-2 text-text"
                    : "text-text-3 hover:bg-surface",
                )}
                aria-pressed={viewMode === "arch"}
                title="Architecture diagram — compound zones + tier lanes (Model 1)"
              >
                <NetworkIcon size={13} />
                Architecture
              </button>
              <button
                type="button"
                onClick={() => setViewMode("crosslink")}
                className={cn(
                  "inline-flex items-center gap-1 px-2.5 h-7 text-xs border-l border-border transition-colors",
                  viewMode === "crosslink"
                    ? "bg-surface-2 text-text"
                    : "text-text-3 hover:bg-surface",
                )}
                aria-pressed={viewMode === "crosslink"}
                title="Cross-linked lenses — select a node to highlight its infra↔code counterpart (Model 2)"
              >
                <Link2Icon size={13} />
                Cross-link
              </button>
              <button
                type="button"
                onClick={() => setViewMode("unified")}
                className={cn(
                  "inline-flex items-center gap-1 px-2.5 h-7 text-xs border-l border-border transition-colors",
                  viewMode === "unified"
                    ? "bg-surface-2 text-text"
                    : "text-text-3 hover:bg-surface",
                )}
                aria-pressed={viewMode === "unified"}
                title="Unified diagram — infra resources and the code that uses them in one architecture picture (Model 3)"
              >
                <LayersIcon size={13} />
                Unified
              </button>
            </div>
          </div>

          {/* Workspace */}
          {viewMode === "unified" ? (
            /* Unified infra+code architecture diagram (Model 3, #4810) — ONE
               compound canvas interleaving IaC resources and the code that uses
               them, with the real code↔infra usage edges drawn. */
            <div className="flex flex-1 min-h-0">
              <UnifiedTopology groupId={groupId} className="flex-1 min-w-0" />
            </div>
          ) : viewMode === "crosslink" ? (
            /* Cross-linked lenses (Model 2, #4810) — Infra | Code side-by-side;
               select a node to highlight its counterpart in the other lens. */
            <div className="flex flex-1 min-h-0">
              <CrossLinkedTopology groupId={groupId} className="flex-1 min-w-0" />
            </div>
          ) : viewMode === "arch" ? (
            /* Architecture diagram (Model 1, #4810/#4811) — compound zones +
               tier lanes + collapsible zones, independent of the broker
               channel filters above. Takes the full canvas. */
            <div className="flex flex-1 min-h-0">
              <CompoundTopology groupId={groupId} className="flex-1 min-w-0" />
            </div>
          ) : (
          <div className="flex flex-1 min-h-0">
            {/* Canvas / list area */}
            <div
              className={cn(
                "flex-1 min-w-0 overflow-y-auto ag-scroll px-4 py-4",
                selectedChannel ? "lg:max-w-[calc(100%-380px)]" : "",
              )}
            >
              {isLoading ? (
                <div className="space-y-3">
                  {[0, 1, 2, 3, 4].map((i) => (
                    <SkeletonFlowUnit key={i} />
                  ))}
                </div>
              ) : isError ? (
                <div className="py-16 text-center">
                  <p className="text-md font-semibold text-text">
                    Couldn&apos;t load topology
                  </p>
                  <p className="text-sm text-text-3 mt-1">
                    Make sure the daemon is running.
                  </p>
                </div>
              ) : filteredChannels.length === 0 ? (
                <div className="py-16 text-center">
                  <p className="text-md font-semibold text-text">No channels match</p>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      setSearch("");
                      setActiveBrokers(new Set());
                    }}
                    className="mt-2"
                  >
                    Clear filters
                  </Button>
                </div>
              ) : viewMode === "map" ? (
                <MapView
                  channels={filteredChannels}
                  brokerGroups={brokerGroups.filter(
                    (bg) =>
                      activeBrokers.size === 0 || activeBrokers.has(bg.broker),
                  )}
                  selectedId={selectedId}
                  onSelect={selectChannel}
                  selection={mapSelection}
                  onSelectNode={setMapSelection}
                />
              ) : (
                <ListView
                  channels={filteredChannels}
                  brokerGroups={brokerGroups}
                  selectedId={selectedId}
                  onSelect={selectChannel}
                />
              )}
            </div>

            {/* Detail panel — 380px, hidden below lg */}
            {selectedChannel && (
              <div
                className="hidden lg:flex flex-col shrink-0 overflow-hidden"
                style={{ width: 380 }}
              >
                <DetailPanel
                  channel={selectedChannel}
                  onClose={closeDetail}
                  groupId={groupId}
                />
              </div>
            )}
          </div>
          )}
        </TabsContent>

        {/* Orphan publishers */}
        <TabsContent
          value="orphan-publishers"
          className="flex-1 min-h-0 flex mt-0"
        >
          <div className="flex-1 min-w-0 overflow-y-auto ag-scroll">
            <OrphanPublisherTab
              groupId={groupId}
              selectedId={selectedId}
              onSelect={selectChannelId}
            />
          </div>
          {selectedChannel && (
            <div
              className="hidden lg:flex flex-col shrink-0 overflow-hidden"
              style={{ width: 380 }}
            >
              <DetailPanel
                channel={selectedChannel}
                onClose={closeDetail}
                groupId={groupId}
              />
            </div>
          )}
        </TabsContent>

        {/* Orphan subscribers */}
        <TabsContent
          value="orphan-subscribers"
          className="flex-1 min-h-0 flex mt-0"
        >
          <div className="flex-1 min-w-0 overflow-y-auto ag-scroll">
            <OrphanSubscriberTab
              groupId={groupId}
              selectedId={selectedId}
              onSelect={selectChannelId}
            />
          </div>
          {selectedChannel && (
            <div
              className="hidden lg:flex flex-col shrink-0 overflow-hidden"
              style={{ width: 380 }}
            >
              <DetailPanel
                channel={selectedChannel}
                onClose={closeDetail}
                groupId={groupId}
              />
            </div>
          )}
        </TabsContent>

        {/* Scheduled */}
        <TabsContent
          value="scheduled"
          className="flex-1 min-h-0 flex mt-0"
        >
          <div className="flex-1 min-w-0 overflow-y-auto ag-scroll">
            <ScheduledTab
              channels={channels}
              selectedId={selectedId}
              onSelect={selectChannel}
            />
          </div>
          {selectedChannel && (
            <div
              className="hidden lg:flex flex-col shrink-0 overflow-hidden"
              style={{ width: 380 }}
            >
              <DetailPanel
                channel={selectedChannel}
                onClose={closeDetail}
                groupId={groupId}
              />
            </div>
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
}
