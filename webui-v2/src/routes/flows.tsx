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

import { useState, useEffect, useMemo, useCallback, useRef } from "react";
import { useParams, useSearchParams } from "react-router-dom";
import {
  Globe, Database, Send, ArrowDownToLine, Wrench, Shield, AlertTriangle,
  Package, CheckCircle2, Layout, Terminal, Clock, TestTube2, Wifi,
  ChevronRight, Search, X, Copy, Share2, ExternalLink,
  Link2, Sparkles, Info, Check,
  type LucideProps,
} from "lucide-react";
import type { ForwardRefExoticComponent, RefAttributes } from "react";
import { toast } from "sonner";

import { useFlows, useFlowDeadEnds, useFlowTruncated, useFlowDetail } from "@/hooks/use-flows";
import type {
  Process, ProcessStep, FlowDeadEnd, EntryKind, StepKind, EntryKindGroup,
  DownstreamDAGNode,
} from "@/data/types";
import { cn } from "@/lib/utils";
import { RepoChip as SharedRepoChip } from "@/lib/repo-color";
import { Skeleton } from "@/components/ui/skeleton";
import { useSetInsight } from "@/components/ui";
import type { InsightValue } from "@/components/ui";
import { FlowDag as SharedFlowDag } from "@/components/flow-dag";
import { useSourcePeek } from "@/components/SourcePeek";
import { flowToDagPayload } from "@/lib/flow-to-dag";

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
          {/* DAG chip in list row (#2027) */}
          {flow.is_dag && (
            <span
              className="inline-flex items-center h-[18px] px-1.5 rounded-xs font-mono text-[10px] font-semibold"
              style={{
                color: "var(--ag-flow-branch, #f59e0b)",
                background: "color-mix(in srgb, var(--ag-flow-branch, #f59e0b) 12%, transparent)",
              }}
              title="Branched DAG flow"
            >
              branched
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

// ─── FlowDag — shared React Flow DAG renderer (#4354) ─────────────────────────
//
// The bespoke 3.3k-line SVG renderer (custom layoutDAG / edge-geometry /
// flow-animation) was replaced by the shared <FlowDag> component
// (components/flow-dag/, PR #4356). That renderer is dagre-laid-out React Flow
// and renders the REAL branching DAG — the old SVG flattened every flow to its
// primary path and could not draw fan-out arms.
//
// The flows DETAIL endpoint now emits the DownstreamDAGResponse payload the
// shared component consumes directly (flow.flow_dag, #4363) — built server-side
// from the same steps + branches_dag the old client adapter reshaped, with
// first-class roles + semantic side-edges. lib/flow-to-dag.ts is retained only
// as a back-compat fallback for an older daemon that doesn't emit flow_dag. Node
// clicks drive the existing StepInspector; the H/V layout toggle now lives
// inside the shared controls bar.
//
// REPLAY (#4362): the step-replay / comet animation + scrubber (originally
// #1922) — dropped in the #4354 React Flow migration because it was welded to
// the old SVG path geometry — is re-implemented on React Flow inside the shared
// <FlowDag enableReplay> component. It reuses the still-present
// lib/flow-animation.ts controller + lib/flow-audio.ts blip, walks the laid-out
// tree in topological order, glows the active node/edge, and rides a comet
// (sampled off the real bezier edge geometry via getPointAtLength) along each
// edge. Enabled here via the `enableReplay` prop below.

function FlowDag({
  flow,
  detailSteps,
  selectedStepIdx,
  onPickStep,
  groupId = "",
}: {
  flow: Process;
  detailSteps?: ProcessStep[];
  selectedStepIdx: number | null;
  onPickStep: (i: number | null) => void;
  groupId?: string;
}) {
  const steps = detailSteps ?? flow.steps;

  // #4363 — the flows DETAIL endpoint now emits the FlowDag payload server-side
  // (flow.flow_dag), built from the same steps + branches_dag the client adapter
  // used, with the same "flow-step-<index>" node ids the selection/replay/click
  // wiring below keys on. Consume it directly; fall back to the client-side
  // flowToDagPayload adapter only for an older daemon that doesn't emit flow_dag.
  const payload = useMemo(
    () => flow.flow_dag ?? flowToDagPayload(flow, steps),
    [flow, steps],
  );

  // Map the selected step index → its DAG node id so the shared renderer can
  // highlight it. The mapper keys node ids on step_index (see flow-to-dag.ts).
  const selectedNodeId =
    selectedStepIdx != null ? `flow-step-${selectedStepIdx}` : null;

  const handleNodeClick = useCallback(
    (node: DownstreamDAGNode) => {
      // Node ids are "flow-step-<index>"; recover the index and toggle it.
      const m = /^flow-step-(\d+)$/.exec(node.id);
      if (!m) return;
      const idx = Number(m[1]);
      onPickStep(selectedStepIdx === idx ? null : idx);
    },
    [onPickStep, selectedStepIdx],
  );

  if (!payload) {
    return (
      <div className="flex-1 min-h-0 flex items-center justify-center text-sm text-text-4">
        No resolved steps to visualise for this flow.
      </div>
    );
  }

  return (
    <SharedFlowDag
      groupId={groupId}
      payload={payload}
      onNodeClick={handleNodeClick}
      selectedNodeId={selectedNodeId}
      enableReplay
      className="flex-1 min-h-0"
    />
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
  const { openSourcePeek } = useSourcePeek();
  const { groupId = "" } = useParams<{ groupId: string }>();

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
        {step.source_file ? (
          <button
            type="button"
            onClick={() =>
              groupId &&
              openSourcePeek({
                groupId,
                file: step.source_file,
                line: step.start_line ?? 0,
                repo: step.repo,
              })
            }
            className="font-mono text-accent hover:underline cursor-pointer"
            title={`${step.source_file}${step.start_line ? `:${step.start_line}` : ""} — open source`}
          >
            {step.source_file}
            {step.start_line ? `:${step.start_line}` : ""}
          </button>
        ) : (
          <span className="font-mono">{step.source_file}</span>
        )}
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

  const selId = selection?.kind === "flow" ? selection.flow.process_id : null;
  useEffect(() => {
    setSelectedStepIdx(null);
    setSideEffectsOpen(false);
  }, [selId]);

  // Esc closes the detail panel. The shared <FlowDag> owns its own canvas
  // interaction (pan/zoom), so there's no in-flight replay to intercept.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key !== "Escape" || !selection) return;
      onClose();
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [selection, onClose]);

  const detailQ = useFlowDetail(
    groupId,
    selection?.kind === "flow" ? selection.flow.process_id : null,
  );

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
          {/* DAG chip — shown when the flow has branch points (#2027) */}
          {flow.is_dag && (
            <span
              className="inline-flex items-center gap-1 h-[22px] px-2 rounded-sm font-mono text-[10px] font-semibold"
              style={{
                color: "var(--ag-flow-branch, #f59e0b)",
                background: "color-mix(in srgb, var(--ag-flow-branch, #f59e0b) 14%, transparent)",
              }}
              title="This flow has fan-out branch points"
            >
              DAG / branched
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
          {/* The DAG layout toggle (H/V) + spine/full + depth now live inside
              the shared <FlowDag> controls bar (#4354), so the header no longer
              owns its own layout segmented control. */}
        </div>
      </header>

      {/* Body — #2115 fix: split into two sections so the DAG fills available
          height. The DAG area is flex-1 min-h-0 (flex child that grows to fill
          the panel). The sections below it scroll independently. */}
      <div className="flex-1 min-h-0 flex flex-col overflow-hidden">
        {/* DAG skeleton */}
        {detailQ.isLoading && !fullFlow.steps?.length && (
          <div className="flex items-center justify-center min-h-[280px] text-text-4 text-sm animate-pulse flex-none">
            Loading flow chain…
          </div>
        )}

        {/* DAG canvas — #2115: flex-1 flex flex-col min-h-0 so the canvas fills
            the remaining panel height and FlowDag's own flex-1 takes effect.
            When a step is selected: 60/40 side-by-side split (same as before). */}
        {(fullFlow.steps?.length ?? 0) > 0 && (
          <div
            className={cn(
              // #2115 key fix: was "flex-none" — now flex-1 so the DAG grows.
              "flex-1 min-h-0 flex flex-col",
              selectedStep ? "flex-row" : "",
            )}
            style={selectedStep ? { flexDirection: "row" } : undefined}
          >
            {/* DAG — 60% when step is open, full-width otherwise */}
            <div
              className={cn(
                "relative flex flex-col flex-1 min-h-0",
                selectedStep ? "flex-none" : "",
              )}
              style={selectedStep ? { width: "60%" } : undefined}
            >
              <FlowDag
                flow={fullFlow}
                detailSteps={fullFlow.steps}
                selectedStepIdx={selectedStepIdx}
                onPickStep={(idx) => {
                  setSelectedStepIdx(idx);
                  // Selecting a step dismisses the side-effects panel so both
                  // don't conflict at once.
                  setSideEffectsOpen(false);
                }}
                groupId={groupId}
              />
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

        {/* Sections — scroll independently below the DAG */}
        <div className="flex-none overflow-y-auto">
          <DetailSections flow={fullFlow} />
        </div>
      </div>
    </div>
  );
}

// ─── Main screen ──────────────────────────────────────────────────────────────

// Screen insight (#4655) — registered with the breadcrumb Insights button via
// useSetInsight. Module-level constant for stable identity across renders.
const FLOWS_INSIGHT: InsightValue = {
  storageKey: "flows",
  human: (
    <>
      End-to-end process flows — each one traces a request from an entry
      point (an HTTP route, a queue consumer, a job) through every
      handler, service and side effect it touches, across repositories
      when the path is cross-stack. Dead-ends and truncated flows
      highlight where a trace stops short of completing.
    </>
  ),
  agent: {
    tool: "archigraph_traces",
    example:
      "Asked 'what happens when a user submits the checkout form?', an agent calls archigraph_traces from the POST /checkout entry point to walk the whole flow — validation, the payment-service call, the order DB write, the confirmation-email publish — and explains the end-to-end path instead of reading each file blind.",
  },
};

export default function FlowsScreen() {
  useSetInsight(FLOWS_INSIGHT);
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

      {/* Insight banner (#4604) */}
      <div className="flex-none px-5 pt-3">
        
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
