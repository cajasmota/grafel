/* ============================================================
   routes/event-flows.tsx — Event Flows screen (#1944 Phase 1)

   Multi-hop pub/sub chains seeded by Channel entities
   (SCOPE.MessageTopic / SCOPE.EventBusEvent). The wire shape mirrors
   /api/flows on the Process-Flow side (same chain / branches_dag /
   step_count keys) so the existing Flows DAG renderer can drive both
   views once the renderer is extracted (#2028 already prepared the
   contract; renderer extraction itself is a follow-up).

   Phase 1 layout (intentionally minimal):
     - Left rail: list of event-flow chains, sorted by channel count
       then step count.
     - Right pane: chain steps rendered as a vertical strip with
       channel nodes coloured distinctly from operation nodes; the
       raw branches_dag payload is exposed via "View DAG JSON" so the
       full DAG renderer (#2028 / flows.tsx) can adopt it without an
       API change.

   Out of scope for Phase 1:
     - DAG branching renderer reuse — Phase 2 (event branching) lands
       on top of #1945 once the per-language extractors emit branch
       reasons for pub/sub.
     - Cross-stack walker — Phase 3 (#1902/#1913 bridges).
     - Conditional routing — Phase 5.
   ============================================================ */

import { useState, useMemo } from "react";
import { useParams, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Radio, Workflow, ChevronRight, Copy } from "lucide-react";
import { toast } from "sonner";

import { api } from "@/lib/api";
import { Badge, Skeleton } from "@/components/ui";
import { useSourcePeek } from "@/components/SourcePeek";
import { cn } from "@/lib/utils";

import type {
  EventFlowListItem,
  EventFlowDetailResponse,
  EventFlowStep,
} from "@/data/types";

// ---------------------------------------------------------------------------
// Hooks — both wrap api.ts so auth headers / base URL are centralised.
// ---------------------------------------------------------------------------

function useEventFlows(groupId: string | undefined) {
  return useQuery({
    queryKey: ["event-flows", groupId],
    queryFn: () => api.listEventFlows(groupId!, { limit: 200 }),
    enabled: Boolean(groupId),
  });
}

function useEventFlowDetail(groupId: string | undefined, eventFlowId: string | null) {
  return useQuery({
    queryKey: ["event-flow-detail", groupId, eventFlowId],
    queryFn: () => api.getEventFlowDetail(groupId!, eventFlowId!),
    enabled: Boolean(groupId) && Boolean(eventFlowId),
  });
}

// ---------------------------------------------------------------------------
// Presentation — channel vs operation node tokens
// ---------------------------------------------------------------------------

function StepNode({ step }: { step: EventFlowStep }) {
  const isChannel = step.is_channel;
  const { openSourcePeek } = useSourcePeek();
  const { groupId = "" } = useParams<{ groupId: string }>();
  return (
    <div
      className={cn(
        "flex items-center gap-3 rounded-md border px-3 py-2",
        isChannel
          ? "border-[var(--pastel-2)] bg-[var(--pastel-2)]/15"
          : "border-border bg-surface-1",
      )}
    >
      <div
        className={cn(
          "flex h-7 w-7 shrink-0 items-center justify-center rounded-full",
          isChannel
            ? "bg-[var(--pastel-2)] text-[var(--pastel-2-ink)]"
            : "bg-surface-2 text-text-2",
        )}
      >
        {isChannel ? <Radio size={14} /> : <Workflow size={14} />}
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium text-text-1">{step.label || step.entity_id}</div>
        <div className="truncate text-xs text-text-3">
          {isChannel ? "channel" : "operation"} · {step.repo}
          {step.source_file && (
            <>
              {" · "}
              <button
                type="button"
                onClick={() =>
                  groupId &&
                  openSourcePeek({
                    groupId,
                    file: step.source_file!,
                    line: step.start_line ?? 0,
                    repo: step.repo,
                  })
                }
                className="text-accent hover:underline cursor-pointer"
                title={`${step.source_file}${step.start_line ? `:${step.start_line}` : ""} — open source`}
              >
                {step.source_file}
                {step.start_line ? `:${step.start_line}` : ""}
              </button>
            </>
          )}
        </div>
      </div>
      <span className="text-xs text-text-4">#{step.step_index}</span>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Left rail — list of chains
// ---------------------------------------------------------------------------

function ChainListItem({
  item,
  selected,
  onClick,
}: {
  item: EventFlowListItem;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "w-full rounded-md border px-3 py-2 text-left transition-colors",
        selected
          ? "border-[var(--accent)] bg-surface-2"
          : "border-border bg-surface-1 hover:bg-surface-2",
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="truncate text-sm font-medium text-text-1">{item.label}</span>
        <ChevronRight size={14} className="shrink-0 text-text-4" />
      </div>
      <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-text-3">
        <Badge tone="neutral">{item.repo}</Badge>
        <span>{item.channel_count} channels</span>
        <span>·</span>
        <span>{item.step_count} steps</span>
      </div>
    </button>
  );
}

// ---------------------------------------------------------------------------
// Right pane — selected chain detail
// ---------------------------------------------------------------------------

function ChainDetail({ detail }: { detail: EventFlowDetailResponse }) {
  const [showDAG, setShowDAG] = useState(false);

  function copyDAG() {
    navigator.clipboard.writeText(detail.branches_dag || "");
    toast.success("DAG JSON copied to clipboard");
  }

  return (
    <div className="flex h-full flex-col gap-4">
      <header className="flex items-center justify-between gap-4">
        <div className="min-w-0">
          <h2 className="truncate text-base font-semibold text-text-1">{detail.label}</h2>
          <p className="text-xs text-text-3">
            seed <span className="font-mono text-text-2">{detail.seed_name || detail.seed_id}</span>
            {" · "}{detail.channel_count} channels{" · "}{detail.step_count} steps
          </p>
        </div>
        <button
          type="button"
          onClick={() => setShowDAG((v) => !v)}
          className="rounded-md border border-border bg-surface-1 px-2.5 py-1 text-xs text-text-2 hover:bg-surface-2"
        >
          {showDAG ? "Hide" : "View"} DAG JSON
        </button>
      </header>

      <div className="flex flex-col gap-2">
        {(detail.steps ?? []).map((step, idx) => (
          <div key={`${step.entity_id}-${idx}`} className="flex flex-col gap-2">
            <StepNode step={step} />
            {idx < (detail.steps ?? []).length - 1 && (
              <div className="ml-3.5 h-3 w-px bg-border" aria-hidden />
            )}
          </div>
        ))}
      </div>

      {showDAG && detail.branches_dag && (
        <div className="rounded-md border border-border bg-surface-1 p-3">
          <div className="mb-2 flex items-center justify-between">
            <span className="text-xs font-medium text-text-2">branches_dag</span>
            <button
              type="button"
              onClick={copyDAG}
              className="flex items-center gap-1 text-xs text-text-3 hover:text-text-1"
            >
              <Copy size={12} /> Copy
            </button>
          </div>
          <pre className="max-h-72 overflow-auto text-xs text-text-3">
            {prettyJSON(detail.branches_dag)}
          </pre>
          <p className="mt-2 text-[11px] text-text-4">
            Shape matches ProcessFlow.branches_dag (#2028); the shared Flows DAG renderer
            consumes this payload unchanged.
          </p>
        </div>
      )}
    </div>
  );
}

function prettyJSON(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}

// ---------------------------------------------------------------------------
// Screen
// ---------------------------------------------------------------------------

export default function EventFlowsScreen() {
  const { groupId } = useParams<{ groupId: string }>();
  const [search, setSearch] = useSearchParams();
  const selectedId = search.get("flow");
  const seedFilter = search.get("seed");

  const { data, isLoading, error } = useEventFlows(groupId);
  const detail = useEventFlowDetail(groupId, selectedId);

  const items = useMemo<EventFlowListItem[]>(() => {
    const all = data?.event_flows ?? [];
    if (!seedFilter) return all;
    return all.filter(
      (i) => i.seed_id === seedFilter || i.event_flow_id === seedFilter,
    );
  }, [data, seedFilter]);

  function selectFlow(id: string | null) {
    const next = new URLSearchParams(search);
    if (id) next.set("flow", id);
    else next.delete("flow");
    setSearch(next, { replace: true });
  }

  return (
    <div className="flex h-full w-full gap-4 p-4">
      {/* Left rail */}
      <aside className="flex w-[380px] shrink-0 flex-col gap-3 rounded-lg border border-border bg-surface-0 p-3">
        <header className="flex items-center justify-between">
          <h1 className="text-sm font-semibold text-text-1">Event Flows</h1>
          <Badge tone="neutral">{items.length}</Badge>
        </header>
        {seedFilter && (
          <button
            type="button"
            onClick={() => {
              const next = new URLSearchParams(search);
              next.delete("seed");
              setSearch(next, { replace: true });
            }}
            className="rounded-md border border-border bg-surface-1 px-2 py-1 text-xs text-text-2 hover:bg-surface-2"
          >
            Clear seed filter ({seedFilter})
          </button>
        )}
        <div className="flex flex-col gap-2 overflow-auto">
          {isLoading && (
            <>
              <Skeleton className="h-14 w-full" />
              <Skeleton className="h-14 w-full" />
              <Skeleton className="h-14 w-full" />
            </>
          )}
          {error && (
            <p className="text-xs text-text-3">Failed to load event flows.</p>
          )}
          {!isLoading && !error && items.length === 0 && (
            <p className="text-xs text-text-3">
              No multi-hop pub/sub chains found in this group yet. Phase 1 walker
              seeds from MessageTopic and EventBusEvent channels — index a repo
              with Kafka, EventBridge, or similar pub/sub edges to populate.
            </p>
          )}
          {items.map((it) => (
            <ChainListItem
              key={it.event_flow_id}
              item={it}
              selected={it.event_flow_id === selectedId}
              onClick={() => selectFlow(it.event_flow_id)}
            />
          ))}
        </div>
      </aside>

      {/* Right pane */}
      <main className="flex-1 overflow-auto rounded-lg border border-border bg-surface-0 p-4">
        {!selectedId && (
          <div className="flex h-full items-center justify-center">
            <p className="text-sm text-text-3">Select an event flow to view its chain.</p>
          </div>
        )}
        {selectedId && detail.isLoading && (
          <div className="flex flex-col gap-2">
            <Skeleton className="h-6 w-1/2" />
            <Skeleton className="h-14 w-full" />
            <Skeleton className="h-14 w-full" />
          </div>
        )}
        {selectedId && detail.error && (
          <p className="text-sm text-text-3">Failed to load event flow detail.</p>
        )}
        {selectedId && detail.data && <ChainDetail detail={detail.data} />}
      </main>
    </div>
  );
}
