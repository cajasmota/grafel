/* ============================================================
   Flows — Process flow explorer.

   Layered DAG per process flow: steps/edges, multi-service traces,
   saga forward + compensation. Statically derived from code; no runtime
   data (no latency, throughput, error rate, timestamps).

   Layout:
     Tab strip (All / Cross-repo / Dead-ends / Truncated)
     Workspace: 400px list rail | flexible detail panel
       - at <1180px: detail stacks over list (stack navigator)

   Data: useFlows / useFlowDeadEnds / useFlowDetail hooks
         -> /api/flows/:group (v1, no wrapper needed for frontend)

   Per docs/screens/flows.md + design_handoff_archigraph/prototypes/.
   ============================================================ */

import { useState, useMemo, useEffect, useRef, useCallback } from "react";
import { useParams, useSearchParams } from "react-router-dom";
import {
  Globe, Database, Send, ArrowDownToLine, Wrench, Shield, AlertTriangle,
  Package, CheckCircle2, Layout, Terminal, Clock, TestTube2, Wifi,
  ChevronRight, Search, X, Copy, Share2, ExternalLink, ZoomIn, ZoomOut,
  Maximize2, Link2, Sparkles, Info, Check, ArrowRight, Rows2, Grid2x2,
  Play, Pause, Square, Volume2, VolumeX, Gauge,
  type LucideProps,
} from "lucide-react";
import type { ForwardRefExoticComponent, RefAttributes } from "react";
import { toast } from "sonner";

import { useFlows, useFlowDeadEnds, useFlowTruncated, useFlowDetail } from "@/hooks/use-flows";
import type {
  Process, ProcessStep, FlowDeadEnd, EntryKind, StepKind, EntryKindGroup,
} from "@/data/types";
import { cn } from "@/lib/utils";
import { RepoChip as SharedRepoChip } from "@/lib/repo-color";
import { Skeleton } from "@/components/ui/skeleton";
import {
  createFlowAnim, useFlowAnim,
  type FlowAnimController, type FlowAnimSnapshot,
} from "@/lib/flow-animation";
import { pointAt, type Pt } from "@/lib/edge-geometry";
import {
  playStepBlip, readFlowAudio, writeFlowAudio,
} from "@/lib/flow-audio";
import { usePrefersReducedMotion } from "@/lib/use-reduced-motion";

// ─── Step-kind metadata ───────────────────────────────────────────────────────

type IconComp = ForwardRefExoticComponent<Omit<LucideProps, "ref"> & RefAttributes<SVGSVGElement>>;

const STEP_META: Record<string, { label: string; color: string; Icon: IconComp }> = {
  http_fetch:       { label: "HTTP Fetch",   color: "#60a5fa", Icon: Globe },
  db_query:         { label: "DB Query",     color: "#2dd4bf", Icon: Database },
  db_write:         { label: "DB Write",     color: "#fbbf24", Icon: Database },
  message_publish:  { label: "Publish",      color: "#c084fc", Icon: Send },
  message_consume:  { label: "Consume",      color: "#818cf8", Icon: ArrowDownToLine },
  transform:        { label: "Transform",    color: "#94a3b8", Icon: Wrench },
  validation:       { label: "Validation",   color: "#4ade80", Icon: Shield },
  side_effect:      { label: "Side Effect",  color: "#fb923c", Icon: AlertTriangle },
  external_lib:     { label: "External Lib", color: "#64748b", Icon: Package },
  test_assert:      { label: "Assert",       color: "#a3e635", Icon: CheckCircle2 },
  component_render: { label: "Render",       color: "#f472b6", Icon: Layout },
  render:           { label: "Render",       color: "#f472b6", Icon: Layout },
  function_call:    { label: "Function",     color: "#94a3b8", Icon: Terminal },
  unknown:          { label: "Unknown",      color: "#94a3b8", Icon: Terminal },
};

const ENTRY_META: Record<string, { label: string; Icon: IconComp }> = {
  http_handler:     { label: "HTTP Handler",     Icon: Globe },
  message_consumer: { label: "Message Consumer", Icon: ArrowDownToLine },
  kafka_consumer:   { label: "Kafka Consumer",   Icon: ArrowDownToLine },
  scheduled_task:   { label: "Scheduled Job",    Icon: Clock },
  component_render: { label: "Component Render", Icon: Layout },
  test:             { label: "Test",             Icon: TestTube2 },
  cli_command:      { label: "CLI Command",       Icon: Terminal },
  ws_handler:       { label: "WebSocket",        Icon: Wifi },
  function:         { label: "Function",          Icon: Terminal },
};

// ─── Helpers ──────────────────────────────────────────────────────────────────

function getHttpVerb(label: string): string | null {
  const m = /^http:(\w+):/i.exec(label ?? "");
  return m ? m[1].toUpperCase() : null;
}

function getStepMeta(sk?: StepKind | string) {
  return STEP_META[sk ?? "unknown"] ?? STEP_META.unknown;
}

function getEntryMeta(ek?: EntryKind | string) {
  return ENTRY_META[ek ?? "function"] ?? ENTRY_META.function;
}

// The detail endpoint serves the qualified entity name as `label`; older builds
// used `name`. Prefer whichever is populated so step nodes are always readable.
function stepName(s: ProcessStep): string {
  return s.name || s.label || s.entity_id || "step";
}

// ─── PathString — highlights {segment} patterns amber ────────────────────────

function PathString({ text }: { text: string }) {
  const re = /(\$\{[^}]+\}|\{[^}]+\})/g;
  const parts = text.split(re);
  return (
    <>
      {parts.map((p, i) =>
        /(\$\{[^}]+\}|\{[^}]+\})/.test(p) ? (
          <span
            key={i}
            className="px-0.5 rounded-xs font-mono"
            style={{
              color: "var(--pastel-3-ink)",
              background: "color-mix(in srgb, var(--pastel-3) 24%, transparent)",
            }}
          >
            {p}
          </span>
        ) : (
          <span key={i}>{p}</span>
        ),
      )}
    </>
  );
}

// ─── FlowLabel — entry → terminal with dim arrow ─────────────────────────────

function FlowLabel({ label }: { label?: string | null }) {
  // Dead-end items have a sparser shape and may carry a null/undefined label.
  // Guard so the Dead-ends tab never crashes on `.split` of undefined.
  const safe = (label ?? "").trim();
  if (!safe) {
    return <span className="text-text-4 italic font-sans">unnamed flow</span>;
  }
  const parts = safe.split(" → ");
  return (
    <>
      {parts.map((p, i) => (
        <span key={i} className="inline-flex items-center gap-0.5">
          <PathString text={p} />
          {i < parts.length - 1 && (
            <span className="text-text-4 text-[10px] mx-1 font-normal">→</span>
          )}
        </span>
      ))}
    </>
  );
}

// ─── Chip variants ────────────────────────────────────────────────────────────

// RepoChip now delegates to the shared repo-color resolver (#1946).
function RepoChip({ name }: { name: string }) {
  return <SharedRepoChip slug={name} />;
}

function CrossRepoChip() {
  return (
    <span
      className="inline-flex items-center h-[18px] px-1.5 rounded-xs font-mono text-[10px]"
      style={{ color: "#a78bfa", background: "color-mix(in srgb, #a78bfa 14%, transparent)" }}
    >
      cross-repo
    </span>
  );
}

function CrossStackChip() {
  return (
    <span
      className="inline-flex items-center h-[18px] px-1.5 rounded-xs font-mono text-[10px]"
      style={{ color: "#a78bfa", background: "color-mix(in srgb, #a78bfa 14%, transparent)" }}
    >
      cross-stack
    </span>
  );
}

function PriorityDot({ hint }: { hint?: "high" | "medium" | "low" }) {
  const color =
    hint === "high" ? "var(--danger)"
    : hint === "medium" ? "var(--warning)"
    : "var(--text-4)";
  return (
    <span
      className="inline-block w-1.5 h-1.5 rounded-full flex-none"
      style={{ background: color }}
      title={`priority: ${hint ?? "low"}`}
    />
  );
}

function AIChip() {
  return (
    <span
      className="inline-flex items-center gap-0.5 h-[18px] px-1.5 rounded-xs text-[10px]"
      style={{ color: "var(--info)", background: "var(--info-soft)" }}
    >
      <Sparkles size={9} />
      AI
    </span>
  );
}

// ─── Skeleton ─────────────────────────────────────────────────────────────────

function SkeletonRow() {
  return (
    <div className="flex gap-2.5 px-3.5 py-2.5 border-b border-border-soft">
      <Skeleton w="w-[22px]" h="h-[22px]" className="rounded-xs flex-none" />
      <div className="flex-1 flex flex-col gap-1.5">
        <Skeleton w="w-3/4" />
        <Skeleton w="w-1/2" h="h-2.5" />
      </div>
    </div>
  );
}

// ─── Flow row ─────────────────────────────────────────────────────────────────

function FlowRow({
  flow,
  selected,
  onClick,
}: {
  flow: Process;
  selected: boolean;
  onClick: () => void;
}) {
  const { Icon } = getEntryMeta(flow.entry_kind);
  const isEnriched = flow.docgen_status === "enriched";
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "w-full text-left px-3.5 py-2.5 border-b border-border-soft relative",
        "focus-visible:outline-none focus-visible:ring-inset focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
        "hover:bg-surface-2 transition-colors duration-100",
        selected ? "bg-accent-soft" : "",
      )}
      style={{ display: "grid", gridTemplateColumns: "22px 1fr", gap: 10 }}
    >
      {selected && (
        <span className="absolute left-0 top-0 bottom-0 w-[3px] bg-accent rounded-r-sm" />
      )}
      <span className="w-[22px] h-[22px] rounded-xs flex items-center justify-center text-text-3 bg-surface-2 border border-border flex-none">
        <Icon size={12} />
      </span>
      <div className="min-w-0 flex flex-col gap-1">
        <div className="font-mono text-[12px] text-text leading-snug flex flex-wrap items-center gap-1">
          <FlowLabel label={flow.label} />
        </div>
        <div className="flex flex-wrap gap-1.5 items-center">
          <RepoChip name={flow.repo} />
          <span className="font-mono text-[10px] text-text-3">{flow.step_count} steps</span>
          {flow.cross_stack && <CrossStackChip />}
          {!flow.cross_stack && flow.is_cross_repo && <CrossRepoChip />}
          {flow.crosses_external_lib && (
            <span className="inline-flex items-center h-[18px] px-1.5 rounded-xs bg-surface-2 border border-dashed border-border font-mono text-[10px] text-text-3">
              external lib
            </span>
          )}
          {flow.terminal_is_phantom && (
            <span
              className="inline-flex h-[18px] px-1.5 rounded-xs font-mono text-[10px]"
              style={{ color: "var(--warning)", background: "color-mix(in srgb, var(--warning) 14%, transparent)" }}
            >
              phantom
            </span>
          )}
          {typeof flow.complexity_score === "number" && (
            <span className="font-mono text-[10px] text-text-4">
              complexity {flow.complexity_score.toFixed(1)}
            </span>
          )}
          <PriorityDot hint={flow.priority_hint} />
          {isEnriched && <AIChip />}
          {flow.docgen_status === "stale" && (
            <span className="font-mono text-[10px]" style={{ color: "var(--warning)" }}>
              stale insights
            </span>
          )}
        </div>
      </div>
    </button>
  );
}

// ─── Group block ──────────────────────────────────────────────────────────────

function GroupBlock({
  groupKind,
  flows,
  selectedId,
  onSelect,
}: {
  groupKind: string;
  flows: Process[];
  selectedId: string | null;
  onSelect: (f: Process) => void;
}) {
  const [open, setOpen] = useState(true);
  const { label, Icon } = getEntryMeta(groupKind);
  return (
    <div>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="sticky top-0 z-10 w-full text-left flex items-center gap-2 px-3.5 py-2 border-b border-border-soft bg-bg-soft font-semibold text-[11px] text-text-3 uppercase tracking-wider"
      >
        <ChevronRight
          size={11}
          className={cn("text-text-3 transition-transform duration-150", open && "rotate-90")}
        />
        <Icon size={11} />
        <span>{label}</span>
        <span className="ml-auto font-mono text-[10px] text-text-4">{flows.length}</span>
      </button>
      {open &&
        flows.map((f) => (
          <FlowRow
            key={f.process_id}
            flow={f}
            selected={selectedId === f.process_id}
            onClick={() => onSelect(f)}
          />
        ))}
    </div>
  );
}

// ─── Dead-end row ─────────────────────────────────────────────────────────────

const DEAD_END_REASON_LABEL: Record<string, string> = {
  no_useful_sink: "No useful sink",
  single_step: "Single-step",
  unresolved_callee: "Unresolved callee",
  phantom_terminal: "Phantom terminal",
  dead_end: "Dead end",
};

function DeadEndRow({
  de,
  selected,
  onClick,
}: {
  de: FlowDeadEnd;
  selected: boolean;
  onClick: () => void;
}) {
  const isExt = de.dead_end_step_id?.includes("::ext:");
  const reasonLabel = DEAD_END_REASON_LABEL[de.reason] ?? de.reason;
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "w-full text-left px-3.5 py-2.5 border-b border-border-soft relative",
        "focus-visible:outline-none focus-visible:ring-inset focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
        "hover:bg-surface-2 transition-colors duration-100",
        selected ? "bg-accent-soft" : "",
      )}
      style={{ display: "grid", gridTemplateColumns: "22px 1fr", gap: 10 }}
    >
      {selected && (
        <span className="absolute left-0 top-0 bottom-0 w-[3px] bg-accent rounded-r-sm" />
      )}
      <span
        className="w-[22px] h-[22px] flex items-center justify-center flex-none"
        style={{ color: "var(--warning)" }}
      >
        <AlertTriangle size={13} />
      </span>
      <div className="min-w-0 flex flex-col gap-1">
        <div className="font-mono text-[12px] text-text leading-snug flex flex-wrap items-center gap-1">
          <FlowLabel label={de.process_name} />
        </div>
        <div className="flex flex-wrap gap-1.5 items-center">
          <RepoChip name={de.repo} />
          <span className="font-mono text-[10px] text-text-3">
            {de.step_count} step{de.step_count === 1 ? "" : "s"}
          </span>
          <span
            className="inline-flex h-[18px] px-1.5 rounded-xs font-mono text-[10px] font-semibold"
            style={{ color: "var(--warning)", background: "color-mix(in srgb, var(--warning) 14%, transparent)" }}
          >
            {reasonLabel}
          </span>
          {de.dead_end_step_name && (
            <span
              className={cn(
                "inline-flex items-center gap-1 h-[18px] px-1.5 rounded-xs font-mono text-[10px]",
                isExt
                  ? "border border-dashed border-border text-text-3"
                  : "bg-surface-2 border border-border text-text-3",
              )}
            >
              died at <span className="text-text ml-1">{de.dead_end_step_name}</span>
              {isExt && <span className="text-text-4 ml-1">external</span>}
            </span>
          )}
          {de.cross_stack && <CrossStackChip />}
        </div>
      </div>
    </button>
  );
}

// ─── List rail ────────────────────────────────────────────────────────────────

type SortKey = "steps" | "complexity" | "kind";
type TabKey = "all" | "crossrepo" | "deadends" | "truncated";

function ListRail({
  groupId,
  tab,
  selectedId,
  onSelectFlow,
  onSelectDeadEnd,
  counts,
}: {
  groupId: string;
  tab: TabKey;
  selectedId: string | null;
  onSelectFlow: (f: Process) => void;
  onSelectDeadEnd: (de: FlowDeadEnd) => void;
  counts: { all: number; crossrepo: number; deadends: number; truncated: number };
}) {
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<SortKey>("steps");
  const [kindFilter, setKindFilter] = useState("all");
  const [showSingleStep, setShowSingleStep] = useState(false);
  const [crossStackOnly, setCrossStackOnly] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);

  const flowsQ = useFlows(groupId, tab, search);
  const deadEndsQ = useFlowDeadEnds(groupId, tab === "deadends");
  const truncatedQ = useFlowTruncated(groupId, tab === "truncated");

  // "/" key focuses search
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (
        e.key === "/" &&
        !(e.target instanceof HTMLInputElement) &&
        !(e.target instanceof HTMLTextAreaElement)
      ) {
        e.preventDefault();
        searchRef.current?.focus();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  const flows = useMemo<Process[]>(() => {
    const raw = flowsQ.data?.processes ?? [];
    let arr = [...raw];
    if (tab === "crossrepo") arr = arr.filter((f) => f.is_cross_repo || f.cross_stack);
    if (crossStackOnly && tab === "all") arr = arr.filter((f) => f.cross_stack);
    if (kindFilter && kindFilter !== "all") arr = arr.filter((f) => f.entry_kind === kindFilter);
    if (search) {
      const q = search.toLowerCase();
      arr = arr.filter(
        (f) =>
          f.entry_name.toLowerCase().includes(q) || f.label.toLowerCase().includes(q),
      );
    }
    if (sort === "steps") arr.sort((a, b) => b.step_count - a.step_count);
    if (sort === "complexity")
      arr.sort((a, b) => (b.complexity_score ?? 0) - (a.complexity_score ?? 0));
    if (sort === "kind")
      arr.sort((a, b) => (a.entry_kind ?? "").localeCompare(b.entry_kind ?? ""));
    return arr;
  }, [flowsQ.data, tab, search, sort, kindFilter, crossStackOnly]);

  const groups = useMemo(() => {
    if (tab !== "all" || kindFilter !== "all") return [{ kind: "_flat", flows }];
    const m = new Map<string, Process[]>();
    for (const f of flows) {
      const k = f.entry_kind ?? "function";
      if (!m.has(k)) m.set(k, []);
      m.get(k)!.push(f);
    }
    return Array.from(m.entries()).map(([kind, fl]) => ({ kind, flows: fl }));
  }, [flows, tab, kindFilter]);

  const deadEnds = useMemo(() => {
    let arr = deadEndsQ.data?.dead_ends ?? [];
    if (!showSingleStep) arr = arr.filter((d) => d.reason !== "single_step");
    if (search) {
      const q = search.toLowerCase();
      arr = arr.filter((d) =>
        (d.process_name ?? "").toLowerCase().includes(q) ||
        d.repo.toLowerCase().includes(q),
      );
    }
    return arr;
  }, [deadEndsQ.data, showSingleStep, search]);

  const truncated = useMemo(() => truncatedQ.data?.processes ?? [], [truncatedQ.data]);

  const entryKindGroups = flowsQ.data?.entry_kind_groups ?? [];

  const isLoading =
    ((tab === "all" || tab === "crossrepo") && flowsQ.isLoading) ||
    (tab === "deadends" && deadEndsQ.isLoading) ||
    (tab === "truncated" && truncatedQ.isLoading);

  const searchPlaceholder =
    tab === "deadends"
      ? "Search dead-ends…"
      : tab === "truncated"
        ? "Search truncated…"
        : "Search by entry or label…";

  const listCount =
    tab === "deadends"
      ? deadEnds.length
      : tab === "truncated"
        ? truncated.length
        : flows.length;

  return (
    <aside
      className="flex flex-col h-full border-r border-border bg-bg-soft min-h-0 min-w-0"
      style={{ overflow: "hidden" }}
    >
      {/* Toolbar */}
      <div className="flex flex-col gap-2 px-3.5 py-3 border-b border-border-soft bg-bg-soft flex-none">
        {/* Search */}
        <div
          className="flex items-center gap-2 h-8 px-2.5 rounded-sm border border-border bg-surface text-text-3 transition-shadow"
          style={{ "--tw-ring-shadow": "none" } as React.CSSProperties}
        >
          <Search size={12} className="flex-none" />
          <input
            ref={searchRef}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={searchPlaceholder}
            className="flex-1 min-w-0 border-0 outline-none bg-transparent text-text text-sm placeholder:text-text-4"
          />
          <span className="font-mono text-[10px] text-text-3 whitespace-nowrap">{listCount}</span>
          <kbd className="inline-flex items-center justify-center h-5 px-1.5 rounded-xs bg-surface-2 border border-border font-mono text-[10px] text-text-3">
            /
          </kbd>
        </div>

        {/* Sort (All + Cross-repo only) */}
        {(tab === "all" || tab === "crossrepo") && (
          <div
            className="inline-flex bg-surface-2 border border-border rounded-sm overflow-hidden h-6 self-start"
            role="group"
            aria-label="Sort"
          >
            {(["steps", "complexity", "kind"] as SortKey[]).map((s) => (
              <button
                key={s}
                type="button"
                onClick={() => setSort(s)}
                className={cn(
                  "px-2 text-[10px] font-medium uppercase tracking-wide border-r last:border-0 border-border",
                  sort === s
                    ? "bg-surface text-text shadow-[inset_0_-2px_0_var(--accent)]"
                    : "text-text-3 hover:text-text",
                )}
              >
                {{ steps: "Steps", complexity: "Complexity", kind: "Kind" }[s]}
              </button>
            ))}
          </div>
        )}

        {/* Entry-kind chips (All tab only) */}
        {tab === "all" && (
          <>
            <div className="flex flex-wrap gap-1">
              <button
                type="button"
                onClick={() => setKindFilter("all")}
                className={cn(
                  "inline-flex items-center gap-1 h-[22px] px-2 rounded-full text-[10px] font-medium border",
                  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
                  kindFilter === "all"
                    ? "bg-accent-soft text-accent-strong border-transparent"
                    : "bg-surface-2 border-border text-text-3 hover:text-text hover:bg-surface-3",
                )}
              >
                All
                <span className="font-mono text-[9px] text-text-4">{counts.all}</span>
              </button>
              {(entryKindGroups as EntryKindGroup[]).map((g) => {
                const { label, Icon } = getEntryMeta(g.kind);
                return (
                  <button
                    key={g.kind}
                    type="button"
                    onClick={() => setKindFilter(kindFilter === g.kind ? "all" : g.kind)}
                    className={cn(
                      "inline-flex items-center gap-1 h-[22px] px-2 rounded-full text-[10px] font-medium border",
                      "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
                      kindFilter === g.kind
                        ? "bg-accent-soft text-accent-strong border-transparent"
                        : "bg-surface-2 border-border text-text-3 hover:text-text hover:bg-surface-3",
                    )}
                  >
                    <Icon size={10} />
                    <span>{label}</span>
                    <span className="font-mono text-[9px] text-text-4">{g.count}</span>
                  </button>
                );
              })}
            </div>
            {/* Cross-stack toggle */}
            <button
              type="button"
              onClick={() => setCrossStackOnly((v) => !v)}
              className="inline-flex items-center gap-1.5 text-[11px] text-text-3 cursor-pointer self-start"
            >
              <span
                className={cn(
                  "w-3 h-3 rounded-[3px] inline-flex items-center justify-center border flex-none",
                  crossStackOnly
                    ? "bg-accent border-accent text-accent-text"
                    : "bg-surface border-border-strong",
                )}
              >
                {crossStackOnly && <Check size={8} />}
              </span>
              Cross-stack only
            </button>
          </>
        )}

        {/* Show single-step toggle (Dead-ends tab) */}
        {tab === "deadends" && (
          <button
            type="button"
            onClick={() => setShowSingleStep((v) => !v)}
            className="inline-flex items-center gap-1.5 text-[11px] text-text-3 cursor-pointer self-start"
          >
            <span
              className={cn(
                "w-3 h-3 rounded-[3px] inline-flex items-center justify-center border flex-none",
                showSingleStep
                  ? "bg-accent border-accent text-accent-text"
                  : "bg-surface border-border-strong",
              )}
            >
              {showSingleStep && <Check size={8} />}
            </span>
            Show single-step flows
          </button>
        )}
      </div>

      {/* List body */}
      <div className="flex-1 min-h-0 overflow-y-auto pb-20">
        {isLoading && (
          <>
            {[0, 1, 2, 3, 4].map((i) => (
              <SkeletonRow key={i} />
            ))}
          </>
        )}

        {/* Truncated positive empty state */}
        {!isLoading && tab === "truncated" && truncated.length === 0 && (
          <div className="flex flex-col items-center justify-center gap-3 py-16 px-6 text-center">
            <CheckCircle2 size={28} style={{ color: "var(--success)" }} />
            <h2 className="text-lg font-semibold text-text m-0">Everything resolves cleanly</h2>
            <p className="text-sm text-text-3 max-w-[380px] leading-relaxed m-0">
              No flows were truncated during extraction — the indexer reached a terminal for every
              walk in this group.
            </p>
          </div>
        )}

        {/* Cross-repo empty */}
        {!isLoading && tab === "crossrepo" && flows.length === 0 && !search && (
          <div className="flex flex-col items-center justify-center gap-3 py-16 px-6 text-center">
            <Globe size={28} className="text-text-4" />
            <h2 className="text-lg font-semibold text-text m-0">No cross-repo flows</h2>
            <p className="text-sm text-text-3 max-w-[380px] leading-relaxed m-0">
              No flows in this group span multiple repositories. That's fine in single-repo systems.
            </p>
          </div>
        )}

        {/* General no-match */}
        {!isLoading &&
          tab !== "truncated" &&
          ((tab === "deadends" && deadEnds.length === 0 && search) ||
            ((tab === "all" || tab === "crossrepo") &&
              flows.length === 0 &&
              (search || kindFilter !== "all"))) && (
          <div className="flex flex-col items-center justify-center gap-3 py-16 px-6 text-center">
            <Search size={28} className="text-text-4" />
            <h2 className="text-lg font-semibold text-text m-0">No flows match</h2>
            <p className="text-sm text-text-3 m-0">
              Try clearing the search box or removing the entry-kind filter.
            </p>
            {search && (
              <button
                type="button"
                onClick={() => setSearch("")}
                className="text-sm text-accent hover:text-accent-strong"
              >
                Clear search
              </button>
            )}
          </div>
        )}

        {/* Dead-ends list */}
        {!isLoading &&
          tab === "deadends" &&
          deadEnds.length > 0 &&
          deadEnds.map((de) => (
            <DeadEndRow
              key={de.process_id}
              de={de}
              selected={selectedId === de.process_id}
              onClick={() => onSelectDeadEnd(de)}
            />
          ))}

        {/* Flow list — grouped or flat */}
        {!isLoading &&
          (tab === "all" || tab === "crossrepo") &&
          flows.length > 0 &&
          (groups[0].kind === "_flat"
            ? flows.map((f) => (
                <FlowRow
                  key={f.process_id}
                  flow={f}
                  selected={selectedId === f.process_id}
                  onClick={() => onSelectFlow(f)}
                />
              ))
            : groups.map((g) => (
                <GroupBlock
                  key={g.kind}
                  groupKind={g.kind}
                  flows={g.flows}
                  selectedId={selectedId}
                  onSelect={onSelectFlow}
                />
              )))}
      </div>
    </aside>
  );
}

// ─── DAG layout constants ─────────────────────────────────────────────────────
//
// Three layout modes selected at render time by step count (adaptive default,
// from #1904) or by explicit user selection (#1907):
//   auto       — adaptive: short/medium/long based on step count
//   horizontal — single row, no wrapping
//   vertical   — single column
//   grid       — 3-column grid
//
// All sizing is expressed as a LayoutMode object so the SVG edge renderer
// and the absolute-positioned nodes always use the same values.

type LayoutMode = {
  nodeW: number;
  nodeH: number;
  hgap: number;
  vgap: number;
  pad: number;
  perLane: number;
};

// User-selectable layout preference ("auto" = adaptive default from #1904)
type UserLayout = "auto" | "horizontal" | "vertical" | "grid";

const LAYOUT_STORAGE_KEY = "archigraph:flows:layout";

const LAYOUT_SHORT: LayoutMode = {
  nodeW: 248,
  nodeH: 84,
  hgap: 96,
  vgap: 72,
  pad: 36,
  perLane: 5,   // ≤5 steps: single row, no wrapping needed
};

const LAYOUT_MEDIUM: LayoutMode = {
  nodeW: 174,   // ~70% of 248 — fits 6-10 nodes on one row
  nodeH: 59,    // ~70% of 84
  hgap: 52,     // proportionally tighter
  vgap: 52,
  pad: 28,
  perLane: 10,  // never wrap for 6-10 steps
};

const LAYOUT_LONG: LayoutMode = {
  nodeW: 220,   // readable but slightly compressed
  nodeH: 80,
  hgap: 72,
  vgap: 64,
  pad: 32,
  perLane: 3,   // 3-column grid for 11+ steps (≤15 → 5 rows; ≤30 → 10 rows)
};

// Explicit layout presets for user-selected modes (#1907)
const LAYOUT_USER_HORIZONTAL: LayoutMode = {
  nodeW: 220,
  nodeH: 80,
  hgap: 72,
  vgap: 64,
  pad: 32,
  perLane: 999, // never wrap — single row
};

const LAYOUT_USER_VERTICAL: LayoutMode = {
  nodeW: 248,
  nodeH: 84,
  hgap: 72,
  vgap: 56,
  pad: 36,
  perLane: 1,   // one per lane = single column
};

const LAYOUT_USER_GRID: LayoutMode = {
  nodeW: 220,
  nodeH: 80,
  hgap: 64,
  vgap: 56,
  pad: 32,
  perLane: 3,   // 3-column grid
};

function pickLayout(stepCount: number): LayoutMode {
  if (stepCount <= 5) return LAYOUT_SHORT;
  if (stepCount <= 10) return LAYOUT_MEDIUM;
  return LAYOUT_LONG;
}

function resolveLayout(stepCount: number, userLayout: UserLayout): LayoutMode {
  if (userLayout === "horizontal") return LAYOUT_USER_HORIZONTAL;
  if (userLayout === "vertical") return LAYOUT_USER_VERTICAL;
  if (userLayout === "grid") return LAYOUT_USER_GRID;
  return pickLayout(stepCount);
}

// Left-to-right layered layout: entry on the left, terminal on the right,
// edges flow horizontally. Long chains wrap onto stacked lanes so the wide
// canvas is used instead of a single tall column.
// Returns the positions array plus the canvas dimensions and the layout mode
// used (so the caller can size nodes consistently with the edges).
function layoutDAG(steps: ProcessStep[], userLayout: UserLayout = "auto") {
  const mode = resolveLayout(steps.length, userLayout);
  const { nodeW, nodeH, hgap, vgap, pad, perLane } = mode;
  const effectivePerLane = Math.max(1, Math.min(perLane, steps.length));
  const positions = steps.map((_, i) => {
    const lane = Math.floor(i / effectivePerLane);
    const col = i % effectivePerLane;
    return {
      x: pad + col * (nodeW + hgap),
      y: pad + lane * (nodeH + vgap),
    };
  });
  const lanes = Math.ceil(steps.length / effectivePerLane);
  const cols = Math.min(effectivePerLane, steps.length);
  const totalWidth = pad * 2 + cols * nodeW + (cols - 1) * hgap;
  const totalHeight = pad * 2 + lanes * nodeH + (lanes - 1) * vgap;
  return { positions, totalHeight, totalWidth, perLane: effectivePerLane, mode };
}

// ─── FlowDag ──────────────────────────────────────────────────────────────────

function FlowDag({
  flow,
  detailSteps,
  selectedStepIdx,
  onPickStep,
  userLayout = "auto",
  anim,
  reducedMotion = false,
  onBridgeMap,
}: {
  flow: Process;
  detailSteps?: ProcessStep[];
  selectedStepIdx: number | null;
  onPickStep: (i: number | null) => void;
  userLayout?: UserLayout;
  anim?: FlowAnimSnapshot;
  reducedMotion?: boolean;
  onBridgeMap?: (bridges: boolean[]) => void;
}) {
  const steps = detailSteps ?? flow.steps ?? [];

  // ── Zoom + pan state ──────────────────────────────────────────────────────
  const viewportRef = useRef<HTMLDivElement>(null);
  const [zoom, setZoom] = useState(1);
  const [pan, setPan] = useState({ x: 0, y: 0 });
  const dragRef = useRef<{
    x: number; y: number; px: number; py: number;
    moved: boolean; captured: boolean; pointerId: number;
  } | null>(null);
  const [dragging, setDragging] = useState(false);

  const { positions, totalHeight, totalWidth, perLane, mode } = useMemo(
    () => layoutDAG(steps, userLayout),
    [steps, userLayout],
  );

  const clampZoom = (z: number) => Math.min(2.5, Math.max(0.3, z));

  // Fit the DAG to the available viewport using both width and height (#1933).
  // Uses min(viewportW / dagW, viewportH / dagH) * 0.95 so the diagram fills
  // the panel on both axes instead of being constrained to width only.
  // Wrapped in useCallback so the ResizeObserver below can hold a stable ref.
  const fitToView = useCallback(() => {
    const vp = viewportRef.current;
    if (!vp || totalWidth === 0 || totalHeight === 0) {
      setZoom(1);
      setPan({ x: 0, y: 0 });
      return;
    }
    const MARGIN = 0.95;
    const z = clampZoom(
      Math.min(
        vp.clientWidth / totalWidth,
        vp.clientHeight / totalHeight,
      ) * MARGIN,
    );
    setZoom(z);
    setPan({
      x: (vp.clientWidth - totalWidth * z) / 2,
      y: (vp.clientHeight - totalHeight * z) / 2,
    });
  // clampZoom is defined inline above — stable; totalWidth/totalHeight come
  // from useMemo and only change when steps/layout change.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [totalWidth, totalHeight]);

  // Zoom around the viewport centre. Reads the *current* zoom/pan state
  // directly (no nested setState updaters — that was why the buttons no-op'd:
  // a setPan nested inside a setZoom functional updater is unreliable) and
  // commits both new values in the same tick.
  const zoomAtCenter = (factor: number) => {
    const vp = viewportRef.current;
    const nz = clampZoom(zoom * factor);
    if (nz === zoom) return;
    if (vp) {
      const cx = vp.clientWidth / 2;
      const cy = vp.clientHeight / 2;
      setPan({
        x: cx - ((cx - pan.x) / zoom) * nz,
        y: cy - ((cy - pan.y) / zoom) * nz,
      });
    }
    setZoom(nz);
  };

  // Fit when the flow changes or the user switches layout.
  // Also re-fit when DAG dimensions change (new process_id → new totalWidth/totalHeight).
  const flowKey = flow.process_id + ":" + steps.length + ":" + userLayout;
  useEffect(() => {
    const t = setTimeout(fitToView, 0);
    return () => clearTimeout(t);
  }, [flowKey, fitToView, totalWidth, totalHeight]);

  // Re-fit whenever the DAG container is resized (e.g. when the step inspector
  // side panel opens/closes, shrinking the DAG from full-width to 60% — #1933).
  useEffect(() => {
    const vp = viewportRef.current;
    if (!vp) return;
    const ro = new ResizeObserver(() => {
      fitToView();
    });
    ro.observe(vp);
    return () => ro.disconnect();
  }, [fitToView]);

  // Scroll-to-zoom (anchored on cursor).
  const onWheel = (e: React.WheelEvent) => {
    e.preventDefault();
    const vp = viewportRef.current;
    if (!vp) return;
    const rect = vp.getBoundingClientRect();
    const mx = e.clientX - rect.left;
    const my = e.clientY - rect.top;
    // Gentle wheel-zoom: small per-tick increment so scrolling feels smooth.
    const step = 1.045;
    const nz = clampZoom(zoom * (e.deltaY < 0 ? step : 1 / step));
    if (nz === zoom) return;
    setPan({
      x: mx - ((mx - pan.x) / zoom) * nz,
      y: my - ((my - pan.y) / zoom) * nz,
    });
    setZoom(nz);
  };

  // Drag-to-pan.
  //
  // CRITICAL: we must NOT call setPointerCapture on pointerdown. Capturing the
  // pointer on the viewport retargets the subsequent `click` event to the
  // viewport instead of the element actually under the cursor — which is why
  // the zoom/fit buttons AND the step nodes were dead (#1554 regressed):
  // their native onClick never fired because the click was stolen by the
  // captured viewport. Instead we only capture once a real drag begins (after
  // the movement threshold) so plain clicks dispatch normally to their target.
  const onPointerDown = (e: React.PointerEvent) => {
    if (e.button !== 0) return;
    // Don't start a pan when the press lands on an interactive control
    // (step node, zoom button, etc.) — let the click reach it.
    const t = e.target as HTMLElement;
    if (t?.closest?.("button, a, input, [data-no-pan]")) return;
    dragRef.current = {
      x: e.clientX, y: e.clientY, px: pan.x, py: pan.y,
      moved: false, captured: false, pointerId: e.pointerId,
    };
    setDragging(true);
  };
  const onPointerMove = (e: React.PointerEvent) => {
    const d = dragRef.current;
    if (!d) return;
    const dx = e.clientX - d.x;
    const dy = e.clientY - d.y;
    if (!d.moved && Math.abs(dx) + Math.abs(dy) > 3) {
      d.moved = true;
      // Capture only now that we're actually panning, so wheel/move stay
      // tracked even if the cursor leaves the viewport mid-drag.
      try {
        (e.currentTarget as HTMLElement).setPointerCapture(d.pointerId);
        d.captured = true;
      } catch {
        /* capture may be unavailable */
      }
    }
    if (d.moved) setPan({ x: d.px + dx, y: d.py + dy });
  };
  const endDrag = (e: React.PointerEvent) => {
    const d = dragRef.current;
    if (d) {
      // A click on empty canvas (no drag) deselects the current step. Because
      // we never capture on a plain click, clicks on step nodes reach their
      // own onClick; here we only handle the empty-canvas deselect.
      if (!d.moved && e.type === "pointerup") {
        const onNode = (e.target as HTMLElement)?.closest?.("button[data-step-node]");
        if (!onNode) onPickStep(null);
      }
      if (d.captured) {
        try {
          (e.currentTarget as HTMLElement).releasePointerCapture(d.pointerId);
        } catch {
          /* pointer may already be released */
        }
      }
      dragRef.current = null;
      setDragging(false);
    }
  };

  if (steps.length === 0) {
    return (
      <div className="flex items-center justify-center min-h-[280px] text-text-4 text-sm">
        No step data available.
      </div>
    );
  }

  // Horizontal edges: exit right edge of node i-1, enter left edge of node i.
  // When the chain wraps to the next lane, route down-then-across.
  //
  // Each edge now carries an explicit `polyline` (the actual points the
  // stroke passes through) so the comet animation can ride the same curve
  // the renderer draws — single source of truth for the line geometry.
  type EdgeRec = {
    from: Pt;
    to: Pt;
    kind: string | null;
    xrepo: boolean;
    wrap: boolean;
    polyline: Pt[];
    labelX: number;
    labelY: number;
    path: string;
  };
  const edges: EdgeRec[] = [];
  for (let i = 1; i < steps.length; i++) {
    const a = positions[i - 1];
    const b = positions[i];
    const wrap = i % perLane === 0; // first node of a new lane
    const from: Pt = { x: a.x + mode.nodeW, y: a.y + mode.nodeH / 2 };
    const to: Pt = { x: b.x, y: b.y + mode.nodeH / 2 };
    const xrepo = steps[i].repo !== steps[i - 1].repo;

    let polyline: Pt[];
    let labelX: number, labelY: number;
    if (wrap) {
      const downY = (from.y + to.y) / 2;
      polyline = [
        from,
        { x: from.x + 14, y: from.y },
        { x: from.x + 14, y: downY },
        { x: to.x - 14, y: downY },
        { x: to.x - 14, y: to.y },
        to,
      ];
      labelX = to.x + 6;
      labelY = to.y - 8;
    } else {
      const midX = (from.x + to.x) / 2;
      polyline = [
        from,
        { x: midX - 6, y: from.y },
        { x: midX + 6, y: to.y },
        to,
      ];
      labelX = midX - 4;
      labelY = (from.y + to.y) / 2 - 6;
    }
    const path =
      "M " + polyline.map((p) => `${p.x} ${p.y}`).join(" L ");

    edges.push({
      from, to,
      kind: steps[i].edge_kind,
      xrepo,
      wrap,
      polyline,
      labelX, labelY, path,
    });
  }

  // Publish the bridge-edge map to the parent so the animation controller
  // knows which edges to traverse slower (~450ms vs ~300ms).
  // edges[i] corresponds to step index i+1 (target step). We index the
  // returned array by target step idx so callers can look up via idx.
  useEffect(() => {
    if (!onBridgeMap) return;
    const map: boolean[] = new Array(steps.length).fill(false);
    edges.forEach((e, i) => {
      map[i + 1] = e.xrepo;
    });
    onBridgeMap(map);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [steps.length, flow.process_id, userLayout]);

  // Animation-derived sets.
  const traversedSet = useMemo(
    () => new Set(anim?.traversedEdges ?? []),
    [anim?.traversedEdges],
  );
  const currentTarget = anim?.currentTarget ?? -1;
  const edgeProgress = anim?.edgeProgress ?? 0;
  const lastScrubDir = anim?.lastScrubDir ?? null;
  // Track which edges to render with reverse-decay (fading out tint).
  // We compute on the fly: if the user scrubbed backward, edges past
  // playhead briefly fade — represented by `decayEdges` (those NOT in
  // traversedSet but were recently). We approximate with a transient
  // CSS class triggered by snapshot changes.
  const [decayEdges, setDecayEdges] = useState<Set<number>>(new Set());
  const prevTraversedRef = useRef<Set<number>>(new Set());
  useEffect(() => {
    if (!anim) return;
    const prev = prevTraversedRef.current;
    const next = new Set(anim.traversedEdges);
    const removed = new Set<number>();
    prev.forEach((idx) => {
      if (!next.has(idx)) removed.add(idx);
    });
    prevTraversedRef.current = next;
    if (removed.size > 0 && lastScrubDir === "backward") {
      setDecayEdges((cur) => {
        const merged = new Set(cur);
        removed.forEach((r) => merged.add(r));
        return merged;
      });
      // Fade-out window ~80ms each per spec; clear after a touch longer
      // so they all complete (we apply CSS transition).
      const t = setTimeout(() => setDecayEdges(new Set()), 220);
      return () => clearTimeout(t);
    }
    return undefined;
  }, [anim?.traversedEdges, lastScrubDir]);

  return (
    <div
      ref={viewportRef}
      className={cn(
        "relative overflow-hidden border-b border-border flex-none select-none",
        dragging ? "cursor-grabbing" : "cursor-grab",
      )}
      style={{
        background:
          "radial-gradient(circle at 1px 1px, var(--canvas-grid) 1px, transparent 1px) 0 0 / 18px 18px, var(--canvas-bg)",
        minHeight: mode === LAYOUT_MEDIUM ? 220 : 320,
        height: mode === LAYOUT_LONG ? 520 : 400,
        maxHeight: mode === LAYOUT_LONG ? 680 : 560,
      }}
      onWheel={onWheel}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={endDrag}
      onPointerLeave={endDrag}
    >
      <div
        className="absolute top-0 left-0 origin-top-left"
        style={{
          width: totalWidth,
          height: totalHeight,
          transform: `translate(${pan.x}px, ${pan.y}px) scale(${zoom})`,
        }}
      >
        {/* Edges */}
        <svg
          className="absolute inset-0 pointer-events-none overflow-visible"
          width={totalWidth}
          height={totalHeight}
        >
          <defs>
            <style>{`
              @media (prefers-reduced-motion: no-preference) {
                .fx-xedge { animation: fx-dash 0.9s linear infinite; }
                .fx-edge-pulse {
                  animation: fx-edge-pulse 150ms ease-out 1;
                }
                .fx-node-bounce {
                  animation: fx-node-bounce 180ms ease-out 1;
                }
              }
              @keyframes fx-dash { to { stroke-dashoffset: -16; } }
              @keyframes fx-edge-pulse {
                0%   { stroke-width: 1.4; opacity: 0.4; }
                50%  { stroke-width: 2.5; opacity: 1; }
                100% { stroke-width: 1.4; opacity: 0.5; }
              }
              @keyframes fx-node-bounce {
                0%   { transform: scale(1); }
                40%  { transform: scale(0.95); }
                70%  { transform: scale(1.05); }
                100% { transform: scale(1); }
              }
              .fx-edge-decay {
                transition: stroke-opacity 80ms ease-out, stroke 80ms ease-out;
              }
              .fx-comet {
                filter: drop-shadow(0 0 6px var(--ag-flow-accent, #60a5fa))
                        drop-shadow(0 0 2px var(--ag-flow-accent, #60a5fa));
              }
            `}</style>
            {/* Chevron markers — one per edge style (regular + bridge).
                These render a static arrowhead at the target end of every
                edge so direction reads without any animation. */}
            <marker
              id="ag-chev"
              viewBox="0 0 10 10"
              refX="9"
              refY="5"
              markerWidth="6"
              markerHeight="6"
              orient="auto-start-reverse"
            >
              <path d="M 0 0 L 10 5 L 0 10 z" fill="var(--ag-flow-edge, #475569)" />
            </marker>
            <marker
              id="ag-chev-bridge"
              viewBox="0 0 10 10"
              refX="9"
              refY="5"
              markerWidth="6"
              markerHeight="6"
              orient="auto-start-reverse"
            >
              <path d="M 0 0 L 10 5 L 0 10 z" fill="var(--ag-flow-bridge, #a78bfa)" />
            </marker>
            <marker
              id="ag-chev-traversed"
              viewBox="0 0 10 10"
              refX="9"
              refY="5"
              markerWidth="6"
              markerHeight="6"
              orient="auto-start-reverse"
            >
              <path d="M 0 0 L 10 5 L 0 10 z" fill="var(--ag-flow-accent, #60a5fa)" />
            </marker>
          </defs>
          {edges.map((ed, i) => {
            const targetIdx = i + 1; // step index this edge arrives at
            const isTraversed = traversedSet.has(targetIdx);
            const isCurrent = currentTarget === targetIdx && (anim?.running || anim?.paused);
            const isDecaying = decayEdges.has(targetIdx);
            const baseStroke = ed.xrepo
              ? "var(--ag-flow-bridge, #a78bfa)"
              : "var(--ag-flow-edge, #475569)";
            const traversedStroke = "var(--ag-flow-accent, #60a5fa)";
            const stroke = isTraversed ? traversedStroke : baseStroke;
            // Trail tinting: traversed ~ full strength, future ~ 25%.
            const strokeOpacity = isTraversed ? 0.9 : isDecaying ? 0.4 : 0.25;
            const markerId = isTraversed
              ? "ag-chev-traversed"
              : ed.xrepo
                ? "ag-chev-bridge"
                : "ag-chev";
            // Pulse class — applied only on the frame the comet arrives.
            // We approximate "on arrival" by detecting traversed state
            // change paired with currentTarget moving forward.
            const justArrived =
              isTraversed &&
              !reducedMotion &&
              lastScrubDir !== "backward" &&
              // The freshly-arrived edge is the one whose target matches the
              // playhead - 1 (most recent arrival).
              anim != null &&
              anim.playhead - 1 === targetIdx;
            return (
              <g key={i}>
                <path
                  d={ed.path}
                  fill="none"
                  stroke={stroke}
                  strokeOpacity={strokeOpacity}
                  strokeWidth={isCurrent ? 2.1 : 1.4}
                  strokeDasharray={ed.xrepo ? "6 4" : undefined}
                  markerEnd={`url(#${markerId})`}
                  className={cn(
                    "fx-edge-decay",
                    ed.xrepo ? "fx-xedge" : undefined,
                    justArrived ? "fx-edge-pulse" : undefined,
                  )}
                />
                {ed.kind && (
                  <text
                    x={ed.labelX}
                    y={ed.labelY}
                    fontSize={9}
                    textAnchor="middle"
                    fontFamily="var(--font-mono)"
                    fill={ed.xrepo
                      ? "var(--ag-flow-bridge, #a78bfa)"
                      : "var(--text-3)"}
                    paintOrder="stroke"
                    stroke="var(--canvas-bg)"
                    strokeWidth={3}
                    strokeLinejoin="round"
                  >
                    {ed.kind}
                    {ed.xrepo ? " · cross-repo" : ""}
                  </text>
                )}
              </g>
            );
          })}
          {/* Comet — only when a current edge is being traversed and the
              user has not requested reduced motion. */}
          {!reducedMotion && anim && anim.running && !anim.paused
            && currentTarget > 0 && currentTarget < steps.length && (() => {
              const ed = edges[currentTarget - 1];
              if (!ed) return null;
              const head = pointAt(ed.polyline, edgeProgress);
              // Trail: 4 fading dots behind the head along the same path.
              const trail = [0.04, 0.08, 0.13, 0.19]
                .map((d) => Math.max(0, edgeProgress - d))
                .map((p) => pointAt(ed.polyline, p));
              return (
                <g className="fx-comet" pointerEvents="none">
                  {trail.map((pt, ti) => (
                    <circle
                      key={ti}
                      cx={pt.x}
                      cy={pt.y}
                      r={2.2 - ti * 0.35}
                      fill="var(--ag-flow-accent, #60a5fa)"
                      opacity={0.55 - ti * 0.12}
                    />
                  ))}
                  <circle
                    cx={head.x}
                    cy={head.y}
                    r={3.6}
                    fill="var(--ag-flow-accent, #60a5fa)"
                  />
                </g>
              );
            })()}
          {/* Paused comet — frozen mid-edge so it's still visible. */}
          {!reducedMotion && anim && anim.paused
            && currentTarget > 0 && currentTarget < steps.length && (() => {
              const ed = edges[currentTarget - 1];
              if (!ed) return null;
              const head = pointAt(ed.polyline, edgeProgress);
              return (
                <g pointerEvents="none">
                  <circle
                    cx={head.x}
                    cy={head.y}
                    r={3.6}
                    fill="var(--ag-flow-accent, #60a5fa)"
                    opacity={0.85}
                  />
                  <circle
                    cx={head.x}
                    cy={head.y}
                    r={6}
                    fill="none"
                    stroke="var(--ag-flow-accent, #60a5fa)"
                    strokeOpacity={0.4}
                  />
                </g>
              );
            })()}
        </svg>

        {/* Nodes */}
        {steps.map((s, i) => {
          const isEntry = i === 0;
          const isTerminal = i === steps.length - 1;
          const isPhantom = flow.terminal_is_phantom && isTerminal;
          const meta = getStepMeta(s.step_kind);
          const name = stepName(s);
          const verb = getHttpVerb(name);
          const isXrepo = i > 0 && steps[i - 1].repo !== s.repo;
          const pos = positions[i];
          // Modest, useful locator: short file:line (basename only) — falls
          // back to the repo when no file is known. No clutter.
          const baseFile = s.source_file
            ? s.source_file.split("/").pop() ?? s.source_file
            : "";
          const locator = baseFile
            ? s.start_line && s.start_line > 0
              ? `${baseFile}:${s.start_line}`
              : baseFile
            : "";

          // Node arrival bounce — applied when this node was the most
          // recent forward arrival in the replay. Reduced-motion suppresses
          // it via the global @media rule.
          const justArrivedNode =
            !reducedMotion &&
            anim != null &&
            anim.lastScrubDir !== "backward" &&
            anim.playhead - 1 === i &&
            i > 0;
          const isInTrail =
            anim != null && i < anim.playhead;
          return (
            <button
              key={s.entity_id}
              type="button"
              data-step-node
              onClick={() => onPickStep(i)}
              className={cn(
                "absolute flex flex-col gap-1 rounded-md border cursor-pointer text-left",
                "transition-shadow duration-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
                selectedStepIdx === i
                  ? "border-accent shadow-[0_0_0_2px_var(--accent-ring)]"
                  : "border-border hover:shadow-[var(--shadow-2)]",
                isPhantom ? "border-dashed opacity-85" : "",
                justArrivedNode ? "fx-node-bounce" : "",
                isInTrail ? "ag-trail-node" : "",
              )}
              style={{
                left: pos.x,
                top: pos.y,
                width: mode.nodeW,
                minHeight: mode.nodeH,
                padding: mode === LAYOUT_MEDIUM ? "5px 8px 5px 10px" : "8px 12px 8px 14px",
                background:
                  isEntry || isTerminal
                    ? `color-mix(in srgb, ${meta.color} 8%, var(--surface))`
                    : "var(--surface)",
                borderLeftWidth: 4,
                borderLeftColor: meta.color,
                boxShadow: selectedStepIdx === i
                  ? "0 0 0 2px var(--accent-ring), var(--shadow-2)"
                  : "var(--shadow-1)",
              }}
            >
              {(isEntry || isTerminal) && (
                <span
                  className="absolute -top-2 left-3 inline-flex items-center h-4 px-1.5 rounded-xs text-[9px] font-bold uppercase tracking-wide"
                  style={{ background: meta.color, color: "var(--bg)" }}
                >
                  {isEntry ? "Entry" : isPhantom ? "Phantom" : "Terminal"}
                </span>
              )}
              <div className="flex items-center gap-1.5 min-w-0">
                <span className="inline-flex items-center justify-center w-4 h-4 rounded-full bg-surface-2 font-mono text-[9px] font-semibold text-text-4 flex-none">
                  {i + 1}
                </span>
                <span className="flex-none" style={{ color: meta.color }}>
                  <meta.Icon size={13} />
                </span>
                {verb && (
                  <span
                    className="font-mono text-[9px] font-bold uppercase px-1 py-0.5 rounded-xs flex-none"
                    style={{
                      color:
                        verb === "GET" ? "var(--pastel-1-ink)"
                        : verb === "POST" ? "var(--pastel-2-ink)"
                        : verb === "DELETE" ? "var(--danger)"
                        : "var(--pastel-6-ink)",
                      background:
                        verb === "GET"
                          ? "color-mix(in srgb, var(--pastel-1) 30%, transparent)"
                          : verb === "POST"
                            ? "color-mix(in srgb, var(--pastel-2) 32%, transparent)"
                            : verb === "DELETE"
                              ? "color-mix(in srgb, var(--danger) 16%, transparent)"
                              : "color-mix(in srgb, var(--pastel-6) 34%, transparent)",
                    }}
                  >
                    {verb}
                  </span>
                )}
                <span
                  className="font-mono text-[11px] text-text font-medium flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap"
                  title={name}
                >
                  {name}
                </span>
              </div>
              <div className="flex items-center justify-between gap-2 min-w-0">
                <span
                  className="inline-flex items-center gap-1 text-[10px] font-semibold whitespace-nowrap flex-none"
                  style={{ color: meta.color }}
                >
                  <span
                    className="w-[6px] h-[6px] rounded-[2px] flex-none"
                    style={{ background: meta.color }}
                  />
                  {meta.label}
                </span>
                <span
                  className="font-mono text-[9px] truncate min-w-0"
                  style={isXrepo ? { color: "#a78bfa" } : { color: "var(--text-3)" }}
                  title={s.repo}
                >
                  {s.repo}
                </span>
              </div>
              {locator && mode !== LAYOUT_MEDIUM && (
                <span
                  className="font-mono text-[9px] text-text-4 truncate block min-w-0"
                  title={s.source_file ? `${s.source_file}${s.start_line ? `:${s.start_line}` : ""}` : ""}
                >
                  {locator}
                </span>
              )}
            </button>
          );
        })}
      </div>

      {/* Canvas controls */}
      <div className="absolute right-3 top-3 flex gap-1 rounded-md border border-border bg-[var(--overlay)] p-1 backdrop-blur-sm z-10">
        {[
          { icon: <Maximize2 size={12} />, title: "Fit to view", onClick: fitToView },
          { icon: <ZoomIn size={12} />, title: "Zoom in", onClick: () => zoomAtCenter(1.2) },
          { icon: <ZoomOut size={12} />, title: "Zoom out", onClick: () => zoomAtCenter(1 / 1.2) },
        ].map((btn) => (
          <button
            key={btn.title}
            type="button"
            title={btn.title}
            onClick={btn.onClick}
            className="w-6 h-6 inline-flex items-center justify-center rounded-sm text-text-3 hover:bg-surface-2 hover:text-text"
          >
            {btn.icon}
          </button>
        ))}
        <span className="inline-flex items-center px-1.5 font-mono text-[9px] text-text-3 select-none">
          {Math.round(zoom * 100)}%
        </span>
      </div>

      {/* Legend */}
      <div className="absolute left-3 bottom-3 flex flex-wrap gap-2 rounded-sm border border-border bg-[var(--overlay)] px-2 py-1.5 max-w-xs backdrop-blur-sm z-10">
        {(
          [
            "http_fetch",
            "db_query",
            "db_write",
            "message_publish",
            "function_call",
            "component_render",
          ] as StepKind[]
        ).map((k) => {
          const m = getStepMeta(k);
          return (
            <span key={k} className="inline-flex items-center gap-1 text-[9px] text-text-3">
              <span
                className="w-[7px] h-[7px] rounded-[2px] flex-none"
                style={{ background: m.color }}
              />
              {m.label}
            </span>
          );
        })}
        <span className="inline-flex items-center gap-1 text-[9px] text-text-3">
          <svg width="14" height="10">
            <line
              x1="0"
              y1="5"
              x2="14"
              y2="5"
              stroke="#a78bfa"
              strokeWidth="1.5"
              strokeDasharray="4 3"
            />
          </svg>
          cross-repo
        </span>
      </div>
    </div>
  );
}

// ─── Step inspector ───────────────────────────────────────────────────────────

function StepInspector({
  step,
  flow,
  totalSteps,
  onClose,
}: {
  step: ProcessStep;
  flow: Process;
  totalSteps: number;
  onClose: () => void;
}) {
  const meta = getStepMeta(step.step_kind);
  const snippet = flow.source_snippets?.[step.entity_id];

  return (
    <div
      // Floating detail card — same surface/border styling as the flow header
      // card. Floats over the bottom of the DAG canvas; only present when a
      // step is selected.
      className="absolute left-3 right-3 bottom-3 z-20 rounded-md border border-border bg-surface shadow-[var(--shadow-3)] px-4 py-3 flex flex-col gap-2 max-h-[60%] overflow-y-auto"
    >
      <div className="flex items-center gap-2">
        <span style={{ color: meta.color }}>
          <meta.Icon size={14} />
        </span>
        <span className="font-mono text-sm text-text font-medium flex-1 min-w-0 truncate">
          {stepName(step)}
        </span>
        <span
          className="font-mono text-[9px] font-bold uppercase px-1.5 py-0.5 rounded-xs flex-none"
          style={{ background: meta.color, color: "var(--bg)" }}
        >
          {meta.label}
        </span>
        <span className="font-mono text-[10px] text-text-3 flex-none">
          Step {step.step_index + 1} of {totalSteps}
        </span>
        <button
          type="button"
          onClick={onClose}
          title="Close"
          className="w-6 h-6 flex items-center justify-center rounded-sm text-text-3 hover:bg-surface-2 hover:text-text flex-none ml-1"
        >
          <X size={12} />
        </button>
      </div>
      <div className="flex flex-wrap gap-2.5 text-[11px] text-text-3">
        <span>{meta.label}</span>
        <span>·</span>
        <span className="font-mono">
          {step.source_file}
          {step.start_line ? `:${step.start_line}` : ""}
        </span>
        <span>·</span>
        <span>{step.repo}</span>
        {step.edge_kind && (
          <>
            <span>·</span>
            <span>
              edge <span className="font-mono">{step.edge_kind}</span>
            </span>
          </>
        )}
      </div>
      {snippet && (
        <pre className="font-mono text-[11px] bg-bg-soft border border-border rounded-sm p-2.5 whitespace-pre-wrap leading-snug text-text-2 max-h-[160px] overflow-y-auto m-0">
          {snippet}
        </pre>
      )}
      <div className="flex gap-1.5">
        <button
          type="button"
          className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-sm text-sm border border-border bg-surface text-text-2 hover:bg-surface-2 hover:text-text"
        >
          <ExternalLink size={12} /> Open source
        </button>
        <button
          type="button"
          onClick={() => {
            void navigator.clipboard.writeText(step.entity_id);
            toast.success("Copied entity id");
          }}
          className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-sm text-sm border border-border bg-surface text-text-2 hover:bg-surface-2 hover:text-text"
        >
          <Copy size={12} /> Copy id
        </button>
        <button
          type="button"
          className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-sm text-sm border-0 bg-transparent text-text-3 hover:bg-surface-2 hover:text-text ml-auto"
        >
          <ExternalLink size={12} /> Open in graph
        </button>
      </div>
    </div>
  );
}

// ─── Collapsible section ──────────────────────────────────────────────────────

function Section({
  title,
  count,
  defaultOpen = true,
  icon,
  children,
}: {
  title: string;
  count?: number;
  defaultOpen?: boolean;
  icon?: React.ReactNode;
  children: React.ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <section className="border-b border-border-soft py-3 last:border-0">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-2 pb-2.5 w-full text-left focus-visible:outline-none"
      >
        <ChevronRight
          size={11}
          className={cn("text-text-3 transition-transform duration-150", open && "rotate-90")}
        />
        {icon && <span className="text-text-3 flex">{icon}</span>}
        <span className="text-sm font-semibold text-text">{title}</span>
        {typeof count === "number" && (
          <span className="font-mono text-[11px] text-text-3">{count}</span>
        )}
      </button>
      {open && <div className="pl-5 flex flex-col gap-2">{children}</div>}
    </section>
  );
}

// ─── Side-effects floating panel ─────────────────────────────────────────────

function SideEffectsPanel({
  flow,
  onClose,
}: {
  flow: Process;
  onClose: () => void;
}) {
  const effects = flow.flow_side_effects ?? [];
  return (
    <div
      // Floating panel — same surface/border styling as the step-detail card.
      // Anchored bottom-left of the DAG canvas overlay.
      className="absolute left-3 right-3 bottom-3 z-20 rounded-md border border-border bg-surface shadow-[var(--shadow-3)] px-4 py-3 flex flex-col gap-2 max-h-[60%] overflow-y-auto"
    >
      <div className="flex items-center gap-2">
        <AlertTriangle size={13} className="text-[var(--warning)] flex-none" />
        <span className="text-sm font-semibold text-text flex-1">Side effects</span>
        <span className="font-mono text-[11px] text-text-3">{effects.length}</span>
        <button
          type="button"
          onClick={onClose}
          title="Close"
          className="w-6 h-6 flex items-center justify-center rounded-sm text-text-3 hover:bg-surface-2 hover:text-text flex-none ml-1"
        >
          <X size={12} />
        </button>
      </div>
      {effects.length > 0 ? (
        <ul className="list-none m-0 p-0 flex flex-col gap-1">
          {effects.map((s, i) => (
            <li key={i} className="text-sm text-text-2 pl-3.5 relative">
              <span className="absolute left-0 top-[8px] w-[5px] h-[5px] bg-text-4 rounded-full" />
              {s}
            </li>
          ))}
        </ul>
      ) : (
        <span className="text-sm text-text-4 italic">
          No observed side effects across this chain.
        </span>
      )}
    </div>
  );
}

// ─── Detail sections ──────────────────────────────────────────────────────────

function DetailSections({ flow }: { flow: Process }) {
  const enr = flow.enrichment;

  return (
    <div className="px-5 pb-20 flex flex-col">
      {/* Data sinks & sources */}
      {enr &&
        (enr.writes_db_table?.length ||
          enr.publishes_to?.length ||
          enr.external_calls?.length ||
          enr.read_sources?.length) ? (
        <Section title="Data sinks & sources" icon={<Link2 size={11} />}>
          <div className="flex flex-col gap-1.5">
            {(enr.writes_db_table?.length ?? 0) > 0 && (
              <div className="flex items-center gap-2 flex-wrap">
                <span className="text-[10px] text-text-3 uppercase tracking-wide min-w-[90px]">
                  Writes DB
                </span>
                {enr.writes_db_table!.map((t) => (
                  <span
                    key={t}
                    className="font-mono text-[11px] px-2 py-0.5 rounded-xs"
                    style={{
                      background: "color-mix(in srgb, var(--pastel-6) 30%, transparent)",
                      color: "var(--pastel-6-ink)",
                    }}
                  >
                    {t}
                  </span>
                ))}
              </div>
            )}
            {(enr.publishes_to?.length ?? 0) > 0 && (
              <div className="flex items-center gap-2 flex-wrap">
                <span className="text-[10px] text-text-3 uppercase tracking-wide min-w-[90px]">
                  Publishes to
                </span>
                {enr.publishes_to!.map((t) => (
                  <span
                    key={t}
                    className="font-mono text-[11px] px-2 py-0.5 rounded-xs"
                    style={{
                      background: "color-mix(in srgb, var(--pastel-5) 30%, transparent)",
                      color: "var(--pastel-5-ink)",
                    }}
                  >
                    {t}
                  </span>
                ))}
              </div>
            )}
            {(enr.external_calls?.length ?? 0) > 0 && (
              <div className="flex items-center gap-2 flex-wrap">
                <span className="text-[10px] text-text-3 uppercase tracking-wide min-w-[90px]">
                  External calls
                </span>
                {enr.external_calls!.map((t) => (
                  <span
                    key={t}
                    className="font-mono text-[11px] px-2 py-0.5 rounded-xs"
                    style={{
                      background: "color-mix(in srgb, var(--pastel-1) 26%, transparent)",
                      color: "var(--pastel-1-ink)",
                    }}
                  >
                    {t}
                  </span>
                ))}
              </div>
            )}
            {(enr.read_sources?.length ?? 0) > 0 && (
              <div className="flex items-center gap-2 flex-wrap">
                <span className="text-[10px] text-text-3 uppercase tracking-wide min-w-[90px]">
                  Reads from
                </span>
                {enr.read_sources!.map((t) => (
                  <span
                    key={t}
                    className="font-mono text-[11px] px-2 py-0.5 rounded-xs"
                    style={{
                      background: "color-mix(in srgb, var(--pastel-6) 30%, transparent)",
                      color: "var(--pastel-6-ink)",
                    }}
                  >
                    {t}
                  </span>
                ))}
              </div>
            )}
          </div>
        </Section>
      ) : null}

      {/* Known gaps */}
      {enr && (enr.gaps?.length ?? 0) > 0 && (
        <Section title="Known gaps" count={enr.gaps!.length} icon={<Info size={11} />}>
          <ul className="list-none m-0 p-0 flex flex-col gap-1">
            {enr.gaps!.map((g, i) => (
              <li key={i} className="text-sm text-text-2 pl-3.5 relative">
                <span className="absolute left-0 top-[8px] w-[5px] h-[5px] bg-text-4 rounded-full" />
                {g}
              </li>
            ))}
          </ul>
        </Section>
      )}
    </div>
  );
}

// ─── Dead-end detail ──────────────────────────────────────────────────────────

function DeadEndDetail({ de, onClose }: { de: FlowDeadEnd; onClose: () => void }) {
  const isExt = de.dead_end_step_id?.includes("::ext:");
  const reasonLabel = DEAD_END_REASON_LABEL[de.reason] ?? de.reason;
  const reasonProse: Record<string, string> = {
    no_useful_sink:
      "The flow walker reached a call chain that never writes to a DB, publishes an event, or makes an external HTTP call. The chain appears to terminate in side-effect-free utility code.",
    single_step:
      "This flow has only one step, so there's no useful chain to trace. It may be a utility function with no callers, or an entry point that delegates immediately to an unresolved symbol.",
    unresolved_callee:
      "The walker encountered a symbol it couldn't resolve — likely an external library or a dynamic call pattern. The chain is cut off at this point.",
    phantom_terminal:
      "The terminal entity exists as a declared type or interface but has no concrete implementation in the indexed repositories.",
    dead_end:
      "The flow reached a node with no outbound edges that qualify as useful sinks or calls.",
  };

  return (
    <div className="flex flex-col h-full bg-bg overflow-hidden">
      {/* Header */}
      <header className="flex flex-col gap-1.5 px-5 py-3.5 border-b border-border bg-surface flex-none">
        <div className="flex items-center gap-2 font-mono text-[var(--text-md)] text-text font-medium">
          <AlertTriangle size={14} style={{ color: "var(--warning)" }} />
          <span className="flex-1 min-w-0 break-words">
            <FlowLabel label={de.process_name} />
          </span>
          <button
            type="button"
            onClick={onClose}
            className="w-6 h-6 flex items-center justify-center rounded-sm text-text-3 hover:bg-surface-2 hover:text-text"
          >
            <X size={12} />
          </button>
        </div>
        <div className="flex flex-wrap gap-1.5">
          <RepoChip name={de.repo} />
          <span className="inline-flex h-[22px] px-2 rounded-sm bg-surface-2 border border-border font-mono text-[10px] text-text-2">
            {de.step_count} step{de.step_count === 1 ? "" : "s"}
          </span>
          <span
            className="inline-flex h-[22px] px-2 rounded-sm font-mono text-[10px] font-semibold"
            style={{
              color: "var(--warning)",
              background: "color-mix(in srgb, var(--warning) 14%, transparent)",
            }}
          >
            {reasonLabel}
          </span>
          {de.cross_stack && <CrossStackChip />}
        </div>
      </header>

      {/* Ghost DAG */}
      <div
        className="border-b border-border flex-none"
        style={{
          background:
            "radial-gradient(circle at 1px 1px, var(--canvas-grid) 1px, transparent 1px) 0 0 / 18px 18px, var(--canvas-bg)",
          minHeight: 220,
          position: "relative",
        }}
      >
        <div className="relative mx-auto" style={{ width: 300, height: 220 }}>
          <svg
            className="absolute inset-0 pointer-events-none"
            width={300}
            height={220}
          >
            <line
              x1={150}
              y1={88}
              x2={150}
              y2={148}
              stroke="#64748b"
              strokeWidth={1.4}
              strokeDasharray="4 3"
            />
          </svg>
          {/* Entry node */}
          <div
            className="absolute flex flex-col gap-1 rounded-md border border-border bg-surface"
            style={{
              left: 40,
              top: 36,
              width: 220,
              padding: "8px 12px 8px 14px",
              borderLeftWidth: 4,
              borderLeftColor: "#94a3b8",
            }}
          >
            <div className="flex items-center gap-1.5">
              <span className="font-mono text-[9px] font-semibold text-text-4 w-4 h-4 inline-flex items-center justify-center rounded-full bg-surface-2">
                1
              </span>
              <Layout size={13} className="text-text-3" />
              <span className="font-mono text-[11px] text-text truncate">
                {(de.process_name ?? "").split(" → ")[0] || de.repo}
              </span>
            </div>
          </div>
          {/* Ghost terminus */}
          <div
            className="absolute flex flex-col gap-1 rounded-md text-center"
            style={{
              left: 60,
              top: 148,
              width: 180,
              padding: "8px 12px",
              border: "1.5px dashed var(--warning)",
              background: "var(--bg-soft)",
              color: "var(--warning)",
            }}
          >
            <div className="font-mono text-[10px] font-medium">
              {de.dead_end_step_name ?? "dead end"}
            </div>
            {isExt && (
              <div className="font-mono text-[9px] opacity-70">external / unresolved</div>
            )}
          </div>
        </div>
      </div>

      {/* Sections */}
      <div className="flex-1 overflow-y-auto px-5 py-4 flex flex-col gap-3">
        <Section title="Why this was flagged" defaultOpen>
          <p className="text-sm text-text-2 leading-relaxed m-0">
            {reasonProse[de.reason] ??
              "This flow was flagged during extraction as a dead-end."}
          </p>
        </Section>
        <Section title="Suggested fix">
          <p className="text-sm text-text-2 leading-relaxed m-0">
            Review the entry chain and add explicit sink calls, or mark the flow as expected
            dead-end in your archigraph config. You can also triage this in the Pending queue.
          </p>
          <button
            type="button"
            onClick={() => toast.info("Open in Pending — not wired yet.")}
            className="self-start inline-flex items-center gap-1.5 h-7 px-2.5 rounded-sm text-sm border border-border bg-surface text-text-2 hover:bg-surface-2 hover:text-text"
          >
            Open in Pending
          </button>
        </Section>
      </div>
    </div>
  );
}

// ─── Detail panel ─────────────────────────────────────────────────────────────

type Selection =
  | { kind: "flow"; flow: Process }
  | { kind: "deadend"; de: FlowDeadEnd };

// ─── FlowDag wrapper that subscribes to the animation controller ─────────────
//
// Isolating the snapshot subscription here means the DetailPanel doesn't
// re-render on every rAF tick; only this leaf does, which in turn passes the
// (small) snapshot object down to FlowDag for rendering.
function FlowDagWithAnim({
  controller,
  ...rest
}: {
  flow: Process;
  detailSteps?: ProcessStep[];
  selectedStepIdx: number | null;
  onPickStep: (i: number | null) => void;
  userLayout?: UserLayout;
  controller: FlowAnimController | null;
  reducedMotion?: boolean;
  onBridgeMap?: (bridges: boolean[]) => void;
}) {
  if (!controller) {
    return <FlowDag {...rest} />;
  }
  return <FlowDagWithAnimInner controller={controller} {...rest} />;
}

function FlowDagWithAnimInner({
  controller,
  ...rest
}: {
  flow: Process;
  detailSteps?: ProcessStep[];
  selectedStepIdx: number | null;
  onPickStep: (i: number | null) => void;
  userLayout?: UserLayout;
  controller: FlowAnimController;
  reducedMotion?: boolean;
  onBridgeMap?: (bridges: boolean[]) => void;
}) {
  const snap = useFlowAnim(controller);
  return <FlowDag {...rest} anim={snap} />;
}

// ─── Replay controls (#1922) ─────────────────────────────────────────────────

const SPEEDS: Array<{ key: string; mult: number; label: string }> = [
  { key: "0.5", mult: 0.5, label: "0.5×" },
  { key: "1",   mult: 1,   label: "1×" },
  { key: "2",   mult: 2,   label: "2×" },
];

function ReplayControls({
  controller,
  totalSteps,
  speedKey,
  onSpeedChange,
  audioOn,
  onAudioToggle,
}: {
  controller: FlowAnimController;
  totalSteps: number;
  speedKey: string;
  onSpeedChange: (k: string) => void;
  audioOn: boolean;
  onAudioToggle: (next: boolean) => void;
}) {
  const snap = useFlowAnim(controller);
  const isPlaying = snap.running && !snap.paused;
  const canReplay = totalSteps >= 2;

  return (
    <div
      className="inline-flex items-center gap-1 h-7 rounded-sm border border-border bg-surface px-1"
      role="group"
      aria-label="Replay controls"
    >
      <button
        type="button"
        title={
          !canReplay
            ? "Need at least 2 steps"
            : isPlaying
              ? "Pause replay (Esc)"
              : snap.paused
                ? "Resume"
                : "Replay all steps"
        }
        disabled={!canReplay}
        onClick={() => {
          if (isPlaying) controller.pause();
          else if (snap.paused) controller.resume();
          else controller.start();
        }}
        className={cn(
          "inline-flex items-center gap-1 h-6 px-2 rounded-sm text-[11px] font-medium",
          "text-text-2 hover:bg-surface-2 hover:text-text disabled:opacity-40",
          isPlaying ? "bg-surface-2 text-text" : "",
        )}
      >
        {isPlaying ? <Pause size={11} /> : <Play size={11} />}
        {isPlaying ? "Pause" : snap.paused ? "Resume" : "Replay all"}
      </button>
      <button
        type="button"
        title="Stop replay"
        disabled={!canReplay || (!snap.running && !snap.paused && snap.playhead === 0)}
        onClick={() => controller.reset()}
        className="inline-flex items-center h-6 w-6 justify-center rounded-sm text-text-3 hover:bg-surface-2 hover:text-text disabled:opacity-40"
      >
        <Square size={10} />
      </button>
      <span className="inline-flex items-center gap-0.5 ml-1 pl-1 border-l border-border">
        <Gauge size={10} className="text-text-4" />
        {SPEEDS.map((s) => (
          <button
            key={s.key}
            type="button"
            onClick={() => onSpeedChange(s.key)}
            className={cn(
              "h-5 px-1 rounded-xs text-[10px] font-mono",
              speedKey === s.key
                ? "bg-surface-2 text-text font-semibold"
                : "text-text-3 hover:text-text",
            )}
            title={`Replay speed ${s.label}`}
          >
            {s.label}
          </button>
        ))}
      </span>
      <button
        type="button"
        onClick={() => onAudioToggle(!audioOn)}
        title={audioOn ? "Audio blip on — click to mute" : "Audio blip off — click to enable"}
        className={cn(
          "inline-flex items-center h-6 w-6 justify-center rounded-sm ml-1",
          audioOn ? "text-text" : "text-text-4",
          "hover:bg-surface-2",
        )}
      >
        {audioOn ? <Volume2 size={11} /> : <VolumeX size={11} />}
      </button>
    </div>
  );
}

// ─── Progress scrubber (#1922) ───────────────────────────────────────────────

function ProgressScrubber({
  controller,
  totalSteps,
  stepLabels,
}: {
  controller: FlowAnimController;
  totalSteps: number;
  stepLabels: string[];
}) {
  const snap = useFlowAnim(controller);
  const trackRef = useRef<HTMLDivElement>(null);
  const [hoverIdx, setHoverIdx] = useState<number | null>(null);
  const [dragging, setDragging] = useState(false);

  if (totalSteps < 2) return null;

  // Fractional position of the playhead across the track (0..1).
  // playhead counts reached nodes (0..totalSteps). Map onto (totalSteps-1)
  // segments. While animating an edge we offset by edgeProgress.
  const segments = totalSteps - 1;
  const baseSeg = Math.max(0, snap.playhead - 1);
  const inFlight = snap.running && !snap.paused && snap.edgeProgress > 0;
  const frac = inFlight
    ? Math.min(1, (baseSeg + snap.edgeProgress) / segments)
    : snap.playhead === 0
      ? 0
      : Math.min(1, baseSeg / segments);

  function idxFromEvent(e: React.PointerEvent | PointerEvent) {
    const el = trackRef.current;
    if (!el) return 0;
    const rect = el.getBoundingClientRect();
    const x = (e as PointerEvent).clientX - rect.left;
    const t = Math.max(0, Math.min(1, x / rect.width));
    return Math.round(t * segments) + 1; // playhead value (1..totalSteps)
  }

  return (
    <div className="px-3 py-2 border-t border-border bg-surface flex items-center gap-2">
      <span className="font-mono text-[10px] text-text-4 tabular-nums w-12 flex-none">
        {snap.playhead} / {totalSteps}
      </span>
      <div
        ref={trackRef}
        className="relative flex-1 h-5 cursor-pointer select-none"
        onPointerDown={(e) => {
          e.currentTarget.setPointerCapture(e.pointerId);
          setDragging(true);
          controller.scrubTo(idxFromEvent(e));
        }}
        onPointerMove={(e) => {
          if (dragging) controller.scrubTo(idxFromEvent(e));
        }}
        onPointerUp={(e) => {
          try {
            e.currentTarget.releasePointerCapture(e.pointerId);
          } catch { /* ignore */ }
          setDragging(false);
        }}
        onMouseMove={(e) => {
          const el = trackRef.current;
          if (!el) return;
          const rect = el.getBoundingClientRect();
          const t = (e.clientX - rect.left) / rect.width;
          const idx = Math.max(0, Math.min(totalSteps - 1, Math.round(t * (totalSteps - 1))));
          setHoverIdx(idx);
        }}
        onMouseLeave={() => setHoverIdx(null)}
      >
        {/* Track */}
        <div
          className="absolute left-0 right-0 top-1/2 -translate-y-1/2 h-[3px] rounded-full"
          style={{ background: "var(--border)" }}
        />
        {/* Filled */}
        <div
          className="absolute left-0 top-1/2 -translate-y-1/2 h-[3px] rounded-full"
          style={{
            width: `${frac * 100}%`,
            background: "var(--ag-flow-accent, #60a5fa)",
            transition: dragging || inFlight ? "none" : "width 120ms linear",
          }}
        />
        {/* Ticks */}
        {Array.from({ length: totalSteps }, (_, i) => (
          <span
            key={i}
            className="absolute top-1/2 -translate-y-1/2"
            style={{
              left: `${(i / segments) * 100}%`,
              width: 1,
              height: i === 0 || i === totalSteps - 1 ? 10 : 6,
              background: "var(--text-4)",
              transform: "translate(-0.5px, -50%)",
            }}
          />
        ))}
        {/* Playhead */}
        <span
          className="absolute top-1/2 -translate-y-1/2 rounded-full pointer-events-none"
          style={{
            left: `${frac * 100}%`,
            transform: "translate(-50%, -50%)",
            width: 11,
            height: 11,
            background: "var(--ag-flow-accent, #60a5fa)",
            boxShadow: "0 0 0 2px var(--surface), 0 0 4px var(--ag-flow-accent, #60a5fa)",
            transition: dragging || inFlight ? "none" : "left 120ms linear",
          }}
        />
        {/* Hover label */}
        {hoverIdx != null && stepLabels[hoverIdx] && (
          <span
            className="absolute -top-5 px-1.5 py-0.5 rounded-xs font-mono text-[9px] bg-surface-2 border border-border text-text-2 whitespace-nowrap pointer-events-none"
            style={{
              left: `${(hoverIdx / segments) * 100}%`,
              transform: "translateX(-50%)",
              maxWidth: 240,
              overflow: "hidden",
              textOverflow: "ellipsis",
            }}
          >
            {hoverIdx + 1}. {stepLabels[hoverIdx]}
          </span>
        )}
      </div>
    </div>
  );
}

const SPEED_KEY = "archigraph:flows:speed";

function DetailPanel({
  selection,
  groupId,
  onClose,
}: {
  selection: Selection | null;
  groupId: string;
  onClose: () => void;
}) {
  const [selectedStepIdx, setSelectedStepIdx] = useState<number | null>(null);
  const [sideEffectsOpen, setSideEffectsOpen] = useState(false);
  const reducedMotion = usePrefersReducedMotion();

  // ── Audio + speed prefs (#1922) ──────────────────────────────────────────
  const [audioOn, setAudioOn] = useState<boolean>(() => readFlowAudio());
  function updateAudio(next: boolean) {
    setAudioOn(next);
    writeFlowAudio(next);
  }

  const [speedKey, setSpeedKey] = useState<string>(() => {
    try {
      const saved = localStorage.getItem(SPEED_KEY);
      if (saved && SPEEDS.some((s) => s.key === saved)) return saved;
    } catch { /* ignore */ }
    return "1";
  });
  function updateSpeed(k: string) {
    setSpeedKey(k);
    try { localStorage.setItem(SPEED_KEY, k); } catch { /* ignore */ }
  }
  const speedMult = SPEEDS.find((s) => s.key === speedKey)?.mult ?? 1;

  // ── Bridge-edge map published by FlowDag ─────────────────────────────────
  const bridgeRef = useRef<boolean[]>([]);

  // ── Animation controller — recreated when the selection or step count
  //    changes. Stored in state so any consumer re-renders when the
  //    controller instance flips. The actual replay snapshot is observed
  //    by children via useFlowAnim(useSyncExternalStore).
  const [controller, setController] = useState<FlowAnimController | null>(null);

  // ── Layout toggle (#1907) ──────────────────────────────────────────────────
  const [userLayout, setUserLayout] = useState<UserLayout>(() => {
    try {
      const saved = localStorage.getItem(LAYOUT_STORAGE_KEY);
      if (saved === "horizontal" || saved === "vertical" || saved === "grid") return saved;
    } catch {
      /* localStorage unavailable */
    }
    return "auto";
  });

  function applyLayout(l: UserLayout) {
    setUserLayout(l);
    try {
      if (l === "auto") {
        localStorage.removeItem(LAYOUT_STORAGE_KEY);
      } else {
        localStorage.setItem(LAYOUT_STORAGE_KEY, l);
      }
    } catch {
      /* ignore */
    }
  }
  // ─────────────────────────────────────────────────────────────────────────

  const selId = selection?.kind === "flow" ? selection.flow.process_id : null;
  useEffect(() => {
    setSelectedStepIdx(null);
    setSideEffectsOpen(false);
  }, [selId]);

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key !== "Escape" || !selection) return;
      // If a replay is running or paused, ESC pauses/resumes — only close
      // the panel when there's no active animation in progress.
      const c = controller;
      if (c) {
        const snap = c.getSnapshot();
        if (snap.running && !snap.paused) {
          c.pause();
          e.preventDefault();
          return;
        }
      }
      onClose();
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [selection, onClose, controller]);

  const detailQ = useFlowDetail(
    groupId,
    selection?.kind === "flow" ? selection.flow.process_id : null,
  );

  // ── Derive total step count + auto speed-bump for very long flows ────────
  const previewFlow = selection?.kind === "flow" ? selection.flow : null;
  const previewSteps =
    detailQ.data?.process?.steps ??
    detailQ.data?.chain_entities ??
    previewFlow?.steps ??
    [];
  const totalSteps = previewSteps.length;

  // Effective speed: respect user choice, but auto-bump to 2× on flows
  // with >80 steps if the user is still on the default 1× (#1922 perf
  // guidance). The slider value itself stays at 1× so the user can
  // still override.
  const effectiveSpeed = totalSteps > 80 && speedKey === "1" ? 2 : speedMult;

  // Construct / re-construct the controller on flow change.
  const ctrlKey = `${selection?.kind === "flow" ? selection.flow.process_id : "none"}:${totalSteps}`;
  const lastCtrlKeyRef = useRef<string>("");
  useEffect(() => {
    if (selection?.kind !== "flow" || totalSteps < 2) {
      setController(null);
      lastCtrlKeyRef.current = "";
      return;
    }
    if (lastCtrlKeyRef.current === ctrlKey) return;
    lastCtrlKeyRef.current = ctrlKey;
    const c = createFlowAnim({
      totalSteps,
      // Looked up live from the bridge map FlowDag publishes via onBridgeMap.
      isBridgeEdge: (idx) => Boolean(bridgeRef.current?.[idx]),
      speed: effectiveSpeed,
      reducedMotion,
    });
    c.setOnArrive(() => {
      if (readFlowAudio()) playStepBlip();
    });
    setController(c);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ctrlKey, reducedMotion]);

  // Push speed updates into the existing controller without re-creating it
  // (re-creating mid-flow would lose the playhead).
  useEffect(() => {
    controller?.setSpeed(effectiveSpeed);
  }, [controller, effectiveSpeed]);

  if (!selection) {
    return (
      <div className="flex flex-col items-center justify-center gap-3 text-center p-10 h-full text-text-3 bg-bg">
        <Link2 size={28} />
        <h3 className="m-0 text-[var(--text-md)] text-text font-semibold">
          Select a flow to see its chain
        </h3>
        <p className="m-0 text-sm max-w-[320px] leading-relaxed">
          Pick any flow on the left and its DAG visualization, callers, downstream effects, and AI
          insights open here.
        </p>
      </div>
    );
  }

  if (selection.kind === "deadend") {
    return <DeadEndDetail de={selection.de} onClose={onClose} />;
  }

  const flow = selection.flow;
  const detailProcess = detailQ.data?.process;
  const detailSteps =
    detailProcess?.steps ?? detailQ.data?.chain_entities ?? flow.steps;
  const sourceSnippets =
    detailProcess?.source_snippets ??
    detailQ.data?.source_snippets ??
    flow.source_snippets;
  const fullFlow: Process = {
    ...flow,
    ...(detailProcess ?? {}),
    source_snippets: sourceSnippets,
    steps: detailSteps,
  };

  const selectedStep =
    selectedStepIdx != null && fullFlow.steps
      ? fullFlow.steps[selectedStepIdx]
      : null;

  function copy(text: string, label: string) {
    void navigator.clipboard.writeText(text).then(
      () => toast.success(`Copied ${label}`),
      () => toast.error("Couldn't access clipboard"),
    );
  }

  return (
    <div className="flex flex-col h-full bg-bg overflow-hidden" key={flow.process_id}>
      {/* Sticky header */}
      <header className="flex flex-col gap-1.5 px-5 py-3.5 border-b border-border bg-surface flex-none">
        <div className="flex items-center gap-2 font-mono text-[var(--text-md)] text-text font-medium">
          {(() => {
            const { Icon } = getEntryMeta(flow.entry_kind);
            return <Icon size={14} />;
          })()}
          <span className="flex-1 min-w-0 break-words leading-snug">
            <FlowLabel label={flow.label} />
          </span>
          <button
            type="button"
            onClick={onClose}
            title="Close"
            className="w-6 h-6 flex items-center justify-center rounded-sm text-text-3 hover:bg-surface-2 hover:text-text flex-none"
          >
            <X size={12} />
          </button>
        </div>

        {/* Chip row */}
        <div className="flex flex-wrap gap-1.5">
          <RepoChip name={flow.repo} />
          <span className="inline-flex h-[22px] px-2 rounded-sm bg-surface-2 border border-border font-mono text-[10px] text-text-2">
            {flow.step_count} steps
          </span>
          {flow.cross_stack && <CrossStackChip />}
          {!flow.cross_stack && flow.is_cross_repo && <CrossRepoChip />}
          {flow.crosses_external_lib && (
            <span className="inline-flex h-[22px] px-2 rounded-sm bg-surface-2 border border-dashed border-border font-mono text-[10px] text-text-3">
              external lib
            </span>
          )}
          {flow.terminal_is_phantom && (
            <span
              className="inline-flex h-[22px] px-2 rounded-sm font-mono text-[10px]"
              style={{
                color: "var(--warning)",
                background: "color-mix(in srgb, var(--warning) 14%, transparent)",
              }}
            >
              phantom terminal
            </span>
          )}
          {typeof flow.complexity_score === "number" && (
            <span className="inline-flex h-[22px] px-2 rounded-sm bg-surface-2 border border-border font-mono text-[10px] text-text-2">
              complexity {flow.complexity_score.toFixed(1)}
            </span>
          )}
          {flow.docgen_status === "enriched" && (
            <span
              className="inline-flex items-center gap-1 h-[22px] px-2 rounded-sm font-mono text-[10px]"
              style={{ color: "var(--info)", background: "var(--info-soft)" }}
            >
              <Sparkles size={10} /> enriched
            </span>
          )}
          {flow.docgen_status === "stale" && (
            <span
              className="inline-flex h-[22px] px-2 rounded-sm font-mono text-[10px]"
              style={{ color: "var(--warning)", background: "var(--warning-soft)" }}
            >
              stale insights
            </span>
          )}
        </div>

        {/* Action row */}
        <div className="flex flex-wrap gap-1.5 mt-1">
          <button
            type="button"
            onClick={() => copy(flow.chain_labels.join(" → "), "chain")}
            className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-sm text-sm border border-border bg-surface text-text-2 hover:bg-surface-2 hover:text-text"
          >
            <Copy size={12} /> Copy chain
          </button>
          <button
            type="button"
            onClick={() => copy(flow.process_id, "process_id")}
            className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-sm text-sm border border-border bg-surface text-text-2 hover:bg-surface-2 hover:text-text"
          >
            <Copy size={12} /> process_id
          </button>
          <button
            type="button"
            onClick={() => copy(window.location.href, "share link")}
            className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-sm text-sm border border-border bg-surface text-text-2 hover:bg-surface-2 hover:text-text"
          >
            <Share2 size={12} /> Share
          </button>
          {(flow.flow_side_effects?.length ?? 0) > 0 && (
            <button
              type="button"
              onClick={() => setSideEffectsOpen((o) => !o)}
              className={cn(
                "inline-flex items-center gap-1.5 h-7 px-2.5 rounded-sm text-sm border border-border bg-surface text-text-2 hover:bg-surface-2 hover:text-text",
                sideEffectsOpen && "bg-surface-2 text-text",
              )}
              title={sideEffectsOpen ? "Hide side effects" : "Show side effects"}
            >
              <AlertTriangle size={12} />
              Side effects
              <span className="font-mono text-[10px] text-text-3 ml-0.5">
                {flow.flow_side_effects!.length}
              </span>
            </button>
          )}
          {flow.enrichment?.linked_endpoint_id && (
            <button
              type="button"
              onClick={() => toast.info("Endpoint link — not wired yet.")}
              className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-sm text-sm border border-border bg-surface text-text-2 hover:bg-surface-2 hover:text-text"
            >
              <ExternalLink size={12} /> Endpoint
            </button>
          )}
          {flow.enrichment?.linked_topic_id && (
            <button
              type="button"
              onClick={() => toast.info("Topic link — not wired yet.")}
              className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-sm text-sm border border-border bg-surface text-text-2 hover:bg-surface-2 hover:text-text"
            >
              <ExternalLink size={12} /> Topic
            </button>
          )}

          {/* Replay controls (#1922) — between layout toggle and other actions */}
          {controller && (
            <ReplayControls
              controller={controller}
              totalSteps={totalSteps}
              speedKey={speedKey}
              onSpeedChange={updateSpeed}
              audioOn={audioOn}
              onAudioToggle={updateAudio}
            />
          )}

          {/* Layout toggle (#1907) — segmented control, right-justified */}
          <div
            className="ml-auto inline-flex items-center bg-surface-2 border border-border rounded-sm overflow-hidden h-7"
            role="group"
            aria-label="DAG layout"
          >
            {(
              [
                { key: "auto" as UserLayout,       label: "Auto",  Icon: undefined },
                { key: "horizontal" as UserLayout,  label: "H",    Icon: ArrowRight },
                { key: "vertical" as UserLayout,    label: "V",    Icon: Rows2 },
                { key: "grid" as UserLayout,        label: "Grid", Icon: Grid2x2 },
              ]
            ).map(({ key, label, Icon }) => (
              <button
                key={key}
                type="button"
                title={
                  key === "auto"       ? "Adaptive (default)"
                  : key === "horizontal" ? "Horizontal — single row"
                  : key === "vertical"   ? "Vertical — single column"
                  : "Grid — 3-column"
                }
                onClick={() => applyLayout(key)}
                className={cn(
                  "px-2 h-full text-[10px] font-medium border-r last:border-0 border-border inline-flex items-center gap-1",
                  userLayout === key
                    ? "bg-surface text-text shadow-[inset_0_-2px_0_var(--accent)]"
                    : "text-text-3 hover:text-text",
                )}
              >
                {Icon ? <Icon size={11} /> : null}
                {label}
              </button>
            ))}
          </div>
        </div>
      </header>

      {/* Body — scrollable */}
      <div className="flex-1 min-h-0 overflow-y-auto flex flex-col">
        {/* DAG skeleton */}
        {detailQ.isLoading && !fullFlow.steps?.length && (
          <div className="flex items-center justify-center min-h-[280px] text-text-4 text-sm animate-pulse">
            Loading flow chain…
          </div>
        )}

        {/* DAG canvas — when a step is selected, switch to 60/40 side-by-side.
            The DAG takes 60% and the StepInspector takes 40%. The side-effects
            panel remains floating (anchored bottom-left inside the DAG area)
            and is only shown when no step is selected. */}
        {(fullFlow.steps?.length ?? 0) > 0 && (
          <div
            className={cn(
              "flex-none",
              selectedStep ? "flex" : "relative",
            )}
            style={selectedStep ? { minHeight: 400 } : undefined}
          >
            {/* DAG — 60% when step is open, full-width otherwise */}
            <div
              className={cn(
                "relative",
                selectedStep ? "flex-none" : "w-full",
              )}
              style={selectedStep ? { width: "60%" } : undefined}
            >
              <FlowDagWithAnim
                flow={fullFlow}
                detailSteps={fullFlow.steps}
                selectedStepIdx={selectedStepIdx}
                onPickStep={(idx) => {
                  setSelectedStepIdx(idx);
                  // Selecting a step dismisses the side-effects panel so both
                  // don't conflict at once.
                  setSideEffectsOpen(false);
                }}
                userLayout={userLayout}
                controller={controller}
                reducedMotion={reducedMotion}
                onBridgeMap={(map) => {
                  bridgeRef.current = map;
                }}
              />
              {/* Scrubber — directly under the DAG, only when steps exist. */}
              {controller && (fullFlow.steps?.length ?? 0) >= 2 && (
                <ProgressScrubber
                  controller={controller}
                  totalSteps={fullFlow.steps?.length ?? 0}
                  stepLabels={(fullFlow.steps ?? []).map((s) => stepName(s))}
                />
              )}
              {/* Floating side-effects panel — only when toggled open and no
                  step is selected (consistent with previous #1895 behavior). */}
              {sideEffectsOpen && !selectedStep && (
                <SideEffectsPanel
                  flow={fullFlow}
                  onClose={() => setSideEffectsOpen(false)}
                />
              )}
            </div>

            {/* Step inspector side panel — 40% when a step is selected */}
            {selectedStep && (
              <div className="flex-1 min-w-0 overflow-hidden">
                <StepInspector
                  step={selectedStep}
                  flow={fullFlow}
                  totalSteps={fullFlow.steps?.length ?? fullFlow.step_count}
                  onClose={() => setSelectedStepIdx(null)}
                />
              </div>
            )}
          </div>
        )}

        {/* Sections */}
        <DetailSections flow={fullFlow} />
      </div>
    </div>
  );
}

// ─── Main screen ──────────────────────────────────────────────────────────────

export default function FlowsScreen() {
  const { groupId = "" } = useParams<{ groupId: string }>();
  const [searchParams, setSearchParams] = useSearchParams();

  const tab = (searchParams.get("tab") ?? "all") as TabKey;

  const [selection, setSelection] = useState<Selection | null>(null);

  // Prefetch all flows to get counts for all tabs
  const flowsQ = useFlows(groupId, "all", "");
  const deadEndsQ = useFlowDeadEnds(groupId, true);
  const truncatedQ = useFlowTruncated(groupId, true);

  const allFlows = flowsQ.data?.processes ?? [];
  const crossRepoFlows = allFlows.filter((f) => f.is_cross_repo || f.cross_stack);

  const counts = {
    all: allFlows.length,
    crossrepo: crossRepoFlows.length,
    deadends: deadEndsQ.data?.dead_ends?.length ?? 0,
    truncated: truncatedQ.data?.processes?.length ?? 0,
  };

  function setTab(t: TabKey) {
    const next = new URLSearchParams(searchParams);
    next.set("tab", t);
    if (t === "deadends" || t === "truncated") {
      next.delete("process_id");
      setSelection(null);
    }
    setSearchParams(next, { replace: true });
  }

  function handleSelectFlow(f: Process) {
    setSelection({ kind: "flow", flow: f });
    const next = new URLSearchParams(searchParams);
    next.set("process_id", f.process_id);
    setSearchParams(next, { replace: true });
  }

  function handleSelectDeadEnd(de: FlowDeadEnd) {
    setSelection({ kind: "deadend", de });
    const next = new URLSearchParams(searchParams);
    next.set("process_id", de.process_id);
    setSearchParams(next, { replace: true });
  }

  function handleClose() {
    setSelection(null);
    const next = new URLSearchParams(searchParams);
    next.delete("process_id");
    setSearchParams(next, { replace: true });
  }

  const TABS: { key: TabKey; label: string }[] = [
    { key: "all", label: "All Flows" },
    { key: "crossrepo", label: "Cross-repo" },
    { key: "deadends", label: "Dead-ends" },
    { key: "truncated", label: "Truncated" },
  ];

  const tabCounts: Record<TabKey, number | "…"> = {
    all: flowsQ.isLoading ? "…" : counts.all,
    crossrepo: flowsQ.isLoading ? "…" : counts.crossrepo,
    deadends: deadEndsQ.isLoading ? "…" : counts.deadends,
    truncated: truncatedQ.isLoading ? "…" : counts.truncated,
  };

  const selectedId =
    selection?.kind === "flow"
      ? selection.flow.process_id
      : selection?.kind === "deadend"
        ? selection.de.process_id
        : null;

  const showDetail = selection !== null;

  return (
    <div className="flex flex-col h-full min-h-0 bg-bg overflow-hidden">
      {/* Tab strip */}
      <div className="flex items-stretch px-5 bg-surface border-b border-border flex-none min-h-[42px]">
        {TABS.map((t) => (
          <button
            key={t.key}
            type="button"
            onClick={() => setTab(t.key)}
            className={cn(
              "inline-flex items-center gap-2 px-3.5 h-[42px] text-[var(--text-base)] font-medium whitespace-nowrap",
              "border-b-2 transition-colors duration-100",
              "focus-visible:outline-none focus-visible:ring-inset focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
              tab === t.key
                ? "text-text border-accent"
                : "text-text-3 border-transparent hover:text-text-2",
            )}
          >
            {t.label}
            <span
              className={cn(
                "font-mono text-[11px] px-1.5 py-0.5 rounded-full border",
                tab === t.key
                  ? "bg-accent-soft text-accent-strong border-transparent"
                  : "bg-surface-2 text-text-3 border-border",
              )}
            >
              {tabCounts[t.key]}
            </span>
          </button>
        ))}
      </div>

      {/* Workspace grid */}
      <div
        className="flex-1 min-h-0 overflow-hidden"
        style={{
          display: "grid",
          gridTemplateColumns: showDetail ? "400px 1fr" : "400px 1fr",
          // Constrain the single grid row to the workspace height. Without an
          // explicit row track the implicit row is `auto` and grows to fit the
          // (tall) flow list, so the inner overflow-y-auto never activates and
          // the list can't scroll. minmax(0,1fr) lets children shrink + scroll.
          gridTemplateRows: "minmax(0, 1fr)",
        }}
      >
        {/* List rail — always rendered (hidden on narrow when detail is open) */}
        <div
          className={cn(
            "min-h-0 h-full overflow-hidden",
            showDetail ? "max-[1180px]:hidden" : "",
          )}
        >
          <ListRail
            groupId={groupId}
            tab={tab}
            selectedId={selectedId}
            onSelectFlow={handleSelectFlow}
            onSelectDeadEnd={handleSelectDeadEnd}
            counts={counts}
          />
        </div>

        {/* Detail panel */}
        <div className="min-h-0 h-full overflow-hidden">
          <DetailPanel
            selection={selection}
            groupId={groupId}
            onClose={handleClose}
          />
        </div>
      </div>
    </div>
  );
}
