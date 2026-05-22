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

import { useState, useMemo, useEffect, useRef } from "react";
import { useParams, useSearchParams } from "react-router-dom";
import {
  Globe, Database, Send, ArrowDownToLine, Wrench, Shield, AlertTriangle,
  Package, CheckCircle2, Layout, Terminal, Clock, TestTube2, Wifi,
  ChevronRight, Search, X, Copy, Share2, ExternalLink, ZoomIn, ZoomOut,
  Maximize2, ArrowUpDown, Download, Link2, Sparkles, Info, Check,
  type LucideProps,
} from "lucide-react";
import type { ForwardRefExoticComponent, RefAttributes } from "react";
import { toast } from "sonner";

import { useFlows, useFlowDeadEnds, useFlowTruncated, useFlowDetail } from "@/hooks/use-flows";
import type {
  Process, ProcessStep, FlowDeadEnd, EntryKind, StepKind, EntryKindGroup,
} from "@/data/types";
import { cn } from "@/lib/utils";
import { api } from "@/lib/api";

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

function FlowLabel({ label }: { label: string }) {
  const parts = label.split(" → ");
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

function RepoChip({ name }: { name: string }) {
  return (
    <span className="inline-flex items-center h-[18px] px-1.5 rounded-xs bg-surface-2 border border-border font-mono text-[10px] text-text-2">
      {name}
    </span>
  );
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
      <div className="w-[22px] h-[22px] rounded-xs bg-surface-2 animate-pulse flex-none" />
      <div className="flex-1 flex flex-col gap-1.5">
        <div className="h-3 w-3/4 rounded bg-surface-2 animate-pulse" />
        <div className="h-2.5 w-1/2 rounded bg-surface-2 animate-pulse" />
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
      arr = arr.filter((d) => d.process_name.toLowerCase().includes(q));
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
      className="flex flex-col border-r border-border bg-bg-soft min-h-0 min-w-0"
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

// ─── DAG constants ────────────────────────────────────────────────────────────

const NODE_W = 220;
const NODE_H = 58;
const NODE_VGAP = 36;
const CANVAS_PAD = 28;

function layoutDAG(steps: ProcessStep[]) {
  const positions = steps.map((_, i) => ({
    x: 0,
    y: CANVAS_PAD + i * (NODE_H + NODE_VGAP),
  }));
  const totalHeight = CANVAS_PAD * 2 + steps.length * (NODE_H + NODE_VGAP);
  const totalWidth = NODE_W + 100;
  return { positions, totalHeight, totalWidth };
}

// ─── FlowDag ──────────────────────────────────────────────────────────────────

function FlowDag({
  flow,
  detailSteps,
  selectedStepIdx,
  onPickStep,
}: {
  flow: Process;
  detailSteps?: ProcessStep[];
  selectedStepIdx: number | null;
  onPickStep: (i: number) => void;
}) {
  const steps = detailSteps ?? flow.steps ?? [];
  if (steps.length === 0) {
    return (
      <div className="flex items-center justify-center min-h-[280px] text-text-4 text-sm">
        No step data available.
      </div>
    );
  }

  const { positions, totalHeight, totalWidth } = layoutDAG(steps);
  const centerX = totalWidth / 2;

  const edges: Array<{
    from: { x: number; y: number };
    to: { x: number; y: number };
    kind: string | null;
    xrepo: boolean;
  }> = [];
  for (let i = 1; i < steps.length; i++) {
    edges.push({
      from: { x: centerX, y: positions[i - 1].y + NODE_H },
      to: { x: centerX, y: positions[i].y },
      kind: steps[i].edge_kind,
      xrepo: steps[i].repo !== steps[i - 1].repo,
    });
  }

  return (
    <div
      className="relative overflow-auto border-b border-border flex-none"
      style={{
        background:
          "radial-gradient(circle at 1px 1px, var(--canvas-grid) 1px, transparent 1px) 0 0 / 18px 18px, var(--canvas-bg)",
        minHeight: 280,
        maxHeight: 520,
      }}
    >
      <div
        className="relative mx-auto"
        style={{ width: totalWidth, height: totalHeight }}
      >
        {/* Edges */}
        <svg
          className="absolute inset-0 pointer-events-none"
          width={totalWidth}
          height={totalHeight}
        >
          <defs>
            <style>{`
              @media (prefers-reduced-motion: no-preference) {
                .fx-xedge { animation: fx-dash 0.9s linear infinite; }
              }
              @keyframes fx-dash { to { stroke-dashoffset: -16; } }
            `}</style>
          </defs>
          {edges.map((ed, i) => {
            const midY = (ed.from.y + ed.to.y) / 2;
            const path = `M ${ed.from.x} ${ed.from.y} L ${ed.from.x} ${midY - 6} L ${ed.to.x} ${midY + 6} L ${ed.to.x} ${ed.to.y}`;
            const stroke = ed.xrepo ? "#a78bfa" : "#475569";
            return (
              <g key={i}>
                <path
                  d={path}
                  fill="none"
                  stroke={stroke}
                  strokeWidth={1.4}
                  strokeDasharray={ed.xrepo ? "5 3" : undefined}
                  className={ed.xrepo ? "fx-xedge" : undefined}
                />
                <polygon
                  points={`${ed.to.x - 4},${ed.to.y - 6} ${ed.to.x + 4},${ed.to.y - 6} ${ed.to.x},${ed.to.y - 1}`}
                  fill={stroke}
                />
                {ed.kind && (
                  <text
                    x={ed.to.x + 8}
                    y={midY + 4}
                    fontSize={9}
                    fontFamily="var(--font-mono)"
                    fill={ed.xrepo ? "#a78bfa" : "var(--text-3)"}
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
        </svg>

        {/* Nodes */}
        {steps.map((s, i) => {
          const isEntry = i === 0;
          const isTerminal = i === steps.length - 1;
          const isPhantom = flow.terminal_is_phantom && isTerminal;
          const meta = getStepMeta(s.step_kind);
          const verb = isTerminal ? getHttpVerb(s.name) : null;
          const isXrepo = i > 0 && steps[i - 1].repo !== s.repo;
          const pos = positions[i];

          return (
            <button
              key={s.entity_id}
              type="button"
              onClick={() => onPickStep(i)}
              className={cn(
                "absolute flex flex-col gap-1 rounded-md border cursor-pointer text-left",
                "transition-shadow duration-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
                selectedStepIdx === i
                  ? "border-accent shadow-[0_0_0_2px_var(--accent-ring)]"
                  : "border-border hover:shadow-[var(--shadow-2)]",
                isPhantom ? "border-dashed opacity-85" : "",
              )}
              style={{
                left: centerX - NODE_W / 2,
                top: pos.y,
                width: NODE_W,
                minHeight: NODE_H,
                padding: "8px 12px 8px 14px",
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
                  className="font-mono text-[11px] text-text flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap"
                  title={s.name}
                >
                  {s.name}
                </span>
              </div>
              <div className="flex items-center justify-between gap-1">
                <span className="text-[9px] text-text-4">{meta.label}</span>
                <span
                  className="font-mono text-[9px]"
                  style={isXrepo ? { color: "#a78bfa" } : { color: "var(--text-3)" }}
                >
                  {s.repo}
                </span>
              </div>
            </button>
          );
        })}
      </div>

      {/* Canvas controls (decorative) */}
      <div className="absolute right-3 top-3 flex gap-1 rounded-md border border-border bg-[var(--overlay)] p-1 backdrop-blur-sm z-10">
        {[
          { icon: <Maximize2 size={12} />, title: "Fit to view" },
          { icon: <ZoomIn size={12} />, title: "Zoom in" },
          { icon: <ZoomOut size={12} />, title: "Zoom out" },
          { icon: <ArrowUpDown size={12} />, title: "Toggle direction" },
          { icon: <Download size={12} />, title: "Export PNG" },
        ].map((btn) => (
          <button
            key={btn.title}
            type="button"
            title={btn.title}
            className="w-6 h-6 inline-flex items-center justify-center rounded-sm text-text-3 hover:bg-surface-2 hover:text-text"
          >
            {btn.icon}
          </button>
        ))}
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
}: {
  step: ProcessStep;
  flow: Process;
  totalSteps: number;
}) {
  const meta = getStepMeta(step.step_kind);
  const snippet = flow.source_snippets?.[step.entity_id];

  return (
    <div className="border-t border-border bg-surface px-5 py-3.5 flex flex-col gap-2 max-h-[320px] overflow-y-auto flex-none">
      <div className="flex items-center gap-2">
        <span style={{ color: meta.color }}>
          <meta.Icon size={14} />
        </span>
        <span className="font-mono text-sm text-text font-medium flex-1 min-w-0 truncate">
          {step.name}
        </span>
        <span
          className="font-mono text-[9px] font-bold uppercase px-1.5 py-0.5 rounded-xs flex-none"
          style={{ background: meta.color, color: "var(--bg)" }}
        >
          {meta.label}
        </span>
        <span className="font-mono text-[10px] text-text-3 flex-none ml-auto">
          Step {step.step_index + 1} of {totalSteps}
        </span>
      </div>
      <div className="flex flex-wrap gap-2.5 text-[11px] text-text-3">
        <span>{step.kind}</span>
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

// ─── Detail sections ──────────────────────────────────────────────────────────

function DetailSections({ flow, groupId }: { flow: Process; groupId: string }) {
  const [genQueued, setGenQueued] = useState(false);
  const enr = flow.enrichment;
  const hasInsights =
    !!enr &&
    (enr.ai_summary || (enr.preconditions?.length ?? 0) > 0 || enr.expected_outcome);
  const status = flow.docgen_status;

  async function handleGenerate() {
    try {
      await api.generateFlowDocs(groupId, flow.process_id);
      setGenQueued(true);
      toast.success("Queued — insights will appear after the next docgen run.");
    } catch {
      toast.error("Failed to queue enrichment.");
    }
  }

  return (
    <div className="px-5 pb-20 flex flex-col">
      {/* AI Insights */}
      <Section title="AI Insights" icon={<Sparkles size={11} />}>
        {status === "stale" && (
          <div
            className="flex items-center gap-2 p-2 rounded-sm text-[12px] mb-1"
            style={{ background: "var(--warning-soft)", color: "var(--warning)" }}
          >
            <Info size={11} />
            Insights may be out of date — index has changed since they were generated.
          </div>
        )}
        {genQueued ? (
          <p className="text-sm text-text-3 italic m-0">
            Queued — insights will appear after the next docgen run.
          </p>
        ) : hasInsights ? (
          <>
            {enr?.ai_summary && (
              <p className="text-sm text-text-2 leading-relaxed max-w-[72ch] m-0">{enr.ai_summary}</p>
            )}
            {(enr?.preconditions?.length ?? 0) > 0 && (
              <div>
                <div className="text-[10px] text-text-3 uppercase tracking-wide mb-1">
                  Preconditions
                </div>
                <ul className="list-none m-0 p-0 flex flex-col gap-1">
                  {enr!.preconditions!.map((p, i) => (
                    <li
                      key={i}
                      className="text-sm text-text-2 pl-3.5 relative"
                    >
                      <span className="absolute left-0 top-[8px] w-[5px] h-[5px] bg-text-4 rounded-full" />
                      {p}
                    </li>
                  ))}
                </ul>
              </div>
            )}
            {enr?.expected_outcome && (
              <div>
                <div className="text-[10px] text-text-3 uppercase tracking-wide mb-1">
                  Expected outcome
                </div>
                <div className="text-sm text-text-2 leading-relaxed">{enr.expected_outcome}</div>
              </div>
            )}
          </>
        ) : (
          <div className="flex items-center gap-2.5 p-3 bg-surface-2 rounded-sm text-sm text-text-2">
            <Sparkles size={14} className="text-text-3 flex-none" />
            <span className="flex-1">No AI insights yet.</span>
            <button
              type="button"
              onClick={() => void handleGenerate()}
              className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-sm text-sm bg-accent text-accent-text hover:bg-accent-strong ml-auto flex-none"
            >
              Generate insights
            </button>
          </div>
        )}
      </Section>

      {/* Side effects */}
      <Section
        title="Side effects"
        count={flow.flow_side_effects?.length ?? 0}
        icon={<AlertTriangle size={11} />}
        defaultOpen
      >
        {(flow.flow_side_effects?.length ?? 0) > 0 ? (
          <ul className="list-none m-0 p-0 flex flex-col gap-1">
            {flow.flow_side_effects!.map((s, i) => (
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
      </Section>

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
                {de.process_name.split(" → ")[0]}
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

  const selId = selection?.kind === "flow" ? selection.flow.process_id : null;
  useEffect(() => {
    setSelectedStepIdx(null);
  }, [selId]);

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape" && selection) {
        onClose();
      }
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

        {/* DAG canvas */}
        {(fullFlow.steps?.length ?? 0) > 0 && (
          <FlowDag
            flow={fullFlow}
            detailSteps={fullFlow.steps}
            selectedStepIdx={selectedStepIdx}
            onPickStep={setSelectedStepIdx}
          />
        )}

        {/* Step inspector */}
        {selectedStep && (
          <StepInspector
            step={selectedStep}
            flow={fullFlow}
            totalSteps={fullFlow.steps?.length ?? fullFlow.step_count}
          />
        )}

        {/* Sections */}
        <DetailSections flow={fullFlow} groupId={groupId} />
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
        }}
      >
        {/* List rail — always rendered (hidden on narrow when detail is open) */}
        <div
          className={cn(
            "min-h-0 overflow-hidden",
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
        <div className="min-h-0 overflow-hidden">
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
