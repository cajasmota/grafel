/* ============================================================
   Topology — Async Message-Channel Map.

   Per design: /Users/jorgecajas/Downloads/design_handoff_archigraph/docs/screens/topology.md

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
  AlertCircle,
  CheckCircle2,
  Map,
  LayoutList,
} from "lucide-react";

import { TopBar } from "@/components/chrome/top-bar";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { SearchInput } from "@/components/ui/input";
import { Pill } from "@/components/ui/pill";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import {
  useTopology,
  useTopologyDetail,
  useOrphanPublishers,
  useOrphanSubscribers,
  flattenChannels,
  deriveCounts,
} from "@/hooks/use-topology";
import type {
  TopologyChannel,
  TopologyBrokerGroup,
  ChannelLifecycle,
  BrokerCanonical,
  TopologyChannelDetail,
  TopologyEntityRef,
  OrphanPublisherEntry,
  OrphanSubscriberEntry,
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
  unknown:               { color: "#94a3b8", bgColor: "#94a3b822", shape: "circle" },
};

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
// § RepoChip
// ---------------------------------------------------------------------------

function RepoChip({
  repo,
  crossRepo = false,
  className,
}: {
  repo: string;
  crossRepo?: boolean;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center h-5 px-2 rounded-full text-xs font-mono border",
        crossRepo
          ? "border-[#a78bfa55] bg-[#a78bfa18] text-[#a78bfa]"
          : "border-border bg-surface-2 text-text-3",
        className,
      )}
    >
      {repo}
    </span>
  );
}

// ---------------------------------------------------------------------------
// § EntityChip
// ---------------------------------------------------------------------------

function EntityChip({
  id,
  crossRepo = false,
}: {
  id: string;
  crossRepo?: boolean;
}) {
  const parts = (id ?? "").split("::");
  const name = (parts[parts.length - 1] ?? "").split(":").pop() || id;
  const repo = parts.length > 1 ? parts[0] : null;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 h-5 px-2 rounded-full text-xs border shrink-0",
        crossRepo
          ? "border-[#a78bfa55] bg-[#a78bfa18] text-[#a78bfa]"
          : "border-border bg-surface-2 text-text-2",
      )}
      title={id}
    >
      {repo && crossRepo && (
        <span className="text-[10px] opacity-70">{repo}/</span>
      )}
      <span className="font-mono truncate max-w-[120px]">{name}</span>
    </span>
  );
}

// ---------------------------------------------------------------------------
// § FlowEdge
// ---------------------------------------------------------------------------

function FlowEdge({ type }: { type: "solid" | "dashed" | "dashed-violet" }) {
  const isDashed = type === "dashed" || type === "dashed-violet";
  const color = type === "dashed-violet" ? "#a78bfa" : "var(--border-strong)";
  return (
    <svg
      width={32}
      height={16}
      viewBox="0 0 32 16"
      aria-hidden
      className="shrink-0"
    >
      <line
        x1={0}
        y1={8}
        x2={28}
        y2={8}
        stroke={color}
        strokeWidth={1.5}
        strokeDasharray={isDashed ? "4 3" : undefined}
      />
      <polygon points="28,4 32,8 28,12" fill={color} />
    </svg>
  );
}

// ---------------------------------------------------------------------------
// § FlowUnit — per-channel mini-Sankey card
// ---------------------------------------------------------------------------

type FlatChannel = TopologyChannel & { lifecycle_state: ChannelLifecycle };

function FlowUnit({
  channel,
  selected,
  onClick,
}: {
  channel: FlatChannel;
  selected: boolean;
  onClick: () => void;
}) {
  const m = brokerMeta(channel.broker_canonical);
  const producerList = channel.producers ?? [];
  const consumerList = channel.consumers ?? [];
  const producers = producerList.slice(0, 3);
  const producerOverflow = producerList.length - 3;
  const consumers = consumerList.slice(0, 3);
  const consumerOverflow = consumerList.length - 3;
  const hasProducers = producerList.length > 0;
  const hasConsumers = consumerList.length > 0;

  const leftEdge = hasProducers ? "solid" : "dashed";
  const rightEdge = hasConsumers ? "solid" : "dashed";

  const lifecycleOutline =
    channel.lifecycle_state === "orphan_publisher" ||
    channel.lifecycle_state === "orphan_subscriber"
      ? "ring-1 ring-amber-400/50"
      : channel.lifecycle_state === "orphan"
        ? "ring-1 ring-slate-500/40 ring-dashed"
        : "";

  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "w-full text-left px-3 py-2 rounded-lg border transition-all duration-100",
        selected
          ? "border-accent bg-accent-soft/20"
          : "border-border bg-surface hover:bg-surface-2 hover:border-border-strong",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
      )}
      aria-selected={selected}
    >
      <div className="flex items-center gap-2 min-h-[40px]">
        {/* Producers */}
        <div className="flex flex-col gap-1 w-[130px] shrink-0 items-end">
          {!hasProducers ? (
            <span className="text-xs text-text-4 italic">no publisher</span>
          ) : (
            <>
              {producers.map((p) => (
                <EntityChip key={p} id={p} crossRepo={channel.cross_repo} />
              ))}
              {producerOverflow > 0 && (
                <span className="text-xs text-text-3">+{producerOverflow} more</span>
              )}
            </>
          )}
        </div>

        <FlowEdge type={leftEdge} />

        {/* Channel node */}
        <div className="flex flex-col items-center gap-1 shrink-0 min-w-[100px]">
          <div className={cn("rounded-md p-0.5", lifecycleOutline)}>
            <BrokerShapeIcon canonical={channel.broker_canonical} size={28} />
          </div>
          <span className="font-mono text-[11px] text-text max-w-[100px] truncate text-center leading-tight">
            {channel.label}
          </span>
          <div className="flex flex-wrap gap-1 justify-center">
            <span
              className="inline-flex items-center h-4 px-1.5 rounded text-[10px] font-medium"
              style={{ color: m.color, background: m.bgColor }}
            >
              {channel.broker_canonical}
            </span>
            {channel.cross_repo && (
              <span className="inline-flex items-center h-4 px-1.5 rounded text-[10px] font-medium border-dashed border border-[#a78bfa] text-[#a78bfa]">
                cross-repo
              </span>
            )}
            {channel.scheduled && (
              <span className="inline-flex items-center gap-0.5 h-4 px-1.5 rounded text-[10px] font-medium bg-yellow-950/30 text-yellow-300 border border-yellow-700/40">
                <Clock size={9} />
                {channel.schedule ?? "scheduled"}
              </span>
            )}
          </div>
        </div>

        <FlowEdge type={rightEdge} />

        {/* Consumers */}
        <div className="flex flex-col gap-1 w-[130px] shrink-0 items-start">
          {!hasConsumers ? (
            <span className="text-xs text-text-4 italic">no subscriber</span>
          ) : (
            <>
              {consumers.map((c) => (
                <EntityChip key={c} id={c} crossRepo={channel.cross_repo} />
              ))}
              {consumerOverflow > 0 && (
                <span className="text-xs text-text-3">+{consumerOverflow} more</span>
              )}
            </>
          )}
        </div>

        {/* Lifecycle chip */}
        <div className="ml-auto pl-2 shrink-0">
          {channel.lifecycle_state !== "active" && (
            <LifecycleChip state={channel.lifecycle_state} />
          )}
        </div>
      </div>
    </button>
  );
}

// ---------------------------------------------------------------------------
// § BrokerBand
// ---------------------------------------------------------------------------

function relativeTime(ts?: string): string | null {
  if (!ts) return null;
  const diff = Date.now() - new Date(ts).getTime();
  const min = Math.floor(diff / 60000);
  if (min < 1) return "just now";
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  return `${Math.floor(hr / 24)}d ago`;
}

function BrokerBand({
  brokerGroup,
  channels,
  selectedId,
  onSelect,
}: {
  brokerGroup: TopologyBrokerGroup;
  channels: FlatChannel[];
  selectedId: string | null;
  onSelect: (ch: FlatChannel) => void;
}) {
  const [open, setOpen] = useState(true);
  const m = brokerMeta(brokerGroup.broker);

  const isDegradedCelery =
    brokerGroup.broker === "celery" &&
    channels.every(
      (ch) => (ch.producers?.length ?? 0) === 0 && (ch.consumers?.length ?? 0) === 0,
    );

  const ts = relativeTime(brokerGroup.last_index_timestamp);
  const hs = brokerGroup.health_summary ?? {
    active: 0,
    orphan_publisher: 0,
    orphan_subscriber: 0,
  };

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
        <div className="ml-auto flex items-center gap-2 text-xs">
          {hs.active > 0 && <span className="text-[var(--success)]">{hs.active} active</span>}
          {hs.orphan_publisher > 0 && (
            <span className="text-amber-400">{hs.orphan_publisher} orphan-pub</span>
          )}
          {hs.orphan_subscriber > 0 && (
            <span className="text-orange-400">{hs.orphan_subscriber} orphan-sub</span>
          )}
          {ts && <span className="text-text-4 ml-1">indexed {ts}</span>}
        </div>
      </button>

      {open && (
        <div className="p-3 space-y-2 bg-bg">
          {isDegradedCelery && (
            <div className="flex items-start gap-2 px-3 py-2 rounded-lg border border-amber-800/40 bg-amber-950/20 text-amber-300 text-sm italic">
              <AlertCircle size={14} className="mt-0.5 shrink-0" />
              <span>
                Producer/consumer edges aren&apos;t indexed for Celery yet — these channels are
                real but their wiring is unknown.
              </span>
            </div>
          )}
          {channels.map((ch) => (
            <FlowUnit
              key={ch.id}
              channel={ch}
              selected={selectedId === ch.id}
              onClick={() => onSelect(ch)}
            />
          ))}
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
}: {
  channels: FlatChannel[];
  brokerGroups: TopologyBrokerGroup[];
  selectedId: string | null;
  onSelect: (ch: FlatChannel) => void;
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

      {/* Broker bands */}
      {brokerGroups.map((bg) => {
        const bandChannels = channels.filter((ch) => ch.broker_canonical === bg.broker);
        if (bandChannels.length === 0) return null;
        return (
          <BrokerBand
            key={bg.broker}
            brokerGroup={bg}
            channels={bandChannels}
            selectedId={selectedId}
            onSelect={onSelect}
          />
        );
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § ListView
// ---------------------------------------------------------------------------

function ListRow({
  channel,
  selected,
  onClick,
}: {
  channel: FlatChannel;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "w-full flex items-center gap-3 px-3 py-2 rounded-md text-left transition-colors",
        selected
          ? "bg-accent-soft/20 border border-accent"
          : "border border-transparent hover:bg-surface-2",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
      )}
    >
      <BrokerShapeIcon canonical={channel.broker_canonical} size={18} />
      <span className="font-mono text-md text-text truncate flex-1">{channel.label}</span>
      <span className="text-xs text-text-3 shrink-0 hidden sm:flex items-center gap-1">
        <span>{channel.producers?.length ?? 0}</span>
        <span className="text-text-4">→</span>
        <BrokerShapeIcon canonical={channel.broker_canonical} size={12} />
        <span className="text-text-4">→</span>
        <span>{channel.consumers?.length ?? 0}</span>
      </span>
      <LifecycleChip state={channel.lifecycle_state} className="shrink-0" />
      <RepoChip repo={channel.repo} className="shrink-0" />
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
}: {
  label: string;
  count?: number;
  children?: React.ReactNode;
  empty?: string;
}) {
  return (
    <div className="border-t border-border pt-3 mt-3">
      <div className="flex items-center gap-1.5 mb-2 text-md font-medium text-text-2">
        <span>{label}</span>
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
            {repo && <RepoChip repo={repo} className="text-[10px]" />}
            <span className="font-mono text-text truncate">{name}</span>
          </div>
        );
      })}
    </div>
  );
}

/**
 * Renders a list of producer/consumer entries from the topic-detail endpoint.
 * The detail endpoint returns rich entity objects (name/kind/repo/source_file),
 * NOT plain entity-id strings — so we render each field explicitly (#1543).
 */
function EntityRefList({ entries }: { entries: (TopologyEntityRef | string)[] }) {
  if (!entries || entries.length === 0) return null;
  return (
    <div className="space-y-1">
      {entries.map((entry, i) => {
        if (typeof entry === "string") {
          // Fallback: plain id string (list-endpoint shape)
          const parts = (entry ?? "").split("::");
          const name = (parts[parts.length - 1] ?? "").split(":").pop() || entry;
          const repo = parts.length > 1 ? parts[0] : null;
          return (
            <div key={entry || i} className="flex items-center gap-2 text-sm">
              {repo && <RepoChip repo={repo} className="text-[10px]" />}
              <span className="font-mono text-text truncate">{name}</span>
            </div>
          );
        }
        // Rich entity object shape from the detail endpoint
        const fileBasename = (entry.source_file ?? "").split("/").pop() ?? "";
        return (
          <div key={entry.entity_id || i} className="flex items-center gap-2 text-sm min-w-0">
            {entry.repo && <RepoChip repo={entry.repo} className="text-[10px] shrink-0" />}
            <span className="font-mono text-text truncate flex-1" title={entry.name}>
              {entry.name}
            </span>
            {fileBasename && (
              <span
                className="text-[10px] text-text-4 font-mono shrink-0 truncate max-w-[140px]"
                title={`${entry.source_file}:${entry.start_line}`}
              >
                {fileBasename}:{entry.start_line}
              </span>
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
          <RepoChip repo={channel.repo} />
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
                <dd className="font-mono text-text-2 truncate">
                  {d.source_file}{d.start_line ? `:${d.start_line}` : ""}
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
          empty={
            producerEntries.length === 0
              ? "No publishers found in indexed code — this is the orphan-subscriber signal."
              : undefined
          }
        >
          {producerEntries.length > 0 ? <EntityRefList entries={producerEntries} /> : null}
        </DetailSection>

        {/* Subscribers */}
        <DetailSection
          label="Subscribers"
          count={consumerEntries.length}
          empty={
            consumerEntries.length === 0
              ? "No subscribers found in indexed code — this is the orphan-publisher signal."
              : undefined
          }
        >
          {consumerEntries.length > 0 ? <EntityRefList entries={consumerEntries} /> : null}
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
        {d?.source_file && (
          <Button variant="ghost" size="sm" className="gap-1.5 text-xs">
            <ExternalLink size={11} />
            Open source
          </Button>
        )}
      </div>
    </aside>
  );
}

// ---------------------------------------------------------------------------
// § Orphan tabs
// ---------------------------------------------------------------------------

function OrphanPublisherTab({ groupId }: { groupId: string }) {
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
  const entries = (
    Array.isArray(data)
      ? data
      : ((data as { orphan_publishers?: OrphanPublisherEntry[] } | undefined)
          ?.orphan_publishers ?? [])
  ) as OrphanPublisherEntry[];
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
            className="flex items-center gap-3 px-3 py-2 rounded-lg border border-border bg-surface"
          >
            <BrokerShapeIcon canonical={brokerKey} size={18} />
            <span className="font-mono text-md text-text truncate flex-1">{e.label}</span>
            <div className="flex gap-1 flex-wrap">
              {(e.producers ?? []).slice(0, 3).map((p) => (
                <EntityChip key={p} id={p} />
              ))}
              {(e.producers?.length ?? 0) > 3 && (
                <span className="text-xs text-text-3">+{(e.producers?.length ?? 0) - 3}</span>
              )}
            </div>
            <span className="text-xs px-2 py-0.5 rounded-full bg-amber-950/30 text-amber-300 border border-amber-700/40 shrink-0">
              no subscriber found
            </span>
            <RepoChip repo={e.repo} />
          </div>
        );
      })}
    </div>
  );
}

function OrphanSubscriberTab({ groupId }: { groupId: string }) {
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
  const entries = (
    Array.isArray(data)
      ? data
      : ((data as { orphan_subscribers?: OrphanSubscriberEntry[] } | undefined)
          ?.orphan_subscribers ?? [])
  ) as OrphanSubscriberEntry[];
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
            className="flex items-center gap-3 px-3 py-2 rounded-lg border border-border bg-surface"
          >
            <BrokerShapeIcon canonical={brokerKey} size={18} />
            <span className="font-mono text-md text-text truncate flex-1">{e.label}</span>
            <div className="flex gap-1 flex-wrap">
              {(e.consumers ?? []).slice(0, 3).map((c) => (
                <EntityChip key={c} id={c} />
              ))}
              {(e.consumers?.length ?? 0) > 3 && (
                <span className="text-xs text-text-3">+{(e.consumers?.length ?? 0) - 3}</span>
              )}
            </div>
            <span
              className={cn(
                "text-xs px-2 py-0.5 rounded-full shrink-0",
                e.reason === "publisher_only_in_external_lib"
                  ? "bg-slate-900/30 text-slate-400 border border-dashed border-slate-600"
                  : "bg-amber-950/30 text-amber-300 border border-amber-700/40",
              )}
            >
              {e.reason === "publisher_only_in_external_lib"
                ? "publisher in external lib"
                : "no publisher found"}
            </span>
            <RepoChip repo={e.repo} />
          </div>
        );
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § Scheduled tab
// ---------------------------------------------------------------------------

function ScheduledTab({ channels }: { channels: FlatChannel[] }) {
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
          className="flex items-center gap-3 px-3 py-2.5 rounded-lg border border-border bg-surface"
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
          <RepoChip repo={ch.repo} />
        </div>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § TopologyScreen — main export
// ---------------------------------------------------------------------------

export default function TopologyScreen() {
  const { groupId = "" } = useParams<{ groupId: string }>();
  const [searchParams, setSearchParams] = useSearchParams();

  const tab = (searchParams.get("tab") ?? "all") as
    | "all"
    | "orphan-publishers"
    | "orphan-subscribers"
    | "scheduled";
  const channelParam = searchParams.get("channel");

  const [viewMode, setViewMode] = useState<"map" | "list">("map");
  const [search, setSearch] = useState("");
  const [activeBrokers, setActiveBrokers] = useState<Set<string>>(new Set());
  const [selectedId, setSelectedId] = useState<string | null>(channelParam);
  const searchRef = useRef<HTMLInputElement>(null);

  const { data, isLoading, isError } = useTopology(groupId);
  const channels = flattenChannels(data);
  const counts = deriveCounts(channels, data?.broker_groups ?? []);
  const brokerGroups = data?.broker_groups ?? [];

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

  const selectedChannel = selectedId
    ? channels.find((ch) => ch.id === selectedId) ?? null
    : null;

  function selectChannel(ch: FlatChannel) {
    setSelectedId(ch.id);
    setSearchParams((prev) => {
      const n = new URLSearchParams(prev);
      n.set("channel", ch.id);
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
        <TopBar group={groupId} surfaceLabel="Topology" />
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
      <TopBar group={groupId} surfaceLabel="Topology" />

      <Tabs value={tab} onValueChange={setTab} className="flex flex-col flex-1 min-h-0">
        {/* Tab strip */}
        <div className="border-b border-border shrink-0 px-4">
          <TabsList className="border-0">
            <TabsTrigger value="all">
              All
              {isLoading ? (
                <span className="ml-1.5 text-text-4">…</span>
              ) : (
                <Pill className="ml-1.5">{counts.total}</Pill>
              )}
            </TabsTrigger>
            <TabsTrigger value="orphan-publishers">
              Orphan pub
              {isLoading ? (
                <span className="ml-1.5 text-text-4">…</span>
              ) : counts.orphanPub > 0 ? (
                <Pill className="ml-1.5 bg-amber-950 text-amber-300 border-amber-700">
                  {counts.orphanPub}
                </Pill>
              ) : (
                <Pill className="ml-1.5">0</Pill>
              )}
            </TabsTrigger>
            <TabsTrigger value="orphan-subscribers">
              Orphan sub
              {isLoading ? (
                <span className="ml-1.5 text-text-4">…</span>
              ) : counts.orphanSub > 0 ? (
                <Pill className="ml-1.5 bg-orange-950 text-orange-300 border-orange-700">
                  {counts.orphanSub}
                </Pill>
              ) : (
                <Pill className="ml-1.5">0</Pill>
              )}
            </TabsTrigger>
            <TabsTrigger value="scheduled">
              Scheduled
              {isLoading ? (
                <span className="ml-1.5 text-text-4">…</span>
              ) : (
                <Pill className="ml-1.5">{counts.scheduled}</Pill>
              )}
            </TabsTrigger>
          </TabsList>
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
                <Map size={13} />
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
            </div>
          </div>

          {/* Workspace */}
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
        </TabsContent>

        {/* Orphan publishers */}
        <TabsContent
          value="orphan-publishers"
          className="flex-1 min-h-0 overflow-y-auto ag-scroll mt-0"
        >
          <OrphanPublisherTab groupId={groupId} />
        </TabsContent>

        {/* Orphan subscribers */}
        <TabsContent
          value="orphan-subscribers"
          className="flex-1 min-h-0 overflow-y-auto ag-scroll mt-0"
        >
          <OrphanSubscriberTab groupId={groupId} />
        </TabsContent>

        {/* Scheduled */}
        <TabsContent
          value="scheduled"
          className="flex-1 min-h-0 overflow-y-auto ag-scroll mt-0"
        >
          <ScheduledTab channels={channels} />
        </TabsContent>
      </Tabs>
    </div>
  );
}
